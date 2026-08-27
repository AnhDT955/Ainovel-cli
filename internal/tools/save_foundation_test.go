package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestSaveFoundationPersistsPlanningTier(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tool := NewSaveFoundationTool(store)
	args, err := json.Marshal(map[string]any{
		"type":    "premise",
		"content": "# 测试书名\n\n## 题材和基调\n测试",
		"scale":   "long",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	meta, err := store.RunMeta.Load()
	if err != nil {
		t.Fatalf("LoadRunMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected run meta to exist")
	}
	if meta.PlanningTier != domain.PlanningTierLong {
		t.Fatalf("expected planning tier %q, got %q", domain.PlanningTierLong, meta.PlanningTier)
	}
}

func TestSaveFoundationPremiseSetsNovelName(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init("novel", 0); err != nil {
		t.Fatalf("Init progress: %v", err)
	}

	tool := NewSaveFoundationTool(store)
	args, err := json.Marshal(map[string]any{
		"type": "premise",
		"content": `# 长夜燃灯

## 题材和基调
东方玄幻，冷硬求生。`,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if progress == nil {
		t.Fatal("expected progress")
	}
	if progress.NovelName != "长夜燃灯" {
		t.Fatalf("expected novel name set, got %q", progress.NovelName)
	}
}

// Đề cương phẳng và các mức scale ngắn đã bị khai tử: mọi tác phẩm đều là truyện dài phân lớp.
// Test cũ ở đây kiểm tra đường hạ cấp long → mid (lưu đè bằng đề cương phẳng); đường đó nay phải bị chặn,
// nếu không một tác phẩm đang phân lớp có thể bị đá về chế độ phẳng và mất sạch cấu trúc tập/cung.
func TestSaveFoundationRejectsFlatOutlineAndShortScales(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	tool := NewSaveFoundationTool(s)

	layeredArgs, _ := json.Marshal(map[string]any{
		"type":    "layered_outline",
		"content": `[{"index":1,"title":"第一卷","theme":"主题","arcs":[{"index":1,"title":"第一弧","goal":"目标","chapters":[{"chapter":1,"title":"第一章","core_event":"开局","hook":"继续"}]},{"index":2,"title":"骨架弧","goal":"待展开","estimated_chapters":400}]}]`,
		"scale":   "long",
	})
	if _, err := tool.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered outline: %v", err)
	}

	cases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "đề cương phẳng bị từ chối",
			args: map[string]any{
				"type":    "outline",
				"content": `[{"chapter":1,"title":"第一章","core_event":"改为中篇","hook":"继续"}]`,
				"scale":   "long",
			},
		},
		{
			name: "scale mid bị từ chối",
			args: map[string]any{"type": "premise", "content": "# Sách\n\nNội dung tiền đề.", "scale": "mid"},
		},
		{
			name: "scale short bị từ chối",
			args: map[string]any{"type": "premise", "content": "# Sách\n\nNội dung tiền đề.", "scale": "short"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.args)
			if _, err := tool.Execute(context.Background(), raw); err == nil {
				t.Fatal("phải bị từ chối")
			} else if !errors.Is(err, errs.ErrToolArgs) {
				t.Fatalf("mong đợi ErrToolArgs, nhận %v", err)
			}
		})
	}

	// Quan trọng nhất: tác phẩm vẫn nguyên trạng phân lớp, không bị lần gọi sai nào đá về chế độ phẳng.
	progress, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if !progress.Layered {
		t.Fatal("tác phẩm bị đá khỏi chế độ phân lớp")
	}
	volumes, err := s.Outline.LoadLayeredOutline()
	if err != nil || len(volumes) != 1 {
		t.Fatalf("đề cương phân lớp phải còn nguyên, nhận %d tập (err=%v)", len(volumes), err)
	}
	if meta, _ := s.RunMeta.Load(); meta == nil || meta.PlanningTier != domain.PlanningTierLong {
		t.Fatalf("mức quy hoạch phải giữ long, nhận %+v", meta)
	}
}

