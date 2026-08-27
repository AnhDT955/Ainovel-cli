package host

import (
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// buildResumePrompt tạo prompt ngắn gọn và nhãn UI dùng cho chức năng Resume, dựa trên thực tế hiện tại.
//
// Ghi chú tái cấu trúc (2026-04-20): Toàn bộ quyết định "bước tiếp theo cụ thể là gì" đã được chuyển xuống Host Flow Router.
// Hàm này không còn lập kế hoạch hành động thay cho Coordinator, chỉ làm ba việc:
//  1. Xác định có cần khôi phục không (Phase=Complete hoặc không có Progress → trả về label rỗng nghĩa là tạo mới)
//  2. Tạo label phù hợp để hiển thị trên UI (kiểu như "Khôi phục: chờ đánh giá cuối cung (V2 A3)")
//  3. Truyền tường minh PendingSteer mà người dùng để lại khi tạm dừng cho Coordinator
//
// Trả về (prompt, label, error). label rỗng nghĩa là không có trạng thái có thể khôi phục (nên tạo mới).
func buildResumePrompt(store *storepkg.Store) (string, string, error) {
	progress, err := store.Progress.Load()
	if err != nil && !os.IsNotExist(err) {
		return "", "", err
	}
	if progress == nil || progress.Phase == domain.PhaseComplete {
		return "", "", nil
	}

	label := describeResume(store, progress)

	var b strings.Builder
	title := progress.NovelName
	if title == "" {
		title = "tiểu thuyết hiện tại"
	}
	b.WriteString(fmt.Sprintf("[Khôi phục] Cuốn sách「%s」", title))
	if n := len(progress.CompletedChapters); n > 0 {
		b.WriteString(fmt.Sprintf("đã hoàn thành %d chương", n))
		if progress.TotalChapters > 0 {
			b.WriteString(fmt.Sprintf("（tổng %d chương）", progress.TotalChapters))
		}
		b.WriteString(fmt.Sprintf("，tổng %d từ", progress.TotalWordCount))
	}
	b.WriteString("。\n")
	b.WriteString("Host sẽ căn cứ thực tế hiện tại để gửi thông điệp `[Host ra lệnh]` bước tiếp theo. Nhận được thì thực thi ngay, không gọi novel_context suy luận trước.\n")

	if meta, _ := store.RunMeta.Load(); meta != nil && meta.PendingSteer != "" {
		b.WriteString("\nNgười dùng đã để lại một ý kiến can thiệp trong thời gian tạm dừng:\n「")
		b.WriteString(meta.PendingSteer)
		b.WriteString("」\nVui lòng đánh giá và xử lý theo quy tắc can thiệp người dùng trong coordinator.md trước.")
	}

	return b.String(), label, nil
}

// describeResume tạo nhãn khôi phục dễ đọc cho người dùng; không ảnh hưởng đến hành vi của Coordinator.
// Toàn bộ định tuyến thực thi do Flow Router suy ra từ thực tế; hàm này chỉ phục vụ UI với nhãn "Khôi phục: xxx".
func describeResume(store *storepkg.Store, progress *domain.Progress) string {
	switch progress.Phase {
	case domain.PhasePremise, domain.PhaseOutline:
		return fmt.Sprintf("Khôi phục: giai đoạn lập kế hoạch (%s)", progress.Phase)
	case domain.PhaseWriting:
		// Mức ưu tiên căn chỉnh theo mức ưu tiên quyết định của Router, để label khớp với lệnh sắp được phát ra.
		if pending, _ := store.Signals.LoadPendingCommit(); pending != nil {
			return fmt.Sprintf("Khôi phục: lưu chương %d bị gián đoạn", pending.Chapter)
		}
		if len(progress.PendingRewrites) > 0 {
			verb := "Viết lại"
			if progress.Flow == domain.FlowPolishing {
				verb = "Đánh bóng"
			}
			return fmt.Sprintf("%s khôi phục: %d chương chờ xử lý", verb, len(progress.PendingRewrites))
		}
		if progress.Flow == domain.FlowReviewing {
			return "Khôi phục: đánh giá bị gián đoạn"
		}
		if progress.InProgressChapter > 0 {
			return fmt.Sprintf("Khôi phục: chương %d đang tiến hành", progress.InProgressChapter)
		}
		if label := describeArcEndLabel(store, progress); label != "" {
			return label
		}
		return fmt.Sprintf("Khôi phục: tiếp tục từ chương %d", progress.NextChapter())
	}
	return "Khôi phục"
}

// describeArcEndLabel tạo nhãn UI phù hợp cho nhiều trạng thái trung gian ở cuối cung/cuối tập.
// Giữ cùng thứ tự với nhánh cuối cung trong flow.Route, đảm bảo label khớp với lệnh đầu tiên của Router.
func describeArcEndLabel(store *storepkg.Store, progress *domain.Progress) string {
	if !progress.Layered || len(progress.CompletedChapters) == 0 {
		return ""
	}
	lastCh := progress.CompletedChapters[len(progress.CompletedChapters)-1]
	boundary, err := store.Outline.CheckArcBoundary(lastCh)
	if err != nil || boundary == nil {
		return ""
	}
	// Giữ cùng thứ tự với Route: OutOfRange được xét trước nhánh cuối cung.
	if boundary.OutOfRange {
		return fmt.Sprintf("Khôi phục: đề cương thiếu chương %d, chờ quyết định tập tiếp theo", progress.NextChapter())
	}
	if !boundary.IsArcEnd {
		return ""
	}
	vol, arc := boundary.Volume, boundary.Arc
	switch {
	case !store.World.HasArcReview(lastCh):
		return fmt.Sprintf("Khôi phục: chờ đánh giá cuối cung (V%d A%d)", vol, arc)
	case !store.Summaries.HasArcSummary(vol, arc):
		return fmt.Sprintf("Khôi phục: chờ tạo tóm tắt cung (V%d A%d)", vol, arc)
	case boundary.IsVolumeEnd && !store.Summaries.HasVolumeSummary(vol):
		return fmt.Sprintf("Khôi phục: chờ tạo tóm tắt tập (V%d)", vol)
	case boundary.NeedsExpansion && boundary.NextArc > 0:
		return fmt.Sprintf("Khôi phục: chờ mở rộng cung tiếp theo (V%d A%d)", boundary.NextVolume, boundary.NextArc)
	case boundary.NeedsNewVolume:
		return fmt.Sprintf("Khôi phục: chờ quyết định tập tiếp theo (cuối V%d)", vol)
	}
	return ""
}
