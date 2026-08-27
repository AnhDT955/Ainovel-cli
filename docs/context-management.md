# Tài liệu quản lý ngữ cảnh

Tài liệu này mô tả hệ thống quản lý ngữ cảnh hiện tại của `ainovel-cli`, bao gồm:

- Tại sao cần quản lý ngữ cảnh
- Ngữ cảnh đến từ đâu
- Cách nén, khôi phục, và bàn giao trong thời gian chạy
- Giá trị, điều kiện kích hoạt và phạm vi áp dụng của từng chiến lược
- Khi có sự cố, nên xem ở đâu trước

Mục tiêu không phải giới thiệu các khái niệm trừu tượng, mà để người bảo trì sau này mở tài liệu này ra là có thể hiểu ngay triển khai hiện tại và điểm đầu vào để gỡ lỗi.

## 1. Mục tiêu thiết kế

Quản lý ngữ cảnh trong dự án này không phải cho kịch bản chat thông thường, mà hướng đến kịch bản sáng tác tiểu thuyết. Nó cần giải quyết đồng thời nhiều vấn đề:

1. Hội thoại dài sẽ vượt quá cửa sổ ngữ cảnh của mô hình.
2. Sáng tác tiểu thuyết cần lưu giữ không phải "lịch sử chat", mà là bộ nhớ tường thuật có cấu trúc.
3. Người viết sau khi nén không được mất trạng thái nhân vật, phục bút, kế hoạch chương, ràng buộc phong cách, các mục biên tập chờ sửa.
4. Khi khôi phục việc viết, không thể giả định mô hình vẫn "nhớ những gì đã nói trước", phải ưu tiên dựa vào các sản phẩm đã lưu trữ.

Vì vậy chúng tôi áp dụng phương án "bộ nhớ phân lớp":

- Bộ nhớ ngắn hạn: phần đuôi các tin nhắn được giữ lại gần nhất
- Bộ nhớ trung hạn: `ContextSummary` được tạo ra từ quá trình nén
- Bộ nhớ dài hạn: các sản phẩm có cấu trúc trong Store của dự án
- Bộ nhớ khôi phục: handoff / restore pack / novel_context

## 2. Kiến trúc tổng thể

### 2.1 Các lớp chính

Quản lý ngữ cảnh hiện tại được chia thành bốn lớp:

1. `agentcore/context`
   Chịu trách nhiệm về ngân sách ngữ cảnh chung, pipeline chiến lược, khung nén/khôi phục.

2. `internal/tools/novel_context`
   Chịu trách nhiệm lắp ráp dữ liệu có cấu trúc từ dự án tiểu thuyết thành ngữ cảnh khả dụng cho vòng hiện tại.

3. `internal/orchestrator/store_summary_*`
   Chịu trách nhiệm nén nhanh dựa trên Store chuyên dùng cho Người viết.

4. `internal/orchestrator/writer_restore.go`
   Chịu trách nhiệm nối thêm một gói khôi phục sau `FullSummary`, đảm bảo Người viết có thể tiếp tục viết.

### 2.2 Luồng dữ liệu

Trong thời gian chạy có hai đường ngữ cảnh chính:

1. Đường làm việc bình thường
   - Agent gọi `novel_context`
   - `novel_context` đọc tóm tắt chương, kế hoạch, nhân vật, dòng thời gian, v.v. từ Store
   - Các dữ liệu này đi vào prompt của vòng hiện tại

2. Đường ngữ cảnh quá dài
   - `ContextManager` phát hiện áp lực token
   - Nén theo thứ tự chiến lược
   - Ưu tiên thử nén nhẹ và nén dựa trên Store
   - Chỉ dùng `FullSummary` bằng LLM khi vẫn chưa đủ
   - Sau `FullSummary` thì tiêm restore pack

## 3. Các file quan trọng

### 3.1 Engine ngữ cảnh chung

- `../agentcore/context/strategy.go`
- `../agentcore/context/engine.go`
- `../agentcore/context/strategy_tool.go`
- `../agentcore/context/strategy_trim.go`
- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/message.go`
- `../agentcore/context/summary_run.go`

Vai trò:

- Định nghĩa `Strategy` / `ForceCompactionStrategy`
- Thực thi chuỗi chiến lược dựa trên ngân sách
- Biểu diễn `ContextSummary` và chuyển đổi qua LLM
- Nén tóm tắt bằng LLM cho `FullSummary`

### 3.2 Kết nối phía dự án

- `internal/orchestrator/agents.go`

Vai trò:

- Lắp ráp `ContextManager` cho Người viết / Điều phối viên
- Tiêm `StoreSummaryCompact` bổ sung cho Người viết
- Cấu hình prompt `FullSummary` tùy chỉnh cho tiểu thuyết cho Người viết
- Cấu hình `writerRestorePack` cho Người viết

### 3.3 Nén và khôi phục phía dự án

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/writer_restore.go`

