package store

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// Hồi quy: điều kiện cũ "CurrentVolume > 0 && CurrentArc > 0" khiến bộ kiểm tra nhất quán TỰ TẮT đúng ở
// trạng thái hỏng nặng nhất — sự cố 2026-07 có V0/A0 trên đĩa suốt 10 chương mà không sinh nổi một cảnh báo.
func TestCheckConsistencyWarnsOnZeroVolumeArc(t *testing.T) {
	s := setupLayeredRaw(t, []domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{arcWithChapters(1, 3)},
	}})
	// Đúng hình dạng sự cố: phân lớp nhưng vị trí chưa bao giờ được gán (đề cương lưu với index rỗng).
	if err := s.Progress.UpdateVolumeArc(0, 0); err != nil {
		t.Fatalf("UpdateVolumeArc: %v", err)
	}

	warnings := s.CheckConsistency()
	if !hasWarning(warnings, "V0 A0") {
		t.Fatalf("V0/A0 phải sinh cảnh báo, nhận: %v", warnings)
	}
}

// Vị trí hợp lệ thì im lặng; vị trí không có trong đề cương thì vẫn phải báo.
func TestCheckConsistencyVolumeArcLookup(t *testing.T) {
	s := setupLayeredRaw(t, []domain.VolumeOutline{{
		Index: 1, Title: "卷一", Theme: "主题",
		Arcs: []domain.ArcOutline{arcWithChapters(1, 3)},
	}})

	if err := s.Progress.UpdateVolumeArc(1, 1); err != nil {
		t.Fatalf("UpdateVolumeArc: %v", err)
	}
	if w := s.CheckConsistency(); len(w) != 0 {
		t.Fatalf("V1/A1 hợp lệ mà vẫn cảnh báo: %v", w)
	}

	if err := s.Progress.UpdateVolumeArc(9, 9); err != nil {
		t.Fatalf("UpdateVolumeArc: %v", err)
	}
	if w := s.CheckConsistency(); !hasWarning(w, "V9 A9") {
		t.Fatalf("V9/A9 không có trong đề cương, phải cảnh báo, nhận: %v", w)
	}
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
