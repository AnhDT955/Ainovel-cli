package flow

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/voocel/agentcore"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// Dispatcher đăng ký lắng nghe sự kiện từ Coordinator, tính toán tuyến đường khi agent phụ trả về và gửi lệnh cho Host.
//
// Vòng đời: Attach trả về một hàm detach; gọi hàm đó khi đóng Host để giải phóng đăng ký.
type Dispatcher struct {
	coordinator *agentcore.Agent
	store       *storepkg.Store

	enabled atomic.Bool // do Host kiểm soát có phát lệnh hay không (nên tắt trước khi khởi động xong)

	// Theo dõi lặp: ghi nhớ Agent+Task đã phát lần gần nhất và số lần phát liên tiếp.
	// Cùng một lệnh tính lại (agent phụ trả về nhưng trạng thái chưa tiến, Route tính lại ra cùng kết quả) không bị nuốt yên lặng,
	// mà được phát lại kèm số lần thực tế — "kết quả route liên tiếp N lần giống nhau" là sự thật chỉ Host quan sát được;
	// nếu im lặng, Coordinator sẽ rơi vào mâu thuẫn giữa "cấm tự quyết bước tiếp theo" (coordinator.md) và
	// "cấm dừng máy" (StopGuard), tự do hành động sẽ dẫn đến vòng lặp freelance kiểu #24.
	// Quyền phán quyết vẫn thuộc LLM: tin phát lại chỉ kèm sự thật và cho phép kiểm tra, không đặt ngưỡng, không ngắt mạch (kiến trúc §10.13).
	// Vì tin có kèm số lần nên mỗi lần khác nhau, không bị ép lệnh giống hệt vào followUpQ nhiều lần.
	lastMu   sync.Mutex
	lastSent *Instruction
	repeats  int

	// Đếm thất bại liên tiếp của agent phụ trên cùng một chương trong hàng đợi viết lại.
	// Trong bộ nhớ chứ không ghi đĩa: khởi động lại tiến trình là một sự thật mới (người dùng
	// đã can thiệp, quota đã hồi, model đã đổi), chương đó xứng đáng có cơ hội mới.
	failMu    sync.Mutex
	failChap  int
	failCount int

	// onRepeat là callback telemetry thuần túy (dùng cho cảnh báo chế độ không giao diện), kích hoạt một lần
	// khi cùng một lệnh được phát đến lần thứ repeatNotifyAt; không ảnh hưởng ngược lại logic phát lệnh, logic phát không hay biết về sự tồn tại của nó.
	onRepeat func(agent, task string, n int)

	// onDropRewrite báo cho người dùng biết một chương đã bị loại khỏi hàng đợi viết lại.
	// Khác onRepeat, đây KHÔNG phải telemetry thuần: nó đi kèm một hành động thật, nên bắt buộc phải báo.
	onDropRewrite func(chapter, fails int, reason string)
}

// repeatNotifyAt cố định không đưa vào cấu hình: đây không phải ngưỡng luồng điều khiển (không kích hoạt hành động nào, chỉ là "gọi người"),
// điều chỉnh không mang lại lợi ích; đưa vào cấu hình lại ngầm ám chỉ có thể chỉnh ra hành vi khác.
const repeatNotifyAt = 3

// rewriteFailLimit là số lần agent phụ được phép thất bại liên tiếp trên cùng một chương
// trong hàng đợi viết lại, trước khi chương đó bị loại khỏi hàng đợi.
//
// Đây là NGOẠI LỆ CÓ CHỦ Ý của Quy tắc sắt 2 ("không đặt ngưỡng, không ngắt mạch; cách thoát
// khỏi bế tắc do LLM phán quyết" — kiến trúc §Quy tắc sắt 2). Quy tắc đó giả định LLM CÓ
// đường thoát; với hàng đợi viết lại thì không: Route ưu tiên hàng đợi hơn mọi việc khác,
// và không tool nào cho phép LLM bỏ một chương ra khỏi hàng đợi (ClearPendingRewrites chưa
// từng được gọi từ bất cứ đâu). Bế tắc quan sát được trong thực tế: chương 28/30/32 lặp lại
// hàng giờ vì writer cụt lượt (max turns) trước khi kịp commit_chapter, sách đứng im.
// Khi nào LLM có tool tự bỏ chương khỏi hàng đợi, ngưỡng này nên được gỡ bỏ.
const rewriteFailLimit = 3

// NewDispatcher tạo Dispatcher. Cần gọi Attach để đăng ký sự kiện trước khi sử dụng.
func NewDispatcher(coordinator *agentcore.Agent, store *storepkg.Store) *Dispatcher {
	d := &Dispatcher{coordinator: coordinator, store: store}
	return d
}

