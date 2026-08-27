package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

// TestLoadEmpty kiểm tra hành vi đọc rỗng thống nhất cho tất cả các domain.
func TestLoadEmpty(t *testing.T) {
	s := newTestStore(t)

	if v, err := s.World.LoadTimeline(); err != nil || v != nil {
		t.Errorf("Timeline: want (nil, nil), got (%v, %v)", v, err)
	}
	if v, err := s.World.LoadForeshadowLedger(); err != nil || v != nil {
		t.Errorf("Foreshadow: want (nil, nil), got (%v, %v)", v, err)
	}
	if v, err := s.World.LoadRelationships(); err != nil || v != nil {
		t.Errorf("Relationships: want (nil, nil), got (%v, %v)", v, err)
	}
	if v, err := s.World.LoadStateChanges(); err != nil || v != nil {
		t.Errorf("StateChanges: want (nil, nil), got (%v, %v)", v, err)
	}
	if v, err := s.World.LoadStyleRules(); err != nil || v != nil {
		t.Errorf("StyleRules: want (nil, nil), got (%v, %v)", v, err)
	}
	if v, err := s.World.LoadWorldRules(); err != nil || v != nil {
		t.Errorf("WorldRules: want (nil, nil), got (%v, %v)", v, err)
	}
	if v, err := s.World.LoadReview(99); err != nil || v != nil {
		t.Errorf("Review: want (nil, nil), got (%v, %v)", v, err)
	}
	if v, err := s.World.LoadLastReview(10); err != nil || v != nil {
		t.Errorf("LastReview: want (nil, nil), got (%v, %v)", v, err)
	}
}

// ── Timeline ──

