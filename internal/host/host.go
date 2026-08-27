package host

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	corecontext "github.com/voocel/agentcore/context"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/agents"
	"github.com/voocel/ainovel-cli/internal/agents/ctxpack"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/host/exp"
	"github.com/voocel/ainovel-cli/internal/host/flow"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/host/req"
	"github.com/voocel/ainovel-cli/internal/host/sim"
	modelreg "github.com/voocel/ainovel-cli/internal/models"
	"github.com/voocel/ainovel-cli/internal/notify"
	"github.com/voocel/ainovel-cli/internal/rules"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// Host là lớp vỏ mỏng thời gian chạy.
// Trách nhiệm: khởi động/khôi phục/tiêm can thiệp/chiếu sự kiện/quản lý mô hình.
// Không thực hiện bất kỳ quyết định lập lịch nào, không tự động tiếp tục khi rảnh.
type Host struct {
	cfg               bootstrap.Config
	bundle            assets.Bundle
	store             *storepkg.Store
	models            *bootstrap.ModelSet
	coordinator       *agentcore.Agent
	coordinatorCtxMgr *corecontext.ContextEngine // liên động SetContextWindow + SetReserveTokens khi chuyển mô hình default/coordinator
	thinkingApplier   agents.ApplyThinking       // liên động live agent (coordinator + agent phụ) khi /model điều chỉnh cường độ suy nghĩ
	askUser           *tools.AskUserTool
	writerRestore     *ctxpack.WriterRestorePack
	observer          *observer
	router            *flow.Dispatcher
	routerDetach      func()
	usage             *UsageTracker
	usageCancel       context.CancelFunc // dừng autoSaveLoop và kích hoạt lần flush cuối cùng
	budget            *BudgetSentinel    // chính sách ngân sách; nil nếu chưa bật (các phương thức an toàn khi nil)
	budgetDetach      func()
	notifier          *notify.Notifier // cảnh báo không người trực; nil nếu chưa bật (Send nil an toàn)

	events   chan Event
	streamCh chan string
	// streamCarry giữ phần văn bản stream chưa gửi được khi streamCh đầy, để gộp vào delta kế tiếp
	// (giữ đúng thứ tự, không mất chữ). Chỉ được truy cập bởi goroutine sự kiện coordinator qua
	// emitDelta/emitClear — không cần khóa. Xem emitDelta.
	streamCarry string
	done        chan struct{}

	mu         sync.Mutex
	lifecycle  lifecycle
	cocreating bool // chiếm dụng đồng sáng tác giai đoạn: chặn can thiệp đồng thời của import/simulate/continue trong cửa sổ paused
	closeOnce  sync.Once
}

type lifecycle string

const (
	lifecycleIdle      lifecycle = "idle"
	lifecycleRunning   lifecycle = "running"
	lifecyclePaused    lifecycle = "paused"
	lifecycleCompleted lifecycle = "completed"
)

// New tạo Host.
func New(cfg bootstrap.Config, bundle assets.Bundle) (*Host, error) {
	cfg.FillDefaults()
	if err := cfg.ValidateBase(); err != nil {
		return nil, err
	}
	slog.Info("Khởi động", "module", "boot", "provider", cfg.Provider, "model", cfg.ModelName, "output", cfg.OutputDir)

	// Khởi goroutine nền để làm mới metadata mô hình (cửa sổ/giá) từ OpenRouter, cache đĩa 24h.
	modelreg.StartPricingRefresh(modelreg.DefaultRegistry(), bootstrap.DefaultConfigDir())

	store := storepkg.NewStore(cfg.OutputDir)
	if err := store.Init(); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	models, err := bootstrap.NewModelSet(cfg)
	if err != nil {
		return nil, fmt.Errorf("create models: %w", err)
	}
	slog.Info("Mô hình sẵn sàng", "module", "boot", "summary", models.Summary())

	usage := NewUsageTracker(models, store)
	// Ưu tiên đọc meta/usage.json; các trường hợp sau đây đều dùng sessions/*.jsonl để backfill một lần:
	//   - File không tồn tại (lần đầu nâng cấp lên phiên bản có persistence)
	//   - Phiên bản schema không khớp (bỏ định dạng cũ sau nâng cấp tương lai)
	//   - File tồn tại nhưng bị hỏng / lỗi IO (không để dữ liệu xấu làm tổng tích lũy về 0 vĩnh viễn)
	// Sau khi backfill xong lập tức SaveNow để cố định kết quả, lần khởi động tiếp theo Load trực tiếp.
	loaded, loadErr := usage.LoadFromStore()
	if loadErr != nil {
		slog.Warn("Tải usage thất bại, sẽ thử backfill từ sessions", "module", "usage", "err", loadErr)
	}
	if !loaded {
		if n, err := usage.ReplaySessions(cfg.OutputDir); err != nil {
			slog.Warn("usage replay thất bại", "module", "usage", "err", err)
		} else if n > 0 {
			slog.Info("Backfill usage từ session hoàn thành", "module", "usage", "messages", n)
			if err := usage.SaveNow(); err != nil {
				slog.Warn("Lưu sau backfill usage thất bại", "module", "usage", "err", err)
			}
		}
	}
	usageCtx, usageCancel := context.WithCancel(context.Background())
	usage.StartAutoSave(usageCtx)

	coordinator, askUser, restore, coordinatorCtxMgr, applyThinking := agents.BuildCoordinator(cfg, store, models, bundle, usage.Record)
	store.Signals.ClearStaleSignals()

	h := &Host{
		cfg:               cfg,
		bundle:            bundle,
		store:             store,
		models:            models,
		coordinator:       coordinator,
		coordinatorCtxMgr: coordinatorCtxMgr,
		thinkingApplier:   applyThinking,
		askUser:           askUser,
		writerRestore:     restore,
		usage:             usage,
		usageCancel:       usageCancel,
		events:            make(chan Event, 100),
		streamCh:          make(chan string, 256),
		done:              make(chan struct{}, 4),
		lifecycle:         lifecycleIdle,
	}
	h.observer = newObserver(coordinator, store, h.emitEvent, h.emitDelta, h.emitClear)
	if cfg.Notify.IsEnabled() {
		h.notifier = notify.New(cfg.Notify.Command, cfg.Notify.Events)
	}
	// Đăng ký BudgetSentinel phải trước Dispatcher: cùng sự kiện ranh giới agent phụ, Abort và FollowUp
	// cạnh tranh nhau, Sentinel đặt cờ Abort trước thì dispatch của Dispatcher tự nhiên bị bỏ, tầng router không biết về ngân sách.
	if sentinel := NewBudgetSentinel(cfg.Budget,
		func() float64 { c, _, _, _, _ := usage.Totals(); return c },
		func(reason string) { h.abortWithEvent(reason, "error") },
		func(level, summary string) {
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
			h.notifier.Send(notify.Notification{Kind: "budget", Level: level, Title: "ainovel: Ngân sách", Body: summary})
		},
	); sentinel != nil {
		h.budget = sentinel
		usage.SetOnCost(sentinel.OnCost)
		h.budgetDetach = coordinator.Subscribe(sentinel.HandleEvent)
		// Cảnh báo vùng mù tính phí: khi mô hình không trả usage thì chi phí luôn là 0, ngân sách không bao giờ kích hoạt — cầu chì không được kết nối phải báo người.
		usage.SetOnMissingUsage(func() {
			const blind = "Vùng mù ngân sách: mô hình không trả dữ liệu usage, thống kê chi phí là 0, giới hạn ngân sách sẽ không kích hoạt (mô hình tùy chỉnh hãy xác nhận giá trong registry hoặc include_usage từ upstream)"
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: blind, Level: "warn"})
			h.notifier.Send(notify.Notification{Kind: "budget", Level: "warn", Title: "ainovel: Ngân sách", Body: blind})
		})
	}
	h.router = flow.NewDispatcher(coordinator, store)
	// Cảnh báo lệnh lặp lại: thuần telemetry, khi chạy không người trực "mô hình có thể đang xoay vòng tại chỗ" đáng báo người xem.
	// Luồng sự kiện và notify phát cùng cặp — notify chỉ là bản sao ngoài màn hình của sự kiện trong màn hình (kiến trúc §2.3).
	h.router.SetOnRepeat(func(agent, task string, n int) {
		body := fmt.Sprintf("Cùng một lệnh đã được đưa ra lần thứ %d (%s): %s", n, agent, task)
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Lệnh lặp lại: " + body, Level: "warn"})
		h.notifier.Send(notify.Notification{Kind: "repeat", Level: "warn", Title: "ainovel: Lệnh lặp lại", Body: body})
	})
	// Chương bị loại khỏi hàng đợi đánh bóng: Host vừa tự tay thay đổi kế hoạch của LLM,
	// nên bắt buộc phải nói ra — bản chính của chương vẫn còn, chỉ là lần đánh bóng này bị bỏ.
	h.router.SetOnDropRewrite(func(chapter, fails int, reason string) {
		body := fmt.Sprintf("Chương %d đã bị loại khỏi hàng đợi đánh bóng sau %d lần thất bại liên tiếp (%s). Bản đã lưu của chương giữ nguyên; sáng tác tiếp tục với phần còn lại của hàng đợi.", chapter, fails, reason)
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Bỏ đánh bóng: " + body, Level: "warn"})
		h.notifier.Send(notify.Notification{Kind: "drop_rewrite", Level: "warn", Title: "ainovel: Bỏ đánh bóng chương", Body: body})
	})
	h.routerDetach = h.router.Attach()

	if err := store.RunMeta.Init(cfg.Style, cfg.Provider, cfg.ModelName); err != nil {
		slog.Error("Khởi tạo thông tin meta chạy thất bại", "module", "boot", "err", err)
	}

	return h, nil
}

