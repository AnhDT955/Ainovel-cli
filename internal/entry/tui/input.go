package tui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/voocel/ainovel-cli/internal/host"
)

// renderInputBox vẽ vùng nhập liệu ở phía dưới màn hình.
// Ô nhập chỉ chịu trách nhiệm nhập liệu và hiển thị gợi ý, không chứa thanh chế độ khởi động.
func renderInputBox(inputView, hints string, snap host.UISnapshot, outputDir string, width int) string {
	innerW := width - 4 // border + padding
	if innerW < 12 {
		innerW = 12
	}

	// Dòng nhập: ký hiệu nhắc + ô nhập liệu
	prompt := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("❯ ")
	inputLine := prompt + inputView

	// Dòng gợi ý: phím tắt bên trái, tiến độ bên phải
	info := buildRightInfo(snap, outputDir)
	line2 := joinInlineSides(hints, info, innerW)

	// Vùng nhập (một hộp duy nhất, tránh hiển thị hai ô nhập về mặt trực quan)
	inputStyle := lipgloss.NewStyle().
		Width(width).
		Border(baseBorder, true, false, true, false).
		BorderForeground(colorDim).
		Padding(0, 1)
	inputBlock := inputStyle.Render(inputLine)

	// Dòng gợi ý (không có viền, nằm sát ngay bên dưới đường ngang dưới)
	hintStyle := lipgloss.NewStyle().
		Width(width).
		Padding(0, 2)
	hintBlock := hintStyle.Render(line2)

	return inputBlock + "\n" + hintBlock + "\n"
}

// buildRightInfo xây dựng thông tin bên phải: nhà cung cấp · model(cửa sổ ngữ cảnh) · chi phí · thư mục.
// Thông tin tiến độ như chương/số từ được hiển thị ở bảng "tổng quan" bên trái, không lặp lại ở đây.
func buildRightInfo(snap host.UISnapshot, outputDir string) string {
	var parts []string

	if snap.Provider != "" {
		parts = append(parts, snap.Provider)
	}
	if snap.ModelName != "" {
		if w := formatContextWindow(snap.ModelContextWindow); w != "" {
			parts = append(parts, snap.ModelName+"("+w+")")
		} else {
			parts = append(parts, snap.ModelName)
		}
	}
	if cost := formatCostUSD(snap.TotalCostUSD); cost != "" {
		parts = append(parts, cost)
	}
	if outputDir != "" {
		parts = append(parts, "./"+filepath.Base(outputDir))
	}

	if len(parts) == 0 {
		return lipgloss.NewStyle().Foreground(colorDim).Render("READY")
	}
	return lipgloss.NewStyle().Foreground(colorDim).Render(strings.Join(parts, " · "))
}

func joinInlineSides(left, right string, width int) string {
	if width <= 0 {
		return left + right
	}
	if strings.TrimSpace(right) == "" {
		return fitInlineLine(left, width)
	}

	right = fitInlineLine(right, width)
	rightW := ansi.StringWidth(right)
	if rightW >= width {
		return right
	}

	leftMax := width - rightW - 1
	if leftMax < 0 {
		leftMax = 0
	}
	left = fitInlineLine(left, leftMax)
	gap := width - ansi.StringWidth(left) - rightW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func fitInlineLine(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	return ansi.Truncate(text, width, "...")
}
