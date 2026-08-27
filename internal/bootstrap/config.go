package bootstrap

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/models"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// DefaultContextWindow là kích thước cửa sổ ngữ cảnh mặc định khi model chưa được đăng ký trong registry.
const DefaultContextWindow = 200000

// CompactRatio là ngưỡng tương đối để kích hoạt nén ngữ cảnh: khi tokens >= window * CompactRatio thì nén.
// 0.85 là giá trị kinh nghiệm, để lại 15% không gian đầu cho "prompt vòng tiếp theo + kết quả công cụ lớn",
// đồng thời cho phép model cửa sổ lớn chủ động nén ở 85%, tránh chờ đầy hết mới nén trong cửa sổ 1M danh nghĩa
// (vùng suy giảm chú ý).
//
// Không để lộ cho người dùng cấu hình: cùng nguồn gốc với context_window đã xóa — trong kiến trúc đa model,
// để người dùng tự chỉnh số liệu qua lại không bằng cố định một giá trị hợp lý trong code.
const CompactRatio = 0.85

// MinCompactReserve là giới hạn dưới của ReserveTokens. Model cửa sổ nhỏ (ví dụ qwen3:8b 32k cục bộ)
// tính reserve theo tỉ lệ 0.15 chỉ được 4800, trong khi một lần phản hồi công cụ commit_chapter có thể
// chiếm 5-8k, nội dung một chương 8-15k — dẫn đến "vừa nén xong lại vượt ngay". Giới hạn dưới 8000
// đảm bảo trong tình huống xấu nhất vẫn còn nửa vòng đệm.
const MinCompactReserve = 8000

// CompactReserveTokens tính ngược ReserveTokens từ CompactRatio và áp dụng sàn MinCompactReserve:
//
//	threshold = window - reserve = window * CompactRatio
//	reserve   = max(MinCompactReserve, window * (1 - CompactRatio))
//
// Dùng cho EngineConfig.ReserveTokens của agentcore.context.Engine.
func CompactReserveTokens(window int) int {
	if window <= 0 {
		return 0
	}
	reserve := window - int(float64(window)*CompactRatio)
	if reserve < MinCompactReserve {
		return MinCompactReserve
	}
	return reserve
}

// ProviderConfig định nghĩa thông tin xác thực cho một nhà cung cấp LLM.
type ProviderConfig struct {
	Type    string   `json:"type,omitempty"`     // Loại giao thức API (openai/anthropic/gemini), chỉ định khi dùng proxy tùy chỉnh
	APIKey  string   `json:"api_key,omitempty"`  // API Key
	BaseURL string   `json:"base_url,omitempty"` // API Base URL
	Models  []string `json:"models,omitempty"`   // Danh sách model tùy chọn, hiển thị khi chuyển đổi trong TUI
	// ExtraBody truyền thẳng các tham số bổ sung vào mỗi yêu cầu của nhà cung cấp này (ví dụ temperature/top_p/min_p/
	// presence_penalty, hoặc các khóa đặc thù của nhà sản xuất như chat_template_kwargs để bật think của nvidia).
	// Endpoint tương thích OpenAI sẽ gộp trực tiếp vào body yêu cầu (theo quy ước extra_body); người dùng tự chịu trách nhiệm về giá trị.
	ExtraBody map[string]any `json:"extra_body,omitempty"`
	// Extra truyền thẳng vào cấu hình cấp nhà cung cấp (litellm.ProviderConfig.Extra), dùng cho HTTP
	// headers, user_agent, anthropic_beta và các tùy chọn client/transport layer.
	Extra map[string]any `json:"extra,omitempty"`
}

// RequiresAPIKey trả về liệu nhà cung cấp này có bắt buộc phải cấu hình api_key hay không.
// Quy ước:
// 1. ollama / bedrock cho phép không có key;
// 2. Cấu hình đã chỉ định Type được coi là proxy tùy chỉnh, cho phép không có key;
// 3. Các nhà cung cấp khác mặc định yêu cầu key, giữ kiểm tra bảo thủ cho giao diện chính thức.
func (pc ProviderConfig) RequiresAPIKey(name string) bool {
	switch name {
	case "ollama", "bedrock":
		return false
	}
	return pc.Type == ""
}

