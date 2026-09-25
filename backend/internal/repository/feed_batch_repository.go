package repository

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FeedBatchRepository struct {
	db *gorm.DB
}

func NewFeedBatchRepository(db *gorm.DB) *FeedBatchRepository {
	return &FeedBatchRepository{db: db}
}

func (r *FeedBatchRepository) List(query dto.PageQuery, feedType string, enabled *bool) ([]model.FeedBatch, int64, error) {
	base := r.db.Model(&model.FeedBatch{})
	if search := strings.TrimSpace(query.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		base = base.Where("LOWER(batch_number) LIKE ? OR LOWER(feed_type) LIKE ?", like, like)
	}
	if feedType != "" {
		base = base.Where("feed_type = ?", feedType)
	}
	if enabled != nil {
		base = base.Where("enabled = ?", *enabled)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []model.FeedBatch
	err := base.Order("feed_type ASC, expiry_date ASC, created_at ASC").
		Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&batches).Error
	return batches, total, err
}

func (r *FeedBatchRepository) Get(id uint) (model.FeedBatch, error) {
	var batch model.FeedBatch
	err := r.db.First(&batch, id).Error
	return batch, err
}

func (r *FeedBatchRepository) GetByNumber(batchNumber string) (model.FeedBatch, error) {
	var batch model.FeedBatch
	err := r.db.Where("batch_number = ?", batchNumber).First(&batch).Error
	return batch, err
}

func (r *FeedBatchRepository) CountByType(feedType string) (int64, error) {
	var count int64
	err := r.db.Model(&model.FeedBatch{}).Where("feed_type = ?", feedType).Count(&count).Error
	return count, err
}

// AvailableForUpdate 锁定指定类型的启用批次并按到期日升序返回（FEFO），
// 过期过滤由调用方结合当前时间完成。
func (r *FeedBatchRepository) AvailableForUpdate(feedType string) ([]model.FeedBatch, error) {
	var batches []model.FeedBatch
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("feed_type = ? AND enabled = ?", feedType, true).
		Order("expiry_date ASC, id ASC").Find(&batches).Error
	return batches, err
}

// Deduct 条件更新原子扣减余量；余量不足时影响行数为 0，防止并发超扣。
// 0.000001 kg 容差用于消除浮点累计误差（如 100-60 的二进制表示误差）。
func (r *FeedBatchRepository) Deduct(id uint, amount float64) (int64, error) {
	result := r.db.Model(&model.FeedBatch{}).
		Where("id = ? AND remaining_amount_kg + 0.000001 >= ?", id, amount).
		Update("remaining_amount_kg", gorm.Expr("remaining_amount_kg - ?", amount))
	return result.RowsAffected, result.Error
}

func (r *FeedBatchRepository) Save(batch *model.FeedBatch) error {
	return r.db.Save(batch).Error
}

func (r *FeedBatchRepository) ConsumptionCount(batchID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.FeedConsumption{}).Where("feed_batch_id = ?", batchID).Count(&count).Error
	return count, err
}

func (r *FeedBatchRepository) ConsumptionsByBatch(batchID uint) ([]model.FeedConsumption, int64, error) {
	var total int64
	if err := r.db.Model(&model.FeedConsumption{}).Where("feed_batch_id = ?", batchID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.FeedConsumption
	err := r.db.Preload("ControlExecution").Preload("ControlExecution.Pond").
		Where("feed_batch_id = ?", batchID).Order("created_at DESC").Find(&items).Error
	return items, total, err
}

func (r *FeedBatchRepository) CreateConsumption(consumption *model.FeedConsumption) error {
	return r.db.Create(consumption).Error
}

func (r *FeedBatchRepository) ConsumptionCountForExecution(executionID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.FeedConsumption{}).Where("control_execution_id = ?", executionID).Count(&count).Error
	return count, err
}
