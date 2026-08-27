package flow

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// rewriteQueueDispatcher dựng một Dispatcher đang đánh bóng chương `target`,
// với hàng đợi `queue`, trên một store thật trong thư mục tạm.
func rewriteQueueDispatcher(t *testing.T, target int, queue []int) (*Dispatcher, *storepkg.Store) {
	t.Helper()
	store := storepkg.NewStore(t.TempDir())
	if err := store.Progress.Save(&domain.Progress{
		Phase:             domain.PhaseWriting,
		Flow:              domain.FlowPolishing,
		Layered:           true,
		CompletedChapters: queue,
		PendingRewrites:   queue,
	}); err != nil {
		t.Fatalf("save progress: %v", err)
	}
	d := &Dispatcher{store: store}
	d.lastSent = &Instruction{Agent: "writer", Task: "Đánh bóng chương", Chapter: target}
	return d, store
}

func pendingRewrites(t *testing.T, store *storepkg.Store) []int {
	t.Helper()
	p, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("load progress: %v", err)
	}
	return p.PendingRewrites
}

// Ngưỡng đã thống nhất là 3 lần. Khẳng định bằng số cụ thể chứ không tham chiếu hằng số:
// các test dưới đây đếm tay đúng 3 lần, nếu hằng đổi mà test vẫn xanh thì test vô nghĩa.
func TestRewriteFailLimitIsThree(t *testing.T) {
	if rewriteFailLimit != 3 {
		t.Fatalf("rewriteFailLimit = %d, chính sách đã chốt là 3", rewriteFailLimit)
	}
}

// Hai lần thất bại đầu không được đụng vào hàng đợi; đúng lần thứ 3 mới loại chương.
func TestDropExhaustedRewrite_DropsAtThirdFailure(t *testing.T) {
	d, store := rewriteQueueDispatcher(t, 32, []int{32, 33})

	var gotChapter, gotFails int
	d.SetOnDropRewrite(func(chapter, fails int, _ string) {
		gotChapter, gotFails = chapter, fails
	})

	if d.dropExhaustedRewrite() {
		t.Fatal("lần thất bại 1: chưa được loại chương")
	}
	if d.dropExhaustedRewrite() {
		t.Fatal("lần thất bại 2: chưa được loại chương")
	}
	if got := pendingRewrites(t, store); len(got) != 2 {
		t.Fatalf("sau 2 lần thất bại hàng đợi = %v, phải còn nguyên [32 33]", got)
	}

	if !d.dropExhaustedRewrite() {
		t.Fatal("lần thất bại 3: phải loại chương khỏi hàng đợi")
	}
	if got := pendingRewrites(t, store); len(got) != 1 || got[0] != 33 {
		t.Errorf("hàng đợi sau khi loại = %v, muốn [33]", got)
	}
	if gotChapter != 32 || gotFails != 3 {
		t.Errorf("cảnh báo = (chương %d, %d lần), muốn (32, 3)", gotChapter, gotFails)
	}
}

// Bộ đếm chỉ tính thất bại LIÊN TIẾP: một lần thành công xen giữa phải xóa sạch nó.
func TestDropExhaustedRewrite_SuccessResetsCounter(t *testing.T) {
	d, store := rewriteQueueDispatcher(t, 32, []int{32, 33})

	d.dropExhaustedRewrite() // thất bại 1
	d.dropExhaustedRewrite() // thất bại 2
	d.resetRewriteFailure()  // agent phụ trả về thành công

	// Hai lần thất bại tiếp theo phải được tính lại từ đầu, không cộng vào 2 lần cũ.
	if d.dropExhaustedRewrite() {
		t.Fatal("sau khi reset, lần thất bại 1 không được loại chương")
	}
	if d.dropExhaustedRewrite() {
		t.Fatal("sau khi reset, lần thất bại 2 không được loại chương")
	}
	if got := pendingRewrites(t, store); len(got) != 2 {
		t.Errorf("hàng đợi = %v, muốn còn nguyên [32 33]", got)
	}
}

// Thất bại rải rác trên các chương khác nhau không được cộng dồn vào cùng một bộ đếm.
func TestDropExhaustedRewrite_CounterIsPerChapter(t *testing.T) {
	d, store := rewriteQueueDispatcher(t, 32, []int{32, 33})

	// Xen kẽ 32/33/32/33... — nếu bộ đếm cộng dồn xuyên chương thì tổng 4 lần đã vượt ngưỡng 3.
	for i := 0; i < 2; i++ {
		d.lastSent = &Instruction{Agent: "writer", Task: "Đánh bóng chương", Chapter: 32}
		if d.dropExhaustedRewrite() {
			t.Fatal("bộ đếm bị cộng dồn xuyên chương (32)")
		}
		d.lastSent = &Instruction{Agent: "writer", Task: "Đánh bóng chương", Chapter: 33}
		if d.dropExhaustedRewrite() {
			t.Fatal("bộ đếm bị cộng dồn xuyên chương (33)")
		}
	}
	if len(pendingRewrites(t, store)) != 2 {
		t.Errorf("hàng đợi = %v, muốn còn nguyên 2 chương", pendingRewrites(t, store))
	}
}

// Chương đã rời hàng đợi (commit_chapter drain xong) thì thất bại sau đó không liên quan tới nó nữa.
func TestDropExhaustedRewrite_IgnoresChapterNoLongerQueued(t *testing.T) {
	d, _ := rewriteQueueDispatcher(t, 99, []int{32})
	for i := 0; i < 5; i++ {
		if d.dropExhaustedRewrite() {
			t.Fatal("chương ngoài hàng đợi không được kích hoạt loại bỏ")
		}
	}
}

// Lệnh không phải của writer (editor/architect thất bại) không đụng tới hàng đợi viết lại.
func TestDropExhaustedRewrite_IgnoresNonWriterDispatch(t *testing.T) {
	d, store := rewriteQueueDispatcher(t, 32, []int{32})
	d.lastSent = &Instruction{Agent: "editor", Task: "Đánh giá cung truyện"}
	for i := 0; i < 5; i++ {
		if d.dropExhaustedRewrite() {
			t.Fatal("lệnh không phải writer không được loại chương")
		}
	}
	if len(pendingRewrites(t, store)) != 1 {
		t.Errorf("hàng đợi = %v, muốn còn nguyên", pendingRewrites(t, store))
	}
}

// Loại chương cuối cùng phải trả flow về writing, nếu không sách kẹt ở polishing với hàng đợi rỗng.
func TestDropExhaustedRewrite_LastChapterReturnsFlowToWriting(t *testing.T) {
	d, store := rewriteQueueDispatcher(t, 32, []int{32})

	for i := 0; i < 3; i++ {
		d.dropExhaustedRewrite()
	}

	p, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("load progress: %v", err)
	}
	if len(p.PendingRewrites) != 0 {
		t.Errorf("hàng đợi = %v, muốn rỗng", p.PendingRewrites)
	}
	if p.Flow != domain.FlowWriting {
		t.Errorf("flow = %s, muốn %s — hàng đợi rỗng phải quay về viết chương mới", p.Flow, domain.FlowWriting)
	}
}
