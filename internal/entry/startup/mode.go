package startup

import "fmt"

// startup layer chịu trách nhiệm điều phối khởi động "trước khi vào Engine".
// Quy ước phân lớp:
// 1. entry/tui, entry/headless là điểm vào của máy chủ;
// 2. startup chịu trách nhiệm các chiến lược khởi động nhanh/đồng sáng tác/tiếp tục viết;
// 3. orchestrator.Engine chỉ chịu trách nhiệm thực thi phiên chính thức, không chịu trách nhiệm chuẩn bị trước chế độ.

// Mode biểu thị loại chiến lược khởi động trước khi vào Engine.
type Mode string

const (
	// ModeQuick sử dụng trực tiếp đầu vào của người dùng làm điểm khởi đầu sáng tác.
	ModeQuick Mode = "quick"
	// ModeCoCreate thực hiện nhiều vòng làm rõ trước, sau đó tạo bản nháp sáng tác để vào Engine.
	ModeCoCreate Mode = "cocreate"
	// ModeContinueFromNovel lắp ghép ngữ cảnh dựa trên nội dung tiểu thuyết hiện có rồi tiếp tục viết.
	ModeContinueFromNovel Mode = "continue_from_novel"
)

// Request mô tả đầu vào thô mà lớp điểm vào gửi cho lớp chiến lược khởi động.
// Điểm vào máy chủ thu thập đầu vào người dùng trước, sau đó startup tổ chức thành kế hoạch có thể vào Engine.
type Request struct {
	Mode        Mode
	UserPrompt  string
	NovelPath   string
	OutputDir   string
	Interactive bool
}

// Plan mô tả kết quả đầu ra của lớp chiến lược khởi động.
// Điểm vào máy chủ không nên tự ghép prompt khởi động chính thức, mà nên dùng Plan rồi điều khiển Engine.
type Plan struct {
	Mode        Mode
	DisplayName string
	StartPrompt string
	ResumeOnly  bool
}

// ErrNotImplemented đánh dấu chiến lược giữ chỗ chưa được triển khai.
var ErrNotImplemented = fmt.Errorf("startup mode not implemented")

// PrepareContinueFromNovel là điểm giữ chỗ thống nhất cho "tiếp tục viết dựa trên tiểu thuyết hiện có".
// TUI/headless trong tương lai đều nên tổ chức đầu vào thành Request trước, rồi từ đây tạo ra Plan có thể vào Engine.
func PrepareContinueFromNovel(req Request) (Plan, error) {
	return Plan{}, fmt.Errorf("%w: %s", ErrNotImplemented, ModeContinueFromNovel)
}
