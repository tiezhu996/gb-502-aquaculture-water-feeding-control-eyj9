package model

import "time"

// FeedBatch 饲料批次台账：登记批号、类型、入库量、到期日与启用状态
type FeedBatch struct {
	Base
	BatchNo    string    `gorm:"size:64;uniqueIndex;not null" json:"batchNo"`
	FeedType   string    `gorm:"size:80;not null;index" json:"feedType"`
	InboundKg  float64   `gorm:"not null" json:"inboundKg"`
	ExpireDate time.Time `gorm:"type:date;not null;index" json:"expireDate"`
	Enabled    bool      `gorm:"not null;index" json:"enabled"`
	Notes      string    `gorm:"type:text" json:"notes"`
}
