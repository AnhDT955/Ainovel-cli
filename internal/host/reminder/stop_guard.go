package reminder

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// StopGuard là tuyến phòng thủ cuối cùng ngăn LLM dừng sớm.
// Khi LLM cố gắng end_turn:
//   - Progress.Phase = Complete → cho phép dừng
//   - Ngược lại, inject user message để agent tiếp tục turn tiếp theo
//   - Bị chặn liên tiếp quá maxConsecutive lần → Escalate để kết thúc run (nghĩa là prompt/reminder bị lỗi nghiêm trọng)
//
// Guard duy trì bộ đếm consecutive block nội bộ; reset về 0 khi được phép dừng hoặc sau khi inject thành công.
// Thứ thực sự điều khiển hành vi Điều phối viên là Reminder + Prompt, StopGuard chỉ là lưới chặn cuối.
const maxConsecutiveBlocks = 5

// NewStopGuard khởi tạo StopGuard dành riêng cho Điều phối viên.
// onBlock tùy chọn, nếu khác nil sẽ được gọi mỗi lần chặn, dùng để kiểm tra.
func NewStopGuard(st *store.Store, onBlock func(reason string, consecutive int32)) agentcore.StopGuard {
	var consecutive atomic.Int32
	var lastBlockTurn atomic.Int64 // TurnIndex của lần block gần nhất; -1 nghĩa là chưa block lần nào
	lastBlockTurn.Store(-1)
	return func(_ context.Context, info agentcore.StopInfo) agentcore.StopDecision {
		progress, _ := st.Progress.Load()
		if progress != nil && progress.Phase == domain.PhaseComplete {
			consecutive.Store(0)
			lastBlockTurn.Store(-1)
			return agentcore.StopDecision{Allow: true}
		}
		// Chỉ tích lũy đếm khi "các turn liền kề liên tiếp bị chặn"; ngược lại coi là vòng mới
		// (LLM đã thực hiện tool call và có tiến triển, hoặc user inject / resume làm TurnIndex giảm), reset đếm.
		last := lastBlockTurn.Load()
		if last < 0 || int64(info.TurnIndex) != last+1 {
			consecutive.Store(0)
		}
		lastBlockTurn.Store(int64(info.TurnIndex))
		n := consecutive.Add(1)
		if n > maxConsecutiveBlocks {
			slog.Error("stop_guard chặn liên tiếp vượt giới hạn, nâng cấp thành kết thúc",
				"module", "host.reminder", "turn", info.TurnIndex, "consecutive", n)
			if onBlock != nil {
				onBlock("escalated", n)
			}
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		inject := "Không được kết thúc hội thoại. Phase chưa đạt Complete, hãy tiếp tục bước tiếp theo (gọi novel_context hoặc gọi agent phụ)."
		if progress != nil && len(progress.PendingRewrites) > 0 {
			inject = fmt.Sprintf("Không được kết thúc hội thoại. Hàng đợi viết lại chưa xử lý xong: %v, hãy gọi writer xử lý ngay.", progress.PendingRewrites)
		}
		slog.Warn("stop_guard chặn end_turn",
			"module", "host.reminder", "turn", info.TurnIndex, "consecutive", n)
		if onBlock != nil {
			onBlock("blocked", n)
		}
		return agentcore.StopDecision{Allow: false, InjectMessage: inject}
	}
}
