package dto

import "time"

type FeedBatchInput struct {
	BatchNumber     string    `json:"batchNumber" binding:"required,min=2,max=64"`
	FeedType        string    `json:"feedType" binding:"required,max=80"`
	InboundAmountKg float64   `json:"inboundAmountKg" binding:"required,gt=0"`
	ExpiryDate      time.Time `json:"expiryDate" binding:"required"`
	Enabled         *bool     `json:"enabled"`
	Notes           string    `json:"notes" binding:"max=500"`
}
