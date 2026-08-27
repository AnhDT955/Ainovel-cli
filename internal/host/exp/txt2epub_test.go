package exp

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestConvertTXTToEPUB(t *testing.T) {
	txtContent := `《Huyền Thiên Trọng Khởi》

═══════════════════════════════════════════
           Tập 1  Thanh Trúc Sơ Hiện
═══════════════════════════════════════════

Chương 1  Xuyên không

Đầu nhức như búa bổ.
Đó là cảm nhận đầu tiên của Diệp Trần.

Chương 2: Hệ thống thức tỉnh

Hệ thống Chí Tôn đã thức tỉnh!
Bắt đầu hành trình mới.
`

	data, err := ConvertTXTToEPUB([]byte(txtContent), "")
	if err != nil {
		t.Fatalf("ConvertTXTToEPUB: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}

	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		files[f.Name] = string(buf)
	}

	// Xác nhận tên sách đã suy luận đúng
	if !strings.Contains(files["OEBPS/content.opf"], "<dc:title>Huyền Thiên Trọng Khởi</dc:title>") {
		t.Errorf("expected book title 'Huyền Thiên Trọng Khởi' in content.opf")
	}

	// Xác nhận bìa sách có tên truyện
	if !strings.Contains(files["OEBPS/cover.xhtml"], "Huyền Thiên Trọng Khởi") {
		t.Errorf("expected book title 'Huyền Thiên Trọng Khởi' in cover.xhtml")
	}

	// Kiểm tra xem có Tập 1 phân tách trong chapter001.xhtml
	ch1 := files["OEBPS/chapter001.xhtml"]
	if !strings.Contains(ch1, `class="volume-divider"`) || !strings.Contains(ch1, "Tập 1 Thanh Trúc Sơ Hiện") {
		t.Errorf("chapter1 missing volume divider: %s", ch1)
	}
	if !strings.Contains(ch1, "Chương 1 Xuyên không") {
		t.Errorf("chapter1 missing display title")
	}
	if !strings.Contains(ch1, "<p>Đầu nhức như búa bổ.</p>") {
		t.Errorf("chapter1 missing content paragraph 1")
	}

	// Kiểm tra chapter002.xhtml
	ch2 := files["OEBPS/chapter002.xhtml"]
	if strings.Contains(ch2, `class="volume-divider"`) {
		t.Errorf("chapter2 should NOT have volume divider")
	}
	if !strings.Contains(ch2, "Chương 2 Hệ thống thức tỉnh") {
		t.Errorf("chapter2 missing display title")
	}
	if !strings.Contains(ch2, "<p>Hệ thống Chí Tôn đã thức tỉnh!</p>") {
		t.Errorf("chapter2 missing content paragraph 1")
	}
}

func TestConvertTXTToEPUB_NoChapters(t *testing.T) {
	txtContent := `Chỉ có lời giới thiệu
Không có bất kỳ chương nào cả.
`
	_, err := ConvertTXTToEPUB([]byte(txtContent), "Không Chương")
	if err == nil {
		t.Fatal("expected error due to no chapters, got nil")
	}
	if !strings.Contains(err.Error(), "không tìm thấy bất kỳ chương nào") {
		t.Errorf("unexpected error: %v", err)
	}
}
