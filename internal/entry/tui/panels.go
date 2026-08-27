package tui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// renderTopBar hiển thị thanh trạng thái trên cùng.
// Trái: nhà cung cấp/model, giữa: tên sách, phải: viên trạng thái.
func renderTopBar(snap host.UISnapshot, width int, spinnerFrame, version string) string {
	novelName := snap.NovelName
	if novelName == "" {
		novelName = "Chưa đặt tên"
	}

	var infoParts []string
	if version != "" {
		infoParts = append(infoParts, "ainovel-cli "+version)
	}
	if snap.Provider != "" {
		infoParts = append(infoParts, snap.Provider)
	}
	if snap.ModelName != "" {
		if w := formatContextWindow(snap.ModelContextWindow); w != "" {
			infoParts = append(infoParts, snap.ModelName+"("+w+")")
		} else {
			infoParts = append(infoParts, snap.ModelName)
		}
	}
	if snap.Style != "" && snap.Style != "default" {
		infoParts = append(infoParts, snap.Style)
	}
	leftText := strings.Join(infoParts, " · ")

	label := snap.StatusLabel
	if label == "" {
		label = "READY"
	}
	color, ok := statusColors[label]
	if !ok {
		color = colorDim
	}
	disp, ok := statusDisplay[label]
	if !ok {
		disp = struct {
			icon  string
			label string
		}{"○", strings.ToLower(label)}
	}
	icon := disp.icon
	if snap.IsRunning && spinnerFrame != "" {
		icon = spinnerFrame
	}
	var status string
	if icon != "" {
		status = statusIconStyle.Foreground(color).Render(icon) + " " + statusLabelStyle.Render(disp.label)
	} else {
		status = statusLabelStyle.Render(disp.label)
	}

	innerW := max(12, width-2)
	titleText := truncate(novelName, max(8, innerW/3))
	centerW := max(16, lipgloss.Width(titleText)+6)
	if centerW > innerW-24 {
		centerW = max(8, innerW-24)
	}
	sideTotal := innerW - centerW
	if sideTotal < 0 {
		sideTotal = 0
		centerW = innerW
	}
	leftW := sideTotal / 2
	rightW := innerW - centerW - leftW

	leftCell := lipgloss.NewStyle().
		Width(leftW).
		AlignHorizontal(lipgloss.Left).
		Foreground(colorDim).
		Render(truncate(leftText, leftW))
	centerCell := lipgloss.NewStyle().
		Width(centerW).
		AlignHorizontal(lipgloss.Center).
		Bold(true).
		Foreground(bodyTextColor).
		Render(titleText)
	rightCell := lipgloss.NewStyle().
		Width(rightW).
		AlignHorizontal(lipgloss.Right).
		Render(status)

	content := leftCell + centerCell + rightCell
	return topBarStyle.Width(width).
		Border(baseBorder, false, false, true, false).
		BorderForeground(colorDim).
		Render(content)
}

// renderStatePanel bọc nội dung thanh trạng thái bên trái (đã có trong stateVP) vào hộp có đường viền bên phải.
// Đối xứng với renderDetailPanel: nội dung được tạo bởi renderStateContent và đưa vào viewport, hàm này chỉ lo khung.
// MaxHeight giới hạn chiều cao, tránh tràn cao hơn cột phải khi thu nhỏ cửa sổ (xem hợp đồng chiều cao trong panels_test.go).
func renderStatePanel(vp viewport.Model, width, height int, focused bool) string {
	borderColor := colorDim
	if focused {
		borderColor = colorAccent
	}
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		Border(baseBorder, false, true, false, false).
		BorderForeground(borderColor).
		Padding(1, 1)
	return style.Render(vp.View())
}

// renderStateContent tạo nội dung thuần túy của thanh trạng thái bên trái (không có viền/khung ngoài), dùng cho stateVP.SetContent.
func renderStateContent(snap host.UISnapshot, contentW int) string {
	contentW = max(12, contentW)
	agents := sidebarAgents(snap.Agents)
	idleAgents := sidebarIdleAgents(snap.Agents)
	var sections []string

	if snap.RecoveryLabel != "" {
		sections = append(sections, lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render(truncate(snap.RecoveryLabel, contentW)))
	}

	var overview strings.Builder
	overview.WriteString(renderField("Trạng thái", snapshotRuntimeStateLabel(snap.RuntimeState)))
	overview.WriteString(renderField("Giai đoạn", snapshotPhaseLabel(snap.Phase)))
	overview.WriteString(renderField("Luồng", snapshotFlowLabel(snap.Flow)))
	if snap.Layered {
		overview.WriteString(renderField("Đã hoàn thành", fmt.Sprintf("%d chương", snap.CompletedCount)))
		// Quy hoạch động phân lớp: cột phải chỉ hiển thị các chương đã mở rộng trong cung hiện tại, "Đã lên kế hoạch" cũng dùng cùng tiêu chí,
		// tránh trộn ước tính thô của EstimatedChapters trong cung khung (ví dụ 92) vào, không khớp với đề cương hiển thị.
		// Giá trị TotalChapters trong progress chỉ dùng cho quyết định ContextProfile nội bộ, không hiển thị ra UI.
		if planned := len(snap.Outline); planned > 0 {
			overview.WriteString(renderField("Đã lên kế hoạch", fmt.Sprintf("%d chương", planned)))
		}
	} else {
		switch {
		case snap.TotalChapters > 0:
			overview.WriteString(renderField("Tiến độ", fmt.Sprintf("%d / %d chương", snap.CompletedCount, snap.TotalChapters)))
		default:
			overview.WriteString(renderField("Đã hoàn thành", fmt.Sprintf("%d chương", snap.CompletedCount)))
		}
	}
	overview.WriteString(renderField("Số từ", formatNumber(snap.TotalWordCount)))
	if label, ch := inProgressDisplay(snap); label != "" {
		overview.WriteString(renderField(label, fmt.Sprintf("Chương %d", ch)))
	}
	if headline := snapshotHeadline(snap); headline != "" {
		label := "Hiện tại"
		if !snap.IsRunning {
			label = "Chờ tiếp tục"
		}
		overview.WriteString(renderHighlightField(label, truncate(headline, contentW-10)))
	}
	sections = append(sections, renderSidebarSection("Tổng quan", overview.String(), contentW))

	if len(agents) > 0 {
		var agentBody strings.Builder
		for _, agent := range agents {
			agentBody.WriteString(renderAgentLine(agent, contentW))
			agentBody.WriteString("\n")
		}
		if len(idleAgents) > 0 {
			agentBody.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render("Chờ: " + truncate(strings.Join(idleAgents, " · "), max(8, contentW-2))))
			agentBody.WriteString("\n")
		}
		sections = append(sections, renderSidebarSection("Nhân vật đang chạy", agentBody.String(), contentW))
	}

	if len(snap.PendingRewrites) > 0 {
		var rewrite strings.Builder
		rewrite.WriteString(renderHighlightField("Hàng đợi", fmt.Sprintf("%v", snap.PendingRewrites)))
		if snap.RewriteReason != "" {
			rewrite.WriteString(renderField("Lý do", truncate(snap.RewriteReason, contentW-10)))
		}
		sections = append(sections, renderSidebarSection("Viết lại", rewrite.String(), contentW))
	}

	if snap.PendingSteer != "" {
		sections = append(sections, renderSidebarSection("Can thiệp",
			renderHighlightField("Đang chờ", truncate(snap.PendingSteer, contentW-10)), contentW))
	}

	if body := renderUsageSidebar(snap, contentW); body != "" {
		sections = append(sections, renderSidebarSection("Sử dụng", body, contentW))
	}

	if body := renderCacheSidebar(snap, contentW); body != "" {
		sections = append(sections, renderSidebarSection("Bộ nhớ đệm", body, contentW))
	}

	if body := renderContextSidebar(snap, contentW); body != "" {
		sections = append(sections, renderSidebarSection("Ngữ cảnh", body, contentW))
	}

	return strings.Join(sections, "\n\n")
}

