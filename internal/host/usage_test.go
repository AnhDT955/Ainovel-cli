package host

import (
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/models"
)

// makeUsageMsg tạo một tin nhắn mà callback OnMessage có thể nhận (kèm Usage).
// Role phải được đặt tường minh là assistant: UsageTracker.Record hiện lọc theo role,
// chỉ tin nhắn assistant mới được cộng dồn (các role khác tự nhiên không mang usage).
func makeUsageMsg(input, cacheRead, cacheWrite, output int) agentcore.AgentMessage {
	return agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite,
		},
	}
}

// Test_pushSample_RingBuffer kiểm tra ngữ nghĩa xoay vòng của cửa sổ trượt:
// N lần đầu append trực tiếp; sau đó ghi đè mục cũ nhất theo sampleIdx. recentSums luôn phản ánh "N lần gần nhất".
func Test_pushSample_RingBuffer(t *testing.T) {
	var tot agentTotals

	for i := 1; i <= recentSampleCap; i++ {
		pushSample(&tot, i, i*100)
	}
	if got := len(tot.samples); got != recentSampleCap {
		t.Fatalf("after %d pushes, samples len=%d want %d", recentSampleCap, got, recentSampleCap)
	}

	pushSample(&tot, 999, 99900)
	if got := len(tot.samples); got != recentSampleCap {
		t.Fatalf("after overflow, samples len=%d want %d (no growth)", got, recentSampleCap)
	}
	cacheRead, input := recentSums(&tot)
	expectedCacheRead := 999
	expectedInput := 99900
	for i := 2; i <= recentSampleCap; i++ {
		expectedCacheRead += i
		expectedInput += i * 100
	}
	if cacheRead != expectedCacheRead || input != expectedInput {
		t.Fatalf("recentSums after overflow = (%d, %d), want (%d, %d)",
			cacheRead, input, expectedCacheRead, expectedInput)
	}
}

// Test_UsageTracker_RecordAccumulates kiểm tra Record cộng dồn đúng với nhiều role,
// tổng hợp toàn bộ = tổng tất cả role; mỗi role độc lập với nhau.
func Test_UsageTracker_RecordAccumulates(t *testing.T) {
	tk := NewUsageTracker(nil, nil) // modelSet=nil → dùng provider Cost làm dự phòng, không ảnh hưởng logic cộng dồn

	tk.Record("writer", makeUsageMsg(1000, 800, 0, 200))
	tk.Record("writer", makeUsageMsg(1500, 1200, 100, 300))
	tk.Record("editor", makeUsageMsg(500, 0, 0, 100))

	cost, in, out, cr, cw := tk.Totals()
	if in != 3000 || out != 600 || cr != 2000 || cw != 100 {
		t.Fatalf("totals = (in=%d out=%d cr=%d cw=%d), want (3000 600 2000 100)", in, out, cr, cw)
	}
	if cost != 0 {
		t.Errorf("cost should be 0 when modelSet=nil and no provider Cost, got %v", cost)
	}

	per := tk.PerAgent()
	if len(per) != 2 {
		t.Fatalf("per-agent len=%d want 2", len(per))
	}
	// PerAgent sắp xếp giảm dần theo CacheRead: writer (2000) phải đứng trước editor (0)
	if per[0].Role != "writer" || per[1].Role != "editor" {
		t.Fatalf("per-agent order = %s,%s want writer,editor", per[0].Role, per[1].Role)
	}
	if per[0].Input != 2500 || per[0].CacheRead != 2000 {
		t.Errorf("writer totals = (in=%d cr=%d), want (2500 2000)", per[0].Input, per[0].CacheRead)
	}
}

