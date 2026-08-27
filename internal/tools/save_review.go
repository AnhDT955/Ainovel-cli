package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveReviewTool lưu kết quả đánh giá của Biên tập viên.
type SaveReviewTool struct {
	store *store.Store
}

func NewSaveReviewTool(store *store.Store) *SaveReviewTool {
	return &SaveReviewTool{store: store}
}

func (t *SaveReviewTool) Name() string { return "save_review" }
func (t *SaveReviewTool) Description() string {
	return "Lưu kết quả đánh giá và cập nhật trạng thái luồng. verdict là một trong accept/polish/rewrite. " +
		"Công cụ thực hiện cổng kiểm tra thẻ điểm nội bộ (có thể nâng cấp verdict), trực tiếp cập nhật flow và pending_rewrites của Progress. " +
		"Trả về dữ liệu thực tế có cấu trúc: final_verdict / affected_chapters / escalation_reason / next_flow / next_chapter"
}
func (t *SaveReviewTool) Label() string { return "Lưu đánh giá" }

// Công cụ ghi (đồng thời cập nhật reviews/ và PendingRewrites/Flow của Progress), cấm chạy đồng thời.
func (t *SaveReviewTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveReviewTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveReviewTool) Schema() map[string]any {
	issueSchema := schema.Object(
		schema.Property("type", schema.Enum("Chiều vấn đề", "consistency", "character", "pacing", "engagement", "continuity", "foreshadow", "hook", "aesthetic")).Required(),
		schema.Property("severity", schema.Enum("Mức độ nghiêm trọng", "critical", "error", "warning")).Required(),
		schema.Property("description", schema.String("Mô tả vấn đề")).Required(),
		schema.Property("evidence", schema.String("Bằng chứng: đoạn trích nguyên văn, tình tiết cụ thể hoặc dữ liệu trạng thái")).Required(),
		schema.Property("suggestion", schema.String("Đề xuất chỉnh sửa")),
	)
	dimensionSchema := schema.Object(
		schema.Property("dimension", schema.Enum("Chiều", "consistency", "character", "pacing", "engagement", "continuity", "foreshadow", "hook", "aesthetic")).Required(),
		schema.Property("score", schema.Int("Điểm số (0-100)")).Required(),
		schema.Property("verdict", schema.Enum("Kết luận chiều (có thể bỏ qua: hệ thống tự suy luận theo score, ≥80 pass / ≥60 warning / <60 fail)", "pass", "warning", "fail")),
		schema.Property("comment", schema.String("Kết luận ngắn gọn cho chiều này; mỗi chiều bắt buộc điền, aesthetic phải trích dẫn nguyên văn hoặc số liệu thống kê cụ thể")).Required(),
	)
	return schema.Object(
		schema.Property("chapter", schema.Int("Số chương được đánh giá (đánh giá toàn cục thì điền số chương mới nhất)")).Required(),
		schema.Property("scope", schema.Enum("Phạm vi đánh giá", "chapter", "global", "arc")).Required(),
		schema.Property("dimensions", schema.Array("Điểm theo từng chiều (mỗi chiều một mục, tám chiều)", dimensionSchema)).Required(),
		schema.Property("issues", schema.Array("Các vấn đề phát hiện được", issueSchema)).Required(),
		schema.Property("contract_status", schema.Enum("Mức độ hoàn thành hợp đồng chương", "met", "partial", "missed")),
		schema.Property("contract_misses", schema.Array("Các mục hợp đồng chưa hoàn thành hoặc vi phạm", schema.String(""))),
		schema.Property("contract_notes", schema.String("Ghi chú ngắn về tình trạng thực hiện hợp đồng")),
		schema.Property("verdict", schema.Enum("Kết luận đánh giá", "accept", "polish", "rewrite")).Required(),
		schema.Property("summary", schema.String("Tóm tắt đánh giá")).Required(),
		schema.Property("affected_chapters", schema.Array("Danh sách số chương cần viết lại hoặc trau chuốt (bắt buộc khi verdict là polish/rewrite)", schema.Int(""))),
	)
}

