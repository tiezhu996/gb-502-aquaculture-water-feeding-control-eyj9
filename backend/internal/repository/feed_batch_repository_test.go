package repository

import (
	"aquaculture-water-feeding-control/backend/internal/model"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		if strings.Contains(err.Error(), "CGO_ENABLED=0") {
			t.Skip("sqlite 集成测试需要 CGO，已跳过")
		}
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.FeedBatch{}, &model.FeedConsumption{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestFeedBatchDeductCondition(t *testing.T) {
	db := newTestDB(t)
	repo := NewFeedBatchRepository(db)
	batch := model.FeedBatch{BatchNumber: "B1", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(24 * time.Hour), Enabled: true}
	if err := repo.Save(&batch); err != nil {
		t.Fatalf("save batch: %v", err)
	}
	rows, err := repo.Deduct(batch.ID, 60)
	if err != nil || rows != 1 {
		t.Fatalf("deduct within balance: rows=%d err=%v", rows, err)
	}
	// 剩余 40（浮点表示可能为 39.999…），多扣 0.001 kg 必须被拒绝
	rows, err = repo.Deduct(batch.ID, 40.001)
	if err != nil {
		t.Fatalf("deduct query err: %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected insufficient deduction to affect 0 rows, got %d", rows)
	}
	rows, _ = repo.Deduct(batch.ID, 40)
	if rows != 1 {
		t.Fatalf("deduct exact remaining: rows=%d", rows)
	}
	rows, _ = repo.Deduct(batch.ID, 0.001)
	if rows != 0 {
		t.Fatalf("expected empty batch deduction rejected, got %d rows", rows)
	}
}

func TestFeedConsumptionUniqueExecutionBatch(t *testing.T) {
	db := newTestDB(t)
	repo := NewFeedBatchRepository(db)
	batch := model.FeedBatch{BatchNumber: "B2", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(24 * time.Hour), Enabled: true}
	if err := repo.Save(&batch); err != nil {
		t.Fatal(err)
	}
	first := model.FeedConsumption{ControlExecutionID: 7, FeedBatchID: batch.ID, FeedType: "配合饲料", AmountKg: 10}
	if err := repo.CreateConsumption(&first); err != nil {
		t.Fatalf("create first consumption: %v", err)
	}
	duplicate := model.FeedConsumption{ControlExecutionID: 7, FeedBatchID: batch.ID, FeedType: "配合饲料", AmountKg: 5}
	if err := repo.CreateConsumption(&duplicate); err == nil {
		t.Fatal("expected unique constraint violation for duplicate execution+batch consumption")
	}
	otherBatch := model.FeedBatch{BatchNumber: "B3", FeedType: "配合饲料", InboundAmountKg: 50, RemainingAmountKg: 50, ExpiryDate: time.Now().Add(48 * time.Hour), Enabled: true}
	if err := repo.Save(&otherBatch); err != nil {
		t.Fatal(err)
	}
	second := model.FeedConsumption{ControlExecutionID: 7, FeedBatchID: otherBatch.ID, FeedType: "配合饲料", AmountKg: 5}
	if err := repo.CreateConsumption(&second); err != nil {
		t.Fatalf("same execution may consume multiple batches: %v", err)
	}
	count, _ := repo.ConsumptionCountForExecution(7)
	if count != 2 {
		t.Fatalf("expected 2 consumptions for execution, got %d", count)
	}
}

func TestFeedBatchDeductNoOversell(t *testing.T) {
	// SQLite 不支持并发写入，使用顺序压测验证条件更新不会超扣；
	// PostgreSQL 下行锁与该 WHERE 条件共同保证并发安全。
	db := newTestDB(t)
	repo := NewFeedBatchRepository(db)
	batch := model.FeedBatch{BatchNumber: "B4", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(24 * time.Hour), Enabled: true}
	if err := repo.Save(&batch); err != nil {
		t.Fatal(err)
	}
	var success, rejected int
	for i := 0; i < 10; i++ {
		rows, err := repo.Deduct(batch.ID, 15)
		if err != nil {
			t.Fatalf("deduct err: %v", err)
		}
		if rows == 1 {
			success++
		} else {
			rejected++
		}
	}
	if success != 6 || rejected != 4 {
		t.Fatalf("expected 6 accepted / 4 rejected, got %d / %d", success, rejected)
	}
	got, err := repo.Get(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RemainingAmountKg > 10+1e-6 || got.RemainingAmountKg < 10-1e-6 {
		t.Fatalf("expected remaining 10, got %v", got.RemainingAmountKg)
	}
}

func TestAvailableForUpdateFefoOrder(t *testing.T) {
	db := newTestDB(t)
	repo := NewFeedBatchRepository(db)
	late := model.FeedBatch{BatchNumber: "LATE", FeedType: "t", InboundAmountKg: 10, RemainingAmountKg: 10, ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: true}
	early := model.FeedBatch{BatchNumber: "EARLY", FeedType: "t", InboundAmountKg: 10, RemainingAmountKg: 10, ExpiryDate: time.Now().Add(24 * time.Hour), Enabled: true}
	disabled := model.FeedBatch{BatchNumber: "OFF", FeedType: "t", InboundAmountKg: 10, RemainingAmountKg: 10, ExpiryDate: time.Now().Add(1 * time.Hour), Enabled: true}
	otherType := model.FeedBatch{BatchNumber: "OTHER", FeedType: "x", InboundAmountKg: 10, RemainingAmountKg: 10, ExpiryDate: time.Now().Add(2 * time.Hour), Enabled: true}
	for _, b := range []*model.FeedBatch{&late, &early, &disabled, &otherType} {
		if err := repo.Save(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.FeedBatch{}).Where("batch_number = ?", "OFF").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	batches, err := repo.AvailableForUpdate("t")
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || batches[0].BatchNumber != "EARLY" || batches[1].BatchNumber != "LATE" {
		t.Fatalf("expected FEFO order [EARLY LATE], got %v", batchNumbers(batches))
	}
}

func batchNumbers(batches []model.FeedBatch) []string {
	out := make([]string, 0, len(batches))
	for _, b := range batches {
		out = append(out, b.BatchNumber)
	}
	return out
}
