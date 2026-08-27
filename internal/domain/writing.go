package domain

// ChapterPlan là kiến trúc lực kéo của một chương, do Người viết tự tạo.
// Không bắt buộc chia cảnh cứng, nhưng phải xác định chuỗi áp lực, bước ngoặt, lựa chọn và hậu quả.
type ChapterPlan struct {
	Chapter         int             `json:"chapter"`
	Title           string          `json:"title"`
	Goal            string          `json:"goal"`
	Conflict        string          `json:"conflict"`
	OpeningPressure string          `json:"opening_pressure"` // Ma sát/nguy cơ cụ thể xuất hiện ngay đầu chương
	Stakes          string          `json:"stakes"`           // Cái giá cụ thể nếu thất bại, trì hoãn hoặc để lộ ý đồ
	PressureChain   []string        `json:"pressure_chain"`   // Ít nhất hai nấc: lực cản tăng -> lựa chọn -> tình thế đổi
	TurningPoint    string          `json:"turning_point"`    // Phát hiện/phản đòn làm đổi cách hiểu hoặc cán cân
	CharacterChoice string          `json:"character_choice"` // Lựa chọn bộc lộ tính cách, có đánh đổi thật
	Consequence     string          `json:"consequence"`      // Dấu vết/nợ/rủi ro/tình thế mới còn lại sau lựa chọn
	Hook            string          `json:"hook"`
	EmotionArc      string          `json:"emotion_arc,omitempty"`
	Notes           string          `json:"notes,omitempty"` // Ghi chú tự do của Agent
	Contract        ChapterContract `json:"contract,omitempty"`
}

// ChapterContract là hợp đồng nghiệm thu chương, dùng chung giữa Người viết và Biên tập viên.
// Định nghĩa các nhịp truyện bắt buộc, các nước đi bị cấm, và các điểm cần kiểm tra.
type ChapterContract struct {
	RequiredBeats    []string `json:"required_beats,omitempty"`    // Các nhịp truyện phải hoàn thành trong chương
	ForbiddenMoves   []string `json:"forbidden_moves,omitempty"`   // Các diễn biến rõ ràng không được xảy ra trong chương
	ContinuityChecks []string `json:"continuity_checks,omitempty"` // Các điểm liên tục cần đặc biệt kiểm tra trong chương
	EvaluationFocus  []string `json:"evaluation_focus,omitempty"`  // Các điểm Biên tập viên cần kiểm tra trọng tâm
	EmotionTarget    string   `json:"emotion_target,omitempty"`    // Tùy chọn: cảm xúc chính chương muốn độc giả cảm nhận
	PayoffPoints     []string `json:"payoff_points,omitempty"`     // Tùy chọn: các điểm cốt truyện/điểm hồi đáp mà chương then chốt muốn giải quyết
	HookGoal         string   `json:"hook_goal,omitempty"`         // Tùy chọn: điểm móc cuối chương muốn tạo ra sức cuốn đọc tiếp
}

// ChapterSummary là tóm tắt chương, dùng cho cửa sổ ngữ cảnh của các chương tiếp theo.
type ChapterSummary struct {
	Chapter    int      `json:"chapter"`
	Summary    string   `json:"summary"`
	Characters []string `json:"characters"`
	KeyEvents  []string `json:"key_events"`
}

// ArcSummary là tóm tắt cấp cung truyện, do Biên tập viên tạo khi kết thúc cung truyện.
type ArcSummary struct {
	Volume    int      `json:"volume"`
	Arc       int      `json:"arc"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyEvents []string `json:"key_events"`
}

// VolumeSummary là tóm tắt cấp tập, được tạo khi kết thúc tập.
type VolumeSummary struct {
	Volume    int      `json:"volume"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyEvents []string `json:"key_events"`
}

