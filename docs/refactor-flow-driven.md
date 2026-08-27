# Đề xuất Tái cấu trúc: Hybrid Coordinator — Host Routing × LLM Phán xét

> Trạng thái: **Đã được chấp thuận và triển khai** (2026-04-20)
> Thời gian nghiên cứu: 2026-04-20
> Tài liệu hiện hành liên quan: `docs/architecture.md` §2 / §3 / §7 / §8 / §13 đã đồng bộ cập nhật
>
> **Đây là bản thảo thứ hai.** Vấn đề của bản thảo đầu tiên (phương án triệt để — xóa hoàn toàn Coordinator) được trình bày tại Phụ lục A, giữ lại mục này để tránh đi lại vết xe đổ.
>
> Kết quả triển khai:
> - `internal/host/flow/` được tạo mới (router.go / state.go / dispatcher.go / router_test.go, 15 nhánh unit test đều pass)
> - `internal/host/reminder/` xóa `flow.go` / `queue_guard.go` / `book_complete.go`; giữ lại StopGuard và Guard của agent phụ
> - `assets/prompts/coordinator.md` giảm từ 88 dòng xuống ~45 dòng (trách nhiệm thu hẹp về thực thi lệnh của Host + phán xét + chọn loại khởi động)
> - `internal/host/resume.go` đơn giản hóa đáng kể, chỉ tạo label và prompt ngắn, bước tiếp theo cụ thể do Router phái xuất sau TurnEnd đầu tiên
> - `internal/store/` bổ sung các phương thức hỗ trợ `HasArcReview` / `HasArcSummary` / `HasVolumeSummary` / `CheckConsistency`
> - Bug `observer.go` khiến trạng thái agent bị kẹt ở working đã được sửa đồng thời

---

## 1. Bối cảnh

### 1.1 Định vị dự án

```
agentcore       — framework agent đa dụng
litellm         — gateway LLM đa dụng
ainovel-cli     — agent chuyên viết tiểu thuyết (dự án này)
```

Không gian quyết định của agent chuyên ngành là **đóng**: lưu đồ cố định, nhánh hữu hạn, dựa trên sự kiện thực tế. Triết lý thiết kế của agent đa dụng ("đặt cược vào năng lực mô hình") khi áp vào kịch bản chuyên ngành có phần thuần túy quá mức.

### 1.2 Mục tiêu người dùng (theo mức ưu tiên)

1. **Ổn định** — viết liên tục không bị gián đoạn do lỗi định tuyến
2. **Hưởng lợi từ LLM nâng cấp** — kiến trúc không đối kháng với năng lực mô hình
3. **Tận dụng tối đa năng lực đa agent** — phân công chức năng rõ ràng

Đề xuất này thực hiện **cải tiến Pareto** giữa ba mục tiêu (không hy sinh mục tiêu nào để đổi lấy mục tiêu khác).

---

## 2. Nghiên cứu hiện trạng

### 2.1 Phân loại các điểm quyết định của Coordinator

Trích xuất từng điểm quyết định trong `coordinator.md`:

| # | Điểm quyết định | Tính chất | Tần suất |
|---|---|---|---|
| 1 | Khởi động chọn architect_long / short | Phán xét (hiểu ngữ nghĩa) | 1 lần/cuốn |
| 2 | Mở rộng đầu vào (<20 chữ tự động bổ sung) | Phán xét (sáng tạo) | 0-1 lần/cuốn |
| 3 | Vòng lặp bổ sung quy hoạch | Định tuyến (dựa trên sự kiện) | 1-3 lần |
| 4 | Bước tiếp theo sau khi lưu chương | **Định tuyến** | **1-2 lần/chương** |
| 5 | Thực thi từng bước đánh giá cuối cung truyện | Định tuyến | 3-5 lần/cung truyện |
| 6 | Phân nhánh verdict đánh giá | Định tuyến (đã code hóa, xem §2.3) | 1 lần/cung truyện |
| 7 | Xử lý can thiệp người dùng | Phán xét (bắt buộc LLM) | Tùy ý |
| 8 | Phái lại khi agent phụ báo lỗi | Định tuyến | Thỉnh thoảng |
| 9 | Xuất tổng kết khi hoàn thành toàn bộ cuốn | Định tuyến | 1 lần |

**Kết luận**: Trong 9 điểm quyết định có 6 điểm là định tuyến thuần túy (tra bảng), 3 điểm mới thực sự cần LLM phán xét. **Tần suất định tuyến cao hơn nhiều so với phán xét** (1-2 lần/chương vs vài lần/cuốn).

### 2.2 Kênh Reminder đã là nửa thành phẩm của việc code hóa quy trình

Các bộ tạo trong `internal/host/reminder/` mỗi vòng dựa trên sự kiện thực tế để tạo **lệnh cụ thể đến mức hành động**:

- `flow.go` → `"flow hiện tại=writing, next_chapter=37. Hãy trực tiếp gọi subagent(writer, \"viết chương 37\")..."`
- `queue_guard.go` → `"flow hiện tại=rewriting, hàng đợi chờ xử lý: [3,5]. Hãy ngay lập tức gọi writer để viết lại từng chương..."`
- `book_complete.go` → `"Toàn bộ cuốn đã hoàn thành. Hãy xuất tổng kết toàn sách..."`

