# Kiến trúc runtime của ainovel-cli

> Để LLM hoàn thành một cuốn tiểu thuyết trong một lần Run, Host chỉ đảm nhận khởi động / khôi phục / định tuyến / quan sát; quyền quyết định được giữ lại tối đa cho mô hình.

---

## 1. Mục tiêu (theo thứ tự ưu tiên)

1. **Ổn định**: Nhập một câu, hệ thống viết ổn định toàn bộ tiểu thuyết (200~500 chương). Không tự ngắt giữa chừng do vấn đề kiến trúc.
2. **Chất lượng có thể cải tiến**: prompt / tài liệu tham khảo / tiêu chí đánh giá / chiến lược ngữ cảnh có thể điều chỉnh độc lập, không ảnh hưởng đến kiến trúc.
3. **Có thể khôi phục**: Sau khi crash, mất mạng, tạm dừng — có thể tiếp tục từ điểm khôi phục gần nhất.
4. **Có thể quan sát**: Tiến độ, sản phẩm, thời gian xử lý của từng chương và từng bước đều có thể kiểm tra.

"Ổn định" là tiền đề, "chất lượng" là tầng trên. Mọi quyết định kiến trúc đều ưu tiên phục vụ tính ổn định.

---

## 2. Nguyên tắc cốt lõi

### 2.1 LLM điều khiển sáng tác và phán quyết, Host điều khiển định tuyến quy trình

Không gian quyết định của agent chuyên ngành là đóng: sơ đồ quy trình cố định, nhánh có hạn, dựa trên dữ liệu thực tế. Hai loại quyết định đi theo hai phương tiện khác nhau:

- **Sáng tác và phán quyết** (ngữ nghĩa / chất lượng / hiểu ý định) → LLM. Khả năng phán quyết của Writer/Editor/Architect/Coordinator được hưởng lợi tuyến tính khi mô hình nâng cấp
- **Định tuyến quy trình** (đọc dữ liệu thực tế, tra bảng) → code. `flow.Router` hàm thuần túy + unit test, tỉ lệ lỗi tiệm cận 0

Host không gọi trực tiếp SubAgent, mà tại mỗi TurnEnd của Điều phối viên, Flow Router tính toán chỉ thị và truyền qua `coordinator.FollowUp("[Host hạ lệnh]…")`.

### 2.2 Công cụ là giao diện duy nhất của tầng dữ liệu thực tế

Mọi tương tác với hệ thống file, Progress, Checkpoint đều được thực hiện qua công cụ. **Công cụ ghi phải có bộ ba nguyên tử**: artifact ghi xuống đĩa + Progress cập nhật + Checkpoint thêm vào, thực hiện trong khóa loại trừ. Chạy lại cùng một công cụ cho kết quả giống nhau hoặc bỏ qua trực tiếp (digest idempotent).

### 2.3 Tầng quan sát chỉ quan sát

UI, chẩn đoán, nhật ký sự kiện đều là người tiêu thụ thụ động được chiếu từ luồng sự kiện / artifact chỉ đọc. Đọc dữ liệu thực tế, không tạo ra dữ liệu thực tế, không ảnh hưởng đến luồng điều khiển.

**`internal/diag` là hệ thống con quan sát duy nhất của engine** — cơ sở hạ tầng hỗ trợ hàng đầu, nhưng không phải lõi sản phẩm (lõi là engine sáng tác ở §6; thiếu diag vẫn viết tiểu thuyết được). Nó đọc chéo hầu hết artifact + session + log + checkpoint, đảm nhận hai vai: ① **Chẩn đoán chất lượng sáng tác** (quy tắc → Finding, báo cáo màn hình `/diag`); ② **Debug runtime + xuất khử nhạy cảm** (bóc xương động hành vi bỏ nội dung chính + tổng hợp vòng lặp → `meta/diag-export.md` ghi đè, để người dùng paste issue; maintainer không có output local vẫn có thể xác định vòng lặp chết/vấn đề ngắt).

**Kỷ luật quan sát (không được lơi lỏng)**: diag có thể chẩn đoán, có thể đề xuất, nhưng **không bao giờ tự tay làm** — không tự động sửa, không tiếp tục chạy, không thay đổi quy trình. Nó càng mạnh, càng có người muốn nó "nhân tiện sửa luôn", càng phải giữ vững ranh giới này, nếu không sẽ sa vào các bẫy idleResume / StallDetector đã bị xóa (xem §10.5, §10.14). Cấu trúc hướng ngoại (như `RuntimeCapture`) được duy trì như hợp đồng cơ sở hạ tầng, đừng tùy tiện thay đổi field.

### 2.4 Tầng dữ liệu thực tế phẳng

Chỉ có ba loại dữ liệu thực tế:

- **Progress** — Chỉ mục tiến độ (đang viết đến chương mấy, danh sách chờ viết lại)
- **Checkpoint** — Bản ghi cập nhật cấp bước (plan / draft / commit / review / arc_summary)
- **Artifact** — Nội dung chương, đề cương, nhân vật, tóm tắt và các sản phẩm khác

Không đưa vào các abstraction như WorkflowInstance / TaskInstance / Command / Dispatcher.

### 2.5 Ba quy tắc sắt

**Quy tắc sắt 1: Công cụ chỉ trả về dữ liệu thực tế, không trả về chỉ thị định tuyến liên lần gọi**. `commit_chapter` trả về các field có cấu trúc như `arc_end_reached` / `next_skeleton_arc`; không nhúng chuỗi chỉ thị dạng `[Hệ thống]`. Field `next_step` trong agent phụ là hướng dẫn nội tuyến trình bày dữ liệu thực tế ("Tôi vừa lưu plan, bước tiếp theo là draft") — không vi phạm quy tắc này — xem §6.4.

