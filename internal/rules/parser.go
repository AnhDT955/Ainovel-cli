package rules

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tập hợp các trường front matter đã biết, dùng để nhận diện trường lạ và ghi vào conflicts.
var knownFrontMatterFields = map[string]struct{}{
	"genre":             {},
	"chapter_words":     {},
	"forbidden_chars":   {},
	"forbidden_phrases": {},
	"fatigue_words":     {},
}

// Parse phân tích nội dung một file rules.md (front matter + Markdown).
//
// Chiến lược khoan dung:
//   - front matter phân tích thất bại toàn bộ: không chặn, phần thân vẫn làm preference, conflicts ghi parse_error
//   - trường lạ: bỏ qua, conflicts ghi unknown_field
//   - kiểu trường sai: bỏ trường đó, conflicts ghi type_error
//   - giá trị trường không hợp lệ (vd chapter_words không parse được thành phạm vi): bỏ, conflicts ghi invalid_value
//
// source là đường dẫn file, chỉ dùng cho conflicts.source; kind quyết định độ ưu tiên.
func Parse(source string, kind SourceKind, content []byte) Parsed {
	parsed := Parsed{Source: source, Kind: kind}

	fmText, bodyText := splitFrontMatter(content)
	parsed.Preference = strings.TrimSpace(bodyText)

	if strings.TrimSpace(fmText) == "" {
		return parsed
	}

	// Unmarshal vào map[string]any trước, rồi parse từng trường theo kiểu mạnh.
	// Cách này phân biệt được "trường không tồn tại" và "trường sai kiểu", đồng thời nhận diện trường lạ.
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fmText), &raw); err != nil {
		parsed.Conflicts = append(parsed.Conflicts, Conflict{
			Source: source,
			Kind:   ConflictParseError,
			Detail: fmt.Sprintf("phân tích YAML front matter thất bại: %v", err),
		})
		return parsed
	}

	for key, val := range raw {
		if _, ok := knownFrontMatterFields[key]; !ok {
			parsed.Conflicts = append(parsed.Conflicts, Conflict{
				Source: source,
				Kind:   ConflictUnknownField,
				Field:  key,
				Detail: fmt.Sprintf("trường lạ %q, Phase 1 chưa hỗ trợ; đã bỏ qua", key),
			})
			continue
		}
		applyField(&parsed, key, val)
	}

	return parsed
}

// splitFrontMatter tách front matter được bao bởi `---` và phần thân còn lại.
//
// Quy ước:
//   - File bắt đầu bằng `---` (cho phép BOM / dòng trống) mới được coi là có front matter
//   - Sau `---` thứ hai là phần thân
//   - Không có front matter: toàn bộ là phần thân
//   - Chỉ có `---` mở đầu mà không có `---` đóng: coi là không có front matter (tránh nuốt cả bài)
func splitFrontMatter(content []byte) (fm, body string) {
	text := string(bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})) // bỏ UTF-8 BOM
	lines := strings.Split(text, "\n")

	// Tìm dòng không rỗng đầu tiên; nếu không phải `---` thì toàn bộ là phần thân
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.TrimSpace(line) == "---" {
			start = i
		}
		break
	}
	if start < 0 {
		return "", text
	}

	// Tìm `---` thứ hai
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		// Có `---` mở đầu nhưng không đóng: thận trọng coi là không có front matter
		return "", text
	}

	fm = strings.Join(lines[start+1:end], "\n")
	body = strings.Join(lines[end+1:], "\n")
	return fm, body
}

// applyField đưa một trường raw vào Parsed.Structured, ghi conflicts khi kiểu không khớp.
func applyField(p *Parsed, key string, val any) {
	switch key {
	case "genre":
		s, ok := asString(val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, "string", val))
			return
		}
		p.Structured.Genre = strings.TrimSpace(s)

	case "chapter_words":
		rng, ok := parseChapterWords(val)
		if !ok {
			p.Conflicts = append(p.Conflicts, Conflict{
				Source: p.Source,
				Kind:   ConflictInvalidValue,
				Field:  key,
				Detail: fmt.Sprintf("chapter_words cần khoảng \"min-max\" (vd 3000-6000) hoặc giá trị đơn (vd 2500), nhận được %v", val),
			})
			return
		}
		p.Structured.ChapterWords = rng

	case "forbidden_chars":
		list, ok := asStringList(p, key, val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, "[]string", val))
			return
		}
		p.Structured.ForbiddenChars = list

	case "forbidden_phrases":
		list, ok := asStringList(p, key, val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, "[]string", val))
			return
		}
		p.Structured.ForbiddenPhrases = list

	case "fatigue_words":
		m, ok := parseFatigueWords(p, val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, "map[string]int hoặc []string", val))
			return
		}
		p.Structured.FatigueWords = m
	}
}

// parseChapterWords phân tích khoảng số từ mỗi chương thành *WordRange, chấp nhận ba cách viết:
//   - chuỗi khoảng "min-max" (vd "3000-6000")
//   - ánh xạ {min, max}
//   - số nguyên dương N đơn (số trần 2500 hoặc chuỗi "2500") — hiểu là "mục tiêu N từ/chương",
//     tự động mở rộng thành khoảng N±20%. Nếu không, người dùng viết giá trị đơn theo trực giác
//     sẽ bị bỏ lặng lẽ, rơi về mặc định nội trang (issue #41).
func parseChapterWords(val any) (*WordRange, bool) {
	switch v := val.(type) {
	case string:
		s := strings.TrimSpace(v)
		if !strings.Contains(s, "-") { // cách viết giá trị đơn, vd "2500"
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				return wordBandAround(n), true
			}
			return nil, false
		}
		parts := strings.Split(s, "-")
		if len(parts) != 2 {
			return nil, false
		}
		minV, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		maxV, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || minV < 0 || maxV < 0 || minV > maxV {
			return nil, false
		}
		return &WordRange{Min: minV, Max: maxV}, true
	case map[string]any:
		minV, ok1 := asInt(v["min"])
		maxV, ok2 := asInt(v["max"])
		if !ok1 || !ok2 || minV < 0 || maxV < 0 || minV > maxV {
			return nil, false
		}
		return &WordRange{Min: minV, Max: maxV}, true
	default: // số trần, YAML parse thành int / float64
		if n, ok := asInt(v); ok && n > 0 {
			return wordBandAround(n), true
		}
		return nil, false
	}
}

