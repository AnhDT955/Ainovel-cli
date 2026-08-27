package tools

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
)

func TestParsePremiseSections(t *testing.T) {
	premise := `# Premise

## 题材和基调
东方玄幻，冷硬成长。

## Định vị thể loại
东方玄幻升级流，面向追求爽点 và quan hệ推进.

## Xung đột cốt lõi
主角必须在宗门规则与个人良知之间做选择。

## 中期转向
旧有修炼路线失效，必须转向禁术体系。
`

	sections := parsePremiseSections(premise)
	if sections["Thể loại và tông điệu"] == "" {
		t.Fatalf("expected Thể loại và tông điệu (legacy alias), got %+v", sections)
	}
	if sections["Định vị thể loại"] == "" {
		t.Fatalf("expected Định vị thể loại section, got %+v", sections)
	}
	if sections["Xung đột cốt lõi"] == "" {
		t.Fatalf("expected Xung đột cốt lõi section, got %+v", sections)
	}
	if sections["Bước ngoặt giữa chuyện"] == "" {
		t.Fatalf("expected Trung kỳ chuyển hướng alias normalized to Bước ngoặt giữa chuyện, got %+v", sections)
	}
}

func TestPremiseStructure(t *testing.T) {
	premise := `## Thể loại và tông điệu
升级流，偏冷硬。

## Định vị thể loại
升级流

## Xung đột cốt lõi
冲突

## Mục tiêu nhân vật chính
目标

## Hướng kết cục
终局

## Vùng cấm viết
禁区

## Điểm bán hàng khác biệt
卖点

## Điểm móc khác biệt
钩子

## Cam kết thực hiện cốt lõi
兑现

## Động cơ truyện
引擎

## Bước ngoặt giữa chuyện
转折
`

	structure := premiseStructure(premise, domain.PlanningTierMid)
	if ready, _ := structure["template_ready"].(bool); !ready {
		t.Fatalf("expected template_ready, got %+v", structure)
	}
	missing, _ := structure["missing"].([]string)
	if len(missing) != 0 {
		t.Fatalf("expected no missing headings, got %+v", missing)
	}
}

func TestPremiseStructureShortAcceptsLegacyHeadingAlias(t *testing.T) {
	premise := `## Thể loại và tông điệu
单卷高压营救。

## Định vị thể loại
短篇高密度冒险。

## Xung đột cốt lõi
主角必须在一夜内救出人质。

## Mục tiêu nhân vật chính
救出人质并活着离开。

## Hướng kết cục
完成任务但付出代价。

## Vùng cấm viết
Không mở rộng.

## Điểm bán hàng khác biệt
时限压力与连续反转。

## Điểm móc khác biệt
每次选择都缩短救援时间。

## Cam kết thực hiện cốt lõi
紧迫感、抉择与反转。

## Tính phù hợp truyện ngắn
核心矛盾和人物弧线都能在单次任务中完成。
`

	structure := premiseStructure(premise, domain.PlanningTierShort)
	if ready, _ := structure["template_ready"].(bool); !ready {
		t.Fatalf("expected short template_ready, got %+v", structure)
	}
}