**Kiến trúc hiện tại tồn tại double dispatch**:
```
Lớp quy tắc: coordinator.md định nghĩa "nếu A thì B"
  ↓
Lớp Reminder: mỗi vòng dựa trên sự kiện cụ thể hóa quy tắc → sinh ra "bây giờ hãy làm B"
  ↓
Lớp LLM: đọc reminder sinh ra tool_call (về cơ bản chỉ là đọc lại reminder)
  ↓
SubAgent thực thi
```

**LLM thực chất chỉ đang "thực thi" lệnh mà Reminder đã đưa ra**. Vòng trung gian này vừa tốn token, vừa đưa vào sự không chắc chắn (LLM có thể không hoàn toàn tuân theo reminder, ví dụ lỗi định tuyến mid đã quan sát được).

### 2.3 Lớp công cụ đã đảm nhận phần lớn phán xét

- `save_review.evaluateScorecardGate()`: cổng kiểm soát bảng điểm, tự động nâng cấp accept lên polish/rewrite
- `save_review.ContractStatus` kiểm tra: contract=missed tự động nâng cấp thành rewrite
- `commit_chapter.CheckArcBoundary()`: tính ngay lập tức `arc_end / needs_expansion / needs_new_volume`
- `commit_chapter.applyCompletion()`: phán định ngay lập tức `book_complete`
- `CommitResult` trả về 17 trường sự kiện thực tế

**Kết luận**: Lớp công cụ đã code hóa phần lớn "phán xét", các quyết định Coordinator đưa ra dựa trên những sự kiện này về cơ bản chỉ là if-else.

### 2.4 Chi phí thực tế của hiện trạng

Mỗi chương vòng lặp LLM của Coordinator:
- **1-2 turns/chương** (đọc system prompt ~3000 token + reminder ~200 token + lịch sử + CommitResult ~500 token → sinh tool_call ~50 token)
- Tiểu thuyết dài 200 chương khoảng **200-400 turns** gọi LLM của Coordinator
- Trong đó **~90% là định tuyến thuần túy** (LLM đọc lại reminder), **~10% là phán xét**

**Mỗi chương ~3500-7000 token chi cho quyết định của Coordinator, 95% là dư thừa** (Reminder đã tính ra câu trả lời).

---

## 3. Phương án thiết kế: Hybrid Coordinator

### 3.1 Tư tưởng cốt lõi

**Chuyển quyết định quy trình từ LLM sang Host, nhưng giữ lại Coordinator làm nút phán xét và kênh thực thi lệnh**.

```
┌──────────────────────────────────────────────────────────┐
│                   Entry (TUI / headless)                   │
└────────────────────────────────┬─────────────────────────┘
                                 │ Start / Resume / Steer
┌────────────────────────────────▼─────────────────────────┐
│                            Host                            │
│                                                             │
│   ┌──────────────────────────────────────────────────┐     │
│   │  Flow Router (thêm mới — cốt lõi)                 │     │
│   │  ───────────                                      │     │
│   │  Đăng ký sự kiện Coordinator: kích hoạt khi       │     │
│   │  subagent tool trả về                             │     │
│   │  Hàm thuần túy: route(Progress, Checkpoint,       │     │
│   │      Boundary) → NextInstruction                  │     │
│   │  Có lệnh → coordinator.FollowUp(lệnh)             │     │
│   │  Không có lệnh (kịch bản phán xét) → không        │     │
│   │      can thiệp, để LLM tự chủ                     │     │
│   └──────────────────────────────────────────────────┘     │
│                                                             │
│   Giữ lại: lifecycle API / Observer / Usage Tracker         │
│   Giữ lại: resume.go (đơn giản hóa, logic cốt lõi không đổi)│
└────────────────────────────────┬─────────────────────────┘
                                 │
┌────────────────────────────────▼─────────────────────────┐
│                    Coordinator Agent (LLM)                  │
│                                                             │
│   Trách nhiệm thu hẹp về hai loại:                          │
│   1. Nhận lệnh Host FollowUp → sinh tool_call tương ứng    │
│   2. Khi Steer của người dùng đến, tự chủ phán xét         │
│      (truy vấn/đánh giá sửa đổi)                           │
│                                                             │
│   coordinator.md: 88 dòng → ~25 dòng                        │
│   MaxTurns: 1000 giữ nguyên (phản hồi steer + thực thi     │
│      lệnh Host)                                             │
└────────────────────────────────┬─────────────────────────┘
                                 │
                                 ▼
         ┌──────────────────────┼───────────────────────┐
         ▼                      ▼                       ▼
    ┌────────┐             ┌────────┐             ┌────────┐
    │Architect│             │ Writer │             │ Editor │
    └────────┘             └────────┘             └────────┘
```

### 3.2 Phân chia lại trách nhiệm

| Lớp | Làm gì | Không làm gì |
|---|---|---|
| **Host / Flow Router** | Đọc sự kiện → định tuyến thuần túy → lệnh FollowUp | Tự gọi SubAgent (vẫn qua Coordinator) |
| **Coordinator** | Thực thi lệnh Host + phán xét can thiệp người dùng + chọn kiến trúc sư khi khởi động | Tự quyết định "bước tiếp theo làm gì" |
| **SubAgent (A/W/E)** | Công việc chức năng của từng agent | Không thay đổi |
| **Lớp công cụ** | Ghi nguyên tử + trả về sự kiện thực tế | Không thay đổi |

