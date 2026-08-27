package diag

import "testing"

// TestRuntimeFindings_Classify chứng minh rằng các chữ ký lặp được phân loại đúng theo hình thái,
// ngưỡng nâng/hạ cấp hoạt động chính xác,
// và tất cả Finding thời gian chạy đều có AutoNone (kỷ luật quan sát viên: chỉ chẩn đoán, không tạo Action).
func TestRuntimeFindings_Classify(t *testing.T) {
	rc := RuntimeCapture{
		Repeats: []RepeatStat{
			{Sig: "coordinator · err: InputValidationError", Count: 14}, // vòng lặp lỗi → critical
			{Sig: "coordinator · subagent", Count: 45},                  // công cụ tần suất cao bình thường → không tạo Finding
			{Sig: "writer · save_plan (args invalid)", Count: 4},        // tham số không hợp lệ → warning
		},
		StuckStep:  "writing.commit_ch07",
		StuckCount: 9, // bị kẹt → critical
		LogKinds:   map[string]int{"stream_idle": 4},
		LogErrors:  270, // tích lũy trong run dài, không nên tạo Finding riêng
	}

	fs := runtimeFindings(&rc)
	sev := map[string]Severity{}
	for _, f := range fs {
		sev[f.Rule] = f.Severity
		if f.AutoLevel != AutoNone {
			t.Errorf("%s phải là AutoNone (kỷ luật quan sát viên), got %s", f.Rule, f.AutoLevel)
		}
	}

	want := map[string]Severity{
		"RepeatedToolError": SevCritical,
		"ArgsInvalidLoop":   SevWarning,
		"StuckStep":         SevCritical,
		"StreamIdleStorm":   SevWarning,
	}
	for rule, w := range want {
		if sev[rule] != w {
			t.Errorf("%s: got %q want %q", rule, sev[rule], w)
		}
	}
	// Công cụ tần suất cao bình thường / lỗi log tích lũy không nên tạo Finding (tránh báo nhầm trong run dài).
	if _, ok := sev["RepeatedToolCall"]; ok {
		t.Error("Công cụ thông thường lặp lại không nên tạo Finding")
	}
	if _, ok := sev["LogErrorBurst"]; ok {
		t.Error("Lỗi log tích lũy không nên tạo Finding riêng")
	}
}

// TestRuntimeFindings_Quiet chứng minh rằng khi không có tín hiệu bất thường thì không tạo Finding thời gian chạy nào (zero false positive).
func TestRuntimeFindings_Quiet(t *testing.T) {
	rc := RuntimeCapture{
		LogKinds:  map[string]int{"stream_idle": 1}, // dưới ngưỡng
		LogErrors: 2,
	}
	if fs := runtimeFindings(&rc); len(fs) != 0 {
		t.Errorf("Trạng thái yên tĩnh không nên tạo Finding, got %d: %+v", len(fs), fs)
	}
}
