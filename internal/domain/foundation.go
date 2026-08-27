package domain

import (
	"fmt"
	"strings"
)

// Bộ nền (premise/characters/world_rules) là NGUỒN SỰ THẬT DUY NHẤT mà Người viết đọc được:
// tài liệu yêu cầu gốc của người dùng không đi vào lịch sử hội thoại của Người viết. Vì vậy một bộ
// nền rỗng không phải là "thiếu dữ liệu tạm thời" — nó khiến toàn bộ các chương sau bị bịa ra từ
// hư không, và không có bước nào phía sau phát hiện được.
//
// Sự cố thật đã gặp: Kiến trúc sư lưu 6 world_rules toàn chuỗi rỗng và 10 nhân vật chỉ có tên vai
// ("Nhân vật chính", description rỗng). Cổng kiểm tra khi đó chỉ đếm `len(rules) == 0` nên coi là
// đã đủ, tự đẩy sang giai đoạn viết, và 68 chương được viết ra mà không hề mang hệ thống tu luyện,
// kim thủ chỉ hay nguyên tắc nhân vật nào của người dùng.
//
// Các hàm dưới đây là chốt chặn cho lớp đó: đo NỘI DUNG chứ không đếm phần tử.

// Tiêu chí ở đây cố ý CHỈ bắt trường rỗng, không bắt trường ngắn.
//
// Ngưỡng độ dài từng được thử và đã bị bỏ: nó chặn nhầm mô tả hợp lệ nhưng súc tích (fixture thật
// có description "独立记者" — 4 ký tự, hoàn toàn dùng được), trong khi vẫn không bắt nổi trường hợp
// nó nhắm tới, vì một mô hình viết nội dung vô nghĩa thì viết vô nghĩa dài. Chế độ hỏng có thật và
// quan sát được là trường RỖNG cùng TÊN VAI TRÒ chung chung — hai thứ đó khách quan và được bắt
// chính xác bên dưới. Phán xét "đủ hay/đủ sâu" thuộc về Kiến trúc sư, không thuộc về validator.

// placeholderNovelNames là các nhãn "chỗ điền" mà mô hình hay chép nguyên xi từ prompt hoặc từ
// tài liệu yêu cầu (tài liệu của người dùng thường mở đầu bằng đúng dòng nhãn "Tên truyện").
// Chúng không bao giờ là tên sách thật.
var placeholderNovelNames = map[string]struct{}{
	"书名":               {},
	"实际书名":             {},
	"示例书名":             {},
	"tên truyện":       {},
	"tên sách":         {},
	"tên thực":         {},
	"ví dụ tên truyện": {},
	"tiêu đề":          {},
	"title":            {},
}

// placeholderCharacterNames là các tên vai trò chung chung mà Kiến trúc sư điền vào khi nó bỏ qua
// tài liệu yêu cầu và chỉ dựng khung mẫu. Không nhân vật thật nào mang những tên này.
var placeholderCharacterNames = map[string]struct{}{
	"nhân vật chính":                 {},
	"nhân vật phụ":                   {},
	"người dẫn đường":                {},
	"đối tác chiến lược":             {},
	"người giữ hậu phương":           {},
	"người đưa tin":                  {},
	"người đưa tin / môi giới":       {},
	"nhân vật của một phe trung gian": {},
	"nhân chứng sống":                {},
	"phản diện":                      {},
	"phản diện lõi":                  {},
	"nam chính":                      {},
	"nữ chính":                       {},
	"主角":                            {},
	"protagonist":                    {},
}

func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// IsPlaceholderNovelName cho biết tên sách có phải là nhãn chỗ điền bị chép lại hay không.
func IsPlaceholderNovelName(name string) bool {
	_, ok := placeholderNovelNames[normalizeKey(strings.Trim(name, "《》\""))]
	return ok
}

// IsPlaceholderCharacterName cho biết tên nhân vật có phải là tên vai trò chung chung hay không.
// "Phản diện tầng 1/2/3" bắt theo tiền tố vì phần đuôi là số tầng tùy ý.
func IsPlaceholderCharacterName(name string) bool {
	key := normalizeKey(name)
	if _, ok := placeholderCharacterNames[key]; ok {
		return true
	}
	return strings.HasPrefix(key, "phản diện tầng")
}

// ExtractNovelName lấy tên sách từ dòng tiêu đề `# Tên` (có thể bọc 《》).
// Trả về chuỗi rỗng khi dòng đầu không phải tiêu đề hoặc chỉ là nhãn chỗ điền.
func ExtractNovelName(premise string) string {
	for raw := range strings.SplitSeq(strings.ReplaceAll(premise, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "# ") {
			return ""
		}
		name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "# ")), "《》\"")
		if IsPlaceholderNovelName(name) {
			return ""
		}
		return name
	}
	return ""
}

