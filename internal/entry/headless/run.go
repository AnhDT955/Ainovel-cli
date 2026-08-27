package headless

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/diag"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/logger"
	"github.com/voocel/ainovel-cli/internal/store"
)

type Options struct {
	Prompt string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run chạy nhân phiên làm việc ở chế độ không giao diện, tiêu thụ trực tiếp các sự kiện Engine và đầu ra streaming.
// Nếu sau này thêm các phương thức khởi động dùng chung như “tiếp tục viết tiểu thuyết có sẵn”,
// không nên đưa thẳng vào đây mà hãy đưa vào internal/entry/startup, rồi để entry headless gọi lại.
func Run(cfg bootstrap.Config, bundle assets.Bundle, opts Options) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.WritePanic("headless", r)
			panic(r)
		}
		logger.WriteErr("headless", err)
	}()

	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	eng, err := host.New(cfg, bundle)
	if err != nil {
		return err
	}
	eng.AskUser().SetHandler(newTerminalAskUser(stdin, stderr).handle)
	cleanup := logger.SetupFile(eng.Dir(), "headless.log", false)
	defer cleanup()
	defer eng.Close()
	// Khi chạy xong hoặc trả về lỗi, xuất một bản chẩn đoán đã ẩn danh hóa để người dùng headless dễ báo issue.
	// (Các trường hợp bị kill từ bên ngoài không đi qua defer, vẫn cần dùng /diag thủ công trong TUI.)
	defer func() { _, _ = diag.Export(store.NewStore(eng.Dir())) }()

	prompt := strings.TrimSpace(opts.Prompt)
	if prompt != "" {
		plan, err := startup.PrepareQuick(startup.Request{
			Mode:        startup.ModeQuick,
			UserPrompt:  prompt,
			OutputDir:   eng.Dir(),
			Interactive: true,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "headless khởi động: %s\n", eng.Dir())
		if err := eng.StartPrepared(plan.StartPrompt); err != nil {
			return err
		}
	} else {
		items, err := eng.ReplayQueue(0)
		if err != nil {
			return err
		}
		roundHasContent, err := replayQueue(items, stdout, stderr)
		if err != nil {
			return err
		}
		label, err := eng.Resume()
		if err != nil {
			return err
		}
		if label == "" {
			return fmt.Errorf("chế độ headless yêu cầu --prompt, hoặc thư mục đầu ra %q phải có phiên có thể khôi phục", eng.Dir())
		}
		fmt.Fprintf(stderr, "headless khôi phục: %s (%s)\n", eng.Dir(), label)
		return consume(eng, stdout, stderr, roundHasContent)
	}

	return consume(eng, stdout, stderr, false)
}

func consume(eng *host.Host, stdout, stderr io.Writer, roundHasContent bool) error {
	for {
		select {
		case ev, ok := <-eng.Events():
			if !ok {
				return nil
			}
			writeEvent(stderr, ev)
		case delta, ok := <-eng.Stream():
			if !ok {
				continue
			}
			if delta == host.StreamClearSentinel {
				if roundHasContent {
					if _, err := io.WriteString(stdout, "\n\n"); err != nil {
						return err
					}
					roundHasContent = false
				}
				continue
			}
			if delta == "" {
				continue
			}
			if _, err := io.WriteString(stdout, delta); err != nil {
				return err
			}
			roundHasContent = true
		case _, ok := <-eng.Done():
			if !ok {
				return nil
			}
			return drainPending(eng, stdout, stderr, roundHasContent)
		}
	}
}

func drainPending(eng *host.Host, stdout, stderr io.Writer, roundHasContent bool) error {
	for {
		select {
		case ev, ok := <-eng.Events():
			if ok {
				writeEvent(stderr, ev)
			}
		case delta, ok := <-eng.Stream():
			if !ok {
				continue
			}
			if delta == host.StreamClearSentinel {
				if roundHasContent {
					if _, err := io.WriteString(stdout, "\n\n"); err != nil {
						return err
					}
					roundHasContent = false
				}
				continue
			}
			if delta != "" {
				if _, err := io.WriteString(stdout, delta); err != nil {
					return err
				}
				roundHasContent = true
			}
		default:
			if roundHasContent {
				if _, err := io.WriteString(stdout, "\n"); err != nil {
					return err
				}
			}
			return nil
		}
	}
}

func writeEvent(w io.Writer, ev host.Event) {
	if w == nil || strings.TrimSpace(ev.Summary) == "" {
		return
	}
	ts := ev.Time.Format("15:04:05")
	if ts == "00:00:00" {
		ts = "--:--:--"
	}
	fmt.Fprintf(w, "[%s] [%s] %s\n", ts, ev.Category, ev.Summary)
}

func replayQueue(items []domain.RuntimeQueueItem, stdout, stderr io.Writer) (bool, error) {
	var roundHasContent bool
	for _, item := range items {
		switch item.Kind {
		case domain.RuntimeQueueUIEvent:
			writeEvent(stderr, host.Event{
				Time:     item.Time,
				Category: item.Category,
				Summary:  item.Summary,
			})
		case domain.RuntimeQueueStreamClear:
			if roundHasContent {
				if _, err := io.WriteString(stdout, "\n\n"); err != nil {
					return roundHasContent, err
				}
				roundHasContent = false
			}
		case domain.RuntimeQueueStreamDelta:
			text := host.ReplayDeltaText(item)
			if text == "" {
				continue
			}
			if _, err := io.WriteString(stdout, text); err != nil {
				return roundHasContent, err
			}
			roundHasContent = true
		}
	}
	return roundHasContent, nil
}