**Tính bất biến quan trọng**:
- ✅ Coordinator vẫn là một agent run liên tục, giữ "cảm nhận toàn sách" xuyên suốt
- ✅ Steer của người dùng vẫn qua `coordinator.Inject()`, khả năng ngắt lập tức được giữ nguyên
- ✅ SubAgentTool vẫn do LLM gọi (đi theo đường dẫn gốc của agentcore), luồng sự kiện / ContextManager / chuyển đổi mô hình đều không thay đổi
- ✅ agentcore không sửa đổi gì

### 3.3 Logic cụ thể của Flow Router

```go
// internal/host/flow/router.go

type NextInstruction struct {
    Agent  string   // architect_long / architect_short / writer / editor
    Task   string   // mô tả nhiệm vụ cho agent phụ
    Reason string   // lý do cho Coordinator xem (tùy chọn, tiện debug)
}

type RouterState struct {
    Progress        *domain.Progress
    LatestCheckpoint *domain.Checkpoint
    // Biên giới cung truyện trong chế độ phân lớp (tính khi chương cuối đã hoàn thành)
    LastCompleted   int
    ArcBoundary     *store.ArcBoundary
    HasArcReview    bool
    HasArcSummary   bool
    // Thiếu thiết lập cơ bản
    FoundationMissing []string
}

// Route trả về lệnh bước tiếp theo. Trả về nil nghĩa là để Coordinator tự phán xét (kịch bản phán xét).
func Route(s RouterState) *NextInstruction {
    p := s.Progress

    // 0. Trạng thái cuối: để LLM xuất tổng kết, không định tuyến
    if p.Phase == domain.PhaseComplete {
        return nil
    }

    // 1. Giai đoạn quy hoạch: phán xét (chọn kiến trúc sư) do LLM làm, không định tuyến
    if p.Phase != domain.PhaseWriting {
        return nil
    }

    // 2. Giai đoạn viết
    // 2a. Ưu tiên hàng đợi viết lại/đánh bóng
    if len(p.PendingRewrites) > 0 {
        ch := p.PendingRewrites[0]
        verb := "viết lại"
        if p.Flow == domain.FlowPolishing {
            verb = "đánh bóng"
        }
        return &NextInstruction{
            Agent:  "writer",
            Task:   fmt.Sprintf("%s chương %d", verb, ch),
            Reason: fmt.Sprintf("Hàng đợi PendingRewrites còn %d chương", len(p.PendingRewrites)),
        }
    }

    // 2b. Đang đánh giá: không định tuyến, để Coordinator đi theo nhánh verdict từ kết quả save_review
    if p.Flow == domain.FlowReviewing {
        return nil
    }

    // 2c. Xử lý sau cuối cung truyện trong chế độ phân lớp
    if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
        b := s.ArcBoundary
        if !s.HasArcReview {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("Đánh giá cấp cung truyện cho tập %d cung truyện %d", b.Volume, b.Arc),
                Reason: "Đánh giá cuối cung truyện chưa hoàn thành",
            }
        }
        if !s.HasArcSummary {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("Tạo tóm tắt tập %d cung truyện %d", b.Volume, b.Arc),
                Reason: "Tóm tắt cung truyện chưa hoàn thành",
            }
        }
        if b.NeedsExpansion {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   fmt.Sprintf("Mở rộng tập %d cung truyện %d (save_foundation type=expand_arc)", b.NextVolume, b.NextArc),
                Reason: "Khung cung truyện tiếp theo chờ mở rộng",
            }
        }
        if b.NeedsNewVolume {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   "Đánh giá và thực thi save_foundation(type=append_volume) hoặc mark_final",
                Reason: "Kết tập cần quyết định thêm tập mới",
            }
        }
    }

    // 2d. Tiếp tục viết bình thường
    next := p.NextChapter()
    return &NextInstruction{
        Agent:  "writer",
        Task:   fmt.Sprintf("Viết chương %d", next),
        Reason: "Tiếp tục viết",
    }
}
```

**Đặc tính của hàm**:
- Hàm thuần túy (đầu vào RouterState, đầu ra NextInstruction)
- Có thể viết unit test (cho trạng thái xác định, khẳng định kết quả định tuyến)
- **Trả về nil là hợp lệ** — có nghĩa là "đây là kịch bản phán xét, hãy để LLM tự chủ"

### 3.4 Thời điểm kích hoạt

Host đăng ký sự kiện `agentcore.EventToolExecEnd`:

```go
coordinator.Subscribe(func(ev agentcore.Event) {
    if ev.Type == agentcore.EventToolExecEnd && ev.Tool == "subagent" && !ev.IsError {
        // SubAgent vừa trả về → đọc trạng thái mới nhất → định tuyến
        h.flowRouter.Dispatch()
    }
})
```

