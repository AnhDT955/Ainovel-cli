package store

import (
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
)

// OutlineStore quản lý tiền đề câu chuyện, đề cương (phẳng/phân cấp) và la bàn.
type OutlineStore struct{ io *IO }

func NewOutlineStore(io *IO) *OutlineStore { return &OutlineStore{io: io} }

// SavePremise lưu tiền đề câu chuyện vào premise.md.
func (s *OutlineStore) SavePremise(content string) error {
	return s.io.WriteMarkdown("premise.md", content)
}

// LoadPremise đọc premise.md. Trả về chuỗi rỗng nếu không tồn tại.
func (s *OutlineStore) LoadPremise() (string, error) {
	data, err := s.io.ReadFile("premise.md")
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// SaveOutline lưu đồng thời outline.json và outline.md (ghi nguyên tử).
func (s *OutlineStore) SaveOutline(entries []domain.OutlineEntry) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("outline.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("outline.md", renderOutline(entries))
	})
}

// LoadOutline đọc đề cương có cấu trúc từ outline.json.
func (s *OutlineStore) LoadOutline() ([]domain.OutlineEntry, error) {
	var entries []domain.OutlineEntry
	if err := s.io.ReadJSON("outline.json", &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// GetChapterOutline lấy mục đề cương của chương được chỉ định.
func (s *OutlineStore) GetChapterOutline(chapter int) (*domain.OutlineEntry, error) {
	entries, err := s.LoadOutline()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Chapter == chapter {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("chapter %d not found in outline", chapter)
}

// NormalizeOutlineIndexes gán lại Index của tập/cung theo đúng vị trí trong mảng: tập đánh số 1..n toàn cục,
// cung đánh số 1..n bên trong từng tập. Đây là quy ước mà tên file tóm tắt (arc-vXXaYY.json) đã dùng sẵn.
//
// Index là dữ liệu DẪN XUẤT từ vị trí, không phải thứ Kiến trúc sư được quyền khai báo — nhưng nó lại là khóa
// tra cứu của CheckArcBoundary, expandArcUnlocked và Summaries.HasArcSummary. Nên chuẩn hóa tại điểm ghi rẻ
// hơn nhiều so với tin model điền đúng rồi validate và bắt nó thử lại.
//
// Sự cố 2026-07 (truyện "Loạn Thế Võ Đạo", chết cứng tại chương 11): architect lưu layered_outline bỏ trống
// trường "index", Go điền zero-value 0 cho toàn bộ 4 tập × 3 cung. Chuỗi hỏng: Router đọc boundary V0/A0 →
// ra lệnh "tóm tắt cung 0 tập 0" → save_arc_summary từ chối vì volume/arc phải > 0 → Editor tự sửa thành
// V1/A1 và ghi ra arc-v01a01.json → nhưng Router vẫn hỏi HasArcSummary(0, 0) → mãi mãi false → phát lại
// đúng lệnh đó vô hạn, nhánh viết chương (ưu tiên 12) không bao giờ tới lượt. Index=0 còn làm expand_arc
// khớp nhầm cung đầu tiên, vì nó tra cung mục tiêu bằng cách so Index.
func NormalizeOutlineIndexes(volumes []domain.VolumeOutline) {
	for vi := range volumes {
		volumes[vi].Index = vi + 1
		for ai := range volumes[vi].Arcs {
			volumes[vi].Arcs[ai].Index = ai + 1
		}
	}
}

// SaveLayeredOutline lưu đề cương phân cấp (chế độ truyện dài, ghi nguyên tử).
func (s *OutlineStore) SaveLayeredOutline(volumes []domain.VolumeOutline) error {
	// Chuẩn hóa TRƯỚC khi validate: thông báo lỗi của ValidateLayeredOutline in ra "V%d A%d", nhãn đó chỉ
	// giúp Kiến trúc sư tìm đúng chỗ sai khi nó mang số thật thay vì V0 A0.
	NormalizeOutlineIndexes(volumes)
	if err := ValidateLayeredOutline(volumes); err != nil {
		return err
	}
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("layered_outline.json", volumes); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("layered_outline.md", renderLayeredOutline(volumes))
	})
}

// ValidateLayeredOutline kiểm tra bất biến cốt lõi của đề cương phân lớp: **không cung khung nào được nằm
// trước một cung đã mở rộng**.
//
// Lý do: FlattenOutline đánh số chương toàn cục chỉ bằng cách đi qua các cung ĐÃ mở rộng — cung khung bị bỏ
// qua hoàn toàn, không chiếm số. Nếu một cung khung nằm trước cung đã mở rộng, thì đến lúc nó được expand_arc,
// mọi chương phía sau bị dịch số và toàn bộ chương đã viết lệch khỏi mục đề cương của chúng.
//
// Trường hợp thật đã gặp: Tập 1 có A1(khung, est 4), A2(khung, est 4), A3(12 chương chi tiết). 12 chương của A3
// bị đánh số 1-12 và được viết ra; A1/A2 trở thành bom hẹn giờ — expand bất kỳ cái nào cũng dịch 36 chương đã viết.
// Chặn ngay tại lúc lưu rẻ hơn nhiều so với phát hiện sau khi đã viết 36 chương.
func ValidateLayeredOutline(volumes []domain.VolumeOutline) error {
	skeleton := "" // cung khung đầu tiên gặp phải
	for _, v := range volumes {
		if err := validateVolumeFields(v); err != nil {
			return err
		}
		for _, a := range v.Arcs {
			label := fmt.Sprintf("V%d A%d %q", v.Index, a.Index, a.Title)
			if !a.IsExpanded() {
				if skeleton == "" {
					skeleton = label
				}
				continue
			}
			if skeleton != "" {
				return fmt.Errorf(
					"đề cương không hợp lệ: cung %s đã có chương chi tiết nhưng nằm SAU cung khung %s. "+
						"Số chương toàn cục chỉ đếm qua các cung đã mở rộng, nên cung khung nằm trước sẽ làm dịch số toàn bộ chương phía sau khi được mở rộng về sau. "+
						"Hãy mở rộng các cung theo đúng thứ tự kể chuyện: mọi cung có chapters phải đứng liền nhau từ đầu, các cung khung (chỉ có estimated_chapters) xếp sau cùng: %w",
					label, skeleton, errs.ErrToolArgs)
			}
		}
	}
	return nil
}

// validateVolumeFields bắt các trường mô tả bị Kiến trúc sư bỏ trống trong một tập.
//
// Cùng lớp lỗi với Index=0 và cùng cách chữa với ValidateOutlineChapters (vốn đã bắt title/core_event rỗng):
// LLM bỏ qua một trường → Go điền zero-value → Store nhận trong im lặng. Nhưng theme/goal không phải trang trí:
// novel_context nạp chúng vào ngữ cảnh Người viết dưới dạng volume_theme/arc_goal, và Biên tập viên đối chiếu
// chúng khi đánh giá cuối cung. Rỗng nghĩa là cả người viết lẫn người duyệt đều không biết cung này để làm gì.
//
// Sự cố 2026-07 ("Loạn Thế Võ Đạo"): cùng lần gọi architect làm rụng "index" cũng làm rụng toàn bộ "goal" và
// "theme" — 12/12 cung và 4/4 tập đều rỗng, và 10 chương đầu được viết ra như vậy. Hai cuốn cũ trên đĩa đều
// điền đủ 100%, nên đây là mất mát thật chứ không phải quy ước "được để trống".
func validateVolumeFields(v domain.VolumeOutline) error {
	if strings.TrimSpace(v.Title) == "" {
		return fmt.Errorf("tập V%d thiếu title: %w", v.Index, errs.ErrToolArgs)
	}
	if strings.TrimSpace(v.Theme) == "" {
		return fmt.Errorf(
			"tập V%d %q thiếu theme (xung đột/chủ đề cốt lõi của tập). Trường này được nạp vào ngữ cảnh Người viết, "+
				"bỏ trống nghĩa là cả tập được viết mà không ai biết nó xoay quanh cái gì: %w",
			v.Index, v.Title, errs.ErrToolArgs)
	}
	for _, a := range v.Arcs {
		if strings.TrimSpace(a.Title) == "" {
			return fmt.Errorf("cung V%d A%d thiếu title: %w", v.Index, a.Index, errs.ErrToolArgs)
		}
		if strings.TrimSpace(a.Goal) == "" {
			return fmt.Errorf(
				"cung V%d A%d %q thiếu goal (mục tiêu cung: mở đầu-thắt nút-chuyển-kết). Người viết đọc trường này qua "+
					"arc_goal và Biên tập viên đối chiếu nó khi đánh giá cuối cung; bỏ trống là viết mù cả cung: %w",
				v.Index, a.Index, a.Title, errs.ErrToolArgs)
		}
	}
	return nil
}

// LoadLayeredOutline đọc đề cương phân cấp.
func (s *OutlineStore) LoadLayeredOutline() ([]domain.VolumeOutline, error) {
	var volumes []domain.VolumeOutline
	if err := s.io.ReadJSON("layered_outline.json", &volumes); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return volumes, nil
}

// ClearLayeredOutline xóa các file đề cương phân cấp.
func (s *OutlineStore) ClearLayeredOutline() error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.RemoveFileUnlocked("layered_outline.json"); err != nil {
			return err
		}
		return s.io.RemoveFileUnlocked("layered_outline.md")
	})
}

