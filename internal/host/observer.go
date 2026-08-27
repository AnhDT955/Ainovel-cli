package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/utils"
	"github.com/voocel/litellm"
)

// errorKind classifies a runtime error into a stable, short label for log
// filtering and alert routing. Returns "" when no special tag applies.
//
// err is the live error chain (may be nil after JSON serialization); msg is
// the rendered string fallback used when the chain has been flattened
// (e.g. inside sub-agent JSON results).
func errorKind(err error, msg string) string {
	if err != nil && litellm.IsStreamIdleError(err) {
		return "stream_idle"
	}
	if msg != "" && agentcore.IsStreamIdleMessage(msg) {
		return "stream_idle"
	}
	return ""
}

// Bộ đếm ID sự kiện tăng đơn điệu; kết hợp với timestamp để tạo ID ổn định.
var eventIDCounter uint64

func nextEventID() string {
	return fmt.Sprintf("e%d", atomic.AddUint64(&eventIDCounter, 1))
}

// activeCall ghi lại ID, thời điểm bắt đầu và summary của một lần gọi đang diễn ra (TOOL / DISPATCH).
// summary được điền vào Event hoàn thành để đảm bảo replay (runtime queue) có thể khôi phục nội dung dòng.
type activeCall struct {
	id      string
	start   time.Time
	summary string
	depth   int
}

// observer đăng ký luồng sự kiện của coordinator và chiếu sang kênh output của Host.
// Đây là observer thuần túy, không tham gia bất kỳ quyết định điều khiển nào.
type observer struct {
	unsub   func()
	emitEv  func(Event)
	emitD   func(string)
	emitC   func()
	store   *storepkg.Store // dùng cho lưu trữ runtime queue (ReplayQueue tiêu thụ)
	agents  map[string]*agentState
	agentMu sync.Mutex

	// aborting được Host đặt tại điểm vào Abort()/Close(), xóa tại Start/Resume/Continue.
	// Trong khi đang đặt, tất cả lỗi sự kiện phát sinh từ context-cancel bị chặn lại (vừa là mong đợi của người dùng,
	// vừa tránh trùng lặp với sự kiện "người dùng tạm dừng thủ công"). Lỗi thực sự (không phải cancel) vẫn báo cáo bình thường.
	aborting atomic.Bool

	streamThinking        bool
	lastThinkingByAgent   map[string]string          // agent → văn bản thinking tích lũy gần nhất (dùng để trích xuất delta tăng dần)
	dispatchStarts        map[string]*activeCall     // agent được dispatch → lần gọi DISPATCH đang diễn ra
	currentDispatchTarget string                     // tên subagent đang thực thi (Args có thể rỗng khi handleToolEnd)
	toolStarts            map[string]*activeCall     // agent → lần gọi TOOL đang diễn ra
	streamExtractors      map[string]*agentExtractor // agent → bộ trích xuất nội dung tham số JSON công cụ hiện tại
	streamHasContent      bool                       // streamRound hiện tại đã có nội dung output chưa (để quyết định có cần phân đoạn không)
	streamLastByte        byte                       // byte cuối cùng của output stream (dùng để bổ sung newline chính xác)
	// utf8Pending giữ các byte UTF-8 multi-byte chưa hoàn chỉnh từ delta trước, tách RIÊNG theo agent
	// và nối vào đầu delta tiếp theo CỦA CÙNG AGENT. Trước đây đây là một buffer dùng chung: khi nhiều
	// luồng đan xen (dispatch song song tasks[]/chain, hoặc coordinator + subagent), byte lẻ của agent
	// này bị ghép nhầm vào chữ của agent kia → ký tự sắp sai thứ tự trong từ. Tách theo agent giữ cho
	// việc nối byte luôn nằm trong đúng một luồng LLM.
	utf8Pending map[string][]byte
}

// agentExtractor ghi lại tên công cụ và thực thể extractor mà một agent đang trích xuất.
// Tên công cụ dùng để phát hiện "lần gọi công cụ mới đã bắt đầu", tránh cache bị nhiễm bẩn từ vòng trước.
type agentExtractor struct {
	tool       string
	ext        *jsonFieldExtractor
	emittedAny bool // extractor này đã tạo ra nội dung nào chưa; dùng để bổ sung phân đoạn trước lần output đầu tiên
}

type agentState struct {
	name    string
	state   string
	tool    string
	summary string
	turn    int
	context AgentContextSnapshot
	updated time.Time
}

func newObserver(coordinator *agentcore.Agent, s *storepkg.Store, emitEv func(Event), emitD func(string), emitC func()) *observer {
	o := &observer{
		emitEv:              emitEv,
		emitD:               emitD,
		emitC:               emitC,
		store:               s,
		agents:              make(map[string]*agentState),
		lastThinkingByAgent: make(map[string]string),
		dispatchStarts:      make(map[string]*activeCall),
		toolStarts:          make(map[string]*activeCall),
		streamExtractors:    make(map[string]*agentExtractor),
		utf8Pending:         make(map[string][]byte),
	}
	o.unsub = coordinator.Subscribe(o.handle)
	return o
}

func (o *observer) finalize() {
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	for _, a := range o.agents {
		a.state = "idle"
		a.tool = ""
	}
}

// setAborting được Host gọi tại các điểm chuyển vòng đời Abort/Close/Start, kiểm soát
// việc có cần chặn các sự kiện phát sinh từ "context canceled" hay không (tránh trùng với "người dùng tạm dừng thủ công").
func (o *observer) setAborting(v bool) { o.aborting.Store(v) }

