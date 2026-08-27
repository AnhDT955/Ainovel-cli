package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const configDirName = ".ainovel"

// DefaultConfigPath trả về đường dẫn file cấu hình toàn cục ~/.ainovel/config.json.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName, "config.json")
}

// DefaultConfigDir trả về đường dẫn thư mục ~/.ainovel; trả về chuỗi rỗng nếu không lấy được thư mục home.
// Chỉ dùng để đọc/ghi các file không bắt buộc tồn tại (như cache model), không tự động tạo thư mục.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName)
}

// configDir trả về đường dẫn thư mục ~/.ainovel, tạo mới nếu chưa tồn tại.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, configDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// projectConfigPath trả về đường dẫn tương đối của file cấu hình cấp dự án ./.ainovel/config.json.
// Thư mục dotdir cấp dự án phản chiếu ~/.ainovel/ toàn cục, dùng lại cùng configDirName; phân giải tương đối theo cwd.
func projectConfigPath() string {
	return filepath.Join(configDirName, "config.json")
}

// LoadConfig tải và hợp nhất cấu hình theo thứ tự ưu tiên:
//  1. ~/.ainovel/config.json (toàn cục)
//  2. ./.ainovel/config.json (ghi đè cấp dự án)
//  3. Đường dẫn chỉ định bởi flagPath (ưu tiên cao nhất)
func LoadConfig(flagPath string) (Config, error) {
	var cfg Config

	// 1. Cấu hình toàn cục. Đây là nền tảng ưu tiên thấp nhất; file lỗi sẽ giáng cấp thành
	//    cảnh báo thay vì chặn — có thể bị ghi đè bởi cấp dự án / --config;
	//    lỗi cứng sẽ chặn người dùng có "cấu hình toàn cục lỗi + --config hợp lệ",
	//    vi phạm ngữ nghĩa "tôi chỉ định rõ ràng cái này" của --config.
	if p := DefaultConfigPath(); p != "" {
		global, found, err := loadOptionalJSON(p)
		switch {
		case err != nil:
			slog.Warn("Cấu hình toàn cục phân tích thất bại, đã bỏ qua (có thể bị ghi đè bởi cấp dự án/--config)", "module", "config", "path", p, "err", err)
		case found:
			cfg = global
		}
	}

	// 2. Ghi đè cấp dự án. File lỗi sẽ fail loud: người dùng chủ động đặt cấu hình
	//    trong thư mục hiện tại, nuốt im lặng sẽ khiến "đã cấu hình nhưng không có hiệu lực"
	//    không thể truy vết (issue #37).
	project, found, err := loadOptionalJSON(projectConfigPath())
	if err != nil {
		return cfg, fmt.Errorf("cấu hình cấp dự án ./.ainovel/config.json phân tích thất bại (vui lòng kiểm tra cú pháp JSON): %w", err)
	}
	if found {
		cfg = mergeConfig(cfg, project)
	}

	// 3. Ghi đè từ CLI flag
	if flagPath != "" {
		override, err := loadJSONFile(flagPath)
		if err != nil {
			return cfg, fmt.Errorf("load config %s: %w", flagPath, err)
		}
		cfg = mergeConfig(cfg, override)
	}

	return cfg, nil
}

// loadOptionalJSON đọc một file cấu hình tùy chọn:
//   - File không tồn tại → (zero, false, nil), để bên gọi quyết định dùng giá trị mặc định/cấp trên
//   - File tồn tại nhưng phân tích thất bại → trả về lỗi (không còn nuốt im lặng — nếu không
//     cấu hình của người dùng "đã cấu hình nhưng không có hiệu lực" mà không thể truy vết,
//     đây chính là nguyên nhân gốc rễ của issue #37)
func loadOptionalJSON(path string) (Config, bool, error) {
	cfg, err := loadJSONFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, err
	}
	return cfg, true, nil
}

// LoadConfigFile đọc một file cấu hình JSON đơn lẻ, hỗ trợ chú thích dòng //.
// Không thực hiện hợp nhất, chỉ trả về cấu hình của file đó. Trả về lỗi nếu file không tồn tại.
func LoadConfigFile(path string) (Config, error) {
	return loadJSONFile(path)
}