// GetChapterFromLayered tìm kiếm theo số chương toàn cục trong đề cương phân cấp.
func (s *OutlineStore) GetChapterFromLayered(chapter int) (*domain.OutlineEntry, error) {
	volumes, err := s.LoadLayeredOutline()
	if err != nil {
		return nil, err
	}
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			for i := range a.Chapters {
				if ch == chapter {
					e := a.Chapters[i]
					e.Chapter = ch
					return &e, nil
				}
				ch++
			}
		}
	}
	return nil, fmt.Errorf("chapter %d not found in layered outline", chapter)
}

// LocateChapter xác định vị trí tập và cung truyện dựa theo số chương toàn cục.
func (s *OutlineStore) LocateChapter(chapter int) (volume, arc int, err error) {
	volumes, err := s.LoadLayeredOutline()
	if err != nil {
		return 0, 0, err
	}
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			for range a.Chapters {
				if ch == chapter {
					return v.Index, a.Index, nil
				}
				ch++
			}
		}
	}
	return 0, 0, fmt.Errorf("chapter %d not found in layered outline", chapter)
}

// ArcPosition mô tả chỗ đứng của một chương trong đề cương phân lớp: nó thuộc tập/cung nào, là chương thứ
// mấy trong cung đó, và cung đó dài bao nhiêu.
//
// Tồn tại vì Progress.CurrentVolume/CurrentArc KHÔNG trả lời được câu hỏi đó cho chương SẮP viết: hai trường
// ấy chỉ được commit_chapter ghi SAU khi một chương xong, nên chúng luôn mô tả cung của chương TRƯỚC. Dùng
// chúng để nạp ngữ cảnh cho chương sắp viết thì tại mọi biên cung, Người viết nhận mục tiêu của cung vừa kết
// thúc, kèm chỉ số vô nghĩa kiểu "chương 11/10 của cung". Các chương giữa cung vẫn đúng nên lỗi rất khó thấy.
type ArcPosition struct {
	Volume       int
	Arc          int
	VolumeTitle  string
	VolumeTheme  string
	ArcTitle     string
	ArcGoal      string
	ChapterIndex int // số thứ tự chương trong cung, bắt đầu từ 1
	ArcChapters  int // tổng số chương của cung
}

