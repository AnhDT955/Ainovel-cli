package exp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// chapterTitleIndex tra cứu tiêu đề theo số chương, trả về chuỗi rỗng nếu không tìm thấy.
type chapterTitleIndex map[int]string

func buildTitleIndex(outline []domain.OutlineEntry) chapterTitleIndex {
	idx := make(chapterTitleIndex, len(outline))
	for _, e := range outline {
		if e.Title != "" {
			idx[e.Chapter] = e.Title
		}
	}
	return idx
}

// chapterLocation là vị trí của một chương trong đề cương phân cấp. Chỉ lưu thông tin tập
// cần thiết cho định dạng xuất — cung truyện không xuất ra (từ góc độ độc giả, cung truyện
// là cấu trúc nội bộ quá chi tiết).
type chapterLocation struct {
	VolumeIdx       int
	VolumeTitle     string
	IsFirstOfVolume bool
}

// buildLocations xây dựng map {chapter -> location} theo thứ tự chương toàn cục của đề cương phân cấp.
// Số chương được tái tạo theo cùng quy tắc với FlattenOutline (cộng dồn theo thứ tự trong cung truyện trong tập),
// để đảm bảo nhất quán với số chương trong Progress.CompletedChapters. Lớp cung truyện vẫn phải
// duyệt qua (cần thiết để tính số chương toàn cục), nhưng không ghi vào location —
// khi xuất chỉ chèn phân cách ở đầu tập.
func buildLocations(volumes []domain.VolumeOutline) map[int]chapterLocation {
	if len(volumes) == 0 {
		return nil
	}
	locs := make(map[int]chapterLocation)
	ch := 0
	for _, v := range volumes {
		firstOfVol := true
		for _, a := range v.Arcs {
			for range a.Chapters {
				ch++
				locs[ch] = chapterLocation{
					VolumeIdx:       v.Index,
					VolumeTitle:     v.Title,
					IsFirstOfVolume: firstOfVol,
				}
				firstOfVol = false
			}
		}
	}
	return locs
}

// chapterHeaderRe khớp dòng đầu tiêu đề Markdown có số chương (# 第N章 / ## Chương 12 ...).
var chapterHeaderRe = regexp.MustCompile(`(?i)^#+\s+(?:第.+?章|Chương\s+\d+)`)

// atxTitleRe trích xuất phần văn bản của tiêu đề ATX (# tiêu đề).
var atxTitleRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)

// stripChapterTitleHeader loại bỏ dòng đầu nếu đó là tiêu đề chương sẽ bị trùng lặp
// với tiêu đề thống nhất của bộ xuất. Hai trường hợp: ① "# 第N章 …" (có số chương);
// ② tiêu đề markdown có nội dung chính xác là tiêu đề chương hiện tại
// (Người viết thường viết tên chương thuần túy làm tiêu đề dòng đầu, ví dụ "# 边村浮生",
// trùng với "第 N 章 边村浮生" do bộ xuất tạo ra). Các h1 khác (như "# 序章") được
// coi là một phần của nội dung và giữ nguyên.
// Bên gọi có trách nhiệm TrimSpace trước, nên các dòng trống đầu không cần xét.
func stripChapterTitleHeader(content, title string) string {
	first, rest, hasNewline := strings.Cut(content, "\n")
	if !isChapterTitleLine(first, title) {
		return content
	}
	if !hasNewline {
		return ""
	}
	return strings.TrimLeft(rest, "\n")
}

func isChapterTitleLine(line, title string) bool {
	if chapterHeaderRe.MatchString(line) {
		return true
	}
	if title = strings.TrimSpace(title); title == "" {
		return false
	}
	m := atxTitleRe.FindStringSubmatch(line)
	return len(m) == 2 && strings.TrimSpace(m[1]) == title
}

// renderTXT ghép nối văn bản cuối cùng.
//
// Thứ tự chương do chapters quyết định (bên gọi đã sắp xếp tăng dần và loại trùng theo số chương).
// bodies/titleIdx/locations đều xử lý theo kiểu "thiếu thì hạ cấp": thiếu tiêu đề chỉ xuất
// "第 N 章"; thiếu định vị phân cấp thì coi như đề cương phẳng.
func renderTXT(
	novelName string,
	chapters []int,
	titleIdx chapterTitleIndex,
	locations map[int]chapterLocation,
	bodies map[int]string,
) string {
	var b strings.Builder

	if name := strings.TrimSpace(novelName); name != "" {
		b.WriteString("《")
		b.WriteString(name)
		b.WriteString("》\n\n")
	}

	useLayered := len(locations) > 0

	for i, ch := range chapters {
		if useLayered {
			if loc, ok := locations[ch]; ok && loc.IsFirstOfVolume {
				b.WriteString("\n═══════════════════════════════════════════\n")
				fmt.Fprintf(&b, "           Tập %d  %s\n", loc.VolumeIdx, strings.TrimSpace(loc.VolumeTitle))
				b.WriteString("═══════════════════════════════════════════\n\n")
			}
		}

		title := strings.TrimSpace(titleIdx[ch])
		if title != "" {
			fmt.Fprintf(&b, "Chương %d  %s\n\n", ch, title)
		} else {
			fmt.Fprintf(&b, "Chương %d\n\n", ch)
		}

		body := stripChapterTitleHeader(strings.TrimSpace(bodies[ch]), title)
		b.WriteString(body)
		b.WriteString("\n")
		if i < len(chapters)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}
