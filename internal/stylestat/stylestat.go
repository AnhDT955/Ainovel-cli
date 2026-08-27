// Package stylestat thực hiện thống kê phong cách toàn bộ tác phẩm dựa trên phần chính văn đã viết,
// chỉ xuất ra các con số thực tế khách quan.
//
// Động lực: Cửa sổ đánh giá nội cung (~10 chương) vốn mù với các khuôn mẫu cố định ở cấp toàn tác phẩm —
// tic câu trúc có thể xuất hiện vài chục lần mỗi chương, cuối chương đồng dạng, lặp lại xuyên chương.
// Nhìn từng chương riêng lẻ thì mỗi chỗ đều "bình thường", chỉ thống kê toàn tác phẩm mới phơi bày được.
// Thống kê giao cho code (xác định, không ảo giác), phán xét giao cho LLM (editor căn cứ số liệu
// để đánh giá từng chiều, writer dựa đó tự tránh).
package stylestat

import (
	"regexp"
	"sort"
	"strings"
)

// minChapters — ít hơn số chương này thì không xuất thống kê: mẫu quá nhỏ, tần suất không có ý nghĩa.
const minChapters = 5

// phraseWindow — khai thác cụm từ động chỉ xét N chương gần nhất: writer cần tránh "cửa miệng hiện tại".
const phraseWindow = 20

// Input là dữ liệu đầu vào để thống kê. Chapters xếp tăng dần theo số chương; Stopwords là
// danh từ riêng như tên nhân vật — bỏ qua khi khai thác cụm từ động (tên xuất hiện tự nhiên
// có tần suất cao, không phải vấn đề phong cách).
type Input struct {
	Chapters  []string
	Titles    []string
	Stopwords []string
}

// Stats là kết quả thống kê phong cách toàn tác phẩm. Tất cả trường đều là số liệu thực tế,
// không chứa bất kỳ nhận định hay chỉ thị nào.
type Stats struct {
	Chapters          int            `json:"chapters"`
	Patterns          []PatternStat  `json:"patterns,omitempty"`
	TopPhrases        []PhraseStat   `json:"top_phrases,omitempty"`
	RepeatedSentences []SentenceStat `json:"repeated_sentences,omitempty"`
	Ending            EndingStat     `json:"ending"`
	OpeningTimeRate   float64        `json:"opening_time_rate"`
	TitleFormats      *TitleStat     `json:"title_formats,omitempty"`
}

// PatternStat là số đếm toàn tác phẩm cho một lớp khuôn câu cố định (tic văn phong AI phổ biến).
type PatternStat struct {
	Name       string  `json:"name"`
	Total      int     `json:"total"`
	PerChapter float64 `json:"per_chapter"`
}

// PhraseStat là cụm từ xuất hiện nhiều được khai thác trong phraseWindow chương gần nhất.
type PhraseStat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// SentenceStat là câu dài lặp lại từng chữ xuyên nhiều chương (bằng chứng trực tiếp của lặp lại phơi bày).
type SentenceStat struct {
	Text     string `json:"text"`
	Chapters int    `json:"chapters"`
	Count    int    `json:"count"`
}

// EndingStat là phân bố hình thức dòng cuối chương. Kết thúc ngắn tự nó hợp lệ,
// chỉ khi đồng dạng toàn tác phẩm mới là vấn đề.
type EndingStat struct {
	ShortRatio  float64 `json:"short_ratio"`
	MedianRunes int     `json:"median_runes"`
}

// TitleStat là số đếm việc dùng lẫn lộn tiền tố "Chương N" trong tiêu đề chương
// (dùng lẫn = dấu vết cơ chế lộ ra trong sản phẩm).
type TitleStat struct {
	WithPrefix    int `json:"with_prefix"`
	WithoutPrefix int `json:"without_prefix"`
}

// patternDefs là các khuôn câu AI phổ biến. Số đếm là xấp xỉ (regex không phân tích ngữ pháp),
// mục đích là so sánh theo chiều dọc với đường cơ sở của chính tác phẩm, độ chính xác tuyệt đối không quan trọng.
var patternDefs = []struct {
	name string
	re   *regexp.Regexp
}{
	{"Cấu trúc chỉnh chuẩn『không phải... mà là... / không chỉ... mà còn...』", regexp.MustCompile(`(?i)(?:không phải[^.!?。！？\n]{1,80}?(?:mà là|mà chỉ)|không chỉ[^.!?。！？\n]{1,80}?mà còn)`)},
	{"So sánh trực tiếp『như thể / hệt như / tựa như / dường như』", regexp.MustCompile(`(?i)(?:như thể|hệt như|tựa như|dường như|vẻ như|phảng phất)`)},
	{"Nhịp im lặng / bất giác『không nói gì / im lặng / không khỏi / bất giác』", regexp.MustCompile(`(?i)(?:không nói gì|im lặng|không khỏi|bất giác|vô thức)`)},
}

