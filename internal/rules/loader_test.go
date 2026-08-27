package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoad_ThreeLayers kiểm tra Default + Global + Project ba lớp được sắp xếp đúng thứ tự tăng dần.
func TestLoad_ThreeLayers(t *testing.T) {
	rulesFS := fstest.MapFS{
		"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")},
	}
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "rules")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(tmp, "project-rules")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "global.md"), []byte("---\nforbidden_chars:\n  - \"——\"\n---\n# Tùy chọn toàn cục\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.md"), []byte("---\nchapter_words: 4000-8000\n---\n# Tùy chọn dự án\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{
		RulesFS:         rulesFS,
		HomeRulesDir:    globalDir,
		ProjectRulesDir: projectDir,
	})

	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %+v", len(layers), layers)
	}
	expectKinds := []SourceKind{SourceDefault, SourceGlobal, SourceProject}
	for i, want := range expectKinds {
		if layers[i].Kind != want {
			t.Errorf("layer[%d].Kind=%v, want %v", i, layers[i].Kind, want)
		}
	}
	// Sau khi Merge, chapter_words của project phải thắng
	b := Merge(layers)
	if b.Structured.ChapterWords == nil || b.Structured.ChapterWords.Min != 4000 {
		t.Errorf("project chapter_words should win, got %+v", b.Structured.ChapterWords)
	}
	// forbidden_chars do global đóng góp được giữ lại khi project chưa khai báo
	if len(b.Structured.ForbiddenChars) != 1 || b.Structured.ForbiddenChars[0] != "——" {
		t.Errorf("global forbidden_chars should propagate, got %v", b.Structured.ForbiddenChars)
	}
	if !strings.Contains(b.Preferences, "Tùy chọn toàn cục") || !strings.Contains(b.Preferences, "Tùy chọn dự án") {
		t.Errorf("merged preferences missing body: %q", b.Preferences)
	}
}

func TestLoad_GenreFieldIsPassThrough(t *testing.T) {
	// Phase 1.1: genre chỉ được truyền thẳng như một trường, không còn kích hoạt tải assets/rules/genres/.
	// Dù fs có chứa genres/xianxia.md cũng không được đọc ra.
	rulesFS := fstest.MapFS{
		"default.md":        {Data: []byte("")},
		"genres/xianxia.md": {Data: []byte("---\nforbidden_chars:\n  - \"——\"\n---\n")},
	}
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project-rules")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.md"), []byte("---\ngenre: xianxia\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{
		RulesFS:         rulesFS,
		ProjectRulesDir: projectDir,
	})

	// Chỉ kỳ vọng default + project, không có lớp genre
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers (no genre loading), got %d: %+v", len(layers), layers)
	}
	b := Merge(layers)
	if b.Structured.Genre != "xianxia" {
		t.Errorf("genre field should be passed through, got %q", b.Structured.Genre)
	}
	// File genre chưa được tải → không nên có "——" từ file thể loại
	if len(b.Structured.ForbiddenChars) != 0 {
		t.Errorf("genres/*.md must not be auto-loaded in Phase 1.1, got %v", b.Structured.ForbiddenChars)
	}
}

func TestLoad_NilFSDoesNotPanic(t *testing.T) {
	// Tham số đầu vào toàn rỗng: không được panic, trả về layers rỗng
	layers := Load(LoadOptions{})
	if len(layers) != 0 {
		t.Errorf("expected 0 layers, got %d", len(layers))
	}
}

func TestLoad_OnlyDefault(t *testing.T) {
	// Chỉ có quy tắc mặc định nội bộ dự án, hai file người dùng đều thiếu
	rulesFS := fstest.MapFS{
		"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")},
	}
	layers := Load(LoadOptions{RulesFS: rulesFS})
	if len(layers) != 1 || layers[0].Kind != SourceDefault {
		t.Errorf("expected only default layer, got %+v", layers)
	}
}

// TestLoad_GlobalDirScansAllMarkdown kiểm tra nhiều file .md trong thư mục global đều được tải,
// gộp theo thứ tự tên file từ điển (file sau ghi đè file trước), file không phải .md bị bỏ qua.
func TestLoad_GlobalDirScansAllMarkdown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nchapter_words: 1000-2000\n---\n# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nchapter_words: 3000-4000\n---\n# B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// File không phải .md phải bị bỏ qua
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("not a rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{HomeRulesDir: dir})
	if len(layers) != 2 {
		t.Fatalf("expected 2 global layers (a.md, b.md), got %d", len(layers))
	}
	for _, p := range layers {
		if p.Kind != SourceGlobal {
			t.Errorf("dir files should be SourceGlobal, got %v", p.Kind)
		}
	}
	// Thứ tự từ điển: a trước b sau, sau khi gộp b ghi đè a
	b := Merge(layers)
	if b.Structured.ChapterWords == nil || b.Structured.ChapterWords.Min != 3000 {
		t.Errorf("later file (b.md) should win on chapter_words, got %+v", b.Structured.ChapterWords)
	}
	if !strings.Contains(b.Preferences, "# A") || !strings.Contains(b.Preferences, "# B") {
		t.Errorf("both files' preferences should be merged, got %q", b.Preferences)
	}
}

// TestLoad_GlobalDirMissing kiểm tra khi thư mục global không tồn tại thì bỏ qua lặng lẽ.
func TestLoad_GlobalDirMissing(t *testing.T) {
	layers := Load(LoadOptions{HomeRulesDir: filepath.Join(t.TempDir(), "does-not-exist")})
	if len(layers) != 0 {
		t.Errorf("missing global dir should yield 0 layers, got %d", len(layers))
	}
}

