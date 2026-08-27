package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveFoundationTool lưu thiết lập nền (premise/outline/characters), dành riêng cho Kiến trúc sư.
type SaveFoundationTool struct {
	store *store.Store
}

func NewSaveFoundationTool(store *store.Store) *SaveFoundationTool {
	return &SaveFoundationTool{store: store}
}

func (t *SaveFoundationTool) Name() string { return "save_foundation" }
func (t *SaveFoundationTool) Description() string {
	return "Lưu thiết lập nền của tiểu thuyết (premise/layered_outline/characters/world_rules/compass v.v.). **Đây là điểm vào lưu trữ duy nhất**: nội dung không được lưu qua công cụ này sẽ không vào Store, chỉ xuất Markdown/JSON trong tin nhắn coi như mất. Tham số cố định là {type, content, scale?, volume?, arc?}. type có thể là premise / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book. **Mọi tác phẩm đều là truyện dài phân lớp**: đề cương phẳng (type=\"outline\") và các mức scale short/mid đã bị khai tử, chỉ còn scale=\"long\". Khi type là premise thì content phải là chuỗi Markdown; các type khác ưu tiên truyền trực tiếp mảng hoặc đối tượng JSON. expand_arc mở rộng chi tiết chương của cung truyện khung xương (cần volume + arc); append_volume thêm tập mới (content là VolumeOutline JSON đầy đủ, bao gồm cấu trúc cung truyện); update_compass cập nhật hướng kết thúc (content là StoryCompass JSON); complete_book thông báo toàn bộ cuốn sách hoàn thành (content truyền đối tượng rỗng {}, đẩy thẳng Phase=Complete; trước khi gọi phải vượt qua danh sách kiểm tra tập cuối, và không có hàng chờ làm lại). scale tùy chọn, chỉ cho phép short / mid / long."
}
func (t *SaveFoundationTool) Label() string { return "Lưu thiết lập" }

// Công cụ ghi (cập nhật chéo domain Outline/Progress/Characters), cấm chạy đồng thời.
func (t *SaveFoundationTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveFoundationTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveFoundationTool) Schema() map[string]any {
	return schema.Object(
		// "outline" (đề cương phẳng) cố ý KHÔNG có trong enum: nó đã bị khai tử, mọi tác phẩm đều phân lớp.
		// Nhánh case "outline" vẫn được giữ trong Execute để model nào lỡ gọi thì nhận lời giải thích rõ ràng
		// thay vì "unknown type".
		schema.Property("type", schema.Enum("loại thiết lập", "premise", "layered_outline", "characters", "world_rules", "expand_arc", "append_volume", "update_compass", "complete_book")).Required(),
		// Không có kiểu tĩnh nào diễn tả được content: hình dạng của nó phụ thuộc vào `type`. Vì vậy tên
		// khóa của TỪNG type phải nằm ngay trong description — đây là thứ duy nhất mô hình chắc chắn đọc.
		// Sự cố 2026-07-16: description cũ chỉ nói "truyền mảng hoặc đối tượng JSON", nên Kiến trúc sư tự
		// phát minh tên khóa (`ten_chuong`/`su_kien`) và bọc mảng vào object — bốn lệnh gọi hỏng liên tiếp.
		schema.Property("content", map[string]any{
			"description": "Nội dung. Truyền THẲNG giá trị JSON, KHÔNG bọc thêm object (`[{...}]` chứ không phải `{\"characters\": [{...}]}`) và KHÔNG stringify mảng con. Hình dạng theo type:\n" +
				"- premise: chuỗi Markdown, dòng đầu là `# <tên sách thật>`\n" +
				"- characters: MẢNG, khóa mỗi phần tử: name, aliases[], role, description, arc (string), traits[] (string[]), tier\n" +
				"- world_rules: MẢNG, khóa mỗi phần tử: category, rule, boundary\n" +
				"- layered_outline: MẢNG VolumeOutline, khóa: index, title, theme, arcs[]; mỗi arc: index, title, goal, estimated_chapters, chapters[]; mỗi chapter: chapter, title, core_event, conflict, stakes, turn, payoff, consequence, hook, scenes[]\n" +
				"- expand_arc: MẢNG chapter (khóa như trên)\n" +
				"- append_volume: MỘT VolumeOutline (object, khóa như trên)\n" +
				"- update_compass: object, khóa: ending_direction, open_threads[], estimated_scale, last_updated\n" +
				"- complete_book: object rỗng {}\n" +
				"Tên khóa là bắt buộc đúng từng ký tự: khóa lạ bị bỏ qua trong im lặng và trường tương ứng sẽ rỗng.",
		}).Required(),
		schema.Property("scale", schema.Enum("mức quy hoạch (chỉ còn long: truyện ngắn/đơn tập không còn được hỗ trợ)", "long")),
		schema.Property("volume", schema.Int("số thứ tự tập mục tiêu (chỉ bắt buộc khi expand_arc)")),
		schema.Property("arc", schema.Int("số thứ tự cung truyện mục tiêu (chỉ bắt buộc khi expand_arc)")),
	)
}

