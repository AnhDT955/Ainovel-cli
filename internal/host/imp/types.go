// Package imp thực hiện việc nhập và phân tích ngược các chương tiểu thuyết từ nguồn ngoài.
//
// Tư tưởng cốt lõi: dùng LLM phân tích ngược foundation + sự kiện từng chương, tái sử dụng
// bộ ba nguyên tử của công cụ save_foundation / commit_chapter để ghi xuống đĩa. Sau khi
// nhập xong, trạng thái store tương đương với "viết xong N chương rồi crash", người gọi
// chỉ cần gọi host.Resume() là có thể tiếp tục viết liền mạch.
//
// Không đi qua Coordinator: nhập là replay tất định, không thuộc phạm vi quyết định của LLM;
// để Coordinator can thiệp chỉ gây thêm bất định. Package này gọi trực tiếp LLM client + công cụ.
package imp

import "time"

// Chapter là một chương đơn sau khi đã tách.
type Chapter struct {
	Title   string
	Content string
}

// Options kiểm soát hành vi nhập.
type Options struct {
	// SourcePath bắt buộc. Đường dẫn tới một file txt/md.
	SourcePath string

	// ResumeFrom tùy chọn. Bắt đầu nhập từ chương thứ N; 0 / 1 nghĩa là từ đầu.
	// Nếu > 1, sẽ bỏ qua bước phân tích ngược Foundation (coi như đã ghi xuống đĩa).
	ResumeFrom int
}

// Stage biểu thị giai đoạn hiện tại của luồng nhập.
type Stage string

const (
	StageSplitting  Stage = "splitting"
	StageFoundation Stage = "foundation"
	StageChapter    Stage = "chapter"
	StageDone       Stage = "done"
	StageError      Stage = "error"
)

// Event là sự kiện tiến trình mà luồng nhập phát ra ra ngoài.
type Event struct {
	Time    time.Time
	Stage   Stage
	Current int    // số chương hiện tại ở giai đoạn chapter; các giai đoạn khác là 0
	Total   int    // tổng số chương
	Message string // mô tả dạng người đọc được
	Err     error  // mang theo lỗi khi StageError
}
