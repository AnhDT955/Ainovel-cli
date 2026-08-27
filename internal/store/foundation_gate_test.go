package store

import (
	"slices"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// Đây là hồi quy cho cơ chế đã cho phép cả một cuốn sách được viết từ bộ nền rỗng.
//
// Phiên bản cũ của FoundationMissing chỉ hỏi `len(rules) == 0`. Bộ nền thật có 6 quy tắc và 10 nhân
// vật — tất cả rỗng — nên cổng trả về "không thiếu gì", save_foundation tự đẩy phase sang writing,
// và không còn bước nào phía sau phát hiện ra. Cổng phải đo NỘI DUNG, và phải bắt được cả dữ liệu
// đã nằm sẵn trên đĩa (không chỉ dữ liệu ghi qua tool).
func TestFoundationMissing_RejectsEmptySkeletonOnDisk(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := s.Outline.SavePremise("# Tên truyện\n\n## Premise\nMột tiểu thuyết dài kỳ."); err != nil {
		t.Fatalf("SavePremise: %v", err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Biến cố khởi phát", CoreEvent: "Lý Xuyên mất chỗ dựa cuối cùng và buộc phải rời thành."},
	}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	// Đi vòng qua save_foundation, ghi thẳng xuống đĩa — mô phỏng đúng dữ liệu đã tồn tại của
	// truyện cũ, và chứng minh cổng không phụ thuộc vào validation ở tầng tool.
	if err := s.Characters.Save([]domain.Character{
		{Name: "Nhân vật chính", Role: "trung tâm hành động", Description: ""},
		{Name: "Phản diện lõi", Role: "thế lực đứng sau", Description: ""},
	}); err != nil {
		t.Fatalf("Save characters: %v", err)
	}
	if err := s.World.SaveWorldRules([]domain.WorldRule{
		{Category: "", Rule: "", Boundary: ""},
		{Category: "", Rule: "", Boundary: ""},
	}); err != nil {
		t.Fatalf("SaveWorldRules: %v", err)
	}

	missing := s.FoundationMissing()
	for _, want := range []string{"premise", "characters", "world_rules"} {
		if !slices.Contains(missing, want) {
			t.Errorf("%q phải bị báo thiếu (bộ nền rỗng), missing=%v", want, missing)
		}
	}
	if slices.Contains(missing, "outline") {
		t.Errorf("outline có nội dung thật, không được báo thiếu: %v", missing)
	}
}

// Hồi quy cho run 2026-07-15: cùng một cơ chế, lần này lọt qua ở outline.
//
// Kiến trúc sư lưu danh sách CUNG TRUYỆN vào chỗ của danh sách CHƯƠNG. Cổng khi đó chỉ hỏi
// `len(o) == 0` nên nhận cả 14 mục, phase tự sang writing với total_chapters=14 (tier long, tối
// thiểu 300 chương), và Người viết bắt đầu draft chương 1 trên đề cương không có core_event nào.
// Dữ liệu dưới đây chép nguyên hình dạng từ output/novel/outline.json của run đó.
func TestFoundationMissing_RejectsArcLabelsSavedAsOutline(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 0, Title: "Core conflict"},
		{Chapter: 0, Title: "Arc 1: Học Đồ Võ Quán Trong Thành Mưa Máu, chương 1-35"},
		{Chapter: 0, Title: "Arc 2: Hắc Bang Đoạt Phố, Một Đao Không Lưu Danh, chương 36-80"},
	}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}

	if missing := s.FoundationMissing(); !slices.Contains(missing, "outline") {
		t.Errorf("nhãn cung truyện không phải đề cương chương, outline phải bị báo thiếu: %v", missing)
	}
}

