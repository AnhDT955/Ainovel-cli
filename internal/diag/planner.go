package diag

import "fmt"

// PlanActions tạo các hành động có thể thực thi dựa trên Finding có độ tin cậy cao.
// Chỉ Finding có Confidence==high && AutoLevel==safe mới tạo ra Action.
func PlanActions(findings []Finding) []Action {
	var actions []Action
	seen := make(map[string]struct{})

	for _, f := range findings {
		if f.Confidence != ConfHigh || f.AutoLevel != AutoSafe {
			continue
		}
		if _, ok := seen[f.Rule]; ok {
			continue
		}
		seen[f.Rule] = struct{}{}

		actions = append(actions, planRule(f)...)
	}
	return actions
}

func planRule(f Finding) []Action {
	key := findingFingerprint(f)

	switch f.Rule {
	case "PhaseFlowMismatch":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEmitNotice, Severity: f.Severity, Summary: f.Title, Message: f.Title, Fingerprint: key},
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: "Sửa lỗi trạng thái máy", Message: "Máy trạng thái bất thường: " + f.Evidence + ". Hãy kiểm tra và sửa trạng thái phase/flow của progress trước, rồi tiếp tục chạy.", Fingerprint: key},
		}
	case "OutlineExhausted":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: "Xử lý đề cương đã cạn kiệt", Message: "Số chương đã hoàn thành đạt giới hạn đã lên kế hoạch. Hãy ưu tiên gọi Kiến trúc sư để mở rộng cung truyện tiếp theo hoặc thêm tập mới, rồi mới tiếp tục viết.", Fingerprint: key},
		}
	case "OrphanedSteer":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: "Tiêu thụ can thiệp người dùng chưa xử lý", Message: "Có lệnh can thiệp người dùng chưa được tiêu thụ, hãy ưu tiên xử lý pending steer trước rồi mới tiếp tục tác vụ hiện tại.", Fingerprint: key},
		}
	default:
		return nil
	}
}

func findingFingerprint(f Finding) string {
	return fmt.Sprintf("%s|%s|%s|%s", f.Rule, f.Target, f.Title, f.Evidence)
}