func TestSaveFoundationAppendVolume(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	tool := NewSaveFoundationTool(s)

	// Tạo layered_outline ban đầu (tập 1)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "第一卷", "theme": "起步",
			"arcs": []map[string]any{{
				"index": 1, "title": "首弧", "goal": "目标",
				"chapters": []map[string]any{{"title": "第一章", "core_event": "开局", "hook": "继续"}},
			}, scaleFillerArc(2)},
		}},
		"scale": "long",
	})
	if _, err := tool.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}

	// append_volume: nối thêm tập 2
	appendArgs, _ := json.Marshal(map[string]any{
		"type": "append_volume",
		"content": map[string]any{
			"index": 2, "title": "第二卷", "theme": "升级",
			"arcs": []map[string]any{{
				"index": 1, "title": "弧一", "goal": "目标",
				"chapters": []map[string]any{{"title": "新章", "core_event": "推进", "hook": "钩子"}},
			}},
		},
	})
	res, err := tool.Execute(context.Background(), appendArgs)
	if err != nil {
		t.Fatalf("Execute append_volume: %v", err)
	}
	var result map[string]any
	json.Unmarshal(res, &result)
	if result["volume"] != float64(2) {
		t.Fatalf("expected volume=2, got %v", result["volume"])
	}

	// Xác minh đề cương có 2 tập
	volumes, _ := s.Outline.LoadLayeredOutline()
	if len(volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(volumes))
	}
	if volumes[1].Title != "第二卷" {
		t.Fatalf("expected title '第二卷', got %q", volumes[1].Title)
	}
}

func TestSaveFoundationAppendVolumeValidation(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	tool := NewSaveFoundationTool(s)

	// Tập ban đầu
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "第一卷", "theme": "起步",
			"arcs": []map[string]any{{
				"index": 1, "title": "首弧", "goal": "目标",
				"chapters": []map[string]any{{"title": "第一章", "core_event": "开局", "hook": "继续"}},
			}, scaleFillerArc(2)},
		}},
		"scale": "long",
	})
	tool.Execute(context.Background(), layeredArgs)

	// Index do Kiến trúc sư khai bị bỏ qua: vị trí quyết định số tập, nên tập trùng Index=1 vẫn được nhận
	// và tự đánh số thành 2. Xem store.NormalizeOutlineIndexes.
	appendArgs, _ := json.Marshal(map[string]any{
		"type": "append_volume",
		"content": map[string]any{
			"index": 1, "title": "重复 Index", "theme": "x",
			"arcs": []map[string]any{{
				"index": 1, "title": "弧一", "goal": "目标",
				"chapters": []map[string]any{{"title": "章", "core_event": "事件", "hook": "钩子"}},
			}},
		},
	})
	if _, err := tool.Execute(context.Background(), appendArgs); err != nil {
		t.Fatalf("append_volume: %v", err)
	}
	volumes, err := s.Outline.LoadLayeredOutline()
	if err != nil {
		t.Fatalf("LoadLayeredOutline: %v", err)
	}
	if len(volumes) != 2 || volumes[1].Index != 2 {
		t.Fatalf("volumes = %d, Index tập cuối = %d; mong đợi 2 tập với Index cuối = 2", len(volumes), volumes[len(volumes)-1].Index)
	}

	// Tập rỗng vẫn phải bị từ chối: đó là lỗi cấu trúc thật, không phải chuyện đánh số.
	emptyArgs, _ := json.Marshal(map[string]any{
		"type":    "append_volume",
		"content": map[string]any{"index": 3, "title": "空卷", "theme": "x"},
	})
	if _, err := tool.Execute(context.Background(), emptyArgs); err == nil {
		t.Fatal("expected error when appending volume with no arcs")
	}
}

