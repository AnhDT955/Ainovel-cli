package utils

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// DecodeText giải mã byte file văn bản do người dùng cung cấp sang UTF-8:
// nếu không phải UTF-8 hợp lệ thì chuyển mã theo GB18030 (tập cha của GBK) —
// phần lớn file txt tiểu thuyết tiếng Trung lưu hành trên mạng được mã hóa GBK,
// đọc trực tiếp như UTF-8 sẽ thành ký tự lỗi. Byte sequence không phải GBK sẽ
// được decoder thay bằng U+FFFD (vốn đã lỗi, để phía gọi xử lý báo lỗi khi
// không khớp). Cuối cùng loại bỏ UTF-8 BOM (nếu không sẽ bám vào đầu dòng khi
// so khớp).
func DecodeText(data []byte) string {
	if !utf8.Valid(data) {
		if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
			data = decoded
		}
	}
	return strings.TrimPrefix(string(data), "\uFEFF")
}