// isCancellationNoise kiểm tra xem một lỗi có phải là nhiễu phát sinh từ abort hay không.
// Chỉ có ý nghĩa khi Host đang ở trạng thái aborting — context.Canceled ngoài abort
// có thể phản ánh vấn đề thực sự (ví dụ ctx bên ngoài bị hủy), vẫn cần báo cáo.
func (o *observer) isCancellationNoise(err error, msg string) bool {
	if !o.aborting.Load() {
		return false
	}
	if err != nil && errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(strings.ToLower(msg), "context canceled")
}

// emitAndLog dùng cho sự kiện "bắt đầu" của lần gọi: gửi cho TUI nhưng không ghi vào runtime queue,
// tránh replay tạo ra "một dòng bắt đầu, một dòng hoàn thành" trùng lặp. slog được host.emitEvent ghi chung.
func (o *observer) emitAndLog(ev Event) {
	o.emitEv(ev)
}

// persistEvent ghi sự kiện vào runtime queue (slog được host.emitEvent ghi chung).
func (o *observer) persistEvent(ev Event) {
	if o.store == nil || o.store.Runtime == nil {
		return
	}
	priority := domain.RuntimePriorityBackground
	switch ev.Category {
	case "SYSTEM", "ERROR":
		priority = domain.RuntimePriorityControl
	}
	_, _ = o.store.Runtime.AppendQueue(domain.RuntimeQueueItem{
		Time:     ev.Time,
		Kind:     domain.RuntimeQueueUIEvent,
		Priority: priority,
		Category: ev.Category,
		Summary:  ev.Summary,
		Payload:  ev,
	})
}

func (o *observer) handle(ev agentcore.Event) {
	switch ev.Type {
	case agentcore.EventToolExecStart:
		o.handleToolStart(ev)
	case agentcore.EventToolExecUpdate:
		o.handleToolUpdate(ev)
	case agentcore.EventToolExecEnd:
		o.handleToolEnd(ev)
	case agentcore.EventMessageUpdate:
		o.handleMessageUpdate(ev)
	case agentcore.EventMessageEnd:
		o.streamClear()
	case agentcore.EventTurnStart:
		if ev.Progress != nil && ev.Progress.Kind == agentcore.ProgressTurnCounter {
			o.updateAgent(ev.Progress.Agent, func(a *agentState) {
				a.turn = ev.Progress.Turn
			})
		}
	case agentcore.EventRetry:
		if ev.RetryInfo != nil {
			msg := ""
			if ev.RetryInfo.Err != nil {
				msg = ev.RetryInfo.Err.Error()
			}
			prefix := fmt.Sprintf("Thử lại (%d/%d): ", ev.RetryInfo.Attempt, ev.RetryInfo.MaxRetries)
			retryEv := Event{
				Time:     time.Now(),
				Category: "SYSTEM",
				Summary:  prefix + truncate(msg, 80),
				Detail:   prefix + msg,
				Kind:     errorKind(ev.RetryInfo.Err, msg),
				Level:    "warn",
			}
			o.emitEv(retryEv)
			o.persistEvent(retryEv)
		}
	case agentcore.EventError:
		if ev.Err != nil {
			fullMsg := ev.Err.Error()
			if o.isCancellationNoise(ev.Err, fullMsg) {
				// Lỗi ctx-cancel phát sinh từ abort thủ công của người dùng; đã có sự kiện "người dùng tạm dừng thủ công", không hiển thị lại.
				slog.Debug("suppressed cancel-derived error", "module", "agent", "msg", fullMsg)
				return
			}
			errEv := Event{
				Time:     time.Now(),
				Category: "ERROR",
				Summary:  truncate(fullMsg, 120),
				Detail:   fullMsg,
				Kind:     errorKind(ev.Err, fullMsg),
				Level:    "error",
			}
			o.emitEv(errEv)
			o.persistEvent(errEv)
		}
	}
}

func (o *observer) handleMessageUpdate(ev agentcore.Event) {
	if ev.Delta == "" {
		return
	}
	// Tham số tool-call của Coordinator là JSON nhiệm vụ gửi cho subagent, không có nội dung đọc được, bỏ qua.
	if ev.DeltaKind == agentcore.DeltaToolCall {
		return
	}
	o.emitStreamDelta(coordinatorStreamKey, ev.Delta, ev.DeltaKind == agentcore.DeltaThinking)
}

func (o *observer) handleToolStart(ev agentcore.Event) {
	if ev.Tool == "" {
		return
	}
	agent := agentFromEvent(ev)

	// Gọi subagent → sự kiện DISPATCH (đang diễn ra)
	if ev.Tool == "subagent" {
		sub := parseSubagentArgs(ev.Args)
		target := sub.agent
		if target == "" {
			target = "subagent"
		}
		dispatchSummary := target
		if sub.task != "" {
			firstLine := strings.TrimSpace(strings.SplitN(sub.task, "\n", 2)[0])
			if firstLine != "" {
				dispatchSummary += "（" + truncate(firstLine, 30) + "）"
			}
		}
		o.updateAgent(agent, func(a *agentState) {
			a.state = "working"
			a.tool = ev.Tool
			a.summary = fmt.Sprintf("%s → %s", agent, dispatchSummary)
		})
		o.currentDispatchTarget = target
		id := nextEventID()
		o.dispatchStarts[target] = &activeCall{id: id, start: time.Now(), summary: dispatchSummary}
		o.emitAndLog(Event{
			ID:       id,
			Time:     time.Now(),
			Category: "DISPATCH",
			Agent:    agent,
			Summary:  dispatchSummary,
			Level:    "info",
		})
		return
	}

	// Công cụ của coordinator (đang diễn ra)
	toolName := displayToolName(ev.Tool, ev.Args)
	o.updateAgent(agent, func(a *agentState) {
		a.state = "working"
		a.tool = ev.Tool
		a.summary = fmt.Sprintf("%s → %s", agent, toolName)
	})
	id := nextEventID()
	o.toolStarts[agent] = &activeCall{id: id, start: time.Now(), summary: toolName}
	o.emitAndLog(Event{
		ID:       id,
		Time:     time.Now(),
		Category: "TOOL",
		Agent:    agent,
		Summary:  toolName,
		Level:    "info",
	})
	o.emitFallbackStreamHeader(ev.Tool)
}