// Test_UsageTracker_ArchitectAliasNormalized kiểm tra architect_short/mid/long
// đều được chuẩn hóa về cùng một key "architect" (tránh bị tách thành nhiều dòng khi /model chuyển sub-role).
func Test_UsageTracker_ArchitectAliasNormalized(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("architect_short", makeUsageMsg(100, 50, 0, 20))
	tk.Record("architect_mid", makeUsageMsg(200, 100, 0, 40))
	tk.Record("architect_long", makeUsageMsg(300, 150, 0, 60))

	per := tk.PerAgent()
	if len(per) != 1 {
		t.Fatalf("aliases must merge to single role, got %d entries: %+v", len(per), per)
	}
	if per[0].Role != "architect" {
		t.Fatalf("merged role name = %q, want architect", per[0].Role)
	}
	if per[0].Input != 600 || per[0].CacheRead != 300 {
		t.Errorf("merged totals = (in=%d cr=%d), want (600 300)", per[0].Input, per[0].CacheRead)
	}
}

func Test_UsageTracker_PerModelAccumulates(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.accumulate("writer", "openrouter", "model-a", agentcore.Usage{Input: 1000, Output: 200, CacheRead: 700})
	tk.accumulate("editor", "openrouter", "model-b", agentcore.Usage{Input: 500, Output: 100})
	tk.accumulate("writer", "openrouter", "model-a", agentcore.Usage{Input: 300, Output: 80, CacheRead: 200})

	perModel := tk.PerModel()
	if len(perModel) != 2 {
		t.Fatalf("per-model len=%d want 2", len(perModel))
	}
	seen := map[string]AgentUsage{}
	for _, m := range perModel {
		seen[m.Model] = m
	}
	if seen["openrouter/model-a"].Input != 1300 || seen["openrouter/model-a"].CacheRead != 900 {
		t.Errorf("model-a totals = %+v", seen["openrouter/model-a"])
	}
	if seen["openrouter/model-b"].Output != 100 {
		t.Errorf("model-b totals = %+v", seen["openrouter/model-b"])
	}

	snap := tk.Snapshot()
	restored := NewUsageTracker(nil, nil)
	restored.applyState(snap)
	if got := restored.PerModel(); len(got) != 2 {
		t.Fatalf("restored per-model len=%d want 2: %+v", len(got), got)
	}
}

func Test_UsageTracker_RecordUsesActualUsageModel(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("writer", agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Provider: "openrouter",
			Model:    "google/gemini-2.5-pro",
			Input:    1000,
			Output:   200,
		},
	})

	perModel := tk.PerModel()
	if len(perModel) != 1 {
		t.Fatalf("per-model len=%d want 1: %+v", len(perModel), perModel)
	}
	if perModel[0].Model != "openrouter/google/gemini-2.5-pro" {
		t.Fatalf("model key = %q, want openrouter/google/gemini-2.5-pro", perModel[0].Model)
	}
	if perModel[0].Input != 1000 || perModel[0].Output != 200 {
		t.Fatalf("model totals = %+v", perModel[0])
	}
}

func Test_UsageTracker_ProviderOnlyDoesNotInventModelKey(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("writer", agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Provider: "openrouter",
			Input:    1000,
			Output:   200,
		},
	})

	if got := tk.PerModel(); len(got) != 0 {
		t.Fatalf("provider-only usage must not create model stats without a model, got %+v", got)
	}
}

