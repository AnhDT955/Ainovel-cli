// Package exp triển khai khả năng xuất các chương đã hoàn thành.
//
// Đối xứng với imp/: chỉ IO cục bộ, không phụ thuộc LLM, không thay đổi trạng thái store. Xuất có thể
// chạy song song với Coordinator (chỉ đọc Progress + bản thảo cuối chương), thuộc năng lực ngang.
//
// Phiên bản đầu chỉ hỗ trợ TXT; EPUB để lại cho vòng tiếp theo.
package exp

import "github.com/voocel/ainovel-cli/internal/store"

// Format định danh định dạng xuất.
type Format string

const (
	// FormatTXT xuất văn bản thuần.
	FormatTXT Format = "txt"
	// FormatEPUB container EPUB 3 chuẩn (zip + xhtml).
	FormatEPUB Format = "epub"
)

// Options kiểm soát hành vi xuất. Giá trị zero tương đương "xuất toàn bộ sang đường dẫn mặc định, báo lỗi nếu file đã tồn tại".
//
// Định dạng: 《Tên sách》 → phân cách tập → nội dung chương. Hai loại dữ liệu nội bộ không đưa vào xuất: tiền đề (bản thiết kế sáng tác,
// bao gồm độc giả mục tiêu / điểm tiêu thụ cốt lõi / vùng cấm viết và các siêu thông tin hậu trường dành cho tác giả và engine, không phải lời tựa cho độc giả);
// phân cách cung (từ góc nhìn độc giả, cung là cấu trúc nội bộ quá chi tiết). Tên sách và phân cách tập luôn được giữ lại.
type Options struct {
	// Format khi là chuỗi rỗng sẽ được suy ra từ hậu tố OutPath (.txt → TXT, .epub → EPUB);
	// khi OutPath cũng rỗng thì dự phòng FormatTXT. Người gọi SDK có thể chỉ định rõ để bỏ qua suy luận.
	Format Format

	// OutPath đường dẫn file xuất; rỗng nghĩa là {novelDir}/{NovelName}.{ext},
	// ext do Format quyết định (NovelName rỗng thì dùng tên thư mục).
	OutPath string

	// From / To phạm vi chương, khoảng đóng. 0 nghĩa là từ chương 1 / đến chương cuối.
	// Các chương chưa hoàn thành trong phạm vi sẽ bị bỏ qua và ghi vào Result.Skipped, không coi là lỗi.
	From, To int

	// Overwrite có ghi đè khi file đã tồn tại không; mặc định từ chối.
	Overwrite bool
}

// Deps là các phụ thuộc cần thiết cho Run. Chỉ có store; xuất không cần LLM, prompt, bundle.
type Deps struct {
	Store *store.Store
}

// Result là tóm tắt sản phẩm của một lần xuất thành công.
type Result struct {
	// Path đường dẫn file thực tế đã ghi (tuyệt đối hoặc tương đối do người gọi truyền vào).
	Path string
	// Chapters số chương thực tế đã ghi.
	Chapters int
	// Bytes kích thước file tính bằng byte (UTF-8).
	Bytes int
	// Skipped các số chương nằm trong phạm vi yêu cầu nhưng chưa hoàn thành.
	Skipped []int
}