func TestTimeline_Append(t *testing.T) {
	s := newTestStore(t)

	if err := s.World.AppendTimelineEvents([]domain.TimelineEvent{
		{Chapter: 1, Time: "清晨", Event: "事件一"},
	}); err != nil {
		t.Fatalf("batch1: %v", err)
	}
	if err := s.World.AppendTimelineEvents([]domain.TimelineEvent{
		{Chapter: 2, Time: "午后", Event: "事件二"},
		{Chapter: 3, Time: "傍晚", Event: "事件三"},
	}); err != nil {
		t.Fatalf("batch2: %v", err)
	}

	loaded, err := s.World.LoadTimeline()
	if err != nil {
		t.Fatalf("LoadTimeline: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("want 3, got %d", len(loaded))
	}
	if loaded[2].Event != "事件三" {
		t.Errorf("third event: %+v", loaded[2])
	}
}

func TestTimeline_LoadRecent(t *testing.T) {
	s := newTestStore(t)
	_ = s.World.SaveTimeline([]domain.TimelineEvent{
		{Chapter: 1}, {Chapter: 3}, {Chapter: 5}, {Chapter: 7},
	})

	for _, tt := range []struct {
		current, window, want int
	}{
		{7, 10, 4}, // tất cả
		{7, 3, 2},  // ch5,ch7
		{5, 2, 3},  // ch3,ch5,ch7
	} {
		got, _ := s.World.LoadRecentTimeline(tt.current, tt.window)
		if len(got) != tt.want {
			t.Errorf("LoadRecent(%d,%d): want %d, got %d", tt.current, tt.window, tt.want, len(got))
		}
	}
}

// ── Foreshadow ──

func TestForeshadow_UpdateLifecycle(t *testing.T) {
	s := newTestStore(t)

	// plant
	_ = s.World.UpdateForeshadow(1, []domain.ForeshadowUpdate{
		{ID: "f1", Action: "plant", Description: "黑影"},
		{ID: "f2", Action: "plant", Description: "断剑"},
	})
	// advance f1, resolve f2
	_ = s.World.UpdateForeshadow(3, []domain.ForeshadowUpdate{
		{ID: "f1", Action: "advance"},
		{ID: "f2", Action: "resolve"},
	})

	all, _ := s.World.LoadForeshadowLedger()
	if len(all) != 2 {
		t.Fatalf("want 2, got %d", len(all))
	}
	if all[0].Status != "advanced" {
		t.Errorf("f1: want advanced, got %s", all[0].Status)
	}
	if all[1].Status != "resolved" || all[1].ResolvedAt != 3 {
		t.Errorf("f2: want resolved@3, got %s@%d", all[1].Status, all[1].ResolvedAt)
	}

	// LoadActive nên loại trừ các mục đã resolved
	active, _ := s.World.LoadActiveForeshadow()
	if len(active) != 1 || active[0].ID != "f1" {
		t.Errorf("active: want [f1], got %v", active)
	}
}

// ── Relationships ──

func TestRelationships_UpdateMerge(t *testing.T) {
	s := newTestStore(t)
	_ = s.World.SaveRelationships([]domain.RelationshipEntry{
		{CharacterA: "张三", CharacterB: "李四", Relation: "师徒", Chapter: 1},
	})

	// Cập nhật bản ghi đã có + thêm mới
	_ = s.World.UpdateRelationships([]domain.RelationshipEntry{
		{CharacterA: "张三", CharacterB: "李四", Relation: "挚友", Chapter: 5},
		{CharacterA: "王五", CharacterB: "赵六", Relation: "同门", Chapter: 5},
	})

	loaded, _ := s.World.LoadRelationships()
	if len(loaded) != 2 {
		t.Fatalf("want 2, got %d", len(loaded))
	}
	if loaded[0].Relation != "挚友" {
		t.Errorf("update failed: %+v", loaded[0])
	}
}

func TestRelationships_PairKeySymmetry(t *testing.T) {
	s := newTestStore(t)
	_ = s.World.SaveRelationships([]domain.RelationshipEntry{
		{CharacterA: "张三", CharacterB: "李四", Relation: "师徒", Chapter: 1},
	})
	// Cập nhật theo thứ tự B-A, phải khớp với cùng một bản ghi
	_ = s.World.UpdateRelationships([]domain.RelationshipEntry{
		{CharacterA: "李四", CharacterB: "张三", Relation: "反目", Chapter: 3},
	})

	loaded, _ := s.World.LoadRelationships()
	if len(loaded) != 1 {
		t.Fatalf("want 1 (merged), got %d", len(loaded))
	}
	if loaded[0].Relation != "反目" {
		t.Errorf("not updated: %+v", loaded[0])
	}
}

// ── StateChanges ──

func TestStateChanges_Append(t *testing.T) {
	s := newTestStore(t)
	_ = s.World.AppendStateChanges([]domain.StateChange{
		{Chapter: 1, Entity: "张三", Field: "realm", NewValue: "练气期"},
	})
	_ = s.World.AppendStateChanges([]domain.StateChange{
		{Chapter: 3, Entity: "张三", Field: "realm", OldValue: "练气期", NewValue: "筑基期"},
	})

	loaded, _ := s.World.LoadStateChanges()
	if len(loaded) != 2 {
		t.Fatalf("want 2, got %d", len(loaded))
	}
	if loaded[1].NewValue != "筑基期" {
		t.Errorf("second: %+v", loaded[1])
	}
}

// ── StyleRules ──

func TestStyleRules_SaveAndLoad(t *testing.T) {
	s := newTestStore(t)
	rules := domain.WritingStyleRules{
		Volume: 1, Arc: 2,
		Prose:    []string{"短句为主"},
		Dialogue: []domain.CharacterVoice{{Name: "张三", Rules: []string{"粗犷"}}},
		Taboos:   []string{"不用网络用语"},
	}
	_ = s.World.SaveStyleRules(rules)

	loaded, _ := s.World.LoadStyleRules()
	if loaded == nil || loaded.Volume != 1 || len(loaded.Dialogue) != 1 {
		t.Errorf("roundtrip failed: %+v", loaded)
	}
}

// ── Reviews ──

func TestReview_SaveAndLoad(t *testing.T) {
	s := newTestStore(t)
	_ = s.World.SaveReview(domain.ReviewEntry{Chapter: 3, Scope: "chapter", Verdict: "polish"})

	loaded, _ := s.World.LoadReview(3)
	if loaded == nil || loaded.Verdict != "polish" {
		t.Errorf("chapter review: %+v", loaded)
	}
}

func TestReview_GlobalScopeIsolation(t *testing.T) {
	s := newTestStore(t)
	_ = s.World.SaveReview(domain.ReviewEntry{Chapter: 5, Scope: "global", Verdict: "accept"})

	// Load theo chapter không được tìm thấy global review
	if got, _ := s.World.LoadReview(5); got != nil {
		t.Errorf("chapter load should not find global: %+v", got)
	}
}

func TestReview_LoadLastReview(t *testing.T) {
	s := newTestStore(t)
	for _, ch := range []int{2, 5, 8} {
		_ = s.World.SaveReview(domain.ReviewEntry{Chapter: ch, Scope: "global", Verdict: "accept"})
	}

	for _, tt := range []struct {
		from, want int
	}{
		{10, 8}, {5, 5}, {3, 2},
	} {
		got, _ := s.World.LoadLastReview(tt.from)
		if got == nil || got.Chapter != tt.want {
			t.Errorf("LoadLastReview(%d): want ch%d, got %+v", tt.from, tt.want, got)
		}
	}
	// from=1 không tìm thấy
	if got, _ := s.World.LoadLastReview(1); got != nil {
		t.Errorf("from=1 should be nil, got %+v", got)
	}
}

// ── WorldRules ──

func TestWorldRules_SaveAndLoad(t *testing.T) {
	s := newTestStore(t)
	rules := []domain.WorldRule{
		{Category: "magic", Rule: "法术消耗精神力", Boundary: "精神力耗尽会昏迷"},
		{Category: "society", Rule: "贵族拥有裁判权", Boundary: "不得越权"},
	}
	_ = s.World.SaveWorldRules(rules)

	if _, err := os.Stat(filepath.Join(s.Dir(), "world_rules.json")); err != nil {
		t.Fatalf("json not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "world_rules.md")); err != nil {
		t.Fatalf("md not created: %v", err)
	}

	loaded, _ := s.World.LoadWorldRules()
	if len(loaded) != 2 || loaded[0].Rule != "法术消耗精神力" {
		t.Errorf("roundtrip: %+v", loaded)
	}
}

func TestRenderWorldRules(t *testing.T) {
	md := renderWorldRules([]domain.WorldRule{
		{Category: "magic", Rule: "法术消耗精神力", Boundary: "精神力耗尽会昏迷"},
		{Category: "society", Rule: "贵族有裁判权"},
		{Category: "magic", Rule: "禁咒需三人", Boundary: "单人施放会死"},
	})

	// Nhóm magic phải xuất hiện trước society
	if strings.Index(md, "## magic") >= strings.Index(md, "## society") {
		t.Error("magic should appear before society")
	}
	if !strings.Contains(md, "Giới hạn：精神力耗尽会昏迷") {
		t.Error("missing boundary")
	}
	// Không có boundary thì không được render dòng boundary rỗng
	if strings.Contains(md, "Giới hạn：\n") {
		t.Error("empty boundary rendered")
	}
}

// Hồi quy: đánh giá cung và đánh giá chương đều gắn với chương cuối cung. Khi dùng chung một tên file,
// người dùng yêu cầu sửa chương cuối cung → editor lưu scope="chapter" → xoá sổ bản đánh giá cung →
// HasArcReview false → Router ra lệnh đánh giá lại cả cung → verdict polish → đánh bóng lại toàn bộ cung.
func TestArcReviewSurvivesChapterReview(t *testing.T) {
	s := newTestStore(t)

	arcReview := domain.ReviewEntry{Chapter: 10, Scope: "arc", Verdict: "accept", Summary: "đánh giá cung 1"}
	if err := s.World.SaveReview(arcReview); err != nil {
		t.Fatalf("SaveReview arc: %v", err)
	}
	if !s.World.HasArcReview(10) {
		t.Fatal("vừa lưu xong mà HasArcReview đã false")
	}

	// Người dùng yêu cầu sửa chương 10 → đánh giá cấp chương cho đúng chương đó
	if err := s.World.SaveReview(domain.ReviewEntry{
		Chapter: 10, Scope: "chapter", Verdict: "rewrite", Summary: "sửa chương 10",
	}); err != nil {
		t.Fatalf("SaveReview chapter: %v", err)
	}

	if !s.World.HasArcReview(10) {
		t.Fatal("đánh giá cấp chương đã xoá sổ đánh giá cung: Router sẽ bắt đánh giá lại cả cung")
	}
	arc, err := s.World.LoadArcReview(10)
	if err != nil || arc == nil || arc.Summary != "đánh giá cung 1" {
		t.Fatalf("LoadArcReview = (%+v, %v), mong đợi bản đánh giá cung nguyên vẹn", arc, err)
	}
	ch, err := s.World.LoadReview(10)
	if err != nil || ch == nil || ch.Summary != "sửa chương 10" {
		t.Fatalf("LoadReview = (%+v, %v), mong đợi bản đánh giá chương", ch, err)
	}
}

// Sách viết trước khi tách tên file có đánh giá cung nằm chung ở reviews/NN.json; resume phải vẫn thấy nó,
// nếu không Router bắt đánh giá lại cung ngay khi mở lại sách cũ.
func TestHasArcReviewFallsBackToLegacyPath(t *testing.T) {
	s := newTestStore(t)

	legacy := domain.ReviewEntry{Chapter: 10, Scope: "arc", Verdict: "accept", Summary: "cũ"}
	if err := s.World.io.WriteJSON("reviews/10.json", legacy); err != nil {
		t.Fatalf("write legacy review: %v", err)
	}
	if !s.World.HasArcReview(10) {
		t.Fatal("không đọc được đánh giá cung của sách cũ nằm ở reviews/10.json")
	}

	// Bản ghi cấp chương ở đường cũ thì KHÔNG được tính là đánh giá cung.
	if err := s.World.io.WriteJSON("reviews/11.json", domain.ReviewEntry{
		Chapter: 11, Scope: "chapter", Verdict: "accept", Summary: "chương",
	}); err != nil {
		t.Fatalf("write legacy chapter review: %v", err)
	}
	if s.World.HasArcReview(11) {
		t.Fatal("đánh giá cấp chương bị nhận nhầm là đánh giá cung")
	}
}