// ProviderType trả về loại giao thức API có hiệu lực.
// Ưu tiên dùng Type tường minh; nếu không thì yêu cầu tên nhà cung cấp đã được đăng ký trong registry của litellm.
func (pc ProviderConfig) ProviderType(name string) (string, error) {
	if pc.Type != "" {
		return pc.Type, nil
	}
	if llm.IsProviderRegistered(name) {
		return name, nil
	}
	return "", fmt.Errorf("provider %q thiếu type và không có trong danh sách nhà cung cấp đã biết của litellm: %w", name, errs.ErrConfig)
}

// ModelRef đại diện cho một tổ hợp provider/model.
type ModelRef struct {
	Provider string `json:"provider"` // Tên nhà cung cấp (key trong map Providers)
	Model    string `json:"model"`    // Tên model (truyền nguyên vẹn, không phân tích)
}

// RoleConfig định nghĩa ghi đè model cho một vai trò cụ thể.
type RoleConfig struct {
	Provider  string     `json:"provider"`            // Tên nhà cung cấp chính (key trong map Providers)
	Model     string     `json:"model"`               // Tên model chính (truyền nguyên vẹn, không phân tích)
	Fallbacks []ModelRef `json:"fallbacks,omitempty"` // Danh sách provider/model dự phòng tường minh
	// Thinking cường độ suy nghĩ của vai trò này (off/minimal/low/medium/high/xhigh), trống = kế thừa mặc định cấp trên.
	// Được kiểm tra bởi agents.ParseThinkingLevel trước khi áp dụng, giá trị vượt cấp coi như trống.
	Thinking string `json:"thinking,omitempty"`
}

// knownRoles là danh sách tên vai trò được hỗ trợ.
var knownRoles = map[string]bool{
	"coordinator": true,
	"architect":   true,
	"writer":      true,
	"editor":      true,
	"extractor":   true,
}

// roleNames liệt kê vai trò hợp lệ cho thông báo lỗi, sắp xếp ổn định.
func roleNames() string {
	names := make([]string, 0, len(knownRoles))
	for name := range knownRoles {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "/")
}

// Config cấu hình ứng dụng tiểu thuyết.
type Config struct {
	// Trường runtime (không serialize ra JSON)
	OutputDir string `json:"-"` // Thư mục gốc đầu ra

	// Cấu hình LLM mặc định
	Provider  string `json:"provider"` // Nhà cung cấp mặc định (key trong map Providers)
	ModelName string `json:"model"`    // Tên model mặc định
	// Thinking cường độ suy nghĩ mặc định cấp trên (off/minimal/low/medium/high/xhigh), trống = không ghi đè (dùng mặc định của model/provider).
	// Khi vai trò không cấu hình thinking riêng thì dùng giá trị này.
	Thinking string `json:"thinking,omitempty"`

	// Kho thông tin xác thực nhà cung cấp
	Providers map[string]ProviderConfig `json:"providers,omitempty"`

	// Ghi đè model theo vai trò
	Roles map[string]RoleConfig `json:"roles,omitempty"`

	// Tham số sáng tác
	Style string `json:"style,omitempty"`

	// ContextWindow kích thước cửa sổ dùng cho nén ngữ cảnh. Để trống (0) thì tự động giải quyết theo tên model:
	// nếu registry có thì dùng cửa sổ thực của model, không có thì dùng DefaultContextWindow.
	// Cấu hình tường minh sẽ được ưu tiên — dùng để chỉ định cửa sổ thực cho model tùy chỉnh không có trong registry,
	// hoặc ghim model cửa sổ lớn xuống giá trị nhỏ hơn để kích hoạt nén sớm hơn (cửa sổ danh nghĩa 1M thường đã suy giảm chú ý từ 200k+).
	// Chỉ ảnh hưởng ngưỡng nén, không thay đổi độ dài yêu cầu thực tế gửi đến LLM API; người dùng tự chịu trách nhiệm về giá trị.
	ContextWindow int `json:"context_window,omitempty"`

	// Budget chính sách ngân sách chi phí cho một cuốn sách; chỉ kích hoạt khi book_usd > 0.
	Budget BudgetConfig `json:"budget,omitzero"`

	// Notify cấu hình cảnh báo không giám sát; mặc định bật (kênh system làm dự phòng).
	Notify NotifyConfig `json:"notify,omitzero"`
}

