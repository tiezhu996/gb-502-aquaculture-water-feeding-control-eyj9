package service

import (
	"aquaculture-water-feeding-control/backend/internal/model"
	"testing"
	"time"
)

func makeBatch(id uint, no string, inbound float64, expireOffsetDays int) model.FeedBatch {
	return model.FeedBatch{
		Base:       model.Base{ID: id},
		BatchNo:    no,
		FeedType:   "对虾配合饲料",
		InboundKg:  inbound,
		ExpireDate: time.Now().UTC().AddDate(0, 0, expireOffsetDays),
		Enabled:    true,
	}
}

func TestAllocateFEFOUsesEarliestExpiryFirst(t *testing.T) {
	batches := []model.FeedBatch{
		makeBatch(1, "EARLY", 10, 5),
		makeBatch(2, "LATER", 100, 60),
	}
	allocations := allocateFEFO(batches, map[uint]float64{}, 30)
	if allocations == nil {
		t.Fatal("expected allocation to succeed")
	}
	if len(allocations) != 2 {
		t.Fatalf("expected 2 batch allocations, got %d", len(allocations))
	}
	if allocations[0].BatchID != 1 || allocations[0].AmountKg != 10 {
		t.Fatalf("first allocation should drain earliest batch fully, got %+v", allocations[0])
	}
	if allocations[1].BatchID != 2 || allocations[1].AmountKg != 20 {
		t.Fatalf("second allocation should take remainder from later batch, got %+v", allocations[1])
	}
}

func TestAllocateFEFORespectsConsumedRemainder(t *testing.T) {
	batches := []model.FeedBatch{
		makeBatch(1, "PARTIAL", 10, 5),
		makeBatch(2, "LATER", 100, 60),
	}
	allocations := allocateFEFO(batches, map[uint]float64{1: 8}, 5)
	if len(allocations) != 2 {
		t.Fatalf("expected 2 allocations using 2kg then 3kg, got %d", len(allocations))
	}
	if allocations[0].AmountKg != 2 || allocations[1].AmountKg != 3 {
		t.Fatalf("unexpected split: %+v", allocations)
	}
}

func TestAllocateFEFOFailsWhenInsufficient(t *testing.T) {
	batches := []model.FeedBatch{
		makeBatch(1, "SMALL", 5, 5),
		makeBatch(2, "SMALL2", 3, 60),
	}
	if allocateFEFO(batches, map[uint]float64{}, 9) != nil {
		t.Fatal("expected nil allocation when remaining stock is insufficient")
	}
	if allocateFEFO(batches, map[uint]float64{1: 5, 2: 3}, 0.1) != nil {
		t.Fatal("expected nil allocation when every batch is exhausted")
	}
}

func TestAllocateFEFOExactFit(t *testing.T) {
	batches := []model.FeedBatch{makeBatch(1, "EXACT", 12.5, 5)}
	allocations := allocateFEFO(batches, map[uint]float64{}, 12.5)
	if len(allocations) != 1 || allocations[0].AmountKg != 12.5 {
		t.Fatalf("expected exact single allocation, got %+v", allocations)
	}
}