func renderAgentLine(agent host.AgentSnapshot, width int) string {
	stateColor := taskStatusColor(agent.State)
	icon := lipgloss.NewStyle().Foreground(stateColor).Render(agentStateIcon(agent.State))
	badge := lipgloss.NewStyle().Foreground(stateColor).Render(agentStateLabel(agent.State))
	name := lipgloss.NewStyle().Bold(true).Foreground(bodyTextColor).Render(agentDisplayName(agent.Name))
	line := icon + " " + name + " " + badge

	taskLine := agentTaskLine(agent)
	if taskLine != "" {
		line += "\n" + lipgloss.NewStyle().Foreground(colorDim).Render("  "+truncate(taskLine, max(8, width-2)))
	}

	detail := agent.Summary
	if agent.Tool != "" {
		detail = agent.Tool
	}
	if agent.State == "idle" && detail == "Chờ lệnh" {
		detail = ""
	}
	if detail != "" && detail != taskLine {
		line += "\n" + lipgloss.NewStyle().Foreground(colorMuted).Render("  "+truncate(detail, max(8, width-2)))
	}
	if ctx := agentContextLine(agent); ctx != "" {
		line += "\n" + lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("  "+truncate(ctx, max(8, width-2)))
	}
	return line
}

func renderSidebarSection(title, body string, width int) string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return ""
	}
	lineW := max(0, width-lipgloss.Width(title)-1)
	header := panelTitleStyle.Render(title) + " " +
		lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))
	card := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colorDim).
		PaddingLeft(1).
		Render(body)
	return header + "\n" + card
}

func sidebarAgents(agents []host.AgentSnapshot) []host.AgentSnapshot {
	var out []host.AgentSnapshot
	for _, agent := range agents {
		if agent.State == "idle" {
			continue
		}
		out = append(out, agent)
	}
	if len(out) == 0 {
		out = append(out, agents...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := out[i], out[j]
		if agentStateRank(li.State) != agentStateRank(lj.State) {
			return agentStateRank(li.State) < agentStateRank(lj.State)
		}
		return agentOrder(li.Name) < agentOrder(lj.Name)
	})
	return out
}

func sidebarIdleAgents(agents []host.AgentSnapshot) []string {
	var names []string
	hasActive := false
	for _, agent := range agents {
		if agent.State != "idle" {
			hasActive = true
			continue
		}
		names = append(names, agentDisplayName(agent.Name))
	}
	if !hasActive {
		return nil
	}
	sort.Strings(names)
	return names
}

// inProgressDisplay tính nhãn và số chương của trường "đang tiến hành".
// Chọn động từ theo flow (đánh bóng/viết lại/viết); khi in_progress_chapter không khớp với flow thì coi là stale:
//   - Chế độ polishing/rewriting mà chương không có trong pending_rewrites → quay về chương đầu hàng đợi
//   - Không hiển thị khi trường bằng 0
func inProgressDisplay(snap host.UISnapshot) (label string, chapter int) {
	ch := snap.InProgressChapter
	switch snap.Flow {
	case "polishing":
		if ch <= 0 || !slices.Contains(snap.PendingRewrites, ch) {
			if len(snap.PendingRewrites) == 0 {
				return "", 0
			}
			ch = snap.PendingRewrites[0]
		}
		return "Đang đánh bóng", ch
	case "rewriting":
		if ch <= 0 || !slices.Contains(snap.PendingRewrites, ch) {
			if len(snap.PendingRewrites) == 0 {
				return "", 0
			}
			ch = snap.PendingRewrites[0]
		}
		return "Đang viết lại", ch
	default:
		if ch <= 0 {
			return "", 0
		}
		return "Đang viết", ch
	}
}

func snapshotHeadline(snap host.UISnapshot) string {
	if snap.PendingSteer != "" {
		if !snap.IsRunning {
			return "Chờ tiếp tục: xử lý can thiệp của người dùng"
		}
		return "Đang chờ xử lý can thiệp của người dùng"
	}
	if len(snap.PendingRewrites) > 0 {
		if !snap.IsRunning {
			return "Chờ tiếp tục: xử lý viết lại"
		}
		return "Đang chờ xử lý viết lại"
	}
	return ""
}

func snapshotPhaseLabel(phase string) string {
	switch phase {
	case "premise":
		return "Tiền đề"
	case "outline":
		return "Đề cương"
	case "writing":
		return "Viết"
	case "complete":
		return "Hoàn thành"
	case "init":
		return "Khởi tạo"
	default:
		if phase == "" {
			return "-"
		}
		return phase
	}
}

func snapshotRuntimeStateLabel(state string) string {
	switch state {
	case "running":
		return "Đang chạy"
	case "pausing":
		return "Đang tạm dừng"
	case "paused":
		return "Đã tạm dừng"
	case "completed":
		return "Đã hoàn thành"
	default:
		return "Rảnh"
	}
}

func snapshotFlowLabel(flow string) string {
	switch flow {
	case "":
		return "-"
	case "writing":
		return "Viết"
	case "reviewing":
		return "Đánh giá"
	case "rewriting":
		return "Viết lại"
	case "polishing":
		return "Đánh bóng"
	case "steering":
		return "Can thiệp"
	default:
		return flow
	}
}

