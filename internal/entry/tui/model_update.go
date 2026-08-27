package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/utils"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTextarea()
		m.updateViewportSize()
		m.refreshDetailViewport()
		m.refreshStateViewport()
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	default:
		if next, cmd, handled := m.handleRuntimeMsg(msg); handled {
			return next, cmd
		}
		return m.handleTextareaMsg(msg)
	}
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.handleOverlayKeyMsg(msg); handled {
		return next, cmd
	}

	if msg.Type == tea.KeyCtrlC {
		if m.quitPending {
			return m, tea.Quit
		}
		m.quitPending = true
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return quitResetMsg{} })
	}
	m.quitPending = false

	if next, cmd, handled := m.handleCommandPaletteKey(msg); handled {
		return next, cmd
	}

	return m.handleBaseKeyMsg(msg)
}

func (m Model) handleOverlayKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case m.askState != nil:
		return m.handleBlockingModalKey(msg, m.handleAskUserKey)
	case m.cocreate != nil:
		return m.handleBlockingModalKey(msg, m.handleCoCreateKey)
	case m.help != nil:
		return m.handleBlockingModalKey(msg, m.handleHelpKey)
	case m.modelSwitch != nil:
		return m.handleBlockingModalKey(msg, m.handleModelSwitchKey)
	case m.report != nil:
		return m.handleBlockingModalKey(msg, m.handleReportKey)
	case m.importer != nil:
		return m.handleBlockingModalKey(msg, m.handleImportKey)
	case m.simulator != nil:
		return m.handleBlockingModalKey(msg, m.handleSimulationKey)
	case m.extractor != nil:
		return m.handleBlockingModalKey(msg, m.handleExtractKey)
	default:
		return m, nil, false
	}
}

func (m Model) handleBlockingModalKey(msg tea.KeyMsg, next func(tea.KeyMsg) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd, bool) {
	if msg.Type == tea.KeyCtrlC {
		if m.quitPending {
			return m, tea.Quit, true
		}
		m.quitPending = true
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return quitResetMsg{} }), true
	}
	m.quitPending = false
	// Phím tắt toàn cục xuyên modal: khi modal đang mở vẫn cần chuyển được chế độ báo chuột,
	// nếu không người dùng không thể kéo chọn và sao chép trong các modal khóa màn hình
	// như đồng sáng tác/help/report.
	if msg.Type == tea.KeyCtrlR {
		next, cmd := m.toggleMouseReporting()
		return next, cmd, true
	}
	model, cmd := next(msg)
	return model, cmd, true
}

// toggleMouseReporting chuyển đổi trạng thái báo chuột. Bật → Tắt để người dùng kéo chọn sao chép nguyên bản;
// Tắt → Bật khôi phục click chuyển focus / cuộn bánh xe. Dùng chung cho cả đường base và blocking modal.
func (m Model) toggleMouseReporting() (Model, tea.Cmd) {
	// Trang chào (modeNew) vốn không bật báo chuột, kéo nguyên bản là có thể sao chép; bỏ qua Ctrl+R ở đây,
	// tránh bật báo cáo nhầm làm hỏng tính năng sao chép nguyên bản. Báo chuột được bật bởi enterRunning khi vào bàn làm việc.
	if m.mode == modeNew {
		return m, nil
	}
	m.mouseOff = !m.mouseOff
	if m.mouseOff {
		return m, tea.DisableMouse
	}
	return m, tea.EnableMouseCellMotion
}

// enterRunning vào bàn làm việc sáng tác: bật báo chuột (bàn làm việc cần click chuyển panel / cuộn bánh xe /
// kéo thanh bên). Lệnh trả về cần được caller Batch vào giá trị trả về cuối cùng.
func (m *Model) enterRunning() tea.Cmd {
	m.mode = modeRunning
	m.mouseOff = false
	return tea.EnableMouseCellMotion
}

