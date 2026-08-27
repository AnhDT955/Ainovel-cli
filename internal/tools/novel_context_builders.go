package tools

import (
	"fmt"
	"slices"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/stylestat"
)

type contextBuildState struct {
	chapter         int
	profile         domain.ContextProfile
	progress        *domain.Progress
	runMeta         *domain.RunMeta
	currentEntry    *domain.OutlineEntry
	chapterPlan     *domain.ChapterPlan
	storyThreads    []domain.RecallItem
	foreshadow      []domain.ForeshadowEntry
	relationships   []domain.RelationshipEntry
	allStateChanges []domain.StateChange
	styleRules      *domain.WritingStyleRules
}

type chapterContextEnvelope struct {
	Working    map[string]any
	Episodic   map[string]any
	References map[string]any
	Selected   map[string]any
}

type architectContextEnvelope struct {
	Planning   map[string]any
	Foundation map[string]any
	References map[string]any
}

func newChapterContextEnvelope() chapterContextEnvelope {
	return chapterContextEnvelope{
		Working:    make(map[string]any),
		Episodic:   make(map[string]any),
		References: make(map[string]any),
		Selected:   make(map[string]any),
	}
}

func newArchitectContextEnvelope() architectContextEnvelope {
	return architectContextEnvelope{
		Planning:   make(map[string]any),
		Foundation: make(map[string]any),
		References: make(map[string]any),
	}
}

func (e chapterContextEnvelope) apply(result map[string]any) {
	// Gộp thay vì ghi đè: luồng chương của Execute sẽ apply hai phong bì liên tiếp (seed + buildChapterContext),
	// nếu gán toàn bộ thì lần apply thứ hai sẽ xóa nội dung container của seed, các canonical path
	// như working_memory.* sẽ thành con trỏ trỏ vào không khí, mô hình chỉ còn dựa vào bản sao
	// ở tầng trên để xử lý mơ hồ.
	mergeEnvelopeSection(result, "working_memory", e.Working)
	mergeEnvelopeSection(result, "episodic_memory", e.Episodic)
	mergeEnvelopeSection(result, "reference_pack", e.References)
	if len(e.Selected) > 0 {
		mergeEnvelopeSection(result, "selected_memory", e.Selected)
	}
	mergeContextSection(result, e.Working)
	mergeContextSection(result, e.Episodic)
	mergeContextSection(result, e.References)
}

// mergeEnvelopeSection gộp section vào container hiện có của result[key]; nếu container chưa tồn tại thì gán trực tiếp.
func mergeEnvelopeSection(result map[string]any, key string, section map[string]any) {
	if existing, ok := result[key].(map[string]any); ok {
		for k, v := range section {
			existing[k] = v
		}
		return
	}
	result[key] = section
}

func (e architectContextEnvelope) apply(result map[string]any) {
	result["planning_memory"] = e.Planning
	result["foundation_memory"] = e.Foundation
	result["reference_pack"] = e.References
	mergeContextSection(result, e.Planning)
	mergeContextSection(result, e.Foundation)
	mergeContextSection(result, e.References)
}

func mergeContextSection(result map[string]any, section map[string]any) {
	for key, value := range section {
		result[key] = value
	}
}

// buildProgressStatus chỉ trả về tóm tắt tiến độ khi Điều phối viên gọi (không truyền chapter),
// Người viết không cần thông tin này, tránh gây nhiễu trong quá trình viết.
func (t *ContextTool) buildProgressStatus(result map[string]any) {
	progress, err := t.store.Progress.Load()
	if err != nil || progress == nil {
		return
	}
	status := map[string]any{
		"phase":              string(progress.Phase),
		"flow":               string(progress.Flow),
		"completed_chapters": len(progress.CompletedChapters),
		"total_chapters":     progress.TotalChapters,
		"next_chapter":       progress.NextChapter(),
		"total_word_count":   progress.TotalWordCount,
	}
	if progress.InProgressChapter > 0 {
		status["in_progress_chapter"] = progress.InProgressChapter
	}
	if len(progress.PendingRewrites) > 0 {
		status["pending_rewrites"] = progress.PendingRewrites
		status["rewrite_reason"] = progress.RewriteReason
	}
	if progress.Layered {
		status["layered"] = true
		status["current_volume"] = progress.CurrentVolume
		status["current_arc"] = progress.CurrentArc
	}
	if progress.Phase == domain.PhaseComplete {
		status["finished"] = true
	}
	result["progress_status"] = status
}