// ── Vòng đời ──

// Start chế độ tạo mới: khởi tạo tiến trình và khởi động vòng lặp dài của coordinator.
func (h *Host) Start(prompt string) error {
	return h.StartPrepared(BuildStartPrompt(prompt))
}

// StartPrepared bắt đầu sáng tác với prompt khởi động đã được sắp xếp xong.
func (h *Host) StartPrepared(promptText string) error {
	h.mu.Lock()
	if h.lifecycle == lifecycleRunning {
		h.mu.Unlock()
		return fmt.Errorf("already running")
	}
	if h.cocreating {
		h.mu.Unlock()
		return fmt.Errorf("đồng sáng tác giai đoạn đang diễn ra, vui lòng kết thúc đồng sáng tác trước")
	}
	h.mu.Unlock()

	promptText = strings.TrimSpace(promptText)
	if promptText == "" {
		return fmt.Errorf("prompt is required")
	}
	if err := h.budget.Refuse(); err != nil {
		return err
	}
	if err := h.store.Checkpoints.Reset(); err != nil {
		return fmt.Errorf("reset checkpoints: %w", err)
	}
	if err := h.store.Progress.Init("", 0); err != nil {
		return fmt.Errorf("init progress: %w", err)
	}

	slog.Info("Bắt đầu sáng tác", "module", "host", "prompt_len", len(promptText))
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Bắt đầu sáng tác", Level: "info"})
	h.observer.setAborting(false)
	// Đặt lại theo dõi lặp lại và bật router trước khi khởi động Prompt, tránh sự kiện vòng đầu đến trước Enable
	h.router.ResetRepeat()
	h.router.Enable()
	if err := h.coordinator.Prompt(context.Background(), promptText); err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	// Chủ động dispatch lệnh đầu tiên một lần: nếu đã vào giai đoạn viết (Phase=Writing), Host hạ lệnh ngay;
	// giai đoạn lập kế hoạch Route trả về nil, không có tác dụng phụ.
	h.router.Dispatch()

	h.mu.Lock()
	h.lifecycle = lifecycleRunning
	h.mu.Unlock()
	go h.waitDone()
	return nil
}

// Resume chế độ khôi phục: tạo resume prompt từ checkpoint + progress rồi khởi động.
func (h *Host) Resume() (string, error) {
	h.mu.Lock()
	if h.lifecycle == lifecycleRunning {
		h.mu.Unlock()
		return "", fmt.Errorf("already running")
	}
	if h.cocreating {
		h.mu.Unlock()
		return "", fmt.Errorf("đồng sáng tác giai đoạn đang diễn ra, vui lòng kết thúc đồng sáng tác trước")
	}
	h.mu.Unlock()

	prompt, label, err := buildResumePrompt(h.store)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", nil // chế độ tạo mới, không có khôi phục
	}
	if err := h.budget.Refuse(); err != nil {
		return "", err
	}

	slog.Info("Khôi phục sáng tác", "module", "host", "label", label)
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Khôi phục sáng tác: " + label, Level: "info"})
	for _, w := range h.store.CheckConsistency() {
		slog.Warn("Cảnh báo nhất quán", "module", "host", "detail", w)
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Cảnh báo nhất quán: " + w, Level: "warn"})
	}
	h.refreshWriterRestore()
	h.observer.setAborting(false)
	h.router.ResetRepeat()
	h.router.Enable()
	if err := h.coordinator.Prompt(context.Background(), prompt); err != nil {
		return "", fmt.Errorf("resume prompt: %w", err)
	}
	// Chủ động dispatch lệnh đầu tiên một lần, tránh Coordinator chỉ trả văn bản cho resume prompt mà StopGuard chặn lặp lại.
	h.router.Dispatch()

	h.mu.Lock()
	h.lifecycle = lifecycleRunning
	h.mu.Unlock()
	go h.waitDone()
	return label, nil
}

// interventionMsg đóng gói văn bản người dùng thành tin nhắn can thiệp mà Coordinator nhận ra.
// Steer và Continue dùng chung cùng framing: lệnh người dùng từ cả hai lối vào đều có tiền tố `[Người dùng can thiệp]`,
// để kích hoạt ổn định phân loại can thiệp trong coordinator.md. Nếu không, văn bản thuần của Continue sẽ bỏ qua quy tắc routing,
// Coordinator mất điểm neo phân loại mà phái nhầm agent phụ (từng gây "sửa chương đã viết" bị phái cho writer va vào guard edit_chapter).
func interventionMsg(text string) agentcore.Message {
	return agentcore.UserMsg("[Người dùng can thiệp] " + text)
}

