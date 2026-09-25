package service

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"strings"
	"time"

	"gorm.io/gorm"
)

type FeedBatchService struct {
	batches       *repository.FeedBatchRepository
	consumptions  *repository.FeedConsumptionRepository
	audit         *AuditService
	transactional bool
}

func NewFeedBatchService(batches *repository.FeedBatchRepository, consumptions *repository.FeedConsumptionRepository, audit *AuditService) *FeedBatchService {
	return &FeedBatchService{batches: batches, consumptions: consumptions, audit: audit}
}

func (s *FeedBatchService) withinTransaction(fn func(*FeedBatchService) error) error {
	return s.audit.WithinTransaction(func(tx *gorm.DB, audit *AuditService) error {
		scoped := &FeedBatchService{
			batches: repository.NewFeedBatchRepository(tx), consumptions: repository.NewFeedConsumptionRepository(tx),
			audit: audit, transactional: true,
		}
		return fn(scoped)
	})
}

func startOfUTCDay(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func parseExpireDate(value string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), time.UTC)
	if err != nil {
		return time.Time{}, err
	}
	return startOfUTCDay(parsed), nil
}

func (s *FeedBatchService) toView(batch model.FeedBatch, consumedKg float64, now time.Time) dto.FeedBatchView {
	remaining := batch.InboundKg - consumedKg
	if remaining < 0 {
		remaining = 0
	}
	return dto.FeedBatchView{
		ID: batch.ID, CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt,
		BatchNo: batch.BatchNo, FeedType: batch.FeedType, InboundKg: batch.InboundKg,
		ExpireDate: batch.ExpireDate, Enabled: batch.Enabled, Notes: batch.Notes,
		ConsumedKg: consumedKg, RemainingKg: remaining,
		Expired: batch.ExpireDate.Before(startOfUTCDay(now)),
	}
}

