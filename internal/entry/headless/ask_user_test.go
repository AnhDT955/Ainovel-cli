package headless

import (
	"context"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/tools"
)

func TestTerminalAskUserSingleSelect(t *testing.T) {
	handler := newTerminalAskUser(strings.NewReader("2\n"), &strings.Builder{})
	resp, err := handler.handle(context.Background(), []tools.Question{
		{
			Question: "Bạn muốn phong cách gì?",
			Header:   "Phong cách",
			Options: []tools.Option{
				{Label: "Hành động", Description: "Thiên về thăng cấp"},
				{Label: "Huyền bí", Description: "Thiên về bí ẩn"},
			},
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := resp.Answers["Bạn muốn phong cách gì?"]; got != "Huyền bí" {
		t.Fatalf("unexpected answer: %q", got)
	}
}

func TestTerminalAskUserCustomInput(t *testing.T) {
	handler := newTerminalAskUser(strings.NewReader("0\nKhông có tuyến tình cảm\n"), &strings.Builder{})
	resp, err := handler.handle(context.Background(), []tools.Question{
		{
			Question: "Còn giới hạn nào khác?",
			Header:   "Giới hạn",
			Options: []tools.Option{
				{Label: "Tối tăm", Description: "Tổng thể u ám"},
				{Label: "Nhẹ nhàng", Description: "Tông nền tươi sáng"},
			},
		},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := resp.Answers["Còn giới hạn nào khác?"]; got != "Tùy chỉnh" {
		t.Fatalf("unexpected answer: %q", got)
	}
	if got := resp.Notes["Còn giới hạn nào khác?"]; got != "Không có tuyến tình cảm" {
		t.Fatalf("unexpected note: %q", got)
	}
}