```go
func (r *FlowRouter) Dispatch() {
    state := r.loadState()
    instruction := Route(state)
    if instruction == nil {
        return // kịch bản phán xét, để LLM tự chủ
    }
    msg := formatInstruction(instruction)
    _ = r.coordinator.FollowUp(agentcore.UserMsg(msg))
}

func formatInstruction(i *NextInstruction) string {
    return fmt.Sprintf(
        "[Host hạ lệnh] Bước tiếp theo: gọi subagent(%s, %q)\n"+
        "Lý do: %s\n"+
        "Đây là lệnh rõ ràng từ lớp quy trình, hãy thực thi ngay lập tức, không gọi novel_context trước, không xuất suy luận trước.",
        i.Agent, i.Task, i.Reason,
    )
}
```

### 3.5 Tính phản hồi và đồng thời

**Đường dẫn Steer của người dùng** (không thay đổi):
```
Steer → coordinator.Inject(UserMsg("[can thiệp người dùng] xxx"))
```

- Đang chạy: tin nhắn được chèn vào hàng đợi run hiện tại
- Idle: resume run
- Paused: xếp hàng

**Đồng thời giữa lệnh định tuyến + Steer**:
- Đều đi vào hàng đợi tin nhắn của Coordinator, được xử lý theo thứ tự gốc của agentcore
- Nếu Host vừa gửi `FollowUp("[Host lệnh] viết chương 37")`, ngay sau đó người dùng Steer `"Dừng lại, điều chỉnh phong cách"`
  - Coordinator xử lý lệnh Host trước? hay xử lý Steer trước?
  - **Ngữ nghĩa của `Inject` là chen lên đầu hàng đợi hiện tại**, nên Steer được xử lý trước
  - Đây là hành vi mong muốn: ưu tiên can thiệp người dùng cao hơn lịch trình thường lệ của Host

**Tránh xung đột giữa lệnh Host và Steer**:
- Flow Router **tạm dừng ngắn** vài turn sau khi nhận tín hiệu "Steer đã được chèn" (để Coordinator xử lý xong Steer rồi mới định tuyến)
- Cảm nhận kết quả xử lý Steer thông qua đăng ký `agentcore.EventMessageEnd` + kiểm tra thay đổi trạng thái Progress

### 3.6 Ví dụ đơn giản hóa coordinator.md

Cắt từ 88 dòng xuống còn khoảng 25 dòng:

```markdown
Bạn là tổng Điều phối viên sáng tác tiểu thuyết.

## Chế độ làm việc của bạn

**Luồng chính**: Host sẽ gửi tin nhắn `[Host hạ lệnh]` sau mỗi lần agent phụ trả về, cho bạn biết bước tiếp theo gọi agent phụ nào làm gì. Nhận lệnh xong ngay lập tức tạo tool_call tương ứng, không gọi novel_context suy luận trước, không đọc lại.

**Phán xét**: Khi gặp những tình huống sau bạn cần tự chủ phán xét (Host sẽ không hạ lệnh, bạn phải chủ động hành động):

### Khi khởi động: Chọn kiến trúc sư

- Mặc định → `architect_long`
- Chỉ khi người dùng yêu cầu rõ ràng truyện ngắn/đơn tập/tiểu phẩm và giới hạn trong 25 chương → `architect_short`

Nếu đầu vào người dùng < 20 chữ, hãy bổ sung hướng khác biệt, độc giả mục tiêu, ít nhất một hook câu chuyện độc đáo vào mô tả task trước khi phái xuất.

### Steer của người dùng

Định dạng: `[can thiệp người dùng] xxx`

- **Loại truy vấn** (hỏi trạng thái/thiết lập): Trả lời trực tiếp bằng văn bản, không cần gọi thêm công cụ; Host sẽ tiếp tục phái xuất.
- **Loại sửa đổi** (yêu cầu đổi thiết lập/viết lại/điều chỉnh phong cách): Đánh giá phạm vi ảnh hưởng:
  - Liên quan thay đổi thiết lập → gọi architect_* làm `save_foundation(type=...)`
  - Liên quan chương đã viết → để công cụ tự động ghi chương mục tiêu vào `PendingRewrites` (có thể ghi rõ ý định viết lại khi gọi writer lần sau)
  - Chỉ ảnh hưởng phong cách về sau → mô tả ngắn gọn yêu cầu, rồi đính kèm vào mô tả task của writer khi nhận lệnh Host tiếp theo

## Công cụ

- `subagent(agent, task)`: gọi agent phụ
- `novel_context`: chỉ dùng khi người dùng truy vấn cần thiết, không gọi sau khi lệnh Host đến

## Agent phụ

- `architect_long` / `architect_short` / `writer` / `editor`

## Cấm

- Gọi novel_context trước khi hành động khi lệnh Host đến
- Tự quyết định bước tiếp theo khi không có Steer người dùng và không có lệnh Host
```

### 3.7 Kênh Reminder thu gọn đáng kể

**Xóa bỏ**:
- `flow.go` (Host FollowUp đã hạ lệnh cụ thể, lời nhắc định tuyến của Reminder mất giá trị)
- `queue_guard.go` (hàng đợi được Host Router đảm bảo)
- `book_complete.go` (Host FollowUp lệnh xuất tổng kết khi Phase=Complete)