func (o *observer) handleToolUpdate(ev agentcore.Event) {
	if ev.Progress == nil {
		return
	}
	switch ev.Progress.Kind {
	case agentcore.ProgressToolDelta:
		if ev.Progress.Delta != "" {
			o.handleSubagentDelta(ev.Progress)
		}
	case agentcore.ProgressToolStart:
		// Lần gọi công cụ nội bộ của agent con (ví dụ writer → draft_chapter).
		// Lưu ý: dòng TOOL có thể đã được handleSubagentDelta phát trước trong giai đoạn nhận diện stream.
		// Ở đây: nếu đã phát → chỉ cập nhật summary (args đã đầy đủ, có thể hiển thị "tool(chương N)"); ngược lại phát bình thường.
		if ev.Progress.Agent == "" || ev.Progress.Tool == "" {
			break
		}
		toolName := displayToolName(ev.Progress.Tool, ev.Progress.Args)
		if call, ok := o.toolStarts[ev.Progress.Agent]; ok {
			if toolName != "" && toolName != call.summary {
				call.summary = toolName
				// Phát sự kiện cập nhật chỉ summary (cùng ID), TUI applyEvent sẽ merge
				o.emitEv(Event{
					ID:       call.id,
					Time:     call.start,
					Category: "TOOL",
					Agent:    ev.Progress.Agent,
					Summary:  toolName,
					Level:    "info",
					Depth:    call.depth,
				})
			}
			o.updateAgent(ev.Progress.Agent, func(a *agentState) {
				a.state = "working"
				a.tool = ev.Progress.Tool
				a.summary = fmt.Sprintf("%s → %s", ev.Progress.Agent, toolName)
			})
			break
		}
		// Chưa phát trước → luồng bình thường
		// (Model không stream tool args sẽ không kích hoạt ensureSubagentToolStarted,
		// fallback header phải được bổ sung trên đường dẫn này, nếu không read_chapter
		// và các công cụ không có extractor sẽ không có header ✻ trên panel stream, sát vào đoạn thinking trước.)
		id := nextEventID()
		o.toolStarts[ev.Progress.Agent] = &activeCall{id: id, start: time.Now(), summary: toolName, depth: 1}
		o.emitAndLog(Event{
			ID:       id,
			Time:     time.Now(),
			Category: "TOOL",
			Agent:    ev.Progress.Agent,
			Summary:  toolName,
			Level:    "info",
			Depth:    1,
		})
		o.updateAgent(ev.Progress.Agent, func(a *agentState) {
			a.state = "working"
			a.tool = ev.Progress.Tool
			a.summary = fmt.Sprintf("%s → %s", ev.Progress.Agent, toolName)
		})
		o.emitFallbackStreamHeader(ev.Progress.Tool)
	case agentcore.ProgressToolEnd:
		delete(o.streamExtractors, ev.Progress.Agent)
		if ev.Progress.Agent == "" {
			return
		}
		call, ok := o.toolStarts[ev.Progress.Agent]
		if !ok {
			return
		}
		delete(o.toolStarts, ev.Progress.Agent)
		// Sự kiện cập nhật cùng ID: TUI định vị dòng TOOL gốc theo ID, điền FinishedAt / Duration.
		// Summary / Depth cũng kèm theo để đảm bảo replay từ runtime queue có thể khôi phục dòng đầy đủ.
		finishEv := Event{
			ID:         call.id,
			Time:       call.start,
			FinishedAt: time.Now(),
			Category:   "TOOL",
			Agent:      ev.Progress.Agent,
			Summary:    call.summary,
			Level:      "info",
			Depth:      call.depth,
			Duration:   time.Since(call.start),
		}
		o.emitEv(finishEv)
		o.persistEvent(finishEv)
	case agentcore.ProgressThinking:
		o.handleThinkingProgress(ev)
	case agentcore.ProgressRetry:
		prefix := fmt.Sprintf("Thử lại (%d/%d): ", ev.Progress.Attempt, ev.Progress.MaxRetries)
		retryEv := Event{
			Time:     time.Now(),
			Category: "SYSTEM",
			Agent:    ev.Progress.Agent,
			Summary:  prefix + truncate(ev.Progress.Message, 80),
			Detail:   prefix + ev.Progress.Message,
			Kind:     errorKind(nil, ev.Progress.Message),
			Level:    "warn",
			Depth:    1,
		}
		o.emitEv(retryEv)
		o.persistEvent(retryEv)
	case agentcore.ProgressToolError:
		delete(o.streamExtractors, ev.Progress.Agent)
		msg := ev.Progress.Message
		if msg == "" {
			msg = "unknown error"
		}
		// Nếu có dòng TOOL đang diễn ra, đánh dấu thất bại tại chỗ; ngược lại thêm dòng ERROR độc lập.
		if call, ok := o.toolStarts[ev.Progress.Agent]; ok {
			delete(o.toolStarts, ev.Progress.Agent)
			finishEv := Event{
				ID:         call.id,
				Time:       call.start,
				FinishedAt: time.Now(),
				Failed:     true,
				Category:   "TOOL",
				Agent:      ev.Progress.Agent,
				Summary:    call.summary,
				Level:      "error",
				Depth:      call.depth,
				Duration:   time.Since(call.start),
			}
			o.emitEv(finishEv)
			o.persistEvent(finishEv)
		}
		// Thêm dòng chi tiết ERROR (bổ sung thông tin lỗi để dễ tra cứu)
		errEv := Event{
			Time:     time.Now(),
			Category: "ERROR",
			Agent:    ev.Progress.Agent,
			Summary:  fmt.Sprintf("%s lỗi: %s", ev.Progress.Tool, truncate(msg, 100)),
			Detail:   fmt.Sprintf("%s lỗi: %s", ev.Progress.Tool, msg),
			Kind:     errorKind(nil, msg),
			Level:    "error",
			Depth:    1,
		}
		o.emitEv(errEv)
		o.persistEvent(errEv)
	case agentcore.ProgressContext:
		o.handleContextProgress(ev)
	}
}

