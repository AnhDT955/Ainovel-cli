package tools

import (
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

var premiseHeadingAliases = map[string]string{
	// ── Canonical keys tiếng Việt (khớp với prompt architect-long.md) ──
	"Thể loại và tông điệu":    "Thể loại và tông điệu",
	"Định vị thể loại":         "Định vị thể loại",
	"Xung đột cốt lõi":        "Xung đột cốt lõi",
	"Mục tiêu nhân vật chính":  "Mục tiêu nhân vật chính",
	"Hướng kết cục":            "Hướng kết cục",
	"Vùng cấm viết":            "Vùng cấm viết",
	"Điểm bán hàng khác biệt":  "Điểm bán hàng khác biệt",
	"Điểm móc khác biệt":       "Điểm móc khác biệt",
	"Cam kết thực hiện cốt lõi": "Cam kết thực hiện cốt lõi",
	"Động cơ truyện":            "Động cơ truyện",
	"Tuyến quan hệ/phát triển":  "Tuyến quan hệ/phát triển",
	"Lộ trình nâng cấp":        "Lộ trình nâng cấp",
	"Bước ngoặt giữa chuyện":   "Bước ngoặt giữa chuyện",
	"Mệnh đề kết cục":          "Mệnh đề kết cục",
	"Tính phù hợp truyện ngắn": "Tính phù hợp truyện ngắn",

	// ── Aliases tiếng Trung (tương thích ngược với workspace cũ) ──
	"题材和基调":   "Thể loại và tông điệu",
	"题材定位":    "Định vị thể loại",
	"核心冲突":    "Xung đột cốt lõi",
	"主角目标":    "Mục tiêu nhân vật chính",
	"终局方向":    "Hướng kết cục",
	"结局方向":    "Hướng kết cục",
	"写作禁区":    "Vùng cấm viết",
	"差异化卖点":   "Điểm bán hàng khác biệt",
	"差异化钩子":   "Điểm móc khác biệt",
	"核心兑现承诺":  "Cam kết thực hiện cốt lõi",
	"故事引擎":    "Động cơ truyện",
	"关系/成长主线": "Tuyến quan hệ/phát triển",
	"升级路径":    "Lộ trình nâng cấp",
	"中段转折":    "Bước ngoặt giữa chuyện",
	"中期转向":    "Bước ngoặt giữa chuyện",
	"终局命题":    "Mệnh đề kết cục",
	"短篇适配性":   "Tính phù hợp truyện ngắn",
	"本作为什么适合短篇/单卷收束": "Tính phù hợp truyện ngắn",
}

func parsePremiseSections(premise string) map[string]string {
	lines := strings.Split(premise, "\n")
	sections := make(map[string]string)
	var current string
	var body []string

	flush := func() {
		if current == "" {
			return
		}
		text := strings.TrimSpace(strings.Join(body, "\n"))
		if text != "" {
			sections[current] = text
		}
		body = body[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heading, ok := canonicalPremiseHeading(trimmed); ok {
			flush()
			current = heading
			continue
		}
		if current != "" {
			body = append(body, line)
		}
	}
	flush()
	return sections
}

func canonicalPremiseHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	title := strings.TrimSpace(strings.TrimLeft(line, "#"))
	if title == "" {
		return "", false
	}
	canonical, ok := premiseHeadingAliases[title]
	return canonical, ok
}

func premiseStructure(premise string, tier domain.PlanningTier) map[string]any {
	sections := parsePremiseSections(premise)
	required := requiredPremiseHeadings(tier)
	found := make([]string, 0, len(required))
	var missing []string
	for _, heading := range required {
		if _, ok := sections[heading]; ok {
			found = append(found, heading)
			continue
		}
		missing = append(missing, heading)
	}

	structure := map[string]any{
		"template_ready": len(missing) == 0,
		"found":          found,
		"missing":        missing,
	}
	if len(sections) > 0 {
		structure["section_count"] = len(sections)
	}
	return structure
}

func requiredPremiseHeadings(tier domain.PlanningTier) []string {
	common := []string{
		"Thể loại và tông điệu",
		"Định vị thể loại",
		"Xung đột cốt lõi",
		"Mục tiêu nhân vật chính",
		"Hướng kết cục",
		"Vùng cấm viết",
		"Điểm bán hàng khác biệt",
		"Điểm móc khác biệt",
		"Cam kết thực hiện cốt lõi",
	}

	switch tier {
	case domain.PlanningTierLong:
		return append(common,
			"Động cơ truyện",
			"Tuyến quan hệ/phát triển",
			"Lộ trình nâng cấp",
			"Bước ngoặt giữa chuyện",
			"Mệnh đề kết cục",
		)
	case domain.PlanningTierMid:
		return append(common,
			"Động cơ truyện",
			"Bước ngoặt giữa chuyện",
		)
	case domain.PlanningTierShort:
		return append(common,
			"Tính phù hợp truyện ngắn",
		)
	default:
		return common
	}
}
