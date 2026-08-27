package store

import (
	"os"
	"strings"
)

// BriefStore giữ bản yêu cầu sáng tác đã chuẩn hóa (brief.md) — tài liệu gốc của người dùng sau khi
// Requirement Extractor ánh xạ, lưu NGUYÊN VĂN và không bao giờ bị nén.
//
// Vì sao phải nằm trên đĩa: thiết kế ban đầu cố ý không ghi brief xuống Store ("brief + chỉ thị mạnh"),
// chỉ ghép nó thành một tin nhắn chat gửi cho Điều phối viên. Hệ quả là brief chỉ sống trong lịch sử
// hội thoại: khi ngữ cảnh chạm ngưỡng nén (context_window × 0.85) nó bị tóm tắt mất, và Người viết
// thì chưa bao giờ nhìn thấy nó — novel_context chỉ nạp premise/outline/world_rules. Khi Kiến trúc sư
// dựng bộ nền sai hoặc rỗng, không còn bất kỳ bản sao nào của yêu cầu người dùng để đối chiếu.
//
// Đặt ở gốc thư mục truyện (cạnh premise.md) thay vì meta/: đây là đầu vào do người dùng sở hữu,
// họ cần đọc và sửa được bằng tay.
type BriefStore struct{ io *IO }

func NewBriefStore(io *IO) *BriefStore { return &BriefStore{io: io} }

// Save ghi brief.md. Chuỗi rỗng bị bỏ qua để không xóa mất brief đã có khi bên gọi truyền nhầm.
func (s *BriefStore) Save(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return s.io.WriteMarkdown("brief.md", content)
}

// Load đọc brief.md. Trả về chuỗi rỗng nếu chưa có (truyện tạo trước khi có tính năng này,
// hoặc khởi động không qua /load).
func (s *BriefStore) Load() (string, error) {
	data, err := s.io.ReadFile("brief.md")
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}