// loadJSONFile đọc file cấu hình JSON, hỗ trợ chú thích dòng //.
// Trả về lỗi nếu file không tồn tại (để bên gọi quyết định có bỏ qua hay không).
func loadJSONFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cleaned := stripJSONComments(data)
	var cfg Config
	if err := json.Unmarshal(cleaned, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// mergeConfig hợp nhất overlay vào base. Các trường có giá trị khác zero sẽ ghi đè, map hợp nhất theo key.
func mergeConfig(base, overlay Config) Config {
	if overlay.Provider != "" {
		base.Provider = overlay.Provider
	}
	if overlay.ModelName != "" {
		base.ModelName = overlay.ModelName
	}
	if overlay.Style != "" {
		base.Style = overlay.Style
	}
	if overlay.ContextWindow > 0 {
		base.ContextWindow = overlay.ContextWindow
	}

	// Providers: key của overlay ghi đè key cùng tên trong base
	if len(overlay.Providers) > 0 {
		if base.Providers == nil {
			base.Providers = make(map[string]ProviderConfig)
		}
		for k, v := range overlay.Providers {
			existing := base.Providers[k]
			if v.Type != "" {
				existing.Type = v.Type
			}
			if v.APIKey != "" {
				existing.APIKey = v.APIKey
			}
			if v.BaseURL != "" {
				existing.BaseURL = v.BaseURL
			}
			if len(v.Models) > 0 {
				existing.Models = append([]string(nil), v.Models...)
			}
			if len(v.ExtraBody) > 0 {
				existing.ExtraBody = cloneMap(v.ExtraBody)
			}
			if len(v.Extra) > 0 {
				existing.Extra = cloneMap(v.Extra)
			}
			base.Providers[k] = existing
		}
	}

	// Roles: key của overlay ghi đè key cùng tên trong base
	if len(overlay.Roles) > 0 {
		if base.Roles == nil {
			base.Roles = make(map[string]RoleConfig)
		}
		for k, v := range overlay.Roles {
			existing := base.Roles[k]
			if v.Provider != "" {
				existing.Provider = v.Provider
			}
			if v.Model != "" {
				existing.Model = v.Model
			}
			if len(v.Fallbacks) > 0 {
				existing.Fallbacks = append([]ModelRef(nil), v.Fallbacks...)
			}
			base.Roles[k] = existing
		}
	}

	// Budget / Notify: ghi đè toàn bộ khối (ngân sách/cảnh báo cấp dự án là khai báo chính sách độc lập,
	// không nối từng trường với toàn cục)
	if overlay.Budget != (BudgetConfig{}) {
		base.Budget = overlay.Budget
	}
	if overlay.Notify.Enabled != nil || overlay.Notify.Command != "" || len(overlay.Notify.Events) > 0 {
		base.Notify = overlay.Notify
	}

	return base
}

func cloneMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// stripJSONComments loại bỏ các chú thích dòng // trong JSON, theo dõi trạng thái dấu ngoặc kép
// để tránh xóa nhầm nội dung bên trong chuỗi.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		b := data[i]

		if escaped {
			out = append(out, b)
			escaped = false
			continue
		}

		if inString {
			out = append(out, b)
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}

		// Không nằm trong chuỗi
		if b == '"' {
			inString = true
			out = append(out, b)
			continue
		}

		// Phát hiện chú thích //
		if b == '/' && i+1 < len(data) && data[i+1] == '/' {
			// Bỏ qua đến cuối dòng
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
			continue
		}

		out = append(out, b)
	}

	return out
}

// WriteStartupError ghi nối tiếp lỗi nghiêm trọng trong giai đoạn khởi động vào ~/.ainovel/last-error.log,
// và trả về đường dẫn file đó (best-effort, trả về chuỗi rỗng nếu thất bại). Khi khởi động bằng
// cách nhấp đúp, cửa sổ console sẽ đóng ngay khi tiến trình kết thúc khiến lỗi thoáng hiện rồi biến mất,
// ghi xuống đĩa là cách duy nhất để người dùng truy vết sau đó.
func WriteStartupError(msg string) string {
	dir := DefaultConfigDir()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "last-error.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), msg); err != nil {
		return ""
	}
	return path
}

// SaveConfig ghi cấu hình vào đường dẫn chỉ định (định dạng JSON, căn lề đẹp).
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