func (t *SaveReviewTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var r domain.ReviewEntry
	if err := json.Unmarshal(args, &r); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if r.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0")
	}
	// verdict là hàm thuần túy của score (≥80 pass / ≥60 warning / <60 fail), được suy luận xác định bởi code —
	// không để LLM cung cấp lại rồi kiểm tra tính nhất quán. Vừa loại bỏ dư thừa, vừa triệt tiêu
	// mâu thuẫn kiểu "score=85 nhưng lại cho warning".
	for i := range r.Dimensions {
		r.Dimensions[i].Verdict = expectedDimensionVerdict(r.Dimensions[i].Score)
	}
	if err := validateReviewEntry(r); err != nil {
		return nil, err
	}

	// Cổng kiểm tra thẻ điểm — logic nâng cấp nội tuyến từ policy/review.go
	finalVerdict := r.Verdict
	var escalationReason string

	if r.Verdict == "accept" {
		// Kiểm tra trạng thái hợp đồng
		if r.ContractStatus == "missed" {
			finalVerdict = "rewrite"
			escalationReason = "Trạng thái thực hiện hợp đồng là missed, nâng cấp thành rewrite"
		} else if r.ContractStatus == "partial" {
			finalVerdict = "polish"
			escalationReason = "Trạng thái thực hiện hợp đồng là partial, nâng cấp thành polish"
		}
		// Cổng kiểm tra thẻ điểm
		if finalVerdict == "accept" {
			if gate := evaluateScorecardGate(r.Dimensions); gate != "" {
				if strings.Contains(gate, "rewrite") {
					finalVerdict = "rewrite"
				} else {
					finalVerdict = "polish"
				}
				escalationReason = gate
			}
		}
	}

	affected := r.AffectedChapters
	if finalVerdict == "rewrite" || finalVerdict == "polish" {
		if len(affected) == 0 && r.Chapter > 0 {
			affected = []int{r.Chapter}
		}
		if err := t.store.Progress.ValidatePendingRewrites(affected); err != nil {
			return nil, fmt.Errorf("validate pending rewrites: %w", err)
		}
	}

	if err := t.store.World.SaveReview(r); err != nil {
		return nil, fmt.Errorf("save review: %w", err)
	}

	// Cập nhật Progress theo final verdict.
	// Nếu ghi thất bại phải trả về sớm — sau đó sẽ append checkpoint đánh giá, nếu nuốt err ở đây
	// Điều phối viên sẽ thấy saved:true nhưng Store vẫn ở trạng thái trung gian với Flow cũ / thiếu PendingRewrites.
	progress, _ := t.store.Progress.Load()
	if finalVerdict == "rewrite" || finalVerdict == "polish" {
		flow := domain.FlowRewriting
		if finalVerdict == "polish" {
			flow = domain.FlowPolishing
		}
		if err := t.store.Progress.SetPendingRewrites(affected, r.Summary); err != nil {
			return nil, fmt.Errorf("set pending rewrites: %w", err)
		}
		if err := t.store.Progress.SetFlow(flow); err != nil {
			return nil, fmt.Errorf("set flow %s: %w", flow, err)
		}
	} else {
		if err := t.store.Progress.SetFlow(domain.FlowWriting); err != nil {
			return nil, fmt.Errorf("set flow writing: %w", err)
		}
	}

	// Đọc snapshot Progress đã cập nhật làm dữ liệu thực tế
	latest, _ := t.store.Progress.Load()
	nextFlow := string(domain.FlowWriting)
	nextChapter := 0
	if latest != nil {
		nextFlow = string(latest.Flow)
		nextChapter = latest.NextChapter()
	}

	// Thêm điểm khôi phục. Tập/cung tra theo chính chương được đánh giá — Progress.CurrentVolume/CurrentArc
	// mô tả chương commit gần nhất nên có thể lệch một cung, và bằng 0 khi đề cương chưa từng gán Index
	// (xem store.ArcPosition). Nhãn checkpoint sai thì khôi phục nhắm nhầm chỗ.
	scope := domain.ChapterScope(r.Chapter)
	if r.Scope == "arc" {
		vol, arc := 0, 0
		if p, perr := t.store.Outline.LocateChapterPosition(r.Chapter); perr == nil && p != nil {
			vol, arc = p.Volume, p.Arc
		} else if progress != nil {
			vol, arc = progress.CurrentVolume, progress.CurrentArc
		}
		scope = domain.ArcScope(vol, arc)
	}
	if _, err := t.store.Checkpoints.AppendArtifact(scope, "review", store.ReviewArtifact(r.Chapter, r.Scope)); err != nil {
		return nil, fmt.Errorf("checkpoint review: %w", err)
	}

	return json.Marshal(map[string]any{
		"saved":             true,
		"chapter":           r.Chapter,
		"scope":             r.Scope,
		"verdict":           r.Verdict,
		"final_verdict":     finalVerdict,
		"escalation_reason": escalationReason,
		"affected_chapters": affected,
		"issues":            len(r.Issues),
		"next_flow":         nextFlow,
		"next_chapter":      nextChapter,
	})
}

