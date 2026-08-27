package host

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/models"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// recentSampleCap là kích thước cửa sổ trượt: chỉ giữ lại N lần gọi gần nhất của mỗi role
// với các mẫu (cacheRead, input), dùng để so sánh "tích lũy vs N lần gần nhất"
// tỉ lệ cache hit ở cột trái, nhận diện "tải nặng giai đoạn đầu" vs "hit thấp ổn định".
const recentSampleCap = 10

// UsageTracker tích lũy token LLM đầu vào/đầu ra và chi phí USD của tất cả agent trong toàn bộ phiên.
//
// Cơ chế hoạt động:
//   - Gọi Record(agentName, msg) mỗi khi callback OnMessage của agent kích hoạt
//   - agentName được ánh xạ thành role (architect_* đều quy về architect), tra ModelSet để biết model hiện tại của role đó
//   - Dùng models.DefaultRegistry để tra giá model, nhân theo 4 hạng mục: input không cache / output / cache read / cache write
//   - Khi không tìm thấy model trong registry, fallback về msg.Usage.Cost.Total (do nhà cung cấp trả, có thể bằng 0)
//   - Sau khi hot-switch model (/model), các tin nhắn tiếp theo tự động tính theo model mới; tin nhắn cũ giữ nguyên chi phí cũ
//
// Đồng thời duy trì chiều per-role (writer/editor/architect/coordinator):
//   - Dữ liệu cache hit tích lũy → hiệu quả tối ưu tổng thể
//   - Cửa sổ trượt N lần gần nhất → phân biệt tải đầu kỳ vs hit thấp ổn định
//   - Cờ CacheCapable → phân biệt "chưa bật" và "thực sự 0% hit"
//
// An toàn đa luồng.
type UsageTracker struct {
	mu       sync.Mutex
	overall  agentTotals
	perAgent map[string]*agentTotals // key là tên role đã quy chuẩn bởi agentRoleName
	perModel map[string]*agentTotals // key là provider/model; khi không rõ provider thì chỉ là model
	modelSet *bootstrap.ModelSet
	store    *storepkg.Store // có thể nil (trong test), khi nil tất cả phương thức persist im lặng noop

	// missingAssistantUsage đếm số lần "nhận được tin nhắn assistant nhưng Usage là nil".
	// Thực tế chủ yếu xảy ra khi backend tự dựng tương thích OpenAI không gửi
	// final usage chunk theo giao thức stream_options.include_usage của OpenAI —
	// partial.Usage luôn nil, mọi trường tích lũy đứng ở 0. Bộ đếm cho phép UI
	// thông báo trực tiếp "upstream không trả usage, không phải lỗi module này",
	// thay vì cứ ngồi debug code panel cache.
	missingAssistantUsage int
	loggedMissingUsage    bool // chỉ warn một lần trong toàn phiên, tránh spam tui.log

	// saveCh được Record kích hoạt không chặn sau mỗi lần tích lũy; autoSaveLoop lắng nghe và ghi xuống đĩa theo debounce.
	// buffered=1: nhiều Record liên tiếp gộp thành một tín hiệu ghi; nếu đầy thì bỏ qua, tick tiếp theo sẽ ghi chung.
	saveCh chan struct{}

	// onCost được gọi ngoài lock sau mỗi lần ghi sổ, mang theo chi phí tích lũy mới nhất (dùng cho BudgetSentinel kiểm tra ngưỡng).
	// Phải được đặt qua SetOnCost trước khi Record chạy đa luồng, sau đó chỉ đọc.
	onCost func(total float64)

	// onMissingUsage được gọi một lần khi lần đầu phát hiện "tin nhắn assistant không có Usage"
	// (cùng thời điểm với slog warn). Khi bật ngân sách, điều này nghĩa là mù chi phí —
	// cost luôn 0, ngân sách không bao giờ kích hoạt, cần thông báo người dùng.
	onMissingUsage func()
}

// usageSample là mẫu cache hit của một lần OnMessage, chỉ ghi tử số và mẫu số tỉ lệ hit.
type usageSample struct {
	CacheRead int
	Input     int
}

