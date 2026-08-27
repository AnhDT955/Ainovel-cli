package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/errs"
)

const (
	// Trong tình huống đầu ra dài + ctx dài, với nhà cung cấp hỗ trợ suy luận
	// (mimo / deepseek-r1 v.v.), phía server có thể không stream reasoning delta.
	// Năm phút bao phủ luồng viết thông thường mà vẫn phát hiện kết nối chết sớm.
	streamIdleTimeout = 5 * time.Minute

	// Requirement Extractor phải đọc trọn tài liệu rồi dựng một JSON lớn. Một số
	// proxy Anthropic giữ im lặng toàn bộ giai đoạn suy nghĩ, thực tế đã vượt quá
	// 5 phút dù request vẫn chạy bình thường. Với riêng extractor, tắt idle
	// watchdog và dùng request timeout hữu hạn làm cầu chì; người dùng vẫn có thể
	// hủy ngay từ modal bằng Esc.
	extractorStreamIdleTimeout = 0
	extractorRequestTimeout    = 20 * time.Minute
)

type modelRuntimeConfig struct {
	requestTimeout    time.Duration
	streamIdleTimeout time.Duration
}

func runtimeConfigForRole(role string) modelRuntimeConfig {
	if role == "extractor" {
		return modelRuntimeConfig{
			requestTimeout:    extractorRequestTimeout,
			streamIdleTimeout: extractorStreamIdleTimeout,
		}
	}
	return modelRuntimeConfig{streamIdleTimeout: streamIdleTimeout}
}

// FailoverEvent biểu diễn một lần chuyển đổi nhà cung cấp tường minh.
// Reason là nhãn ngắn (rate_limit / timeout / stream_idle / network), dùng cho log có cấu trúc.
type FailoverEvent struct {
	Role         string
	Reason       string
	FromProvider string
	FromModel    string
	ToProvider   string
	ToModel      string
	Err          error
}

// FailoverReporter được gọi khi xảy ra chuyển đổi nhà cung cấp tường minh.
type FailoverReporter func(FailoverEvent)

type modelTarget struct {
	provider string
	name     string
	model    agentcore.ChatModel
}

// SwappableModel là wrapper ChatModel có thể hoán đổi nóng.
// Các yêu cầu đã bắt đầu tiếp tục dùng instance cũ; các yêu cầu tiếp theo tự động chuyển sang instance mới.
type SwappableModel struct {
	*agentcore.SwappableModel
	mu       sync.RWMutex
	provider string
	name     string
}

func NewSwappableModel(provider, name string, model agentcore.ChatModel) *SwappableModel {
	return &SwappableModel{
		SwappableModel: agentcore.NewSwappableModel(model),
		provider:       provider,
		name:           name,
	}
}

func (m *SwappableModel) ProviderName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}

func (m *SwappableModel) Info() llm.ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if info, ok := m.SwappableModel.Current().(interface{ Info() llm.ModelInfo }); ok {
		modelInfo := info.Info()
		if modelInfo.Name == "" {
			modelInfo.Name = m.name
		}
		if modelInfo.Provider == "" {
			modelInfo.Provider = m.provider
		}
		return modelInfo
	}
	return llm.ModelInfo{
		Name:     m.name,
		Provider: m.provider,
	}
}

func (m *SwappableModel) Swap(provider, name string, model agentcore.ChatModel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SwappableModel.Swap(model)
	m.provider = provider
	m.name = name
}

func (m *SwappableModel) Current() (provider, name string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider, m.name
}

// ModelSet lưu giữ các instance mô hình phân bổ theo vai trò; vai trò chưa cấu hình sẽ fallback về mô hình mặc định.
type ModelSet struct {
	Default   *SwappableModel
	models    map[string]*SwappableModel
	fallbacks map[string][]modelTarget
	config    Config
}

// ForRole trả về mô hình cho vai trò chỉ định; trả về mô hình mặc định nếu chưa cấu hình.
func (ms *ModelSet) ForRole(role string) agentcore.ChatModel {
	if m, ok := ms.models[role]; ok {
		return m
	}
	return ms.Default
}