func (t *SaveFoundationTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
		Scale   string          `json:"scale"`
		Volume  int             `json:"volume"`
		Arc     int             `json:"arc"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	content, err := normalizeFoundationContent(a.Content)
	if err != nil {
		return nil, err
	}
	if a.Scale != "" {
		switch domain.PlanningTier(a.Scale) {
		case domain.PlanningTierLong:
		case domain.PlanningTierShort, domain.PlanningTierMid:
			// short/mid là hai mức chỉ tồn tại để phục vụ đề cương phẳng, mà đề cương phẳng đã bị khai tử
			// (xem case "outline"). Để chúng lọt thì tác phẩm được ghi nhãn ngắn nhưng vẫn phải đi đường
			// phân lớp, rồi va vào sàn 300 chương của complete_book và không bao giờ kết thúc được.
			return nil, fmt.Errorf(
				"mức quy hoạch %q không còn được hỗ trợ: mọi tác phẩm đều là truyện dài phân lớp. Hãy truyền scale=\"long\" "+
					"và lập kế hoạch bằng save_foundation(type=\"layered_outline\"): %w", a.Scale, errs.ErrToolArgs)
		default:
			return nil, fmt.Errorf("invalid scale %q, expected long: %w", a.Scale, errs.ErrToolArgs)
		}
		if err := t.store.RunMeta.SetPlanningTier(domain.PlanningTier(a.Scale)); err != nil {
			return nil, fmt.Errorf("save planning tier: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	result := map[string]any{"saved": true, "type": a.Type, "scale": a.Scale}

	// Giai đoạn viết cấm ghi đè toàn bộ đề cương, chỉ cho phép thao tác tăng dần (expand_arc / append_volume)
	if (a.Type == "outline" || a.Type == "layered_outline") && t.isWriting() {
		return nil, fmt.Errorf(
			"giai đoạn viết cấm dùng %s để ghi đè toàn bộ đề cương. Hãy dùng expand_arc để mở rộng cung truyện khung xương, hoặc append_volume để thêm tập mới: %w", a.Type, errs.ErrToolPrecondition)
	}

	decode := func(typeName string, out any) error {
		return decodeFoundationJSON(typeName, content, out)
	}

	switch a.Type {
	case "premise":
		// Bộ nền là nguồn sự thật duy nhất của Người viết (tài liệu yêu cầu gốc không đi vào lịch sử
		// hội thoại của nó), nên nội dung rỗng/chỗ điền phải bị chặn TẠI ĐÂY. Xem domain/foundation.go.
		if err := domain.ValidatePremise(content); err != nil {
			return nil, fmt.Errorf("%w: %w", err, errs.ErrToolArgs)
		}
		name := domain.ExtractNovelNameFromPremise(content)
		if err := t.store.Outline.SavePremise(content); err != nil {
			return nil, fmt.Errorf("save premise: %w: %w", errs.ErrStoreWrite, err)
		}
		if name != "" {
			_ = t.store.Progress.SetNovelName(name)
			result["novel_name"] = name
		}
		_ = t.store.Progress.UpdatePhase(domain.PhasePremise)

	case "outline":
		// Đề cương phẳng đã bị khai tử: mọi tác phẩm đều phải đi bằng layered_outline.
		//
		// Lý do kỹ thuật: đề cương phẳng không mang cấu trúc tập/cung, nên expand_arc, append_volume,
		// đánh giá/tóm tắt cuối cung đều mất chỗ bám — và nó CHƯA BAO GIỜ có bộ chặn vượt biên, nên chương
		// ngoài đề cương được nhận trong im lặng và sách cứ phình ngoài kế hoạch.
		//
		// Lý do sản phẩm: sàn 300 chương của complete_book áp cho mọi tác phẩm, nên một cuốn ngắn dù phẳng
		// hay phân lớp cũng không kết thúc gọn được. Giữ hai chế độ chỉ tạo ra hai cách hỏng khác nhau.
		return nil, fmt.Errorf(
			"đề cương phẳng không còn được hỗ trợ: mọi tác phẩm đều dùng đề cương phân lớp. "+
				"Hãy gọi save_foundation(type=\"layered_outline\", scale=\"long\", content=<mảng VolumeOutline>) — "+
				"cấu trúc tập/cung là thứ expand_arc và append_volume dựa vào để mở rộng truyện: %w", errs.ErrToolArgs)

	case "layered_outline":
		var volumes []domain.VolumeOutline
		if err := decode("layered_outline", &volumes); err != nil {
			return nil, err
		}
		// Kiểm tra trên bản đã trải phẳng — đây chính là thứ Người viết đọc — và kiểm tra TRƯỚC mọi
		// lần ghi: nếu để SaveLayeredOutline chạy trước, một layered_outline.json rác sẽ nằm lại trên
		// đĩa sau khi tool trả lỗi. FlattenOutline đã đánh số chương toàn cục nên trường chapter ở đây
		// luôn hợp lệ, chỉ còn title/core_event cần soi. Danh sách rỗng cũng bị bắt tại đây: đề cương
		// phân tầng mà không cung nào có chương chi tiết thì Người viết không có gì để viết
		// (prompt yêu cầu cung đầu của Tập 1 phải được mở rộng).
		flat := domain.FlattenOutline(volumes)
		if err := domain.ValidateOutlineChapters(flat); err != nil {
			return nil, fmt.Errorf("%w%s: %w", err, unknownKeyHint(content, &volumes), errs.ErrToolArgs)
		}
		total := domain.TotalChapters(volumes)
		if err := validatePlannedScale(total, volumes); err != nil {
			return nil, err
		}
		if err := t.store.Outline.SaveLayeredOutline(volumes); err != nil {
			// Lỗi bất biến đề cương (cung khung nằm trước cung đã mở rộng) là lỗi đầu vào của Kiến trúc sư,
			// kèm sẵn hướng dẫn sửa — trả thẳng để lần thử lại nhắm đúng chỗ.
			if errors.Is(err, errs.ErrToolArgs) {
				return nil, err
			}
			return nil, fmt.Errorf("save layered_outline: %w: %w", errs.ErrStoreWrite, err)
		}
		if err := t.store.Outline.SaveOutline(flat); err != nil {
			return nil, fmt.Errorf("save flattened outline: %w: %w", errs.ErrStoreWrite, err)
		}
		_ = t.store.Progress.UpdatePhase(domain.PhaseOutline)
		_ = t.store.Progress.SetTotalChapters(total)
		_ = t.store.Progress.SetLayered(true)
		if len(volumes) > 0 && len(volumes[0].Arcs) > 0 {
			_ = t.store.Progress.UpdateVolumeArc(volumes[0].Index, volumes[0].Arcs[0].Index)
		}
		result["volumes"] = len(volumes)
		result["chapters"] = total

	case "characters":
		var chars []domain.Character
		if err := decode("characters", &chars); err != nil {
			return nil, err
		}
		if err := domain.ValidateCharacters(chars); err != nil {
			return nil, fmt.Errorf("%w%s: %w", err, unknownKeyHint(content, &chars), errs.ErrToolArgs)
		}
		if err := t.store.Characters.Save(chars); err != nil {
			return nil, fmt.Errorf("save characters: %w: %w", errs.ErrStoreWrite, err)
		}
		result["count"] = len(chars)

	case "world_rules":
		var rules []domain.WorldRule
		if err := decode("world_rules", &rules); err != nil {
			return nil, err
		}
		if err := domain.ValidateWorldRules(rules); err != nil {
			return nil, fmt.Errorf("%w%s: %w", err, unknownKeyHint(content, &rules), errs.ErrToolArgs)
		}
		if err := t.store.World.SaveWorldRules(rules); err != nil {
			return nil, fmt.Errorf("save world_rules: %w: %w", errs.ErrStoreWrite, err)
		}
		result["count"] = len(rules)

	case "expand_arc":
		if a.Volume <= 0 || a.Arc <= 0 {
			return nil, fmt.Errorf("expand_arc requires volume and arc parameters: %w", errs.ErrToolArgs)
		}
		var chapters []domain.OutlineEntry
		if err := decode("expand_arc chapters", &chapters); err != nil {
			return nil, err
		}
		// Cùng chốt chặn với layered_outline: sau khi truyện dài buộc phải đi bằng đề cương phân tầng,
		// đây là đường chính để chương mới xuất hiện — không kiểm ở đây thì khung rỗng chỉ đổi chỗ.
		if err := domain.ValidateOutlineChapters(chapters); err != nil {
			return nil, fmt.Errorf("%w%s: %w", err, unknownKeyHint(content, &chapters), errs.ErrToolArgs)
		}
		// force=false: Agent không được ghi đè cung đã mở rộng hay chèn chương vào vùng đã viết.
		// Các lỗi từ ExpandArc đã mang sẵn phân loại (ErrToolPrecondition/ErrToolArgs) và hướng dẫn sửa,
		// nên trả thẳng — bọc thêm ErrStoreWrite sẽ phân loại sai thành lỗi ghi đĩa.
		if err := t.store.ExpandArc(a.Volume, a.Arc, chapters, false); err != nil {
			if errors.Is(err, errs.ErrToolPrecondition) || errors.Is(err, errs.ErrToolArgs) {
				return nil, err
			}
			return nil, fmt.Errorf("expand arc: %w: %w", errs.ErrStoreWrite, err)
		}
		result["volume"] = a.Volume
		result["arc"] = a.Arc
		result["chapters"] = len(chapters)

	case "append_volume":
		if p, _ := t.store.Progress.Load(); p != nil && p.Phase == domain.PhaseComplete {
			return nil, fmt.Errorf("toàn bộ cuốn sách đã hoàn thành (phase=complete), không cho phép thêm tập mới: %w", errs.ErrToolPrecondition)
		}
		var vol domain.VolumeOutline
		if err := decode("append_volume", &vol); err != nil {
			return nil, err
		}
		// Chỉ soi các chương thực sự có mặt: một tập mới hoàn toàn ở dạng khung xương (các cung chỉ có
		// estimated_chapters, chờ expand_arc mở rộng sau) là hợp lệ — nhưng chương đã viết ra thì phải
		// có title/core_event thật, giống mọi đường ghi chương khác.
		if chapters := domain.FlattenOutline([]domain.VolumeOutline{vol}); len(chapters) > 0 {
			if err := domain.ValidateOutlineChapters(chapters); err != nil {
				return nil, fmt.Errorf("%w: %w", err, errs.ErrToolArgs)
			}
		}
		if err := t.store.AppendVolume(vol); err != nil {
			return nil, fmt.Errorf("append volume: %w: %w", errs.ErrStoreWrite, err)
		}
		result["volume"] = vol.Index
		result["arcs"] = len(vol.Arcs)
		chCount := 0
		for _, arc := range vol.Arcs {
			chCount += len(arc.Chapters)
		}
		if chCount > 0 {
			result["chapters"] = chCount
		}

	case "complete_book":
		// Điểm vào duy nhất để hoàn thành toàn bộ cuốn sách: đẩy thẳng Phase=Complete.
		// Chỉ cho phép ở giai đoạn Writing, ngăn giai đoạn quy hoạch gọi nhầm bỏ qua toàn bộ quá trình viết.
		// Từ chối khi có hàng chờ làm lại — đảm bảo PendingRewrites chạy hết mới được kết thúc.
		progress, perr := t.store.Progress.Load()
		if perr != nil {
			return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, perr)
		}
		if progress == nil {
			return nil, fmt.Errorf("progress chưa được khởi tạo: %w", errs.ErrToolPrecondition)
		}
		if progress.Phase != domain.PhaseWriting {
			return nil, fmt.Errorf("complete_book chỉ có thể gọi ở giai đoạn writing (phase hiện tại=%s): %w", progress.Phase, errs.ErrToolPrecondition)
		}
		if len(progress.PendingRewrites) > 0 {
			return nil, fmt.Errorf("còn %d chương trong hàng chờ làm lại, xử lý xong rồi mới gọi complete_book: %w", len(progress.PendingRewrites), errs.ErrToolPrecondition)
		}
		// Ràng buộc cứng: truyện tối thiểu 300 chương mới được kết thúc
		const minChaptersForComplete = 300
		if len(progress.CompletedChapters) < minChaptersForComplete {
			return nil, fmt.Errorf("chưa đủ tối thiểu %d chương (hiện có %d chương), không thể kết thúc sách. Tiếp tục mở rộng cốt truyện bằng expand_arc / append_volume: %w",
				minChaptersForComplete, len(progress.CompletedChapters), errs.ErrToolPrecondition)
		}
		if err := t.store.Progress.MarkComplete(); err != nil {
			return nil, fmt.Errorf("mark complete: %w: %w", errs.ErrStoreWrite, err)
		}
		result["book_complete"] = true
		result["phase"] = string(domain.PhaseComplete)

	case "update_compass":
		var compass domain.StoryCompass
		if err := decode("compass", &compass); err != nil {
			return nil, err
		}
		// Tầng công cụ bắt buộc ghi đè LastUpdated bằng số chương đã hoàn thành hiện tại, không tin LLM tự điền.
		// LLM thường quên điền hoặc để 0, khiến diag.CompassDrift báo sai, Router định tuyến lệch.
		if p, _ := t.store.Progress.Load(); p != nil {
			compass.LastUpdated = p.LatestCompleted()
		}
		if err := t.store.Outline.SaveCompass(compass); err != nil {
			return nil, fmt.Errorf("save compass: %w: %w", errs.ErrStoreWrite, err)
		}
		result["ending_direction"] = compass.EndingDirection
		result["last_updated"] = compass.LastUpdated

	default:
		return nil, fmt.Errorf("unknown type %q, expected premise/layered_outline/characters/world_rules/expand_arc/append_volume/update_compass/complete_book: %w", a.Type, errs.ErrToolArgs)
	}

	// điểm khôi phục
	scope := domain.GlobalScope()
	if a.Type == "expand_arc" {
		scope = domain.ArcScope(a.Volume, a.Arc)
	} else if a.Type == "append_volume" {
		scope = domain.GlobalScope()
	}
	if _, err := t.store.Checkpoints.AppendArtifact(scope, a.Type, foundationArtifact(a.Type)); err != nil {
		return nil, fmt.Errorf("checkpoint foundation %s: %w: %w", a.Type, errs.ErrStoreWrite, err)
	}

	// Trả về các mục chưa hoàn thành còn lại, hướng dẫn Kiến trúc sư tiếp tục hoặc kết thúc;
	// khi đủ đầy, đẩy phase sang writing một lần, tránh Điều phối viên phải quay lại giao việc.
	remaining := t.store.FoundationMissing()
	ready := len(remaining) == 0
	result["remaining"] = remaining
	result["foundation_ready"] = ready
	if ready {
		if p, _ := t.store.Progress.Load(); p != nil &&
			p.Phase != domain.PhaseWriting && p.Phase != domain.PhaseComplete {
			_ = t.store.Progress.UpdatePhase(domain.PhaseWriting)
			result["phase"] = string(domain.PhaseWriting)
		}
	}
	return json.Marshal(result)
}

func foundationArtifact(t string) string {
	switch t {
	case "premise":
		return "premise.md"
	case "outline":
		return "outline.json"
	case "layered_outline", "expand_arc", "append_volume":
		return "layered_outline.json"
	case "complete_book":
		return "meta/progress.json"
	case "characters":
		return "characters.json"
	case "world_rules":
		return "world_rules.json"
	case "update_compass":
		return "meta/compass.json"
	default:
		return ""
	}
}

// decodeFoundationJSON phân tích trường content của save_foundation, khi thất bại sẽ kèm vị trí dòng/cột
// và gợi ý sửa lỗi phổ biến nhất, giúp LLM lần thử lại có thể xác định trực tiếp thay vì đoán mò.
//
// Trước khi báo lỗi, hai chế độ hỏng SAI HÌNH DẠNG nhưng JSON hợp lệ được sửa tại chỗ (xem
// repairFoundationShape). Chúng có cách sửa tất định, nên bắt Kiến trúc sư sinh lại cả đoạn chỉ đốt
// thêm một lượt gọi rồi thường hỏng y hệt — sự cố 2026-07-16: ba lệnh gọi liên tiếp
// (layered_outline/characters/world_rules) chết cùng một lỗi bọc object, lượt sửa sau lại chết vì
// chapters bị mã hoá thành chuỗi.
func decodeFoundationJSON(typeName, content string, out any) error {
	err := json.Unmarshal([]byte(content), out)
	if err == nil {
		return nil
	}
	if repaired, ok := repairFoundationShape(content, err, out); ok {
		if err2 := json.Unmarshal([]byte(repaired), out); err2 == nil {
			return nil
		}
		// Bản vá không khớp kiểu đích: rơi về lỗi gốc bên dưới. Không báo lỗi của bản vá vì vị trí
		// dòng/cột của nó thuộc về chuỗi đã bị viết lại, không còn trỏ đúng vào payload thật.
	}
	// Sai HÌNH DẠNG, không sai cú pháp: JSON hoàn toàn hợp lệ, chỉ là cấu trúc không khớp kiểu đích.
	// Hint cú pháp bên dưới lạc đề ở đây — không có dấu ngoặc kép nào thiếu escape để mà sửa — nên nó
	// đẩy Kiến trúc sư vào cảnh sinh lại nguyên đoạn và hỏng y hệt lượt sau. Chế độ hỏng quan sát được
	// là bọc mảng vào một object ({"characters": [...]}) và điền object vào chỗ của một trường vô hướng.
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		line, col := offsetToLineCol(content, int(ute.Offset))
		if ute.Field != "" {
			return fmt.Errorf("parse %s JSON (line %d col %d): trường %q nhận %s nhưng phải là %s — "+
				"JSON hợp lệ, chỉ sai kiểu của đúng trường này, sửa mỗi nó chứ đừng sinh lại cả đoạn: %w",
				typeName, line, col, ute.Field, ute.Value, ute.Type, err)
		}
		if ute.Type.Kind() == reflect.Slice {
			return fmt.Errorf("parse %s JSON (line %d col %d): content nhận %s ở cấp cao nhất nhưng phải là MẢNG JSON `[{...}, {...}]` — "+
				"JSON hợp lệ, chỉ sai hình dạng: bỏ tầng object bọc ngoài (kiểu {\"%s\": [...]}) và truyền thẳng mảng làm giá trị của content: %w",
				typeName, line, col, ute.Value, typeName, err)
		}
		return fmt.Errorf("parse %s JSON (line %d col %d): content nhận %s nhưng phải là %s — "+
			"JSON hợp lệ, chỉ sai hình dạng ở cấp cao nhất: %w",
			typeName, line, col, ute.Value, ute.Type, err)
	}
	hint := `Nguyên nhân phổ biến: dấu ngoặc kép trong giá trị chuỗi chưa được escape thành \", xuống dòng chưa escape thành \n, hoặc thiếu dấu phẩy giữa các trường của đối tượng. Hãy sinh lại toàn bộ đoạn một lần nữa.`
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line, col := offsetToLineCol(content, int(se.Offset))
		return fmt.Errorf("parse %s JSON (line %d col %d): %w — %s", typeName, line, col, err, hint)
	}
	return fmt.Errorf("parse %s JSON: %w — %s", typeName, err, hint)
}

