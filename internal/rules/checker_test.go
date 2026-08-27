package rules

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// findViolation tìm vi phạm đầu tiên trong kết quả theo rule + target.
func findViolation(vs []Violation, rule, target string) *Violation {
	for i := range vs {
		if vs[i].Rule == rule && vs[i].Target == target {
			return &vs[i]
		}
	}
	return nil
}

func TestCheck_EmptyStructured(t *testing.T) {
	vs := Check("任何内容", -1, Structured{}, noCeiling())
	if vs != nil {
		t.Errorf("empty structured should return nil, got %+v", vs)
	}
}

func TestCheck_ForbiddenChars(t *testing.T) {
	text := "他笑了——又叹了口气——离去。"
	vs := Check(text, -1, Structured{
		ForbiddenChars: []string{"——"},
	}, noCeiling())
	v := findViolation(vs, "forbidden_chars", "——")
	if v == nil {
		t.Fatal("expected forbidden_chars violation")
	}
	if v.Severity != SeverityError {
		t.Errorf("severity=%s, want error", v.Severity)
	}
	if v.Actual != 2 {
		t.Errorf("actual=%v, want 2", v.Actual)
	}
}

func TestCheck_ForbiddenCharsNotPresent(t *testing.T) {
	vs := Check("普通文本无违规", -1, Structured{
		ForbiddenChars: []string{"——"},
	}, noCeiling())
	if len(vs) != 0 {
		t.Errorf("expected no violations, got %+v", vs)
	}
}

func TestCheck_ForbiddenPhrases(t *testing.T) {
	text := "不是……而是真相被掩盖了。这里探讨核心动机。"
	vs := Check(text, -1, Structured{
		ForbiddenPhrases: []string{"不是……而是", "核心动机"},
	}, noCeiling())
	if len(vs) != 2 {
		t.Errorf("expected 2 violations, got %d: %+v", len(vs), vs)
	}
	for _, v := range vs {
		if v.Severity != SeverityError {
			t.Errorf("severity=%s, want error", v.Severity)
		}
	}
}

func TestCheck_FatigueWordsUnderLimit(t *testing.T) {
	text := "他不禁笑了。"
	vs := Check(text, -1, Structured{
		FatigueWords: map[string]int{"不禁": 1},
	}, noCeiling())
	if len(vs) != 0 {
		t.Errorf("under limit should not violate, got %+v", vs)
	}
}

func TestCheck_FatigueWordsAtLimit(t *testing.T) {
	// limit=1, actual=1 → không vi phạm
	text := "他不禁笑了。"
	vs := Check(text, -1, Structured{
		FatigueWords: map[string]int{"不禁": 1},
	}, noCeiling())
	if len(vs) != 0 {
		t.Errorf("at limit should not violate (limit 1 actual 1), got %+v", vs)
	}
}

func TestCheck_FatigueWordsOverLimit(t *testing.T) {
	// limit=1, actual=3 → warning
	text := "他不禁笑了，又不禁皱眉，最后不禁离去。"
	vs := Check(text, -1, Structured{
		FatigueWords: map[string]int{"不禁": 1},
	}, noCeiling())
	v := findViolation(vs, "fatigue_words", "不禁")
	if v == nil {
		t.Fatal("expected fatigue_words violation")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("severity=%s, want warning", v.Severity)
	}
	if v.Limit != 1 {
		t.Errorf("limit=%v, want 1", v.Limit)
	}
	if v.Actual != 3 {
		t.Errorf("actual=%v, want 3", v.Actual)
	}
}

// Kiểm thử biên số từ. Sàn cứng (rng.Min), trần mềm (WordCeiling) — rng.Max KHÔNG còn là trần.
// Sàn 3000:
//   actual 3000 → đúng sàn → no violation
//   actual 2401 → deviation = 599/3000 ≈ 19.97% → warning
//   actual 2400 → deviation = 600/3000 = 20% → error (>= threshold)
// Trần mềm (median 10000 × 1.5 = 15000):
//   actual 15000 → đúng trần → no violation
//   actual 15001 → warning (không bao giờ error, dù vượt bao nhiêu)

