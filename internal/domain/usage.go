package domain

import "time"

// UsageSchemaVersion là số phiên bản tương thích của meta/usage.json.
// Nếu ngữ nghĩa các trường AgentUsageTotals thay đổi trong tương lai, tăng giá trị này lên;
// UsageStore.Load khi gặp phiên bản khác sẽ bỏ qua và kích hoạt replay để tái tạo lại.
const UsageSchemaVersion = 2

// UsageState là snapshot có thể lưu trữ của tổng token / cost tích lũy.
// Được duy trì trong bộ nhớ bởi UsageTracker, định kỳ debounce ghi xuống meta/usage.json.
//
// Lưu ý: các samples cửa sổ trượt bên trong UsageTracker ("tỷ lệ cache hit N lần gần nhất")
// **không được lưu trữ** — chúng chỉ phục vụ chẩn đoán ngắn hạn trên UI,
// khởi động lại tiến trình sẽ bắt đầu từ đầu và tích lũy lại sau vài vòng.
// MissingAssistantUsage vẫn được lưu trữ vì giá trị chẩn đoán tích lũy qua các lần khởi động.
type UsageState struct {
	Schema       int                         `json:"schema"`
	UpdatedAt    time.Time                   `json:"updated_at"`
	Overall      AgentUsageTotals            `json:"overall"`
	PerAgent     map[string]AgentUsageTotals `json:"per_agent"`
	PerModel     map[string]AgentUsageTotals `json:"per_model,omitempty"`
	MissingUsage int                         `json:"missing_assistant_usage"`
}

// AgentUsageTotals là dạng có thể lưu trữ của tổng đếm tích lũy cho một agent đơn lẻ (hoặc overall).
type AgentUsageTotals struct {
	Input        int     `json:"input"`
	Output       int     `json:"output"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	Cost         float64 `json:"cost_usd"`
	Saved        float64 `json:"saved_usd"`
	CacheCapable bool    `json:"cache_capable"`
}
