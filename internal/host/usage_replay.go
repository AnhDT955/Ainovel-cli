package host

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/agentcore"
)

// sessionRecord là dạng phân tích nhẹ của một bản ghi đơn trong meta/sessions/*.jsonl —
// chỉ lấy các trường cần thiết để tích lũy usage. Các trường lớn như Content
// bỏ qua phân tích để tiết kiệm IO lúc khởi động.
//
// Ưu tiên quy kết mô hình theo ba cấp giảm dần:
//  1. Usage.Provider/Model — mô hình phản hồi thực tế được agentcore/litellm truyền qua (ưu tiên nhất)
//  2. Meta(_meta)          — khi upstream không truyền qua, phía ghi bổ sung "mô hình hiệu lực tại thời điểm đó" qua ModelLookup
//  3. Cả hai đều thiếu    — replay lui về effectiveModel, suy ngược từ ModelSet hiện tại (độ chính xác giảm)
type sessionRecord struct {
	Role  agentcore.Role     `json:"role"`
	Usage *agentcore.Usage   `json:"usage,omitempty"`
	Meta  *sessionRecordMeta `json:"_meta,omitempty"`
}

type sessionRecordMeta struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// ReplaySessions quét meta/sessions/coordinator.jsonl và meta/sessions/agents/*.jsonl,
// tái tích lũy usage của từng tin nhắn assistant vào tracker. Trả về số bản ghi đã bổ sung.
//
// Ràng buộc gọi: chỉ gọi một lần khi meta/usage.json bị thiếu (nâng cấp lần đầu
// hoặc schema thay đổi) để bổ sung dữ liệu lịch sử.
// Việc lưu trữ thường ngày dùng SaveNow / autoSaveLoop.
//
// Độ chính xác phụ thuộc vào ba cấp giảm dần trong comment của sessionRecord —
// cấp 3 (thiếu cả Usage lẫn _meta) chỉ xảy ra với log cũ hơn hoặc khi upstream bị lỗi.
func (t *UsageTracker) ReplaySessions(rootDir string) (int, error) {
	if t == nil {
		return 0, nil
	}
	sessionsDir := filepath.Join(rootDir, "meta", "sessions")
	info, err := os.Stat(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}

	total := 0
	if n, err := t.replayFile(filepath.Join(sessionsDir, "coordinator.jsonl"), "coordinator"); err != nil {
		slog.Warn("replay coordinator session failed", "module", "usage", "err", err)
	} else {
		total += n
	}

	agentsDir := filepath.Join(sessionsDir, "agents")
	walkErr := filepath.WalkDir(agentsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		agentName := parseAgentNameFromFile(name)
		if agentName == "" {
			return nil
		}
		n, fileErr := t.replayFile(path, agentName)
		if fileErr != nil {
			slog.Warn("replay agent session failed", "module", "usage", "file", name, "err", fileErr)
			return nil
		}
		total += n
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return total, walkErr
	}
	return total, nil
}

// replayFile quét một file jsonl đơn, đưa tất cả tin nhắn assistant có Usage vào accumulate.
// agentName được truyền từ phía gọi (coordinator hoặc tên sub-agent phân tích từ tên file).
func (t *UsageTracker) replayFile(path, agentName string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	role := agentRoleName(agentName)
	count := 0
	scanner := bufio.NewScanner(f)
	// Mỗi dòng có thể rất dài (tin nhắn assistant + tool args, v.v. đều được làm phẳng),
	// nới rộng buffer lên 4MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec sessionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Role != agentcore.RoleAssistant || rec.Usage == nil {
			continue
		}
		provider, modelName := usageActualModel(rec.Usage)
		if rec.Meta != nil {
			if provider == "" {
				provider = rec.Meta.Provider
			}
			if modelName == "" {
				modelName = rec.Meta.Model
			}
		}
		t.accumulate(role, provider, modelName, *rec.Usage)
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scan %s: %w", path, err)
	}
	return count, nil
}

// parseAgentNameFromFile trích xuất tên agent từ "writer-ch01.jsonl" / "architect_short-001.jsonl"
// (phần trước dấu "-"). Quy ước đặt tên xem store/session.go::subAgentPath:
// agentName không chứa dash, suffix là ch<n> hoặc số thứ tự tăng dần.
func parseAgentNameFromFile(name string) string {
	base := strings.TrimSuffix(name, ".jsonl")
	if i := strings.Index(base, "-"); i > 0 {
		return base[:i]
	}
	return ""
}