func renderUsageSidebar(snap host.UISnapshot, width int) string {
	if snap.TotalInputTokens <= 0 && snap.TotalOutputTokens <= 0 && snap.TotalCostUSD <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(renderField("Đầu vào", formatTokensCompact(snap.TotalInputTokens)))
	b.WriteString(renderField("Đầu ra", formatTokensCompact(snap.TotalOutputTokens)))
	if cost := formatCostUSD(snap.TotalCostUSD); cost != "" {
		b.WriteString(renderField("Chi phí", cost))
	}
	if saved := formatCostUSD(snap.TotalSavedUSD); saved != "" {
		b.WriteString(renderField("Tiết kiệm", saved))
	}
	if snap.BudgetLimitUSD > 0 {
		pct := snap.TotalCostUSD / snap.BudgetLimitUSD * 100
		b.WriteString(renderField("Ngân sách", fmt.Sprintf("$%.2f/$%.2f (%.0f%%)", snap.TotalCostUSD, snap.BudgetLimitUSD, pct)))
	}

	agentStats := usageStatsByCost(snap.CachePerAgent)
	if len(agentStats) > 0 {
		b.WriteString(renderUsageGroupHeader("Nhân vật", width))
		limit := min(len(agentStats), 4)
		for i := 0; i < limit; i++ {
			a := agentStats[i]
			b.WriteString(renderUsageLine(agentDisplayName(a.Role), eventAgentColor(a.Role), a.Input, a.Output, a.Cost, width))
			b.WriteString("\n")
		}
	}
	modelStats := usageStatsByCost(snap.CachePerModel)
	if len(modelStats) > 0 {
		b.WriteString(renderUsageGroupHeader("Model", width))
		limit := min(len(modelStats), 4)
		for i := 0; i < limit; i++ {
			a := modelStats[i]
			b.WriteString(renderUsageLine(modelDisplayName(a.Model), bodyTextColor, a.Input, a.Output, a.Cost, width))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func usageStatsByCost(in []host.AgentCacheStat) []host.AgentCacheStat {
	out := append([]host.AgentCacheStat(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Input+out[i].Output > out[j].Input+out[j].Output
	})
	return out
}

func renderUsageGroupHeader(label string, width int) string {
	line := lipgloss.NewStyle().Foreground(colorDim).
		Render(strings.Repeat("·", max(8, width-lipgloss.Width(label)-3)))
	return lipgloss.NewStyle().Foreground(colorMuted).Render(label+" ") + line + "\n"
}

func renderUsageLine(name string, color lipgloss.TerminalColor, input, output int, cost float64, width int) string {
	nameW := 11
	if width < 24 {
		nameW = 8
	}
	nameCell := lipgloss.NewStyle().Foreground(color).Width(nameW).
		Render(truncate(name, nameW))
	tokens := formatTokensCompact(input + output)
	right := tokens
	if costStr := formatCostUSD(cost); costStr != "" {
		right += " · " + costStr
	}
	return fitInlineLine(nameCell+lipgloss.NewStyle().Foreground(colorDim).Render(right), width)
}

func modelDisplayName(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "unknown"
	}
	parts := strings.Split(model, "/")
	if len(parts) >= 3 {
		return strings.Join(parts[1:], "/")
	}
	if len(parts) == 2 {
		return parts[1]
	}
	return model
}

// renderCacheSidebar hiển thị khu vực "Bộ nhớ đệm" ở cột trái.
//
// Ba trạng thái:
//  1. Chưa tiêu thụ token nào: trả về rỗng, không hiển thị section
//  2. Tất cả role trong phiên hiện tại đều dùng model không hỗ trợ prompt cache: chỉ hiển thị một dòng "Chưa bật"
//  3. Đã bật: trên cùng "Tỉ lệ hit tích lũy/gần 10 · tiết kiệm · đọc/ghi" + phân cách + từng dòng role
//
// Dòng role: khi capable hiển thị hai số "tích lũy/gần N%"; khi không capable hiển thị "Chưa bật".
// So sánh tích lũy vs gần N lần có thể nhận biết "bị kéo tụt giai đoạn đầu" vs "hit thấp ở trạng thái ổn định".
func renderCacheSidebar(snap host.UISnapshot, width int) string {
	// Upstream streaming không gửi OpenAI final usage chunk — dữ liệu tích lũy đều bằng 0,
	// nhưng đây không phải "chưa bật cache" hay "lượng dùng quá thấp bị ẩn đi", phải hiển thị rõ,
	// nếu không người dùng sẽ cứ tưởng code cache đã viết nhưng không hiển thị. Ưu tiên cao nhất.
	if snap.MissingAssistantUsage > 0 && snap.TotalInputTokens <= 0 {
		warn := lipgloss.NewStyle().Foreground(colorError).Bold(true).
			Render(fmt.Sprintf("⚠ Upstream chưa trả usage (%d lần)", snap.MissingAssistantUsage))
		hint := lipgloss.NewStyle().Foreground(colorDim).Italic(true).
			Render(truncate("Kiểm tra provider stream_options.include_usage", max(8, width-2)))
		return warn + "\n" + hint + "\n"
	}

	if snap.TotalInputTokens <= 0 && snap.TotalCacheWriteTokens <= 0 {
		return ""
	}

	// Chưa bật toàn bộ → hiển thị một dòng giải thích, tránh người dùng nhầm "0% hit cần điều tra"
	if !snap.OverallCacheCapable && snap.TotalCacheReadTokens == 0 && snap.TotalCacheWriteTokens == 0 {
		return lipgloss.NewStyle().Foreground(colorDim).Italic(true).
			Render(truncate("Model hiện tại chưa bật prompt cache", max(8, width-2))) + "\n"
	}

	var b strings.Builder

	// Chỉ số tổng hợp trên cùng: tích lũy + gần N mỗi dòng một hàng, nhãn rõ ràng, tránh "X% · gần N Y%" trộn
	// ba loại dấu phân cách (dấu phần trăm / chấm giữa / chữ) gây mơ hồ ngữ nghĩa.
	overallHit := cacheHitRate(snap.TotalCacheReadTokens, snap.TotalInputTokens)
	b.WriteString(renderField("Hit tích lũy", colorPercent(overallHit)))
	if snap.OverallRecentSamples > 0 && snap.OverallRecentInput > 0 {
		recent := cacheHitRate(snap.OverallRecentCacheRead, snap.OverallRecentInput)
		b.WriteString(renderField(fmt.Sprintf("Hit gần %d", snap.OverallRecentSamples), colorPercent(recent)))
	}

	if savedStr := formatCostUSD(snap.TotalSavedUSD); savedStr != "" {
		b.WriteString(renderField("Tiết kiệm", savedStr))
	}

	// Lượng đọc/ghi mỗi dòng một hàng. Lượng ghi bằng 0 là bình thường với OpenAI / Gemini —
	// hai hãng này dùng automatic transparent caching, ghi cache hoàn toàn miễn phí (lần đầu chưa hit tính giá input bình thường,
	// tạo cache không thu thêm phụ phí), nên giao thức không có trường cache_creation, không cần thiết.
	// Chỉ Anthropic / Bedrock mới báo lượng ghi vì họ tính phụ phí ghi (5m +25%/1h +100%),
	// phải hiển thị để người dùng tính hóa đơn.
	b.WriteString(renderField("Đọc cache", formatTokensCompact(snap.TotalCacheReadTokens)))
	if snap.TotalCacheWriteTokens > 0 {
		b.WriteString(renderField("Ghi cache", formatTokensCompact(snap.TotalCacheWriteTokens)))
	} else if snap.TotalCacheReadTokens > 0 {
		hint := lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("(cache tự động không phụ phí)")
		b.WriteString(renderField("Ghi cache", "0 "+hint))
	}

	if len(snap.CachePerAgent) > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(colorDim).
			Render(strings.Repeat("·", max(8, width-12))))
		b.WriteString("\n")
		for _, a := range snap.CachePerAgent {
			b.WriteString(renderCacheAgentLine(a, width))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// colorPercent tô màu chuỗi phần trăm theo tỉ lệ hit, chỉ dùng cho cột giá trị.
func colorPercent(p float64) string {
	return lipgloss.NewStyle().Foreground(cacheHitColor(p)).Bold(true).
		Render(formatPercent(p))
}

// renderCacheAgentLine hiển thị dòng từng role: tên role + tỉ lệ hit + lượng đọc cache / tổng đầu vào.
//
// Hiển thị cả tử số lẫn mẫu số (cacheRead / input) để người dùng nhìn là kiểm chứng được tỉ lệ hit,
// cũng có thể nhận ra "phần trăm cao nhưng mẫu nhỏ" là dữ liệu may mắn (ví dụ 100% / 1k ít tin cậy hơn 80% / 300k).
//
// Phần trăm ưu tiên dùng giá trị ổn định của cửa sổ trượt; khi cửa sổ không có mẫu thì dùng tích lũy.
// Toàn bộ cột trái chỉ có chỗ này dùng "/", ngữ nghĩa chuyên dụng (phép chia toán học: lượng hit cache / tổng đầu vào), không lẫn với dấu phân cách khác.
//
// Ba trạng thái:
//
//	Chưa bật     "WRITER        Chưa bật"
//	Đã bật       "WRITER        85%  · 323k / 394k"
//	Không cache  hiển thị rõ "Chưa bật", không trộn 0/0 gây nhầm lẫn
func renderCacheAgentLine(a host.AgentCacheStat, width int) string {
	// Tên role giữ nhất quán với khu "Nhân vật đang chạy"; Width 12 để COORDINATOR dài nhất
	// vẫn còn 1 cột dấu cách làm phân cách, các role khác tự đệm bên phải.
	roleStyle := lipgloss.NewStyle().Foreground(eventAgentColor(a.Role)).Width(12)
	role := roleStyle.Render(agentDisplayName(a.Role))

	if !a.CacheCapable {
		dim := lipgloss.NewStyle().Foreground(colorDim).Italic(true)
		_ = width
		return role + dim.Render("Chưa bật")
	}

	// Ưu tiên tỉ lệ hit ổn định; khi cửa sổ không có mẫu thì dùng tích lũy.
	hit := cacheHitRate(a.RecentCacheRead, a.RecentInput)
	if a.RecentSamples == 0 || a.RecentInput == 0 {
		hit = cacheHitRate(a.CacheRead, a.Input)
	}
	// Phần trăm cố định rộng 4 cột ("100%"), tránh cột lượng đọc nhảy trái phải giữa "5%" và "85%".
	pctCell := lipgloss.NewStyle().Width(4).
		Render(colorPercent(hit))

	// Đọc tích lũy / đầu vào tích lũy — dù phần trăm bên trên là giá trị cửa sổ trượt, tử mẫu đều dùng tích lũy, vì
	// "thấy quy mô" mới là nhu cầu chính của cột này; phần trăm riêng đã cung cấp tín hiệu ổn định.
	tokens := lipgloss.NewStyle().Foreground(colorDim).Render(
		" · " + formatTokensCompact(a.CacheRead) + " / " + formatTokensCompact(a.Input))
	_ = width
	return role + pctCell + tokens
}

// cacheHitRate tính phần trăm bằng cách chia trực tiếp với ngữ nghĩa input đã bao gồm cacheRead.
// Trả về 0 khi input == 0, tránh hit giả.
func cacheHitRate(cacheRead, input int) float64 {
	if input <= 0 {
		return 0
	}
	return float64(cacheRead) / float64(input) * 100
}

// cacheHitColor tô màu tỉ lệ hit: >=50% xanh lá / 20-50% vàng / <20% đỏ.
// Ngược chiều với tỉ lệ sử dụng ngữ cảnh: tỉ lệ hit cache càng cao càng tốt.
func cacheHitColor(percent float64) lipgloss.AdaptiveColor {
	switch {
	case percent >= 50:
		return colorSuccess
	case percent >= 20:
		return colorReview
	default:
		return colorError
	}
}

func formatPercent(p float64) string {
	if p <= 0 {
		return "0%"
	}
	if p < 10 {
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

// formatTokensCompact hiển thị số token dưới dạng gọn "8.2k" / "1.4M".
// Dùng cho dòng per-role hẹp, tránh xung đột với kiểu dấu phẩy của formatNumber.
func formatTokensCompact(n int) string {
	if n <= 0 {
		return "0"
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func renderContextSidebar(snap host.UISnapshot, width int) string {
	if snap.ContextWindow <= 0 && snap.ContextStrategy == "" && snap.ContextScope == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(renderContextUsageField("Ngữ cảnh chính", snap.ContextPercent, snap.ContextTokens, snap.ContextWindow))
	if strategy := contextStrategyLabel(snap.ContextStrategy); strategy != "" {
		b.WriteString(renderField("Chiến lược gần nhất", truncate(strategy, max(8, width-12))))
	}
	if scope := contextScopeLabel(snap.ContextScope); scope != "" {
		b.WriteString(renderField("Chế độ xem hiện tại", scope))
	}
	if snap.ContextSummaryCount > 0 {
		b.WriteString(renderField("Tóm tắt", fmt.Sprintf("%d mục", snap.ContextSummaryCount)))
	}
	if snap.ContextActiveMessages > 0 {
		b.WriteString(renderField("Số tin nhắn", fmt.Sprintf("%d", snap.ContextActiveMessages)))
	}
	if snap.ContextCompactedCount > 0 || snap.ContextKeptCount > 0 {
		b.WriteString(renderField("Viết lại gần nhất", fmt.Sprintf("%d → %d", snap.ContextCompactedCount, snap.ContextKeptCount)))
	}
	return b.String()
}

func contextScopeLabel(scope string) string {
	switch scope {
	case "baseline":
		return "Cơ sở"
	case "projected":
		return "Dự kiến"
	case "recovered":
		return "Đã khôi phục"
	case "committed":
		return "Đã lưu"
	case "skipped":
		return "Bỏ qua do ngắt mạch"
	default:
		return scope
	}
}

func contextStrategyLabel(strategy string) string {
	switch strategy {
	case "":
		return ""
	case "tool_result_microcompact":
		return "Nén nhỏ kết quả công cụ"
	case "light_trim":
		return "Cắt nhẹ"
	case "full_summary":
		return "Tóm tắt toàn bộ"
	default:
		return strategy
	}
}

func agentDisplayName(name string) string {
	return strings.ToUpper(name)
}

func agentTaskLine(agent host.AgentSnapshot) string {
	if agent.TaskKind != "" {
		return taskKindLabel(agent.TaskKind)
	}
	if agent.Summary != "" {
		return agent.Summary
	}
	return ""
}

func agentContextLine(agent host.AgentSnapshot) string {
	ctx := agent.Context
	if ctx.ContextWindow <= 0 || ctx.Tokens <= 0 {
		return ""
	}
	percentColor := contextPercentColor(ctx.Percent)
	percentStr := lipgloss.NewStyle().Foreground(percentColor).Render(fmt.Sprintf("ctx %.0f%%", ctx.Percent))
	parts := []string{percentStr}
	if scope := contextScopeLabel(ctx.Scope); scope != "" {
		parts = append(parts, scope)
	}
	if strategy := contextStrategyLabel(ctx.Strategy); strategy != "" {
		parts = append(parts, strategy)
	}
	return strings.Join(parts, " · ")
}

func agentStateRank(state string) int {
	switch state {
	case "running":
		return 0
	case "failed":
		return 1
	default:
		return 2
	}
}

func agentOrder(name string) int {
	switch {
	case strings.HasPrefix(name, "architect"):
		return 0
	case name == "coordinator":
		return 1
	case name == "editor":
		return 2
	case name == "writer":
		return 3
	default:
		return 9
	}
}

func agentStateLabel(state string) string {
	switch state {
	case "running":
		return "Đang chạy"
	case "failed":
		return "Lỗi"
	case "idle":
		return "Chờ lệnh"
	default:
		return state
	}
}

func agentStateIcon(state string) string {
	switch state {
	case "running":
		return "●"
	case "failed":
		return "×"
	default:
		return "·"
	}
}

func taskStatusColor(status string) lipgloss.AdaptiveColor {
	switch status {
	case "running":
		return colorSuccess
	case "queued":
		return colorMuted
	case "failed", "canceled":
		return colorError
	case "succeeded":
		return colorSuccess
	default:
		return colorDim
	}
}

func taskKindLabel(kind string) string {
	switch kind {
	case "foundation_plan":
		return "Lên kế hoạch nền tảng"
	case "chapter_write":
		return "Viết chương"
	case "chapter_review":
		return "Đánh giá chương"
	case "chapter_rewrite":
		return "Viết lại chương"
	case "chapter_polish":
		return "Đánh bóng chương"
	case "arc_expand":
		return "Mở rộng cung"
	case "volume_append":
		return "Lên kế hoạch tập tiếp theo"
	case "steer_apply":
		return "Xử lý can thiệp"
	case "coordinator_decision":
		return "Điều phối tiến trình"
	default:
		return kind
	}
}

// renderEventContent hiển thị danh sách sự kiện thành luồng sự kiện phân cấp.
// DISPATCH là tiêu đề cấp cao nhất, công cụ của agent phụ được thụt lề, tạo cây điều phối rõ ràng.
// spinnerFrame dùng để hiển thị biểu tượng động cho dòng "đang tiến hành" (đồng bộ với spinner của topbar).
func renderEventContent(events []host.Event, width, spinnerFrame int) string {
	var b strings.Builder
	for i, ev := range events {
		b.WriteString(renderEventLine(ev, width, spinnerFrame))
		if i < len(events)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Khung spinner cho sự kiện gọi đang chạy (bubbles.Spinner.Dot, độc lập với MiniDot của thanh trên cùng).
var eventRunningFrames = toolSpinnerFrames

func runningSpinner(frame int) string {
	return eventRunningFrames[frame%len(eventRunningFrames)]
}

func renderEventLine(ev host.Event, width, spinnerFrame int) string {
	tsStr := lipgloss.NewStyle().Foreground(colorDim).Render(ev.Time.Format("15:04:05"))
	indent := ""
	if ev.Depth > 0 {
		indent = "  "
	}
	maxSumW := max(20, width-12-ev.Depth*2)

	running := ev.Running()
	durStr := renderEventDuration(ev.Duration)

	switch {
	case ev.Category == "DISPATCH":
		// Ba trạng thái: đang chạy (accent spinner + đậm) / thất bại (đỏ ✕) / hoàn thành (xanh ✓)
		var icon string
		switch {
		case running:
			icon = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(runningSpinner(spinnerFrame))
		case ev.Failed:
			icon = lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
		default:
			icon = lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
		}
		sum := renderDispatchSummary(ev.Summary, maxSumW)
		if running {
			// Đang chạy giữ nguyên nhưng in đậm
			sum = lipgloss.NewStyle().Bold(true).Render(sum)
		}
		line := tsStr + " " + icon + " " + sum
		if !running {
			line += durStr
		}
		return line

	case ev.Category == "DONE":
		// Tương thích dữ liệu replay cũ; luồng mới không tạo sự kiện DONE độc lập nữa
		icon := lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
		color := eventAgentColor(ev.Agent)
		name := lipgloss.NewStyle().Foreground(color).Render(agentDisplayName(ev.Agent))
		return tsStr + " " + icon + " " + name + durStr

	case ev.Category == "TOOL" && ev.Depth == 0:
		// Công cụ của coordinator
		var icon, sum string
		switch {
		case running:
			icon = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(runningSpinner(spinnerFrame))
			sum = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(truncate(ev.Summary, maxSumW))
		case ev.Failed:
			icon = lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
			sum = lipgloss.NewStyle().Foreground(colorError).Render(truncate(ev.Summary, maxSumW))
		default:
			icon = lipgloss.NewStyle().Foreground(colorTool).Render("◇")
			sum = lipgloss.NewStyle().Foreground(colorTool).Render(truncate(ev.Summary, maxSumW))
		}
		line := tsStr + " " + icon + " " + sum
		if !running {
			line += durStr
		}
		return line

	case ev.Category == "TOOL":
		// Công cụ nội bộ của agent phụ (Depth=1)
		var icon, sum string
		switch {
		case running:
			icon = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(runningSpinner(spinnerFrame))
			sum = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(truncate(ev.Summary, maxSumW))
		case ev.Failed:
			icon = lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
			sum = lipgloss.NewStyle().Foreground(colorError).Render(truncate(ev.Summary, maxSumW))
		default:
			icon = lipgloss.NewStyle().Foreground(colorDim).Render("├")
			sum = lipgloss.NewStyle().Foreground(colorMuted).Render(truncate(ev.Summary, maxSumW))
		}
		line := tsStr + " " + indent + icon + " " + sum
		if !running {
			line += durStr
		}
		return line

	case ev.Category == "ERROR":
		icon := lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
		errStyle := lipgloss.NewStyle().Foreground(colorError)
		lines := wrapStreamText(ev.Summary, maxSumW)
		first := tsStr + " " + indent + icon + " " + errStyle.Render(lines[0])
		pad := strings.Repeat(" ", 10+len(indent))
		for _, l := range lines[1:] {
			first += "\n" + pad + errStyle.Render(l)
		}
		if durStr != "" {
			first += durStr
		}
		return first

	case ev.Category == "SYSTEM":
		icon := lipgloss.NewStyle().Foreground(colorAccent).Render("⚙")
		sumColor := colorMuted
		if ev.Level == "warn" {
			sumColor = colorAccent
		}
		sum := lipgloss.NewStyle().Foreground(sumColor).Render(truncate(ev.Summary, maxSumW))
		return tsStr + " " + indent + icon + " " + sum

	case ev.Category == "USER":
		// Echo lại văn bản Steer / Continue mà người dùng gửi từ ô nhập; khác hình thái với ⚙ của SYSTEM, dùng ✎ gợi ý "nhập liệu".
		// Màu dùng colorAccent2 (xanh ngọc) để phân biệt với vàng của SYSTEM, tránh đọc nhầm thành tin nhắn hệ thống.
		icon := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render("✎")
		sum := lipgloss.NewStyle().Foreground(colorAccent2).Render(truncate(ev.Summary, maxSumW))
		return tsStr + " " + indent + icon + " " + sum

	case ev.Category == "CONTEXT" || ev.Category == "COMPACT":
		icon := lipgloss.NewStyle().Foreground(colorContext).Render("⚙")
		sumColor := colorContext
		if ev.Level == "debug" {
			sumColor = colorMuted
		}
		sum := lipgloss.NewStyle().Foreground(sumColor).Render(truncate(ev.Summary, maxSumW))
		return tsStr + " " + indent + icon + " " + sum

	default:
		// Category đã biết dùng màu ánh xạ; category chưa biết theo màu mặc định terminal, tránh ép colorText.
		if color, ok := categoryColors[ev.Category]; ok {
			icon := lipgloss.NewStyle().Foreground(color).Render("·")
			sum := lipgloss.NewStyle().Foreground(color).Render(truncate(ev.Summary, maxSumW))
			return tsStr + " " + indent + icon + " " + sum
		}
		icon := lipgloss.NewStyle().Foreground(colorDim).Render("·")
		return tsStr + " " + indent + icon + " " + truncate(ev.Summary, maxSumW)
	}
}

// renderDispatchSummary hiển thị tóm tắt DISPATCH: tên Agent dùng màu theo role, nhiệm vụ dùng màu mờ.
func renderDispatchSummary(summary string, maxW int) string {
	agentName := summary
	taskPart := ""
	if idx := strings.Index(summary, "（"); idx > 0 {
		agentName = summary[:idx]
		taskPart = summary[idx:]
	}
	displayName := agentDisplayName(agentName)
	color := eventAgentColor(agentName)
	nameW := lipgloss.Width(displayName)
	if nameW >= maxW {
		return lipgloss.NewStyle().Foreground(color).Bold(true).Render(truncate(displayName, maxW))
	}
	result := lipgloss.NewStyle().Foreground(color).Bold(true).Render(displayName)
	if taskPart != "" {
		remaining := maxW - nameW
		if remaining > 2 {
			result += lipgloss.NewStyle().Foreground(colorMuted).Render(truncate(taskPart, remaining))
		}
	}
	return result
}

// eventAgentColor trả về màu chủ đề tương ứng với role của Agent.
func eventAgentColor(agent string) lipgloss.AdaptiveColor {
	switch {
	case strings.HasPrefix(agent, "architect"):
		return colorAccent2
	case agent == "writer":
		return colorTool
	case agent == "editor":
		return colorReview
	default:
		return colorAccent
	}
}

// renderEventDuration hiển thị Duration với chú thích ngoặc màu mờ, trả về rỗng nếu bằng 0.
func renderEventDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return " " + lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("("+formatDuration(d)+")")
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}

func renderEventActivity(snap host.UISnapshot, frame, width int) string {
	if !snap.IsRunning {
		return ""
	}
	return renderEventSparkle(frame, width)
}

var sparkleFrames = []string{
	"✦  ·   ✧   ·  ✦",
	"·  ✧   ·  ✦   ·",
	"  ✧   ·  ✦   · ",
	"   ·  ✦   ·  ✧ ",
	"✧   ·  ✦  ·   ✧",
	" ·  ✧   ·  ✦  ·",
	"✦   ·  ✧   ·  ✦",
	" ·  ✦   ·  ✧   ",
}

func renderEventSparkle(frame, width int) string {
	pattern := sparkleFrames[frame%len(sparkleFrames)]

	var b strings.Builder
	for _, ch := range pattern {
		switch ch {
		case '✦':
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#d4a21a")).Bold(true).Render("✦"))
		case '✧':
			b.WriteString(lipgloss.NewStyle().Foreground(colorAccent2).Render("✧"))
		case '·':
			b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render("·"))
		default:
			b.WriteRune(ch)
		}
	}
	_ = width
	return " " + b.String()
}

// renderEventFlowViewport bọc bảng luồng sự kiện vào viewport.
func renderEventFlowViewport(vp viewport.Model, width, height int, focused bool) string {
	// Thanh tiêu đề
	titleColor := colorDim
	if focused {
		titleColor = colorAccent
	}
	title := lipgloss.NewStyle().Foreground(titleColor).Render(":: Luồng sự kiện")
	lineW := width - lipgloss.Width(title) - 4
	if lineW < 0 {
		lineW = 0
	}
	separator := lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))
	header := " " + title + " " + separator

	vpH := height - 1
	if vpH < 1 {
		vpH = 1
	}
	style := lipgloss.NewStyle().
		Width(width).
		Height(vpH).
		Padding(0, 1)

	return header + "\n" + style.Render(vp.View())
}

// renderStreamPanel hiển thị bảng đầu ra trực tiếp (nửa dưới cột giữa).
func renderStreamPanel(vp viewport.Model, width, height int, focused, running bool, frame int) string {
	// Thanh tiêu đề phân cách (luôn nổi bật): tiền tố thanh đứng dày + luôn Bold + màu nhấn, tránh trùng màu với suy nghĩ xám nhạt nghiêng
	// Khi focused thêm gạch chân, phân biệt trạng thái focus.
	titleStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Underline(focused)
	title := titleStyle.Render("▍Đầu ra trực tiếp")
	if running {
		status := renderStreamActivity(frame)
		title += " " + status
	}
	lineW := width - lipgloss.Width(title) - 4
	if lineW < 0 {
		lineW = 0
	}
	separator := lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))
	header := " " + title + " " + separator

	// Nội dung viewport (height bao gồm dòng header, chiều cao viewport thực tế cần trừ 1).
	// vpStyle bên ngoài không đặt Foreground — màu chính văn bản chương do contentStyle bên trong
	// renderChapterBlock quản lý (nâu đậm nền sáng / mặc định terminal nền tối). Nếu thêm Foreground bên ngoài,
	// ở theme nền sáng các khối điều phối agent (✻ vàng + nhãn xanh ngọc) sẽ bị màu nâu đậm "đè" thành màu văn thường.
	vpH := height - 1
	if vpH < 1 {
		vpH = 1
	}
	vpStyle := lipgloss.NewStyle().
		Width(width).
		Height(vpH).
		Padding(0, 1)

	return header + "\n" + vpStyle.Render(vp.View())
}

var streamCursorFrames = []string{"·", "✢", "✳", "✶", "✻", "✽"}

func renderStreamCursor(frame int) string {
	f := frame % len(streamCursorFrames)
	var dots [3]string
	for i := range 3 {
		dots[i] = streamCursorFrames[(f+i)%len(streamCursorFrames)]
	}
	trail := dots[0] + " " + dots[1] + " " + dots[2]
	return "\n" + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(trail)
}

var streamActivityFrames = [][2]string{
	{"✦", "✧"},
	{"✦", "✧"},
	{"✧", "✦"},
	{"✧", "✦"},
	{"✦", "✧"},
	{"✦", "✧"},
	{"✧", "✦"},
	{"✧", "✦"},
}

func renderStreamActivity(frame int) string {
	pair := streamActivityFrames[frame%len(streamActivityFrames)]
	major := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(pair[0])
	minor := lipgloss.NewStyle().Foreground(colorAccent2).Render(pair[1])
	return major + " " + minor
}

// renderStreamContent hiển thị đầu ra trực tiếp thành các khối ngữ nghĩa theo từng lượt.
// Khối điều phối agent (bắt đầu bằng ▸ hoặc ✻) dùng tiêu đề accent + chỉ thị mờ; khối văn bản theo màu mặc định terminal.
// cursor không rỗng thì thêm vào cuối, biểu thị AI đang xuất nội dung.
func renderStreamContent(rounds []string, width int, cursor string) string {
	if width < 24 {
		width = 24
	}

	var blocks []string
	for _, round := range rounds {
		text := strings.TrimSpace(round)
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "▸") || strings.HasPrefix(text, "✻") {
			blocks = append(blocks, renderAgentBlock(text, width))
		} else {
			blocks = append(blocks, renderChapterBlock(text, width))
		}
	}
	result := strings.Join(blocks, "\n\n")
	if cursor != "" {
		result += cursor
	}
	return result
}

// renderAgentBlock hiển thị khối điều phối Agent: biểu tượng + tiêu đề + đường phân cách + chỉ thị nhiệm vụ.
//
// label dùng colorAccent2 xanh ngọc + Bold + Underline ba lớp nhấn — trước đây colorAccent
// vàng + Bold trên nền tối quá gần với dòng suy nghĩ xám colorDim, khó phân biệt chính phụ. Xanh ngọc là màu lạnh,
// tách biệt hoàn toàn về màu sắc với xám ấm của dòng suy nghĩ; Underline ổn định trên mọi terminal, là neo thị giác
// đáng tin hơn Bold. Biểu tượng ✻ ngược lại dùng vàng làm neo, tạo tương phản hai màu với label.
func renderAgentBlock(text string, width int) string {
	headerLine, body, _ := strings.Cut(text, "\n")

	iconStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Underline(true)

	// Tách biểu tượng tiền tố (✻ hoặc ▸) và label nội dung, tô màu riêng; định dạng cũ không có biểu tượng giữ một màu.
	var headerStyled string
	if first, rest, ok := strings.Cut(headerLine, " "); ok && (first == "✻" || first == "▸") {
		headerStyled = iconStyle.Render(first) + " " + labelStyle.Render(rest)
	} else {
		headerStyled = labelStyle.Render(headerLine)
	}

	// Dòng tiêu đề + đường phân cách (lineW dùng chiều rộng thị giác của headerLine, không phải chiều rộng byte sau render)
	titleW := lipgloss.Width(headerLine)
	lineW := max(0, width-titleW-1)
	header := headerStyled +
		" " + lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))

	var b strings.Builder
	b.WriteString(header)

	// Chỉ thị nhiệm vụ: màu mờ, thụt lề 2 cột; để một dòng trống giữa header và body, tránh dính vào nhau.
	body = strings.TrimSpace(body)
	if body != "" {
		taskStyle := lipgloss.NewStyle().Foreground(colorMuted)
		lines := wrapStreamText(body, max(16, width-6))
		b.WriteString("\n\n")
		for i, line := range lines {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(taskStyle.Render("  " + line))
		}
	}
	return b.String()
}

