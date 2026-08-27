package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Khởi động lạnh chế độ đồng sáng tác: làm rõ yêu cầu từ đầu, tạo ra chỉ thị sáng tác cho toàn bộ cuốn sách.
const coCreateSystemPrompt = `Bạn là trợ lý đồng sáng tác tiểu thuyết. Nhiệm vụ của bạn không phải bắt đầu viết tiểu thuyết ngay, mà là giúp người dùng làm rõ yêu cầu sáng tác qua nhiều lượt hội thoại ngắn, đồng thời liên tục tổng hợp thành một đoạn chỉ thị sáng tác có thể giao trực tiếp cho engine sáng tác.

Mỗi lượt trả lời phải tuân thủ nghiêm ngặt định dạng XML sau, gồm bốn thẻ xuất hiện theo thứ tự, mỗi thẻ đều phải có thẻ mở và thẻ đóng đúng cú pháp:

<reply>
Trả lời tự nhiên cho người dùng: trước tiên hồi đáp ý kiến của người dùng, sau đó đặt tối đa 1 đến 2 câu hỏi quan trọng nhất hiện tại. Nếu thông tin đã đủ để bắt đầu sáng tác, hãy thông báo cho người dùng có thể nhấn Ctrl+S để bắt đầu.
</reply>

<draft>
Bản thảo chỉ thị sáng tác hiện tại (đầy đủ), dùng Markdown: bắt đầu trực tiếp từ tiêu đề cấp hai, ví dụ "## Chủ đề", "## Yếu tố chính", "## Thông tin cần làm rõ"; dùng dấu đầu dòng để liệt kê các điểm chính. Mỗi lượt phải **cập nhật tích lũy** trên các kết luận đã có, tiếp thu ý định mới nhất của người dùng; dù lượt này không có gì mới cũng phải viết lại toàn bộ bản thảo — không được bỏ qua, không được viết "（giữ nguyên lượt trước）" hay các nội dung giữ chỗ tương tự.
</draft>
` + coCreateProtocolTail

// Đồng sáng tác theo giai đoạn: tiểu thuyết đã viết được một phần, lên kế hoạch hướng đi của "giai đoạn tiếp theo". Người gọi cần nối tóm tắt trạng thái câu chuyện hiện tại
// vào sau prompt này (phần "## Trạng thái câu chuyện hiện tại"), để mô hình lên kế hoạch dựa trên nội dung đã viết.
const stageCoCreateSystemPrompt = `Bạn là trợ lý "đồng sáng tác theo giai đoạn" cho tiểu thuyết. Cuốn tiểu thuyết này đã được viết một phần (tiến độ xem ở phần "Trạng thái câu chuyện hiện tại" bên dưới). Người dùng tạm dừng lại và muốn cùng bạn lên kế hoạch hướng đi của "giai đoạn tiếp theo" trước khi tiếp tục sáng tác.

Nhiệm vụ của bạn không phải viết tiếp nội dung, mà là giúp người dùng làm rõ hướng đi của đoạn tiếp theo (vài chương tới / cung truyện tiếp / tập tiếp) qua nhiều lượt hội thoại ngắn, đồng thời liên tục tổng hợp thành một "brief hướng đi tiếp theo" để engine sáng tác tiến hành theo đó.

Nguyên tắc cứng: Tất cả đề xuất phải nhất quán với các sự kiện, nhân vật, phục bút đã xảy ra trong "Trạng thái câu chuyện hiện tại", tuyệt đối không được phủ nhận hay bỏ qua nội dung đã viết; chỉ lên kế hoạch "tiếp theo sẽ đi đâu", không thiết kế lại toàn bộ cuốn sách.

Mỗi lượt trả lời phải tuân thủ nghiêm ngặt định dạng XML sau, gồm bốn thẻ xuất hiện theo thứ tự, mỗi thẻ đều phải có thẻ mở và thẻ đóng đúng cú pháp:

<reply>
Trả lời tự nhiên cho người dùng: trước tiên hồi đáp ý kiến của người dùng, sau đó đặt tối đa 1 đến 2 câu hỏi quan trọng nhất hiện tại. Nếu hướng đi tiếp theo đã đủ rõ ràng, hãy thông báo người dùng có thể nhấn Ctrl+S để giao hướng đi cho engine sáng tác và tiếp tục sáng tác.
</reply>

<draft>
"Brief hướng đi tiếp theo" hiện tại (đầy đủ), dùng Markdown: bắt đầu trực tiếp từ tiêu đề cấp hai, ví dụ "## Hướng đi tiếp theo", "## Bước ngoặt chính", "## Phục bút cần giải quyết", "## Nhịp truyện và dung lượng"; dùng dấu đầu dòng để liệt kê các điểm chính. Mỗi lượt phải **cập nhật tích lũy** trên các kết luận đã có, tiếp thu ý định mới nhất của người dùng; dù lượt này không có gì mới cũng phải viết lại toàn bộ brief — không được bỏ qua, không được viết "（giữ nguyên lượt trước）" hay các nội dung giữ chỗ tương tự.
</draft>
` + coCreateProtocolTail

