package host

import (
	"strings"
	"testing"
)

// TestEmitDeltaCoalesceNoLossNoReorder xác minh Lỗi A: khi streamCh đầy, emitDelta phải GỘP
// (không vứt) và giữ ĐÚNG thứ tự — nối carry vào đầu delta kế, không đảo như cách gộp qua kênh.
func TestEmitDeltaCoalesceNoLossNoReorder(t *testing.T) {
	h := &Host{streamCh: make(chan string, 4)}
	inputs := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, s := range inputs {
		h.emitDelta(s) // không có consumer → kênh đầy sau 4 mảnh, phần còn lại dồn vào carry
	}

	var got strings.Builder
	for {
		select {
		case s := <-h.streamCh:
			got.WriteString(s)
			continue
		default:
		}
		break
	}
	got.WriteString(h.streamCarry) // phần đuôi chưa gửi được vẫn giữ nguyên, đúng thứ tự

	want := strings.Join(inputs, "")
	if got.String() != want {
		t.Fatalf("mất chữ hoặc đảo thứ tự:\n got  = %q\n want = %q", got.String(), want)
	}
}

// TestEmitDeltaSentinelNotMergedIntoText xác minh sentinel ranh giới round không bị gộp vào văn bản.
func TestEmitDeltaSentinelNotMergedIntoText(t *testing.T) {
	h := &Host{streamCh: make(chan string, 8)}
	h.emitDelta("round1")
	h.emitDelta(StreamClearSentinel)
	h.emitDelta("round2")

	var items []string
	for {
		select {
		case s := <-h.streamCh:
			items = append(items, s)
			continue
		default:
		}
		break
	}
	if len(items) != 3 || items[0] != "round1" || items[1] != StreamClearSentinel || items[2] != "round2" {
		t.Fatalf("sentinel bị gộp/lệch: %#v", items)
	}
}

// TestEmitStreamDeltaPerAgentUTF8 xác minh Lỗi B: byte UTF-8 lẻ của một agent KHÔNG được ghép
// nhầm vào văn bản của agent khác khi hai luồng đan xen.
func TestEmitStreamDeltaPerAgentUTF8(t *testing.T) {
	var got strings.Builder
	o := &observer{
		emitD:               func(s string) { got.WriteString(s) },
		emitC:               func() {},
		utf8Pending:         make(map[string][]byte),
		lastThinkingByAgent: make(map[string]string),
	}

	// "chạ": 'ạ' = U+1EA1 = 3 bytes (E1 BA A1). Cắt agent1 sao cho delta đầu kết thúc GIỮA rune.
	b := []byte("chạ")
	part1 := string(b[:len(b)-2]) // "ch" + byte E1 (leading byte lẻ)
	part2 := string(b[len(b)-2:]) // BA A1 (phần còn lại của 'ạ')

	// "ch" ra ngay, byte E1 lẻ được buffer riêng cho agent1
	o.emitStreamDelta("agent1", part1, false)
	// văn bản agent2 KHÔNG được dính byte E1 còn treo của agent1
	o.emitStreamDelta("agent2", "xin chào", false)
	// E1+BA+A1 ghép lại thành 'ạ' hoàn chỉnh
	o.emitStreamDelta("agent1", part2, false)

	want := "ch" + "xin chào" + "ạ"
	if got.String() != want {
		t.Fatalf("byte lẻ ghép nhầm luồng:\n got  = %q\n want = %q", got.String(), want)
	}
	if strings.ContainsRune(got.String(), '�') {
		t.Fatalf("xuất hiện ký tự thay thế U+FFFD (rune bị vỡ): %q", got.String())
	}
}