// LocateChapterPosition tra vị trí đầy đủ của một chương trong đề cương phân lớp.
// Trả về (nil, nil) khi chương nằm ngoài vùng đã mở rộng hoặc không ở chế độ phân lớp — bên gọi tự quyết
// định cách xử lý, vì "chưa có chỗ trong đề cương" là trạng thái hợp lệ ở đường ngữ cảnh (khác đường ghi).
func (s *OutlineStore) LocateChapterPosition(chapter int) (*ArcPosition, error) {
	volumes, err := s.LoadLayeredOutline()
	if err != nil || len(volumes) == 0 {
		return nil, err
	}
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			n := len(a.Chapters)
			if chapter >= ch && chapter < ch+n {
				return &ArcPosition{
					Volume:       v.Index,
					Arc:          a.Index,
					VolumeTitle:  v.Title,
					VolumeTheme:  v.Theme,
					ArcTitle:     a.Title,
					ArcGoal:      a.Goal,
					ChapterIndex: chapter - ch + 1,
					ArcChapters:  n,
				}, nil
			}
			ch += n
		}
	}
	return nil, nil
}

// ArcBoundary thông tin biên giới cung truyện.
type ArcBoundary struct {
	IsArcEnd       bool
	IsVolumeEnd    bool
	Volume         int
	Arc            int
	NextVolume     int
	NextArc        int
	NeedsExpansion bool
	NeedsNewVolume bool // cuối tập và layered_outline hiện tại không có tập tiếp theo

	// OutOfRange: số chương nằm ngoài vùng đã mở rộng của đề cương phân lớp (chapter > tổng số chương đã mở rộng).
	// Trạng thái này nghĩa là đề cương đã bị thu ngắn hoặc chưa theo kịp tiến độ viết. Khi đó Volume/Arc mô tả
	// cung khung ĐẦU TIÊN chờ mở rộng (nếu có), NeedsExpansion/NeedsNewVolume chỉ ra hành động cần làm để chữa.
	//
	// Bên gọi PHẢI phân biệt OutOfRange với biên giới cung bình thường:
	//   - Đường ghi (commit_chapter): từ chối, chương này không có chỗ hợp lệ trong đề cương.
	//   - Đường định tuyến (flow.Route): giao architect_long expand_arc/append_volume để chữa.
	// Trước đây trường hợp này trả về nil, khiến Router bỏ qua toàn bộ nhánh cuối cung và rơi vào
	// vòng lặp "giao writer viết chương không tồn tại" vĩnh viễn.
	OutOfRange bool
}