func noCeiling() WordCeiling { return WordCeiling{} }

func TestCheck_ChapterWordsInRange(t *testing.T) {
	rng := &WordRange{Min: 3000, Max: 6000}
	vs := Check("", 4000, Structured{ChapterWords: rng}, noCeiling())
	if len(vs) != 0 {
		t.Errorf("in range should yield no violation, got %+v", vs)
	}
	// Đúng sàn
	vs = Check("", 3000, Structured{ChapterWords: rng}, noCeiling())
	if len(vs) != 0 {
		t.Errorf("at min should be in range, got %+v", vs)
	}
}

func TestCheck_ChapterWordsSlightlyBelow(t *testing.T) {
	// actual 2401 → deviation = 599/3000 = 0.1996... < 20% → warning
	rng := &WordRange{Min: 3000, Max: 6000}
	vs := Check("", 2401, Structured{ChapterWords: rng}, noCeiling())
	if len(vs) != 1 || vs[0].Rule != "chapter_words" {
		t.Fatalf("expected 1 chapter_words violation, got %+v", vs)
	}
	if vs[0].Severity != SeverityWarning {
		t.Errorf("severity=%s, want warning at <20%%", vs[0].Severity)
	}
	if vs[0].Deviation >= ChapterWordsDeviationThreshold {
		t.Errorf("deviation=%f should be < %f", vs[0].Deviation, ChapterWordsDeviationThreshold)
	}
}

func TestCheck_ChapterWordsAtThreshold(t *testing.T) {
	// actual 2400 → deviation = 600/3000 = 0.2 == 20% → error (>= threshold)
	rng := &WordRange{Min: 3000, Max: 6000}
	vs := Check("", 2400, Structured{ChapterWords: rng}, noCeiling())
	if len(vs) != 1 || vs[0].Severity != SeverityError {
		t.Errorf("expected error at 20%% threshold, got %+v", vs)
	}
}

// rng.Max không còn sinh vi phạm: đây chính là bức tường cứng đã ép writer cắt nát chương.
func TestCheck_ChapterWordsAboveMaxIsNotAViolation(t *testing.T) {
	rng := &WordRange{Min: 3000, Max: 6000}
	for _, wc := range []int{7200, 13481, 33670} {
		vs := Check("", wc, Structured{ChapterWords: rng}, noCeiling())
		if len(vs) != 0 {
			t.Errorf("wordCount=%d: rng.Max không còn là trần, muốn 0 vi phạm, got %+v", wc, vs)
		}
	}
}

// Trần mềm chỉ cảnh báo, không bao giờ error — dù vượt gấp nhiều lần.
func TestCheck_SoftCeilingNeverErrors(t *testing.T) {
	rng := &WordRange{Min: 3000, Max: 6000}
	ceiling := ComputeWordCeiling([]int{10000, 10000, 10000}) // median 10000 → soft_max 15000

	vs := Check("", 15000, Structured{ChapterWords: rng}, ceiling)
	if len(vs) != 0 {
		t.Errorf("đúng trần mềm không phải vi phạm, got %+v", vs)
	}

	for _, wc := range []int{15001, 33670} {
		vs := Check("", wc, Structured{ChapterWords: rng}, ceiling)
		if len(vs) != 1 || vs[0].Rule != "chapter_words" {
			t.Fatalf("wordCount=%d: muốn 1 vi phạm chapter_words, got %+v", wc, vs)
		}
		if vs[0].Severity != SeverityWarning {
			t.Errorf("wordCount=%d: severity=%s, vượt trần mềm luôn phải là warning", wc, vs[0].Severity)
		}
	}
}

// Chưa đủ mẫu → chưa có "độ dài tự nhiên" → không áp trần. Các chương đầu tự định hình chuẩn mực.
func TestComputeWordCeiling_ColdStart(t *testing.T) {
	for _, counts := range [][]int{nil, {12000}, {12000, 12000}} {
		if c := ComputeWordCeiling(counts); c.Active() {
			t.Errorf("samples=%d: chưa đủ mẫu thì không được áp trần, got soft_max=%d", len(counts), c.SoftMax)
		}
	}
}