**Quy tắc sắt 2: Định tuyến quy trình do Flow Router đảm nhận**. `Route(state) → *Instruction` trong `internal/host/flow/router.go` là hàm thuần túy, đăng ký `EventToolExecEnd` rồi truyền qua `FollowUp`. Trả về nil có nghĩa là "tình huống phán quyết, để LLM tự chủ". **Kênh chỉ thị không im lặng**: Khi Route liên tục tính ra cùng một chỉ thị (có nghĩa là trạng thái chưa cập nhật sau lần phát trước), Dispatcher đính kèm sự thực "lần phát thứ N" để phát lại thay vì im lặng nuốt — "kết quả định tuyến lặp lại" là sự thực chỉ Host có thể quan sát; im lặng sẽ khiến Điều phối viên rơi vào mâu thuẫn kép "không có chỉ thị không được hành động / StopGuard không cho phép dừng". Không đặt ngưỡng, không ngắt mạch; cách thoát khỏi tình trạng bế tắc do LLM phán quyết.

**Quy tắc sắt 3: Điều phối viên không thể vật lý end_turn, trừ khi Phase=Complete**. StopGuard tầng agentcore chặn `end_turn` và inject user message; liên tiếp chặn 5 lần sẽ nâng cấp terminate. Ba agent phụ (architect / writer / editor) có `CheckpointDeltaGuard` riêng.

---

## 3. Toàn cảnh kiến trúc

```
[Entry: TUI / chế độ không giao diện]
        │ prompt / steer
[Host vỏ mỏng]
   ├── observer        sự kiện → chiếu UI/nhật ký
   ├── flow.Dispatcher đăng ký ToolExecEnd → Route(state) → FollowUp
   └── usage / quản lý mô hình
        │
[Điều phối viên (LLM, MaxTurns=100_000)]
   ├── Khi khởi động phán quyết architect_short / long
   ├── Nhận [Host hạ lệnh] → tạo subagent tool_call
   └── Nhận [Người dùng can thiệp] → phán quyết tự chủ
        │
[architect / writer / editor SubAgent (mỗi cái có run + context + mô hình độc lập)]
        │ gọi công cụ
[Tools]  novel_context · read_chapter · plan_chapter · draft_chapter · edit_chapter
         check_consistency · commit_chapter · save_review · save_arc_summary
         save_volume_summary · save_foundation
        │ bộ ba nguyên tử
[Store: hệ thống file (tmp + rename)]
   Progress · Checkpoints · Outline · Drafts · Summaries · Characters · World · Signals
```

| Tầng | Làm gì | Không làm gì |
|---|---|---|
| Entry | Hiển thị, nhận đầu vào | Quyết định nghiệp vụ |
| Host | Khởi động/khôi phục/can thiệp/chiếu sự kiện/định tuyến Flow | Bỏ qua Điều phối viên gọi trực tiếp SubAgent; ghi trạng thái |
| Điều phối viên | Thực thi chỉ thị Host, phán quyết Steer người dùng, khởi động chọn người lập kế hoạch | Tự quyết định bước tiếp theo của mỗi chương; ghi file |
| Agents | Suy nghĩ, viết lách, đánh giá | Đọc ghi Store trực tiếp |
| Tools | IO nguyên tử + checkpoint + idempotent | Chỉ thị định tuyến liên agent |
| Store | Ghi xuống đĩa hệ thống file | Logic nghiệp vụ |

Phụ thuộc một chiều: `entry → host → agents → tools → store → domain`. `tools/` không tham chiếu `agents/host/`, `host/` không tham chiếu trực tiếp `tools/store/`. Module độc lập ngang: `errs/` có thể được bất kỳ tầng nào tham chiếu, `diag/` đăng ký luồng sự kiện host + chỉ đọc `store/`.

---

## 4. Mô hình dữ liệu

### 4.1 Progress (`internal/domain/runtime.go`)

```go
type Progress struct {
    NovelName         string
    Phase             Phase           // init / premise / outline / writing / complete
    CurrentChapter    int
    TotalChapters     int
    CompletedChapters []int
    TotalWordCount    int
    ChapterWordCounts map[int]int
    InProgressChapter int             // chương đang được viết
    Flow              FlowState       // writing / reviewing / rewriting / polishing / steering
    PendingRewrites   []int
    StrandHistory     []string        // chuỗi dominant_strand
    HookHistory       []string        // chuỗi hook_type
    CurrentVolume, CurrentArc int     // phân tầng tiểu thuyết dài
    Layered           bool
}
```

Logic điều khiển chỉ đọc các field dữ liệu thực tế nêu trên, không phụ thuộc vào bất kỳ "timestamp cập nhật" nào — thông tin thời gian được mang bởi `OccurredAt` của checkpoint.

### 4.2 Checkpoint (`internal/domain/checkpoint.go`)

```go
type Scope      struct { Kind ScopeKind; Chapter, Volume, Arc int }
type Checkpoint struct {
    Seq        int64       // tăng đơn điệu
    Scope      Scope       // chapter / arc / volume / global
    Step       string      // plan / draft / commit / review / arc_summary / ...
    Artifact   string
    Digest     string
    OccurredAt time.Time
}
```

Lưu trữ: `meta/checkpoints.jsonl`, chỉ thêm vào. Ghi trùng lặp cùng `Scope+Step+Digest` được coi là idempotent, không tạo dòng mới.

### 4.3 Artifact và Signals

Artifact nằm trong `store/outline.go` `drafts.go` `summaries.go` `characters.go` `world.go` — mỗi loại sản phẩm đều có thể được checkpoint tham chiếu.

Signals: `PendingCommit` (khôi phục ngắt commit) / `PendingSteer` (can thiệp người dùng trong thời gian dừng máy). Đọc khi khởi động/khôi phục, không đọc trong lúc chạy.

---

## 5. Giao ước công cụ

Công cụ là điểm tương tác duy nhất giữa tầng dữ liệu thực tế và Agent.

### 5.1 Công cụ đọc

`novel_context(scope)` / `read_chapter(n)` — có thể gọi bất kỳ lúc nào, không phụ thuộc vào trạng thái tiền đề, trả về dữ liệu đủ để LLM quyết định độc lập.

### 5.2 Công cụ ghi (bộ ba nguyên tử)

