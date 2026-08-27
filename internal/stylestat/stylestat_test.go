package stylestat

import (
	"strings"
	"testing"
)

func chapterWith(body string) string {
	return "# 标题\n" + body
}

func TestComputeBelowMinChapters(t *testing.T) {
	in := Input{Chapters: []string{"a", "b", "c", "d"}}
	if Compute(in) != nil {
		t.Fatal("below minChapters should return nil")
	}
}

func TestComputePatterns(t *testing.T) {
	body := "Không phải anh giận dữ, mà là sợ hãi. Hệt như một con dốc dài. Anh dường như im lặng không nói gì.\nChính văn.\n"
	chapters := make([]string, 6)
	for i := range chapters {
		chapters[i] = chapterWith(body)
	}
	s := Compute(Input{Chapters: chapters})
	if s == nil {
		t.Fatal("expected stats")
	}
	want := map[string]int{
		"Cấu trúc chỉnh chuẩn『không phải... mà là... / không chỉ... mà còn...』": 6,
		"So sánh trực tiếp『như thể / hệt như / tựa như / dường như』":             12,
		"Nhịp im lặng / bất giác『không nói gì / im lặng / không khỏi / bất giác』":   12,
	}
	for _, p := range s.Patterns {
		if w, ok := want[p.Name]; ok && p.Total != w {
			t.Errorf("%s total: got %d want %d", p.Name, p.Total, w)
		}
		if p.PerChapter != float64(want[p.Name])/6.0 {
			// PerChapter is rounded to 1 decimal place, e.g. 1.0 or 2.0
			expectedPerChapter := float64(want[p.Name]) / 6.0
			expectedPerChapter = float64(int(expectedPerChapter*10+0.5)) / 10
			if p.PerChapter != expectedPerChapter {
				t.Errorf("%s per_chapter: got %v want %v", p.Name, p.PerChapter, expectedPerChapter)
			}
		}
	}
	if len(s.Patterns) != 3 {
		t.Errorf("want 3 pattern classes, got %d: %+v", len(s.Patterns), s.Patterns)
	}
}

func TestComputeTopPhrasesWithStopwords(t *testing.T) {
	// "đỉnh núi thanh vân" xuất hiện với tần suất cao; "Lâm Phong" là tên nhân vật nên bị lọc bỏ
	line := "Mọi người nhìn về phía đỉnh núi thanh vân, Lâm Phong đứng chắp tay sau lưng.\n"
	chapters := make([]string, 10)
	for i := range chapters {
		chapters[i] = chapterWith(strings.Repeat(line, 3))
	}
	s := Compute(Input{Chapters: chapters, Stopwords: []string{"Lâm Phong"}})
	if s == nil {
		t.Fatal("expected stats")
	}
	var hasMountain, hasName bool
	for _, p := range s.TopPhrases {
		if strings.Contains(p.Text, "núi thanh vân") || strings.Contains(p.Text, "đỉnh núi") {
			hasMountain = true
		}
		if strings.Contains(p.Text, "lâm") || strings.Contains(p.Text, "phong") {
			hasName = true
		}
	}
	if !hasMountain {
		t.Errorf("expected thanh vân mountain phrase mined, got %+v", s.TopPhrases)
	}
	if hasName {
		t.Errorf("character name should be filtered, got %+v", s.TopPhrases)
	}
}

func TestComputeRepeatedSentences(t *testing.T) {
	motto := "此生未能远行，望你替我看看远方的山海。"
	chapters := make([]string, 6)
	for i := range chapters {
		body := "平常正文，没有什么重复。\n"
		if i%2 == 0 {
			body += motto + "\n"
		}
		chapters[i] = chapterWith(body)
	}
	s := Compute(Input{Chapters: chapters})
	if s == nil {
		t.Fatal("expected stats")
	}
	if len(s.RepeatedSentences) == 0 {
		t.Fatalf("expected repeated sentence, got none")
	}
	got := s.RepeatedSentences[0]
	if got.Chapters != 3 || got.Count != 3 {
		t.Errorf("repeated sentence: %+v", got)
	}
	if !strings.HasPrefix(got.Text, "此生未能远行") {
		t.Errorf("text: %q", got.Text)
	}
}

func TestComputeEndingAndOpening(t *testing.T) {
	short := chapterWith("Suốt đêm không ngủ.\nChính văn rất dài rất dài.\nAnh ấy đi rồi.")
	long := chapterWith("Chuyện ban ngày.\nChính văn.\nĐây là một câu kết thúc cực kỳ cực kỳ dài, vượt xa ngưỡng ba mươi ký tự dùng để kiểm tra trung vị.")
	chapters := []string{short, short, short, long, long}
	s := Compute(Input{Chapters: chapters})
	if s == nil {
		t.Fatal("expected stats")
	}
	if s.Ending.ShortRatio != 0.6 {
		t.Errorf("short_ratio: got %v want 0.6", s.Ending.ShortRatio)
	}
	if s.OpeningTimeRate != 0.6 {
		t.Errorf("opening_time_rate: got %v want 0.6", s.OpeningTimeRate)
	}
}

func TestComputeTitleFormats(t *testing.T) {
	chapters := make([]string, 5)
	for i := range chapters {
		chapters[i] = chapterWith("正文。")
	}
	// Dùng lẫn lộn → báo cáo
	s := Compute(Input{Chapters: chapters, Titles: []string{"第一章 风起", "云涌", "第3章 雷动"}})
	if s.TitleFormats == nil || s.TitleFormats.WithPrefix != 2 || s.TitleFormats.WithoutPrefix != 1 {
		t.Errorf("title formats: %+v", s.TitleFormats)
	}
	// Đồng nhất → không báo cáo
	s = Compute(Input{Chapters: chapters, Titles: []string{"风起", "云涌"}})
	if s.TitleFormats != nil {
		t.Errorf("uniform titles should not report: %+v", s.TitleFormats)
	}
}