func (m Model) handleCommandPaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.compActive {
		return m, nil, false
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.clearCommandPalette()
		return m, nil, true
	case tea.KeyUp:
		if m.compIdx > 0 {
			m.compIdx--
		}
		return m, nil, true
	case tea.KeyDown:
		if m.compIdx < len(m.compItems)-1 {
			m.compIdx++
		}
		return m, nil, true
	case tea.KeyTab:
		m.acceptCommandCompletion()
		return m, nil, true
	case tea.KeyEnter:
		item, ok := m.acceptCommandCompletion()
		if !ok {
			return m, nil, true
		}
		if item.AutoExecute {
			m.textarea.Reset()
			next, cmd := m.handleSlashCommand(slashCommand{name: item.Name})
			return next, cmd, true
		}
		return m, nil, true
	default:
		return m, nil, false
	}
}

func (m Model) handleBaseKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Phòng thủ giới hạn tốc độ: dán \n trong terminal không hỗ trợ bracketed paste sẽ thoái hóa thành
	// các KeyEnter liên tiếp; người thật nhấn Enter và ký tự trước đó thường cách nhau > 100ms,
	// < 50ms rất có thể là mảnh vụn còn sót của luồng dán.
	// Chỉ ghi lại KeyRunes (luồng ký tự) — phím chức năng (↑↓/Tab/Ctrl-x) không nên làm bẩn giới hạn tốc độ,
	// nếu không người dùng lật lịch sử chọn xong ngay lập tức nhấn Enter sẽ bị nuốt nhầm.
	if msg.Type == tea.KeyRunes {
		m.lastKeyAt = time.Now()
	}
	switch msg.Type {
	case tea.KeyEscape:
		if m.mode == modeRunning && m.snapshot.IsRunning {
			return m, abortRuntime(m.runtime)
		}
		m.textarea.Reset()
		m.historyIdx = len(m.inputHistory)
		m.historyDraft = ""
		m.refitTextareaHeight()
		m.clearCommandPalette()
		return m, nil
	case tea.KeyCtrlL:
		m.resetOutputPanels()
		return m, nil
	case tea.KeyCtrlU:
		// Xóa nội dung nhập hiện tại; đồng thời thoát khỏi chế độ duyệt lịch sử.
		m.textarea.Reset()
		m.historyIdx = len(m.inputHistory)
		m.historyDraft = ""
		m.refitTextareaHeight()
		m.clearCommandPalette()
		return m, nil
	case tea.KeyCtrlR:
		return m.toggleMouseReporting()
	case tea.KeyTab:
		if m.mode == modeNew {
			if m.cocreate != nil {
				return m, nil
			}
			if m.startupMode == startupModeQuick {
				m.startupMode = startupModeCoCreate
			} else {
				m.startupMode = startupModeQuick
			}
			m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
			return m, nil
		}
		m.focusPane = (m.focusPane + 1) % focusPaneCount
		return m, nil
	case tea.KeyEnter:
		// Alt+Enter là xuống dòng chủ động, để textarea.Update xử lý (KeyMap.InsertNewline đã bind vào phím này).
		if msg.Alt {
			break
		}
		// Khoảng cách với lần nhấn phím không phải Enter trước đó quá ngắn → coi là mảnh vụn \n của luồng dán:
		// thay bằng dấu cách để giữ khoảng trắng trực quan, ngữ nghĩa nhất quán với đường cleanHumanKeyRunes ("abc\ndef" → "abc def").
		// Phòng thủ môi trường terminal bracketed paste bị vô hiệu (SSH cũ/một số cấu hình tmux).
		if !m.lastKeyAt.IsZero() && time.Since(m.lastKeyAt) < 50*time.Millisecond {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
			m.refitTextareaHeight()
			return m, cmd
		}
		return m.handleEnterKey()
	case tea.KeyUp:
		// Nhập nhiều dòng: để textarea xử lý di chuyển con trỏ trong dòng (rơi vào textarea.Update sau switch)
		if m.textareaIsMultiline() {
			break
		}
		// Một dòng: ưu tiên lật lịch sử, không có lịch sử khả dụng thì fallback cuộn luồng sự kiện
		if m.tryHistoryUp() {
			return m, nil
		}
		return m.handleVerticalScrollKey(msg, true)
	case tea.KeyDown:
		if m.textareaIsMultiline() {
			break
		}
		if m.tryHistoryDown() {
			return m, nil
		}
		return m.handleVerticalScrollKey(msg, false)
	case tea.KeyPgUp:
		return m.handleVerticalScrollKey(msg, true)
	case tea.KeyPgDown:
		return m.handleVerticalScrollKey(msg, false)
	case tea.KeyEnd:
		switch m.focusPane {
		case focusStream:
			m.streamScroll = true
			m.streamVP.GotoBottom()
		case focusDetail:
			m.detailVP.GotoBottom()
		case focusState:
			m.stateVP.GotoBottom()
		default:
			m.autoScroll = true
			m.viewport.GotoBottom()
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes && (containsSGRFragment(string(msg.Runes)) || isCSILeak(msg.Runes)) {
		return m, nil
	}
	var ok bool
	if msg, ok = cleanHumanKeyRunes(msg); !ok {
		return m, nil
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	m.updateCommandPalette()
	return m, cmd
}

func (m Model) handleEnterKey() (tea.Model, tea.Cmd) {
	text := utils.CleanInputLine(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	m.clearCommandPalette()
	if cmd, ok := parseSlashCommand(text); ok {
		m.pushInputHistory(text)
		m.textarea.Reset()
		m.refitTextareaHeight()
		return m.handleSlashCommand(cmd)
	}

	m.pushInputHistory(text)
	m.textarea.Reset()
	m.refitTextareaHeight()
	return m.applyComposedInput(text)
}

// applyComposedInput định tuyến một đoạn văn bản đầu vào đã sẵn sàng (do người dùng gõ tay
// hoặc do /load nạp từ file) tới hành động phù hợp theo trạng thái hiện tại.
// Tách riêng để lệnh Enter và /load dùng chung, tránh lặp lại logic phân nhánh theo mode.
func (m Model) applyComposedInput(text string) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeNew:
		m.err = nil
		if m.startupMode == startupModeQuick {
			plan, err := startup.PrepareQuick(startup.Request{
				Mode:        startup.ModeQuick,
				UserPrompt:  text,
				OutputDir:   m.runtime.Dir(),
				Interactive: true,
			})
			if err != nil {
				m.err = err
				return m, nil
			}
			return m, startRuntime(m.runtime, plan)
		}
		m.cocreate = newCoCreateState(text)
		return m, m.sendCoCreate()
	case modeRunning:
		// Không hiển thị lại sự kiện USER cục bộ — điểm vào Host.Continue/Steer đã emit sự kiện "USER",
		// đi qua kênh events trở về TUI. Kiến trúc §2.3: tầng quan sát chỉ quan sát, không tạo ra thực tế.
		if !m.snapshot.IsRunning {
			return m, continueRuntime(m.runtime, text)
		}
		return m, steerRuntime(m.runtime, text)
	case modeDone:
		// Người dùng nhập sau khi hoàn thành (yêu cầu làm lại/tiếp tục viết): kích hoạt vòng chạy mới.
		// Continue ở trạng thái dừng đi qua Inject tự động khôi phục, Điều phối viên nhận [can thiệp người dùng]
		// rồi định tuyến theo coordinator.md — nếu yêu cầu làm lại chương đã viết thì gọi reopen_book
		// mở lại sách vào trạng thái làm lại. Chuyển về modeRunning vào lại bàn làm việc;
		// khi vòng này chạy xong doneMsg(complete) sẽ đặt lại modeDone. Lệnh slash đã xử lý ở trên, không qua nhánh này.
		m.mode = modeRunning
		return m, continueRuntime(m.runtime, text)
	default:
		return m, nil
	}
}

func (m Model) handleVerticalScrollKey(msg tea.KeyMsg, upward bool) (tea.Model, tea.Cmd) {
	if m.focusPane == focusStream {
		if upward {
			m.streamScroll = false
		}
		var cmd tea.Cmd
		m.streamVP, cmd = m.streamVP.Update(msg)
		if !upward && m.streamVP.AtBottom() {
			m.streamScroll = true
		}
		return m, cmd
	}
	if m.focusPane == focusDetail {
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	if m.focusPane == focusState {
		var cmd tea.Cmd
		m.stateVP, cmd = m.stateVP.Update(msg)
		return m, cmd
	}
	if upward {
		m.autoScroll = false
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	if !upward && m.viewport.AtBottom() {
		m.autoScroll = true
	}
	return m, cmd
}

func (m Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.cocreate != nil {
		// Phân luồng chuột theo tọa độ X: nửa trái màn hình = panel conv, nửa phải = panel prompt.
		// Modal căn giữa và conv chiếm ~58% bên trái, dùng đường giữa màn hình để phân biệt là đủ chính xác.
		// Người dùng cuộn bánh xe trong vùng conv sẽ tự động dừng follow (để có thể dừng ổn định ở một vị trí lịch sử nào đó).
		var cmd tea.Cmd
		if msg.X < m.width/2 {
			m.cocreate.convFollow = false
			m.cocreate.convVP, cmd = m.cocreate.convVP.Update(msg)
			if m.cocreate.convVP.AtBottom() {
				m.cocreate.convFollow = true
			}
		} else {
			m.cocreate.promptVP, cmd = m.cocreate.promptVP.Update(msg)
		}
		return m, cmd
	}
	if m.modelSwitch != nil || m.askState != nil {
		return m, nil
	}
	if pane, ok := m.paneAtMouse(msg.X, msg.Y); ok {
		m.hoverPane = pane
		m.hoverActive = true
		if msg.Action == tea.MouseActionPress {
			m.focusPane = pane
		}
	} else {
		m.hoverActive = false
	}

	var cmd tea.Cmd
	if m.focusPane == focusStream {
		m.streamVP, cmd = m.streamVP.Update(msg)
		if msg.Action == tea.MouseActionPress {
			m.streamScroll = m.streamVP.AtBottom()
		}
		return m, cmd
	}
	if m.focusPane == focusDetail {
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	if m.focusPane == focusState {
		m.stateVP, cmd = m.stateVP.Update(msg)
		return m, cmd
	}
	m.viewport, cmd = m.viewport.Update(msg)
	if msg.Action == tea.MouseActionPress {
		m.autoScroll = m.viewport.AtBottom()
	}
	return m, cmd
}

func (m Model) handleRuntimeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case eventMsg:
		ev := host.Event(msg)
		m.applyEvent(ev)
		m.refreshEventViewport()
		return m, listenEvents(m.runtime), true
	case bootstrapMsg:
		// Phát lại lịch sử sự kiện trước khi xử lý lỗi: Resume bị từ chối (như vượt ngân sách) là đường bình thường,
		// người dùng cần đọc lý do từ chối trong khi có thể nhìn thấy lịch sử, không phải đối mặt với luồng sự kiện trống.
		m.applyRuntimeReplay(msg.replay)
		if msg.err != nil {
			m.err = msg.err
			return m, fetchSnapshot(m.runtime), true
		}
		if msg.resumed && m.mode == modeNew {
			enableMouse := m.enterRunning()
			m.resizeTextarea()
			m.textarea.Placeholder = defaultSteerPlaceholder()
			return m, tea.Batch(fetchSnapshot(m.runtime), enableMouse), true
		}
		return m, fetchSnapshot(m.runtime), true
	case askUserMsg:
		m.askState = newAskUserState(askUserRequest(msg))
		m.textarea.Blur()
		m.applyEvent(host.Event{
			Time: time.Now(), Category: "SYSTEM", Summary: "Đang chờ người dùng bổ sung thông tin quan trọng", Level: "info",
		})
		m.refreshEventViewport()
		return m, listenAskUser(m.askBridge), true
	case snapshotMsg:
		m.snapshot = host.UISnapshot(msg)
		m.syncRuntimePlaceholder()
		m.refreshEventViewport()
		m.refreshStreamViewport()
		m.refreshDetailViewport()
		m.refreshStateViewport()
		return m, tickSnapshot(m.runtime), true
	case doneMsg:
		m.snapshot.IsRunning = false
		m.refreshEventViewport()
		m.refreshStreamViewport()
		m.refreshStateViewport()
		if msg.complete {
			m.abortPending = false
			m.mode = modeDone
			// Trạng thái hoàn thành không khóa ô nhập: dừng tự động tiếp tục viết, nhưng người dùng vẫn có thể
			// nhập yêu cầu làm lại (nhập ở modeDone đi qua Continue kích hoạt vòng chạy mới,
			// Điều phối viên định tuyến đến reopen_book), các lệnh /export, /model
			// cũng cần dùng được, ô nhập phải giữ focus (issue #27, #38).
			m.textarea.Placeholder = "Sáng tác đã hoàn thành · Có thể nhập yêu cầu làm lại (vd: \"Viết lại chương 3\"), /export để xuất truyện, hoặc nhập / để xem lệnh"
			return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime), m.textarea.Focus()), true
		}
		if m.abortPending {
			m.abortPending = false
			m.snapshot.RuntimeState = "paused"
			m.syncRuntimePlaceholder()
		} else {
			m.textarea.Placeholder = "Chạy bị gián đoạn, nhập bất kỳ nội dung gì để tiếp tục sáng tác"
		}
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime)), true
	case abortResultMsg:
		if msg.stopped {
			m.abortPending = true
			m.textarea.Placeholder = "Đang tạm dừng sáng tác..."
		}
		return m, nil, true
	case reportLoadedMsg:
		if m.report == nil || msg.reqID != m.report.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.report.load(msg.report, paddedModalContentWidth(boxW), msg.exportPath, msg.finishedAt)
		return m, nil, true
	case importEventMsg:
		if m.importer == nil || msg.reqID != m.importer.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.importer.appendEvent(msg.ev, paddedModalContentWidth(boxW))
		if msg.ev.Stage == imp.StageError {
			return m, nil, true
		}
		if msg.ev.Stage == imp.StageDone {
			// Nhập truyện thành công → tự động tiếp nối tiếp tục viết: Resume sẽ bật Router và gửi lệnh đầu tiên,
			// đi qua đúng luồng tiếp tục viết như "mở lại dự án khôi phục" (bù đắp kết nối nhập→tiếp tục trong cùng phiên).
			// bootstrapMsg tiếp theo sẽ enterRunning() chuyển sang trạng thái sáng tác.
			return m, bootstrapRuntime(m.runtime), true
		}
		return m, listenImportEvent(msg.reqID, msg.ch), true
	case simEventMsg:
		if m.simulator == nil || msg.reqID != m.simulator.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.simulator.appendEvent(msg.ev, paddedModalContentWidth(boxW))
		if msg.terminal() {
			return m, nil, true
		}
		return m, listenSimulationEvent(msg.reqID, msg.ch), true
	case extractEventMsg:
		if m.extractor == nil || msg.reqID != m.extractor.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.extractor.appendEvent(msg.ev, paddedModalContentWidth(boxW))
		if msg.terminal() {
			// Xong (thành công → review gate chờ Enter, hoặc lỗi → chờ Esc): ngừng lắng nghe.
			return m, nil, true
		}
		return m, listenExtractEvent(msg.reqID, msg.ch), true
	case exportDoneMsg:
		if msg.err != nil {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: "Xuất truyện thất bại: " + msg.err.Error(), Level: "error",
			})
		} else if msg.result != nil {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "SYSTEM", Summary: formatExportSuccess(msg.result), Level: "success",
			})
		}
		m.refreshEventViewport()
		return m, nil, true
	case startResultMsg:
		next, cmd := m.handleStartResultMsg(msg)
		return next, cmd, true
	case cocreateDeltaMsg:
		if m.cocreate == nil || msg.reqID != m.cocreate.reqID {
			return m, nil, true
		}
		m.cocreate.applyDelta(msg.kind, msg.text)
		return m, listenCoCreateDelta(m.cocreate), true
	case cocreateDoneMsg:
		next, cmd := m.handleCoCreateDoneMsg(msg)
		return next, cmd, true
	case steerResultMsg:
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime)), true
	case continueResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: msg.err.Error(), Level: "error",
			})
			m.refreshEventViewport()
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus()), true
		}
		m.err = nil
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime), m.textarea.Focus()), true
	case spinnerTickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		if m.snapshot.IsRunning {
			// Làm mới hiển thị spinner ngôi sao / thanh trên (350ms) đều đi qua đây
			m.refreshEventViewport()
		}
		return m, tickSpinner(), true
	case toolSpinnerTickMsg:
		m.toolSpinnerIdx = (m.toolSpinnerIdx + 1) % len(toolSpinnerFrames)
		// Làm mới spinner của dòng "đang tiến hành" trong luồng sự kiện (150ms, nhịp độc lập).
		// Khung spinner chỉ ảnh hưởng đến dòng sự kiện đang chạy, các dòng đã hoàn thành có đầu ra byte-for-byte như nhau;
		// khi không có sự kiện đang chạy thì toàn bộ việc render lại là vô nghĩa, bỏ qua.
		if m.snapshot.IsRunning && m.hasRunningEvent() {
			m.refreshEventViewport()
		}
		return m, tickToolSpinner(), true
	case cursorTickMsg:
		m.cursorIdx++
		if m.snapshot.IsRunning {
			// Nhấp nháy con trỏ cần render lại toàn bộ panel luồng (con trỏ nằm ở cuối content);
			// tiện thể xóa luôn dirty, flush tick ngay sau không cần lặp lại.
			m.refreshStreamViewport()
			m.streamDirty = false
		}
		return m, tickCursor(), true
	case streamDeltaMsg:
		if len(m.streamRounds) == 0 {
			m.streamRounds = append(m.streamRounds, "")
		}
		m.streamRounds[len(m.streamRounds)-1] += string(msg)
		// Không refreshStreamViewport ngay lập tức, để streamFlushTick gộp làm mới ở 60fps.
		// Khi LLM stream tốc độ cao mỗi giây hàng chục token, làm mới từng cái là mỗi giây hàng chục lần render lại toàn bộ 32 đoạn.
		m.streamDirty = true
		return m, listenStream(m.runtime), true
	case streamClearMsg:
		// Ranh giới round: flush hết delta đã tích lũy trước, round mới mới có thể căn chỉnh hiển thị
		if m.flushStreamIfDirty() && m.streamScroll {
			m.streamVP.GotoBottom()
		}
		if len(m.streamRounds) == 0 {
			m.streamRounds = append(m.streamRounds, "")
		} else if strings.TrimSpace(m.streamRounds[len(m.streamRounds)-1]) != "" {
			m.streamRounds = append(m.streamRounds, "")
		}
		m.trimStreamRounds()
		m.streamRound = len(m.streamRounds)
		m.refreshStreamViewport()
		if m.streamScroll {
			m.streamVP.GotoBottom()
		}
		return m, listenStream(m.runtime), true
	case streamFlushTickMsg:
		if m.flushStreamIfDirty() && m.streamScroll {
			m.streamVP.GotoBottom()
		}
		return m, tickStreamFlush(), true
	case quitResetMsg:
		m.quitPending = false
		return m, nil, true
	default:
		return m, nil, false
	}
}

