package host

import (
	"strings"
	"testing"
)

// feedAll nạp toàn bộ input một lần, trả về output tích lũy.
func feedAll(t *testing.T, tool, input string) string {
	t.Helper()
	e := newToolExtractor(tool)
	if e == nil {
		t.Fatalf("no extractor for tool %q", tool)
	}
	return e.Feed(input)
}

// feedChunked nạp input theo từng mảnh kích thước chỉ định, xác minh kết quả streaming khớp với nạp một lần.
func feedChunked(t *testing.T, tool, input string, chunk int) string {
	t.Helper()
	e := newToolExtractor(tool)
	if e == nil {
		t.Fatalf("no extractor for tool %q", tool)
	}
	var b strings.Builder
	for i := 0; i < len(input); i += chunk {
		end := min(i+chunk, len(input))
		b.WriteString(e.Feed(input[i:end]))
	}
	return b.String()
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("expected substring %q in:\n---\n%s\n---", want, got)
	}
}

func mustNotContain(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Errorf("unexpected substring %q in:\n---\n%s\n---", want, got)
	}
}

// ── Chế độ chung: obj phẳng ──

func TestExtract_PlanChapter(t *testing.T) {
	in := `{"chapter":1,"title":"卖身契","goal":"建立矿场基线","conflict":"父债","hook":"灰矿","emotion_arc":"压抑"}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "✻ Lên kế hoạch")
	mustContain(t, out, "chapter: 1")
	mustContain(t, out, "title: 卖身契")
	mustContain(t, out, "goal: 建立矿场基线")
	mustContain(t, out, "conflict: 父债")
	mustContain(t, out, "hook: 灰矿")
	mustContain(t, out, "emotion_arc: 压抑")
}

// ── Chế độ chung: obj lồng nhau + mảng ──

func TestExtract_FoundationCharacters(t *testing.T) {
	in := `{"type":"characters","scale":"long","content":[` +
		`{"name":"沈砺","role":"主角","aliases":["灰脉","沈七"],"description":"边荒少年。","traits":["克制","多疑"]},` +
		`{"name":"顾小灯","role":"重要配角","description":"药坊试药童女。"}` +
		`]}`
	out := feedAll(t, "save_foundation", in)
	mustContain(t, out, "✻ Cài đặt")
	mustContain(t, out, "type: characters")
	mustContain(t, out, "scale: long")
	// Render chung: hiển thị tất cả các trường, kể cả aliases / traits trước đây bị whitelist bỏ qua
	mustContain(t, out, "name: 沈砺")
	mustContain(t, out, "role: 主角")
	mustContain(t, out, "aliases:")
	mustContain(t, out, "- 灰脉")
	mustContain(t, out, "- 沈七")
	mustContain(t, out, "description: 边荒少年。")
	mustContain(t, out, "traits:")
	mustContain(t, out, "- 克制")
	mustContain(t, out, "- 多疑")
	mustContain(t, out, "name: 顾小灯")
	mustContain(t, out, "role: 重要配角")
}

func TestExtract_FoundationLayeredOutline(t *testing.T) {
	in := `{"type":"layered_outline","content":[` +
		`{"index":1,"title":"矿火微明","arcs":[` +
		`{"index":1,"title":"乌鳞矿役","goal":"求活","chapters":[` +
		`{"chapter":1,"title":"卖身契","core_event":"被卖入矿场。"}` +
		`]}]}]}`
	out := feedAll(t, "save_foundation", in)
	mustContain(t, out, "type: layered_outline")
	// Cuốn
	mustContain(t, out, "index: 1")
	mustContain(t, out, "title: 矿火微明")
	// Cung truyện
	mustContain(t, out, "title: 乌鳞矿役")
	mustContain(t, out, "goal: 求活")
	// Chương
	mustContain(t, out, "chapter: 1")
	mustContain(t, out, "title: 卖身契")
	mustContain(t, out, "core_event: 被卖入矿场。")
	// Thụt lề lồng nhau thể hiện cấp độ phân cấp
	mustContain(t, out, "arcs:\n")
	mustContain(t, out, "chapters:\n")
}

func TestExtract_FoundationUpdateCompass(t *testing.T) {
	in := `{"type":"update_compass","content":{"ending_direction":"独自飞升 vs 切断血祭","open_threads":["灰脉钥匙","活人票账簿"],"estimated_scale":"5-6 卷"}}`
	out := feedAll(t, "save_foundation", in)
	mustContain(t, out, "type: update_compass")
	mustContain(t, out, "ending_direction: 独自飞升 vs 切断血祭")
	mustContain(t, out, "estimated_scale: 5-6 卷")
	mustContain(t, out, "open_threads:")
	mustContain(t, out, "- 灰脉钥匙")
	mustContain(t, out, "- 活人票账簿")
}

// ── save_review: chứa mảng object + mảng số ──

func TestExtract_SaveReview(t *testing.T) {
	in := `{"chapter":3,"scope":"chapter","verdict":"polish","summary":"节奏略慢。","dimensions":[{"dimension":"hook","score":55,"verdict":"fail"}],"issues":[{"type":"hook","severity":"error","description":"章末缺钩子。"}],"affected_chapters":[3,4]}`
	out := feedAll(t, "save_review", in)
	mustContain(t, out, "✻ Đánh giá")
	mustContain(t, out, "verdict: polish")
	mustContain(t, out, "summary: 节奏略慢。")
	mustContain(t, out, "dimension: hook")
	mustContain(t, out, "score: 55")
	mustContain(t, out, "verdict: fail")
	mustContain(t, out, "type: hook")
	mustContain(t, out, "severity: error")
	mustContain(t, out, "description: 章末缺钩子。")
	mustContain(t, out, "- 3")
	mustContain(t, out, "- 4")
}

// ── commit_chapter: lồng nhau phức tạp ──

func TestExtract_CommitChapter(t *testing.T) {
	in := `{"chapter":1,"summary":"被卖入矿场。","characters":["沈砺","母亲"],"key_events":["签卖身契"],"foreshadow_updates":[{"id":"f1","action":"plant","description":"灰矿发烫。"}],"state_changes":[{"entity":"沈砺","field":"身份","old_value":"采药少年","new_value":"矿场杂役"}]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "✻ Lưu chương")
	mustContain(t, out, "summary: 被卖入矿场。")
	mustContain(t, out, "- 沈砺")
	mustContain(t, out, "- 母亲")
	mustContain(t, out, "- 签卖身契")
	mustContain(t, out, "id: f1")
	mustContain(t, out, "action: plant")
	mustContain(t, out, "description: 灰矿发烫。")
	mustContain(t, out, "entity: 沈砺")
	mustContain(t, out, "field: 身份")
	mustContain(t, out, "old_value: 采药少年")
	mustContain(t, out, "new_value: 矿场杂役")
}

