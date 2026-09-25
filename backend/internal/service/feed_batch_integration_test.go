package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type feedFixture struct {
	db           *gorm.DB
	executions   *ExecutionService
	batchService *FeedBatchService
	actor        Actor
	pond         model.Pond
	plan         model.FeedingPlan
	execution    model.ControlExecution
}

func newFeedFixture(t *testing.T) *feedFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Pond{}, &model.WaterReading{}, &model.FeedingPlan{},
		&model.ControlExecution{}, &model.FeedBatch{}, &model.FeedConsumption{}, &model.AuditLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pond := model.Pond{Code: "P-T01", Name: "测试塘", Species: "南美白对虾", AreaSquareMeters: 2000, CapacityKg: 100000, GrowthStage: "成长期", Status: constants.PondStatusActive, Manager: "测试员"}
	if err := db.Create(&pond).Error; err != nil {
		t.Fatalf("create pond: %v", err)
	}
	now := time.Now().UTC()
	reading := model.WaterReading{
		PondID: pond.ID, DissolvedOxygen: 7.2, Temperature: 26, PH: 7.4, Ammonia: 0.1, Turbidity: 20,
		MeasuredAt: now.Add(-1 * time.Hour), Source: "manual", RiskLevel: constants.RiskNormal,
	}
	if err := db.Create(&reading).Error; err != nil {
		t.Fatalf("create reading: %v", err)
	}
	plan := model.FeedingPlan{
		PondID: pond.ID, Name: "对虾秋季计划", Version: 1, DailyAmountKg: 100, FrequencyPerDay: 2,
		FeedType: "对虾配合饲料", TargetGrowthStage: "成长期", MinOxygen: 5,
		StartDate: now.Add(-24 * time.Hour), EndDate: now.Add(30 * 24 * time.Hour),
		Status: constants.PlanStatusApproved, CreatedBy: "测试员",
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	execution := model.ControlExecution{
		PondID: pond.ID, FeedingPlanID: plan.ID, ScheduledAt: now.Add(2 * time.Hour),
		PlannedAmountKg: 40, Status: constants.ExecutionScheduled, Operator: "值班操作员",
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	audit := NewAuditService(repository.NewAuditRepository(db))
	f := &feedFixture{
		db: db,
		executions: NewExecutionService(
			repository.NewExecutionRepository(db), repository.NewPlanRepository(db),
			repository.NewPondRepository(db), repository.NewReadingRepository(db),
			repository.NewFeedBatchRepository(db), repository.NewFeedConsumptionRepository(db), audit,
		),
		batchService: NewFeedBatchService(repository.NewFeedBatchRepository(db), repository.NewFeedConsumptionRepository(db), audit),
		actor:        Actor{UserID: 1, Username: "operator", DisplayName: "值班操作员", Role: "operator"},
		pond:         pond, plan: plan, execution: execution,
	}
	return f
}

func dayOffset(days int) string {
	value := time.Now().UTC().AddDate(0, 0, days)
	return value.Format("2006-01-02")
}

func (f *feedFixture) createBatch(t *testing.T, batchNo, feedType string, inbound float64, expireOffset int, enabled bool) {
	t.Helper()
	input := dto.FeedBatchInput{BatchNo: batchNo, FeedType: feedType, InboundKg: inbound, ExpireDate: dayOffset(expireOffset), Enabled: &enabled, Notes: "集成测试"}
	if _, err := f.batchService.Create(input, f.actor); err != nil {
		t.Fatalf("create batch %s: %v", batchNo, err)
	}
}

func (f *feedFixture) completeInput() dto.CompleteExecutionInput {
	return dto.CompleteExecutionInput{ActualAmountKg: 40, OxygenSnapshot: 6.5, Feedback: "摄食正常，按计划完成本次投喂。"}
}

func (f *feedFixture) consumptionCount() int64 {
	var count int64
	f.db.Model(&model.FeedConsumption{}).Count(&count)
	return count
}

func TestCompleteFailsWithoutMatchingFeedType(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-OTHER", "鱼用膨化饲料", 500, 60, true)
	_, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor)
	if err == nil {
		t.Fatal("completion must be rejected when feed type does not match")
	}
}

func TestCompleteRejectsExpiredBatches(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-OLD", "对虾配合饲料", 500, -1, true)
	_, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor)
	if err == nil {
		t.Fatal("completion must be rejected when all matching batches are expired")
	}
	if f.consumptionCount() != 0 {
		t.Fatal("rejected completion must not leave consumption rows")
	}
}

func TestCompleteRejectsDisabledBatches(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-OFF", "对虾配合饲料", 500, 60, false)
	if _, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor); err == nil {
		t.Fatal("completion must be rejected when matching batches are disabled")
	}
}

func TestCompleteRejectsInsufficientRemaining(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-LOW", "对虾配合饲料", 10, 60, true)
	_, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor)
	if err == nil {
		t.Fatal("completion must be rejected when remaining stock is insufficient")
	}
}