var (
	sentenceSplit = regexp.MustCompile(`[.!?。！？\n]+`)
	openingTimeRe = regexp.MustCompile(`(?i)(?:đêm|sáng sớm|bình minh|trời sáng|tỉnh dậy|ánh ban mai|suốt đêm|chạng vạng)`)
	titlePrefixRe = regexp.MustCompile(`(?i)^#{0,2}\s*(?:[Cc]hương\s+\d+|[Cc]hương\s+[零〇一二三四五六七八九十百千万\d]+|第[零〇一二三四五六七八九十百千万\d]+章)`)
)

// shortEndingRunes — dòng cuối không vượt quá số ký tự này thì tính là "kết thúc ngắn".
const shortEndingRunes = 30

// Compute tính thống kê phong cách toàn tác phẩm; trả về nil nếu số chương chưa đủ.
func Compute(in Input) *Stats {
	n := len(in.Chapters)
	if n < minChapters {
		return nil
	}
	all := strings.Join(in.Chapters, "\n")

	s := &Stats{Chapters: n}
	for _, def := range patternDefs {
		total := len(def.re.FindAllStringIndex(all, -1))
		if total == 0 {
			continue
		}
		s.Patterns = append(s.Patterns, PatternStat{
			Name:       def.name,
			Total:      total,
			PerChapter: round1(float64(total) / float64(n)),
		})
	}
	s.TopPhrases = minePhrases(recentWindow(in.Chapters), in.Stopwords)
	s.RepeatedSentences = repeatedSentences(in.Chapters)
	s.Ending = endingShape(in.Chapters)
	s.OpeningTimeRate = openingTimeRate(in.Chapters)
	s.TitleFormats = titleFormats(in.Titles)
	return s
}

func recentWindow(chapters []string) []string {
	if len(chapters) <= phraseWindow {
		return chapters
	}
	return chapters[len(chapters)-phraseWindow:]
}

// minePhrases khai thác các cụm từ 2-4 từ xuất hiện nhiều trong recentWindow chương gần nhất.
// Lọc: hư từ ở đầu/cuối, trùng danh từ riêng (stopwords).
func minePhrases(chapters []string, stopwords []string) []PhraseStat {
	text := strings.Join(chapters, "\n")
	threshold := max(8, len(chapters)/2)
	counts := make(map[string]int)

	lowerStopwords := make(map[string]bool)
	for _, w := range stopwords {
		for _, f := range strings.Fields(strings.ToLower(w)) {
			lowerStopwords[f] = true
		}
	}

	clauseSplitRe := regexp.MustCompile(`[.,;:!?。！？\n\r"“”（）()\[\]{}—–\-«»「」『』*#\x00-\x1F]+`)
	wordRe := regexp.MustCompile(`\p{L}+`)

	clauses := clauseSplitRe.Split(text, -1)
	for _, clause := range clauses {
		words := wordRe.FindAllString(strings.ToLower(clause), -1)
		for size := 2; size <= 4; size++ {
			for i := 0; i+size <= len(words); i++ {
				gramWords := words[i : i+size]
				if !validGram(gramWords, lowerStopwords) {
					continue
				}
				phrase := strings.Join(gramWords, " ")
				counts[phrase]++
			}
		}
	}

	type cand struct {
		text  string
		count int
	}
	var cands []cand
	for g, c := range counts {
		if c < threshold {
			continue
		}
		cands = append(cands, cand{g, c})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].count != cands[j].count {
			return cands[i].count > cands[j].count
		}
		if len(cands[i].text) != len(cands[j].text) {
			return len(cands[i].text) > len(cands[j].text)
		}
		return cands[i].text < cands[j].text
	})

	var out []PhraseStat
	for _, c := range cands {
		if len(out) >= 8 {
			break
		}
		dup := false
		for _, picked := range out {
			if strings.Contains(picked.Text, c.text) || strings.Contains(c.text, picked.Text) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, PhraseStat{Text: c.text, Count: c.count})
		}
	}
	return out
}

