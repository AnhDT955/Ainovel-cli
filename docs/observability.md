# Sổ tay quan sát

Khi chạy tiểu thuyết dài tập, làm sao biết các cơ chế có đang hoạt động đúng không?

Tài liệu này không chép lại toàn bộ quy tắc diag, mà hướng đến **vận hành thực tế**: bạn đang chạy đến chương N, nên mở file nào, xem trường nào, đánh giá trạng thái khoẻ hay bất thường.

---

## 1. Quy trình chẩn đoán chung

```
1. /diag                       # Tự động chẩn đoán, xem phần Findings
2. cd output/{novel}/meta/     # Trực tiếp cat các sản phẩm chính
3. cat meta/sessions/coordinator.jsonl | tail  # Xem hành vi LLM vài vòng gần nhất
```

Những điều `/diag` không bao phủ được (bao gồm các mục "chẩn đoán chờ bổ sung" liệt kê trong tài liệu này), cần thực hiện bước 2-3 thủ công.

### Báo issue: xuất chẩn đoán đã ẩn danh

Mỗi lần `/diag` đều ghi thêm file `output/{novel}/meta/diag-export.md` — một bản chẩn đoán **đã ẩn danh** (nội dung tiểu thuyết / prompt / suy nghĩ đã được loại bỏ, chỉ giữ lại khung hành vi: tên tool, chuỗi lỗi, số lần lặp, phase/flow, bước bị kẹt, phân loại lỗi log). Khi gặp vòng lặp vô tận / sự cố gián đoạn, dán file này vào GitHub issue là đủ để maintainer định vị nguyên nhân, không cần dữ liệu `output/` của người dùng.

---

## 2. Bảng tra nhanh sản phẩm chính

Sắp xếp theo "đường chẩn đoán phổ biến nhất khi xảy ra sự cố":

| Sản phẩm | Đường dẫn | Xem gì | Khoẻ | Bất thường |
|---|---|---|---|---|
| Tiến độ | `meta/progress.json` | `phase` / `flow` / `completed_chapters` | phase tiến đơn điệu, flow nằm trong tập hợp hợp lệ | phase thụt lùi / flow kẹt ở một trạng thái |
| Chỉ nam | `meta/compass.json` | Khoảng cách giữa `last_updated` và chương mới nhất | gap < 15 chương | gap > 15 chương (CompassDrift kích hoạt) |
| Danh sách diễn viên phụ | `meta/cast_ledger.json` | Số mục / tỷ lệ điền brief_role / tính nhất quán tên | Xem §4 | Xem §4 |
| Sổ theo dõi phục bút | `meta/foreshadow.json` | Số chương trì hoãn dài nhất của `status="planted"` | < số chương/3 | > số chương/3 (StaleForeshadow kích hoạt) |
| Đề cương | `meta/layered_outline.json` | Số chương còn lại chưa viết trong tập hiện tại | Đã triển khai trước 1-2 chương | Đang viết chương hiện tại nhưng chương tiếp theo chưa có outline (OutlineExhausted) |
| Hồ sơ nhân vật | `meta/characters.json` | Có thể tìm thấy nhân vật core/important trong tóm tắt N chương gần nhất không | Đều tìm thấy | Vắng mặt (GhostCharacter kích hoạt) |
| Điểm khôi phục | `meta/checkpoints.jsonl` | `step` của dòng cuối cùng có khớp với progress không | Khớp | Không khớp (khôi phục sau crash chưa tự lành) |
| Phiên Điều phối viên | `meta/sessions/coordinator.jsonl` | Mẫu tool_call của 5-10 vòng gần nhất | Mỗi vòng tiến nhanh | Cùng một tool gọi rỗng nhiều lần (vòng lặp kẹt) |

---

## 3. Quan sát chỉ nam (compass)

**Thời gian sửa**: 2026-05-08 (commit `fix: update_compass 工具自动填 last_updated`)

### Xem gì

```bash
cat output/{novel}/meta/compass.json
```

Ngữ nghĩa các trường:
- `ending_direction`: hướng kết truyện (phải nhất quán với đoạn "hướng kết truyện" trong `premise.md`)
- `open_threads`: tuyến dài đang hoạt động (Kiến trúc sư thêm/xoá ở mỗi ranh giới tập)
- `estimated_scale`: quy mô dự tính (ví dụ "4-6 tập", cập nhật ở mỗi ranh giới tập)
- `last_updated`: **tool tự điền** bằng số chương đã hoàn thành lớn nhất tại thời điểm cập nhật (không còn phụ thuộc LLM tự điền)