// agentTotals là bộ đếm tích lũy của một agent.
//   - Saved là chênh lệch "nếu tính theo giá không cache" so với chi phí thực tế dựa trên dữ liệu hit hiện tại
//   - CacheCapable chỉ được đặt true sau khi role đó có ít nhất một lần gọi qua model đã biết hỗ trợ cache
//   - samples là ring buffer độ dài cố định, recentSampleCap lần đầu append thẳng, sau đó luân chuyển theo sampleIdx
type agentTotals struct {
	Input        int
	Output       int
	CacheRead    int
	CacheWrite   int
	Cost         float64
	Saved        float64
	CacheCapable bool
	samples      []usageSample
	sampleIdx    int
}

func NewUsageTracker(set *bootstrap.ModelSet, store *storepkg.Store) *UsageTracker {
	return &UsageTracker{
		modelSet: set,
		store:    store,
		perAgent: make(map[string]*agentTotals, 4),
		perModel: make(map[string]*agentTotals, 4),
		saveCh:   make(chan struct{}, 1),
	}
}

// Record phân phát một tin nhắn agent sang hai nhánh: tích lũy / chẩn đoán.
//
// Nhánh tích lũy chỉ kiểm tra Usage có tồn tại hay không — "tin nhắn nào mang Usage"
// là chi tiết lắp ráp của adapter agentcore/litellm (upstream protocol đặt usage ở
// top-level response), quy tắc lắp ráp thay đổi sau này không cần sửa chỗ này.
// Nhánh chẩn đoán yêu cầu Role=Assistant và Content không rỗng, tránh AbortMsg /
// tin khôi phục lỗi / tool / tin user làm ô nhiễm bộ đếm missingAssistantUsage.
func (t *UsageTracker) Record(agentName string, msg agentcore.AgentMessage) {
	if t == nil {
		return
	}
	m, ok := msg.(agentcore.Message)
	if !ok {
		return
	}
	if m.Usage == nil {
		if m.Role == agentcore.RoleAssistant && len(m.Content) > 0 {
			t.flagMissingUsage(agentName)
		}
		return
	}
	role := agentRoleName(agentName)
	provider, modelName := usageActualModel(m.Usage)
	t.accumulate(role, provider, modelName, *m.Usage)
}

func usageActualModel(u *agentcore.Usage) (provider, modelName string) {
	if u == nil {
		return "", ""
	}
	return strings.TrimSpace(u.Provider), strings.TrimSpace(u.Model)
}

// flagMissingUsage đếm một sự kiện "có vẻ là phản hồi LLM thật nhưng không lấy được usage",
// chỉ ghi log warn một lần trong toàn phiên để tránh spam tui.log.
func (t *UsageTracker) flagMissingUsage(agentName string) {
	t.mu.Lock()
	t.missingAssistantUsage++
	shouldLog := !t.loggedMissingUsage
	t.loggedMissingUsage = true
	t.mu.Unlock()
	if shouldLog {
		slog.Warn("Phản hồi LLM không mang dữ liệu usage, panel cache/chi phí sẽ không tích lũy — thường do upstream streaming không gửi final usage chunk theo giao thức include_usage của OpenAI",
			"module", "usage", "agent", agentName)
		if t.onMissingUsage != nil {
			t.onMissingUsage()
		}
	}
	t.notifyDirty()
}

// SetOnMissingUsage đăng ký callback một lần cho "lần đầu phát hiện thiếu usage".
// Phải gọi một lần trong giai đoạn khởi tạo Host, trước khi Record chạy đa luồng.
func (t *UsageTracker) SetOnMissingUsage(cb func()) {
	if t == nil {
		return
	}
	t.onMissingUsage = cb
}

// notifyDirty kích hoạt không chặn một tín hiệu ghi xuống đĩa, autoSaveLoop sẽ thực sự ghi theo debounce.
// Kênh tín hiệu buffered=1: nhiều Record liên tiếp gộp thành một yêu cầu lưu là đủ.
func (t *UsageTracker) notifyDirty() {
	if t == nil || t.saveCh == nil {
		return
	}
	select {
	case t.saveCh <- struct{}{}:
	default:
	}
}

