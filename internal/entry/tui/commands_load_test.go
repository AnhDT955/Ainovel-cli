package tui

import (
	"path/filepath"
	"strings"
	"testing"
)

// splitArgsLikeCLI mô phỏng cách parseSlashCommand tách đối số theo khoảng trắng,
// để test phản ánh đúng những gì Run của /load thực sự nhận được.
func splitArgsLikeCLI(rest string) []string {
	return strings.Fields(rest)
}

func TestParseLoadPath_WithSpacesAndQuotes(t *testing.T) {
	want := filepath.Join("D:", "AnhDT", "Git", "Ainovel", "yeu cau sang tac.md")

	// Người dùng gõ: /load "…/yeu cau sang tac.md" — path bị tách theo khoảng trắng.
	args := splitArgsLikeCLI("\"" + want + "\"")
	if len(args) < 2 {
		t.Fatalf("kỳ vọng path bị tách thành nhiều phần, nhận %d phần", len(args))
	}

	got, err := parseLoadPath(args)
	if err != nil {
		t.Fatalf("parseLoadPath lỗi bất ngờ: %v", err)
	}
	if got != want {
		t.Fatalf("path khôi phục sai:\n got %q\nwant %q", got, want)
	}
}

func TestParseLoadPath_SinglePlainArg(t *testing.T) {
	got, err := parseLoadPath([]string{"truyen1.md"})
	if err != nil {
		t.Fatalf("lỗi bất ngờ: %v", err)
	}
	if got != "truyen1.md" {
		t.Fatalf("kỳ vọng truyen1.md, nhận %q", got)
	}
}

func TestParseLoadPath_MissingPath(t *testing.T) {
	if _, err := parseLoadPath(nil); err == nil {
		t.Fatal("kỳ vọng lỗi khi thiếu đường dẫn, nhận nil")
	}
	if _, err := parseLoadPath([]string{"   "}); err == nil {
		t.Fatal("kỳ vọng lỗi khi đường dẫn chỉ có khoảng trắng, nhận nil")
	}
}