// Continue tiếp tục với prompt chỉ định. Gọi khi người dùng nhập vào hộp nhập sau khi dừng.
func (h *Host) Continue(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("text is required")
	}
	h.mu.Lock()
	if h.cocreating {
		h.mu.Unlock()
		return fmt.Errorf("đồng sáng tác giai đoạn đang diễn ra, vui lòng kết thúc đồng sáng tác trước")
	}
	running := h.lifecycle == lifecycleRunning
	h.mu.Unlock()

	h.emitEvent(Event{Time: time.Now(), Category: "USER", Summary: "[Tiếp tục] " + text, Level: "info"})

	if running {
		h.coordinator.FollowUp(interventionMsg(text))
		return nil
	}
	// Sau khi dừng → tiêm và tự động khôi phục (khôi phục run cũng chịu ràng buộc tiền đề ngân sách)
	if err := h.budget.Refuse(); err != nil {
		return err
	}
	h.refreshWriterRestore()
	h.observer.setAborting(false)
	_, err := h.coordinator.Inject(interventionMsg(text))
	if err != nil {
		return fmt.Errorf("inject: %w", err)
	}
	h.mu.Lock()
	h.lifecycle = lifecycleRunning
	h.mu.Unlock()
	go h.waitDone()
	return nil
}

// Steer gửi can thiệp của người dùng.
func (h *Host) Steer(text string) {
	h.mu.Lock()
	running := h.lifecycle == lifecycleRunning
	h.mu.Unlock()

	h.emitEvent(Event{Time: time.Now(), Category: "USER", Summary: "[Người dùng can thiệp] " + text, Level: "info"})

	msg := interventionMsg(text)
	if running {
		if _, err := h.coordinator.Inject(msg); err != nil {
			slog.Error("steer inject thất bại", "module", "host", "err", err)
		}
		return
	}
	// Đã dừng: lưu bền vào lần khởi động tiếp theo + phản hồi trạng thái hệ thống ("đã lưu" là thông báo hệ thống ngoài sự kiện USER)
	_ = h.store.RunMeta.SetPendingSteer(text)
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Can thiệp đã lưu, sẽ có hiệu lực lần khởi động tiếp theo", Level: "info"})
}

// Abort tạm dừng coordinator hiện tại.
func (h *Host) Abort() bool {
	return h.abortWithEvent("Người dùng tạm dừng thủ công sáng tác hiện tại", "warn")
}

// abortWithEvent thực hiện tạm dừng với sự kiện lý do chỉ định. Dừng do ngân sách và tạm dừng thủ công
// dùng chung cùng cơ chế dừng, chỉ khác văn bản sự kiện (dừng do ngân sách = lệnh Abort người dùng đã ký trước, ngữ nghĩa tương đương tạm dừng thủ công).
func (h *Host) abortWithEvent(summary, level string) bool {
	h.mu.Lock()
	running := h.lifecycle == lifecycleRunning
	if running {
		h.lifecycle = lifecyclePaused
	}
	h.mu.Unlock()
	if !running {
		return false
	}
	// Đặt cờ phải trước coordinator.Abort: lan truyền cancel sẽ ngay lập tức gây ra sự kiện
	// lỗi stream init / agent phụ, observer dựa vào cờ này nhận ra là nhiễu phát sinh từ abort và chặn lại.
	h.observer.setAborting(true)
	h.coordinator.Abort()
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
	return true
}

// Close dừng coordinator và đóng kênh sự kiện.
//
// Ngữ nghĩa persistence Usage: hủy autoSaveLoop trước (nó tự flush lần dirty cuối cùng),
// rồi bổ sung một lần SaveNow đồng bộ để kết thúc. Khoảng trống đã biết: sau AbortSilent nếu vẫn có
// lời gọi LLM in-flight trả về, OnMessage → Record được kích hoạt sẽ cập nhật bộ nhớ nhưng **không được lưu bền**.
// Phần "vài trăm token cuối" bị mất này sẽ được session jsonl replay tự động bù lại khi khởi động tiếp theo.
func (h *Host) Close() {
	h.observer.setAborting(true)
	h.coordinator.AbortSilent()
	if h.routerDetach != nil {
		h.routerDetach()
		h.routerDetach = nil
	}
	if h.budgetDetach != nil {
		h.budgetDetach()
		h.budgetDetach = nil
	}
	if h.usageCancel != nil {
		h.usageCancel()
		h.usageCancel = nil
	}
	if err := h.usage.SaveNow(); err != nil {
		slog.Warn("Lưu usage trước khi thoát thất bại", "module", "usage", "err", err)
	}
	h.closeOnce.Do(func() {
		close(h.done)
		close(h.events)
		close(h.streamCh)
	})
}

// waitDone chờ coordinator dừng và phát sự kiện trạng thái cuối.
//
// Không thực hiện bất kỳ tiếp tục nào. Run kết thúc = Host vào trạng thái cuối:
//   - Phase=Complete  → đánh dấu completed, phát sự kiện "sáng tác hoàn thành"
//   - Khác            → đánh dấu idle, phát sự kiện "Coordinator dừng"
//
// Người dùng muốn tiếp tục sáng tác chỉ có hai con đường: Continue thủ công (tiêm khi dừng) hoặc khởi động lại tiến trình theo Resume.
// Xem docs/architecture.md §13.3, §8.3.
func (h *Host) waitDone() {
	h.coordinator.WaitForIdle()
	h.observer.finalize()

	h.mu.Lock()
	progress, _ := h.store.Progress.Load()
	if progress != nil && progress.Phase == domain.PhaseComplete {
		h.lifecycle = lifecycleCompleted
		summary := fmt.Sprintf("Sáng tác hoàn thành: %d chương %d chữ", len(progress.CompletedChapters), progress.TotalWordCount)
		h.mu.Unlock()
		slog.Info(summary, "module", "host")
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "success"})
		h.notifier.Send(notify.Notification{
			Kind: "run_end", Level: "info", Title: "ainovel: Sáng tác hoàn thành",
			Body: h.runEndBody(progress.NovelName, summary),
		})
	} else {
		wasRunning := h.lifecycle == lifecycleRunning
		if wasRunning {
			h.lifecycle = lifecycleIdle
		}
		completed := 0
		name := ""
		if progress != nil {
			completed = len(progress.CompletedChapters)
			name = progress.NovelName
		}
		h.mu.Unlock()
		if wasRunning {
			summary := fmt.Sprintf("Coordinator dừng (đã hoàn thành %d chương)", completed)
			slog.Warn(summary, "module", "host")
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "warn"})
			h.notifier.Send(notify.Notification{
				Kind: "run_end", Level: "warn", Title: "ainovel: Sáng tác dừng",
				Body: h.runEndBody(name, summary),
			})
		}
	}

	select {
	case h.done <- struct{}{}:
	default:
	}
}

