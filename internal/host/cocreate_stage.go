package host

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/store"
)

// buildStoryStateSummary tạo một bản tóm tắt ngắn gọn về trạng thái hiện tại của truyện, giúp trợ lý đồng sáng tác theo giai đoạn nắm được "đã viết đến đâu".
// Tái sử dụng các điểm truy cập Store, chỉ lấy các thông tin cấp cao cần thiết cho định hướng lập kế hoạch (tiến độ / la bàn / tập gần nhất / nhân vật chính / phục bút đang mở);
// Không kéo nội dung chính văn, không nạp toàn bộ JSON của novel_context — đồng sáng tác là hội thoại, cần tổng quan dễ đọc, không phải ngữ cảnh viết lách.
// Bất kỳ mục nào thiếu đều bỏ qua (best-effort), trả về chuỗi rỗng nếu chưa có tiến độ khả dụng.
func buildStoryStateSummary(s *store.Store) string {
	if s == nil {
		return ""
	}
	var b strings.Builder

	if progress, _ := s.Progress.Load(); progress != nil {
		if name := strings.TrimSpace(progress.NovelName); name != "" {
			fmt.Fprintf(&b, "- Tên sách：《%s》\n", name)
		}
		fmt.Fprintf(&b, "- Tiến độ：Đã hoàn thành %d chương", len(progress.CompletedChapters))
		if progress.TotalChapters > 0 {
			fmt.Fprintf(&b, " / kế hoạch %d chương", progress.TotalChapters)
		}
		fmt.Fprintf(&b, "，khoảng %d chữ，chương tiếp theo là chương %d\n", progress.TotalWordCount, progress.NextChapter())
		if progress.Layered && progress.CurrentVolume > 0 {
			fmt.Fprintf(&b, "- Vị trí hiện tại：Tập %d Cung %d\n", progress.CurrentVolume, progress.CurrentArc)
		}
	}

	if compass, _ := s.Outline.LoadCompass(); compass != nil {
		if dir := strings.TrimSpace(compass.EndingDirection); dir != "" {
			fmt.Fprintf(&b, "- Hướng kết thúc：%s\n", dir)
		}
		if compass.EstimatedScale != "" {
			fmt.Fprintf(&b, "- Quy mô dự kiến：%s\n", compass.EstimatedScale)
		}
		if len(compass.OpenThreads) > 0 {
			fmt.Fprintf(&b, "- Tuyến dài đang mở：%s\n", strings.Join(compass.OpenThreads, "；"))
		}
	}

	// Tóm tắt tập gần nhất, giúp trợ lý biết truyện vừa đi đến đâu
	if vols, _ := s.Summaries.LoadAllVolumeSummaries(); len(vols) > 0 {
		last := vols[len(vols)-1]
		fmt.Fprintf(&b, "- Gần nhất《%s》：%s\n", last.Title, truncate(last.Summary, 200))
	}

	// Nhân vật chính (core/important), tối đa 8 người
	if chars, _ := s.Characters.Load(); len(chars) > 0 {
		var names []string
		for _, c := range chars {
			if c.Tier == "secondary" || c.Tier == "decorative" {
				continue
			}
			line := c.Name
			if role := strings.TrimSpace(c.Role); role != "" {
				line += "（" + role + "）"
			}
			names = append(names, line)
			if len(names) >= 8 {
				break
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "- Nhân vật chính：%s\n", strings.Join(names, "、"))
		}
	}

	// Phục bút chưa thu hồi, tối đa 6 mục
	if fs, _ := s.World.LoadActiveForeshadow(); len(fs) > 0 {
		var items []string
		for _, f := range fs {
			items = append(items, truncate(f.Description, 40))
			if len(items) >= 6 {
				break
			}
		}
		fmt.Fprintf(&b, "- Phục bút chưa thu：%s\n", strings.Join(items, "；"))
	}

	return strings.TrimSpace(b.String())
}

// stageSystemPrompt tạo system prompt đầy đủ cho đồng sáng tác theo giai đoạn: stage prompt + bản tóm tắt trạng thái truyện hiện tại.
// Bản tóm tắt được đính kèm ở cuối như phụ lục dữ liệu (ngăn cách bằng dòng kẻ), tương ứng với chỉ dẫn "tiến độ xem bên dưới" trong prompt.
func stageSystemPrompt(s *store.Store) string {
	prompt := stageCoCreateSystemPrompt
	if summary := buildStoryStateSummary(s); summary != "" {
		prompt += "\n\n---\n## Trạng thái truyện hiện tại\n（Đây là bản tóm tắt khách quan về nội dung đã viết, dùng để tham chiếu khi lập kế hoạch tiếp theo — không sao chép nguyên văn vào <draft>）\n" + summary
	}
	return prompt
}
