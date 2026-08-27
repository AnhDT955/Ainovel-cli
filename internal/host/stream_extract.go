package host

import (
	"strings"
	"unicode/utf8"
)

// toolDisplays cấu hình chiến lược hiển thị cho từng tool trên panel stream. Các tool
// không có trong bảng này sẽ không tham gia render stream (observer bỏ qua DeltaToolCall).
//
// Chế độ thông thường (nakedKey rỗng): tokenizer render args JSON từ LLM thành văn bản
// dạng thụt lề "key: value", object/array lồng nhau thụt lề theo cấp, string/number/bool
// xuất stream. Hoàn toàn tách biệt khỏi schema — LLM xuất thêm một trường thì panel tự
// thêm một dòng, không cần thay đổi code.
//
// Chế độ naked stream (nakedKey khác rỗng): chỉ xuất nguyên bản giá trị string của
// trường đích ở cấp obj cao nhất, bỏ qua tất cả các trường khác. Dùng cho draft_chapter
// để toàn bộ markdown của chương không bị bọc thành "content: # …".
// header luôn bắt đầu bằng "✻ ": đây là tiền tố quy ước để TUI renderStreamContent
// đi vào nhánh renderAgentBlock (vàng ✻ + nền cyan gạch chân xanh label + dim gạch
// ngang), nhất quán với fallback header (streamHeaderFallback); đổi thành văn bản thường
// sẽ rơi vào nhánh nội dung chính, vẽ bằng màu terminal mặc định, title không còn nổi bật.
var toolDisplays = map[string]toolDisplay{
	"draft_chapter": {nakedKey: "content"},

	"plan_chapter":        {header: "✻ Lên kế hoạch"},
	"edit_chapter":        {header: "✻ Chỉnh sửa"},
	"commit_chapter":      {header: "✻ Lưu chương"},
	"save_review":         {header: "✻ Đánh giá"},
	"save_arc_summary":    {header: "✻ Tóm tắt cung truyện"},
	"save_volume_summary": {header: "✻ Tóm tắt tập"},
	"save_foundation":     {header: "✻ Cài đặt"},
	"read_chapter":        {header: "✻ Đọc chương"},
	"check_consistency":   {header: "✻ Kiểm tra nhất quán"},
	"novel_context":       {header: "✻ Truy vấn ngữ cảnh"},
}

type toolDisplay struct {
	header   string
	nakedKey string
}

// jsonFieldExtractor là JSON tokenizer hoạt động theo kiểu stream. Điều khiển máy trạng
// thái từng byte, chuyển đổi args tool từ LLM thành văn bản có thể đọc được. Mỗi instance
// chỉ phục vụ một lần gọi tool, Done()=true sau khi container cấp cao nhất đóng lại.
type jsonFieldExtractor struct {
	cfg toolDisplay

	state pState
	stack []byte // stack container: 'O' obj / 'A' arr

	keyBuf strings.Builder

	escape bool
	uHex   []byte

	started bool // đã emit ký tự nào chưa (dùng để xuống dòng giữa header và key đầu tiên)

	done bool
}

type pState int

const (
	psRoot         pState = iota
	psBeforeKey           // trong obj: chờ key tiếp theo hoặc }
	psInKey               // trong obj: đang phân tích key
	psAfterKey            // trong obj: chờ :
	psBeforeValue         // chờ ký tự bắt đầu value
	psStringStream        // giá trị string, emit cooked theo stream
	psStringSkip          // giá trị string, bỏ qua (chế độ naked stream, không phải trường đích)
	psNumberStream        // số, emit theo stream
	psNumberSkip          // số, bỏ qua
	psPrimStream          // true/false/null, emit theo stream
	psPrimSkip            // true/false/null, bỏ qua
	psDone                // container cấp cao nhất đã đóng
)

func newToolExtractor(tool string) *jsonFieldExtractor {
	cfg, ok := toolDisplays[tool]
	if !ok {
		return nil
	}
	return &jsonFieldExtractor{cfg: cfg}
}

func (e *jsonFieldExtractor) Done() bool { return e.done }

func (e *jsonFieldExtractor) Feed(chunk string) string {
	if e.done || chunk == "" {
		return ""
	}
	var out strings.Builder
	for i := 0; i < len(chunk); i++ {
		e.step(chunk[i], &out)
		if e.done {
			break
		}
	}
	return out.String()
}

// ── Stack container / thụt lề ──

func (e *jsonFieldExtractor) push(kind byte) {
	e.stack = append(e.stack, kind)
}

func (e *jsonFieldExtractor) pop() {
	if len(e.stack) == 0 {
		return
	}
	e.stack = e.stack[:len(e.stack)-1]
}

func (e *jsonFieldExtractor) parent() byte {
	if len(e.stack) == 0 {
		return 0
	}
	return e.stack[len(e.stack)-1]
}