// runEndBody tạo nội dung thông báo run_end: tên sách + tóm tắt tiến trình + tổng chi phí.
func (h *Host) runEndBody(novelName, summary string) string {
	if name := strings.TrimSpace(novelName); name != "" {
		summary = "《" + name + "》" + summary
	}
	cost, _, _, _, _ := h.usage.Totals()
	if cost > 0 {
		summary += fmt.Sprintf(" · Chi phí $%.2f", cost)
	}
	return summary
}

// ── Kênh ──

// StreamClearSentinel gửi một tin đơn qua streamCh để báo hiệu "xóa round stream hiện tại".
// Không dùng clearCh độc lập nữa — hai kênh không có thứ tự khiến ✻ header thường rơi vào cuối round trước.
const StreamClearSentinel = "\x00\x00CLEAR\x00\x00"

func (h *Host) Events() <-chan Event        { return h.events }
func (h *Host) Stream() <-chan string       { return h.streamCh }
func (h *Host) Done() <-chan struct{}       { return h.done }
func (h *Host) Dir() string                 { return h.store.Dir() }
func (h *Host) AskUser() *tools.AskUserTool { return h.askUser }

// SaveBrief lưu bản yêu cầu sáng tác đã trích xuất vào brief.md, để Kiến trúc sư và Người viết đọc
// lại được qua novel_context ở mọi lượt sau — kể cả khi lịch sử hội thoại đã bị nén.
// Phải gọi TRƯỚC Start: brief là đầu vào của chính lượt lập kế hoạch đầu tiên.
func (h *Host) SaveBrief(content string) error { return h.store.Brief.Save(content) }

// ── Phát sự kiện ──

