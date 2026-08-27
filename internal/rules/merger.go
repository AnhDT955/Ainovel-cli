package rules

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Merge gộp nhiều nguồn do loader trả về thành Bundle cuối cùng.
//
// Quy tắc gộp:
//   - Trường cấu trúc thông thường: ưu tiên nguồn gần nhất (sau ghi đè trước); nếu nhiều nguồn khai báo cùng trường với giá trị khác nhau thì ghi field_conflict
//   - fatigue_words: gộp theo từng từ; nếu cùng từ được khai báo ở nhiều nguồn với ngưỡng khác nhau thì ưu tiên nguồn gần nhất và ghi field_conflict
//   - Nội dung Markdown: ghép theo thứ tự nguồn, mỗi đoạn có tiêu đề nguồn, không ghi đè
//   - sources: tất cả đường dẫn file tải thành công
//   - conflicts: conflicts trong quá trình parse + field_conflict trong quá trình gộp
//
// Tham số layers phải được sắp xếp tăng dần theo SourceKind (dạng đầu ra của loader.Load).
func Merge(layers []Parsed) Bundle {
	bundle := Bundle{
		Structured:  Structured{},
		Preferences: "",
		Sources:     make([]string, 0, len(layers)),
		Conflicts:   nil,
	}

	// Giai đoạn A: thu thập tất cả nguồn khai báo cho từng trường, phục vụ phát hiện xung đột sau này
	declarations := map[string][]Parsed{}
	declare := func(field string, p Parsed) {
		declarations[field] = append(declarations[field], p)
	}
	for _, p := range layers {
		if p.Structured.Genre != "" {
			declare("genre", p)
		}
		if p.Structured.ChapterWords != nil {
			declare("chapter_words", p)
		}
		if len(p.Structured.ForbiddenChars) > 0 {
			declare("forbidden_chars", p)
		}
		if len(p.Structured.ForbiddenPhrases) > 0 {
			declare("forbidden_phrases", p)
		}
		if len(p.Structured.FatigueWords) > 0 {
			declare("fatigue_words", p)
		}
	}

	// Giai đoạn B: gộp các trường cấu trúc thành kết quả cuối cùng.
	// Trường vô hướng/danh sách giữ nguyên quy tắc ghi đè theo nguồn gần nhất;
	// fatigue_words là map, gộp theo từng từ để người dùng chỉ cần thêm ít từ sáo rỗng mới.
	for _, p := range layers {
		if p.Structured.Genre != "" {
			bundle.Structured.Genre = p.Structured.Genre
		}
		if p.Structured.ChapterWords != nil {
			bundle.Structured.ChapterWords = p.Structured.ChapterWords
		}
		if len(p.Structured.ForbiddenChars) > 0 {
			bundle.Structured.ForbiddenChars = p.Structured.ForbiddenChars
		}
		if len(p.Structured.ForbiddenPhrases) > 0 {
			bundle.Structured.ForbiddenPhrases = p.Structured.ForbiddenPhrases
		}
		if len(p.Structured.FatigueWords) > 0 {
			bundle.Structured.FatigueWords = mergeFatigueWords(bundle.Structured.FatigueWords, p.Structured.FatigueWords)
		}
	}

	// Giai đoạn C: tạo field_conflict (chỉ tính xung đột khi có nhiều nguồn + giá trị không nhất quán)
	for field, sources := range declarations {
		if len(sources) < 2 {
			continue
		}
		if field == "fatigue_words" {
			bundle.Conflicts = append(bundle.Conflicts, fatigueWordConflicts(sources)...)
			continue
		}
		if allEqual(field, sources) {
			continue
		}
		bundle.Conflicts = append(bundle.Conflicts, Conflict{
			Source: sources[len(sources)-1].Source,
			Kind:   ConflictFieldConflict,
			Field:  field,
			Detail: describeFieldConflict(field, sources),
		})
	}

	// Giai đoạn D: gộp nội dung Markdown tùy chỉnh
	var sb strings.Builder
	for _, p := range layers {
		if strings.TrimSpace(p.Preference) == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "## [%s] %s\n\n", p.Kind, p.Source)
		sb.WriteString(p.Preference)
	}
	bundle.Preferences = sb.String()

	// Giai đoạn E: tổng hợp sources và conflicts từ quá trình parse
	for _, p := range layers {
		bundle.Sources = append(bundle.Sources, p.Source)
		bundle.Conflicts = append(bundle.Conflicts, p.Conflicts...)
	}

	return bundle
}

