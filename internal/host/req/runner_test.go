package req

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/voocel/agentcore"
)

type fakeLLM struct {
	response string
	err      error
}

type streamingLLM struct {
	response        string
	err             error
	closeBeforeDone bool
	generateCalls   int
	streamCalls     int
}

func (s *streamingLLM) Generate(_ context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	s.generateCalls++
	return nil, fmt.Errorf("đường Generate đồng bộ không được gọi")
}

func (s *streamingLLM) GenerateStream(_ context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	s.streamCalls++
	ch := make(chan agentcore.StreamEvent, 2)
	if s.err != nil {
		ch <- agentcore.StreamEvent{Type: agentcore.StreamEventError, Err: s.err}
		close(ch)
		return ch, nil
	}
	msg := agentcore.Message{Role: agentcore.RoleAssistant, Content: []agentcore.ContentBlock{agentcore.TextBlock(s.response)}}
	ch <- agentcore.StreamEvent{Type: agentcore.StreamEventTextDelta, Delta: s.response, Message: msg}
	if !s.closeBeforeDone {
		ch <- agentcore.StreamEvent{Type: agentcore.StreamEventDone, Message: msg}
	}
	close(ch)
	return ch, nil
}

func (f *fakeLLM) Generate(_ context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &agentcore.LLMResponse{
		Message: agentcore.Message{
			Role:      agentcore.RoleAssistant,
			Content:   []agentcore.ContentBlock{agentcore.TextBlock(f.response)},
			Timestamp: time.Now(),
		},
	}, nil
}

func TestParseJSONPayload_Variants(t *testing.T) {
	cases := []string{
		`{"title":"A","brief":"B"}`,
		"```json\n{\"title\":\"A\",\"brief\":\"B\"}\n```",
		"Đây là kết quả:\n{\"title\":\"A\",\"brief\":\"B\"}\nHết.",
	}
	for i, c := range cases {
		var r Result
		if err := parseJSONPayload(c, &r); err != nil {
			t.Fatalf("case %d parse lỗi: %v", i, err)
		}
		if r.Title != "A" || r.Brief != "B" {
			t.Fatalf("case %d sai: %+v", i, r)
		}
	}
}

func TestExtract_OK(t *testing.T) {
	resp := `{"title":"Xuyên Không","brief":"# Xuyên Không\n\nnội dung","coverage":{"mapped":["Tên truyện","17 thế lực"],"missing":[],"notes":"ok"}}`
	res, err := Extract(context.Background(), &fakeLLM{response: resp}, "system", "tài liệu")
	if err != nil {
		t.Fatalf("Extract lỗi: %v", err)
	}
	if res.Title != "Xuyên Không" {
		t.Fatalf("title sai: %q", res.Title)
	}
	if len(res.Coverage.Mapped) != 2 {
		t.Fatalf("coverage mapped sai: %+v", res.Coverage)
	}
}

func TestExtract_UuTienStreamingDeTranhProxyTimeout(t *testing.T) {
	resp := `{"title":"X","brief":"## Nội dung","coverage":{"mapped":[],"missing":[],"notes":""}}`
	llm := &streamingLLM{response: resp}
	res, err := Extract(context.Background(), llm, "system", "tài liệu")
	if err != nil {
		t.Fatalf("Extract streaming lỗi: %v", err)
	}
	if res.Title != "X" {
		t.Fatalf("title sai: %q", res.Title)
	}
	if llm.streamCalls != 1 || llm.generateCalls != 0 {
		t.Fatalf("phải ưu tiên streaming, stream=%d generate=%d", llm.streamCalls, llm.generateCalls)
	}
}

func TestExtract_StreamingBaoLoiGiuaLuong(t *testing.T) {
	_, err := Extract(context.Background(), &streamingLLM{err: fmt.Errorf("proxy 520")}, "system", "doc")
	if err == nil || !strings.Contains(err.Error(), "proxy 520") {
		t.Fatalf("phải nổi lỗi stream, nhận: %v", err)
	}
}

