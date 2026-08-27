package host

import (
	"fmt"
	"math"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
)

// Máy trạng thái ngân sách: tiến một chiều, mỗi lần chuyển trạng thái kích hoạt đúng một tác dụng phụ, không lùi.
// Tăng ngân sách = người dùng tái ủy quyền = khởi động lại sau khi đổi cấu hình / Host instance mới, không hoàn trạng thái trong instance này.
const (
	budgetNormal      int32 = iota // Chưa đến ngưỡng cảnh báo
	budgetWarned                   // Đã cảnh báo, chưa vượt ngưỡng
	budgetStopPending              // Đã vượt ngưỡng, chờ dừng tại ranh giới agent phụ
	budgetStopped                  // Đã thực thi dừng máy
)

// BudgetSentinel giám sát chi phí tích lũy, thực thi chính sách ngân sách của người dùng (khối config budget).
//
// Định vị kiến trúc (architecture.md §8.3/§10): không đánh giá hành vi mô hình — dừng khi vượt ngưỡng
// tương đương người dùng Abort thủ công tại thời điểm đó, Host chỉ thực thi một lệnh đã được ký trước.
// Nó ảnh hưởng đến luồng điều khiển nên không phải observer, được định vị ngang hàng với flow.Dispatcher
// là thành phần chính sách Host; tầng Route/công cụ không biết đến nó.
//
// Thời điểm dừng: mặc định tại ranh giới agent phụ (HandleEvent lắng nghe EventToolExecEnd(tool=subagent),
// cùng điểm kích hoạt với Dispatcher), không lãng phí chương đang xử lý; khi hardStop=true thì dừng ngay khi vượt ngưỡng.
// Ràng buộc thứ tự đăng ký: Sentinel phải đăng ký trước Dispatcher — sau khi Abort được đặt,
// FollowUp của Dispatcher tự nhiên thất bại, không cần thêm nhận thức ngân sách vào tầng định tuyến.
type BudgetSentinel struct {
	limit     float64
	warnRatio float64
	hardStop  bool

	costNow func() float64              // Chi phí tích lũy hiện tại (gói usage.Totals; có thể inject stub để test)
	abort   func(reason string)         // Wrapper dừng Host (kèm sự kiện lý do)
	report  func(level, summary string) // Kênh xuất cảnh báo (emitEvent + notify, được inject bởi Host)

	state atomic.Int32

	// Phát hiện vùng mù tính phí: mô hình không có giá trong registry và provider không tự báo cost
	// thì mỗi lần ghi phí tăng thêm $0, ngân sách âm thầm vô hiệu. Phát hiện bằng "nhiều lần tăng
	// liên tiếp bằng 0" thay vì total==0 — cách sau không bắt được trường hợp giữa chừng dùng /model
	// chuyển sang mô hình không có giá (total dừng ở giá trị lịch sử khác 0 nhưng không tăng nữa).
	// Mô hình miễn phí cũng rơi vào đây, thông báo "ngân sách sẽ không kích hoạt" áp dụng cho chúng như nhau.
	lastTotal   atomic.Uint64 // math.Float64bits(chi phí tích lũy lần callback trước)
	zeroStreak  atomic.Int32
	blindWarned atomic.Bool
}

// blindZeroStreak là số lần ghi phí tăng bằng 0 liên tiếp trước khi cảnh báo. Mô hình tính phí bình thường
// mỗi lần tăng phải > 0 (cost là float tích lũy không làm tròn), lấy 5 chỉ để tránh nhiễu cực đoan,
// không phải ngưỡng có thể điều chỉnh theo chính sách.
const blindZeroStreak = 5

// NewBudgetSentinel tạo BudgetSentinel; trả về nil khi chính sách chưa được bật (tất cả method đều an toàn với nil).
func NewBudgetSentinel(cfg bootstrap.BudgetConfig, costNow func() float64, abort func(reason string), report func(level, summary string)) *BudgetSentinel {
	if !cfg.Enabled() {
		return nil
	}
	return &BudgetSentinel{
		limit:     cfg.BookUSD,
		warnRatio: cfg.WarnRatio,
		hardStop:  cfg.HardStop,
		costNow:   costNow,
		abort:     abort,
		report:    report,
	}
}