// buildUserRules tiêm Bundle đã gộp vào working_memory.user_rules (canonical path).
//
// Tiêm một điểm duy nhất: dù Người viết / Biên tập viên / Kiến trúc sư / Điều phối viên gọi novel_context
// theo luồng nào, đều lấy được preferences nhất quán tại working_memory.user_rules.
// Luồng architect ban đầu không có working_memory, hàm này sẽ tạo mới theo nhu cầu (chỉ chứa user_rules);
// luồng chapter > 0 đã có working_memory sẵn, gán trực tiếp vào đó.
//
// Dù Bundle rỗng vẫn tiêm, giữ ổn định trường dữ liệu, tránh LLM gặp user_rules=null rồi rẽ nhánh bất thường.
//
// Chiến lược tiêm: chỉ cho LLM thấy structured + preferences — đây mới là hai mục cần tuân theo khi sáng tác.
// sources / conflicts là thông tin chẩn đoán (để người dùng tự debug xung đột), không đưa vào LLM;
// CLI sẽ hiển thị chúng trên panel chẩn đoán khi cần.
func (t *ContextTool) buildUserRules(result map[string]any) {
	bundle := rules.Merge(rules.Load(t.rulesOpts))
	payload := map[string]any{
		"structured":  bundle.Structured,
		"preferences": bundle.Preferences,
	}
	working, ok := result["working_memory"].(map[string]any)
	if !ok {
		working = map[string]any{}
		result["working_memory"] = working
	}
	working["user_rules"] = payload
}

// buildUserDirectives tiêm yêu cầu sáng tác dài hạn của người dùng vào working_memory.user_directives (canonical path).
//
// Cũng là tiêm một điểm duy nhất như buildUserRules: dù Người viết / Biên tập viên / Kiến trúc sư /
// Điều phối viên đi theo luồng nào đều lấy được danh sách nhất quán. Danh sách rỗng cũng tiêm [],
// giữ ổn định trường dữ liệu (theo tiền lệ của user_rules),
// đồng thời để bài kiểm tra tính nhất quán của con trỏ prompt có thể phân tích tự nhiên.
// Hình dạng từng mục xem tại directiveFacts.
func (t *ContextTool) buildUserDirectives(result map[string]any, warn func(string, error)) {
	list, err := t.store.Directives.Load()
	if err != nil {
		warn("user_directives", err)
		return
	}
	working, ok := result["working_memory"].(map[string]any)
	if !ok {
		working = map[string]any{}
		result["working_memory"] = working
	}
	working["user_directives"] = directiveFacts(list)
}

func (t *ContextTool) buildSimulationProfile(result map[string]any, sectionKey string, warn func(string, error)) {
	profile, err := t.store.Simulation.Load()
	if err != nil {
		warn("simulation_profile", err)
		return
	}
	compact := domain.CompactSimulationProfile(profile)
	if compact == nil {
		return
	}
	section, ok := result[sectionKey].(map[string]any)
	if !ok {
		section = map[string]any{}
		result[sectionKey] = section
	}
	section["simulation_profile"] = compact
	result["simulation_profile"] = true
}