func mergeFatigueWords(dst, src map[string]int) map[string]int {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]int, len(src))
	}
	for word, limit := range src {
		dst[word] = limit
	}
	return dst
}

func fatigueWordConflicts(sources []Parsed) []Conflict {
	type declaration struct {
		source string
		limit  int
	}
	byWord := make(map[string][]declaration)
	for _, p := range sources {
		for word, limit := range p.Structured.FatigueWords {
			if word == "" {
				continue
			}
			byWord[word] = append(byWord[word], declaration{source: p.Source, limit: limit})
		}
	}

	words := make([]string, 0, len(byWord))
	for word := range byWord {
		words = append(words, word)
	}
	sort.Strings(words)

	var conflicts []Conflict
	for _, word := range words {
		ds := byWord[word]
		if len(ds) < 2 {
			continue
		}
		first := ds[0].limit
		allSame := true
		for _, d := range ds[1:] {
			if d.limit != first {
				allSame = false
				break
			}
		}
		if allSame {
			continue
		}
		parts := make([]string, 0, len(ds))
		for _, d := range ds {
			parts = append(parts, fmt.Sprintf("%s=%d", d.source, d.limit))
		}
		winner := ds[len(ds)-1]
		conflicts = append(conflicts, Conflict{
			Source: winner.source,
			Kind:   ConflictFieldConflict,
			Field:  "fatigue_words." + word,
			Detail: fmt.Sprintf("Trường fatigue_words[%q] được khai báo ở nhiều nguồn với ngưỡng không nhất quán: %s; ưu tiên nguồn gần nhất: %s",
				word, strings.Join(parts, " | "), winner.source),
		})
	}
	return conflicts
}

// allEqual kiểm tra xem giá trị của cùng một trường ở nhiều nguồn có hoàn toàn nhất quán không;
// nếu nhất quán thì không báo xung đột.
//
// Trường danh sách về mặt ngữ nghĩa không quan tâm thứ tự, nhưng thực tế yaml deserialize
// đã giữ nguyên thứ tự khai báo — hai cấu hình hoàn toàn giống nhau sẽ trả về true với
// reflect.DeepEqual, đáp ứng tiêu chí "giá trị nhất quán".
// Trường hợp đặc biệt: thứ tự khác nhau nhưng phần tử giống nhau được xử lý là "không nhất quán"
// — điều này chấp nhận được (chỉ là thông tin, không chặn xử lý).
func allEqual(field string, sources []Parsed) bool {
	if len(sources) < 2 {
		return true
	}
	first := extractField(field, sources[0].Structured)
	for _, p := range sources[1:] {
		if !reflect.DeepEqual(first, extractField(field, p.Structured)) {
			return false
		}
	}
	return true
}

func extractField(field string, s Structured) any {
	switch field {
	case "genre":
		return s.Genre
	case "chapter_words":
		if s.ChapterWords == nil {
			return nil
		}
		return *s.ChapterWords
	case "forbidden_chars":
		return s.ForbiddenChars
	case "forbidden_phrases":
		return s.ForbiddenPhrases
	case "fatigue_words":
		return s.FatigueWords
	default:
		return nil
	}
}

// describeFieldConflict mô tả xung đột theo cách con người có thể đọc được:
// liệt kê tất cả nguồn kèm giá trị của từng nguồn.
// Cuối cùng chú thích nguồn có hiệu lực (ưu tiên nguồn gần nhất).
func describeFieldConflict(field string, sources []Parsed) string {
	var parts []string
	for _, p := range sources {
		parts = append(parts, fmt.Sprintf("%s=%s", p.Source, formatFieldValue(field, p.Structured)))
	}
	winner := sources[len(sources)-1]
	return fmt.Sprintf(
		"Trường %s được khai báo ở nhiều nguồn với giá trị không nhất quán: %s; ưu tiên nguồn gần nhất: %s",
		field, strings.Join(parts, " | "), winner.Source,
	)
}

func formatFieldValue(field string, s Structured) string {
	switch field {
	case "genre":
		return s.Genre
	case "chapter_words":
		if s.ChapterWords == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%d-%d", s.ChapterWords.Min, s.ChapterWords.Max)
	case "forbidden_chars":
		return fmt.Sprintf("%v", s.ForbiddenChars)
	case "forbidden_phrases":
		return fmt.Sprintf("%v", s.ForbiddenPhrases)
	case "fatigue_words":
		return fmt.Sprintf("%v", s.FatigueWords)
	default:
		return "<unknown>"
	}
}
