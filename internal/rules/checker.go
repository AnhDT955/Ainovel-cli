package rules

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Check thực hiện kiểm tra cơ học nội dung chương theo các quy tắc có cấu trúc, trả về danh sách vi phạm thực tế.
//
// Hợp đồng thiết kế:
//   - Chỉ trả về sự thật, không ra lệnh (nguyên tắc sắt)
//   - Không chặn bất kỳ luồng gọi nào
//   - severity được ánh xạ cố định theo loại quy tắc (xem bảng chú thích trong types.go)
//
// Tham số:
//   - text: nội dung chương (bản cuối hoặc bản nháp đều được)
//   - wordCount: số từ của chương (đếm theo rune). Nếu <0, checker tự tính để tránh caller quét O(n) lặp lại.
//   - s: quy tắc có cấu trúc đã hợp nhất; nếu IsEmpty thì trả về nil luôn.
//   - ceiling: trần mềm thích ứng đo từ các chương đã viết (xem ceiling.go). Truyền WordCeiling{}
//     nghĩa là chưa đủ mẫu — khi đó chương dài bao nhiêu cũng không bị nêu.
func Check(text string, wordCount int, s Structured, ceiling WordCeiling) []Violation {
	if s.IsEmpty() && !ceiling.Active() {
		return nil
	}
	if wordCount < 0 {
		wordCount = utf8.RuneCountInString(text)
	}

	var violations []Violation
	violations = appendForbiddenChars(violations, text, s.ForbiddenChars)
	violations = appendForbiddenPhrases(violations, text, s.ForbiddenPhrases)
	violations = appendFatigueWords(violations, text, s.FatigueWords)
	violations = appendChapterWords(violations, wordCount, s.ChapterWords, ceiling)
	return violations
}

// forbidden_chars: xuất hiện ≥1 lần là error.
// Mỗi quy tắc chỉ tạo một violation, actual là số lần xuất hiện.
func appendForbiddenChars(vs []Violation, text string, list []string) []Violation {
	for _, ch := range list {
		if ch == "" {
			continue
		}
		n := strings.Count(text, ch)
		if n == 0 {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "forbidden_chars",
			Target:   ch,
			Actual:   n,
			Severity: SeverityError,
		})
	}
	return vs
}

// forbidden_phrases: xuất hiện ≥1 lần là error; hành vi giống forbidden_chars, chỉ khác tên rule.
func appendForbiddenPhrases(vs []Violation, text string, list []string) []Violation {
	for _, ph := range list {
		if ph == "" {
			continue
		}
		n := strings.Count(text, ph)
		if n == 0 {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "forbidden_phrases",
			Target:   ph,
			Actual:   n,
			Severity: SeverityError,
		})
	}
	return vs
}

// fatigue_words: vi phạm khi số lần xuất hiện trong chương vượt ngưỡng, mức warning.
// Không tích lũy qua nhiều chương — vấn đề liên chương sẽ xử lý sau bằng công cụ chẩn đoán.
func appendFatigueWords(vs []Violation, text string, m map[string]int) []Violation {
	for word, limit := range m {
		if word == "" || limit <= 0 {
			continue
		}
		n := strings.Count(text, word)
		if n <= limit {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "fatigue_words",
			Target:   word,
			Limit:    limit,
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	return vs
}

// chapter_words: sàn cứng, trần mềm.
//
// SÀN (rng.Min) giữ nguyên hành vi cũ — lệch <20% warning, ≥20% error. Chương hụt từ là
// triệu chứng thật (writer bị cụt lượt giữa chừng, hoặc bỏ bớt mạch truyện), cần nêu thành lỗi.
//
// TRẦN không còn lấy từ rng.Max. Trần cố định sinh error trên gần như mọi chương khi văn phong
// thực tế dài hơn con số trong rules, mà editor ánh xạ error → đánh bóng/viết lại, nên chương
// bị ép cắt ngắn và mất ngữ nghĩa vốn có. Nay trần đo từ chính các chương draft_chapter đã viết
// (ceiling), và luôn chỉ ở mức warning — thừa từ không bao giờ tự nó khởi động vòng viết lại.
// rng.Max vẫn được giữ trong rules và vẫn tới tay architect như tham số thiết kế mật độ đề cương
// (xem architect-long.md §"ngân sách số từ"), chỉ không còn là bức tường lúc lưu chương.
func appendChapterWords(vs []Violation, wordCount int, rng *WordRange, ceiling WordCeiling) []Violation {
	// Sàn
	if rng != nil && rng.Min > 0 && wordCount < rng.Min {
		deviation := float64(rng.Min-wordCount) / float64(rng.Min)
		severity := SeverityWarning
		if deviation >= ChapterWordsDeviationThreshold {
			severity = SeverityError
		}
		return append(vs, Violation{
			Rule:      "chapter_words",
			Limit:     fmt.Sprintf("≥%d", rng.Min),
			Actual:    wordCount,
			Deviation: deviation,
			Severity:  severity,
		})
	}

	// Trần mềm
	if ceiling.Active() && wordCount > ceiling.SoftMax {
		return append(vs, Violation{
			Rule:      "chapter_words",
			Limit:     fmt.Sprintf("≲%d (trần mềm = trung vị %d từ của %d chương gần nhất × %.1f)", ceiling.SoftMax, ceiling.Median, ceiling.Samples, CeilingFactor),
			Actual:    wordCount,
			Deviation: float64(wordCount-ceiling.SoftMax) / float64(ceiling.SoftMax),
			Severity:  SeverityWarning, // không bao giờ nâng lên error
		})
	}
	return vs
}
