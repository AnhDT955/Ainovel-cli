package startup

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/host"
)

// CoCreateSession lưu trữ trạng thái phi UI cho chế độ đồng sáng tác.
type CoCreateSession struct {
	history        []host.CoCreateMessage
	draftPrompt    string
	ready          bool
	streamReply    string
	streamThinking string
	suggestions    []string
}

func NewCoCreateSession(initial string) *CoCreateSession {
	return &CoCreateSession{
		history: []host.CoCreateMessage{
			{Role: "user", Content: strings.TrimSpace(initial)},
		},
	}
}

func (s *CoCreateSession) History() []host.CoCreateMessage {
	if s == nil {
		return nil
	}
	return append([]host.CoCreateMessage(nil), s.history...)
}

func (s *CoCreateSession) ApplyReply(reply host.CoCreateReply) {
	if s == nil {
		return
	}
	s.streamReply = ""
	s.streamThinking = ""
	// history lưu toàn bộ Raw ba đoạn phía assistant (bao gồm [DRAFT]) để model vòng sau
	// thấy được bản nháp mình đã viết vòng trước và tiếp tục cập nhật trên đó; nếu chỉ lưu
	// Message thì [DRAFT] sẽ hoàn toàn không vào cửa sổ ngữ cảnh, mỗi vòng model chỉ có thể
	// tóm lại từ hội thoại và dễ mất chi tiết ban đầu. Ở đường dự phòng Raw == Message, tương đương.
	text := strings.TrimSpace(reply.Raw)
	if text == "" {
		text = strings.TrimSpace(reply.Message)
	}
	if text != "" {
		s.history = append(s.history, host.CoCreateMessage{Role: "assistant", Content: text})
	}
	// Chỉ ghi đè draft khi Prompt không rỗng: đường dự phòng parse sẽ trả về Prompt="",
	// lúc đó phải giữ nguyên draft vòng trước, nếu không "chỉ thị sáng tác hiện tại" mà
	// người dùng đã tích lũy sẽ bị xóa bởi phản hồi bị cắt đứt.
	if prompt := strings.TrimSpace(reply.Prompt); prompt != "" {
		s.draftPrompt = prompt
	}
	s.ready = reply.Ready
	// suggestions ghi đè trực tiếp (kể cả ghi đè thành rỗng): gợi ý mỗi vòng chỉ có nghĩa cho thời điểm hiện tại.
	s.suggestions = append(s.suggestions[:0], reply.Suggestions...)
}

func (s *CoCreateSession) AppendUser(text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// Người dùng đã quyết định câu tiếp theo muốn nói, suggestions lập tức vô hiệu,
	// tránh gợi ý cũ vẫn còn treo trên ô nhập khi AI chưa kịp phản hồi gây nhầm lẫn.
	s.suggestions = nil
	s.history = append(s.history, host.CoCreateMessage{Role: "user", Content: text})
}

// ApplyDelta nhận tích lũy luồng streaming; kind="thinking" ghi vào luồng suy luận, "reply" ghi vào xem trước phản hồi.
// Hai luồng tích lũy riêng biệt, TUI có thể tô màu theo từng khối, cho người dùng thấy LLM đang hoạt động ngay cả trong giai đoạn thinking.
func (s *CoCreateSession) ApplyDelta(kind, text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	switch kind {
	case host.CoCreateProgressThinking:
		s.streamThinking = text
	case host.CoCreateProgressReply:
		s.streamReply = text
	}
}

func (s *CoCreateSession) StreamReply() string {
	if s == nil {
		return ""
	}
	return s.streamReply
}

func (s *CoCreateSession) StreamThinking() string {
	if s == nil {
		return ""
	}
	return s.streamThinking
}

func (s *CoCreateSession) DraftPrompt() string {
	if s == nil {
		return ""
	}
	return s.draftPrompt
}

func (s *CoCreateSession) Suggestions() []string {
	if s == nil {
		return nil
	}
	return s.suggestions
}

func (s *CoCreateSession) Ready() bool {
	if s == nil {
		return false
	}
	return s.ready
}

func (s *CoCreateSession) CanStart() bool {
	return strings.TrimSpace(s.DraftPrompt()) != ""
}

func (s *CoCreateSession) InitialInput() string {
	if s == nil || len(s.history) == 0 {
		return ""
	}
	return strings.TrimSpace(s.history[0].Content)
}

func (s *CoCreateSession) BuildPlan() (Plan, error) {
	if s == nil || !s.CanStart() {
		return Plan{}, fmt.Errorf("cocreate draft prompt is required")
	}
	return Plan{
		Mode:        ModeCoCreate,
		DisplayName: "Kế hoạch đồng sáng tác",
		StartPrompt: host.BuildStartPrompt(s.DraftPrompt()),
	}, nil
}
