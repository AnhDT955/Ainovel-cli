package exp

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"fmt"
	"html"
	"strings"
	"time"
)

// renderEPUB đóng gói tập hợp chương thành luồng byte EPUB 3.
//
// Cấu trúc gói (OEBPS là container OPS package):
//
//	mimetype                    (phải là mục zip đầu tiên + Method=Store không nén)
//	META-INF/container.xml      (trỏ đến OEBPS/content.opf)
//	OEBPS/content.opf           (metadata + manifest + spine)
//	OEBPS/nav.xhtml             (điều hướng EPUB 3)
//	OEBPS/style.css             (typography tối giản)
//	OEBPS/cover.xhtml           (tên sách, tùy chọn)
//	OEBPS/chapterNNN.xhtml      (mỗi chương một file)
func renderEPUB(
	novelName string,
	chapters []int,
	titleIdx chapterTitleIndex,
	locations map[int]chapterLocation,
	bodies map[int]string,
) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. mimetype phải là mục zip đầu tiên + Store (không nén) + nội dung chính xác không có BOM
	mt, err := zw.CreateHeader(&zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	})
	if err != nil {
		return nil, fmt.Errorf("create mimetype: %w", err)
	}
	if _, err := mt.Write([]byte("application/epub+zip")); err != nil {
		return nil, err
	}

	if err := zipDeflate(zw, "META-INF/container.xml", containerXML); err != nil {
		return nil, err
	}
	if err := zipDeflate(zw, "OEBPS/style.css", styleCSS); err != nil {
		return nil, err
	}

	hasCover := strings.TrimSpace(novelName) != ""
	if hasCover {
		if err := zipDeflate(zw, "OEBPS/cover.xhtml", renderCoverXHTML(novelName)); err != nil {
			return nil, err
		}
	}

	for _, ch := range chapters {
		loc, hasLoc := locations[ch]
		title := strings.TrimSpace(titleIdx[ch])
		body := stripChapterTitleHeader(strings.TrimSpace(bodies[ch]), title)
		xhtml := renderChapterXHTML(ch, title, loc, hasLoc, body)
		if err := zipDeflate(zw, "OEBPS/"+chapterFileName(ch), xhtml); err != nil {
			return nil, err
		}
	}

	if err := zipDeflate(zw, "OEBPS/nav.xhtml", renderNavXHTML(hasCover, chapters, titleIdx)); err != nil {
		return nil, err
	}

	if err := zipDeflate(zw, "OEBPS/content.opf", renderOPF(novelName, hasCover, chapters)); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize zip: %w", err)
	}
	return buf.Bytes(), nil
}

// zipDeflate ghi một mục thông thường (có nén).
func zipDeflate(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	_, err = w.Write([]byte(content))
	return err
}

func chapterFileName(ch int) string {
	return fmt.Sprintf("chapter%03d.xhtml", ch)
}

// chapterID là id của manifest item; tương ứng một-một với tên file.
func chapterID(ch int) string {
	return fmt.Sprintf("ch%03d", ch)
}

// Template cố định ────────────────────────────────────────────────

const containerXML = `<?xml version="1.0" encoding="utf-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`

const styleCSS = `body { font-family: serif; line-height: 1.7; margin: 1em; }
h1.book-title { font-size: 2em; text-align: center; margin: 4em 0 1em; }
.volume-divider { font-size: 1.6em; text-align: center; margin: 4em 0 1em; font-weight: bold; }
h1.chapter-title { font-size: 1.4em; text-align: center; margin: 2em 0 1.5em; }
p { text-indent: 2em; margin: 0.5em 0; }
`

// XHTML chương ────────────────────────────────────────────────

