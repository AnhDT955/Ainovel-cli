package domain

import (
	"strings"
	"testing"
)

// Bộ nền của "Thien dao bang" (2026-07) đúng như nó nằm trên đĩa: Kiến trúc sư lưu 6 quy tắc thế giới
// toàn chuỗi rỗng và 10 nhân vật chỉ mang tên vai trò. Cổng kiểm tra khi đó chỉ đếm số phần tử nên coi
// là đã đủ, tự đẩy sang giai đoạn viết, và 68 chương ra đời không mang hệ thống tu luyện nào của người
// dùng. Các test dưới đây neo chính xác dữ liệu đó.

var realWorldEmptyRules = []WorldRule{
	{Category: "", Rule: "", Boundary: ""},
	{Category: "", Rule: "", Boundary: ""},
	{Category: "", Rule: "", Boundary: ""},
	{Category: "", Rule: "", Boundary: ""},
	{Category: "", Rule: "", Boundary: ""},
	{Category: "", Rule: "", Boundary: ""},
}

var realWorldPlaceholderChars = []Character{
	{Name: "Nhân vật chính", Role: "trung tâm hành động và nhận thức", Description: ""},
	{Name: "Người dẫn đường", Role: "hiểu luật ngầm", Description: ""},
	{Name: "Phản diện tầng 1", Role: "đối thủ trực diện", Description: ""},
	{Name: "Phản diện lõi", Role: "thế lực đứng sau", Description: ""},
}

func TestValidateWorldRules_RejectsRealEmptySkeleton(t *testing.T) {
	err := ValidateWorldRules(realWorldEmptyRules)
	if err == nil {
		t.Fatal("6 quy tắc rỗng phải bị từ chối — đây chính là bộ nền đã làm hỏng cả cuốn sách")
	}
	if !strings.Contains(err.Error(), "rule rỗng") {
		t.Errorf("thông báo lỗi phải chỉ ra trường rule rỗng, got: %v", err)
	}
}

func TestValidateCharacters_RejectsRealPlaceholderSkeleton(t *testing.T) {
	err := ValidateCharacters(realWorldPlaceholderChars)
	if err == nil {
		t.Fatal("nhân vật chỉ có tên vai trò và description rỗng phải bị từ chối")
	}
	for _, want := range []string{"Nhân vật chính", "Phản diện tầng 1", "description rỗng"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("lỗi phải nêu %q, got: %v", want, err)
		}
	}
}

func TestValidatePremise_RejectsCopiedPlaceholderTitle(t *testing.T) {
	// premise.md thật mở đầu bằng đúng chữ "# Tên truyện" — mô hình chép nguyên xi chỗ điền
	// từ prompt khởi động, và tài liệu yêu cầu của người dùng cũng mở đầu bằng nhãn đó.
	err := ValidatePremise("# Tên truyện\n\n## Premise\nMột tiểu thuyết web novel dài kỳ.")
	if err == nil {
		t.Fatal("tiêu đề `# Tên truyện` là nhãn chỗ điền, phải bị từ chối")
	}
	if !strings.Contains(err.Error(), "chỗ điền") {
		t.Errorf("lỗi phải giải thích đây là nhãn chỗ điền, got: %v", err)
	}
}

func TestValidatePremise_AcceptsRealTitle(t *testing.T) {
	if err := ValidatePremise("# Loạn Thế Võ Đạo\n\n## Thể loại và tông điệu\nVõ hiệp"); err != nil {
		t.Fatalf("tên sách thật phải được chấp nhận: %v", err)
	}
}

// Mô tả súc tích là hợp lệ. Bản đầu của validator đặt ngưỡng độ dài và đã chặn nhầm đúng fixture này
// (description "独立记者", 4 ký tự) — ngưỡng độ dài đã bị bỏ, chỉ trường RỖNG mới là lỗi.
func TestValidateCharacters_AcceptsTerseButRealDescription(t *testing.T) {
	chars := []Character{{Name: "林晚", Role: "主角", Description: "独立记者"}}
	if err := ValidateCharacters(chars); err != nil {
		t.Fatalf("mô tả ngắn nhưng có thật phải được chấp nhận: %v", err)
	}
	if !chars[0].HasSubstance() {
		t.Error("HasSubstance phải đồng ý với ValidateCharacters")
	}
}

func TestValidateWorldRules_AcceptsRealRules(t *testing.T) {
	rules := []WorldRule{{
		Category: "magic",
		Rule:     "Hỗn Nguyên Đạo lục: cày độ thuần thục thì không có bình cảnh cách trở",
		Boundary: "Không thể bỏ qua thứ tự Luyện Bì → Luyện Tủy",
	}}
	if err := ValidateWorldRules(rules); err != nil {
		t.Fatalf("quy tắc có thật phải được chấp nhận: %v", err)
	}
	if !rules[0].HasSubstance() {
		t.Error("HasSubstance phải đồng ý với ValidateWorldRules")
	}
}

func TestHasSubstance_MatchesRealSkeleton(t *testing.T) {
	for _, r := range realWorldEmptyRules {
		if r.HasSubstance() {
			t.Fatal("quy tắc rỗng không được coi là có nội dung")
		}
	}
	for _, c := range realWorldPlaceholderChars {
		if c.HasSubstance() {
			t.Fatalf("nhân vật khung mẫu %q không được coi là có nội dung", c.Name)
		}
	}
}

func TestExtractNovelName(t *testing.T) {
	cases := []struct{ premise, want string }{
		{"# Loạn Thế Võ Đạo\n\nnội dung", "Loạn Thế Võ Đạo"},
		{"# 《Huyền Thiên》\n", "Huyền Thiên"},
		{"# Tên truyện\n", ""},   // chỗ điền tiếng Việt
		{"# 书名\n", ""},          // chỗ điền tiếng Trung
		{"# TÊN TRUYỆN\n", ""},   // so khớp không phân biệt hoa thường
		{"không có tiêu đề", ""}, // dòng đầu không phải heading
	}
	for _, c := range cases {
		if got := ExtractNovelName(c.premise); got != c.want {
			t.Errorf("ExtractNovelName(%q) = %q, want %q", c.premise, got, c.want)
		}
	}
}