Vai trò:

- Trước khi tóm tắt bằng LLM, ưu tiên dùng dữ liệu Store để nén nhanh
- Xây dựng thống nhất ngữ cảnh có cấu trúc cần thiết cho việc nén và khôi phục Người viết
- Nối thêm một restore message thuần bộ nhớ sau `FullSummary`

### 3.4 Lắp ráp ngữ cảnh có cấu trúc

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`
- `internal/domain/runtime.go`

Vai trò:

- Định nghĩa `ContextProfile` / `MemoryPolicy`
- Quyết định tải bao nhiêu tóm tắt chương, bao nhiêu dòng thời gian, có bật tóm tắt phân lớp không
- Lắp ráp chương, nhân vật, phục bút, dòng thời gian, kinh nghiệm biên tập, v.v. từ Store ra

### 3.5 Bàn giao và khôi phục

- `internal/orchestrator/handoff_policy.go`
- `internal/orchestrator/recovery_engine.go`

Vai trò:

- Trong các giai đoạn viết dài / làm lại / xem lại, ưu tiên dựa vào handoff
- Khi khôi phục, ghép gói bàn giao có cấu trúc vào prompt

### 3.6 Khả năng quan sát

- `internal/orchestrator/run.go`
- `internal/orchestrator/runtime.go`
- `internal/entry/tui/panels.go`

Vai trò:

- Ghi lại sự kiện ghi lại ngữ cảnh
- Xuất tên chiến lược, thay đổi token, số tin nhắn được giữ lại
- Cho phép TUI thấy ngữ cảnh hiện tại là `projected` hay `compacted`

## 4. ContextManager được lắp ráp như thế nào

Người viết và Điều phối viên đều dùng `newContextManager`, nhưng cấu hình khác nhau.

Các tham số quan trọng của `contextManagerConfig` hiện tại:

- `ContextWindow`
  Tổng cửa sổ ngữ cảnh của mô hình.

- `ReserveTokens`
  Token dự phòng cho đầu ra của mô hình.

- `KeepRecentTokens`
  Ngân sách phần đuôi tin nhắn gần nhất được giữ lại khi nén.

- `ToolMicrocompact`
  Cấu hình nén vi mô kết quả công cụ.

- `ExtraStrategies`
  Các chiến lược nén bổ sung phía dự án. Hiện tại Người viết dùng để gắn `StoreSummaryCompact`.

- `Summary`
  Cấu hình `FullSummary`, bao gồm prompt tùy chỉnh và post-summary hook.

Giá trị cấu hình thực tế hiện tại:

| Tham số | Người viết | Điều phối viên |
|---------|-----------|----------------|
| ReserveTokens | 16.384 | 32.000 |
| KeepRecentTokens | 20.000 | 30.000 |
| CommitOnProject | false | true |
| IdleThreshold | 5 phút | Không có |
| ExtraStrategies | StoreSummaryCompact | Không có |
| Prompt Summary tùy chỉnh | Phiên bản tường thuật tiểu thuyết | Mặc định (phiên bản trợ lý code) |

Ngưỡng kích hoạt nén = `ContextWindow - ReserveTokens`. Ví dụ với cửa sổ 128K, Người viết kích hoạt ở ~112K, Điều phối viên ở ~96K.

Thứ tự pipeline chiến lược hiện tại của Người viết:

1. `ToolResultMicrocompact`
2. `LightTrim`
3. `StoreSummaryCompact`
4. `FullSummary`

Thứ tự này có ý nghĩa rõ ràng:

- Trước tiên dùng cách rẻ nhất để dọn nhiễu công cụ
- Rồi cắt bớt các khối văn bản quá dài
- Nếu dữ liệu Store đủ, thực hiện nén có cấu trúc không tốn LLM
- Cuối cùng mới lui về tóm tắt bằng LLM

## 5. Vai trò của từng chiến lược

### 5.1 ToolResultMicrocompact

Vị trí triển khai:

- `../agentcore/context/strategy_tool.go`

Vai trò:

- Dọn dẹp các `tool_result` trong lịch sử
- Thay thế kết quả công cụ cũ bằng văn bản placeholder ngắn gọn

Giá trị:

- Nội dung trả về của công cụ thường có kích thước lớn nhưng mật độ thông tin thấp
- Nhiều kết quả công cụ cũ chỉ là "nhiễu quá trình", không phải bộ nhớ tiểu thuyết

Đặc điểm cấu hình hiện tại của Người viết:

- Đặt `IdleThreshold = 5m`

Điều này có nghĩa:

- Nếu tin nhắn assistant gần nhất đã ở trạng thái nhàn rỗi quá ngưỡng
- Sẽ giảm mạnh hơn số kết quả công cụ cũ được giữ lại

Phạm vi áp dụng:

- Nhiều vòng `novel_context`
- Sau nhiều vòng công cụ read / check / draft

### 5.2 LightTrim

Vị trí triển khai:

- `../agentcore/context/strategy_trim.go`

Vai trò:

- Cắt bớt các khối văn bản rất dài
- Giữ lại đầu và đuôi, thay thế phần giữa bằng placeholder

Giá trị:

- Giữ nguyên cấu trúc tin nhắn
- Chi phí thấp
- Rất phù hợp để xử lý nội dung chương quá dài hoặc đầu ra có đoạn lớn

Phạm vi áp dụng:

- Khi một tin nhắn đơn quá dài nhưng chưa cần tóm tắt toàn bộ lịch sử

### 5.3 StoreSummaryCompact

Vị trí triển khai:

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`

