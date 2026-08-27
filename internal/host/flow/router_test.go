package flow

import (
	"fmt"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// helper: tạo một Progress đang ở giai đoạn Writing, chế độ phân lớp.
func writingProgress(completed []int, flow domain.FlowState) *domain.Progress {
	return &domain.Progress{
		Phase:             domain.PhaseWriting,
		Flow:              flow,
		Layered:           true,
		CompletedChapters: completed,
	}
}

func TestRoute_NilProgress(t *testing.T) {
	if got := Route(State{Progress: nil}); got != nil {
		t.Fatalf("expected nil for nil progress, got %+v", got)
	}
}

func TestRoute_PhaseComplete(t *testing.T) {
	s := State{Progress: &domain.Progress{Phase: domain.PhaseComplete}}
	if got := Route(s); got != nil {
		t.Fatalf("expected nil at PhaseComplete, got %+v", got)
	}
}

func TestRoute_NonWritingPhasesDelegateToLLM(t *testing.T) {
	for _, phase := range []domain.Phase{domain.PhaseInit, domain.PhasePremise, domain.PhaseOutline} {
		s := State{Progress: &domain.Progress{Phase: phase}, FoundationMissing: []string{"premise"}}
		if got := Route(s); got != nil {
			t.Fatalf("phase %s should return nil, got %+v", phase, got)
		}
	}
}

// Hồi quy sự cố 2026-07-16: phase bị đẩy sang writing trong khi layered_outline chưa từng lưu được
// (Layered=false nên nhánh OutOfRange không bắt) — Router phái writer viết vô hạn với đề cương 0 chương.
// Thiếu outline/layered_outline ở giai đoạn viết phải phái architect_long chữa bộ nền, cấm phái writer.
func TestRoute_WritingPhaseMissingOutlineDispatchesArchitect(t *testing.T) {
	p := &domain.Progress{Phase: domain.PhaseWriting, Flow: domain.FlowWriting, CompletedChapters: []int{1, 2}}
	for _, missing := range [][]string{
		{"outline", "layered_outline"},
		{"outline"},
		{"layered_outline"},
	} {
		got := Route(State{Progress: p, LastCompleted: 2, FoundationMissing: missing})
		if got == nil || got.Agent != "architect_long" {
			t.Fatalf("missing=%v: expected architect_long, got %+v", missing, got)
		}
	}

	// Thiếu các mục khác (không phải đề cương) không được chặn đường viết.
	got := Route(State{Progress: p, LastCompleted: 2, FoundationMissing: []string{"compass"}})
	if got == nil || got.Agent != "writer" {
		t.Fatalf("expected writer when only compass missing, got %+v", got)
	}
}

func TestRoute_PendingRewritesFirst(t *testing.T) {
	p := writingProgress([]int{1, 2}, domain.FlowRewriting)
	p.PendingRewrites = []int{3, 5}
	got := Route(State{Progress: p})
	if got == nil || got.Agent != "writer" {
		t.Fatalf("expected writer for rewrites, got %+v", got)
	}
	if got.Task != "Viết lại chương 3" {
		t.Errorf("expected 'Viết lại chương 3', got %q", got.Task)
	}
	if got.Chapter != 3 {
		t.Errorf("expected Chapter=3, got %d", got.Chapter)
	}
}

func TestRoute_PendingPolishingVerb(t *testing.T) {
	p := writingProgress([]int{1}, domain.FlowPolishing)
	p.PendingRewrites = []int{2}
	got := Route(State{Progress: p})
	if got == nil || got.Task != "Đánh bóng chương 2" {
		t.Fatalf("expected polish verb, got %+v", got)
	}
}

func TestRoute_ReviewingDelegatesToLLM(t *testing.T) {
	p := writingProgress([]int{1, 2}, domain.FlowReviewing)
	if got := Route(State{Progress: p}); got != nil {
		t.Fatalf("expected nil during reviewing, got %+v", got)
	}
}

func TestRoute_SteeringDelegatesToLLM(t *testing.T) {
	p := writingProgress([]int{1}, domain.FlowSteering)
	if got := Route(State{Progress: p}); got != nil {
		t.Fatalf("expected nil during steering, got %+v", got)
	}
}

func TestRoute_ArcEndNeedsReview(t *testing.T) {
	p := writingProgress([]int{10}, domain.FlowWriting)
	s := State{
		Progress:      p,
		LastCompleted: 10,
		ArcBoundary: &storepkg.ArcBoundary{
			IsArcEnd: true,
			Volume:   1,
			Arc:      2,
		},
	}
	got := Route(s)
	if got == nil || got.Agent != "editor" {
		t.Fatalf("expected editor for arc review, got %+v", got)
	}
	if got.Reason != "Đánh giá cuối cung truyện chưa hoàn thành" {
		t.Errorf("reason mismatch: %q", got.Reason)
	}
}

func TestRoute_ArcEndHasReviewNeedsSummary(t *testing.T) {
	p := writingProgress([]int{10}, domain.FlowWriting)
	s := State{
		Progress:      p,
		LastCompleted: 10,
		ArcBoundary: &storepkg.ArcBoundary{
			IsArcEnd: true,
			Volume:   1,
			Arc:      2,
		},
		HasArcReview: true,
	}
	got := Route(s)
	if got == nil || got.Agent != "editor" || got.Reason != "Tóm tắt cung truyện chưa hoàn thành" {
		t.Fatalf("expected arc summary editor call, got %+v", got)
	}
}

func TestRoute_VolumeEndNeedsVolumeSummary(t *testing.T) {
	p := writingProgress([]int{20}, domain.FlowWriting)
	s := State{
		Progress:      p,
		LastCompleted: 20,
		ArcBoundary: &storepkg.ArcBoundary{
			IsArcEnd:    true,
			IsVolumeEnd: true,
			Volume:      1,
			Arc:         3,
		},
		HasArcReview:  true,
		HasArcSummary: true,
	}
	got := Route(s)
	if got == nil || got.Reason != "Tóm tắt tập chưa hoàn thành" {
		t.Fatalf("expected volume summary request, got %+v", got)
	}
}

func TestRoute_NeedsArcExpansion(t *testing.T) {
	p := writingProgress([]int{10}, domain.FlowWriting)
	s := State{
		Progress:      p,
		LastCompleted: 10,
		ArcBoundary: &storepkg.ArcBoundary{
			IsArcEnd:       true,
			Volume:         1,
			Arc:            2,
			NextVolume:     1,
			NextArc:        3,
			NeedsExpansion: true,
		},
		HasArcReview:  true,
		HasArcSummary: true,
	}
	got := Route(s)
	if got == nil || got.Agent != "architect_long" {
		t.Fatalf("expected architect_long for expansion, got %+v", got)
	}
	if got.Reason != "Skeleton cung truyện tiếp theo cần được mở rộng" {
		t.Errorf("reason mismatch: %q", got.Reason)
	}
}

func TestRoute_NeedsNewVolume(t *testing.T) {
	p := writingProgress([]int{30}, domain.FlowWriting)
	s := State{
		Progress:      p,
		LastCompleted: 30,
		ArcBoundary: &storepkg.ArcBoundary{
			IsArcEnd:       true,
			IsVolumeEnd:    true,
			Volume:         2,
			Arc:            4,
			NeedsNewVolume: true,
		},
		HasArcReview:     true,
		HasArcSummary:    true,
		HasVolumeSummary: true,
	}
	got := Route(s)
	if got == nil || got.Agent != "architect_long" || got.Reason != "Cuối tập cần quyết định thêm tập mới hay kết thúc toàn bộ tác phẩm" {
		t.Fatalf("expected append_volume/complete_book dispatch, got %+v", got)
	}
}

func TestRoute_NormalContinue(t *testing.T) {
	p := writingProgress([]int{1, 2, 3}, domain.FlowWriting)
	p.TotalChapters = 20
	got := Route(State{Progress: p, LastCompleted: 3})
	if got == nil || got.Agent != "writer" {
		t.Fatalf("expected writer for next chapter, got %+v", got)
	}
	if got.Task != "Viết chương 4" {
		t.Errorf("expected 'Viết chương 4', got %q", got.Task)
	}
	if got.Chapter != 4 {
		t.Errorf("expected Chapter=4, got %d", got.Chapter)
	}
}

func TestRoute_ArcEndNonLayeredSkipsBoundary(t *testing.T) {
	// Chế độ không phải Layered: dù ArcBoundary khác nil vẫn không đi vào nhánh cuối cung truyện
	p := &domain.Progress{
		Phase:             domain.PhaseWriting,
		Flow:              domain.FlowWriting,
		Layered:           false,
		CompletedChapters: []int{10},
		TotalChapters:     20,
	}
	s := State{
		Progress:      p,
		LastCompleted: 10,
		ArcBoundary:   &storepkg.ArcBoundary{IsArcEnd: true, Volume: 1, Arc: 2},
	}
	got := Route(s)
	if got == nil || got.Agent != "writer" {
		t.Fatalf("non-layered should fall through to writer, got %+v", got)
	}
}

// Hồi quy vòng lặp: đề cương ngắn hơn tiến độ viết phải sinh ra lệnh chữa cháy cho architect_long,
// tuyệt đối không được giao writer viết chương không có trong đề cương.
// Trước đây CheckArcBoundary trả nil ở trạng thái này → Router bỏ qua nhánh cuối cung và rơi xuống
// "Viết chương 37" vĩnh viễn (đã viết 36 chương, đề cương chỉ còn 25).
func TestRoute_OutOfRangeDispatchesArchitectNotWriter(t *testing.T) {
	p := writingProgress([]int{1, 2, 3}, domain.FlowWriting)
	p.Layered = true
	s := State{
		Progress:      p,
		LastCompleted: 3,
		ArcBoundary:   &storepkg.ArcBoundary{OutOfRange: true, Volume: 3, Arc: 3, NeedsNewVolume: true},
	}
	got := Route(s)
	if got == nil || got.Agent != "architect_long" {
		t.Fatalf("expected architect_long to repair short outline, got %+v", got)
	}
	if !contains(got.Task, "append_volume") {
		t.Errorf("expected append_volume task, got %q", got.Task)
	}
}

// OutOfRange phải được xét TRƯỚC nhánh cuối cung: boundary ở trạng thái này mô tả chỗ cần chữa,
// không phải vị trí thật của chương, nên không được rơi vào editor/expand_arc bình thường.
func TestRoute_OutOfRangeTakesPriorityOverArcEnd(t *testing.T) {
	p := writingProgress([]int{1, 2, 3}, domain.FlowWriting)
	p.Layered = true
	s := State{
		Progress:      p,
		LastCompleted: 3,
		ArcBoundary: &storepkg.ArcBoundary{
			OutOfRange: true, IsArcEnd: true, NeedsNewVolume: true,
		},
		HasArcReview: false, // nhánh cuối cung sẽ đòi editor nếu OutOfRange không chặn trước
	}
	got := Route(s)
	if got == nil || got.Agent != "architect_long" {
		t.Fatalf("expected architect_long, got %+v", got)
	}
}

// Hàng đợi viết lại vẫn phải được ưu tiên hơn việc chữa đề cương: các chương trong hàng đợi đã có bản thảo,
// viết lại chúng không phụ thuộc vào phần đề cương còn thiếu ở phía trước.
func TestRoute_PendingRewritesWinOverOutOfRange(t *testing.T) {
	p := writingProgress([]int{1, 2, 3}, domain.FlowPolishing)
	p.Layered = true
	p.PendingRewrites = []int{2}
	s := State{
		Progress:      p,
		LastCompleted: 3,
		ArcBoundary:   &storepkg.ArcBoundary{OutOfRange: true, NeedsNewVolume: true},
	}
	got := Route(s)
	if got == nil || got.Agent != "writer" || got.Chapter != 2 {
		t.Fatalf("expected writer polishing ch2, got %+v", got)
	}
}

func TestFormatMessage(t *testing.T) {
	msg := FormatMessage(&Instruction{Agent: "writer", Task: "Viết chương 5", Reason: "Viết tiếp"})
	for _, want := range []string{"[Host ra lệnh]", "writer", "Viết chương 5", "Viết tiếp", "không được gọi novel_context trước"} {
		if !contains(msg, want) {
			t.Errorf("message missing %q: %s", want, msg)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestDispatcher_TrackRepeat(t *testing.T) {
	// Không cần coordinator / store thật; trackRepeat chỉ đọc cache nội bộ.
	d := &Dispatcher{}
	inst := &Instruction{Agent: "writer", Task: "Viết chương 5", Reason: "Viết tiếp"}
	if got := d.trackRepeat(inst); got != 1 {
		t.Fatalf("lần đầu hạ lệnh phải tính 1, got %d", got)
	}
	if got := d.trackRepeat(inst); got != 2 {
		t.Fatalf("cùng Agent+Task lặp lại phải tính 2, got %d", got)
	}
	// Reason khác nhau, Agent+Task giống nhau vẫn coi là cùng lệnh và tiếp tục cộng dồn
	sameTaskDiffReason := &Instruction{Agent: "writer", Task: "Viết chương 5", Reason: "Tiếp tục sau cuối cung"}
	if got := d.trackRepeat(sameTaskDiffReason); got != 3 {
		t.Fatalf("chỉ khác Reason vẫn tính là lặp, cộng dồn lên 3, got %d", got)
	}
	other := &Instruction{Agent: "writer", Task: "Viết chương 6", Reason: "Viết tiếp"}
	if got := d.trackRepeat(other); got != 1 {
		t.Fatalf("Task thay đổi phải reset về 1, got %d", got)
	}
	d.ResetRepeat()
	if got := d.trackRepeat(other); got != 1 {
		t.Fatalf("sau ResetRepeat lần đầu phải tính 1, got %d", got)
	}
}

func TestFormatDispatchMessage_RepeatNotice(t *testing.T) {
	inst := &Instruction{Agent: "writer", Task: "Viết chương 5", Reason: "Viết tiếp"}
	first := formatDispatchMessage(inst, 1)
	if first != FormatMessage(inst) {
		t.Fatalf("lần đầu hạ lệnh không được đính kèm ghi chú lặp: %s", first)
	}
	third := formatDispatchMessage(inst, 3)
	for _, want := range []string{"lần thứ 3", "sự thật route chưa thay đổi", "novel_context", "chuyển sang agent phụ khác"} {
		if !contains(third, want) {
			t.Errorf("ghi chú lặp thiếu %q: %s", want, third)
		}
	}
}

func TestDispatcher_OnRepeatFiresOnceAtThreshold(t *testing.T) {
	d := &Dispatcher{}
	var fired []string
	d.SetOnRepeat(func(agent, task string, n int) {
		fired = append(fired, fmt.Sprintf("%s|%s|%d", agent, task, n))
	})

	inst := &Instruction{Agent: "writer", Task: "Viết chương 5"}
	for range 6 {
		d.trackRepeat(inst) // n=1..6: chỉ callback đúng một lần khi n==3
	}
	if len(fired) != 1 || fired[0] != fmt.Sprintf("writer|Viết chương 5|%d", repeatNotifyAt) {
		t.Fatalf("phải trigger đúng một lần tại lần thứ %d, got %v", repeatNotifyAt, fired)
	}

	// Sau khi đổi key sẽ được tái vũ trang: đổi task rồi lặp tiếp 3 lần → trigger thêm một lần
	other := &Instruction{Agent: "writer", Task: "Viết chương 6"}
	for range 3 {
		d.trackRepeat(other)
	}
	if len(fired) != 2 {
		t.Fatalf("sau khi đổi key phải tái vũ trang, got %v", fired)
	}
}
