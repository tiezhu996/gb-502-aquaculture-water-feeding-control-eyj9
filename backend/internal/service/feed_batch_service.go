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
	repo          *repository.FeedBatchRepository
	audit         *AuditService
	transactional bool
}

func (s *FeedBatchService) withinTransaction(fn func(*FeedBatchService) error) error {
	return s.audit.WithinTransaction(func(tx *gorm.DB, audit *AuditService) error {
		return fn(&FeedBatchService{repo: repository.NewFeedBatchRepository(tx), audit: audit, transactional: true})
	})
}

func NewFeedBatchService(repo *repository.FeedBatchRepository, audit *AuditService) *FeedBatchService {
	return &FeedBatchService{repo: repo, audit: audit}
}

func (s *FeedBatchService) List(query dto.PageQuery, feedType string, enabled *bool) (dto.PageResult[model.FeedBatch], error) {
	query.Normalize()
	items, total, err := s.repo.List(query, strings.TrimSpace(feedType), enabled)
	if err != nil {
		return dto.PageResult[model.FeedBatch]{}, WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	return dto.PageResult[model.FeedBatch]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (s *FeedBatchService) Get(id uint) (model.FeedBatch, error) {
	batch, err := s.repo.Get(id)
	if err == gorm.ErrRecordNotFound {
		return model.FeedBatch{}, NewError(CodeNotFound, "饲料批次不存在")
	}
	if err != nil {
		return model.FeedBatch{}, WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	return batch, nil
}

func (s *FeedBatchService) Consumptions(batchID uint) ([]model.FeedConsumption, int64, error) {
	if _, err := s.Get(batchID); err != nil {
		return nil, 0, err
	}
	items, total, err := s.repo.ConsumptionsByBatch(batchID)
	if err != nil {
		return nil, 0, WrapError(CodeInternal, "查询批次投喂明细失败", err)
	}
	return items, total, nil
}

func (s *FeedBatchService) Create(input dto.FeedBatchInput, actor Actor) (model.FeedBatch, error) {
	if !s.transactional {
		var result model.FeedBatch
		err := s.withinTransaction(func(scoped *FeedBatchService) error {
			var inner error
			result, inner = scoped.Create(input, actor)
			return inner
		})
		return result, err
	}
	if err := validateBatchInput(input); err != nil {
		return model.FeedBatch{}, err
	}
	batchNumber := strings.ToUpper(strings.TrimSpace(input.BatchNumber))
	if _, err := s.repo.GetByNumber(batchNumber); err == nil {
		return model.FeedBatch{}, NewError(CodeConflict, "批次批号已存在")
	} else if err != gorm.ErrRecordNotFound {
		return model.FeedBatch{}, WrapError(CodeInternal, "查询批次批号失败", err)
	}
	batch := model.FeedBatch{
		BatchNumber:       batchNumber,
		FeedType:          strings.TrimSpace(input.FeedType),
		InboundAmountKg:   input.InboundAmountKg,
		RemainingAmountKg: input.InboundAmountKg,
		ExpiryDate:        input.ExpiryDate.UTC(),
		Enabled:           true,
		Notes:             strings.TrimSpace(input.Notes),
	}
	if input.Enabled != nil {
		batch.Enabled = *input.Enabled
	}
	if err := s.repo.Save(&batch); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return model.FeedBatch{}, NewError(CodeConflict, "批次批号已存在")
		}
		return model.FeedBatch{}, WrapError(CodeInternal, "登记饲料批次失败", err)
	}
	if err := s.audit.Record(actor, "create", "feed_batch", batch.ID, nil, batch, "登记饲料批次"); err != nil {
		return model.FeedBatch{}, err
	}
	return batch, nil
}

func (s *FeedBatchService) Update(id uint, input dto.FeedBatchInput, actor Actor) (model.FeedBatch, error) {
	if !s.transactional {
		var result model.FeedBatch
		err := s.withinTransaction(func(scoped *FeedBatchService) error {
			var inner error
			result, inner = scoped.Update(id, input, actor)
			return inner
		})
		return result, err
	}
	if err := validateBatchInput(input); err != nil {
		return model.FeedBatch{}, err
	}
	batch, err := s.repo.Get(id)
	if err == gorm.ErrRecordNotFound {
		return model.FeedBatch{}, NewError(CodeNotFound, "饲料批次不存在")
	}
	if err != nil {
		return model.FeedBatch{}, WrapError(CodeInternal, "查询饲料批次失败", err)
	}
	before := batch
	newBatchNumber := strings.ToUpper(strings.TrimSpace(input.BatchNumber))
	if existing, err := s.repo.GetByNumber(newBatchNumber); err == nil && existing.ID != id {
		return model.FeedBatch{}, NewError(CodeConflict, "批次批号已存在")
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return model.FeedBatch{}, WrapError(CodeInternal, "查询批次批号失败", err)
	}

	consumed, err := s.repo.ConsumptionCount(id)
	if err != nil {
		return model.FeedBatch{}, WrapError(CodeInternal, "检查批次消耗记录失败", err)
	}
	if consumed > 0 {
		// 已发生扣减：批号/类型/入库量不可再改，避免台账与消耗明细对不上。
		if batch.FeedType != strings.TrimSpace(input.FeedType) {
			return model.FeedBatch{}, NewError(CodeConflict, "批次已有投喂消耗，不能修改饲料类型")
		}
		if input.InboundAmountKg < batch.InboundAmountKg {
			return model.FeedBatch{}, NewError(CodeValidation, "批次已有投喂消耗，入库量不能小于原始入库量")
		}
		if input.InboundAmountKg != batch.InboundAmountKg {
			// 仅允许补登入库量，增加额同步进入余量。
			batch.RemainingAmountKg += input.InboundAmountKg - batch.InboundAmountKg
		}
	} else {
		batch.RemainingAmountKg = input.InboundAmountKg
	}
	batch.BatchNumber = newBatchNumber
	batch.FeedType = strings.TrimSpace(input.FeedType)
	batch.InboundAmountKg = input.InboundAmountKg
	batch.ExpiryDate = input.ExpiryDate.UTC()
	if input.Enabled != nil {
		batch.Enabled = *input.Enabled
	}
	batch.Notes = strings.TrimSpace(input.Notes)
	if err := s.repo.Save(&batch); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return model.FeedBatch{}, NewError(CodeConflict, "批次批号已存在")
		}
		return model.FeedBatch{}, WrapError(CodeInternal, "更新饲料批次失败", err)
	}
	if err := s.audit.Record(actor, "update", "feed_batch", batch.ID, before, batch, "更新饲料批次信息或启用状态"); err != nil {
		return model.FeedBatch{}, err
	}
	return batch, nil
}

func validateBatchInput(input dto.FeedBatchInput) error {
	expiryDay := dateOnly(input.ExpiryDate.UTC())
	if expiryDay.Before(dateOnly(time.Now().UTC())) {
		return NewError(CodeValidation, "到期日不能早于今天，临期或过期饲料不得登记入库")
	}
	return nil
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
