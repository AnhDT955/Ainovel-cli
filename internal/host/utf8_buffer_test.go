package host

import "testing"

func TestIncompleteUTF8Suffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"ascii only", "hello", 0},
		{"complete 2-byte", "caf\xc3\xa9", 0},           // café — é = C3 A9 (2 bytes, hoàn chỉnh)
		{"complete 3-byte", "l\xe1\xba\xa1c", 0},         // lạc — ạ = E1 BA A1 (3 bytes, hoàn chỉnh) + c
		{"complete 3-byte end", "\xe4\xb8\xad", 0},       // 中 = E4 B8 AD (3 bytes)
		{"incomplete 3-byte 1", "abc\xe4", 1},             // E4 cần thêm 2 bytes
		{"incomplete 3-byte 2", "abc\xe4\xb8", 2},         // E4 B8 cần thêm 1 byte
		{"incomplete 2-byte", "abc\xc3", 1},               // C3 cần thêm 1 byte
		{"incomplete 4-byte 1", "abc\xf0", 1},             // F0 cần thêm 3 bytes
		{"incomplete 4-byte 2", "abc\xf0\x9f", 2},         // F0 9F cần thêm 2 bytes
		{"incomplete 4-byte 3", "abc\xf0\x9f\x98", 3},     // F0 9F 98 cần thêm 1 byte
		{"complete 4-byte", "abc\xf0\x9f\x98\x80", 0},     // 😀 hoàn chỉnh
		{"vietnamese a-dot", "L\xe1\xba\xa1c", 0},         // Lạc hoàn chỉnh
		{"split vietnamese", "L\xe1\xba", 2},               // Lạ thiếu 1 byte (A1)
		{"split vietnamese 1", "L\xe1", 1},                 // L + E1 thiếu 2 bytes
		{"mixed complete", "Trường An\xe1\xba\xa1", 0},    // ạ hoàn chỉnh
		{"mixed incomplete", "Trường An\xe1\xba", 2},       // ạ thiếu 1 byte
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := incompleteUTF8Suffix(tt.input)
			if got != tt.want {
				t.Errorf("incompleteUTF8Suffix(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestUTF8BufferingRoundtrip kiểm tra rằng chia chuỗi tại ranh giới UTF-8 lẻ
// rồi nối lại bằng buffer vẫn giữ nguyên nội dung.
func TestUTF8BufferingRoundtrip(t *testing.T) {
	original := "Thiếu chủ của một thương hành nhỏ ở thành Lạc Dương"
	// Thử chia tại mọi vị trí byte
	for i := 1; i < len(original); i++ {
		chunk1 := original[:i]
		chunk2 := original[i:]

		// Mô phỏng buffering
		n := incompleteUTF8Suffix(chunk1)
		emit1 := chunk1[:len(chunk1)-n]
		pending := chunk1[len(chunk1)-n:]
		emit2 := pending + chunk2

		reassembled := emit1 + emit2
		if reassembled != original {
			t.Errorf("split at byte %d: reassembled %q != original %q (n=%d)", i, reassembled, original, n)
		}
	}
}
