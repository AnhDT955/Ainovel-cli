package utils

import "strings"

// JSONFieldExtractor trích xuất giá trị chuỗi của một trường chỉ định từ các mảnh JSON streaming.
//
// Khi LLM sinh tool call theo dạng stream, tham số đến từng mảnh một (OpenAI/Anthropic)
// hoặc một lần duy nhất (Gemini). Bộ trích xuất này dùng máy trạng thái quét từng ký tự,
// phát hiện key mục tiêu rồi trích xuất giá trị chuỗi của nó, xử lý cả escape JSON.
type JSONFieldExtractor struct {
	key      string // key cần khớp, ví dụ `"content"` hoặc `"task"`
	state    extractState
	matchPos int
	escape   bool
	buf      strings.Builder
}

type extractState int

const (
	stateScan    extractState = iota // đang quét, tìm key mục tiêu
	stateColon                       // đã khớp key, chờ dấu hai chấm và dấu ngoặc kép mở
	stateExtract                     // đang trích xuất giá trị chuỗi
)

func NewFieldExtractor(fieldName string) *JSONFieldExtractor {
	return &JSONFieldExtractor{key: `"` + fieldName + `"`}
}

// Feed xử lý một đoạn delta, trả về văn bản đã trích xuất (có thể rỗng).
func (e *JSONFieldExtractor) Feed(delta string) string {
	e.buf.Reset()
	for _, r := range delta {
		switch e.state {
		case stateScan:
			e.feedScan(r)
		case stateColon:
			e.feedColon(r)
		case stateExtract:
			e.feedExtract(r)
		}
	}
	return e.buf.String()
}

func (e *JSONFieldExtractor) feedScan(r rune) {
	if e.matchPos < len(e.key) && byte(r) == e.key[e.matchPos] {
		e.matchPos++
		if e.matchPos == len(e.key) {
			e.state = stateColon
			e.matchPos = 0
		}
		return
	}
	e.matchPos = 0
	if byte(r) == e.key[0] {
		e.matchPos = 1
	}
}

func (e *JSONFieldExtractor) feedColon(r rune) {
	switch r {
	case ':', ' ', '\t':
		// bỏ qua
	case '"':
		e.state = stateExtract
		e.escape = false
	default:
		e.state = stateScan
		e.matchPos = 0
		if byte(r) == e.key[0] {
			e.matchPos = 1
		}
	}
}

func (e *JSONFieldExtractor) feedExtract(r rune) {
	if e.escape {
		e.escape = false
		switch r {
		case 'n':
			e.buf.WriteByte('\n')
		case 't':
			e.buf.WriteByte('\t')
		case 'r':
			e.buf.WriteByte('\r')
		case '"', '\\', '/':
			e.buf.WriteRune(r)
		default:
			e.buf.WriteByte('\\')
			e.buf.WriteRune(r)
		}
		return
	}
	switch r {
	case '\\':
		e.escape = true
	case '"':
		e.state = stateScan
		e.matchPos = 0
	default:
		e.buf.WriteRune(r)
	}
}

// Reset đặt lại trạng thái (gọi khi bắt đầu lượt tin nhắn LLM mới).
func (e *JSONFieldExtractor) Reset() {
	e.state = stateScan
	e.matchPos = 0
	e.escape = false
}

// ThinkingSep là ký tự phân tách giữa văn bản suy nghĩ và nội dung chính.
// StreamFilter chèn ký tự này trước đoạn văn bản suy nghĩ; TUI dựa vào đó để chuyển kiểu hiển thị.
const ThinkingSep = "\x02"

// StreamFilter phân biệt phản hồi văn bản và tool call JSON của SubAgent.
// Phản hồi văn bản được đánh dấu là nội dung suy nghĩ (có tiền tố ThinkingSep); tool call JSON chỉ trích xuất trường chỉ định.
//
// Nguyên tắc phán định: gặp { thì vào chế độ JSON (theo dõi độ sâu ngoặc nhọn),
// khi độ sâu về 0 thì quay lại chế độ văn bản.
type StreamFilter struct {
	fieldExt   *JSONFieldExtractor
	mode       filterMode
	braceDepth int
	inString   bool // đang ở trong chuỗi JSON (không đếm ngoặc nhọn)
	escJSON    bool // escape bên trong chuỗi JSON
	thinking   bool // hiện đang ở đoạn văn bản suy nghĩ
	buf        strings.Builder
}

type filterMode int

const (
	filterText filterMode = iota // phản hồi văn bản, truyền thẳng qua
	filterJSON                   // tool call JSON, trích xuất trường mục tiêu
)

func NewStreamFilter(fieldName string) *StreamFilter {
	return &StreamFilter{fieldExt: NewFieldExtractor(fieldName)}
}

// Feed xử lý một đoạn delta, trả về văn bản có thể hiển thị.
// Phản hồi văn bản được xuất trực tiếp; giá trị trường mục tiêu trong JSON được trích xuất và xuất ra; phần còn lại của cấu trúc JSON bị bỏ qua.
func (f *StreamFilter) Feed(delta string) string {
	f.buf.Reset()
	for _, r := range delta {
		switch f.mode {
		case filterText:
			if r == '{' {
				f.thinking = false
				f.mode = filterJSON
				f.braceDepth = 1
				f.inString = false
				f.escJSON = false
				f.fieldExt.Reset()
				f.feedExtractor(r)
			} else {
				if !f.thinking {
					f.thinking = true
					f.buf.WriteString(ThinkingSep)
				}
				f.buf.WriteRune(r)
			}
		case filterJSON:
			f.feedExtractor(r)
			f.trackBraces(r)
		}
	}
	return f.buf.String()
}

// feedExtractor đưa từng ký tự vào fieldExt, ghi kết quả trích xuất vào buf.
func (f *StreamFilter) feedExtractor(r rune) {
	if text := f.fieldExt.Feed(string(r)); text != "" {
		f.buf.WriteString(text)
	}
}

// trackBraces theo dõi độ sâu ngoặc nhọn JSON, chuyển về chế độ văn bản khi độ sâu về 0.
func (f *StreamFilter) trackBraces(r rune) {
	if f.escJSON {
		f.escJSON = false
		return
	}
	if f.inString {
		switch r {
		case '\\':
			f.escJSON = true
		case '"':
			f.inString = false
		}
		return
	}
	switch r {
	case '"':
		f.inString = true
	case '{':
		f.braceDepth++
	case '}':
		f.braceDepth--
		if f.braceDepth <= 0 {
			f.mode = filterText
		}
	}
}

// Reset đặt lại trạng thái.
func (f *StreamFilter) Reset() {
	f.mode = filterText
	f.braceDepth = 0
	f.inString = false
	f.escJSON = false
	f.thinking = false
	f.fieldExt.Reset()
}
