package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveDirectiveTool lưu bền vững các yêu cầu sáng tác lâu dài của người dùng (chỉ Điều phối viên nắm giữ).
// Ghi xuống meta/user_directives.json, novel_context tiêm vào working_memory.user_directives,
// tất cả agent phụ tự động thấy mỗi chương — không phụ thuộc Điều phối viên chuyển tay thủ công khi phân công, có hiệu lực qua nén và khởi động lại.
type SaveDirectiveTool struct {
	store *store.Store
}

func NewSaveDirectiveTool(s *store.Store) *SaveDirectiveTool {
	return &SaveDirectiveTool{store: s}
}

func (t *SaveDirectiveTool) Name() string  { return "save_directive" }
func (t *SaveDirectiveTool) Label() string { return "Lưu chỉ thị lâu dài" }

func (t *SaveDirectiveTool) Description() string {
	return "Lưu bền vững các yêu cầu sáng tác lâu dài của người dùng (ví dụ \"sau này tăng tỉ lệ đối thoại\" \"tiêu đề chương chỉ dùng tiếng Việt\"). " +
		"Sau khi lưu, tất cả agent phụ đều thấy trong working_memory.user_directives mỗi chương, không cần chuyển tay nữa. " +
		"action=add thêm một mục (text bắt buộc, giữ nguyên ý định người dùng, có thể cô đọng lại); " +
		"action=remove xóa theo số thứ tự (index bắt buộc, số thứ tự xem trong danh sách trả về lần trước). " +
		"Trả về danh sách đầy đủ sau khi cập nhật. Chỉ lưu yêu cầu dạng trạng thái (mô tả đúng mọi lúc khi đọc lại); " +
		"chỉ thị dạng tương đối/hành động (ví dụ \"thêm 10 chương\") không được lưu — công cụ này không phân công agent phụ, lưu cũng như không có ai thực thi, hãy dùng route agent phụ để xử lý ngay."
}

// Công cụ ghi, cấm chạy đồng thời.
func (t *SaveDirectiveTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveDirectiveTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveDirectiveTool) ActivityDescription(_ json.RawMessage) string { return "Lưu chỉ thị lâu dài" }

func (t *SaveDirectiveTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("action", schema.Enum("Loại thao tác", "add", "remove")).Required(),
		schema.Property("text", schema.String("Nội dung yêu cầu (bắt buộc khi add): một câu nêu rõ yêu cầu, giữ nguyên ý người dùng")),
		schema.Property("index", schema.Int("Số thứ tự mục cần xóa (bắt buộc khi remove, 1-based, xem index trong danh sách trả về)")),
	)
}

func (t *SaveDirectiveTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Action string `json:"action"`
		Text   string `json:"text"`
		Index  int    `json:"index"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}

	var (
		list []domain.UserDirective
		err  error
	)
	switch a.Action {
	case "add":
		text := strings.TrimSpace(a.Text)
		if text == "" {
			return nil, fmt.Errorf("add yêu cầu text không rỗng: %w", errs.ErrToolArgs)
		}
		chapter, total := 0, 0
		if progress, perr := t.store.Progress.Load(); perr == nil && progress != nil {
			chapter = progress.NextChapter()
			total = progress.TotalChapters
		}
		list, err = t.store.Directives.Add(domain.UserDirective{
			Text:          text,
			Chapter:       chapter,
			TotalChapters: total,
			CreatedAt:     time.Now().Format(time.RFC3339),
		})
	case "remove":
		if a.Index < 1 {
			return nil, fmt.Errorf("remove yêu cầu index >= 1: %w", errs.ErrToolArgs)
		}
		list, err = t.store.Directives.Remove(a.Index)
	default:
		return nil, fmt.Errorf("unknown action %q: %w", a.Action, errs.ErrToolArgs)
	}
	if err != nil {
		return nil, err
	}

	items := directiveFacts(list)
	return json.Marshal(map[string]any{
		"saved":      true,
		"directives": items,
		"count":      len(items),
	})
}

// directiveFacts chuyển chỉ thị lâu dài thành dạng view sự kiện cho LLM (kết quả công cụ và tiêm envelope cùng dạng):
// at_* là snapshot tiến độ tại thời điểm ban hành — chỉ thị có hiệu lực từ at_chapter trở đi,
// biểu thức tương đối có thể dựa vào at_total_chapters để xét xem đã thỏa mãn chưa. created_at là thông tin kiểm toán, không đưa vào LLM.
func directiveFacts(list []domain.UserDirective) []map[string]any {
	items := make([]map[string]any, len(list))
	for i, d := range list {
		items[i] = map[string]any{
			"index":             i + 1,
			"text":              d.Text,
			"at_chapter":        d.Chapter,
			"at_total_chapters": d.TotalChapters,
		}
	}
	return items
}