// Enable bật phát lệnh theo tuyến đường; khi tắt, EventToolExecEnd đến sẽ không gửi FollowUp.
// Host bật sau khi hoàn thành prompt đầu tiên trong Start/Resume, tránh xung đột với luồng khởi động.
func (d *Dispatcher) Enable() { d.enabled.Store(true) }

// Attach đăng ký lắng nghe sự kiện Coordinator; hàm trả về dùng để hủy đăng ký khi đóng.
func (d *Dispatcher) Attach() func() {
	return d.coordinator.Subscribe(d.handle)
}

func (d *Dispatcher) handle(ev agentcore.Event) {
	if !d.enabled.Load() {
		return
	}
	// Điểm kích hoạt chính xác: agent phụ trả về thành công, hoặc reopen_book mở lại sách đã hoàn thành vào trạng thái làm lại.
	// Cả hai đều đã tiến lớp thực tế, cần ngay một lần tính Route để xác định bước tiếp theo — reopen_book không phải subagent
	// (pha complete cần vượt qua completePhaseGate), nếu không kích hoạt ở đây, hàng đợi làm lại sau khi mở lại sẽ không có dispatcher.
	// Không dùng EventModelResponse vì agentcore emit nó mỗi lần LLM call hoàn thành,
	// sẽ ép cùng một lệnh vào followUpQ nhiều lần; Steer kiểu truy vấn bị coordinator.md ràng buộc tiếp tục gọi subagent
	// trong cùng một turn, từ đó chạm điểm kích hoạt này.
	if ev.Type != agentcore.EventToolExecEnd {
		return
	}
	if ev.Tool != "subagent" && ev.Tool != "reopen_book" {
		return
	}
	// Agent phụ thất bại: không có tiến triển thực tế nào để định tuyến lại, nhưng thất bại
	// LẶP LẠI trên cùng một chương của hàng đợi viết lại là sự thật chỉ Host quan sát được,
	// và là bế tắc duy nhất LLM không tự thoát được (xem rewriteFailLimit).
	if ev.IsError {
		d.trackRewriteFailure()
		return
	}
	d.resetRewriteFailure()
	d.Dispatch()
}

// trackRewriteFailure đếm thất bại liên tiếp trên chương đang được đánh bóng/viết lại;
// khi một chương bị loại, hàng đợi đã đổi nên phải định tuyến lại ngay, nếu không cuốn sách
// đứng im chờ sự kiện kế tiếp vốn sẽ không bao giờ tới.
func (d *Dispatcher) trackRewriteFailure() {
	if d.dropExhaustedRewrite() {
		d.ResetRepeat()
		d.Dispatch()
	}
}

// dropExhaustedRewrite cộng một lần thất bại cho chương đang xử lý và loại nó khỏi hàng đợi
// khi chạm rewriteFailLimit. Trả về true nếu vừa loại một chương.
func (d *Dispatcher) dropExhaustedRewrite() bool {
	chapter := d.pendingRewriteTarget()
	if chapter <= 0 {
		return false
	}

	d.failMu.Lock()
	if d.failChap != chapter {
		d.failChap = chapter
		d.failCount = 0
	}
	d.failCount++
	n := d.failCount
	d.failMu.Unlock()

	if n < rewriteFailLimit {
		slog.Warn("rewrite chapter failed", "module", "host.flow", "chapter", chapter, "fails", n, "limit", rewriteFailLimit)
		return false
	}

	reason := fmt.Sprintf("agent phụ thất bại %d lần liên tiếp", n)
	if err := d.store.Progress.DropRewrite(chapter); err != nil {
		slog.Error("drop rewrite chapter failed", "module", "host.flow", "chapter", chapter, "err", err)
		return false
	}
	slog.Warn("rewrite chapter dropped from queue", "module", "host.flow", "chapter", chapter, "fails", n, "reason", reason)

	d.failMu.Lock()
	d.failChap = 0
	d.failCount = 0
	d.failMu.Unlock()

	if d.onDropRewrite != nil {
		d.onDropRewrite(chapter, n, reason)
	}
	return true
}

// resetRewriteFailure xóa bộ đếm khi agent phụ trả về thành công — bộ đếm chỉ tính thất bại LIÊN TIẾP.
func (d *Dispatcher) resetRewriteFailure() {
	d.failMu.Lock()
	d.failChap = 0
	d.failCount = 0
	d.failMu.Unlock()
}

// pendingRewriteTarget trả về chương mà lệnh gần nhất giao cho writer xử lý, và chỉ khi chương đó
// thực sự đang nằm trong hàng đợi viết lại. Đọc lại từ đĩa chứ không tin lastSent: giữa lúc phát
// lệnh và lúc thất bại, commit_chapter có thể đã drain chương đó khỏi hàng đợi rồi.
func (d *Dispatcher) pendingRewriteTarget() int {
	d.lastMu.Lock()
	last := d.lastSent
	d.lastMu.Unlock()
	if last == nil || last.Agent != "writer" || last.Chapter <= 0 || d.store == nil {
		return 0
	}
	p, err := d.store.Progress.Load()
	if err != nil || p == nil || !slices.Contains(p.PendingRewrites, last.Chapter) {
		return 0
	}
	return last.Chapter
}

