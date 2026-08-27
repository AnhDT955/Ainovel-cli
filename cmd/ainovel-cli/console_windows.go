//go:build windows

package main

import "syscall"

func init() {
	// Thiết lập console code page thành UTF-8 (65001) để hiển thị đúng
	// các ký tự Unicode (tiếng Việt, tiếng Trung, emoji, v.v.).
	// Trên Windows, console mặc định sử dụng code page hệ thống (thường là 437 hoặc 936),
	// khiến output UTF-8 từ Go bị giải mã sai thành ký tự lỗi.
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	setConsoleCP := kernel32.NewProc("SetConsoleCP")
	setConsoleOutputCP.Call(65001) // UTF-8 output
	setConsoleCP.Call(65001)       // UTF-8 input
}