// renderChapterBlock hiển thị khối văn bản, tự động phân biệt nội dung suy nghĩ và văn bản chương.
// Nội dung suy nghĩ (đoạn được đánh dấu ThinkingSep) dùng colorDim nghiêng; văn bản chương dùng bodyTextColor:
// nền tối kế thừa màu mặc định terminal, nền sáng dùng nâu đậm giữ tông ấm.
func renderChapterBlock(text string, width int) string {
	contentStyle := lipgloss.NewStyle().Foreground(bodyTextColor)
	thinkStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)
	wrapW := max(16, width-4)

	// Tách theo ThinkingSep: đoạn lẻ là suy nghĩ, đoạn chẵn là văn bản
	// Định dạng: [văn bản] \x02 [suy nghĩ] [văn bản] \x02 [suy nghĩ] ...
	parts := strings.Split(text, utils.ThinkingSep)

	var b strings.Builder
	for i, part := range parts {
		part = strings.TrimRight(part, " \n")
		if part == "" {
			continue
		}
		isThinking := i > 0 && i%2 != 0 // Đoạn lẻ sau ThinkingSep là suy nghĩ

		style := contentStyle
		if isThinking {
			style = thinkStyle
		}

		lines := wrapStreamText(part, wrapW)
		for j, line := range lines {
			if b.Len() > 0 && j == 0 {
				b.WriteString("\n\n") // Dòng trống giữa các đoạn: tạo khoảng cách thị giác giữa suy nghĩ và văn bản
			} else if j > 0 {
				b.WriteString("\n")
			}
			b.WriteString(style.Render(line))
		}
	}
	return b.String()
}

