package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/voocel/ainovel-cli/internal/tools"
)

//go:embed prompts/*.md
var promptsFS embed.FS

//go:embed references
var referencesFS embed.FS

//go:embed styles/*.md
var stylesFS embed.FS

//go:embed rules
var rulesFS embed.FS

// Prompts đại diện cho tập hợp các prompt được nhúng sẵn.
type Prompts struct {
	Coordinator      string
	ArchitectLong    string
	Writer           string
	Editor           string
	ImportFoundation string
	ImportAnalyzer   string
	SimulationSource string
	SimulationMerge  string
	RequirementExtractor string
}

// Bundle đại diện cho tập hợp tài nguyên tĩnh cần thiết khi chạy.
type Bundle struct {
	References tools.References
	Prompts    Prompts
	Styles     map[string]string
	// RulesFS là cây con assets/rules (thư mục gốc chứa trực tiếp default.md).
	// Người gọi truyền vào rules.Load làm nguồn quy tắc nội tuyến.
	RulesFS fs.FS
}

// Load trả về tập hợp tài nguyên tương ứng với phong cách được chỉ định.
func Load(style string) Bundle {
	return Bundle{
		References: loadReferences(style),
		Prompts:    loadPrompts(),
		Styles:     loadStyles(),
		RulesFS:    loadRulesFS(),
	}
}

// loadRulesFS trả về hệ thống tệp con của assets/rules; thư mục gốc chứa trực tiếp default.md.
// Nếu fs.Sub thất bại (về lý thuyết không nên xảy ra) trả về nil, rules.Load sẽ bỏ qua nguồn nội tuyến.
func loadRulesFS() fs.FS {
	sub, err := fs.Sub(rulesFS, "rules")
	if err != nil {
		return nil
	}
	return sub
}

func loadReferences(style string) tools.References {
	if style == "" {
		style = "default"
	}
	refs := tools.References{
		ChapterGuide:      mustRead(referencesFS, "references/chapter-guide.md"),
		HookTechniques:    mustRead(referencesFS, "references/hook-techniques.md"),
		QualityChecklist:  mustRead(referencesFS, "references/quality-checklist.md"),
		OutlineTemplate:   mustRead(referencesFS, "references/outline-template.md"),
		CharacterTemplate: mustRead(referencesFS, "references/character-template.md"),
		ChapterTemplate:   mustRead(referencesFS, "references/chapter-template.md"),
		Consistency:       mustRead(referencesFS, "references/consistency.md"),
		ContentExpansion:  mustRead(referencesFS, "references/content-expansion.md"),
		DialogueWriting:   mustRead(referencesFS, "references/dialogue-writing.md"),
		LongformPlanning:  mustRead(referencesFS, "references/longform-planning.md"),
		Differentiation:   mustRead(referencesFS, "references/differentiation.md"),
		AntiAITone:        mustRead(referencesFS, "references/anti-ai-tone.md"),
	}
	if style != "" && style != "default" {
		genreDir := "references/genres/" + style + "/"
		if data, err := referencesFS.ReadFile(genreDir + "style-references.md"); err == nil {
			refs.StyleReference = string(data)
		}
		if data, err := referencesFS.ReadFile(genreDir + "arc-templates.md"); err == nil {
			refs.ArcTemplates = string(data)
		}
	}
	return refs
}

func loadPrompts() Prompts {
	return Prompts{
		Coordinator:      withSimulationGuidance(mustRead(promptsFS, "prompts/coordinator.md"), "coordinator"),
		ArchitectLong:    withSimulationGuidance(mustRead(promptsFS, "prompts/architect-long.md"), "architect"),
		Writer:           withSimulationGuidance(mustRead(promptsFS, "prompts/writer.md"), "writer"),
		Editor:           withSimulationGuidance(mustRead(promptsFS, "prompts/editor.md"), "editor"),
		ImportFoundation: mustRead(promptsFS, "prompts/import-foundation.md"),
		ImportAnalyzer:   mustRead(promptsFS, "prompts/import-chapter-analyzer.md"),
		SimulationSource: mustRead(promptsFS, "prompts/simulation-source.md"),
		SimulationMerge:  mustRead(promptsFS, "prompts/simulation-merge.md"),
		RequirementExtractor: mustRead(promptsFS, "prompts/requirement-extractor.md"),
	}
}

func withSimulationGuidance(prompt, role string) string {
	return prompt + "\n\n" + strings.ReplaceAll(simulationGuidance, "{{role}}", role)
}

const simulationGuidance = `## Hồ sơ phong cách mô phỏng

Khi novel_context trả về simulation_profile, phải coi đó là ràng buộc định hướng phong cách mô phỏng cho tác phẩm hiện tại. {{role}} cần đọc các trường style, lexicon, plot_design, hook_design, pacing_density, reader_engagement và role_guidance trong đó.

Nguyên tắc sử dụng: học hỏi cấu trúc, nhịp truyện, điểm móc, cách giải phóng thông tin và kỹ thuật thu hút độc giả; không sao chép câu văn gốc, nhân vật, địa danh, thiết lập riêng hay cầu nối cố định. Nếu simulation_profile mâu thuẫn với yêu cầu tường minh của người dùng, ưu tiên tuân theo yêu cầu của người dùng.`

func loadStyles() map[string]string {
	styles := make(map[string]string)
	entries, err := stylesFS.ReadDir("styles")
	if err != nil {
		return styles
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		data, err := stylesFS.ReadFile("styles/" + e.Name())
		if err != nil {
			continue
		}
		styles[name] = string(data)
	}
	return styles
}

func mustRead(fs embed.FS, path string) string {
	data, err := fs.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("embed read %s: %v", path, err))
	}
	return string(data)
}