// CharacterSnapshot là ảnh chụp trạng thái nhân vật, được ghi lại tại ranh giới cung truyện.
type CharacterSnapshot struct {
	Volume     int    `json:"volume"`
	Arc        int    `json:"arc"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Power      string `json:"power,omitempty"`
	Motivation string `json:"motivation"`
	Relations  string `json:"relations,omitempty"`
}

// OutlineFeedback là phản hồi của Người viết về đề cương, có thể gửi khi lưu chương.
type OutlineFeedback struct {
	Deviation  string `json:"deviation"`  // Mô tả sự lệch khỏi đề cương
	Suggestion string `json:"suggestion"` // Đề xuất điều chỉnh
}

// WritingStyleRules là các quy tắc phong cách viết được rút ra từ các chương đã viết, do Biên tập viên tạo tại ranh giới cung truyện.
// Thay thế các đoạn văn gốc (style_anchors / voice_samples), dùng quy tắc thay vì sao chép nguyên văn.
type WritingStyleRules struct {
	Volume    int              `json:"volume"`
	Arc       int              `json:"arc"`
	Prose     []string         `json:"prose"`      // 3-5 quy tắc phong cách tự sự, mỗi quy tắc ≤50 từ
	Dialogue  []CharacterVoice `json:"dialogue"`   // Quy tắc phong cách đối thoại của nhân vật
	Taboos    []string         `json:"taboos"`     // Danh sách cấm kỵ
	UpdatedAt string           `json:"updated_at"` // Timestamp ISO8601
}

// CharacterVoice là quy tắc phong cách đối thoại của một nhân vật cụ thể.
type CharacterVoice struct {
	Name  string   `json:"name"`
	Rules []string `json:"rules"` // 2-3 quy tắc đặc trưng ngôn ngữ, mỗi quy tắc ≤30 từ
}

// RelatedChapter là chương liên quan được đề xuất đọc lại.
type RelatedChapter struct {
	Chapter int    `json:"chapter"`
	Reason  string `json:"reason"`
}

// RecallItem là thông tin lịch sử dài hạn được chọn lọc để hồi tưởng theo nhiệm vụ hiện tại.
// Không thay thế các sản phẩm chính thức; chỉ đảm nhiệm việc bơm lại một lượng nhỏ thông tin lịch sử thực sự liên quan vào model cho lượt hiện tại.
type RecallItem struct {
	Kind    string `json:"kind"`
	Key     string `json:"key,omitempty"`
	Chapter int    `json:"chapter,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// CommitResult là giá trị trả về có cấu trúc của công cụ commit_chapter.
// Chỉ chứa các trường dữ liệu thực tế; "bước tiếp theo làm gì" do kênh Reminder tự tạo dựa trên Progress hiện tại.
type CommitResult struct {
	Chapter        int              `json:"chapter"`
	Committed      bool             `json:"committed"`
	WordCount      int              `json:"word_count"`
	NextChapter    int              `json:"next_chapter"`
	ReviewRequired bool             `json:"review_required"`
	ReviewReason   string           `json:"review_reason,omitempty"`
	HookType       string           `json:"hook_type,omitempty"`
	DominantStrand string           `json:"dominant_strand,omitempty"`
	Feedback       *OutlineFeedback `json:"feedback,omitempty"`
	// Tín hiệu phân tầng truyện dài
	ArcEnd         bool `json:"arc_end,omitempty"`
	VolumeEnd      bool `json:"volume_end,omitempty"`
	Volume         int  `json:"volume,omitempty"`
	Arc            int  `json:"arc,omitempty"`
	NeedsExpansion bool `json:"needs_expansion,omitempty"`  // Cung truyện tiếp theo là khung xương, cần mở rộng thành chương
	NeedsNewVolume bool `json:"needs_new_volume,omitempty"` // Cần Kiến trúc sư tạo tập mới
	NextVolume     int  `json:"next_volume,omitempty"`      // Số thứ tự cung truyện/tập tiếp theo
	NextArc        int  `json:"next_arc,omitempty"`         // Số thứ tự cung truyện tiếp theo
	// Dữ liệu hoàn thành: sau lần lưu chương này toàn bộ cuốn sách đã hoàn thành chưa
	BookComplete bool `json:"book_complete,omitempty"`
	// Ảnh chụp Progress.Flow hiện tại (writing / reviewing / rewriting / polishing)
	Flow string `json:"flow,omitempty"`
}