// TestSaveFoundationAppendVolumeRejectsAfterComplete xác minh rằng append_volume không được phép sau khi Phase=Complete.
// Thay thế ngữ nghĩa cũ "từ chối nối thêm tập Final" (trường Final đã bị xóa).
func TestSaveFoundationAppendVolumeRejectsAfterComplete(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	tool := NewSaveFoundationTool(s)
	appendArgs, _ := json.Marshal(map[string]any{
		"type": "append_volume",
		"content": map[string]any{
			"index": 1, "title": "尝试续写", "theme": "x",
			"arcs": []map[string]any{{
				"index": 1, "title": "弧", "goal": "g",
				"chapters": []map[string]any{{"title": "章", "core_event": "e", "hook": "h"}},
			}},
		},
	})
	if _, err := tool.Execute(context.Background(), appendArgs); err == nil {
		t.Fatal("expected error when appending after Phase=Complete")
	}
}

func TestSaveFoundationUpdateCompass(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tool := NewSaveFoundationTool(s)
	args, _ := json.Marshal(map[string]any{
		"type": "update_compass",
		"content": map[string]any{
			"ending_direction": "主角面对最终抉择",
			"open_threads":     []string{"线索A", "关系B"},
			"estimated_scale":  "预计 4-6 卷",
		},
	})
	_, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute update_compass: %v", err)
	}

	compass, err := s.Outline.LoadCompass()
	if err != nil {
		t.Fatalf("LoadCompass: %v", err)
	}
	if compass == nil || compass.EndingDirection != "主角面对最终抉择" {
		t.Fatalf("unexpected compass: %+v", compass)
	}
	if len(compass.OpenThreads) != 2 {
		t.Fatalf("expected 2 open threads, got %d", len(compass.OpenThreads))
	}
}

func TestSaveFoundationUpdateCompassOverridesLastUpdated(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Save(&domain.Progress{
		NovelName:         "光斑",
		Phase:             domain.PhaseWriting,
		CompletedChapters: []int{1, 2, 3, 5, 4}, // thứ tự lộn xộn, xác minh lấy max chứ không phải len
	}); err != nil {
		t.Fatalf("Save progress: %v", err)
	}

	tool := NewSaveFoundationTool(s)
	args, _ := json.Marshal(map[string]any{
		"type": "update_compass",
		"content": map[string]any{
			"ending_direction": "主角面对最终抉择",
			"open_threads":     []string{"线索A"},
			"last_updated":     0, // LLM thường quên điền hoặc để 0
		},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute update_compass: %v", err)
	}

	compass, err := s.Outline.LoadCompass()
	if err != nil {
		t.Fatalf("LoadCompass: %v", err)
	}
	if compass.LastUpdated != 5 {
		t.Fatalf("expected LastUpdated=5 (max of CompletedChapters), got %d", compass.LastUpdated)
	}
}

func TestSaveFoundationUpdateCompassRequiresDirection(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tool := NewSaveFoundationTool(s)
	args, _ := json.Marshal(map[string]any{
		"type":    "update_compass",
		"content": map[string]any{"estimated_scale": "3 卷"},
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error when ending_direction is empty")
	}
}

// content truyền thẳng mảng JSON (không bọc chuỗi) phải giải mã được — đây là dạng architect-long.md dạy dùng.
func TestSaveFoundationAcceptsDirectJSONArrayContent(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tool := NewSaveFoundationTool(s)
	args, err := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "第一卷", "theme": "主题",
			"arcs": []map[string]any{{
				"index": 1, "title": "第一弧", "goal": "目标",
				"chapters": []map[string]any{{
					"chapter":    1,
					"title":      "第一章",
					"core_event": "主角登场",
					"hook":       "继续",
					"scenes":     []string{"场景一", "场景二"},
				}},
			}, scaleFillerArc(2)},
		}},
		"scale": "long",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	outline, err := s.Outline.LoadOutline()
	if err != nil {
		t.Fatalf("LoadOutline: %v", err)
	}
	if len(outline) != 1 || outline[0].Title != "第一章" {
		t.Fatalf("unexpected outline: %+v", outline)
	}
}

