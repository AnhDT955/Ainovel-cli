package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/host/req"
)

// preserveDirective là chỉ thị bảo toàn ghép thêm vào brief khi bắt đầu sáng tác,
// đảm bảo ràng buộc "không đổi tên/không lược bỏ" luôn tới được Kiến trúc sư kể cả khi
// model extractor quên nhúng mục ràng buộc vào brief (phòng thủ ở tầng code).
const preserveDirective = "[Ràng buộc bảo toàn] Nội dung trên được trích xuất từ tài liệu yêu cầu chi tiết do người dùng cung cấp. Khi lập kế hoạch, PHẢI bảo toàn nguyên văn toàn bộ tên truyện, tên nhân vật, tên thế lực, tên hệ thống và các cấp độ đã liệt kê — không đổi tên, không lược bỏ. Được phép bổ sung chi tiết còn thiếu nhưng không được mâu thuẫn với thông tin đã cho."

// extractState là trạng thái modal của Requirement Extractor.
// Khác simulationState ở chỗ khi hoàn tất thành công sẽ vào "review gate":
// hiển thị brief + coverage và chờ người dùng bấm Enter để bắt đầu sáng tác, hoặc Esc để hủy.
type extractState struct {
	reqID      int
	source     string
	stage      req.Stage
	startedAt  time.Time
	finishedAt time.Time
	history    []extractLine
	err        error
	done       bool
	result     *req.Result
	cancel     context.CancelFunc
	viewport   viewport.Model
}

type extractLine struct {
	at      time.Time
	stage   req.Stage
	message string
	err     error
}

type extractEventMsg struct {
	reqID int
	ev    req.Event
	ch    <-chan req.Event
}

func (m extractEventMsg) terminal() bool {
	return m.ev.Stage == req.StageDone || m.ev.Stage == req.StageError
}

func newExtractState(reqID int, source string, width, height int, cancel context.CancelFunc) *extractState {
	boxW, boxH := reportModalSize(width, height)
	contentW := paddedModalContentWidth(boxW)
	vp := viewport.New(contentW, boxH-4)
	s := &extractState{
		reqID:     reqID,
		source:    source,
		stage:     req.StageRead,
		startedAt: time.Now(),
		cancel:    cancel,
		viewport:  vp,
	}
	s.refresh(contentW)
	return s
}

func (s *extractState) appendEvent(ev req.Event, contentW int) {
	s.stage = ev.Stage
	if ev.Err != nil {
		s.err = ev.Err
	}
	if ev.Result != nil {
		s.result = ev.Result
	}
	s.history = append(s.history, extractLine{at: ev.Time, stage: ev.Stage, message: ev.Message, err: ev.Err})
	if ev.Stage == req.StageDone || ev.Stage == req.StageError {
		s.done = true
		s.finishedAt = ev.Time
	}
	s.refresh(contentW)
}

// ready trả về true khi extractor hoàn tất thành công và có brief để bắt đầu sáng tác.
func (s *extractState) ready() bool {
	return s.done && s.err == nil && s.result != nil && strings.TrimSpace(s.result.Brief) != ""
}