func (t *ContextTool) buildBaseContext(result map[string]any, warn func(string, error)) {
	t.buildRequirementBrief(result, warn)
	if premise, err := t.store.Outline.LoadPremise(); err == nil && premise != "" {
		result["premise"] = premise
		if sections := parsePremiseSections(premise); len(sections) > 0 {
			result["premise_sections"] = sections
		}
		tier := domain.PlanningTier("")
		if meta, err := t.store.RunMeta.Load(); err == nil && meta != nil {
			tier = meta.PlanningTier
		}
		result["premise_structure"] = premiseStructure(premise, tier)
	} else {
		warn("premise", err)
	}
	if outline, err := t.store.Outline.LoadOutline(); err == nil && outline != nil {
		result["outline"] = outline
	} else {
		warn("outline", err)
	}
	if rules, err := t.store.World.LoadWorldRules(); err == nil && len(rules) > 0 {
		result["world_rules"] = rules
	} else {
		warn("world_rules", err)
	}
}

func (t *ContextTool) prepareChapterContext(chapter int, envelope *chapterContextEnvelope, warn func(string, error)) contextBuildState {
	state := contextBuildState{
		chapter: chapter,
		profile: domain.NewContextProfile(0),
	}

	progress, err := t.store.Progress.Load()
	warn("progress", err)
	runMeta, err := t.store.RunMeta.Load()
	warn("run_meta", err)
	state.progress = progress
	state.runMeta = runMeta

	if runMeta != nil && runMeta.PlanningTier != "" {
		envelope.Episodic["planning_tier"] = runMeta.PlanningTier
	}
	if progress != nil && progress.TotalChapters > 0 {
		state.profile = domain.NewContextProfile(progress.TotalChapters)
	}
	if progress == nil || !progress.Layered {
		state.profile.Layered = false
	}

	currentEntry, currentEntryErr := t.store.Outline.GetChapterOutline(chapter)
	if currentEntryErr == nil {
		envelope.Working["current_chapter_outline"] = currentEntry
	} else {
		warn("current_chapter_outline", currentEntryErr)
	}
	state.currentEntry = currentEntry

	chapterPlan, chapterPlanErr := t.store.Drafts.LoadChapterPlan(chapter)
	if chapterPlanErr == nil && chapterPlan != nil {
		envelope.Working["chapter_plan"] = chapterPlan
		if len(chapterPlan.Contract.RequiredBeats) > 0 ||
			len(chapterPlan.Contract.ForbiddenMoves) > 0 ||
			len(chapterPlan.Contract.ContinuityChecks) > 0 ||
			len(chapterPlan.Contract.EvaluationFocus) > 0 ||
			chapterPlan.Contract.EmotionTarget != "" ||
			len(chapterPlan.Contract.PayoffPoints) > 0 ||
			chapterPlan.Contract.HookGoal != "" {
			envelope.Working["chapter_contract"] = chapterPlan.Contract
		}
	} else {
		warn("chapter_plan", chapterPlanErr)
	}
	state.chapterPlan = chapterPlan

	// Xác định có đang viết lại chương này không: quyết định novel_context có bổ sung "sự kiện dành riêng cho viết lại" hay không.
	isRewrite := progress != nil && slices.Contains(progress.PendingRewrites, chapter)

	// Phơi bày thực tế bản nháp đã tồn tại chưa: để Người viết khi được tái phân công có thể tự quyết
	// bỏ qua viết lại hay ghi đè. Chỉ phơi bày exists + word_count, không tiêm nội dung
	// (nội dung để Người viết dùng read_chapter kéo về khi cần).
	if _, draftWords, draftErr := t.store.Drafts.LoadChapterContent(chapter); draftErr == nil && draftWords > 0 {
		envelope.Working["chapter_draft"] = map[string]any{
			"exists":     true,
			"word_count": draftWords,
		}
	} else if draftErr != nil {
		warn("chapter_draft", draftErr)
	}

	// Khi viết lại, chuyển "lý do thay đổi + vị trí cần sửa" cho Người viết: lý do lấy từ hàng đợi
	// làm lại, phê bình cụ thể lấy từ đánh giá chương đó (selectReviewLessons chỉ thu hồi
	// chapter-1..chapter-3, vừa đúng bỏ sót bản thân chương hiện tại, mà Người viết cũng
	// không có công cụ đọc đánh giá). Nội dung không tiêm ở đây — giữ nguyên quy ước
	// "nội dung kéo về bằng read_chapter khi cần".
	if isRewrite {
		brief := map[string]any{"reason": progress.RewriteReason}
		if review, reviewErr := t.store.World.LoadReview(chapter); reviewErr == nil && review != nil {
			if review.Summary != "" {
				brief["review_summary"] = review.Summary
			}
			if len(review.Issues) > 0 {
				brief["issues"] = review.Issues
			}
			if len(review.ContractMisses) > 0 {
				brief["contract_misses"] = review.ContractMisses
			}
		} else if reviewErr != nil {
			warn("rewrite_review", reviewErr)
		}
		envelope.Working["rewrite_brief"] = brief
	}

	foreshadow, foreshadowErr := t.store.World.LoadActiveForeshadow()
	warn("foreshadow_ledger", foreshadowErr)
	state.foreshadow = foreshadow

	relationships, relErr := t.store.World.LoadRelationships()
	warn("relationship_state", relErr)
	if len(relationships) > 0 {
		envelope.Episodic["relationship_state"] = relationships
	}
	state.relationships = relationships

	allStateChanges, scErr := t.store.World.LoadStateChanges()
	warn("recent_state_changes", scErr)
	state.allStateChanges = allStateChanges
	if len(allStateChanges) > 0 {
		start := max(chapter-2, 1)
		var recent []domain.StateChange
		for _, c := range allStateChanges {
			if c.Chapter >= start && c.Chapter < chapter {
				recent = append(recent, c)
			}
		}
		if len(recent) > 0 {
			envelope.Episodic["recent_state_changes"] = recent
		}
	}

	styleRules, styleErr := t.store.World.LoadStyleRules()
	warn("style_rules", styleErr)
	state.styleRules = styleRules
	state.storyThreads = t.selectStoryThreads(state)
	if len(state.storyThreads) > 0 && len(state.storyThreads) < storyThreadRecallMinSelected {
		state.storyThreads = nil
	}

	return state
}

