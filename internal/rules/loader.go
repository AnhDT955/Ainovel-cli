package rules

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadOptions là tham số đầu vào của Load.
//
// File không tồn tại không bị coi là lỗi, loader bỏ qua yên lặng; lỗi phân tích không chặn luồng,
// conflicts sẽ được parser ghi vào Parsed.Conflicts.
type LoadOptions struct {
	// RulesFS là cây thư mục assets/rules. Quy ước thư mục gốc chứa trực tiếp default.md.
	// Thường lấy qua fs.Sub(embedFS, "rules"); nil nghĩa là bỏ qua quy tắc tích hợp sẵn.
	RulesFS fs.FS

	// HomeRulesDir là thư mục ~/.ainovel/rules/; loader quét tất cả .md cấp trên (gộp theo thứ tự tên file). Trống nghĩa là bỏ qua.
	HomeRulesDir string

	// ProjectRulesDir là thư mục ./.ainovel/rules/ (giống toàn cục, cũng quét tất cả .md cấp trên). Trống nghĩa là bỏ qua.
	ProjectRulesDir string
}

// Load đọc theo thứ tự Default → Global → Project, trả về danh sách Parsed đã sắp xếp tăng dần.
//
// merger chỉ cần gộp theo thứ tự danh sách, phần sau ghi đè phần trước.
// Không áp dụng tải hai giai đoạn — các lớp mở rộng như Genre / Learned chưa được mở cho đến khi có nội dung thực sự.
func Load(opts LoadOptions) []Parsed {
	var layers []Parsed
	if p, ok := readFromFS(opts.RulesFS, "default.md", SourceDefault, "assets/rules/default.md"); ok {
		layers = append(layers, p)
	}
	layers = append(layers, readDirFromDisk(opts.HomeRulesDir, SourceGlobal)...)
	layers = append(layers, readDirFromDisk(opts.ProjectRulesDir, SourceProject)...)
	return layers
}

// readFromFS đọc và phân tích từ fs.FS; file không tồn tại trả về (Parsed{}, false).
// displayPath dùng cho Parsed.Source (để hiển thị dạng "assets/rules/..." trong sources/conflicts).
func readFromFS(fsys fs.FS, name string, kind SourceKind, displayPath string) (Parsed, bool) {
	if fsys == nil {
		return Parsed{}, false
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		// File không tồn tại thì bỏ qua yên lặng; lỗi khác cũng không chặn (loader không báo lỗi theo thiết kế)
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return Parsed{}, false
		}
		// Lỗi IO hiếm gặp: để lộ dưới dạng parse_error, tránh nuốt yên lặng
		return Parsed{
			Source: displayPath,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: displayPath,
				Kind:   ConflictParseError,
				Detail: "đọc thất bại: " + err.Error(),
			}},
		}, true
	}
	return Parse(displayPath, kind, data), true
}

// readFromDisk đọc và phân tích từ đường dẫn tuyệt đối; đường dẫn trống hoặc file không tồn tại trả về (Parsed{}, false).
func readFromDisk(absPath string, kind SourceKind) (Parsed, bool) {
	if strings.TrimSpace(absPath) == "" {
		return Parsed{}, false
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Parsed{}, false
		}
		return Parsed{
			Source: absPath,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: absPath,
				Kind:   ConflictParseError,
				Detail: "đọc thất bại: " + err.Error(),
			}},
		}, true
	}
	return Parse(absPath, kind, data), true
}

// readDirFromDisk quét tất cả file .md cấp trên trong thư mục (theo thứ tự tên file), phân tích từng file thành Parsed.
// Thứ tự tên file đảm bảo thứ tự gộp của nhiều file cùng lớp ổn định, có thể dự đoán (sau ghi đè trước).
// Bỏ qua thư mục con và file ẩn/tạm của editor bắt đầu bằng . (như macOS ._x.md, emacs .#x.md),
// tránh nội dung nhị phân từ file bẩn bị inject vào LLM như văn bản tùy chọn.
// Đường dẫn trống hoặc thư mục không tồn tại trả về nil (bỏ qua yên lặng, nhất quán với file đơn thiếu);
// thư mục tồn tại nhưng đọc thất bại (quyền / đường dẫn thực ra là file) sẽ lộ ConflictParseError, không nuốt lỗi —
// nhất quán với hợp đồng chịu lỗi của readFromFS / readFromDisk.
// Không đệ quy vào thư mục con — giữ cấu trúc phẳng, tránh tạo ra tầng lớp ẩn.
func readDirFromDisk(dir string, kind SourceKind) []Parsed {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return []Parsed{{
			Source: dir,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: dir,
				Kind:   ConflictParseError,
				Detail: "đường dẫn quy tắc là file, không phải thư mục",
			}},
		}}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []Parsed{{
			Source: dir,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: dir,
				Kind:   ConflictParseError,
				Detail: "đọc thư mục quy tắc thất bại: " + err.Error(),
			}},
		}}
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var out []Parsed
	for _, name := range names {
		if p, ok := readFromDisk(filepath.Join(dir, name), kind); ok {
			out = append(out, p)
		}
	}
	return out
}

// ainovelDirName là tên dotdir dùng chung cho cả cấp user và cấp project của ainovel.
// ~/.ainovel/rules/ toàn cục và ./.ainovel/rules/ theo dự án đối xứng nhau qua đây.
const ainovelDirName = ".ainovel"

