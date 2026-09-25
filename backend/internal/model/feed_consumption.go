package model

// FeedConsumption 一次执行完成产生的批次消耗明细。
// 每条记录对应执行在单个批次上按先到期先出（FEFO）分配的实际用量，
// 记录一经创建不可修改；(control_execution_id, feed_batch_id) 唯一，
// 保证同一执行对同一批次只扣减一次。
type FeedConsumption struct {
	Base
	ControlExecutionID uint              `gorm:"not null;uniqueIndex:idx_consumption_execution_batch;index" json:"controlExecutionId"`
	ControlExecution   *ControlExecution `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"controlExecution,omitempty"`
	FeedBatchID        uint              `gorm:"not null;uniqueIndex:idx_consumption_execution_batch;index" json:"feedBatchId"`
	FeedBatch          *FeedBatch        `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"feedBatch,omitempty"`
	FeedType           string            `gorm:"size:80;not null;index" json:"feedType"`
	AmountKg           float64           `gorm:"not null" json:"amountKg"`
}
