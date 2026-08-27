package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/store"
)

// Bốn ca dưới đây là payload thật của sự cố 2026-07-16 (meta/sessions/agents/architect_long-ch01.jsonl):
// bốn lệnh gọi save_foundation liên tiếp chết vì bốn cách gói JSON khác nhau, không cái nào là lỗi cú pháp.

func newShapeToolStore(t *testing.T) (*SaveFoundationTool, *store.Store) {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("novel", 0); err != nil {
		t.Fatalf("Init progress: %v", err)
	}
	return NewSaveFoundationTool(s), s
}

func execFoundation(t *testing.T, tool *SaveFoundationTool, payload map[string]any) error {
	t.Helper()
	args, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, err = tool.Execute(context.Background(), args)
	return err
}

// Chế độ hỏng #1: mô hình lặp tên tham số thành khóa bọc ngoài — `{"characters": [...]}`.
func TestSaveFoundationUnwrapsObjectEnvelope(t *testing.T) {
	tool, s := newShapeToolStore(t)

	err := execFoundation(t, tool, map[string]any{
		"type": "characters",
		"content": map[string]any{
			"characters": []map[string]any{
				{"name": "Lý Xuyên", "description": "Người xuyên không cày độ thuần thục.", "role": "nhân vật chính"},
			},
		},
	})
	if err != nil {
		t.Fatalf("envelope phải được gỡ, nhận lỗi: %v", err)
	}

	chars, err := s.Characters.Load()
	if err != nil {
		t.Fatalf("Load characters: %v", err)
	}
	if len(chars) != 1 || chars[0].Name != "Lý Xuyên" {
		t.Fatalf("nội dung không được giữ nguyên sau khi gỡ envelope: %+v", chars)
	}
}

// Envelope chỉ được gỡ khi ứng viên là DUY NHẤT: hai khóa mảng thì ý định mơ hồ, không đoán.
func TestSaveFoundationRejectsAmbiguousEnvelope(t *testing.T) {
	tool, _ := newShapeToolStore(t)

	err := execFoundation(t, tool, map[string]any{
		"type": "characters",
		"content": map[string]any{
			"characters": []map[string]any{{"name": "A", "description": "d"}},
			"extras":     []map[string]any{{"name": "B", "description": "d"}},
		},
	})
	if err == nil {
		t.Fatal("envelope mơ hồ (hai khóa mảng) phải bị từ chối chứ không được đoán")
	}
}

// Chế độ hỏng #2: mảng con bị mã hoá hai lần thành chuỗi — `"chapters": "[{...}]"`.
func TestSaveFoundationRevivesDoubleEncodedChapters(t *testing.T) {
	tool, s := newShapeToolStore(t)

	chapters, err := json.Marshal([]map[string]any{
		{"chapter": 1, "title": "Tỉnh Trong Tiếng Mưa Máu", "core_event": "Lý Xuyên tỉnh lại trong thân xác nguyên chủ."},
	})
	if err != nil {
		t.Fatalf("Marshal chapters: %v", err)
	}

	err = execFoundation(t, tool, map[string]any{
		"type":  "layered_outline",
		"scale": "long",
		"content": []map[string]any{{
			"index": 1, "title": "Tập 1", "theme": "Khởi đầu",
			"arcs": []map[string]any{
				{"index": 1, "title": "Arc 1", "goal": "Nhập môn", "chapters": string(chapters)},
				{"index": 2, "title": "Arc 2", "goal": "Bến tàu", "estimated_chapters": 320},
			},
		}},
	})
	if err != nil {
		t.Fatalf("chapters mã hoá hai lần phải được gỡ, nhận lỗi: %v", err)
	}

	vols, err := s.Outline.LoadLayeredOutline()
	if err != nil {
		t.Fatalf("LoadLayeredOutline: %v", err)
	}
	if len(vols) != 1 || len(vols[0].Arcs) != 2 {
		t.Fatalf("cấu trúc tập/cung sai: %+v", vols)
	}
	got := vols[0].Arcs[0].Chapters
	if len(got) != 1 || got[0].Title != "Tỉnh Trong Tiếng Mưa Máu" {
		t.Fatalf("nội dung chương không được giữ nguyên: %+v", got)
	}
}