// accumulate tích lũy một tin nhắn có Usage vào ba bộ đếm: overall / per-role / per-model.
// provider/model rỗng nghĩa là "lấy model của role từ ModelSet hiện tại" (luồng thời gian thực);
// không rỗng nghĩa là "bắt buộc tính theo model chỉ định" (luồng replay dùng _meta trong session jsonl).
// resolveCost thực thi ngoài lock (chỉ đọc modelSet/Registry); trong lock chỉ làm phép cộng.
func (t *UsageTracker) accumulate(role, provider, modelName string, u agentcore.Usage) {
	provider, modelName = t.effectiveModel(role, provider, modelName)
	cost, saved, capable := t.resolveCost(modelName, u)

	t.mu.Lock()
	addUsage(&t.overall, u, cost, saved, capable)

	per := t.perAgent[role]
	if per == nil {
		per = &agentTotals{}
		t.perAgent[role] = per
	}
	addUsage(per, u, cost, saved, capable)

	if key := modelUsageKey(provider, modelName); key != "" {
		perModel := t.perModel[key]
		if perModel == nil {
			perModel = &agentTotals{}
			t.perModel[key] = perModel
		}
		addUsage(perModel, u, cost, saved, capable)
	}
	total := t.overall.Cost
	t.mu.Unlock()

	t.notifyDirty()
	if t.onCost != nil {
		t.onCost(total)
	}
}

// SetOnCost đăng ký callback ghi sổ (mang theo chi phí tích lũy mới nhất, gọi ngoài lock).
// Phải gọi một lần trong giai đoạn khởi tạo Host, trước khi Record chạy đa luồng.
func (t *UsageTracker) SetOnCost(cb func(total float64)) {
	if t == nil {
		return
	}
	t.onCost = cb
}

func (t *UsageTracker) effectiveModel(role, provider, modelName string) (string, string) {
	provider = strings.TrimSpace(provider)
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		if t != nil && t.modelSet != nil {
			p, m, _ := t.modelSet.CurrentSelection(role)
			return p, m
		}
		return "", ""
	}
	if provider == "" && t != nil && t.modelSet != nil {
		p, m, _ := t.modelSet.CurrentSelection(role)
		if m == modelName {
			provider = p
		}
	}
	return provider, modelName
}

func modelUsageKey(provider, modelName string) string {
	provider = strings.TrimSpace(provider)
	modelName = strings.TrimSpace(modelName)
	switch {
	case modelName == "":
		return ""
	case provider == "":
		return modelName
	default:
		return provider + "/" + modelName
	}
}

// addUsage cộng dồn token và chi phí của một lần gọi vào một bộ totals.
// Phải được gọi trong khi giữ UsageTracker.mu.
//
// CacheCapable ưu tiên phán định theo "thực tế": chỉ cần thấy CacheRead hoặc CacheWrite > 0
// là đã chứng minh upstream thực sự làm prompt caching. CacheReadCostPer1M trong registry
// chỉ là fallback, vì các model backend tự dựng (mimo-v2.5-pro / proxy nội địa v.v.) thường
// không có trong chỉ mục giá BerriAI/litellm, nhưng dữ liệu cache trong Usage hoàn toàn có,
// UI không nên nhầm thành "chưa bật".
func addUsage(t *agentTotals, u agentcore.Usage, cost, saved float64, capable bool) {
	t.Input += u.Input
	t.Output += u.Output
	t.CacheRead += u.CacheRead
	t.CacheWrite += u.CacheWrite
	t.Cost += cost
	t.Saved += saved
	if capable || u.CacheRead > 0 || u.CacheWrite > 0 {
		t.CacheCapable = true
	}
	pushSample(t, u.CacheRead, u.Input)
}

// pushSample đẩy một mẫu vào ring buffer. recentSampleCap lần đầu append thẳng, sau đó luân chuyển ghi đè.
func pushSample(t *agentTotals, cacheRead, input int) {
	s := usageSample{CacheRead: cacheRead, Input: input}
	if len(t.samples) < recentSampleCap {
		t.samples = append(t.samples, s)
		return
	}
	t.samples[t.sampleIdx] = s
	t.sampleIdx = (t.sampleIdx + 1) % recentSampleCap
}

// recentSums trả về tổng cacheRead và input trong cửa sổ trượt, làm tử số/mẫu số cho "tỉ lệ hit N lần gần nhất".
// Dùng sum/sum thay vì "trung bình các tỉ lệ đơn lẻ" để tránh khuếch đại nhiễu từ mẫu nhỏ (input=vài trăm token).
func recentSums(t *agentTotals) (cacheRead, input int) {
	for _, s := range t.samples {
		cacheRead += s.CacheRead
		input += s.Input
	}
	return cacheRead, input
}

