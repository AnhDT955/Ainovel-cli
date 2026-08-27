package domain

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/errs"
)

// Quy tắc chuyển trạng thái (phiên bản tối giản)
//
// Phase đại diện cho giai đoạn lớn, áp dụng ràng buộc “chỉ tiến không lùi”:
//
//	init -> premise -> outline -> writing -> complete
//	  \---------> outline ------^
//	  \-----------------> writing
//
// Flow đại diện cho luồng đang hoạt động, cho phép chuyển đổi trong giai đoạn viết,
// nhưng không cho phép các bước nhảy bất thường rõ ràng:
//
//	writing   -> reviewing / rewriting / polishing / steering / writing
//	reviewing -> writing / rewriting / polishing / steering / reviewing
//	rewriting -> writing / steering / rewriting
//	polishing -> writing / steering / polishing
//	steering  -> writing / reviewing / rewriting / polishing / steering
//
// Trạng thái rỗng (zero value) được coi là “chưa khởi tạo”, cho phép chuyển sang bất kỳ trạng thái hợp lệ không rỗng nào.

var phaseOrder = map[Phase]int{
	PhaseInit:     1,
	PhasePremise:  2,
	PhaseOutline:  3,
	PhaseWriting:  4,
	PhaseComplete: 5,
}

// CanTransitionPhase kiểm tra xem Phase có được phép chuyển hay không.
// Quy tắc giữ đơn giản: cho phép chuyển cùng trạng thái, cho phép tiến, không cho phép lùi.
func CanTransitionPhase(from, to Phase) bool {
	if to == "" {
		return false
	}
	if from == "" || from == to {
		return true
	}
	fromOrder, fromOK := phaseOrder[from]
	toOrder, toOK := phaseOrder[to]
	if !fromOK || !toOK {
		return false
	}
	return toOrder >= fromOrder
}

// ValidatePhaseTransition xác thực xem việc chuyển Phase có hợp lệ hay không.
func ValidatePhaseTransition(from, to Phase) error {
	if CanTransitionPhase(from, to) {
		return nil
	}
	return fmt.Errorf("invalid phase transition: %q -> %q: %w", from, to, errs.ErrPhaseTransition)
}

// CanTransitionFlow kiểm tra xem FlowState có được phép chuyển hay không.
func CanTransitionFlow(from, to FlowState) bool {
	if to == "" {
		return false
	}
	if from == "" || from == to {
		return true
	}

	switch from {
	case FlowWriting:
		return to == FlowReviewing || to == FlowRewriting || to == FlowPolishing || to == FlowSteering
	case FlowReviewing:
		return to == FlowWriting || to == FlowRewriting || to == FlowPolishing || to == FlowSteering
	case FlowRewriting:
		return to == FlowWriting || to == FlowSteering
	case FlowPolishing:
		return to == FlowWriting || to == FlowSteering
	case FlowSteering:
		return to == FlowWriting || to == FlowReviewing || to == FlowRewriting || to == FlowPolishing
	default:
		return false
	}
}

// ValidateFlowTransition xác thực xem việc chuyển FlowState có hợp lệ hay không.
func ValidateFlowTransition(from, to FlowState) error {
	if CanTransitionFlow(from, to) {
		return nil
	}
	return fmt.Errorf("invalid flow transition: %q -> %q: %w", from, to, errs.ErrFlowTransition)
}
