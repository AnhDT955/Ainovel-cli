package domain

// CastEntry là một bản ghi nhân vật phụ trong danh sách diễn viên.
//
// Tách biệt khỏi Character (characters.json, do Architect quản lý):
//   - CastEntry được tự động tích lũy bởi công cụ commit_chapter, ghi lại "các nhân vật phụ có tên đã xuất hiện"
//   - Character do Architect thiết kế tường minh, ghi lại cung truyện nhân cách/đặc điểm/tier của nhân vật chính và nhân vật phụ quan trọng
//
// Khi trùng tên thì Character được ưu tiên (nhân vật cốt lõi không vào cast_ledger), tránh trùng lặp.
type CastEntry struct {
	Name string `json:"name"`
	// Aliases hiện chưa có kênh ghi; dự phòng cho công cụ "người dùng hợp nhất bí danh" trong tương lai
	// (ví dụ khai báo 'Lý chưởng quầy' và 'Lão Lý' là cùng một người). MergeAppearances đã hỗ trợ tìm kiếm theo bí danh.
	Aliases          []string `json:"aliases,omitempty"`
	BriefRole        string   `json:"brief_role,omitempty"` // Định vị một câu (do Người viết điền khi lần đầu xuất hiện, có thể bổ sung sau; không bị ghi đè)
	FirstSeenChapter int      `json:"first_seen_chapter"`
	LastSeenChapter  int      `json:"last_seen_chapter"`
	// AppearanceCount được suy ra từ len(AppearanceChapters), được đồng bộ khi merge.
	// Giữ lại trường tường minh để UI/JSON đọc trực tiếp, không cần tính lại mỗi lần.
	AppearanceCount    int   `json:"appearance_count"`
	AppearanceChapters []int `json:"appearance_chapters"`
	// Promoted đánh dấu bản ghi này đã được thăng cấp vào characters.json. RecentActive sẽ bỏ qua các bản ghi này,
	// tránh gọi lại trùng với hồ sơ cốt lõi. Kênh thăng cấp hiện chưa được triển khai, trường là hook dự phòng.
	Promoted bool `json:"promoted,omitempty"`
}

// CastIntro là khai báo giới thiệu của Người viết về nhân vật mới xuất hiện khi commit chương.
// Chỉ được áp dụng khi tên đó xuất hiện lần đầu hoặc BriefRole trong ledger vẫn còn trống.
type CastIntro struct {
	Name      string `json:"name"`
	BriefRole string `json:"brief_role"`
}
