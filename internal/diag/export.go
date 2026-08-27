package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/store"
)

// ExportRelPath là vị trí cố định của file chẩn đoán đã ẩn danh, tương đối với thư mục output (ghi đè, một bản duy nhất).
const ExportRelPath = "meta/diag-export.md"

// Export thực hiện chẩn đoán đầy đủ + render + ghi file, trả về đường dẫn tuyệt đối đã ghi. Dùng cho headless / gọi ngoài.
func Export(s *store.Store) (string, error) {
	rep, rc := Diagnose(s)
	return WriteExport(s, rep, rc)
}

// WriteExport render và ghi Report + RuntimeCapture đã tính sẵn xuống đĩa, không thu thập lại.
// Dùng cho lệnh /diag tái sử dụng kết quả từ Diagnose.
func WriteExport(s *store.Store, rep Report, rc RuntimeCapture) (string, error) {
	data := RenderExport(rep, rc)
	abs := filepath.Join(s.Dir(), filepath.FromSlash(ExportRelPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

// RenderExport kết hợp Report sáng tác + dữ liệu thu thập runtime thành Markdown đã ẩn danh.
func RenderExport(rep Report, rc RuntimeCapture) []byte {
	var b strings.Builder
	st := rep.Stats

	b.WriteString("# diag-export\n\n")
	fmt.Fprintf(&b, "> Thời gian tạo %s · %s/%s\n", time.Now().Format("2006-01-02 15:04:05"), rc.GoOS, rc.GoArch)
	b.WriteString("> ⚠️ Đã ẩn danh: nội dung tiểu thuyết / prompt / suy nghĩ đã được xóa, chỉ giữ lại khung hành vi. Có thể dán thẳng vào issue.\n\n")

	// 1. Môi trường
	b.WriteString("## 1. Môi trường\n\n")
	fmt.Fprintf(&b, "- Giai đoạn `%s`", orDash(st.Phase))
	if st.Flow != "" {
		fmt.Fprintf(&b, " / flow `%s`", st.Flow)
	}
	fmt.Fprintf(&b, " · Chương %d/%d · Số từ %d\n", st.CompletedChapters, st.TotalChapters, st.TotalWords)
	if st.PlanningTier != "" {
		fmt.Fprintf(&b, "- Lập kế hoạch `%s`\n", st.PlanningTier)
	}
	for _, m := range rc.Models {
		fmt.Fprintf(&b, "- %s → `%s` / `%s`\n", m.Agent, orDash(m.Provider), orDash(m.Model))
	}

	// 2. Phát hiện chẩn đoán (chỉ runtime; chẩn đoán sáng tác có chứa cốt truyện/phục bút được giữ trên màn hình /diag, không đưa vào bản export có thể chia sẻ)
	b.WriteString("\n## 2. Phát hiện chẩn đoán (runtime)\n\n")
	rf := runtimeFindings(&rc)
	sortFindings(rf)
	if len(rf) == 0 {
		b.WriteString("Không phát hiện bất thường runtime.\n")
	} else {
		for _, f := range rf {
			fmt.Fprintf(&b, "- [%s] %s\n", f.Severity, f.Title)
			if f.Evidence != "" {
				fmt.Fprintf(&b, "  - Bằng chứng: %s\n", f.Evidence)
			}
			if f.Suggestion != "" {
				fmt.Fprintf(&b, "  - → %s\n", f.Suggestion)
			}
		}
	}

	// 3. Tín hiệu runtime (tổng hợp thô)
	b.WriteString("\n## 3. Tín hiệu runtime\n\n")
	wrote := false
	if rc.CurrentStep != "" {
		fmt.Fprintf(&b, "- Step hiện tại `%s`\n", rc.CurrentStep)
		wrote = true
	}
	if rc.StuckStep != "" {
		fmt.Fprintf(&b, "- ⚠️ Bị kẹt: liên tục dừng ở `%s` ×%d\n", rc.StuckStep, rc.StuckCount)
		wrote = true
	}
	if len(rc.Repeats) > 0 {
		b.WriteString("- Chữ ký tần suất cao (cửa sổ gần ≥3 lần, bao gồm tool lặp bình thường, chỉ mang tính tham khảo):\n")
		for _, r := range rc.Repeats {
			fmt.Fprintf(&b, "  - `%s` ×%d\n", r.Sig, r.Count)
		}
		wrote = true
	}
	if len(rc.DupContent) > 0 {
		b.WriteString("- Sinh lặp cùng đoạn văn bản (cùng sha):\n")
		for _, d := range rc.DupContent {
			fmt.Fprintf(&b, "  - sha=%s ×%d\n", d.Sha, d.Count)
		}
		wrote = true
	}
	if len(rc.LogKinds) > 0 {
		b.WriteString("- Phân loại lỗi log: ")
		b.WriteString(joinKinds(rc.LogKinds))
		b.WriteString("\n")
		wrote = true
	}
	if rc.LogErrors > 0 || rc.LogWarns > 0 {
		fmt.Fprintf(&b, "- Log error ×%d · warn ×%d\n", rc.LogErrors, rc.LogWarns)
		wrote = true
	}
	if rc.StopGuard > 0 {
		fmt.Fprintf(&b, "- StopGuard chặn ×%d\n", rc.StopGuard)
		wrote = true
	}
	if !wrote {
		b.WriteString("- Không có tín hiệu bất thường runtime rõ ràng.\n")
	}

	// 4. Đuôi khung hành vi
	fmt.Fprintf(&b, "\n## 4. Đuôi khung hành vi (%d mục cuối)\n\n", len(rc.Tail))
	if len(rc.Tail) == 0 {
		b.WriteString("(Không có bản ghi phiên)\n")
	} else {
		b.WriteString("```\n")
		for _, ev := range rc.Tail {
			b.WriteString(formatSkel(ev))
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	}

	// 5. Kiểm tra ẩn danh
	b.WriteString("\n## 5. Kiểm tra ẩn danh\n\n")
	fmt.Fprintf(&b, "- Khối văn bản đã che %d chỗ · Nội dung lọt ra ngoài 0 chỗ\n", rc.RedactedTexts)
	if len(rc.Sources) > 0 {
		fmt.Fprintf(&b, "- Nguồn dữ liệu: %s\n", strings.Join(rc.Sources, " · "))
	}

	return []byte(b.String())
}

// formatSkel render một sự kiện khung thành một dòng duy nhất, thể hiện thứ tự dispatch.
func formatSkel(ev SkelEvent) string {
	var parts []string
	parts = append(parts, "["+ev.Agent+"/"+ev.Role+"]")
	for _, t := range ev.Tools {
		parts = append(parts, t.Name+formatArgs(t.Args)+invalidTag(t))
	}
	if ev.ErrClass != "" {
		parts = append(parts, "err: "+ev.ErrClass)
	}
	if len(ev.Tools) == 0 && ev.ErrClass == "" && ev.TextSha != "" {
		parts = append(parts, "text<sha="+ev.TextSha+">")
	}
	return strings.Join(parts, " ")
}

func formatArgs(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+": "+args[k])
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

func invalidTag(t SkelTool) string {
	if !t.Invalid {
		return ""
	}
	if t.ParseErr != "" {
		return " ⚠️args-invalid(" + firstLine(t.ParseErr, 80) + ")"
	}
	return " ⚠️args-invalid"
}

func joinKinds(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, m[k]))
	}
	return strings.Join(parts, " · ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
