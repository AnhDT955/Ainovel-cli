package diag

// Severity biểu thị mức độ nghiêm trọng của một phát hiện.
type Severity string

const (
	SevCritical Severity = "critical" // chặn tiến độ hoặc gây hỏng dữ liệu
	SevWarning  Severity = "warning"  // có thể làm giảm chất lượng hoặc lãng phí token
	SevInfo     Severity = "info"     // mục có thể tối ưu
)

// Category phân nhóm các phát hiện theo chiều dữ liệu.
type Category string

const (
	CatFlow     Category = "flow"     // luồng bị kẹt, trạng thái bất thường, vấn đề khôi phục
	CatQuality  Category = "quality"  // điểm đánh giá, thực thi hợp đồng, tính nhất quán
	CatPlanning Category = "planning" // khoảng trống đề cương, phục bút lệch hướng, la bàn lỗi thời
	CatContext  Category = "context"  // bất thường nhân vật/dòng thời gian/quan hệ
)

// Confidence biểu thị độ tin cậy của phán định từ quy tắc.
type Confidence string

const (
	ConfHigh   Confidence = "high"   // độ chắc chắn cao, đáng tin cậy
	ConfMedium Confidence = "medium" // phán đoán heuristic, có thể sai
	ConfLow    Confidence = "low"    // tín hiệu sơ bộ, chỉ để tham khảo
)

// AutoLevel biểu thị Finding có thể chuyển thành hành động tự động hay không.
type AutoLevel string

const (
	AutoNone    AutoLevel = "none"    // chỉ báo cáo, không tự động
	AutoSuggest AutoLevel = "suggest" // đề xuất hành động nhưng cần xác nhận thủ công
	AutoSafe    AutoLevel = "safe"    // có thể tự động thực thi an toàn
)

// Finding là một kết quả chẩn đoán có thể hành động.
type Finding struct {
	Rule       string     // tên quy tắc, ví dụ "StaleForeshadow"
	Category   Category   // phân loại
	Severity   Severity   // mức độ nghiêm trọng
	Confidence Confidence // độ tin cậy phán định
	AutoLevel  AutoLevel  // mức độ tự động hóa
	Target     string     // vùng tác động đề xuất, ví dụ "runtime.flow"
	Title      string     // tóm tắt một dòng
	Evidence   string     // bằng chứng dữ liệu cụ thể
	Suggestion string     // đề xuất cải thiện (trỏ đến prompt/flow/config)
}

// RuleFunc là chữ ký thống nhất của quy tắc chẩn đoán.
type RuleFunc func(snap *Snapshot) []Finding

// ActionKind biểu thị loại hành động chẩn đoán.
type ActionKind string

const (
	ActionEmitNotice      ActionKind = "emit_notice"       // phát thông báo hệ thống
	ActionEnqueueFollowUp ActionKind = "enqueue_follow_up" // chèn coordinator follow-up
)

// Action là hành động có thể thực thi do Planner tạo ra từ Finding có độ tin cậy cao.
type Action struct {
	SourceRule  string     // tên quy tắc nguồn
	Kind        ActionKind // loại hành động
	Severity    Severity   // kế thừa từ Finding
	Summary     string     // mô tả ngắn
	Message     string     // thông điệp truyền vào luồng điều khiển
	Fingerprint string     // dấu vân tay ổn định của Finding nguồn, dùng để khử trùng lặp lúc chạy
}

// Stats là các chỉ số tổng quan hiển thị cùng với các phát hiện.
type Stats struct {
	CompletedChapters int
	TotalChapters     int
	TotalWords        int
	AvgWordsPerCh     int
	Phase             string
	Flow              string
	PlanningTier      string
	ReviewCount       int
	RewriteCount      int
	AvgReviewScore    float64
	ForeshadowOpen    int
	ForeshadowStale   int
}

// Report là toàn bộ đầu ra của một lần chạy chẩn đoán.
type Report struct {
	Stats    Stats
	Findings []Finding
	Actions  []Action
}