func TestExtract_StreamingDongSomKhongNhanKetQuaCatCut(t *testing.T) {
	_, err := Extract(context.Background(), &streamingLLM{response: `{"title":"X"`, closeBeforeDone: true}, "system", "doc")
	if err == nil || !strings.Contains(err.Error(), "stream closed before done") {
		t.Fatalf("phải báo stream bị đóng sớm, nhận: %v", err)
	}
}

func TestExtract_EmptyBrief(t *testing.T) {
	resp := `{"title":"X","brief":"   ","coverage":{}}`
	if _, err := Extract(context.Background(), &fakeLLM{response: resp}, "system", "doc"); err == nil {
		t.Fatal("kỳ vọng lỗi khi brief rỗng")
	}
}

// brief là Markdown nên có thể chứa hàng rào code. Parser không được nhầm nó là lớp bọc.
func TestParseResult_BriefChuaHangRaoCode(t *testing.T) {
	brief := "## Tên truyện\nXuyên Không\n\n```\nkhối code trong brief\n```\n\n## Ràng buộc bảo toàn\nPHẢI bảo toàn nguyên văn."
	cov := Coverage{Mapped: []string{"Tên truyện"}, Missing: []string{}, Notes: "ok"}
	payload, err := json.Marshal(Result{Title: "Xuyên Không", Brief: brief, Coverage: cov})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cases := map[string]string{
		"json thuần":   string(payload),
		"bọc fence":    "```json\n" + string(payload) + "\n```",
		"có lời dẫn":   "Kết quả:\n" + string(payload),
		"có đuôi thừa": string(payload) + "\n\nHy vọng giúp ích!",
	}
	for name, in := range cases {
		res, err := parseResult(in)
		if err != nil {
			t.Fatalf("%s: lỗi không mong đợi: %v", name, err)
		}
		if res.Brief != brief {
			t.Fatalf("%s: brief sai:\n có: %q\nmuốn: %q", name, res.Brief, brief)
		}
		if res.Title != "Xuyên Không" {
			t.Fatalf("%s: title sai: %q", name, res.Title)
		}
	}
}

// Lớp sửa chữa: model bọc kết quả trong một lớp vỏ -> JSON hợp lệ nhưng brief nằm sâu bên trong.
// Đây chính là ca sinh ra lỗi "brief rỗng trong kết quả trích xuất".
func TestParseResult_ModelBocLopVo(t *testing.T) {
	res, err := parseResult(`{"result":{"title":"Xuyên Không","brief":"## Tên truyện\nXuyên Không","coverage":{"mapped":["a"],"missing":[],"notes":"ok"}}}`)
	if err != nil {
		t.Fatalf("kỳ vọng cứu được brief, nhận lỗi: %v", err)
	}
	if !strings.Contains(res.Brief, "Xuyên Không") {
		t.Fatalf("brief sai: %q", res.Brief)
	}
	if res.Title != "Xuyên Không" {
		t.Fatalf("title sai: %q", res.Title)
	}
}

// JSON bị cắt cụt giữa chừng vẫn cứu được phần brief đã sinh ra.
func TestParseResult_JSONCatCut(t *testing.T) {
	res, err := parseResult(`{"title":"X","brief":"## Tên truyện\nXuyên Không Tu Tiên\n\n## Thế lực\n- Huyền Thiên Tông`)
	if err != nil {
		t.Fatalf("kỳ vọng cứu được brief, nhận lỗi: %v", err)
	}
	if !strings.Contains(res.Brief, "Huyền Thiên Tông") {
		t.Fatalf("brief sai: %q", res.Brief)
	}
}

// Khi thật sự không có brief, lỗi phải kèm phản hồi thô để chẩn đoán được.
func TestParseResult_LoiKemPhanHoiTho(t *testing.T) {
	_, err := parseResult(`{"title":"X","coverage":{"mapped":[],"missing":[],"notes":"n"}}`)
	if err == nil {
		t.Fatal("kỳ vọng lỗi khi không có brief")
	}
	if !strings.Contains(err.Error(), `"title":"X"`) {
		t.Fatalf("lỗi phải kèm phản hồi thô, nhận: %v", err)
	}
}

