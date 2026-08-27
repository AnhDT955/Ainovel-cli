// Package models cung cấp bảng đăng ký metadata LLM (cửa sổ ngữ cảnh, giới hạn output, giá),
// nguồn dữ liệu từ OpenRouter API, baseline biên dịch + làm mới lúc chạy.
package models

//go:generate go run gen_models.go

import (
	"strings"
	"sync"
)

// ModelEntry mô tả một mô hình LLM đã biết.
type ModelEntry struct {
	Provider            string  `json:"provider"`               // Tên nhà cung cấp đã chuẩn hóa theo OpenRouter (anthropic/openai/gemini/...)
	ID                  string  `json:"id"`                     // ID mô hình (không bao gồm tiền tố nhà cung cấp)
	Name                string  `json:"name"`                   // Tên hiển thị
	ContextWindow       int     `json:"context_window"`         // Cửa sổ ngữ cảnh đầu vào
	MaxTokens           int     `json:"max_tokens"`             // Giới hạn output mỗi lần
	InputCostPer1M      float64 `json:"input_cost_per_1m"`      // Giá đầu vào (USD/1M token)
	OutputCostPer1M     float64 `json:"output_cost_per_1m"`     // Giá đầu ra
	CacheReadCostPer1M  float64 `json:"cache_read_cost_per_1m"` // Giá đọc cache
	CacheWriteCostPer1M float64 `json:"cache_write_cost_per_1m"`
}

// ModelRegistry lưu trữ các mô hình đã biết, hỗ trợ phân giải mờ và hợp nhất lúc chạy.
type ModelRegistry struct {
	mu     sync.RWMutex
	models []ModelEntry
}

// NewModelRegistry trả về một bảng đăng ký đã nạp baseline biên dịch.
func NewModelRegistry() *ModelRegistry {
	r := &ModelRegistry{}
	r.models = append(r.models, generatedModels...)
	return r
}

var (
	defaultRegistry     *ModelRegistry
	defaultRegistryOnce sync.Once
)

// DefaultRegistry trả về bảng đăng ký toàn cục (lazy load, thread-safe).
// Gọi StartPricingRefresh trong giai đoạn khởi động để cập nhật giá/thông tin cửa sổ ở nền.
func DefaultRegistry() *ModelRegistry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = NewModelRegistry()
	})
	return defaultRegistry
}

// Resolve tìm kiếm một mục theo định danh mô hình (có thể là "provider/model", ID đầy đủ, hoặc tên một phần).
//
// Thứ tự khớp:
//  1. Nếu chứa "/", tìm chính xác theo "provider/model"
//  2. Khớp chính xác/hậu tố ngày
//  3. Khớp chuỗi con (ID hoặc Name chứa pattern)
//
// Khi khớp nhiều kết quả, ưu tiên trả về alias không có hậu tố ngày (ví dụ claude-sonnet-4 ưu tiên hơn claude-sonnet-4-20250514).
func (r *ModelRegistry) Resolve(pattern string) (*ModelEntry, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if idx := strings.Index(pattern, "/"); idx > 0 {
		prov := pattern[:idx]
		modelID := pattern[idx+1:]
		if entry, ok := lookupModelEntry(r.models, prov, modelID); ok {
			return &entry, true
		}
		// Tiền tố vendor của OpenRouter (google/, x-ai/) không nhất thiết bằng tên Provider nội bộ,
		// thử lại chỉ với modelID để đảm bảo "google/gemini-2.5-pro" khớp được mục gemini.
		if entry, ok := lookupModelEntry(r.models, "", modelID); ok {
			return &entry, true
		}
	}

	if entry, ok := lookupModelEntry(r.models, "", pattern); ok {
		return &entry, true
	}

	lower := strings.ToLower(pattern)
	normalized := normalizeModelLookupID(pattern)
	var candidates []int
	for i := range r.models {
		if strings.Contains(normalizeModelLookupID(r.models[i].ID), normalized) ||
			strings.Contains(strings.ToLower(r.models[i].ID), lower) ||
			strings.Contains(strings.ToLower(r.models[i].Name), lower) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}

	best := candidates[0]
	for _, i := range candidates[1:] {
		if !hasDatedSuffix(r.models[i].ID) && hasDatedSuffix(r.models[best].ID) {
			best = i
		}
	}
	entry := r.models[best]
	return &entry, true
}

// ResolveContextWindow trả về cửa sổ ngữ cảnh của một mô hình; trả về 0 nếu không tìm thấy.
func (r *ModelRegistry) ResolveContextWindow(pattern string) int {
	if e, ok := r.Resolve(pattern); ok {
		return e.ContextWindow
	}
	return 0
}

// List trả về tất cả mô hình (filter tùy chọn, chuỗi rỗng nghĩa là lấy toàn bộ).
func (r *ModelRegistry) List(filter string) []ModelEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if filter == "" {
		return append([]ModelEntry{}, r.models...)
	}
	lower := strings.ToLower(filter)
	normalized := normalizeModelLookupID(filter)
	var out []ModelEntry
	for _, m := range r.models {
		if strings.Contains(strings.ToLower(m.Provider), lower) ||
			strings.Contains(normalizeModelLookupID(m.ID), normalized) ||
			strings.Contains(strings.ToLower(m.ID), lower) ||
			strings.Contains(strings.ToLower(m.Name), lower) {
			out = append(out, m)
		}
	}
	return out
}

// MergeModels hợp nhất theo provider+id không phân biệt hoa thường.
// Giá/cửa sổ/MaxTokens/Name khác không sẽ ghi đè mục hiện có; mục mới được thêm trực tiếp.
func (r *ModelRegistry) MergeModels(fetched []ModelEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := make(map[string]int, len(r.models))
	for i, m := range r.models {
		idx[strings.ToLower(m.Provider+"/"+m.ID)] = i
	}
	for _, f := range fetched {
		key := strings.ToLower(f.Provider + "/" + f.ID)
		if i, ok := idx[key]; ok {
			if f.InputCostPer1M > 0 || f.OutputCostPer1M > 0 {
				r.models[i].InputCostPer1M = f.InputCostPer1M
				r.models[i].OutputCostPer1M = f.OutputCostPer1M
				r.models[i].CacheReadCostPer1M = f.CacheReadCostPer1M
				r.models[i].CacheWriteCostPer1M = f.CacheWriteCostPer1M
			}
			if f.ContextWindow > 0 {
				r.models[i].ContextWindow = f.ContextWindow
			}
			if f.MaxTokens > 0 {
				r.models[i].MaxTokens = f.MaxTokens
			}
			if f.Name != "" {
				r.models[i].Name = f.Name
			}
		} else {
			r.models = append(r.models, f)
			idx[key] = len(r.models) - 1
		}
	}
}
