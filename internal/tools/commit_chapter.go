package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// CommitChapterTool lưu chương: tải nội dung → lưu bản chính → tạo tóm tắt → cập nhật trạng thái → cập nhật tiến độ.
type CommitChapterTool struct {
	store     *store.Store
	rulesOpts rules.LoadOptions // Tùy chọn; khi LoadOptions rỗng sẽ không tạo rule_violations
}

func NewCommitChapterTool(store *store.Store) *CommitChapterTool {
	return &CommitChapterTool{store: store}
}

// WithRules nhận tùy chọn tải quy tắc người dùng, giúp rule_violations kèm theo kết quả kiểm tra quy tắc người dùng.
// Nếu không gọi phương thức này thì chỉ thực thi Lint giới hạn tối thiểu tích hợp sẵn (kiểm tra cơ chế tồn dư, luôn bật).
func (t *CommitChapterTool) WithRules(opts rules.LoadOptions) *CommitChapterTool {
	t.rulesOpts = opts
	return t
}

// commitOutput nhúng thêm trường mở rộng lên trên domain.CommitResult, giữ package domain không phụ thuộc rules.
// Vì trường nhúng được JSON marshaler đưa lên cấp trên (promoted), kết quả serialize tương đương cấu trúc phẳng.
type commitOutput struct {
	domain.CommitResult
	RuleViolations []rules.Violation `json:"rule_violations,omitempty"`
}

func (t *CommitChapterTool) Name() string { return "commit_chapter" }
func (t *CommitChapterTool) Description() string {
	return "Lưu bản chính của chương. Tải bản nháp và lưu thành bản chính, cập nhật timeline, phục bút, quan hệ, trạng thái nhân vật và tiến độ." +
		"Trả về dữ liệu có cấu trúc: next_chapter / review_required / arc_end / volume_end / needs_expansion / book_complete / flow, v.v."
}
func (t *CommitChapterTool) Label() string { return "Lưu chương" }

// Công cụ ghi (thao tác nguyên tử liên miền: bản nháp→bản chính→tóm tắt→tiến độ→điểm khôi phục), cấm đồng thời.
func (t *CommitChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *CommitChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *CommitChapterTool) Schema() map[string]any {
	timelineSchema := schema.Object(
		schema.Property("time", schema.String("Thời gian trong truyện")).Required(),
		schema.Property("event", schema.String("Mô tả sự kiện")).Required(),
		schema.Property("characters", schema.Array("Nhân vật liên quan", schema.String(""))),
	)
	foreshadowSchema := schema.Object(
		schema.Property("id", schema.String("ID phục bút")).Required(),
		schema.Property("action", schema.Enum("Thao tác", "plant", "advance", "resolve")).Required(),
		schema.Property("description", schema.String("Mô tả phục bút (chỉ bắt buộc khi plant)")),
	)
	relationshipSchema := schema.Object(
		schema.Property("character_a", schema.String("Nhân vật A")).Required(),
		schema.Property("character_b", schema.String("Nhân vật B")).Required(),
		schema.Property("relation", schema.String("Mô tả quan hệ hiện tại")).Required(),
	)
	stateChangeSchema := schema.Object(
		schema.Property("entity", schema.String("Tên nhân vật hoặc thực thể")).Required(),
		schema.Property("field", schema.String("Thuộc tính thay đổi")).Required(),
		schema.Property("old_value", schema.String("Giá trị trước khi thay đổi")),
		schema.Property("new_value", schema.String("Giá trị sau khi thay đổi")).Required(),
		schema.Property("reason", schema.String("Lý do thay đổi")),
	)
	feedbackSchema := schema.Object(
		schema.Property("deviation", schema.String("Mô tả sai lệch so với đề cương")).Required(),
		schema.Property("suggestion", schema.String("Đề xuất điều chỉnh đề cương cho phần tiếp theo")).Required(),
	)
	return schema.Object(
		schema.Property("chapter", schema.Int("Số chương")).Required(),
		schema.Property("summary", schema.String("Tóm tắt nội dung chương này (tối đa 200 chữ)")).Required(),
		schema.Property("characters", schema.Array("Tên nhân vật xuất hiện trong chương này", schema.String(""))).Required(),
		schema.Property("key_events", schema.Array("Các sự kiện chính của chương này", schema.String(""))).Required(),
		schema.Property("timeline_events", schema.Array("Sự kiện timeline trong chương này", timelineSchema)),
		schema.Property("foreshadow_updates", schema.Array("Thao tác phục bút", foreshadowSchema)),
		schema.Property("relationship_changes", schema.Array("Thay đổi quan hệ", relationshipSchema)),
		schema.Property("state_changes", schema.Array("Thay đổi trạng thái nhân vật/thực thể", stateChangeSchema)),
		schema.Property("cast_intros", schema.Array("Giới thiệu nhân vật phụ xuất hiện lần đầu trong chương này và có thể xuất hiện lại (không bao gồm nhân vật chính và các nhân vật đã có trong characters.json)", schema.Object(
			schema.Property("name", schema.String("Tên nhân vật")).Required(),
			schema.Property("brief_role", schema.String("Định vị một câu (ví dụ: chủ quán trọ / tay đánh bạc)")).Required(),
		))),
		schema.Property("hook_type", schema.Enum("Loại điểm móc cuối chương", "crisis", "mystery", "desire", "emotion", "choice")),
		schema.Property("dominant_strand", schema.Enum("Tuyến kể chủ đạo của chương này", "quest", "fire", "constellation")),
		schema.Property("feedback", feedbackSchema),
	)
}