### Đánh giá mức độ khoẻ

| Tín hiệu | Đánh giá |
|---|---|
| `last_updated` nằm trong khoảng `[latest-15, latest]` | Khoẻ |
| `last_updated` trễ hơn latest quá 15 chương | Kiến trúc sư chưa cập nhật ở ranh giới cung/tập — kiểm tra prompt architect-long.md |
| `last_updated == 0` | **Dữ liệu bẩn trước lần sửa này**, lần `update_compass` tiếp theo sẽ tự lành |
| `ending_direction` không khớp với đoạn "hướng kết truyện" trong premise.md | Kiến trúc sư đã ngầm thay đổi ý định người dùng — ghi lại, quyết định có cần đóng băng trường không (vấn đề thiết kế, xem todo.md) |

### Cách xác nhận bản sửa có hiệu lực

So sánh trước và sau khi chạy tiểu thuyết dài:
- **Trước khi sửa**: Chạy 30+ chương, `compass.last_updated` rất có thể là `0` hoặc một số chương đầu nào đó
- **Sau khi sửa**: Mỗi lần Kiến trúc sư gọi `update_compass`, `last_updated` đều bị tầng tool ghi đè bằng latest hiện tại

---

## 4. Quan sát danh sách diễn viên phụ (cast_ledger)

**Tính năng triển khai**: 2026-05-08 (commit `feat: 新增配角名册自动追踪次要角色`)

### Xem gì

```bash
cat output/{novel}/meta/cast_ledger.json | jq 'length'                     # Tổng số mục
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.brief_role == "" or .brief_role == null)] | length'  # Số mục thiếu brief_role
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.appearance_count >= 3)] | length'   # Số lần xuất hiện nhiều (≥3 lần)
cat output/{novel}/meta/cast_ledger.json | jq 'sort_by(-.appearance_count) | .[:10]'  # 10 nhân vật xuất hiện nhiều nhất
```

### Đánh giá mức độ khoẻ

| Chiều | Khoẻ | Bất thường | Xử lý |
|---|---|---|---|
| **Số mục vs số chương đã hoàn thành** | Số mục ledger ≈ số chương × 0.3-0.6 | > số chương × 0.8 (nhân vật thoáng qua bị nhập sai) | Kiểm tra đoạn `cast_intros` trong writer.md có đủ rõ không |
| **Tỷ lệ điền brief_role** | Thiếu < 30% | Thiếu > 50% | Người viết bỏ sót nhiều — hướng dẫn prompt chưa đủ |
| **Độ tương đồng tên trùng** | Không có nhân vật nghi vấn nhiều tên | Đồng thời xuất hiện "Li X" / "Lão Li" / "X chưởng quỹ" | LLM trôi dạt tên — thêm ràng buộc vào prompt "dùng tên nhất quán" hoặc thêm steer người dùng để gộp tool |
| **Nhân vật xuất hiện nhiều** | Mục có `appearance_count >= 5` ít | Nhiều mục xuất hiện cao tần xuyên cung truyện | Nên cân nhắc thăng cấp vào hồ sơ cốt lõi (kênh thăng cấp giai đoạn 3) |
| **Recall có được tiêu thụ không** | Khi Người viết viết đến nhân vật cũ, trường characters trong commit_chapter chứa tên đã có trong ledger | Người viết tái phát minh cùng một tên (xuất hiện "Lão Châu A" và "Lão Châu B") | recent_cast recall chưa được tiêu thụ — kiểm tra đoạn "tính liên tục diễn viên phụ" trong writer.md |

### Xác minh luồng dữ liệu (đầu cuối)

Sau khi chạy 5 chương:
1. `cat meta/cast_ledger.json` không được rỗng (trừ khi mỗi chương chỉ dùng nhân vật cốt lõi)
2. Nếu Người viết ở chương 1 giới thiệu "Lão Châu":
   - `cast_ledger` phải có mục `Lão Châu`, `appearance_count=1`
3. Nếu chương 5 lại viết Lão Châu:
   - `Lão Châu.appearance_count=2`, `last_seen_chapter=5`
4. Trong `meta/sessions/agents/writer-*.jsonl` của chương 5, giá trị trả về của novel_context phải thấy Lão Châu trong `episodic_memory.recent_cast`
5. Nếu bước trên thấy nhưng Người viết không tiêu thụ (Lão Châu viết ra không khớp chương 1) — đây là vấn đề prompt

### Hiện chưa có chẩn đoán tự động (nhưng snapshot đã được tải)