// ForRoleChain trả về mô hình của vai trò đầu tiên được cấu hình tường minh trong roles,
// cuối cùng mới đến mô hình mặc định.
//
// Dùng cho vai trò phụ có thể "mượn" cấu hình của vai trò chính khi người dùng chưa khai báo:
// ví dụ extractor chưa cấu hình thì lùi về architect, giữ nguyên hành vi cũ thay vì
// nhảy thẳng xuống model mặc định.
func (ms *ModelSet) ForRoleChain(roles ...string) agentcore.ChatModel {
	for _, role := range roles {
		if m, ok := ms.models[role]; ok {
			return m
		}
	}
	return ms.Default
}

// ForRoleWithFailover trả về mô hình vai trò có fallback cấp độ từng yêu cầu.
// Chỉ có hiệu lực khi vai trò đó được cấu hình tường minh fallbacks; nếu không sẽ thoái hóa về mô hình thông thường.
func (ms *ModelSet) ForRoleWithFailover(role string, report FailoverReporter) agentcore.ChatModel {
	primary, ok := ms.models[role]
	if !ok {
		return ms.Default
	}
	targets := ms.fallbacks[role]
	if len(targets) == 0 {
		return primary
	}
	return &failoverModel{
		role:      role,
		primary:   primary,
		fallbacks: append([]modelTarget(nil), targets...),
		report:    report,
	}
}

// Summary trả về tóm tắt phân bổ mô hình (dùng cho log).
func (ms *ModelSet) Summary() string {
	var parts []string
	for role, m := range ms.models {
		provider, name := m.Current()
		parts = append(parts, fmt.Sprintf("%s=%s/%s", role, provider, name))
	}
	if len(parts) == 0 {
		provider, name := ms.Default.Current()
		return fmt.Sprintf("default=%s/%s", provider, name)
	}
	provider, name := ms.Default.Current()
	return fmt.Sprintf("default=%s/%s %s", provider, name, strings.Join(parts, " "))
}

// CurrentSelection trả về provider/model đang có hiệu lực của vai trò.
// Khi role rỗng hoặc là "default" thì trả về mô hình mặc định.
func (ms *ModelSet) CurrentSelection(role string) (provider, model string, explicit bool) {
	if role == "" || role == "default" {
		provider, model = ms.Default.Current()
		return provider, model, true
	}
	if sw, ok := ms.models[role]; ok {
		provider, model = sw.Current()
		return provider, model, true
	}
	provider, model = ms.Default.Current()
	return provider, model, false
}

// Swap chuyển đổi mô hình mặc định hoặc mô hình của vai trò chỉ định.
// Khi role rỗng hoặc là "default" thì chuyển mô hình mặc định; các vai trò khác được ghi đè tường minh.
func (ms *ModelSet) Swap(role, provider, model string) error {
	pc, ok := ms.config.Providers[provider]
	if !ok {
		return fmt.Errorf("provider %q is not configured: %w", provider, errs.ErrConfig)
	}
	next, err := createModelFromConfigForRole(provider, model, pc, make(map[string]agentcore.ChatModel), role)
	if err != nil {
		return fmt.Errorf("chuyển đổi mô hình thất bại: %w", err)
	}

	if role == "" || role == "default" {
		ms.Default.Swap(provider, model, next)
		return nil
	}

	if !knownRoles[role] {
		return fmt.Errorf("unknown role %q: %w", role, errs.ErrConfig)
	}

	if existing, ok := ms.models[role]; ok {
		existing.Swap(provider, model, next)
		return nil
	}
	ms.models[role] = NewSwappableModel(provider, model, next)
	return nil
}

// ModelName trích xuất tên mô hình hiện tại từ ChatModel; trả về chuỗi rỗng nếu thất bại.
// Hỗ trợ hoán đổi nóng của SwappableModel: luôn trả về giá trị mới nhất tại thời điểm gọi.
func ModelName(m agentcore.ChatModel) string {
	if info, ok := m.(interface{ Info() llm.ModelInfo }); ok {
		return info.Info().Name
	}
	return ""
}

