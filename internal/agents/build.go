package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	corecontext "github.com/voocel/agentcore/context"
	"github.com/voocel/agentcore/subagent"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/agents/ctxpack"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/host/reminder"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// logRulesLoaded in ra thực trạng tải rules trong giai đoạn khởi động: thư mục rules của sách,
// nguồn thực tế đọc được, giá trị kiểm tra số từ có hiệu lực.
// File rules đặt sai đường dẫn sẽ bị loader bỏ qua thầm lặng, nguồn cũng không vào LLM
// (chỉ thấy được ở panel /diag), đặt sai mà không có phản hồi là trở ngại lớn nhất
// khi người dùng gỡ lỗi. Dòng log khởi động này giúp phát hiện ngay "sai đường dẫn / số từ chưa ghi vào front matter".
func logRulesLoaded(opts rules.LoadOptions) {
	b := rules.Merge(rules.Load(opts))
	words := "chưa đặt (không kiểm tra số từ)"
	if w := b.Structured.ChapterWords; w != nil {
		words = fmt.Sprintf("%d-%d", w.Min, w.Max)
	}
	slog.Info("tải rules",
		"thư mục rules sách", opts.ProjectRulesDir,
		"nguồn đã tải", b.Sources,
		"số từ mỗi chương", words)
}

// agentToRole chuẩn hóa tên subagent thành tên role mà ModelSet nhận ra.
// architect_long dùng cấu hình role architect.
// Tương đương với host.agentRoleName; vì build và host độc lập nhau nên mỗi bên giữ một bản.
func agentToRole(name string) string {
	if strings.HasPrefix(name, "architect_") {
		return "architect"
	}
	return name
}

// subagentMaxRetries là giới hạn retry LLM thống nhất cho tất cả SubAgentConfig và Coordinator.
// Chiến lược lùi: lũy thừa 1s/2s/4s/8s/16s (bị giới hạn bởi maxDelay), ưu tiên tuân theo
// server Retry-After. Kết hợp ToolsAreIdempotent=true để các lỗi retryable như stream-idle /
// 503 / mạng chập chờn có thể được retry ngay tại lớp subagent thay vì ném toàn bộ
// subagent về coordinator để phân phát lại.
// Nguyên tắc sắt #1 đảm bảo các tool ghi dùng checkpoint+digest bất biến, retry là an toàn.
const subagentMaxRetries = 5

// UsageRecorder là callback tùy chọn của BuildCoordinator để ghi lại mức sử dụng;
// chữ ký giống OnMessage, được gọi mỗi lần có một tin nhắn agent,
// tầng Host chịu trách nhiệm tổng hợp. nil nghĩa là không theo dõi.
type UsageRecorder func(agentName string, msg agentcore.AgentMessage)

// ApplyThinking áp dụng cường độ thinking của một role cụ thể lên agent đang chạy
// (dùng khi điều chỉnh /model lúc runtime).
// coordinator → Agent.SetThinkingLevel; architect → hai subagent architect_*;
// writer/editor → subagent tương ứng. level rỗng = dùng mặc định của model/provider.
// Các tên role khác bị bỏ qua.
type ApplyThinking func(role string, level agentcore.ThinkingLevel)

// ParseThinkingLevel chuyển chuỗi cấu hình thành agentcore.ThinkingLevel.
// "" hợp lệ (= không ghi đè/kế thừa); các giá trị còn lại phải là off/minimal/low/medium/high/xhigh,
// nếu không sẽ trả về error (lúc khởi động hạ cấp thành rỗng và warn, lúc runtime hiển thị error cho người dùng).
func ParseThinkingLevel(s string) (agentcore.ThinkingLevel, error) {
	lv := agentcore.ThinkingLevel(strings.ToLower(strings.TrimSpace(s)))
	switch lv {
	case "", agentcore.ThinkingOff, agentcore.ThinkingMinimal, agentcore.ThinkingLow,
		agentcore.ThinkingMedium, agentcore.ThinkingHigh, agentcore.ThinkingXHigh:
		return lv, nil
	default:
		return "", fmt.Errorf("cường độ thinking không hợp lệ %q (các giá trị: off/minimal/low/medium/high/xhigh)", s)
	}
}