// ValidatePremise từ chối premise không mang tên sách thật.
//
// Chỉ kiểm tra tiêu đề chứ không kiểm tra độ dài phần thân: phần thân là đất diễn của Kiến trúc sư,
// còn tiêu đề là thứ duy nhất ở đây có đáp án đúng/sai khách quan — và cũng chính là chỗ đã hỏng
// trong sự cố thật (premise.md mở đầu bằng đúng chữ "# Tên truyện").
func ValidatePremise(premise string) error {
	trimmed := strings.TrimSpace(premise)
	if trimmed == "" {
		return fmt.Errorf("premise rỗng")
	}
	first := trimmed
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		first = strings.TrimSpace(trimmed[:idx])
	}
	if !strings.HasPrefix(first, "# ") {
		return fmt.Errorf("dòng đầu tiên của premise phải là tiêu đề Markdown chứa TÊN SÁCH THẬT, ví dụ `# Loạn Thế Võ Đạo`; hiện tại là %q", first)
	}
	name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(first, "# ")), "《》\"")
	if name == "" {
		return fmt.Errorf("tiêu đề premise rỗng: phải là tên sách thật")
	}
	if IsPlaceholderNovelName(name) {
		return fmt.Errorf("tiêu đề premise là %q — đây là nhãn chỗ điền bị chép lại, không phải tên sách. "+
			"Hãy đặt một tên sách thật lấy từ tài liệu yêu cầu của người dùng (nếu tài liệu đã nêu tên thì dùng nguyên văn tên đó)", name)
	}
	return nil
}