func TestCompleteAllocatesFEFOAndKeepsDetails(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-EARLY", "对虾配合饲料", 10, 5, true)
	f.createBatch(t, "FB-LATER", "对虾配合饲料", 100, 60, true)
	completed, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor)
	if err != nil {
		t.Fatalf("completion should succeed with sufficient stock: %v", err)
	}
	if len(completed.Consumptions) != 2 {
		t.Fatalf("expected FEFO split over 2 batches, got %d", len(completed.Consumptions))
	}
	early := completed.Consumptions[0]
	later := completed.Consumptions[1]
	if early.FeedBatch == nil || early.FeedBatch.BatchNo != "FB-EARLY" || early.AmountKg != 10 {
		t.Fatalf("earliest expiring batch must be drained first, got %+v", early)
	}
	if later.AmountKg != 30 {
		t.Fatalf("remainder must come from later batch, got %v", later.AmountKg)
	}
	detail, err := f.batchService.Get(early.FeedBatchID)
	if err != nil {
		t.Fatalf("get batch detail: %v", err)
	}
	if detail.RemainingKg != 0 || len(detail.Consumptions) != 1 {
		t.Fatalf("batch detail should show zero remaining and one linked feeding, got %+v", detail.FeedBatchView)
	}
	if detail.Consumptions[0].ControlExecutionID != f.execution.ID {
		t.Fatal("consumption detail must link back to the execution")
	}
	available, err := f.batchService.Available("对虾配合饲料")
	if err != nil {
		t.Fatalf("list available: %v", err)
	}
	var laterRemaining float64
	for _, batch := range available {
		if batch.BatchNo == "FB-LATER" {
			laterRemaining = batch.RemainingKg
		}
	}
	if laterRemaining != 70 {
		t.Fatalf("later batch remaining should be 70 kg, got %v", laterRemaining)
	}
}

func TestCompletedDeductionCannotChange(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-EARLY", "对虾配合饲料", 10, 5, true)
	f.createBatch(t, "FB-LATER", "对虾配合饲料", 100, 60, true)
	if _, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if _, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor); err == nil {
		t.Fatal("completed execution must not be completed a second time")
	}
	if f.consumptionCount() != 2 {
		t.Fatal("second completion attempt must not add consumption rows")
	}
	update := dto.UpdateExecutionInput{
		ScheduledAt: f.execution.ScheduledAt, PlannedAmountKg: f.execution.PlannedAmountKg,
		Weather: f.execution.Weather, Status: constants.ExecutionScheduled,
	}
	if _, err := f.executions.Update(f.execution.ID, update, f.actor); err == nil {
		t.Fatal("completed execution must be frozen against edits")
	}
}

func TestConcurrentCompletionOnlyDeductsOnce(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-STOCK", "对虾配合饲料", 500, 60, true)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, failures := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("expected exactly one concurrent completion to succeed, got %d ok / %d fail", successes, failures)
	}
	if f.consumptionCount() != 1 {
		t.Fatalf("exactly one consumption record allowed, found %d", f.consumptionCount())
	}
	var inbound, consumed float64
	f.db.Raw("SELECT inbound_kg FROM feed_batches LIMIT 1").Row().Scan(&inbound)
	f.db.Raw("SELECT COALESCE(SUM(amount_kg), 0) FROM feed_consumptions").Row().Scan(&consumed)
	if inbound-consumed != 460 {
		t.Fatalf("stock should be deducted once by 40 kg: inbound=%v consumed=%v", inbound, consumed)
	}
}

func TestBatchLedgerLocksConsumedFields(t *testing.T) {
	f := newFeedFixture(t)
	f.createBatch(t, "FB-EARLY", "对虾配合饲料", 10, 5, true)
	f.createBatch(t, "FB-LATER", "对虾配合饲料", 100, 60, true)
	if _, err := f.executions.Complete(f.execution.ID, f.completeInput(), f.actor); err != nil {
		t.Fatalf("completion: %v", err)
	}
	var early model.FeedBatch
	if err := f.db.Where("batch_no = ?", "FB-EARLY").First(&early).Error; err != nil {
		t.Fatalf("load batch: %v", err)
	}
	tampered := dto.FeedBatchInput{BatchNo: "FB-EARLY", FeedType: "鱼用膨化饲料", InboundKg: 10, ExpireDate: dayOffset(5), Notes: "改类型"}
	if _, err := f.batchService.Update(early.ID, tampered, f.actor); err == nil {
		t.Fatal("feed type of a consumed batch must be locked")
	}
	disableInput := dto.FeedBatchInput{BatchNo: "FB-EARLY", FeedType: "对虾配合饲料", InboundKg: 10, ExpireDate: dayOffset(5), Enabled: boolPtr(false), Notes: "停用"}
	if _, err := f.batchService.Update(early.ID, disableInput, f.actor); err != nil {
		t.Fatalf("disabling a consumed batch should be allowed: %v", err)
	}
	if err := f.batchService.Delete(early.ID, f.actor); err == nil {
		t.Fatal("deleting a consumed batch must be rejected")
	}
}

func boolPtr(v bool) *bool { return &v }
