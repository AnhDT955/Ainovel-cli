package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestCommitChapterRejectsNonPendingRewrite(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := store.Progress.SetPendingRewrites([]int{2}, "kiểm tra viết lại"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := store.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := store.Drafts.SaveDraft(3, "Đây là nội dung chương sai."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewCommitChapterTool(store)
	args, err := json.Marshal(map[string]any{
		"chapter":         3,
		"summary":         "lưu chương sai",
		"characters":      []string{"nhân vật chính"},
		"key_events":      []string{"lưu nhầm"},
		"timeline_events": []any{},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected commit to be rejected during rewrite flow")
	}

	if _, err := os.Stat(dir + "/chapters/03.md"); !os.IsNotExist(err) {
		t.Fatalf("chapter should not be persisted, stat err=%v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.CompletedChapters) != 1 || progress.CompletedChapters[0] != 2 {
		t.Fatalf("completed chapters should only contain original chapter 2, got %v", progress.CompletedChapters)
	}
	if progress.CurrentChapter != 3 {
		t.Fatalf("current chapter should not advance beyond original progress, got %d", progress.CurrentChapter)
	}
}

func TestCommitChapterAllowsPendingRewrite(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := store.Progress.SetPendingRewrites([]int{2}, "kiểm tra viết lại"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := store.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := store.Drafts.SaveDraft(2, "Đây là nội dung chương đúng đang chờ viết lại."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewCommitChapterTool(store)
	args, err := json.Marshal(map[string]any{
		"chapter":         2,
		"summary":         "lưu chương đúng",
		"characters":      []string{"nhân vật chính"},
		"key_events":      []string{"hoàn thành viết lại"},
		"timeline_events": []any{},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(dir + "/chapters/02.md"); err != nil {
		t.Fatalf("chapter should be persisted: %v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.CompletedChapters) != 1 || progress.CompletedChapters[0] != 2 {
		t.Fatalf("unexpected completed chapters: %v", progress.CompletedChapters)
	}
	pending, err := store.Signals.LoadPendingCommit()
	if err != nil {
		t.Fatalf("LoadPendingCommit: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected pending commit cleared, got %+v", pending)
	}
}

// TestCommitChapterUpdatesCastLedger xác minh: commit_chapter cộng dồn characters của chương vào cast_ledger,
// brief_role do cast_intros cung cấp được sử dụng, và nhân vật cốt lõi trong characters.json không vào ledger.
func TestCommitChapterUpdatesCastLedger(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	// commit_chapter nay chặn cả đề cương rỗng, nên chương được commit phải có mục đề cương.
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Vào trọ", CoreEvent: "Lâm Mặc vào trọ", Hook: "gặp người lạ"},
	}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	// Thiết lập hồ sơ nhân vật cốt lõi (những nhân vật này không được vào cast_ledger)
	if err := s.Characters.Save([]domain.Character{
		{Name: "Lâm Mặc", Role: "nhân vật chính", Tier: "core"},
		{Name: "Lý Thanh Yến", Role: "sư phụ", Tier: "important"},
	}); err != nil {
		t.Fatalf("Save core characters: %v", err)
	}
	if err := s.Drafts.SaveDraft(1, "Nội dung chương một, Lâm Mặc gặp chủ quán trọ Lão Chu và tiểu đồng A Vân."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    1,
		"summary":    "Lâm Mặc vào trọ",
		"characters": []string{"Lâm Mặc", "Lý Thanh Yến", "Lão Chu", "A Vân"},
		"key_events": []string{"vào trọ"},
		"cast_intros": []any{
			map[string]any{"name": "Lão Chu", "brief_role": "chủ quán trọ"},
			map[string]any{"name": "A Vân", "brief_role": "tiểu đồng quán trọ"},
		},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entries, err := s.Cast.Load()
	if err != nil {
		t.Fatalf("Cast.Load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries (Lão Chu/A Vân), got %d: %+v", len(entries), entries)
	}
	byName := map[string]domain.CastEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if e, ok := byName["Lão Chu"]; !ok || e.BriefRole != "chủ quán trọ" || e.FirstSeenChapter != 1 {
		t.Errorf("Lão Chu entry wrong: %+v", e)
	}
	if e, ok := byName["A Vân"]; !ok || e.BriefRole != "tiểu đồng quán trọ" || e.AppearanceCount != 1 {
		t.Errorf("A Vân entry wrong: %+v", e)
	}
	if _, ok := byName["Lâm Mặc"]; ok {
		t.Errorf("nhân vật cốt lõi Lâm Mặc không được vào ledger")
	}
	if _, ok := byName["Lý Thanh Yến"]; ok {
		t.Errorf("nhân vật cốt lõi Lý Thanh Yến không được vào ledger")
	}
}

// TestCommitChapterRejectsPolishWithoutDraftChange xác minh: sau khi chương đã hoàn thành đưa vào hàng đợi chỉnh sửa/viết lại,
// nếu writer bỏ qua draft_chapter và commit thẳng (nội dung drafts và chapters hoàn toàn giống nhau),
// commit_chapter phải từ chối, buộc writer gọi draft_chapter để ghi phiên bản mới trước.
// TestCommitChapterNonLayeredRecompletesAfterRework xác minh sách không phân lớp sau khi hoàn thành rồi reopen để sửa lại,
// commit chương đã sửa xong, khi hàng đợi trống sẽ tự động quay lại trạng thái complete (nhánh không phân lớp kiểm tra hoàn kết sau khi drain).
func TestCommitChapterNonLayeredRecompletesAfterRework(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 300); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// 300 chương hoàn thành và kết thúc. Chương 300 có sẵn drafts/chapters để phục vụ sửa lại.
	ch300 := "Nội dung gốc chương 300, dùng để mô phỏng bản nháp đã lưu chương."
	if err := s.Drafts.SaveDraft(300, ch300); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(300, ch300); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	for ch := 1; ch < 300; ch++ {
		if err := s.Progress.MarkChapterComplete(ch, 100, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete(%d): %v", ch, err)
		}
	}
	if err := s.Progress.MarkChapterComplete(300, len([]rune(ch300)), "", ""); err != nil {
		t.Fatalf("MarkChapterComplete(300): %v", err)
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	// reopen chương 300 → phase về writing, PendingRewrites=[300], flow=rewriting
	if err := s.Progress.Reopen([]int{300}, "sửa lại"); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	// Lưu chương sau khi sửa lại (bản nháp phải khác bản cuối mới được chấp nhận)
	if err := s.Drafts.SaveDraft(300, ch300+"\n\nĐoạn mới thêm sau khi sửa lại."); err != nil {
		t.Fatalf("SaveDraft (reworked): %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    300,
		"summary":    "tóm tắt sau khi sửa lại",
		"characters": []string{"nhân vật chính"},
		"key_events": []string{"dọn dẹp"},
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute rework commit: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["book_complete"] != true {
		t.Errorf("book_complete = %v, want true", payload["book_complete"])
	}

	p, _ := s.Progress.Load()
	if p.Phase != domain.PhaseComplete {
		t.Errorf("phase = %s, want complete (phải tự động hoàn kết lại)", p.Phase)
	}
	if len(p.PendingRewrites) != 0 {
		t.Errorf("PendingRewrites = %v, want empty", p.PendingRewrites)
	}
}

// TestCommitChapterLayeredReopenRecompletesDespiteOpenThread xác minh kết thúc: sách phân lớp sau reopen
// để sửa lại, dù compass vẫn còn luồng dài chưa khép (có thể bị xáo trộn khi sửa), khi hàng đợi trống vẫn
// hoàn kết theo "cấu trúc đầy đủ" mà không bị kẹt ở writing, ngăn vòng lặp vô hạn viết vượt cuối sách
// (§6.5 / họ known_outline_exhaustion).
// Phản chứng: nếu nhánh reopen vẫn dùng layeredBookComplete theo chất lượng, open thread sẽ trả false,
// book_complete sẽ sai và kiểm tra thất bại.
func TestCommitChapterLayeredReopenRecompletesDespiteOpenThread(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Một tập một cung truyện 300 chương, tất cả đã triển khai
	foundation := NewSaveFoundationTool(s)
	var chs []map[string]any
	for ch := 1; ch <= 300; ch++ {
		chs = append(chs, map[string]any{
			"title":      fmt.Sprintf("Chương thứ %d", ch),
			"core_event": "mở đầu",
			"hook":       "tiếp theo",
		})
	}
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "mục tiêu",
				"chapters": chs,
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}

	// Đánh dấu đủ 300 chương hoàn thành để thỏa mãn ràng buộc tối thiểu hoàn kết
	ch2 := "Nội dung gốc chương hai, mô phỏng bản nháp đã lưu chương."
	for ch := 1; ch <= 300; ch++ {
		body := fmt.Sprintf("Nội dung chương %d.", ch)
		if ch == 2 {
			body = ch2
		}
		if err := s.Drafts.SaveDraft(ch, body); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		if err := s.Drafts.SaveFinalChapter(ch, body); err != nil {
			t.Fatalf("SaveFinalChapter %d: %v", ch, err)
		}
		if err := s.Progress.MarkChapterComplete(ch, len([]rune(body)), "", ""); err != nil {
			t.Fatalf("MarkChapterComplete %d: %v", ch, err)
		}
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	// Mô phỏng "sửa lại làm xáo trộn luồng dài": compass vẫn còn open thread chưa khép
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "nhân vật chính về quê", OpenThreads: []string{"kẻ thù chưa trừ"}}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}

	// reopen chương 2 → lưu chương sau khi sửa lại (bản nháp phải khác bản cuối mới được chấp nhận)
	if err := s.Progress.Reopen([]int{2}, "sửa lại"); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if err := s.Drafts.SaveDraft(2, ch2+"\n\nĐoạn mới thêm sau khi sửa lại."); err != nil {
		t.Fatalf("SaveDraft reworked: %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 2, "summary": "tóm tắt sau khi sửa lại", "characters": []string{"nhân vật chính"}, "key_events": []string{"dọn dẹp"},
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute rework commit: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if bc, _ := out["book_complete"].(bool); !bc {
		t.Error("sau khi reopen sửa lại và hàng đợi trống phải hoàn kết theo cấu trúc đầy đủ (dù luồng dài chưa khép)")
	}
	p, _ := s.Progress.Load()
	if p.Phase != domain.PhaseComplete {
		t.Errorf("phase = %s, want complete", p.Phase)
	}
	if p.ReopenedFromComplete {
		t.Error("sau khi hoàn kết lại ReopenedFromComplete phải được xóa")
	}
}

func TestCommitChapterRejectsPolishWithoutDraftChange(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Mô phỏng chương 2 đã hoàn thành bình thường: nội dung drafts và chapters giống nhau.
	original := "Nội dung gốc chương hai, dùng để mô phỏng bản nháp đã lưu chương."
	if err := s.Drafts.SaveDraft(2, original); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, original); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(original)), "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}

	// Vào hàng đợi chỉnh sửa: Flow=Polishing, PendingRewrites=[2]
	if err := s.Progress.SetPendingRewrites([]int{2}, "kiểm tra chỉnh sửa"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}

	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"summary":    "giả vờ đã chỉnh sửa",
		"characters": []string{"nhân vật chính"},
		"key_events": []string{"không có thay đổi"},
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected commit to be rejected when drafts equals final content")
	}

	// Viết một bản nháp khác → phải được chấp nhận
	polished := original + "\n\nĐoạn mới thêm sau khi chỉnh sửa."
	if err := s.Drafts.SaveDraft(2, polished); err != nil {
		t.Fatalf("SaveDraft (polished): %v", err)
	}
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute after real polish: %v", err)
	}
}

// TestCommitChapterLayeredRejectsOutOfRangeChapter xác minh trong chế độ phân lớp,
// commit chương vượt phạm vi layered_outline phải thất bại cứng, không phải chỉ slog.Warn cho qua.
// Đây là phanh vật lý ngăn "writer chạy trần sau khi phán quyết sai" (trường hợp tác phẩm ch204..347).
func TestCommitChapterLayeredRejectsOutOfRangeChapter(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Tạo một layered_outline chỉ có 1 tập 1 cung truyện 1 chương
	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "mục tiêu",
				"chapters": []map[string]any{
					{"title": "Chương đầu", "core_event": "mở đầu", "hook": "tiếp theo"},
				},
			}, scaleFillerArc(2)},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	// commit chương 2 vượt phạm vi phải thất bại cứng
	if err := s.Drafts.SaveDraft(2, "Nội dung chương vượt phạm vi, phải bị chặn."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"summary":    "chương vượt phạm vi",
		"characters": []string{"nhân vật chính"},
		"key_events": []string{"không được phép"},
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected commit to fail when chapter out of layered outline range")
	}

	// File chương không được lưu đĩa, Progress không được tiến
	if _, statErr := os.Stat(dir + "/chapters/02.md"); !os.IsNotExist(statErr) {
		t.Fatalf("chapter 2 should not be persisted, stat err=%v", statErr)
	}
	progress, _ := s.Progress.Load()
	if len(progress.CompletedChapters) != 0 {
		t.Fatalf("CompletedChapters should stay empty, got %v", progress.CompletedChapters)
	}
}

// TestCommitChapterLayeredAutoCompletesWhenDone xác minh cơ chế hoàn kết tất định trong chế độ phân lớp:
// khi đề cương đã triển khai đầy đủ và viết xong + không có cung truyện khung + không có viết lại +
// phục bút hoạt động bằng không + luồng dài trong chỉ nam đã khép,
// commit chương cuối tự động đẩy Phase=Complete mà không cần kiến trúc sư chủ động gọi complete_book.
// Đây là bản vá lỗi livelock do 9bf26a5 xóa cơ chế tự hoàn kết phân lớp (mô hình ở cuối tập cuối
// không append cũng không complete → người viết chạy trần vòng lặp vô hạn vượt phạm vi).
func TestCommitChapterLayeredAutoCompletesWhenDone(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Một tập một cung truyện 300 chương, tất cả đã triển khai (không có cung truyện khung)
	foundation := NewSaveFoundationTool(s)
	var chs []map[string]any
	for ch := 1; ch <= 300; ch++ {
		chs = append(chs, map[string]any{
			"title":      fmt.Sprintf("Chương thứ %d", ch),
			"core_event": "mở đầu",
			"hook":       "tiếp theo",
		})
	}
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "mục tiêu",
				"chapters": chs,
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	// Luồng dài trong chỉ nam đã khép (OpenThreads rỗng)
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "nhân vật chính về quê"}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)
	// Đánh dấu đủ 299 chương trước (chương 300 sẽ được commit trong test)
	for ch := 1; ch <= 299; ch++ {
		if err := s.Drafts.SaveDraft(ch, fmt.Sprintf("Nội dung chương %d.", ch)); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		if err := s.Progress.MarkChapterComplete(ch, 3000, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete %d: %v", ch, err)
		}
	}

	tool := NewCommitChapterTool(s)
	commit := func(ch int) map[string]any {
		if err := s.Drafts.SaveDraft(ch, fmt.Sprintf("Nội dung chương %d, dùng để kiểm tra hoàn kết tất định.", ch)); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		args, _ := json.Marshal(map[string]any{
			"chapter": ch, "summary": "tóm tắt", "characters": []string{"nhân vật chính"}, "key_events": []string{"sự kiện"},
		})
		raw, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute ch%d: %v", ch, err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("Unmarshal ch%d: %v", ch, err)
		}
		return out
	}

	// Chương 299: chưa viết xong, không nên hoàn kết
	if bc, _ := commit(299)["book_complete"].(bool); bc {
		t.Fatal("viết xong chương 299 không nên kích hoạt hoàn kết")
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("viết xong chương 299 phase không nên là complete")
	}

	// Chương 300 (chương cuối + đạt min 300 chương): phải tự động hoàn kết
	if bc, _ := commit(300)["book_complete"].(bool); !bc {
		t.Fatal("viết xong chương cuối phải tự động hoàn kết")
	}
	if p, _ := s.Progress.Load(); p.Phase != domain.PhaseComplete {
		t.Fatalf("expected phase=complete, got %s", p.Phase)
	}
}

// TestCommitChapterLayeredNoAutoCompleteWithOpenThreads xác minh tính thận trọng: khi vẫn còn luồng dài hoạt động
// thì dù viết đủ số chương cũng không tự động hoàn kết, để lại quyền phán quyết "có tiếp tục không" cho kiến trúc sư.
func TestCommitChapterLayeredNoAutoCompleteWithOpenThreads(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "Tập một", "theme": "chủ đề",
			"arcs": []map[string]any{{
				"index": 1, "title": "Cung một", "goal": "mục tiêu",
				"chapters": []map[string]any{{"title": "Chương đầu", "core_event": "mở đầu", "hook": "tiếp theo"}},
			}, scaleFillerArc(2)},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	// Vẫn còn luồng dài hoạt động chưa khép
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "nhân vật chính về quê", OpenThreads: []string{"kẻ thù chưa trừ"}}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	if err := s.Drafts.SaveDraft(1, "Nội dung chương duy nhất, nhưng luồng dài chưa khép."); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 1, "summary": "tóm tắt", "characters": []string{"nhân vật chính"}, "key_events": []string{"sự kiện"},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("không nên tự động hoàn kết khi vẫn còn luồng dài hoạt động chưa khép")
	}
}