Mỗi lần gọi thành công phải: artifact ghi xuống đĩa → Progress cập nhật → checkpoint thêm vào. Ba bước hoàn thành trong khóa loại trừ.

| Công cụ | Artifact | Step |
|---|---|---|
| `plan_chapter` | drafts/chXX.plan.json | plan |
| `draft_chapter` | drafts/chXX.draft.md | draft |
| `edit_chapter` | drafts/chXX.draft.md | edit |
| `check_consistency` | Không có (chỉ đọc, trả về inline) | consistency_check |
| `commit_chapter` | chapters/chXX.md + Progress | commit |
| `save_review` | reviews/chXX.json (global là chXX-global.json) | review |
| `save_arc_summary` | summaries/arc-vNNaNN.json | arc_summary |
| `save_volume_summary` | summaries/vol-vNN.json | volume_summary |
| `save_foundation` | foundation/*.json | premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book |

`commit_chapter` đảm nhận phát hiện kết thúc cung/tập/toàn sách, trả về 19 field dữ liệu thực tế (`arc_end` / `needs_expansion` / `book_complete` v.v.; khi bật kiểm tra quy tắc cơ học thì thêm `rule_violations`). `save_review` đảm nhận nâng cấp verdict (cổng chấm điểm, hợp đồng missed → rewrite). Những logic trước đây rải rác ở tầng policy nay được cố định trong nội bộ công cụ.

`edit_chapter` là wrapper mỏng của `agentcore.EditTool`, kiểm tra quyền sở hữu đảm bảo chương đã hoàn thành phải nằm trong `PendingRewrites` mới được chỉnh sửa.

### 5.3 Phân tầng lỗi

| Loại lỗi | Tầng xử lý | Hành động |
|---|---|---|
| Network timeout / streaming EOF | Tools | Thử lại 3 lần |
| provider 429/503 | litellm | failover sang nhà cung cấp dự phòng |
| Xác thực / mô hình không tồn tại | Tools | Ném lên terminal |
| Thiếu artifact tiền đề | Tools | Ném lên conflict, LLM gọi `novel_context` rồi thử lại |
| Tham số công cụ không hợp lệ | Tools | Ném lên validation, LLM sửa tham số |
| MaxTurns cạn | agentcore | run kết thúc, Host phát done |
| Tin nhắn không hợp lệ từ LLM (thinking-only stop, v.v.) | agentcore (`llm/litellm.go` `convertMessages`) | Đẩy vào stack dự phòng + lọc khi pop; Host không nhận biết |
| Phản hồi streaming rỗng / suy nghĩ dài | litellm (`StreamIdleTimeout=5min`) | watchdog kích hoạt thử lại |

### 5.4 Idempotent

Trước khi thực thi mỗi công cụ ghi, kiểm tra checkpoint trước: nếu `Step+Digest` của checkpoint mới nhất trong scope hiện tại giống với lần này, trả về trực tiếp sản phẩm đã có. LLM có thể yên tâm thử lại mà không tạo ra chương trùng lặp hoặc tiến độ sai lệch.

---

## 6. Lắp ráp Agent

> Một Prompt siêu lớn + một Agent duy nhất chạy xong một cuốn sách là khả thi về lý thuyết, nhưng ba vấn đề sẽ cản trở tính ổn định: **bùng nổ ngữ cảnh** (200 chương dù nén mạnh cũng thoái hóa), **nhiễu loạn trách nhiệm** (lập kế hoạch nghiêm túc / sáng tác tưởng tượng / đánh giá phê phán pha loãng lẫn nhau trong cùng một prompt), **mất lợi ích từ mô hình dị thể** (lập kế hoạch dùng Opus, viết lách dùng Sonnet, đánh giá dùng Pro — chọn mô hình độc lập là không gian tối ưu hóa chi phí/chất lượng đáng kể cho tiểu thuyết dài). Topo đa agent vì vậy là cần thiết.

### 6.1 Điều phối viên

Người điều khiển vòng lặp chính duy nhất. Được lắp ráp trong `internal/agents/build.go`:

```go
agent := agentcore.NewAgent(
    agentcore.WithModel(coordinatorModel),
    agentcore.WithSystemPrompt(bundle.Prompts.Coordinator),
    agentcore.WithTools(subagentTool, contextTool),
    agentcore.WithMaxTurns(100_000),
    agentcore.WithToolsAreIdempotent(true),
    agentcore.WithMaxToolErrors(0),  // không ngắt mạch subagent
    agentcore.WithMaxRetries(subagentMaxRetries),
    agentcore.WithContextManager(...),
    agentcore.WithStopGuard(reminder.NewStopGuard(store, nil)),
    agentcore.WithToolGate(completePhaseGate(store)),  // phase=complete chặn cứng phát subagent
)
```

Trách nhiệm: khi khởi động chọn người lập kế hoạch → vòng lặp bổ sung kế hoạch → nhận `[Host hạ lệnh]` ngay lập tức tạo `subagent` tool_call tương ứng → xử lý `[Người dùng can thiệp]` phán quyết tự chủ → sau `book_complete=true` xuất tóm tắt.

Không làm: ghi file, đọc trực tiếp Progress (dùng novel_context), tự quyết định bước tiếp theo khi chỉ thị Host đến.

> **Tại sao không xóa Điều phối viên và để Host gọi trực tiếp agent phụ?** Trông có vẻ "gọn hơn", nhưng sẽ mất bốn thứ: (1) Quyết định "bước tiếp theo làm gì" được giữ ở tầng LLM, mô hình nâng cấp trực tiếp được hưởng lợi; (2) Phán quyết mềm về verdict đánh giá (accept/polish/rewrite + phạm vi ảnh hưởng) được chuyển ra khỏi Go code; (3) Đánh giá ảnh hưởng của Steer người dùng giao cho mô hình — câu "động cơ nhân vật phụ phải rõ hơn" cần viết lại những chương nào, Điều phối viên có thể phán quyết nhưng Host hard-code thì không; (4) Nhánh bất thường (phản hồi đề cương từ writer, editor phát hiện lỗ hổng thế giới quan) được mô hình tự xử lý, tránh phải viết Go state machine cho từng nhánh. **Xóa Điều phối viên là chuyển cược từ "mô hình ngày càng mạnh" sang "Go code của tôi ngày càng mạnh" — đây không phải cược hay**.

### 6.2 Topo agent phụ và mô hình dị thể

```
Điều phối viên (1 agent run, MaxTurns=100_000)
    ↓ subagent()
architect_short/long  ·  writer  ·  editor
    ↓ gọi công cụ
Store (môi trường hợp tác, các agent phụ không giao tiếp trực tiếp với nhau)
```

Bộ đếm turn của agent phụ là độc lập (nguyên gốc agentcore), không chiếm quota 100_000 turn của Điều phối viên. Các agent phụ giao tiếp qua artifact có cấu trúc trong Store, Điều phối viên chỉ truyền "mô tả nhiệm vụ" chứ không chuyển nội dung.

`bootstrap.ModelSet` hỗ trợ mô hình cấp vai trò: coordinator/architect/writer/editor đều có cấu hình độc lập + provider failover. Writer chạy Sonnet thay vì Opus có thể tiết kiệm một bậc chi phí trong tiểu thuyết dài 200 chương.

### 6.3 Ba chế độ hợp tác

Các agent phụ không giao tiếp trực tiếp, mọi luồng thông tin đều đi qua artifact có cấu trúc trong Store. Ba chế độ bao phủ toàn bộ workflow của hệ thống:

**Chế độ A · Bàn giao tuần tự (nhánh chính)**: Điều phối viên → Kiến trúc sư lập kế hoạch → Người viết chương 1..N → Biên tập viên đánh giá cuối cung → Người viết viết lại. Chế độ phổ biến nhất, Điều phối viên dùng `novel_context` tra trạng thái hiện tại để quyết định gọi ai tiếp theo.

**Chế độ B · Phản hồi đánh giá (vòng kín)**: Người viết phát hiện đề cương lệch trong bản nháp → Giá trị trả về của `commit_chapter` mang field `writer_feedback` → Điều phối viên thấy phản hồi phán quyết có nên nâng cấp thành lời gọi architect để điều chỉnh đề cương. Người viết không gọi trực tiếp Kiến trúc sư, phản hồi được gửi về Điều phối viên qua field có cấu trúc.

**Chế độ C · Mở rộng khung xương (kế hoạch cuộn)**: `commit_chapter` phát hiện cung tiếp theo vẫn là khung xương → trả về `arc_end_reached + next_skeleton_arc` → Flow Router phát chỉ thị → Điều phối viên gọi architect_long mở rộng các chương chi tiết của cung tiếp theo → Người viết tiếp tục. Khả năng "kế hoạch cuộn" tiểu thuyết dài chính là vòng kín này.

### 6.4 Ràng buộc code của quy trình agent phụ (không dựa vào nạng prompt)

> Ban đầu quy trình writer dựa vào ràng buộc "nghiêm ngặt theo thứ tự sau" trong `writer.md`. LLM thường vi phạm — bỏ qua plan nhảy thẳng draft, sau commit tiếp tục nói thêm một đoạn tiêu tốn token, chỉ viết nội dung vào chat mà không ghi xuống. **Ràng buộc quy trình bằng prompt không ổn định**, mạnh yếu hoàn toàn phụ thuộc vào mức độ "nghe lời" của mô hình lúc đó, mô hình nâng cấp còn có thể "sáng tạo mà không tuân thủ".

Bốn tầng ràng buộc code (cùng hiệu lực):

| Tầng | Điểm tác dụng | Vai trò |
|---|---|---|
| `StopAfterTools` / `StopAfterToolResult` | `agents/build.go` SubAgentConfig | Công cụ quan trọng thành công → end_turn thoát subagent run. Writer nhấn `commit_chapter` dừng ngay (`StopAfterTools`); `save_arc_summary`/`save_volume_summary` của Editor, kết thúc cung/tập của Architect đi `StopAfterToolResult`. `save_review` của Editor không dừng cứng — nếu không sẽ cắt ngang StopGuard, kết thúc giao cho `NewEditorStopGuard` |
| `CheckpointDeltaGuard` | `host/reminder/subagent_guards.go` | Lấy checkpoint baseline làm ranh giới, trước khi kết thúc lượt này phải thấy checkpoint mới của bước tương ứng, nếu không từ chối `end_turn`; chặn liên tiếp 3 lần nâng cấp terminate (dự phòng vòng lặp chết của mô hình yếu) |
| `next_step` nội tuyến trong công cụ | Field giá trị trả về của các công cụ | Mỗi dữ liệu thực tế kèm "gợi ý bước tiếp theo". Ví dụ `plan_chapter` trả về `next_step: "Lập tức gọi draft_chapter..."`. LLM thấy dữ liệu thực tế là biết bước tiếp theo, không cần quay lại system prompt tìm |
| Kiểm tra quyền sở hữu/tiền đề trong công cụ | `edit_chapter` `commit_chapter` v.v. | Chặn vật lý ở tầng dữ liệu: `edit_chapter` từ chối sửa chương đã hoàn thành nhưng không có trong `PendingRewrites`; `commit_chapter` từ chối commit rỗng khi bản nháp == bản cuối; `ConcurrencySafe=false` ngăn race condition đồng thời |

writer.md trong kiến trúc mới chỉ đảm nhận: hướng dẫn chất lượng viết, mô hình nhận thức chạy tiếp từ điểm dừng, giải thích hợp đồng chương. **Không còn làm điều phối quy trình** — khi LLM bỏ bước thì prompt không cứu, code sẽ cứu. Architect / editor cũng có bốn tầng ràng buộc tương tự trong công cụ/Guard riêng của chúng.

> Về quy tắc sắt 1: `next_step` là trình bày dữ liệu thực tế nội tuyến trong công cụ ("Tôi vừa lưu plan"), không phải điều phối quy trình được Host inject xuyên lần gọi. Việc định tuyến liên agent phụ ở tầng Điều phối viên vẫn đi nghiêm ngặt qua Flow Router → FollowUp.

### 6.5 Phụ thuộc agentcore

`../agentcore` là thư viện Agent dùng chung của dự án (liên kết go.work). Tất cả primitive được dùng trong kiến trúc mới đều đã tồn tại: `Prompt` / `Inject` / `FollowUp` / `Subscribe` / `WithMaxTurns` / `WithStopGuard` / `WithToolGate` / `SubAgentConfig` / `WithContextManager`.

**Ranh giới sửa đổi**:

- Có thể vào agentcore: chiến lược ContextManager mới, adapter nhà cung cấp mới, loại sự kiện mới, mẫu inject tin nhắn chung
- Không vào agentcore: mô hình nghiệp vụ như Progress/Checkpoint/Scope, công cụ nghiệp vụ như novel_context/commit_chapter, quy tắc nghiệp vụ như phát hiện kết thúc cung/cổng đánh giá

Tiêu chí phán quyết: Giả sử agentcore trong tương lai sẽ được coding agent / customer service agent sử dụng, khả năng mới thêm vào vẫn có ý nghĩa trong những tình huống đó mới được phép vào. **Cấm viết patch vá víu ở tầng ứng dụng** (proxy, wrapper, monkey patch) — thiếu khả năng thì vào agentcore sửa trực tiếp.

**Khả năng cố ý không dùng** (tránh lạm dụng):

- `Agent.TaskRuntime() / Tasks() / StopTask()` — Task manager nền tích hợp trong agentcore (background subagent fire-and-forget). Kiến trúc mới mọi lời gọi agent phụ đều là foreground đồng bộ, **không sử dụng**
- `Agent.FollowUp(msg)` — **Người dùng hợp pháp duy nhất là `flow.Dispatcher`**, dùng để hạ `[Host hạ lệnh]`. Các phương thức công khai khác của Host bị cấm gọi. Steer người dùng đi `Inject` (giữ khả năng ngắt ngay), Resume đi `Prompt` khởi chạy Run mới
- `Agent.Steer(msg)` — Interface steering cũ, kiến trúc mới dùng `Inject` toàn bộ
- `WithPermission*` — Cơ chế phê duyệt quyền (người phê duyệt thủ công các thao tác nguy hiểm), ứng dụng viết tiểu thuyết không có thao tác nguy hiểm, **không sử dụng**

**Policy hook đã bật**: `WithToolGate` — Mục đích duy nhất là khi `phase=complete` chặn cứng phát `subagent` (`agents/build.go` `completePhaseGate`). Sau khi hoàn thành, nếu người dùng yêu cầu viết tiếp/viết lại, Coordinator LLM vẫn có thể tự phát agent phụ, mà Writer viết chương vượt phạm vi sẽ bị `commit_chapter` từ chối, `CheckpointDeltaGuard` lại không cho `end_turn` → vòng lặp chết. Flow Router trả về nil khi complete chỉ chặn được Host tự động phát, không chặn được LLM chủ động phát, nên Gate bổ sung một lớp bảo vệ trạng thái cuối ở điểm yết hầu. Đây là dự phòng quy trình hẹp mục đích, **không phải luồng phê duyệt `WithPermission*`**, không được nhầm lẫn hai loại.

---

## 7. Tầng Host

### 7.1 Cấu trúc

```go
type Host struct {
    cfg               bootstrap.Config
    bundle            assets.Bundle
    store             *store.Store
    models            *bootstrap.ModelSet
    coordinator       *agentcore.Agent
    coordinatorCtxMgr *corecontext.ContextEngine  // liên động cửa sổ ngữ cảnh khi đổi mô hình
    askUser           *tools.AskUserTool
    writerRestore     *ctxpack.WriterRestorePack

    observer     *observer
    router       *flow.Dispatcher  // đăng ký + Route + FollowUp
    routerDetach func()
    usage        *UsageTracker
    usageCancel  context.CancelFunc
    budget       *BudgetSentinel   // Thành phần chính sách Host: thực thi tuyên bố ngân sách người dùng (tương đương Abort thay mặt), đăng ký trước Dispatcher
    notifier     *notify.Notifier  // Tầng quan sát: bản sao ngoài màn hình của ba loại cảnh báo run_end/repeat/budget, không bao giờ can thiệp luồng điều khiển

    events, streamCh, done chan ...

    mu        sync.Mutex
    lifecycle lifecycle  // idle / running / paused / completed
    closeOnce sync.Once
}
```

### 7.2 API công khai

**Vòng đời** (điểm vào Run của Điều phối viên): `Start` / `StartPrepared` / `Resume` / `Continue` / `Steer` / `Abort` / `Close`

**Kênh quan sát**: `Events` / `Stream` / `Done` (làm rỗng luồng đi sentinel trong streamCh)

**Tổng hợp UI**: `Snapshot()` — TUI kéo một lần lấy tất cả dữ liệu hiển thị

**Cấu hình/Mở rộng**: Quản lý mô hình (`SwitchModel`), nhập tiểu thuyết ngoài để phân tích ngược (`ImportFrom`), hội thoại đồng sáng tác (`CoCreateStream`), phát lại sự kiện (`ReplayQueue`), phân tích hành văn mô phỏng (`Simulate`/`ImportSimulationProfile`), xuất (`Export`)

Không có các phương thức lập lịch nghiệp vụ như `decideNext` `retryActiveTask`. Flow Router là tổ hợp mỏng hàm thuần túy + FollowUp, không giữ trạng thái ngầm như "nhiệm vụ đang thử lại".

### 7.3 Hình thái `waitDone`

```go
func (h *Host) waitDone() {
    h.coordinator.WaitForIdle()
    h.observer.finalize()

    if Phase == Complete { lifecycle=completed; phát sự kiện "sáng tác hoàn thành" }
    else if running        { lifecycle=idle;     phát sự kiện "Điều phối viên dừng (đã hoàn thành N chương)" }

    select { case h.done <- struct{}{}: default: }
}
```

Ba việc: chờ idle → chuyển lifecycle → phát sự kiện trạng thái cuối + đẩy tín hiệu done. **Cấm `Inject` / `FollowUp` / `Prompt` xuất hiện trong thân hàm**. Sau khi LLM chạy xong một lần Run, toàn bộ Host vào trạng thái cuối.

Muốn chạy tiếp chỉ có hai cách: người dùng chủ động `Continue`/`Start`, hoặc khởi động lại tiến trình đi `Resume`.

> Bài học lịch sử: Đã từng thêm patch `idleResumeCount` tự động khởi động lại Run vào hàm này. Trong lần chạy dài mimo duy nhất thực sự kích hoạt, 100% không cứu được, ngược lại che đậy nguyên nhân thật ở tầng agentcore "tin nhắn thinking-only stop đi vào lịch sử". **"Khởi động lại phòng thủ" ở tầng Host luôn là sửa sai chỗ**. Xem `feedback_no_host_resilience.md` và mục 5 §10.

---

## 8. Khởi động và khôi phục

### 8.1 Tạo mới

```
Người dùng: "yêu cầu một câu"
  → Host.Start
    → store.Progress.Init / store.Checkpoints.Reset
    → coordinator.Prompt(userPrompt) + flow.Dispatcher.Enable + Dispatch
    → Vòng lặp dài Điều phối viên: lập kế hoạch → viết 1..N → đánh giá → done
```

### 8.2 Khôi phục (khởi động lại sau crash)

```
Tiến trình khởi động
  → Đọc Progress + Checkpoint gần nhất + PendingCommit + PendingSteer
  → buildResumePrompt → thông báo ngắn (không phải chỉ thị cấp bước)
  → coordinator.Prompt(resumePrompt) + Dispatcher.Enable + Dispatch
  → Điều phối viên tiếp tục theo chỉ thị Host
```

Resume dùng `Prompt` khởi chạy Run mới (bộ đếm turn reset, ngữ cảnh sạch), không phải `FollowUp`. Bước tiếp theo cụ thể do Flow Router suy ra từ tầng dữ liệu thực tế sau TurnEnd đầu tiên.

### 8.3 Can thiệp người dùng

| Điểm vào | Tiền tố | Ngữ nghĩa | Triển khai |
|---|---|---|---|
| `Steer(text)` | `[Người dùng can thiệp]` | Sửa đổi/truy vấn, cần Điều phối viên phán quyết | Khi đang chạy dùng `Inject`; khi dừng máy ghi PendingSteer vào `meta/run.json` |
| `Continue(text)` | `[Người dùng can thiệp]` | Tiếp tục viết, đánh thức sau khi dừng máy | Khi đang chạy dùng `FollowUp`; khi dừng máy dùng `Inject` tự động khôi phục run |

Hai điểm vào thống nhất qua helper `interventionMsg` thêm tiền tố `[Người dùng can thiệp]` — đây là neo cho phân loại can thiệp trong `coordinator.md`; trước đây Continue gửi văn bản trần sẽ bỏ qua phân loại, bị phái nhầm writer sửa chương đã viết (đã sửa).

Ngữ nghĩa `Inject`: khi đang chạy chen vào hàng đợi run hiện tại; khi rảnh tự động khôi phục run và inject; khi tạm dừng xếp hàng chờ khôi phục.

**Lớp lưu trữ của can thiệp dài hạn**: "Yêu cầu dài hạn chỉ ảnh hưởng đến viết lách tiếp theo" trong phân loại can thiệp (loại phong cách/khuynh hướng) được Điều phối viên gọi `save_directive` ghi xuống `meta/user_directives.json` (tối đa 20 mục, add khử trùng lặp / remove theo số thứ tự), `novel_context` inject vào `working_memory.user_directives` — mọi agent phụ đều tự động nhìn thấy mỗi chương, có hiệu lực xuyên nén, xuyên khởi động lại, không phụ thuộc vào ký ức hội thoại và phân phối lệnh của Điều phối viên. Ba loại can thiệp còn lại bản thân đã ghi Store (độ dài → compass/outline, thiết lập → foundation, sửa chương cũ → PendingRewrites). Đi phong bì, không đi system prompt: bảo vệ cache tiền tố system xuyên chương của writer.

Mỗi chỉ thị khi ghi kèm **snapshot tiến độ lúc hạ lệnh** (at_chapter / at_total_chapters): chỉ thị có hiệu lực hướng về phía sau từ at_chapter (editor không truy hồi chương cũ); nếu chỉ thị tương đối ("tăng 10 chương") bị lưu nhầm thành yêu cầu dài hạn, người đọc có thể dựa snapshot phán định đã thỏa mãn chứ không thực thi lại. Đường chính của chỉ thị hành động vẫn là dịch thuật khi ghi theo route tương ứng (architect/editor → trạng thái tuyệt đối của đề cương/compass/PendingRewrites), snapshot là bảo hiểm khi phân loại sai.

---

## 9. Cấu trúc thư mục

```
internal/
  domain/         Dữ liệu thuần túy: Phase / FlowState / Progress / Checkpoint / Scope / Story / Plan /
                  Review / StateChange / Quy tắc chuyển tiếp Phase-Flow
  store/          Lưu trữ hệ thống file (tmp+rename + bộ ba): progress / checkpoints / outline /
                  drafts / summaries / characters / world / signals / run_meta / runtime / session
  tools/          11 công cụ Agent, loại ghi đều có bộ ba nguyên tử + digest idempotent + ConcurrencySafe=false
                  + premise_structure (dùng nội bộ trong save_foundation) + ask_user
  agents/         build.go lắp ráp Điều phối viên + ba agent phụ; ctxpack/ chiến lược nén ngữ cảnh Writer
  host/           host.go + resume.go + observer.go + events.go + usage.go + usage_replay.go
                  + stream_extract.go + cocreate.go
    flow/         router.go (hàm thuần túy 11 nhánh) + state.go + dispatcher.go + router_test.go
    reminder/     stop_guard.go (Điều phối viên) + subagent_guards.go (CheckpointDeltaGuard ×3)
    imp/          Nhập phân tích ngược tiểu thuyết ngoài: split → foundation → phân tích từng chương
    exp/          Xuất các chương đã hoàn thành: ghép chương → TXT / EPUB 3, hậu tố đường dẫn điều khiển; chỉ đọc thuần, không phụ thuộc LLM
  entry/          tui (Bubble Tea) / chế độ không giao diện / startup
  bootstrap/      config + ModelSet + provider failover + wizard thiết lập
  models/         Danh bạ mô hình công khai OpenRouter v.v. + làm mới giá (cache đĩa 24h)
  errs/           Phân tầng lỗi
  diag/           Module chẩn đoán chỉ đọc đăng ký luồng sự kiện host
  utils/          Di tích kiến trúc cũ (một ít công cụ phân tích, code mới không nên phụ thuộc)

assets/
  prompts/        coordinator (~55 dòng) / architect-short|long / writer / editor / import-* / simulation-*
  references/     Kỹ thuật viết + mẫu thể loại + lập kế hoạch tiểu thuyết dài v.v.
  styles/         mặc định/fantasy/romance/suspense

../agentcore     Framework Agent dùng chung (thư mục anh em go.work, có thể thêm khả năng chung, không thêm nghiệp vụ)
../litellm       Gateway LLM
```

### 9.1 Các mốc tiến hóa

| Thời gian | Tái cấu trúc | Hiệu quả ròng |
|---|---|---|
| 2026-04-10 | `internal/orchestrator/` (6342 dòng) → `host/` + `agents/` | Lõi runtime -74% |
| 2026-04-20 | Hybrid Coordinator: tạo mới `host/flow/`, `reminder/` gọn lại, `coordinator.md` 88 dòng → 45 dòng | Tỉ lệ lỗi định tuyến tiệm cận 0 |
| 2026-05-02 | agentcore `WithMaxToolErrors(0)` + `isReasoningOnlyStopAssistant`; `StreamIdleTimeout=5min`; xóa patch tiếp tục chạy `idleResumeCount` | mimo / streaming suy nghĩ chậm chạy thông |
| 2026-06-05 | Vòng kín kế hoạch cuộn (`expand_arc`/`append_volume`) + `/import` phân tích ngược phân tầng tiếp tục viết + can thiệp độ dài người dùng | 200+ chương chạy thông lần đầu |

Thực đo: hy3-preview free 12 chương / 73 phút, mimo-v2.5-pro 10 chương / 84.000 chữ (trung bình chương 8400), đều chạy xong một lần; tiểu thuyết dài gpt-5.4 《凡骨》 235 chương / 1.270.000 chữ / trung bình chương 5407, vòng kín kế hoạch cuộn chạy thông.

---

## 10. Những việc rõ ràng không làm

Vi phạm tức là kiến trúc đã lệch hướng.

1. **Không đưa vào khái niệm Task / Job / WorkItem**. "Nhiệm vụ hiện tại" hiển thị trên UI là chiếu từ luồng sự kiện, không phải dữ liệu thực tế.
2. **Không đưa vào Dispatcher / Scheduler / Ready Evaluator**. Quyền quyết định nằm ở Coordinator LLM và tầng công cụ.
3. **Không làm cơ chế "tiếp tục chạy khi rảnh" dạng `idle_dispatch`**. Coordinator Run kết thúc = Host phát done.
4. **Không để Host bỏ qua Điều phối viên gọi trực tiếp SubAgent**. Flow Router thông qua `coordinator.FollowUp` để Điều phối viên tạo tool_call. Resume dùng `Prompt` khởi chạy Run mới.
5. **Không thêm patch tự động tiếp tục chạy ở Host khi LLM dừng bất thường**. Run kết thúc = Host vào trạng thái cuối. `idleResumeCount` trước đây đã bị xóa (xem §7.3, `feedback_no_host_resilience.md`).
6. **Không suy ra nhiệm vụ hoàn thành dựa trên "tool exec end"**. Bằng chứng duy nhất của hoàn thành là checkpoint được ghi vào.
7. **Không làm mô hình bốn tầng WorkflowInstance / TaskInstance / Command + Apply v.v.**. Tầng dữ liệu thực tế chỉ có ba loại Progress + Checkpoint + Artifact.
8. **Không hỗ trợ task song song**. Một Coordinator Run hoạt động duy nhất, một cuốn sách tiến hành tuần tự. Nhiều tiểu thuyết hãy dùng nhiều tiến trình.
9. **Không thực hiện gọi LLM ở tầng công cụ** (ngoại trừ bản thân công cụ Agent). Chỉ thuần IO + kiểm tra + idempotent.
10. **Không để UI đọc trực tiếp Store**. Chỉ được đăng ký sự kiện hoặc đọc `Snapshot()` của Host.
11. **Không dùng file tín hiệu làm IPC**. Host đọc thẳng Progress + Checkpoint + đề cương phân tầng, `flow.Route` suy ra chỉ thị từ dữ liệu thực tế là định tuyến chuyên ngành hợp lý.
12. **Không viết state machine Flow ở phía Host**. Nhãn Flow chỉ được cập nhật bởi công cụ, Router chỉ đọc không ghi.
13. **Không viết hard-code dự phòng cho "ảo giác LLM"**. Tối ưu prompt, cải thiện cấu trúc giá trị trả về của công cụ, làm `novel_context` trình bày dữ liệu thực tế rõ hơn — thay vì Host cưỡng chế thay đổi quy trình.
14. **Không để diag / tầng quan sát can thiệp luồng điều khiển**. Chẩn đoán chỉ đọc, chỉ tạo Finding và xuất khử nhạy cảm; tự động sửa / tiếp tục chạy / thay đổi quy trình đều không làm (xem §2.3 kỷ luật quan sát).
15. **Ngân sách và cảnh báo không vào Route/tầng công cụ, cảnh báo không vào luồng điều khiển**. `BudgetSentinel` là thành phần chính sách Host (thực thi Abort người dùng đã ký trước, không đánh giá hành vi mô hình); `notify` là thuần quan sát (không thử lại, không thay đổi phát, không dừng máy). `flow.Route` giữ là hàm thuần túy, không nhận thức về cả hai.

---

## 11. Chiến lược xác minh

### 11.1 Kịch bản ổn định

- **A Chạy dài**: 80~200 chương chạy xong một lần, Phase=complete. Cho phép provider failover, tools transient thử lại; cấm Host tiếp tục chạy hoặc Điều phối viên chạy nhiều lần Run.
- **B Khôi phục crash**: Kill tiến trình sau draft chương N / trước commit → Resume → tiếp tục từ consistency_check, không viết lại bản nháp đã ghi xuống. `checkpoints.jsonl` không có bước trùng lặp.
- **C Provider bị nhiễu**: Mô phỏng 503 gián đoạn → litellm failover; vòng lặp chính LLM không nhận thức.
- **D Can thiệp người dùng**: Steer khi đang chạy → Điều phối viên xử lý ở turn tiếp theo; Steer khi dừng máy → resume prompt lần sau bao gồm.

### 11.2 Tuân thủ (có thể viết thành linter / test)

- `internal/host/` không được phép `import "internal/scheduler"` hay các package lập lịch tương tự
- Số lượng API vòng đời trong `host.go` ổn định; phương thức công khai mới thêm chỉ được là loại "điểm vào mở rộng" (đồng sáng tác/nhập/quản lý mô hình)
- Trong thân hàm `waitDone` không được phép có `coordinator.Inject` / `FollowUp` / `Prompt`
- Code liên quan `recovery` chỉ được xuất hiện trong `host/resume.go`
- `flow.Route` phải là hàm thuần túy: cấm đọc Store / bất kỳ IO nào

### 11.3 Cải tiến chất lượng

Sửa `writer.md` ngay lập tức tạo ra thay đổi phong cách; thêm tiêu chí đánh giá editor mới tương thích ngược (save_review nhận JSON có cấu trúc). Thêm một file md tài liệu tham khảo mới cần nối ba chỗ (`tools.References` field + `loadReferences` trong `assets/load.go` + inject `writerReferences`/`architectReferences` trong `novel_context.go`), không phải đặt vào thư mục là tự động tải — `References` là ánh xạ field tường minh, thuận tiện cắt giảm theo vai trò/chương.

**Thống kê phong cách toàn sách (`internal/stylestat`)**: Cửa sổ đánh giá trong cung tự nhiên mù quáng với các vấn đề cố định cấp toàn sách như "tic câu trung bình vài chục lần mỗi chương, hình thái cuối chương đồng cấu, sao chép từng chữ xuyên chương" — xem từng chương một thì mỗi chỗ đều bình thường. `novel_context` chạy thống kê xác định trên toàn bộ chương đã hoàn thành (loại mẫu câu / cụm từ tần suất cao trong cửa sổ gần / câu lặp xuyên chương / hình thái cuối chương / tiêu đề dùng hỗn hợp format), inject vào `episodic_memory.style_stats`: editor phán quyết theo số liệu ở tiêu chí aesthetic, writer dựa đó tự tránh. **Thống kê thuộc code, phán quyết thuộc LLM** — ngưỡng không hard-code trong code, số liệu có thành vấn đề hay không do mô hình phán quyết theo thể loại. Song song với đó, `rules.Lint` là đáy sản phẩm (markdown còn sót / đoạn phi tiếng Trung) luôn thực thi trong commit_chapter, chỉ trả về dữ liệu thực tế.

---

## 12. Tóm tắt

> **Để LLM hoàn thành một cuốn tiểu thuyết trong một lần Run, Host chỉ đảm nhận khởi động / khôi phục / định tuyến / quan sát, ghi chép dữ liệu thực tế được công cụ ghi xuống nguyên tử, quyền quyết định được giữ lại tối đa cho mô hình.**

Không có workflow engine, không có task queue, không có Dispatcher, không có scheduler. Chỉ có:

- Một Điều phối viên 100_000 turn
- Ba loại agent phụ chức năng (ngữ cảnh và mô hình độc lập)
- 11 công cụ nguyên tử
- Một file checkpoint jsonl
- Vỏ Host ~860 dòng
- Hàm thuần túy Flow Router ~150 dòng (11 nhánh + unit test)

Mỗi dòng code nghiệp vụ Host là một cược đối chọi với việc nâng cấp mô hình. **Host tối giản, Prompt (tầng chất lượng) tối đa, Công cụ mạnh nhất** khiến kiến trúc tự động tốt hơn mỗi năm — Điều phối viên quyết định chính xác hơn, Người viết viết tốt hơn, Biên tập viên đánh giá chính xác hơn, Kiến trúc sư lập kế hoạch tinh tế hơn, tất cả đều là lợi ích trực tiếp khi đổi mô hình mà kiến trúc không cần biết.

Ngược lại, hard-code trong Host các quy tắc như "lần review trước nói cần viết lại chương 3, 5" hay "liên tiếp 3 lần không tiến triển thì dừng máy", mô hình nâng cấp sẽ biến chúng thành **lợi ích âm**: phán quyết đáng lẽ LLM làm trở thành thừa, logic bảo vệ trở thành báo lỗi. **Tệ nhất là không ai dám xóa — xóa đi tức là "tin vào mô hình", gánh nặng tâm lý còn khó dọn hơn code**. Code kiểu này để lại càng nhiều, chi phí tái cấu trúc trong tương lai càng cao.

**Khả năng mở rộng đến từ điểm mở rộng đúng**: Đổi phong cách → sửa prompt; tiêu chí đánh giá mới → sửa prompt; thể loại mới → thêm tài liệu tham khảo; loại agent phụ mới → thêm một dòng SubAgentConfig; song song nhiều tiểu thuyết → nhiều tiến trình.

Kỷ luật duy nhất: **Khi có người muốn "làm Host thông minh hơn một chút", hãy hỏi trước "tại sao không làm LLM thông minh hơn một chút"**. Câu hỏi này không trả lời được lý do "Host phải làm", thì đừng thêm code vào Host.
