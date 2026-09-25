package service

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"testing"
	"time"
)

func TestFeedBatchRegisterAndToggle(t *testing.T) {
	fx := newFeedFixture(t, nil)
	actor := Actor{Username: "manager", DisplayName: "主管"}
	input := dto.FeedBatchInput{
		BatchNumber: "fb-new-01", FeedType: "配合饲料", InboundAmountKg: 500,
		ExpiryDate: time.Now().Add(60 * 24 * time.Hour), Enabled: nil, Notes: "首批",
	}
	batch, err := fx.batchSvc.Create(input, actor)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if batch.RemainingAmountKg != 500 {
		t.Fatalf("new batch remaining should equal inbound, got %v", batch.RemainingAmountKg)
	}
	// 默认启用
	if !batch.Enabled {
		t.Fatal("new batch should be enabled by default")
	}
	// 重复批号拒绝
	_, err = fx.batchSvc.Create(input, actor)
	if appErr, ok := err.(*AppError); !ok || appErr.Code != CodeConflict {
		t.Fatalf("duplicate batch number should conflict, got %v", err)
	}
	// 停用后不参与扣减
	disabled := false
	if _, err := fx.batchSvc.Update(batch.ID, dto.FeedBatchInput{
		BatchNumber: "FB-NEW-01", FeedType: "配合饲料", InboundAmountKg: 500,
		ExpiryDate: input.ExpiryDate, Enabled: &disabled, Notes: "首批",
	}, actor); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_, err = fx.exec.Complete(fx.execID, completeInput(10), actor)
	assertConflict(t, err, "停用")
}

func TestFeedBatchExpiredRegistrationRejected(t *testing.T) {
	fx := newFeedFixture(t, nil)
	_, err := fx.batchSvc.Create(dto.FeedBatchInput{
		BatchNumber: "FB-OLD", FeedType: "配合饲料", InboundAmountKg: 100,
		ExpiryDate: time.Now().Add(-48 * time.Hour),
	}, Actor{Username: "manager"})
	if appErr, ok := err.(*AppError); !ok || appErr.Code != CodeValidation {
		t.Fatalf("expired batch registration should be validation error, got %v", err)
	}
}

func TestFeedBatchConsumedBatchImmutableType(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "KEEP", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: true},
	})
	if _, err := fx.exec.Complete(fx.execID, completeInput(20), Actor{Username: "operator"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var batch model.FeedBatch
	fx.db.Where("batch_number = ?", "KEEP").First(&batch)
	enabled := true
	// 已有消耗后改类型必须拒绝
	_, err := fx.batchSvc.Update(batch.ID, dto.FeedBatchInput{
		BatchNumber: "KEEP", FeedType: "其他饲料", InboundAmountKg: 100,
		ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: &enabled,
	}, Actor{Username: "manager"})
	assertConflict(t, err, "饲料类型")
	// 补登入库量允许，增加额进入余量
	updated, err := fx.batchSvc.Update(batch.ID, dto.FeedBatchInput{
		BatchNumber: "KEEP", FeedType: "配合饲料", InboundAmountKg: 120,
		ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: &enabled,
	}, Actor{Username: "manager"})
	if err != nil {
		t.Fatalf("inbound top-up: %v", err)
	}
	if updated.RemainingAmountKg != 100 {
		t.Fatalf("remaining should be 80 consumed + 20 topped up = 100, got %v", updated.RemainingAmountKg)
	}
}

func TestFeedBatchConsumptionsView(t *testing.T) {
	fx := newFeedFixture(t, []model.FeedBatch{
		{BatchNumber: "VIEW", FeedType: "配合饲料", InboundAmountKg: 100, RemainingAmountKg: 100, ExpiryDate: time.Now().Add(72 * time.Hour), Enabled: true},
	})
	if _, err := fx.exec.Complete(fx.execID, completeInput(25), Actor{Username: "operator", DisplayName: "操作员"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var batch model.FeedBatch
	fx.db.Where("batch_number = ?", "VIEW").First(&batch)
	items, total, err := fx.batchSvc.Consumptions(batch.ID)
	if err != nil {
		t.Fatalf("consumptions: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].AmountKg != 25 {
		t.Fatalf("unexpected consumptions: total=%d len=%d", total, len(items))
	}
	if items[0].ControlExecution == nil || items[0].ControlExecution.Pond == nil {
		t.Fatal("consumption view should preload execution and pond")
	}
}
