package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/entry/headless"
	"github.com/voocel/ainovel-cli/internal/entry/tui"
	"github.com/voocel/ainovel-cli/internal/logger"
	"github.com/voocel/ainovel-cli/internal/rules"
	buildversion "github.com/voocel/ainovel-cli/internal/version"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// headlessMode ghi lại liệu lần khởi động này có phải chế độ không giao diện hay không,
// để die quyết định có tạm dừng khi thoát lỗi hay không.
var headlessMode bool

func main() {
	opts, args, err := parseCLIOptions(os.Args[1:])
	if err != nil {
		die("flags: %v", err)
	}
	if opts.Version {
		buildversion.Print(os.Stdout, versionInfo())
		return
	}
	if opts.Update {
		if err := runSelfUpdate(opts.UpdateVersion); err != nil {
			fmt.Fprintf(os.Stderr, "update: %v\n", err)
			os.Exit(1)
		}
		return
	}
	headlessMode = opts.Headless

	// Khởi động lần đầu
	if bootstrap.NeedsSetup(opts.ConfigPath) {
		if opts.Headless {
			die("lỗi: chế độ không giao diện không hỗ trợ khởi động lần đầu, vui lòng chạy TUI một lần để hoàn tất cấu hình")
		}
		setupCfg, err := bootstrap.RunSetup()
		if err != nil {
			die("setup: %v", err)
		}
		// Tiếp tục với cấu hình đã tạo sau khi khởi động hoàn tất
		runWithConfig(setupCfg, opts, args)
		return
	}

	// Tải cấu hình
	cfg, err := bootstrap.LoadConfig(opts.ConfigPath)
	if err != nil {
		die("config: %v", err)
	}

	runWithConfig(cfg, opts, args)
}

// die xử lý thống nhất việc thoát do lỗi nghiêm trọng: in ra stderr, ghi vào ~/.ainovel/last-error.log,
// và tạm dừng chờ nhấn Enter trong terminal tương tác (không phải chế độ không giao diện) —
// khi khởi động bằng cách nhấp đúp, cửa sổ console sẽ đóng ngay khi tiến trình kết thúc,
// nếu không tạm dừng thì lỗi sẽ thoáng qua, đó chính là nguyên nhân người dùng không thể
// xác định sự cố như đã ghi nhận trong issue #37.
func die(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, msg)
	if path := bootstrap.WriteStartupError(msg); path != "" {
		fmt.Fprintf(os.Stderr, "（chi tiết lỗi đã được ghi vào %s）\n", path)
	}
	if path := logger.WriteError("startup", msg); path != "" {
		fmt.Fprintf(os.Stderr, "（nhật ký lỗi: %s）\n", path)
	}
	if !headlessMode && stdinIsTerminal() {
		fmt.Fprint(os.Stderr, "\nNhấn Enter để thoát...")
		fmt.Fscanln(os.Stdin)
	}
	os.Exit(1)
}

// stdinIsTerminal kiểm tra xem đầu vào chuẩn có được kết nối với terminal (thiết bị ký tự) hay không.
// Trả về true khi khởi động bằng nhấp đúp hoặc terminal tương tác;
// trả về false khi dùng pipe, chuyển hướng hoặc CI. Xấp xỉ không phụ thuộc,
// đủ để phân biệt có cần tạm dừng hay không.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func runWithConfig(cfg bootstrap.Config, opts cliOptions, args []string) {
	rules.EnsureHomeRulesDir()

	if len(args) > 0 {
		die("lỗi: không còn hỗ trợ truyền yêu cầu tiểu thuyết trực tiếp qua dòng lệnh, vui lòng khởi động rồi nhập trong ô nhập liệu TUI")
	}

	bundle := assets.Load(cfg.Style)
	if opts.Headless {
		prompt, err := loadPrompt(opts)
		if err != nil {
			die("lỗi: %v", err)
		}
		if err := headless.Run(cfg, bundle, headless.Options{Prompt: prompt}); err != nil {
			die("lỗi: %v", err)
		}
		return
	}
	if opts.Prompt != "" || opts.PromptFile != "" {
		die("lỗi: --prompt/--prompt-file chỉ có thể dùng trong chế độ --headless")
	}
	if err := tui.Run(cfg, bundle, versionInfo().Version); err != nil {
		die("lỗi: %v", err)
	}
}

