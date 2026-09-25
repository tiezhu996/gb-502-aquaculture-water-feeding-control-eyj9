package repository

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"strings"
	"time"

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
		base = base.Where("LOWER(batch_no) LIKE ? OR LOWER(feed_type) LIKE ?", like, like)
	}
	if feedType != "" {
		base = base.Where("feed_type = ?", feedType)
	}
	if enabled != nil {
		base = base.Where("enabled = ?", *enabled)
	}
	if query.Status == "enabled" {
		base = base.Where("enabled = ?", true)
	}
	if query.Status == "disabled" {
		base = base.Where("enabled = ?", false)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []model.FeedBatch
	err := base.Order("expire_date ASC, id ASC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&batches).Error
	return batches, total, err
}

func (r *FeedBatchRepository) Get(id uint) (model.FeedBatch, error) {
	var batch model.FeedBatch
	err := r.db.First(&batch, id).Error
	return batch, err
}

func (r *FeedBatchRepository) GetForUpdate(id uint) (model.FeedBatch, error) {
	var batch model.FeedBatch
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&batch, id).Error
	return batch, err
}

// AvailableForUpdate 取同类型、启用且未过期的批次并加行锁，按到期日先出排序
func (r *FeedBatchRepository) AvailableForUpdate(feedType string, dayStart time.Time) ([]model.FeedBatch, error) {
	var batches []model.FeedBatch
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("feed_type = ? AND enabled = ? AND expire_date >= ?", feedType, true, dayStart).
		Order("expire_date ASC, id ASC").
		Find(&batches).Error
	return batches, err
}

// AllForUpdate 锁定同类型全部批次（含停用、过期），供完成时逐类判定拒绝原因
func (r *FeedBatchRepository) AllForUpdate(feedType string) ([]model.FeedBatch, error) {
	var batches []model.FeedBatch
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("feed_type = ?", feedType).
		Order("expire_date ASC, id ASC").
		Find(&batches).Error
	return batches, err
}

// Available 不加锁地查询同类型、启用且未过期批次，供执行页展示本批剩余
func (r *FeedBatchRepository) Available(feedType string, dayStart time.Time) ([]model.FeedBatch, error) {
	var batches []model.FeedBatch
	err := r.db.Where("feed_type = ? AND enabled = ? AND expire_date >= ?", feedType, true, dayStart).
		Order("expire_date ASC, id ASC").
		Find(&batches).Error
	return batches, err
}

func (r *FeedBatchRepository) Create(batch *model.FeedBatch) error {
	return r.db.Select("*").Create(batch).Error
}

func (r *FeedBatchRepository) Save(batch *model.FeedBatch) error {
	return r.db.Select("*").Save(batch).Error
}

func (r *FeedBatchRepository) Delete(batch *model.FeedBatch) error {
	return r.db.Delete(batch).Error
}

func (r *FeedBatchRepository) DistinctFeedTypes() ([]string, error) {
	var types []string
	err := r.db.Model(&model.FeedBatch{}).Distinct("feed_type").Order("feed_type ASC").Pluck("feed_type", &types).Error
	return types, err
}