// HasNextArc kiểm tra xem còn cung truyện tiếp theo hay không.
func (b *ArcBoundary) HasNextArc() bool {
	return b.NextVolume > 0 || b.NextArc > 0
}

// CheckArcBoundary kiểm tra xem một chương có phải là chương cuối của cung/tập không.
//
// Trả về (nil, nil) chỉ khi không ở chế độ phân lớp (layered_outline rỗng).
// Khi chapter vượt quá vùng đã mở rộng, trả về boundary với OutOfRange=true thay vì nil —
// xem ghi chú tại ArcBoundary.OutOfRange.
func (s *OutlineStore) CheckArcBoundary(chapter int) (*ArcBoundary, error) {
	volumes, err := s.LoadLayeredOutline()
	if err != nil || len(volumes) == 0 {
		return nil, err
	}

	type arcPos struct {
		volIdx, arcIdx int
		volume, arc    int
		chInArc        int
		arcLen         int
	}

	ch := 1
	var cur *arcPos
	for vi, v := range volumes {
		for ai, a := range v.Arcs {
			for ci := range a.Chapters {
				if ch == chapter {
					cur = &arcPos{
						volIdx:  vi,
						arcIdx:  ai,
						volume:  v.Index,
						arc:     a.Index,
						chInArc: ci,
						arcLen:  len(a.Chapters),
					}
				}
				ch++
			}
		}
	}
	if cur == nil {
		return outOfRangeBoundary(volumes), nil
	}

	b := &ArcBoundary{
		Volume: cur.volume,
		Arc:    cur.arc,
	}

	isLastChInArc := cur.chInArc == cur.arcLen-1
	isLastArcInVol := cur.arcIdx == len(volumes[cur.volIdx].Arcs)-1

	// Next*/NeedsExpansion/NeedsNewVolume chỉ có ý nghĩa ở cuối cung, nếu không Điều phối viên sẽ hiểu nhầm là cần mở rộng cung tiếp theo sớm.
	if !isLastChInArc {
		return b, nil
	}

	b.IsArcEnd = true
	if isLastArcInVol {
		b.IsVolumeEnd = true
	}

	found := false
	for vi := cur.volIdx; vi < len(volumes); vi++ {
		startArc := 0
		if vi == cur.volIdx {
			startArc = cur.arcIdx + 1
		}
		for ai := startArc; ai < len(volumes[vi].Arcs); ai++ {
			b.NextVolume = volumes[vi].Index
			b.NextArc = volumes[vi].Arcs[ai].Index
			b.NeedsExpansion = !volumes[vi].Arcs[ai].IsExpanded()
			found = true
			break
		}
		if found {
			break
		}
	}

	if b.IsVolumeEnd && !found {
		b.NeedsNewVolume = true
	}

	return b, nil
}