func (t *ContextTool) buildChapterContext(result map[string]any, state contextBuildState, warn func(string, error)) {
	envelope := newChapterContextEnvelope()
	result["memory_policy"] = domain.NewChapterMemoryPolicy(state.progress, state.profile, state.currentEntry != nil)

	if state.profile.Layered {
		t.loadLayeredCharacters(envelope.Episodic, state.chapter, warn)
	} else {
		t.loadFilteredCharacters(envelope.Episodic, state.chapter, warn)
	}

	t.buildChapterEpisodicMemory(&envelope, state, warn)
	t.buildChapterWorkingMemory(&envelope, state, warn)
	t.buildChapterReferencePack(&envelope, state)
	t.buildChapterSelectedMemory(&envelope, state, warn)
	t.buildStyleStats(&envelope, state)
	envelope.apply(result)
}

// buildStyleStats thống kê phong cách toàn tập ở tất cả các chương đã hoàn thành,
// tiêm vào episodic_memory.style_stats.
// Cửa sổ đánh giá trong cung truyện tự nhiên mù với "tic câu xuất hiện vài chục lần mỗi chương,
// cấu trúc cuối chương đồng dạng, lặp từ xuyên chương" — chỉ thống kê toàn tập mới phơi bày được.
// Thống kê do code xử lý (tính xác định), phán quyết do LLM đảm nhận (Biên tập viên chấm điểm
// theo con số ở chiều aesthetic, Người viết dựa đó tự tránh). Khi chưa đủ chương, stylestat
// trả về nil, không tiêm.
func (t *ContextTool) buildStyleStats(envelope *chapterContextEnvelope, state contextBuildState) {
	if state.progress == nil || len(state.progress.CompletedChapters) == 0 {
		return
	}
	completed := slices.Clone(state.progress.CompletedChapters)
	slices.Sort(completed)
	chapters := make([]string, 0, len(completed))
	for _, ch := range completed {
		// Bỏ qua nếu đọc một chương riêng lẻ thất bại: thống kê là sự kiện best-effort, không vì thiếu một chương mà từ bỏ toàn cục
		if text, err := t.store.Drafts.LoadChapterText(ch); err == nil && text != "" {
			chapters = append(chapters, text)
		}
	}

	var titles []string
	if outline, err := t.store.Outline.LoadOutline(); err == nil {
		for _, entry := range outline {
			titles = append(titles, entry.Title)
		}
	}

	stats := stylestat.Compute(stylestat.Input{
		Chapters:  chapters,
		Titles:    titles,
		Stopwords: t.styleStopwords(),
	})
	if stats == nil {
		return
	}
	envelope.Episodic["style_stats"] = stats
}