`diag.Snapshot.CastLedger` đã được đọc trong `Load()`, có thể được các quy tắc tiêu thụ trực tiếp — nhưng hiện chưa viết quy tắc nào. Việc xác minh vẫn phải dùng lệnh `jq` thủ công ở trên.

Nếu sau này muốn bổ sung quy tắc chẩn đoán (ứng viên):
- `CastBriefRoleMissing`: cảnh báo khi tỷ lệ thiếu > 50%
- `CastBloat`: cảnh báo khi số mục > số chương × 0.8
- `CastPromotionCandidate`: appearance_count ≥ 5 và xuyên cung truyện → gợi ý thăng cấp

Ngưỡng chưa nên quyết định ngay — đợi có dữ liệu tiểu thuyết dài thực tế, xem phân phối thật rồi mới định. Bản thân code quy tắc chỉ cần 30-50 dòng.

---

## 5. Người viết có đang hoạt động đúng kỳ vọng không

Khi chạy tiểu thuyết dài, điều quan tâm nhất là **Người viết có thực sự hành động theo prompt không**. Quan sát trực tiếp nhất là session log:

```bash
ls output/{novel}/meta/sessions/agents/    # Mỗi agent phụ một file jsonl
tail -50 output/{novel}/meta/sessions/agents/writer-*.jsonl
```

Xem một số hành vi cụ thể:

| Hành vi kỳ vọng | Biểu hiện trong jsonl |
|---|---|
| Người viết đã xem recent_cast | Giá trị trả về của tool novel_context có trường `episodic_memory.recent_cast` không rỗng |
| Người viết điền cast_intros trong commit_chapter | Tham số tool_call `cast_intros` là mảng không rỗng (chỉ ở chương giới thiệu nhân vật mới) |
| Người viết dùng gợi ý chương liên quan | Số lần gọi `read_chapter` > 1 (mặc định 1 lần, vượt quá nghĩa là đã tra lại) |
| Người viết không vi phạm thứ tự tool | Chuỗi tool_call nghiêm ngặt theo: `novel_context → read_chapter → plan_chapter → draft_chapter → check_consistency → commit_chapter` |

Nếu trong jsonl thấy Người viết gọi rỗng novel_context nhiều lần, hoặc sau commit_chapter lại gọi tool khác — là prompt chưa kiểm soát được.

---

## 6. Ngưỡng đỏ khi chạy dài

Khi chạy tiểu thuyết 100+ chương, bất kỳ điều nào dưới đây kích hoạt thì nên dừng lại để kiểm tra:

- [ ] CompassDrift kích hoạt và kéo dài qua 2 cung truyện chưa giải quyết
- [ ] Số mục cast_ledger > số chương đã hoàn thành × 0.8
- [ ] Tỷ lệ điền brief_role trong cast_ledger < 30%
- [ ] Cùng một nhân vật xuất hiện nghi vấn nhiều tên ("Lão Li" / "Li chưởng quỹ" cùng tồn tại)
- [ ] Người viết khi viết chương mới không đọc nhân vật cũ đã có trong recent_cast (tái phát minh)
- [ ] Trong session Điều phối viên xuất hiện ≥ 5 lần gọi rỗng novel_context liên tiếp
- [ ] Bất kỳ chương nào sau commit mà `meta/checkpoints.jsonl` không có step `commit_chapter` tương ứng

4 điều đầu là mức độ khoẻ của cơ chế mới lần này; 3 điều sau là tính ổn định của cơ chế đã có.

---

## 7. Quy chuẩn bảo trì tài liệu

**Khi thêm sản phẩm tầng thực tế mới (tạo một `meta/*.json` / `meta/*.jsonl` mới), đồng bộ:**

1. Thêm một dòng tra nhanh vào §2 của tài liệu này
2. Nếu sản phẩm cần quan sát chuyên biệt (không phải phán xét đơn giản "tồn tại/không tồn tại"), thêm đoạn chuyên đề §X
3. Nếu muốn chẩn đoán tự động, tải vào `internal/diag/snapshot.go::Load` và thêm quy tắc vào `internal/diag/rules_*.go`

**Không nên:**
- Không chép toàn bộ quy tắc trong `internal/diag/` vào tài liệu này (đó là tài liệu tham khảo quy tắc, không phải sổ tay quan sát)
- Không viết quy tắc chẩn đoán cho mọi cơ chế — ngưỡng dựa trên cảm tính sẽ sai, hãy quan sát trước rồi bổ sung sau
