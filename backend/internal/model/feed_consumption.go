package model

// FeedConsumption 饲料批次消耗明细：一次执行完成可跨多个批次，每个批次至多一条扣减。
// 重复完成由执行记录状态行锁（scheduled/running -> completed）阻止，
// (执行, 批次) 联合唯一索引作为第二道防线。
type FeedConsumption struct {
	Base
	FeedBatchID        uint              `gorm:"not null;index;uniqueIndex:uniq_consumption_execution_batch,priority:2" json:"feedBatchId"`
	FeedBatch          *FeedBatch        `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"feedBatch,omitempty"`
	ControlExecutionID uint              `gorm:"not null;index;uniqueIndex:uniq_consumption_execution_batch,priority:1" json:"controlExecutionId"`
	ControlExecution   *ControlExecution `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"controlExecution,omitempty"`
	FeedingPlanID      uint              `gorm:"not null;index" json:"feedingPlanId"`
	FeedingPlan        *FeedingPlan      `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"feedingPlan,omitempty"`
	PondID             uint              `gorm:"not null;index" json:"pondId"`
	Pond               *Pond             `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"pond,omitempty"`
	FeedType           string            `gorm:"size:80;not null" json:"feedType"`
	AmountKg           float64           `gorm:"not null" json:"amountKg"`
}