// completeBookSetup tạo một Store tối giản đang ở giai đoạn writing, dùng cho các test complete_book.
// complete_book không kiểm tra tính đầy đủ của các chương trong layered_outline (trách nhiệm phán định thuộc về "danh sách phán định hoàn kết" của LLM),
// tầng công cụ chỉ kiểm tra PendingRewrites rỗng và progress đã được khởi tạo.
// Ràng buộc cứng: tối thiểu 300 chương mới cho phép complete_book, nên setup sẽ đánh dấu đủ 300 chương hoàn thành.
func completeBookSetup(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)
	// Đánh dấu 300 chương hoàn thành để thỏa mãn ràng buộc tối thiểu
	for ch := 1; ch <= 300; ch++ {
		if err := s.Progress.MarkChapterComplete(ch, 3000, "", ""); err != nil {
			t.Fatalf("MarkChapterComplete(%d): %v", ch, err)
		}
	}
	return s
}

func TestSaveFoundationCompleteBookPushesPhaseComplete(t *testing.T) {
	s := completeBookSetup(t)
	tool := NewSaveFoundationTool(s)
	args, _ := json.Marshal(map[string]any{
		"type": "complete_book", "content": map[string]any{},
	})
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute complete_book: %v", err)
	}
	var result map[string]any
	_ = json.Unmarshal(res, &result)
	if result["book_complete"] != true {
		t.Fatalf("expected book_complete=true, got %+v", result)
	}
	if result["phase"] != string(domain.PhaseComplete) {
		t.Fatalf("expected phase=complete, got %v", result["phase"])
	}
	progress, _ := s.Progress.Load()
	if progress.Phase != domain.PhaseComplete {
		t.Fatalf("expected progress.Phase=complete, got %s", progress.Phase)
	}
}

func TestSaveFoundationCompleteBookRejectsBeforeWriting(t *testing.T) {
	// Gọi nhầm complete_book trong giai đoạn lập kế hoạch phải bị từ chối, nếu không sẽ bỏ qua toàn bộ quá trình viết.
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhasePremise)
	_ = s.Progress.UpdatePhase(domain.PhaseOutline)
	tool := NewSaveFoundationTool(s)
	args, _ := json.Marshal(map[string]any{
		"type": "complete_book", "content": map[string]any{},
	})
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected error when phase != writing")
	}
	progress, _ := s.Progress.Load()
	if progress.Phase != domain.PhaseOutline {
		t.Fatalf("phase should remain outline, got %s", progress.Phase)
	}
}

func TestSaveFoundationCompleteBookRejectsWithPendingRewrites(t *testing.T) {
	s := completeBookSetup(t)
	if err := s.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := s.Progress.SetPendingRewrites([]int{2}, "尾章节奏过快"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	tool := NewSaveFoundationTool(s)
	args, _ := json.Marshal(map[string]any{
		"type": "complete_book", "content": map[string]any{},
	})
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected error when PendingRewrites non-empty")
	}
	progress, _ := s.Progress.Load()
	if progress.Phase == domain.PhaseComplete {
		t.Fatalf("phase should not be Complete with PendingRewrites: %s", progress.Phase)
	}
}

