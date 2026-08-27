package store

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/voocel/agentcore"
)

// SessionStore ghi nối tiếp lịch sử hội thoại LLM vào file JSONL.
// Nội dung kích thước lớn (nội dung tiểu thuyết, ngữ cảnh đầy đủ) được thay bằng nhãn placeholder [session_compact: ...].
type SessionStore struct {
	io      *IO
	mu      sync.Mutex
	seq     map[string]int    // số thứ tự chạy của agent (dùng khi không trích xuất được số chương)
	taskKey map[string]string // "agentName|task" → suffix, cùng một run dùng chung một file
}

func NewSessionStore(io *IO) *SessionStore {
	return &SessionStore{io: io, seq: make(map[string]int), taskKey: make(map[string]string)}
}

// ModelLookup tra cứu provider/model "đang có hiệu lực tại thời điểm đó" theo tên agent khi logger ghi.
// Dùng kiểu func thay vì interface để tiện inject quy tắc chuẩn hoá bằng closure (vd: architect_short → architect).
// Trả về chuỗi rỗng nghĩa là không xác định; caller vẫn ghi bình thường nhưng không kèm _meta, khi replay sẽ fallback về ModelSet.
type ModelLookup func(agentName string) (provider, model string)

// CoordinatorLogger trả về callback OnMessage cho coordinator.
// lookup có thể nil, khi đó ghi không kèm _meta (tương thích cocreate và các luồng không có vai trò).
func (s *SessionStore) CoordinatorLogger(lookup ModelLookup) func(agentcore.AgentMessage) {
	return func(msg agentcore.AgentMessage) {
		var meta *sessionLogMeta
		if lookup != nil {
			meta = lookupMeta(lookup, "coordinator")
		}
		if err := s.logEntry("meta/sessions/coordinator.jsonl", msg, meta); err != nil {
			slog.Warn("session log failed", "agent", "coordinator", "err", err)
		}
	}
}

// SubAgentLogger trả về callback OnMessage cho agent phụ.
func (s *SessionStore) SubAgentLogger(lookup ModelLookup) func(agentName, task string, msg agentcore.AgentMessage) {
	return func(agentName, task string, msg agentcore.AgentMessage) {
		rel := s.subAgentPath(agentName, task)
		var meta *sessionLogMeta
		if lookup != nil {
			meta = lookupMeta(lookup, agentName)
		}
		if err := s.logEntry(rel, msg, meta); err != nil {
			slog.Warn("session log failed", "agent", agentName, "err", err)
		}
	}
}

func lookupMeta(lookup ModelLookup, agentName string) *sessionLogMeta {
	provider, model := lookup(agentName)
	if provider == "" && model == "" {
		return nil
	}
	return &sessionLogMeta{Provider: provider, Model: model}
}

// LogCoCreate ghi nối tiếp một bản ghi hội thoại đồng sáng tác vào meta/sessions/cocreate.jsonl.
// Giai đoạn đồng sáng tác chưa gắn với tiểu thuyết cụ thể, thống nhất ghi vào thư mục gốc OutputDir mặc định (output/novel),
// cùng vị trí với coordinator.jsonl / agents/* của quá trình sáng tác chính thức, tiện tra cứu.
func (s *SessionStore) LogCoCreate(entry any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal cocreate session: %w", err)
	}
	data = append(data, '\n')
	return s.io.AppendLine("meta/sessions/cocreate.jsonl", data)
}

// Log ghi nối tiếp một tin nhắn vào đường dẫn chỉ định, tự động nén nội dung lớn.
// Không kèm _meta (điểm vào tương thích ngược; chỉ dùng cho cocreate và các luồng không có vai trò).
func (s *SessionStore) Log(rel string, msg agentcore.AgentMessage) error {
	return s.logEntry(rel, msg, nil)
}