// repairFoundationShape sửa các sai hình dạng TẤT ĐỊNH của content: JSON hoàn toàn hợp lệ, ý định của
// Kiến trúc sư không mơ hồ, chỉ là cách gói khác với kiểu đích. Trả về payload đã sửa để gọi lại
// json.Unmarshal; ok=false nghĩa là không nhận ra chế độ hỏng nào và lỗi gốc phải được giữ nguyên.
//
// Chỉ chạy trên đường THẤT BẠI, nên không có rủi ro với payload vốn đã đúng.
func repairFoundationShape(content string, cause error, out any) (string, bool) {
	if fixed, ok := unwrapFoundationEnvelope(content, out); ok {
		return fixed, true
	}
	return reviveEmbeddedJSON(content, cause)
}

// unwrapFoundationEnvelope gỡ tầng object bọc ngoài khi kiểu đích là MẢNG: `{"characters": [...]}` → `[...]`.
//
// Đây là chế độ hỏng thường gặp nhất của save_foundation (sự cố 2026-07-16 dính cả ba loại nội dung mảng
// trong cùng một lượt). Nguyên nhân: schema của `content` là kiểu tự do, nên mô hình lặp lại tên tham số
// thành khóa bọc ngoài theo phản xạ.
//
// Điều kiện chặt để không đoán bừa: đúng MỘT khóa có giá trị là mảng. `{"volumes": [...], "note": "..."}`
// vẫn gỡ được vì chỉ một khóa là mảng; `{"a": [...], "b": [...]}` thì mơ hồ nên trả về nguyên trạng.
func unwrapFoundationEnvelope(content string, out any) (string, bool) {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Slice {
		return "", false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &obj) != nil {
		return "", false
	}
	found := ""
	for _, raw := range obj {
		trimmed := strings.TrimSpace(string(raw))
		if strings.HasPrefix(trimmed, "[") {
			if found != "" {
				return "", false // nhiều hơn một mảng ứng viên: không đoán
			}
			found = trimmed
		}
	}
	if found == "" {
		return "", false
	}
	return found, true
}

