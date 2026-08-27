package diag

import (
	"errors"
	"fmt"
	"os"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Snapshot là ảnh chụp chỉ đọc toàn bộ sản phẩm trong thư mục output.
// Tất cả hàm quy tắc chỉ nhận Snapshot, không truy cập hệ thống file trực tiếp.
type Snapshot struct {
	Progress      *domain.Progress
	RunMeta       *domain.RunMeta
	Compass       *domain.StoryCompass
	Outline       []domain.OutlineEntry
	Volumes       []domain.VolumeOutline
	Characters    []domain.Character
	CastLedger    []domain.CastEntry
	WorldRules    []domain.WorldRule
	Timeline      []domain.TimelineEvent
	Foreshadow    []domain.ForeshadowEntry
	Relationships []domain.RelationshipEntry
	StateChanges  []domain.StateChange
	StyleRules    *domain.WritingStyleRules
	Reviews       map[int]*domain.ReviewEntry
	Plans         map[int]*domain.ChapterPlan
	Summaries     map[int]*domain.ChapterSummary

	LoadErrors []string // Lỗi tải không phải NotExist — phân biệt "không có dữ liệu" và "đọc lỗi"
}

// Load đọc toàn bộ sản phẩm từ store và xây dựng ảnh chụp chỉ đọc.
// File không tồn tại được coi là "không có dữ liệu" (trường giữ giá trị zero); các lỗi khác ghi vào LoadErrors.
func Load(s *store.Store) Snapshot {
	snap := Snapshot{
		Reviews:   make(map[int]*domain.ReviewEntry),
		Plans:     make(map[int]*domain.ChapterPlan),
		Summaries: make(map[int]*domain.ChapterSummary),
	}

	check := func(name string, err error) {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			snap.LoadErrors = append(snap.LoadErrors, fmt.Sprintf("%s: %v", name, err))
		}
	}

	var err error
	snap.Progress, err = s.Progress.Load()
	check("progress", err)
	snap.RunMeta, err = s.RunMeta.Load()
	check("run_meta", err)
	snap.Compass, err = s.Outline.LoadCompass()
	check("compass", err)
	snap.Outline, err = s.Outline.LoadOutline()
	check("outline", err)
	snap.Volumes, err = s.Outline.LoadLayeredOutline()
	check("volumes", err)
	snap.Characters, err = s.Characters.Load()
	check("characters", err)
	snap.CastLedger, err = s.Cast.Load()
	check("cast_ledger", err)
	snap.WorldRules, err = s.World.LoadWorldRules()
	check("world_rules", err)
	snap.Timeline, err = s.World.LoadTimeline()
	check("timeline", err)
	snap.Foreshadow, err = s.World.LoadForeshadowLedger()
	check("foreshadow", err)
	snap.Relationships, err = s.World.LoadRelationships()
	check("relationships", err)
	snap.StateChanges, err = s.World.LoadStateChanges()
	check("state_changes", err)
	snap.StyleRules, err = s.World.LoadStyleRules()
	check("style_rules", err)

	if snap.Progress != nil {
		for _, ch := range snap.Progress.CompletedChapters {
			if plan, err := s.Drafts.LoadChapterPlan(ch); err == nil && plan != nil {
				snap.Plans[ch] = plan
			} else {
				check(fmt.Sprintf("plan_ch%d", ch), err)
			}
			if summary, err := s.Summaries.LoadSummary(ch); err == nil && summary != nil {
				snap.Summaries[ch] = summary
			} else {
				check(fmt.Sprintf("summary_ch%d", ch), err)
			}
			if review, err := s.World.LoadReview(ch); err == nil && review != nil {
				snap.Reviews[ch] = review
			} else {
				check(fmt.Sprintf("review_ch%d", ch), err)
			}
		}
	}

	return snap
}

// CompletedCount trả về số chương đã hoàn thành (truy cập an toàn).
func (s *Snapshot) CompletedCount() int {
	if s.Progress == nil {
		return 0
	}
	return len(s.Progress.CompletedChapters)
}

// LatestCompleted trả về số chương đã hoàn thành lớn nhất; nếu không có thì trả về 0.
func (s *Snapshot) LatestCompleted() int {
	if s.Progress == nil {
		return 0
	}
	max := 0
	for _, ch := range s.Progress.CompletedChapters {
		if ch > max {
			max = ch
		}
	}
	return max
}