// setupFlatBook dựng một cuốn short/mid: đề cương phẳng n chương, không phân lớp.
func setupFlatBook(t *testing.T, n int) *store.Store {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", n); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	var entries []domain.OutlineEntry
	for i := 1; i <= n; i++ {
		entries = append(entries, domain.OutlineEntry{
			Chapter: i, Title: fmt.Sprintf("ch%d", i), CoreEvent: "sự kiện", Hook: "móc",
		})
	}
	if err := s.Outline.SaveOutline(entries); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	return s
}

func commitFlatChapter(t *testing.T, s *store.Store, chapter int) (map[string]any, error) {
	t.Helper()
	if err := s.Drafts.SaveDraft(chapter, fmt.Sprintf("Nội dung chương %d.", chapter)); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	args, _ := json.Marshal(map[string]any{
		"chapter": chapter, "summary": "tóm tắt", "key_events": []string{"sự kiện"},
	})
	raw, err := NewCommitChapterTool(s).Execute(context.Background(), args)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if uerr := json.Unmarshal(raw, &out); uerr != nil {
		t.Fatalf("Unmarshal result: %v", uerr)
	}
	return out, nil
}

// Hồi quy: đề cương phẳng trước đây KHÔNG có bộ chặn vượt biên nào — chương ngoài đề cương được nhận trong
// im lặng, nên sách short/mid cứ phình mãi ngoài kế hoạch mà không ai báo.
func TestCommitChapterRejectsChapterBeyondFlatOutline(t *testing.T) {
	s := setupFlatBook(t, 3)
	for ch := 1; ch <= 3; ch++ {
		if _, err := commitFlatChapter(t, s, ch); err != nil {
			t.Fatalf("commit chương %d: %v", ch, err)
		}
	}

	if _, err := commitFlatChapter(t, s, 4); err == nil {
		t.Fatal("chương 4 nằm ngoài đề cương 3 chương, phải bị từ chối")
	}
}

