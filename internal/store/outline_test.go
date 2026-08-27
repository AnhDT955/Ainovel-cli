package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
)

func setupLayered(t *testing.T, volumes []domain.VolumeOutline) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Outline.SaveLayeredOutline(volumes); err != nil {
		t.Fatalf("SaveLayeredOutline: %v", err)
	}
	// save_foundation luôn ghi kèm đề cương phẳng; fixture phản ánh điều đó để các assertion
	// về outline.json có ý nghĩa.
	if err := s.Outline.SaveOutline(domain.FlattenOutline(volumes)); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	if err := s.Progress.SetLayered(true); err != nil {
		t.Fatalf("SetLayered: %v", err)
	}
	return s
}

// setupLayeredRaw ghi thẳng layered_outline.json, bỏ qua ValidateLayeredOutline.
// Dùng để dựng các đề cương hỏng có THẬT trên đĩa từ trước khi có validation — các lớp bảo vệ
// phía sau vẫn phải xử lý đúng dữ liệu cũ, không được dựa vào việc validation đã chặn từ đầu.
func setupLayeredRaw(t *testing.T, volumes []domain.VolumeOutline) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := s.Outline.io.WriteJSON("layered_outline.json", volumes); err != nil {
		t.Fatalf("write raw layered_outline: %v", err)
	}
	if err := s.Progress.SetLayered(true); err != nil {
		t.Fatalf("SetLayered: %v", err)
	}
	return s
}

func TestCheckArcBoundaryNeedsNewVolume(t *testing.T) {
	// Chỉ có 1 tập 1 cung truyện 1 chương, và không phải Final → phải kích hoạt NeedsNewVolume
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "第一卷", Theme: "起步",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "首弧", Goal: "目标",
			Chapters: []domain.OutlineEntry{{Title: "第一章", CoreEvent: "开局", Hook: "继续"}},
		}},
	}})

	b, err := s.Outline.CheckArcBoundary(1) // Chương 1 = chương cuối của cung truyện/tập
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if b == nil {
		t.Fatal("expected boundary, got nil")
	}
	if !b.IsArcEnd || !b.IsVolumeEnd {
		t.Fatalf("expected arc+volume end, got arc=%v vol=%v", b.IsArcEnd, b.IsVolumeEnd)
	}
	if !b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=true")
	}
	if b.NextVolume != 0 || b.NextArc != 0 {
		t.Fatalf("expected no next, got vol=%d arc=%d", b.NextVolume, b.NextArc)
	}
}

func TestCheckArcBoundaryLastVolumeRequiresDecision(t *testing.T) {
	// Chương cuối của tập đơn → kích hoạt NeedsNewVolume, để Router cho Kiến trúc sư chọn một trong hai:
	// append_volume tiếp tục viết / complete_book kết thúc.
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "唯一卷", Theme: "主题",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "唯一弧", Goal: "收束",
			Chapters: []domain.OutlineEntry{{Title: "终章", CoreEvent: "结局", Hook: "无"}},
		}},
	}})

	b, err := s.Outline.CheckArcBoundary(1)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if !b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=true at last expanded chapter")
	}
	if b.HasNextArc() {
		t.Fatal("expected no next arc")
	}
}

func TestCheckArcBoundaryNextArcInSameVolume(t *testing.T) {
	// 2 cung truyện: kết thúc cung truyện 1 phải trỏ sang cung truyện 2, không kích hoạt NeedsNewVolume
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "第一卷", Theme: "起步",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "首弧", Goal: "目标", Chapters: []domain.OutlineEntry{{Title: "章一", CoreEvent: "事件", Hook: "钩子"}}},
			{Index: 2, Title: "次弧", Goal: "目标2", EstimatedChapters: 10},
		},
	}})

	b, err := s.Outline.CheckArcBoundary(1)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if !b.IsArcEnd {
		t.Fatal("expected arc end")
	}
	if b.IsVolumeEnd {
		t.Fatal("expected not volume end (second arc exists)")
	}
	if b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=false")
	}
	if b.NextVolume != 1 || b.NextArc != 2 {
		t.Fatalf("expected next vol=1 arc=2, got vol=%d arc=%d", b.NextVolume, b.NextArc)
	}
	if !b.NeedsExpansion {
		t.Fatal("expected NeedsExpansion=true for skeleton arc")
	}
}