func (m Model) handleStartResultMsg(msg startResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		if m.mode != modeNew {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: msg.err.Error(), Level: "error",
			})
			m.refreshEventViewport()
		}
		if m.cocreate != nil {
			m.cocreate.awaiting = false
			m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus())
		}
		if m.mode == modeNew {
			m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus())
		}
		return m, fetchSnapshot(m.runtime)
	}

	if m.mode == modeNew {
		m.cocreate = nil
		enableMouse := m.enterRunning()
		m.resizeTextarea()
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus(), enableMouse)
	}

	return m, fetchSnapshot(m.runtime)
}

func (m Model) handleCoCreateDoneMsg(msg cocreateDoneMsg) (tea.Model, tea.Cmd) {
	if m.cocreate == nil || msg.reqID != m.cocreate.reqID {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		m.cocreate.awaiting = false
		m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
		return m, m.textarea.Focus()
	}
	m.err = nil
	m.cocreate.apply(msg.reply)
	m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
	return m, m.textarea.Focus()
}

func (m Model) handleTextareaMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	m.updateCommandPalette()
	return m, cmd
}

// applyEvent áp dụng một sự kiện vào m.events:
// - Có ID và đã tồn tại → cập nhật tại chỗ (gộp các trường trạng thái hoàn thành, giữ nguyên Time / Summary lần đầu)
// - Sự kiện mới → thêm vào, ghi vào eventIndex nếu cần
// - Vượt quá maxEvents thì cắt bớt dạng trượt và xây lại chỉ mục
func (m *Model) applyEvent(ev host.Event) {
	if ev.ID != "" {
		if idx, ok := m.eventIndex[ev.ID]; ok && idx >= 0 && idx < len(m.events) {
			existing := &m.events[idx]
			if !ev.FinishedAt.IsZero() {
				existing.FinishedAt = ev.FinishedAt
			}
			if ev.Duration > 0 {
				existing.Duration = ev.Duration
			}
			if ev.Failed {
				existing.Failed = true
			}
			if ev.Level != "" {
				existing.Level = ev.Level
			}
			// Cho phép ghi đè Summary khi không rỗng (trạng thái kết thúc có thể mang thông tin bổ sung); nếu không thì giữ nguyên lần đầu
			if ev.Summary != "" {
				existing.Summary = ev.Summary
			}
			return
		}
	}

	m.events = append(m.events, ev)
	if ev.ID != "" {
		m.eventIndex[ev.ID] = len(m.events) - 1
	}
	if len(m.events) > maxEvents {
		drop := len(m.events) - maxEvents
		m.events = m.events[drop:]
		m.rebuildEventIndex()
	}
}

