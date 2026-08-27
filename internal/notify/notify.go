// Package notify cung cấp kênh cảnh báo không cần người trực.
//
// Định vị kiến trúc (architecture.md §2.3): hành động thuần quan sát —
// cảnh báo không bao giờ can thiệp luồng điều khiển
// (không thử lại, không điều phối lại, không dừng hệ thống),
// chỉ "la to" các sự kiện đã có trong TUI ra ngoài màn hình.
// Send thực thi bất đồng bộ, không bao giờ chặn Host, thất bại chỉ ghi slog.
package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Notification chứa toàn bộ thông tin của một cảnh báo.
type Notification struct {
	Kind  string `json:"kind"`  // run_end / repeat / budget
	Level string `json:"level"` // info / warn / error
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Notifier phân phối thông báo theo cấu hình. Giá trị zero không dùng được,
// phải tạo qua New; an toàn với nil (Send là noop).
type Notifier struct {
	command string          // khi không rỗng thay thế kênh system (push điện thoại đi qua đây)
	events  map[string]bool // nil = cho qua tất cả kind
	timeout time.Duration
}

// New tạo Notifier. Nếu command rỗng thì dùng kênh system tích hợp (macOS osascript /
// Linux notify-send; không tìm thấy lệnh thì giảm cấp im lặng thành chỉ ghi slog);
// nếu events không rỗng thì chỉ cho qua các kind được liệt kê.
func New(command string, events []string) *Notifier {
	n := &Notifier{command: strings.TrimSpace(command), timeout: 10 * time.Second}
	if len(events) > 0 {
		n.events = make(map[string]bool, len(events))
		for _, ev := range events {
			n.events[ev] = true
		}
	}
	return n
}

// Send gửi một thông báo bất đồng bộ. Lọc, thực thi, xử lý thất bại đều không ảnh hưởng bên gọi.
func (n *Notifier) Send(nt Notification) {
	if !n.allows(nt.Kind) {
		return
	}
	go n.deliver(nt)
}

// allows trả về liệu kind có được cho qua không (Notifier nil / không có trong events thì chặn).
func (n *Notifier) allows(kind string) bool {
	if n == nil {
		return false
	}
	return n.events == nil || n.events[kind]
}

// deliver thực thi một lần gửi đồng bộ (chạy bên trong goroutine; test có thể gọi trực tiếp để assert đồng bộ).
func (n *Notifier) deliver(nt Notification) {
	defer func() { recover() }()
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()

	var err error
	if n.command != "" {
		err = runCommand(ctx, n.command, nt)
	} else {
		err = runSystem(ctx, nt)
	}
	if err != nil {
		slog.Warn("Gửi thông báo thất bại", "module", "notify", "kind", nt.Kind, "err", err)
	}
}

// runCommand thực thi lệnh do người dùng cấu hình: các trường được truyền qua biến môi trường
// (một dòng curl không cần phụ thuộc ngoài, không có rủi ro injection),
// JSON đầy đủ đồng thời ghi vào stdin (tình huống phân phối phức tạp tự phân tích).
// Timeout được ctx cưỡng chế dừng.
func runCommand(ctx context.Context, command string, nt Notification) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(),
		"NOTIFY_KIND="+nt.Kind,
		"NOTIFY_LEVEL="+nt.Level,
		"NOTIFY_TITLE="+nt.Title,
		"NOTIFY_BODY="+nt.Body,
	)
	payload, _ := json.Marshal(nt)
	cmd.Stdin = strings.NewReader(string(payload))
	return cmd.Run()
}

// runSystem thông báo desktop tích hợp: chỉ áp dụng cho tình huống "người ngồi trước máy tính",
// không tìm thấy lệnh thì giảm cấp im lặng.
func runSystem(ctx context.Context, nt Notification) error {
	switch runtime.GOOS {
	case "darwin":
		script := "display notification " + appleScriptString(nt.Body) + " with title " + appleScriptString(nt.Title)
		return exec.CommandContext(ctx, "osascript", "-e", script).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			slog.Info("Thông báo giảm cấp thành log (không có notify-send)", "module", "notify", "title", nt.Title, "body", nt.Body)
			return nil
		}
		return exec.CommandContext(ctx, "notify-send", nt.Title, nt.Body).Run()
	default:
		slog.Info("Thông báo giảm cấp thành log (nền tảng không có kênh system)", "module", "notify", "title", nt.Title, "body", nt.Body)
		return nil
	}
}

// appleScriptString bọc văn bản tùy ý thành chuỗi literal của AppleScript.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