// styleStopwords thu thập tên nhân vật và bí danh để lọc khi khai thác cụm từ — tên xuất hiện tự nhiên có tần suất cao, không phải vấn đề phong cách.
func (t *ContextTool) styleStopwords() []string {
	var words []string
	if chars, err := t.store.Characters.Load(); err == nil {
		for _, c := range chars {
			words = append(words, c.Name)
			words = append(words, c.Aliases...)
		}
	}
	if cast, err := t.store.Cast.RecentActive(50); err == nil {
		for _, e := range cast {
			words = append(words, e.Name)
			words = append(words, e.Aliases...)
		}
	}
	return words
}

func (t *ContextTool) buildChapterWorkingMemory(envelope *chapterContextEnvelope, state contextBuildState, warn func(string, error)) {
	if next, err := t.store.Outline.GetChapterOutline(state.chapter + 1); err == nil && next != nil {
		envelope.Working["next_chapter_outline"] = next
	}

	if state.profile.Layered {
		t.loadLayeredSummaries(envelope.Working, state.chapter, state.profile.SummaryWindow, warn)
	} else {
		if summaries, err := t.store.Summaries.LoadRecentSummaries(state.chapter, state.profile.SummaryWindow); err == nil && len(summaries) > 0 {
			envelope.Working["recent_summaries"] = summaries
		} else {
			warn("recent_summaries", err)
		}
	}

	if timeline, err := t.store.World.LoadRecentTimeline(state.chapter, state.profile.TimelineWindow); err == nil && len(timeline) > 0 {
		envelope.Working["timeline"] = timeline
	} else {
		warn("timeline", err)
	}

	if state.progress != nil {
		checkpoint := map[string]any{
			"in_progress_chapter": state.progress.InProgressChapter,
		}
		if len(state.progress.StrandHistory) > 0 {
			checkpoint["strand_history"] = state.progress.StrandHistory
		}
		if len(state.progress.HookHistory) > 0 {
			checkpoint["hook_history"] = state.progress.HookHistory
		}
		envelope.Working["checkpoint"] = checkpoint
	}

	if state.chapter > 1 {
		if prevText, err := t.store.Drafts.LoadChapterText(state.chapter - 1); err == nil && prevText != "" {
			runes := []rune(prevText)
			if len(runes) > 800 {
				runes = runes[len(runes)-800:]
			}
			envelope.Working["previous_tail"] = string(runes)
		}
	}
}

func (t *ContextTool) buildChapterSelectedMemory(envelope *chapterContextEnvelope, state contextBuildState, warn func(string, error)) {
	if len(state.storyThreads) > 0 {
		envelope.Selected["story_threads"] = state.storyThreads
	}
	if lessons := t.selectReviewLessons(state.chapter, warn); len(lessons) > 0 {
		envelope.Selected["review_lessons"] = lessons
	}
}

