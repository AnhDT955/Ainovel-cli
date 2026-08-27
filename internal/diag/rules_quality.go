package diag

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ChronicLowDimension phát hiện một chiều đánh giá liên tục thấp điểm qua nhiều chương.
func ChronicLowDimension(snap *Snapshot) []Finding {
	if len(snap.Reviews) < 2 {
		return nil
	}

	dimSums := make(map[string]float64)
	dimCounts := make(map[string]int)
	for _, r := range snap.Reviews {
		for _, d := range r.Dimensions {
			dimSums[d.Dimension] += float64(d.Score)
			dimCounts[d.Dimension]++
		}
	}

	var findings []Finding
	for name, sum := range dimSums {
		count := dimCounts[name]
		if count < 2 {
			continue
		}
		avg := sum / float64(count)
		if avg >= ThresholdDimScoreLow {
			continue
		}
		findings = append(findings, Finding{
			Rule:       "ChronicLowDimension",
			Category:   CatQuality,
			Severity:   SevWarning,
			Confidence: ConfMedium,
			AutoLevel:  AutoNone,
			Target:     "prompt.writer",
			Title:      fmt.Sprintf("Chiều [%s] liên tục thấp điểm (trung bình %.0f)", name, avg),
			Evidence:   fmt.Sprintf("Tổng cộng %d lần đánh giá, điểm trung bình %.1f", count, avg),
			Suggestion: fmt.Sprintf("Kiểm tra hướng dẫn về %s trong prompt Writer có rõ ràng không, hoặc tiêu chí chấm điểm %s trong prompt Editor có hợp lý không.", name, name),
		})
	}
	return findings
}

// ContractMissPattern phát hiện tỉ lệ thực hiện hợp đồng quá thấp.
func ContractMissPattern(snap *Snapshot) []Finding {
	if len(snap.Reviews) == 0 {
		return nil
	}

	var total, missed int
	var missedChapters []string
	for ch, r := range snap.Reviews {
		total++
		if r.ContractStatus == "partial" || r.ContractStatus == "missed" {
			missed++
			missedChapters = append(missedChapters, fmt.Sprintf("ch%d", ch))
		}
	}
	if total == 0 {
		return nil
	}
	rate := float64(missed) / float64(total)
	if rate <= ThresholdContractMissRate {
		return nil
	}
	return []Finding{{
		Rule:       "ContractMissPattern",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.writer",
		Title:      fmt.Sprintf("Tỉ lệ thực hiện hợp đồng thấp (%.0f%% không đạt)", rate*100),
		Evidence:   fmt.Sprintf("Không đạt: [%s], tổng %d/%d", strings.Join(missedChapters, ", "), missed, total),
		Suggestion: "Người viết có thể chưa đọc contract, hoặc required_beats trong contract quá khắt khe. Kiểm tra sự phối hợp giữa plan_chapter và writer.md.",
	}}
}

// HookWeakChain phát hiện điểm móc cuối chương liên tục yếu.
func HookWeakChain(snap *Snapshot) []Finding {
	if len(snap.Reviews) < ThresholdHookWeakChain {
		return nil
	}

	chapters := sortedChapterReviews(snap)
	var weakChain []int
	for _, ch := range chapters {
		review := snap.Reviews[ch]
		if review == nil || review.Scope != "chapter" {
			continue
		}
		hook := review.Dimension("hook")
		if hook == nil || hook.Score >= ThresholdHookWeakScore {
			if len(weakChain) >= ThresholdHookWeakChain {
				break
			}
			weakChain = weakChain[:0]
			continue
		}
		weakChain = append(weakChain, ch)
	}
	if len(weakChain) < ThresholdHookWeakChain {
		return nil
	}

	var parts []string
	for _, ch := range weakChain {
		if hook := snap.Reviews[ch].Dimension("hook"); hook != nil {
			parts = append(parts, fmt.Sprintf("ch%d(%d)", ch, hook.Score))
		}
	}
	return []Finding{{
		Rule:       "HookWeakChain",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.writer",
		Title:      fmt.Sprintf("Điểm móc cuối chương liên tục yếu (%d chương liên tiếp)", len(weakChain)),
		Evidence:   strings.Join(parts, ", "),
		Suggestion: "Kiểm tra việc thực thi hook_goal trong writer.md có rõ ràng không, nếu cần hãy nêu rõ mong muốn đọc tiếp của chương này trong plan_chapter, và hiệu chỉnh tiêu chí chứng minh hook của Editor.",
	}}
}

