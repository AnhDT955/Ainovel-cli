package diag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/store"
)

// sentinel là một đoạn "nội dung tiểu thuyết" tuyệt đối không được xuất hiện trong bản xuất.
const sentinel = "雪夜里主角揭穿了反派的惊天阴谋这是机密正文"

// writeSession ghi một số tin nhắn theo định dạng sessions/*.jsonl vào thư mục output tạm thời.
func writeSession(t *testing.T, rel string, msgs []agentcore.Message) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "meta", "sessions", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var b strings.Builder
	for _, m := range msgs {
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func commitCall(chapterRaw string) agentcore.Message {
	args := json.RawMessage(`{"chapter":` + chapterRaw + `,"content":"` + sentinel + sentinel + `"}`)
	return agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.ToolCallBlock(agentcore.ToolCall{Name: "commit_chapter", Args: args})},
	}
}

func errResult(msg string) agentcore.Message {
	return agentcore.Message{
		Role:     agentcore.RoleTool,
		Content:  []agentcore.ContentBlock{agentcore.TextBlock(msg)},
		Metadata: map[string]any{"is_error": true},
	}
}

// TestExport_DeathLoopShape tái hiện end-to-end issue #34: model string hóa chapter
// trong commit_chapter dẫn đến vòng lặp validation. Kiểm tra rằng bản xuất định vị được lỗi và nội dung tiểu thuyết không bị lộ.
func TestExport_DeathLoopShape(t *testing.T) {
	var msgs []agentcore.Message
	// Một đoạn nội dung coordinator thuần túy (<4KB, bỏ qua session_compact), phải được ẩn.
	msgs = append(msgs, agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	// 14 lượt commit_chapter(chapter:"7") + InputValidationError.
	for range 14 {
		msgs = append(msgs, commitCall(`"7"`))
		msgs = append(msgs, errResult("InputValidationError: chapter must be int"))
	}

	dir := writeSession(t, "coordinator.jsonl", msgs)
	s := store.NewStore(dir)
	rep, rc := Diagnose(s)
	out := string(RenderExport(rep, rc))

	if strings.Contains(out, sentinel) {
		t.Fatalf("Nội dung tiểu thuyết bị lộ! Bản xuất chứa sentinel:\n%s", out)
	}
	if !strings.Contains(out, `chapter: "7"`) {
		t.Errorf("Thiếu tín hiệu lỗi kiểu dữ liệu chapter: \"7\" (nguyên nhân gốc #34)\n%s", out)
	}
	if !strings.Contains(out, "InputValidationError") {
		t.Errorf("Chuỗi lỗi chưa được giữ lại\n%s", out)
	}
	if !strings.Contains(out, "×14") {
		t.Errorf("Tổng hợp lặp chưa liệt kê ×14\n%s", out)
	}
	// Phase 2: kiểm tra runtime phải phân loại vòng lặp này là RepeatedToolError mức critical.
	if !strings.Contains(out, "Công cụ lặp lại cùng một lỗi") {
		t.Errorf("Kiểm tra runtime chưa tạo ra RepeatedToolError\n%s", out)
	}
	if !strings.Contains(out, "[critical]") {
		t.Errorf("14 lần lặp phải được nâng lên mức critical\n%s", out)
	}
}

// TestExport_NumberVsStringArg chứng minh rằng projection phân biệt được kiểu scalar và string:
// chapter:7 (số) được giữ là 7, chapter:"7" (string) được giữ là "7".
func TestExport_NumberVsStringArg(t *testing.T) {
	intDir := writeSession(t, "coordinator.jsonl", []agentcore.Message{commitCall(`7`)})
	si := store.NewStore(intDir)
	repInt, rcInt := Diagnose(si)
	outInt := string(RenderExport(repInt, rcInt))
	if !strings.Contains(outInt, "chapter: 7") || strings.Contains(outInt, `chapter: "7"`) {
		t.Errorf("Tham số số phải render thành chapter: 7 (không có dấu ngoặc kép)\n%s", outInt)
	}
}

// TestProjectValue_ProseArgRedacted bảo vệ ranh giới ẩn danh: giá trị ngắn dạng định danh được giữ,
// giá trị ngắn chứa tiếng Trung/khoảng trắng (như dispatch task, chapter title) đều bị ẩn.
func TestProjectValue_ProseArgRedacted(t *testing.T) {
	keep := map[string]string{
		`"7"`:       `"7"`,       // số bị string hóa (tín hiệu #34)
		`"premise"`: `"premise"`, // enum
		`"writer"`:  `"writer"`,  // tên vai trò
		`7`:         `7`,         // scalar số
		`true`:      `true`,      // scalar bool
	}
	for in, want := range keep {
		if got := projectValue([]byte(in)); got != want {
			t.Errorf("Phải giữ %s: got %q want %q", in, got, want)
		}
	}
	// Chứa tiếng Trung / khoảng trắng → phải ẩn, và không được chứa nội dung gốc.
	prose := []string{`"第7章 雪夜的真相"`, `"雪夜杀机"`, `"主角揭穿阴谋"`}
	for _, in := range prose {
		got := projectValue([]byte(in))
		if !strings.HasPrefix(got, "<redacted") {
			t.Errorf("Giá trị ngắn tiếng Trung/có khoảng trắng phải được ẩn: %s → %q", in, got)
		}
		if strings.Contains(got, "雪夜") || strings.Contains(got, "主角") {
			t.Errorf("Sau khi ẩn vẫn còn nội dung gốc: %s → %q", in, got)
		}
	}
}

// TestWriteExport_WritesFile chứng minh đường dẫn hàm thuần túy: không phụ thuộc TUI, ghi ra đường dẫn tương đối cố định.
func TestWriteExport_WritesFile(t *testing.T) {
	dir := writeSession(t, "coordinator.jsonl", []agentcore.Message{commitCall(`"7"`), errResult("boom")})
	s := store.NewStore(dir)

	rep, rc := Diagnose(s)
	path, err := WriteExport(s, rep, rc)
	if err != nil {
		t.Fatalf("WriteExport: %v", err)
	}
	if want := filepath.Join(dir, filepath.FromSlash(ExportRelPath)); path != want {
		t.Errorf("Đường dẫn sai: got %s want %s", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "diag-export") {
		t.Errorf("Nội dung file bất thường\n%s", data)
	}
	if strings.Contains(string(data), sentinel) {
		t.Errorf("File xuất ra chứa nội dung tiểu thuyết")
	}
}

// TestRedactMessage_DupSha chứng minh rằng cùng một đoạn văn xuất hiện lặp lại sẽ tạo ra cùng sha (tín hiệu vòng lặp).
func TestRedactMessage_DupSha(t *testing.T) {
	a := redactMessage("coordinator", agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	b := redactMessage("coordinator", agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	if a.TextSha == "" || a.TextSha != b.TextSha {
		t.Errorf("Cùng nội dung phải cho cùng sha: %q vs %q", a.TextSha, b.TextSha)
	}
	if a.Redacted != 1 {
		t.Errorf("Phải ẩn 1 khối văn bản, got %d", a.Redacted)
	}
}
