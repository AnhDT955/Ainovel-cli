package tui

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/host/exp"
)

func TestParseExportArgs_Format(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantFormat exp.Format
		wantPath   string
		wantErr    bool
	}{
		{name: "mặc định không nêu định dạng", args: nil, wantFormat: ""},
		{name: "từ khoá vị trí txt", args: []string{"txt"}, wantFormat: exp.FormatTXT},
		{name: "từ khoá vị trí epub", args: []string{"epub"}, wantFormat: exp.FormatEPUB},
		{name: "từ khoá vị trí hoa/thường", args: []string{"EPUB"}, wantFormat: exp.FormatEPUB},
		{name: "tham số format=epub", args: []string{"format=epub"}, wantFormat: exp.FormatEPUB},
		{name: "cờ --epub", args: []string{"--epub"}, wantFormat: exp.FormatEPUB},
		{name: "định dạng + path + range", args: []string{"epub", "out/book.epub", "from=1", "to=3"}, wantFormat: exp.FormatEPUB, wantPath: "out/book.epub"},
		{name: "path thường vẫn là path", args: []string{"book.txt"}, wantFormat: "", wantPath: "book.txt"},
		{name: "định dạng không hợp lệ", args: []string{"format=pdf"}, wantErr: true},
		{name: "chỉ định mâu thuẫn", args: []string{"txt", "--epub"}, wantErr: true},
		{name: "trùng định dạng thì chấp nhận", args: []string{"epub", "format=epub"}, wantFormat: exp.FormatEPUB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseExportArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("mong đợi lỗi, nhận opts=%+v", opts)
				}
				return
			}
			if err != nil {
				t.Fatalf("không mong đợi lỗi: %v", err)
			}
			if opts.Format != tc.wantFormat {
				t.Errorf("Format = %q, mong đợi %q", opts.Format, tc.wantFormat)
			}
			if opts.OutPath != tc.wantPath {
				t.Errorf("OutPath = %q, mong đợi %q", opts.OutPath, tc.wantPath)
			}
		})
	}
}