// ValidateCharacters từ chối hồ sơ nhân vật chỉ có khung mà không có người.
func ValidateCharacters(chars []Character) error {
	if len(chars) == 0 {
		return fmt.Errorf("danh sách nhân vật rỗng")
	}
	var problems []string
	for i, c := range chars {
		name := strings.TrimSpace(c.Name)
		switch {
		case name == "":
			problems = append(problems, fmt.Sprintf("nhân vật #%d thiếu name", i+1))
		case IsPlaceholderCharacterName(name):
			problems = append(problems, fmt.Sprintf("nhân vật #%d có name=%q — đây là tên VAI TRÒ chung chung, không phải tên riêng của nhân vật", i+1, name))
		}
		if strings.TrimSpace(c.Description) == "" {
			problems = append(problems, fmt.Sprintf("nhân vật %q có description rỗng", nameOrIndex(name, i)))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("hồ sơ nhân vật chưa có nội dung thật:\n  - %s\n\n"+
		"Mỗi nhân vật phải có TÊN RIÊNG (lấy nguyên văn từ tài liệu yêu cầu của người dùng nếu tài liệu đã nêu) và một description mô tả cụ thể con người đó. "+
		"Tên vai trò như \"Nhân vật chính\", \"Phản diện tầng 1\" là khung mẫu, không được lưu",
		strings.Join(problems, "\n  - "))
}

// ValidateWorldRules từ chối quy tắc thế giới rỗng.
//
// Đây là chốt chặn quan trọng nhất: hệ thống tu luyện, cấp bậc công pháp và cơ chế đặc thù của
// người dùng sống ở đây, và không có nơi nào khác trong bộ nền chứa được chúng.
func ValidateWorldRules(rules []WorldRule) error {
	if len(rules) == 0 {
		return fmt.Errorf("danh sách quy tắc thế giới rỗng")
	}
	var problems []string
	for i, r := range rules {
		if strings.TrimSpace(r.Category) == "" {
			problems = append(problems, fmt.Sprintf("quy tắc #%d thiếu category", i+1))
		}
		if strings.TrimSpace(r.Rule) == "" {
			problems = append(problems, fmt.Sprintf("quy tắc #%d có trường rule rỗng", i+1))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("quy tắc thế giới chưa có nội dung thật:\n  - %s\n\n"+
		"Mỗi mục phải mô tả một quy tắc cụ thể của thế giới. Nếu tài liệu yêu cầu của người dùng có nêu hệ thống tu luyện, "+
		"bảng cấp bậc/phẩm cấp hay cơ chế đặc thù, PHẢI ghi chúng vào đây nguyên văn — đây là nơi duy nhất Người viết đọc được các thiết lập đó",
		strings.Join(problems, "\n  - "))
}

// ValidateOutline từ chối đề cương chỉ có nhãn mà không có chương.
//
// Sự cố thật (2026-07-15): Kiến trúc sư lưu 14 mục dạng
// {chapter: 0, title: "Arc 1: Hắc Bang Đoạt Phố, chương 36-80", core_event: "", hook: ""} —
// đó là danh sách CUNG TRUYỆN bị nhét vào chỗ của danh sách CHƯƠNG, kèm một mục rác tên
// "Core conflict". Cổng khi đó chỉ hỏi `len(entries) == 0` nên nhận hết, save_foundation tự đẩy
// phase sang writing với total_chapters=14, và Người viết bắt đầu chương 1 trên đề cương rỗng.
// Đây đúng là lỗ hổng đã được bịt cho characters/world_rules nhưng bỏ sót ở outline.
//
// Tiêu chí giữ nguyên tinh thần của Validate{Characters,WorldRules}: chỉ bắt trường RỖNG và số
// chương không hợp lệ — hai thứ khách quan. Chất lượng nhịp truyện là đất diễn của Kiến trúc sư.
// core_event nằm trong danh sách bắt buộc vì nó là thứ Người viết thực sự dựa vào để viết
// (prompt Kiến trúc sư: mỗi chương cần title/core_event cùng kiến trúc conflict→consequence); một đề cương chỉ có
// title là một mục lục, không phải kế hoạch.
func ValidateOutline(entries []OutlineEntry) error {
	return validateOutlineEntries(entries, true)
}

// ValidateOutlineChapters kiểm tra nội dung các mục chương nhưng BỎ QUA trường chapter — dùng cho
// expand_arc, nơi FlattenOutline đánh số chương toàn cục theo vị trí cung, nên số do Kiến trúc sư
// truyền vào sẽ bị ghi đè và không có gì để đúng/sai.
func ValidateOutlineChapters(entries []OutlineEntry) error {
	return validateOutlineEntries(entries, false)
}

func validateOutlineEntries(entries []OutlineEntry, requireChapterNo bool) error {
	if len(entries) == 0 {
		return fmt.Errorf("đề cương rỗng")
	}
	var problems []string
	for i, e := range entries {
		label := strings.TrimSpace(e.Title)
		if label == "" {
			label = fmt.Sprintf("#%d", i+1)
		}
		if requireChapterNo && e.Chapter <= 0 {
			problems = append(problems, fmt.Sprintf("mục #%d (%s) có chapter=%d — số chương phải là số nguyên bắt đầu từ 1; mỗi mục là MỘT chương, không phải một cung truyện", i+1, label, e.Chapter))
		}
		if strings.TrimSpace(e.Title) == "" {
			problems = append(problems, fmt.Sprintf("mục #%d thiếu title", i+1))
		}
		if strings.TrimSpace(e.CoreEvent) == "" {
			problems = append(problems, fmt.Sprintf("mục #%d (%s) có core_event rỗng", i+1, label))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("đề cương chưa có nội dung thật:\n  - %s\n\n"+
		"Mỗi mục phải là MỘT CHƯƠNG với `title` là tên chương và `core_event` mô tả sự kiện cốt lõi xảy ra trong "+
		"chương đó (`chapter` là số thứ tự chương liên tục 1, 2, 3...). Danh sách cung truyện "+
		"(\"Arc 1: ..., chương 1-35\") KHÔNG phải là đề cương chương — cấu trúc cung truyện thuộc về layered_outline",
		strings.Join(clampProblems(problems), "\n  - "))
}

// clampProblems giữ thông báo lỗi ở kích thước dùng được: một đề cương dài hỏng toàn bộ sẽ sinh
// hàng trăm dòng, đủ để chiếm trọn lượt sửa của Kiến trúc sư mà không nói thêm được gì.
func clampProblems(problems []string) []string {
	const limit = 10
	if len(problems) <= limit {
		return problems
	}
	return append(problems[:limit:limit], fmt.Sprintf("... và %d vấn đề tương tự khác", len(problems)-limit))
}

func nameOrIndex(name string, i int) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("#%d", i+1)
}

// HasSubstance cho biết một hồ sơ nhân vật có nội dung thật hay chỉ là khung mẫu.
func (c Character) HasSubstance() bool {
	name := strings.TrimSpace(c.Name)
	return name != "" &&
		!IsPlaceholderCharacterName(name) &&
		strings.TrimSpace(c.Description) != ""
}

// HasSubstance cho biết một quy tắc thế giới có nội dung thật hay chỉ là ô trống.
func (r WorldRule) HasSubstance() bool {
	return strings.TrimSpace(r.Rule) != ""
}

// HasSubstance cho biết một mục đề cương có phải là một chương thật hay chỉ là nhãn.
// Phải đồng ý với ValidateOutline: cổng FoundationMissing và tầng công cụ dùng chung định nghĩa này.
func (e OutlineEntry) HasSubstance() bool {
	return e.Chapter > 0 &&
		strings.TrimSpace(e.Title) != "" &&
		strings.TrimSpace(e.CoreEvent) != ""
}