func (t *CommitChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter             int                        `json:"chapter"`
		Summary             string                     `json:"summary"`
		Characters          []string                   `json:"characters"`
		KeyEvents           []string                   `json:"key_events"`
		TimelineEvents      []domain.TimelineEvent     `json:"timeline_events"`
		ForeshadowUpdates   []domain.ForeshadowUpdate  `json:"foreshadow_updates"`
		RelationshipChanges []domain.RelationshipEntry `json:"relationship_changes"`
		StateChanges        []domain.StateChange       `json:"state_changes"`
		CastIntros          []domain.CastIntro         `json:"cast_intros"`
		HookType            string                     `json:"hook_type"`
		DominantStrand      string                     `json:"dominant_strand"`
		Feedback            *domain.OutlineFeedback    `json:"feedback"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if t.store.Progress.IsChapterCompleted(a.Chapter) {
		// Dọn dẹp PendingCommit có thể còn tồn đọng (xảy ra khi crash sau ProgressMarked, trước ClearPendingCommit)
		if pending, _ := t.store.Signals.LoadPendingCommit(); pending != nil && pending.Chapter == a.Chapter {
			_ = t.store.Signals.ClearPendingCommit()
		}
		// Đường dẫn đánh bóng/viết lại: chương đã hoàn thành nhưng vẫn trong pending_rewrites, cho phép ghi đè và drain hàng đợi
		progress, _ := t.store.Progress.Load()
		if progress != nil && slices.Contains(progress.PendingRewrites, a.Chapter) {
			return t.executeRewriteCommit(a.Chapter, a.Summary, a.Characters, a.KeyEvents,
				a.HookType, a.DominantStrand, progress)
		}
		return t.buildSkipResult(a.Chapter, progress)
	}
	existingPending, err := t.store.Signals.LoadPendingCommit()
	if err != nil {
		return nil, fmt.Errorf("load pending commit: %w: %w", errs.ErrStoreRead, err)
	}
	if existingPending != nil && existingPending.Chapter != a.Chapter {
		return nil, fmt.Errorf("tồn tại lần lưu chương chưa khôi phục: chương %d (giai đoạn %s), vui lòng khôi phục hoặc nộp lại chương đó trước: %w", existingPending.Chapter, existingPending.Stage, errs.ErrToolConflict)
	}
	if err := t.store.Progress.ValidateChapterWork(a.Chapter); err != nil {
		// Xung đột hàng đợi giữ nguyên (đã có phân loại ErrToolConflict); các lỗi IO khác xếp vào Precondition.
		if errors.Is(err, errs.ErrToolConflict) {
			return nil, err
		}
		return nil, fmt.Errorf("chương hiện tại không được phép lưu: %w: %w", errs.ErrToolPrecondition, err)
	}

	// Chặn vượt giới hạn: phải đặt trước mọi thao tác ghi, nếu không commit vượt giới
	// sẽ làm hỏng file chương, tóm tắt và Progress. boundary được tái sử dụng ở bước 6b để tính tín hiệu cung/cuốn.
	var boundary *store.ArcBoundary
	if progress, perr := t.store.Progress.Load(); perr == nil && progress != nil {
		if progress.Layered {
			b, bErr := t.store.Outline.CheckArcBoundary(a.Chapter)
			if bErr != nil {
				return nil, fmt.Errorf("kiểm tra biên giới cung thất bại chapter=%d: %w: %w", a.Chapter, errs.ErrStoreRead, bErr)
			}
			// b.OutOfRange mang đúng ngữ nghĩa mà nhánh này vẫn luôn chặn; b == nil giờ chỉ còn nghĩa là
			// layered_outline rỗng (không thực sự ở chế độ phân lớp) — vẫn phải chặn vì Progress nói Layered=true.
			if b == nil || b.OutOfRange {
				return nil, fmt.Errorf(
					"chương %d nằm ngoài phạm vi đề cương phân lớp: cần expand_arc để mở rộng cung hoặc append_volume để thêm cuốn trước khi viết; nếu toàn bộ sách đã hoàn thành hãy gọi save_foundation type=complete_book: %w",
					a.Chapter, errs.ErrToolPrecondition)
			}
			boundary = b
		} else if entries, oErr := t.store.Outline.LoadOutline(); oErr == nil {
			// Đề cương phẳng trước đây KHÔNG có bộ chặn nào: chương ngoài đề cương được nhận trong im lặng.
			// Hậu quả: viết xong chương cuối, Router lấy NextChapter() = chương_cuối+1 (không có trần) và phái
			// viết tiếp, Người viết bịa ra chương không có trong kế hoạch, commit nhận — sách phẳng cứ thế
			// phình mãi mà không ai báo. Chặn ở đây đưa chế độ phẳng về ngang chế độ phân lớp.
			//
			// Đề cương RỖNG cũng bị chặn, không còn nhường cho FoundationMissing: cổng đó chỉ được hỏi ở
			// giai đoạn lập kế hoạch, nên một khi phase đã bị đẩy sang writing (dù sai) thì không ai gác cửa
			// này nữa. Sự cố 2026-07-16 ("Loạn Thế Võ Đạo" lần nhập lại): layered_outline chưa từng lưu được
			// (Layered=false → nhánh phân lớp phía trên không bắt), outline phẳng 0 chương → nhánh này cho
			// qua trong im lặng, và 6 chương/88k từ được commit hoàn toàn không có kế hoạch. Đường ghi hợp lệ
			// duy nhất không có đề cương là import — nhưng luồng import lưu outline TRƯỚC khi commit chương
			// (xem imp/foundation.go), nên không bị khoá nhầm.
			if !slices.ContainsFunc(entries, func(e domain.OutlineEntry) bool { return e.Chapter == a.Chapter }) {
				return nil, fmt.Errorf(
					"chương %d không có mục tương ứng trong đề cương phẳng (đề cương phủ %d chương): hãy để Kiến trúc sư bổ sung đề cương bằng save_foundation type=layered_outline (khi chưa có đề cương) hoặc type=expand_arc/append_volume (khi cần nối tiếp) trước khi viết chương này; nếu sách đã viết đủ thì đây là chương thừa: %w",
					a.Chapter, len(entries), errs.ErrToolPrecondition)
			}
		}
	}

	// 1. Tải nội dung chương
	content, wordCount, err := t.store.Drafts.LoadChapterContent(a.Chapter)
	if err != nil {
		return nil, fmt.Errorf("load chapter content: %w: %w", errs.ErrStoreRead, err)
	}
	if content == "" {
		return nil, fmt.Errorf("no content found for chapter %d: %w", a.Chapter, errs.ErrToolPrecondition)
	}

	now := time.Now().Format(time.RFC3339)
	pending := domain.PendingCommit{
		Chapter:        a.Chapter,
		Stage:          domain.CommitStageStarted,
		Summary:        a.Summary,
		HookType:       a.HookType,
		DominantStrand: a.DominantStrand,
		StartedAt:      now,
		UpdatedAt:      now,
	}
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("save pending commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 2. Lưu bản chính
	if err := t.store.Drafts.SaveFinalChapter(a.Chapter, content); err != nil {
		return nil, fmt.Errorf("save final chapter: %w: %w", errs.ErrStoreWrite, err)
	}

	// 3. Lưu tóm tắt
	summary := domain.ChapterSummary{
		Chapter:    a.Chapter,
		Summary:    a.Summary,
		Characters: a.Characters,
		KeyEvents:  a.KeyEvents,
	}
	if err := t.store.Summaries.SaveSummary(summary); err != nil {
		return nil, fmt.Errorf("save summary: %w: %w", errs.ErrStoreWrite, err)
	}

	// 4. Cập nhật gia số trạng thái
	if len(a.TimelineEvents) > 0 {
		for i := range a.TimelineEvents {
			a.TimelineEvents[i].Chapter = a.Chapter
		}
		if err := t.store.World.AppendTimelineEvents(a.TimelineEvents); err != nil {
			return nil, fmt.Errorf("append timeline: %w: %w", errs.ErrStoreWrite, err)
		}
	}
	if len(a.ForeshadowUpdates) > 0 {
		if err := t.store.World.UpdateForeshadow(a.Chapter, a.ForeshadowUpdates); err != nil {
			return nil, fmt.Errorf("update foreshadow: %w: %w", errs.ErrStoreWrite, err)
		}
	}
	if len(a.RelationshipChanges) > 0 {
		for i := range a.RelationshipChanges {
			a.RelationshipChanges[i].Chapter = a.Chapter
		}
		if err := t.store.World.UpdateRelationships(a.RelationshipChanges); err != nil {
			return nil, fmt.Errorf("update relationships: %w: %w", errs.ErrStoreWrite, err)
		}
	}
	if len(a.StateChanges) > 0 {
		for i := range a.StateChanges {
			a.StateChanges[i].Chapter = a.Chapter
		}
		if err := t.store.World.AppendStateChanges(a.StateChanges); err != nil {
			return nil, fmt.Errorf("append state changes: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// 4b. Tích lũy danh sách nhân vật phụ: nhân vật không phải cốt lõi xuất hiện trong chương này được đưa vào cast_ledger để novel_context truy xuất.
	// Khi thất bại chỉ cảnh báo, không chặn lưu chương — danh sách này là dữ liệu phụ, có thể tự phục hồi qua lần lưu chương tiếp theo.
	if len(a.Characters) > 0 {
		coreNames := loadCoreCharacterNameSet(t.store)
		if err := t.store.Cast.MergeAppearances(a.Chapter, a.Characters, a.CastIntros, coreNames); err != nil {
			slog.Warn("tích lũy danh sách nhân vật phụ thất bại, bỏ qua", "module", "commit", "chapter", a.Chapter, "err", err)
		}
	}

	pending.Stage = domain.CommitStageStateApplied
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("update pending commit stage: %w: %w", errs.ErrStoreWrite, err)
	}

	// 5. Cập nhật tiến độ
	if err := t.store.Progress.MarkChapterComplete(a.Chapter, wordCount, a.HookType, a.DominantStrand); err != nil {
		return nil, fmt.Errorf("mark chapter complete: %w: %w", errs.ErrStoreWrite, err)
	}

	// 6. Kiểm tra xem có cần xét duyệt không
	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	completedCount := 0
	if progress != nil {
		completedCount = len(progress.CompletedChapters)
	}

	// 6b. Tín hiệu cung/cuốn chế độ dài: boundary đã được xác thực ở đầu vào, đảm bảo khác nil khi Layered
	var arcEnd, volumeEnd, needsExpansion, needsNewVolume bool
	var vol, arc, nextVol, nextArc int
	if progress != nil && progress.Layered && boundary != nil {
		arcEnd = boundary.IsArcEnd
		volumeEnd = boundary.IsVolumeEnd
		vol = boundary.Volume
		arc = boundary.Arc
		needsExpansion = boundary.NeedsExpansion
		needsNewVolume = boundary.NeedsNewVolume
		nextVol = boundary.NextVolume
		nextArc = boundary.NextArc
		_ = t.store.Progress.UpdateVolumeArc(vol, arc)
	}

	var reviewRequired bool
	var reviewReason string
	if progress != nil && progress.Layered {
		reviewRequired, reviewReason = domain.ShouldArcReview(arcEnd, volumeEnd, vol, arc)
	} else {
		reviewRequired, reviewReason = domain.ShouldReview(completedCount)
	}

	// 7. Xây dựng tín hiệu có cấu trúc
	result := domain.CommitResult{
		Chapter:        a.Chapter,
		Committed:      true,
		WordCount:      wordCount,
		NextChapter:    a.Chapter + 1,
		ReviewRequired: reviewRequired,
		ReviewReason:   reviewReason,
		HookType:       a.HookType,
		DominantStrand: a.DominantStrand,
		Feedback:       a.Feedback,
		ArcEnd:         arcEnd,
		VolumeEnd:      volumeEnd,
		Volume:         vol,
		Arc:            arc,
		NeedsExpansion: needsExpansion,
		NeedsNewVolume: needsNewVolume,
		NextVolume:     nextVol,
		NextArc:        nextArc,
	}

	// 8. Xác định trạng thái hoàn tất: không phân lớp viết xong chương cuối / phân lớp viết xong chương cuối của cuốn cuối → MarkComplete
	if t.applyCompletion(&result, progress) {
		result.BookComplete = true
	}
	if p, _ := t.store.Progress.Load(); p != nil {
		result.Flow = string(p.Flow)
	}

	pending.Stage = domain.CommitStageProgressMarked
	pending.Result = &result
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("update pending commit result: %w: %w", errs.ErrStoreWrite, err)
	}

	// 9. Xóa trạng thái trung gian của tiến độ
	if err := t.store.Progress.ClearInProgress(); err != nil {
		return nil, fmt.Errorf("clear in-progress: %w: %w", errs.ErrStoreWrite, err)
	}
	if err := t.store.Signals.ClearPendingCommit(); err != nil {
		return nil, fmt.Errorf("clear pending commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 10. Thêm điểm khôi phục
	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(a.Chapter), "commit",
		fmt.Sprintf("chapters/%02d.md", a.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 11. Kiểm tra quy tắc cơ học (chỉ trả về dữ liệu thực tế, không chặn)
	violations := t.checkRules(content, wordCount, a.Chapter)
	return json.Marshal(commitOutput{CommitResult: result, RuleViolations: violations})
}

// checkRules kiểm tra cơ học nội dung chương: Lint giới hạn tối thiểu tích hợp sẵn (kiểm tra cơ chế tồn dư, luôn thực thi)
// + Check quy tắc người dùng (khi rulesOpts rỗng hoàn toàn, loader trả về layers rỗng, checker trả về nil).
//
// Trần số từ được đo từ chính các chương đã hoàn thành chứ không lấy từ rules — chương đang commit
// bị loại khỏi mẫu để nó không tự nâng trần cho chính mình.
func (t *CommitChapterTool) checkRules(text string, wordCount, chapter int) []rules.Violation {
	violations := rules.Lint(text)
	bundle := rules.Merge(rules.Load(t.rulesOpts))
	ceiling := rules.ComputeWordCeiling(t.recentWordCounts(chapter))
	return append(violations, rules.Check(text, wordCount, bundle.Structured, ceiling)...)
}

// recentWordCounts trả về số từ các chương đã hoàn thành theo thứ tự chương tăng dần,
// bỏ qua chương đang được lưu (khi đánh bóng, chương đó đã có sẵn số từ của bản cũ trong Progress).
func (t *CommitChapterTool) recentWordCounts(exclude int) []int {
	progress, err := t.store.Progress.Load()
	if err != nil || progress == nil {
		return nil
	}
	chapters := make([]int, 0, len(progress.ChapterWordCounts))
	for ch := range progress.ChapterWordCounts {
		if ch != exclude {
			chapters = append(chapters, ch)
		}
	}
	slices.Sort(chapters)
	counts := make([]int, 0, len(chapters))
	for _, ch := range chapters {
		counts = append(counts, progress.ChapterWordCounts[ch])
	}
	return counts
}

// executeRewriteCommit xử lý lưu chương khi đánh bóng/viết lại: ghi đè bản chính và tóm tắt, cập nhật số từ, drain hàng đợi.
// Bỏ qua toàn bộ thao tác bổ sung trạng thái thế giới (timeline / foreshadow / relationship / state_changes) và kiểm tra biên giới cung,
// vì những thứ này đã được áp dụng trong lần lưu gốc của chương.
func (t *CommitChapterTool) executeRewriteCommit(
	chapter int,
	summary string,
	characters, keyEvents []string,
	hookType, dominantStrand string,
	progress *domain.Progress,
) (json.RawMessage, error) {
	// 1. Tải nội dung sau khi đánh bóng
	content, wordCount, err := t.store.Drafts.LoadChapterContent(chapter)
	if err != nil {
		return nil, fmt.Errorf("rewrite: load chapter content: %w: %w", errs.ErrStoreRead, err)
	}
	if content == "" {
		return nil, fmt.Errorf("no content found for chapter %d: %w", chapter, errs.ErrToolPrecondition)
	}

	// 2. Kiểm tra cứng: drafts hoàn toàn giống bản chính hiện tại → chưa thực sự đánh bóng/viết lại (writer bỏ qua draft_chapter)
	// Từ chối lưu, buộc writer gọi draft_chapter(mode=write) trước để ghi phiên bản mới.
	existingFinal, _ := t.store.Drafts.LoadChapterText(chapter)
	if existingFinal != "" && existingFinal == content {
		mode := "viết lại"
		if progress != nil && progress.Flow == domain.FlowPolishing {
			mode = "đánh bóng"
		}
		return nil, fmt.Errorf("nội dung drafts và chapters của chương %d hoàn toàn giống nhau, không phát hiện thay đổi %s. Vui lòng gọi draft_chapter(mode=write, chapter=%d) để ghi nội dung mới sau khi %s, rồi mới commit_chapter: %w",
			chapter, mode, chapter, mode, errs.ErrToolPrecondition)
	}

	// 3. Ghi đè bản chính
	if err := t.store.Drafts.SaveFinalChapter(chapter, content); err != nil {
		return nil, fmt.Errorf("rewrite: save final chapter: %w: %w", errs.ErrStoreWrite, err)
	}

	// 3. Ghi đè tóm tắt
	if err := t.store.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter:    chapter,
		Summary:    summary,
		Characters: characters,
		KeyEvents:  keyEvents,
	}); err != nil {
		return nil, fmt.Errorf("rewrite: save summary: %w: %w", errs.ErrStoreWrite, err)
	}

	// 4. Cập nhật số từ (MarkChapterComplete với chương đã hoàn thành là idempotent: thay thế số từ, slice.Contains ngăn thêm trùng vào hàng đợi)
	if err := t.store.Progress.MarkChapterComplete(chapter, wordCount, hookType, dominantStrand); err != nil {
		return nil, fmt.Errorf("rewrite: update word count: %w: %w", errs.ErrStoreWrite, err)
	}

	// 5. Drain hàng đợi chờ xử lý; khi hàng đợi rỗng CompleteRewrite sẽ tự chuyển flow về writing
	if err := t.store.Progress.CompleteRewrite(chapter); err != nil {
		return nil, fmt.Errorf("rewrite: complete rewrite: %w: %w", errs.ErrStoreWrite, err)
	}

	// 6. Điểm khôi phục
	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(chapter), "commit",
		fmt.Sprintf("chapters/%02d.md", chapter),
	); err != nil {
		return nil, fmt.Errorf("rewrite: checkpoint commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 7. Đọc snapshot Progress sau khi drain, trả về dữ liệu thực tế
	mode := "rewrite"
	if progress.Flow == domain.FlowPolishing {
		mode = "polish"
	}
	latest, _ := t.store.Progress.Load()
	remaining := []int{}
	nextChapter := chapter + 1
	flow := string(domain.FlowWriting)
	if latest != nil {
		remaining = append(remaining, latest.PendingRewrites...)
		nextChapter = latest.NextChapter()
		flow = string(latest.Flow)
	}
	drained := len(remaining) == 0

	// Sau khi hàng đợi rỗng mới kiểm tra hoàn tất: lưu sau khi viết lại không đi qua applyCompletion của đường chính,
	// hoàn tất chỉ có thể kích hoạt tại đây.
	//   - Phân lớp + viết tiến về phía trước: dùng layeredBookComplete cấp chất lượng (yêu cầu các tuyến dài thu lại), chưa thỏa thì nhường kiến trúc sư.
	//   - Phân lớp + viết lại reopen (ReopenedFromComplete): viết lại chỉ sửa chương đã có, không thêm/bớt cấu trúc,
	//     khi cấu trúc còn nguyên vẹn thì hoàn tất lại — nếu viết lại làm xáo trộn một tuyến truyện thì kẹt ở writing,
	//     cuối cuốn cuối sẽ rơi vào vòng lặp vô hạn do viết vượt giới hạn.
	//   - Không phân lớp: viết đủ TotalChapters là hoàn tất (viết lại không thêm/bớt chương, ban đầu đã đủ).
	bookComplete := false
	if drained && latest != nil {
		reComplete := false
		// Sàn 300 chỉ áp cho phân lớp, giống applyCompletion — xem lý do ở đó.
		hasFloor := len(latest.CompletedChapters) >= minChaptersForAutoComplete
		switch {
		case latest.Layered && latest.ReopenedFromComplete:
			reComplete = hasFloor && t.layeredStructurallyComplete(latest)
		case latest.Layered:
			reComplete = hasFloor && t.layeredBookComplete(latest)
		default:
			reComplete = latest.TotalChapters > 0 && len(latest.CompletedChapters) >= latest.TotalChapters
		}
		if reComplete {
			if cerr := t.store.Progress.MarkComplete(); cerr == nil {
				bookComplete = true
				if p, _ := t.store.Progress.Load(); p != nil {
					flow = string(p.Flow)
				}
			}
		}
	}

	// Giống đường chính: rewrite/polish cũng kiểm tra cơ học và kèm rule_violations
	violations := t.checkRules(content, wordCount, chapter)
	return json.Marshal(map[string]any{
		"chapter":         chapter,
		"rewritten":       true,
		"mode":            mode,
		"word_count":      wordCount,
		"remaining_queue": remaining,
		"queue_drained":   drained,
		"next_chapter":    nextChapter,
		"flow":            flow,
		"book_complete":   bookComplete,
		"rule_violations": violations,
	})
}

// buildSkipResult tạo kết quả trả về cho "lưu trùng lặp chương đã hoàn thành", căn chỉnh với commit bình thường.
// Điều phối viên dựa vào đây để quyết định phân phát tiếp (writer/editor/architect), tránh ảo giác do nhận prose.
func (t *CommitChapterTool) buildSkipResult(chapter int, progress *domain.Progress) (json.RawMessage, error) {
	_, wordCount, _ := t.store.Drafts.LoadChapterContent(chapter)

	result := domain.CommitResult{
		Chapter:     chapter,
		Committed:   true,
		WordCount:   wordCount,
		NextChapter: chapter + 1,
	}

	if progress != nil && progress.Layered {
		// OutOfRange: boundary mô tả cung cần chữa, không phải vị trí của chương này — không được
		// đổ vào CommitResult, nếu không Điều phối viên nhận tín hiệu cung/tập sai lệch.
		if boundary, _ := t.store.Outline.CheckArcBoundary(chapter); boundary != nil && !boundary.OutOfRange {
			result.ArcEnd = boundary.IsArcEnd
			result.VolumeEnd = boundary.IsVolumeEnd
			result.Volume = boundary.Volume
			result.Arc = boundary.Arc
			result.NeedsExpansion = boundary.NeedsExpansion
			result.NeedsNewVolume = boundary.NeedsNewVolume
			result.NextVolume = boundary.NextVolume
			result.NextArc = boundary.NextArc
		}
		result.ReviewRequired, result.ReviewReason = domain.ShouldArcReview(result.ArcEnd, result.VolumeEnd, result.Volume, result.Arc)
	} else if progress != nil {
		result.ReviewRequired, result.ReviewReason = domain.ShouldReview(len(progress.CompletedChapters))
	}

	if progress != nil {
		if progress.Phase == domain.PhaseComplete {
			result.BookComplete = true
		}
		result.Flow = string(progress.Flow)
	}

	return json.Marshal(result)
}

// loadCoreCharacterNameSet tải tập hợp tên nhân vật đã có trong characters.json (bao gồm bí danh).
// Dùng làm tập lọc "nhân vật cốt lõi đã biết" cho cast_ledger — nhân vật cốt lõi không vào danh sách phụ.
// Khi tải thất bại trả về nil (khi merge tất cả characters đều vào ledger, chấp nhận được).
func loadCoreCharacterNameSet(s *store.Store) map[string]bool {
	chars, err := s.Characters.Load()
	if err != nil || len(chars) == 0 {
		return nil
	}
	set := make(map[string]bool, len(chars)*2)
	for _, c := range chars {
		if c.Name != "" {
			set[c.Name] = true
		}
		for _, alias := range c.Aliases {
			if alias != "" {
				set[alias] = true
			}
		}
	}
	return set
}

// applyCompletion kiểm tra xem lần lưu này có khiến toàn bộ sách hoàn tất không; nếu có thì MarkComplete và trả về true.
//   - Không phân lớp: viết đủ tổng số chương đã hẹn là hoàn tất.
//   - Phân lớp: kiến trúc sư gọi save_foundation type=complete_book là đường chính; đây thêm một lớp đảm bảo
//     tự động — khi sách đã khách quan thỏa điều kiện hoàn tất (xem layeredBookComplete) thì tự kết thúc.
//     Ngăn model không gọi append_volume cũng không gọi complete_book ở điểm cuối, dẫn đến "người viết viết vượt giới →
//     StopGuard chặn → thử lại vô hạn" (livelock, nguyên nhân gốc của trường hợp ch204..347).
//
// Ràng buộc cứng: truyện tối thiểu 300 chương mới được tự động hoàn tất.
const minChaptersForAutoComplete = 300

func (t *CommitChapterTool) applyCompletion(result *domain.CommitResult, progress *domain.Progress) bool {
	if progress == nil {
		return false
	}
	if progress.Layered {
		// Chặn cứng: truyện dài phân lớp chưa đủ 300 chương thì không tự động hoàn tất.
		if len(progress.CompletedChapters) < minChaptersForAutoComplete {
			return false
		}
		if t.layeredBookComplete(progress) {
			_ = t.store.Progress.MarkComplete()
			return true
		}
		return false
	}
	// Không phân lớp: đề cương phẳng ĐÃ là hợp đồng về độ dài, viết đủ là xong — sàn 300 không áp ở đây.
	// Áp sàn cho chế độ phẳng sẽ khoá chết một cuốn short/mid: nó không bao giờ hoàn tất được, mà chương
	// vượt đề cương thì commit từ chối, nên Router phái viết mãi một chương không thể lưu.
	if progress.TotalChapters > 0 && result.NextChapter > progress.TotalChapters {
		_ = t.store.Progress.MarkComplete()
		return true
	}
	return false
}

// layeredStructurallyComplete kiểm định phân lớp dài có "hoàn tất về mặt cấu trúc" không:
// hàng đợi viết lại rỗng + không còn cung xương sống chờ mở rộng + tất cả chương đã mở rộng đều đã viết.
// Đây là dữ liệu thực tế xác định về trạng thái kết thúc, không bao gồm đánh giá ngữ nghĩa như phục bút/tuyến dài —
// dùng làm lưới an toàn "chống vòng lặp vô hạn trạng thái kết thúc" (tái hoàn tất sau khi hàng đợi viết lại rỗng).
func (t *CommitChapterTool) layeredStructurallyComplete(progress *domain.Progress) bool {
	// 1. Hàng đợi viết lại phải rỗng
	if len(progress.PendingRewrites) > 0 {
		return false
	}
	volumes, err := t.store.Outline.LoadLayeredOutline()
	if err != nil || len(volumes) == 0 {
		return false
	}
	// 2. Không còn cung xương sống nào chờ mở rộng (vẫn còn nội dung cần viết theo kế hoạch)
	for i := range volumes {
		for j := range volumes[i].Arcs {
			if !volumes[i].Arcs[j].IsExpanded() {
				return false
			}
		}
	}
	// 3. Tất cả chương đã mở rộng phải được viết hoàn chỉnh
	expanded := len(domain.FlattenOutline(volumes))
	return expanded > 0 && len(progress.CompletedChapters) >= expanded
}

// layeredBookComplete dùng dữ liệu khách quan để phán định phân lớp dài có thực sự hoàn tất không,
// đối chiếu với các hạng mục định lượng trong danh sách kiểm tra hoàn tất của architect-long.md + dữ liệu cấu trúc.
// Ngoài cấu trúc hoàn chỉnh còn yêu cầu phục bút归零, tuyến dài thu lại — bất kỳ điều kiện nào chưa thỏa
// đều nhường lại kiến trúc sư tiếp tục expand_arc / append_volume, tuyệt đối không kết thúc khi câu chuyện chưa xong.
// Khi không có compass thì đánh giá thận trọng là chưa hoàn tất. Đây là phán định "cấp chất lượng" cho viết tiến về phía trước,
// nghiêm ngặt hơn layeredStructurallyComplete.
func (t *CommitChapterTool) layeredBookComplete(progress *domain.Progress) bool {
	if !t.layeredStructurallyComplete(progress) {
		return false
	}
	// 4. Phục bút hoạt động phải归零 (tất cả đã thực hiện lời hứa)
	if active, aerr := t.store.World.LoadActiveForeshadow(); aerr != nil || len(active) > 0 {
		return false
	}
	// 5. Các tuyến dài hoạt động trong compass phải được thu lại (không có compass / tuyến dài chưa xong đều trả lại kiến trúc sư phán quyết)
	compass, cerr := t.store.Outline.LoadCompass()
	if cerr != nil || compass == nil || len(compass.OpenThreads) > 0 {
		return false
	}
	return true
}
