package bootstrap

import "testing"

func TestConfigResolveThinking(t *testing.T) {
	cfg := Config{
		Thinking: "low", // mặc định cấp cao nhất
		Roles: map[string]RoleConfig{
			"writer":    {Provider: "p", Model: "m", Thinking: "high"}, // ghi đè theo vai trò
			"architect": {Provider: "p", Model: "m"},                   // không có thinking, nên dùng giá trị mặc định
		},
	}

	cases := []struct {
		role string
		want string
	}{
		{"writer", "high"},     // ghi đè vai trò được ưu tiên
		{"architect", "low"},   // vai trò chưa cấu hình → dùng mặc định cấp cao nhất
		{"editor", "low"},      // vai trò không tồn tại → mặc định cấp cao nhất
		{"", "low"},            // rỗng → mặc định cấp cao nhất
		{"default", "low"},     // default → mặc định cấp cao nhất
		{"coordinator", "low"}, // chưa cấu hình → mặc định cấp cao nhất
	}
	for _, c := range cases {
		if got := cfg.ResolveThinking(c.role); got != c.want {
			t.Errorf("ResolveThinking(%q) = %q, want %q", c.role, got, c.want)
		}
	}

	// Khi mặc định cấp cao nhất cũng rỗng, vai trò chưa ghi đè trả về "" (không ghi đè).
	empty := Config{Roles: map[string]RoleConfig{"writer": {Thinking: "xhigh"}}}
	if got := empty.ResolveThinking("editor"); got != "" {
		t.Errorf("khi mặc định rỗng, editor phải trả về \"\", nhận được %q", got)
	}
	if got := empty.ResolveThinking("writer"); got != "xhigh" {
		t.Errorf("khi mặc định rỗng, ghi đè của writer phải có hiệu lực, nhận được %q", got)
	}
}