// arcWithChapters tạo cung đã mở rộng với n chương.
func arcWithChapters(idx, n int) domain.ArcOutline {
	a := domain.ArcOutline{Index: idx, Title: "arc", Goal: "goal"}
	for i := range n {
		a.Chapters = append(a.Chapters, domain.OutlineEntry{
			Title: fmt.Sprintf("ch%d", i+1), CoreEvent: "event", Hook: "hook",
		})
	}
	return a
}

// Hồi quy vòng lặp: chương đã viết vượt quá vùng đề cương đã mở rộng phải trả về boundary OutOfRange
// — trước đây trả nil, khiến Router bỏ qua nhánh cuối cung và im lặng giao writer viết mãi.
//
// Mục tiêu chữa cháy là append_volume chứ không phải expand_arc, kể cả khi vẫn còn cung khung:
// cung khung V1A2 bắt đầu tại chương 13, nằm giữa vùng đã viết (1..20), mở rộng nó sẽ dịch số
// các chương đã viết — và checkExpandSafety sẽ từ chối, tạo ra một vòng lặp mới.
func TestCheckArcBoundaryOutOfRangeNeverTargetsSkeletonInWrittenRegion(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{
			arcWithChapters(1, 12),
			{Index: 2, Title: "骨架弧", Goal: "目标", EstimatedChapters: 10},
		},
	}})

	// Đề cương chỉ phủ 12 chương, nhưng đã viết tới chương 20.
	b, err := s.Outline.CheckArcBoundary(20)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if b == nil {
		t.Fatal("expected out-of-range boundary, got nil (đây chính là lỗ hổng gây vòng lặp)")
	}
	if !b.OutOfRange {
		t.Fatal("expected OutOfRange=true")
	}
	if b.NeedsExpansion {
		t.Fatal("expand_arc ở trạng thái OutOfRange luôn dịch số vùng đã viết, không được đề xuất")
	}
	if !b.NeedsNewVolume {
		t.Fatal("expected NeedsNewVolume=true")
	}
}

// Chương nằm trong vùng đã mở rộng thì boundary bình thường, không được đánh dấu OutOfRange.
func TestCheckArcBoundaryInRangeNotOutOfRange(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{
			arcWithChapters(1, 12),
			{Index: 2, Title: "骨架弧", Goal: "目标", EstimatedChapters: 10},
		},
	}})

	b, err := s.Outline.CheckArcBoundary(12)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if b == nil || b.OutOfRange {
		t.Fatalf("expected normal in-range boundary, got %+v", b)
	}
	if !b.IsArcEnd || !b.NeedsExpansion || b.NextArc != 2 {
		t.Fatalf("expected arc end pointing at V1A2 for expansion, got %+v", b)
	}
}

// Hồi quy trực tiếp của sự cố gốc: expand_arc nhắm nhầm vào cung ĐÃ mở rộng thì ghi đè im lặng,
// làm đề cương phẳng co lại (36 → 25 chương) và kéo theo vòng lặp không thể chữa.
func TestExpandArcRejectsAlreadyExpandedArc(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{
			arcWithChapters(1, 12),
			{Index: 2, Title: "骨架弧", Goal: "目标", EstimatedChapters: 10},
		},
	}})

	err := s.ExpandArc(1, 1, []domain.OutlineEntry{{Title: "ch", CoreEvent: "e", Hook: "h"}}, false)
	if err == nil {
		t.Fatal("expected rejection when re-expanding an already-expanded arc")
	}
	if !errors.Is(err, errs.ErrToolPrecondition) {
		t.Fatalf("expected ErrToolPrecondition, got %v", err)
	}

	// Đề cương phải còn nguyên vẹn 12 chương.
	volumes, _ := s.Outline.LoadLayeredOutline()
	if n := len(volumes[0].Arcs[0].Chapters); n != 12 {
		t.Fatalf("outline bị ghi đè: expected 12 chapters intact, got %d", n)
	}
	flat, _ := s.Outline.LoadOutline()
	if len(flat) != 12 {
		t.Fatalf("flat outline bị co lại: expected 12, got %d", len(flat))
	}
}