func renderChapterXHTML(ch int, title string, loc chapterLocation, hasLoc bool, body string) string {
	var b strings.Builder
	displayTitle := fmt.Sprintf("Chương %d", ch)
	if title != "" {
		displayTitle = fmt.Sprintf("Chương %d %s", ch, title)
	}

	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
`, html.EscapeString(displayTitle))

	if hasLoc && loc.IsFirstOfVolume {
		fmt.Fprintf(&b, "  <div class=\"volume-divider\">Tập %d %s</div>\n",
			loc.VolumeIdx, html.EscapeString(strings.TrimSpace(loc.VolumeTitle)))
	}

	fmt.Fprintf(&b, "  <h1 class=\"chapter-title\">%s</h1>\n", html.EscapeString(displayTitle))
	for _, para := range splitParagraphs(body) {
		fmt.Fprintf(&b, "  <p>%s</p>\n", html.EscapeString(para))
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// splitParagraphs tách đoạn theo dòng trống; nhiều dòng trống liên tiếp tính là một lần tách.
// Các đoạn trả về đều đã TrimSpace và không rỗng.
// Xuống dòng trong đoạn (dấu \n đơn) được giữ lại thành khoảng trắng — <p> trong XHTML
// không giữ nguyên xuống dòng, trình duyệt tự wrap.
func splitParagraphs(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	parts := strings.Split(body, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Xuống dòng trong đoạn đổi thành khoảng trắng, tránh mất nội dung khi render XHTML
		p = strings.ReplaceAll(p, "\n", " ")
		out = append(out, p)
	}
	return out
}

// Bìa ────────────────────────────────────────────────

func renderCoverXHTML(novelName string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head>
  <title>Bìa</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
`)
	if name := strings.TrimSpace(novelName); name != "" {
		fmt.Fprintf(&b, "  <h1 class=\"book-title\">%s</h1>\n", html.EscapeString(name))
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// nav.xhtml (điều hướng EPUB 3) ────────────────────────────────────────────────

func renderNavXHTML(hasCover bool, chapters []int, titleIdx chapterTitleIndex) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh-CN">
<head>
  <title>Mục lục</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
  <nav epub:type="toc">
    <h1>Mục lục</h1>
    <ol>
`)
	if hasCover {
		b.WriteString("      <li><a href=\"cover.xhtml\">Bìa</a></li>\n")
	}

	// Liệt kê chương phẳng. Nhóm theo tập/cung truyện trong trình đọc thực ra không gọn
	// bằng mục lục một cấp (trình đọc tự gập lại), hơn nữa nav EPUB 3 lồng ol trên một số
	// trình đọc sẽ render lạ. Giữ đơn giản.
	for _, ch := range chapters {
		title := strings.TrimSpace(titleIdx[ch])
		display := fmt.Sprintf("Chương %d", ch)
		if title != "" {
			display = fmt.Sprintf("Chương %d %s", ch, title)
		}
		fmt.Fprintf(&b, "      <li><a href=\"%s\">%s</a></li>\n",
			chapterFileName(ch), html.EscapeString(display))
	}

	b.WriteString(`    </ol>
  </nav>
</body>
</html>
`)
	return b.String()
}

// content.opf ────────────────────────────────────────────────

func renderOPF(novelName string, hasCover bool, chapters []int) string {
	bookID := bookIdentifier(novelName)
	modified := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	title := strings.TrimSpace(novelName)
	if title == "" {
		title = "Untitled"
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid" xml:lang="zh-CN">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">%s</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:language>zh-CN</dc:language>
    <dc:creator>ainovel-cli</dc:creator>
    <meta property="dcterms:modified">%s</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="css" href="style.css" media-type="text/css"/>
`, html.EscapeString(bookID), html.EscapeString(title), modified)

	if hasCover {
		b.WriteString(`    <item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/>` + "\n")
	}
	for _, ch := range chapters {
		fmt.Fprintf(&b, `    <item id="%s" href="%s" media-type="application/xhtml+xml"/>`+"\n",
			chapterID(ch), chapterFileName(ch))
	}

	b.WriteString("  </manifest>\n  <spine>\n")
	if hasCover {
		b.WriteString(`    <itemref idref="cover"/>` + "\n")
	}
	b.WriteString(`    <itemref idref="nav"/>` + "\n")
	for _, ch := range chapters {
		fmt.Fprintf(&b, `    <itemref idref="%s"/>`+"\n", chapterID(ch))
	}
	b.WriteString("  </spine>\n</package>\n")
	return b.String()
}

// bookIdentifier tạo chuỗi UUID ổn định từ tên tiểu thuyết.
//
// **Chỉ dùng novelName, không trộn danh sách chương**: danh tính tác phẩm nên gắn với
// "đây là cuốn sách nào", không gắn với "phạm vi xuất" hay "xuất đến chương mấy tại
// thời điểm xuất". Xuất lại cùng một cuốn thì ID không đổi, trình đọc nhận ra đây là
// bản cập nhật của cùng một tác phẩm (việc có cập nhật hay không do timestamp
// dcterms:modified đảm nhận). novelName rỗng dùng chung ID là edge case đã biết:
// khi người dùng không đặt tên cho hai cuốn khác nhau thì trách nhiệm thuộc về họ.
func bookIdentifier(novelName string) string {
	h := sha1.New()
	h.Write([]byte(novelName))
	sum := h.Sum(nil)
	// Định dạng theo kiểu UUID (8-4-4-4-12), không yêu cầu đúng RFC 4122 — EPUB chỉ cần chuỗi duy nhất ổn định.
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x",
		sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
