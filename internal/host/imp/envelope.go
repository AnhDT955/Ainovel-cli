package imp

import (
	"fmt"
	"regexp"
	"strings"
)

// envelopeTagRe khớp dòng === TAG === (có thể có khoảng trắng trước/sau), không phân biệt hoa thường.
var envelopeTagRe = regexp.MustCompile(`(?m)^\s*===\s*([A-Z_]+)\s*===\s*$`)

// parseTaggedEnvelope phân tích đầu ra nhiều đoạn có dạng `=== TAG ===\nbody...` thành map.
// key là tên thẻ viết hoa, value là nội dung đoạn tương ứng (đã trim khoảng trắng đầu/cuối).
// Nếu xuất hiện thẻ trùng lặp, thẻ sau sẽ ghi đè thẻ trước.
func parseTaggedEnvelope(text string) map[string]string {
	matches := envelopeTagRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make(map[string]string, len(matches))
	for i, m := range matches {
		tag := strings.ToUpper(text[m[2]:m[3]])
		bodyStart := m[1]
		bodyEnd := len(text)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		out[tag] = strings.TrimSpace(text[bodyStart:bodyEnd])
	}
	return out
}

// requireTags kiểm tra envelope phải chứa các thẻ yêu cầu và không được rỗng.
func requireTags(env map[string]string, tags ...string) error {
	var missing []string
	for _, t := range tags {
		if strings.TrimSpace(env[t]) == "" {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tags: %s", strings.Join(missing, ", "))
	}
	return nil
}
