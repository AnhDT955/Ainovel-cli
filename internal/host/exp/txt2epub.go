package exp

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// txt2epubSeparatorRe khớp với các dòng phân cách trang trí chứa toàn ký tự =, _, *, -, ~ hoặc ═
	txt2epubSeparatorRe = regexp.MustCompile(`^[\s=_*\-~═]*$`)

	// Index pattern hỗ trợ: số, số La Mã, hoặc các từ số cơ bản trong tiếng Việt/Trung
	indexPattern = `(?i:\d+|[ivxldcm]+|Một|Hai|Ba|Bốn|Năm|Sáu|Bảy|Tám|Chín|Mười|Mốt|Chục|Trăm|一|二|三|四|五|六|七|八|九|十|百|千)`

	// txt2epubVolumeHeaderRe khớp các dòng dạng "Tập X: Tên Tập", "Tập X  Tên Tập", "Volume X", v.v.
	txt2epubVolumeHeaderRe = regexp.MustCompile(`^\s*(?i:Tập|Volume|Quyển|Vol)\s+(` + indexPattern + `)(?:\s+[\-.:]*\s+|\s+)(.*)$`)

	// txt2epubChapterHeaderRe khớp các dòng dạng "Chương X: Tên Chương", "Chương X  Tên Chương", "Chapter X", v.v.
	txt2epubChapterHeaderRe = regexp.MustCompile(`^(?i)(?:#+\s*)?(?:(?:Chương|Chapter|Ch)(?:\s+|\.|\d)|第\s*)(` + indexPattern + `)(?:[\s\-.:]+(.*))?$`)
)

// ConvertTXTToEPUB nhận nội dung file văn bản TXT của một tiểu thuyết, phân tích cú pháp
// thành các Tập (Volume), Chương (Chapter) và Nội dung chương, sau đó gọi hàm renderEPUB
// để trả về luồng byte EPUB hoàn chỉnh.
func ConvertTXTToEPUB(txtContent []byte, novelName string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(txtContent))

	var chapters []int
	titleIdx := make(chapterTitleIndex)
	locations := make(map[int]chapterLocation)
	bodies := make(map[int]string)

	var currentVolIdx int
	var currentVolTitle string
	var hasVolume bool
	var hasNewVolume bool

	var currentChNum int
	var currentChTitle string
	var currentChBody strings.Builder
	var inChapter bool

	var lastChNum int
	var lastVolIdx int

	firstLines := make([]string, 0, 5)

	saveCurrentChapter := func() {
		if inChapter {
			chapters = append(chapters, currentChNum)
			titleIdx[currentChNum] = currentChTitle
			bodies[currentChNum] = currentChBody.String()
			currentChBody.Reset()
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// 1. Bỏ qua dòng phân cách trang trí
		if trimmed != "" && txt2epubSeparatorRe.MatchString(trimmed) {
			continue
		}

		// 2. Kiểm tra nếu là tiêu đề Tập
		if m := txt2epubVolumeHeaderRe.FindStringSubmatch(trimmed); len(m) == 3 {
			saveCurrentChapter()
			inChapter = false

			volStr := m[1]
			volTitle := strings.TrimSpace(m[2])

			volIdx, err := strconv.Atoi(volStr)
			if err != nil {
				lastVolIdx++
				volIdx = lastVolIdx
			} else {
				lastVolIdx = volIdx
			}

			currentVolIdx = volIdx
			currentVolTitle = volTitle
			hasVolume = true
			hasNewVolume = true
			continue
		}

		// 3. Kiểm tra nếu là tiêu đề Chương
		if m := txt2epubChapterHeaderRe.FindStringSubmatch(trimmed); len(m) == 3 {
			saveCurrentChapter()

			chStr := m[1]
			chTitle := strings.TrimSpace(m[2])

			chNum, err := strconv.Atoi(chStr)
			if err != nil {
				lastChNum++
				chNum = lastChNum
			} else {
				lastChNum = chNum
			}

			currentChNum = chNum
			currentChTitle = chTitle
			inChapter = true

			if hasVolume {
				locations[chNum] = chapterLocation{
					VolumeIdx:       currentVolIdx,
					VolumeTitle:     currentVolTitle,
					IsFirstOfVolume: hasNewVolume,
				}
				hasNewVolume = false
			}
			continue
		}

		// 4. Nội dung thường
		if inChapter {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine != "" {
				if currentChBody.Len() > 0 && !strings.HasSuffix(currentChBody.String(), "\n\n") {
					currentChBody.WriteString("\n")
				}
				currentChBody.WriteString(trimmedLine)
				currentChBody.WriteString("\n\n")
			}
		} else {
			// Thu thập dòng không trống đầu tiên để suy luận tên truyện
			if trimmed != "" && len(firstLines) < 5 {
				firstLines = append(firstLines, trimmed)
			}
		}
	}

	// Lưu chương cuối cùng
	saveCurrentChapter()

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Suy luận tên tiểu thuyết từ dòng đầu tiên nếu novelName để trống
	if novelName == "" {
		if len(firstLines) > 0 {
			first := firstLines[0]
			// Thử bóc ký tự 《 》 nếu có
			if strings.HasPrefix(first, "《") && strings.HasSuffix(first, "》") {
				novelName = strings.Trim(first, "《》")
			} else {
				novelName = first
			}
		} else {
			novelName = "Untitled"
		}
	}

	if len(chapters) == 0 {
		return nil, fmt.Errorf("không tìm thấy bất kỳ chương nào trong nội dung TXT")
	}

	return renderEPUB(novelName, chapters, titleIdx, locations, bodies)
}
