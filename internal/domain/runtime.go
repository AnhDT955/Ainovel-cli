package domain

// Phase biểu thị giai đoạn sáng tác tiểu thuyết.
type Phase string

const (
	PhaseInit     Phase = "init"
	PhasePremise  Phase = "premise"
	PhaseOutline  Phase = "outline"
	PhaseWriting  Phase = "writing"
	PhaseComplete Phase = "complete"
)

// FlowState loại luồng xử lý đang hoạt động, dùng để khôi phục từ điểm khôi phục.
type FlowState string

const (
	FlowWriting   FlowState = "writing"
	FlowReviewing FlowState = "reviewing"
	FlowRewriting FlowState = "rewriting"
	FlowPolishing FlowState = "polishing"
	FlowSteering  FlowState = "steering"
)

// PlanningTier biểu thị cấp độ độ dài trong kế hoạch tác phẩm.
type PlanningTier string

const (
	PlanningTierShort PlanningTier = "short"
	PlanningTierMid   PlanningTier = "mid"
	PlanningTierLong  PlanningTier = "long"
)

// Progress theo dõi tiến trình, lưu vào meta/progress.json.
type Progress struct {
	NovelName         string      `json:"novel_name"`
	Phase             Phase       `json:"phase"`
	CurrentChapter    int         `json:"current_chapter"`
	TotalChapters     int         `json:"total_chapters"`
	CompletedChapters []int       `json:"completed_chapters"`
	TotalWordCount    int         `json:"total_word_count"`
	ChapterWordCounts map[int]int `json:"chapter_word_counts,omitempty"` // số từ mỗi chương, hỗ trợ hiệu chỉnh tổng số từ khi viết lại
	InProgressChapter int         `json:"in_progress_chapter,omitempty"` // chương đang được viết (khôi phục ở cấp cảnh)
	CompletedScenes   []int       `json:"completed_scenes,omitempty"`    // số cảnh đã hoàn thành trong chương hiện tại
	Flow              FlowState   `json:"flow,omitempty"`                // luồng hiện tại
	PendingRewrites   []int       `json:"pending_rewrites,omitempty"`    // hàng đợi các chương chờ viết lại
	RewriteReason     string      `json:"rewrite_reason,omitempty"`      // lý do viết lại
	StrandHistory     []string    `json:"strand_history,omitempty"`      // ghi lại dominant_strand theo thứ tự chương
	HookHistory       []string    `json:"hook_history,omitempty"`        // ghi lại hook_type theo thứ tự chương
	// Theo dõi phân tầng truyện dài (chỉ dùng ở chế độ dài; truyện ngắn/vừa để giá trị zero)
	CurrentVolume int  `json:"current_volume,omitempty"`
	CurrentArc    int  `json:"current_arc,omitempty"`
	Layered       bool `json:"layered,omitempty"`
	// ReopenedFromComplete đánh dấu cuốn sách này được mở lại từ trạng thái hoàn chỉnh qua reopen để vào chế độ rà soát lại.
	// Rà soát lại chỉ sửa các chương đã có, không thêm/bớt cấu trúc, nên khi hàng đợi trống thì
	// cho phép hoàn chỉnh theo tiêu chí "cấu trúc nguyên vẹn" (tránh vòng lặp vô tận
	// writing → viết vượt phạm vi sau khi phục bút bị xáo trộn); viết thuận chiều không đặt cờ này,
	// điều kiện hoàn chỉnh vẫn giữ ngữ nghĩa bảo thủ là thu gọn mạch truyện.
	ReopenedFromComplete bool `json:"reopened_from_complete,omitempty"`
}

// IsResumable kiểm tra xem có thể tiếp tục từ điểm ngắt hay không.
func (p *Progress) IsResumable() bool {
	return p.Phase == PhaseWriting && p.CurrentChapter > 0
}

// NextChapter trả về số thứ tự chương tiếp theo cần viết.
func (p *Progress) NextChapter() int {
	return p.LatestCompleted() + 1
}