func (h *Host) emitEvent(ev Event) {
	defer func() { recover() }()
	// Điểm slog duy nhất cho tất cả sự kiện. Sự kiện agentcore được observer dịch và sự kiện
	// SYSTEM tự phát của Host (Start/Abort/Resume...) đều ghi log ở đây, tránh ESC abort và thoát ngoài
	// không phân biệt được trên tui.log.
	if ev.Summary != "" || ev.Detail != "" {
		level := slog.LevelInfo
		switch ev.Level {
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
		// Log đầy đủ Detail (dùng để tra cứu, không cắt ngắn); khi Detail rỗng mới dùng Summary.
		msg := ev.Detail
		if msg == "" {
			msg = ev.Summary
		}
		attrs := []any{"module", "event", "category", ev.Category, "agent", ev.Agent}
		if ev.Kind != "" {
			attrs = append(attrs, "kind", ev.Kind)
		}
		slog.Log(context.Background(), level, msg, attrs...)
	}
	select {
	case h.events <- ev:
	default:
		select {
		case <-h.events:
		default:
		}
		select {
		case h.events <- ev:
		default:
		}
	}
}

// emitDelta gửi một mảnh stream tới TUI mà KHÔNG làm mất chữ và KHÔNG đảo thứ tự. streamCh có
// buffer hữu hạn; khi LLM stream nhanh hơn tốc độ TUI tiêu thụ, kênh đầy. Phiên bản cũ vứt thẳng
// mảnh cũ nhất chưa đọc rồi nhét mảnh mới → nội dung khuyết giữa dòng (từ dính nhau, mất dấu cách,
// ký tự biến mất trong từ).
//
// Cách sửa: GỘP phía nhà sản xuất. Kênh Go chỉ cho lấy phần tử ở đầu (cũ nhất), không chạm được
// đuôi, nên gộp "cũ nhất + mới" rồi đẩy lại sẽ ĐẢO thứ tự. Thay vào đó, khi kênh đầy ta GIỮ mảnh
// văn bản lại ở streamCarry và nối nó vào ĐẦU delta của lần gọi sau — thứ tự [carry][delta] luôn
// đúng, không mảnh nào bị mất.
//
// Bất biến: chỉ một goroutine (vòng sự kiện coordinator) gọi emitDelta/emitClear, nên streamCarry
// không cần khóa và thao tác không bao giờ block/stall agent loop.
//
// StreamClearSentinel (ranh giới round) không được gộp lẫn vào văn bản (nếu không hai round dính
// nhau, ✻ header lệch đoạn): đẩy nốt carry trước rồi gửi sentinel riêng. Nếu kênh vẫn đầy đúng lúc
// ranh giới round (hiếm), bỏ qua sentinel — chỉ mất một khoảng ngắt, không mất chữ.
//
// Rò rỉ tối thiểu đã biết: nếu một run bị abort đúng lúc kênh đang quá tải, streamCarry có thể còn
// sót phần đuôi và bị nối vào delta đầu tiên của run kế. Hiếm và chỉ là nhiễu hiển thị nhỏ; không
// reset xuyên goroutine để tránh data race trên streamCarry.
func (h *Host) emitDelta(delta string) {
	defer func() { recover() }()

	if delta == StreamClearSentinel {
		h.flushStreamCarry() // đẩy nốt văn bản tồn trước ranh giới round, giữ thứ tự
		select {
		case h.streamCh <- delta:
		default: // quá tải đúng ranh giới round: bỏ qua, chỉ mất khoảng ngắt
		}
		return
	}

	if h.streamCarry != "" {
		delta = h.streamCarry + delta // nối carry vào ĐẦU: [carry][delta]
		h.streamCarry = ""
	}
	select {
	case h.streamCh <- delta:
	default:
		// Kênh đầy: giữ lại để gộp và gửi ở lần sau — không mất chữ, không đảo thứ tự.
		h.streamCarry = delta
	}
}

// flushStreamCarry cố đẩy phần văn bản còn tồn ở streamCarry; nếu kênh vẫn đầy thì giữ nguyên
// để lần emitDelta sau gộp tiếp. Chỉ gọi từ goroutine sự kiện coordinator.
func (h *Host) flushStreamCarry() {
	if h.streamCarry == "" {
		return
	}
	select {
	case h.streamCh <- h.streamCarry:
		h.streamCarry = ""
	default:
	}
}

func (h *Host) emitClear() {
	// Đi qua streamCh bằng "sentinel", đảm bảo đến TUI theo thứ tự trên cùng một kênh với emitDelta.
	h.emitDelta(StreamClearSentinel)
}

// ── Snapshot (tổng hợp trạng thái TUI) ──

func (h *Host) Snapshot() UISnapshot {
	h.mu.Lock()
	state := h.lifecycle
	provider, model, _ := h.models.CurrentSelection("default")
	h.mu.Unlock()

	// Phân giải động cửa sổ ngữ cảnh của mô hình hiện tại, tự động phản ánh sau khi /model chuyển đổi
	modelWindow, _ := h.cfg.ResolveContextWindow(model)
	cost, tokIn, tokOut, cacheRead, cacheWrite := h.usage.Totals()
	saved := h.usage.SavedUSD()
	overallCapable := h.usage.OverallCacheCapable()
	recentRead, recentInput, recentSamples := h.usage.OverallRecent()
	perAgent := h.usage.PerAgent()
	cacheStats := make([]AgentCacheStat, 0, len(perAgent))
	for _, a := range perAgent {
		cacheStats = append(cacheStats, AgentCacheStat{
			Role:            a.Role,
			Input:           a.Input,
			Output:          a.Output,
			CacheRead:       a.CacheRead,
			CacheWrite:      a.CacheWrite,
			Cost:            a.Cost,
			Saved:           a.Saved,
			CacheCapable:    a.CacheCapable,
			RecentCacheRead: a.RecentCacheRead,
			RecentInput:     a.RecentInput,
			RecentSamples:   a.RecentSamples,
		})
	}
	perModel := h.usage.PerModel()
	modelStats := make([]AgentCacheStat, 0, len(perModel))
	for _, a := range perModel {
		modelStats = append(modelStats, AgentCacheStat{
			Model:        a.Model,
			Input:        a.Input,
			Output:       a.Output,
			CacheRead:    a.CacheRead,
			CacheWrite:   a.CacheWrite,
			Cost:         a.Cost,
			Saved:        a.Saved,
			CacheCapable: a.CacheCapable,
		})
	}

	snap := UISnapshot{
		Provider:               provider,
		ModelName:              model,
		ModelContextWindow:     modelWindow,
		Style:                  h.cfg.Style,
		RuntimeState:           string(state),
		IsRunning:              state == lifecycleRunning,
		TotalInputTokens:       tokIn,
		TotalOutputTokens:      tokOut,
		TotalCacheReadTokens:   cacheRead,
		TotalCacheWriteTokens:  cacheWrite,
		TotalCostUSD:           cost,
		TotalSavedUSD:          saved,
		BudgetLimitUSD:         h.budget.Limit(),
		OverallCacheCapable:    overallCapable,
		OverallRecentCacheRead: recentRead,
		OverallRecentInput:     recentInput,
		OverallRecentSamples:   recentSamples,
		CachePerAgent:          cacheStats,
		CachePerModel:          modelStats,
		MissingAssistantUsage:  h.usage.MissingAssistantUsage(),
	}

	progress, _ := h.store.Progress.Load()
	if progress != nil {
		snap.NovelName = strings.TrimSpace(progress.NovelName)
		snap.Phase = string(progress.Phase)
		snap.Flow = string(progress.Flow)
		snap.CurrentChapter = progress.CurrentChapter
		snap.TotalChapters = progress.TotalChapters
		snap.CompletedCount = len(progress.CompletedChapters)
		snap.TotalWordCount = progress.TotalWordCount
		snap.InProgressChapter = progress.InProgressChapter
		snap.PendingRewrites = progress.PendingRewrites
		snap.RewriteReason = progress.RewriteReason
		snap.Layered = progress.Layered
		if progress.CurrentVolume > 0 {
			snap.CurrentVolumeArc = fmt.Sprintf("Tập %d·Cung %d", progress.CurrentVolume, progress.CurrentArc)
		}
	}
	if snap.NovelName == "" {
		if premise, _ := h.store.Outline.LoadPremise(); premise != "" {
			snap.NovelName = domain.ExtractNovelNameFromPremise(premise)
		}
	}
	if meta, _ := h.store.RunMeta.Load(); meta != nil {
		snap.PendingSteer = meta.PendingSteer
	}

	snap.Agents = h.observer.agentSnapshots()
	h.fillContextStatus(&snap)
	snap.StatusLabel = deriveStatusLabel(snap)

	// Nhãn khôi phục
	if _, label, err := buildResumePrompt(h.store); err == nil && label != "" {
		snap.RecoveryLabel = label
	}

	h.fillDetails(&snap, progress)

	return snap
}

// fillContextStatus điền thông tin sức khỏe ngữ cảnh của Coordinator.
func (h *Host) fillContextStatus(snap *UISnapshot) {
	if h.coordinator == nil {
		return
	}
	if usage := h.coordinator.BaselineContextUsage(); usage != nil {
		snap.ContextTokens = usage.Tokens
		snap.ContextWindow = usage.ContextWindow
		snap.ContextPercent = usage.Percent
	}
	if ctx := h.coordinator.ContextSnapshot(); ctx != nil {
		snap.ContextScope = ctx.Scope
		snap.ContextStrategy = ctx.LastStrategy
		snap.ContextActiveMessages = ctx.ActiveMessages
		snap.ContextSummaryCount = ctx.SummaryMessages
		snap.ContextCompactedCount = ctx.LastCompactedCount
		snap.ContextKeptCount = ctx.LastKeptCount
		if snap.ContextTokens == 0 {
			if ctx.BaselineUsage != nil {
				snap.ContextTokens = ctx.BaselineUsage.Tokens
				snap.ContextWindow = ctx.BaselineUsage.ContextWindow
				snap.ContextPercent = ctx.BaselineUsage.Percent
			} else if ctx.Usage != nil {
				snap.ContextTokens = ctx.Usage.Tokens
				snap.ContextWindow = ctx.Usage.ContextWindow
				snap.ContextPercent = ctx.Usage.Percent
			}
		}
	}
}

// fillDetails điền khu vực chi tiết: thiết lập, nhân vật, commit/review/tóm tắt gần đây.
func (h *Host) fillDetails(snap *UISnapshot, progress *domain.Progress) {
	if premise, _ := h.store.Outline.LoadPremise(); premise != "" {
		snap.Premise = truncate(premise, 80)
	}
	if outline, _ := h.store.Outline.LoadOutline(); len(outline) > 0 {
		for _, e := range outline {
			snap.Outline = append(snap.Outline, OutlineSnapshot{
				Chapter: e.Chapter, Title: e.Title, CoreEvent: e.CoreEvent,
			})
		}
	}
	if progress != nil && progress.Layered {
		if compass, _ := h.store.Outline.LoadCompass(); compass != nil {
			snap.CompassDirection = compass.EndingDirection
			snap.CompassScale = compass.EstimatedScale
		}
		if volumes, _ := h.store.Outline.LoadLayeredOutline(); len(volumes) > 0 {
			for _, v := range volumes {
				if v.Index > progress.CurrentVolume {
					snap.NextVolumeTitle = v.Title
					break
				}
			}
		}
	}
	if chars, _ := h.store.Characters.Load(); len(chars) > 0 {
		for _, c := range chars {
			label := c.Name
			if c.Role != "" {
				label += "（" + c.Role + "）"
			}
			snap.Characters = append(snap.Characters, label)
		}
	}
	if ledger, _ := h.store.Cast.Load(); len(ledger) > 0 {
		snap.SupportingCount = len(ledger)
		recent, _ := h.store.Cast.RecentActive(5)
		for _, e := range recent {
			label := e.Name
			if e.BriefRole != "" {
				label += "（" + e.BriefRole + "）"
			}
			snap.RecentSupporting = append(snap.RecentSupporting, label)
		}
	}
	if progress != nil && len(progress.CompletedChapters) > 0 {
		lastCh := progress.CompletedChapters[len(progress.CompletedChapters)-1]
		wc := progress.ChapterWordCounts[lastCh]
		snap.LastCommitSummary = fmt.Sprintf("Chương %d %d chữ", lastCh, wc)
	}
	currentCh := 1
	if progress != nil && len(progress.CompletedChapters) > 0 {
		currentCh = progress.CompletedChapters[len(progress.CompletedChapters)-1]
	}
	if review, err := h.store.World.LoadLastReview(currentCh); err == nil && review != nil {
		snap.LastReviewSummary = fmt.Sprintf("verdict=%s %d vấn đề", review.Verdict, len(review.Issues))
		if len(review.AffectedChapters) > 0 {
			snap.LastReviewSummary += fmt.Sprintf(" ảnh hưởng %v", review.AffectedChapters)
		}
	}
	if cp := h.store.Checkpoints.LatestGlobal(); cp != nil {
		snap.LastCheckpointName = fmt.Sprintf("%s.%s", cp.Scope, cp.Step)
	}
	if progress != nil {
		for i := len(progress.CompletedChapters) - 1; i >= 0 && len(snap.RecentSummaries) < 2; i-- {
			ch := progress.CompletedChapters[i]
			if summary, err := h.store.Summaries.LoadSummary(ch); err == nil && summary != nil {
				snap.RecentSummaries = append(snap.RecentSummaries,
					fmt.Sprintf("Chương %d: %s", ch, truncate(summary.Summary, 50)))
			}
		}
	}
}

func deriveStatusLabel(s UISnapshot) string {
	switch {
	case s.Phase == string(domain.PhaseComplete):
		return "COMPLETE"
	case s.Flow == string(domain.FlowReviewing):
		return "REVIEW"
	case s.Flow == string(domain.FlowRewriting) || s.Flow == string(domain.FlowPolishing):
		return "REWRITE"
	case s.RuntimeState == "running":
		return "RUNNING"
	default:
		return "READY"
	}
}

// ── Quản lý mô hình ──

func (h *Host) ConfiguredProviders() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	providers := make([]string, 0, len(h.cfg.Providers))
	for name := range h.cfg.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return providers
}

