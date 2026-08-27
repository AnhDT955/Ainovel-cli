package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// DraftChapterTool viết toàn bộ bản nháp một chương, thay thế pipeline cũ write_scene + polish_chapter.
// Agent tự quyết định viết một lần hay chia nhỏ để tiếp tục.
type DraftChapterTool struct {
	store *store.Store
}

func NewDraftChapterTool(store *store.Store) *DraftChapterTool {
	return &DraftChapterTool{store: store}
}

func (t *DraftChapterTool) Name() string { return "draft_chapter" }
func (t *DraftChapterTool) Description() string {
	return "Viết nội dung chính của chương. mode=write ghi đè toàn bộ chương, mode=append nối thêm vào bản nháp hiện có (tiếp tục/chỉnh sửa)"
}
func (t *DraftChapterTool) Label() string { return "Viết chương" }

// Công cụ ghi, cấm chạy đồng thời (race condition đọc-sửa-ghi).
func (t *DraftChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *DraftChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *DraftChapterTool) Schema() map[string]any {
	// mode được đánh dấu required để tương thích với OpenAI strict tool calling —
	// chế độ strict yêu cầu tất cả properties đều có mặt trong danh sách required.
	// Hành vi cũ "bỏ qua mode thì mặc định là write" nay yêu cầu model truyền
	// tường minh mode="write"; nhánh default trong Execute không đổi.
	return schema.Object(
		schema.Property("chapter", schema.Int("Số chương")).Required(),
		schema.Property("content", schema.String("Nội dung chính của chương")).Required(),
		schema.Property("mode", schema.Enum("Chế độ viết", "write", "append")).Required(),
	)
}

// StrictSchema bật strict tool calling của OpenAI, buộc model tuân thủ schema nghiêm ngặt:
// tất cả trường required phải được điền, arguments không thể "EOT sớm" ra object rỗng.
// litellm chuyển tiếp trường strict; các backend hỗ trợ như OpenAI / xAI sẽ thực thi,
// các backend khác bỏ qua trường không nhận biết theo thông lệ HTTP/JSON.
// Anthropic/Gemini/Bedrock đi qua chuỗi chuyển đổi riêng nên không thấy trường này.
func (t *DraftChapterTool) StrictSchema() bool { return true }

func (t *DraftChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int    `json:"chapter"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if a.Content == "" {
		return nil, fmt.Errorf("content must not be empty: %w", errs.ErrToolArgs)
	}
	if err := t.store.Progress.ValidateChapterWork(a.Chapter); err != nil {
		return nil, err
	}
	if t.store.Progress.IsChapterCompleted(a.Chapter) {
		// Luồng chỉnh sửa/viết lại: chương đã hoàn thành nhưng vẫn còn trong pending_rewrites, cho phép ghi đè bản nháp
		progress, _ := t.store.Progress.Load()
		inRewriteQueue := progress != nil && slices.Contains(progress.PendingRewrites, a.Chapter)
		if !inRewriteQueue {
			return json.Marshal(map[string]any{
				"chapter":   a.Chapter,
				"skipped":   true,
				"completed": true,
				"reason":    fmt.Sprintf("Chương %d đã được lưu hoàn thành, không thể ghi đè", a.Chapter),
			})
		}
	}
	// Cùng guard đề cương với plan_chapter: draft_chapter là đường viết thứ hai, thiếu guard ở đây thì
	// Writer chỉ cần bỏ qua lỗi của plan_chapter là viết được chương không có trong kế hoạch (xem ghi
	// chú tại requireOutlineEntry — sự cố 2026-07-16 đã đi đúng đường vòng đó).
	if err := requireOutlineEntry(t.store, a.Chapter); err != nil {
		return nil, err
	}
	if err := t.store.Progress.StartChapter(a.Chapter); err != nil {
		return nil, fmt.Errorf("mark chapter in progress: %w", err)
	}

	switch a.Mode {
	case "append":
		if err := t.store.Drafts.AppendDraft(a.Chapter, a.Content); err != nil {
			return nil, fmt.Errorf("append draft: %w", err)
		}
		full, err := t.store.Drafts.LoadDraft(a.Chapter)
		if err != nil {
			return nil, fmt.Errorf("load draft after append: %w", err)
		}
		if _, err := t.store.Checkpoints.AppendArtifact(
			domain.ChapterScope(a.Chapter), "draft",
			fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		); err != nil {
			return nil, fmt.Errorf("checkpoint draft: %w", err)
		}
		return json.Marshal(t.result(a.Chapter, "append", utf8.RuneCountInString(full)))
	default: // write
		if err := t.store.Drafts.SaveDraft(a.Chapter, a.Content); err != nil {
			return nil, fmt.Errorf("save draft: %w", err)
		}
		if _, err := t.store.Checkpoints.AppendArtifact(
			domain.ChapterScope(a.Chapter), "draft",
			fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		); err != nil {
			return nil, fmt.Errorf("checkpoint draft: %w", err)
		}
		return json.Marshal(t.result(a.Chapter, "write", utf8.RuneCountInString(a.Content)))
	}
}

// result dựng kết quả trả về cho writer, kèm dải độ dài tự nhiên của tác phẩm.
//
// natural_band là lý do draft_chapter tồn tại như nơi quyết định độ dài chương: writer nhìn thấy
// độ dài thực tế của các chương trước NGAY LÚC VIẾT, nên tự căn được từ đầu. Trước đây thông tin
// này chỉ xuất hiện lúc commit_chapter dưới dạng vi phạm chapter_words — quá muộn, chương đã viết
// xong rồi, và cách duy nhất còn lại là cắt ngắn, tức phá ngữ nghĩa vốn có của chương.
// Đây là DỮ LIỆU, không phải mệnh lệnh: vượt band không chặn commit, không tự khởi động viết lại.
func (t *DraftChapterTool) result(chapter int, mode string, wordCount int) map[string]any {
	out := map[string]any{
		"written":    true,
		"chapter":    chapter,
		"mode":       mode,
		"word_count": wordCount,
		"next_step":  "Trước tiên read_chapter(source=draft) để đọc lại bản nháp, rồi gọi check_consistency, cuối cùng commit_chapter",
	}
	if ceiling := t.naturalBand(chapter); ceiling.Active() {
		out["natural_band"] = map[string]any{
			"median":   ceiling.Median,
			"soft_max": ceiling.SoftMax,
			"samples":  ceiling.Samples,
			"note":     "Độ dài tự nhiên đo từ các chương đã viết, không phải hạn mức. Vượt soft_max chỉ là tín hiệu chương này dài bất thường so với chính tác phẩm — cân nhắc tách cảnh sang chương sau ngay từ bây giờ, TUYỆT ĐỐI không cắt bỏ mạch truyện đã viết để ép cho vừa.",
		}
	}
	return out
}

// naturalBand đo dải độ dài từ các chương đã hoàn thành, bỏ qua chương đang viết.
func (t *DraftChapterTool) naturalBand(exclude int) rules.WordCeiling {
	progress, err := t.store.Progress.Load()
	if err != nil || progress == nil {
		return rules.WordCeiling{}
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
	return rules.ComputeWordCeiling(counts)
}
