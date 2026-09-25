package dto

import "time"

type FeedBatchInput struct {
	BatchNo    string  `json:"batchNo" binding:"required,min=2,max=64"`
	FeedType   string  `json:"feedType" binding:"required,max=80"`
	InboundKg  float64 `json:"inboundKg" binding:"required,gt=0"`
	ExpireDate string  `json:"expireDate" binding:"required"`
	Enabled    *bool   `json:"enabled"`
	Notes      string  `json:"notes" binding:"max=1000"`
}

type FeedBatchEnabledInput struct {
	Enabled bool `json:"enabled"`
}

type FeedBatchView struct {
	ID          uint      `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	BatchNo     string    `json:"batchNo"`
	FeedType    string    `json:"feedType"`
	InboundKg   float64   `json:"inboundKg"`
	ExpireDate  time.Time `json:"expireDate"`
	Enabled     bool      `json:"enabled"`
	Notes       string    `json:"notes"`
	ConsumedKg  float64   `json:"consumedKg"`
	RemainingKg float64   `json:"remainingKg"`
	Expired     bool      `json:"expired"`
}

type FeedBatchDetail struct {
	FeedBatchView
	Consumptions []FeedConsumptionView `json:"consumptions"`
}

type FeedConsumptionView struct {
	ID                 uint       `json:"id"`
	CreatedAt          time.Time  `json:"createdAt"`
	FeedBatchID        uint       `json:"feedBatchId"`
	BatchNo            string     `json:"batchNo"`
	ControlExecutionID uint       `json:"controlExecutionId"`
	FeedingPlanID      uint       `json:"feedingPlanId"`
	PlanName           string     `json:"planName"`
	PondID             uint       `json:"pondId"`
	PondName           string     `json:"pondName"`
	FeedType           string     `json:"feedType"`
	AmountKg           float64    `json:"amountKg"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
	Operator           string     `json:"operator"`
}
