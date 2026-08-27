package rules

import "slices"

// Tham số của trần mềm thích ứng. Cố ý KHÔNG đưa vào file cấu hình người dùng:
// bản chất của trần mềm là "độ dài tự nhiên do draft_chapter quyết định, code chỉ đo lại".
// Cho phép chỉnh tay sẽ dựng lại đúng bức tường cứng mà nó sinh ra để thay thế.
const (
	// CeilingSampleSize là số chương gần nhất dùng để tính trung vị.
	CeilingSampleSize = 10
	// CeilingFactor là hệ số nhân trung vị để ra trần mềm. 1.5 đủ rộng để nhịp truyện
	// dao động tự nhiên (cao trào dài hơn chương chuyển tiếp) mà vẫn bắt được chương dị thường.
	CeilingFactor = 1.5
	// CeilingMinSamples là số mẫu tối thiểu. Dưới ngưỡng này chưa có "độ dài tự nhiên"
	// để nói tới, trần mềm không tồn tại — các chương đầu tự do định hình chuẩn mực.
	CeilingMinSamples = 3
)

// WordCeiling là trần mềm thích ứng cho số từ một chương, suy ra từ chính các chương
// draft_chapter đã viết ra trước đó thay vì một con số cố định trong rules.
//
// Lý do tồn tại: chapter_words cố định (mặc định 3000-6000) sinh ra severity=error trên
// gần như mọi chương khi văn phong thực tế dài hơn — editor thấy error liền xếp chương vào
// hàng đợi đánh bóng, writer buộc phải cắt 13k về 6k và làm nát ngữ nghĩa vốn có của chương.
// Trần mềm đo độ dài THỰC TẾ của tác phẩm, nên chỉ chương lệch hẳn khỏi chuẩn mực của chính
// nó mới bị nêu, và chỉ ở mức warning.
type WordCeiling struct {
	SoftMax int `json:"soft_max"` // 0 = chưa đủ mẫu, không áp trần
	Median  int `json:"median"`
	Samples int `json:"samples"`
}

// Active cho biết trần mềm đã có hiệu lực chưa.
func (c WordCeiling) Active() bool { return c.SoftMax > 0 }

// ComputeWordCeiling tính trần mềm từ số từ các chương đã hoàn thành.
// counts nhận theo thứ tự chương tăng dần; chỉ CeilingSampleSize phần tử cuối được dùng,
// để trần bám theo văn phong hiện tại thay vì bị các chương mở đầu (thường ngắn hơn) kéo xuống.
func ComputeWordCeiling(counts []int) WordCeiling {
	sample := slices.Clone(counts)
	if len(sample) > CeilingSampleSize {
		sample = sample[len(sample)-CeilingSampleSize:]
	}
	if len(sample) < CeilingMinSamples {
		return WordCeiling{}
	}
	med := median(sample)
	if med <= 0 {
		return WordCeiling{}
	}
	return WordCeiling{
		SoftMax: int(float64(med) * CeilingFactor),
		Median:  med,
		Samples: len(sample),
	}
}

// median trả về trung vị. Dùng trung vị chứ không phải trung bình: một chương 33k từ
// kéo trung bình lên đủ để tự hợp thức hóa chính nó, trung vị thì không.
func median(xs []int) int {
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
