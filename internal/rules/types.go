// Package rules thực hiện lớp đầu vào bền vững (Policy) cho tuỳ chọn người dùng.
//
// Rule là loại dữ liệu thứ tư, ngang hàng với Progress / Checkpoint / Artifact, nhưng ngược tính chất:
// ba loại trước là đầu ra của hệ thống, Rule là đầu vào bền vững từ ý định người dùng.
//
// Ràng buộc thiết kế (không thương lượng):
//   - Công cụ chỉ trả sự thật, không trả chỉ thị (Violation là sự thật, editor tự quyết có kích hoạt viết lại không)
//   - Không tạo thêm đường verdict mới (tái dùng PendingRewrites)
//   - Không thêm trường mức độ nghiêm trọng (severity được ánh xạ cố định theo loại quy tắc, editor tự phán xét ngữ nghĩa)
//   - Không nuốt xung đột im lặng (mọi ngoại lệ đều vào Bundle.Conflicts, để LLM và /diag thấy được)
//   - Không động vào Flow Router (rule không tham gia định tuyến)
package rules

// SourceKind đánh dấu nguồn gốc quy tắc, dùng để sắp xếp ưu tiên gần nhất khi hợp nhất.
// Giá trị càng lớn càng gần: Project > Global > Default.
//
// Từ Phase 1.1 chỉ hỗ trợ ba lớp. Lớp Genre / Learned chưa mở —
// khi thực sự cần mở rộng thì thêm hằng số và bổ sung loader, không để sẵn khung rỗng.
type SourceKind int

const (
	// SourceDefault — quy tắc mặc định tích hợp sẵn (assets/rules/default.md), ưu tiên thấp nhất.
	SourceDefault SourceKind = iota
	// SourceGlobal — tuỳ chọn toàn cục của người dùng (tất cả .md trong ~/.ainovel/rules/, hợp nhất theo thứ tự tên file), dùng chung mọi cuốn sách.
	SourceGlobal
	// SourceProject — quy tắc của cuốn sách này (tất cả .md trong ./.ainovel/rules/, hợp nhất theo thứ tự tên file), ưu tiên cao nhất.
	SourceProject
)

// String trả về tên có thể đọc được của nguồn gốc, dùng làm tiêu đề nguồn khi ghép markdown và trong conflicts.detail.
func (k SourceKind) String() string {
	switch k {
	case SourceDefault:
		return "default"
	case SourceGlobal:
		return "global"
	case SourceProject:
		return "project"
	default:
		return "unknown"
	}
}

// WordRange biểu thị khoảng số từ cho phép của một chương; nil nghĩa là chưa khai báo.
type WordRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Structured chứa các trường có cấu trúc từ front matter.
//
// Khi phân tích một file đơn, Parsed.Structured chỉ điền các trường được khai báo trong file đó, còn lại giữ giá trị zero.
// Sau khi hợp nhất, Bundle.Structured là kết quả tổng thể theo ưu tiên gần nhất từ các nguồn.
type Structured struct {
	Genre            string         `json:"genre,omitempty"`
	ChapterWords     *WordRange     `json:"chapter_words,omitempty"`
	ForbiddenChars   []string       `json:"forbidden_chars,omitempty"`
	ForbiddenPhrases []string       `json:"forbidden_phrases,omitempty"`
	FatigueWords     map[string]int `json:"fatigue_words,omitempty"`
}

// IsEmpty dùng để xác định có hoàn toàn không có quy tắc có cấu trúc không; checker có thể bỏ qua nếu đúng.
func (s Structured) IsEmpty() bool {
	return s.Genre == "" &&
		s.ChapterWords == nil &&
		len(s.ForbiddenChars) == 0 &&
		len(s.ForbiddenPhrases) == 0 &&
		len(s.FatigueWords) == 0
}

// ConflictKind đánh dấu loại xung đột hoặc ngoại lệ, giúp LLM và bảng chẩn đoán phân loại xử lý.
type ConflictKind string

const (
	// ConflictParseError — toàn bộ front matter phân tích thất bại; phần nội dung vẫn được inject làm tuỳ chọn.
	ConflictParseError ConflictKind = "parse_error"
	// ConflictUnknownField — người dùng viết trường chưa được hỗ trợ ở Phase 1 (tương thích tiến về phía trước).
	ConflictUnknownField ConflictKind = "unknown_field"
	// ConflictTypeError — kiểu dữ liệu trường sai (ví dụ forbidden_chars viết thành chuỗi đơn); trường đó bị bỏ qua.
	ConflictTypeError ConflictKind = "type_error"
	// ConflictFieldConflict — cùng một trường có cấu trúc nhưng giá trị từ nhiều nguồn không nhất quán; ưu tiên gần nhất được áp dụng.
	ConflictFieldConflict ConflictKind = "field_conflict"
	// ConflictInvalidValue — giá trị trường không hợp lệ (ví dụ chapter_words: "abc"); trường đó bị bỏ qua.
	ConflictInvalidValue ConflictKind = "invalid_value"
)

