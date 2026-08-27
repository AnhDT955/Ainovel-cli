package diag

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SkelEvent là khung hành vi của một tin nhắn hội thoại sau khi ẩn danh: giữ lại các tín hiệu
// cấu trúc (vai / công cụ / lỗi / vân tay lặp lại), toàn bộ văn bản tự do (nội dung,
// prompt, suy nghĩ) đều bị che. Đây là lớp chiếu nghiêm hơn store.compactMessage —
// lớp sau nén theo dung lượng (>4KB), còn ở đây không xét dung lượng,
// mọi văn bản đều không được xuất ra ngoài.
type SkelEvent struct {
	Agent    string     // phiên nguồn: coordinator / writer-ch07 …
	Role     string     // assistant / tool / user
	Tools    []SkelTool // các lời gọi công cụ trong tin nhắn này
	ErrClass string     // role=tool và is_error: dòng đầu lỗi (chuỗi lỗi framework, không chứa nội dung)
	TextSha  string     // hash ngắn của văn bản đã che; cùng sha = lặp lại cùng một đoạn (tín hiệu vòng lặp)
	Redacted int        // số khối văn bản/suy nghĩ bị che trong tin nhắn này (dùng để tự kiểm tra ẩn danh)
}

// SkelTool là chiếu ẩn danh của một lần gọi công cụ.
type SkelTool struct {
	Name     string            // tên công cụ (tín hiệu cấu trúc, không chứa nội dung)
	Args     map[string]string // key → giá trị scalar gốc / chuỗi ngắn có dấu nháy / "<redacted len sha>"
	Invalid  bool              // ArgsInvalid: tham số từ model không thể phân tích được (tín hiệu #34)
	ParseErr string            // ArgsParseError: lý do phân tích thất bại
}

// redactMessage chiếu một agentcore.Message thành khung hành vi.
func redactMessage(agent string, m agentcore.Message) SkelEvent {
	ev := SkelEvent{Agent: agent, Role: string(m.Role)}
	isErr, _ := m.Metadata["is_error"].(bool)

	var text strings.Builder
	for _, b := range m.Content {
		switch b.Type {
		case agentcore.ContentText:
			// Kết quả lỗi tool giữ lại dòng đầu: đây là chuỗi lỗi do chúng ta tạo ra (như InputValidationError),
			// không chứa nội dung, và là chìa khóa để định vị vòng lặp. Các văn bản còn lại đều vào pool che.
			if m.Role == agentcore.RoleTool && isErr && ev.ErrClass == "" {
				ev.ErrClass = firstLine(b.Text, 160)
				continue
			}
			if strings.TrimSpace(b.Text) != "" {
				text.WriteString(b.Text)
				ev.Redacted++
			}
		case agentcore.ContentThinking:
			if strings.TrimSpace(b.Thinking) != "" {
				text.WriteString(b.Thinking)
				ev.Redacted++
			}
		case agentcore.ContentToolCall:
			if b.ToolCall != nil {
				ev.Tools = append(ev.Tools, redactToolCall(b.ToolCall))
			}
		}
	}
	if t := text.String(); t != "" {
		ev.TextSha = shortHash(t)
	}
	return ev
}

// redactToolCall chiếu một lần gọi công cụ: tên công cụ + tham số (giá trị ẩn danh) + đánh dấu ngoại lệ phân tích.
func redactToolCall(tc *agentcore.ToolCall) SkelTool {
	return SkelTool{
		Name:     tc.Name,
		Args:     redactArgs(tc.Args),
		Invalid:  tc.ArgsInvalid,
		ParseErr: tc.ArgsParseError,
	}
}

// redactArgs chiếu đối tượng tham số công cụ thành key → giá trị ẩn danh. Trả về nil
// nếu tham số không phải object (ArgsInvalid/ParseErr đã được ghi riêng trong SkelTool).
func redactArgs(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = projectValue(v)
	}
	return out
}

// projectValue chiếu một giá trị tham số đơn theo kiểu JSON:
//   - scalar (số / bool / null): giá trị gốc là tín hiệu cấu trúc, giữ nguyên (chapter: 7)
//   - chuỗi ngắn dạng định danh: giữ kèm dấu nháy, lộ kiểu (chapter: "7" ← tín hiệu số-hóa-chuỗi của #34)
//   - chuỗi chứa CJK / khoảng trắng / văn bản dài, object, array: che thành <redacted …> (không xuất nội dung)
//   - đã là placeholder [session_compact: …]: an toàn và có thông tin, giữ nguyên
func projectValue(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return ""
	}
	switch s[0] {
	case '"':
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return redactPlaceholder(s)
		}
		if strings.HasPrefix(str, store.CompactTag) {
			return str
		}
		// Chỉ giữ lại các giá trị ngắn "giống định danh/số/enum" (chapter:"7", type:"premise", agent:"writer");
		// bất kỳ chuỗi nào chứa CJK, khoảng trắng hoặc ký tự đặc biệt khác đều coi là nội dung, che toàn bộ.
		if utf8.RuneCountInString(str) <= 32 && isStructuralToken(str) {
			return strconv.Quote(str)
		}
		return redactPlaceholder(str)
	case '{':
		return fmt.Sprintf("<redacted object len=%d>", len(raw))
	case '[':
		return fmt.Sprintf("<redacted array len=%d>", len(raw))
	default:
		return s
	}
}

// isStructuralToken kiểm tra xem chuỗi có "giống định danh" không — chỉ gồm ký tự
// ASCII chữ / số / `_-.:/`, không có khoảng trắng, không có CJK.
// Dùng để phân biệt tín hiệu cấu trúc (giữ lại) với đoạn nội dung (che đi).
func isStructuralToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == ':' || r == '/':
		default:
			return false
		}
	}
	return true
}

func redactPlaceholder(s string) string {
	return fmt.Sprintf("<redacted len=%d sha=%s>", utf8.RuneCountInString(s), shortHash(s))
}

// shortHash lấy hash ngắn của văn bản; chỉ dùng để xác định "đoạn văn bản này có xuất hiện lặp lại không",
// không phải mục đích mã hóa bảo mật.
func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// firstLine lấy dòng đầu tiên và cắt theo rune, dùng để tóm tắt chuỗi lỗi.
func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	if utf8.RuneCountInString(s) > max {
		r := []rune(s)
		s = string(r[:max]) + "…"
	}
	return s
}
