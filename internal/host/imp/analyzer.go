package imp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// validHookTypes / validStrands giữ nhất quán với schema của commit_chapter.
var (
	validHookTypes = map[string]bool{"crisis": true, "mystery": true, "desire": true, "emotion": true, "choice": true}
	validStrands   = map[string]bool{"quest": true, "fire": true, "constellation": true}
)

// ChapterAnalysis là sản phẩm có cấu trúc được suy ngược từ một chương đơn, các trường khớp trực tiếp với tham số đầu vào của commit_chapter.
type ChapterAnalysis struct {
	Summary             string
	Characters          []string
	KeyEvents           []string
	TimelineEvents      []domain.TimelineEvent
	ForeshadowUpdates   []domain.ForeshadowUpdate
	RelationshipChanges []domain.RelationshipEntry
	StateChanges        []domain.StateChange
	HookType            string
	DominantStrand      string
}

// AnalyzeChapter dùng một lần gọi LLM, suy ngược các dữ kiện cần thiết cho commit_chapter từ nội dung một chương đơn.
// hooksContext là ảnh chụp nhanh của bể phục bút đã biết (có thể rỗng), dùng để LLM tái sử dụng các ID hiện có.
func AnalyzeChapter(
	ctx context.Context,
	llm LLMChat,
	systemPrompt string,
	chapter int,
	chapterTitle, chapterContent string,
	premise, charactersBlock string,
	activeHooks []domain.ForeshadowEntry,
) (*ChapterAnalysis, error) {
	if llm == nil {
		return nil, fmt.Errorf("llm is nil")
	}
	if strings.TrimSpace(chapterContent) == "" {
		return nil, fmt.Errorf("chapter %d: empty content", chapter)
	}

	user := buildAnalyzerUserPrompt(chapter, chapterTitle, chapterContent, premise, charactersBlock, activeHooks)
	resp, err := llm.Generate(ctx, []agentcore.Message{
		agentcore.SystemMsg(systemPrompt),
		agentcore.UserMsg(user),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("llm generate ch%d: %w", chapter, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("ch%d: nil response", chapter)
	}
	return parseAnalyzerOutput(resp.Message.TextContent())
}

func buildAnalyzerUserPrompt(
	chapter int,
	title, content, premise, charactersBlock string,
	hooks []domain.ForeshadowEntry,
) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Hãy phân tích nội dung chương %d và xuất 9 đoạn === TAG ===.\n\n", chapter)
	if title != "" {
		fmt.Fprintf(&sb, "Tiêu đề chương：%s\n\n", title)
	}

	if strings.TrimSpace(premise) != "" {
		sb.WriteString("## Tiền đề câu chuyện (tham khảo)\n\n")
		sb.WriteString(premise)
		sb.WriteString("\n\n")
	}
	if strings.TrimSpace(charactersBlock) != "" {
		sb.WriteString("## Nhân vật đã biết (tham khảo)\n\n")
		sb.WriteString(charactersBlock)
		sb.WriteString("\n\n")
	}

	if len(hooks) > 0 {
		sb.WriteString("## Bể phục bút đã biết (hãy tái sử dụng ID, không tạo mới)\n\n")
		for _, h := range hooks {
			fmt.Fprintf(&sb, "- `%s` [%s]：%s（đặt tại chương %d）\n",
				h.ID, h.Status, h.Description, h.PlantedAt)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Nội dung chương này\n\n")
	sb.WriteString(content)
	sb.WriteString("\n")
	return sb.String()
}

func parseAnalyzerOutput(text string) (*ChapterAnalysis, error) {
	env := parseTaggedEnvelope(text)
	if env == nil {
		return nil, fmt.Errorf("no === TAG === envelope in analyzer output")
	}
	if err := requireTags(env, "SUMMARY", "CHARACTERS", "KEY_EVENTS", "HOOK_TYPE", "DOMINANT_STRAND"); err != nil {
		return nil, err
	}

	a := &ChapterAnalysis{
		Summary:        strings.TrimSpace(env["SUMMARY"]),
		HookType:       strings.ToLower(strings.TrimSpace(env["HOOK_TYPE"])),
		DominantStrand: strings.ToLower(strings.TrimSpace(env["DOMINANT_STRAND"])),
	}
	if a.Summary == "" {
		return nil, fmt.Errorf("summary is empty")
	}
	if !validHookTypes[a.HookType] {
		return nil, fmt.Errorf("invalid hook_type %q (want crisis/mystery/desire/emotion/choice)", a.HookType)
	}
	if !validStrands[a.DominantStrand] {
		return nil, fmt.Errorf("invalid dominant_strand %q (want quest/fire/constellation)", a.DominantStrand)
	}

	if err := decodeJSON("characters", env["CHARACTERS"], &a.Characters); err != nil {
		return nil, err
	}
	if len(a.Characters) == 0 {
		return nil, fmt.Errorf("characters array is empty")
	}
	if err := decodeJSON("key_events", env["KEY_EVENTS"], &a.KeyEvents); err != nil {
		return nil, err
	}
	if len(a.KeyEvents) == 0 {
		return nil, fmt.Errorf("key_events array is empty")
	}

	if err := decodeOptionalArray("timeline", env["TIMELINE"], &a.TimelineEvents); err != nil {
		return nil, err
	}
	if err := decodeOptionalArray("foreshadow", env["FORESHADOW"], &a.ForeshadowUpdates); err != nil {
		return nil, err
	}
	if err := decodeOptionalArray("relationships", env["RELATIONSHIPS"], &a.RelationshipChanges); err != nil {
		return nil, err
	}
	if err := decodeOptionalArray("state_changes", env["STATE_CHANGES"], &a.StateChanges); err != nil {
		return nil, err
	}
	for i, fu := range a.ForeshadowUpdates {
		if fu.Action == "plant" && strings.TrimSpace(fu.Description) == "" {
			return nil, fmt.Errorf("foreshadow[%d] action=plant requires description (id=%s)", i, fu.ID)
		}
	}
	return a, nil
}

// decodeOptionalArray cho phép tag bị thiếu hoặc là chuỗi rỗng; chỉ parse khi không rỗng.
func decodeOptionalArray(label, body string, out any) error {
	body = stripFences(body)
	if body == "" || body == "[]" {
		return nil
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("parse %s JSON: %w", label, err)
	}
	return nil
}

// PersistChapter lưu kết quả phân tích xuống đĩa: trước tiên ghi bản nháp chương, sau đó gọi commit_chapter thực hiện bộ ba thao tác nguyên tử.
// Các chương đã hoàn thành sẽ bị bỏ qua bởi kiểm tra idempotent của chính commit_chapter, vẫn trả về nil để vòng lặp tiếp tục.
func PersistChapter(
	ctx context.Context,
	st *store.Store,
	commitTool *tools.CommitChapterTool,
	chapter int,
	title, content string,
	a *ChapterAnalysis,
) error {
	if a == nil {
		return fmt.Errorf("nil analysis")
	}
	if commitTool == nil {
		return fmt.Errorf("nil commit tool")
	}

	// 1. Lưu bản nháp xuống đĩa (commit_chapter đọc nội dung từ drafts/{ch}.draft.md)
	if err := st.Drafts.SaveDraft(chapter, content); err != nil {
		return fmt.Errorf("save draft ch%d: %w", chapter, err)
	}

	// 2. Đánh dấu bắt đầu viết (ValidateChapterWork không chặn dưới FlowWriting, nhưng progress cần bước này để nhất quán)
	if err := st.Progress.StartChapter(chapter); err != nil {
		return fmt.Errorf("start chapter ch%d: %w", chapter, err)
	}

	// 3. Xây dựng tham số đầu vào cho commit_chapter (tiêu đề chương được truyền vào chỉ để ghi log, commit_chapter không đọc title)
	args := map[string]any{
		"chapter":         chapter,
		"summary":         a.Summary,
		"characters":      a.Characters,
		"key_events":      a.KeyEvents,
		"hook_type":       a.HookType,
		"dominant_strand": a.DominantStrand,
	}
	if len(a.TimelineEvents) > 0 {
		args["timeline_events"] = a.TimelineEvents
	}
	if len(a.ForeshadowUpdates) > 0 {
		args["foreshadow_updates"] = a.ForeshadowUpdates
	}
	if len(a.RelationshipChanges) > 0 {
		args["relationship_changes"] = a.RelationshipChanges
	}
	if len(a.StateChanges) > 0 {
		args["state_changes"] = a.StateChanges
	}
	_ = title

	raw, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal commit args ch%d: %w", chapter, err)
	}
	if _, err := commitTool.Execute(ctx, raw); err != nil {
		return fmt.Errorf("commit ch%d: %w", chapter, err)
	}
	return nil
}
