package host

import (
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// Event là sự kiện có cấu trúc mà TUI tiêu thụ.
//
// Đối với hai loại sự kiện gọi TOOL / DISPATCH, phần bắt đầu và kết thúc của cùng một lần gọi dùng chung một ID:
// Khi bắt đầu, phát sự kiện có FinishedAt là giá trị zero (TUI hiển thị theo kiểu "đang xử lý");
// Khi kết thúc, phát thêm một sự kiện cùng ID, điền FinishedAt + Duration (+ Failed),
// TUI định vị theo ID để cập nhật tại chỗ trên cùng dòng, tránh dư thừa kiểu "một dòng khi bắt đầu, lại thêm dòng khi hoàn thành".
//
// Các sự kiện không phải gọi như SYSTEM / ERROR / CONTEXT có ID rỗng, mỗi sự kiện được thêm độc lập.
type Event struct {
	ID         string    // Dùng chung cho bắt đầu/kết thúc của cùng một lần gọi; rỗng với sự kiện không phải gọi
	Time       time.Time // Thời điểm phát lần đầu (thời điểm bắt đầu)
	FinishedAt time.Time // Giá trị zero = đang xử lý; khác zero = đã hoàn thành
	Failed     bool      // Đã hoàn thành nhưng thất bại (chỉ có ý nghĩa ở trạng thái hoàn thành)
	Category   string    // DISPATCH / TOOL / SYSTEM / REVIEW / CHECK / ERROR / CONTEXT
	Agent      string    // Agent phát sinh sự kiện
	Summary    string
	Detail     string        // Nội dung đầy đủ, ghi vào log không cắt ngắn để tiện tra cứu; nếu rỗng thì dùng Summary. UI chỉ đọc Summary
	Kind       string        // Phân loại lỗi (ví dụ stream_idle), xuất cùng log để lọc/cảnh báo; rỗng thì không xuất
	Level      string        // info / warn / error / success
	Depth      int           // 0 = tầng điều phối viên, 1 = tầng agent phụ
	Duration   time.Duration // Thời gian thực thi khi hoàn thành
}

// Running trả về sự kiện có đang trong trạng thái xử lý hay không.
// Chỉ các sự kiện gọi (TOOL / DISPATCH có ID) mới có thể đang xử lý; các loại khác luôn trả về false.
func (e Event) Running() bool {
	return e.ID != "" && e.FinishedAt.IsZero()
}

// UISnapshot là ảnh chụp trạng thái tổng hợp cần thiết để TUI hiển thị.
type UISnapshot struct {
	Provider           string
	NovelName          string
	ModelName          string
	ModelContextWindow int // Cửa sổ ngữ cảnh của model mặc định hiện tại (phân tích thời gian thực khi chuyển /model)
	Style              string
	RuntimeState       string // idle / running / pausing / paused / completed
	StatusLabel        string
	Phase              string
	Flow               string
	CurrentChapter     int
	TotalChapters      int
	CompletedCount     int
	TotalWordCount     int
	InProgressChapter  int
	PendingRewrites    []int
	RewriteReason      string
	PendingSteer       string
	RecoveryLabel      string
	IsRunning          bool
	Agents             []AgentSnapshot

	// Ngữ cảnh
	ContextTokens         int
	ContextWindow         int
	ContextPercent        float64
	ContextScope          string
	ContextStrategy       string
	ContextActiveMessages int
	ContextSummaryCount   int
	ContextCompactedCount int
	ContextKeptCount      int

	// Tổng lượng dùng (toàn bộ phiên, qua tất cả agent và lần chuyển model)
	TotalInputTokens      int
	TotalOutputTokens     int
	TotalCacheReadTokens  int
	TotalCacheWriteTokens int
	TotalCostUSD          float64
	TotalSavedUSD         float64 // Số USD tiết kiệm được nhờ CacheRead (so với tính toàn bộ theo giá input không cache)
	BudgetLimitUSD        float64 // Ngân sách tối đa (config budget.book_usd); 0 = chưa bật

	// Chẩn đoán cache
	OverallCacheCapable    bool // Ít nhất một role đã chạy model hỗ trợ prompt cache (phân biệt "chưa bật" và "0% trúng cache")
	OverallRecentCacheRead int  // Tổng cacheRead của N lần gần nhất trong cửa sổ trượt
	OverallRecentInput     int  // Tổng input của N lần gần nhất trong cửa sổ trượt
	OverallRecentSamples   int  // Số mẫu trong cửa sổ trượt (≤ recentSampleCap)

	// MissingAssistantUsage > 0 thường có nghĩa là streaming phía trên không gửi
	// final usage chunk theo giao thức stream_options.include_usage của OpenAI (phổ biến với proxy tự dựng),
	// khiến UsageTracker không nhận được bất kỳ dữ liệu tích lũy nào. UI dựa vào đây để
	// thông báo rõ cho người dùng kiểm tra backend,
	// tránh người dùng nhầm tưởng module cache bị lỗi.
	MissingAssistantUsage int

	// Cache theo chiều per-role, sắp xếp giảm dần theo CacheRead, đã lọc các role chưa dùng token
	CachePerAgent []AgentCacheStat
	CachePerModel []AgentCacheStat

	// Cài đặt cơ bản
	Premise          string
	Outline          []OutlineSnapshot
	Characters       []string
	SupportingCount  int      // Tổng số nhân vật phụ trong danh sách nhân vật phụ
	RecentSupporting []string // Nhân vật phụ hoạt động gần đây (tối đa 5, sắp xếp giảm dần theo LastSeenChapter)
	Layered          bool
	CurrentVolumeArc string
	NextVolumeTitle  string
	CompassDirection string
	CompassScale     string

	// Chi tiết
	LastCommitSummary  string
	LastReviewSummary  string
	LastCheckpointName string
	RecentSummaries    []string
}

// OutlineSnapshot là tóm tắt hiển thị của một mục trong đề cương.
type OutlineSnapshot struct {
	Chapter   int
	Title     string
	CoreEvent string
}

// AgentSnapshot là phép chiếu hiển thị trạng thái của Agent.
type AgentSnapshot struct {
	Name      string
	State     string
	TaskID    string
	TaskKind  string
	Summary   string
	Tool      string
	Turn      int
	Context   AgentContextSnapshot
	UpdatedAt time.Time
}

// AgentCacheStat là tổng lượt trúng cache của một agent đơn lẻ (chiếu vào cột trái).
// HitRate = CacheRead / Input; Input ở tầng litellm đã được chuẩn hóa theo ngữ nghĩa "bao gồm CacheRead".
//
// CacheCapable dùng để phân biệt hai trường hợp 0% trúng cache:
//   - true  → Model hỗ trợ prompt cache, 0% là do thiết kế prompt kém hoặc tiền tố không ổn định, cần tối ưu
//   - false → Model/nhà cung cấp không hỗ trợ prompt cache, 0% là bình thường, không cần tra cứu
//
// Recent* là dữ liệu trúng cache của cửa sổ trượt (N lần gọi gần nhất), so sánh với tích lũy có thể nhận ra "bị kéo xuống từ trước" vs "trúng thấp ở trạng thái ổn định".
type AgentCacheStat struct {
	Role            string
	Model           string
	Input           int
	Output          int
	CacheRead       int
	CacheWrite      int
	Cost            float64
	Saved           float64
	CacheCapable    bool
	RecentCacheRead int
	RecentInput     int
	RecentSamples   int
}

// AgentContextSnapshot là tình trạng sử dụng ngữ cảnh của Agent.
type AgentContextSnapshot struct {
	Tokens          int
	ContextWindow   int
	Percent         float64
	Scope           string
	Strategy        string
	ActiveMessages  int
	SummaryMessages int
	CompactedCount  int
	KeptCount       int
}

// CoCreateMessage là tin nhắn trong hội thoại đồng sáng tác.
type CoCreateMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CoCreateReply là phản hồi LLM trong hội thoại đồng sáng tác. Raw giữ nguyên toàn bộ bốn đoạn gốc của model,
// dùng để ghi lại history để vòng tiếp theo model thấy được [DRAFT] của vòng trước, từ đó thực sự
// tích lũy cập nhật trên bản nháp đã có (chỉ dùng Message không có [DRAFT] sẽ khiến model mỗi vòng tổng hợp lại từ hội thoại).
// Suggestions là những gợi ý "bạn có thể muốn nói tiếp" do AI chủ động đưa ra, khi người dùng bí ý nhấn phím số để điền ngay vào ô nhập.
type CoCreateReply struct {
	Message     string
	Prompt      string
	Ready       bool
	Suggestions []string
	Raw         string
}

// ReplayDeltaText trích xuất văn bản streaming có thể phát lại từ mục hàng đợi runtime.
func ReplayDeltaText(item domain.RuntimeQueueItem) string {
	if payload, ok := item.Payload.(map[string]any); ok {
		if text, ok := payload["delta"].(string); ok {
			return text
		}
	}
	return ""
}

// BuildStartPrompt đóng gói yêu cầu của người dùng thành prompt khởi động cho Điều phối viên.
//
// Câu về tiêu đề Premise được diễn đạt như một CHỖ ĐIỀN kèm ví dụ, chứ không phải một chuỗi mẫu.
// Bản trước viết "dòng đầu tiên của Premise bắt buộc phải là `# Tên truyện`" — mô hình hiểu theo
// nghĩa đen và chép đúng chữ "# Tên truyện" vào premise.md. Tài liệu yêu cầu của người dùng cũng
// thường mở đầu bằng đúng nhãn "Tên truyện", nên cách hiểu sai đó lại càng có vẻ hợp lý.
// domain.ValidatePremise chặn kết quả này ở đường ghi; câu chữ dưới đây chặn nó từ đầu nguồn.
func BuildStartPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	return "Hãy bắt đầu sáng tác một cuốn tiểu thuyết dựa theo yêu cầu sáng tác dưới đây. Sau khi vào giai đoạn lập kế hoạch, dòng đầu tiên của Premise bắt buộc phải là một tiêu đề Markdown chứa TÊN SÁCH THẬT — ví dụ `# Loạn Thế Võ Đạo`, chứ KHÔNG phải chép nguyên chữ \"Tên truyện\" hay bất kỳ nhãn chỗ điền nào khác. Nếu tài liệu yêu cầu đã nêu tên truyện thì dùng nguyên văn tên đó. Số chương do bạn tự quyết định dựa trên nhu cầu câu chuyện; nếu đề tài và xung đột phù hợp với thể loại trường thiên nhiều kỳ, hãy ưu tiên lập kế hoạch phân lớp dài thay vì nén thành dạng tóm tắt ngắn.\n\n[Yêu cầu sáng tác]\n" +
		prompt +
		"\n\nNếu một số chi tiết chưa được nêu rõ, hãy tự bổ sung mà không đi ngược lại hướng của người dùng."
}