// Mở rộng một cung khung nằm TRƯỚC chương cuối đã viết sẽ chèn chương vào giữa và dịch số
// toàn bộ chương đã viết — phải bị từ chối.
func TestExpandArcRejectsInsertionIntoWrittenRegion(t *testing.T) {
	// Hình dạng đề cương hỏng có thật trên đĩa (cung khung nằm trước cung đã mở rộng); ghi thô vì
	// ValidateLayeredOutline giờ đã chặn không cho tạo mới hình dạng này.
	s := setupLayeredRaw(t, []domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "骨架弧", Goal: "目标", EstimatedChapters: 4}, // khung nằm TRƯỚC
			arcWithChapters(2, 12),
		},
	}})
	if err := s.Progress.Save(&domain.Progress{
		Phase:             domain.PhaseWriting,
		Layered:           true,
		CompletedChapters: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
	}); err != nil {
		t.Fatalf("Save progress: %v", err)
	}

	// V1A1 bắt đầu tại chương 1 — nằm giữa vùng đã viết (1..12).
	err := s.ExpandArc(1, 1, []domain.OutlineEntry{
		{Title: "a", CoreEvent: "e", Hook: "h"},
		{Title: "b", CoreEvent: "e", Hook: "h"},
	}, false)
	if err == nil {
		t.Fatal("expected rejection when expanding into already-written region")
	}
	if !errors.Is(err, errs.ErrToolPrecondition) {
		t.Fatalf("expected ErrToolPrecondition, got %v", err)
	}

	// force=true là lối thoát sửa chữa có chủ đích của người dùng.
	if err := s.ExpandArc(1, 1, []domain.OutlineEntry{{Title: "a", CoreEvent: "e", Hook: "h"}}, true); err != nil {
		t.Fatalf("force expand should succeed: %v", err)
	}
}

// Mở rộng cung khung nằm SAU vùng đã viết là đường chính, phải hoạt động bình thường.
func TestExpandArcAllowsSkeletonAfterWrittenRegion(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{
			arcWithChapters(1, 12),
			{Index: 2, Title: "骨架弧", Goal: "目标", EstimatedChapters: 10},
		},
	}})
	if err := s.Progress.Save(&domain.Progress{
		Phase:             domain.PhaseWriting,
		Layered:           true,
		CompletedChapters: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
	}); err != nil {
		t.Fatalf("Save progress: %v", err)
	}

	if err := s.ExpandArc(1, 2, []domain.OutlineEntry{
		{Title: "ch13", CoreEvent: "e", Hook: "h"},
		{Title: "ch14", CoreEvent: "e", Hook: "h"},
	}, false); err != nil {
		t.Fatalf("ExpandArc: %v", err)
	}

	flat, _ := s.Outline.LoadOutline()
	if len(flat) != 14 {
		t.Fatalf("expected 14 chapters after expansion, got %d", len(flat))
	}
	// Vùng đã viết không được dịch số.
	if flat[0].Title != "ch1" || flat[11].Title != "ch12" {
		t.Fatalf("written region renumbered: ch1=%q ch12=%q", flat[0].Title, flat[11].Title)
	}
	if flat[12].Title != "ch13" {
		t.Fatalf("expected ch13 at index 12, got %q", flat[12].Title)
	}
	// TotalChapters phải phản ánh số chương thật, không còn cộng estimate của cung đã mở rộng.
	p, _ := s.Progress.Load()
	if p.TotalChapters != 14 {
		t.Fatalf("expected TotalChapters=14, got %d", p.TotalChapters)
	}
}

// Bất biến đánh số: cung khung không được nằm trước cung đã mở rộng.
// Đây chính là hình dạng đề cương đã gây ra sự cố (V1: A1 khung, A2 khung, A3 12 chương).
func TestSaveLayeredOutlineRejectsSkeletonBeforeExpanded(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	err := s.Outline.SaveLayeredOutline([]domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{
			{Index: 1, Title: "骨架一", Goal: "g", EstimatedChapters: 4},
			{Index: 2, Title: "骨架二", Goal: "g", EstimatedChapters: 4},
			arcWithChapters(3, 12),
		},
	}})
	if err == nil {
		t.Fatal("expected rejection: skeleton arcs before an expanded arc break global numbering")
	}
	if !errors.Is(err, errs.ErrToolArgs) {
		t.Fatalf("expected ErrToolArgs, got %v", err)
	}
}

