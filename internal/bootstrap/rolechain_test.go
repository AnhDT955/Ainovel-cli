package bootstrap

import (
	"testing"
	"time"
)

func TestForRoleChain(t *testing.T) {
	def := NewSwappableModel("p", "model-default", nil)
	arch := NewSwappableModel("p", "model-architect", nil)
	extr := NewSwappableModel("p", "model-extractor", nil)

	nameOf := func(ms *ModelSet) string {
		m := ms.ForRoleChain("extractor", "architect")
		sm, ok := m.(*SwappableModel)
		if !ok {
			t.Fatalf("kiểu trả về lạ: %T", m)
		}
		_, name := sm.Current()
		return name
	}

	// 1. Có extractor -> dùng extractor
	ms := &ModelSet{Default: def, models: map[string]*SwappableModel{"architect": arch, "extractor": extr}}
	if got := nameOf(ms); got != "model-extractor" {
		t.Fatalf("kỳ vọng extractor, nhận %s", got)
	}

	// 2. Chưa cấu hình extractor -> lùi về architect (giữ nguyên hành vi cũ)
	ms = &ModelSet{Default: def, models: map[string]*SwappableModel{"architect": arch}}
	if got := nameOf(ms); got != "model-architect" {
		t.Fatalf("kỳ vọng architect, nhận %s", got)
	}

	// 3. Không cấu hình gì -> model mặc định
	ms = &ModelSet{Default: def, models: map[string]*SwappableModel{}}
	if got := nameOf(ms); got != "model-default" {
		t.Fatalf("kỳ vọng default, nhận %s", got)
	}
}

func TestKnownRoles_CoExtractor(t *testing.T) {
	if !knownRoles["extractor"] {
		t.Fatal("extractor phải là role hợp lệ")
	}
	if got := roleNames(); got != "architect/coordinator/editor/extractor/writer" {
		t.Fatalf("roleNames sai: %s", got)
	}
}

// Cấu hình role extractor phải qua được validate (nếu không, người dùng khai vào là app báo lỗi).
func TestValidate_ChapNhanRoleExtractor(t *testing.T) {
	c := &Config{
		Provider:  "p",
		ModelName: "m",
		Providers: map[string]ProviderConfig{"p": {APIKey: "k", BaseURL: "https://x.test"}},
		Roles: map[string]RoleConfig{
			"extractor": {Provider: "p", Model: "big-model", Thinking: "medium"},
		},
	}
	c.FillDefaults()
	if err := c.ValidateBase(); err != nil {
		t.Fatalf("role extractor phải hợp lệ, nhận lỗi: %v", err)
	}
}

func TestRuntimeConfig_ExtractorKhongBiIdleWatchdogCatNham(t *testing.T) {
	extractor := runtimeConfigForRole("extractor")
	if extractor.streamIdleTimeout != 0 {
		t.Fatalf("extractor phải tắt idle watchdog, nhận %s", extractor.streamIdleTimeout)
	}
	if extractor.requestTimeout != 20*time.Minute {
		t.Fatalf("extractor request timeout sai: %s", extractor.requestTimeout)
	}

	writer := runtimeConfigForRole("writer")
	if writer.streamIdleTimeout != 5*time.Minute {
		t.Fatalf("role thường vẫn phải giữ watchdog 5 phút, nhận %s", writer.streamIdleTimeout)
	}
	if writer.requestTimeout != 0 {
		t.Fatalf("role thường phải dùng request timeout mặc định, nhận %s", writer.requestTimeout)
	}
}