**Giữ lại**:
- `subagent_guards.go` (StopGuard cho Writer/Architect/Editor, đảm bảo agent phụ không kết thúc tay không)
- Bổ sung một `foundation_reminder.go` nhẹ: thông báo cho Coordinator những thiếu sót trong giai đoạn quy hoạch (đây là **thông tin cần cho phán xét** chứ không phải lệnh định tuyến)

**StopGuard được giữ lại**:
- StopGuard của Coordinator được giữ lại (chặn end_turn khi `Phase != Complete` làm lưới an toàn cuối)
- Bổ sung nhắc nhở khi "nhận lệnh Host nhưng chưa gọi subagent tương ứng trong lượt này"

### 3.8 Đơn giản hóa nhẹ resume.go

Hiện tại `buildResumePrompt` sinh lệnh ngôn ngữ tự nhiên chính xác đến từng bước theo điểm khôi phục (121 dòng).

Kiến trúc mới:
- Khi Resume đọc Progress trước, Flow Router tính ra `NextInstruction`
- Coordinator nhận một prompt resume **rất ngắn**, sau đó chờ lệnh FollowUp của Host

```
[Khôi phục] Cuốn sách "xxx" đã hoàn thành N chương, đang ở giai đoạn XX.
Hãy chờ lệnh tiếp theo từ Host, hoặc xử lý can thiệp người dùng có thể còn lại từ lúc dừng.
```

Hầu hết logic nhánh được chuyển xuống Flow Router (Router vốn đã định tuyến theo trạng thái, Resume không cần đường dẫn đặc biệt).

---

## 4. Đánh giá mức độ đạt mục tiêu

### 4.1 Tính ổn định

| Rủi ro | Hiện tại | Kiến trúc mới |
|---|---|---|
| Coordinator chọn sai architect | Đã xảy ra (lỗi định tuyến mid) | Khởi động vẫn là phán xét, nhưng prompt từ 3 lựa chọn thành 2 ngôi (đã làm), diện lỗi thu hẹp đáng kể |
| Coordinator không tuân thủ "chỉ nói viết chương N" | Đã xảy ra | Host hạ lệnh theo định dạng cố định, không cần LLM tạo mô tả task nữa |
| Coordinator bỏ sót kiểm tra queue_drained | Đã xảy ra | Host Router ép đi theo thứ tự |
| Coordinator quên gọi editor sau khi lưu chương cuối cung truyện | Có thể xảy ra | Host Router phát hiện IsArcEnd && !HasArcReview rồi phái xuất trực tiếp |
| Thiếu nhánh khôi phục sau crash | Đã biết lỗ hổng | State machine của Flow Router tự nhiên bao phủ mọi nhánh |
| StopGuard chặn liên tiếp 5 lần nâng cấp fatal | Tồn tại | Sau khi lệnh Host rõ ràng, LLM khó bị chặn liên tiếp (trừ khi prompt hỏng nặng) |

### 4.2 Lợi nhuận từ nâng cấp LLM

| Chiều | Mức giữ lại |
|---|---|
| Nâng cấp mô hình Writer → chất lượng viết | 100% |
| Nâng cấp mô hình Editor → độ chính xác đánh giá | 100% |
| Nâng cấp mô hình Architect → quy hoạch tinh tế | 100% |
| **Nâng cấp mô hình Coordinator → phán xét chính xác hơn** | **100%** (kịch bản phán xét được giữ) |
| ~~Nâng cấp mô hình Coordinator → định tuyến chính xác hơn~~ | Từ bỏ (tỉ lệ lỗi định tuyến vốn phải là 0, không cần LLM thông minh hơn) |

**Giữ lại quan trọng**: Các kịch bản phán xét như đánh giá can thiệp người dùng, chọn loại kiến trúc sư, phán xét biên giới verdict vẫn do LLM xử lý, hưởng lợi trực tiếp từ nâng cấp mô hình.

### 4.3 Năng lực đa agent

- Số lượng SubAgent, chức năng, cách lắp ráp **hoàn toàn không thay đổi**
- Dị cấu mô hình (coordinator/architect/writer/editor cấu hình độc lập) **hoàn toàn không thay đổi**
- Coordinator vẫn là run liên tục, giữ "góc nhìn toàn sách"
- Phương tiện cộng tác (sản phẩm trong Store) không thay đổi

### 4.4 Tính phản hồi

- Khả năng ngắt Steer của người dùng qua `coordinator.Inject` **được giữ nguyên hoàn toàn**
- Host Router phái xuất lệnh khi SubAgent trả về, đi cùng kênh tin nhắn với Steer của người dùng
- Ưu tiên của Inject cao hơn FollowUp (ngữ nghĩa `Inject` là chen hàng), Steer không bị lệnh Host chèn ép

### 4.5 Chi phí token

Hiện tại mỗi chương: Coordinator ~3500-7000 token × 1-2 turns = 3500-14000 token

Kiến trúc mới mỗi chương:
- Prompt Coordinator giảm từ ~3000 token xuống ~800 token
- Mỗi chương vẫn cần 1 turn (Coordinator đọc lệnh FollowUp + sinh tool_call)
- Tổng cộng ~1000-1500 token

**Tiết kiệm 60-80%**. Tiểu thuyết dài 200 chương tiết kiệm khoảng 400k-1M token (không bằng phương án triệt để 100%, nhưng không hy sinh tính phản hồi và góc nhìn toàn sách).

