package rules

import (
	"regexp"
	"strings"
)

// Lint kiểm tra đường đáy tích hợp sẵn: quét phần chính văn tìm tàn dư cơ chế,
// không liên quan đến quy tắc người dùng, luôn thực thi khi lưu chương.
// Cùng hợp đồng với Check — chỉ trả về sự thật (nguyên tắc sắt số một),
// không chặn luồng xử lý, để bộ phận đánh giá/người dùng phán quyết.
//
// Hiện có ba loại (toàn bộ từ lỗi thực chứng của sản phẩm chạy dài thực tế):
//   - markdown_residue: chính văn còn sót ** in đậm, dòng tiêu đề # ngoài dòng đầu (xuất txt sẽ lộ ký tự)
//   - non_cjk_fragments: đoạn ký tự Latin liên tiếp (mô hình trộn ngôn ngữ, ví dụ chính văn tiếng Trung lẫn "pattern")
func Lint(text string) []Violation {
	var vs []Violation
	vs = appendMarkdownResidue(vs, text)
	vs = appendNonCJKFragments(vs, text)
	return vs
}

func appendMarkdownResidue(vs []Violation, text string) []Violation {
	if n := strings.Count(text, "**"); n > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "**",
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	headings := 0
	seenContent := false
	for line := range strings.SplitSeq(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// Tiêu đề # ở dòng không trống đầu tiên là định dạng hợp lệ của file chương (không cố định số dòng, chấp nhận dòng trống dẫn đầu)
		first := !seenContent
		seenContent = true
		if !first && strings.HasPrefix(t, "#") {
			headings++
		}
	}
	if headings > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "#",
			Actual:   headings,
			Severity: SeverityWarning,
		})
	}
	return vs
}

var latinFragmentRe = regexp.MustCompile(`[A-Za-z]{2,}`)

// appendNonCJKFragments báo cáo tổng số lần xuất hiện đoạn ký tự Latin và các ví dụ đã loại trùng.
// Tiếng Anh hợp lệ của thể loại hiện đại (tên thương hiệu/viết tắt) cũng sẽ bị phát hiện — sự thật mức warning, để bộ phận đánh giá phán quyết theo thể loại.
func appendNonCJKFragments(vs []Violation, text string) []Violation {
	matches := latinFragmentRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return vs
	}
	seen := make(map[string]struct{})
	var examples []string
	for _, m := range matches {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		if len(examples) < 3 {
			examples = append(examples, m)
		}
	}
	return append(vs, Violation{
		Rule:     "non_cjk_fragments",
		Target:   strings.Join(examples, "、"),
		Actual:   len(matches),
		Severity: SeverityWarning,
	})
}