// BudgetConfig là tuyên bố chính sách ngân sách của người dùng cho một cuốn sách. Dừng khi vượt giới hạn
// tương đương người dùng thủ công Abort tại thời điểm đó — Host chỉ thực thi thay, không đánh giá hành vi model
// (ranh giới hợp hiến §10 kiến trúc).
type BudgetConfig struct {
	BookUSD   float64 `json:"book_usd,omitempty"`   // Bắt buộc để kích hoạt; 0/mặc định = không giới hạn
	WarnRatio float64 `json:"warn_ratio,omitempty"` // Mức cảnh báo, mặc định 0.8
	HardStop  bool    `json:"hard_stop,omitempty"`  // true = dừng ngay khi vượt; mặc định chờ agent phụ hiện tại hoàn thành
}

// Enabled trả về liệu chính sách ngân sách có được bật hay không.
func (b BudgetConfig) Enabled() bool { return b.BookUSD > 0 }

// NotifyConfig cấu hình kênh cảnh báo không giám sát.
type NotifyConfig struct {
	Enabled *bool    `json:"enabled,omitempty"` // Mặc định true (kênh system dùng được không cần cấu hình)
	Command string   `json:"command,omitempty"` // Tùy chọn, khi cấu hình sẽ thay thế kênh system (push điện thoại dùng đây)
	Events  []string `json:"events,omitempty"`  // Tùy chọn, lọc theo kind (run_end/repeat/budget), mặc định bật tất cả
}

// IsEnabled trả về liệu cảnh báo có được bật hay không (mặc định true).
func (n NotifyConfig) IsEnabled() bool { return n.Enabled == nil || *n.Enabled }

// ValidateBase kiểm tra cấu hình cơ bản.
func (c *Config) ValidateBase() error {
	if err := validateConfigText("provider", c.Provider); err != nil {
		return err
	}
	if err := validateConfigText("model", c.ModelName); err != nil {
		return err
	}

	if c.Provider == "" {
		return fmt.Errorf("provider is required: %w", errs.ErrConfig)
	}
	if c.ModelName == "" {
		return fmt.Errorf("model is required: %w", errs.ErrConfig)
	}

	// Nhà cung cấp mặc định phải có thông tin xác thực
	pc, ok := c.Providers[c.Provider]
	if !ok {
		return fmt.Errorf("provider %q chưa được cấu hình thông tin xác thực trong providers; nếu bạn ghi đè provider trong ./.ainovel/config.json, cần khai báo đồng thời providers.%s (bao gồm api_key/base_url), không thể chỉ thay provider cấp trên: %w", c.Provider, c.Provider, errs.ErrConfig)
	}
	if pc.RequiresAPIKey(c.Provider) && pc.APIKey == "" {
		return fmt.Errorf("provider %q has no api_key configured: %w", c.Provider, errs.ErrConfig)
	}
	if err := validateProviderConfigText(c.Provider, pc); err != nil {
		return err
	}
	for name, provider := range c.Providers {
		if err := validateConfigText("provider name", name); err != nil {
			return err
		}
		if err := validateProviderConfigText(name, provider); err != nil {
			return err
		}
	}

	// Kiểm tra ghi đè vai trò
	for role, rc := range c.Roles {
		if err := validateConfigText("role name", role); err != nil {
			return err
		}
		if err := validateConfigText(fmt.Sprintf("role %q provider", role), rc.Provider); err != nil {
			return err
		}
		if err := validateConfigText(fmt.Sprintf("role %q model", role), rc.Model); err != nil {
			return err
		}
		if !knownRoles[role] {
			return fmt.Errorf("unknown role %q in roles config (valid: %s): %w", role, roleNames(), errs.ErrConfig)
		}
		if rc.Provider == "" || rc.Model == "" {
			return fmt.Errorf("role %q must have both provider and model: %w", role, errs.ErrConfig)
		}
		if err := c.validateModelRef(
			fmt.Sprintf("role %q", role),
			ModelRef{Provider: rc.Provider, Model: rc.Model},
		); err != nil {
			return err
		}
		for i, fallback := range rc.Fallbacks {
			if err := validateConfigText(fmt.Sprintf("role %q fallback[%d] provider", role, i), fallback.Provider); err != nil {
				return err
			}
			if err := validateConfigText(fmt.Sprintf("role %q fallback[%d] model", role, i), fallback.Model); err != nil {
				return err
			}
			if err := c.validateModelRef(
				fmt.Sprintf("role %q fallback[%d]", role, i),
				fallback,
			); err != nil {
				return err
			}
		}
	}

	// Kiểm tra chính sách ngân sách
	if c.Budget.BookUSD < 0 {
		return fmt.Errorf("budget.book_usd must be >= 0: %w", errs.ErrConfig)
	}
	if c.Budget.Enabled() && (c.Budget.WarnRatio <= 0 || c.Budget.WarnRatio >= 1) {
		return fmt.Errorf("budget.warn_ratio must be in (0, 1): %w", errs.ErrConfig)
	}

	// Kiểm tra cấu hình cảnh báo
	if err := validateConfigText("notify.command", c.Notify.Command); err != nil {
		return err
	}
	for _, ev := range c.Notify.Events {
		if !knownNotifyEvents[ev] {
			return fmt.Errorf("unknown notify event %q (valid: run_end/repeat/budget): %w", ev, errs.ErrConfig)
		}
	}

	return nil
}