// DefaultProjectRulesDir ghép đường dẫn tuyệt đối của ./.ainovel/rules/ (dựa trên thư mục dự án đã cho).
// Người gọi truyền vào thư mục gốc dự án, tránh phụ thuộc vào cwd bên trong loader; đối xứng với DefaultHomeRulesDir.
func DefaultProjectRulesDir(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	return filepath.Join(projectDir, ainovelDirName, "rules")
}

// DefaultHomeRulesDir ghép đường dẫn tuyệt đối của thư mục ~/.ainovel/rules/.
// Trả về chuỗi rỗng nếu không giải được home (người gọi sẽ bỏ qua nguồn này).
func DefaultHomeRulesDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ainovelDirName, "rules")
}

// homeRulesReadme là nội dung hướng dẫn được ghi vào ~/.ainovel/rules/README.txt lần đầu khởi tạo,
// giúp người dùng khám phá điểm mở rộng tùy chọn toàn cục này và biết cách viết.
// Cố ý dùng đuôi .txt thay vì .md — loader chỉ quét .md, file hướng dẫn này sẽ không bị inject vào LLM như quy tắc.
const homeRulesReadme = `Đặt tùy chọn viết toàn cục tại đây, có hiệu lực với tất cả các sách.

Đơn giản nhất: tạo một file .md mới (ví dụ my-style.md), viết tùy chọn bằng ngôn ngữ tự nhiên —
không cần định dạng đặc biệt, không cần YAML:

    # Nhân vật
    - Nhân vật chính đừng viết kiểu thánh nhân, lạnh bên ngoài nóng bên trong là được
    # Phong cách
    - Ưu tiên cảm nhận cơ thể (đốt ngón tay trắng bệch) thay vì nhãn cảm xúc (căng thẳng)
    - Hội thoại đừng quá văn hoa

Những điều này sẽ được giao nguyên cho biên tập viên xem xét theo ngữ nghĩa. Nhiều file .md gộp theo thứ tự tên;
file ẩn bắt đầu bằng dấu chấm và file không phải .md đều bị bỏ qua (nên README.txt này sẽ không bị coi là quy tắc).

Nâng cao (tùy chọn): muốn kiểm tra cứng, xác định như "số từ / từ cấm",
có thể thêm một đoạn YAML front matter ở đầu file — commit_chapter sẽ đếm từng chữ, báo lỗi bắt buộc:

    ---
    chapter_words: 3000-6000          # phạm vi số từ mỗi chương
    forbidden_phrases: ["theo một nghĩa nào đó"]  # cụm từ bị cấm, xuất hiện là báo lỗi
    fatigue_words: {không khỏi: 1}    # từ sáo rỗng, vượt ngưỡng mỗi chương sẽ cảnh báo
    ---
    (bên dưới viết tùy chọn ngôn ngữ tự nhiên như bình thường)

Không viết cũng không sao: các câu sáo AI phổ biến và từ sáo rỗng đã có nền tảng kiểm tra cơ học tích hợp sẵn.

Ưu tiên tải (cao → thấp): ./.ainovel/rules/*.md (sách này) > ~/.ainovel/rules/*.md (đây) > mặc định tích hợp
`

// EnsureHomeRulesDir cố gắng tạo thư mục ~/.ainovel/rules/ và ghi README.txt hướng dẫn,
// giúp người dùng khám phá điểm mở rộng tùy chọn toàn cục này và biết cách viết.
// nice-to-have, không phải đường dẫn quan trọng: lỗi giải home hoặc lỗi ghi đều bị nuốt yên lặng, không bao giờ chặn khởi động.
func EnsureHomeRulesDir() {
	if dir := DefaultHomeRulesDir(); dir != "" {
		_ = ensureRulesDirAt(dir)
	}
}

// ensureRulesDirAt tạo thư mục và ghi README.txt theo mẫu hướng dẫn hiện tại, là nhân kiểm thử được của EnsureHomeRulesDir.
// README.txt là file hướng dẫn do hệ thống tạo (tùy chọn người dùng viết trong *.md, không được loader tải),
// mỗi lần đều ghi đè bằng mẫu mới nhất — không giữ nội dung cũ, cũng không cần logic tương thích phiên bản.
func ensureRulesDirAt(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "README.txt"), []byte(homeRulesReadme), 0o644)
}

// DefaultOptions xây dựng LoadOptions thông dụng dựa trên thư mục làm việc hiện tại.
//
// Phù hợp để Host gọi một lần khi khởi động, cho ContextTool / CommitChapterTool tái sử dụng cùng một cấu hình.
// Khi giải cwd thất bại, ProjectRulesDir để trống (loader sẽ bỏ qua nguồn này).
//
// Ngữ nghĩa đường dẫn: ProjectRulesDir gắn với **thư mục làm việc hiện tại (cwd)** chứ không phải outputDir.
// Người dùng cd vào thư mục khác để bắt đầu viết sách khác, ./.ainovel/rules/ tự nhiên đi theo cwd;
// nếu muốn chia sẻ qua nhiều sách, đặt vào thư mục toàn cục ~/.ainovel/rules/ (tất cả .md trong đó đều được tải).
func DefaultOptions(rulesFS fs.FS) LoadOptions {
	cwd, _ := os.Getwd()
	return LoadOptions{
		RulesFS:         rulesFS,
		HomeRulesDir:    DefaultHomeRulesDir(),
		ProjectRulesDir: DefaultProjectRulesDir(cwd),
	}
}
