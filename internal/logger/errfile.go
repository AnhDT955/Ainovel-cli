package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Thư mục Logs nằm cạnh file thực thi (D:\AnhDT\Git\AI-Novel\Ainovel\Logs khi chạy ainovel-cli.exe
// trong thư mục dự án). Đặt biến môi trường AINOVEL_LOG_DIR để ghi sang nơi khác.
const logDirEnv = "AINOVEL_LOG_DIR"

var errFileMu sync.Mutex

// LogsDir trả về thư mục chứa file log lỗi.
// Thứ tự ưu tiên: AINOVEL_LOG_DIR > thư mục chứa file thực thi > thư mục làm việc hiện tại.
func LogsDir() string {
	if dir := strings.TrimSpace(os.Getenv(logDirEnv)); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "Logs")
	}
	wd, err := os.Getwd()
	if err != nil {
		return "Logs"
	}
	return filepath.Join(wd, "Logs")
}

// WriteError ghi nối tiếp một lỗi vào Logs/error-YYYY-MM-DD.txt và trả về đường dẫn file
// (chuỗi rỗng nếu ghi thất bại). scope là nơi phát sinh lỗi, ví dụ "tui" hoặc "config".
// Best-effort: không bao giờ trả về lỗi để tránh che khuất lỗi gốc mà nó đang ghi lại.
func WriteError(scope, msg string) string {
	if strings.TrimSpace(msg) == "" {
		return ""
	}

	dir := LogsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "error-"+time.Now().Format("2006-01-02")+".txt")

	errFileMu.Lock()
	defer errFileMu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ""
	}
	defer f.Close()

	if scope == "" {
		scope = "app"
	}
	if _, err := fmt.Fprintf(f, "[%s] [%s] %s\n", time.Now().Format(time.RFC3339), scope, msg); err != nil {
		return ""
	}
	return path
}

// WriteErr là dạng tiện dụng của WriteError cho giá trị error. Bỏ qua err == nil.
func WriteErr(scope string, err error) string {
	if err == nil {
		return ""
	}
	return WriteError(scope, err.Error())
}

// WritePanic ghi một panic kèm stack trace. Dùng trong defer:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        logger.WritePanic("tui", r)
//	        panic(r)
//	    }
//	}()
func WritePanic(scope string, r any) string {
	return WriteError(scope, fmt.Sprintf("panic: %v\n%s", r, debug.Stack()))
}
