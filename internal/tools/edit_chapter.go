package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/voocel/agentcore/schema"
	agentcoretools "github.com/voocel/agentcore/tools"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// EditChapterTool thực hiện thay thế chuỗi định điểm trên bản nháp chương, phù hợp cho cảnh chỉnh sửa.
// So với draft_chapter viết lại toàn chương, tiết kiệm token hơn 10x.
//
// Hợp đồng ghi đĩa: chỉ sửa drafts/{ch:02d}.draft.md, không được sửa trực tiếp chapters/ (bản hoàn chỉnh do commit_chapter độc quyền).
// Ngữ nghĩa Seed: drafts không tồn tại nhưng chapters có → tự động sao chép chapters vào drafts làm điểm khởi đầu.
// Kiểm tra quyền sở hữu: khi chương đã hoàn thành phải có trong hàng đợi PendingRewrites, nếu không sẽ từ chối.
//
// Công cụ này là lớp bọc mỏng của agentcore.EditTool, logic tìm-thay (đối sánh đa cấp chịu lỗi, xuất diff, giữ nguyên dòng kết/BOM)
// đều tái sử dụng triển khai thượng nguồn.
type EditChapterTool struct {
	store *store.Store
	edit  *agentcoretools.EditTool
}

func NewEditChapterTool(s *store.Store) *EditChapterTool {
	return &EditChapterTool{
		store: s,
		edit:  agentcoretools.NewEdit(s.Dir(), nil),
	}
}

func (t *EditChapterTool) Name() string  { return "edit_chapter" }
func (t *EditChapterTool) Label() string { return "Chỉnh sửa chương" }

// ReadOnly khai báo rõ là công cụ ghi (phối hợp ConcurrencySafeTool ngăn lịch trình đồng thời).
func (t *EditChapterTool) ReadOnly(_ json.RawMessage) bool { return false }

// ConcurrencySafe cấm rõ ràng xử lý đồng thời: nhiều lần edit_chapter song song trên cùng chương sẽ gây race condition đọc-sửa-ghi,
// ngay cả khi song song trên các chương khác nhau cũng sẽ làm xáo trộn thứ tự điểm khôi phục. Chạy tuần tự là ổn định nhất.
func (t *EditChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

// ActivityDescription cung cấp mô tả hoạt động hiện tại của công cụ cho UI/log.
func (t *EditChapterTool) ActivityDescription(_ json.RawMessage) string {
	return "Chỉnh sửa bản nháp chương"
}

func (t *EditChapterTool) Description() string {
	return "Thay thế chuỗi định điểm trên bản nháp chương (ưu tiên cho cảnh chỉnh sửa, tiết kiệm token hơn draft_chapter viết lại toàn chương). " +
		"Tìm old_string và thay bằng new_string, yêu cầu khớp chính xác và duy nhất (nhiều chỗ khớp cần replace_all=true). " +
		"Ghi vào drafts/{ch}.draft.md; tự động tạo seed từ chapters khi drafts không tồn tại. " +
		"Từ chối thực thi khi chương đã hoàn thành và không có trong hàng đợi PendingRewrites. Mỗi lần gọi chỉ sửa một chỗ, nhiều chỗ cần gọi nhiều lần."
}

func (t *EditChapterTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int("Số thứ tự chương")).Required(),
		schema.Property("old_string", schema.String("Đoạn văn gốc chính xác cần thay thế, nhiều dòng cần bao gồm ký tự xuống dòng; không dùng replace_all thì phải xuất hiện duy nhất trong bản nháp")).Required(),
		schema.Property("new_string", schema.String("Văn bản mới sau khi thay thế")).Required(),
		schema.Property("replace_all", schema.Bool("Thay thế tất cả các kết quả khớp (mặc định false)")),
	)
}

func (t *EditChapterTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter    int    `json:"chapter"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if a.OldString == "" {
		return nil, fmt.Errorf("old_string không được để trống: %w", errs.ErrToolArgs)
	}
	if a.OldString == a.NewString {
		return nil, fmt.Errorf("old_string và new_string giống nhau, không cần sửa: %w", errs.ErrToolArgs)
	}

	// Kiểm tra quyền sở hữu: chương đã hoàn thành phải có trong hàng đợi tái viết, tránh làm bẩn bản hoàn chỉnh
	if t.store.Progress.IsChapterCompleted(a.Chapter) {
		progress, _ := t.store.Progress.Load()
		if progress == nil || !slices.Contains(progress.PendingRewrites, a.Chapter) {
			return nil, fmt.Errorf("chương %d đã hoàn thành và không có trong hàng đợi PendingRewrites, không thể chỉnh sửa; muốn sửa hãy để editor đánh giá kích hoạt tái viết/chỉnh sửa trước: %w", a.Chapter, errs.ErrToolPrecondition)
		}
	}

	// Seed: sao chép từ chapters vào drafts khi drafts không tồn tại làm điểm khởi đầu
	if err := t.ensureDraft(a.Chapter); err != nil {
		return nil, err
	}

	// Ủy quyền cho agentcore.EditTool thực hiện tìm-thay
	subArgs, _ := json.Marshal(map[string]any{
		"path":        fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		"file_path":   fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		"old_text":    a.OldString,
		"old_string":  a.OldString,
		"new_text":    a.NewString,
		"new_string":  a.NewString,
		"replace_all": a.ReplaceAll,
	})
	result, err := t.edit.Execute(ctx, subArgs)
	if err != nil {
		return nil, fmt.Errorf("apply edit: %w: %w", errs.ErrToolPrecondition, err)
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(a.Chapter), "edit",
		fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint edit: %w: %w", errs.ErrStoreWrite, err)
	}

	// Hướng dẫn bổ sung: để writer biết các bước tiếp theo, tránh bỏ sót check_consistency / commit_chapter
	var passthrough map[string]any
	if err := json.Unmarshal(result, &passthrough); err != nil {
		return result, nil
	}
	passthrough["chapter"] = a.Chapter
	passthrough["next_step"] = "edit đã ghi đĩa. Nếu còn lỗi nghiêm trọng có thể gọi lại edit_chapter; nếu không hãy check_consistency rồi commit_chapter"
	return json.Marshal(passthrough)
}

// ensureDraft đảm bảo drafts/{ch}.draft.md tồn tại:
//   - Đã có bản nháp → trả về ngay
//   - Không có bản nháp nhưng có bản hoàn chỉnh → sao chép bản hoàn chỉnh vào drafts làm điểm khởi đầu chỉnh sửa (phổ biến trong cảnh chỉnh sửa)
//   - Cả hai đều không có → báo lỗi, nhắc dùng draft_chapter tạo bản nháp trước
func (t *EditChapterTool) ensureDraft(chapter int) error {
	draft, err := t.store.Drafts.LoadDraft(chapter)
	if err != nil {
		return fmt.Errorf("load draft: %w: %w", errs.ErrStoreRead, err)
	}
	if draft != "" {
		return nil
	}
	text, err := t.store.Drafts.LoadChapterText(chapter)
	if err != nil {
		return fmt.Errorf("load chapter: %w: %w", errs.ErrStoreRead, err)
	}
	if text == "" {
		return fmt.Errorf("chương %d không có bản nháp cũng không có bản hoàn chỉnh, hãy gọi draft_chapter(mode=write, chapter=%d) tạo bản nháp trước: %w", chapter, chapter, errs.ErrToolPrecondition)
	}
	if err := t.store.Drafts.SaveDraft(chapter, text); err != nil {
		return fmt.Errorf("seed draft from chapter: %w: %w", errs.ErrStoreWrite, err)
	}
	return nil
}