func (s *FeedBatchService) List(query dto.PageQuery, feedType string, enabled *bool) (dto.PageResult[dto.FeedBatchView], error) {
	query.Normalize()
	batches, total, err := s.batches.List(query, strings.TrimSpace(feedType), enabled)
	if err != nil {
		return dto.PageResult[dto.FeedBatchView]{}, WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	ids := make([]uint, 0, len(batches))
	for _, batch := range batches {
		ids = append(ids, batch.ID)
	}
	consumed, err := s.consumptions.SumByBatchIDs(ids)
	if err != nil {
		return dto.PageResult[dto.FeedBatchView]{}, WrapError(CodeInternal, "汇总批次余量失败", err)
	}
	now := time.Now().UTC()
	items := make([]dto.FeedBatchView, 0, len(batches))
	for _, batch := range batches {
		items = append(items, s.toView(batch, consumed[batch.ID], now))
	}
	return dto.PageResult[dto.FeedBatchView]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (s *FeedBatchService) Get(id uint) (dto.FeedBatchDetail, error) {
	batch, err := s.batches.Get(id)
	if err == gorm.ErrRecordNotFound {
		return dto.FeedBatchDetail{}, NewError(CodeNotFound, "饲料批次不存在")
	}
	if err != nil {
		return dto.FeedBatchDetail{}, WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	consumedMap, err := s.consumptions.SumByBatchIDs([]uint{id})
	if err != nil {
		return dto.FeedBatchDetail{}, WrapError(CodeInternal, "汇总批次余量失败", err)
	}
	records, err := s.consumptions.ListByBatchID(id, 100)
	if err != nil {
		return dto.FeedBatchDetail{}, WrapError(CodeInternal, "查询关联投喂失败", err)
	}
	views := make([]dto.FeedConsumptionView, 0, len(records))
	for _, record := range records {
		views = append(views, consumptionToView(record, batch.BatchNo))
	}
	return dto.FeedBatchDetail{FeedBatchView: s.toView(batch, consumedMap[id], time.Now().UTC()), Consumptions: views}, nil
}

func consumptionToView(record model.FeedConsumption, batchNo string) dto.FeedConsumptionView {
	view := dto.FeedConsumptionView{
		ID: record.ID, CreatedAt: record.CreatedAt, FeedBatchID: record.FeedBatchID, BatchNo: batchNo,
		ControlExecutionID: record.ControlExecutionID, FeedingPlanID: record.FeedingPlanID,
		PondID: record.PondID, FeedType: record.FeedType, AmountKg: record.AmountKg,
	}
	if record.FeedBatch != nil {
		view.BatchNo = record.FeedBatch.BatchNo
	}
	if record.Pond != nil {
		view.PondName = record.Pond.Name
	}
	if record.FeedingPlan != nil {
		view.PlanName = record.FeedingPlan.Name
	}
	if record.ControlExecution != nil {
		view.CompletedAt = record.ControlExecution.CompletedAt
		view.Operator = record.ControlExecution.Operator
	}
	return view
}

// Available 执行页展示：某类型启用且未过期批次的剩余量（先到期先出）
func (s *FeedBatchService) Available(feedType string) ([]dto.FeedBatchView, error) {
	feedType = strings.TrimSpace(feedType)
	if feedType == "" {
		return nil, NewError(CodeValidation, "饲料类型不能为空")
	}
	now := time.Now().UTC()
	batches, err := s.batches.Available(feedType, startOfUTCDay(now))
	if err != nil {
		return nil, WrapError(CodeInternal, "查询可用饲料批次失败", err)
	}
	ids := make([]uint, 0, len(batches))
	for _, batch := range batches {
		ids = append(ids, batch.ID)
	}
	consumed, err := s.consumptions.SumByBatchIDs(ids)
	if err != nil {
		return nil, WrapError(CodeInternal, "汇总批次余量失败", err)
	}
	items := make([]dto.FeedBatchView, 0, len(batches))
	for _, batch := range batches {
		if remaining := batch.InboundKg - consumed[batch.ID]; remaining > 1e-9 {
			items = append(items, s.toView(batch, consumed[batch.ID], now))
		}
	}
	return items, nil
}

func (s *FeedBatchService) Create(input dto.FeedBatchInput, actor Actor) (dto.FeedBatchView, error) {
	if !s.transactional {
		var result dto.FeedBatchView
		err := s.withinTransaction(func(scoped *FeedBatchService) error {
			var inner error
			result, inner = scoped.Create(input, actor)
			return inner
		})
		return result, err
	}
	batch, err := s.buildBatch(input, model.FeedBatch{Enabled: true})
	if err != nil {
		return dto.FeedBatchView{}, err
	}
	if err := s.batches.Create(&batch); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return dto.FeedBatchView{}, NewError(CodeConflict, "饲料批号已存在")
		}
		return dto.FeedBatchView{}, WrapError(CodeInternal, "登记饲料批次失败", err)
	}
	if err := s.audit.Record(actor, "create", "feed_batch", batch.ID, nil, batch, "登记饲料批次入库"); err != nil {
		return dto.FeedBatchView{}, err
	}
	return s.toView(batch, 0, time.Now().UTC()), nil
}

func (s *FeedBatchService) buildBatch(input dto.FeedBatchInput, existing model.FeedBatch) (model.FeedBatch, error) {
	expireDate, err := parseExpireDate(input.ExpireDate)
	if err != nil {
		return model.FeedBatch{}, NewError(CodeValidation, "到期日格式无效，应为 YYYY-MM-DD")
	}
	batchNo := strings.ToUpper(strings.TrimSpace(input.BatchNo))
	if batchNo == "" {
		return model.FeedBatch{}, NewError(CodeValidation, "批号不能为空")
	}
	if input.InboundKg <= 0 {
		return model.FeedBatch{}, NewError(CodeValidation, "入库量必须大于 0")
	}
	batch := existing
	batch.BatchNo = batchNo
	batch.FeedType = strings.TrimSpace(input.FeedType)
	batch.InboundKg = input.InboundKg
	batch.ExpireDate = expireDate
	if input.Enabled != nil {
		batch.Enabled = *input.Enabled
	}
	batch.Notes = strings.TrimSpace(input.Notes)
	return batch, nil
}

func (s *FeedBatchService) Update(id uint, input dto.FeedBatchInput, actor Actor) (dto.FeedBatchView, error) {
	if !s.transactional {
		var result dto.FeedBatchView
		err := s.withinTransaction(func(scoped *FeedBatchService) error {
			var inner error
			result, inner = scoped.Update(id, input, actor)
			return inner
		})
		return result, err
	}
	batch, err := s.batches.GetForUpdate(id)
	if err == gorm.ErrRecordNotFound {
		return dto.FeedBatchView{}, NewError(CodeNotFound, "饲料批次不存在")
	}
	if err != nil {
		return dto.FeedBatchView{}, WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	usedCount, err := s.consumptions.CountByBatchID(id)
	if err != nil {
		return dto.FeedBatchView{}, WrapError(CodeInternal, "检查批次消耗失败", err)
	}
	if usedCount > 0 {
		batchNo := strings.ToUpper(strings.TrimSpace(input.BatchNo))
		if batchNo != batch.BatchNo || strings.TrimSpace(input.FeedType) != batch.FeedType || input.InboundKg != batch.InboundKg {
			return dto.FeedBatchView{}, NewError(CodeConflict, "批次已发生投喂消耗，批号、类型和入库量不能修改")
		}
	}
	before := batch
	updated, err := s.buildBatch(input, batch)
	if err != nil {
		return dto.FeedBatchView{}, err
	}
	if input.Enabled != nil {
		updated.Enabled = *input.Enabled
	}
	if err := s.batches.Save(&updated); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return dto.FeedBatchView{}, NewError(CodeConflict, "饲料批号已存在")
		}
		return dto.FeedBatchView{}, WrapError(CodeInternal, "更新饲料批次失败", err)
	}
	if err := s.audit.Record(actor, "update", "feed_batch", updated.ID, before, updated, "更新饲料批次台账信息"); err != nil {
		return dto.FeedBatchView{}, err
	}
	consumed, err := s.consumptions.SumByBatchIDs([]uint{id})
	if err != nil {
		return dto.FeedBatchView{}, WrapError(CodeInternal, "汇总批次余量失败", err)
	}
	return s.toView(updated, consumed[id], time.Now().UTC()), nil
}

