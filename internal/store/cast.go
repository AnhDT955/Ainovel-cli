package store

import (
	"os"
	"slices"
	"sort"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// CastStore quản lý danh sách nhân vật phụ (meta/cast_ledger.json).
//
// Danh sách nhân vật phụ ghi lại "các nhân vật phụ có tên đã xuất hiện", độc lập với characters.json (hồ sơ nhân vật cốt lõi):
//   - characters.json: nhân vật chính + nhân vật phụ quan trọng do Kiến trúc sư thiết kế tường minh, không chỉnh sửa trong quá trình viết
//   - cast_ledger.json: công cụ commit_chapter tự động tích lũy, tất cả nhân vật phụ có tên không thuộc cốt lõi
//
// MergeAppearances là idempotent: lưu chương cùng một chương nhiều lần sẽ không cộng dồn AppearanceCount.
type CastStore struct{ io *IO }

func NewCastStore(io *IO) *CastStore { return &CastStore{io: io} }

const castLedgerPath = "meta/cast_ledger.json"

// Load đọc danh sách nhân vật phụ. Trả về slice rỗng nếu file không tồn tại.
func (s *CastStore) Load() ([]domain.CastEntry, error) {
	var entries []domain.CastEntry
	if err := s.io.ReadJSON(castLedgerPath, &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// Save lưu toàn bộ danh sách nhân vật phụ (ghi nguyên tử).
func (s *CastStore) Save(entries []domain.CastEntry) error {
	return s.io.WriteJSON(castLedgerPath, entries)
}

// MergeAppearances hợp nhất bản ghi xuất hiện trong chương này vào danh sách.
//
// Tham số:
//   - chapter: số chương hiện tại
//   - characters: mảng tên nhân vật xuất hiện trong chương (lấy từ commit_chapter.Characters)
//   - intros: giới thiệu nhân vật mới do Người viết khai báo tường minh (xuất hiện lần đầu hoặc bổ sung BriefRole)
//   - knownCore: tập hợp tên nhân vật cốt lõi đã có trong characters.json (bỏ qua khi ghi vào ledger)
//
// Hành vi:
//   - Tên có trong knownCore: bỏ qua (hồ sơ nhân vật cốt lõi là đầu mối ghi duy nhất)
//   - Tên đã có trong ledger và chapter đã có trong AppearanceChapters: bỏ qua hoàn toàn (idempotent)
//   - Tên đã có trong ledger nhưng chapter là mới: cập nhật LastSeenChapter + nối thêm chapter + count++
//   - Tên chưa có trong ledger: thêm mục mới
//   - BriefRole trong intros chỉ được áp dụng khi BriefRole của mục ledger vẫn còn trống, tránh ghi đè giới thiệu cũ hơn
func (s *CastStore) MergeAppearances(
	chapter int,
	characters []string,
	intros []domain.CastIntro,
	knownCore map[string]bool,
) error {
	if chapter <= 0 || len(characters) == 0 {
		return nil
	}
	return s.io.WithWriteLock(func() error {
		var entries []domain.CastEntry
		if err := s.io.ReadJSONUnlocked(castLedgerPath, &entries); err != nil && !os.IsNotExist(err) {
			return err
		}

		introMap := make(map[string]string, len(intros))
		for _, in := range intros {
			if in.Name != "" {
				introMap[in.Name] = in.BriefRole
			}
		}

		index := make(map[string]int, len(entries))
		for i, e := range entries {
			index[e.Name] = i
			for _, alias := range e.Aliases {
				index[alias] = i
			}
		}

		seen := make(map[string]bool, len(characters))
		for _, name := range characters {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if knownCore[name] {
				continue
			}
			if i, ok := index[name]; ok {
				entry := &entries[i]
				if !slices.Contains(entry.AppearanceChapters, chapter) {
					entry.AppearanceChapters = append(entry.AppearanceChapters, chapter)
					entry.AppearanceCount = len(entry.AppearanceChapters)
					if chapter > entry.LastSeenChapter {
						entry.LastSeenChapter = chapter
					}
					if chapter < entry.FirstSeenChapter || entry.FirstSeenChapter == 0 {
						entry.FirstSeenChapter = chapter
					}
				}
				if entry.BriefRole == "" {
					if br, ok := introMap[name]; ok && br != "" {
						entry.BriefRole = br
					}
				}
				continue
			}
			entries = append(entries, domain.CastEntry{
				Name:               name,
				BriefRole:          introMap[name],
				FirstSeenChapter:   chapter,
				LastSeenChapter:    chapter,
				AppearanceCount:    1,
				AppearanceChapters: []int{chapter},
			})
		}
		return s.io.WriteJSONUnlocked(castLedgerPath, entries)
	})
}

// RecentActive trả về N mục nhân vật phụ hoạt động gần đây nhất (sắp xếp giảm dần theo LastSeenChapter).
// Dùng cho novel_context để triệu hồi "nhân vật phụ xuất hiện gần đây" mà Người viết có thể cần khi viết chương tiếp theo.
//
// Các mục đã được thăng cấp lên characters.json (Promoted=true) sẽ bị bỏ qua, tránh triệu hồi trùng lặp với hồ sơ cốt lõi.
func (s *CastStore) RecentActive(limit int) ([]domain.CastEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	entries, err := s.Load()
	if err != nil {
		return nil, err
	}
	active := entries[:0:0]
	for _, e := range entries {
		if e.Promoted {
			continue
		}
		active = append(active, e)
	}
	if len(active) == 0 {
		return nil, nil
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].LastSeenChapter != active[j].LastSeenChapter {
			return active[i].LastSeenChapter > active[j].LastSeenChapter
		}
		return active[i].AppearanceCount > active[j].AppearanceCount
	})
	if len(active) > limit {
		active = active[:limit]
	}
	return active, nil
}