// Bộ nền rỗng phải bị từ chối NGAY tại đường ghi, kèm hướng dẫn sửa, thay vì được lưu im lặng rồi
// tự đẩy phase sang writing. Đây là hồi quy cho sự cố 2026-07: 6 world_rules rỗng + 10 nhân vật chỉ
// có tên vai đã lọt qua cổng chỉ-đếm-phần-tử và cả cuốn sách được viết từ khung rỗng đó.
func TestSaveFoundationRejectsEmptySkeleton(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		content any
		// wantHint là đoạn phải xuất hiện trong lỗi để lượt sửa của Kiến trúc sư nhắm đúng chỗ.
		wantHint string
		// wantMissing là mục phải VẪN bị coi là thiếu sau khi từ chối (chứng minh không có dữ liệu rác
		// nào kịp rơi xuống đĩa). Rỗng = bỏ qua kiểm tra này.
		wantMissing string
	}{
		{
			name:        "world_rules toàn chuỗi rỗng",
			typ:         "world_rules",
			content:     []map[string]string{{"category": "", "rule": "", "boundary": ""}, {"category": "", "rule": "", "boundary": ""}},
			wantHint:    "rule rỗng",
			wantMissing: "world_rules",
		},
		{
			name:        "nhân vật chỉ có tên vai trò",
			typ:         "characters",
			content:     []map[string]any{{"name": "Nhân vật chính", "role": "trung tâm", "description": ""}},
			wantHint:    "tên VAI TRÒ",
			wantMissing: "characters",
		},
		{
			name:     "premise chép nguyên nhãn chỗ điền",
			typ:      "premise",
			content:  "# Tên truyện\n\n## Premise\nMột tiểu thuyết dài kỳ.",
			wantHint: "chỗ điền",
		},
		// Run 2026-07-15: nhãn cung truyện bị nhét vào chỗ danh sách chương, core_event rỗng toàn bộ;
		// cổng chỉ-đếm-phần-tử nhận hết → phase sang writing với total_chapters=14. Sự cố đó đến qua
		// type="outline", nay đã bị khai tử — nhưng cùng lỗi ấy vẫn tới được qua layered_outline, nên
		// hình dạng rác được giữ nguyên ở đây trên đường còn sống.
		{
			name: "layered_outline nhét nhãn cung vào chỗ chương, core_event rỗng",
			typ:  "layered_outline",
			content: []map[string]any{{
				"index": 1, "title": "Tập 1", "theme": "Mất chỗ dựa",
				"arcs": []map[string]any{{
					"index": 1, "title": "Cung 1", "goal": "Rời thành",
					"chapters": []map[string]any{
						{"chapter": 1, "title": "Xuyên qua", "core_event": "", "hook": ""},
						{"chapter": 0, "title": "Arc 1: Học Đồ Võ Quán Trong Thành Mưa Máu, chương 1-35", "core_event": "", "hook": ""},
					},
				}},
			}},
			wantHint: "core_event rỗng",
			// layered_outline ghi cả layered_outline.json lẫn outline.json phẳng; bị từ chối thì
			// KHÔNG file nào được tồn tại, nên outline vẫn phải thiếu.
			wantMissing: "outline",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := store.NewStore(t.TempDir())
			if err := st.Init(); err != nil {
				t.Fatalf("Init: %v", err)
			}
			tool := NewSaveFoundationTool(st)
			args, err := json.Marshal(map[string]any{"type": tc.typ, "content": tc.content})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			_, err = tool.Execute(context.Background(), args)
			if err == nil {
				t.Fatal("bộ nền rỗng phải bị từ chối, không được lưu im lặng")
			}
			if !errors.Is(err, errs.ErrToolArgs) {
				t.Errorf("phải phân loại là lỗi đầu vào để Kiến trúc sư thử lại, got: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantHint) {
				t.Errorf("lỗi phải nêu %q để lần thử lại nhắm đúng chỗ, got: %v", tc.wantHint, err)
			}

			// Không được để lại dữ liệu rác trên đĩa sau khi từ chối.
			if tc.wantMissing != "" {
				if missing := st.FoundationMissing(); !slices.Contains(missing, tc.wantMissing) {
					t.Errorf("%s bị từ chối thì %q phải vẫn được coi là thiếu, missing=%v", tc.typ, tc.wantMissing, missing)
				}
			}
		})
	}
}