// Trần bám theo văn phong hiện tại: chỉ CeilingSampleSize chương gần nhất được tính.
func TestComputeWordCeiling_UsesRecentWindowOnly(t *testing.T) {
	counts := make([]int, 0, 30)
	for i := 0; i < 20; i++ {
		counts = append(counts, 3000) // các chương mở đầu ngắn, phải bị bỏ ra ngoài cửa sổ
	}
	for i := 0; i < CeilingSampleSize; i++ {
		counts = append(counts, 12000)
	}
	c := ComputeWordCeiling(counts)
	if c.Median != 12000 {
		t.Errorf("median=%d, want 12000 (chỉ %d chương gần nhất)", c.Median, CeilingSampleSize)
	}
	if c.SoftMax != 18000 {
		t.Errorf("soft_max=%d, want 18000", c.SoftMax)
	}
}

// Trung vị chứ không phải trung bình: một chương dị thường không được tự nâng trần cho mình.
func TestComputeWordCeiling_OutlierDoesNotRaiseCeiling(t *testing.T) {
	c := ComputeWordCeiling([]int{10000, 10000, 10000, 10000, 100000})
	if c.Median != 10000 {
		t.Errorf("median=%d, want 10000 — chương 100k không được kéo trần lên", c.Median)
	}
}

func TestCheck_AutoWordCount(t *testing.T) {
	// Khi wordCount = -1, checker tự tính số từ
	text := strings.Repeat("汉", 2500) // 2500 ký tự Hán
	rng := &WordRange{Min: 3000, Max: 6000}
	vs := Check(text, -1, Structured{ChapterWords: rng}, noCeiling())
	if len(vs) != 1 || vs[0].Rule != "chapter_words" {
		t.Fatalf("expected 1 chapter_words violation, got %+v", vs)
	}
	if vs[0].Actual != 2500 {
		t.Errorf("auto wordCount=%v, want 2500", vs[0].Actual)
	}
	if vs[0].Actual != utf8.RuneCountInString(text) {
		t.Errorf("auto count mismatch: %v vs rune count %d", vs[0].Actual, utf8.RuneCountInString(text))
	}
}

func TestCheck_MultipleRulesAtOnce(t *testing.T) {
	text := "他不禁——又不禁——离去。"
	rng := &WordRange{Min: 3000, Max: 6000}
	s := Structured{
		ChapterWords:   rng,
		ForbiddenChars: []string{"——"},
		FatigueWords:   map[string]int{"不禁": 1},
	}
	vs := Check(text, 10, s, noCeiling()) // 10 từ → hụt sàn 3000

	// Nên đồng thời kích hoạt ba loại: forbidden_chars + fatigue_words + chapter_words
	rules := map[string]bool{}
	for _, v := range vs {
		rules[v.Rule] = true
	}
	if !rules["forbidden_chars"] || !rules["fatigue_words"] || !rules["chapter_words"] {
		t.Errorf("expected all three rules triggered, got %+v", rules)
	}
}

func TestCheck_FatigueZeroLimitSkipped(t *testing.T) {
	// limit=0 là giá trị không hợp lệ, nên bỏ qua toàn bộ quy tắc (parser cũng lọc, đây là phòng thủ)
	text := "不禁不禁不禁"
	vs := Check(text, -1, Structured{
		FatigueWords: map[string]int{"不禁": 0},
	}, noCeiling())
	if len(vs) != 0 {
		t.Errorf("limit=0 should be skipped, got %+v", vs)
	}
}

func TestCheck_EmptyTargetsSkipped(t *testing.T) {
	// Mục tiêu chuỗi rỗng không được tạo ra false positive
	vs := Check("任何文本", -1, Structured{
		ForbiddenChars:   []string{""},
		ForbiddenPhrases: []string{""},
		FatigueWords:     map[string]int{"": 1},
	}, noCeiling())
	if len(vs) != 0 {
		t.Errorf("empty targets should be skipped, got %+v", vs)
	}
}