// roleThinking phân tích cường độ thinking hiệu lực của một role; giá trị không hợp lệ
// sẽ bị hạ cấp thành rỗng (không ghi đè) và ghi warn.
func roleThinking(cfg bootstrap.Config, role string) agentcore.ThinkingLevel {
	lv, err := ParseThinkingLevel(cfg.ResolveThinking(role))
	if err != nil {
		slog.Warn("bỏ qua cấu hình cường độ thinking không hợp lệ", "module", "agent", "role", role, "err", err)
		return ""
	}
	return lv
}

// subagentContextFactory tạo ContextManagerFactory dùng chung cho các subagent không có
// chiến lược khôi phục riêng (architect/editor). Trước đây các agent này không gắn
// ContextManager nào: agentcore chỉ nén khi ContextManager != nil, nên lịch sử phình
// vô hạn qua 15-20 turn (read_chapter 8-15k token/chương, novel_context tới 60KB) —
// nguồn gốc các request input vượt 100k token dù writer/coordinator đã có ngưỡng nén.
// Factory được xây lại mỗi lần gọi subagent nên cửa sổ ngữ cảnh tự liên động khi /model đổi,
// cùng cơ chế với writer.
func subagentContextFactory(cfg bootstrap.Config, agent string) func(agentcore.ChatModel) agentcore.ContextManager {
	return func(model agentcore.ChatModel) agentcore.ContextManager {
		window, _ := cfg.ResolveContextWindow(bootstrap.ModelName(model))
		return newContextManager(contextManagerConfig{
			Model:            model,
			ContextWindow:    window,
			ReserveTokens:    bootstrap.CompactReserveTokens(window),
			KeepRecentTokens: 20000,
			Agent:            agent,
			ToolMicrocompact: &corecontext.ToolResultMicrocompactConfig{
				// KeepRecent=3 đồng bộ với writer/coordinator: dọn sớm các tool result cũ
				// (novel_context/read_chapter là khối lớn nhất trong lịch sử các agent này).
				KeepRecent:    3,
				IdleThreshold: 5 * time.Minute,
			},
		})
	}
}

