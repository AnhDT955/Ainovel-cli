package notify

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAllowsFilter(t *testing.T) {
	if New("", nil).allows("repeat") != true {
		t.Error("events mặc định nên cho qua tất cả")
	}
	n := New("", []string{"run_end", "budget"})
	if !n.allows("run_end") || !n.allows("budget") {
		t.Error("kind được liệt kê nên được cho qua")
	}
	if n.allows("repeat") {
		t.Error("kind không được liệt kê nên bị chặn")
	}
	var nilN *Notifier
	if nilN.allows("run_end") {
		t.Error("nil Notifier nên chặn tất cả")
	}
	nilN.Send(Notification{Kind: "run_end"}) // không nên panic
}

func TestCommandChannelEnvAndStdin(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found, skipping on Windows")
	}
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	jsonFile := filepath.Join(dir, "stdin.json")

	n := New(`echo "$NOTIFY_KIND|$NOTIFY_LEVEL|$NOTIFY_TITLE|$NOTIFY_BODY" > `+envFile+` && cat > `+jsonFile, nil)
	nt := Notification{Kind: "budget", Level: "warn", Title: "ainovel: ngân sách", Body: "đã chi $8.00"}
	n.deliver(nt) // gọi đồng bộ để kiểm tra kết quả

	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("command chưa được thực thi: %v", err)
	}
	if got := strings.TrimSpace(string(env)); got != "budget|warn|ainovel: ngân sách|đã chi $8.00" {
		t.Errorf("biến môi trường truyền không khớp: %q", got)
	}

	raw, err := os.ReadFile(jsonFile)
	if err != nil {
		t.Fatalf("stdin chưa được truyền: %v", err)
	}
	var decoded Notification
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("stdin không phải JSON hợp lệ: %v", err)
	}
	if decoded != nt {
		t.Errorf("stdin JSON không khớp: %+v", decoded)
	}
}

func TestCommandChannelTimeoutKill(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found, skipping on Windows")
	}
	n := New("sleep 30", nil)
	n.timeout = 200 * time.Millisecond

	start := time.Now()
	n.deliver(Notification{Kind: "run_end"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hết thời gian chờ nhưng chưa buộc dừng, bị chặn %v", elapsed)
	}
}