// checkExpandSafety bảo vệ vùng đề cương đã được viết khỏi bị ghi đè hoặc dịch số.
//
// Hai lớp bảo vệ, đều cần thiết:
//  1. Cung đã mở rộng → từ chối. expand_arc là thao tác một chiều "khung → chi tiết"; gọi lại trên cung
//     đã có chương nghĩa là Kiến trúc sư nhắm nhầm mục tiêu. Trước đây thao tác này ghi đè im lặng,
//     làm đề cương phẳng co lại (ví dụ 36 → 25 chương) và kéo theo vòng lặp không thể chữa.
//  2. Cung nằm trong vùng đã viết → từ chối. FlattenOutline chỉ đánh số qua các cung đã mở rộng, nên
//     mở rộng một cung khung nằm TRƯỚC chương cuối đã viết sẽ chèn chương vào giữa và dịch số toàn bộ
//     các chương phía sau — mọi chương đã viết bỗng thuộc về mục đề cương khác.
//
// force=true chỉ dùng cho thao tác sửa chữa có chủ đích của người dùng, không dành cho Agent.
func checkExpandSafety(target *domain.ArcOutline, volumeIdx, arcIdx, startCh, latestCompleted int, force bool) error {
	if force {
		return nil
	}
	if target.IsExpanded() {
		return fmt.Errorf(
			"cung V%d A%d đã được mở rộng (%d chương, bắt đầu từ chương %d), không thể mở rộng lại: expand_arc chỉ dùng cho cung khung chưa có chương. "+
				"Hãy kiểm tra lại volume/arc mục tiêu — mở rộng đè lên cung đã có sẽ làm hỏng số chương của toàn bộ đề cương: %w",
			volumeIdx, arcIdx, len(target.Chapters), startCh, errs.ErrToolPrecondition)
	}
	if latestCompleted > 0 && startCh <= latestCompleted {
		return fmt.Errorf(
			"cung V%d A%d bắt đầu tại chương %d, nằm trong vùng đã viết (đã hoàn thành tới chương %d): mở rộng ở đây sẽ chèn chương vào giữa và dịch số toàn bộ các chương đã viết. "+
				"Chỉ được mở rộng các cung nằm sau chương %d: %w",
			volumeIdx, arcIdx, startCh, latestCompleted, latestCompleted, errs.ErrToolPrecondition)
	}
	return nil
}

// outOfRangeBoundary tạo boundary mô tả trạng thái "số chương đã viết vượt quá vùng đề cương đã mở rộng".
//
// Hành động chữa cháy luôn là append_volume/complete_book, KHÔNG BAO GIỜ là expand_arc. Chứng minh:
// mọi cung khung đều bắt đầu tại chương (tổng_chương_đã_mở_rộng + 1) hoặc sớm hơn — cung khung không
// chiếm số chương nào. Mà OutOfRange nghĩa là chapter > tổng_chương_đã_mở_rộng, tức chapter >= điểm bắt
// đầu của mọi cung khung. Vậy mở rộng bất kỳ cung khung nào cũng chèn chương vào vùng đã viết và dịch số
// toàn bộ chương đã viết — đúng thứ checkExpandSafety từ chối. Chỉ append_volume nối được vào cuối.
//
// Đây là lý do phải trả boundary thay vì nil: trạng thái này luôn là dữ liệu đã lệch (đề cương bị ghi đè
// hoặc chương được viết ngoài kế hoạch), nhưng nó vẫn phải sinh ra một hành động kết thúc được,
// thay vì để Router im lặng rơi xuống nhánh "giao writer viết tiếp" và lặp vô hạn.
// Store.CheckConsistency cảnh báo song song để người dùng biết đề cương và chương đã lệch nhau.
func outOfRangeBoundary(volumes []domain.VolumeOutline) *ArcBoundary {
	b := &ArcBoundary{OutOfRange: true, NeedsNewVolume: true}
	if n := len(volumes); n > 0 {
		last := volumes[n-1]
		b.Volume = last.Index
		if m := len(last.Arcs); m > 0 {
			b.Arc = last.Arcs[m-1].Index
		}
	}
	return b
}