// Hồi quy sự cố 2026-07-16: đề cương RỖNG hoàn toàn (layered_outline chưa từng lưu được → Layered=false,
// outline phẳng 0 chương) từng được cho qua trong im lặng vì bộ chặn chỉ kích hoạt khi len(entries) > 0 —
// 6 chương/88k từ được commit không có bất kỳ kế hoạch nào. Đề cương rỗng giờ phải chặn cứng.
func TestCommitChapterRejectsEmptyFlatOutline(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	// Mô phỏng trạng thái sự cố: phase đã bị đẩy sang writing nhưng không có file đề cương nào trên đĩa.
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	if _, err := commitFlatChapter(t, s, 1); err == nil {
		t.Fatal("commit với đề cương rỗng phải bị từ chối")
	}
	if s.Progress.IsChapterCompleted(1) {
		t.Fatal("chương không được đánh dấu hoàn thành khi đề cương rỗng")
	}
}

// Hồi quy: sàn 300 chương từng áp cho cả sách phẳng, nên một cuốn short không bao giờ hoàn tất được —
// cộng với bộ chặn vượt biên ở trên thì Router phái viết mãi một chương không thể lưu.
func TestFlatBookCompletesAtOutlineLength(t *testing.T) {
	s := setupFlatBook(t, 3)
	for ch := 1; ch <= 2; ch++ {
		out, err := commitFlatChapter(t, s, ch)
		if err != nil {
			t.Fatalf("commit chương %d: %v", ch, err)
		}
		if out["book_complete"] == true {
			t.Fatalf("chương %d đã báo hoàn tất trong khi đề cương có 3 chương", ch)
		}
	}

	out, err := commitFlatChapter(t, s, 3)
	if err != nil {
		t.Fatalf("commit chương 3: %v", err)
	}
	if out["book_complete"] != true {
		t.Fatalf("viết đủ 3/3 chương phải hoàn tất, nhận: %v", out)
	}
	p, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("Load progress: %v", err)
	}
	if p.Phase != domain.PhaseComplete {
		t.Fatalf("phase = %s, mong đợi %s", p.Phase, domain.PhaseComplete)
	}
}

// scaleFillerArc trả về một cung khung đủ lớn để kế hoạch vượt ngưỡng tối thiểu của validatePlannedScale.
//
// Các fixture layered dưới đây chỉ cần một đề cương hợp lệ chứ không quan tâm quy mô, nhưng save_foundation
// nay từ chối mọi kế hoạch ban đầu dưới 300 chương (xem validatePlannedScale). Cung khung KHÔNG chiếm số
// chương toàn cục — FlattenOutline chỉ đếm qua cung đã mở rộng — nên nó không làm lệch các assertion về
// phạm vi chương, và phải xếp SAU cung đã mở rộng để không vi phạm bất biến đề cương.
func scaleFillerArc(idx int) map[string]any {
	return map[string]any{
		"index": idx, "title": "Cung khung", "goal": "chờ mở rộng", "estimated_chapters": 400,
	}
}
