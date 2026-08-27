package req

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// maxRequirementRunes giới hạn kích thước tài liệu đưa vào LLM, tránh vượt cửa sổ ngữ cảnh.
// Tài liệu yêu cầu thường ngắn (vài KB); mốc này rộng rãi cho cả bản world-bible dài.
const maxRequirementRunes = 100000

// Run chạy Requirement Extractor trong goroutine và trả kênh sự kiện tiến trình.
// Luồng phụ độc lập (không đi qua coordinator/flow router), theo đúng mẫu sim.Run/imp.Run.
func Run(ctx context.Context, deps Deps, opts Options) (<-chan Event, error) {
	if deps.LLM == nil {
		return nil, fmt.Errorf("deps incomplete: llm is required")
	}
	if strings.TrimSpace(deps.Prompt) == "" {
		return nil, fmt.Errorf("deps incomplete: prompt is required")
	}
	if strings.TrimSpace(opts.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}

	dumpPath := strings.TrimSpace(opts.RawDumpPath)
	if dumpPath == "" {
		dumpPath = RawDumpPath()
	}

	events := make(chan Event, 8)
	go func() {
		defer close(events)
		emit := func(stage Stage, msg string, err error, result *Result) {
			ev := Event{Time: time.Now(), Stage: stage, Message: msg, Err: err, Result: result}
			select {
			case events <- ev:
			case <-ctx.Done():
			}
		}

		emit(StageRead, "Đang đọc tài liệu yêu cầu...", nil, nil)
		data, err := os.ReadFile(opts.Path)
		if err != nil {
			emit(StageError, "Đọc file thất bại", err, nil)
			return
		}
		content := strings.TrimSpace(utils.DecodeText(data))
		if content == "" {
			emit(StageError, "File rỗng", fmt.Errorf("file rỗng: %s", opts.Path), nil)
			return
		}

		if err := ctx.Err(); err != nil {
			emit(StageError, "Đã hủy", err, nil)
			return
		}

		emit(StageExtract, "Đang trích xuất và ánh xạ yêu cầu...", nil, nil)
		result, err := Extract(ctx, deps.LLM, deps.Prompt, content, thinkingOpts(deps.Thinking)...)
		if err != nil {
			emit(StageError, "Trích xuất yêu cầu thất bại", annotateRawDump(err, dumpPath), nil)
			return
		}
		emit(StageDone, "Trích xuất hoàn tất", nil, result)
	}()
	return events, nil
}

// RawDumpPath là nơi ghi phản hồi thô của LLM khi trích xuất hỏng.
// Tên cố định (ghi đè mỗi lần) để người dùng luôn biết tìm ở đâu.
func RawDumpPath() string {
	return filepath.Join(os.TempDir(), "ainovel-extract-raw.txt")
}

// annotateRawDump ghi phản hồi thô ra đĩa và gắn đường dẫn vào lỗi.
// Đoạn trích trong thông báo lỗi bị cắt ngắn cho vừa TUI; file này giữ nguyên toàn bộ.
//
// path phải do lời gọi truyền vào (Options.RawDumpPath) chứ không mặc định cứng ở đây:
// test từng ghi đè file dump thật, khiến dữ liệu giả của test bị nhầm là phản hồi model.
func annotateRawDump(err error, path string) error {
	var pe *ParseError
	if !errors.As(err, &pe) {
		return err
	}
	if writeErr := os.WriteFile(path, []byte(pe.Raw), 0o600); writeErr != nil {
		return err
	}
	return fmt.Errorf("%w\n\n[Phản hồi đầy đủ đã ghi vào: %s]", err, path)
}

// Extract ánh xạ tài liệu thành Result (brief + coverage).
//
// Gọi LLM một lần; nếu model phớt lờ schema (đã gặp thực tế: model tự bịa cấu trúc
// theo hình dạng tài liệu, không có trường brief) thì gọi thêm đúng MỘT lượt sửa.
// Không thử lại vô hạn: model đã hỏng hai lần liên tiếp thì lỗi cần nổi lên cho người dùng thấy.
func Extract(ctx context.Context, llm LLMChat, systemPrompt, content string, opts ...agentcore.CallOption) (*Result, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return nil, fmt.Errorf("system prompt is required")
	}
	msgs := []agentcore.Message{
		agentcore.SystemMsg(systemPrompt),
		agentcore.UserMsg(buildUserPrompt(content)),
	}

	resp, err := generate(ctx, llm, msgs, opts...)
	if err != nil {
		return nil, err
	}
	result, parseErr := parseResult(resp.Message.TextContent())
	if parseErr == nil {
		return result, nil
	}

	var pe *ParseError
	if !errors.As(parseErr, &pe) {
		return nil, parseErr
	}

	msgs = append(msgs, resp.Message, agentcore.UserMsg(buildRepairPrompt()))
	repaired, err := generate(ctx, llm, msgs, opts...)
	if err != nil {
		return nil, parseErr // lượt sửa không gọi được: giữ lỗi gốc, nó mô tả đúng vấn đề hơn
	}
	result, repairErr := parseResult(repaired.Message.TextContent())
	if repairErr != nil {
		return nil, repairErr
	}
	return result, nil
}

