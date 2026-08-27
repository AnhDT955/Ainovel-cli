package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// readFixture đọc file fixture trong thư mục testdata; không tìm thấy thì t.Fatal.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParse_Basic(t *testing.T) {
	data := readFixture(t, "basic.rules.md")
	p := Parse("testdata/basic.rules.md", SourceProject, data)

	if p.Structured.Genre != "xianxia" {
		t.Errorf("genre=%q, want xianxia", p.Structured.Genre)
	}
	if p.Structured.ChapterWords == nil || p.Structured.ChapterWords.Min != 3000 || p.Structured.ChapterWords.Max != 6000 {
		t.Errorf("chapter_words=%+v, want {3000,6000}", p.Structured.ChapterWords)
	}
	wantChars := []string{"——", "（"}
	if !reflect.DeepEqual(p.Structured.ForbiddenChars, wantChars) {
		t.Errorf("forbidden_chars=%v, want %v", p.Structured.ForbiddenChars, wantChars)
	}
	wantPhrases := []string{"不是……而是", "核心动机"}
	if !reflect.DeepEqual(p.Structured.ForbiddenPhrases, wantPhrases) {
		t.Errorf("forbidden_phrases=%v, want %v", p.Structured.ForbiddenPhrases, wantPhrases)
	}
	wantFatigue := map[string]int{"不禁": 1, "竟然": 1, "仿佛": 2}
	if !reflect.DeepEqual(p.Structured.FatigueWords, wantFatigue) {
		t.Errorf("fatigue_words=%v, want %v", p.Structured.FatigueWords, wantFatigue)
	}
	if !strings.Contains(p.Preference, "# 风格") {
		t.Errorf("preference missing markdown body, got %q", p.Preference)
	}
	if len(p.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %d: %+v", len(p.Conflicts), p.Conflicts)
	}
}

