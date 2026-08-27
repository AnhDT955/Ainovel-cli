package startup

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/host"
)

// PrepareQuick chuyển đổi đầu vào trực tiếp thành kế hoạch khởi động nhanh có thể đưa vào Engine.
func PrepareQuick(req Request) (Plan, error) {
	prompt := strings.TrimSpace(req.UserPrompt)
	if prompt == "" {
		return Plan{}, fmt.Errorf("prompt is required")
	}
	return Plan{
		Mode:        ModeQuick,
		DisplayName: "Bắt đầu nhanh",
		StartPrompt: host.BuildStartPrompt(prompt),
	}, nil
}