func wrapStreamText(text string, width int) []string {
	if width < 8 {
		return []string{text}
	}

	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(raw) == "" {
			out = append(out, "")
			continue
		}
		prefix, rest, nextPrefix := parseWrapPrefix(raw)
		wrapped := wrapRunes(rest, max(4, width-lipgloss.Width(prefix)))
		for i, line := range wrapped {
			if i == 0 {
				out = append(out, prefix+line)
				continue
			}
			out = append(out, nextPrefix+line)
		}
	}
	return out
}

func parseWrapPrefix(line string) (prefix, content, nextPrefix string) {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	trimmed := strings.TrimSpace(line)

	switch {
	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "), strings.HasPrefix(trimmed, "• "):
		prefix = indent + trimmed[:2]
		content = strings.TrimSpace(trimmed[2:])
		nextPrefix = indent + "  "
		return prefix, content, nextPrefix
	case orderedListPrefix(trimmed) != "":
		marker := orderedListPrefix(trimmed)
		prefix = indent + marker
		content = strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
		nextPrefix = indent + strings.Repeat(" ", lipgloss.Width(marker))
		return prefix, content, nextPrefix
	case strings.HasPrefix(trimmed, "```"):
		return indent, trimmed, indent
	default:
		return indent, trimmed, indent
	}
}