func (s *extractState) refresh(contentW int) {
	titleStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	mutedStyle := lipgloss.NewStyle().Foreground(colorMuted)
	okStyle := lipgloss.NewStyle().Foreground(colorSuccess)
	errStyle := lipgloss.NewStyle().Foreground(colorError)
	stageStyle := lipgloss.NewStyle().Foreground(colorAccent2)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Trích xuất yêu cầu sáng tác"))
	b.WriteString("\n\n")
	if s.source != "" {
		b.WriteString(dimStyle.Render("Nguồn "))
		b.WriteString(s.source)
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("Bắt đầu "))
	b.WriteString(formatReportTime(s.startedAt))
	if !s.finishedAt.IsZero() {
		b.WriteString(dimStyle.Render("  Hoàn thành "))
		b.WriteString(formatReportTime(s.finishedAt))
	}
	b.WriteString("\n\n")

	b.WriteString(mutedStyle.Render("Giai đoạn "))
	b.WriteString(stageStyle.Render(string(s.stage)))
	b.WriteString("\n")
	for _, ln := range s.history {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(ln.at.Format("15:04:05")))
		b.WriteString(" ")
		b.WriteString(stageStyle.Render(string(ln.stage)))
		b.WriteString(" ")
		if ln.err != nil {
			// Lỗi trích xuất kèm phản hồi thô của model nên rất dài — phải wrap,
			// nếu không phần chẩn đoán quan trọng nhất bị cắt mất khỏi màn hình.
			b.WriteString(errStyle.Render(wrapText(ln.message+" - "+ln.err.Error(), contentW)))
		} else {
			b.WriteString(wrapText(ln.message, contentW))
		}
	}
	b.WriteString("\n\n")

	// Khi hoàn tất thành công: hiển thị báo cáo coverage + xem trước brief để BOSS duyệt.
	if s.ready() {
		res := s.result
		if strings.TrimSpace(res.Title) != "" {
			b.WriteString(titleStyle.Render("Tên truyện"))
			b.WriteString("\n")
			b.WriteString(wrapText(res.Title, contentW))
			b.WriteString("\n\n")
		}

		b.WriteString(titleStyle.Render("Coverage — đã ánh xạ"))
		b.WriteString(" ")
		b.WriteString(dimStyle.Render(fmt.Sprintf("(%d mục)", len(res.Coverage.Mapped))))
		b.WriteString("\n")
		for _, item := range res.Coverage.Mapped {
			b.WriteString(okStyle.Render("  ✓ "))
			b.WriteString(wrapText(item, contentW-4))
			b.WriteString("\n")
		}
		b.WriteString("\n")

		b.WriteString(titleStyle.Render("Coverage — còn thiếu / chưa chắc"))
		b.WriteString(" ")
		b.WriteString(dimStyle.Render(fmt.Sprintf("(%d mục)", len(res.Coverage.Missing))))
		b.WriteString("\n")
		if len(res.Coverage.Missing) == 0 {
			b.WriteString(okStyle.Render("  (không có — tài liệu đã được ánh xạ đầy đủ)"))
			b.WriteString("\n")
		}
		for _, item := range res.Coverage.Missing {
			b.WriteString(errStyle.Render("  ! "))
			b.WriteString(wrapText(item, contentW-4))
			b.WriteString("\n")
		}
		if strings.TrimSpace(res.Coverage.Notes) != "" {
			b.WriteString("\n")
			b.WriteString(dimStyle.Render("Ghi chú: "))
			b.WriteString(wrapText(res.Coverage.Notes, contentW))
			b.WriteString("\n")
		}
		b.WriteString("\n")

		b.WriteString(titleStyle.Render("Xem trước brief"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(wrapText(res.Brief, contentW)))
		b.WriteString("\n\n")
		b.WriteString(okStyle.Render("Enter Bắt đầu sáng tác"))
		b.WriteString(dimStyle.Render("   ·   Esc Hủy"))
	} else {
		switch {
		case !s.done:
			b.WriteString(dimStyle.Render("Esc Hủy"))
		case s.err != nil:
			b.WriteString(errStyle.Render("Trích xuất yêu cầu thất bại"))
			b.WriteString("\n")
			b.WriteString(dimStyle.Render("Esc Đóng bảng"))
		}
	}

	s.viewport.SetContent(b.String())
	if !s.done {
		s.viewport.GotoBottom()
	} else if s.ready() {
		s.viewport.GotoTop()
	}
}

func renderExtractModal(width, height int, s *extractState) string {
	if s == nil {
		return ""
	}
	boxW, boxH := reportModalSize(width, height)
	contentW := paddedModalContentWidth(boxW)
	if s.viewport.Width != contentW {
		s.viewport.Width = contentW
		s.refresh(contentW)
	}
	if s.viewport.Height != boxH-4 {
		s.viewport.Height = boxH - 4
	}
	hint := "  ↑↓ Cuộn · Enter Bắt đầu · Esc Hủy/Đóng"
	modal := renderPaddedModalFrame(boxW, boxH, "Trích xuất yêu cầu", hint, strings.Split(s.viewport.View(), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func (m Model) handleExtractKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.extractor == nil {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		// Chỉ có tác dụng ở review gate: bắt đầu sáng tác với brief đã trích xuất.
		if !m.extractor.ready() {
			return m, nil
		}
		brief := strings.TrimSpace(m.extractor.result.Brief)
		// Ghi brief xuống đĩa TRƯỚC khi chạy: từ đây trở đi nó là bản sao bền của yêu cầu người dùng
		// mà novel_context nạp lại ở mọi lượt. Tin nhắn chat bên dưới chỉ là bản mồi cho lượt đầu —
		// nó sẽ bị nén mất khi ngữ cảnh đầy, còn brief.md thì không.
		if err := m.runtime.SaveBrief(brief); err != nil {
			m.extractor = nil
			m.err = err
			return m, m.textarea.Focus()
		}
		text := brief + "\n\n" + preserveDirective
		plan, err := startup.PrepareQuick(startup.Request{
			Mode:        startup.ModeQuick,
			UserPrompt:  text,
			OutputDir:   m.runtime.Dir(),
			Interactive: true,
		})
		if err != nil {
			m.extractor = nil
			m.err = err
			return m, m.textarea.Focus()
		}
		m.extractor = nil
		return m, startRuntime(m.runtime, plan)
	case tea.KeyEsc:
		if !m.extractor.done && m.extractor.cancel != nil {
			m.extractor.cancel()
			return m, nil
		}
		m.extractor = nil
		return m, m.textarea.Focus()
	case tea.KeyUp:
		m.extractor.viewport.ScrollUp(1)
	case tea.KeyDown:
		m.extractor.viewport.ScrollDown(1)
	case tea.KeyPgUp:
		m.extractor.viewport.HalfPageUp()
	case tea.KeyPgDown:
		m.extractor.viewport.HalfPageDown()
	}
	return m, nil
}

// startExtract khởi động Requirement Extractor từ lệnh /load.
func startExtract(rt *host.Host, reqID int, path string, width, height int) (*extractState, tea.Cmd, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := rt.ExtractRequirement(ctx, path)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	state := newExtractState(reqID, path, width, height, cancel)
	return state, listenExtractEvent(reqID, ch), nil
}

func listenExtractEvent(reqID int, ch <-chan req.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return extractEventMsg{reqID: reqID, ev: ev, ch: ch}
	}
}
