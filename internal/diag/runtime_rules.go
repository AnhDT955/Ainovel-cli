package diag

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/store"
)

// Ngưỡng phát hiện trong thời gian chạy.
const (
	repeatCritical = 8 // số lần lặp gần cuối đạt ngưỡng này sẽ nâng lên critical
	streamIdleWarn = 3 // ngưỡng cảnh báo tích lũy của stream_idle
)

// RuntimeRuleFunc là chữ ký thống nhất cho các quy tắc chẩn đoán thời gian chạy
// (tương ứng với RuleFunc phía sáng tác).
// Tham số đầu vào là RuntimeCapture đã được ẩn danh và tổng hợp, đầu ra là Finding dạng báo cáo —
// tất cả đều là AutoNone, chỉ chẩn đoán, không tạo Action
// (nguyên tắc quan sát thuần túy, xem architecture.md §2.3).
type RuntimeRuleFunc func(rc *RuntimeCapture) []Finding

var runtimeRules = []RuntimeRuleFunc{
	repeatedErrors,
	stuckStep,
	streamIdleStorm,
}

// runtimeFindings chạy toàn bộ các quy tắc thời gian chạy.
func runtimeFindings(rc *RuntimeCapture) []Finding {
	var out []Finding
	for _, rule := range runtimeRules {
		out = append(out, rule(rc)...)
	}
	return out
}

// Diagnose là điểm vào chẩn đoán đầy đủ của /diag: chẩn đoán sáng tác + tín hiệu thời gian chạy
// + phát hiện thời gian chạy. Trả về Report đã hợp nhất và RuntimeCapture gốc
// (để tái sử dụng khi xuất, tránh thu thập lại).
// Finding thời gian chạy chỉ được gộp vào Findings để hiển thị, không thay đổi Actions —
// giữ nguyên tính quan sát thuần túy.
func Diagnose(s *store.Store) (Report, RuntimeCapture) {
	rep := Analyze(s)
	rc := CaptureRuntime(s)
	rep.Findings = append(rep.Findings, runtimeFindings(&rc)...)
	sortFindings(rep.Findings)
	return rep, rc
}

// repeatedErrors chỉ đánh dấu "lỗi / tham số không hợp lệ xuất hiện lặp lại gần cuối" thành Finding.
// Không xử lý các lần lặp công cụ thông thường — subagent/novel_context/read_chapter, v.v.
// vốn có tần suất cao trong các chạy dài, số lần tích lũy không phải tín hiệu vòng lặp;
// trường hợp "lặp mà không tiến triển" thực sự sẽ được stuckStep bắt lại.
func repeatedErrors(rc *RuntimeCapture) []Finding {
	var out []Finding
	for _, r := range rc.Repeats {
		var rule, title, sugg string
		switch {
		case strings.Contains(r.Sig, " · err: "):
			rule = "RepeatedToolError"
			title = "Công cụ lặp lại cùng một lỗi"
			sugg = "Cùng một công cụ liên tục trả về cùng một lỗi gần đây, thường do tham số của model không hợp lệ hoặc không khớp contract của công cụ; kiểm tra xác thực công cụ trong agentcore / quy ước tham số trong prompt (xem #34)."
		case strings.Contains(r.Sig, "(args invalid)"):
			rule = "ArgsInvalidLoop"
			title = "Tham số liên tục không thể phân tích"
			sugg = "Tham số từ model không thể phân tích nhưng vẫn tiếp tục thử lại; kiểm tra xem agentcore có thực hiện ép kiểu nới lỏng cho loại này không (xem #34)."
		default:
			continue // lặp công cụ thông thường không tạo Finding
		}
		sev := SevWarning
		if r.Count >= repeatCritical {
			sev = SevCritical
		}
		out = append(out, Finding{
			Rule:       rule,
			Category:   CatFlow,
			Severity:   sev,
			Confidence: ConfHigh,
			AutoLevel:  AutoNone,
			Target:     "runtime.flow",
			Title:      title,
			Evidence:   fmt.Sprintf("`%s` ×%d", r.Sig, r.Count),
			Suggestion: sugg,
		})
	}
	return out
}

// stuckStep phát hiện điểm khôi phục liên tục dừng tại cùng một step.
func stuckStep(rc *RuntimeCapture) []Finding {
	if rc.StuckStep == "" {
		return nil
	}
	sev := SevWarning
	if rc.StuckCount >= repeatCritical {
		sev = SevCritical
	}
	return []Finding{{
		Rule:       "StuckStep",
		Category:   CatFlow,
		Severity:   sev,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      "Điểm khôi phục bị kẹt tại cùng một step",
		Evidence:   fmt.Sprintf("Liên tục dừng tại `%s` ×%d", rc.StuckStep, rc.StuckCount),
		Suggestion: "Cùng một step được ghi liên tục mà không tiến triển; kết hợp với các chữ ký lặp ở trên để xác định agent phụ nào đang bị kẹt.",
	}}
}

// streamIdleStorm phát hiện gián đoạn stream xảy ra thường xuyên (#32).
func streamIdleStorm(rc *RuntimeCapture) []Finding {
	n := rc.LogKinds["stream_idle"]
	if n < streamIdleWarn {
		return nil
	}
	return []Finding{{
		Rule:       "StreamIdleStorm",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.provider",
		Title:      "Gián đoạn stream xảy ra thường xuyên (stream_idle)",
		Evidence:   fmt.Sprintf("stream_idle ×%d", n),
		Suggestion: "Upstream không phát token trong thời gian dài bị watchdog ngắt nhầm; tăng streamIdleTimeout cho model suy nghĩ chậm, hoặc kiểm tra tính ổn định kết nối của nhà cung cấp (xem #32).",
	}}
}