// NewModelSet tạo tập hợp đa mô hình từ cấu hình.
// Các tổ hợp provider+model giống nhau sẽ tái sử dụng cùng một instance.
func NewModelSet(cfg Config) (*ModelSet, error) {
	cache := make(map[string]agentcore.ChatModel)

	// Tạo mô hình mặc định
	defaultPC := cfg.DefaultProviderConfig()
	defaultModel, err := createModelFromConfig(cfg.Provider, cfg.ModelName, defaultPC, cache)
	if err != nil {
		return nil, fmt.Errorf("default model: %w", err)
	}

	ms := &ModelSet{
		Default:   NewSwappableModel(cfg.Provider, cfg.ModelName, defaultModel),
		models:    make(map[string]*SwappableModel),
		fallbacks: make(map[string][]modelTarget),
		config:    cfg,
	}

	// Tạo mô hình ghi đè theo vai trò
	for role, rc := range cfg.Roles {
		pc, ok := cfg.Providers[rc.Provider]
		if !ok {
			return nil, fmt.Errorf("role %s references unknown provider %q: %w", role, rc.Provider, errs.ErrConfig)
		}
		m, err := createModelFromConfigForRole(rc.Provider, rc.Model, pc, cache, role)
		if err != nil {
			return nil, fmt.Errorf("role %s model: %w", role, err)
		}
		ms.models[role] = NewSwappableModel(rc.Provider, rc.Model, m)
		slog.Info("Phân bổ mô hình theo vai trò", "module", "config", "role", role, "provider", rc.Provider, "model", rc.Model)
		if len(rc.Fallbacks) == 0 {
			continue
		}

		targets := make([]modelTarget, 0, len(rc.Fallbacks))
		for _, fallback := range rc.Fallbacks {
			fpc, ok := cfg.Providers[fallback.Provider]
			if !ok {
				return nil, fmt.Errorf("role %s fallback references unknown provider %q: %w", role, fallback.Provider, errs.ErrConfig)
			}
			fm, err := createModelFromConfigForRole(fallback.Provider, fallback.Model, fpc, cache, role)
			if err != nil {
				return nil, fmt.Errorf("role %s fallback %s/%s: %w", role, fallback.Provider, fallback.Model, err)
			}
			targets = append(targets, modelTarget{
				provider: fallback.Provider,
				name:     fallback.Model,
				model:    fm,
			})
		}
		ms.fallbacks[role] = targets
	}

	return ms, nil
}

// createModelFromConfig tạo hoặc tái sử dụng instance ChatModel.
func createModelFromConfig(providerKey, model string, pc ProviderConfig, cache map[string]agentcore.ChatModel) (agentcore.ChatModel, error) {
	return createModelFromConfigWithRuntime(providerKey, model, pc, cache, runtimeConfigForRole(""))
}

func createModelFromConfigForRole(providerKey, model string, pc ProviderConfig, cache map[string]agentcore.ChatModel, role string) (agentcore.ChatModel, error) {
	return createModelFromConfigWithRuntime(providerKey, model, pc, cache, runtimeConfigForRole(role))
}

func createModelFromConfigWithRuntime(providerKey, model string, pc ProviderConfig, cache map[string]agentcore.ChatModel, runtime modelRuntimeConfig) (agentcore.ChatModel, error) {
	cacheKey := fmt.Sprintf("%s|%s|request=%s|idle=%s", providerKey, model, runtime.requestTimeout, runtime.streamIdleTimeout)
	if m, ok := cache[cacheKey]; ok {
		return m, nil
	}

	providerType, err := pc.ProviderType(providerKey)
	if err != nil {
		return nil, fmt.Errorf("phân tích kiểu nhà cung cấp thất bại: %w", err)
	}

	modelOpts := []llm.ModelOption{
		llm.WithAPIKey(pc.APIKey),
		llm.WithBaseURL(pc.BaseURL),
		llm.WithStreamIdleTimeout(runtime.streamIdleTimeout),
		llm.WithProviderExtra(pc.Extra),
		llm.WithExtra(pc.ExtraBody),
	}
	if runtime.requestTimeout > 0 {
		modelOpts = append(modelOpts, llm.WithRequestTimeout(runtime.requestTimeout))
	}
	m, err := llm.NewModel(providerType, model, modelOpts...)
	if err != nil {
		return nil, fmt.Errorf("provider %s (%s): %w: %w", providerKey, providerType, errs.ErrProvider, err)
	}
	cache[cacheKey] = m
	return m, nil
}

