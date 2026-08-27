package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/ainovel-cli/internal/host/exp"
)

func main() {
	inputPath := flag.String("input", "", "Đường dẫn đến file TXT đầu vào (bắt buộc)")
	outputPath := flag.String("output", "", "Đường dẫn file EPUB đầu ra (tùy chọn, mặc định thay thế đuôi .txt bằng .epub)")
	title := flag.String("title", "", "Tên truyện cho bìa sách (tùy chọn, tự động nhận diện từ dòng đầu tiên nếu để trống)")
	help := flag.Bool("help", false, "Hiển thị hướng dẫn sử dụng")
	h := flag.Bool("h", false, "Hiển thị hướng dẫn sử dụng")

	flag.Parse()

	if *help || *h {
		printUsage()
		return
	}

	if *inputPath == "" {
		fmt.Fprintln(os.Stderr, "Lỗi: Vui lòng cung cấp đường dẫn file TXT đầu vào thông qua tham số -input.")
		fmt.Fprintln(os.Stderr, "Chạy 'txt2epub -help' để xem hướng dẫn sử dụng.")
		os.Exit(1)
	}

	// Đọc file đầu vào
	txtContent, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lỗi: Không thể đọc file đầu vào %s: %v\n", *inputPath, err)
		os.Exit(1)
	}

	// Suy luận đường dẫn đầu ra nếu trống
	outPath := *outputPath
	if outPath == "" {
		ext := filepath.Ext(*inputPath)
		outPath = strings.TrimSuffix(*inputPath, ext) + ".epub"
	}

	fmt.Printf("Đang chuyển đổi: %s ...\n", filepath.Base(*inputPath))

	// Thực hiện chuyển đổi sang EPUB
	epubBytes, err := exp.ConvertTXTToEPUB(txtContent, *title)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lỗi khi chuyển đổi sang EPUB: %v\n", err)
		os.Exit(1)
	}

	// Ghi file đầu ra
	if err := os.WriteFile(outPath, epubBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Lỗi khi ghi file EPUB đầu ra %s: %v\n", outPath, err)
		os.Exit(1)
	}

	fmt.Printf("✓ Chuyển đổi thành công! File EPUB đã được ghi tại: %s\n", outPath)
}

func printUsage() {
	fmt.Println("=== Tool chuyển đổi file TXT tiểu thuyết sang EPUB ===")
	fmt.Println("Cách sử dụng:")
	fmt.Println("  go run cmd/txt2epub/main.go [options]")
	fmt.Println()
	fmt.Println("Các tham số hỗ trợ:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Ví dụ:")
	fmt.Println("  go run cmd/txt2epub/main.go -input output/novel/truyen.txt -title \"Tên Truyện\"")
}