func generate(ctx context.Context, llm LLMChat, msgs []agentcore.Message, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	if streamLLM, ok := llm.(LLMStream); ok {
		return generateStream(ctx, streamLLM, msgs, opts...)
	}

	resp, err := llm.Generate(ctx, msgs, nil, opts...)
	if err != nil {
		return nil, fmt.Errorf("llm extract: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("llm extract: nil response")
	}
	return resp, nil
}

// generateStream gom phản hồi streaming về cùng hình dạng LLMResponse mà bộ
// parser hiện tại sử dụng. Việc tiêu thụ delta ngay khi server phát giữ kết nối
// sống trong lúc model tạo brief dài, tránh ngưỡng timeout của proxy trên đường
// request đồng bộ.
func generateStream(ctx context.Context, llm LLMStream, msgs []agentcore.Message, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	events, err := llm.GenerateStream(ctx, msgs, nil, opts...)
	if err != nil {
		return nil, fmt.Errorf("llm extract: %w", err)
	}

	var partial agentcore.Message
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("llm extract: %w", ctx.Err())
		case ev, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("llm extract: stream closed before done (partial bytes: %d)", len(partial.TextContent()))
			}
			switch ev.Type {
			case agentcore.StreamEventDone:
				return &agentcore.LLMResponse{Message: ev.Message}, nil
			case agentcore.StreamEventError:
				if ev.Err == nil {
					return nil, fmt.Errorf("llm extract: stream failed")
				}
				return nil, fmt.Errorf("llm extract: %w", ev.Err)
			default:
				partial = ev.Message
			}
		}
	}
}

// thinkingOpts chuyển cường độ thinking đã được tầng gọi xác thực thành CallOption.
// Trống = không ghi đè, để mặc định của model/provider quyết định.
func thinkingOpts(level agentcore.ThinkingLevel) []agentcore.CallOption {
	if level == "" {
		return nil
	}
	return []agentcore.CallOption{agentcore.WithThinking(level)}
}

// parseResult dựng Result từ phản hồi thô, qua hai lớp:
//  1. Parse JSON chặt chẽ (đường đi bình thường).
//  2. Nếu lớp 1 hỏng hoặc không có brief: quét thẳng trường "brief"/"title" trong văn bản thô.
//     Lớp này cứu được các ca thực tế hay gặp — model bọc kết quả trong một lớp vỏ
//     (`{"result":{...}}`), JSON bị cắt cụt, hoặc chuỗi có ký tự không escape đúng —
//     vì brief mới là thứ bắt buộc phải có, coverage chỉ để hiển thị.
func parseResult(text string) (*Result, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return nil, fmt.Errorf("phản hồi rỗng")
	}

	var result Result
	strictErr := parseJSONPayload(raw, &result)
	if strictErr == nil && strings.TrimSpace(result.Brief) != "" {
		return &result, nil
	}

	if brief := strings.TrimSpace(utils.NewFieldExtractor("brief").Feed(raw)); brief != "" {
		result.Brief = brief
		if strings.TrimSpace(result.Title) == "" {
			result.Title = strings.TrimSpace(utils.NewFieldExtractor("title").Feed(raw))
		}
		return &result, nil
	}

	if strictErr != nil {
		return nil, &ParseError{Reason: fmt.Sprintf("không đọc được JSON từ phản hồi: %v", strictErr), Raw: raw}
	}
	return nil, &ParseError{Reason: "phản hồi không có trường brief", Raw: raw}
}

// ParseError mang theo phản hồi thô của LLM để tầng trên ghi ra đĩa phục vụ chẩn đoán.
// Không có nó thì lỗi trích xuất là ngõ cụt: không cách nào biết model đã trả về gì.
type ParseError struct {
	Reason string
	Raw    string
}

func (e *ParseError) Error() string {
	return e.Reason + " — phản hồi nhận được: " + snippet(e.Raw)
}

