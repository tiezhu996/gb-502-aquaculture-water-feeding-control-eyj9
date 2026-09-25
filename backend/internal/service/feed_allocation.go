package service

import (
	"aquaculture-water-feeding-control/backend/internal/model"
	"math"
)

type batchAllocation struct {
	BatchID  uint
	BatchNo  string
	AmountKg float64
}

const allocationRoundingUnit = 1000 // 扣减保留 3 位小数（kg，即 1 克精度）

// allocateFEFO 按先到期先出，从各启用批次余量中分配 amount；调用方须保证批次已过滤
// 过期、停用、类型不符且已按到期日升序加锁排序。返回扣减明细；余量不足时返回 nil。
func allocateFEFO(batches []model.FeedBatch, consumed map[uint]float64, amount float64) []batchAllocation {
	remaining := amount
	allocations := make([]batchAllocation, 0, len(batches))
	for _, batch := range batches {
		if remaining <= 1e-9 {
			break
		}
		available := batch.InboundKg - consumed[batch.ID]
		if available <= 1e-9 {
			continue
		}
		take := available
		if available > remaining {
			take = math.Round(remaining*allocationRoundingUnit) / allocationRoundingUnit
		}
		if take <= 0 {
			continue
		}
		if take > available+1e-9 {
			take = available
		}
		allocations = append(allocations, batchAllocation{BatchID: batch.ID, BatchNo: batch.BatchNo, AmountKg: take})
		remaining -= take
	}
	if remaining > 1e-6 {
		return nil
	}
	return allocations
}
