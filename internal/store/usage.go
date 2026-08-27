package store

import (
	"os"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// UsageStore lưu trữ lâu dài lượng dùng token / chi phí tích lũy vào meta/usage.json.
// Ghi dữ liệu qua atomic write của IO (tmp + rename), Save sẽ ghi đè toàn bộ state mỗi lần.
type UsageStore struct{ io *IO }

func NewUsageStore(io *IO) *UsageStore { return &UsageStore{io: io} }

// Load đọc usage.json. Trả về (nil, nil) khi file không tồn tại hoặc phiên bản schema không khớp,
// để bên gọi tự quyết định có thực hiện session replay để bổ sung lại hay không.
func (s *UsageStore) Load() (*domain.UsageState, error) {
	var state domain.UsageState
	if err := s.io.ReadJSON("meta/usage.json", &state); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if state.Schema != domain.UsageSchemaVersion {
		return nil, nil
	}
	return &state, nil
}

// Save ghi đè toàn bộ state xuống đĩa. Bên gọi chịu trách nhiệm debounce / throttle.
func (s *UsageStore) Save(state domain.UsageState) error {
	state.Schema = domain.UsageSchemaVersion
	return s.io.WriteJSON("meta/usage.json", state)
}