// ── edit_chapter: chế độ chung + string nhiều dòng ──

func TestExtract_EditChapter(t *testing.T) {
	in := `{"chapter":24,"old_string":"沈砺低头不语。\n他攥紧了拳头。","new_string":"沈砺没有抬头，喉结滚动一下。\n指节攥得发白。","replace_all":false}`
	out := feedAll(t, "edit_chapter", in)
	mustContain(t, out, "✻ Chỉnh sửa")
	mustContain(t, out, "chapter: 24")
	mustContain(t, out, "old_string: 沈砺低头不语。\n他攥紧了拳头。")
	mustContain(t, out, "new_string: 沈砺没有抬头，喉结滚动一下。\n指节攥得发白。")
	mustContain(t, out, "replace_all: false")
}

// ── Công cụ đọc: mật độ thông tin args thấp nhưng header + trường quan trọng vẫn phải hiển thị ──

func TestExtract_ReadChapter(t *testing.T) {
	in := `{"chapter":234,"source":"final"}`
	out := feedAll(t, "read_chapter", in)
	mustContain(t, out, "✻ Đọc chương")
	mustContain(t, out, "chapter: 234")
	mustContain(t, out, "source: final")
}

func TestExtract_CheckConsistency(t *testing.T) {
	out := feedAll(t, "check_consistency", `{"chapter":234}`)
	mustContain(t, out, "✻ Kiểm tra nhất quán")
	mustContain(t, out, "chapter: 234")
}

// Xử lý fallback args rỗng: khi coordinator gọi novel_context không truyền tham số thì args là {},
// không được hoàn toàn im lặng, phải xuất ra ít nhất header để người dùng nhận biết lời gọi.
func TestExtract_NovelContextEmptyArgs(t *testing.T) {
	out := feedAll(t, "novel_context", `{}`)
	mustContain(t, out, "✻ Truy vấn ngữ cảnh")
}