// expandArcUnlocked phương thức nội bộ, được gọi trong quá trình phối hợp liên miền tại Store.ExpandArc.
//
// latestCompleted là số chương đã hoàn thành lớn nhất, dùng để bảo vệ vùng đã viết:
// mở rộng chỉ được phép ở phần đề cương nằm SAU chương cuối đã viết.
func (s *OutlineStore) expandArcUnlocked(volumeIdx, arcIdx int, chapters []domain.OutlineEntry, latestCompleted int, force bool) ([]domain.VolumeOutline, error) {
	var volumes []domain.VolumeOutline
	if err := s.io.ReadJSONUnlocked("layered_outline.json", &volumes); err != nil {
		return nil, fmt.Errorf("load layered_outline: %w", err)
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("expand_arc phải cung cấp ít nhất một chương (volume=%d, arc=%d): %w", volumeIdx, arcIdx, errs.ErrToolArgs)
	}

	// Một lượt duyệt vừa tìm cung mục tiêu, vừa tính startCh — số chương toàn cục của chương đầu tiên
	// trong cung đó, theo đúng quy tắc của FlattenOutline (chỉ các cung đã mở rộng mới chiếm số chương).
	var target *domain.ArcOutline
	startCh := 1
	for vi := range volumes {
		for ai := range volumes[vi].Arcs {
			arc := &volumes[vi].Arcs[ai]
			if volumes[vi].Index == volumeIdx && arc.Index == arcIdx {
				target = arc
				break
			}
			startCh += len(arc.Chapters)
		}
		if target != nil {
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("arc not found: volume=%d, arc=%d: %w", volumeIdx, arcIdx, errs.ErrToolArgs)
	}

	if err := checkExpandSafety(target, volumeIdx, arcIdx, startCh, latestCompleted, force); err != nil {
		return nil, err
	}

	target.Chapters = chapters
	target.EstimatedChapters = 0
	if err := s.io.WriteJSONUnlocked("layered_outline.json", volumes); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("layered_outline.md", renderLayeredOutline(volumes)); err != nil {
		return nil, err
	}
	flat := domain.FlattenOutline(volumes)
	if err := s.io.WriteJSONUnlocked("outline.json", flat); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("outline.md", renderOutline(flat)); err != nil {
		return nil, err
	}
	return volumes, nil
}

// appendVolumeUnlocked phương thức nội bộ, được gọi trong quá trình phối hợp liên miền tại Store.AppendVolume.
func (s *OutlineStore) appendVolumeUnlocked(vol domain.VolumeOutline) ([]domain.VolumeOutline, error) {
	var volumes []domain.VolumeOutline
	if err := s.io.ReadJSONUnlocked("layered_outline.json", &volumes); err != nil {
		return nil, fmt.Errorf("load layered_outline: %w", err)
	}
	// Index của tập mới do vị trí quyết định, không lấy theo lời khai của Kiến trúc sư — cùng lý do với
	// NormalizeOutlineIndexes: nó là khóa tra cứu của boundary/expand_arc/tóm tắt, và một tập nối vào cuối
	// thì chỉ có duy nhất một số hợp lệ.
	vol.Index = 1
	if n := len(volumes); n > 0 {
		vol.Index = volumes[n-1].Index + 1
	}
	for ai := range vol.Arcs {
		vol.Arcs[ai].Index = ai + 1
	}
	if err := validateAppendVolume(vol); err != nil {
		return nil, err
	}
	volumes = append(volumes, vol)
	if err := s.io.WriteJSONUnlocked("layered_outline.json", volumes); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("layered_outline.md", renderLayeredOutline(volumes)); err != nil {
		return nil, err
	}
	flat := domain.FlattenOutline(volumes)
	if err := s.io.WriteJSONUnlocked("outline.json", flat); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("outline.md", renderOutline(flat)); err != nil {
		return nil, err
	}
	return volumes, nil
}

// validateAppendVolume soi nội dung tập mới. Không còn kiểm tra Index tăng dần: appendVolumeUnlocked đã tự
// gán Index theo vị trí trước khi gọi hàm này, nên ràng buộc đó luôn đúng theo cách xây dựng.
func validateAppendVolume(vol domain.VolumeOutline) error {
	if len(vol.Arcs) == 0 {
		return fmt.Errorf("tập mới phải chứa ít nhất một cung truyện")
	}
	// Cùng bộ lọc trường rỗng với SaveLayeredOutline: append_volume là đường ghi đề cương thứ hai, để lọt ở
	// đây thì mọi tập nối thêm về sau đều có thể thiếu theme/goal.
	if err := validateVolumeFields(vol); err != nil {
		return err
	}
	if !vol.Arcs[0].IsExpanded() {
		return fmt.Errorf("cung truyện đầu tiên của tập mới phải chứa các chương chi tiết")
	}
	return nil
}

// SaveCompass lưu la bàn định hướng kết thúc.
func (s *OutlineStore) SaveCompass(compass domain.StoryCompass) error {
	if compass.EndingDirection == "" {
		return fmt.Errorf("ending_direction không được để trống")
	}
	return s.io.WriteJSON("meta/compass.json", compass)
}

// LoadCompass đọc la bàn định hướng kết thúc.
func (s *OutlineStore) LoadCompass() (*domain.StoryCompass, error) {
	var c domain.StoryCompass
	if err := s.io.ReadJSON("meta/compass.json", &c); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func renderLayeredOutline(volumes []domain.VolumeOutline) string {
	var b strings.Builder
	b.WriteString("# Đề cương phân cấp\n\n")
	ch := 1
	for _, v := range volumes {
		fmt.Fprintf(&b, "## Tập %d: %s\n\n", v.Index, v.Title)
		fmt.Fprintf(&b, "**Chủ đề**: %s\n\n", v.Theme)
		for _, a := range v.Arcs {
			fmt.Fprintf(&b, "### Cung %d: %s\n\n", a.Index, a.Title)
			fmt.Fprintf(&b, "**Mục tiêu**: %s\n\n", a.Goal)
			if !a.IsExpanded() {
				fmt.Fprintf(&b, "*(chưa mở rộng, ước tính %d chương)*\n\n", a.EstimatedChapters)
				continue
			}
			for _, e := range a.Chapters {
				fmt.Fprintf(&b, "#### Chương %d: %s\n\n", ch, e.Title)
				fmt.Fprintf(&b, "**Sự kiện cốt lõi**: %s\n\n", e.CoreEvent)
				if e.Hook != "" {
					fmt.Fprintf(&b, "**Điểm móc**: %s\n\n", e.Hook)
				}
				ch++
			}
		}
	}
	return b.String()
}

func renderOutline(entries []domain.OutlineEntry) string {
	var b strings.Builder
	b.WriteString("# Đề cương\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "## Chương %d: %s\n\n", e.Chapter, e.Title)
		fmt.Fprintf(&b, "**Sự kiện cốt lõi**: %s\n\n", e.CoreEvent)
		if e.Conflict != "" {
			fmt.Fprintf(&b, "**Xung đột**: %s\n\n", e.Conflict)
		}
		if e.Stakes != "" {
			fmt.Fprintf(&b, "**Cái giá**: %s\n\n", e.Stakes)
		}
		if e.Turn != "" {
			fmt.Fprintf(&b, "**Bước ngoặt**: %s\n\n", e.Turn)
		}
		if e.Payoff != "" {
			fmt.Fprintf(&b, "**Hồi đáp**: %s\n\n", e.Payoff)
		}
		if e.Consequence != "" {
			fmt.Fprintf(&b, "**Hậu quả**: %s\n\n", e.Consequence)
		}
		if e.Hook != "" {
			fmt.Fprintf(&b, "**Điểm móc**: %s\n\n", e.Hook)
		}
		if len(e.Scenes) > 0 {
			b.WriteString("**Cảnh**: \n")
			for i, sc := range e.Scenes {
				fmt.Fprintf(&b, "%d. %s\n", i+1, sc)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