func (t *ContextTool) buildChapterEpisodicMemory(envelope *chapterContextEnvelope, state contextBuildState, warn func(string, error)) {
	if len(state.foreshadow) > 0 && len(state.storyThreads) == 0 {
		envelope.Episodic["foreshadow_ledger"] = state.foreshadow
	}

	// Danh sách nhân vật phụ: thu hồi các nhân vật phụ hoạt động gần đây, giúp Người viết
	// giữ nhất quán giọng điệu/vai trò khi đưa nhân vật cũ trở lại.
	// Không thu hồi toàn bộ (truyện dài sẽ phình to), chỉ lấy N nhân vật hoạt động gần nhất,
	// sắp xếp giảm dần theo LastSeenChapter.
	if recentCast, err := t.store.Cast.RecentActive(15); err == nil && len(recentCast) > 0 {
		simplified := make([]map[string]any, 0, len(recentCast))
		for _, e := range recentCast {
			item := map[string]any{
				"name":             e.Name,
				"first_seen":       e.FirstSeenChapter,
				"last_seen":        e.LastSeenChapter,
				"appearance_count": e.AppearanceCount,
			}
			if e.BriefRole != "" {
				item["brief_role"] = e.BriefRole
			}
			if len(e.Aliases) > 0 {
				item["aliases"] = e.Aliases
			}
			simplified = append(simplified, item)
		}
		envelope.Episodic["recent_cast"] = simplified
	} else if err != nil {
		warn("recent_cast", err)
	}

	if state.progress != nil && state.progress.TotalChapters > 30 && state.currentEntry != nil {
		if related := t.buildRelatedChapters(
			state.chapter,
			state.currentEntry,
			state.foreshadow,
			state.relationships,
			state.allStateChanges,
		); len(related) > 0 {
			envelope.Episodic["related_chapters"] = related
		}
	}

	if state.profile.Layered && state.progress != nil {
		// Vị trí phải tra theo CHƯƠNG ĐANG DỰNG NGỮ CẢNH, không lấy từ Progress.CurrentVolume/CurrentArc:
		// hai trường đó mô tả cung của chương vừa commit xong, nên ở mọi biên cung chúng trỏ về cung TRƯỚC.
		// Xem ghi chú tại store.ArcPosition.
		p, err := t.store.Outline.LocateChapterPosition(state.chapter)
		if err != nil {
			warn("layered_outline", err)
		}
		var pos map[string]any
		switch {
		case p != nil:
			pos = map[string]any{
				"volume":             p.Volume,
				"arc":                p.Arc,
				"volume_title":       p.VolumeTitle,
				"volume_theme":       p.VolumeTheme,
				"arc_title":          p.ArcTitle,
				"arc_goal":           p.ArcGoal,
				"arc_total_chapters": p.ArcChapters,
				"arc_chapter_index":  p.ChapterIndex,
			}
		default:
			// Chương chưa có chỗ trong đề cương (chờ expand_arc/append_volume). Nói thẳng là chưa biết vị trí,
			// thay vì bịa ra cung của chương trước — Người viết cần thấy đúng thực tế đó.
			pos = map[string]any{
				"volume":         state.progress.CurrentVolume,
				"arc":            state.progress.CurrentArc,
				"outline_status": fmt.Sprintf("chương %d chưa có mục trong đề cương phân lớp; tập/cung ở trên là vị trí của chương đã viết gần nhất", state.chapter),
			}
		}
		envelope.Episodic["position"] = pos
	}
}

func (t *ContextTool) buildChapterReferencePack(envelope *chapterContextEnvelope, state contextBuildState) {
	if state.styleRules != nil {
		envelope.References["style_rules"] = state.styleRules
	} else {
		var maxCompleted int
		if state.progress != nil {
			maxCompleted = maxCompletedChapter(state.progress.CompletedChapters)
		}
		if anchors := t.store.Drafts.ExtractStyleAnchors(3, maxCompleted); len(anchors) > 0 {
			envelope.References["style_anchors"] = anchors
		}

		if state.currentEntry != nil {
			var voiceSamples []map[string]any
			chars, _ := t.store.Characters.Load()
			for _, c := range chars {
				if c.Tier == "secondary" || c.Tier == "decorative" {
					continue
				}
				samples := t.store.Drafts.ExtractDialogue(c.Name, c.Aliases, 3, maxCompleted)
				if len(samples) > 0 {
					voiceSamples = append(voiceSamples, map[string]any{
						"character": c.Name,
						"samples":   samples,
					})
				}
				if len(voiceSamples) >= 5 {
					break
				}
			}
			if len(voiceSamples) > 0 {
				envelope.References["voice_samples"] = voiceSamples
			}
		}
	}

	envelope.References["references"] = t.writerReferences(state.chapter)
}