func (s *FeedBatchService) SetEnabled(id uint, enabled bool, actor Actor) (dto.FeedBatchView, error) {
	if !s.transactional {
		var result dto.FeedBatchView
		err := s.withinTransaction(func(scoped *FeedBatchService) error {
			var inner error
			result, inner = scoped.SetEnabled(id, enabled, actor)
			return inner
		})
		return result, err
	}
	batch, err := s.batches.GetForUpdate(id)
	if err == gorm.ErrRecordNotFound {
		return dto.FeedBatchView{}, NewError(CodeNotFound, "饲料批次不存在")
	}
	if err != nil {
		return dto.FeedBatchView{}, WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	if batch.Enabled == enabled {
		consumed, err := s.consumptions.SumByBatchIDs([]uint{id})
		if err != nil {
			return dto.FeedBatchView{}, WrapError(CodeInternal, "汇总批次余量失败", err)
		}
		return s.toView(batch, consumed[id], time.Now().UTC()), nil
	}
	before := batch
	batch.Enabled = enabled
	if err := s.batches.Save(&batch); err != nil {
		return dto.FeedBatchView{}, WrapError(CodeInternal, "更新批次启用状态失败", err)
	}
	reason := "停用饲料批次，停用后不再参与投喂扣减"
	if enabled {
		reason = "启用饲料批次，参与同类型投喂先到期先出"
	}
	if err := s.audit.Record(actor, "update", "feed_batch", batch.ID, before, batch, reason); err != nil {
		return dto.FeedBatchView{}, err
	}
	consumed, err := s.consumptions.SumByBatchIDs([]uint{id})
	if err != nil {
		return dto.FeedBatchView{}, WrapError(CodeInternal, "汇总批次余量失败", err)
	}
	return s.toView(batch, consumed[id], time.Now().UTC()), nil
}

func (s *FeedBatchService) Delete(id uint, actor Actor) error {
	if !s.transactional {
		return s.withinTransaction(func(scoped *FeedBatchService) error { return scoped.Delete(id, actor) })
	}
	batch, err := s.batches.GetForUpdate(id)
	if err == gorm.ErrRecordNotFound {
		return NewError(CodeNotFound, "饲料批次不存在")
	}
	if err != nil {
		return WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	usedCount, err := s.consumptions.CountByBatchID(id)
	if err != nil {
		return WrapError(CodeInternal, "检查批次消耗失败", err)
	}
	if usedCount > 0 {
		return NewError(CodeConflict, "批次已发生投喂消耗，不能删除，可改为停用")
	}
	if err := s.batches.Delete(&batch); err != nil {
		return WrapError(CodeInternal, "删除饲料批次失败", err)
	}
	return s.audit.Record(actor, "delete", "feed_batch", batch.ID, batch, nil, "删除尚无消耗的饲料批次")
}
