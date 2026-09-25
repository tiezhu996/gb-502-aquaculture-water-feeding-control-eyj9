package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type feedFixture struct {
	db        *gorm.DB
	exec      *ExecutionService
	batchSvc  *FeedBatchService
	planID    uint
	pondID    uint
	execID    uint
	createdID uint
}

func newFeedFixture(t *testing.T, batches []model.FeedBatch) feedFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:svc-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		if strings.Contains(err.Error(), "CGO_ENABLED=0") {
			t.Skip("sqlite 集成测试需要 CGO，已跳过")
		}
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Pond{}, &model.WaterReading{}, &model.FeedingPlan{},
		&model.ControlExecution{}, &model.AuditLog{}, &model.FeedBatch{}, &model.FeedConsumption{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	pond := model.Pond{Code: "P-T1", Name: "测试塘", Species: "对虾", AreaSquareMeters: 1000, CapacityKg: 5000, GrowthStage: "成长期", Status: constants.PondStatusActive, Manager: "测试员"}
	if err := db.Create(&pond).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.FeedingPlan{
		PondID: pond.ID, Name: "测试计划", Version: 1, DailyAmountKg: 100, FrequencyPerDay: 2,
		FeedType: "配合饲料", TargetGrowthStage: "成长期", MinOxygen: 4,
		StartDate: now.Add(-24 * time.Hour), EndDate: now.Add(7 * 24 * time.Hour),
		Status: constants.PlanStatusApproved, CreatedBy: "tester",
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	reading := model.WaterReading{
		PondID: pond.ID, DissolvedOxygen: 7, Temperature: 25, PH: 7.5, Ammonia: 0.1, Turbidity: 20,
		MeasuredAt: now, Source: "manual", RiskLevel: constants.RiskNormal,
	}
	if err := db.Create(&reading).Error; err != nil {
		t.Fatal(err)
	}
	execution := model.ControlExecution{
		PondID: pond.ID, FeedingPlanID: plan.ID, ScheduledAt: now.Add(time.Hour),
		PlannedAmountKg: 40, Status: constants.ExecutionScheduled, Operator: "测试员", OxygenSnapshot: 7,
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	for i := range batches {
		enabled := batches[i].Enabled
		batches[i].Enabled = true // GORM 对带 default 标签的零值字段在 INSERT 时省略，先按启用写入
		if err := db.Create(&batches[i]).Error; err != nil {
			t.Fatalf("seed batch: %v", err)
		}
		if !enabled {
			if err := db.Model(&model.FeedBatch{}).Where("id = ?", batches[i].ID).Update("enabled", false).Error; err != nil {
				t.Fatal(err)
			}
			batches[i].Enabled = false
		}
	}
	audit := NewAuditService(repository.NewAuditRepository(db))
	exec := NewExecutionService(
		repository.NewExecutionRepository(db), repository.NewPlanRepository(db),
		repository.NewPondRepository(db), repository.NewReadingRepository(db),
		repository.NewFeedBatchRepository(db), audit,
	)
	return feedFixture{db: db, exec: exec, batchSvc: NewFeedBatchService(repository.NewFeedBatchRepository(db), audit), planID: plan.ID, pondID: pond.ID, execID: execution.ID}
}

func completeInput(amount float64) dto.CompleteExecutionInput {
	return dto.CompleteExecutionInput{ActualAmountKg: amount, OxygenSnapshot: 7, Feedback: "现场摄食正常，按计划完成投喂操作"}
}

func TestCompleteFefoDeductsEarliestExpiryFirst(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "LATER", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: true},
		{BatchNumber: "EARLIER", FeedType: "配合饲料", InboundAmountKg: 30, RemainingAmountKg: 30, ExpiryDate: time.Now().Add(24 * time.Hour), Enabled: true},
	})
	if _, err := fx.exec.Complete(fx.execID, completeInput(50), Actor{Username: "operator", DisplayName: "操作员"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var earlier, later model.FeedBatch
	fx.db.Where("batch_number = ?", "EARLIER").First(&earlier)
	fx.db.Where("batch_number = ?", "LATER").First(&later)
	if earlier.RemainingAmountKg != 0 {
		t.Fatalf("earlier batch should be depleted, got %v", earlier.RemainingAmountKg)
	}
	if later.RemainingAmountKg != 80 {
		t.Fatalf("later batch should keep 80 kg, got %v", later.RemainingAmountKg)
	}
	var consumptions []model.FeedConsumption
	fx.db.Order("amount_kg DESC").Find(&consumptions)
	if len(consumptions) != 2 {
		t.Fatalf("expected 2 consumption lines, got %d", len(consumptions))
	}
}

func TestCompleteRejectsExpiredBatches(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "EXPIRED", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(-24 * time.Hour), Enabled: true},
	})
	_, err := fx.exec.Complete(fx.execID, completeInput(10), Actor{Username: "operator"})
	if err == nil || err.Error() == "" {
		t.Fatal("expected completion rejected when only expired batches exist")
	}
	assertConflict(t, err, "过期")
	var count int64
	fx.db.Model(&model.FeedConsumption{}).Count(&count)
	if count != 0 {
		t.Fatalf("no consumptions expected on rejection, got %d", count)
	}
	var exec model.ControlExecution
	fx.db.First(&exec, fx.execID)
	if exec.Status != constants.ExecutionScheduled {
		t.Fatalf("execution must stay scheduled after rejection, got %s", exec.Status)
	}
}