func (t *ContextTool) buildArchitectContext(result map[string]any, warn func(string, error)) {
	envelope := newArchitectContextEnvelope()
	result["memory_policy"] = domain.NewArchitectMemoryPolicy()
	t.buildArchitectPlanning(&envelope, warn)
	t.buildArchitectFoundation(&envelope, warn)
	t.buildArchitectReferences(&envelope, warn)
	envelope.apply(result)
}

func (t *ContextTool) buildArchitectPlanning(envelope *architectContextEnvelope, warn func(string, error)) {
	runMeta, err := t.store.RunMeta.Load()
	warn("run_meta", err)
	if runMeta != nil && runMeta.PlanningTier != "" {
		envelope.Planning["planning_tier"] = runMeta.PlanningTier
	}

	var layered []domain.VolumeOutline
	if l, err := t.store.Outline.LoadLayeredOutline(); err == nil && len(l) > 0 {
		layered = l
		envelope.Planning["layered_outline"] = layered
		var skeletonArcs []map[string]any
		for _, v := range layered {
			for _, a := range v.Arcs {
				if !a.IsExpanded() {
					skeletonArcs = append(skeletonArcs, map[string]any{
						"volume":             v.Index,
						"arc":                a.Index,
						"title":              a.Title,
						"goal":               a.Goal,
						"estimated_chapters": a.EstimatedChapters,
					})
				}
			}
		}
		if len(skeletonArcs) > 0 {
			envelope.Planning["skeleton_arcs"] = skeletonArcs
		}
	} else {
		warn("layered_outline", err)
	}

	var compass *domain.StoryCompass
	if c, err := t.store.Outline.LoadCompass(); err == nil && c != nil {
		compass = c
		envelope.Planning["compass"] = compass
	} else {
		warn("compass", err)
	}
	if volSummaries, err := t.store.Summaries.LoadAllVolumeSummaries(); err == nil && len(volSummaries) > 0 {
		envelope.Planning["volume_summaries"] = volSummaries
	} else {
		warn("volume_summaries", err)
	}

	// completion_signals tập trung các sự kiện then chốt về "toàn tập đã nên kết thúc chưa",
	// giúp Kiến trúc sư nhìn thấy bức tranh đối chiếu ngay khi phán quyết complete_book / append_volume.
	// Nếu để rải rác trong progress / compass / foreshadow / layered_outline, LLM dễ bỏ sót khi tự tổng hợp.
	envelope.Planning["completion_signals"] = t.completionSignals(layered, compass)
}

func (t *ContextTool) completionSignals(layered []domain.VolumeOutline, compass *domain.StoryCompass) map[string]any {
	signals := map[string]any{}
	// Ràng buộc cứng: tối thiểu 300 chương mới cho phép kết thúc, từ 400 chương có thể kết thúc linh hoạt
	const minChaptersRequired = 300
	const gracefulEndThreshold = 400
	signals["min_chapters_required"] = minChaptersRequired
	if progress, _ := t.store.Progress.Load(); progress != nil {
		completed := len(progress.CompletedChapters)
		signals["completed_chapters"] = completed
		signals["total_word_count"] = progress.TotalWordCount
		signals["phase"] = string(progress.Phase)
		signals["can_end_gracefully"] = completed >= gracefulEndThreshold
		signals["chapters_until_min"] = max(minChaptersRequired-completed, 0)
	}
	if len(layered) > 0 {
		signals["planned_chapters"] = len(domain.FlattenOutline(layered))
		signals["volumes_total"] = len(layered)
	}
	if compass != nil {
		if compass.EstimatedScale != "" {
			signals["compass_estimated_scale"] = compass.EstimatedScale
		}
		signals["open_threads_count"] = len(compass.OpenThreads)
	}
	if active, err := t.store.World.LoadActiveForeshadow(); err == nil {
		signals["active_foreshadow_count"] = len(active)
	}
	return signals
}