// snippet cắt phản hồi thô thành đoạn ngắn để nhét vào thông báo lỗi,
// giúp chẩn đoán ngay trên TUI thay vì chỉ thấy "thất bại" trống trơn.
func snippet(s string) string {
	const limit = 400
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// buildUserPrompt nhắc lại schema ngay trong lượt user.
//
// Bản cũ chỉ nói "ánh xạ tài liệu ... thành JSON theo schema" — câu đó đọc lên là
// "biến tài liệu này thành JSON", và model làm đúng nghĩa đen: nó sinh JSON mô phỏng
// hình dạng tài liệu (ten_truyen/the_loai/the_luc_chinh...) thay vì schema ta cần.
// Schema chỉ nằm ở system prompt nên bị lấn át. Nay nêu thẳng ba trường bắt buộc ở đây.
func buildUserPrompt(content string) string {
	return `Đọc tài liệu dưới đây và viết một BẢN YÊU CẦU SÁNG TÁC chuẩn hóa.

Trả về DUY NHẤT một đối tượng JSON có đúng ba trường ở cấp cao nhất:
  "title"    — chuỗi: tên truyện nguyên văn (chuỗi rỗng nếu tài liệu không nêu).
  "brief"    — chuỗi: TOÀN BỘ nội dung tài liệu viết lại thành một văn bản Markdown liền mạch.
  "coverage" — đối tượng: {"mapped": [...], "missing": [...], "notes": "..."}.

Đây KHÔNG phải việc chuyển tài liệu thành JSON theo cấu trúc của nó. Mọi thông tin
(thể loại, cốt truyện, hệ thống, cấp bậc, thế lực, nhân vật...) phải nằm BÊN TRONG
chuỗi "brief" dưới dạng Markdown — không tách thành các trường JSON riêng.

--- TÀI LIỆU ---
` + clamp(content) + `
--- HẾT TÀI LIỆU ---`
}

// buildRepairPrompt là lượt sửa khi model phớt lờ schema.
// Model đã làm xong phần khó (đọc hiểu tài liệu); lượt này chỉ yêu cầu đóng gói lại cho đúng,
// nên rẻ và tỉ lệ thành công cao hơn là bỏ đi làm lại từ đầu.
func buildRepairPrompt() string {
	return `Phản hồi trên SAI ĐỊNH DẠNG: nó thiếu trường bắt buộc "brief".

Hãy trả lại DUY NHẤT một đối tượng JSON có đúng ba trường cấp cao nhất: "title", "brief", "coverage".

Giữ nguyên toàn bộ nội dung bạn vừa trích xuất, nhưng gộp hết vào chuỗi "brief" dưới dạng
Markdown liền mạch (dùng \n cho xuống dòng), thay vì tách ra thành các trường JSON riêng.
Không được lược bỏ bất kỳ thông tin nào đã có ở phản hồi trên.`
}

func clamp(s string) string {
	runes := []rune(s)
	if len(runes) <= maxRequirementRunes {
		return s
	}
	return string(runes[:maxRequirementRunes]) + "\n\n[...tài liệu bị cắt bớt do quá dài...]"
}

// parseJSONPayload trích JSON từ phản hồi LLM: chấp nhận JSON thuần hoặc bọc trong ```json fence,
// và bỏ qua văn bản thừa quanh đối tượng.
func parseJSONPayload(text string, v any) error {
	s := stripOuterFence(strings.TrimSpace(text))
	if s == "" {
		return fmt.Errorf("phản hồi rỗng")
	}
	obj, ok := firstJSONObject(s)
	if !ok {
		return fmt.Errorf("không tìm thấy đối tượng JSON trong phản hồi")
	}
	return json.Unmarshal([]byte(obj), v)
}

// stripOuterFence chỉ bóc hàng rào code khi phản hồi MỞ ĐẦU bằng ```.
// Hàng rào nằm giữa phản hồi là nội dung của brief (brief là Markdown, có thể chứa khối code),
// không phải lớp bọc — bóc nó sẽ xé nát payload.
func stripOuterFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return s
	}
	s = s[nl+1:] // bỏ dòng mở ```json / ```
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j] // bỏ hàng rào đóng cuối cùng
	}
	return strings.TrimSpace(s)
}

// firstJSONObject trả về đối tượng JSON đầu tiên trong s bằng cách đếm ngoặc nhọn cân bằng,
// bỏ qua ngoặc nằm trong chuỗi. An toàn hơn cách cắt `{` đầu tiên tới `}` cuối cùng —
// cách đó ăn cả văn bản thừa phía sau và vỡ khi brief chứa dấu ngoặc nhọn.
func firstJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	var depth int
	var inStr, esc bool
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	// Ngoặc không cân bằng (phản hồi bị cắt cụt): trả phần còn lại để lỗi unmarshal nói rõ nguyên nhân.
	return s[start:], true
}