// phanHoiSaiSchemaThucTe là phản hồi THẬT đã ghi lại được từ model:
// nó phớt lờ schema và tự bịa cấu trúc theo hình dạng tài liệu, không có trường brief.
const phanHoiSaiSchemaThucTe = `{
  "ten_truyen": "Loạn Thế Võ Đạo: Ta Dựa Vào Độ Thuần Thục Cản Thành Vô Địch",
  "the_loai": ["Xuyen khong", "He thong", "Vo hiep / Huyen huyen"],
  "tom_tat_cot_truyen": "Ly Xuyen xuyen qua mot vuong trieu dang trong thoi ky bap benh...",
  "the_luc_chinh": {"vo_lam": ["Cac dai Vo quan", "Danh mon chinh phai"]}
}`

// scriptedLLM trả lần lượt các phản hồi đã định sẵn, ghi lại số lượt gọi.
type scriptedLLM struct {
	responses []string
	calls     int
	lastMsgs  []agentcore.Message
}

func (s *scriptedLLM) Generate(_ context.Context, msgs []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	s.lastMsgs = msgs
	i := s.calls
	s.calls++
	if i >= len(s.responses) {
		return nil, fmt.Errorf("gọi LLM quá số lượt kịch bản (%d)", s.calls)
	}
	return &agentcore.LLMResponse{Message: agentcore.Message{
		Role:      agentcore.RoleAssistant,
		Content:   []agentcore.ContentBlock{agentcore.TextBlock(s.responses[i])},
		Timestamp: time.Now(),
	}}, nil
}

// Model phớt lờ schema ở lượt đầu -> lượt sửa phải cứu được.
func TestExtract_LuotSuaKhiModelPhotLoSchema(t *testing.T) {
	good := `{"title":"Loạn Thế Võ Đạo","brief":"## Tên truyện\nLoạn Thế Võ Đạo\n\n## Thế lực\n- Các đại Võ quán","coverage":{"mapped":["Tên truyện"],"missing":[],"notes":"ok"}}`
	llm := &scriptedLLM{responses: []string{phanHoiSaiSchemaThucTe, good}}

	res, err := Extract(context.Background(), llm, "system", "tài liệu")
	if err != nil {
		t.Fatalf("kỳ vọng lượt sửa cứu được, nhận lỗi: %v", err)
	}
	if llm.calls != 2 {
		t.Fatalf("kỳ vọng đúng 2 lượt gọi (gốc + sửa), nhận %d", llm.calls)
	}
	if !strings.Contains(res.Brief, "Các đại Võ quán") {
		t.Fatalf("brief sai: %q", res.Brief)
	}
	// Lượt sửa phải cho model thấy lại phản hồi hỏng của chính nó để gộp nội dung vào brief.
	if len(llm.lastMsgs) != 4 {
		t.Fatalf("kỳ vọng 4 message ở lượt sửa (system+user+assistant+repair), nhận %d", len(llm.lastMsgs))
	}
	if !strings.Contains(llm.lastMsgs[2].TextContent(), "ten_truyen") {
		t.Fatalf("lượt sửa phải kèm phản hồi hỏng của model, nhận: %q", llm.lastMsgs[2].TextContent())
	}
}

// Hỏng hai lượt liên tiếp thì phải báo lỗi kèm phản hồi thô, không thử lại vô hạn.
func TestExtract_HongCaHaiLuot(t *testing.T) {
	llm := &scriptedLLM{responses: []string{phanHoiSaiSchemaThucTe, phanHoiSaiSchemaThucTe}}
	_, err := Extract(context.Background(), llm, "system", "doc")
	if err == nil {
		t.Fatal("kỳ vọng lỗi khi model hỏng cả hai lượt")
	}
	if llm.calls != 2 {
		t.Fatalf("kỳ vọng dừng sau 2 lượt, nhận %d", llm.calls)
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("lỗi phải là *ParseError để ghi được raw dump, nhận %T", err)
	}
	if !strings.Contains(pe.Raw, "ten_truyen") {
		t.Fatalf("raw phải là phản hồi thô của model, nhận: %q", pe.Raw)
	}
}