// LatestCompleted trả về số chương đã hoàn thành lớn nhất; trả về 0 nếu chưa có chương nào hoàn thành.
func (p *Progress) LatestCompleted() int {
	max := 0
	for _, ch := range p.CompletedChapters {
		if ch > max {
			max = ch
		}
	}
	return max
}

// ExtractNovelNameFromPremise trích xuất tên sách từ dòng đầu tiên `# Tên sách` (có thể bọc bằng 《》) trong tiền đề.
// Đôi khi mô hình sẽ chép lại placeholder từ prompt thay vì tạo tên thật, các giá trị đó
// được coi là chưa trích xuất và trả về chuỗi rỗng,
// để tầng trên xử lý dự phòng (UI hiển thị "Chưa đặt tên"), tránh hiển thị trực tiếp chữ "Tên sách".
//
// Lưu ý: trả về chuỗi rỗng ở đây CHỈ để hiển thị an toàn, không phải để nuốt lỗi. Đường ghi
// (save_foundation) phải gọi ValidatePremise và từ chối thẳng — xem foundation.go.
func ExtractNovelNameFromPremise(premise string) string {
	return ExtractNovelName(premise)
}

// ContextProfile chiến lược tải ngữ cảnh, tự động điều chỉnh theo tổng số chương.
type ContextProfile struct {
	SummaryWindow  int  // tải tóm tắt N chương gần nhất
	TimelineWindow int  // tải dòng thời gian N chương gần nhất
	Layered        bool // true = bật tải tóm tắt phân tầng (tóm tắt tập + cung truyện + chương)
}

// MemoryPolicy biểu thị chính sách sử dụng bộ nhớ được chia sẻ lúc chạy.
// Được dùng cho cả đầu ra ngữ cảnh lẫn quyết định handoff / reminder ở tầng host.
type MemoryPolicy struct {
	Mode                string `json:"mode,omitempty"`
	SummaryWindow       int    `json:"summary_window,omitempty"`
	TimelineWindow      int    `json:"timeline_window,omitempty"`
	LayeredSummaries    bool   `json:"layered_summaries,omitempty"`
	SummaryStrategy     string `json:"summary_strategy,omitempty"`
	WorkingRefresh      string `json:"working_refresh,omitempty"`
	EpisodicRefresh     string `json:"episodic_refresh,omitempty"`
	PlanningRefresh     string `json:"planning_refresh,omitempty"`
	FoundationRefresh   string `json:"foundation_refresh,omitempty"`
	PlanningFocus       string `json:"planning_focus,omitempty"`
	FoundationFocus     string `json:"foundation_focus,omitempty"`
	PreviousTailChars   int    `json:"previous_tail_chars,omitempty"`
	ChapterPlanEnabled  bool   `json:"chapter_plan_enabled,omitempty"`
	RelatedLookup       bool   `json:"related_chapter_lookup,omitempty"`
	CurrentOutlineBound bool   `json:"current_outline_bound,omitempty"`
	TotalChapters       int    `json:"total_chapters,omitempty"`
	HandoffPreferred    bool   `json:"handoff_preferred,omitempty"`
	ReadOnlyThreshold   int    `json:"read_only_threshold,omitempty"`
}

// NewContextProfile tính toán chiến lược ngữ cảnh dựa trên tổng số chương.
func NewContextProfile(totalChapters int) ContextProfile {
	switch {
	case totalChapters <= 15:
		return ContextProfile{SummaryWindow: 10, TimelineWindow: 10}
	case totalChapters <= 50:
		return ContextProfile{SummaryWindow: 5, TimelineWindow: 8}
	default:
		return ContextProfile{SummaryWindow: 3, TimelineWindow: 5, Layered: true}
	}
}