// BuildCoordinator lắp ráp Coordinator Agent cùng các SubAgent của nó.
// Trả về Agent, AskUserTool, WriterRestorePack, tham chiếu ContextEngine của Coordinator,
// và closure ApplyThinking — tầng Host cần gọi trực tiếp SetContextWindow +
// SetReserveTokens khi chuyển /model để liên động cửa sổ ngữ cảnh của model mới
// (writer/architect/editor dùng ContextManagerFactory tự xây lại, không cần ref;
// chỉ coordinator thường trực mới cần), và liên động cường độ thinking các role
// thông qua ApplyThinking. Tầng Host nhận luồng sự kiện qua Agent.Subscribe,
// không cần callback emit nữa.
func BuildCoordinator(
	cfg bootstrap.Config,
	store *store.Store,
	models *bootstrap.ModelSet,
	bundle assets.Bundle,
	recordUsage UsageRecorder,
) (*agentcore.Agent, *tools.AskUserTool, *ctxpack.WriterRestorePack, *corecontext.ContextEngine, ApplyThinking) {
	// Công cụ dùng chung
	rulesOpts := rules.DefaultOptions(bundle.RulesFS)
	logRulesLoaded(rulesOpts)
	contextTool := tools.NewContextTool(store, bundle.References, cfg.Style, rulesOpts)
	readChapter := tools.NewReadChapterTool(store)
	askUser := tools.NewAskUserTool()

	architectTools := []agentcore.Tool{
		contextTool,
		tools.NewSaveFoundationTool(store),
	}
	writerTools := []agentcore.Tool{
		contextTool,
		readChapter,
		tools.NewPlanChapterTool(store),
		tools.NewDraftChapterTool(store),
		tools.NewEditChapterTool(store),
		tools.NewCheckConsistencyTool(store),
		tools.NewCommitChapterTool(store).WithRules(rulesOpts),
	}
	editorTools := []agentcore.Tool{
		contextTool,
		readChapter,
		tools.NewSaveReviewTool(store),
		tools.NewSaveArcSummaryTool(store),
		tools.NewSaveVolumeSummaryTool(store),
	}

	// Provider failover chỉ ghi log, không thông báo tầng host
	reportFailover := func(ev bootstrap.FailoverEvent) {
		slog.Warn("chuyển nhà cung cấp",
			"module", "agent",
			"role", ev.Role,
			"reason", ev.Reason,
			"from", fmt.Sprintf("%s/%s", ev.FromProvider, ev.FromModel),
			"to", fmt.Sprintf("%s/%s", ev.ToProvider, ev.ToModel),
			"err", ev.Err,
		)
	}

	architectModel := models.ForRoleWithFailover("architect", reportFailover)
	writerModel := models.ForRoleWithFailover("writer", reportFailover)
	editorModel := models.ForRoleWithFailover("editor", reportFailover)
	coordinatorModel := models.ForRoleWithFailover("coordinator", reportFailover)

	// ContextManager của Coordinator được tạo một lần khi xây dựng Agent, dựa trên model khởi động.
	// Khi chuyển /model sang model có cửa sổ nhỏ hơn lúc runtime, nên cấu hình tường minh context_window để dự phòng.
	_, coordinatorModelName, _ := models.CurrentSelection("coordinator")
	coordinatorContextWindow, coordinatorSource := cfg.ResolveContextWindow(coordinatorModelName)
	// ContextManager của Writer được xây lại mỗi lần gọi bởi factory, cửa sổ ngữ cảnh tự động
	// theo model swap (xem factory bên dưới).
	_, writerModelName, _ := models.CurrentSelection("writer")
	writerContextWindow, writerSource := cfg.ResolveContextWindow(writerModelName)
	bootstrap.LogContextWindowChoice("coordinator", coordinatorModelName, coordinatorContextWindow, coordinatorSource)
	bootstrap.LogContextWindowChoice("writer", writerModelName, writerContextWindow, writerSource)

	// modelLookup gắn _meta:{provider,model} vào mỗi tin nhắn assistant khi ghi session,
	// để replay không phụ thuộc vào "ModelSet hiện tại" để suy ngược cost lịch sử;
	// chuyển model lúc runtime cũng tính toán chính xác.
	modelLookup := func(agentName string) (string, string) {
		role := agentToRole(agentName)
		provider, name, _ := models.CurrentSelection(role)
		return provider, name
	}
	baseOnMsg := store.Sessions.SubAgentLogger(modelLookup)
	onMsg := func(agentName, task string, msg agentcore.AgentMessage) {
		baseOnMsg(agentName, task, msg)
		if recordUsage != nil {
			recordUsage(agentName, msg)
		}
	}
	baseCoordinatorLog := store.Sessions.CoordinatorLogger(modelLookup)
	coordinatorOnMessage := func(msg agentcore.AgentMessage) {
		baseCoordinatorLog(msg)
		if recordUsage != nil {
			recordUsage("coordinator", msg)
		}
	}

	architectStopGuardFactory := func(_, _ string) agentcore.StopGuard {
		return reminder.NewArchitectStopGuard(store)
	}
	architectThinking := roleThinking(cfg, "architect")
	architectLong := subagent.Config{
		Name:               "architect_long",
		Description:        "Kiến trúc sư truyện dài: tạo thiết định phân tầng và đề cương cung truyện cho truyện đăng nhiều kỳ, có thể mở rộng liên tục",
		Model:              architectModel,
		SystemPrompt:       bundle.Prompts.ArchitectLong,
		Tools:              architectTools,
		MaxTurns:           20,
		MaxRetries:         subagentMaxRetries,
		ThinkingLevel:      architectThinking,
		ToolsAreIdempotent: true,
		OnMessage:          onMsg,
		StopAfterToolResult: func(toolName string, result json.RawMessage) bool {
			r := decodeSaveFoundationResult(toolName, result)
			switch r.Type {
			case "update_compass", "expand_arc", "complete_book":
				return true
			default:
				return false
			}
		},
		StopGuardFactory:      architectStopGuardFactory,
		ContextManagerFactory: subagentContextFactory(cfg, "architect_long"),
	}

	// Tài liệu tham khảo tĩnh (consistency/hook/quality-checklist/anti-ai-tone) nằm trong system prompt
	// thay vì trả qua novel_context: system prompt chỉ xuất hiện một lần mỗi phiên và là prefix ổn định
	// để provider cache, còn tool result bị gửi lặp lại ở mọi lượt sau. Editor cũng nhận khối này vì
	// anti_ai_tone/quality_checklist là tiêu chí kiểm duyệt dùng chung (trước đây editor nhận qua
	// writerReferences khi gọi novel_context với chapter).
	staticRefs := tools.StaticReferencesSection(bundle.References)

	writerPrompt := bundle.Prompts.Writer
	if style, ok := bundle.Styles[cfg.Style]; ok {
		writerPrompt += "\n\n" + style
	}
	writerPrompt += staticRefs

	restore := &ctxpack.WriterRestorePack{}
	restore.Refresh(store)

	writer := subagent.Config{
		Name:               "writer",
		Description:        "Người viết: tự chủ hoàn thành việc phác thảo ý tưởng, viết, tự kiểm duyệt và lưu chương cho một chương",
		Model:              writerModel,
		SystemPrompt:       writerPrompt,
		Tools:              writerTools,
		MaxTurns:           30,
		MaxRetries:         subagentMaxRetries,
		ThinkingLevel:      roleThinking(cfg, "writer"),
		ToolsAreIdempotent: true,
		StopAfterTools:     []string{"commit_chapter"},
		OnMessage:          onMsg,
		StopGuardFactory: func(_, _ string) agentcore.StopGuard {
			return reminder.NewWriterStopGuard(store)
		},
		ContextManagerFactory: func(model agentcore.ChatModel) agentcore.ContextManager {
			// Được xây lại mỗi lần gọi subagent(writer), đọc tên model mới nhất từ runModel hiện tại.
			// Chuyển writer qua /model, chương tiếp theo tự động dùng cửa sổ ngữ cảnh mới.
			window, _ := cfg.ResolveContextWindow(bootstrap.ModelName(model))
			return newContextManager(contextManagerConfig{
				Model:            model,
				ContextWindow:    window,
				ReserveTokens:    bootstrap.CompactReserveTokens(window),
				KeepRecentTokens: 20000,
				Agent: "writer",
				ToolMicrocompact: &corecontext.ToolResultMicrocompactConfig{
					// KeepRecent=3 (mặc định 5): khi nén kích hoạt, chỉ bảo vệ 3 tool result gần nhất
					// (đủ cho read_chapter/check/edit đang dở), dọn sớm các kết quả novel_context cũ —
					// khối lớn nhất trong lịch sử, và đã khôi phục được qua StoreSummaryCompact/restore pack.
					KeepRecent:    3,
					IdleThreshold: 5 * time.Minute,
				},
				ExtraStrategies: []corecontext.Strategy{
					ctxpack.NewStoreSummaryCompact(ctxpack.StoreSummaryCompactConfig{
						Store:            store,
						KeepRecentTokens: 20000,
					}),
				},
				Summary: &corecontext.FullSummaryConfig{
					PostSummaryHooks:    []corecontext.PostSummaryHook{restore.Hook()},
					SystemPrompt:        ctxpack.WriterSummarySystemPrompt,
					SummaryPrompt:       ctxpack.WriterSummaryPrompt,
					UpdateSummaryPrompt: ctxpack.WriterUpdateSummaryPrompt,
					TurnPrefixPrompt:    ctxpack.WriterTurnPrefixPrompt,
				},
			})
		},
	}

	editor := subagent.Config{
		Name:               "editor",
		Description:        "Biên tập viên: đọc bản gốc, phát hiện vấn đề từ cả hai góc độ cấu trúc và thẩm mỹ",
		Model:              editorModel,
		SystemPrompt:       bundle.Prompts.Editor + staticRefs,
		Tools:              editorTools,
		MaxTurns:           20,
		MaxRetries:         subagentMaxRetries,
		ThinkingLevel:      roleThinking(cfg, "editor"),
		ToolsAreIdempotent: true,
		OnMessage:          onMsg,
		// Chỉ dừng khi đạt sản phẩm trạng thái cuối là loại tóm tắt; save_review không còn dừng cứng —
		// thoát qua StopAfterTool sẽ bỏ qua StopGuard (agentcore loop.go); nếu save_review dừng cứng,
		// editor được phân công "tóm tắt cung truyện nhưng phải kiểm duyệt trước" sẽ bị cắt ngang
		// tại save_review, không đến được save_arc_summary. Việc kết thúc nhiệm vụ kiểm duyệt/tóm tắt
		// được giao cho NewEditorStopGuard nhận biết nhiệm vụ xử lý.
		StopAfterToolResult: func(toolName string, _ json.RawMessage) bool {
			return toolName == "save_arc_summary" || toolName == "save_volume_summary"
		},
		StopGuardFactory: func(_, task string) agentcore.StopGuard {
			return reminder.NewEditorStopGuard(store, task)
		},
		ContextManagerFactory: subagentContextFactory(cfg, "editor"),
	}

	subagentTool := &SanitizedSubagentTool{
		Tool: subagent.New(architectLong, writer, editor),
	}

	coordinatorEngine := newContextManager(contextManagerConfig{
		Model:            coordinatorModel,
		ContextWindow:    coordinatorContextWindow,
		ReserveTokens:    bootstrap.CompactReserveTokens(coordinatorContextWindow),
		KeepRecentTokens: 30000,
		Agent:            "coordinator",
		CommitOnProject:  true,
		// Coordinator sống xuyên suốt cả phiên nên tool result cũ (novel_context tới 60KB, kết quả
		// subagent) tích lũy nhanh; KeepRecent=3 để lần nén nào cũng dọn được nhiều hơn mặc định (5).
		ToolMicrocompact: &corecontext.ToolResultMicrocompactConfig{
			KeepRecent: 3,
		},
	})

	agent := agentcore.NewAgent(
		agentcore.WithModel(coordinatorModel),
		agentcore.WithSystemPrompt(bundle.Prompts.Coordinator),
		agentcore.WithTools(subagentTool, contextTool, tools.NewSaveDirectiveTool(store), tools.NewReopenBookTool(store)),
		agentcore.WithMaxTurns(100_000),
		agentcore.WithOnMessage(coordinatorOnMessage),
		agentcore.WithToolsAreIdempotent(true),
		// subagent là kênh chính của luồng xử lý; lỗi thực sự nên được trả về tường minh cho Host,
		// không nên vô hiệu hóa vĩnh viễn tool trong một lần chạy.
		agentcore.WithMaxToolErrors(0),
		agentcore.WithMaxRetries(subagentMaxRetries),
		agentcore.WithContextManager(coordinatorEngine),
		agentcore.WithStopGuard(reminder.NewStopGuard(store, nil)),
		// Chặn cứng việc phân phát subagent khi phase=complete, ngăn Writer lặp vô tận.
		agentcore.WithToolGate(completePhaseGate(store)),
	)
	// Cường độ thinking của Coordinator: áp dụng kết quả phân tích vô điều kiện. Khi chưa cấu hình
	// thì là rỗng (không gửi thinking, dùng mặc định provider), nhất quán với các subagent
	// (Config.ThinkingLevel mặc định rỗng) — tránh ghi đè ThinkingLow mặc định của agentcore
	// mà ép tất cả provider gửi low (kể cả GLM/Ollama vốn bị ép bật thinking).
	agent.SetThinkingLevel(roleThinking(cfg, "coordinator"))

	// Liên động cường độ thinking các role lúc runtime: coordinator qua Agent, subagent qua subagentTool override.
	applyThinking := func(role string, level agentcore.ThinkingLevel) {
		switch role {
		case "coordinator":
			agent.SetThinkingLevel(level)
		case "architect":
			subagentTool.SetThinkingLevel("architect_long", level)
		case "writer", "editor":
			subagentTool.SetThinkingLevel(role, level)
		}
	}

	return agent, askUser, restore, coordinatorEngine, applyThinking
}