// Totals trả về snapshot tổng tích lũy.
func (t *UsageTracker) Totals() (cost float64, input, output, cacheRead, cacheWrite int) {
	if t == nil {
		return 0, 0, 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.Cost, t.overall.Input, t.overall.Output, t.overall.CacheRead, t.overall.CacheWrite
}

// SavedUSD trả về tổng USD tiết kiệm được nhờ cache hit tích lũy.
func (t *UsageTracker) SavedUSD() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.Saved
}

// OverallRecent trả về tổng cacheRead, tổng input và số mẫu trong cửa sổ trượt (≤ recentSampleCap lần).
func (t *UsageTracker) OverallRecent() (cacheRead, input, samples int) {
	if t == nil {
		return 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	r, in := recentSums(&t.overall)
	return r, in, len(t.overall.samples)
}

// OverallCacheCapable cho biết tổng thể có ít nhất một lần qua model đã biết hỗ trợ cache hay không.
func (t *UsageTracker) OverallCacheCapable() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.CacheCapable
}

// MissingAssistantUsage trả về số lần tích lũy "nhận được tin nhắn assistant nhưng Usage là nil".
// Lớn hơn 0 thường nghĩa là upstream streaming không gửi final usage chunk theo OpenAI,
// UI dùng để hiển thị gợi ý thay vì nhầm rằng module cache bị lỗi.
func (t *UsageTracker) MissingAssistantUsage() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.missingAssistantUsage
}

// ── Lưu trữ bền vững ──