// Hình dạng hợp lệ: các cung đã mở rộng đứng liền từ đầu, cung khung xếp sau — kể cả vắt qua nhiều tập.
func TestSaveLayeredOutlineAcceptsExpandedThenSkeleton(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := s.Outline.SaveLayeredOutline([]domain.VolumeOutline{
		{
			Index: 1, Title: "卷一", Theme: "主题",
			Arcs: []domain.ArcOutline{
				arcWithChapters(1, 12),
				{Index: 2, Title: "骨架", Goal: "g", EstimatedChapters: 4},
			},
		},
		{
			Index: 2, Title: "卷二", Theme: "主题",
			Arcs: []domain.ArcOutline{
				{Index: 1, Title: "骨架", Goal: "g", EstimatedChapters: 12},
			},
		},
	}); err != nil {
		t.Fatalf("valid outline rejected: %v", err)
	}
}

// Tái hiện sự cố 2026-07: architect lưu đề cương bỏ trống trường "index" → Go điền 0 cho mọi tập/cung →
// Router ra lệnh "tóm tắt cung 0 tập 0" vĩnh viễn và không chương nào viết tiếp được.
// SaveLayeredOutline phải tự đánh số thay vì lưu nguyên trạng.
func TestSaveLayeredOutlineNormalizesZeroIndexes(t *testing.T) {
	arc := func(title string) domain.ArcOutline {
		return domain.ArcOutline{
			Title: title, Goal: "mục tiêu",
			Chapters: []domain.OutlineEntry{{Title: "chương", CoreEvent: "sự kiện", Hook: "móc"}},
		}
	}
	s := setupLayered(t, []domain.VolumeOutline{
		{Title: "Tập một", Theme: "khởi", Arcs: []domain.ArcOutline{arc("cung 1"), arc("cung 2"), arc("cung 3")}},
		{Title: "Tập hai", Theme: "tiến", Arcs: []domain.ArcOutline{arc("cung 1"), arc("cung 2")}},
	})

	volumes, err := s.Outline.LoadLayeredOutline()
	if err != nil {
		t.Fatalf("LoadLayeredOutline: %v", err)
	}
	for vi, v := range volumes {
		if v.Index != vi+1 {
			t.Errorf("Index tập tại vị trí %d = %d, mong đợi %d", vi, v.Index, vi+1)
		}
		for ai, a := range v.Arcs {
			if a.Index != ai+1 {
				t.Errorf("Index cung tại V%d vị trí %d = %d, mong đợi %d", v.Index, ai, a.Index, ai+1)
			}
		}
	}

	// Điều thực sự quan trọng: boundary tại chương cuối cung 1 phải trỏ tới V1/A1 — đúng khóa mà
	// save_arc_summary chấp nhận (volume/arc > 0) và HasArcSummary tra cứu (arc-v01a01.json).
	b, err := s.Outline.CheckArcBoundary(1)
	if err != nil {
		t.Fatalf("CheckArcBoundary: %v", err)
	}
	if !b.IsArcEnd || b.Volume != 1 || b.Arc != 1 {
		t.Fatalf("boundary = %+v, mong đợi cuối cung tại V1/A1", b)
	}
	if b.NextVolume != 1 || b.NextArc != 2 {
		t.Fatalf("cung kế tiếp = V%d/A%d, mong đợi V1/A2", b.NextVolume, b.NextArc)
	}
}