// PayoffMissPattern phát hiện các chương có payoff_points nhưng lâu dài không thực hiện được.
func PayoffMissPattern(snap *Snapshot) []Finding {
	var total, missed int
	var details []string
	for ch, plan := range snap.Plans {
		if plan == nil || len(plan.Contract.PayoffPoints) == 0 {
			continue
		}
		review := snap.Reviews[ch]
		if review == nil {
			continue
		}
		total++
		if review.ContractStatus == "partial" || review.ContractStatus == "missed" {
			missed++
			details = append(details, fmt.Sprintf("ch%d(%d payoff)", ch, len(plan.Contract.PayoffPoints)))
		}
	}
	if total < 2 {
		return nil
	}
	rate := float64(missed) / float64(total)
	if rate <= ThresholdPayoffMissRate {
		return nil
	}
	sort.Strings(details)
	return []Finding{{
		Rule:       "PayoffMissPattern",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.writer",
		Title:      fmt.Sprintf("Tỉ lệ thực hiện nhịp truyện/cao trào thấp (%.0f%% không đạt)", rate*100),
		Evidence:   fmt.Sprintf("Các chương chưa thực hiện: [%s], tổng %d/%d", strings.Join(details, ", "), missed, total),
		Suggestion: "Kiểm tra payoff_points trong plan_chapter có quá nhiều hoặc quá mơ hồ không, đảm bảo Người viết thực hiện rõ ràng trong bản chính thay vì chỉ phục bút.",
	}}
}

// ExcessiveRewrites phát hiện tỉ lệ viết lại quá cao.
func ExcessiveRewrites(snap *Snapshot) []Finding {
	if len(snap.Reviews) < 2 {
		return nil
	}

	var total, rewrites int
	for _, r := range snap.Reviews {
		total++
		if r.Verdict == "rewrite" {
			rewrites++
		}
	}
	if total == 0 {
		return nil
	}
	rate := float64(rewrites) / float64(total)
	if rate <= ThresholdRewriteRate {
		return nil
	}
	return []Finding{{
		Rule:       "ExcessiveRewrites",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.editor",
		Title:      fmt.Sprintf("Tỉ lệ viết lại quá cao (%d/%d = %.0f%%)", rewrites, total, rate*100),
		Evidence:   fmt.Sprintf("Tổng %d lần đánh giá, %d lần viết lại", total, rewrites),
		Suggestion: "Người viết liên tục cho ra nội dung thấp hơn ngưỡng của Biên tập viên. Kiểm tra tiêu chuẩn chất lượng trong prompt Người viết có đồng nhất với tiêu chí đánh giá của Biên tập viên không.",
	}}
}

// WordCountAnomaly phát hiện số từ chương bất thường.
func WordCountAnomaly(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.ChapterWordCounts) < 3 {
		return nil
	}
	wc := snap.Progress.ChapterWordCounts

	var sum float64
	for _, w := range wc {
		sum += float64(w)
	}
	avg := sum / float64(len(wc))
	if avg == 0 {
		return nil
	}

	var anomalies []string
	for ch, w := range wc {
		ratio := float64(w) / avg
		if ratio < ThresholdWordShortRatio {
			anomalies = append(anomalies, fmt.Sprintf("ch%d(%d từ,%.0f%%)", ch, w, ratio*100))
		} else if ratio > ThresholdWordLongRatio {
			anomalies = append(anomalies, fmt.Sprintf("ch%d(%d từ,%.0f%%)", ch, w, ratio*100))
		}
	}
	if len(anomalies) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "WordCountAnomaly",
		Category:   CatQuality,
		Severity:   SevInfo,
		Confidence: ConfLow,
		AutoLevel:  AutoNone,
		Target:     "context.window",
		Title:      fmt.Sprintf("Số từ chương bất thường (trung bình %d từ)", int(math.Round(avg))),
		Evidence:   strings.Join(anomalies, "; "),
		Suggestion: "Chương quá ngắn có thể do đầu ra bị cắt xén (giới hạn token), chương quá dài có thể tiêu tốn quá nhiều cửa sổ ngữ cảnh. Kiểm tra cấu hình max_tokens của mô hình.",
	}}
}

func sortedChapterReviews(snap *Snapshot) []int {
	chapters := make([]int, 0, len(snap.Reviews))
	for ch := range snap.Reviews {
		chapters = append(chapters, ch)
	}
	sort.Ints(chapters)
	return chapters
}