// handleSubagentDelta phân luồng văn bản và tham số tool-call của subagent:
// - DeltaText trực tiếp stream ra dưới dạng markdown
// - DeltaToolCall chỉ trích xuất trường nội dung dài của các công cụ đã biết (như draft_chapter.content);
//   tham số JSON của các công cụ khác đều bị bỏ qua
func (o *observer) handleSubagentDelta(p *agentcore.ProgressPayload) {
	if p.DeltaKind != agentcore.DeltaToolCall {
		o.emitStreamDelta(p.Agent, p.Delta, false)
		return
	}
	if p.Tool == "" {
		return // tên công cụ chưa sẵn sàng, thử lại ở delta tiếp theo
	}

	// Khi nhận diện được tên công cụ trong stream, phát sớm sự kiện TOOL đang diễn ra để spinner bao phủ
	// toàn bộ giai đoạn LLM sinh tool_call (thường chiếm 99% tổng thời gian gọi).
	// Khi ProgressToolStart thực sự đến, nếu toolStarts đã có bản ghi, chỉ bổ sung summary.
	o.ensureSubagentToolStarted(p.Agent, p.Tool)

	cur, ok := o.streamExtractors[p.Agent]
	// Sau khi args của cùng một tool call đã đóng (gặp } ở cấp cao nhất), vẫn có thể nhận thêm trailing delta:
	// Một số provider (thực đo trên deepseek-v4-flash) chia một args thành nhiều chunk,
	// chunk cuối cùng còn kèm khoảng trắng hoặc ký tự lặp sau `}`. Nếu xử lý theo kiểu
	// "tên công cụ khớp + Done thì xây lại", extractor mới lại emit thêm ✻ header và
	// phân tích phần đuôi token như args mới. Những delta này là rác thừa, bỏ qua.
	if ok && cur.tool == p.Tool && cur.ext.Done() {
		return
	}
	// Tên công cụ thay đổi hoặc chưa tạo: tạo mới.
	if !ok || cur.tool != p.Tool {
		ext := newToolExtractor(p.Tool)
		if ext == nil {
			delete(o.streamExtractors, p.Agent)
			return
		}
		cur = &agentExtractor{tool: p.Tool, ext: ext}
		o.streamExtractors[p.Agent] = cur
	}
	if emitted := cur.ext.Feed(p.Delta); emitted != "" {
		if !cur.emittedAny {
			cur.emittedAny = true
			// streamClear để ✻ header của extractor rơi vào điểm bắt đầu round mới, phối hợp với
			// renderStreamContent kiểm tra HasPrefix("✻") để đi đường renderAgentBlock được highlight;
			// dùng ensureStreamParagraphBreak chỉ chèn dòng trống không mở round mới, ✻ vẫn bị
			// bao bởi thinking/nội dung trước đó, rơi vào renderChapterBlock vẽ màu mặc định.
			o.streamClear()
			// streamClear đã xóa streamExtractors phòng thủ. cur hiện tại vẫn cần tiếp tục Feed
			// các delta tiếp theo của tool call này, phải đăng ký lại ngay; nếu không delta tiếp theo
			// sẽ tạo extractor mới, bắt đầu phân tích từ giữa args (vào psBeforeKey tại `{` của đối tượng lồng),
			// coi timeline_events.time / foreshadow_updates.id như trường cấp cao nhất,
			// TUI hiển thị thêm ✻ header lặp lại.
			o.streamExtractors[p.Agent] = cur
		}
		o.emitStreamDelta(p.Agent, emitted, false)
	}
}

func (o *observer) handleThinkingProgress(ev agentcore.Event) {
	agent := ev.Progress.Agent
	thinking := ev.Progress.Thinking
	if agent == "" || thinking == "" {
		return
	}

	prev := o.lastThinkingByAgent[agent]
	delta := thinking
	if strings.HasPrefix(thinking, prev) {
		delta = thinking[len(prev):]
	}
	o.lastThinkingByAgent[agent] = thinking
	if delta == "" {
		return
	}
	o.emitStreamDelta(agent, delta, true)
}