// Dispatch tính toán tuyến đường ngay lập tức và gửi lệnh; Host có thể chủ động gọi vào thời điểm đặc biệt (ví dụ sau Resume).
func (d *Dispatcher) Dispatch() {
	state := LoadState(d.store)
	inst := Route(state)
	if inst == nil {
		return
	}
	n := d.trackRepeat(inst)
	// Tác vụ Người viết: đánh dấu chương là đang tiến hành ngay lúc phát lệnh, đề cương bên phải TUI phản ánh ngay "▸ đang tiến hành",
	// không cần chờ plan_chapter thực sự thực thi (plan_chapter sẽ gọi StartChapter lần nữa, idempotent).
	if inst.Agent == "writer" && inst.Chapter > 0 && d.store != nil {
		if err := d.store.Progress.ValidateChapterWork(inst.Chapter); err != nil {
			slog.Error("flow router refuses invalid writer dispatch", "module", "host.flow", "chapter", inst.Chapter, "err", err)
			return
		}
		if err := d.store.Progress.StartChapter(inst.Chapter); err != nil {
			slog.Warn("flow router pre-mark in-progress failed", "module", "host.flow", "chapter", inst.Chapter, "err", err)
		}
	}
	msg := formatDispatchMessage(inst, n)
	slog.Debug("flow router dispatch", "module", "host.flow", "agent", inst.Agent, "reason", inst.Reason, "repeat", n)
	d.coordinator.FollowUp(agentcore.UserMsg(msg))
}

// formatDispatchMessage tạo nội dung lệnh gửi đến Coordinator.
// Khi n>1, kèm thêm sự thật về việc lặp — thông báo "sau lần phát trước, sự thật route chưa thay đổi" và mở quyền kiểm tra,
// để LLM tự phán quyết tiếp tục thực hiện hay chuyển sang agent phụ khác; Host không áp đặt bất kỳ nhánh bắt buộc nào.
func formatDispatchMessage(inst *Instruction, n int) string {
	msg := FormatMessage(inst)
	if n > 1 {
		msg += fmt.Sprintf("\n（Lưu ý: Đây là lần thứ %d lệnh này được phát — sau lần phát trước, sự thật route chưa thay đổi. Lần này được phép gọi novel_context kiểm tra sự thật trước, rồi phán quyết tiếp tục thực hiện hoặc chuyển sang agent phụ khác.）", n)
	}
	return msg
}

// SetOnRepeat đăng ký callback telemetry cho lệnh lặp. Phải gọi một lần trước khi Attach/bắt đầu phát lệnh.
func (d *Dispatcher) SetOnRepeat(cb func(agent, task string, n int)) {
	d.onRepeat = cb
}

// SetOnDropRewrite đăng ký callback cảnh báo khi một chương bị loại khỏi hàng đợi viết lại.
// Phải gọi một lần trước khi Attach.
func (d *Dispatcher) SetOnDropRewrite(cb func(chapter, fails int, reason string)) {
	d.onDropRewrite = cb
}

// trackRepeat ghi lại số lần phát liên tiếp cùng một lệnh và trả về số lần hiện tại (1 = lệnh mới).
// Dùng đẳng thức Agent+Task (không so Reason vì Reason là văn bản phụ trợ cho người đọc).
// Khi số lần đúng bằng repeatNotifyAt, kích hoạt onRepeat một lần ngoài lock (sau khi khóa thay đổi thì đặt lại bộ đếm).
func (d *Dispatcher) trackRepeat(next *Instruction) int {
	d.lastMu.Lock()
	if d.lastSent != nil && d.lastSent.Agent == next.Agent && d.lastSent.Task == next.Task {
		d.repeats++
	} else {
		cp := *next
		d.lastSent = &cp
		d.repeats = 1
	}
	n := d.repeats
	d.lastMu.Unlock()

	if n == repeatNotifyAt && d.onRepeat != nil {
		d.onRepeat(next.Agent, next.Task, n)
	}
	return n
}

// ResetRepeat xóa theo dõi lặp. Host gọi khi Resume / Start mới,
// đảm bảo lệnh đầu tiên sau khi khôi phục hoặc tạo mới được phát với ngữ nghĩa "lần thứ 1".
func (d *Dispatcher) ResetRepeat() {
	d.lastMu.Lock()
	defer d.lastMu.Unlock()
	d.lastSent = nil
	d.repeats = 0
}