var gramEdgeStopWords = map[string]bool{
	"và": true, "nhưng": true, "thì": true, "là": true, "mà": true, "ở": true,
	"của": true, "cho": true, "để": true, "với": true, "trong": true, "ngoài": true,
	"trên": true, "dưới": true, "các": true, "những": true, "một": true, "này": true,
	"kia": true, "đó": true, "ấy": true, "tôi": true, "anh": true, "chị": true,
	"ta": true, "họ": true, "nó": true, "như": true, "bị": true, "được": true,
	"sự": true, "cuộc": true, "việc": true, "cái": true, "con": true, "người": true,
	"ra": true, "vào": true, "lên": true, "xuống": true, "đến": true, "đi": true,
	"lại": true, "qua": true, "theo": true, "từ": true, "về": true,
	"thế": true, "nào": true, "gì": true, "đâu": true, "ai": true, "sao": true,
}

func validGram(words []string, lowerStopwords map[string]bool) bool {
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		if lowerStopwords[w] {
			return false
		}
		if len(w) <= 1 && w != "y" && w != "ả" {
			return false
		}
	}
	if gramEdgeStopWords[words[0]] || gramEdgeStopWords[words[len(words)-1]] {
		return false
	}
	return true
}

// stopwordBigrams tách danh từ riêng thành các mảnh 2 ký tự: tên người thường xuất hiện
// một phần trong văn ("Cửu Uyên chắp tay" chứa "Cửu Uyên"), khớp theo tên đầy đủ sẽ bỏ sót.
// Thà lọc nghiêm hơn — bớt một cụm từ thực tế không sao, còn tên người lọt vào danh sách
// cửa miệng mới là nhiễu.
func stopwordBigrams(stopwords []string) []string {
	var grams []string
	for _, w := range stopwords {
		runes := []rune(strings.TrimSpace(w))
		if len(runes) < 2 {
			continue
		}
		for i := 0; i+2 <= len(runes); i++ {
			grams = append(grams, string(runes[i:i+2]))
		}
	}
	return grams
}

func hitStopword(gram string, stopGrams []string) bool {
	for _, g := range stopGrams {
		if strings.Contains(gram, g) {
			return true
		}
	}
	return false
}

// repeatedSentences tìm các câu ≥12 ký tự lặp lại từng chữ xuyên ≥3 chương, lấy top 5 theo số lần.
func repeatedSentences(chapters []string) []SentenceStat {
	type rec struct {
		count    int
		chapters map[int]struct{}
	}
	seen := make(map[string]*rec)
	for ci, text := range chapters {
		for _, sent := range sentenceSplit.Split(text, -1) {
			// Bỏ dấu ngoặc kép bao quanh rồi gộp: cùng một câu thoại có/không có ngoặc mở không nên tính là hai câu khác
			sent = strings.Trim(strings.TrimSpace(sent), `"""''「」『』`)
			if len([]rune(sent)) < 12 {
				continue
			}
			r := seen[sent]
			if r == nil {
				r = &rec{chapters: make(map[int]struct{})}
				seen[sent] = r
			}
			r.count++
			r.chapters[ci] = struct{}{}
		}
	}

	var out []SentenceStat
	for sent, r := range seen {
		if len(r.chapters) < 3 {
			continue
		}
		out = append(out, SentenceStat{Text: truncateRunes(sent, 40), Chapters: len(r.chapters), Count: r.count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Text < out[j].Text
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func endingShape(chapters []string) EndingStat {
	var lengths []int
	short := 0
	for _, text := range chapters {
		line := lastNonEmptyLine(text)
		if line == "" {
			continue
		}
		n := len([]rune(line))
		lengths = append(lengths, n)
		if n <= shortEndingRunes {
			short++
		}
	}
	if len(lengths) == 0 {
		return EndingStat{}
	}
	sort.Ints(lengths)
	return EndingStat{
		ShortRatio:  round2(float64(short) / float64(len(lengths))),
		MedianRunes: lengths[len(lengths)/2],
	}
}

func openingTimeRate(chapters []string) float64 {
	hit := 0
	for _, text := range chapters {
		if openingTimeRe.MatchString(firstParagraph(text)) {
			hit++
		}
	}
	return round2(float64(hit) / float64(len(chapters)))
}

func titleFormats(titles []string) *TitleStat {
	if len(titles) == 0 {
		return nil
	}
	t := &TitleStat{}
	for _, title := range titles {
		if strings.TrimSpace(title) == "" {
			continue
		}
		if titlePrefixRe.MatchString(title) {
			t.WithPrefix++
		} else {
			t.WithoutPrefix++
		}
	}
	// Chỉ báo cáo khi có dùng lẫn lộn; định dạng đồng nhất không phải vấn đề thực tế
	if t.WithPrefix == 0 || t.WithoutPrefix == 0 {
		return nil
	}
	return t
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// firstParagraph lấy dòng đầu tiên không rỗng và không phải tiêu đề Markdown
// (dòng đầu file chương thường là tiêu đề # ).
func firstParagraph(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