func (o *observer) handleContextProgress(ev agentcore.Event) {
	if ev.Progress == nil || len(ev.Progress.Meta) == 0 {
		return
	}
	var payload struct {
		Tokens        int     `json:"tokens"`
		ContextWindow int     `json:"context_window"`
		Percent       float64 `json:"percent"`
		Scope         string  `json:"scope"`
		Strategy      string  `json:"strategy"`
	}
	if json.Unmarshal(ev.Progress.Meta, &payload) != nil {
		return
	}

	agent := ev.Progress.Agent
	if agent == "" {
		agent = "coordinator"
	}

	// Cập nhật snapshot agent (thanh bên TUI luôn hiển thị)
	o.updateAgent(agent, func(a *agentState) {
		a.context = AgentContextSnapshot{
			Tokens:        payload.Tokens,
			ContextWindow: payload.ContextWindow,
			Percent:       payload.Percent,
			Scope:         payload.Scope,
			Strategy:      payload.Strategy,
		}
	})

	level := "info"
	if payload.Percent > 85 {
		level = "warn"
	}
	summary := fmt.Sprintf("%s cửa sổ ngữ cảnh %.0f%% (%d/%d) chiến lược: %s", agent, payload.Percent, payload.Tokens, payload.ContextWindow, payload.Strategy)

	depth := 0
	if agent != "coordinator" {
		depth = 1
	}

	if payload.Strategy != "" {
		// Đã kích hoạt nén → luồng sự kiện + log
		ctxEv := Event{Time: time.Now(), Category: "SYSTEM", Agent: agent, Summary: summary, Level: level, Depth: depth}
		o.emitEv(ctxEv)
		o.persistEvent(ctxEv)
	} else {
		// Báo cáo mức sử dụng thông thường → chỉ log
		slogLevel := slog.LevelInfo
		if level == "warn" {
			slogLevel = slog.LevelWarn
		}
		slog.Log(context.Background(), slogLevel, summary, "module", "context", "agent", agent)
	}
}

func (o *observer) handleToolEnd(ev agentcore.Event) {
	agent := agentFromEvent(ev)
	// Công cụ kết thúc: chuyển trạng thái về idle, nếu không thanh bên sẽ mãi ở working.
	// Trạng thái của dispatchTarget khi kết thúc dispatch agent con sẽ được xóa riêng bên dưới.
	o.updateAgent(agent, func(a *agentState) {
		a.tool = ""
		a.state = "idle"
	})
	delete(o.lastThinkingByAgent, agent)

	// Lấy bản ghi DISPATCH đang diễn ra (ev.Args của handleToolEnd có thể rỗng, lấy từ currentDispatchTarget)
	var dispatchCall *activeCall
	var dispatchTarget string
	if ev.Tool == "subagent" {
		dispatchTarget = o.currentDispatchTarget
		o.currentDispatchTarget = ""
		if dispatchTarget == "" {
			if sub := parseSubagentArgs(ev.Args); sub.agent != "" {
				dispatchTarget = sub.agent
			}
		}
		if dispatchTarget == "" {
			dispatchTarget = "subagent"
		}
		if call, ok := o.dispatchStarts[dispatchTarget]; ok {
			dispatchCall = call
			delete(o.dispatchStarts, dispatchTarget)
		}
		// Dispatch kết thúc: reset trạng thái agent con về idle (cần làm ở cả đường thành công/thất bại/lỗi)
		if dispatchTarget != "subagent" {
			o.updateAgent(dispatchTarget, func(a *agentState) {
				a.state = "idle"
				a.tool = ""
			})
		}
	}

	// Lấy bản ghi công cụ trực tiếp của coordinator (không phải subagent) đang diễn ra (hiếm, nhưng đảm bảo nhất quán)
	var toolCall *activeCall
	if ev.Tool != "subagent" {
		if call, ok := o.toolStarts[agent]; ok {
			toolCall = call
			delete(o.toolStarts, agent)
		}
	}

	// Trạng thái hoàn thành thống nhất (thành công/thất bại), cập nhật dòng gốc theo cùng ID
	emitFinish := func(call *activeCall, category, agentName string, failed bool) {
		if call == nil {
			return
		}
		level := "success"
		if failed {
			level = "error"
		}
		finishEv := Event{
			ID:         call.id,
			Time:       call.start,
			FinishedAt: time.Now(),
			Failed:     failed,
			Category:   category,
			Agent:      agentName,
			Summary:    call.summary,
			Level:      level,
			Depth:      call.depth,
			Duration:   time.Since(call.start),
		}
		o.emitEv(finishEv)
		o.persistEvent(finishEv)
	}
	emitDispatchFinish := func(failed bool) {
		emitFinish(dispatchCall, "DISPATCH", dispatchTarget, failed)
	}
	emitToolFinish := func(failed bool) {
		emitFinish(toolCall, "TOOL", agent, failed)
	}
	// Dự phòng: nếu khi subagent kết thúc, bên trong subagent đó còn có lần gọi TOOL chưa hoàn thành
	// (ví dụ ensureSubagentToolStarted đã phát sự kiện đang diễn ra, nhưng abort/context cancel
	// khiến ProgressToolEnd không đến), ở đây bắt buộc phát finish, tránh dòng TOOL mãi "đang diễn ra".
	// Trạng thái đồng bộ theo dispatch.
	flushOrphanSubagentTool := func(failed bool) {
		if dispatchTarget == "" {
			return
		}
		call, ok := o.toolStarts[dispatchTarget]
		if !ok {
			return
		}
		delete(o.toolStarts, dispatchTarget)
		delete(o.streamExtractors, dispatchTarget)
		emitFinish(call, "TOOL", dispatchTarget, failed)
	}

	if ev.IsError {
		depth := 0
		if agent != "coordinator" {
			depth = 1
		}
		errText := ""
		if len(ev.Result) > 0 {
			errText = string(ev.Result)
		}
		// ctx-cancel phát sinh từ abort thủ công của người dùng: vẫn phải dọn trạng thái
		// (dòng dispatch / tool phải về trạng thái hoàn thành), nhưng bỏ qua dòng ERROR độc lập
		// + log lỗi, nhất quán với đường EventError.
		if o.isCancellationNoise(nil, errText) {
			slog.Debug("suppressed cancel-derived tool error", "module", "agent", "tool", ev.Tool, "msg", errText)
			flushOrphanSubagentTool(true)
			emitDispatchFinish(true)
			emitToolFinish(true)
			return
		}
		summary := fmt.Sprintf("%s thất bại", ev.Tool)
		detail := summary
		kind := ""
		if errText != "" {
			kind = errorKind(nil, errText)
			detail = fmt.Sprintf("%s → %s: %s", agent, ev.Tool, errText)
			summary += ": " + truncate(errText, 120)
		}
		flushOrphanSubagentTool(true)
		emitDispatchFinish(true)
		emitToolFinish(true)
		errEv := Event{
			Time:     time.Now(),
			Category: "ERROR",
			Agent:    agent,
			Summary:  summary,
			Detail:   detail,
			Kind:     kind,
			Level:    "error",
			Depth:    depth,
		}
		o.emitEv(errEv)
		o.persistEvent(errEv)
		return
	}

	if errEv, fullErr := o.subagentResultErrorEvent(ev); errEv != nil {
		if o.isCancellationNoise(nil, fullErr) {
			slog.Debug("suppressed cancel-derived subagent error", "module", "agent", "tool", ev.Tool, "msg", fullErr)
			flushOrphanSubagentTool(true)
			emitDispatchFinish(true)
			return
		}
		if dispatchTarget != "" && dispatchTarget != "subagent" {
			errEv.Agent = dispatchTarget
		}
		flushOrphanSubagentTool(true)
		emitDispatchFinish(true)
		o.emitEv(*errEv)
		o.persistEvent(*errEv)
		return
	}

	// subagent hoàn thành thành công → cập nhật dòng DISPATCH gốc thành trạng thái hoàn thành (kèm thời gian)
	if ev.Tool == "subagent" {
		flushOrphanSubagentTool(false)
		emitDispatchFinish(false)
		return
	}

	// Công cụ trực tiếp của coordinator hoàn thành thành công
	emitToolFinish(false)
}