// Văn xuôi mở đầu bằng '[' không phải JSON — không được đụng vào, và không được nuốt lỗi gốc.
func TestSaveFoundationLeavesProseStringsAlone(t *testing.T) {
	tool, _ := newShapeToolStore(t)

	err := execFoundation(t, tool, map[string]any{
		"type":  "layered_outline",
		"scale": "long",
		"content": []map[string]any{{
			"index": 1, "title": "Tập 1", "theme": "Khởi đầu",
			"arcs": []map[string]any{{"index": 1, "title": "Arc 1", "goal": "g", "chapters": "[chương 1-35] chưa mở rộng"}},
		}},
	})
	if err == nil {
		t.Fatal("chuỗi văn xuôi ở vị trí mảng phải giữ nguyên lỗi kiểu, không được 'sửa' bừa")
	}
	if !strings.Contains(err.Error(), "arcs.chapters") {
		t.Fatalf("lỗi phải trỏ đúng trường hỏng, nhận: %v", err)
	}
}

// Chế độ hỏng #3: đúng hình dạng nhưng SAI TÊN KHÓA — encoding/json bỏ qua im lặng, struct ra rỗng.
func TestSaveFoundationReportsUnknownKeys(t *testing.T) {
	tool, _ := newShapeToolStore(t)

	err := execFoundation(t, tool, map[string]any{
		"type": "world_rules",
		"content": []map[string]any{
			{"loai": "tu luyện", "noi_dung": "Độ thuần thục cày đủ thì phá hạn.", "gioi_han": "Không vượt Lục Địa Thần Tiên."},
		},
	})
	if err == nil {
		t.Fatal("world_rules sai tên khóa phải bị từ chối")
	}
	msg := err.Error()
	for _, want := range []string{"SAI TÊN KHÓA", "loai", "noi_dung", "category", "rule"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("thông báo lỗi thiếu %q — Kiến trúc sư vẫn phải đoán. Nhận: %s", want, msg)
		}
	}
}

// Khóa lạ nằm sâu trong arcs[].chapters[] vẫn phải được chỉ mặt kèm đường dẫn:
// đây đúng là chỗ hỏng thật của layered_outline (mọi chương rỗng cả title lẫn core_event).
func TestSaveFoundationReportsNestedUnknownKeys(t *testing.T) {
	tool, _ := newShapeToolStore(t)

	err := execFoundation(t, tool, map[string]any{
		"type":  "layered_outline",
		"scale": "long",
		"content": []map[string]any{{
			"index": 1, "title": "Tập 1", "theme": "Khởi đầu",
			"arcs": []map[string]any{{
				"index": 1, "title": "Arc 1", "goal": "g",
				"chapters": []map[string]any{
					{"chapter": 1, "ten_chuong": "Tỉnh Trong Tiếng Mưa Máu", "su_kien": "Lý Xuyên tỉnh lại."},
				},
			}},
		}},
	})
	if err == nil {
		t.Fatal("chapters sai tên khóa phải bị từ chối")
	}
	msg := err.Error()
	for _, want := range []string{"arcs.chapters.ten_chuong", "core_event"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("hint phải trỏ đường dẫn lồng nhau và nêu khóa hợp lệ, thiếu %q. Nhận: %s", want, msg)
		}
	}
}

// Bộ nền rỗng THẬT không được gắn hint sai địa chỉ: khóa đúng, nội dung mới là thứ thiếu.
func TestSaveFoundationNoKeyHintWhenKeysAreCorrect(t *testing.T) {
	tool, _ := newShapeToolStore(t)

	err := execFoundation(t, tool, map[string]any{
		"type":    "world_rules",
		"content": []map[string]any{{"category": "", "rule": "", "boundary": ""}},
	})
	if err == nil {
		t.Fatal("quy tắc rỗng phải bị từ chối")
	}
	if strings.Contains(err.Error(), "SAI TÊN KHÓA") {
		t.Fatalf("khóa vốn đã đúng, hint sai địa chỉ sẽ đẩy Kiến trúc sư đi sửa nhầm chỗ: %s", err.Error())
	}
}
