package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/internal/host"
)

type slashCommandSpec struct {
	Name        string
	Aliases     []string
	Group       string
	Usage       string
	Description string
	AutoExecute bool
	Hidden      bool
	NeedsIdle   bool
	Run         func(m Model, args []string) (tea.Model, tea.Cmd)
}

type slashCommand struct {
	name string
	args []string
}

func parseSlashCommand(text string) (slashCommand, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return slashCommand{}, false
	}
	fields := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(fields) == 0 {
		return slashCommand{}, false
	}
	return slashCommand{name: strings.ToLower(fields[0]), args: fields[1:]}, true
}

func (s slashCommandSpec) matches(name string) bool {
	if s.Name == name {
		return true
	}
	for _, alias := range s.Aliases {
		if strings.EqualFold(alias, name) {
			return true
		}
	}
	return false
}

func commandRegistryInstance() commandRegistry {
	return newCommandRegistry([]slashCommandSpec{
		{
			Name:        "help",
			Group:       "system",
			Usage:       "/help",
			Description: "Xem danh sách lệnh",
			AutoExecute: true,
			Run: func(m Model, _ []string) (tea.Model, tea.Cmd) {
				m.help = newHelpState(m.width, m.height)
				m.textarea.Blur()
				return m, nil
			},
		},
		{
			Name:        "model",
			Group:       "system",
			Usage:       "/model [role]",
			Description: "Chuyển đổi mô hình mặc định hoặc theo vai trò",
			AutoExecute: true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				roleHint := ""
				if len(args) > 0 {
					roleHint = args[0]
					if normalizeRoleKey(roleHint) == "" {
						m.applyEvent(host.Event{
							Time: time.Now(), Category: "ERROR", Summary: "Vai trò không xác định: " + roleHint, Level: "error",
						})
						m.refreshEventViewport()
						return m, nil
					}
				}
				m.modelSwitch = newModelSwitchState(m.runtime, roleHint)
				m.textarea.Blur()
				return m, nil
			},
		},
		{
			Name:        "diag",
			Group:       "analysis",
			Usage:       "/diag",
			Description: "Chẩn đoán tình trạng sáng tác tiểu thuyết",
			AutoExecute: true,
			Run: func(m Model, _ []string) (tea.Model, tea.Cmd) {
				m.reportSeq++
				m.report = newReportState(m.width, m.height, m.reportSeq, time.Now())
				m.textarea.Blur()
				return m, loadReport(m.runtime.Dir(), m.reportSeq)
			},
		},
		{
			Name:        "import",
			Group:       "writing",
			Usage:       "/import <path> [from=N]",
			Description: "Nhập truyện bên ngoài để tiếp tục viết",
			NeedsIdle:   true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				m.importSeq++
				state, listenCmd, err := startImport(m.runtime, m.importSeq, args, m.width, m.height)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Khởi động nhập truyện thất bại: " + err.Error(), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.importer = state
				m.textarea.Blur()
				return m, listenCmd
			},
		},
		{
			Name:        "load",
			Group:       "writing",
			Usage:       "/load <path.md>",
			Description: "Đọc file yêu cầu, trích xuất + xem coverage rồi bắt đầu sáng tác",
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				if m.mode != modeNew {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "/load chỉ dùng khi bắt đầu truyện mới", Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				path, err := parseLoadPath(args)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Nạp yêu cầu từ file thất bại: " + err.Error(), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.extractSeq++
				state, listenCmd, err := startExtract(m.runtime, m.extractSeq, path, m.width, m.height)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Khởi động trích xuất yêu cầu thất bại: " + err.Error(), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.extractor = state
				m.textarea.Blur()
				return m, listenCmd
			},
		},
		{
			Name:        "cocreate",
			Aliases:     []string{"plan"},
			Group:       "writing",
			Usage:       "/cocreate",
			Description: "Tạm dừng sáng tác, đồng sáng tác lên kế hoạch cho các giai đoạn tiếp theo",
			AutoExecute: true,
			Run: func(m Model, _ []string) (tea.Model, tea.Cmd) {
				if m.mode != modeRunning {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Đồng sáng tác giai đoạn chỉ khả dụng khi đang sáng tác", Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				if !m.runtime.PauseForCoCreate() {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Không thể vào đồng sáng tác giai đoạn: toàn bộ tác phẩm đã hoàn thành hoặc đang trong quá trình đồng sáng tác", Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.cocreate = newStageCoCreateState()
				m.resizeTextarea()
				m.textarea.Blur()
				return m, m.sendCoCreate()
			},
		},
		{
			Name:        "simulate",
			Group:       "writing",
			Usage:       "/simulate",
			Description: "Đọc ./simulate để tạo hoặc cập nhật tăng dần hồ sơ mô phỏng phong cách viết",
			NeedsIdle:   true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				m.simSeq++
				state, listenCmd, err := startSimulate(m.runtime, m.simSeq, args, m.width, m.height)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Khởi động hồ sơ mô phỏng phong cách viết thất bại: " + err.Error(), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.simulator = state
				m.textarea.Blur()
				return m, listenCmd
			},
		},
		{
			Name:        "importsim",
			Group:       "writing",
			Usage:       "/importsim <profile.json>",
			Description: "Nhập hồ sơ mô phỏng phong cách có sẵn và hợp nhất theo dấu vân tay ngữ liệu",
			NeedsIdle:   true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				m.simSeq++
				state, listenCmd, err := startImportSimulation(m.runtime, m.simSeq, args, m.width, m.height)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Nhập hồ sơ mô phỏng phong cách thất bại: " + err.Error(), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.simulator = state
				m.textarea.Blur()
				return m, listenCmd
			},
		},
		{
			Name:        "export",
			Group:       "writing",
			Usage:       "/export [txt|epub] [path] [from=N] [to=M] [--overwrite]",
			Description: "Xuất truyện các chương đã hoàn thành sang TXT/EPUB (mặc định TXT; suy ra từ hậu tố path nếu không nêu định dạng)",
			AutoExecute: true,
			Run: func(m Model, args []string) (tea.Model, tea.Cmd) {
				cmd, err := startExport(m.runtime, args)
				if err != nil {
					m.applyEvent(host.Event{
						Time: time.Now(), Category: "ERROR", Summary: "Khởi động xuất truyện thất bại: " + err.Error(), Level: "error",
					})
					m.refreshEventViewport()
					return m, nil
				}
				m.applyEvent(host.Event{
					Time: time.Now(), Category: "SYSTEM", Summary: "Đang xuất truyện...", Level: "info",
				})
				m.refreshEventViewport()
				return m, cmd
			},
		},
	})
}