// wordBandAround mở rộng "mục tiêu N từ/chương" thành khoảng thoải mái ±20% (vd 2500 → 2000-3000),
// để cách viết giá trị đơn tương đương một khoảng hợp lý, thay vì tường cứng N-N
// (khoảng chật sẽ gây vòng lặp nén vô tận).
func wordBandAround(n int) *WordRange {
	return &WordRange{Min: n * 4 / 5, Max: n * 6 / 5}
}

// parseFatigueWords chấp nhận cả map[string]int (kèm ngưỡng) lẫn []string (ngưỡng mặc định 1).
//
// Mỗi key sai kiểu hoặc ngưỡng không hợp lệ đều ghi conflict vào p.Conflicts, không bao giờ nuốt lặng.
// Trả về (map, true) nghĩa là có phần tử hợp lệ; (nil, false) nghĩa là sai kiểu toàn bộ hoặc mọi phần tử đều không hợp lệ.
func parseFatigueWords(p *Parsed, val any) (map[string]int, bool) {
	switch v := val.(type) {
	case map[string]any:
		out := make(map[string]int, len(v))
		for k, raw := range v {
			trimmed := strings.TrimSpace(k)
			if trimmed == "" {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictInvalidValue,
					Field:  "fatigue_words",
					Detail: "fatigue_words xuất hiện key trống; đã bỏ qua",
				})
				continue
			}
			n, ok := asInt(raw)
			if !ok {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictTypeError,
					Field:  "fatigue_words." + trimmed,
					Detail: fmt.Sprintf("fatigue_words[%q] cần ngưỡng int, nhận được %T(%v); đã bỏ key này", trimmed, raw, raw),
				})
				continue
			}
			if n <= 0 {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictInvalidValue,
					Field:  "fatigue_words." + trimmed,
					Detail: fmt.Sprintf("fatigue_words[%q] ngưỡng phải > 0, nhận được %d; đã bỏ key này", trimmed, n),
				})
				continue
			}
			out[trimmed] = n
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	case []any:
		out := make(map[string]int, len(v))
		for i, raw := range v {
			s, ok := raw.(string)
			if !ok {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictTypeError,
					Field:  fmt.Sprintf("fatigue_words[%d]", i),
					Detail: fmt.Sprintf("phần tử danh sách fatigue_words cần string, nhận được %T(%v); đã bỏ phần tử này", raw, raw),
				})
				continue
			}
			s = strings.TrimSpace(s)
			if s == "" {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictInvalidValue,
					Field:  fmt.Sprintf("fatigue_words[%d]", i),
					Detail: "phần tử danh sách fatigue_words là chuỗi trống; đã bỏ qua",
				})
				continue
			}
			out[s] = 1
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

// asString / asInt / asStringList là các tiện ích chuẩn hóa kiểu sau khi yaml.v3 deserialize.
//
// Chiến lược nghiêm ngặt (Debug-First): chỉ chấp nhận đúng kiểu mục tiêu, không tự chuyển kiểu khác.
// Lỗi kiểu do bên gọi ghi vào conflicts, không sửa lặng trong hàm tiện ích.

// asString chỉ chấp nhận string scalar.
// Lưu ý: trong YAML, `genre: 42` (không có dấu nháy) sẽ được deserialize thành int, hàm này coi là lỗi kiểu.
// Người dùng nên viết `genre: "42"` để khai báo string rõ ràng.
func asString(v any) (string, bool) {
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}

// asInt chấp nhận mọi kiểu nguyên; float64 chỉ chấp nhận khi đúng là số nguyên (YAML parse số thành float64 mặc định).
// Chuỗi số không tự chuyển nữa — tránh nhầm với "trường vô tình để thành chuỗi".
func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		// Chỉ chấp nhận khi float đúng là số nguyên (vd yaml parse `5` → float64(5.0))
		if x == float64(int(x)) {
			return int(x), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// asStringList mỗi phần tử phải là string; kiểu khác thì bỏ phần tử đó và ghi vào conflicts.
// Trả về (list, true) nghĩa là có phần tử hợp lệ; (nil, false) nghĩa là sai kiểu toàn bộ hoặc mọi phần tử đều không hợp lệ.
func asStringList(p *Parsed, field string, v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for i, raw := range arr {
		s, ok := raw.(string)
		if !ok {
			p.Conflicts = append(p.Conflicts, Conflict{
				Source: p.Source,
				Kind:   ConflictTypeError,
				Field:  fmt.Sprintf("%s[%d]", field, i),
				Detail: fmt.Sprintf("phần tử danh sách %s cần string, nhận được %T(%v); đã bỏ phần tử này", field, raw, raw),
			})
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func typeErr(source, field, expected string, got any) Conflict {
	return Conflict{
		Source: source,
		Kind:   ConflictTypeError,
		Field:  field,
		Detail: fmt.Sprintf("trường %s sai kiểu, cần %s, nhận được %T(%v); đã bỏ", field, expected, got, got),
	}
}