// OnCost được UsageTracker gọi sau mỗi lần ghi phí, truyền vào chi phí tích lũy mới nhất (ngoài lock).
// Một lần callback có thể vượt qua hai mức (normal→warned→stopPending), hai tác dụng phụ đều được kích hoạt.
func (s *BudgetSentinel) OnCost(total float64) {
	if s == nil {
		return
	}
	if prev := s.lastTotal.Swap(math.Float64bits(total)); total == math.Float64frombits(prev) {
		if s.zeroStreak.Add(1) >= blindZeroStreak && s.blindWarned.CompareAndSwap(false, true) {
			s.report("warn", fmt.Sprintf("Vùng mù ngân sách: liên tục ghi phí nhưng chi phí tích lũy dừng ở $%.2f không tăng nữa (mô hình hiện tại không có giá trong registry và provider không tự báo cost, hoặc là mô hình miễn phí) — ngân sách sẽ không kích hoạt", total))
		}
	} else {
		s.zeroStreak.Store(0)
	}
	if total >= s.limit*s.warnRatio && s.state.CompareAndSwap(budgetNormal, budgetWarned) {
		s.report("warn", fmt.Sprintf("Cảnh báo ngân sách: đã chi $%.2f, đạt %.0f%% ngân sách $%.2f", total, s.warnRatio*100, s.limit))
	}
	if total >= s.limit && s.state.CompareAndSwap(budgetWarned, budgetStopPending) {
		if s.hardStop {
			s.report("error", fmt.Sprintf("Hết ngân sách: đã chi $%.2f, vượt ngân sách $%.2f, dừng ngay", total, s.limit))
			s.stop(total)
			return
		}
		s.report("error", fmt.Sprintf("Hết ngân sách: đã chi $%.2f, vượt ngân sách $%.2f, sẽ dừng sau khi agent phụ hiện tại hoàn thành", total, s.limit))
	}
}

// HandleEvent thực thi lệnh dừng đang chờ tại ranh giới agent phụ. Phải đăng ký trước Dispatcher.
// Không bỏ qua IsError — lỗi trả về cũng là ranh giới, không nên trì hoãn dừng vì agent phụ thất bại.
func (s *BudgetSentinel) HandleEvent(ev agentcore.Event) {
	if s == nil {
		return
	}
	if ev.Type != agentcore.EventToolExecEnd || ev.Tool != "subagent" {
		return
	}
	if s.state.Load() != budgetStopPending {
		return
	}
	s.stop(s.costNow())
}

func (s *BudgetSentinel) stop(total float64) {
	if s.state.CompareAndSwap(budgetStopPending, budgetStopped) {
		s.abort(fmt.Sprintf("Dừng do hết ngân sách: đã chi $%.2f, vượt ngân sách $%.2f; tăng budget.book_usd trong cấu hình để tiếp tục", total, s.limit))
	}
}

// Refuse kiểm tra trước khi khởi động: trả về lỗi từ chối nếu ngân sách đã cạn (được gọi ở đường phục hồi Start/Resume/Continue).
// Người dùng tăng ngân sách = tái ủy quyền, với cấu hình mới Refuse sẽ tự nhiên cho phép.
func (s *BudgetSentinel) Refuse() error {
	if s == nil {
		return nil
	}
	if cost := s.costNow(); cost >= s.limit {
		return fmt.Errorf("cuốn sách này đã chi $%.2f, đạt giới hạn ngân sách $%.2f; hãy tăng budget.book_usd trong cấu hình rồi thử lại", cost, s.limit)
	}
	return nil
}

// Limit trả về giới hạn ngân sách (dùng để hiển thị trên TUI); trả về 0 nếu chưa bật.
func (s *BudgetSentinel) Limit() float64 {
	if s == nil {
		return 0
	}
	return s.limit
}