// Snapshot sao chép trạng thái tích lũy hiện tại thành domain.UsageState có thể serialize.
// Ring buffer samples không đưa vào snapshot — đó là cửa sổ chẩn đoán ngắn hạn, ít ý nghĩa khi giữa các tiến trình.
func (t *UsageTracker) Snapshot() domain.UsageState {
	if t == nil {
		return domain.UsageState{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := domain.UsageState{
		Schema:       domain.UsageSchemaVersion,
		UpdatedAt:    time.Now(),
		Overall:      totalsSnapshot(&t.overall),
		PerAgent:     make(map[string]domain.AgentUsageTotals, len(t.perAgent)),
		PerModel:     make(map[string]domain.AgentUsageTotals, len(t.perModel)),
		MissingUsage: t.missingAssistantUsage,
	}
	for role, v := range t.perAgent {
		state.PerAgent[role] = totalsSnapshot(v)
	}
	for model, v := range t.perModel {
		state.PerModel[model] = totalsSnapshot(v)
	}
	return state
}

// LoadFromStore đọc snapshot đã lưu từ store.Usage và nạp lại vào bộ nhớ. Trả về true nghĩa là
// đã tải thành công một trạng thái không rỗng (schema khớp); false nghĩa là không có file hoặc
// không dùng được, bên gọi nên tiếp tục replay session để nạp lại từ đầu.
func (t *UsageTracker) LoadFromStore() (bool, error) {
	if t == nil || t.store == nil {
		return false, nil
	}
	state, err := t.store.Usage.Load()
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	}
	t.applyState(*state)
	return true, nil
}

// SaveNow ghi snapshot hiện tại xuống đĩa ngay lập tức. Cả autoSaveLoop lẫn Close đều dùng hàm này.
func (t *UsageTracker) SaveNow() error {
	if t == nil || t.store == nil {
		return nil
	}
	return t.store.Usage.Save(t.Snapshot())
}

// StartAutoSave khởi một goroutine lắng nghe saveCh + debounce ghi đĩa. Trước khi ctx done,
// sẽ flush trạng thái chưa lưu lần cuối. Close kích hoạt flush + thoát bằng cách cancel ctx.
func (t *UsageTracker) StartAutoSave(ctx context.Context) {
	if t == nil || t.store == nil {
		return
	}
	go t.autoSaveLoop(ctx)
}

// autoSaveLoop giảm tần suất tín hiệu dirty cao thành ghi đĩa 500ms một lần.
//
// Thiết kế: 500ms là giá trị kinh nghiệm — mỗi chương 1-2 LLM turn, ghi 1-2 lần là hoàn toàn chấp nhận được;
// dù người dùng thoát bằng ctrl+C không kịp kích hoạt timer, nhánh hủy ctx cũng sẽ flush lần cuối.
// Trường hợp crash thực sự (OS kill -9) sẽ mất tích lũy trong 0.5s gần nhất —
// session jsonl upstream vẫn là sự thật đầy đủ, lần khởi động sau sẽ replay từ sessions/ để bù đắp chênh lệch.
func (t *UsageTracker) autoSaveLoop(ctx context.Context) {
	const debounce = 500 * time.Millisecond
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	var pending bool
	flush := func() {
		if err := t.SaveNow(); err != nil {
			slog.Warn("Ghi usage xuống đĩa thất bại", "module", "usage", "err", err)
		}
		pending = false
	}
	for {
		select {
		case <-ctx.Done():
			if pending {
				flush()
			}
			return
		case <-t.saveCh:
			if pending {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			timer.Reset(debounce)
			pending = true
		case <-timer.C:
			flush()
		}
	}
}

// applyState ghi snapshot đã lưu trở lại bộ nhớ. Chỉ gọi lúc khởi động (LoadFromStore / sau replay),
// lúc đó autoSaveLoop chưa chạy và Record chưa đa luồng, không cần lock; nhưng vẫn giữ mu
// phòng test hoặc thứ tự gọi tương lai thay đổi gây ra đa luồng.
func (t *UsageTracker) applyState(state domain.UsageState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.overall = totalsFromState(state.Overall)
	if state.PerAgent == nil {
		t.perAgent = make(map[string]*agentTotals, 4)
	} else {
		t.perAgent = make(map[string]*agentTotals, len(state.PerAgent))
		for role, v := range state.PerAgent {
			tot := totalsFromState(v)
			t.perAgent[role] = &tot
		}
	}
	if state.PerModel == nil {
		t.perModel = make(map[string]*agentTotals, 4)
	} else {
		t.perModel = make(map[string]*agentTotals, len(state.PerModel))
		for model, v := range state.PerModel {
			tot := totalsFromState(v)
			t.perModel[model] = &tot
		}
	}
	t.missingAssistantUsage = state.MissingUsage
}

// totalsSnapshot sao chép agentTotals trong bộ nhớ thành domain.AgentUsageTotals có thể lưu trữ.
// Ring buffer samples cố tình không đưa ra ngoài — xem chú thích UsageState.
func totalsSnapshot(t *agentTotals) domain.AgentUsageTotals {
	if t == nil {
		return domain.AgentUsageTotals{}
	}
	return domain.AgentUsageTotals{
		Input:        t.Input,
		Output:       t.Output,
		CacheRead:    t.CacheRead,
		CacheWrite:   t.CacheWrite,
		Cost:         t.Cost,
		Saved:        t.Saved,
		CacheCapable: t.CacheCapable,
	}
}

// totalsFromState khôi phục dạng lưu trữ thành agentTotals trong bộ nhớ. samples để trống,
// sau khi khởi động lại sẽ tích lũy từ đầu, sau vài lần Record là phục hồi ngữ nghĩa "tỉ lệ hit N lần gần nhất".
func totalsFromState(s domain.AgentUsageTotals) agentTotals {
	return agentTotals{
		Input:        s.Input,
		Output:       s.Output,
		CacheRead:    s.CacheRead,
		CacheWrite:   s.CacheWrite,
		Cost:         s.Cost,
		Saved:        s.Saved,
		CacheCapable: s.CacheCapable,
	}
}

// AgentUsage là snapshot lượng sử dụng tích lũy của một agent (hiển thị cho UI).
type AgentUsage struct {
	Role            string
	Model           string
	Input           int
	Output          int
	CacheRead       int
	CacheWrite      int
	Cost            float64
	Saved           float64
	CacheCapable    bool
	RecentCacheRead int
	RecentInput     int
	RecentSamples   int
}

// PerAgent trả về lượng sử dụng tích lũy của từng role. Kết quả sắp xếp giảm dần theo CacheRead; role chưa tiêu thụ token nào sẽ bị bỏ qua.
func (t *UsageTracker) PerAgent() []AgentUsage {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]AgentUsage, 0, len(t.perAgent))
	for role, v := range t.perAgent {
		if v.Input == 0 && v.Output == 0 {
			continue
		}
		recentRead, recentInput := recentSums(v)
		out = append(out, AgentUsage{
			Role:            role,
			Input:           v.Input,
			Output:          v.Output,
			CacheRead:       v.CacheRead,
			CacheWrite:      v.CacheWrite,
			Cost:            v.Cost,
			Saved:           v.Saved,
			CacheCapable:    v.CacheCapable,
			RecentCacheRead: recentRead,
			RecentInput:     recentInput,
			RecentSamples:   len(v.samples),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CacheRead != out[j].CacheRead {
			return out[i].CacheRead > out[j].CacheRead
		}
		return out[i].Input > out[j].Input
	})
	return out
}

// PerModel trả về lượng sử dụng tích lũy của từng model. Kết quả sắp xếp giảm dần theo chi phí, sau đó theo lượng input.
func (t *UsageTracker) PerModel() []AgentUsage {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]AgentUsage, 0, len(t.perModel))
	for model, v := range t.perModel {
		if v.Input == 0 && v.Output == 0 {
			continue
		}
		out = append(out, AgentUsage{
			Model:        model,
			Input:        v.Input,
			Output:       v.Output,
			CacheRead:    v.CacheRead,
			CacheWrite:   v.CacheWrite,
			Cost:         v.Cost,
			Saved:        v.Saved,
			CacheCapable: v.CacheCapable,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Input > out[j].Input
	})
	return out
}