var expectedReviewDimensions = map[string]struct{}{
	"consistency": {},
	"character":   {},
	"pacing":      {},
	"engagement":  {},
	"continuity":  {},
	"foreshadow":  {},
	"hook":        {},
	"aesthetic":   {},
}

func validateReviewEntry(r domain.ReviewEntry) error {
	if strings.TrimSpace(r.Scope) == "" {
		return fmt.Errorf("scope is required")
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	for _, issue := range r.Issues {
		if strings.TrimSpace(issue.Description) == "" {
			return fmt.Errorf("issue description is required")
		}
		if strings.TrimSpace(issue.Evidence) == "" {
			return fmt.Errorf("issue evidence is required")
		}
	}
	if err := validateDimensions(r.Dimensions); err != nil {
		return err
	}
	if (r.Verdict == "rewrite" || r.Verdict == "polish") && len(r.AffectedChapters) == 0 {
		return fmt.Errorf("affected_chapters is required when verdict=%s", r.Verdict)
	}
	return nil
}

func validateDimensions(dimensions []domain.DimensionScore) error {
	if len(dimensions) != len(expectedReviewDimensions) {
		return fmt.Errorf("dimensions must contain exactly %d entries", len(expectedReviewDimensions))
	}

	seen := make(map[string]struct{}, len(dimensions))
	for _, dim := range dimensions {
		if _, ok := expectedReviewDimensions[dim.Dimension]; !ok {
			return fmt.Errorf("unknown dimension: %s", dim.Dimension)
		}
		if _, ok := seen[dim.Dimension]; ok {
			return fmt.Errorf("duplicate dimension: %s", dim.Dimension)
		}
		seen[dim.Dimension] = struct{}{}
		if dim.Score < 0 || dim.Score > 100 {
			return fmt.Errorf("invalid score for %s: %d", dim.Dimension, dim.Score)
		}
		if strings.TrimSpace(dim.Comment) == "" {
			return fmt.Errorf("dimension comment is required: %s", dim.Dimension)
		}
	}
	return nil
}

func expectedDimensionVerdict(score int) string {
	switch {
	case score >= 80:
		return "pass"
	case score >= 60:
		return "warning"
	default:
		return "fail"
	}
}

// criticalDimensions định nghĩa các chiều quan trọng sẽ kích hoạt nâng cấp verdict.
var criticalDimensions = map[string]struct{}{
	"consistency": {},
	"character":   {},
	"engagement":  {},
	"continuity":  {},
}

// evaluateScorecardGate kiểm tra xem thẻ điểm có cần nâng cấp verdict không.
// Trả về chuỗi rỗng nghĩa là không nâng cấp.
func evaluateScorecardGate(dimensions []domain.DimensionScore) string {
	var criticalFails []string
	var polishIssues []string

	for _, dim := range dimensions {
		_, isCritical := criticalDimensions[dim.Dimension]
		if isCritical && (dim.Verdict == "fail" || dim.Score < 60) {
			criticalFails = append(criticalFails, fmt.Sprintf("%s(%d)", dim.Dimension, dim.Score))
		} else if dim.Verdict == "warning" || (isCritical && dim.Score < 80) {
			polishIssues = append(polishIssues, fmt.Sprintf("%s(%d)", dim.Dimension, dim.Score))
		}
	}

	if len(criticalFails) > 0 {
		return fmt.Sprintf("rewrite: chiều quan trọng không đạt chuẩn %v", criticalFails)
	}
	if len(polishIssues) > 0 {
		return fmt.Sprintf("polish: một số chiều cần trau chuốt thêm %v", polishIssues)
	}
	return ""
}