var knownNotifyEvents = map[string]bool{"run_end": true, "repeat": true, "budget": true}

func validateProviderConfigText(name string, pc ProviderConfig) error {
	fields := []struct {
		label string
		value string
	}{
		{label: fmt.Sprintf("provider %q type", name), value: pc.Type},
		{label: fmt.Sprintf("provider %q api_key", name), value: pc.APIKey},
		{label: fmt.Sprintf("provider %q base_url", name), value: pc.BaseURL},
	}
	for _, field := range fields {
		if err := validateConfigText(field.label, field.value); err != nil {
			return err
		}
	}
	for i, model := range pc.Models {
		if err := validateConfigText(fmt.Sprintf("provider %q models[%d]", name, i), model); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigText(name, value string) error {
	if utils.ContainsControl(value) {
		return fmt.Errorf("%s contains control character: %w", name, errs.ErrConfig)
	}
	return nil
}

// DefaultProviderConfig trả về cấu hình thông tin xác thực của nhà cung cấp mặc định.
func (c *Config) DefaultProviderConfig() ProviderConfig {
	if c.Providers == nil {
		return ProviderConfig{}
	}
	return c.Providers[c.Provider]
}

// FillDefaults điền các giá trị mặc định.
func (c *Config) FillDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = filepath.Join("output", "novel")
	}
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	if c.Roles == nil {
		c.Roles = make(map[string]RoleConfig)
	}
	if c.Style == "" {
		c.Style = "default"
	}
	if c.Budget.Enabled() && c.Budget.WarnRatio == 0 {
		c.Budget.WarnRatio = 0.8
	}
}

// ContextWindowSource đánh dấu nguồn gốc của giá trị cửa sổ ngữ cảnh, dùng cho log/chẩn đoán.
type ContextWindowSource string

const (
	CtxWindowConfig   ContextWindowSource = "config"   // Chỉ định tường minh qua context_window trong file cấu hình
	CtxWindowRegistry ContextWindowSource = "registry" // Khớp với dữ liệu cơ sở OpenRouter
	CtxWindowDefault  ContextWindowSource = "default"  // Dự phòng (proxy tùy chỉnh / model chưa biết)
)