func TestParse_InvalidFrontMatter(t *testing.T) {
	data := readFixture(t, "invalid-frontmatter.rules.md")
	p := Parse("testdata/invalid-frontmatter.rules.md", SourceProject, data)

	// Chịu lỗi: tất cả trường cấu trúc đều rỗng, nhưng phần thân vẫn được dùng làm tùy chọn
	if !p.Structured.IsEmpty() {
		t.Errorf("structured should be empty on parse_error, got %+v", p.Structured)
	}
	if !strings.Contains(p.Preference, "正文应当仍作为偏好注入") {
		t.Errorf("preference should still be parsed despite front matter failure; got %q", p.Preference)
	}
	if len(p.Conflicts) == 0 {
		t.Fatal("expected at least one conflict for parse error")
	}
	found := false
	for _, c := range p.Conflicts {
		if c.Kind == ConflictParseError {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ConflictParseError, got %+v", p.Conflicts)
	}
}

func TestParse_UnknownFields(t *testing.T) {
	data := readFixture(t, "unknown-fields.rules.md")
	p := Parse("testdata/unknown-fields.rules.md", SourceProject, data)

	// Các trường đã biết nên được tải bình thường
	if p.Structured.Genre != "xianxia" {
		t.Errorf("genre=%q, want xianxia", p.Structured.Genre)
	}
	if p.Structured.ChapterWords == nil || p.Structured.ChapterWords.Min != 2000 {
		t.Errorf("chapter_words=%+v, want {2000,4000}", p.Structured.ChapterWords)
	}
	// Các trường không xác định đưa vào conflicts
	unknowns := map[string]bool{}
	for _, c := range p.Conflicts {
		if c.Kind == ConflictUnknownField {
			unknowns[c.Field] = true
		}
	}
	if !unknowns["forbidden_emojis"] || !unknowns["secret_field"] {
		t.Errorf("expected forbidden_emojis & secret_field in conflicts, got %+v", p.Conflicts)
	}
}

func TestParse_TypeErrors(t *testing.T) {
	data := readFixture(t, "type-errors.rules.md")
	p := Parse("testdata/type-errors.rules.md", SourceProject, data)

	// Chính sách nghiêm ngặt (Debug-First): mọi trường sai kiểu đều bị loại bỏ và ghi vào conflicts, không "đoán thay người dùng".

	// genre: 42 là int, phán định nghiêm ngặt là type_error → loại bỏ
	if p.Structured.Genre != "" {
		t.Errorf("genre should be discarded on type error (int instead of string), got %q", p.Structured.Genre)
	}

	// chapter_words: "not-a-range" → invalid_value
	if p.Structured.ChapterWords != nil {
		t.Errorf("chapter_words should be nil on invalid_value, got %+v", p.Structured.ChapterWords)
	}

	// forbidden_chars: "should-be-list" → type_error (cấp cao nhất)
	if len(p.Structured.ForbiddenChars) != 0 {
		t.Errorf("forbidden_chars should be empty on type_error, got %v", p.Structured.ForbiddenChars)
	}

	// forbidden_phrases: [1, 2] → mỗi phần tử đều là int, phán định nghiêm ngặt → loại bỏ tất cả → list rỗng
	if len(p.Structured.ForbiddenPhrases) != 0 {
		t.Errorf("forbidden_phrases should be empty on element type errors, got %v", p.Structured.ForbiddenPhrases)
	}

	// fatigue_words: true → không phải map cũng không phải list → type_error, loại bỏ toàn bộ
	if len(p.Structured.FatigueWords) != 0 {
		t.Errorf("fatigue_words should be empty on type_error, got %+v", p.Structured.FatigueWords)
	}

	// Tất cả các trường lỗi đều phải đưa vào conflicts
	fields := map[string]bool{}
	for _, c := range p.Conflicts {
		fields[c.Field] = true
	}
	expected := []string{"genre", "chapter_words", "forbidden_chars", "fatigue_words"}
	for _, f := range expected {
		if !fields[f] {
			t.Errorf("expected conflict for %s, got fields=%v conflicts=%+v", f, fields, p.Conflicts)
		}
	}
	// Xung đột cấp phần tử của forbidden_phrases, tên trường là forbidden_phrases[0]/[1]
	hasPhrasesElement := false
	for _, c := range p.Conflicts {
		if strings.HasPrefix(c.Field, "forbidden_phrases") {
			hasPhrasesElement = true
		}
	}
	if !hasPhrasesElement {
		t.Errorf("expected per-element conflict on forbidden_phrases, got %+v", p.Conflicts)
	}

	// Phần thân vẫn phải được đưa vào
	if !strings.Contains(p.Preference, "类型错误") {
		t.Errorf("preference should be parsed despite type errors; got %q", p.Preference)
	}
}

// TestParse_FatigueWordsPartialInvalid kiểm tra: khi một số key trong map fatigue_words có ngưỡng không hợp lệ,
// mỗi key không hợp lệ đều ghi một conflict, các key hợp lệ vẫn được giữ nguyên.
func TestParse_FatigueWordsPartialInvalid(t *testing.T) {
	content := []byte("---\nfatigue_words:\n" +
		"  正常: 2\n" +
		"  零阈值: 0\n" +
		"  负阈值: -1\n" +
		"  非整数: \"abc\"\n" +
		"---\n")
	p := Parse("inline", SourceProject, content)

	if v, ok := p.Structured.FatigueWords["正常"]; !ok || v != 2 {
		t.Errorf("legitimate key should be kept, got %v", p.Structured.FatigueWords)
	}
	if _, ok := p.Structured.FatigueWords["零阈值"]; ok {
		t.Errorf("zero threshold should be dropped")
	}
	if _, ok := p.Structured.FatigueWords["负阈值"]; ok {
		t.Errorf("negative threshold should be dropped")
	}
	if _, ok := p.Structured.FatigueWords["非整数"]; ok {
		t.Errorf("non-int threshold should be dropped")
	}

	// Mỗi key không hợp lệ đều phải có conflict riêng
	keys := map[string]bool{}
	for _, c := range p.Conflicts {
		keys[c.Field] = true
	}
	for _, key := range []string{"fatigue_words.零阈值", "fatigue_words.负阈值", "fatigue_words.非整数"} {
		if !keys[key] {
			t.Errorf("expected conflict on %s, got fields=%v", key, keys)
		}
	}
}

func TestParse_Empty(t *testing.T) {
	p := Parse("testdata/empty.rules.md", SourceProject, []byte{})
	if !p.Structured.IsEmpty() {
		t.Errorf("empty file should yield empty structured, got %+v", p.Structured)
	}
	if p.Preference != "" {
		t.Errorf("empty file should yield empty preference, got %q", p.Preference)
	}
	if len(p.Conflicts) != 0 {
		t.Errorf("empty file should yield no conflicts, got %+v", p.Conflicts)
	}
}

func TestParse_FatigueAsList(t *testing.T) {
	data := readFixture(t, "fatigue-as-list.rules.md")
	p := Parse("testdata/fatigue-as-list.rules.md", SourceProject, data)

	want := map[string]int{"不禁": 1, "竟然": 1, "仿佛": 1}
	if !reflect.DeepEqual(p.Structured.FatigueWords, want) {
		t.Errorf("fatigue_words=%v, want %v", p.Structured.FatigueWords, want)
	}
	if len(p.Conflicts) != 0 {
		t.Errorf("list form should be accepted without conflict, got %+v", p.Conflicts)
	}
}

func TestParse_NoFrontMatter(t *testing.T) {
	data := readFixture(t, "no-frontmatter.rules.md")
	p := Parse("testdata/no-frontmatter.rules.md", SourceProject, data)

	if !p.Structured.IsEmpty() {
		t.Errorf("no front matter, structured should be empty, got %+v", p.Structured)
	}
	if !strings.Contains(p.Preference, "仅有正文") {
		t.Errorf("preference should contain body, got %q", p.Preference)
	}
	if len(p.Conflicts) != 0 {
		t.Errorf("no front matter, no conflicts expected, got %+v", p.Conflicts)
	}
}

// TestParse_ChapterWordsObjectForm kiểm tra chapter_words tương thích với dạng map {min, max} (ngoài chuỗi).
func TestParse_ChapterWordsObjectForm(t *testing.T) {
	content := []byte("---\nchapter_words:\n  min: 2500\n  max: 5500\n---\n")
	p := Parse("inline", SourceProject, content)
	if p.Structured.ChapterWords == nil {
		t.Fatal("expected ChapterWords to be set")
	}
	if p.Structured.ChapterWords.Min != 2500 || p.Structured.ChapterWords.Max != 5500 {
		t.Errorf("got %+v, want {2500, 5500}", p.Structured.ChapterWords)
	}
}

// TestParse_ChapterWordsSingleValue kiểm tra cách viết giá trị đơn (số nguyên / chuỗi) được mở rộng thành khoảng ±20%.
// Chống hồi quy issue #41: người dùng viết giá trị đơn theo trực giác, trước đây bị bỏ qua lặng lẽ và rơi về giá trị mặc định nội tại.
func TestParse_ChapterWordsSingleValue(t *testing.T) {
	cases := []struct {
		name             string
		content          string
		wantMin, wantMax int
	}{
		{"bare int", "---\nchapter_words: 2500\n---\n", 2000, 3000},
		{"quoted string", "---\nchapter_words: \"2500\"\n---\n", 2000, 3000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Parse("inline", SourceProject, []byte(tc.content))
			if p.Structured.ChapterWords == nil {
				t.Fatalf("expected ChapterWords to be set, conflicts=%+v", p.Conflicts)
			}
			if p.Structured.ChapterWords.Min != tc.wantMin || p.Structured.ChapterWords.Max != tc.wantMax {
				t.Errorf("got %+v, want {%d, %d}", p.Structured.ChapterWords, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// TestParse_ChapterWordsInvalidRange xác nhận min>max được coi là giá trị không hợp lệ.
func TestParse_ChapterWordsInvalidRange(t *testing.T) {
	content := []byte("---\nchapter_words: 6000-3000\n---\n")
	p := Parse("inline", SourceProject, content)
	if p.Structured.ChapterWords != nil {
		t.Errorf("min>max should be rejected, got %+v", p.Structured.ChapterWords)
	}
	found := false
	for _, c := range p.Conflicts {
		if c.Kind == ConflictInvalidValue && c.Field == "chapter_words" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ConflictInvalidValue for chapter_words, got %+v", p.Conflicts)
	}
}

// TestParse_FrontMatterUnclosed xác nhận khi dấu --- mở đầu không được đóng lại, toàn bộ nội dung được xử lý như phần thân.
func TestParse_FrontMatterUnclosed(t *testing.T) {
	content := []byte("---\ngenre: xianxia\n# 没有闭合的 --- \n\n# 偏好\n\n- 内容\n")
	p := Parse("inline", SourceProject, content)
	if !p.Structured.IsEmpty() {
		t.Errorf("unclosed front matter, structured should be empty, got %+v", p.Structured)
	}
	if !strings.Contains(p.Preference, "genre: xianxia") {
		t.Errorf("unclosed: whole content should be body, got %q", p.Preference)
	}
}
