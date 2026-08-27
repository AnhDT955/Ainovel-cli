package tui

import "github.com/charmbracelet/lipgloss"

// Bảng màu chủ đề — gam ấm phong cách giấy cổ
// AdaptiveColor: Light = giá trị nền sáng, Dark = giá trị nền tối
//
// Nguyên tắc thiết kế: Gam Light giữ nguyên (đã điều chỉnh ổn định trên nền sáng);
// Gam Dark nâng độ sáng ~25% so với Light, tăng độ bão hòa nhẹ để đảm bảo
// đủ tương phản trên nền tối (colorDim trước đây là #6b6355 gần như vô hình
// trên nền đen #1c1c1c, khiến đường phân cách/văn bản phụ biến mất).
//
// colorAccent2 nền tối đổi từ #7a9e7e sang xanh ngọc #5fb8a3 để phân biệt
// với colorSuccess màu "xanh lành mạnh" — trước đây hai màu giống nhau hoàn toàn,
// gây nhầm lẫn giữa màu nhãn của architect agent và cảm giác "tỉ lệ trúng cao".
// bodyTextColor là chiến lược màu tiền cảnh cho "văn bản trung tính":
//   - Terminal tối → NoColor, kế thừa màu tiền cảnh mặc định của terminal, tránh
//     ép cứng #e8e0d0 trắng sữa lên theme người dùng tự cấu hình (người dùng
//     thực tế thấy màu mặc định nền tối dễ đọc hơn).
//   - Terminal sáng → dùng gam Light của colorText (nâu đậm #3d3529), giữ cảm giác
//     ấm của thương hiệu; màu đen mặc định trên nền sáng quá cứng, nâu đậm đã
//     điều chỉnh nhìn mềm mại hơn trên nền sáng.
//
// AdaptiveColor cần có giá trị ở cả hai đầu, không có tùy chọn "không màu",
// vì vậy kiểm tra nền một lần lúc khởi động, sau đó tất cả tổng quan/nội dung
// chương/mô tả lệnh và các "văn bản trung tính" đều tham chiếu bodyTextColor.
var bodyTextColor lipgloss.TerminalColor = func() lipgloss.TerminalColor {
	if lipgloss.HasDarkBackground() {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color("#3d3529")
}()

var (
	colorText    = lipgloss.AdaptiveColor{Light: "#3d3529", Dark: "#e8e0d0"}
	colorDim     = lipgloss.AdaptiveColor{Light: "#8a7e6b", Dark: "#8a8175"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#7a7060", Dark: "#b8b09c"}
	colorAccent  = lipgloss.AdaptiveColor{Light: "#b8860b", Dark: "#e5b449"}
	colorAccent2 = lipgloss.AdaptiveColor{Light: "#3d7a42", Dark: "#5fb8a3"}
	colorRunning = lipgloss.AdaptiveColor{Light: "#6f8641", Dark: "#b5d075"}
	colorSuccess = lipgloss.AdaptiveColor{Light: "#3d7a42", Dark: "#7ec488"}
	colorError   = lipgloss.AdaptiveColor{Light: "#b5433a", Dark: "#e07060"}
	colorReview  = lipgloss.AdaptiveColor{Light: "#b07530", Dark: "#e09b5a"}
	colorContext = lipgloss.AdaptiveColor{Light: "#6b5a9e", Dark: "#a890d8"}
	colorTool    = lipgloss.AdaptiveColor{Light: "#3a7a8a", Dark: "#7ec5d8"}
)

// Ánh xạ màu nhãn trạng thái
var statusColors = map[string]lipgloss.AdaptiveColor{
	"READY":    colorDim,
	"PAUSING":  colorAccent,
	"PAUSED":   colorAccent,
	"RUNNING":  colorRunning,
	"REVIEW":   colorReview,
	"REWRITE":  colorReview,
	"COMPLETE": colorSuccess,
	"ERROR":    colorError,
}

// Hiển thị trạng thái: icon + nhãn tiếng Việt. Nhất quán với chủ đề gam ấm tổng thể,
// tránh khối màu đặc trông lạc lõng.
// Icon của RUNNING để trống, được spinner frame điền động vào, tạo cảm giác
// chuyển động hòa vào chỉ báo trạng thái.
var statusDisplay = map[string]struct {
	icon  string
	label string
}{
	"READY":    {"○", "Sẵn sàng"},
	"RUNNING":  {"", "Đang chạy"},
	"REVIEW":   {"◆", "Xem xét"},
	"REWRITE":  {"◆", "Làm lại"},
	"COMPLETE": {"●", "Hoàn thành"},
	"PAUSED":   {"⏸", "Tạm dừng"},
	"PAUSING":  {"⏸", "Đang dừng"},
	"ERROR":    {"✕", "Lỗi"},
}

// Ánh xạ màu theo danh mục sự kiện
var categoryColors = map[string]lipgloss.AdaptiveColor{
	"DISPATCH": colorAccent,
	"DONE":     colorSuccess,
	"TOOL":     colorTool,
	"SYSTEM":   colorAccent,
	"USER":     colorAccent2,
	"REVIEW":   colorReview,
	"CHECK":    colorSuccess,
	"ERROR":    colorError,
	"AGENT":    colorMuted,
	"CONTEXT":  colorContext,
	"COMPACT":  colorContext,
}

// Các style cơ bản
var (
	baseBorder = lipgloss.RoundedBorder()

	topBarStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1)

	statusIconStyle = lipgloss.NewStyle().
			Bold(true)

	statusLabelStyle = lipgloss.NewStyle().
				Foreground(colorText)

	panelTitleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	fieldLabelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(10)

	// fieldValueStyle / cardContentStyle dùng bodyTextColor — các giá trị trong
	// khu vực tổng quan (trạng thái chạy, số chương đã hoàn thành, số từ, v.v.),
	// mục đề cương, danh sách nhân vật, tóm tắt chương và các "nội dung văn bản
	// trung tính" khác: trên nền tối theo màu tiền cảnh mặc định của terminal
	// (tránh ép trắng sữa đè lên theme), trên nền sáng dùng nâu đậm giữ cảm giác ấm.
	// Các phần tử mang ngữ nghĩa mạnh (tiêu đề, giá trị nổi bật, trạng thái,
	// lỗi, tô màu tỉ lệ trúng, v.v.) vẫn dùng màu chủ đề colorAccent/colorError.
	fieldValueStyle = lipgloss.NewStyle().Foreground(bodyTextColor)

	highlightValueStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	contextUsageMetaStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	cardTitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	cardContentStyle = lipgloss.NewStyle().Foreground(bodyTextColor)
)