// Tái hiện phần thứ hai của sự cố 2026-07: cùng lần gọi architect làm rụng "index" cũng làm rụng toàn bộ
// "goal"/"theme", và Store nhận trong im lặng — 10 chương được viết ra mà không ai biết mục tiêu cung.
func TestSaveLayeredOutlineRejectsEmptyGoalAndTheme(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cases := []struct {
		name    string
		volumes []domain.VolumeOutline
		want    string
	}{
		{
			name: "thiếu theme của tập",
			volumes: []domain.VolumeOutline{{
				Index: 1, Title: "卷一", Theme: "  ",
				Arcs: []domain.ArcOutline{arcWithChapters(1, 3)},
			}},
			want: "theme",
		},
		{
			name: "thiếu goal của cung",
			volumes: []domain.VolumeOutline{{
				Index: 1, Title: "卷一", Theme: "主题",
				Arcs: []domain.ArcOutline{{
					Index: 1, Title: "弧一", Goal: "",
					Chapters: []domain.OutlineEntry{{Title: "ch", CoreEvent: "e", Hook: "h"}},
				}},
			}},
			want: "goal",
		},
		{
			name: "cung khung cũng phải có goal",
			volumes: []domain.VolumeOutline{{
				Index: 1, Title: "卷一", Theme: "主题",
				Arcs: []domain.ArcOutline{
					arcWithChapters(1, 3),
					{Index: 2, Title: "骨架", Goal: "", EstimatedChapters: 4},
				},
			}},
			want: "goal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Outline.SaveLayeredOutline(tc.volumes)
			if err == nil {
				t.Fatalf("đề cương thiếu %s phải bị từ chối", tc.want)
			}
			if !errors.Is(err, errs.ErrToolArgs) {
				t.Fatalf("mong đợi ErrToolArgs, nhận %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("thông báo lỗi phải nêu rõ trường %q, nhận: %v", tc.want, err)
			}
		})
	}
}

// Vị trí của một chương phải tra theo chính chương đó. Trước đây ngữ cảnh Người viết đọc
// Progress.CurrentVolume/CurrentArc — vốn mô tả chương đã commit gần nhất — nên tại mọi biên cung,
// chương đầu cung nhận mục tiêu của cung TRƯỚC kèm chỉ số vô nghĩa ("chương 11/10 của cung").
func TestLocateChapterPosition(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{
		{
			Index: 1, Title: "Tập một", Theme: "khởi",
			Arcs: []domain.ArcOutline{arcWithChapters(1, 10), arcWithChapters(2, 5)},
		},
		{
			Index: 2, Title: "Tập hai", Theme: "tiến",
			Arcs: []domain.ArcOutline{arcWithChapters(1, 5)},
		},
	})

	cases := []struct {
		chapter                int
		volume, arc            int
		chapterIndex, arcTotal int
	}{
		{chapter: 1, volume: 1, arc: 1, chapterIndex: 1, arcTotal: 10},
		{chapter: 10, volume: 1, arc: 1, chapterIndex: 10, arcTotal: 10},
		{chapter: 11, volume: 1, arc: 2, chapterIndex: 1, arcTotal: 5}, // chương đầu cung 2: đúng chỗ lỗi cũ
		{chapter: 16, volume: 2, arc: 1, chapterIndex: 1, arcTotal: 5}, // chương đầu tập 2
		{chapter: 20, volume: 2, arc: 1, chapterIndex: 5, arcTotal: 5},
	}
	for _, tc := range cases {
		p, err := s.Outline.LocateChapterPosition(tc.chapter)
		if err != nil {
			t.Fatalf("LocateChapterPosition(%d): %v", tc.chapter, err)
		}
		if p == nil {
			t.Fatalf("chương %d phải có vị trí trong đề cương", tc.chapter)
		}
		if p.Volume != tc.volume || p.Arc != tc.arc {
			t.Errorf("chương %d ở V%d/A%d, mong đợi V%d/A%d", tc.chapter, p.Volume, p.Arc, tc.volume, tc.arc)
		}
		if p.ChapterIndex != tc.chapterIndex || p.ArcChapters != tc.arcTotal {
			t.Errorf("chương %d là chương %d/%d của cung, mong đợi %d/%d",
				tc.chapter, p.ChapterIndex, p.ArcChapters, tc.chapterIndex, tc.arcTotal)
		}
	}

	// Ngoài vùng đề cương → (nil, nil), để bên gọi nói thẳng "chưa biết vị trí" thay vì bịa cung của chương trước.
	p, err := s.Outline.LocateChapterPosition(21)
	if err != nil || p != nil {
		t.Fatalf("chương ngoài đề cương phải trả (nil, nil), nhận (%+v, %v)", p, err)
	}
}