// coCreateProtocolTail là phần đuôi giao thức đầu ra dùng chung cho cả hai chế độ đồng sáng tác (<ready> / <suggestions> + quy chuẩn đầu ra).
// Hai chế độ chỉ khác nhau ở ngữ cảnh mở đầu và ngữ nghĩa của <draft>, giao thức hoàn toàn giống nhau.
const coCreateProtocolTail = `
<ready>false</ready>

<suggestions>
1-3 gợi ý "câu người dùng có thể muốn nói tiếp theo", mỗi gợi ý một dòng bắt đầu bằng "- ". Đây là hướng dẫn khi người dùng bị bí,
nhấn phím số để điền vào ô nhập liệu, người dùng có thể chỉnh sửa rồi gửi.

Yêu cầu:
- Đứng từ góc độ người dùng, như người dùng đang nói chuyện với bạn, không viết thành câu hỏi của trợ lý.
- Mỗi gợi ý không quá 25 ký tự, đa dạng cách diễn đạt, tránh đơn điệu.
- Đưa ra xu hướng / lựa chọn / bổ sung ý định, không viết thay toàn bộ thiết lập cho người dùng.
</suggestions>

Quy chuẩn đầu ra:
- Phải dùng bốn thẻ XML: <reply> / <draft> / <ready> / <suggestions>, mỗi thẻ đều phải mở và đóng đầy đủ.
- Tên thẻ chỉ được dùng chữ thường tiếng Anh, không được viết thành <REPLY> / <REWRITE> / <trả lời> hay bất kỳ biến thể nào khác.
- Ngoài thẻ không được thêm bất kỳ giải thích, suy nghĩ hay code fence nào.
- Trong <draft> cho phép nhiều dòng Markdown, xuống dòng trực tiếp, không cần escape.
- <ready> chỉ viết true hoặc false. Điền true khi thông tin đã đủ.
- Khi <ready>true</ready> thì <suggestions> có thể để trống (giữ lại thẻ rỗng <suggestions></suggestions> là được).`

// CoCreateProgressKind xác định loại nội dung trong callback streaming.
const (
	CoCreateProgressThinking = "thinking"
	CoCreateProgressReply    = "reply"
)

// coCreateMaxTokens giới hạn token đầu ra cho một lượt đồng sáng tác.
// Không đặt quá nhỏ: với reasoning model (gemini-*-antigravity đốt hàng trăm token cho
// phần "suy nghĩ" trước khi ra chữ), trần thấp khiến toàn bộ ngân sách bị reasoning ăn hết,
// model kẹt giữa chừng và trả về rỗng → "cocreate empty response". Bản <draft> lại tích lũy
// đầy đủ mỗi lượt nên còn phình thêm. 8192 để dư chỗ cho reasoning + draft dài, vẫn nằm trong
// giới hạn output của các model phổ biến.
const coCreateMaxTokens = 8192

// Đầu ra XML bốn đoạn. Kiểu XML mạnh hơn marker dạng dấu ngoặc vuông — trong dữ liệu huấn luyện của Claude/GPT
// có rất nhiều định dạng kiểu <thinking>...</thinking>, mô hình gần như không bao giờ viết lại <reply> thành <REWRITE>
// hay biến thể khác; thẻ đóng cũng giúp cắt chính xác hơn giữa chừng streaming (không phụ thuộc vào việc tìm marker tiếp theo để cắt đuôi).
const (
	tagReply       = "reply"
	tagDraft       = "draft"
	tagReady       = "ready"
	tagSuggestions = "suggestions"
)