// resolveCost trả về đồng thời cost / saved / capable của tin nhắn này.
//   - cost: tính theo 4 hạng mục nếu registry có model; nếu không thì fallback về cost do nhà cung cấp trả
//   - saved: chỉ > 0 khi registry có model, CacheRead > 0 và InputCost > CacheReadCost
//   - capable: registry có model và CacheReadCostPer1M > 0 → đã biết hỗ trợ prompt caching
//
// modelName ưu tiên dùng giá trị bên gọi truyền vào (khi replay lấy từ _meta.model trong session jsonl).
func (t *UsageTracker) resolveCost(modelName string, u agentcore.Usage) (cost, saved float64, capable bool) {
	if entry, ok := models.DefaultRegistry().Resolve(modelName); ok {
		c := computeCost(u, *entry)
		s := computeSaved(u, *entry)
		canCache := entry.CacheReadCostPer1M > 0
		if c > 0 {
			return c, s, canCache
		}
	}
	if u.Cost != nil {
		return u.Cost.Total, 0, false
	}
	return 0, 0, false
}

// agentRoleName quy chuẩn tên subagent thành tên role.
// architect_short/mid/long đều quy về architect; các tên khác giữ nguyên.
func agentRoleName(agentName string) string {
	if strings.HasPrefix(agentName, "architect_") {
		return "architect"
	}
	return agentName
}

// computeCost tính chi phí USD của một lần gọi theo đơn giá $/1M token.
//
// Tiền đề ngữ nghĩa (được đảm bảo thống nhất bởi các adapter litellm của từng provider,
// xem điểm lắp ráp Usage trong anthropic.go / bedrock.go / openai.go / gemini.go / compat.go):
//
//	u.Input  = toàn bộ token đầu vào, **bao gồm** CacheRead; không bao gồm CacheWrite
//	u.Output = token đầu ra
//
// Do đó nonCachedInput = u.Input - u.CacheRead đúng với mọi nhà cung cấp.
// Nhánh dự phòng giữ lại để phòng trường hợp provider nào đó trả dữ liệu sai trong tương lai mà không crash.
func computeCost(u agentcore.Usage, e models.ModelEntry) float64 {
	nonCachedInput := u.Input - u.CacheRead
	if nonCachedInput < 0 {
		nonCachedInput = u.Input
	}
	c := 0.0
	c += float64(nonCachedInput) * e.InputCostPer1M / 1_000_000
	c += float64(u.Output) * e.OutputCostPer1M / 1_000_000
	c += float64(u.CacheRead) * e.CacheReadCostPer1M / 1_000_000
	c += float64(u.CacheWrite) * e.CacheWriteCostPer1M / 1_000_000
	return c
}

// computeSaved ước tính USD tiết kiệm được từ CacheRead hit so với "tính theo giá input thông thường".
// Lưu ý: phí thặng dư của CacheWrite không được khấu trừ — đó là chi phí cần thiết để dọn đường
// cho các hit sau, lợi nhuận thực tế được thu hồi dần qua CacheRead tích lũy.
func computeSaved(u agentcore.Usage, e models.ModelEntry) float64 {
	if u.CacheRead <= 0 || e.InputCostPer1M <= 0 {
		return 0
	}
	delta := e.InputCostPer1M - e.CacheReadCostPer1M
	if delta <= 0 {
		return 0
	}
	return float64(u.CacheRead) * delta / 1_000_000
}