// trimStreamRounds cắt bớt streamRounds xuống còn maxStreamRounds đoạn; phần vượt quá bị bỏ từ đầu.
// Thời điểm gọi: sau mỗi lần streamClear mở vòng mới, và sau khi replay đã nạp xong tất cả mục lịch sử.
func (m *Model) trimStreamRounds() {
	if len(m.streamRounds) <= maxStreamRounds {
		return
	}
	drop := len(m.streamRounds) - maxStreamRounds
	m.streamRounds = m.streamRounds[drop:]
}

func (m *Model) rebuildEventIndex() {
	m.eventIndex = make(map[string]int, len(m.events))
	for i, e := range m.events {
		if e.ID != "" {
			m.eventIndex[e.ID] = i
		}
	}
}

func (m *Model) resetOutputPanels() {
	m.events = nil
	m.eventIndex = make(map[string]int)
	m.viewport.SetContent("")
	m.viewport.GotoTop()
	m.streamBuf.Reset()
	m.streamRounds = nil
	m.streamVP.SetContent("")
	m.streamVP.GotoTop()
	m.streamRound = 0
}

func (m *Model) applyRuntimeReplay(items []domain.RuntimeQueueItem) {
	for _, item := range items {
		switch item.Kind {
		case domain.RuntimeQueueUIEvent:
			// Luồng sự kiện không phát lại: trong hàng đợi chỉ có sự kiện trạng thái hoàn thành,
			// và các trường cần để render như Agent/Depth/Duration/Level không được khôi phục theo replay,
			// các dòng ra sẽ thiếu sót. Thà để panel trống còn hơn có dữ liệu nửa vời.
			continue
		case domain.RuntimeQueueStreamClear:
			if len(m.streamRounds) == 0 {
				m.streamRounds = append(m.streamRounds, "")
			} else if strings.TrimSpace(m.streamRounds[len(m.streamRounds)-1]) != "" {
				m.streamRounds = append(m.streamRounds, "")
			}
		case domain.RuntimeQueueStreamDelta:
			text := host.ReplayDeltaText(item)
			if text == "" {
				continue
			}
			if len(m.streamRounds) == 0 {
				m.streamRounds = append(m.streamRounds, "")
			}
			m.streamRounds[len(m.streamRounds)-1] += text
		}
	}
	m.trimStreamRounds()
	m.streamRound = len(m.streamRounds)
	m.refreshEventViewport()
	m.refreshStreamViewport()
}
