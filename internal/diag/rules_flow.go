package diag

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// InvalidPendingRewrites phát hiện các chương chưa hoàn thành lẫn vào hàng đợi làm lại.
func InvalidPendingRewrites(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.PendingRewrites) == 0 {
		return nil
	}
	p := snap.Progress
	completed := append([]int(nil), p.CompletedChapters...)
	slices.Sort(completed)

	var invalid []int
	for _, ch := range p.PendingRewrites {
		if ch <= 0 || !slices.Contains(completed, ch) {
			invalid = append(invalid, ch)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	slices.Sort(invalid)
	return []Finding{{
		Rule:       "InvalidPendingRewrites",
		Category:   CatFlow,
		Severity:   SevCritical,
		Confidence: ConfHigh,
		AutoLevel:  AutoSuggest,
		Target:     "meta/progress.json",
		Title:      fmt.Sprintf("Hàng đợi làm lại chứa chương chưa hoàn thành: [%s]", intsToStr(invalid)),
		Evidence:   fmt.Sprintf("pending_rewrites=[%s], completed_chapters=[%s], flow=%s", intsToStr(p.PendingRewrites), intsToStr(completed), p.Flow),
		Suggestion: "Đây là lỗi hỏng bất biến trạng thái. Hãy dừng chạy rồi chỉnh sửa meta/progress.json, xóa các chương chưa hoàn thành khỏi pending_rewrites; nếu hàng đợi rỗng, đổi flow thành writing và xóa rewrite_reason.",
	}}
}

// RewritePendingPressure phát hiện các chương đang chờ viết lại (chỉ kiểm tra trạng thái tồn tại, không xét tình trạng tắc nghẽn).
func RewritePendingPressure(snap *Snapshot) []Finding {
	if snap.Progress == nil {
		return nil
	}
	p := snap.Progress
	if len(p.PendingRewrites) == 0 {
		return nil
	}
	if p.Flow != domain.FlowRewriting && p.Flow != domain.FlowPolishing {
		return nil
	}
	chapters := intsToStr(p.PendingRewrites)
	return []Finding{{
		Rule:       "RewritePendingPressure",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      fmt.Sprintf("Chương đang chờ viết lại: [%s]", chapters),
		Evidence:   fmt.Sprintf("flow=%s, pending_rewrites=[%s]", p.Flow, chapters),
		Suggestion: "Kiểm tra tiêu chí đánh giá của Biên tập viên có quá nghiêm khắc không, hoặc prompt viết lại của Người viết có hiệu quả không." +
			" Nếu cần can thiệp thủ công, hãy gửi lệnh can thiệp trong hộp nhập liệu.",
	}}
}

// OrphanedSteer phát hiện lệnh chuyển hướng của người dùng chưa được tiêu thụ.
func OrphanedSteer(snap *Snapshot) []Finding {
	if snap.RunMeta == nil || snap.RunMeta.PendingSteer == "" {
		return nil
	}
	if snap.Progress != nil && snap.Progress.Flow == domain.FlowSteering {
		return nil // Đang xử lý, không tính là bị bỏ sót
	}
	return []Finding{{
		Rule:       "OrphanedSteer",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoSafe,
		Target:     "runtime.recovery",
		Title:      "Tồn tại lệnh chuyển hướng chưa được tiêu thụ",
		Evidence:   fmt.Sprintf("pending_steer=%q, flow=%s", truncStr(snap.RunMeta.PendingSteer, 60), flowStr(snap.Progress)),
		Suggestion: "Lệnh steer này đã được lưu bền vững nhưng chưa được Điều phối viên tiêu thụ. Kiểm tra logic phục hồi sau gián đoạn, hoặc gửi lại để ghi đè.",
	}}
}

// PhaseFlowMismatch phát hiện giai đoạn và trạng thái flow không khớp nhau.
func PhaseFlowMismatch(snap *Snapshot) []Finding {
	if snap.Progress == nil {
		return nil
	}
	p := snap.Progress
	if p.Phase == domain.PhaseWriting || p.Phase == "" {
		return nil
	}
	if p.Flow == "" || p.Flow == domain.FlowWriting {
		return nil
	}
	return []Finding{{
		Rule:       "PhaseFlowMismatch",
		Category:   CatFlow,
		Severity:   SevCritical,
		Confidence: ConfHigh,
		AutoLevel:  AutoSafe,
		Target:     "runtime.flow",
		Title:      fmt.Sprintf("Giai đoạn/trạng thái flow không khớp: phase=%s, flow=%s", p.Phase, p.Flow),
		Evidence:   fmt.Sprintf("phase=%s không nên xuất hiện flow=%s không phải trạng thái ban đầu", p.Phase, p.Flow),
		Suggestion: "Máy trạng thái có thể bị hỏng, cần kiểm tra thủ công các trường phase và flow trong meta/progress.json.",
	}}
}

// ChapterGaps phát hiện số chương bị nhảy trong danh sách các chương đã hoàn thành.
func ChapterGaps(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.CompletedChapters) < 2 {
		return nil
	}
	sorted := append([]int(nil), snap.Progress.CompletedChapters...)
	sort.Ints(sorted)

	var gaps []int
	for i := 1; i < len(sorted); i++ {
		for ch := sorted[i-1] + 1; ch < sorted[i]; ch++ {
			gaps = append(gaps, ch)
		}
	}
	if len(gaps) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "ChapterGaps",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      fmt.Sprintf("Số chương bị nhảy: thiếu [%s]", intsToStr(gaps)),
		Evidence:   fmt.Sprintf("completed=[%s]", intsToStr(sorted)),
		Suggestion: "commit_chapter có thể đã bị gián đoạn giữa chừng. Kiểm tra meta/pending_commit.json xem có commit chưa hoàn thành không.",
	}}
}

func flowStr(p *domain.Progress) string {
	if p == nil {
		return "<nil>"
	}
	return string(p.Flow)
}

func truncStr(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}

func intsToStr(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("%d", n)
	}
	return strings.Join(parts, ", ")
}
