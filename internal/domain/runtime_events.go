package domain

import "time"

// RuntimeQueuePriority biểu thị mức độ ưu tiên của hàng đợi runtime.
type RuntimeQueuePriority string

const (
	RuntimePriorityControl    RuntimeQueuePriority = "control"
	RuntimePriorityBackground RuntimeQueuePriority = "background"
)

// RuntimeQueueKind biểu thị loại mục trong hàng đợi runtime.
type RuntimeQueueKind string

const (
	RuntimeQueueUIEvent     RuntimeQueueKind = "ui_event"
	RuntimeQueueStreamDelta RuntimeQueueKind = "stream_delta"
	RuntimeQueueStreamClear RuntimeQueueKind = "stream_clear"
	RuntimeQueueControl     RuntimeQueueKind = "control"
)

// RuntimeQueueItem là bản ghi lưu trữ của hàng đợi runtime thống nhất.
type RuntimeQueueItem struct {
	Seq      int64                `json:"seq"`
	Time     time.Time            `json:"time"`
	Kind     RuntimeQueueKind     `json:"kind"`
	Priority RuntimeQueuePriority `json:"priority"`
	TaskID   string               `json:"task_id,omitempty"`
	Agent    string               `json:"agent,omitempty"`
	Category string               `json:"category,omitempty"`
	Summary  string               `json:"summary,omitempty"`
	Payload  any                  `json:"payload,omitempty"`
}

// RuntimeTaskLogEntry là bản ghi lưu trữ nhật ký chạy của một task đơn lẻ.
type RuntimeTaskLogEntry struct {
	Time    time.Time `json:"time"`
	TaskID  string    `json:"task_id,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	Event   string    `json:"event"`
	Tool    string    `json:"tool,omitempty"`
	Summary string    `json:"summary,omitempty"`
	Payload any       `json:"payload,omitempty"`
}