// buildRequirementBrief nạp brief.md — yêu cầu gốc của người dùng — vào ngữ cảnh.
//
// Nạp cho CẢ hai đường (Kiến trúc sư và Người viết), có chủ đích. Kiến trúc sư cần nó để dựng bộ nền
// đúng; Người viết cần nó vì bộ nền có thể đã bỏ sót chi tiết, và trước đây Người viết hoàn toàn không
// có đường nào chạm tới yêu cầu người dùng — nó chỉ thấy premise/outline/world_rules do Kiến trúc sư
// viết lại. Khi bộ nền rỗng, Người viết buộc phải bịa cả thế giới ra.
//
// Không nằm trong trimOrder của trimByBudget: đây là đầu vào do người dùng sở hữu, cắt nó đi là quay
// lại đúng lỗi cũ. Brief thực tế chỉ vài KB.
func (t *ContextTool) buildRequirementBrief(result map[string]any, warn func(string, error)) {
	brief, err := t.store.Brief.Load()
	if err != nil {
		warn("brief", err)
		return
	}
	if strings.TrimSpace(brief) == "" {
		return
	}
	result["requirement_brief"] = brief
	result["requirement_brief_usage"] = "Yêu cầu sáng tác NGUYÊN VĂN do người dùng cung cấp — đây là nguồn sự thật cao nhất. " +
		"Mọi tên riêng, hệ thống, cấp bậc và thế lực nêu ở đây phải xuất hiện đúng nguyên văn trong truyện. " +
		"Khi bộ nền (premise/characters/world_rules) mâu thuẫn hoặc bỏ sót so với tài liệu này, hãy bám theo tài liệu này."
}

func (t *ContextTool) buildArchitectFoundation(envelope *architectContextEnvelope, warn func(string, error)) {
	if premise, err := t.store.Outline.LoadPremise(); err == nil && premise != "" {
		if sections := parsePremiseSections(premise); len(sections) > 0 {
			envelope.Foundation["premise_sections"] = sections
		}
		tier := domain.PlanningTier("")
		if meta, err := t.store.RunMeta.Load(); err == nil && meta != nil {
			tier = meta.PlanningTier
		}
		envelope.Foundation["premise_structure"] = premiseStructure(premise, tier)
	} else {
		warn("premise", err)
	}

	if chars, err := t.store.Characters.Load(); err == nil && chars != nil {
		envelope.Foundation["characters"] = chars
	} else {
		warn("characters", err)
	}

	if snapshots, err := t.store.Characters.LoadLatestSnapshots(); err == nil && len(snapshots) > 0 {
		envelope.Foundation["character_snapshots"] = snapshots
	} else {
		warn("character_snapshots", err)
	}
	if rules, err := t.store.World.LoadWorldRules(); err == nil && len(rules) > 0 {
		envelope.Foundation["world_rules"] = rules
	} else {
		warn("world_rules", err)
	}
	if foreshadow, err := t.store.World.LoadActiveForeshadow(); err == nil && len(foreshadow) > 0 {
		envelope.Foundation["foreshadow_ledger"] = foreshadow
	} else {
		warn("foreshadow_ledger", err)
	}
	envelope.Foundation["foundation_status"] = t.foundationStatus()
}

func (t *ContextTool) buildArchitectReferences(envelope *architectContextEnvelope, warn func(string, error)) {
	if styleRules, err := t.store.World.LoadStyleRules(); err == nil && styleRules != nil {
		envelope.References["style_rules"] = styleRules
	} else {
		warn("style_rules", err)
	}

	envelope.References["references"] = t.architectReferences()
}