// Tier long phải đi bằng layered_outline: đề cương phẳng không mang cấu trúc tập/cung nên
// expand_arc và append_volume mất chỗ bám, và truyện "tối thiểu 300 chương" hết đường mở rộng.
// Run 2026-07-15 chốt lại đúng như vậy: outline phẳng 14 mục, total_chapters=14, tier=long.
func TestSaveFoundationRejectsFlatOutlineForLongTier(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	tool := NewSaveFoundationTool(st)

	args, err := json.Marshal(map[string]any{
		"type":    "outline",
		"scale":   "long",
		"content": []map[string]any{{"chapter": 1, "title": "Xuyên qua", "core_event": "Lý Xuyên tỉnh dậy trong thân xác học đồ."}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	_, err = tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("tier long không được nhận đề cương phẳng")
	}
	if !errors.Is(err, errs.ErrToolArgs) {
		t.Errorf("phải là lỗi đầu vào để Kiến trúc sư gọi lại đúng type, got: %v", err)
	}
	if !strings.Contains(err.Error(), "layered_outline") {
		t.Errorf("lỗi phải chỉ ra type đúng cần dùng, got: %v", err)
	}
	if o, _ := st.Outline.LoadOutline(); len(o) != 0 {
		t.Errorf("đề cương phẳng bị từ chối thì không được ghi xuống đĩa, got %d mục", len(o))
	}
}

// Kiến trúc sư thường xuyên quên truyền scale ở các lượt sau. Khi đó tier phải lấy từ RunMeta của
// tác phẩm, nếu không "" sẽ bị hiểu nhầm là không phải truyện dài và đề cương phẳng lại lọt qua.
func TestSaveFoundationRejectsFlatOutlineWhenScaleOmitted(t *testing.T) {
	st := store.NewStore(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := st.RunMeta.SetPlanningTier(domain.PlanningTierLong); err != nil {
		t.Fatalf("SetPlanningTier: %v", err)
	}
	tool := NewSaveFoundationTool(st)

	args, err := json.Marshal(map[string]any{
		"type":    "outline",
		"content": []map[string]any{{"chapter": 1, "title": "Xuyên qua", "core_event": "Lý Xuyên tỉnh dậy trong thân xác học đồ."}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); !errors.Is(err, errs.ErrToolArgs) {
		t.Errorf("tier long đã lưu trong RunMeta phải chặn được đề cương phẳng dù lượt gọi bỏ trống scale, got: %v", err)
	}
}

// Sự cố thật (2026-07-15, 23:25:58): Kiến trúc sư gọi save_foundation(type="characters") với content là
// {"characters": [...]} thay vì mảng trần, và world_rules hỏng y hệt cùng lượt. JSON hợp lệ nên lỗi rơi
// vào nhánh mặc định của decodeFoundationJSON và nhận hint cú pháp ("escape dấu ngoặc kép...") — lạc đề
// hoàn toàn với một lỗi hình dạng, nên lần thử lại phải đoán mò. Test này khoá thông điệp theo LOẠI lỗi.
func TestDecodeFoundationJSONShapeErrors(t *testing.T) {
	// Envelope một khóa từng chỉ được BÁO LỖI cho tử tế; sau 2026-07-16 nó được gỡ thẳng (xem
	// unwrapFoundationEnvelope): ý định không hề mơ hồ, nên bắt sinh lại cả đoạn chỉ đốt thêm một lượt.
	// Thông báo lỗi hình dạng bên dưới vẫn còn nguyên giá trị cho các ca THẬT SỰ mơ hồ.
	t.Run("mảng bị bọc trong object được gỡ thay vì báo lỗi", func(t *testing.T) {
		var chars []domain.Character
		if err := decodeFoundationJSON("characters", `{"characters":[{"name":"Lý Xuyên"}]}`, &chars); err != nil {
			t.Fatalf("envelope một khóa phải được gỡ: %v", err)
		}
		if len(chars) != 1 || chars[0].Name != "Lý Xuyên" {
			t.Fatalf("nội dung phải được giữ nguyên sau khi gỡ: %+v", chars)
		}
	})

	t.Run("envelope nhiều khóa mảng vẫn báo lỗi hình dạng", func(t *testing.T) {
		var chars []domain.Character
		err := decodeFoundationJSON("characters", `{"characters":[{"name":"A"}],"extras":[{"name":"B"}]}`, &chars)
		if err == nil {
			t.Fatal("hai khóa mảng là mơ hồ, phải bị từ chối chứ không được đoán")
		}
		if strings.Contains(err.Error(), "escape") {
			t.Errorf("lỗi hình dạng không được nhận hint cú pháp: %v", err)
		}
		if !strings.Contains(err.Error(), "MẢNG JSON") {
			t.Errorf("phải nói rõ content cần là mảng: %v", err)
		}
	})

	// prompt Kiến trúc sư cảnh báo đúng chế độ hỏng này ("arc: string ... không phải object
	// {start/middle/end}"), nên nó phải được chỉ tên trường thay vì bị bảo sinh lại cả đoạn.
	t.Run("sai kiểu ở trường lồng nhau", func(t *testing.T) {
		var chars []domain.Character
		err := decodeFoundationJSON("characters", `[{"name":"Lý Xuyên","arc":{"start":"a","end":"b"}}]`, &chars)
		if err == nil {
			t.Fatal("arc dạng object phải bị từ chối")
		}
		if !strings.Contains(err.Error(), `"arc"`) {
			t.Errorf("phải chỉ đúng tên trường sai kiểu: %v", err)
		}
	})

	t.Run("lỗi cú pháp thật vẫn giữ hint cú pháp", func(t *testing.T) {
		var chars []domain.Character
		err := decodeFoundationJSON("characters", `[{"name":"Lý Xuyên" "role":"x"}]`, &chars)
		if err == nil {
			t.Fatal("JSON thiếu dấu phẩy phải bị từ chối")
		}
		if !strings.Contains(err.Error(), "escape") {
			t.Errorf("lỗi cú pháp vẫn cần hint cú pháp: %v", err)
		}
	})

	t.Run("mảng hợp lệ vẫn qua", func(t *testing.T) {
		var chars []domain.Character
		if err := decodeFoundationJSON("characters", `[{"name":"Lý Xuyên","description":"phóng viên độc lập"}]`, &chars); err != nil {
			t.Fatalf("content hợp lệ bị từ chối: %v", err)
		}
		if len(chars) != 1 || chars[0].Name != "Lý Xuyên" {
			t.Fatalf("decode sai: %+v", chars)
		}
	})
}

// arcJSON dựng một cung; n>0 → cung đã mở rộng với n chương, n=0 → cung khung với estimated_chapters=est.
func arcJSON(idx int, title string, n, est int) map[string]any {
	a := map[string]any{"index": idx, "title": title, "goal": "mục tiêu " + title}
	if n > 0 {
		var chs []map[string]any
		for i := 1; i <= n; i++ {
			chs = append(chs, map[string]any{
				"title": fmt.Sprintf("%s ch%d", title, i), "core_event": "sự kiện", "hook": "móc",
			})
		}
		a["chapters"] = chs
	} else {
		a["estimated_chapters"] = est
	}
	return a
}

// Tái hiện sự cố 2026-07 ("Loạn Thế Võ Đạo"): brief.md ghi 12 arc / 820 chương kèm phạm vi chương từng arc;
// Kiến trúc sư giữ đúng 12 tên arc rồi nén thành 10/10/10 + 5×9 = 75 chương, mở rộng chi tiết cả 12 cung.
// Kế hoạch đó tự mâu thuẫn — engine từ chối complete_book dưới 300 chương — nhưng trước đây được nhận im lặng.
func TestSaveFoundationRejectsCompressedPlan(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	tool := NewSaveFoundationTool(s)

	var volumes []map[string]any
	counts := [][]int{{10, 10, 10}, {5, 5, 5}, {5, 5, 5}, {5, 5, 5}}
	for vi, arcCounts := range counts {
		var arcs []map[string]any
		for ai, n := range arcCounts {
			arcs = append(arcs, arcJSON(ai+1, fmt.Sprintf("V%dA%d", vi+1, ai+1), n, 0))
		}
		volumes = append(volumes, map[string]any{
			"index": vi + 1, "title": fmt.Sprintf("Tập %d", vi+1), "theme": "chủ đề", "arcs": arcs,
		})
	}
	args, _ := json.Marshal(map[string]any{"type": "layered_outline", "content": volumes, "scale": "long"})

	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("kế hoạch 75 chương phải bị từ chối: engine không cho complete_book dưới 300 chương")
	}
	if !errors.Is(err, errs.ErrToolArgs) {
		t.Fatalf("mong đợi ErrToolArgs để Kiến trúc sư thử lại, nhận %v", err)
	}
	// Lỗi phải chỉ ra CẢ hai vấn đề, vì cách sửa khác nhau.
	if !strings.Contains(err.Error(), "75 chương") || !strings.Contains(err.Error(), "300") {
		t.Errorf("lỗi phải nêu quy mô thực tế và ngưỡng, nhận: %v", err)
	}
	if !strings.Contains(err.Error(), "khung xương") {
		t.Errorf("lỗi phải chỉ ra việc mở rộng chi tiết cả 12 cung, nhận: %v", err)
	}

	// Không được để lại dữ liệu rác: bị từ chối thì đề cương vẫn phải là chưa có.
	if vols, _ := s.Outline.LoadLayeredOutline(); len(vols) != 0 {
		t.Fatalf("đề cương bị từ chối mà vẫn ghi xuống đĩa: %d tập", len(vols))
	}
}

// Hình dạng ĐÚNG theo brief của BOSS: cung đầu mở rộng 35 chương, 11 cung còn lại là khung xương với
// estimated_chapters đúng phạm vi brief → tổng 820 chương.
func TestSaveFoundationAcceptsBriefScalePlan(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	tool := NewSaveFoundationTool(s)

	est := [][]int{{0, 45, 55}, {75, 90, 80}, {90, 90, 80}, {70, 50, 60}}
	var volumes []map[string]any
	for vi, arcEst := range est {
		var arcs []map[string]any
		for ai, e := range arcEst {
			if vi == 0 && ai == 0 {
				arcs = append(arcs, arcJSON(1, "Học Đồ Võ Quán", 35, 0)) // cung đầu: 35 chương chi tiết
				continue
			}
			arcs = append(arcs, arcJSON(ai+1, fmt.Sprintf("V%dA%d", vi+1, ai+1), 0, e))
		}
		volumes = append(volumes, map[string]any{
			"index": vi + 1, "title": fmt.Sprintf("Tập %d", vi+1), "theme": "chủ đề", "arcs": arcs,
		})
	}
	args, _ := json.Marshal(map[string]any{"type": "layered_outline", "content": volumes, "scale": "long"})

	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("kế hoạch 820 chương đúng chuẩn bị từ chối: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["chapters"] != float64(820) {
		t.Fatalf("tổng quy mô = %v, mong đợi 820 (35 chương chi tiết + estimated của 11 cung khung)", out["chapters"])
	}
	p, _ := s.Progress.Load()
	if p.TotalChapters != 820 {
		t.Fatalf("progress.total_chapters = %d, mong đợi 820", p.TotalChapters)
	}
	// Chỉ cung đầu chiếm số chương thật; phần còn lại chờ expand_arc.
	if flat, _ := s.Outline.LoadOutline(); len(flat) != 35 {
		t.Fatalf("đề cương phẳng có %d chương, mong đợi 35 (chỉ cung đầu được mở rộng)", len(flat))
	}
}