// NewChapterMemoryPolicy tạo chính sách bộ nhớ lúc chạy cho chương dựa trên tiến trình và chiến lược ngữ cảnh.
func NewChapterMemoryPolicy(progress *Progress, profile ContextProfile, currentOutlineBound bool) MemoryPolicy {
	policy := MemoryPolicy{
		Mode:                "chapter",
		SummaryWindow:       profile.SummaryWindow,
		TimelineWindow:      profile.TimelineWindow,
		LayeredSummaries:    profile.Layered,
		WorkingRefresh:      "Làm mới mỗi lần tải theo chương",
		EpisodicRefresh:     "Làm mới khi lưu chương, đánh giá và thay đổi trạng thái truyện dài",
		PreviousTailChars:   800,
		ChapterPlanEnabled:  true,
		CurrentOutlineBound: currentOutlineBound,
		ReadOnlyThreshold:   5,
	}
	if profile.Layered {
		policy.SummaryStrategy = "Tóm tắt tập + tóm tắt cung truyện + tóm tắt chương gần nhất"
	} else {
		policy.SummaryStrategy = "Tóm tắt chương gần nhất"
	}
	if progress != nil {
		policy.TotalChapters = progress.TotalChapters
		if progress.TotalChapters > 30 {
			policy.RelatedLookup = true
		}
		if progress.Flow == FlowReviewing || progress.Flow == FlowRewriting || progress.Flow == FlowPolishing {
			policy.HandoffPreferred = true
		}
		if progress.Layered && len(progress.CompletedChapters) >= 6 {
			policy.HandoffPreferred = true
		}
		if len(progress.CompletedChapters) >= 12 {
			policy.HandoffPreferred = true
		}
		if progress.Layered && len(progress.CompletedChapters) >= 6 {
			policy.ReadOnlyThreshold = 4
		}
		if len(progress.CompletedChapters) >= 12 {
			policy.ReadOnlyThreshold = 4
		}
	}
	return policy
}

// NewArchitectMemoryPolicy trả về chính sách bộ nhớ dùng trong giai đoạn lập kế hoạch.
func NewArchitectMemoryPolicy() MemoryPolicy {
	return MemoryPolicy{
		Mode:               "architect",
		PlanningRefresh:    "Làm mới khi cập nhật cấu trúc tập/cung truyện, la bàn hoặc tóm tắt",
		FoundationRefresh:  "Làm mới khi thay đổi nhân vật, phục bút, thiết định",
		PlanningFocus:      "Đề cương phân tầng, la bàn, tóm tắt tập",
		FoundationFocus:    "Hồ sơ nhân vật, snapshot nhân vật, sổ theo dõi phục bút",
		HandoffPreferred:   true,
		ChapterPlanEnabled: false,
		ReadOnlyThreshold:  4,
	}
}

// RunMeta thông tin meta lúc chạy, lưu vào meta/run.json.
type RunMeta struct {
	StartedAt    string       `json:"started_at"`
	Provider     string       `json:"provider,omitempty"`
	Style        string       `json:"style"`
	Model        string       `json:"model"`
	PlanningTier PlanningTier `json:"planning_tier,omitempty"`
	SteerHistory []SteerEntry `json:"steer_history,omitempty"`
	PendingSteer string       `json:"pending_steer,omitempty"` // lệnh Steer chưa hoàn thành, tái nạp khi khôi phục từ gián đoạn
}

// SteerEntry bản ghi can thiệp của người dùng.
type SteerEntry struct {
	Input     string `json:"input"`
	Timestamp string `json:"timestamp"`
}

// UserDirective yêu cầu sáng tác dài hạn do người dùng đưa ra, có hiệu lực liên tục qua các chương.
// Lưu vào meta/user_directives.json, được novel_context nạp vào
// working_memory.user_directives để tất cả các agent phụ tuân thủ.
//
// Chapter/TotalChapters là snapshot tiến trình tại thời điểm đưa ra chỉ thị: giúp chỉ thị có
// điểm bắt đầu rõ ràng (không áp dụng ngược về trước), đồng thời cho phép bên đọc
// xác định các chỉ thị tương đối được lưu nhầm (ví dụ "thêm 10 chương") là đã thực hiện,
// thay vì mỗi lần đọc lại lại thực thi một lần nữa.
type UserDirective struct {
	Text          string `json:"text"`
	Chapter       int    `json:"chapter"`        // tiến trình viết tại thời điểm đưa ra chỉ thị
	TotalChapters int    `json:"total_chapters"` // tổng số chương theo kế hoạch tại thời điểm đưa ra chỉ thị
	CreatedAt     string `json:"created_at"`     // RFC3339
}
