package store

import (
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// WorldStore quản lý dòng thời gian, phục bút, quan hệ nhân vật, thay đổi trạng thái, quy tắc thế giới, quy tắc phong cách, đánh giá và bàn giao.
type WorldStore struct{ io *IO }

func NewWorldStore(io *IO) *WorldStore { return &WorldStore{io: io} }

// ── Dòng thời gian ──

// SaveTimeline ghi toàn bộ timeline.json + timeline.md (ghi nguyên tử).
func (s *WorldStore) SaveTimeline(events []domain.TimelineEvent) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("timeline.json", events); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("timeline.md", renderTimeline(events))
	})
}

// LoadTimeline đọc dòng thời gian.
func (s *WorldStore) LoadTimeline() ([]domain.TimelineEvent, error) {
	var events []domain.TimelineEvent
	if err := s.io.ReadJSON("timeline.json", &events); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return events, nil
}

// AppendTimelineEvents thêm các sự kiện vào dòng thời gian.
func (s *WorldStore) AppendTimelineEvents(newEvents []domain.TimelineEvent) error {
	return s.io.WithWriteLock(func() error {
		var existing []domain.TimelineEvent
		if err := s.io.ReadJSONUnlocked("timeline.json", &existing); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		all := append(existing, newEvents...)
		if err := s.io.WriteJSONUnlocked("timeline.json", all); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("timeline.md", renderTimeline(all))
	})
}

