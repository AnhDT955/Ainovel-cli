package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestDraftChapterRejectsUnfinishedPendingRewrite(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 80); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	for ch := 1; ch <= 58; ch++ {
		if err := s.Progress.MarkChapterComplete(ch, 3000, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete(%d): %v", ch, err)
		}
	}

	p, _ := s.Progress.Load()
	p.Flow = domain.FlowPolishing
	p.PendingRewrites = []int{65}
	if err := s.Progress.Save(p); err != nil {
		t.Fatalf("Save corrupt progress: %v", err)
	}

	tool := NewDraftChapterTool(s)
	args, err := json.Marshal(map[string]any{
		"chapter": 65,
		"content": "错误写入未来章节。",
		"mode":    "write",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "pending_rewrites chỉ được chứa các chương đã hoàn thành") {
		t.Fatalf("expected invalid pending_rewrites rejection, got %v", err)
	}
	progress, _ := s.Progress.Load()
	if progress.InProgressChapter == 65 {
		t.Fatalf("future chapter should not become in progress")
	}
}

// draft_chapter phải từ chối chương không có trong đề cương — kể cả khi đề cương rỗng hoàn toàn.
// Tái hiện sự cố 2026-07-16: plan_chapter chặn đúng nhưng Writer bỏ qua và gọi thẳng draft_chapter
// (khi đó chưa có guard), StartChapter đẩy phase sang writing và 6 chương được viết không kế hoạch.
func TestDraftChapterRejectsChapterWithoutOutline(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("Progress.Init: %v", err)
	}
	// Mô phỏng trạng thái sự cố: premise đã lưu nhưng đề cương chưa từng được lưu thành công.
	_ = s.Progress.UpdatePhase(domain.PhasePremise)

	tool := NewDraftChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 1,
		"content": "Nội dung chương bịa ngoài kế hoạch.",
		"mode":    "write",
	})
	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "chưa có trong đề cương") {
		t.Fatalf("expected outline guard rejection, got %v", err)
	}

	// Guard phải chặn TRƯỚC StartChapter: phase không được bị đẩy sang writing, bản nháp không được ghi.
	p, _ := s.Progress.Load()
	if p.Phase == domain.PhaseWriting {
		t.Fatal("phase must not be promoted to writing by a rejected draft")
	}
	if content, _ := s.Drafts.LoadDraft(1); content != "" {
		t.Fatal("draft must not be written when chapter is outside the outline")
	}

	// Có đề cương nhưng chương nằm ngoài phạm vi → cũng phải chặn.
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "起头", CoreEvent: "建立处境", Hook: "发现线索"},
	}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	args, _ = json.Marshal(map[string]any{
		"chapter": 2,
		"content": "Chương vượt phạm vi đề cương.",
		"mode":    "write",
	})
	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "chưa có trong đề cương") {
		t.Fatalf("expected out-of-outline rejection, got %v", err)
	}
}