// writeIndent ghi thụt lề hiện tại. Độ sâu = số cấp lồng nhau = len(stack)-1 (bên trong container gốc không thụt lề).
func (e *jsonFieldExtractor) writeIndent(out *strings.Builder) {
	depth := len(e.stack) - 1
	for range depth {
		out.WriteString("  ")
	}
}

// ── Máy trạng thái ──

func (e *jsonFieldExtractor) step(c byte, out *strings.Builder) {
	switch e.state {
	case psRoot:
		switch c {
		case '{':
			e.push('O')
			e.state = psBeforeKey
		case '[':
			// Thực tế không xảy ra (tool args luôn là obj); xử lý phòng ngừa: coi là root arr
			e.push('A')
			e.state = psBeforeValue
		}
	case psBeforeKey:
		switch c {
		case '"':
			e.keyBuf.Reset()
			e.escape = false
			e.state = psInKey
		case '}':
			e.closeContainer(out)
		case ' ', '\t', '\n', '\r', ',':
		}
	case psInKey:
		if e.escape {
			e.keyBuf.WriteByte(c)
			e.escape = false
			return
		}
		if c == '\\' {
			e.escape = true
			return
		}
		if c == '"' {
			e.emitKeyLine(out, e.keyBuf.String())
			e.state = psAfterKey
			return
		}
		e.keyBuf.WriteByte(c)
	case psAfterKey:
		if c == ':' {
			e.state = psBeforeValue
		}
	case psBeforeValue:
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			return
		}
		switch c {
		case '"':
			e.beginString(out)
		case '{':
			e.beginNested('O', out)
		case '[':
			e.beginNested('A', out)
		case ']', '}':
			e.closeContainer(out)
		case 't', 'f', 'n':
			e.beginPrim(c, out)
		default:
			if c == '-' || (c >= '0' && c <= '9') {
				e.beginNumber(c, out)
			}
		}
	case psStringStream:
		e.handleStringByte(c, out, false)
	case psStringSkip:
		e.handleStringByte(c, out, true)
	case psNumberStream:
		if isNumberByte(c) {
			out.WriteByte(c)
			return
		}
		e.afterValueChar(c, out)
	case psNumberSkip:
		if isNumberByte(c) {
			return
		}
		e.afterValueChar(c, out)
	case psPrimStream:
		if c >= 'a' && c <= 'z' {
			out.WriteByte(c)
			return
		}
		e.afterValueChar(c, out)
	case psPrimSkip:
		if c >= 'a' && c <= 'z' {
			return
		}
		e.afterValueChar(c, out)
	case psDone:
	}
}

// ── Render dòng ──

// emitKeyLine được gọi khi key trong obj đã phân tích xong, ghi ra tiền tố "<lf><indent>key:".
// Ở chế độ naked stream không ghi tiền tố key (key được lưu trong keyBuf để beginString kiểm tra).
func (e *jsonFieldExtractor) emitKeyLine(out *strings.Builder, key string) {
	if e.cfg.nakedKey != "" {
		return
	}
	if !e.started {
		if e.cfg.header != "" {
			out.WriteString(e.cfg.header)
			out.WriteByte('\n')
		}
		e.started = true
	} else {
		out.WriteByte('\n')
	}
	e.writeIndent(out)
	out.WriteString(key)
	out.WriteByte(':')
}

// emitArrayItem được gọi khi bắt đầu mỗi phần tử trong arr, ghi ra "<lf><indent>-".
// Phần tử primitive theo sau bằng khoảng trắng rồi emit giá trị; phần tử struct do lồng nhau tự xuống dòng.
func (e *jsonFieldExtractor) emitArrayItem(out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		return
	}
	if !e.started {
		if e.cfg.header != "" {
			out.WriteString(e.cfg.header)
			out.WriteByte('\n')
		}
		e.started = true
	} else {
		out.WriteByte('\n')
	}
	e.writeIndent(out)
	out.WriteByte('-')
}

// ── Bắt đầu value ──

func (e *jsonFieldExtractor) beginString(out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		// Naked stream: chỉ xuất giá trị string của key đích trong obj cấp cao nhất
		if e.cfg.nakedKey == e.keyBuf.String() && len(e.stack) == 1 && e.stack[0] == 'O' {
			e.state = psStringStream
		} else {
			e.state = psStringSkip
		}
		e.escape = false
		e.uHex = nil
		return
	}
	// Thông thường: trường obj theo sau "key: " (đã emit "key:", bổ sung khoảng trắng); phần tử arr theo sau "- "
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	e.state = psStringStream
	e.escape = false
	e.uHex = nil
}

func (e *jsonFieldExtractor) beginNumber(first byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		e.state = psNumberSkip
		return
	}
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	out.WriteByte(first)
	e.state = psNumberStream
}