// Truyện dài không được đi bằng đề cương phẳng: expand_arc / append_volume / tóm tắt phân tầng đều
// đọc từ layered_outline, nên một tier long chỉ có outline.json là ngõ cụt — đúng trạng thái mà run
// 2026-07-15 rơi vào sau khi cổng nhận nhầm 14 mục nhãn cung.
func TestFoundationMissing_LongTierRequiresLayeredOutline(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.RunMeta.SetPlanningTier(domain.PlanningTierLong); err != nil {
		t.Fatalf("SetPlanningTier: %v", err)
	}

	if err := s.Outline.SavePremise("# Loạn Thế Võ Đạo\n\n## Thể loại\nVõ hiệp vô địch lưu."); err != nil {
		t.Fatalf("SavePremise: %v", err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Xuyên qua", CoreEvent: "Lý Xuyên tỉnh dậy trong thân xác học đồ võ quán."},
	}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	if err := s.Characters.Save([]domain.Character{
		{Name: "Lý Xuyên", Role: "nhân vật chính", Description: "Thiếu niên giấu tài, cẩu đạo tích lũy độ thuần thục."},
	}); err != nil {
		t.Fatalf("Save characters: %v", err)
	}
	if err := s.World.SaveWorldRules([]domain.WorldRule{
		{Category: "Cảnh giới", Rule: "Lộ tuyến võ đạo đi từ Luyện Bì tới Lục Địa Thần Tiên."},
	}); err != nil {
		t.Fatalf("SaveWorldRules: %v", err)
	}

	if missing := s.FoundationMissing(); !slices.Contains(missing, "layered_outline") {
		t.Errorf("tier long chưa có đề cương phân tầng thì chưa đủ bộ nền: %v", missing)
	}
}

func TestFoundationMissing_AcceptsRealFoundation(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := s.Outline.SavePremise("# Loạn Thế Võ Đạo\n\n## Thể loại và tông điệu\nVõ hiệp huyền huyễn."); err != nil {
		t.Fatalf("SavePremise: %v", err)
	}
	if err := s.Outline.SaveOutline([]domain.OutlineEntry{
		{Chapter: 1, Title: "Xuyên qua", CoreEvent: "Lý Xuyên tỉnh dậy trong thân xác một học đồ võ quán sắp bị đuổi."},
	}); err != nil {
		t.Fatalf("SaveOutline: %v", err)
	}
	if err := s.Characters.Save([]domain.Character{
		{Name: "Lý Xuyên", Role: "nhân vật chính", Description: "Thiếu niên xuyên không, giấu tài theo cẩu đạo."},
	}); err != nil {
		t.Fatalf("Save characters: %v", err)
	}
	if err := s.World.SaveWorldRules([]domain.WorldRule{
		{Category: "magic", Rule: "Cày độ thuần thục thì không có bình cảnh cách trở.", Boundary: "Phải theo thứ tự Luyện Bì → Luyện Tủy."},
	}); err != nil {
		t.Fatalf("SaveWorldRules: %v", err)
	}

	if missing := s.FoundationMissing(); len(missing) != 0 {
		t.Errorf("bộ nền có nội dung thật phải được coi là đủ, missing=%v", missing)
	}
}

func TestBriefStore_RoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if got, err := s.Brief.Load(); err != nil || got != "" {
		t.Fatalf("truyện chưa có brief phải trả rỗng không lỗi: %q, %v", got, err)
	}

	const brief = "# Loạn Thế Võ Đạo\n\nHỗn Nguyên Đạo lục, độ thuần thục, cẩu đạo."
	if err := s.Brief.Save(brief); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Brief.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != brief {
		t.Errorf("brief phải được giữ NGUYÊN VĂN.\ngot:  %q\nwant: %q", got, brief)
	}

	// Chuỗi rỗng không được xóa mất brief đã có.
	if err := s.Brief.Save("   "); err != nil {
		t.Fatalf("Save empty: %v", err)
	}
	if got, _ := s.Brief.Load(); got != brief {
		t.Errorf("lưu chuỗi rỗng không được ghi đè brief đã có, got %q", got)
	}
}