func commandSpecs() []slashCommandSpec {
	return commandRegistryInstance().Visible()
}

// parseLoadPath khôi phục đường dẫn file từ đối số của /load.
// Vì parseSlashCommand tách đối số theo khoảng trắng, đường dẫn chứa dấu cách sẽ đến đây
// dưới dạng nhiều phần tử; ghép lại rồi bỏ ngoặc kép/nháy bao ngoài để khôi phục path gốc.
// Việc đọc và giải mã file do Requirement Extractor (gói req) đảm nhận.
func parseLoadPath(args []string) (string, error) {
	path := strings.Trim(strings.TrimSpace(strings.Join(args, " ")), "\"'")
	if path == "" {
		return "", fmt.Errorf("thiếu đường dẫn, dùng: /load <path.md>")
	}
	return path, nil
}

func (m Model) handleSlashCommand(cmd slashCommand) (tea.Model, tea.Cmd) {
	spec, ok := commandRegistryInstance().Find(cmd.name)
	if !ok {
		m.applyEvent(host.Event{
			Time: time.Now(), Category: "ERROR", Summary: "Lệnh không xác định: /" + cmd.name, Level: "error",
		})
		m.refreshEventViewport()
		return m, nil
	}
	if spec.NeedsIdle && m.snapshot.IsRunning {
		m.applyEvent(host.Event{
			Time: time.Now(), Category: "ERROR", Summary: "Lệnh chỉ có thể thực thi khi ở trạng thái rảnh: /" + spec.Name, Level: "error",
		})
		m.refreshEventViewport()
		return m, nil
	}
	return spec.Run(m, cmd.args)
}