func orderedListPrefix(line string) string {
	end := strings.Index(line, ". ")
	if end <= 0 {
		return ""
	}
	for _, r := range line[:end] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return line[:end+2]
}

func wrapRunes(text string, width int) []string {
	if text == "" {
		return []string{""}
	}
	if width < 2 {
		return []string{text}
	}

	var lines []string
	var current strings.Builder
	currentWidth := 0

	for _, r := range text {
		rw := lipgloss.Width(string(r))
		if currentWidth > 0 && currentWidth+rw > width {
			lines = append(lines, strings.TrimRight(current.String(), " "))
			current.Reset()
			currentWidth = 0
			if r == ' ' {
				continue
			}
		}
		current.WriteRune(r)
		currentWidth += rw
	}
	if current.Len() > 0 {
		lines = append(lines, strings.TrimRight(current.String(), " "))
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// outlineGridThreshold ngưỡng số chương để chuyển sang nhiều cột cho đề cương.
// Giới hạn trên tier ngắn là 25 chương, dưới 20 chương đơn cột vừa một màn hình và giữ được huy hiệu "đang tiến hành";
// Chế độ layered dài tự nhiên vượt 20 khi cuộn mở rộng, chuyển mượt sang nhiều cột.
const outlineGridThreshold = 20

// renderOutlineSection chọn bố cục theo số chương: ít thì đơn cột (có huy hiệu "đang tiến hành"), nhiều thì lưới nhiều cột.
func renderOutlineSection(snap host.UISnapshot, contentW int) string {
	if len(snap.Outline) < outlineGridThreshold {
		return renderOutlineList(snap, contentW)
	}
	return renderOutlineGrid(snap, contentW)
}

// renderOutlineList danh sách chương đơn cột (dùng cho truyện ngắn). Cuối mỗi dòng có huy hiệu "đang tiến hành", nhịp đọc dọc gần với mục lục hơn.
func renderOutlineList(snap host.UISnapshot, contentW int) string {
	var b strings.Builder
	for _, e := range snap.Outline {
		ch := fmt.Sprintf("%2d", e.Chapter)
		var marker, chStyle string
		titleStyle := cardContentStyle
		switch {
		case snap.CompletedCount >= e.Chapter:
			marker = lipgloss.NewStyle().Foreground(colorSuccess).Render("●")
			chStyle = lipgloss.NewStyle().Foreground(colorDim).Render(ch)
		case snap.InProgressChapter == e.Chapter:
			marker = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸")
			chStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(ch)
			titleStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		default:
			marker = lipgloss.NewStyle().Foreground(colorDim).Render("○")
			chStyle = lipgloss.NewStyle().Foreground(colorDim).Render(ch)
			titleStyle = lipgloss.NewStyle().Foreground(colorMuted)
		}
		title := truncate(e.Title, contentW-6)
		line := marker + chStyle + " " + titleStyle.Render(title)
		if snap.InProgressChapter == e.Chapter {
			line += lipgloss.NewStyle().Foreground(colorAccent).Italic(true).Render(" Đang tiến hành")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// renderOutlineGrid đổ các chương đề cương vào lưới nhiều cột theo "ưu tiên cột", tránh để trắng nhiều ở màn hình rộng.
// Số cột tự thích ứng theo contentW (1-4), chương trong cột tăng liên tục ("đọc hết cột này rồi sang cột kế").
// Đánh đổi với bố cục đơn cột: bỏ huy hiệu " Đang tiến hành" ở cuối dòng — huy hiệu phá vỡ căn chỉnh cột trong lưới nhiều cột,
// và ký hiệu ▸ + vàng + "Đang viết Chương N" ở thanh tổng quan bên trái đã truyền đạt đủ thông tin đang tiến hành.
func renderOutlineGrid(snap host.UISnapshot, contentW int) string {
	n := len(snap.Outline)
	if n == 0 {
		return ""
	}
	chNumW := 2
	titleW := 0
	for _, e := range snap.Outline {
		if w := len(strconv.Itoa(e.Chapter)); w > chNumW {
			chNumW = w
		}
		if w := lipgloss.Width(e.Title); w > titleW {
			titleW = w
		}
	}
	// Giới hạn chiều rộng tiêu đề tối đa 14 (khoảng 7 chữ Hán); tiêu đề dài bị cắt bớt, tránh một vài tiêu đề dài kéo rộng toàn bộ cell
	if titleW > 14 {
		titleW = 14
	} else if titleW < 4 {
		titleW = 4
	}
	cellW := 3 + chNumW + titleW // marker(1) + khoảng trắng(1) + số chương + khoảng trắng(1) + tiêu đề
	gutter := 4
	cols := (contentW + gutter) / (cellW + gutter)
	if cols < 1 {
		cols = 1
	} else if cols > 4 {
		cols = 4
	}
	rows := (n + cols - 1) / cols

	var b strings.Builder
	cellStyle := lipgloss.NewStyle().Width(cellW)
	gutterStr := strings.Repeat(" ", gutter)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			idx := c*rows + r
			if idx >= n {
				break
			}
			cell := renderOutlineCell(snap.Outline[idx], snap, chNumW, titleW)
			// Khi cột tiếp theo còn cell thì căn theo cellW + gutter; ngược lại cell hiện tại là cuối dòng, không bổ sung
			if c < cols-1 && (c+1)*rows+r < n {
				b.WriteString(cellStyle.Render(cell))
				b.WriteString(gutterStr)
			} else {
				b.WriteString(cell)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderOutlineCell hiển thị một cell chương: hoàn thành (xanh ●) / đang tiến hành (vàng ▸) / chưa bắt đầu (mờ ○).
func renderOutlineCell(e host.OutlineSnapshot, snap host.UISnapshot, chNumW, titleW int) string {
	chStr := fmt.Sprintf("%*d", chNumW, e.Chapter)
	title := truncateWidth(e.Title, titleW)
	var marker, chRendered, titleRendered string
	switch {
	case snap.CompletedCount >= e.Chapter:
		marker = lipgloss.NewStyle().Foreground(colorSuccess).Render("●")
		chRendered = lipgloss.NewStyle().Foreground(colorDim).Render(chStr)
		titleRendered = cardContentStyle.Render(title)
	case snap.InProgressChapter == e.Chapter:
		marker = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸")
		chRendered = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(chStr)
		titleRendered = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	default:
		marker = lipgloss.NewStyle().Foreground(colorDim).Render("○")
		chRendered = lipgloss.NewStyle().Foreground(colorDim).Render(chStr)
		titleRendered = lipgloss.NewStyle().Foreground(colorMuted).Render(title)
	}
	return marker + " " + chRendered + " " + titleRendered
}

// truncateWidth cắt chuỗi theo "chiều rộng thị giác" (ký tự Trung/Việt/CJK tính 2 cột), đồng nguồn với lipgloss.Width.
// truncate thông thường tính theo số rune, với tiếng Trung sẽ cắt gấp đôi chiều rộng, không dùng được khi cần căn cột.
func truncateWidth(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if cur+rw > maxW {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	return b.String()
}

// renderDetailContent xây dựng nội dung bảng chi tiết bên phải.
// Ưu tiên hiển thị thông tin cơ bản (đề cương, nhân vật), sau đó là thông tin vận hành (lưu chương, đánh giá, v.v.).
func renderDetailContent(snap host.UISnapshot, contentW int) string {
	var b strings.Builder

	// Đề cương
	if len(snap.Outline) > 0 {
		outlineHeader := ":: Đề cương"
		if snap.Layered {
			outlineHeader = fmt.Sprintf(":: Đề cương (%s · đề cương quy hoạch động)", snap.CurrentVolumeArc)
		}
		b.WriteString(panelTitleStyle.Render(outlineHeader))
		b.WriteString("\n")
		b.WriteString(renderOutlineSection(snap, contentW))
		// Gợi ý kế hoạch cuộn
		compassStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)
		if snap.Layered {
			if snap.NextVolumeTitle != "" {
				b.WriteString(compassStyle.Render("  ┄ Tập tiếp theo: " + snap.NextVolumeTitle))
				b.WriteString("\n")
			}
			b.WriteString(compassStyle.Render("  ··· Các chương tiếp theo tự động tạo khi sáng tác tiến triển"))
			b.WriteString("\n")
			if snap.CompassDirection != "" {
				direction := fmt.Sprintf("  → Kết cục: %s", snap.CompassDirection)
				if snap.CompassScale != "" {
					direction += "（" + snap.CompassScale + "）"
				}
				b.WriteString(compassStyle.Render(truncate(direction, contentW)))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	// Nhân vật
	if len(snap.Characters) > 0 {
		b.WriteString(panelTitleStyle.Render(":: Nhân vật"))
		b.WriteString("\n")
		for _, c := range snap.Characters {
			b.WriteString(cardContentStyle.Render("· " + truncate(c, contentW-2)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Hệ sinh thái nhân vật phụ: tổng số nhân vật phụ đã xuất hiện tích lũy + top 5 hoạt động gần nhất
	if snap.SupportingCount > 0 {
		b.WriteString(panelTitleStyle.Render(":: Nhân vật phụ"))
		b.WriteString("\n")
		b.WriteString(cardContentStyle.Render(truncate(fmt.Sprintf("Đã xuất hiện: %d nhân vật", snap.SupportingCount), contentW)))
		b.WriteString("\n")
		for _, name := range snap.RecentSupporting {
			b.WriteString(cardContentStyle.Render("· " + truncate(name, contentW-2)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Tiền đề
	if snap.Premise != "" {
		b.WriteString(panelTitleStyle.Render(":: Tiền đề"))
		b.WriteString("\n")
		for _, line := range wrapStreamText(snap.Premise, contentW) {
			b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n\n")
	}

	if snap.LastCommitSummary != "" {
		b.WriteString(cardTitleStyle.Render("~ Lưu chương gần nhất ~"))
		b.WriteString("\n")
		b.WriteString(cardContentStyle.Render(snap.LastCommitSummary))
		b.WriteString("\n\n")
	}

	if snap.LastReviewSummary != "" {
		b.WriteString(cardTitleStyle.Render("~ Đánh giá gần nhất ~"))
		b.WriteString("\n")
		b.WriteString(cardContentStyle.Render(snap.LastReviewSummary))
		b.WriteString("\n\n")
	}

	if len(snap.RecentSummaries) > 0 {
		b.WriteString(cardTitleStyle.Render("~ Tóm tắt ~"))
		b.WriteString("\n")
		for _, s := range snap.RecentSummaries {
			b.WriteString(cardContentStyle.Render(truncate(s, contentW)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderDetailPanel hiển thị bảng chi tiết có thể cuộn bên phải.
func renderDetailPanel(vp viewport.Model, width, height int, focused bool) string {
	borderColor := colorDim
	if focused {
		borderColor = colorAccent
	}
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		Border(baseBorder, false, false, false, true).
		BorderForeground(borderColor).
		Padding(0, 1)

	return style.Render(vp.View())
}

// renderWelcome hiển thị màn hình đầu tiên khi tạo mới.
func renderWelcome(width, height int, errMsg string, mode startupMode) string {
	// Tiêu đề gọn
	title := lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Render("A I N O V E L")

	// Phụ đề
	subtitle := lipgloss.NewStyle().
		Foreground(colorMuted).
		Italic(true).
		Render("AI-Powered Novel Creation Engine")

	// Đường phân cách
	divW := 44
	if divW > width-8 {
		divW = width - 8
	}
	divider := lipgloss.NewStyle().Foreground(colorDim).
		Render(strings.Repeat("~", divW))

	// Điểm nổi bật tính năng
	features := []struct{ icon, label, desc string }{
		{">>", "Đa model cộng tác", "Kiến trúc sư lên kế hoạch / Người viết sáng tác / Biên tập viên đánh giá"},
		{"::", "Điểm khôi phục", "Tự động tiếp tục từ tiến độ cuối sau sự cố hoặc gián đoạn"},
		{"<>", "Can thiệp thời gian thực", "Điều chỉnh hướng đi cốt truyện bất kỳ lúc nào trong quá trình sáng tác"},
		{"##", "Truyện dài phân lớp", "Hỗ trợ cấu trúc phân lớp tập-cung-chương cho truyện dài"},
	}
	iconStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	featLabelStyle := lipgloss.NewStyle().Foreground(bodyTextColor)
	descStyle := lipgloss.NewStyle().Foreground(colorDim)
	var featLines []string
	for _, f := range features {
		line := iconStyle.Render(f.icon) + " " +
			featLabelStyle.Render(f.label) + "  " +
			descStyle.Render(f.desc)
		featLines = append(featLines, line)
	}
	feats := strings.Join(featLines, "\n")

	// Gợi ý nhập liệu
	prompt := lipgloss.NewStyle().Foreground(bodyTextColor).Render("Nhập yêu cầu tiểu thuyết của bạn bên dưới để bắt đầu sáng tác")

	modeLine := lipgloss.NewStyle().
		Foreground(colorMuted).
		Render("Chế độ hiện tại: " + mode.label() + " · " + mode.subtitle())

	// Ví dụ
	examples := []string{
		"Viết một tiểu thuyết trinh thám đô thị 12 chương, nhân vật chính là nữ pháp y",
		"Sáng tác một truyện tiên hiệp dài, nhân vật chính tu luyện từ phàm nhân đến phi thăng",
		"Viết một truyện ngắn khoa học viễn tưởng về tình huống đạo đức sau khi AI thức tỉnh",
	}
	exStyle := lipgloss.NewStyle().Foreground(colorAccent)
	dotStyle := lipgloss.NewStyle().Foreground(colorDim)
	var exLines []string
	for _, ex := range examples {
		exLines = append(exLines, dotStyle.Render("  . ")+exStyle.Render(ex))
	}
	exBlock := strings.Join(exLines, "\n")

	// Lắp ráp
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(subtitle)
	b.WriteString("\n\n")
	b.WriteString(divider)
	b.WriteString("\n\n")
	b.WriteString(feats)
	b.WriteString("\n\n")
	b.WriteString(divider)
	b.WriteString("\n\n")
	b.WriteString(modeLine)
	b.WriteString("\n\n")
	b.WriteString(prompt)
	b.WriteString("\n\n")
	b.WriteString(exBlock)
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Italic(true).
		Render("Tab chuyển chế độ · Chế độ bắt đầu nhanh nhấn Enter để sáng tác · Chế độ đồng sáng tác nhấn Enter để vào hội thoại"))

	if errMsg != "" {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("! " + errMsg))
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		AlignHorizontal(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Render(b.String())
}