func coCreateStream(ctx context.Context, models *bootstrap.ModelSet, sessions *store.SessionStore, sysPrompt string, history []CoCreateMessage, onProgress func(kind, text string)) (reply CoCreateReply, err error) {
	if len(history) == 0 {
		return CoCreateReply{}, fmt.Errorf("cocreate history is empty")
	}

	model := models.ForRole("thinking")
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	msgs := []agentcore.Message{agentcore.SystemMsg(sysPrompt)}
	for _, item := range history {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "assistant":
			msgs = append(msgs, assistantMsg(content))
		default:
			msgs = append(msgs, agentcore.UserMsg(content))
		}
	}

	var raw, thinking strings.Builder

	// Để điều tra các sự cố ngẫu nhiên như "cocreate empty response", cần xem mô hình thực sự trả về gì.
	// Mỗi lượt ghi đầy đủ vào <output>/meta/sessions/cocreate.jsonl, cùng vị trí với session log của quá trình sáng tác chính thức.
	start := time.Now()
	defer func() {
		if sessions == nil {
			return
		}
		_ = sessions.LogCoCreate(coCreateLogEntry{
			Time:         time.Now(),
			DurationMS:   time.Since(start).Milliseconds(),
			InputHistory: history,
			RawResponse:  raw.String(),
			RawLen:       len([]rune(raw.String())),
			Thinking:     thinking.String(),
			ParsedReply:  reply.Message,
			ParsedDraft:  reply.Prompt,
			ParsedReady:  reply.Ready,
			ParsedSugs:   reply.Suggestions,
			Error:        errString(err),
		})
	}()

	streamCh, err := model.GenerateStream(ctx, msgs, nil, agentcore.WithMaxTokens(coCreateMaxTokens))
	if err != nil {
		return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", err)
	}

	var streamed bool
	for ev := range streamCh {
		switch ev.Type {
		case agentcore.StreamEventThinkingDelta:
			thinking.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressThinking, thinking.String())
			}
		case agentcore.StreamEventTextDelta:
			streamed = true
			raw.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressReply, extractReplyPreview(raw.String()))
			}
		case agentcore.StreamEventDone:
			if !streamed {
				raw.WriteString(ev.Message.TextContent())
			}
		case agentcore.StreamEventError:
			if ev.Err != nil {
				return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", ev.Err)
			}
			return CoCreateReply{}, fmt.Errorf("cocreate generate failed")
		}
	}

	// Fallback kênh: các mô hình kiểu thinking (R1/GLM-Z1/QwQ, v.v.) đôi khi viết toàn bộ câu trả lời vào
	// reasoning_content rồi không chuyển sang kênh final answer, khiến raw rỗng nhưng thinking chứa
	// đầy đủ bốn đoạn. Thực nghiệm xem trong meta/sessions/cocreate.jsonl — dùng trực tiếp thinking làm raw để parse,
	// tầng giao thức đã có xử lý giảm cấp (khi không có marker [REPLY] thì coi cả đoạn là reply), UI không bị ảnh hưởng.
	rawText := raw.String()
	if strings.TrimSpace(rawText) == "" {
		if t := strings.TrimSpace(thinking.String()); t != "" {
			rawText = t
		}
	}
	reply, err = parseCoCreateResponse(rawText)
	return reply, err
}