// reviveEmbeddedJSON gỡ JSON bị mã hoá hai lần thành chuỗi, ở bất kỳ độ sâu nào:
// `{"chapters": "[{...}]"}` → `{"chapters": [{...}]}`.
//
// Sự cố 2026-07-16: `trường "arcs.chapters" nhận string nhưng phải là []domain.OutlineEntry`. Mô hình
// tự stringify mảng con dù prompt đã cấm — chuỗi đó vẫn chứa nguyên vẹn dữ liệu, nên vứt cả đề cương
// 12KB đi để sinh lại là lãng phí.
//
// Chỉ chạy khi lỗi gốc đúng là "gặp string ở chỗ cần cấu trúc", giữ phạm vi hẹp nhất có thể. Chuỗi văn
// xuôi không bị đụng tới: nó không parse ra JSON hợp lệ nên `json.Valid` loại thẳng.
func reviveEmbeddedJSON(content string, cause error) (string, bool) {
	var ute *json.UnmarshalTypeError
	if !errors.As(cause, &ute) || ute.Value != "string" {
		return "", false
	}
	var tree any
	if err := json.Unmarshal([]byte(content), &tree); err != nil {
		return "", false
	}
	revived, changed := reviveNode(tree)
	if !changed {
		return "", false
	}
	fixed, err := json.Marshal(revived)
	if err != nil {
		return "", false
	}
	return string(fixed), true
}

