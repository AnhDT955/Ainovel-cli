package store

import (
	"os"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// SignalStore quản lý các file tín hiệu một lần (kết quả lưu chương/review, trạng thái chờ khôi phục).
type SignalStore struct{ io *IO }

func NewSignalStore(io *IO) *SignalStore { return &SignalStore{io: io} }

// SaveLastCommit lưu kết quả lưu chương gần nhất vào meta/last_commit.json.
func (s *SignalStore) SaveLastCommit(result domain.CommitResult) error {
	return s.io.WriteJSON("meta/last_commit.json", result)
}

// LoadLastCommit đọc kết quả lưu chương gần nhất.
func (s *SignalStore) LoadLastCommit() (*domain.CommitResult, error) {
	var r domain.CommitResult
	if err := s.io.ReadJSON("meta/last_commit.json", &r); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// LoadAndClearLastCommit đọc và xóa tín hiệu lưu chương theo cách nguyên tử, tránh race condition TOCTOU.
func (s *SignalStore) LoadAndClearLastCommit() (*domain.CommitResult, error) {
	s.io.mu.Lock()
	defer s.io.mu.Unlock()
	var r domain.CommitResult
	if err := s.io.ReadJSONUnlocked("meta/last_commit.json", &r); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	_ = s.io.RemoveFileUnlocked("meta/last_commit.json")
	return &r, nil
}

// ClearLastCommit xóa file tín hiệu lưu chương.
func (s *SignalStore) ClearLastCommit() error {
	return s.io.RemoveFile("meta/last_commit.json")
}

// SavePendingCommit lưu trạng thái lưu chương đang chờ khôi phục.
func (s *SignalStore) SavePendingCommit(pending domain.PendingCommit) error {
	return s.io.WriteJSON("meta/pending_commit.json", pending)
}

// LoadPendingCommit đọc trạng thái lưu chương đang chờ khôi phục.
func (s *SignalStore) LoadPendingCommit() (*domain.PendingCommit, error) {
	var pending domain.PendingCommit
	if err := s.io.ReadJSON("meta/pending_commit.json", &pending); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &pending, nil
}

// ClearPendingCommit xóa trạng thái lưu chương đang chờ khôi phục.
func (s *SignalStore) ClearPendingCommit() error {
	return s.io.RemoveFile("meta/pending_commit.json")
}

// SaveLastReview lưu kết quả review gần nhất vào meta/last_review.json.
func (s *SignalStore) SaveLastReview(r domain.ReviewEntry) error {
	return s.io.WriteJSON("meta/last_review.json", r)
}

// LoadLastReviewSignal đọc file tín hiệu review.
func (s *SignalStore) LoadLastReviewSignal() (*domain.ReviewEntry, error) {
	var r domain.ReviewEntry
	if err := s.io.ReadJSON("meta/last_review.json", &r); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ClearLastReview xóa file tín hiệu review.
func (s *SignalStore) ClearLastReview() error {
	return s.io.RemoveFile("meta/last_review.json")
}

// LoadAndClearLastReview đọc và xóa tín hiệu review theo cách nguyên tử.
func (s *SignalStore) LoadAndClearLastReview() (*domain.ReviewEntry, error) {
	s.io.mu.Lock()
	defer s.io.mu.Unlock()
	var r domain.ReviewEntry
	if err := s.io.ReadJSONUnlocked("meta/last_review.json", &r); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	_ = s.io.RemoveFileUnlocked("meta/last_review.json")
	return &r, nil
}

// ClearStaleSignals dọn dẹp các file tín hiệu còn sót lại (gọi khi khởi động lại tiến trình).
func (s *SignalStore) ClearStaleSignals() {
	_ = s.io.RemoveFile("meta/last_commit.json")
	_ = s.io.RemoveFile("meta/last_review.json")
}
