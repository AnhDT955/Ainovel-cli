package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/host/exp"
)

// exportDoneMsg là kết quả cuối cùng của lệnh /export.
//
// Khác với /import chạy theo luồng sự kiện: xuất truyện là IO nội bộ đồng bộ, không có tiến độ trung gian;
// chạy xong trong goroutine thì gửi một lần thông điệp này.
type exportDoneMsg struct {
	result *exp.Result
	err    error
}

// startExport phân tích tham số và trả về tea.Cmd.
// Quá trình xuất truyện thực sự chạy trong tea.Cmd (tránh chặn UI), sau khi hoàn thành sẽ gửi exportDoneMsg.
func startExport(rt *host.Host, args []string) (tea.Cmd, error) {
	opts, err := parseExportArgs(args)
	if err != nil {
		return nil, err
	}
	return func() tea.Msg {
		// 30 giây đủ để ghi một tiểu thuyết vừa và dài ra đĩa; timeout chỉ là lưới an toàn tránh treo.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := rt.Export(ctx, opts)
		return exportDoneMsg{result: res, err: err}
	}, nil
}

// parseExportArgs phân tích `/export [txt|epub] [path] [from=N] [to=M] [--overwrite]`.
//
// Định dạng có thể chỉ định rõ theo ba cách: từ khoá vị trí `txt`/`epub`, tham số `format=txt|epub`,
// hoặc cờ tắt `--txt`/`--epub`. Khi không chỉ định, exp.Run tự suy ra từ hậu tố đường dẫn (mặc định TXT).
// Định dạng chỉ định rõ luôn thắng suy luận theo hậu tố.
//
// Tham số vị trí path: tối đa một, dùng làm đường dẫn đầu ra; mặc định do exp.Run quyết định ({novelDir}/{NovelName}.{ext}).
func parseExportArgs(args []string) (exp.Options, error) {
	var opts exp.Options
	for _, a := range args {
		if a == "--overwrite" {
			opts.Overwrite = true
			continue
		}
		if a == "--txt" || a == "--epub" {
			if err := setExportFormat(&opts, strings.TrimPrefix(a, "--")); err != nil {
				return exp.Options{}, err
			}
			continue
		}
		if k, v, ok := strings.Cut(a, "="); ok {
			switch strings.ToLower(k) {
			case "from":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return exp.Options{}, fmt.Errorf("from phải là số nguyên không âm: %q", v)
				}
				opts.From = n
			case "to":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return exp.Options{}, fmt.Errorf("to phải là số nguyên không âm: %q", v)
				}
				opts.To = n
			case "format":
				if err := setExportFormat(&opts, v); err != nil {
					return exp.Options{}, err
				}
			default:
				return exp.Options{}, fmt.Errorf("tham số không xác định %q (hỗ trợ: format / from / to)", k)
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			return exp.Options{}, fmt.Errorf("flag không xác định %q", a)
		}
		// Từ khoá định dạng dạng vị trí: cho phép `/export txt` / `/export epub` đọc tự nhiên.
		if lower := strings.ToLower(a); lower == string(exp.FormatTXT) || lower == string(exp.FormatEPUB) {
			if err := setExportFormat(&opts, lower); err != nil {
				return exp.Options{}, err
			}
			continue
		}
		if opts.OutPath != "" {
			return exp.Options{}, fmt.Errorf("chỉ hỗ trợ một tham số đường dẫn: %q", a)
		}
		opts.OutPath = a
	}
	return opts, nil
}

// setExportFormat gán định dạng xuất, từ chối giá trị không hợp lệ hoặc chỉ định mâu thuẫn hai lần.
func setExportFormat(opts *exp.Options, v string) error {
	var f exp.Format
	switch strings.ToLower(strings.TrimSpace(v)) {
	case string(exp.FormatTXT):
		f = exp.FormatTXT
	case string(exp.FormatEPUB):
		f = exp.FormatEPUB
	default:
		return fmt.Errorf("định dạng không hợp lệ %q (hỗ trợ: txt / epub)", v)
	}
	if opts.Format != "" && opts.Format != f {
		return fmt.Errorf("định dạng bị chỉ định mâu thuẫn: %q và %q", opts.Format, f)
	}
	opts.Format = f
	return nil
}

// formatExportSuccess render Result thành tóm tắt sự kiện.
func formatExportSuccess(res *exp.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✓ Đã xuất truyện %d chương / %s sang %s", res.Chapters, humanBytes(res.Bytes), res.Path)
	if n := len(res.Skipped); n > 0 {
		fmt.Fprintf(&b, " (bỏ qua %d chương chưa hoàn thành: %s)", n, briefIntList(res.Skipped, 5))
	}
	return b.String()
}

func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func briefIntList(xs []int, max int) string {
	if len(xs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(xs))
	for i, x := range xs {
		if i >= max {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, strconv.Itoa(x))
	}
	return strings.Join(parts, ",")
}
