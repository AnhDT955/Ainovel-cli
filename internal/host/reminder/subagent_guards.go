package reminder

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/store"
)

// subagentMaxConsecutiveBlocks sau N lần chặn liên tiếp sẽ nâng cấp thành terminate, tránh vòng lặp vô tận với mô hình yếu.
const subagentMaxConsecutiveBlocks = 5

// hardStopReasons là các lý do từ chối từ phía provider không thể khôi phục bằng tin nhắn thúc giục. Việc inject
// "phải commit" vào các lý do này là vô hiệu, ngược lại mỗi lần lại phát sinh một lần gọi LLM đầy đủ tiêu thụ token,
// và cuối cùng sau khi escalate khiến coordinator phải phái lại toàn bộ SubAgent, gây lãng phí nhân bội
// (thực nghiệm ch02 gặp safety: một lần viết chương phát sinh 3 lần phái lại, 17 lần gọi LLM, tỷ lệ thành công
// từ 50% giảm xuống 2.8%).
//
// Lưu ý StopReasonError / StopReasonAborted không cần liệt kê: agentcore trong
// loop.go nhận hai stop reason này sẽ trực tiếp terminate run, hoàn toàn không gọi StopGuard.
// Ở đây chỉ liệt kê các ngữ nghĩa từ chối của provider thực sự đi qua StopGuard.
var hardStopReasons = map[agentcore.StopReason]struct{}{
	"safety":         {},
	"content_filter": {},
}

// newCheckpointDeltaGuard tạo một StopGuard:
// sau baseline, nếu chưa xuất hiện checkpoint của step được chỉ định, sẽ từ chối end_turn.
// baseline được capture bởi caller tại thời điểm factory, đảm bảo ngữ nghĩa đúng theo từng run.
func newCheckpointDeltaGuard(st *store.Store, agentName string, requiredSteps []string, blockMsg string) agentcore.StopGuard {
	var baseline int64
	if cp := st.Checkpoints.LatestGlobal(); cp != nil {
		baseline = cp.Seq
	}
	need := make(map[string]struct{}, len(requiredSteps))
	for _, s := range requiredSteps {
		need[s] = struct{}{}
	}
	var consecutive atomic.Int32
	return func(_ context.Context, info agentcore.StopInfo) agentcore.StopDecision {
		// Lỗi không thể khôi phục: nâng cấp ngay, không lãng phí một lần thúc giục.
		if _, hard := hardStopReasons[info.Message.StopReason]; hard {
			slog.Error("subagent stop_guard phát hiện dừng không thể khôi phục, escalate ngay",
				"module", "host.reminder", "agent", agentName,
				"turn", info.TurnIndex, "stop_reason", info.Message.StopReason)
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		// Quét từ cuối: checkpoint mới ở phần đuôi, gặp <= baseline thì break.
		all := st.Checkpoints.All()
		for i := len(all) - 1; i >= 0; i-- {
			cp := all[i]
			if cp.Seq <= baseline {
				break
			}
			if _, ok := need[cp.Step]; ok {
				consecutive.Store(0)
				return agentcore.StopDecision{Allow: true}
			}
		}
		n := consecutive.Add(1)
		if n > subagentMaxConsecutiveBlocks {
			slog.Error("subagent stop_guard chặn liên tiếp vượt giới hạn, nâng cấp thành terminate",
				"module", "host.reminder", "agent", agentName, "turn", info.TurnIndex, "consecutive", n)
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		slog.Warn("subagent stop_guard chặn end_turn",
			"module", "host.reminder", "agent", agentName, "turn", info.TurnIndex, "consecutive", n)
		return agentcore.StopDecision{Allow: false, InjectMessage: blockMsg}
	}
}

// NewWriterStopGuard yêu cầu writer trong lượt này phải tạo ra ít nhất một lần commit_chapter thành công.
func NewWriterStopGuard(st *store.Store) agentcore.StopGuard {
	return newCheckpointDeltaGuard(st, "writer",
		[]string{"commit"},
		"Bạn phải gọi commit_chapter để lưu chương trước khi kết thúc. draft_chapter chỉ là lưu bản nháp, không tính hoàn thành.",
	)
}

// NewArchitectStopGuard yêu cầu architect trong lượt này phải ghi đĩa ít nhất một lần save_foundation.
func NewArchitectStopGuard(st *store.Store) agentcore.StopGuard {
	return newCheckpointDeltaGuard(st, "architect",
		[]string{
			"premise", "outline", "layered_outline", "characters", "world_rules",
			"expand_arc", "append_volume", "update_compass", "complete_book",
		},
		"Bạn PHẢI gọi công cụ save_foundation(...) để lưu kết quả, chưa lưu thì không được kết thúc. Ví dụ: save_foundation(type=\"premise\", scale=\"long\", content=\"# Tên truyện\\n...\"). Chỉ xuất văn bản Markdown/JSON mà không gọi công cụ = dữ liệu bị mất hoàn toàn.",
	)
}

// NewEditorStopGuard yêu cầu editor trong lượt này phải ghi đĩa sản phẩm khớp với "nhiệm vụ" trước khi kết thúc.
//
// Nhận biết nhiệm vụ: khi được phái để tạo tóm tắt, chỉ save_review (phục thẩm) không tính hoàn thành — phải xuất ra tóm tắt tương ứng.
// Ngược lại editor "được phái tạo cung tóm tắt nhưng lại phục thẩm trước" sẽ thỏa mãn tiêu chí cũ lỏng lẻo và kết thúc sớm, cung tóm tắt mãi không ghi đĩa
// (kết hợp cơ chế dedup của dispatcher từng gây vòng lặp chết xương cung giữa cuốn, xem outline-exhaustion-livelock).
// Thoát bằng StopAfterTool sẽ bỏ qua StopGuard (loop.go), nên build.go đồng bộ chuyển save_review ra khỏi hard-stop,
// cho phép sau phục thẩm tiếp tục đi đến công cụ tóm tắt, rồi để guard này kiểm soát việc kết thúc.
func NewEditorStopGuard(st *store.Store, task string) agentcore.StopGuard {
	switch {
	case strings.Contains(task, "save_volume_summary") || strings.Contains(task, "tóm tắt tập"):
		return newCheckpointDeltaGuard(st, "editor", []string{"volume_summary"},
			"Nhiệm vụ lần này là tạo tóm tắt cuốn: bạn phải gọi save_volume_summary để ghi đĩa trước khi kết thúc, save_review phục thẩm không tính hoàn thành.")
	case strings.Contains(task, "save_arc_summary") || strings.Contains(task, "tóm tắt cung"):
		return newCheckpointDeltaGuard(st, "editor", []string{"arc_summary"},
			"Nhiệm vụ lần này là tạo tóm tắt cung: bạn phải gọi save_arc_summary để ghi đĩa trước khi kết thúc, save_review phục thẩm không tính hoàn thành.")
	default:
		// Phục thẩm hoặc nhiệm vụ tạm thời: bất kỳ phục thẩm/tóm tắt nào ghi đĩa đều được (giữ hành vi lỏng lẻo như cũ).
		return newCheckpointDeltaGuard(st, "editor",
			[]string{"review", "arc_summary", "volume_summary"},
			"Bạn phải gọi một trong các hàm save_review / save_arc_summary / save_volume_summary để ghi đĩa kết quả trước khi kết thúc.")
	}
}