// coCreateLogEntry là cấu trúc một dòng ghi vào meta/sessions/cocreate.jsonl.
// Tên trường theo quy ước jsonl để dễ truy vấn trực tiếp (snake_case), thuận tiện lọc bằng jq.
type coCreateLogEntry struct {
	Time         time.Time         `json:"time"`
	DurationMS   int64             `json:"duration_ms"`
	InputHistory []CoCreateMessage `json:"input_history"`
	RawResponse  string            `json:"raw_response"`
	RawLen       int               `json:"raw_len"`
	Thinking     string            `json:"thinking,omitempty"`
	ParsedReply  string            `json:"parsed_reply"`
	ParsedDraft  string            `json:"parsed_draft"`
	ParsedReady  bool              `json:"parsed_ready"`
	ParsedSugs   []string          `json:"parsed_sugs,omitempty"`
	Error        string            `json:"error,omitempty"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func assistantMsg(text string) agentcore.Message {
	return agentcore.Message{
		Role:      agentcore.RoleAssistant,
		Content:   []agentcore.ContentBlock{agentcore.TextBlock(text)},
		Timestamp: time.Now(),
	}
}

// parseCoCreateResponse phân tích đầu ra XML. Nếu mô hình không tuân thủ giao thức (trả về ngôn ngữ tự nhiên trực tiếp),
// cả đoạn được hiển thị làm reply, draft để trống để session giữ lại lượt trước.
func parseCoCreateResponse(raw string) (CoCreateReply, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CoCreateReply{}, fmt.Errorf("cocreate empty response")
	}

	reply, draft, ready, suggestions := splitCoCreateMarkers(raw)
	if reply == "" {
		// Mô hình không tuân thủ giao thức XML: coi cả đoạn là reply.
		return CoCreateReply{Message: raw, Prompt: "", Ready: false, Raw: raw}, nil
	}
	return CoCreateReply{
		Message:     reply,
		Prompt:      draft,
		Ready:       ready,
		Suggestions: suggestions,
		Raw:         raw,
	}, nil
}

// splitCoCreateMarkers tách văn bản theo bốn thẻ XML.
// Thẻ có thể bị thiếu (giữa chừng streaming hoặc mô hình bỏ sót), trường tương ứng với thẻ thiếu sẽ là rỗng / false / nil.
// Khi thiếu thẻ đóng, extractTagContent sẽ lấy đến cuối chuỗi, vẫn cố gắng parse.
func splitCoCreateMarkers(s string) (reply, draft string, ready bool, suggestions []string) {
	reply = extractTagContent(s, tagReply)
	draft = extractTagContent(s, tagDraft)
	readyStr := strings.ToLower(extractTagContent(s, tagReady))
	ready = readyStr == "true" || readyStr == "yes"
	suggestions = parseSuggestions(extractTagContent(s, tagSuggestions))
	return
}

// extractTagContent trích xuất văn bản giữa <tag>...</tag> từ chuỗi s.
// Xử lý dự phòng cho ba tình huống ngẫu nhiên, tránh giảm cấp làm mất trường:
//  1. Có mở không đóng (giữa chừng streaming) → cắt đến trước thẻ mở đã biết tiếp theo
//  2. Không mở có đóng (typo của mô hình, ví dụ <suggestions> viết thành <uggestions>) → bắt đầu từ vị trí kết thúc
//     của thẻ đóng hoàn chỉnh đã biết gần nhất, đến trước </tag>
//  3. reply hoàn toàn không có thẻ mở (mô hình mở đầu bằng ngôn ngữ tự nhiên, cuối dán </reply>) → từ đầu đến </reply>
func extractTagContent(s, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	oIdx := strings.Index(s, open)
	if oIdx >= 0 {
		rest := s[oIdx+len(open):]
		if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
			return strings.TrimSpace(rest[:cIdx])
		}
		// Có mở không đóng → cắt đến trước thẻ mở đã biết tiếp theo
		for _, other := range []string{"<reply>", "<draft>", "<ready>", "<suggestions>"} {
			if other == open {
				continue
			}
			if idx := strings.Index(rest, other); idx >= 0 {
				rest = rest[:idx]
			}
		}
		return strings.TrimSpace(rest)
	}

	// Không mở có đóng → bắt đầu từ vị trí kết thúc của thẻ đóng hoàn chỉnh đã biết gần nhất, đến </tag>.
	if cIdx := strings.Index(s, closeTag); cIdx >= 0 {
		prefix := s[:cIdx]
		start := 0
		for _, t := range []string{"</reply>", "</draft>", "</ready>", "</suggestions>"} {
			if t == closeTag {
				continue
			}
			if i := strings.LastIndex(prefix, t); i >= 0 {
				if end := i + len(t); end > start {
					start = end
				}
			}
		}
		return strings.TrimSpace(prefix[start:])
	}
	return ""
}

// parseSuggestions trích xuất từng dòng trong đoạn <suggestions>, loại bỏ tiền tố danh sách "- " / "* " / "1. " v.v.
// Giữ tối đa 3 gợi ý; bỏ qua dòng trống, quá ngắn (<2 ký tự), hoặc cả dòng trông như thẻ XML (dư thừa typo thẻ mở,
// ví dụ <uggestions>).
func parseSuggestions(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Cả dòng trông như thẻ XML → bỏ qua (chống ô nhiễm từ typo thẻ mở)
		if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") {
			continue
		}
		// Bỏ tiền tố danh sách
		switch {
		case strings.HasPrefix(line, "- "):
			line = strings.TrimSpace(line[2:])
		case strings.HasPrefix(line, "* "):
			line = strings.TrimSpace(line[2:])
		case isOrderedSuggestion(line):
			line = stripOrderedPrefix(line)
		}
		if len([]rune(line)) < 2 {
			continue
		}
		out = append(out, line)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

// isOrderedSuggestion kiểm tra đầu dòng có dạng "1. " / "12. " (số + dấu chấm + khoảng trắng) không.
func isOrderedSuggestion(line string) bool {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(line) && line[i] == '.' && line[i+1] == ' '
}

func stripOrderedPrefix(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return line
	}
	return strings.TrimSpace(line[i+2:])
}

// extractReplyPreview xem trước streaming: khi raw vẫn đang tăng trưởng, trả về một đoạn văn bản có thể hiển thị cho UI.
// Tìm nội dung sau <reply>, cắt đến </reply> hoặc trước thẻ mở tiếp theo <draft>.
// Khi mô hình tuân thủ nửa vời (thiếu thẻ mở <reply>), phần từ đầu đến </reply> hoặc <draft> đều được tính là reply.
func extractReplyPreview(raw string) string {
	trimmed := strings.TrimSpace(raw)
	open := "<" + tagReply + ">"
	closeTag := "</" + tagReply + ">"
	draftOpen := "<" + tagDraft + ">"

	rest := trimmed
	if rIdx := strings.Index(trimmed, open); rIdx >= 0 {
		rest = trimmed[rIdx+len(open):]
	}
	if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
		return strings.TrimSpace(rest[:cIdx])
	}
	if dIdx := strings.Index(rest, draftOpen); dIdx >= 0 {
		rest = rest[:dIdx]
	}
	return strings.TrimSpace(rest)
}