// Lượt đầu đã đúng thì tuyệt đối không gọi LLM lần hai (tốn tiền vô ích).
func TestExtract_KhongGoiLuotSuaKhiLuotDauDung(t *testing.T) {
	good := `{"title":"X","brief":"## Nội dung","coverage":{"mapped":[],"missing":[],"notes":""}}`
	llm := &scriptedLLM{responses: []string{good}}
	if _, err := Extract(context.Background(), llm, "system", "doc"); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if llm.calls != 1 {
		t.Fatalf("kỳ vọng đúng 1 lượt gọi, nhận %d", llm.calls)
	}
}

func TestExtract_LLMError(t *testing.T) {
	if _, err := Extract(context.Background(), &fakeLLM{err: fmt.Errorf("boom")}, "system", "doc"); err == nil {
		t.Fatal("kỳ vọng lỗi khi LLM lỗi")
	}
}

func TestRun_FileNotFound(t *testing.T) {
	ch, err := Run(context.Background(), Deps{LLM: &fakeLLM{}, Prompt: "p"}, Options{Path: "khong-ton-tai-xyz-123.md"})
	if err != nil {
		t.Fatalf("Run không nên lỗi đồng bộ: %v", err)
	}
	var last Event
	for ev := range ch {
		last = ev
	}
	if last.Stage != StageError {
		t.Fatalf("kỳ vọng StageError, nhận %s", last.Stage)
	}
}

// Khi parse hỏng, phản hồi thô phải được ghi ra đĩa để chẩn đoán được nguyên nhân.
// Lưu ý: luôn truyền RawDumpPath vào t.TempDir(), tuyệt đối không để test đụng vào
// RawDumpPath() thật — dữ liệu giả của test sẽ bị nhầm là phản hồi thật của model.
func TestRun_GhiRawDumpKhiParseHong(t *testing.T) {
	src := filepath.Join(t.TempDir(), "truyen.md")
	if err := os.WriteFile(src, []byte("Tên truyện\nXuyên Không"), 0o600); err != nil {
		t.Fatal(err)
	}
	dump := filepath.Join(t.TempDir(), "raw.txt")

	const bad = `{"khong_phai_schema_nay":"..."}`
	ch, err := Run(context.Background(),
		Deps{LLM: &fakeLLM{response: bad}, Prompt: "p"},
		Options{Path: src, RawDumpPath: dump})
	if err != nil {
		t.Fatal(err)
	}
	var last Event
	for ev := range ch {
		last = ev
	}
	if last.Stage != StageError {
		t.Fatalf("kỳ vọng StageError, nhận %s", last.Stage)
	}
	got, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("kỳ vọng có file dump: %v", err)
	}
	if string(got) != bad {
		t.Fatalf("dump phải là phản hồi nguyên văn:\n có: %q\nmuốn: %q", got, bad)
	}
	if !strings.Contains(last.Err.Error(), dump) {
		t.Fatalf("lỗi phải chỉ ra đường dẫn dump, nhận: %v", last.Err)
	}
}

func TestRun_MissingDeps(t *testing.T) {
	if _, err := Run(context.Background(), Deps{Prompt: "p"}, Options{Path: "x.md"}); err == nil {
		t.Fatal("kỳ vọng lỗi khi thiếu LLM")
	}
	if _, err := Run(context.Background(), Deps{LLM: &fakeLLM{}}, Options{Path: "x.md"}); err == nil {
		t.Fatal("kỳ vọng lỗi khi thiếu prompt")
	}
	if _, err := Run(context.Background(), Deps{LLM: &fakeLLM{}, Prompt: "p"}, Options{}); err == nil {
		t.Fatal("kỳ vọng lỗi khi thiếu path")
	}
}
