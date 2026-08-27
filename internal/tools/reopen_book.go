package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ReopenBookTool mở lại cuốn sách đã hoàn thành để vào trạng thái làm lại (chỉ Điều phối viên nắm giữ).
// Sau khi hoàn tất, completePhaseGate chặn cứng mọi lệnh phái SubAgent, người dùng không thể làm lại các chương đã viết.
// Công cụ này không phải SubAgent, có thể gọi trong giai đoạn complete: nó nguyên tử chuyển phase về writing, đưa chương mục tiêu vào
// PendingRewrites, flow=rewriting, sau đó Flow Router theo hàng đợi làm lại hiện có phái Người viết lần lượt viết lại từng chương,
// hàng đợi chạy xong thì commit_chapter tự động hoàn tất lại. Gate / Router / edit / commit không cần thay đổi logic.
type ReopenBookTool struct {
	store *store.Store
}

func NewReopenBookTool(s *store.Store) *ReopenBookTool {
	return &ReopenBookTool{store: s}
}

func (t *ReopenBookTool) Name() string  { return "reopen_book" }
func (t *ReopenBookTool) Label() string { return "Mở lại để làm lại" }

func (t *ReopenBookTool) Description() string {
	return "Mở lại toàn bộ cuốn sách đã hoàn thành (phase=complete) để vào trạng thái làm lại, dùng khi người dùng yêu cầu viết lại/trau chuốt một số chương sau khi hoàn tất." +
		"chapters là danh sách số chương đã hoàn thành cần làm lại; sau khi gọi, các chương này vào hàng đợi viết lại, Host sẽ lần lượt phái Người viết viết lại từng chương, hoàn tất hết tự động hoàn tất lại." +
		"Chỉ dùng khi toàn bộ sách đã hoàn thành và người dùng rõ ràng yêu cầu sửa các chương đã viết; người dùng muốn thêm tình tiết/mở rộng dung lượng không thuộc làm lại, không dùng công cụ này."
}

// Công cụ ghi, cấm chạy song song.
func (t *ReopenBookTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *ReopenBookTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *ReopenBookTool) ActivityDescription(_ json.RawMessage) string {
	return "Mở lại toàn bộ sách để làm lại"
}

func (t *ReopenBookTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapters", schema.Array("Danh sách số chương đã hoàn thành cần làm lại (ít nhất một chương)", schema.Int(""))).Required(),
		schema.Property("reason", schema.String("Lý do làm lại (tùy chọn, ví dụ \"dọn dẹp ký tự đặc biệt\")")),
	)
}

func (t *ReopenBookTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapters []int  `json:"chapters"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if len(a.Chapters) == 0 {
		return nil, fmt.Errorf("chapters không được rỗng, cần chỉ rõ các chương cần làm lại: %w", errs.ErrToolArgs)
	}

	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if progress == nil {
		return nil, fmt.Errorf("progress chưa được khởi tạo: %w", errs.ErrToolPrecondition)
	}
	// Chỉ có thể làm lại các chương đã viết; số chương không nằm trong tập đã hoàn thành thuộc viết tiếp/vượt phạm vi, từ chối rõ ràng để hướng người dùng đi điều chỉnh dung lượng.
	var invalid []int
	for _, ch := range a.Chapters {
		if !slices.Contains(progress.CompletedChapters, ch) {
			invalid = append(invalid, ch)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("chương %v chưa viết xong, reopen chỉ có thể làm lại các chương đã hoàn thành (thêm/mở rộng tình tiết vui lòng dùng điều chỉnh dung lượng): %w", invalid, errs.ErrToolPrecondition)
	}

	// Kiểm tra tiền điều kiện phase được xử lý bên trong store.Reopen (chỉ có thể gọi ở giai đoạn complete).
	if err := t.store.Progress.Reopen(a.Chapters, a.Reason); err != nil {
		return nil, fmt.Errorf("reopen: %w: %w", errs.ErrStoreWrite, err)
	}

	// điểm khôi phục: đối xứng với complete_book (GlobalScope + meta/progress.json).
	if _, err := t.store.Checkpoints.AppendArtifact(domain.GlobalScope(), "reopen", "meta/progress.json"); err != nil {
		return nil, fmt.Errorf("checkpoint reopen: %w: %w", errs.ErrStoreWrite, err)
	}

	return json.Marshal(map[string]any{
		"reopened":         true,
		"phase":            string(domain.PhaseWriting),
		"pending_rewrites": a.Chapters,
		"next_step":        "Đã mở lại và đưa các chương mục tiêu vào hàng đợi. Vui lòng chờ lệnh từ Host phái Người viết làm lại từng chương; sau khi hoàn tất tất cả sẽ tự động hoàn tất lại.",
	})
}
