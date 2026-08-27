package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// PlanChapterTool lưu ý tưởng xây dựng chương, Agent tự quyết định độ chi tiết khi lập kế hoạch.
type PlanChapterTool struct {
	store *store.Store
}

func NewPlanChapterTool(store *store.Store) *PlanChapterTool {
	return &PlanChapterTool{store: store}
}

func (t *PlanChapterTool) Name() string { return "plan_chapter" }
func (t *PlanChapterTool) Description() string {
	return "Lưu kiến trúc kịch tính của chương: mục tiêu, cái giá, chuỗi áp lực, bước ngoặt, lựa chọn, hậu quả và điểm móc. Không dùng như bản tóm tắt sự kiện"
}
func (t *PlanChapterTool) Label() string { return "Lập kế hoạch chương" }

// Công cụ ghi, cấm chạy song song.
func (t *PlanChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *PlanChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *PlanChapterTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int("Số chương")).Required(),
		schema.Property("title", schema.String("Tiêu đề chương")).Required(),
		schema.Property("goal", schema.String("Mục tiêu cụ thể nhân vật điểm nhìn muốn đạt trong chương")).Required(),
		schema.Property("conflict", schema.String("Lực cản có chủ thể/ý chí hoặc hoàn cảnh trực tiếp chống lại mục tiêu")).Required(),
		schema.Property("opening_pressure", schema.String("Ma sát, nguy cơ hoặc câu hỏi cụ thể xuất hiện trong ba đoạn đầu; không dùng mô tả không khí chung")).Required(),
		schema.Property("stakes", schema.String("Cái giá cụ thể nếu thất bại, trì hoãn hoặc bị lộ; nêu ai/cái gì bị mất")).Required(),
		schema.Property("pressure_chain", schema.Array("Ít nhất hai nấc áp lực theo nhân quả. Mỗi nấc nêu lực cản tăng lên, lựa chọn/phản ứng của nhân vật và tình thế bị đổi", schema.String(""))).Required(),
		schema.Property("turning_point", schema.String("Phát hiện, phản đòn hoặc biến cố giữa chương làm đổi cách hiểu hay cán cân; phải nảy sinh từ chi tiết đã có")).Required(),
		schema.Property("character_choice", schema.String("Lựa chọn chủ động bộc lộ tính cách và buộc nhân vật chấp nhận một đánh đổi thật")).Required(),
		schema.Property("consequence", schema.String("Tình thế, dấu vết, món nợ hoặc rủi ro còn lại sau lựa chọn; cuối chương không được trở về nguyên trạng")).Required(),
		schema.Property("hook", schema.String("Điểm móc cuối chương")).Required(),
		schema.Property("emotion_arc", schema.String("Cung cảm xúc")),
		schema.Property("notes", schema.String("Ghi chú tự do (bất cứ điều gì bạn cần nhớ khi viết)")),
		schema.Property("required_beats", schema.Array("Các nhịp truyện bắt buộc phải hoàn thành trong chương", schema.String(""))),
		schema.Property("forbidden_moves", schema.Array("Các diễn biến rõ ràng không được xảy ra trong chương", schema.String(""))),
		schema.Property("continuity_checks", schema.Array("Các điểm liên tục cần kiểm tra đặc biệt trong chương", schema.String(""))),
		schema.Property("evaluation_focus", schema.Array("Các hạng mục Biên tập viên cần kiểm tra trọng tâm", schema.String(""))),
		schema.Property("emotion_target", schema.String("Tùy chọn: cảm xúc chính mà chương muốn độc giả cảm nhận")),
		schema.Property("payoff_points", schema.Array("Tùy chọn: các điểm cốt truyện hoặc điểm trả lời mà chương then chốt muốn hồi đáp", schema.String(""))),
		schema.Property("hook_goal", schema.String("Tùy chọn: ham muốn đọc tiếp hoặc mục tiêu gây căng thẳng mà cuối chương muốn tạo ra")),
	)
}