// coordinatorStreamKey là khóa luồng utf8Pending cho chính coordinator (message delta cấp cao nhất
// không thuộc subagent nào). Dùng khóa cố định, khác biệt với mọi tên subagent thực tế.
const coordinatorStreamKey = "__coordinator__"

// emitStreamDelta phát một mảnh văn bản của một luồng (agent) ra panel stream.
// agent là khóa luồng: byte UTF-8 lẻ chỉ được nối lại trong phạm vi cùng một agent, tránh
// ghép nhầm sang luồng khác khi các agent stream đan xen (xem chú thích ở trường utf8Pending).
func (o *observer) emitStreamDelta(agent, delta string, thinking bool) {
	if delta == "" {
		return
	}
	// Nối lại các byte UTF-8 chưa hoàn chỉnh từ delta trước của CÙNG agent (nếu có).
	// SSE streaming có thể cắt ký tự UTF-8 multi-byte (như tiếng Việt ạ = 3 bytes)
	// giữa hai chunk. Nếu không buffer, TUI render sẽ gặp byte lẻ, Go range string
	// thay bằng U+FFFD, mất ký tự vĩnh viễn.
	if pending := o.utf8Pending[agent]; len(pending) > 0 {
		delta = string(pending) + delta
		delete(o.utf8Pending, agent)
	}
	if n := incompleteUTF8Suffix(delta); n > 0 {
		o.utf8Pending[agent] = append([]byte{}, delta[len(delta)-n:]...)
		delta = delta[:len(delta)-n]
	}
	if delta == "" {
		return
	}
	if thinking != o.streamThinking {
		o.emitD(utils.ThinkingSep)
		o.streamThinking = thinking
	}
	o.emitD(delta)
	o.streamHasContent = true
	o.streamLastByte = delta[len(delta)-1]
}

// ensureSubagentToolStarted khi nhận diện được tool_call lần đầu trong stream, đăng ký trước
// một lần gọi TOOL đang diễn ra cho agent đó, để spinner của luồng sự kiện bao phủ
// "LLM stream sinh tham số tool_call" (thường chiếm 99% tổng thời gian gọi).
// args lúc này chưa đầy đủ, tạm dùng tên công cụ thuần túy làm summary;
// khi ProgressToolStart thực sự đến sẽ bổ sung summary kèm tham số.
func (o *observer) ensureSubagentToolStarted(agent, tool string) {
	if agent == "" || tool == "" {
		return
	}
	if _, ok := o.toolStarts[agent]; ok {
		return // đã có lần gọi đang diễn ra, idempotent
	}
	id := nextEventID()
	o.toolStarts[agent] = &activeCall{
		id:      id,
		start:   time.Now(),
		summary: tool, // dùng tên công cụ thuần, ProgressToolStart đến sẽ có thể cập nhật thành tool(chương N)
		depth:   1,
	}
	o.emitAndLog(Event{
		ID:       id,
		Time:     time.Now(),
		Category: "TOOL",
		Agent:    agent,
		Summary:  tool,
		Level:    "info",
		Depth:    1,
	})
	o.updateAgent(agent, func(a *agentState) {
		a.state = "working"
		a.tool = tool
	})
	o.emitFallbackStreamHeader(tool)
}