---

## 5. Ảnh hưởng đến docs/architecture.md

### 5.1 Điều chỉnh §2 Nguyên tắc cốt lõi

**Nguyên tắc một** (LLM điều khiển vòng lặp chính) → Điều chỉnh thành:
```
LLM điều khiển sáng tác và phán xét, Host điều khiển định tuyến quy trình.

- Sáng tác và phán xét (quyết định cần hiểu ngữ nghĩa, phán đoán chất lượng, nhận diện ý định) vẫn để LLM
- Định tuyến quy trình (đọc sự kiện → tra bảng → phát lệnh) do code Host đảm nhận
- Host không bỏ qua Coordinator để gọi trực tiếp SubAgent, mà thông qua FollowUp hạ lệnh rõ ràng,
  giữ lại Coordinator làm kênh thực thi lệnh và nút phán xét
```

**Nguyên tắc hai** (đặt cược vào năng lực mô hình, không đặt cược vào hardcode) → Điều chỉnh thành:
```
Đặt cược vào mô hình ở chiều sáng tác và phán xét (Writer/Editor/Architect/năng lực phán xét Coordinator),
dùng code biểu đạt ở chiều định tuyến quy trình (không gian quyết định của agent chuyên ngành là đóng,
nhiệm vụ tra bảng LLM không có lợi thế nâng cấp).
```

### 5.2 Điều chỉnh §13 Danh sách cấm

- §13.13 "Không làm Host đọc file tín hiệu → chèn lệnh bước tiếp theo làm mặt phẳng điều khiển xác định" →
  **Sửa lại cách diễn đạt**: "Không dùng file tín hiệu làm IPC (đọc Progress + Checkpoint trực tiếp là đủ), Host đọc sự kiện rồi hạ lệnh gọi agent phụ cụ thể qua `coordinator.FollowUp`, là định tuyến chuyên ngành hợp lý"
- §13.14 "Không hardcode chuyển đổi Flow trạng thái máy" →
  **Sửa lại cách diễn đạt**: "Nhãn Flow vẫn chỉ do công cụ cập nhật (không viết 'nếu A thì SetFlow(B)' trong Host), nhưng Flow Router có thể dựa trên Flow và các sự kiện khác để quyết định bước tiếp theo gọi ai"

### 5.3 Điều chỉnh §7 Lắp ráp Agent

- Giữ lại lắp ráp Coordinator
- `coordinator.md` giảm từ 88 dòng xuống ~25 dòng
- Kênh Reminder thu gọn (xóa flow/queue_guard/book_complete, giữ foundation/subagent_guards)
- Bổ sung package `internal/host/flow/`

---

## 6. Điểm yếu đã biết (liệt kê trung thực)

### 6.1 Tiến hóa dài hạn của Flow Router

- Khi thêm kịch bản mới (trạng thái flow mới, xử lý sau cuối cung truyện mới), switch-case của Router sẽ dài thêm
- Cần ràng buộc nghiêm ngặt: **chỉ xử lý định tuyến, không xử lý logic nghiệp vụ**; quy tắc quyết định phải có unit test
- Bài học cảnh báo từ `handleSubAgentDone` v0.0.1 luôn có hiệu lực; nhưng phương án này tránh trượt thành God Object thông qua "hàm thuần túy + unit test + chỉ gọi sự kiện thuần túy"

### 6.2 Sự phức tạp của can thiệp người dùng

- Thiết kế hiện tại giao Steer hoàn toàn cho LLM của Coordinator phán xét
- Nhưng một số Steer trải dài nhiều loại (ví dụ: "sửa rõ nhân vật A mấy chương trước + sau đó cho nó thêm tuyến phụ")
- Cần dựa vào năng lực LLM để phân giải, prompt cần đưa ra hướng dẫn rõ ràng
- **Phần này hưởng lợi trực tiếp từ nâng cấp mô hình** (so với hardcode phân loại enum InterventionAgent, LLM phán xét linh hoạt phù hợp hơn với kịch bản thực tế)

### 6.3 Phụ thuộc tiền đề vào tính nhất quán lớp sự kiện

- Router quyết định dựa trên Progress + điểm khôi phục, lớp sự kiện phải đáng tin cậy
- `withWriteLock` hiện tại được đóng gói tốt, bộ ba của commit_chapter hoàn thành nguyên tử
- Nhưng nếu lớp sự kiện không nhất quán (ví dụ Progress nói chương 3 hoàn thành nhưng thư mục chapters/ không có), Router sẽ ra quyết định sai
- Đề xuất: thêm **kiểm tra tính nhất quán lớp sự kiện** một lần khi khởi động (nếu phát hiện Progress.CompletedChapters không khớp thư mục chapters/, báo warning)

### 6.4 Coordinator vẫn có khả năng định tuyến bằng LLM

- Dù lệnh rõ ràng, LLM có thể "sáng tạo" không thực thi (ví dụ sinh một đoạn suy nghĩ rồi mới gọi công cụ)
- StopGuard làm lưới an toàn: nhận lệnh Host nhưng lượt này chưa gọi subagent thì chèn nhắc nhở
- Đây là lưới an toàn, không phải cấm đoán — mô hình mạnh thỉnh thoảng "thêm bước suy nghĩ" cũng không phải điều xấu