func (t *PlanChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	plan, err := decodeChapterPlanArgs(args)
	if err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if plan.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if err := validateChapterPlanDrama(plan); err != nil {
		return nil, fmt.Errorf("chapter plan is too shallow: %w: %w", err, errs.ErrToolArgs)
	}
	if t.store.Progress.IsChapterCompleted(plan.Chapter) {
		return json.Marshal(map[string]any{
			"chapter":   plan.Chapter,
			"skipped":   true,
			"completed": true,
			"reason":    fmt.Sprintf("Chương %d đã được lưu hoàn thành, không thể lập kế hoạch lại", plan.Chapter),
		})
	}
	if err := t.store.Progress.ValidateChapterWork(plan.Chapter); err != nil {
		return nil, err
	}
	if err := requireOutlineEntry(t.store, plan.Chapter); err != nil {
		return nil, err
	}

	if err := t.store.Drafts.SaveChapterPlan(plan); err != nil {
		return nil, fmt.Errorf("save chapter plan: %w", err)
	}
	if err := t.store.Progress.StartChapter(plan.Chapter); err != nil {
		return nil, fmt.Errorf("mark chapter in progress: %w", err)
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(plan.Chapter), "plan",
		fmt.Sprintf("drafts/%02d.plan.json", plan.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint chapter plan: %w", err)
	}

	return json.Marshal(map[string]any{
		"planned":   true,
		"chapter":   plan.Chapter,
		"next_step": "Ngay lập tức gọi draft_chapter(chapter=số_chương_này, content=chuỗi_nội_dung_đầy_đủ) để viết nội dung, không lập kế hoạch lại cùng một chương",
	})
}

// requireOutlineEntry từ chối làm việc trên chương chưa có trong đề cương — guard chung của
// plan_chapter VÀ draft_chapter.
//
// Không có mục đề cương nghĩa là novel_context không cấp được title/core_event cho chương này, và Writer
// buộc phải bịa toàn bộ nội dung từ chương trước. Trước đây tầng này im lặng cho qua: Writer bịa kế hoạch,
// đến commit_chapter mới bị chặn vì "ngoài phạm vi đề cương" — sau khi đã đốt một lượt sinh văn bản đầy đủ.
// Chặn ngay tại đây biến lỗi mơ hồ đó thành một thông báo nói rõ ai phải làm gì tiếp theo.
//
// Guard phải nằm ở CẢ draft_chapter chứ không riêng plan_chapter: sự cố 2026-07-16 ("Loạn Thế Võ Đạo"
// lần nhập lại), plan_chapter chặn đúng "đề cương hiện có 0 chương" nhưng Writer phớt lờ lỗi và gọi
// thẳng draft_chapter — vốn không có guard — rồi StartChapter đẩy phase sang writing, 6 chương được
// viết không kế hoạch. Đường viết nào cũng phải đi qua cùng một cửa.
func requireOutlineEntry(s *store.Store, chapter int) error {
	entry, err := s.Outline.GetChapterOutline(chapter)
	if err == nil && entry != nil {
		return nil
	}
	total := len(mustLoadOutline(s))
	if total == 0 {
		return fmt.Errorf(
			"chương %d chưa có trong đề cương (đề cương hiện có 0 chương — bộ nền chưa có đề cương nào): Writer không được tự bịa chương ngoài kế hoạch. "+
				"Hãy để Kiến trúc sư gọi save_foundation type=layered_outline lưu đề cương hoàn chỉnh trước, rồi mới viết: %w",
			chapter, errs.ErrToolPrecondition)
	}
	return fmt.Errorf(
		"chương %d chưa có trong đề cương (đề cương hiện có %d chương): Writer không được tự bịa chương ngoài kế hoạch. "+
			"Hãy để Kiến trúc sư gọi save_foundation type=expand_arc (mở rộng cung khung tiếp theo) hoặc type=append_volume (thêm tập mới) trước, rồi mới viết: %w",
		chapter, total, errs.ErrToolPrecondition)
}

// mustLoadOutline trả về đề cương phẳng, danh sách rỗng khi đọc lỗi (chỉ dùng cho thông báo lỗi).
func mustLoadOutline(s *store.Store) []domain.OutlineEntry {
	entries, err := s.Outline.LoadOutline()
	if err != nil {
		return nil
	}
	return entries
}

func decodeChapterPlanArgs(args json.RawMessage) (domain.ChapterPlan, error) {
	var a struct {
		Chapter          int      `json:"chapter"`
		Title            string   `json:"title"`
		Goal             string   `json:"goal"`
		Conflict         string   `json:"conflict"`
		OpeningPressure  string   `json:"opening_pressure"`
		Stakes           string   `json:"stakes"`
		PressureChain    []string `json:"pressure_chain"`
		TurningPoint     string   `json:"turning_point"`
		CharacterChoice  string   `json:"character_choice"`
		Consequence      string   `json:"consequence"`
		Hook             string   `json:"hook"`
		EmotionArc       string   `json:"emotion_arc"`
		Notes            string   `json:"notes"`
		RequiredBeats    []string `json:"required_beats"`
		ForbiddenMoves   []string `json:"forbidden_moves"`
		ContinuityChecks []string `json:"continuity_checks"`
		EvaluationFocus  []string `json:"evaluation_focus"`
		EmotionTarget    string   `json:"emotion_target"`
		PayoffPoints     []string `json:"payoff_points"`
		HookGoal         string   `json:"hook_goal"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return domain.ChapterPlan{}, err
	}

	return domain.ChapterPlan{
		Chapter:         a.Chapter,
		Title:           a.Title,
		Goal:            a.Goal,
		Conflict:        a.Conflict,
		OpeningPressure: a.OpeningPressure,
		Stakes:          a.Stakes,
		PressureChain:   a.PressureChain,
		TurningPoint:    a.TurningPoint,
		CharacterChoice: a.CharacterChoice,
		Consequence:     a.Consequence,
		Hook:            a.Hook,
		EmotionArc:      a.EmotionArc,
		Notes:           a.Notes,
		Contract: domain.ChapterContract{
			RequiredBeats:    a.RequiredBeats,
			ForbiddenMoves:   a.ForbiddenMoves,
			ContinuityChecks: a.ContinuityChecks,
			EvaluationFocus:  a.EvaluationFocus,
			EmotionTarget:    a.EmotionTarget,
			PayoffPoints:     a.PayoffPoints,
			HookGoal:         a.HookGoal,
		},
	}, nil
}

// validateChapterPlanDrama là cổng tối thiểu chống kế hoạch kiểu "đến nơi -> quan sát -> nhận đồ -> hết chương".
// Tool schema giúp model điền đúng, nhưng Execute vẫn tự bảo vệ vì lời gọi có thể đến từ client cũ
// hoặc đường khôi phục không đi qua lớp xác thực schema.
func validateChapterPlanDrama(plan domain.ChapterPlan) error {
	required := []struct {
		name  string
		value string
	}{
		{"title", plan.Title},
		{"goal", plan.Goal},
		{"conflict", plan.Conflict},
		{"opening_pressure", plan.OpeningPressure},
		{"stakes", plan.Stakes},
		{"turning_point", plan.TurningPoint},
		{"character_choice", plan.CharacterChoice},
		{"consequence", plan.Consequence},
		{"hook", plan.Hook},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if len(plan.PressureChain) < 2 {
		return fmt.Errorf("pressure_chain needs at least 2 escalating steps")
	}
	for i, step := range plan.PressureChain {
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("pressure_chain[%d] must not be empty", i)
		}
	}
	return nil
}