func reviveNode(node any) (any, bool) {
	switch n := node.(type) {
	case map[string]any:
		changed := false
		for k, child := range n {
			if next, c := reviveNode(child); c {
				n[k] = next
				changed = true
			}
		}
		return n, changed
	case []any:
		changed := false
		for i, child := range n {
			if next, c := reviveNode(child); c {
				n[i] = next
				changed = true
			}
		}
		return n, changed
	case string:
		s := strings.TrimSpace(n)
		if !strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "{") {
			return node, false
		}
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) != nil {
			return node, false // văn xuôi mở đầu bằng [ hoặc {: để nguyên
		}
		revived, _ := reviveNode(parsed) // chuỗi lồng chuỗi: gỡ tiếp
		return revived, true
	}
	return node, false
}

// unknownKeyHint chỉ ra các khóa mà Kiến trúc sư gửi nhưng không thuộc schema — thứ encoding/json BỎ QUA
// TRONG IM LẶNG, khiến struct decode ra rỗng và validator chỉ báo được "thiếu category" mà không nói nổi
// vì sao thiếu.
//
// Sự cố 2026-07-16: world_rules trả về "quy tắc #1 thiếu category / quy tắc #1 có trường rule rỗng" cho cả
// 12 mục. Không mục nào rỗng thật — mô hình chỉ đặt tên khóa khác. Nó không có cách nào biết điều đó từ
// thông báo lỗi, nên lượt sửa tiếp theo lại đoàn một bộ tên khóa khác. Echo lại đúng khóa đã nhận biến
// vòng đoán mò thành một lần sửa nhắm trúng.
//
// Soi đệ quy theo cây kiểu chứ không chỉ ở cấp cao nhất: với layered_outline, các khóa hỏng nằm tận
// arcs[].chapters[] (mọi chương rỗng cả title lẫn core_event), còn cấp cao nhất thì hoàn toàn hợp lệ.
//
// Trả về chuỗi rỗng khi không có khóa lạ (khi đó lỗi rỗng là thật, thông báo của validator đã đủ).
func unknownKeyHint(content string, out any) string {
	found := map[string]struct{}{}
	var order []string
	collectUnknownKeys(json.RawMessage(content), reflect.TypeOf(out), "", found, &order)
	if len(order) == 0 {
		return ""
	}
	return fmt.Sprintf("\n\nNguyên nhân gần như chắc chắn là SAI TÊN KHÓA: %s. encoding/json bỏ qua khóa lạ trong "+
		"im lặng, nên các trường trên bị để rỗng dù bạn đã viết nội dung cho chúng. Hãy đổi TÊN KHÓA cho khớp và "+
		"giữ nguyên nội dung đã viết — đừng nghĩ lại nội dung, đừng sinh lại cả đoạn.",
		strings.Join(clampKeyProblems(order), "; "))
}