// LoadRecentTimeline trả về các sự kiện dòng thời gian trong window chương gần nhất.
func (s *WorldStore) LoadRecentTimeline(current, window int) ([]domain.TimelineEvent, error) {
	all, err := s.LoadTimeline()
	if err != nil {
		return nil, err
	}
	minCh := max(current-window, 1)
	var filtered []domain.TimelineEvent
	for _, e := range all {
		if e.Chapter >= minCh {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

// ── Phục bút ──

// SaveForeshadowLedger ghi toàn bộ foreshadow_ledger.json + foreshadow_ledger.md (ghi nguyên tử).
func (s *WorldStore) SaveForeshadowLedger(entries []domain.ForeshadowEntry) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("foreshadow_ledger.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("foreshadow_ledger.md", renderForeshadow(entries))
	})
}

// LoadForeshadowLedger đọc sổ theo dõi phục bút.
func (s *WorldStore) LoadForeshadowLedger() ([]domain.ForeshadowEntry, error) {
	var entries []domain.ForeshadowEntry
	if err := s.io.ReadJSON("foreshadow_ledger.json", &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// UpdateForeshadow áp dụng hàng loạt các thao tác tăng dần trên phục bút.
func (s *WorldStore) UpdateForeshadow(chapter int, updates []domain.ForeshadowUpdate) error {
	return s.io.WithWriteLock(func() error {
		var entries []domain.ForeshadowEntry
		if err := s.io.ReadJSONUnlocked("foreshadow_ledger.json", &entries); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		idx := make(map[string]int, len(entries))
		for i, e := range entries {
			idx[e.ID] = i
		}
		for _, u := range updates {
			switch u.Action {
			case "plant":
				idx[u.ID] = len(entries)
				entries = append(entries, domain.ForeshadowEntry{
					ID:          u.ID,
					Description: u.Description,
					PlantedAt:   chapter,
					Status:      "planted",
				})
			case "advance":
				if i, ok := idx[u.ID]; ok {
					entries[i].Status = "advanced"
				}
			case "resolve":
				if i, ok := idx[u.ID]; ok {
					entries[i].Status = "resolved"
					entries[i].ResolvedAt = chapter
				}
			}
		}
		if err := s.io.WriteJSONUnlocked("foreshadow_ledger.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("foreshadow_ledger.md", renderForeshadow(entries))
	})
}

// LoadActiveForeshadow trả về các mục phục bút chưa được giải quyết.
func (s *WorldStore) LoadActiveForeshadow() ([]domain.ForeshadowEntry, error) {
	all, err := s.LoadForeshadowLedger()
	if err != nil {
		return nil, err
	}
	var active []domain.ForeshadowEntry
	for _, e := range all {
		if e.Status != "resolved" {
			active = append(active, e)
		}
	}
	return active, nil
}

// ── Quan hệ nhân vật ──

// SaveRelationships ghi toàn bộ relationship_state.json + relationship_state.md (ghi nguyên tử).
func (s *WorldStore) SaveRelationships(entries []domain.RelationshipEntry) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("relationship_state.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("relationship_state.md", renderRelationships(entries))
	})
}

// LoadRelationships đọc trạng thái quan hệ nhân vật.
func (s *WorldStore) LoadRelationships() ([]domain.RelationshipEntry, error) {
	var entries []domain.RelationshipEntry
	if err := s.io.ReadJSON("relationship_state.json", &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// UpdateRelationships hợp nhất các thay đổi quan hệ.
func (s *WorldStore) UpdateRelationships(changes []domain.RelationshipEntry) error {
	return s.io.WithWriteLock(func() error {
		var existing []domain.RelationshipEntry
		if err := s.io.ReadJSONUnlocked("relationship_state.json", &existing); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		idx := make(map[string]int, len(existing))
		for i, e := range existing {
			idx[pairKey(e.CharacterA, e.CharacterB)] = i
		}
		for _, c := range changes {
			key := pairKey(c.CharacterA, c.CharacterB)
			if i, ok := idx[key]; ok {
				existing[i].Relation = c.Relation
				existing[i].Chapter = c.Chapter
			} else {
				idx[key] = len(existing)
				existing = append(existing, c)
			}
		}
		if err := s.io.WriteJSONUnlocked("relationship_state.json", existing); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("relationship_state.md", renderRelationships(existing))
	})
}

// ── Thay đổi trạng thái ──

// AppendStateChanges thêm các thay đổi trạng thái nhân vật.
func (s *WorldStore) AppendStateChanges(changes []domain.StateChange) error {
	return s.io.WithWriteLock(func() error {
		var existing []domain.StateChange
		if err := s.io.ReadJSONUnlocked("meta/state_changes.json", &existing); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
		return s.io.WriteJSONUnlocked("meta/state_changes.json", append(existing, changes...))
	})
}

// LoadStateChanges đọc toàn bộ bản ghi thay đổi trạng thái.
func (s *WorldStore) LoadStateChanges() ([]domain.StateChange, error) {
	var changes []domain.StateChange
	if err := s.io.ReadJSON("meta/state_changes.json", &changes); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return changes, nil
}

// ── Quy tắc thế giới ──

// SaveWorldRules ghi toàn bộ world_rules.json + world_rules.md (ghi nguyên tử).
func (s *WorldStore) SaveWorldRules(rules []domain.WorldRule) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("world_rules.json", rules); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("world_rules.md", renderWorldRules(rules))
	})
}

// LoadWorldRules đọc quy tắc thế giới.
func (s *WorldStore) LoadWorldRules() ([]domain.WorldRule, error) {
	var rules []domain.WorldRule
	if err := s.io.ReadJSON("world_rules.json", &rules); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return rules, nil
}

// ── Quy tắc phong cách ──

// SaveStyleRules lưu quy tắc phong cách viết.
func (s *WorldStore) SaveStyleRules(rules domain.WritingStyleRules) error {
	return s.io.WriteJSON("meta/style_rules.json", rules)
}

// LoadStyleRules đọc quy tắc phong cách viết.
func (s *WorldStore) LoadStyleRules() (*domain.WritingStyleRules, error) {
	var rules domain.WritingStyleRules
	if err := s.io.ReadJSON("meta/style_rules.json", &rules); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &rules, nil
}

// ── Đánh giá ──

// ReviewArtifact trả về đường dẫn tương đối nơi một đánh giá được lưu, theo scope.
//
// Ba scope ba file riêng: đánh giá cung và đánh giá chương ĐỀU gắn với chương cuối cung, nên nếu dùng
// chung tên file thì cái sau xoá sổ cái trước trong im lặng. Hậu quả có thật: người dùng yêu cầu sửa
// chương cuối cung → editor lưu save_review(scope="chapter") đè lên bản đánh giá cung → HasArcReview
// thành false → Router ra lệnh đánh giá lại cả cung → verdict polish → xếp hàng đánh bóng lại toàn bộ
// cung. Vòng đó tự thoát được, nhưng đốt sạch một lượt đánh bóng vô ích.
func ReviewArtifact(chapter int, scope string) string {
	switch scope {
	case "global":
		return fmt.Sprintf("reviews/%02d-global.json", chapter)
	case "arc":
		return fmt.Sprintf("reviews/%02d-arc.json", chapter)
	default:
		return fmt.Sprintf("reviews/%02d.json", chapter)
	}
}

// SaveReview lưu kết quả đánh giá.
func (s *WorldStore) SaveReview(r domain.ReviewEntry) error {
	return s.io.WriteJSON(ReviewArtifact(r.Chapter, r.Scope), r)
}

// HasArcReview kiểm tra xem chương được chỉ định (chương cuối cung truyện) đã lưu đánh giá scope=arc chưa.
// Nếu đọc thất bại thì coi như "chưa lưu", để Router thiên về phái lại thay vì bỏ qua.
func (s *WorldStore) HasArcReview(chapter int) bool {
	rv, err := s.loadReviewFile(ReviewArtifact(chapter, "arc"))
	if err == nil && rv != nil {
		return true
	}
	// Đường lùi cho sách viết trước khi tách tên file: đánh giá cung khi đó nằm chung ở reviews/NN.json.
	// Không có bước di trú nào — chỉ cần đọc được cả hai chỗ là sách cũ resume đúng, và mọi lần ghi mới
	// đều rơi vào tên file mới.
	rv, err = s.LoadReview(chapter)
	return err == nil && rv != nil && rv.Scope == "arc"
}

// LoadReview đọc kết quả đánh giá cấp chương.
func (s *WorldStore) LoadReview(chapter int) (*domain.ReviewEntry, error) {
	return s.loadReviewFile(ReviewArtifact(chapter, "chapter"))
}

// LoadArcReview đọc kết quả đánh giá cấp cung gắn với chương cuối cung.
func (s *WorldStore) LoadArcReview(chapter int) (*domain.ReviewEntry, error) {
	r, err := s.loadReviewFile(ReviewArtifact(chapter, "arc"))
	if err != nil || r != nil {
		return r, err
	}
	// Cùng đường lùi với HasArcReview.
	r, err = s.LoadReview(chapter)
	if err != nil || r == nil || r.Scope != "arc" {
		return nil, err
	}
	return r, nil
}

func (s *WorldStore) loadReviewFile(rel string) (*domain.ReviewEntry, error) {
	var r domain.ReviewEntry
	if err := s.io.ReadJSON(rel, &r); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// LoadLastReview đọc lần đánh giá toàn cục gần nhất.
func (s *WorldStore) LoadLastReview(fromChapter int) (*domain.ReviewEntry, error) {
	for ch := fromChapter; ch >= 1; ch-- {
		var r domain.ReviewEntry
		if err := s.io.ReadJSON(fmt.Sprintf("reviews/%02d-global.json", ch), &r); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		return &r, nil
	}
	return nil, nil
}

// ── render helpers ──

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func renderTimeline(events []domain.TimelineEvent) string {
	var b strings.Builder
	b.WriteString("# Dòng thời gian\n\n")
	for _, e := range events {
		chars := ""
		if len(e.Characters) > 0 {
			chars = "（" + strings.Join(e.Characters, "、") + "）"
		}
		fmt.Fprintf(&b, "- **Chương %d [%s]**：%s%s\n", e.Chapter, e.Time, e.Event, chars)
	}
	return b.String()
}

func renderForeshadow(entries []domain.ForeshadowEntry) string {
	var b strings.Builder
	b.WriteString("# Sổ theo dõi phục bút\n\n")
	for _, e := range entries {
		status := e.Status
		if e.ResolvedAt > 0 {
			status = fmt.Sprintf("đã giải quyết（chương %d）", e.ResolvedAt)
		}
		fmt.Fprintf(&b, "- **[%s]** %s — đặt tại chương %d, trạng thái：%s\n",
			e.ID, e.Description, e.PlantedAt, status)
	}
	return b.String()
}

func renderRelationships(entries []domain.RelationshipEntry) string {
	var b strings.Builder
	b.WriteString("# Quan hệ nhân vật\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "- **%s ↔ %s**：%s（chương %d）\n",
			e.CharacterA, e.CharacterB, e.Relation, e.Chapter)
	}
	return b.String()
}

func renderWorldRules(rules []domain.WorldRule) string {
	grouped := make(map[string][]domain.WorldRule)
	var order []string
	for _, r := range rules {
		cat := r.Category
		if cat == "" {
			cat = "other"
		}
		if _, exists := grouped[cat]; !exists {
			order = append(order, cat)
		}
		grouped[cat] = append(grouped[cat], r)
	}

	var b strings.Builder
	b.WriteString("# Quy tắc thế giới quan\n\n")
	for _, cat := range order {
		fmt.Fprintf(&b, "## %s\n\n", cat)
		for _, r := range grouped[cat] {
			fmt.Fprintf(&b, "- **Quy tắc**：%s\n", r.Rule)
			if r.Boundary != "" {
				fmt.Fprintf(&b, "  - Giới hạn：%s\n", r.Boundary)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