### 6.5 Yêu cầu phủ sóng kiểm thử tăng cao

- Flow Router là hàm thuần túy, phải có unit test đầy đủ (phủ sóng mọi tổ hợp Phase × Flow × Boundary)
- Kiểm thử tích hợp: mô phỏng chuỗi đầy đủ "commit → router → FollowUp → coordinator phản hồi → subagent"
- Kiểm thử khôi phục sau crash: kill tiến trình rồi resume, khẳng định Router suy ra đúng bước tiếp theo

---

## 7. Lộ trình triển khai

### Giai đoạn 1: Tăng cường lớp sự kiện (khoảng 0.5 ngày)

- Bổ sung kiểm tra nhất quán §6.3: quét một lần khi khởi động/Resume, tạo warning
- Đảm bảo API `store.HasArcReview(vol, arc)` và `HasArcSummary(vol, arc)` khả dụng (nếu chưa có thì thêm)

### Giai đoạn 2: Đưa vào khung xương Flow Router (khoảng 1 ngày)

- Tạo mới package `internal/host/flow/`:
  - `route.go` — hàm thuần túy `Route(state) → *NextInstruction`
  - `dispatcher.go` — đăng ký sự kiện + FollowUp hạ lệnh
  - `route_test.go` — unit test bao phủ mọi nhánh
- Kiểm soát có bật hay không qua switch config `flow_driven: true/false`
- Mặc định tắt (false), chạy đối chiếu trước

### Giai đoạn 3: Kích hoạt và xác nhận (khoảng 1 ngày)

- Bật `flow_driven: true`
- Chạy một tiểu thuyết 30-50 chương, so sánh chỉ số:
  - Số lần gọi LLM của Coordinator
  - Số lỗi định tuyến (phải là 0)
  - Tính phản hồi (steer ngắt có bình thường không)
- Sửa bug, tinh chỉnh quy tắc Router

### Giai đoạn 4: Đơn giản hóa coordinator.md + thu gọn Reminder (khoảng 0.5 ngày)

- Sửa coordinator.md theo §3.6
- Xóa `reminder/flow.go / queue_guard.go / book_complete.go`
- Giữ lại foundation reminder cần thiết
- Cập nhật StopGuard của agent phụ nếu cần (thường không cần)

### Giai đoạn 5: Đơn giản hóa resume.go (khoảng 0.5 ngày)

- Xóa phần lớn nhánh của `buildResumePrompt`
- Thay thế bằng tin nhắn chung ngắn gọn "[Khôi phục] Hãy chờ lệnh Host"
- Sau khi Resume, Router tự nhiên suy ra hành động tiếp theo

### Giai đoạn 6: Cập nhật tài liệu kiến trúc (khoảng 0.5 ngày)

- Sửa `docs/architecture.md` §2 / §13 / §7 theo §5
- Đổi trạng thái tài liệu đề xuất này thành "Đã được chấp thuận", lưu trữ vào `docs/history/`

### Giai đoạn 7: Giai đoạn quan sát (2-4 tuần)

- Chạy liên tục 2-3 tiểu thuyết dài (mỗi cuốn 100+ chương)
- Ghi lại mọi lỗi định tuyến (nếu có), vấn đề phản hồi, hành vi bất ngờ của Coordinator
- Tinh chỉnh quy tắc Router và coordinator.md dựa trên quan sát

**Tổng cộng khoảng 4 ngày triển khai + giai đoạn quan sát**.

---

## 8. Bảng so sánh

| Chiều | Kiến trúc hiện tại | Hybrid (phương án này) | Phương án triệt để (Phụ lục A) |
|---|---|---|---|
| Tính ổn định | Trung bình (LLM thỉnh thoảng định tuyến sai) | **Cao** | Cao |
| Tính phản hồi | Cao | **Cao** | **Thấp** (Host gọi trực tiếp SubAgent không thể ngắt) |
| Lợi nhuận LLM | 100% | **100%** | 85% (từ bỏ chiều định tuyến) |
| Tiết kiệm token | 0 | ~70% | ~95% |
| Góc nhìn toàn sách | Có | **Có** | Không (mỗi SubAgent độc lập) |
| Chi phí triển khai | - | Trung bình (khoảng 4 ngày) | Cao (khoảng 1 tuần + sửa agentcore) |
| Cập nhật tài liệu | - | Nhỏ (tinh chỉnh §2/§13) | Lớn (viết lại nguyên tắc §2) |
| Cần sửa agentcore | - | Không | Có thể (gọi trực tiếp SubAgent) |
| Độ khó rollback | - | Thấp (switch config) | Cao |

---

## 9. Điểm quyết định

1. **Có chấp thuận đề xuất này (Hybrid Coordinator) không?** [ ] Chấp thuận · [ ] Chấp thuận sau khi sửa đổi · [ ] Không chấp thuận
2. Giai đoạn 3 có làm PR độc lập để xác nhận trước không? [ ]
3. Điều chỉnh `docs/architecture.md` §2 / §13 có xử lý trong lần này không? [ ]
4. Độ dài giai đoạn quan sát: [ ] 2 tuần · [ ] 4 tuần · [ ] Lâu hơn

---

