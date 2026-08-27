package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/logger"
)

// Run khởi động TUI.
// Quy ước phân tầng chế độ khởi động:
// 1. Chế độ nhanh, chế độ đồng sáng tác thuộc “biên soạn khởi động”;
// 2. Phiên sáng tác chính thức vào host.Host;
// 3. Tương lai nếu thêm các chế độ dùng chung như “tiếp tục tiểu thuyết có sẵn”, thống nhất đặt vào internal/entry/startup.
func Run(cfg bootstrap.Config, bundle assets.Bundle, version string) (err error) {
	// TUI chạy trong alt-screen: panic ở đây không đi qua die() nên cần ghi lại tại chỗ.
	defer func() {
		if r := recover(); r != nil {
			logger.WritePanic("tui", r)
			panic(r)
		}
		logger.WriteErr("tui", err)
	}()

	rt, err := host.New(cfg, bundle)
	if err != nil {
		return err
	}
	bridge := newAskUserBridge()
	rt.AskUser().SetHandler(bridge.handler)
	cleanup := logger.SetupFile(rt.Dir(), "tui.log", false)
	defer cleanup()
	defer rt.Close()

	m := NewModel(rt, bridge, version)
	// Không bật báo cáo chuột toàn cục khi khởi động: trang chào mừng không cần chuột,
	// tắt báo cáo giúp giữ nguyên tính năng kéo-chọn-sao chép gốc của terminal.
	// Khi vào bàn làm việc sáng tác (modeRunning), enterRunning sẽ bật báo cáo,
	// để hỗ trợ nhấp chuyển panel / cuộn chuột / kéo thanh bên.
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
