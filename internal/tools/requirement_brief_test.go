package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// brief mô phỏng tài liệu yêu cầu thật: các danh từ riêng dưới đây là thứ đã biến mất hoàn toàn
// khỏi 68 chương được sinh ra (chương 1 có 0 lần nhắc "Hỗn Nguyên", 0 lần "Nhập Lưu", 0 lần "cẩu đạo").
const testBrief = `# Loạn Thế Võ Đạo

## Hệ thống
Hỗn Nguyên Đạo lục — cày độ thuần thục không có bình cảnh.

## Cảnh giới
Bất Nhập Lưu (Luyện Bì → Luyện Tủy) → Nhập Lưu → Siêu Phàm.

## Nguyên tắc nhân vật
Cẩu đạo: điệu thấp làm người, đã ra tay là dứt điểm.`

func briefStore(t *testing.T) *store.Store {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Brief.Save(testBrief); err != nil {
		t.Fatalf("Save brief: %v", err)
	}
	return s
}

func execContext(t *testing.T, s *store.Store, args map[string]any) map[string]any {
	t.Helper()
	tool := NewContextTool(s, References{}, "wuxia", rules.LoadOptions{})
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	out, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return result
}

// Hồi quy cốt lõi: trước bản vá, Người viết KHÔNG có đường nào chạm tới yêu cầu người dùng —
// novel_context chỉ nạp premise/outline/world_rules do Kiến trúc sư viết lại. Khi bộ nền rỗng,
// Người viết buộc phải bịa cả thế giới. Brief phải tới được đường chương.
func TestContextTool_WriterPathIncludesRequirementBrief(t *testing.T) {
	s := briefStore(t)
	result := execContext(t, s, map[string]any{"chapter": 1})

	got, ok := result["requirement_brief"].(string)
	if !ok {
		t.Fatal("đường Người viết (chapter=N) phải có requirement_brief")
	}
	for _, noun := range []string{"Hỗn Nguyên Đạo lục", "Nhập Lưu", "Luyện Bì", "Cẩu đạo"} {
		if !strings.Contains(got, noun) {
			t.Errorf("brief phải giữ nguyên văn danh từ riêng %q", noun)
		}
	}
	if _, ok := result["requirement_brief_usage"].(string); !ok {
		t.Error("phải kèm chú thích cho model biết brief là nguồn sự thật cao nhất")
	}
}

// Kiến trúc sư là bên chuyển yêu cầu thành bộ nền, nên nó phải nhận brief NGUYÊN VĂN.
func TestContextTool_ArchitectPathIncludesRequirementBrief(t *testing.T) {
	s := briefStore(t)
	result := execContext(t, s, map[string]any{}) // không truyền chapter = đường architect

	got, ok := result["requirement_brief"].(string)
	if !ok {
		t.Fatal("đường Kiến trúc sư (không có chapter) phải có requirement_brief")
	}
	if got != testBrief {
		t.Errorf("Kiến trúc sư phải nhận brief nguyên văn, không phải bản rút gọn.\ngot: %q", got)
	}
}

// Truyện tạo trước khi có brief.md (hoặc khởi động không qua /load) vẫn phải chạy bình thường.
func TestContextTool_NoBriefIsNotAnError(t *testing.T) {
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result := execContext(t, s, map[string]any{"chapter": 1})

	if _, exists := result["requirement_brief"]; exists {
		t.Error("không có brief thì không được bịa ra khóa rỗng")
	}
	if warnings, ok := result["_warnings"].([]any); ok {
		for _, w := range warnings {
			if s, _ := w.(string); strings.Contains(s, "brief") {
				t.Errorf("thiếu brief là trạng thái hợp lệ, không được cảnh báo: %v", s)
			}
		}
	}
}

// Brief nằm ngoài trimOrder: nó là đầu vào do người dùng sở hữu. Cắt nó đi là quay lại đúng lỗi cũ.
func TestContextTool_BriefSurvivesBudgetTrim(t *testing.T) {
	s := briefStore(t)
	// Bơm phình các mục ưu tiên thấp để ép trimByBudget chạy.
	bulk := strings.Repeat("Lý Xuyên đứng trong sân võ quán, khí huyết trào lên. ", 2000)
	var events []domain.TimelineEvent
	for i := 1; i <= 400; i++ {
		events = append(events, domain.TimelineEvent{Chapter: i, Time: "ngày " + string(rune('0'+i%10)), Event: bulk})
	}
	if err := s.World.SaveTimeline(events); err != nil {
		t.Fatalf("SaveTimeline: %v", err)
	}

	result := execContext(t, s, map[string]any{"chapter": 400})

	if _, ok := result["requirement_brief"].(string); !ok {
		t.Fatal("requirement_brief bị cắt mất khi ngữ cảnh phình to — đúng lỗi mà bản vá này chữa")
	}
	if trimmed, ok := result["_trimmed"].([]any); !ok || len(trimmed) == 0 {
		t.Log("cảnh báo: trim không kích hoạt, test này chưa chứng minh được điều cần chứng minh")
	}
}