// Test_UsageTracker_RecentWindowReflectsLatest kiểm tra cửa sổ trượt phản ánh "N lần gần nhất",
// không bị kéo lùi bởi lần đầu ít hit — đây chính là vấn đề "kéo lùi giai đoạn đầu vs hit thấp trạng thái ổn định" mà P1 giải quyết.
func Test_UsageTracker_RecentWindowReflectsLatest(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	// 5 lần đầu hit rất thấp (kịch bản chương mở đầu)
	for i := 0; i < 5; i++ {
		tk.Record("writer", makeUsageMsg(1000, 0, 0, 200))
	}
	// 8 lần sau (>5) hit cao (kịch bản trạng thái ổn định)
	for i := 0; i < 8; i++ {
		tk.Record("writer", makeUsageMsg(1000, 900, 0, 200))
	}

	per := tk.PerAgent()
	if len(per) != 1 {
		t.Fatalf("len=%d want 1", len(per))
	}
	w := per[0]

	// Tích lũy: 13 lần trong đó 8 lần có hit → 7200/13000 ≈ 55.4%
	cumulativeRate := float64(w.CacheRead) / float64(w.Input) * 100
	if cumulativeRate < 50 || cumulativeRate > 60 {
		t.Errorf("cumulative hit rate = %.1f%%, want ~55%%", cumulativeRate)
	}

	// Cửa sổ trượt: 10 lần gần nhất có 8 lần hit cao + 2 lần zero hit → 7200/10000 = 72%
	if w.RecentSamples != recentSampleCap {
		t.Errorf("recent samples = %d, want %d (window full)", w.RecentSamples, recentSampleCap)
	}
	recentRate := float64(w.RecentCacheRead) / float64(w.RecentInput) * 100
	if recentRate < 70 || recentRate > 75 {
		t.Errorf("recent hit rate = %.1f%%, want ~72%% (proves window dropped early misses)", recentRate)
	}
	// Điểm mấu chốt: N lần gần đây cao hơn rõ ràng so với tích lũy, chứng minh 0 giai đoạn đầu đã bị đẩy ra khỏi cửa sổ
	if recentRate <= cumulativeRate {
		t.Errorf("recent (%.1f%%) must exceed cumulative (%.1f%%) once window slides past early misses",
			recentRate, cumulativeRate)
	}
}

// Test_computeSaved kiểm tra thuật toán saved: CacheRead × (giá Input - giá CacheRead);
// khi chênh lệch giá ≤ 0 hoặc InputCost ≤ 0 thì trả về 0 (phụ phí CacheWrite không được khấu trừ).
func Test_computeSaved(t *testing.T) {
	cases := []struct {
		name  string
		usage agentcore.Usage
		entry models.ModelEntry
		want  float64
	}{
		{
			name:  "anthropic 5m hit tiết kiệm 90%",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 80_000},
			entry: models.ModelEntry{InputCostPer1M: 3.0, CacheReadCostPer1M: 0.3},
			want:  80_000 * (3.0 - 0.3) / 1_000_000, // 0.216
		},
		{
			name:  "không hit saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 0},
			entry: models.ModelEntry{InputCostPer1M: 3.0, CacheReadCostPer1M: 0.3},
			want:  0,
		},
		{
			name:  "mô hình chưa có giá saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 50_000},
			entry: models.ModelEntry{InputCostPer1M: 0, CacheReadCostPer1M: 0},
			want:  0,
		},
		{
			name:  "chênh lệch giá bất thường saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 50_000},
			entry: models.ModelEntry{InputCostPer1M: 1.0, CacheReadCostPer1M: 2.0}, // cache lại đắt hơn
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeSaved(tc.usage, tc.entry)
			if got != tc.want {
				t.Errorf("computeSaved=%v want %v", got, tc.want)
			}
		})
	}
}

// Test_UsageTracker_CacheCapableSticky kiểm tra CacheCapable một khi đã đặt true thì không bị thu hồi.
// Lịch sử đã chạy mô hình hỗ trợ cache → dữ liệu hit tích lũy hợp lệ; chuyển sang mô hình không hỗ trợ giữa chừng không được làm cờ bị thu hồi.
//
// Mô phỏng bằng cách gán trực tiếp perAgent (đường resolveCost cần ModelSet+Registry, lớp tích hợp đã phủ).
func Test_UsageTracker_CacheCapableSticky(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	// Mô phỏng "đã từng chạy mô hình hỗ trợ cache + đã hit"
	tk.perAgent["writer"] = &agentTotals{
		Input: 1000, CacheRead: 500, Output: 200, CacheCapable: true,
	}
	// Tiếp theo thêm một lần gọi "mô hình không hỗ trợ cache"
	tk.Record("writer", makeUsageMsg(500, 0, 0, 100))

	per := tk.PerAgent()
	if len(per) != 1 || per[0].Role != "writer" {
		t.Fatalf("expected single writer entry, got %+v", per)
	}
	if !per[0].CacheCapable {
		t.Errorf("CacheCapable must remain true after switching to non-capable model")
	}
	if per[0].CacheRead != 500 || per[0].Input != 1500 {
		t.Errorf("totals after merge = (in=%d cr=%d), want (1500 500)",
			per[0].Input, per[0].CacheRead)
	}
}

