package repository

import (
	"aquaculture-water-feeding-control/backend/internal/model"

	"gorm.io/gorm"
)

type FeedConsumptionRepository struct {
	db *gorm.DB
}

func NewFeedConsumptionRepository(db *gorm.DB) *FeedConsumptionRepository {
	return &FeedConsumptionRepository{db: db}
}

// SumByBatchIDs 按批次汇总已消耗量（已完成扣减不可改，即历史全部消耗）
func (r *FeedConsumptionRepository) SumByBatchIDs(batchIDs []uint) (map[uint]float64, error) {
	result := make(map[uint]float64)
	if len(batchIDs) == 0 {
		return result, nil
	}
	type row struct {
		FeedBatchID uint
		Total       float64
	}
	var rows []row
	err := r.db.Model(&model.FeedConsumption{}).
		Select("feed_batch_id AS feed_batch_id, COALESCE(SUM(amount_kg), 0) AS total").
		Where("feed_batch_id IN ?", batchIDs).
		Group("feed_batch_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		result[item.FeedBatchID] = item.Total
	}
	return result, nil
}

func (r *FeedConsumptionRepository) CountByBatchID(batchID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.FeedConsumption{}).Where("feed_batch_id = ?", batchID).Count(&count).Error
	return count, err
}

func (r *FeedConsumptionRepository) ListByBatchID(batchID uint, limit int) ([]model.FeedConsumption, error) {
	var items []model.FeedConsumption
	err := r.db.Preload("Pond").Preload("FeedingPlan").Preload("ControlExecution").
		Where("feed_batch_id = ?", batchID).Order("created_at DESC, id DESC").Limit(limit).Find(&items).Error
	return items, err
}

func (r *FeedConsumptionRepository) Create(items []model.FeedConsumption) error {
	if len(items) == 0 {
		return nil
	}
	// 同一事务内批量插入；(执行, 批次) 联合唯一索引在多行 VALUES 下可被各数据库正确校验
	return r.db.Create(&items).Error
}
