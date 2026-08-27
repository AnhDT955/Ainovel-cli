package diag

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// StaleForeshadow phát hiện các phục bút đã trồng lâu mà chưa được đẩy tiến.
func StaleForeshadow(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Foreshadow) == 0 {
		return nil
	}
	latest := snap.LatestCompleted()
	threshold := staleForeshadowThreshold(snap.CompletedCount())

	var stale []string
	for _, f := range snap.Foreshadow {
		if f.Status != "planted" {
			continue
		}
		gap := latest - f.PlantedAt
		if gap > threshold {
			stale = append(stale, fmt.Sprintf("%s(ch%d đặt phục bút, đã qua %d chương)", f.ID, f.PlantedAt, gap))
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "StaleForeshadow",
		Category:   CatPlanning,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.foreshadow",
		Title:      fmt.Sprintf("Phục bút đình trệ: %d mục vượt quá %d chương chưa được đẩy tiến", len(stale), threshold),
		Evidence:   strings.Join(stale, "; "),
		Suggestion: "Tải phục bút nhắc nhở trong novel_context có thể chưa có hiệu lực, hoặc prompt Người viết thiếu hướng dẫn đẩy tiến phục bút. Kiểm tra foreshadow_ledger và logic nạp ngữ cảnh.",
	}}
}

// CompassDrift phát hiện tình trạng compass không được cập nhật lâu.
func CompassDrift(snap *Snapshot) []Finding {
	if snap.Progress == nil || !snap.Progress.Layered {
		return nil
	}
	if snap.Compass == nil {
		if snap.CompletedCount() > 5 {
			return []Finding{{
				Rule:       "CompassDrift",
				Category:   CatPlanning,
				Severity:   SevWarning,
				Confidence: ConfMedium,
				AutoLevel:  AutoNone,
				Target:     "prompt.architect",
				Title:      "Chế độ truyện dài thiếu compass",
				Evidence:   fmt.Sprintf("layered=true, completed=%d, compass=nil", snap.CompletedCount()),
				Suggestion: "Kiến trúc sư phải tạo compass trong lần lập kế hoạch ban đầu. Kiểm tra architect-long.md xem có chứa lệnh tạo compass không.",
			}}
		}
		return nil
	}

	gap := snap.LatestCompleted() - snap.Compass.LastUpdated
	if gap <= ThresholdCompassDrift {
		return nil
	}
	return []Finding{{
		Rule:       "CompassDrift",
		Category:   CatPlanning,
		Severity:   SevInfo,
		Confidence: ConfLow,
		AutoLevel:  AutoNone,
		Target:     "prompt.architect",
		Title:      fmt.Sprintf("Compass chưa được cập nhật trong %d chương", gap),
		Evidence:   fmt.Sprintf("last_updated=ch%d, latest=ch%d, open_threads=%d", snap.Compass.LastUpdated, snap.LatestCompleted(), len(snap.Compass.OpenThreads)),
		Suggestion: "Kiến trúc sư nên cập nhật compass tại ranh giới cung/tập. Kiểm tra architect-long.md xem có chứa lệnh cập nhật compass không.",
	}}
}

// OutlineExhausted phát hiện tình trạng đề cương đã cạn nhưng tiểu thuyết chưa hoàn tất.
func OutlineExhausted(snap *Snapshot) []Finding {
	if snap.Progress == nil {
		return nil
	}
	p := snap.Progress
	if p.Phase == domain.PhaseComplete || p.Phase == domain.PhaseInit {
		return nil
	}

	completed := snap.CompletedCount()
	if completed == 0 {
		return nil
	}

	outlinedCount := p.TotalChapters
	if outlinedCount <= 0 {
		outlinedCount = len(snap.Outline)
	}
	if outlinedCount <= 0 {
		return nil
	}

	if completed < outlinedCount {
		return nil
	}

	return []Finding{{
		Rule:       "OutlineExhausted",
		Category:   CatPlanning,
		Severity:   SevCritical,
		Confidence: ConfHigh,
		AutoLevel:  AutoSafe,
		Target:     "runtime.recovery",
		Title:      fmt.Sprintf("Đề cương cạn kiệt: đã hoàn thành %d chương >= đã lập kế hoạch %d chương", completed, outlinedCount),
		Evidence:   fmt.Sprintf("phase=%s, completed=%d, outlined=%d", p.Phase, completed, outlinedCount),
		Suggestion: "Tín hiệu mở rộng/tập mới có thể chưa được kích hoạt. Kiểm tra chiến lược lưu chương phía host và logic khôi phục, xác nhận phát hiện ranh giới cung, expand_arc hoặc append_volume có hoạt động bình thường không.",
	}}
}

// MissingSummaries phát hiện các chương đã hoàn thành nhưng thiếu tóm tắt.
func MissingSummaries(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.CompletedChapters) == 0 {
		return nil
	}

	var missing []int
	for _, ch := range snap.Progress.CompletedChapters {
		if _, ok := snap.Summaries[ch]; !ok {
			missing = append(missing, ch)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "MissingSummaries",
		Category:   CatPlanning,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      fmt.Sprintf("Thiếu tóm tắt: %d chương không có tóm tắt", len(missing)),
		Evidence:   fmt.Sprintf("missing=[%s]", intsToStr(missing)),
		Suggestion: "Tóm tắt là yếu tố then chốt cho tính liên tục của ngữ cảnh. Kiểm tra logic ghi tóm tắt trong commit_chapter có hoạt động bình thường không.",
	}}
}