// sessionLogEntry nhúng agentcore.Message + _meta tuỳ chọn.
// agentcore.Message là plain struct (không có MarshalJSON), khi nhúng vào json marshal
// sẽ tự động trải phẳng lên cấp trên; _meta được kiểm soát bằng omitempty —
// chỉ inject khi là assistant + Usage != nil, tin nhắn user/tool không kèm _meta,
// khi parse jsonl cũ _meta=nil là noop.
type sessionLogEntry struct {
	agentcore.Message
	Meta *sessionLogMeta `json:"_meta,omitempty"`
}

type sessionLogMeta struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// logEntry tuần tự hoá tin nhắn và đính kèm _meta nếu cần. meta đã được tính sẵn bởi lookupMeta;
// hàm nội bộ chỉ ghi meta cho tin nhắn "đã phát sinh lượng dùng LLM" (assistant + Usage != nil),
// các tin nhắn khác giữ nguyên dạng tuần tự hoá agentcore.Message thuần.
func (s *SessionStore) logEntry(rel string, msg agentcore.AgentMessage, meta *sessionLogMeta) error {
	m, ok := msg.(agentcore.Message)
	if !ok {
		return nil // bỏ qua tin nhắn không phải LLM (vd: kiểu tuỳ chỉnh)
	}
	compacted := compactMessage(m)
	entry := sessionLogEntry{Message: compacted}
	if compacted.Role == agentcore.RoleAssistant && compacted.Usage != nil {
		entry.Meta = usageMeta(compacted.Usage)
		if entry.Meta == nil {
			entry.Meta = meta
		}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal session message: %w", err)
	}
	data = append(data, '\n')
	return s.io.AppendLine(rel, data)
}

func usageMeta(usage *agentcore.Usage) *sessionLogMeta {
	if usage == nil || (usage.Provider == "" && usage.Model == "") {
		return nil
	}
	return &sessionLogMeta{
		Provider: usage.Provider,
		Model:    usage.Model,
	}
}

// subAgentPath tạo đường dẫn file theo agentName+task.
func (s *SessionStore) subAgentPath(agentName, task string) string {
	suffix := extractChapter(task)
	if suffix != "" {
		return fmt.Sprintf("meta/sessions/agents/%s-%s.jsonl", agentName, suffix)
	}
	key := agentName + "|" + task
	s.mu.Lock()
	if cached, ok := s.taskKey[key]; ok {
		s.mu.Unlock()
		return fmt.Sprintf("meta/sessions/agents/%s-%s.jsonl", agentName, cached)
	}
	s.seq[agentName]++
	suffix = fmt.Sprintf("%03d", s.seq[agentName])
	s.taskKey[key] = suffix
	s.mu.Unlock()
	return fmt.Sprintf("meta/sessions/agents/%s-%s.jsonl", agentName, suffix)
}

var chapterRe = regexp.MustCompile(`(?i)(?:第\s*(\d+)\s*章|[Cc]hương\s+(\d+))`)

func extractChapter(task string) string {
	m := chapterRe.FindStringSubmatch(task)
	if len(m) < 2 {
		return ""
	}
	val := m[1]
	if val == "" && len(m) > 2 {
		val = m[2]
	}
	if val == "" {
		return ""
	}
	n, _ := strconv.Atoi(val)
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("ch%02d", n)
}

// compactMessage nhân bản tin nhắn và thay thế nội dung lớn.
func compactMessage(m agentcore.Message) agentcore.Message {
	if len(m.Content) == 0 {
		return m
	}
	blocks := make([]agentcore.ContentBlock, len(m.Content))
	copy(blocks, m.Content)

	toolName := toolNameFromMeta(m.Metadata)

	for i := range blocks {
		switch blocks[i].Type {
		case agentcore.ContentText:
			blocks[i].Text = compactText(m.Role, toolName, blocks[i].Text)
		case agentcore.ContentToolCall:
			if blocks[i].ToolCall != nil {
				blocks[i].ToolCall = compactToolCall(blocks[i].ToolCall)
			}
		}
	}
	m.Content = blocks
	return m
}

func toolNameFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta["tool_name"].(string); ok {
		return v
	}
	return ""
}

// compactText nén nội dung text của tool result.
func compactText(role agentcore.Role, toolName, text string) string {
	if role != agentcore.RoleTool || len(text) < 4096 {
		return text
	}
	switch toolName {
	case "novel_context":
		summary := extractJSONField(text, "_loading_summary")
		return fmt.Sprintf("[session_compact: novel_context %dB | %s]", len(text), summary)
	case "read_chapter":
		chars := utf8.RuneCountInString(text)
		return fmt.Sprintf("[session_compact: read_chapter %d chữ | xem chapters/]", chars)
	default:
		if len(text) > 8192 {
			chars := utf8.RuneCountInString(text)
			return fmt.Sprintf("[session_compact: %s %d chữ]", toolName, chars)
		}
		return text
	}
}

// compactToolCall nén các trường nội dung lớn trong args của tool call.
func compactToolCall(tc *agentcore.ToolCall) *agentcore.ToolCall {
	switch tc.Name {
	case "draft_chapter":
		return compactArgsContent(tc, "Nội dung chương N", "drafts/")
	case "save_foundation":
		return compactFoundationArgs(tc)
	default:
		return tc
	}
}

func compactArgsContent(tc *agentcore.ToolCall, label, ref string) *agentcore.ToolCall {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		return tc
	}
	contentRaw, ok := args["content"]
	if !ok || len(contentRaw) < 4096 {
		return tc
	}
	var content string
	if err := json.Unmarshal(contentRaw, &content); err != nil {
		// content không phải chuỗi (có thể là JSON object), dùng số byte
		placeholder := fmt.Sprintf("[session_compact: %s %dB | xem %s]", label, len(contentRaw), ref)
		args["content"], _ = json.Marshal(placeholder)
	} else {
		chars := utf8.RuneCountInString(content)
		ch := extractJSONFieldInt(tc.Args, "chapter")
		if ch > 0 {
			label = fmt.Sprintf("Nội dung chương %d", ch)
			ref = fmt.Sprintf("drafts/%02d.draft.md", ch)
		}
		placeholder := fmt.Sprintf("[session_compact: %s %d chữ | xem %s]", label, chars, ref)
		args["content"], _ = json.Marshal(placeholder)
	}
	clone := *tc
	clone.Args, _ = json.Marshal(args)
	return &clone
}

func compactFoundationArgs(tc *agentcore.ToolCall) *agentcore.ToolCall {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		return tc
	}
	contentRaw, ok := args["content"]
	if !ok || len(contentRaw) < 4096 {
		return tc
	}
	typeName := "foundation"
	var t string
	if json.Unmarshal(args["type"], &t) == nil && t != "" {
		typeName = t
	}
	placeholder := fmt.Sprintf("[session_compact: %s %dB | xem store]", typeName, len(contentRaw))
	args["content"], _ = json.Marshal(placeholder)
	clone := *tc
	clone.Args, _ = json.Marshal(args)
	return &clone
}

// extractJSONField trích xuất giá trị chuỗi của trường chỉ định từ chuỗi JSON.
func extractJSONField(jsonStr, field string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var val string
	if err := json.Unmarshal(raw, &val); err != nil {
		return string(raw)
	}
	return val
}

func extractJSONFieldInt(data json.RawMessage, field string) int {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return 0
	}
	raw, ok := m[field]
	if !ok {
		return 0
	}
	var val int
	if err := json.Unmarshal(raw, &val); err != nil {
		return 0
	}
	return val
}

// CompactTag là tiền tố nhãn placeholder, tiện tìm kiếm và khôi phục.
const CompactTag = "[session_compact:"

// IsCompacted kiểm tra xem văn bản đã được nén chưa.
func IsCompacted(text string) bool {
	return strings.HasPrefix(text, CompactTag)
}