## Phụ lục A: Phương án triệt để đã đánh giá (Xóa hoàn toàn Coordinator)

> Phương án bản thảo đầu tiên. Bị hạ cấp thành tham khảo do các vấn đề như tính phản hồi suy giảm, tính khả thi kỹ thuật đáng ngờ, mất góc nhìn toàn sách của Coordinator.

Cốt lõi của phương án triệt để: Host gọi trực tiếp `SubAgentTool.Execute`, không qua LLM Coordinator.

**Các vấn đề đã xác định**:

1. **Tính phản hồi thụt lùi**: `SubAgentTool.Execute` là cuộc gọi đồng bộ chặn, Steer của người dùng phải chờ SubAgent hiện tại trả về mới được xử lý. Kiến trúc hiện tại `Inject` có thể ngắt ngay lập tức.
2. **Tính khả thi kỹ thuật đáng ngờ**:
   - Host gọi trực tiếp SubAgentTool vi phạm thông lệ sử dụng agentcore
   - Luồng sự kiện (`Event` của `Subscribe`) có thể không nổi bọt đúng về observer
   - Đường dẫn callback `ContextManagerFactory` / `OnMessage` của SubAgent chưa rõ
   - Cần sửa agentcore hoặc sửa lớn observer
3. **Mất góc nhìn toàn sách của Coordinator**: Mỗi SubAgent run độc lập, không có "LLM canh gác liên tục". Trong chạy dài, phôi trôi phong cách, nhân vật rời rạc mất đi một lớp bảo vệ vô hình.
4. **InterventionAgent đơn giản hóa quá mức**: Phương án triệt để dùng enum (query/modify_setting/rewrite_chapters/adjust_style/noop) phân loại ý định người dùng, Steer thực tế có thể trải dài nhiều loại, schema cứng sẽ phân loại sai.
5. **Khối lượng viết lại tài liệu kiến trúc lớn**: §2 nguyên tắc cốt lõi bị lật đổ, 30% luận điểm tài liệu bị ảnh hưởng.
6. **FlowDriver sẽ phình thành God Object**: Một vòng lặp chứa mọi logic định tuyến, mỗi khi thêm kịch bản đều phải sửa, đồng cấu với `handleSubAgentDone` v0.0.1.

Phương án Hybrid tránh được 4 vấn đề đầu, vấn đề thứ 5 giảm xuống thành tinh chỉnh, vấn đề thứ 6 được kiểm soát qua "hàm thuần túy + unit test".

---

## Phụ lục B: Chi tiết vị trí điểm quyết định

| Điểm quyết định | Vị trí hiện tại | Vị trí kiến trúc mới | Loại |
|---|---|---|---|
| Chọn kiến trúc sư | coordinator.md L26-29 | LLM Coordinator phán xét (khi khởi động) | Phán xét |
| Mở rộng đầu vào | coordinator.md L31 | LLM Coordinator phán xét (khi khởi động) | Phán xét |
| Vòng lặp bổ sung quy hoạch | coordinator.md L36-38 | Nhánh Host Router Phase=Premise/Outline (trả nil để LLM tự chủ hoặc FollowUp architect rõ ràng) | Hỗn hợp |
| Bước tiếp theo mỗi chương | coordinator.md L46-51 + reminder/flow | **Nhánh Host Router 2d** (FollowUp writer) | Định tuyến |
| Đánh giá cuối cung truyện | coordinator.md L78-82 | **Nhánh Host Router 2c** (FollowUp editor/architect) | Định tuyến |
| Phân nhánh verdict | coordinator.md L59-61 + công cụ save_review | Lớp công cụ đã code hóa, Router chỉ đọc Flow | Định tuyến (đã hoàn thành) |
| Can thiệp người dùng | coordinator.md L67-70 | LLM Coordinator phán xét (khi nhận tin nhắn Inject) | Phán xét |
| Phái lại khi kiến trúc sư báo lỗi | coordinator.md L40 | Host Router phát hiện FoundationMissing chưa thay đổi, đếm số lần thử lại | Định tuyến |
| Tổng kết khi hoàn thành toàn sách | coordinator.md L63-65 + reminder/book_complete | Host Router phát hiện Phase=Complete → FollowUp "xuất tổng kết" | Định tuyến |

---

## Phụ lục C: Vị trí mã nguồn tham khảo

- `assets/prompts/coordinator.md` — chờ đơn giản hóa
- `internal/host/reminder/flow.go` / `queue_guard.go` / `book_complete.go` — chờ xóa bỏ
- `internal/host/reminder/subagent_guards.go` — giữ lại
- `internal/host/reminder/stop_guard.go` — giữ lại + thêm kiểm tra "phải thực thi khi nhận lệnh Host"
- `internal/host/resume.go` — đơn giản hóa đáng kể
- `internal/host/observer.go` — đăng ký mới EventToolExecEnd để kích hoạt Router
- `internal/host/flow/` — package mới bổ sung
- `internal/tools/commit_chapter.go` L220-280 — 17 trường CommitResult đã đầy đủ
- `internal/tools/save_review.go` L76-116 — nâng cấp verdict và chuyển đổi Flow đã code hóa
- `internal/store/outline.go` `CheckArcBoundary` — API sự kiện biên giới cung truyện
