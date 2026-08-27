package imp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// Deps truyền các dependency có thể thay thế của runner vào một lần, tiện cho việc mock khi test.
type Deps struct {
	Store      *store.Store
	CommitTool *tools.CommitChapterTool
	LLM        LLMChat // Cùng một model là đủ, foundation/analyzer đều là phân tích ngược có cấu trúc
	Prompts    Prompts
}

// Prompts là hai đoạn prompt được sử dụng trong luồng imp.
type Prompts struct {
	Foundation string // Phân tích ngược foundation
	Analyzer   string // Phân tích ngược từng chương
}

// Run thực thi toàn bộ luồng import: split → foundation → vòng lặp chương.
// Chạy trong goroutine riêng; kênh Events được đóng bởi hàm này.
//
// Quyết định thiết kế:
//   - Toàn bộ luồng là blocking (CLI long task), bên gọi chịu trách nhiệm mở goroutine lắng nghe kênh;
//   - Bất kỳ bước nào thất bại đều kết thúc ngay, gửi event StageError;
//   - Giai đoạn chapter bỏ qua lặng lẽ các chương đã hoàn thành (commit_chapter có tính idempotent làm lưới đỡ, nhưng bỏ qua LLM tiết kiệm token hơn).
func Run(ctx context.Context, deps Deps, opts Options) (<-chan Event, error) {
	if deps.Store == nil || deps.CommitTool == nil || deps.LLM == nil {
		return nil, fmt.Errorf("deps incomplete")
	}
	if strings.TrimSpace(opts.SourcePath) == "" {
		return nil, fmt.Errorf("source path is required")
	}

	events := make(chan Event, 32)

	go func() {
		defer close(events)
		emit := func(stage Stage, current, total int, msg string, err error) {
			ev := Event{Time: time.Now(), Stage: stage, Current: current, Total: total, Message: msg, Err: err}
			select {
			case events <- ev:
			case <-ctx.Done():
			}
		}

		// ── 1. Tách chương ──
		emit(StageSplitting, 0, 0, "Đang tách chương...", nil)
		chapters, err := SplitFile(opts.SourcePath)
		if err != nil {
			emit(StageError, 0, 0, "Tách chương thất bại", err)
			return
		}
		total := len(chapters)
		if total == 0 {
			emit(StageError, 0, 0,
				"Không nhận diện được chương nào: hỗ trợ tiêu đề dạng「Chương N」「第N章/回/话/卷/节/幕」「Mở đầu/Ngoại truyện」「序章/楔子/尾声/番外」"+
					"「Chapter N / Prologue」, tương thích Markdown #, khoảng trắng toàn bộ, bao bởi【】và mã hóa GBK."+
					"Vui lòng xác nhận tệp là văn bản tiểu thuyết có chia chương.",
				fmt.Errorf("no chapters matched"))
			return
		}
		emit(StageSplitting, 0, total, fmt.Sprintf("Tách chương hoàn tất: %d chương", total), nil)

		// ── 2. Phân tích ngược Foundation (bỏ qua nếu đã đầy đủ) ──
		if needsFoundation(deps.Store, opts) {
			emit(StageFoundation, 0, total, "Đang phân tích ngược Foundation (một lần gọi LLM)...", nil)
			fr, err := ReverseFoundation(ctx, deps.LLM, deps.Prompts.Foundation, chapters)
			if err != nil {
				emit(StageError, 0, total, "Phân tích ngược Foundation thất bại", err)
				return
			}
			scale := pickScale(total)
			if err := PersistFoundation(ctx, deps.Store, scale, fr); err != nil {
				emit(StageError, 0, total, "Ghi Foundation xuống đĩa thất bại", err)
				return
			}
			emit(StageFoundation, 0, total,
				fmt.Sprintf("Foundation sẵn sàng: %d nhân vật / %d quy tắc / %d chương đề cương (tập một)",
					len(fr.Characters), len(fr.WorldRules), len(domain.FlattenOutline(fr.Volumes))),
				nil)
		} else {
			emit(StageFoundation, 0, total, "Foundation đã tồn tại, bỏ qua phân tích ngược", nil)
		}

		// ── 3. Vòng lặp chương ──
		premise, _ := deps.Store.Outline.LoadPremise()
		charactersBlock := loadCharactersBlock(deps.Store)

		startIdx := 0
		if opts.ResumeFrom > 1 {
			startIdx = opts.ResumeFrom - 1
		}
		for i := startIdx; i < total; i++ {
			if err := ctx.Err(); err != nil {
				emit(StageError, i+1, total, "Người dùng hủy", err)
				return
			}
			chNum := i + 1
			ch := chapters[i]

			// Đã hoàn thành → bỏ qua LLM
			if deps.Store.Progress.IsChapterCompleted(chNum) {
				emit(StageChapter, chNum, total, fmt.Sprintf("Chương %d đã hoàn thành, bỏ qua", chNum), nil)
				continue
			}

			emit(StageChapter, chNum, total, fmt.Sprintf("Đang phân tích chương %d/%d: %s", chNum, total, ch.Title), nil)

			activeHooks, _ := deps.Store.World.LoadActiveForeshadow()
			analysis, err := AnalyzeChapter(ctx, deps.LLM, deps.Prompts.Analyzer,
				chNum, ch.Title, ch.Content, premise, charactersBlock, activeHooks)
			if err != nil {
				emit(StageError, chNum, total, fmt.Sprintf("Phân tích chương %d thất bại", chNum), err)
				return
			}

			if err := PersistChapter(ctx, deps.Store, deps.CommitTool, chNum, ch.Title, ch.Content, analysis); err != nil {
				emit(StageError, chNum, total, fmt.Sprintf("Ghi chương %d xuống đĩa thất bại", chNum), err)
				return
			}
			emit(StageChapter, chNum, total, fmt.Sprintf("Nhập chương %d hoàn tất", chNum), nil)
		}

		emit(StageDone, total, total, fmt.Sprintf("Nhập hoàn tất: %d chương", total), nil)
	}()

	return events, nil
}

// needsFoundation kiểm tra xem có cần phân tích ngược foundation hay không.
// Nếu người dùng đặt ResumeFrom > 1 tường minh, coi như "tiếp tục nhập", bỏ qua phân tích ngược; ngược lại xét theo trạng thái Store.
func needsFoundation(st *store.Store, opts Options) bool {
	if opts.ResumeFrom > 1 {
		return false
	}
	return len(st.FoundationMissing()) > 0
}

// pickScale chọn mức quy hoạch hợp lý dựa theo số chương; short ≤25, mid ≤80, còn lại là long.
// Không ảnh hưởng đến bản thân import, chỉ ảnh hưởng đến việc Điều phối viên chọn prompt kiến trúc sư khi tiếp tục viết.
func pickScale(total int) domain.PlanningTier {
	switch {
	case total <= 25:
		return domain.PlanningTierShort
	case total <= 80:
		return domain.PlanningTierMid
	default:
		return domain.PlanningTierLong
	}
}

// loadCharactersBlock render hồ sơ nhân vật thành khối văn bản ngắn gọn (name/role + một câu mô tả),
// chỉ dùng làm ngữ cảnh tham khảo cho LLM, không cần cấu trúc chặt chẽ.
func loadCharactersBlock(st *store.Store) string {
	chars, err := st.Characters.Load()
	if err != nil || len(chars) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, c := range chars {
		fmt.Fprintf(&sb, "- **%s**（%s）：%s\n", c.Name, c.Role, oneLine(c.Description))
	}
	return sb.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