func TestExtract_NovelContextWithChapter(t *testing.T) {
	out := feedAll(t, "novel_context", `{"chapter":234}`)
	mustContain(t, out, "✻ Truy vấn ngữ cảnh")
	mustContain(t, out, "chapter: 234")
}

// ── Chế độ luồng trực tiếp ──

func TestExtract_DraftChapterRawMarkdown(t *testing.T) {
	in := `{"chapter":1,"content":"# 第一章\n\n沈砺站在矿口。\n"}`
	out := feedAll(t, "draft_chapter", in)
	// Luồng trực tiếp: không trang trí, không tiền tố key
	mustNotContain(t, out, "【")
	mustNotContain(t, out, "content:")
	mustNotContain(t, out, "chapter:")
	mustContain(t, out, "# 第一章")
	mustContain(t, out, "沈砺站在矿口。")
}

func TestExtract_DraftChapterIgnoresOtherFields(t *testing.T) {
	// Các trường ngoài content phải được bỏ qua im lặng, không làm bẩn output
	in := `{"chapter":7,"summary":"meta","content":"正文","extra_array":[1,2,3]}`
	out := feedAll(t, "draft_chapter", in)
	mustContain(t, out, "正文")
	mustNotContain(t, out, "meta")
	mustNotContain(t, out, "summary")
	mustNotContain(t, out, "7")
	mustNotContain(t, out, "1")
}

// ── Bất biến hành vi ──

func TestExtract_UnknownTool(t *testing.T) {
	if e := newToolExtractor("nonexistent_tool"); e != nil {
		t.Errorf("expected nil for unknown tool")
	}
}

func TestExtract_DoneAfterClose(t *testing.T) {
	e := newToolExtractor("plan_chapter")
	e.Feed(`{"title":"x"}`)
	if !e.Done() {
		t.Error("expected Done after closing brace")
	}
}

// ── Bất biến phân mảnh streaming ──

// Cùng một input phân mảnh theo 1/3/7/13 byte, output phải hoàn toàn giống với nạp một lần.
func TestExtract_ChunkedEqualsWhole(t *testing.T) {
	cases := []struct {
		tool  string
		input string
	}{
		{"plan_chapter", `{"title":"卖身契","goal":"目标","conflict":"父债","hook":"灰矿","emotion_arc":"压抑"}`},
		{"save_foundation", `{"type":"characters","content":[{"name":"沈砺","role":"主角","aliases":["灰脉","沈七"]}]}`},
		{"save_foundation", `{"type":"layered_outline","content":[{"index":1,"title":"矿火","arcs":[{"index":1,"title":"矿役","goal":"求活","chapters":[{"chapter":1,"title":"卖身契"}]}]}]}`},
		{"save_review", `{"verdict":"accept","summary":"good","dimensions":[{"dimension":"hook","score":85,"verdict":"pass"}],"issues":[]}`},
		{"draft_chapter", `{"chapter":1,"content":"# 第一章\n\n正文。\n"}`},
	}
	for _, tc := range cases {
		whole := feedAll(t, tc.tool, tc.input)
		for _, chunk := range []int{1, 3, 7, 13} {
			got := feedChunked(t, tc.tool, tc.input, chunk)
			if got != whole {
				t.Errorf("tool=%s chunk=%d differs from whole\n--- whole ---\n%s\n--- chunked ---\n%s", tc.tool, chunk, whole, got)
			}
		}
	}
}

// ── Escape và Unicode ──

func TestExtract_EscapeSequences(t *testing.T) {
	in := `{"goal":"行1\n行2 \"引号\" \\反斜线 中字"}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "行1\n行2")
	mustContain(t, out, `"引号"`)
	mustContain(t, out, `\反斜线`)
	mustContain(t, out, "中字")
}

func TestExtract_UnicodeEscape(t *testing.T) {
	// 中 = 中 (Unicode escape cho ký tự tiếng Trung)
	in := `{"goal":"中文"}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "中文")
}

// ── Container rỗng / cấu trúc đơn giản ──

func TestExtract_EmptyArrays(t *testing.T) {
	in := `{"key_events":[],"characters":["沈砺"]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "key_events:")
	mustContain(t, out, "characters:")
	mustContain(t, out, "- 沈砺")
}

func TestExtract_BoolAndNull(t *testing.T) {
	in := `{"foreshadow_updates":[{"id":"f1","action":"plant","description":null}],"chapter":1,"summary":"x","characters":["a"],"key_events":["b"]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "id: f1")
	mustContain(t, out, "action: plant")
	mustContain(t, out, "description: null")
}