func (h *Host) ConfiguredModels(provider string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.CandidateModels(provider)
}

func (h *Host) CurrentModelSelection(role string) (string, string, bool) {
	return h.models.CurrentSelection(role)
}

func (h *Host) SwitchModel(role, provider, model string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if provider == "" || model == "" {
		return fmt.Errorf("provider and model are required")
	}
	if err := h.models.Swap(role, provider, model); err != nil {
		return err
	}
	if role == "" || role == "default" {
		h.cfg.Provider = provider
		h.cfg.ModelName = model
	} else {
		if h.cfg.Roles == nil {
			h.cfg.Roles = make(map[string]bootstrap.RoleConfig)
		}
		rc := h.cfg.Roles[role]
		rc.Provider = provider
		rc.Model = model
		h.cfg.Roles[role] = rc
	}
	if path := bootstrap.DefaultConfigPath(); path != "" {
		if err := bootstrap.SaveConfig(path, h.cfg); err != nil {
			slog.Warn("Lưu cấu hình thất bại", "module", "host", "err", err)
		}
	}
	// Khi chuyển sang mô hình chưa đăng ký thì in một dòng warn, nhắc người dùng đang dùng fallback 128k — truyện dài dễ bị nén sớm.
	logRole := role
	if logRole == "" {
		logRole = "default"
	}
	window, source := h.cfg.ResolveContextWindow(model)
	bootstrap.LogContextWindowChoice(logRole, model, window, source)

	// Khi chuyển sang default/coordinator, liên động cửa sổ và reserve của coordinator engine.
	// writer/architect/editor dùng ContextManagerFactory tự động tái tạo theo mô hình mới, không cần liên động.
	// Không liên động sẽ gây ra: khi chuyển 1M→128k, coordinator engine vẫn tính threshold theo 1M,
	// tích lũy messages vượt 128k sẽ báo lỗi API; khi 128k→1M thì threshold bị ghim ở 96k, lãng phí ngữ cảnh dài.
	//
	// Quan trọng: phải dùng models.CurrentSelection("coordinator") để lấy mô hình "coordinator thực tế đang dùng"
	// tính cửa sổ — không dùng trực tiếp model đích chuyển đổi. Khi người dùng cấu hình roles.coordinator mô hình riêng,
	// chuyển default không ảnh hưởng mô hình thực tế của coordinator; dùng cửa sổ của đích chuyển đổi để SetContextWindow sẽ sai
	// khi điều chỉnh threshold coordinator engine sang giá trị không liên quan (ví dụ: chuyển default sang mô hình 1M thì kéo
	// threshold của coordinator engine 200k lên 891k, viết quá 200k sẽ báo lỗi API ngay).
	if h.coordinatorCtxMgr != nil && (role == "" || role == "default" || role == "coordinator") {
		_, coordinatorModel, _ := h.models.CurrentSelection("coordinator")
		coordinatorWindow, coordSource := h.cfg.ResolveContextWindow(coordinatorModel)
		h.coordinator.SetContextWindow(coordinatorWindow)
		h.coordinatorCtxMgr.SetContextWindow(coordinatorWindow)
		h.coordinatorCtxMgr.SetReserveTokens(bootstrap.CompactReserveTokens(coordinatorWindow))
		// Khi mô hình thực tế của coordinator khác đích chuyển đổi (người dùng chuyển default nhưng coordinator có role riêng),
		// LogContextWindowChoice ở trên in cửa sổ của default, không nhất quán với giá trị thực tế; bổ sung thêm một dòng.
		if coordinatorModel != model {
			bootstrap.LogContextWindowChoice("coordinator", coordinatorModel, coordinatorWindow, coordSource)
		}
	}

	h.emitEvent(Event{
		Time:     time.Now(),
		Category: "SYSTEM",
		Summary:  fmt.Sprintf("Đã chuyển mô hình: %s → %s/%s", role, provider, model),
		Level:    "info",
	})
	return nil
}

// roleExtractor là vai trò riêng cho Requirement Extractor (/load).
// Tách khỏi architect để dùng được model mạnh cho một lần chạy mỗi cuốn sách,
// mà không kéo theo chi phí của architect vốn bị gọi liên tục.
const roleExtractor = "extractor"

// concreteThinkingRoles là các role cụ thể có thể áp dụng cường độ suy nghĩ (nhất quán với routing agents.ApplyThinking).
// Khi gọi default thì lặp qua từng role cụ thể để áp dụng lại theo ResolveThinking.
//
// Không có extractor: ApplyThinking chỉ định tuyến tới các agent đang chạy, còn extractor
// là luồng phụ gọi LLM trực tiếp và tự đọc ResolveThinking ở mỗi lần chạy (xem ExtractRequirement).
var concreteThinkingRoles = []string{"coordinator", "architect", "writer", "editor"}

// CurrentThinking trả về chuỗi cường độ suy nghĩ hiện tại của một role (để panel /model đồng bộ giá trị hiện tại).
func (h *Host) CurrentThinking(role string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.ResolveThinking(strings.ToLower(strings.TrimSpace(role)))
}