// TestLoad_GlobalDirIgnoresHiddenAndSubdirs khóa cứng: file ẩn/file tạm của editor (bắt đầu bằng .)
// bị bỏ qua, thư mục con không được duyệt đệ quy —
// ngăn nội dung nhị phân bẩn bị tiêm vào phần thân tùy chọn LLM.
func TestLoad_GlobalDirIgnoresHiddenAndSubdirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.md"), []byte("---\nchapter_words: 3000-6000\n---\n# real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// AppleDouble trên macOS / khóa emacs / file ẩn thông thường — đều phải bị bỏ qua
	for _, dirty := range []string{"._real.md", ".#lock.md", ".hidden.md"} {
		if err := os.WriteFile(filepath.Join(dir, dirty), []byte("\x00binary garbage\x00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// File .md trong thư mục con không được duyệt đệ quy
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.md"), []byte("---\nchapter_words: 1-2\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{HomeRulesDir: dir})
	if len(layers) != 1 {
		t.Fatalf("expected only real.md loaded (hidden/dirty/subdir ignored), got %d layers", len(layers))
	}
	if layers[0].Source != filepath.Join(dir, "real.md") {
		t.Errorf("loaded wrong file: %s", layers[0].Source)
	}
	if b := Merge(layers); strings.Contains(b.Preferences, "garbage") || strings.Contains(b.Preferences, "\x00") {
		t.Errorf("dirty file content leaked into preferences: %q", b.Preferences)
	}
}

// TestLoad_GlobalDirIsFileExposesConflict kiểm tra khi đường dẫn rules bị tạo nhầm thành file (không phải thư mục)
// thì phải lộ conflict thay vì nuốt lỗi lặng lẽ — nhất quán với hợp đồng dung sai lỗi IO đơn file.
func TestLoad_GlobalDirIsFileExposesConflict(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rules")
	if err := os.WriteFile(p, []byte("oops, should be a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	layers := Load(LoadOptions{HomeRulesDir: p})
	if len(layers) != 1 || len(layers[0].Conflicts) == 0 {
		t.Fatalf("expected 1 layer carrying a conflict, got %+v", layers)
	}
	if layers[0].Conflicts[0].Kind != ConflictParseError {
		t.Errorf("expected ConflictParseError, got %v", layers[0].Conflicts[0].Kind)
	}
}

// TestEnsureRulesDirAt kiểm tra việc chuẩn bị thư mục + README.txt: ghi hướng dẫn, luôn ghi đè bằng template mới nhất,
// và README.txt (không phải .md) không bị loader xử lý như một quy tắc.
func TestEnsureRulesDirAt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := ensureRulesDirAt(dir); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(dir, "README.txt")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("README.txt should be written: %v", err)
	}
	if !strings.Contains(string(data), "front matter") {
		t.Errorf("README.txt missing guidance, got %q", data)
	}

	// Luôn ghi đè bằng template mới nhất: nội dung cũ (ví dụ đường dẫn còn là ./rules.md) sẽ được làm mới khi ensure lại
	if err := os.WriteFile(readme, []byte("Nội dung cũ ./rules.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureRulesDirAt(dir); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(readme); string(again) != homeRulesReadme {
		t.Errorf("README.txt should be refreshed to latest template, got %q", again)
	}

	// README.txt không được xem là quy tắc (loader chỉ quét .md)
	if layers := Load(LoadOptions{HomeRulesDir: dir}); len(layers) != 0 {
		t.Errorf("README.txt must not be loaded as a rule, got %d layers", len(layers))
	}
}

// TestDefaultProjectRulesDir khóa cứng thư mục quy tắc cấp dự án phản chiếu toàn cục: ./.ainovel/rules/.
func TestDefaultProjectRulesDir(t *testing.T) {
	proj := filepath.Join("/tmp", "demo-book")
	want := filepath.Join(proj, ".ainovel", "rules")
	if got := DefaultProjectRulesDir(proj); got != want {
		t.Errorf("DefaultProjectRulesDir=%q, want %q", got, want)
	}
	if got := DefaultProjectRulesDir(""); got != "" {
		t.Errorf("Thư mục gốc dự án rỗng phải trả về chuỗi rỗng, nhận được %q", got)
	}
}

// TestDefaultOptions_LoadsProjectRulesFromDotAinovel kiểm tra end-to-end:
// DefaultOptions nạp ./.ainovel/rules/ trong cwd vào nguồn SourceProject.
func TestDefaultOptions_LoadsProjectRulesFromDotAinovel(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	rulesDir := filepath.Join(proj, ".ainovel", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "book.md"), []byte("---\nchapter_words: 4000-8000\n---\n# Tùy chọn cuốn sách này\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rulesFS := fstest.MapFS{"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")}}
	layers := Load(DefaultOptions(rulesFS))

	var got *Parsed
	for i := range layers {
		if layers[i].Kind == SourceProject {
			got = &layers[i]
		}
	}
	if got == nil {
		t.Fatalf("Phải tải được lớp quy tắc dự án từ ./.ainovel/rules/, nhận được %+v", layers)
	}
	if b := Merge(layers); b.Structured.ChapterWords == nil || b.Structured.ChapterWords.Min != 4000 {
		t.Errorf("Quy tắc dự án phải ghi đè chapter_words mặc định, nhận được %+v", b.Structured.ChapterWords)
	}
}