// clampKeyProblems giữ hint ở kích thước dùng được: một payload sai toàn bộ bộ khóa sinh ra rất nhiều
// đường dẫn, mà chỉ vài cái đầu đã đủ để Kiến trúc sư nhận ra bộ tên khóa của nó sai.
func clampKeyProblems(problems []string) []string {
	const limit = 6
	if len(problems) <= limit {
		return problems
	}
	return append(problems[:limit:limit], fmt.Sprintf("... và %d khóa lạ khác", len(problems)-limit))
}

// collectUnknownKeys đi song song giữa JSON thô và kiểu đích, ghi lại khóa không thuộc schema kèm đường dẫn.
// Khoá lạ được gom theo đường dẫn (không theo chỉ số phần tử) vì một mảng 300 chương sai khóa chỉ là MỘT lỗi.
func collectUnknownKeys(raw json.RawMessage, t reflect.Type, path string, found map[string]struct{}, order *[]string) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return
		}
		for _, item := range items {
			collectUnknownKeys(item, t.Elem(), path, found, order)
		}
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return
		}
		byName := jsonFieldsOf(t)
		for key, val := range obj {
			field, ok := byName[strings.ToLower(key)]
			if !ok {
				at := path + key
				if _, dup := found[at]; dup {
					continue
				}
				found[at] = struct{}{}
				*order = append(*order, fmt.Sprintf("khóa %q không tồn tại (khóa hợp lệ tại %s: %v)",
					at, pathLabel(path), validNamesOf(byName)))
				continue
			}
			collectUnknownKeys(val, field.Type, path+key+".", found, order)
		}
	}
}

