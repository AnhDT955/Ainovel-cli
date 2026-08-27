package domain

// StateChange ghi lại sự thay đổi trạng thái của nhân vật/thực thể.
type StateChange struct {
	Chapter  int    `json:"chapter"`
	Entity   string `json:"entity"`              // tên nhân vật hoặc thực thể
	Field    string `json:"field"`               // thuộc tính thay đổi: realm/location/status/power/relation, v.v.
	OldValue string `json:"old_value,omitempty"` // trước khi thay đổi (có thể để trống nếu xuất hiện lần đầu)
	NewValue string `json:"new_value"`           // sau khi thay đổi
	Reason   string `json:"reason,omitempty"`    // nguyên nhân thay đổi
}
