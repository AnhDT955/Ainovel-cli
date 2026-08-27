package req

import (
	"context"
	"testing"

	"github.com/voocel/agentcore"
)

// capturingLLM ghi lại CallOption thực sự tới được Generate.
type capturingLLM struct {
	response string
	gotLevel agentcore.ThinkingLevel
	calls    int
}

func (c *capturingLLM) Generate(_ context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	c.calls++
	cfg := &agentcore.CallConfig{}
	for _, o := range opts {
		o(cfg)
	}
	c.gotLevel = cfg.ThinkingLevel
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(c.response)},
	}}, nil
}

const okResp = `{"title":"X","brief":"## Nội dung","coverage":{"mapped":[],"missing":[],"notes":""}}`

// Thinking cấu hình cho role extractor phải thực sự xuống tới lệnh gọi LLM,
// nếu không nó bị bỏ qua âm thầm (req không đi qua agents.ApplyThinking).
func TestExtract_ThinkingXuongToiLLM(t *testing.T) {
	llm := &capturingLLM{response: okResp}
	if _, err := Extract(context.Background(), llm, "system", "doc", thinkingOpts(agentcore.ThinkingHigh)...); err != nil {
		t.Fatal(err)
	}
	if llm.gotLevel != agentcore.ThinkingHigh {
		t.Fatalf("thinking không tới được LLM: %q", llm.gotLevel)
	}
}

// Thinking trống = không ghi đè, để mặc định của model/provider quyết định.
func TestExtract_ThinkingTrongThiKhongGhiDe(t *testing.T) {
	llm := &capturingLLM{response: okResp}
	if _, err := Extract(context.Background(), llm, "system", "doc", thinkingOpts("")...); err != nil {
		t.Fatal(err)
	}
	if llm.gotLevel != "" {
		t.Fatalf("kỳ vọng không ghi đè, nhận %q", llm.gotLevel)
	}
}