Vai trò:

- Khi ngữ cảnh Người viết quá dài
- Ưu tiên dùng bộ nhớ có cấu trúc từ Store lưu trữ để thay thế các tin nhắn cũ
- Không gọi LLM

Đây không phải tóm tắt hội thoại, mà là "thay thế bộ nhớ có cấu trúc".

Dữ liệu cốt lõi hiện được giữ lại bao gồm:

- Tiến độ hiện tại
- Tóm tắt chương gần nhất
- Kế hoạch chương hiện tại
- Đề cương chương hiện tại
- Tóm tắt cung truyện hiện tại
- Tóm tắt tập hiện tại
- Snapshot nhân vật
- Phục bút đang hoạt động
- Các vấn đề biên tập chờ sửa
- Dòng thời gian gần nhất
- Quy tắc phong cách

Điều kiện kích hoạt:

- Chương hiện tại lớn hơn 1
- Trong Store đã có đủ tóm tắt lịch sử
- Và chương hiện tại có ít nhất dữ liệu trạng thái làm việc
  - `chapter_plan` hoặc `current_outline`

Giá trị:

- Giảm số lần nén bằng LLM
- Tránh thông tin quan trọng của tiểu thuyết bị trôi dạt khi tóm tắt
- Để bộ nhớ dài hạn ưu tiên dựa vào dữ kiện đã lưu, không phải lịch sử chat

Tại sao chỉ dùng cho Người viết:

- Đây là chiến lược nghiệp vụ tiểu thuyết, không phải chiến lược framework chung
- Điều phối viên / Biên tập viên có chế độ ngữ cảnh khác
- Xác thực trên Người viết trước — nơi cần nhất bộ nhớ sáng tác liên tục

### 5.4 FullSummary

Vị trí triển khai:

- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/summary_run.go`

Vai trò:

- Khi các lớp trên vẫn chưa đủ, dùng mô hình tạo `ContextSummary`
- Giữ lại phần đuôi tin nhắn gần nhất
- Biến ngữ cảnh cũ hơn thành một điểm khôi phục có cấu trúc

Điểm khác biệt của Người viết so với trợ lý code mặc định:

- Người viết dùng prompt tóm tắt tùy chỉnh
- Nội dung tóm tắt yêu cầu rõ ràng phải giữ lại:
  - Tiến độ hiện tại
  - Trạng thái tức thời của nhân vật
  - Phục bút và manh mối đang hoạt động
  - Phản hồi biên tập và các mục chờ sửa
  - Phong cách và nhịp truyện
  - Các quyết định quan trọng
  - Bước tiếp theo
  - Ngữ cảnh then chốt

Giá trị:

- Là chiến lược phòng thủ cuối cùng
- Dù dữ liệu Store không đủ, vẫn có thể duy trì tính liên tục qua LLM

### 5.5 Cầu dao ngắt mạch (Circuit Breaker)

Vị trí triển khai:

- `../agentcore/context/engine.go`

Vai trò:

- Khi nén liên tiếp thất bại đạt ngưỡng (mặc định 3 lần), bỏ qua nén cho vòng hiện tại
- Khi bỏ qua vẫn phát ra `RewriteEvent` (`Reason = "circuit_breaker"`)
- TUI sẽ hiển thị scope là "Cầu dao ngắt"
- Áp dụng chế độ bán mở: bỏ qua một vòng rồi thử lại lần sau, thành công thì reset, thất bại lại thì bỏ qua lại

Tại sao cần:

- Tóm tắt bằng LLM có thể liên tiếp thất bại do mạng, mô hình từ chối, v.v.
- Không có cầu dao ngắt, mỗi vòng Project sẽ thử rồi thất bại, lãng phí lời gọi API
- Trong phiên viết dài, sự lãng phí này sẽ tích lũy

Gỡ lỗi:

- Nếu TUI liên tục hiển thị "Cầu dao ngắt", nghĩa là đường tóm tắt LLM có vấn đề
- Kiểm tra sự kiện ghi lại ngữ cảnh có `reason=circuit_breaker` trong slog
- Cầu dao ngắt không ảnh hưởng đến `StoreSummaryCompact` (nó không gọi LLM)

### 5.6 Ước tính token (nhận biết CJK)

Vị trí triển khai:

- `../agentcore/context/usage.go`

Vai trò:

- Tất cả kiểm soát ngân sách, thời điểm kích hoạt nén đều phụ thuộc ước tính token
- `estimateTextTokens` tự động phát hiện xem văn bản có chủ yếu là ký tự CJK không
- Văn bản chủ yếu CJK: `runes × 1.5`
- Văn bản chủ yếu ASCII: `bytes / 4`

Tại sao không thể dùng `bytes/4` chuẩn:

- Một ký tự tiếng Trung UTF-8 = 3 bytes
- `bytes/4` sẽ ước tính một ký tự tiếng Trung là 0,75 token, thực tế khoảng 1,5 token
- Ước tính thấp hơn 2 lần sẽ khiến nén kích hoạt bị trễ nghiêm trọng

Phạm vi ảnh hưởng:

- `EstimateTokens` (một tin nhắn đơn)
- `EstimateTotal` (danh sách tin nhắn)
- `EstimateContextTokens` (ước tính hỗn hợp: Usage LLM báo cáo + ước tính tin nhắn đuôi)
- Cắt ngân sách trong `store_summary_builder.go`

Lưu ý: args của ToolCall là JSON (chủ yếu ASCII), vẫn dùng `bytes/4`, không bị ảnh hưởng bởi điều chỉnh CJK.

## 6. Tại sao Người viết có hai bộ "bộ nhớ sau nén"

Hiện tại Người viết có hai đường trông có vẻ tương tự nhưng trách nhiệm khác nhau:

### 6.1 StoreSummaryCompact

Trách nhiệm:

- Trực tiếp thay thế các tin nhắn cũ trong quá trình nén

Đặc điểm:

- Xảy ra trước `FullSummary`
- Không dùng LLM
- Dùng Store để thay thế lịch sử cũ hơn

### 6.2 writerRestorePack

Vị trí triển khai:

- `internal/orchestrator/writer_restore.go`

Trách nhiệm:

- Nối thêm một restore message sau `FullSummary`

Đặc điểm:

- Xảy ra sau khi LLM nén xong
- Được tiêm qua `PostSummaryHook`
- Dùng để bổ sung thông tin có cấu trúc mà Người viết cần thấy khi khôi phục tiếp tục sáng tác

Tại sao cần cả hai:

- `StoreSummaryCompact` không phải lúc nào cũng khớp
  - Ví dụ chương đầu tiên hoặc khi dữ liệu Store chưa đủ
- `FullSummary` dù tốt đến đâu cũng có thể bỏ sót thông tin chính xác trong Store
- Vì vậy restore pack là lớp bảo hiểm cuối cùng

Hiện tại cả hai đã dùng chung `store_summary_builder.go`, tránh sự phân kỳ về khẩu径.

## 7. Vai trò của novel_context

Vị trí triển khai:

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`

`novel_context` không phải chiến lược nén, nó là "bộ lắp ráp ngữ cảnh có cấu trúc" trong thời gian chạy.

Nó chia dữ liệu trong Store thành các loại:

- `working_memory`
  - Kế hoạch chương hiện tại
  - Đề cương chương hiện tại
  - Tóm tắt chương gần nhất
  - Dòng thời gian
  - Điểm khôi phục
  - Previous tail