func TestAppendVolumeValidation(t *testing.T) {
	s := setupLayered(t, []domain.VolumeOutline{{
		Index: 1, Title: "第一卷", Theme: "起步",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "首弧", Goal: "目标",
			Chapters: []domain.OutlineEntry{{Title: "章", CoreEvent: "事件", Hook: "钩子"}},
		}},
	}})

	validVol := domain.VolumeOutline{
		Index: 2, Title: "第二卷", Theme: "升级",
		Arcs: []domain.ArcOutline{{
			Index: 1, Title: "弧一", Goal: "目标",
			Chapters: []domain.OutlineEntry{{Title: "新章", CoreEvent: "推进", Hook: "钩子"}},
		}},
	}

	// Thêm bình thường phải thành công
	if err := s.AppendVolume(validVol); err != nil {
		t.Fatalf("AppendVolume valid: %v", err)
	}

	// Index sai do Kiến trúc sư khai (ở đây là 0, đúng hình dạng sự cố 2026-07) không còn bị từ chối:
	// vị trí quyết định Index, nên tập này phải được nhận và tự đánh số thành 3.
	if err := s.AppendVolume(domain.VolumeOutline{
		Index: 0, Title: "第三卷", Theme: "x",
		Arcs: []domain.ArcOutline{
			{Index: 0, Title: "弧", Goal: "g", Chapters: []domain.OutlineEntry{{Title: "ch", CoreEvent: "e", Hook: "h"}}},
			{Index: 0, Title: "弧二", Goal: "g", Chapters: []domain.OutlineEntry{{Title: "ch2", CoreEvent: "e2", Hook: "h2"}}},
		},
	}); err != nil {
		t.Fatalf("AppendVolume với index=0: %v", err)
	}
	vols, err := s.Outline.LoadLayeredOutline()
	if err != nil {
		t.Fatalf("LoadLayeredOutline: %v", err)
	}
	last := vols[len(vols)-1]
	if last.Index != 3 {
		t.Fatalf("Index tập được nối = %d, mong đợi 3", last.Index)
	}
	if last.Arcs[0].Index != 1 || last.Arcs[1].Index != 2 {
		t.Fatalf("Index cung = %d,%d, mong đợi 1,2", last.Arcs[0].Index, last.Arcs[1].Index)
	}

	// Không có cung truyện → thất bại
	if err := s.AppendVolume(domain.VolumeOutline{Index: 3, Title: "空", Theme: "x"}); err == nil {
		t.Fatal("expected error for volume with no arcs")
	}

	// Cung truyện đầu tiên không có chương → thất bại
	if err := s.AppendVolume(domain.VolumeOutline{
		Index: 3, Title: "骨架", Theme: "x",
		Arcs: []domain.ArcOutline{{Index: 1, Title: "弧", Goal: "g", EstimatedChapters: 10}},
	}); err == nil {
		t.Fatal("expected error for first arc without chapters")
	}
}

// Ghi chú: ngữ nghĩa dùng tập Final để từ chối append đã được đẩy xuống tầng save_foundation (Phase=Complete từ chối),
// xem save_foundation_test.go::TestSaveFoundationAppendVolumeRejectsAfterComplete.
// Tầng store chỉ giữ lại kiểm tra cấu trúc (Index tăng dần / cung truyện đầu tiên có chương, v.v.).

func TestSaveAndLoadCompass(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// direction rỗng phải thất bại
	if err := s.Outline.SaveCompass(domain.StoryCompass{EstimatedScale: "3 卷"}); err == nil {
		t.Fatal("expected error for empty ending_direction")
	}

	// Lưu bình thường
	compass := domain.StoryCompass{
		EndingDirection: "主角面对最终抉择",
		OpenThreads:     []string{"线索A", "关系B"},
		EstimatedScale:  "预计 4-6 卷",
		LastUpdated:     12,
	}
	if err := s.Outline.SaveCompass(compass); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}

	loaded, err := s.Outline.LoadCompass()
	if err != nil {
		t.Fatalf("LoadCompass: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected compass, got nil")
	}
	if loaded.EndingDirection != "主角面对最终抉择" {
		t.Fatalf("expected direction %q, got %q", "主角面对最终抉择", loaded.EndingDirection)
	}
	if len(loaded.OpenThreads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(loaded.OpenThreads))
	}
}
