// Package store cung cấp lưu trữ bền vững dựa trên hệ thống file.
//
// Kiến trúc: 1 nền tảng IO + nhiều sub-store + 1 gốc tổng hợp.
// Mỗi sub-store giữ một instance IO độc lập và một sync.RWMutex riêng.
// Các thao tác đọc/ghi trên các miền chính (Progress, Outline, Drafts, Summaries, v.v.) không chặn lẫn nhau;
// WorldStore gộp nhiều miền nhỏ ít dùng để chia sẻ chung một khóa.
//
// Gốc tổng hợp Store giữ tham chiếu đến tất cả sub-store và chịu trách nhiệm
// thực hiện các thao tác nguyên tử liên miền (ExpandArc, AppendVolume, ClearHandledSteer).
//
// Phân chia sub-store:
//   - ProgressStore: trạng thái chính về tiến độ (meta/progress.json)
//   - OutlineStore: tiền đề, đề cương (phẳng/phân cấp), la bàn
//   - DraftStore: ý tưởng chương, bản nháp, bản cuối
//   - SummaryStore: tóm tắt theo chương/cung truyện/tập
//   - RunMetaStore: siêu dữ liệu Run (mô hình, lịch sử can thiệp)
//   - SignalStore: file tín hiệu một lần (phục hồi PendingCommit)
//   - CheckpointStore: điểm khôi phục cấp step (meta/checkpoints.jsonl)
//   - RuntimeStore: hàng đợi sự kiện runtime (meta/runtime/*.jsonl)
//   - CharacterStore: hồ sơ nhân vật, ảnh chụp trạng thái
//   - WorldStore: dòng thời gian, phục bút, quan hệ, thay đổi trạng thái, quy tắc thế giới, quy tắc phong cách, đánh giá
package store
