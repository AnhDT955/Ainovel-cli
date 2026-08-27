package store

import (
	"fmt"
	"os"
	"sync"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// Store là gốc kết hợp của quản lý trạng thái, giữ tất cả các store con.
type Store struct {
	dir string

	Progress    *ProgressStore
	Outline     *OutlineStore
	Brief       *BriefStore
	Drafts      *DraftStore
	Summaries   *SummaryStore
	RunMeta     *RunMetaStore
	Directives  *DirectivesStore
	Signals     *SignalStore
	Runtime     *RuntimeStore
	Characters  *CharacterStore
	Cast        *CastStore
	World       *WorldStore
	Checkpoints *CheckpointStore
	Sessions    *SessionStore
	Usage       *UsageStore
	Simulation  *SimulationStore

	crossMu sync.Mutex // bảo vệ các thao tác nguyên tử liên miền
}

// NewStore tạo bộ quản lý trạng thái, dir là thư mục gốc đầu ra của tiểu thuyết.
func NewStore(dir string) *Store {
	io := newIO(dir)
	outline := NewOutlineStore(io)
	return &Store{
		dir:         dir,
		Progress:    NewProgressStore(newIO(dir)),
		Outline:     outline,
		Brief:       NewBriefStore(newIO(dir)),
		Drafts:      NewDraftStore(newIO(dir)),
		Summaries:   NewSummaryStore(newIO(dir), outline),
		RunMeta:     NewRunMetaStore(newIO(dir)),
		Directives:  NewDirectivesStore(newIO(dir)),
		Signals:     NewSignalStore(newIO(dir)),
		Runtime:     NewRuntimeStore(newIO(dir)),
		Characters:  NewCharacterStore(newIO(dir), outline),
		Cast:        NewCastStore(newIO(dir)),
		World:       NewWorldStore(newIO(dir)),
		Checkpoints: NewCheckpointStore(io),
		Sessions:    NewSessionStore(newIO(dir)),
		Usage:       NewUsageStore(newIO(dir)),
		Simulation:  NewSimulationStore(newIO(dir)),
	}
}

// Dir trả về thư mục gốc đầu ra.
func (s *Store) Dir() string { return s.dir }

// CheckConsistency thực hiện một lần kiểm tra nông trên tầng dữ liệu, dùng để sinh cảnh báo khi khởi động/phục hồi.
// Hoàn toàn chỉ đọc: không sửa dữ liệu, chỉ trả về mô tả vấn đề có thể đọc được. Bên gọi quyết định cách hiển thị (log / UI).
// Để tránh chi phí IO khi quét toàn bộ thư mục, chỉ kiểm tra các điểm then chốt của Progress:
//   - Chương hoàn thành cuối cùng phải có bản thảo hoàn chỉnh trong chapters/
//   - Ở chế độ Layered, Volume/Arc hiện tại phải tìm được trong layered_outline
func (s *Store) CheckConsistency() []string {
	var warnings []string
	progress, err := s.Progress.Load()
	if err != nil || progress == nil {
		return warnings
	}
	if n := len(progress.CompletedChapters); n > 0 {
		lastCh := progress.CompletedChapters[n-1]
		if text, err := s.Drafts.LoadChapterText(lastCh); err == nil && text == "" {
			warnings = append(warnings, fmt.Sprintf("progress đánh dấu chương %d đã hoàn thành, nhưng chapters/%02d.md không tồn tại hoặc rỗng", lastCh, lastCh))
		}
	}
	// Đề cương ngắn hơn tiến độ viết: các chương đã viết không còn mục đề cương tương ứng.
	// Đây là dấu hiệu đề cương đã bị ghi đè/co lại (xem checkExpandSafety). Router có lối thoát tự động,
	// nhưng người dùng vẫn cần biết vì nội dung đề cương và chương đã viết đã lệch nhau.
	if progress.Layered {
		if volumes, err := s.Outline.LoadLayeredOutline(); err == nil && len(volumes) > 0 {
			expanded := len(domain.FlattenOutline(volumes))
			if last := progress.LatestCompleted(); expanded > 0 && last > expanded {
				warnings = append(warnings, fmt.Sprintf(
					"đề cương phân lớp chỉ phủ %d chương nhưng đã viết tới chương %d: các chương %d-%d không còn mục đề cương tương ứng, đề cương có thể đã bị ghi đè",
					expanded, last, expanded+1, last))
			}
		}
	}

	// V/A phải tra được trong đề cương. Điều kiện ">0" cũ khiến bộ kiểm tra này TỰ TẮT đúng ở trạng thái hỏng
	// nặng nhất: sự cố 2026-07 có V0/A0 trên đĩa suốt 10 chương mà không sinh nổi một cảnh báo, vì 0 không lớn
	// hơn 0. Layered=true luôn kéo theo V/A ≥ 1 (save_foundation gán ngay khi lưu đề cương), nên V/A ≤ 0 ở đây
	// tự nó đã là dữ liệu hỏng và phải được nói ra.
	if progress.Layered {
		if progress.CurrentVolume <= 0 || progress.CurrentArc <= 0 {
			warnings = append(warnings, fmt.Sprintf(
				"progress ở chế độ phân lớp nhưng vị trí hiện tại là V%d A%d: đề cương nhiều khả năng được lưu với index rỗng. "+
					"Đánh giá/tóm tắt cuối cung sẽ bị từ chối (volume và arc phải > 0) và Router lặp vô hạn tại biên cung",
				progress.CurrentVolume, progress.CurrentArc))
		} else if volumes, err := s.Outline.LoadLayeredOutline(); err == nil && len(volumes) > 0 {
			found := false
			for _, v := range volumes {
				if v.Index != progress.CurrentVolume {
					continue
				}
				for _, a := range v.Arcs {
					if a.Index == progress.CurrentArc {
						found = true
						break
					}
				}
				break
			}
			if !found {
				warnings = append(warnings, fmt.Sprintf("progress hiện tại V%d A%d không tìm thấy mục tương ứng trong đề cương phân lớp", progress.CurrentVolume, progress.CurrentArc))
			}
		}
	}
	return warnings
}

// FoundationMissing trả về các mục còn thiếu trong cài đặt nền tảng, theo thứ tự ổn định dùng cho Prompt/Reminder.
// Tier long yêu cầu thêm layered_outline; đã có layered_outline thì yêu cầu thêm compass.
//
// "Thiếu" được đo bằng NỘI DUNG chứ không bằng số phần tử. Phiên bản trước chỉ kiểm tra `len(...) == 0`,
// nên một bộ nền gồm 6 world_rules toàn chuỗi rỗng và 10 nhân vật không có description vẫn được coi là
// đã đủ: save_foundation tự đẩy phase sang writing và cả cuốn sách được viết từ khung rỗng. Cổng này
// và domain.Validate* dùng chung một định nghĩa "có nội dung thật" (xem domain/foundation.go), nên dữ
// liệu cũ đã nằm sẵn trên đĩa cũng bị bắt lại chứ không chỉ dữ liệu ghi mới.
func (s *Store) FoundationMissing() []string {
	var missing []string
	if p, _ := s.Outline.LoadPremise(); domain.ValidatePremise(p) != nil {
		missing = append(missing, "premise")
	}
	if o, _ := s.Outline.LoadOutline(); countSubstantiveOutlineEntries(o) == 0 {
		missing = append(missing, "outline")
	}
	if c, _ := s.Characters.Load(); countSubstantiveCharacters(c) == 0 {
		missing = append(missing, "characters")
	}
	if r, _ := s.World.LoadWorldRules(); countSubstantiveRules(r) == 0 {
		missing = append(missing, "world_rules")
	}
	layered, _ := s.Outline.LoadLayeredOutline()
	// Truyện dài PHẢI có đề cương phân tầng: cả cơ chế tiếp diễn của nó (expand_arc / append_volume /
	// tóm tắt cung-tập) đều đọc từ layered_outline. Một đề cương phẳng ở tier long là ngõ cụt —
	// đã gặp thật: Kiến trúc sư lưu `outline` phẳng 14 mục, cổng nhận, rồi truyện "300 chương tối
	// thiểu" bước vào giai đoạn viết với total_chapters=14 và không còn đường mở rộng.
	if meta, _ := s.RunMeta.Load(); meta != nil && meta.PlanningTier == domain.PlanningTierLong && len(layered) == 0 {
		missing = append(missing, "layered_outline")
	}
	if len(layered) > 0 {
		if c, _ := s.Outline.LoadCompass(); c == nil {
			missing = append(missing, "compass")
		}
	}
	return missing
}

func countSubstantiveOutlineEntries(entries []domain.OutlineEntry) int {
	n := 0
	for _, e := range entries {
		if e.HasSubstance() {
			n++
		}
	}
	return n
}

func countSubstantiveCharacters(chars []domain.Character) int {
	n := 0
	for _, c := range chars {
		if c.HasSubstance() {
			n++
		}
	}
	return n
}

func countSubstantiveRules(rules []domain.WorldRule) int {
	n := 0
	for _, r := range rules {
		if r.HasSubstance() {
			n++
		}
	}
	return n
}

// Init tạo cấu trúc thư mục con cần thiết.
func (s *Store) Init() error {
	return s.Progress.io.EnsureDirs([]string{
		"chapters", "summaries", "drafts", "reviews", "meta", "meta/runtime", "meta/runtime/tasks", "meta/sessions", "meta/sessions/agents",
	})
}

// ── Phương thức điều phối liên miền ──

// ExpandArc mở rộng cung truyện khung thành các chương chi tiết (Outline + Progress liên động).
//
// force bỏ qua các kiểm tra an toàn (cung đã mở rộng / cung nằm trong vùng đã viết); chỉ dành cho
// thao tác sửa chữa có chủ đích của người dùng. Agent luôn gọi với force=false.
func (s *Store) ExpandArc(volumeIdx, arcIdx int, chapters []domain.OutlineEntry, force bool) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	// Đọc tiến độ TRƯỚC khi giữ khóa Outline: expandArcUnlocked cần biết chương cuối đã viết để từ chối
	// các lần mở rộng làm dịch số vùng đã viết. Thứ tự khóa Outline → Progress được giữ nguyên bên dưới.
	latestCompleted := 0
	if p, err := s.Progress.Load(); err == nil && p != nil {
		latestCompleted = p.LatestCompleted()
	}

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	volumes, err := s.Outline.expandArcUnlocked(volumeIdx, arcIdx, chapters, latestCompleted, force)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.TotalChapters(volumes)
	return s.Progress.saveUnlocked(p)
}

// AppendVolume thêm tập mới vào cuối đề cương phân lớp (Outline + Progress liên động).
func (s *Store) AppendVolume(vol domain.VolumeOutline) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	volumes, err := s.Outline.appendVolumeUnlocked(vol)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.TotalChapters(volumes)
	return s.Progress.saveUnlocked(p)
}

// ClearHandledSteer xóa PendingSteer theo cách nguyên tử và đặt lại trạng thái FlowSteering
// (RunMeta + Progress liên động).
func (s *Store) ClearHandledSteer() error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.RunMeta.io.mu.Lock()
	defer s.RunMeta.io.mu.Unlock()

	meta, err := s.RunMeta.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if meta != nil && meta.PendingSteer != "" {
		meta.PendingSteer = ""
		if err := s.RunMeta.saveUnlocked(*meta); err != nil {
			return err
		}
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if p != nil && p.Flow == domain.FlowSteering {
		if err := domain.ValidateFlowTransition(p.Flow, domain.FlowWriting); err != nil {
			return err
		}
		p.Flow = domain.FlowWriting
		if err := s.Progress.saveUnlocked(p); err != nil {
			return err
		}
	}
	return nil
}