// SetRoleThinking đặt cường độ suy nghĩ cho một role (hoặc default): xác thực→lưu bền→liên động live agent→sự kiện.
// Cấu trúc tương tự SwitchModel; độc lập với lựa chọn mô hình, có thể điều chỉnh riêng. level rỗng = không ghi đè (kế thừa).
func (h *Host) SetRoleThinking(role, level string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	parsed, err := agents.ParseThinkingLevel(level)
	if err != nil {
		return err
	}
	role = strings.ToLower(strings.TrimSpace(role))

	// Lưu bền: role cụ thể ghi Roles[role].Thinking, default/"" ghi Thinking ở cấp cao nhất.
	if role == "" || role == "default" {
		h.cfg.Thinking = string(parsed)
	} else {
		if h.cfg.Roles == nil {
			h.cfg.Roles = make(map[string]bootstrap.RoleConfig)
		}
		rc := h.cfg.Roles[role]
		rc.Thinking = string(parsed)
		h.cfg.Roles[role] = rc
	}
	if path := bootstrap.DefaultConfigPath(); path != "" {
		if err := bootstrap.SaveConfig(path, h.cfg); err != nil {
			slog.Warn("Lưu cấu hình thất bại", "module", "host", "err", err)
		}
	}

	// Liên động live: role cụ thể áp dụng trực tiếp; default thì duyệt từng role cụ thể áp dụng lại theo ResolveThinking
	// (những role đã bị ghi đè ở cấp role thì giữ nguyên, chưa ghi đè thì nhận mặc định mới).
	if h.thinkingApplier != nil {
		if role == "" || role == "default" {
			for _, r := range concreteThinkingRoles {
				lv, _ := agents.ParseThinkingLevel(h.cfg.ResolveThinking(r))
				h.thinkingApplier(r, lv)
			}
		} else {
			h.thinkingApplier(role, parsed)
		}
	}

	logRole := role
	if logRole == "" {
		logRole = "default"
	}
	shown := string(parsed)
	if shown == "" {
		shown = "mặc định (kế thừa)"
	}
	h.emitEvent(Event{
		Time:     time.Now(),
		Category: "SYSTEM",
		Summary:  fmt.Sprintf("Đã chuyển cường độ suy nghĩ: %s → %s", logRole, shown),
		Level:    "info",
	})
	return nil
}

// ── Phát lại sự kiện ──

func (h *Host) ReplayQueue(afterSeq int64) ([]domain.RuntimeQueueItem, error) {
	if h.store == nil || h.store.Runtime == nil {
		return nil, nil
	}
	return h.store.Runtime.LoadQueueAfter(afterSeq)
}

// ── Đồng sáng tác ──

// CoCreateStream khởi động lạnh đồng sáng tác: làm rõ yêu cầu từ đầu, tạo ra lệnh sáng tác cho cả cuốn sách.
func (h *Host) CoCreateStream(ctx context.Context, history []CoCreateMessage, onProgress func(kind, text string)) (CoCreateReply, error) {
	return coCreateStream(ctx, h.models, h.store.Sessions, coCreateSystemPrompt, history, onProgress)
}

// StageCoCreateStream đồng sáng tác giai đoạn: lập kế hoạch hướng tiếp theo dựa trên nội dung đã viết.
// System prompt = stage prompt + tóm tắt trạng thái câu chuyện hiện tại, để trợ lý biết "đã viết gì rồi".
func (h *Host) StageCoCreateStream(ctx context.Context, history []CoCreateMessage, onProgress func(kind, text string)) (CoCreateReply, error) {
	return coCreateStream(ctx, h.models, h.store.Sessions, stageSystemPrompt(h.store), history, onProgress)
}

// stagePlanPrefix đóng gói "brief hướng tiếp theo" từ đồng sáng tác thành một can thiệp quy hoạch giai đoạn, giao Coordinator phán xét.
// Chỉ dán nhãn thực tế [Quy hoạch giai đoạn] + trình bày trung tính, không viết cứng "cách thực hiện" — routing cụ thể (compass / architect /
// save_directive) giao cho tiêu chí "quy hoạch giai đoạn" trong coordinator.md, tránh tạo nguồn sự thật thứ hai với prompt,
// cũng không chặn yêu cầu phong cách đi theo directive (giữ "phân loại giao LLM phán xét"). Continue sẽ chồng thêm tiền tố [Người dùng can thiệp].
const stagePlanPrefix = "[Quy hoạch giai đoạn] Tôi đã tạm dừng sáng tác và cùng trợ lý đồng sáng tác xem xét hướng tiếp theo dưới đây. Vui lòng phân loại can thiệp theo tiêu chí của bạn để quyết định cách thực hiện, rồi tiếp tục sáng tác. Hướng tiếp theo như sau:\n\n"

// PauseForCoCreate vào đồng sáng tác giai đoạn: đặt cờ chiếm dụng đồng sáng tác, nếu đang chạy thì đồng thời tạm dừng coordinator.
// Trả về false nghĩa là không thể vào (toàn bộ sách đã hoàn thành hoặc đang trong đồng sáng tác), người gọi có thể bỏ qua.
// Cờ chiếm dụng trong cửa sổ đồng sáng tác chặn can thiệp đồng thời của import/simulate/start/resume/continue —
// khi đang chạy tạm dừng thì lifecycle=paused, mutex ==running hiện có không còn hiệu lực, dùng cờ này bù vào;
// đã dừng (idle/paused) cũng cho phép vào, sau khi lập kế hoạch xong dùng Continue để tiếp tục.
func (h *Host) PauseForCoCreate() bool {
	h.mu.Lock()
	if h.cocreating || h.lifecycle == lifecycleCompleted {
		h.mu.Unlock()
		return false
	}
	h.cocreating = true
	running := h.lifecycle == lifecycleRunning
	h.mu.Unlock()

	// Khi đang chạy thì tái sử dụng abortWithEvent để dừng (running→paused + setAborting + Abort + sự kiện), cùng thứ tự với
	// tạm dừng thủ công, không chép lại; đã dừng (idle/paused) chỉ đặt cờ, sau khi lập kế hoạch xong dùng Continue tiếp tục.
	if running {
		h.abortWithEvent("Vào đồng sáng tác giai đoạn, sáng tác đã tạm dừng", "info")
	} else {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Vào đồng sáng tác giai đoạn", Level: "info"})
	}
	return true
}

// ResumeFromCoCreate kết thúc đồng sáng tác giai đoạn: tiêm hướng tiếp theo từ đồng sáng tác làm can thiệp và khôi phục sáng tác.
// Sau khi xóa cờ chiếm dụng thì tái sử dụng đường tiêm khi dừng của Continue (chịu ràng buộc tiền đề ngân sách).
// Lưu ý: khi draft rỗng thì trả về sớm mà không xóa cờ là có chủ ý (đồng sáng tác chưa kết thúc); guard canStart() phía TUI
// dùng cùng tiêu chí "không rỗng", đảm bảo đường này không thể đạt được, cocreating sẽ không bị rò rỉ vì điều này.
func (h *Host) ResumeFromCoCreate(draft string) error {
	draft = strings.TrimSpace(draft)
	if draft == "" {
		return fmt.Errorf("draft is required")
	}
	h.mu.Lock()
	if !h.cocreating {
		h.mu.Unlock()
		return fmt.Errorf("not in co-create")
	}
	h.cocreating = false
	h.mu.Unlock()

	// Abort của PauseForCoCreate là bất đồng bộ: chờ run cũ hội tụ trước khi khôi phục, về cùng tiền đề "thực sự đã dừng"
	// như Continue sau tạm dừng thủ công, tránh steer lệnh tiếp tục vào run cũ đang thoát. Khi vào đồng sáng tác từ trạng thái
	// không chạy (chưa Abort) thì coordinator vốn đã idle, WaitForIdle trả về ngay.
	h.coordinator.WaitForIdle()

	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Đồng sáng tác giai đoạn hoàn thành, đã tiêm hướng tiếp theo và khôi phục sáng tác", Level: "info"})
	return h.Continue(stagePlanPrefix + draft)
}