func TestCompleteRejectsDisabledBatches(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "DISABLED", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: false},
	})
	_, err := fx.exec.Complete(fx.execID, completeInput(10), Actor{Username: "operator"})
	assertConflict(t, err, "停用")
}

func TestCompleteRejectsTypeMismatch(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "OTHER", FeedType: "膨化饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: true},
	})
	_, err := fx.exec.Complete(fx.execID, completeInput(10), Actor{Username: "operator"})
	assertConflict(t, err, "配合饲料")
}

func TestCompleteRejectsInsufficientBalance(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "SMALL1", FeedType: "配合饲料", InboundAmountKg: 20, RemainingAmountKg: 20, ExpiryDate: time.Now().Add(24 * time.Hour), Enabled: true},
		{BatchNumber: "SMALL2", FeedType: "配合饲料", InboundAmountKg: 20, RemainingAmountKg: 20, ExpiryDate: time.Now().Add(48 * time.Hour), Enabled: true},
	})
	_, err := fx.exec.Complete(fx.execID, completeInput(50), Actor{Username: "operator"})
	assertConflict(t, err, "余量不足")
	var remaining float64
	fx.db.Model(&model.FeedBatch{}).Select("COALESCE(SUM(remaining_amount_kg),0)").Scan(&remaining)
	if remaining != 40 {
		t.Fatalf("balances must be untouched after rejection, got total %v", remaining)
	}
}

func TestCompleteIdempotentDeduction(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "B", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: true},
	})
	if _, err := fx.exec.Complete(fx.execID, completeInput(30), Actor{Username: "operator"}); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	// 已完成执行不能再次提交反馈（状态终态 + 消耗明细双重保护）
	_, err := fx.exec.Complete(fx.execID, completeInput(30), Actor{Username: "operator"})
	assertConflict(t, err, "不能提交反馈")
	var remaining float64
	fx.db.Model(&model.FeedBatch{}).Select("remaining_amount_kg").Scan(&remaining)
	if remaining != 70 {
		t.Fatalf("second completion must not deduct again, remaining=%v", remaining)
	}
	var count int64
	fx.db.Model(&model.FeedConsumption{}).Where("control_execution_id = ?", fx.execID).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 immutable consumption line, got %d", count)
	}
}

func assertConflict(t *testing.T, err error, wantPart string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected conflict error containing %q, got nil", wantPart)
	}
	appErr, ok := err.(*AppError)
	if !ok || appErr.Code != CodeConflict {
		t.Fatalf("expected CodeConflict, got %T %v", err, err)
	}
	if !contains(appErr.Message, wantPart) {
		t.Fatalf("error message %q should contain %q", appErr.Message, wantPart)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