func pathLabel(path string) string {
	if path == "" {
		return "cấp này"
	}
	return strings.TrimSuffix(path, ".")
}

func jsonFieldsOf(t reflect.Type) map[string]reflect.StructField {
	fields := map[string]reflect.StructField{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[strings.ToLower(name)] = f
	}
	return fields
}

func validNamesOf(fields map[string]reflect.StructField) []string {
	names := slices.Collect(maps.Keys(fields))
	slices.Sort(names)
	return names
}

func offsetToLineCol(s string, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s) {
		offset = len(s)
	}
	line, col := 1, 1
	for i := 0; i < offset; i++ {
		if s[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func normalizeFoundationContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("content is required: %w", errs.ErrToolArgs)
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	if !json.Valid(raw) {
		return "", fmt.Errorf("invalid content: expected Markdown string or valid JSON value: %w", errs.ErrToolArgs)
	}
	return string(raw), nil
}

// effectiveTier trả về mức quy hoạch đang áp dụng cho lượt gọi này: scale truyền vào ưu tiên hơn
// (nó vừa được ghi xuống RunMeta ở đầu Execute), nếu lượt gọi bỏ trống thì lấy mức đã lưu của tác
// phẩm. Không thể chỉ tin vào tham số: Kiến trúc sư thường xuyên quên truyền scale ở các lượt sau,
// và khi đó "" sẽ bị hiểu nhầm là không phải truyện dài.
// minPlannedChapters là quy mô tối thiểu mà một kế hoạch ban đầu phải phủ.
//
// Không phải con số tuỳ tiện: nó bằng đúng minChaptersForComplete của complete_book. Một đề cương lập ra
// dưới ngưỡng này là kế hoạch TỰ MÂU THUẪN — nó mô tả một cuốn sách mà chính engine từ chối cho kết thúc.
const minPlannedChapters = 300

// validatePlannedScale chặn kiểu hỏng "Kiến trúc sư nén cả cuốn sách vào vài chục chương".
//
// Sự cố 2026-07 ("Loạn Thế Võ Đạo"): brief.md ghi rõ 12 arc kèm phạm vi chương — "Arc 1 ... từ chương 1
// đến 35", "Arc 2 ... từ chương 36 đến 80", ... tổng 820 chương. Kiến trúc sư đọc được brief đó (novel_context
// nạp requirement_brief), giữ đúng cả 12 tên arc và đúng thứ tự, rồi tự ý phát minh số chương của riêng
// mình: 10/10/10 và 5 cho chín cung còn lại — tổng 75. Prompt architect-long.md đã nói sẵn "tối thiểu 300
// chương" và "Tập 2-5 để ở dạng khung xương", nó vẫn mở rộng chi tiết cả 12 cung. Prompt không chặn nổi,
// nên phải chặn ở tầng công cụ.
//
// Hai lỗi được soi riêng vì cách sửa khác nhau:
//  1. Tổng quy mô < 300: kế hoạch không thể kết thúc hợp lệ.
//  2. Không còn cung khung nào: Kiến trúc sư đã "chốt" toàn bộ cuốn sách ngay từ đầu, nên expand_arc không
//     còn chỗ bám và truyện không thể lớn lên theo compass. Đây chính là hình dạng của lần hỏng thật.
func validatePlannedScale(total int, volumes []domain.VolumeOutline) error {
	if total >= minPlannedChapters {
		return nil
	}
	expandedArcs, skeletonArcs := 0, 0
	for _, v := range volumes {
		for _, a := range v.Arcs {
			if a.IsExpanded() {
				expandedArcs++
			} else {
				skeletonArcs++
			}
		}
	}
	hint := ""
	if skeletonArcs == 0 && expandedArcs > 1 {
		hint = fmt.Sprintf(
			" Bạn đã mở rộng chi tiết cả %d/%d cung ngay từ đầu: chỉ cung ĐẦU TIÊN của Tập 1 mới cần danh sách chương, "+
				"mọi cung còn lại phải ở dạng khung xương (title + goal + estimated_chapters, KHÔNG có chapters) để expand_arc mở rộng dần về sau.",
			expandedArcs, expandedArcs)
	}
	return fmt.Errorf(
		"kế hoạch chỉ phủ %d chương, dưới mức tối thiểu %d: engine từ chối complete_book khi chưa đủ %d chương, "+
			"nên đề cương này mô tả một cuốn sách không bao giờ kết thúc được.%s "+
			"Nếu requirement_brief có nêu phạm vi chương cho từng arc thì phải theo ĐÚNG con số đó — đừng tự đặt lại quy mô: %w",
		total, minPlannedChapters, minPlannedChapters, hint, errs.ErrToolArgs)
}

func (t *SaveFoundationTool) effectiveTier(scale string) domain.PlanningTier {
	if scale != "" {
		return domain.PlanningTier(scale)
	}
	if m, _ := t.store.RunMeta.Load(); m != nil {
		return m.PlanningTier
	}
	return ""
}

func (t *SaveFoundationTool) isWriting() bool {
	p, _ := t.store.Progress.Load()
	return p != nil && p.Phase == domain.PhaseWriting
}