- `episodic_memory`
  - Trạng thái nhân vật
  - Trạng thái quan hệ
  - Các thay đổi trạng thái gần nhất
  - Phục bút

- `reference_pack`
  - Dữ liệu thiết lập và tham chiếu ổn định hơn

- `selected_memory`
  - Một lượng nhỏ bộ nhớ quan trọng được chọn theo nhiệm vụ hiện tại

Giá trị:

- Nó quyết định ngữ cảnh tiểu thuyết có cấu trúc thực sự "được nạp cho mô hình" trong mỗi vòng
- `StoreSummaryCompact` không gọi nó trực tiếp, nhưng tái sử dụng cùng nguồn dữ liệu và cách tiếp cận lắp ráp

## 8. ContextProfile và MemoryPolicy

Vị trí triển khai:

- `internal/domain/runtime.go`

### 8.1 ContextProfile

Vai trò:

- Quyết định kích thước cửa sổ tải dựa trên tổng số chương

Quy tắc hiện tại:

- `<= 15` chương
  - `10` tóm tắt chương gần nhất
  - `10` dòng thời gian gần nhất

- `<= 50` chương
  - `5` tóm tắt chương gần nhất
  - `8` dòng thời gian gần nhất

- `> 50` chương
  - `3` tóm tắt chương gần nhất
  - `5` dòng thời gian gần nhất
  - Bật tóm tắt phân lớp

Giá trị:

- Kiểm soát quy mô ngữ cảnh
- Tránh nhét toàn bộ lịch sử vào prompt khi viết dài

### 8.2 MemoryPolicy

Vai trò:

- Ghi rõ chiến lược sử dụng ngữ cảnh hiện tại
- Cung cấp cho đầu ra của `novel_context`
- Cung cấp cho logic handoff / reminder / chẩn đoán

Các trường quan trọng:

- `SummaryWindow`
- `TimelineWindow`
- `LayeredSummaries`
- `SummaryStrategy`
- `HandoffPreferred`
- `ReadOnlyThreshold`

Giá trị:

- Biến "hệ thống hiện tại nên sử dụng bộ nhớ như thế nào" từ logic ngầm thành chiến lược thời gian chạy tường minh

## 9. Vai trò của handoff

Vị trí triển khai:

- `internal/orchestrator/handoff_policy.go`

Khi tác phẩm bước vào giai đoạn dài hơn, phức tạp hơn, phụ thuộc nhiều hơn vào các sản phẩm có cấu trúc, hệ thống sẽ nghiêng về handoff.

Gói handoff sẽ ghi lại:

- Giai đoạn hiện tại và flow
- Vị trí chương tiếp theo
- Lần lưu chương gần nhất
- Lần xem lại gần nhất
- Tóm tắt gần nhất
- Memory policy hiện tại
- Hướng dẫn khôi phục

Giá trị:

- Khi gián đoạn khôi phục không phụ thuộc vào lịch sử chat
- Trong các kịch bản làm lại, xem lại, viết dài, ưu tiên dựa vào sản phẩm có cấu trúc

## 10. Khả năng quan sát và gỡ lỗi

### 10.1 Sự kiện ghi lại ngữ cảnh

Vị trí triển khai:

- `internal/orchestrator/run.go`

Mỗi lần ghi lại ngữ cảnh sẽ xuất ra qua `contextRewriteCallback`:

- `reason`
- `strategy`
- `committed`
- `tokens_before`
- `tokens_after`
- `messages_before`
- `messages_after`
- `compacted_count`
- `kept_count`
- `split_turn`
- `incremental`
- `summary_runes`
- `duration_ms`

Điều này sẽ đồng thời đi vào:

- `slog`
- Hàng đợi runtime boundary
- Sự kiện `COMPACT` của TUI

### 10.2 Có thể thấy gì trong TUI

TUI sẽ hiển thị:

- Token ngữ cảnh hiện tại (với màu chuyển sắc theo tình trạng sức khỏe)
- Context window
- Scope ngữ cảnh hiện tại (bao gồm "Cầu dao ngắt")
- Tên chiến lược cuối cùng hiện tại
- Số lượng summary

Ý nghĩa màu sắc phần trăm ngữ cảnh (triển khai trong `internal/entry/tui/layout.go`):

