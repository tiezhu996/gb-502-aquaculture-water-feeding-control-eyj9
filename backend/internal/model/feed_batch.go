package model

import "time"

// FeedBatch 饲料批次台账：登记批号、类型、入库量、到期日和启用状态。
// RemainingAmountKg 为当前余量，入库时等于入库量，执行完成时按 FEFO 原子扣减，
// 只减不增；已完成执行的扣减不可回滚或修改。
type FeedBatch struct {
	Base
	BatchNumber       string    `gorm:"size:64;uniqueIndex;not null" json:"batchNumber"`
	FeedType          string    `gorm:"size:80;not null;index" json:"feedType"`
	InboundAmountKg   float64   `gorm:"not null" json:"inboundAmountKg"`
	RemainingAmountKg float64   `gorm:"not null" json:"remainingKg"`
	ExpiryDate        time.Time `gorm:"not null;index" json:"expiryDate"`
	Enabled           bool      `gorm:"not null;default:true;index" json:"enabled"`
	Notes             string    `gorm:"type:text" json:"notes"`
}

// IsExpired 到期日当天仍可使用，次日 00:00（UTC）起视为过期。
func (b FeedBatch) IsExpired(now time.Time) bool {
	return now.UTC().After(b.ExpiryDate.UTC())
}