// emitFallbackStreamHeader bổ sung một dòng tiêu đề ✻ vào panel stream cho các công cụ không có extractor.
// Cả ba đường dẫn đều phải gọi để đảm bảo nhất quán:
//  1. ensureSubagentToolStarted —— subagent stream tool args (DeltaToolCall)
//  2. handleToolUpdate ProgressToolStart —— subagent non-stream tool args
//  3. handleToolStart —— công cụ của coordinator
//
// Thiếu bất kỳ đường nào, cùng một công cụ sẽ "khi writer gọi có ✻, khi coordinator gọi không có ✻" hoặc ngược lại.
func (o *observer) emitFallbackStreamHeader(tool string) {
	if _, has := toolDisplays[tool]; has {
		return // có extractor, header do extractor tự output
	}
	o.streamClear()
	// Header là chuỗi hoàn chỉnh (kết thúc bằng "\n") nên không sinh byte lẻ; khóa luồng nào cũng an toàn.
	o.emitStreamDelta(coordinatorStreamKey, streamHeaderFallback(tool)+"\n", false)
}

// streamHeaderFallback tạo văn bản header stream cho các công cụ không có extractor,
// để người dùng luôn thấy "đang gọi công cụ gì" dù là công cụ đọc nhẹ.
//
// Tiền tố "✻ " là ký hiệu quy ước cho "khối agent dispatch" — renderStreamContent của TUI
// thấy tiền tố này sẽ đi đường renderAgentBlock để render (icon + label highlight + divider),
// ngược lại rơi vào đường nội dung chính với màu terminal mặc định, header trông như văn bản thường không nổi bật.
func streamHeaderFallback(tool string) string {
	label := tool
	switch tool {
	case "ask_user":
		label = "Hỏi người dùng"
	}
	return "✻ " + label
}

// streamClear thông báo TUI mở streamRound mới, đồng thời reset trạng thái liên quan đến phân đoạn.
// Về mặt logic round mới là "stream rỗng", nếu không lần emit đầu tiên của extractor tiếp theo
// sẽ bổ sung nhầm dòng trống dẫn đầu.
//
// streamThinking cũng phải reset cùng: emitStreamDelta dùng streamThinking để theo dõi
// đoạn trước có phải thinking không. Trong round mới chưa có nội dung nào, lần emit tiếp theo
// (thinking=false) không nên chèn ThinkingSep. Nếu không, fallback header (như ✻ đọc chương)
// sẽ bị \x02 chiếm đầu, renderStreamContent HasPrefix("✻") không khớp, toàn đoạn rơi vào
// đường nội dung chính rồi bị ThinkingSep cắt thành đoạn thinking, title bị vẽ màu thinking.
func (o *observer) streamClear() {
	// Flush pending UTF-8 bytes của mọi agent trước khi clear: tránh mất ký tự cuối cùng của round.
	// Byte lẻ tại ranh giới round hiếm (LLM thường kết thúc bằng ký tự hoàn chỉnh),
	// nhưng phòng thủ cho trường hợp abort giữa chừng.
	if len(o.utf8Pending) > 0 {
		for agent, pending := range o.utf8Pending {
			if len(pending) > 0 {
				o.emitD(string(pending))
			}
			delete(o.utf8Pending, agent)
		}
	}
	o.emitC()
	o.streamHasContent = false
	o.streamLastByte = 0
	o.streamThinking = false
	// ProgressToolEnd của subagent trước đã delete, đây xóa phòng thủ.
	if len(o.streamExtractors) > 0 {
		o.streamExtractors = make(map[string]*agentExtractor)
	}
}

func (o *observer) subagentResultErrorEvent(ev agentcore.Event) (*Event, string) {
	if ev.Tool != "subagent" || len(ev.Result) == 0 {
		return nil, ""
	}
	sub := parseSubagentArgs(ev.Args)
	errMsg := parseSubagentResultError(ev.Result)
	if errMsg == "" {
		return nil, ""
	}

	target := "subagent"
	if sub.agent != "" {
		target = sub.agent
	}
	fullErr := fmt.Sprintf("%s thất bại: %s", target, errMsg)
	return &Event{
		Time:     time.Now(),
		Category: "ERROR",
		Agent:    "coordinator",
		Summary:  fmt.Sprintf("%s thất bại: %s", target, truncate(errMsg, 120)),
		Detail:   fullErr,
		Kind:     errorKind(nil, errMsg),
		Level:    "error",
	}, fullErr
}

func (o *observer) updateAgent(name string, fn func(*agentState)) {
	if name == "" {
		return
	}
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	a, ok := o.agents[name]
	if !ok {
		a = &agentState{name: name, state: "idle"}
		o.agents[name] = a
	}
	fn(a)
	a.updated = time.Now()
}

func (o *observer) agentSnapshots() []AgentSnapshot {
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	snaps := make([]AgentSnapshot, 0, len(o.agents))
	for _, a := range o.agents {
		snaps = append(snaps, AgentSnapshot{
			Name:      a.name,
			State:     a.state,
			Summary:   a.summary,
			Tool:      a.tool,
			Turn:      a.turn,
			Context:   a.context,
			UpdatedAt: a.updated,
		})
	}
	return snaps
}

func agentFromEvent(ev agentcore.Event) string {
	if ev.Progress != nil && ev.Progress.Agent != "" {
		return ev.Progress.Agent
	}
	return "coordinator"
}