| Màu sắc | Điều kiện | Ý nghĩa |
|---------|-----------|---------|
| Xanh lá | < 70% | Dồi dào, cách xa ngưỡng nén |
| Vàng | 70-85% | Gần đến ngưỡng nén |
| Đỏ | > 85% | Sắp hoặc đang nén |

Nhãn tiếng Việt của Scope:

| Scope | Hiển thị | Ý nghĩa |
|-------|----------|---------|
| baseline | Cơ sở | Trạng thái bình thường |
| projected | Chiếu | Xem trước nén tạm thời |
| compacted | Đã nén | Nén đã có hiệu lực |
| recovered | Đã khôi phục | Khôi phục sau tràn |
| skipped | Cầu dao ngắt | Nén bị cầu dao ngắt bỏ qua |

Giá trị:

- Có thể nhanh chóng đánh giá tình trạng sức khỏe ngữ cảnh hiện tại
- Khi vàng/đỏ có thể dự đoán nén sắp xảy ra
- Thấy "Cầu dao ngắt" nghĩa là đường tóm tắt LLM có vấn đề

### 10.3 Khi có sự cố nên xem ở đâu trước

#### Tình huống 1: Người viết mất kế hoạch chương sau khi nén

Xem trước:

- `novel_context` có tiêm ổn định `chapter_plan` không
- `store_summary_builder.go` có lấy được `chapterPlan` không
- `writerRestorePack` có được làm mới không

File trọng tâm:

- `internal/tools/novel_context_builders.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/session.go`

#### Tình huống 2: Mất trạng thái nhân vật/phục bút sau khi nén

Xem trước:

- `LoadLatestSnapshots`
- `LoadActiveForeshadow`
- `store_summary_builder.go`
- Prompt tóm tắt Người viết có bị ghi đè không

#### Tình huống 3: Nén thường xuyên nhưng không khớp store_summary

Xem trước:

- Chương hiện tại có phải `<= 1` không
- Đã có recent summaries / arc / volume summary chưa
- Có tồn tại `chapter_plan` hoặc `current_outline` không
- `writer.Context.Strategy` cuối cùng ghi lại có phải `full_summary` không

#### Tình huống 4: Ngữ cảnh không đủ sau khi khôi phục

Xem trước:

- handoff có được tạo không
- restore pack có được làm mới không
- recovery prompt có tiêm handoff không

#### Tình huống 5: Kết quả công cụ quá nhiều khiến ngữ cảnh phình to

Xem trước:

- `ToolResultMicrocompact` có khớp không
- `IdleThreshold` có hiệu lực không

## 11. Các đánh đổi trong triển khai hiện tại

### Các hướng đã xác định rõ sẽ giữ

1. Không nhét logic nghiệp vụ tiểu thuyết vào `agentcore`
2. Ưu tiên dựa vào Store có cấu trúc, không phải lịch sử chat
3. Người viết dùng prompt tóm tắt tiểu thuyết chuyên biệt
4. Nén và khôi phục dùng chung builder nhất có thể, tránh phân kỳ khẩu径

### Các giới hạn hiện tại vẫn cố ý giữ lại

1. `StoreSummaryCompact` chỉ dùng cho Người viết
2. Chương đầu tiên không khớp store-based compact
3. Khi dữ liệu Store không đủ vẫn fallback về `FullSummary`
4. `writerRestorePack` là bù đắp nối thêm, không thay thế `FullSummary`

Những giới hạn này không phải khiếm khuyết, mà là ranh giới được đặt ra để kiểm soát độ phức tạp trong giai đoạn hiện tại.

## 12. Tóm tắt một câu

Quản lý ngữ cảnh của dự án này không đơn giản là "nén hội thoại dài thành ngắn", mà là:

`Ưu tiên dùng bộ nhớ tiểu thuyết có cấu trúc để duy trì tính liên tục, chỉ để LLM tóm tắt hội thoại khi thực sự cần thiết; và trong cả ba giai đoạn nén, khôi phục, bàn giao đều cố gắng tối đa dựa vào cùng một bộ sản phẩm lưu trữ.`

Nếu bạn muốn thay đổi hệ thống này sau này, hãy ưu tiên giữ ba điều sau:

1. Không để bộ nhớ quan trọng của Người viết lại chỉ phụ thuộc vào lịch sử chat.
2. Không để `store_summary` và `writer_restore` phân kỳ khẩu径.
3. Khi xuất hiện vấn đề về tính liên tục, trước tiên kiểm tra xem sản phẩm có cấu trúc có đi vào ngữ cảnh không, rồi mới quyết định có cần sửa prompt không.