// Test_UsageTracker_PerAgentSkipsZero kiểm tra role chưa tiêu thụ token không xuất hiện trong PerAgent.
func Test_UsageTracker_PerAgentSkipsZero(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	// Tạo một role nhưng không tiêu thụ token (trường hợp cực đoan)
	tk.perAgent["ghost"] = &agentTotals{}
	tk.Record("writer", makeUsageMsg(100, 50, 0, 20))

	per := tk.PerAgent()
	if len(per) != 1 || per[0].Role != "writer" {
		t.Fatalf("ghost role with zero tokens must be skipped, got %+v", per)
	}
}

// Test_UsageTracker_MissingAssistantUsageCounted kiểm tra ranh giới phán định của
// missingAssistantUsage:
//   - Đường cộng dồn chỉ nhìn Usage != nil (không ràng buộc với Role)
//   - Đường chẩn đoán yêu cầu Role=Assistant và Content không rỗng — đây mới là
//     "một lần phản hồi LLM thực sự nhưng không lấy được usage", tương ứng điển hình
//     khi upstream streaming không gửi final chunk include_usage của OpenAI.
//     Các trường hợp khác (tin nhắn user/tool, assistant content rỗng) đều không tính missing.
func Test_UsageTracker_MissingAssistantUsageCounted(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	withContent := func(text string) agentcore.Message {
		return agentcore.Message{
			Role:    agentcore.RoleAssistant,
			Content: []agentcore.ContentBlock{agentcore.TextBlock(text)},
		}
	}

	// assistant + có Content + nil Usage → trông như phản hồi thực nhưng thiếu usage, tính vào chẩn đoán
	tk.Record("writer", withContent("hi"))
	tk.Record("writer", withContent("again"))
	// assistant nhưng Content rỗng → đường phục hồi bất thường hoặc tin nhắn giữ chỗ, không tính missing
	tk.Record("writer", agentcore.Message{Role: agentcore.RoleAssistant})
	// Tin nhắn user/tool tự nhiên không mang usage, dù Content có hay không cũng không tính missing
	tk.Record("writer", agentcore.Message{Role: agentcore.RoleUser, Content: []agentcore.ContentBlock{agentcore.TextBlock("u")}})
	tk.Record("writer", agentcore.Message{Role: agentcore.RoleTool, Content: []agentcore.ContentBlock{agentcore.TextBlock("t")}})
	// Có usage bình thường → đi vào đường cộng dồn, không tính vào chẩn đoán
	tk.Record("writer", makeUsageMsg(100, 50, 0, 20))

	if got := tk.MissingAssistantUsage(); got != 2 {
		t.Errorf("MissingAssistantUsage=%d, want 2", got)
	}
	_, in, _, _, _ := tk.Totals()
	if in != 100 {
		t.Errorf("đường bình thường bị phá vỡ tích lũy, input=%d want 100", in)
	}
}