func displayToolName(tool string, args json.RawMessage) string {
	if len(args) == 0 {
		return tool
	}
	switch tool {
	case "save_foundation":
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(args, &p) == nil && p.Type != "" {
			return fmt.Sprintf("%s[%s]", tool, p.Type)
		}
	case "commit_chapter", "plan_chapter", "draft_chapter", "check_consistency":
		var p struct {
			Chapter int `json:"chapter"`
		}
		if json.Unmarshal(args, &p) == nil && p.Chapter > 0 {
			return fmt.Sprintf("%s(chương %d)", tool, p.Chapter)
		}
	case "save_review":
		var p struct {
			Chapter int    `json:"chapter"`
			Scope   string `json:"scope"`
			Verdict string `json:"verdict"`
		}
		if json.Unmarshal(args, &p) == nil {
			label := ""
			switch p.Scope {
			case "arc":
				label = "cung này"
			case "global":
				label = "toàn cục"
			default:
				if p.Chapter > 0 {
					label = fmt.Sprintf("chương %d", p.Chapter)
				}
			}
			if label == "" {
				return tool
			}
			if p.Verdict != "" {
				return fmt.Sprintf("%s(%s·%s)", tool, label, p.Verdict)
			}
			return fmt.Sprintf("%s(%s)", tool, label)
		}
	case "novel_context":
		var p struct {
			Chapter int `json:"chapter"`
		}
		if json.Unmarshal(args, &p) == nil && p.Chapter > 0 {
			return fmt.Sprintf("%s(chương %d)", tool, p.Chapter)
		}
	case "read_chapter":
		var p struct {
			Chapter   int    `json:"chapter"`
			Source    string `json:"source"`
			Character string `json:"character"`
		}
		if json.Unmarshal(args, &p) == nil && p.Chapter > 0 {
			suffix := ""
			if p.Character != "" {
				suffix = "·" + p.Character + " hội thoại"
			} else if p.Source == "draft" {
				suffix = "·bản nháp"
			}
			return fmt.Sprintf("%s(chương %d%s)", tool, p.Chapter, suffix)
		}
	}
	return tool
}

type subagentInvocation struct {
	agent string
	task  string
}

func parseSubagentResultError(result json.RawMessage) string {
	if len(result) == 0 {
		return ""
	}
	// Lỗi phổ biến: đối tượng {"error": "..."} (unknown agent / invalid model / agent con thực thi thất bại)
	var obj struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(result, &obj); err == nil && obj.Error != "" {
		return obj.Error
	}
	// Tương thích lỗi chuỗi thuần của SubAgentTool trong agentcore:
	// "Invalid parameters: ..." / "background mode requires ..." / "Too many parallel tasks ..."
	// Đây là lỗi kiểm tra tham số tầng tool, is_error=false nhưng nội dung là thông báo lỗi,
	// cần nhận diện là lỗi để tránh nhầm là thành công.
	var s string
	if json.Unmarshal(result, &s) == nil && isSubagentErrorString(s) {
		return s
	}
	return ""
}

var subagentErrorPrefixes = []string{
	"Invalid parameters",
	"background mode requires",
	"Too many parallel tasks",
}

func isSubagentErrorString(s string) bool {
	for _, p := range subagentErrorPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func parseSubagentArgs(args json.RawMessage) subagentInvocation {
	if len(args) == 0 {
		return subagentInvocation{}
	}
	var p struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}
	if json.Unmarshal(args, &p) == nil && p.Agent != "" {
		return subagentInvocation{agent: p.Agent, task: p.Task}
	}
	return subagentInvocation{}
}

// incompleteUTF8Suffix trả về số byte ở cuối s tạo thành ký tự UTF-8 multi-byte chưa hoàn chỉnh.
// Trả về 0 nếu s kết thúc bằng ký tự hoàn chỉnh hoặc rỗng.
//
// Ký tự UTF-8 multi-byte bắt đầu bằng leading byte (11xxxxxx) theo sau bởi continuation bytes (10xxxxxx):
//
//	110xxxxx → 2 bytes tổng cộng
//	1110xxxx → 3 bytes tổng cộng (tiếng Việt, CJK)
//	11110xxx → 4 bytes tổng cộng (emoji, ký tự hiếm)
//
// Hàm này quét ngược tối đa 3 byte từ cuối để tìm leading byte, đếm số continuation bytes
// theo sau nó, so sánh với số byte mong đợi. Nếu thiếu, trả về số byte đã có (chờ phần còn lại).
func incompleteUTF8Suffix(s string) int {
	n := len(s)
	if n == 0 {
		return 0
	}
	// Quét ngược tối đa 3 byte (ký tự UTF-8 dài nhất = 4 bytes, vậy tối đa 3 byte trailing chưa đủ)
	scan := 3
	if scan > n {
		scan = n
	}
	for i := 1; i <= scan; i++ {
		c := s[n-i]
		if c < 0x80 {
			// ASCII thuần — hoàn chỉnh, không có ký tự multi-byte lẻ phía sau
			return 0
		}
		if c&0xC0 == 0xC0 {
			// Đây là leading byte. Xác định số byte mong đợi.
			var expected int
			switch {
			case c&0xE0 == 0xC0:
				expected = 2
			case c&0xF0 == 0xE0:
				expected = 3
			case c&0xF8 == 0xF0:
				expected = 4
			default:
				// Byte không hợp lệ, không buffer
				return 0
			}
			if i < expected {
				return i // thiếu continuation bytes
			}
			return 0 // đủ bytes
		}
		// continuation byte (10xxxxxx) — tiếp tục quét ngược tìm leading byte
	}
	// Quét hết 3 byte mà không tìm thấy leading byte — dữ liệu lỗi, không buffer
	return 0
}