type cliOptions struct {
	ConfigPath    string
	Headless      bool
	Prompt        string
	PromptFile    string
	Version       bool
	Update        bool
	UpdateVersion string
}

// parseCLIOptions trích xuất các flag CLI, trả về tùy chọn và các tham số còn lại.
func parseCLIOptions(argv []string) (cliOptions, []string, error) {
	var opts cliOptions
	var args []string
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--version", "-v":
			opts.Version = true
		case "version":
			if i+1 < len(argv) {
				return opts, nil, fmt.Errorf("version không nhận tham số")
			}
			opts.Version = true
		case "update":
			if opts.Update {
				return opts, nil, fmt.Errorf("update chỉ được chỉ định một lần")
			}
			opts.Update = true
			if i+1 < len(argv) {
				if strings.HasPrefix(argv[i+1], "-") {
					return opts, nil, fmt.Errorf("update chỉ nhận một tham số phiên bản tùy chọn")
				}
				opts.UpdateVersion = argv[i+1]
				i++
			}
			if i+1 < len(argv) {
				return opts, nil, fmt.Errorf("update chỉ nhận một tham số phiên bản tùy chọn")
			}
		case "--config":
			if i+1 >= len(argv) {
				return opts, nil, fmt.Errorf("--config thiếu giá trị")
			}
			opts.ConfigPath = argv[i+1]
			i++
		case "--headless":
			opts.Headless = true
		case "--prompt":
			if i+1 >= len(argv) {
				return opts, nil, fmt.Errorf("--prompt thiếu giá trị")
			}
			opts.Prompt = argv[i+1]
			i++
		case "--prompt-file":
			if i+1 >= len(argv) {
				return opts, nil, fmt.Errorf("--prompt-file thiếu giá trị")
			}
			opts.PromptFile = argv[i+1]
			i++
		default:
			args = append(args, argv[i])
		}
	}
	if opts.Prompt != "" && opts.PromptFile != "" {
		return opts, nil, fmt.Errorf("--prompt và --prompt-file không thể dùng đồng thời")
	}
	if opts.Version && (opts.Update || opts.ConfigPath != "" || opts.Headless || opts.Prompt != "" || opts.PromptFile != "" || len(args) > 0) {
		return opts, nil, fmt.Errorf("version không thể dùng chung với các tham số khởi động khác")
	}
	if opts.Update && (opts.ConfigPath != "" || opts.Headless || opts.Prompt != "" || opts.PromptFile != "" || len(args) > 0) {
		return opts, nil, fmt.Errorf("update không thể dùng chung với các tham số khởi động khác")
	}
	return opts, args, nil
}

func versionInfo() buildversion.Info {
	return buildversion.Resolve(buildversion.Info{
		Version: version,
		Commit:  commit,
		Date:    date,
	})
}

func runSelfUpdate(target string) error {
	info := versionInfo()
	result, err := buildversion.Update(context.Background(), buildversion.UpdateOptions{
		Repo:           "voocel/ainovel-cli",
		BinaryName:     "ainovel-cli",
		TargetVersion:  target,
		CurrentVersion: info.Version,
	})
	if err != nil {
		return err
	}
	if !result.Updated {
		fmt.Printf("ainovel-cli đã là phiên bản mới nhất %s\n", result.Version)
		return nil
	}
	fmt.Printf("ainovel-cli đã được cập nhật lên %s\n", result.Version)
	fmt.Printf("Vị trí cài đặt: %s\n", result.Path)
	return nil
}

func loadPrompt(opts cliOptions) (string, error) {
	if opts.PromptFile == "" {
		return strings.TrimSpace(opts.Prompt), nil
	}

	var data []byte
	var err error
	if opts.PromptFile == "-" {
		data, err = os.ReadFile("/dev/stdin")
	} else {
		data, err = os.ReadFile(opts.PromptFile)
	}
	if err != nil {
		return "", fmt.Errorf("đọc prompt thất bại: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