// Conflict là một bản ghi xung đột hoặc ngoại lệ.
//
// Không bao giờ chặn quá trình tải — mọi ngoại lệ đều được phơi bày ở đây cho LLM và /diag, không xử lý im lặng.
type Conflict struct {
	Source string       `json:"source"`          // đường dẫn file (tuyệt đối hoặc tương đối, ghi theo nguồn)
	Kind   ConflictKind `json:"kind"`            // loại xung đột
	Field  string       `json:"field,omitempty"` // tên trường bị ảnh hưởng (ví dụ forbidden_chars); để trống khi parse_error
	Detail string       `json:"detail"`          // chi tiết có thể đọc được (gồm danh sách nguồn / thông báo lỗi)
}

// Parsed là kết quả phân tích một file rules.md đơn lẻ.
type Parsed struct {
	Source     string     // đường dẫn file
	Kind       SourceKind // loại nguồn gốc, dùng cho ưu tiên khi hợp nhất
	Structured Structured // các trường front matter được khai báo trong file này
	Preference string     // nội dung Markdown của file (phần ngoài front matter)
	Conflicts  []Conflict // các conflicts phát sinh trong quá trình phân tích file (trường lạ / lỗi kiểu)
}

// Bundle là dạng cuối cùng sau khi hợp nhất, được inject vào working_memory.user_rules.
//
// Ánh xạ trường sang JSON đầu ra:
//
//	{
//	  "structured": {...},
//	  "preferences": "...markdown đã hợp nhất...",
//	  "sources": ["..."],
//	  "conflicts": [...]
//	}
type Bundle struct {
	Structured  Structured `json:"structured"`
	Preferences string     `json:"preferences"`
	Sources     []string   `json:"sources"`
	Conflicts   []Conflict `json:"conflicts"`
}

// IsEmpty cho biết Bundle hoàn toàn rỗng (trường có cấu trúc rỗng + nội dung tuỳ chọn rỗng).
// Khi inject user_rules vẫn nên giữ lại Bundle rỗng để LLM không phải xử lý nil.
func (b Bundle) IsEmpty() bool {
	return b.Structured.IsEmpty() && b.Preferences == ""
}

// Severity đánh dấu mức độ nghiêm trọng của Violation.
// Ánh xạ cố định (người dùng không thể cấu hình):
//
//	forbidden_chars xuất hiện             -> Error
//	forbidden_phrases xuất hiện           -> Error
//	fatigue_words vượt ngưỡng             -> Warning
//	chapter_words hụt sàn < 20%           -> Warning
//	chapter_words hụt sàn >= 20%          -> Error
//	chapter_words vượt trần mềm           -> Warning (luôn luôn; xem ceiling.go)
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// ChapterWordsDeviationThreshold định nghĩa ngưỡng độ lệch chapter_words để nâng lên error (20%).
const ChapterWordsDeviationThreshold = 0.20

// Violation là đầu ra của checker: tuyên bố sự thật rằng chương này vi phạm một quy tắc máy móc nào đó.
//
// Lưu ý: commit_chapter truyền thẳng violations vào JSON trả về, không chặn commit;
// editor khi xem xét sẽ ánh xạ các sự thật này vào tám chiều hiện có (aesthetic/pacing/character/consistency...),
// để LLM tự quyết định có nâng verdict kích hoạt polish/rewrite không.
type Violation struct {
	Rule      string   `json:"rule"`                // forbidden_chars / forbidden_phrases / fatigue_words / chapter_words
	Target    string   `json:"target,omitempty"`    // đối tượng vi phạm cụ thể (từ/ký tự nào); để trống với chapter_words
	Limit     any      `json:"limit,omitempty"`     // ngưỡng; fatigue_words=int / chapter_words="3000-6000" / forbidden_*=rỗng
	Actual    any      `json:"actual"`              // giá trị thực tế; fatigue_words/forbidden_*=số lần xuất hiện / chapter_words=số từ chương
	Deviation float64  `json:"deviation,omitempty"` // tỷ lệ lệch chapter_words (0~1), các quy tắc khác để trống
	Severity  Severity `json:"severity"`            // error / warning
}