// CancelCoCreate từ bỏ đồng sáng tác giai đoạn: xóa cờ chiếm dụng, giữ trạng thái tạm dừng (người dùng có thể tiếp tục trong hộp nhập hoặc khởi động lại Resume).
func (h *Host) CancelCoCreate() {
	h.mu.Lock()
	if !h.cocreating {
		h.mu.Unlock()
		return
	}
	h.cocreating = false
	h.mu.Unlock()
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: "Đã thoát đồng sáng tác giai đoạn, sáng tác giữ trạng thái tạm dừng (có thể tiếp tục trong hộp nhập)", Level: "info"})
}

// ── Tiện ích ──

func (h *Host) refreshWriterRestore() {
	if h.writerRestore != nil {
		h.writerRestore.Refresh(h.store)
	}
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// ImportFrom khởi động một lần nhập khẩu phản suy từ tiểu thuyết bên ngoài: phân đoạn → phản suy foundation → phân tích từng chương lưu xuống đĩa.
// Loại trừ lẫn nhau với Coordinator; sau khi nhập khẩu xong người gọi có thể lập tức Resume() để tiếp tục viết.
// Kênh sự kiện trả về được đóng bởi imp.Run, người gọi có trách nhiệm tiêu thụ (đầy thì bỏ để tránh chặn goroutine phân tích).
func (h *Host) ImportFrom(ctx context.Context, opts imp.Options) (<-chan imp.Event, error) {
	if err := h.guardExclusive("nhập khẩu"); err != nil {
		return nil, err
	}

	rulesOpts := rules.DefaultOptions(h.bundle.RulesFS)
	deps := imp.Deps{
		Store:      h.store,
		CommitTool: tools.NewCommitChapterTool(h.store).WithRules(rulesOpts),
		LLM:        h.models.ForRole("architect"),
		Prompts: imp.Prompts{
			Foundation: h.bundle.Prompts.ImportFoundation,
			Analyzer:   h.bundle.Prompts.ImportAnalyzer,
		},
	}
	return imp.Run(ctx, deps, opts)
}

// Simulate đọc thư mục simulate và tạo hoặc cập nhật gia tăng hồ sơ hành văn mô phỏng.
func (h *Host) Simulate(ctx context.Context) (<-chan sim.Event, error) {
	if err := h.guardExclusive("tạo hồ sơ hành văn mô phỏng"); err != nil {
		return nil, err
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working dir: %w", err)
	}
	deps := sim.Deps{
		Store: h.store,
		LLM:   h.models.ForRole("architect"),
		Prompts: sim.Prompts{
			Source: h.bundle.Prompts.SimulationSource,
			Merge:  h.bundle.Prompts.SimulationMerge,
		},
	}
	return sim.Run(ctx, deps, sim.Options{SourceDir: filepath.Join(wd, "simulate")})
}

// ExtractRequirement chạy Requirement Extractor: đọc một file yêu cầu sáng tác có cấu trúc,
// ánh xạ thành brief chuẩn hóa + báo cáo coverage, trả về cho TUI review trước khi bắt đầu sáng tác.
// Luồng phụ độc lập, không đụng flow.
//
// Dùng role riêng "extractor" chứ không dùng chung architect: đây là việc chạy MỘT lần cho mỗi
// cuốn sách nhưng đòi hỏi bám sát chỉ dẫn và chép chính xác danh từ riêng (kể cả dấu tiếng Việt),
// nên đáng dùng model mạnh; trong khi architect bị gọi liên tục nên thường được ghim ở model rẻ.
// Chưa cấu hình extractor thì lùi về architect — giữ nguyên hành vi cũ.
func (h *Host) ExtractRequirement(ctx context.Context, path string) (<-chan req.Event, error) {
	if err := h.guardExclusive("trích xuất yêu cầu sáng tác"); err != nil {
		return nil, err
	}
	thinking, err := agents.ParseThinkingLevel(h.cfg.ResolveThinking(roleExtractor))
	if err != nil {
		slog.Warn("bỏ qua cấu hình cường độ thinking không hợp lệ", "module", "req", "role", roleExtractor, "err", err)
		thinking = ""
	}
	deps := req.Deps{
		LLM:      h.models.ForRoleChain(roleExtractor, "architect"),
		Prompt:   h.bundle.Prompts.RequirementExtractor,
		Thinking: thinking,
	}
	return req.Run(ctx, deps, req.Options{Path: path})
}

// ImportSimulationProfile nhập khẩu hồ sơ hành văn mô phỏng đã tạo trước đó.
func (h *Host) ImportSimulationProfile(ctx context.Context, path string) (<-chan sim.Event, error) {
	if err := h.guardExclusive("nhập khẩu hồ sơ hành văn mô phỏng"); err != nil {
		return nil, err
	}
	return sim.RunImport(ctx, h.store, path)
}

// guardExclusive kiểm tra chiếm dụng độc quyền: từ chối các lối vào thay đổi trạng thái (import/simulate)
// khi coordinator đang chạy hoặc trong cửa sổ đồng sáng tác giai đoạn.
// Bù khoảng trống đồng thời khi chỉ kiểm tra ==running trong thời gian paused.
func (h *Host) guardExclusive(action string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.lifecycle == lifecycleRunning:
		return fmt.Errorf("coordinator đang chạy, vui lòng tạm dừng trước khi %s", action)
	case h.cocreating:
		return fmt.Errorf("đồng sáng tác giai đoạn đang diễn ra, vui lòng kết thúc đồng sáng tác trước khi %s", action)
	}
	return nil
}

// Export xuất các chương đã hoàn thành ra file bên ngoài (hiện chỉ hỗ trợ TXT).
//
// Khác với ImportFrom: xuất là thao tác chỉ đọc (không động đến Progress / Checkpoint),
// vì vậy **không yêu cầu Coordinator rảnh** — có thể xuất "sản phẩm giai đoạn hiện tại" bất cứ lúc nào trong khi viết.
// Chỉ đọc snapshot nhất quán của Progress.CompletedChapters + bản thảo cuối chương + đề cương + premise.
func (h *Host) Export(ctx context.Context, opts exp.Options) (*exp.Result, error) {
	return exp.Run(ctx, exp.Deps{Store: h.store}, opts)
}