// ResolveContextWindow giải quyết cửa sổ ngữ cảnh hiệu lực dùng cho nén, theo thứ tự ưu tiên:
//  1. ContextWindow > 0 trong file cấu hình → dùng trực tiếp (ưu tiên cao nhất, có thể vượt cửa sổ thực của model)
//  2. Tra cứu models.DefaultRegistry theo tên model (dữ liệu cơ sở OpenRouter + làm mới mỗi 24h)
//  3. Dự phòng DefaultContextWindow (proxy tùy chỉnh / model chưa biết)
//
// Lưu ý: giá trị trả về chỉ dùng để tính ngưỡng nén, không thu nhỏ độ dài yêu cầu thực tế gửi đến LLM API.
func (c Config) ResolveContextWindow(modelName string) (int, ContextWindowSource) {
	if c.ContextWindow > 0 {
		return c.ContextWindow, CtxWindowConfig
	}
	if rw := models.DefaultRegistry().ResolveContextWindow(modelName); rw > 0 {
		return rw, CtxWindowRegistry
	}
	return DefaultContextWindow, CtxWindowDefault
}

// ResolveThinking trả về chuỗi cường độ suy nghĩ có hiệu lực cho một vai trò (off/minimal/low/medium/high/xhigh hoặc trống).
// Thứ tự ưu tiên: Roles[role].Thinking cấp vai trò → Thinking mặc định cấp trên → "" (không ghi đè, dùng mặc định model/provider).
// Khi role trống hoặc là "default" thì lấy thẳng giá trị mặc định cấp trên. Tính hợp lệ của giá trị do agents.ParseThinkingLevel kiểm tra.
func (c Config) ResolveThinking(role string) string {
	if role != "" && role != "default" {
		if rc, ok := c.Roles[role]; ok && rc.Thinking != "" {
			return rc.Thinking
		}
	}
	return c.Thinking
}

// LogContextWindowChoice ghi log quyết định chọn cửa sổ ngữ cảnh cho một vai trò. Khi source=default thì phát Warn
// để thông báo model này chưa có trong registry (kể cả OpenRouter không có), việc nén ngữ cảnh sẽ dùng cửa sổ
// dự phòng — nếu cửa sổ thực của model lớn hơn, có thể chỉ định tường minh qua context_window trong file cấu hình
// để tránh bị nén quá sớm và mất lịch sử.
func LogContextWindowChoice(role, model string, window int, source ContextWindowSource) {
	attrs := []any{"module", "context", "role", role, "model", model, "window", window, "source", source}
	switch source {
	case CtxWindowDefault:
		slog.Warn("Model chưa được nhận dạng, dùng cửa sổ dự phòng (proxy tùy chỉnh hoặc OpenRouter chưa có, có thể chỉ định tường minh qua context_window)", attrs...)
	case CtxWindowConfig:
		slog.Info("Cửa sổ ngữ cảnh (từ context_window trong file cấu hình)", attrs...)
	default:
		slog.Info("Cửa sổ ngữ cảnh", attrs...)
	}
}

// CandidateModels trả về danh sách model có thể chuyển đổi dưới một nhà cung cấp nhất định.
// Ưu tiên dùng models đã khai báo tường minh trong provider; đồng thời bổ sung các model của nhà cung cấp đó đã xuất hiện trong cấu hình hiện tại.
func (c Config) CandidateModels(provider string) []string {
	if provider == "" {
		return nil
	}

	seen := make(map[string]bool)
	models := make([]string, 0, 4)
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			return
		}
		seen[model] = true
		models = append(models, model)
	}

	if pc, ok := c.Providers[provider]; ok {
		for _, model := range pc.Models {
			add(model)
		}
	}
	if c.Provider == provider {
		add(c.ModelName)
	}
	for _, rc := range c.Roles {
		if rc.Provider == provider {
			add(rc.Model)
		}
		for _, fallback := range rc.Fallbacks {
			if fallback.Provider == provider {
				add(fallback.Model)
			}
		}
	}
	return models
}

func (c Config) validateModelRef(owner string, ref ModelRef) error {
	if ref.Provider == "" || ref.Model == "" {
		return fmt.Errorf("%s must have both provider and model: %w", owner, errs.ErrConfig)
	}

	pc, ok := c.Providers[ref.Provider]
	if !ok {
		return fmt.Errorf("%s references provider %q which is not configured: %w", owner, ref.Provider, errs.ErrConfig)
	}
	if pc.RequiresAPIKey(ref.Provider) && pc.APIKey == "" {
		return fmt.Errorf("%s references provider %q which has no api_key: %w", owner, ref.Provider, errs.ErrConfig)
	}
	return nil
}
