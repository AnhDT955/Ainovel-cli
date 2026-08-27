// Package flow triển khai Flow Router theo ngành dọc: Host quyết định dựa trên thực tế
// xem SubAgent nào sẽ được gọi tiếp theo và làm gì.
//
// Nguyên tắc thiết kế:
//   - Route là hàm thuần túy: đầu vào là State, đầu ra là *Instruction. Không có IO, không gọi Store, có thể unit test độc lập.
//   - State được LoadState (không thuần túy) xây dựng từ Store, đọc toàn bộ dữ liệu cần thiết cho routing một lần.
//   - Trả về nil là hợp lệ: nghĩa là "để Coordinator LLM tự quyết định".
//
// Router bao gồm các quyết định kiểu "tra bảng" (bước tiếp theo mỗi chương, hậu xử lý cuối cung truyện, điều phối theo hàng đợi),
// không bao gồm các quyết định kiểu "hiểu ngữ nghĩa" (chọn kiến trúc sư, xử lý Steer của người dùng, xuất tóm tắt).
package flow

import (
	"fmt"
	"slices"

	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// Instruction chỉ thị cho Host bước tiếp theo yêu cầu Coordinator gọi SubAgent nào và nhiệm vụ gì.
type Instruction struct {
	Agent   string // architect_long / writer / editor
	Task    string // mô tả nhiệm vụ giao cho SubAgent
	Reason  string // lý do dành cho Coordinator xem (tùy chọn, tiện debug và ghi log)
	Chapter int    // số chương liên quan đến nhiệm vụ writer (tiếp tục/viết lại/đánh bóng); 0 = không liên quan (nhiệm vụ editor/architect)
}

// State là đầu vào của Route: tất cả dữ liệu thực tế phải được khai báo rõ ràng ở đây, Route không được đọc Store nội bộ.
type State struct {
	Progress *domain.Progress

	// Chương đã hoàn thành cuối cùng (phần tử cuối của Progress.CompletedChapters); 0 nghĩa là chưa bắt đầu viết.
	LastCompleted int

	// Thông tin ranh giới cung truyện của chương trước; khi IsArcEnd=false các trường còn lại không có ý nghĩa.
	// Nên là nil khi LastCompleted=0 hoặc không ở chế độ Layered.
	ArcBoundary *storepkg.ArcBoundary

	// Ba dữ liệu hậu xử lý cuối cung truyện: đánh giá / tóm tắt cung / tóm tắt tập đã hoàn thành chưa.
	HasArcReview     bool
	HasArcSummary    bool
	HasVolumeSummary bool

	// Các mục thiếu trong cài đặt nền tảng (tín hiệu bổ sung trong giai đoạn lập kế hoạch).
	FoundationMissing []string
}

// Route trả về chỉ thị bước tiếp theo dựa trên dữ liệu thực tế; trả về nil nghĩa là để Coordinator LLM tự quyết định.
//
// Mức độ ưu tiên quyết định (loại trừ lẫn nhau, khớp từ trên xuống):
//  1. Phase=Complete        → nil (LLM xuất tóm tắt)
//  2. Phase!=Writing        → nil (LLM quyết định chọn kiến trúc sư / bổ sung kế hoạch)
//  2b. Phase=Writing nhưng thiếu outline/layered_outline → architect_long(save_foundation) — dữ liệu lệch, cấm phái writer
//  3. PendingRewrites không rỗng  → writer viết lại/đánh bóng theo hàng đợi
//  4. Flow=Reviewing        → nil (editor vừa lưu review, phân nhánh verdict do tầng công cụ xử lý)
//  5. Flow=Steering         → nil (đang xử lý can thiệp của người dùng)
//  6. Đề cương ngắn hơn tiến độ viết (OutOfRange) → architect_long(expand_arc / append_volume)
//  7. Thiếu đánh giá cuối cung truyện           → editor(arc review)
//  8. Có đánh giá nhưng thiếu tóm tắt cung  → editor(arc summary)
//  9. Cuối tập có tóm tắt cung nhưng thiếu tóm tắt tập → editor(volume summary)
//
// 10. Cung truyện tiếp theo là skeleton           → architect_long(expand_arc)
// 11. Cuối tập cần quyết định tập tiếp theo       → architect_long(append_volume / complete_book)
// 12. Các trường hợp còn lại                  → writer(viết next_chapter)
func Route(s State) *Instruction {
	p := s.Progress
	if p == nil {
		return nil
	}

	// 1. Trạng thái kết thúc: để LLM xuất tóm tắt
	if p.Phase == domain.PhaseComplete {
		return nil
	}

	// 2. Giai đoạn lập kế hoạch do Coordinator quyết định (chọn architect_long/short + vòng lặp bổ sung)
	if p.Phase != domain.PhaseWriting {
		return nil
	}

	// 2b. Đang ở giai đoạn viết nhưng bộ nền KHÔNG có đề cương: dữ liệu đã lệch — phase bị đẩy sang
	// writing trong khi đề cương chưa từng được lưu thành công (hoặc file đề cương bị mất). Phải chặn
	// TRƯỚC mọi nhánh phái writer, nếu không Router sẽ lặp "viết chương tiếp theo" vô hạn và cả cuốn
	// sách được viết không kế hoạch: sự cố 2026-07-16 ("Loạn Thế Võ Đạo" lần nhập lại), Kiến trúc sư
	// lưu layered_outline thất bại 3 lần rồi chết vì quota, Coordinator vẫn phái Writer, draft_chapter
	// đẩy phase sang writing — 6 chương/88k từ được viết với đề cương 0 chương.
	// Nhánh 6 (OutOfRange) không bắt được trạng thái này vì nó yêu cầu p.Layered=true, mà Layered chỉ
	// được bật khi layered_outline lưu thành công — đúng bước đã thất bại.
	if slices.Contains(s.FoundationMissing, "outline") || slices.Contains(s.FoundationMissing, "layered_outline") {
		return &Instruction{
			Agent: "architect_long",
			Task:  "Bộ nền đang thiếu đề cương: gọi save_foundation type=layered_outline để lưu đề cương phân lớp hoàn chỉnh trước, tuyệt đối không viết chương khi chưa có đề cương",
			Reason: fmt.Sprintf(
				"Giai đoạn viết nhưng đề cương chưa tồn tại (foundation thiếu: %v): phải hoàn thành đề cương trước khi viết bất kỳ chương nào",
				s.FoundationMissing),
		}
	}

	// 3. Hàng đợi viết lại/đánh bóng được ưu tiên (dữ liệu đã được tầng công cụ ghi đĩa, Router chỉ điều phối theo danh sách)
	if len(p.PendingRewrites) > 0 {
		ch := p.PendingRewrites[0]
		verb := "Viết lại"
		if p.Flow == domain.FlowPolishing {
			verb = "Đánh bóng"
		}
		return &Instruction{
			Agent:   "writer",
			Task:    fmt.Sprintf("%s chương %d", verb, ch),
			Reason:  fmt.Sprintf("Hàng đợi PendingRewrites còn %d chương", len(p.PendingRewrites)),
			Chapter: ch,
		}
	}

	// 4. Đang đánh giá: save_review vừa ghi đĩa, nâng/hạ cấp verdict do tầng công cụ xử lý, router không can thiệp
	if p.Flow == domain.FlowReviewing {
		return nil
	}

	// 5. Đang xử lý can thiệp của người dùng: Coordinator đang quyết định, Host không chiếm quyền
	if p.Flow == domain.FlowSteering {
		return nil
	}

	// 6. Đề cương ngắn hơn tiến độ viết: chương tiếp theo không có chỗ trong đề cương phân lớp.
	// Phải chặn TRƯỚC nhánh "viết tiếp" — nếu không Router sẽ giao writer viết một chương không tồn tại,
	// writer bịa kế hoạch, commit_chapter từ chối vì ngoài phạm vi, và vòng lặp lặp lại vô hạn.
	// Hành động chữa cháy luôn là append_volume/complete_book chứ không phải expand_arc — xem chứng minh
	// tại store.outOfRangeBoundary (mở rộng cung khung ở trạng thái này luôn dịch số vùng đã viết).
	if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.OutOfRange {
		return &Instruction{
			Agent: "architect_long",
			Task:  "Đánh giá rồi gọi save_foundation type=append_volume (tiếp tục viết) hoặc type=complete_book (kết thúc toàn bộ tác phẩm)",
			Reason: fmt.Sprintf(
				"Đề cương chỉ phủ tới trước chương %d nhưng đã viết tới chương %d: cần nối tập mới hoặc kết thúc tác phẩm, không được viết tiếp khi chương chưa có trong đề cương",
				p.NextChapter(), s.LastCompleted),
		}
	}

	// 7-11. Hậu xử lý cuối cung truyện trong chế độ phân lớp
	if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
		b := s.ArcBoundary
		switch {
		case !s.HasArcReview:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf("Thực hiện đánh giá cấp cung truyện cho tập %d cung %d (scope=arc)", b.Volume, b.Arc),
				Reason: "Đánh giá cuối cung truyện chưa hoàn thành",
			}
		case !s.HasArcSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf("Tạo tóm tắt cung %d tập %d (save_arc_summary)", b.Arc, b.Volume),
				Reason: "Tóm tắt cung truyện chưa hoàn thành",
			}
		case b.IsVolumeEnd && !s.HasVolumeSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf("Tạo tóm tắt tập %d (save_volume_summary)", b.Volume),
				Reason: "Tóm tắt tập chưa hoàn thành",
			}
		case b.NeedsExpansion && b.NextArc > 0:
			return &Instruction{
				Agent:  "architect_long",
				Task:   fmt.Sprintf("Mở rộng cung %d tập %d (save_foundation type=expand_arc)", b.NextArc, b.NextVolume),
				Reason: "Skeleton cung truyện tiếp theo cần được mở rộng",
			}
		case b.NeedsNewVolume:
			return &Instruction{
				Agent:  "architect_long",
				Task:   "Đánh giá rồi gọi save_foundation type=append_volume (tiếp tục viết) hoặc type=complete_book (kết thúc toàn bộ tác phẩm)",
				Reason: "Cuối tập cần quyết định thêm tập mới hay kết thúc toàn bộ tác phẩm",
			}
		}
	}

	// 12. Tiếp tục viết bình thường
	next := p.NextChapter()
	if next <= 0 {
		return nil
	}
	return &Instruction{
		Agent:   "writer",
		Task:    fmt.Sprintf("Viết chương %d", next),
		Reason:  "Tiếp tục viết chương tiếp theo",
		Chapter: next,
	}
}

// FormatMessage định dạng Instruction thành tin nhắn người dùng gửi cho Coordinator.
// Định dạng cố định, giúp Coordinator prompt nhận dạng và LLM phản hồi trực tiếp.
func FormatMessage(i *Instruction) string {
	return fmt.Sprintf(
		"[Host ra lệnh] Bước tiếp theo: gọi subagent(%s, %q)\nLý do: %s\nĐây là lệnh từ tầng luồng, hãy thực thi ngay, không được gọi novel_context trước, không được xuất suy luận trước.",
		i.Agent, i.Task, i.Reason,
	)
}