// Test_UsageTracker_CacheCapableFromFacts kiểm tra CacheCapable vẫn có thể được đánh dấu true
// dựa trên "thực tế" khi không tìm thấy mô hình trong registry:
// mô hình backend tự xây / proxy nội địa thường không có trong chỉ mục pricing của BerriAI/litellm,
// resolveCost trả về capable=false; nhưng chỉ cần backend thực sự trả về CacheRead hoặc CacheWrite > 0,
// điều đó chứng minh mô hình khách quan hỗ trợ prompt cache, dòng per-role không nên hiển thị "chưa kích hoạt".
func Test_UsageTracker_CacheCapableFromFacts(t *testing.T) {
	tk := NewUsageTracker(nil, nil) // modelSet=nil → resolveCost luôn trả capable=false

	// Một lần có CacheWrite (mô phỏng lần đầu ghi vào cache, registry chưa đánh dấu capable, nhưng thực tế chứng minh hỗ trợ)
	tk.Record("writer", makeUsageMsg(1000, 0, 200, 100))
	per := tk.PerAgent()
	if len(per) != 1 || !per[0].CacheCapable {
		t.Fatalf("CacheWrite > 0 phải lập tức đánh dấu CacheCapable=true, got %+v", per)
	}
	if !tk.OverallCacheCapable() {
		t.Errorf("overall CacheCapable cũng phải được đồng bộ đặt true")
	}

	// Chiều ngược lại: role hoàn toàn không có hoạt động cache, CacheCapable phải giữ false
	tk.Record("editor", makeUsageMsg(500, 0, 0, 100))
	per = tk.PerAgent()
	for _, a := range per {
		if a.Role == "editor" && a.CacheCapable {
			t.Errorf("editor không có CacheRead/Write nào, CacheCapable không được đánh dấu nhầm thành true")
		}
	}
}

// Test_UsageTracker_AccumulatesAnyRoleWithUsage kiểm tra đường cộng dồn tách biệt với Role:
// ngay cả khi trong tương lai một adapter nào đó gắn usage vào tin nhắn thuộc role không phải assistant,
// vẫn cộng dồn đúng. Giữ vững hợp đồng "quy tắc gắn và quy tắc cộng dồn tách biệt nhau".
func Test_UsageTracker_AccumulatesAnyRoleWithUsage(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	// Tạo một tin nhắn lý thuyết hiếm gặp: tin nhắn không phải assistant nhưng có Usage
	hypothetical := agentcore.Message{
		Role:  agentcore.RoleSystem,
		Usage: &agentcore.Usage{Input: 200, Output: 50, CacheRead: 100},
	}
	tk.Record("writer", hypothetical)

	_, in, out, cr, _ := tk.Totals()
	if in != 200 || out != 50 || cr != 100 {
		t.Errorf("không cộng dồn theo trường Usage, got (in=%d out=%d cr=%d) want (200 50 100)", in, out, cr)
	}
	if tk.MissingAssistantUsage() != 0 {
		t.Errorf("có Usage không được tính vào missing")
	}
}

// Test_UsageTracker_OnCostCallback kiểm tra điểm kết nối của bộ canh ngân sách: sau mỗi lần ghi,
// callback bên ngoài khóa mang chi phí tích lũy mới nhất (bao gồm đường provider tự báo cost).
func Test_UsageTracker_OnCostCallback(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	var got []float64
	tk.SetOnCost(func(total float64) { got = append(got, total) })

	msg := func(cost float64) agentcore.AgentMessage {
		return agentcore.Message{
			Role:  agentcore.RoleAssistant,
			Usage: &agentcore.Usage{Input: 100, Output: 10, Cost: &agentcore.Cost{Total: cost}},
		}
	}
	tk.Record("writer", msg(0.5))
	tk.Record("writer", msg(0.25))

	if len(got) != 2 || got[0] != 0.5 || got[1] != 0.75 {
		t.Fatalf("onCost should carry growing totals, got %v", got)
	}
}

// Test_UsageTracker_OnMissingUsageOnce kiểm tra callback điểm mù chỉ kích hoạt lần đầu tiên.
func Test_UsageTracker_OnMissingUsageOnce(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	fired := 0
	tk.SetOnMissingUsage(func() { fired++ })

	noUsage := agentcore.Message{Role: agentcore.RoleAssistant, Content: []agentcore.ContentBlock{agentcore.TextBlock("nội dung")}}
	tk.Record("writer", noUsage)
	tk.Record("writer", noUsage)
	tk.Record("editor", noUsage)

	if fired != 1 {
		t.Fatalf("onMissingUsage should fire exactly once, got %d", fired)
	}
}