func (e *jsonFieldExtractor) beginPrim(first byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		e.state = psPrimSkip
		return
	}
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	out.WriteByte(first)
	e.state = psPrimStream
}

func (e *jsonFieldExtractor) beginNested(kind byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		// Chế độ naked stream không mở rộng lồng nhau; dùng độ sâu stack để theo dõi đến } / ] tương ứng
		e.push(kind)
		if kind == 'O' {
			e.state = psBeforeKey
		} else {
			e.state = psBeforeValue
		}
		return
	}
	// Chế độ thông thường: khi phần tử arr là cấu trúc lồng nhau, emit trước một dòng riêng "<indent>-"
	// (sau ":" của obj key không có khoảng trắng, để sub-key lồng nhau tự xuống dòng tiếp theo)
	if e.parent() == 'A' {
		e.emitArrayItem(out)
	}
	e.push(kind)
	if kind == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// closeContainer xử lý } hoặc ].
func (e *jsonFieldExtractor) closeContainer(out *strings.Builder) {
	e.pop()
	if len(e.stack) == 0 {
		// Args rỗng (như novel_context không truyền tham số): emitKeyLine không có cơ hội xuất header,
		// bổ sung tại đây để tránh tình trạng "không có tiêu đề cũng không có nội dung".
		if !e.started && e.cfg.nakedKey == "" && e.cfg.header != "" {
			out.WriteString(e.cfg.header)
			out.WriteByte('\n')
			e.started = true
		}
		// Xuống dòng kết thúc để tạo ranh giới rõ ràng giữa panel và đầu ra tiếp theo
		if e.started {
			out.WriteByte('\n')
		}
		e.state = psDone
		e.done = true
		return
	}
	if e.parent() == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// ── String stream ──

func (e *jsonFieldExtractor) handleStringByte(c byte, out *strings.Builder, skipping bool) {
	if e.uHex != nil {
		e.uHex = append(e.uHex, c)
		if len(e.uHex) == 4 {
			if r, ok := parseHex4(e.uHex); ok && !skipping {
				var buf [4]byte
				n := utf8.EncodeRune(buf[:], r)
				out.Write(buf[:n])
			}
			e.uHex = nil
		}
		return
	}
	if e.escape {
		e.escape = false
		if !skipping {
			writeEscapedByte(out, c)
		}
		if c == 'u' {
			e.uHex = make([]byte, 0, 4)
		}
		return
	}
	if c == '\\' {
		e.escape = true
		return
	}
	if c == '"' {
		e.afterValueDone()
		return
	}
	if !skipping {
		out.WriteByte(c)
	}
}

func writeEscapedByte(out *strings.Builder, c byte) {
	switch c {
	case 'n':
		out.WriteByte('\n')
	case 't':
		out.WriteByte('\t')
	case 'r':
		out.WriteByte('\r')
	case '"':
		out.WriteByte('"')
	case '\\':
		out.WriteByte('\\')
	case '/':
		out.WriteByte('/')
	case 'b', 'f':
		// Backspace / form feed: bỏ qua
	case 'u':
		// Bộ đệm uHex được tạo bởi caller; không xuất tại đây
	default:
		out.WriteByte('\\')
		out.WriteByte(c)
	}
}

// ── Kết thúc ──

// afterValueDone chuyển sang trạng thái tiếp theo sau khi string đóng lại (đọc được `"` cuối).
func (e *jsonFieldExtractor) afterValueDone() {
	e.escape = false
	e.uHex = nil
	if len(e.stack) == 0 {
		e.state = psDone
		e.done = true
		return
	}
	if e.parent() == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// afterValueChar quyết định trạng thái tiếp theo khi đọc được "ký tự kết thúc" của number/primitive.
// Ký tự này có thể là , / } / ] / khoảng trắng, hàm này xử lý và điều phối.
func (e *jsonFieldExtractor) afterValueChar(c byte, out *strings.Builder) {
	switch c {
	case '}', ']':
		e.closeContainer(out)
	case ',', ' ', '\t', '\n', '\r':
		if len(e.stack) == 0 {
			e.state = psDone
			e.done = true
			return
		}
		if e.parent() == 'O' {
			e.state = psBeforeKey
		} else {
			e.state = psBeforeValue
		}
	}
}

// ── Tiện ích ──

func isNumberByte(c byte) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'-', '+', '.', 'e', 'E':
		return true
	}
	return false
}

func parseHex4(b []byte) (rune, bool) {
	var r rune
	for _, d := range b {
		var v rune
		switch {
		case d >= '0' && d <= '9':
			v = rune(d - '0')
		case d >= 'a' && d <= 'f':
			v = rune(d-'a') + 10
		case d >= 'A' && d <= 'F':
			v = rune(d-'A') + 10
		default:
			return 0, false
		}
		r = r*16 + v
	}
	return r, true
}