// ── Trường hợp biên: mảng lồng mảng, lồng sâu ──

func TestExtract_NestedArrays(t *testing.T) {
	// affected_chapters là mảng int; ở đây thay bằng mảng lồng mảng để xác minh
	in := `{"summary":"x","key_events":[],"characters":["a"],"foreshadow_updates":[],"relationship_changes":[]}`
	out := feedAll(t, "commit_chapter", in)
	mustContain(t, out, "summary: x")
	mustContain(t, out, "key_events:")
	mustContain(t, out, "- a")
}

func TestExtract_DeeplyNested(t *testing.T) {
	in := `{"a":{"b":{"c":{"d":"deep"}}}}`
	e := newToolExtractor("plan_chapter")
	out := e.Feed(in)
	mustContain(t, out, "a:")
	mustContain(t, out, "b:")
	mustContain(t, out, "c:")
	mustContain(t, out, "d: deep")
	if !e.Done() {
		t.Error("expected Done after final closing brace")
	}
}

// ── chunk cắt giữa byte của ký tự UTF-8 đa byte ──

func TestExtract_ChunkSplitInUTF8(t *testing.T) {
	// "中" là 3 byte (E4 B8 AD). Đặt kích thước mảnh là 1 để đảm bảo mỗi byte được nạp riêng lẻ.
	in := `{"goal":"中文测试"}`
	whole := feedAll(t, "plan_chapter", in)
	chunked := feedChunked(t, "plan_chapter", in, 1)
	if whole != chunked {
		t.Errorf("byte-by-byte chunked output differs from whole:\n--- whole ---\n%s\n--- chunked ---\n%s", whole, chunked)
	}
	mustContain(t, chunked, "中文测试")
}

// ── Chế độ luồng trực tiếp: key trùng tên trong obj lồng nhau không được nhận nhầm ──

func TestExtract_NakedKeyOnlyTopLevel(t *testing.T) {
	// "content" xuất hiện hai nơi: trong obj lồng nhau + cấp cao nhất. Chỉ cái ở cấp cao nhất mới được stream ra.
	in := `{"meta":{"content":"嵌套不应输出"},"content":"顶层应输出"}`
	out := feedAll(t, "draft_chapter", in)
	mustContain(t, out, "顶层应输出")
	mustNotContain(t, out, "嵌套不应输出")
}

// ── Chế độ luồng trực tiếp: khi content không phải string thì bỏ qua hoàn toàn ──

func TestExtract_NakedKeyNonStringValue(t *testing.T) {
	// content bị viết nhầm thành object (không nên xảy ra nhưng phải chịu đựng được)
	in := `{"content":{"unexpected":true}}`
	out := feedAll(t, "draft_chapter", in)
	if out != "" {
		t.Errorf("expected empty output, got: %q", out)
	}
}

// ── Sau khi cấp cao nhất đóng, Feed tiếp theo không sản sinh output ──

func TestExtract_FeedAfterDone(t *testing.T) {
	e := newToolExtractor("plan_chapter")
	e.Feed(`{"title":"x"}`)
	if !e.Done() {
		t.Fatal("expected Done")
	}
	if got := e.Feed(`junk`); got != "" {
		t.Errorf("expected empty output after Done, got: %q", got)
	}
}

// ── chunk rỗng / input rỗng ──

func TestExtract_EmptyFeed(t *testing.T) {
	e := newToolExtractor("plan_chapter")
	if got := e.Feed(""); got != "" {
		t.Errorf("expected empty output for empty feed, got: %q", got)
	}
	if e.Done() {
		t.Error("Done should be false before any input")
	}
}

// ── Mảng lồng trực tiếp trong mảng (không qua obj) ──

func TestExtract_ArrayOfArrays(t *testing.T) {
	in := `{"matrix":[[1,2],[3,4]]}`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "matrix:")
	mustContain(t, out, "- 1")
	mustContain(t, out, "- 2")
	mustContain(t, out, "- 3")
	mustContain(t, out, "- 4")
}

// ── Số theo sau là khoảng trắng rồi dấu phân cách ──

func TestExtract_NumberWithTrailingSpace(t *testing.T) {
	// "chapter": 1 ,  ← số có nhiều khoảng trắng trước và sau
	in := `{ "chapter" : 1 , "title" : "x" }`
	out := feedAll(t, "plan_chapter", in)
	mustContain(t, out, "chapter: 1")
	mustContain(t, out, "title: x")
}