type failoverModel struct {
	role      string
	primary   *SwappableModel
	fallbacks []modelTarget
	report    FailoverReporter
}

func (m *failoverModel) Generate(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	current := m.currentTarget()
	resp, err := current.model.Generate(ctx, messages, tools, opts...)
	if err == nil {
		return resp, nil
	}

	next, reason, ok := m.pickFallback(current, err)
	if !ok {
		return nil, err
	}
	m.reportFailover(current, next, reason, err)
	return next.model.Generate(ctx, messages, tools, opts...)
}

func (m *failoverModel) GenerateStream(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	out := make(chan agentcore.StreamEvent, 100)

	go func() {
		defer close(out)

		current := m.currentTarget()
		fallbackUsed := false

	retry:
		source, resp, err := m.startAttempt(ctx, current, messages, tools, opts...)
		if err != nil {
			if !fallbackUsed {
				if next, reason, ok := m.pickFallback(current, err); ok {
					fallbackUsed = true
					m.reportFailover(current, next, reason, err)
					current = next
					goto retry
				}
			}
			out <- agentcore.StreamEvent{Type: agentcore.StreamEventError, Err: err}
			return
		}
		if resp != nil {
			out <- agentcore.StreamEvent{
				Type:       agentcore.StreamEventDone,
				Message:    resp.Message,
				StopReason: resp.Message.StopReason,
			}
			return
		}

		forwarded := false
		for ev := range source {
			switch ev.Type {
			case agentcore.StreamEventError:
				if ev.Err != nil && !forwarded && !fallbackUsed {
					if next, reason, ok := m.pickFallback(current, ev.Err); ok {
						fallbackUsed = true
						m.reportFailover(current, next, reason, ev.Err)
						current = next
						goto retry
					}
				}
				out <- ev
				return
			case agentcore.StreamEventDone:
				out <- ev
				return
			default:
				forwarded = true
				out <- ev
			}
		}
	}()

	return out, nil
}

func (m *failoverModel) SupportsTools() bool {
	return m.primary != nil && m.primary.SupportsTools()
}

func (m *failoverModel) ProviderName() string {
	if m.primary == nil {
		return ""
	}
	return m.primary.ProviderName()
}

func (m *failoverModel) Info() llm.ModelInfo {
	if m.primary == nil {
		return llm.ModelInfo{}
	}
	return m.primary.Info()
}

func (m *failoverModel) currentTarget() modelTarget {
	if m.primary == nil {
		return modelTarget{}
	}
	provider, name := m.primary.Current()
	return modelTarget{
		provider: provider,
		name:     name,
		model:    m.primary,
	}
}

func (m *failoverModel) pickFallback(current modelTarget, err error) (modelTarget, string, bool) {
	if err == nil || current.model == nil {
		return modelTarget{}, "", false
	}
	if errors.Is(err, context.Canceled) {
		return modelTarget{}, "", false
	}

	if !agentcore.IsFailoverEligible(err) {
		return modelTarget{}, agentcore.FailoverReason(err), false
	}
	reason := agentcore.FailoverReason(err)
	for _, target := range m.fallbacks {
		if target.provider == current.provider && target.name == current.name {
			continue
		}
		if target.model == nil {
			continue
		}
		return target, reason, true
	}
	return modelTarget{}, reason, false
}

func (m *failoverModel) reportFailover(from, to modelTarget, reason string, err error) {
	if m.report != nil {
		m.report(FailoverEvent{
			Role:         m.role,
			Reason:       reason,
			FromProvider: from.provider,
			FromModel:    from.name,
			ToProvider:   to.provider,
			ToModel:      to.name,
			Err:          err,
		})
	}
}

func (m *failoverModel) startAttempt(ctx context.Context, target modelTarget, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, *agentcore.LLMResponse, error) {
	if target.model == nil {
		return nil, nil, fmt.Errorf("no model configured")
	}

	streamCh, err := target.model.GenerateStream(ctx, messages, tools, opts...)
	if err == nil {
		return streamCh, nil, nil
	}

	resp, genErr := target.model.Generate(ctx, messages, tools, opts...)
	if genErr != nil {
		return nil, nil, genErr
	}
	return nil, resp, nil
}
