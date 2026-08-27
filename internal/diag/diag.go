package diag

import (
	"fmt"
	"sort"

	"github.com/voocel/ainovel-cli/internal/store"
)

// ── Ngưỡng chẩn đoán ─────────────────────────────────────────────

const (
	ThresholdDimScoreLow      = 70  // ChronicLowDimension: cảnh báo khi điểm trung bình các chiều thấp hơn giá trị này
	ThresholdContractMissRate = 0.3 // ContractMissPattern: tỉ lệ hợp đồng không đạt tối đa
	ThresholdRewriteRate      = 0.5 // ExcessiveRewrites: tỉ lệ viết lại tối đa
	ThresholdWordShortRatio   = 0.4 // WordCountAnomaly: số từ thấp hơn giá trị trung bình theo tỉ lệ này bị coi là bất thường
	ThresholdWordLongRatio    = 2.5 // WordCountAnomaly: số từ cao hơn giá trị trung bình theo tỉ lệ này bị coi là bất thường
	ThresholdHookWeakScore    = 75  // HookWeakChain: hook dưới điểm này bị coi là yếu
	ThresholdHookWeakChain    = 3   // HookWeakChain: ngưỡng số chương liên tiếp yếu
	ThresholdPayoffMissRate   = 0.4 // PayoffMissPattern: tỉ lệ payoff chưa được thực hiện tối đa
	ThresholdCompassDrift     = 15  // CompassDrift: số chương tối đa chưa cập nhật la bàn
	ThresholdTimelineGapRate  = 0.3 // TimelineGaps: tỉ lệ thiếu hụt dung sai tối đa
	ThresholdForeshadowMin    = 8   // StaleForeshadow: số chương tối thiểu để phục bút bị coi là đình trệ
)

// allRules sắp xếp theo thứ tự: flow → quality → planning → context.
var allRules = []RuleFunc{
	// Flow
	InvalidPendingRewrites,
	RewritePendingPressure,
	OrphanedSteer,
	PhaseFlowMismatch,
	ChapterGaps,
	// Quality
	ChronicLowDimension,
	ContractMissPattern,
	HookWeakChain,
	PayoffMissPattern,
	ExcessiveRewrites,
	WordCountAnomaly,
	// Planning
	StaleForeshadow,
	CompassDrift,
	OutlineExhausted,
	MissingSummaries,
	// Context
	GhostCharacter,
	TimelineGaps,
	RelationshipStagnation,
}

// Analyze là điểm vào duy nhất của hệ thống chẩn đoán.
func Analyze(s *store.Store) Report {
	snap := Load(s)

	var findings []Finding
	for _, e := range snap.LoadErrors {
		findings = append(findings, Finding{
			Rule:       "LoadError",
			Category:   CatFlow,
			Severity:   SevWarning,
			Confidence: ConfHigh,
			AutoLevel:  AutoNone,
			Target:     "runtime.flow",
			Title:      fmt.Sprintf("Tải sản phẩm thất bại: %s", e),
			Suggestion: "Tệp có thể bị hỏng hoặc thiếu quyền truy cập, kết quả của các quy tắc chẩn đoán liên quan có thể không đầy đủ.",
		})
	}
	for _, rule := range allRules {
		findings = append(findings, rule(&snap)...)
	}
	sortFindings(findings)

	return Report{
		Stats:    buildStats(&snap),
		Findings: findings,
		Actions:  PlanActions(findings),
	}
}

func buildStats(snap *Snapshot) Stats {
	st := Stats{}
	if snap.Progress == nil {
		return st
	}
	p := snap.Progress
	st.CompletedChapters = len(p.CompletedChapters)
	st.TotalChapters = p.TotalChapters
	st.TotalWords = p.TotalWordCount
	st.Phase = string(p.Phase)
	st.Flow = string(p.Flow)

	if st.CompletedChapters > 0 {
		st.AvgWordsPerCh = st.TotalWords / st.CompletedChapters
	}

	if snap.RunMeta != nil {
		st.PlanningTier = string(snap.RunMeta.PlanningTier)
	}

	// Thống kê đánh giá
	st.ReviewCount = len(snap.Reviews)
	var totalScore float64
	var dimCount int
	for _, r := range snap.Reviews {
		if r.Verdict == "rewrite" {
			st.RewriteCount++
		}
		for _, d := range r.Dimensions {
			totalScore += float64(d.Score)
			dimCount++
		}
	}
	if dimCount > 0 {
		st.AvgReviewScore = totalScore / float64(dimCount)
	}

	// Thống kê phục bút
	latest := snap.LatestCompleted()
	for _, f := range snap.Foreshadow {
		if f.Status == "planted" || f.Status == "advanced" {
			st.ForeshadowOpen++
			if f.Status == "planted" && latest-f.PlantedAt > staleForeshadowThreshold(st.CompletedChapters) {
				st.ForeshadowStale++
			}
		}
	}
	return st
}

// sortFindings sắp xếp theo mức độ nghiêm trọng: critical > warning > info.
func sortFindings(findings []Finding) {
	order := map[Severity]int{SevCritical: 0, SevWarning: 1, SevInfo: 2}
	sort.SliceStable(findings, func(i, j int) bool {
		return order[findings[i].Severity] < order[findings[j].Severity]
	})
}

// staleForeshadowThreshold tính ngưỡng đình trệ phục bút dựa trên tổng số chương.
func staleForeshadowThreshold(completedChapters int) int {
	t := completedChapters / 3
	if t < ThresholdForeshadowMin {
		return ThresholdForeshadowMin
	}
	return t
}
