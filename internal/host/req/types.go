package req

import (
	"context"
	"time"

	"github.com/voocel/agentcore"
)

// LLMChat là giao diện phụ thuộc tối thiểu của gói req với ChatModel:
// chỉ cần một lần sinh văn bản thông thường (giống sim/imp).
type LLMChat interface {
	Generate(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error)
}

// LLMStream là khả năng streaming tùy chọn của model. Extractor ưu tiên đường
// này vì phản hồi JSON có thể rất dài; request đồng bộ dễ bị reverse proxy
// (đặc biệt Cloudflare) ngắt 520/524 trước khi model trả byte đầu tiên.
//
// Giữ nó thành interface riêng để các fake/model tối giản chỉ có Generate vẫn
// hoạt động, đồng thời model thật (agentcore.ChatModel) tự động dùng streaming.
type LLMStream interface {
	GenerateStream(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error)
}

// Deps là phụ thuộc của Requirement Extractor: một model và system prompt.
// Không cần Store — extractor chỉ sinh brief + coverage rồi trả về cho TUI review,
// không ghi đĩa (thiết kế "brief + chỉ thị mạnh", không chạm store schema).
type Deps struct {
	LLM    LLMChat
	Prompt string

	// Thinking là cường độ suy nghĩ của role extractor, ĐÃ được tầng gọi xác thực
	// (agents.ParseThinkingLevel); trống = không ghi đè.
	//
	// req gọi LLM trực tiếp chứ không qua agents.ApplyThinking — vốn chỉ định tuyến cho
	// coordinator/architect/writer/editor — nên phải tự truyền xuống Generate;
	// nếu không, cấu hình thinking của role extractor sẽ bị bỏ qua âm thầm.
	Thinking agentcore.ThinkingLevel
}

// Options mô tả đầu vào cho một lần trích xuất.
type Options struct {
	Path string // đường dẫn file yêu cầu (.md hoặc văn bản)

	// RawDumpPath là nơi ghi phản hồi thô khi parse hỏng; rỗng thì dùng RawDumpPath() mặc định.
	// Test PHẢI đặt trường này (t.TempDir()) để không ghi đè file chẩn đoán thật của người dùng.
	RawDumpPath string
}

type Stage string

const (
	StageRead    Stage = "read"
	StageExtract Stage = "extract"
	StageDone    Stage = "done"
	StageError   Stage = "error"
)

// Coverage là báo cáo độ bao phủ do extractor tự kiểm tra:
// Mapped = các mục trong tài liệu đã được đưa vào brief; Missing = mục chưa chắc/không rõ vị trí.
type Coverage struct {
	Mapped  []string `json:"mapped"`
	Missing []string `json:"missing"`
	Notes   string   `json:"notes"`
}

// Result là sản phẩm cuối của extractor.
type Result struct {
	Title    string   `json:"title"`
	Brief    string   `json:"brief"`
	Coverage Coverage `json:"coverage"`
}

// Event là tín hiệu tiến trình gửi về TUI. Result chỉ khác nil ở StageDone.
type Event struct {
	Time    time.Time
	Stage   Stage
	Message string
	Err     error
	Result  *Result
}
