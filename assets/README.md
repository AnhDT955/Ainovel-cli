# Bản đồ nội dung thư mục assets

Trước khi thêm "một đoạn văn / một tài liệu / một quy tắc" vào hệ thống, hãy tra bảng dưới đây để xác định nơi đặt, rồi xem cách kết nối.

| Thư mục | Chứa gì | Ai sử dụng | Cách kết nối |
|---|---|---|---|
| `prompts/` | System prompt thường trú cho từng vai (coordinator / writer / editor / architect×2) và prompt tác vụ một lần (import×2 / simulation×2) | `agents/build.go` lắp ráp; imp / sim runner | Trường Prompts trong `load.go`. Lưu ý: simulation_guidance được inject khi `load.go` tải, không thấy trong file md |
| `references/` | Tài liệu kiến thức viết lách không phụ thuộc thể loại. Không đưa vào system prompt; được `novel_context` cắt tỉa theo vai / chương rồi inject vào `reference_pack` | writer / editor / architect | **Ba điểm kết nối**: thêm trường vào `tools.References` + `load.go` loadReferences đọc + `novel_context.go` writerReferences / architectReferences inject. Đặt vào thư mục không tự động tải |
| `references/genres/<style>/` | Kiến thức chuyên biệt theo thể loại (style-references / arc-templates) | Như trên, tải khi `style != default` | `load.go` loadReferences |
| `rules/` | Giá trị mặc định cho các quy tắc cơ học (số từ / từ cấm / từ sáo rỗng), code kiểm tra bắt buộc khi lưu chương | rules loader hợp nhất ba lớp: nội trang → `~/.ainovel/rules/` → `./.ainovel/rules/` của dự án | `rules/default.md`; định dạng tầng người dùng xem `rules.md.example` ở thư mục gốc. Chỉ đặt chuỗi cố định có độ dài xác định; mẫu có biến giao cho biên tập viên phán đoán ngữ nghĩa |
| `styles/<style>.md` | Chỉ dẫn phong cách viết theo thể loại | Ghép vào system prompt của **writer** (`agents/build.go`) | Tên file chính là giá trị `config.style`. Cùng khái niệm thể loại với `references/genres/<style>/` nhưng hai dạng tải khác nhau: cái trước là chỉ dẫn phong cách, cái sau là tài liệu kiến thức |

## Phán đoán phân loại nội dung mới (năm câu hỏi)

1. Quy trình này có cần được **bảo đảm** không? → Không viết vào prompt, viết ràng buộc trong code (StopAfterTools / tool guard / Flow Router)
2. Đây là tiêu chí phán quyết (khi nào điều phối ai)? → `prompts/coordinator.md`
3. Đây là tiêu chuẩn thẩm mỹ / thực thi của một vai? → `prompts/<role>.md`
4. Đây là quy tắc có thể liệt kê cơ học (từ cấm / số từ / ngưỡng)? → `rules/` (code cưỡng chế, không tốn token LLM)
5. Đây là tài liệu kiến thức viết lách? → `references/` (nhớ kết nối ba điểm)

## Đảm bảo tính nhất quán

Đường dẫn envelope mà prompt tham chiếu (`working_memory.*` v.v.) và tài liệu tham số commit_chapter của writer.md
được `prompts_consistency_test.go` kiểm tra tự động — hai loại trượt này không báo lỗi, chỉ làm mô hình âm thầm kém đi, cần đèn đỏ test mới phát hiện.
Đoạn quy trình trong prompt là "hướng dẫn sử dụng", sự thật quy trình nằm ở tầng code; khi hai bên lệch nhau thì lấy code làm chuẩn rồi quay lại sửa prompt.