// completePhaseGate trả về một ToolGate: từ chối tất cả phân phát subagent khi phase=complete.
// Ngăn Coordinator LLM tiếp tục gọi Writer/Architect sau khi sách đã hoàn thành, tránh vòng lặp vô tận.
func completePhaseGate(st *store.Store) agentcore.ToolGate {
	return func(_ context.Context, req agentcore.GateRequest) (*agentcore.GateDecision, error) {
		if req.Call.Name != "subagent" {
			return nil, nil
		}
		// fail-open: khi Load lỗi hoặc progress rỗng thì cho qua hết, không để lỗi đọc thoáng qua
		// làm kẹt việc phân phát bình thường. Chi phí duy nhất là nếu đọc thất bại đúng lúc phase=complete
		// thì deadlock có thể tái xuất hiện (xác suất cực thấp, chấp nhận được).
		progress, _ := st.Progress.Load()
		if progress != nil && progress.Phase == domain.PhaseComplete {
			return &agentcore.GateDecision{
				Allowed: false,
				Reason:  "Toàn bộ sách đã hoàn thành (phase=complete), không thể trực tiếp phân phát subagent. Nếu người dùng muốn làm lại chương đã viết, hãy gọi reopen_book(chapters=[...]) để mở lại sách sang trạng thái làm lại (sau đó sẽ tự động phân phát writer viết lại); nếu người dùng muốn thêm cốt truyện mới, hãy thông báo cần tạo dự án mới.",
			}, nil
		}
		return nil, nil
	}
}

