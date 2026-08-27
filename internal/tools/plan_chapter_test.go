package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeChapterPlanPreservesDramaArchitecture(t *testing.T) {
	args := json.RawMessage(`{
		"chapter": 7,
		"title": "Dấu Máu Trên Khế",
		"goal": "Rời võ quán mà không để lộ ký ức đã thay đổi",
		"conflict": "Quản sự khóa cửa và thử hắn bằng món nợ của nguyên chủ",
		"opening_pressure": "Khế nợ được đặt trước mặt khi người làm chứng đã chờ sẵn",
		"stakes": "Điểm chỉ thì mất tự do; từ chối vụng về sẽ lộ hắn không còn là nguyên chủ",
		"pressure_chain": [
			"Hắn kéo dài thời gian đọc khế → quản sự gọi người giữ tay → hắn giả đau vết thương → thoát một nhịp nhưng bị khóa cửa",
			"Hắn tìm sai dấu → đối phương nhắc bí mật chỉ nguyên chủ biết → hắn dùng mảnh ký ức trả lời nửa đúng → phá được khế nhưng gieo nghi ngờ"
		],
		"turning_point": "Mùi mực kích hoạt ký ức cho thấy con dấu là giả",
		"character_choice": "Chấp nhận lộ khả năng biết chữ để bẻ khế thay vì tiếp tục giả ngu",
		"consequence": "Món nợ bị hủy nhưng quản sự bắt đầu cho người theo dõi",
		"hook": "Người theo dõi mang tín vật của kẻ đã giết nguyên chủ"
	}`)

	plan, err := decodeChapterPlanArgs(args)
	if err != nil {
		t.Fatalf("decodeChapterPlanArgs: %v", err)
	}
	if err := validateChapterPlanDrama(plan); err != nil {
		t.Fatalf("validateChapterPlanDrama: %v", err)
	}
	if plan.OpeningPressure == "" || plan.Stakes == "" || len(plan.PressureChain) != 2 {
		t.Fatalf("drama architecture was not preserved: %+v", plan)
	}
	if !strings.Contains(plan.Consequence, "theo dõi") {
		t.Fatalf("unexpected consequence: %q", plan.Consequence)
	}
}

func TestValidateChapterPlanDramaRejectsActivityList(t *testing.T) {
	plan, err := decodeChapterPlanArgs(json.RawMessage(`{
		"chapter": 8,
		"title": "Vào Chợ",
		"goal": "Mua bí kíp",
		"conflict": "Phải giữ kín thân phận",
		"hook": "Thấy một quầy hàng lạ"
	}`))
	if err != nil {
		t.Fatalf("decodeChapterPlanArgs: %v", err)
	}
	err = validateChapterPlanDrama(plan)
	if err == nil || !strings.Contains(err.Error(), "opening_pressure") {
		t.Fatalf("expected shallow plan rejection, got %v", err)
	}
}

func TestValidateChapterPlanDramaRequiresEscalation(t *testing.T) {
	plan, err := decodeChapterPlanArgs(json.RawMessage(`{
		"chapter": 9,
		"title": "Một Giá Hai Mạng",
		"goal": "Lấy bí kíp",
		"conflict": "Chủ quầy muốn dò thân phận",
		"opening_pressure": "Chủ quầy giữ lại tín vật",
		"stakes": "Mất tín vật sẽ mất đường lui",
		"pressure_chain": ["Hai bên mặc cả"],
		"turning_point": "Chủ quầy gọi đúng tên nguyên chủ",
		"character_choice": "Bỏ bí kíp để giữ vỏ bọc",
		"consequence": "Đối phương xác nhận hắn biết tín vật có vấn đề",
		"hook": "Kẻ theo dõi xuất hiện ở cửa sau"
	}`))
	if err != nil {
		t.Fatalf("decodeChapterPlanArgs: %v", err)
	}
	err = validateChapterPlanDrama(plan)
	if err == nil || !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("expected pressure chain rejection, got %v", err)
	}
}
