package domain

// TimelineEvent sự kiện trên dòng thời gian.
type TimelineEvent struct {
	Chapter    int      `json:"chapter"`
	Time       string   `json:"time"`
	Event      string   `json:"event"`
	Characters []string `json:"characters,omitempty"`
}

// ForeshadowEntry điều mục phục bút.
type ForeshadowEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	PlantedAt   int    `json:"planted_at"`
	Status      string `json:"status"` // planted / advanced / resolved
	ResolvedAt  int    `json:"resolved_at,omitempty"`
}

// ForeshadowUpdate thao tác cập nhật tăng dần phục bút.
type ForeshadowUpdate struct {
	ID          string `json:"id"`
	Action      string `json:"action"` // plant / advance / resolve
	Description string `json:"description,omitempty"`
}

// RelationshipEntry điều mục quan hệ nhân vật.
type RelationshipEntry struct {
	CharacterA string `json:"character_a"`
	CharacterB string `json:"character_b"`
	Relation   string `json:"relation"`
	Chapter    int    `json:"chapter"`
}

// ConsistencyIssue vấn đề tính nhất quán.
type ConsistencyIssue struct {
	Type        string `json:"type"`     // consistency / character / pacing / engagement / continuity / foreshadow / hook / aesthetic
	Severity    string `json:"severity"` // critical / error / warning
	Description string `json:"description"`
	Evidence    string `json:"evidence,omitempty"` // bằng chứng: đoạn nguyên văn, tình tiết cụ thể hoặc dữ liệu trạng thái
	Suggestion  string `json:"suggestion,omitempty"`
}

// DimensionScore điểm đánh giá theo từng chiều.
type DimensionScore struct {
	Dimension string `json:"dimension"`         // consistency / character / pacing / engagement / continuity / foreshadow / hook / aesthetic
	Score     int    `json:"score"`             // 0-100
	Verdict   string `json:"verdict"`           // pass / warning / fail
	Comment   string `json:"comment,omitempty"` // kết luận ngắn gọn cho chiều này
}

// ReviewEntry điều mục đánh giá của Biên tập viên.
type ReviewEntry struct {
	Chapter          int                `json:"chapter"`
	Scope            string             `json:"scope"` // chapter / global / arc
	Issues           []ConsistencyIssue `json:"issues"`
	Dimensions       []DimensionScore   `json:"dimensions,omitempty"`      // điểm theo từng chiều
	ContractStatus   string             `json:"contract_status,omitempty"` // met / partial / missed
	ContractMisses   []string           `json:"contract_misses,omitempty"` // các điều khoản contract chưa đạt
	ContractNotes    string             `json:"contract_notes,omitempty"`  // mô tả ngắn về mức độ thực hiện contract
	Verdict          string             `json:"verdict"`                   // accept / polish / rewrite
	Summary          string             `json:"summary"`
	AffectedChapters []int              `json:"affected_chapters,omitempty"` // số chương cần viết lại/đánh bóng
}

// CriticalCount trả về số lượng vấn đề ở mức độ critical.
func (r *ReviewEntry) CriticalCount() int {
	n := 0
	for _, issue := range r.Issues {
		if issue.Severity == "critical" {
			n++
		}
	}
	return n
}

// ErrorCount trả về số lượng vấn đề ở mức độ error.
func (r *ReviewEntry) ErrorCount() int {
	n := 0
	for _, issue := range r.Issues {
		if issue.Severity == "error" {
			n++
		}
	}
	return n
}

// Dimension trả về điểm của chiều chỉ định; trả về nil nếu không tồn tại.
func (r *ReviewEntry) Dimension(name string) *DimensionScore {
	if r == nil {
		return nil
	}
	for i := range r.Dimensions {
		if r.Dimensions[i].Dimension == name {
			return &r.Dimensions[i]
		}
	}
	return nil
}