type saveFoundationResult struct {
	Type            string `json:"type"`
	FoundationReady bool   `json:"foundation_ready"`
}

func decodeSaveFoundationResult(toolName string, result json.RawMessage) saveFoundationResult {
	if toolName != "save_foundation" {
		return saveFoundationResult{}
	}
	var r saveFoundationResult
	_ = json.Unmarshal(result, &r)
	return r
}

type SanitizedSubagentTool struct {
	*subagent.Tool
}

func (s *SanitizedSubagentTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	sanitized := sanitizeSubagentArgs(args)
	return s.Tool.Execute(ctx, sanitized)
}

func sanitizeSubagentArgs(args json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return args
	}

	agent, _ := m["agent"].(string)
	task, _ := m["task"].(string)

	hasChain := false
	if chainVal, ok := m["chain"]; ok {
		if arr, isArr := chainVal.([]any); isArr && len(arr) > 0 {
			hasChain = true
		}
	}

	hasTasks := false
	if tasksVal, ok := m["tasks"]; ok {
		if arr, isArr := tasksVal.([]any); isArr && len(arr) > 0 {
			hasTasks = true
		}
	}

	if agent != "" && task != "" {
		sanitized := map[string]any{
			"agent": agent,
			"task":  task,
		}
		if bg, ok := m["background"]; ok {
			sanitized["background"] = bg
		}
		if desc, ok := m["description"]; ok {
			sanitized["description"] = desc
		}
		if model, ok := m["model"]; ok {
			sanitized["model"] = model
		}
		if res, err := json.Marshal(sanitized); err == nil {
			return res
		}
	} else if hasChain {
		sanitized := map[string]any{
			"chain": m["chain"],
		}
		if bg, ok := m["background"]; ok {
			sanitized["background"] = bg
		}
		if desc, ok := m["description"]; ok {
			sanitized["description"] = desc
		}
		if model, ok := m["model"]; ok {
			sanitized["model"] = model
		}
		if res, err := json.Marshal(sanitized); err == nil {
			return res
		}
	} else if hasTasks {
		sanitized := map[string]any{
			"tasks": m["tasks"],
		}
		if bg, ok := m["background"]; ok {
			sanitized["background"] = bg
		}
		if desc, ok := m["description"]; ok {
			sanitized["description"] = desc
		}
		if model, ok := m["model"]; ok {
			sanitized["model"] = model
		}
		if res, err := json.Marshal(sanitized); err == nil {
			return res
		}
	}

	return args
}
