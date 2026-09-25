package handler

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type FeedBatchHandler struct {
	service *service.FeedBatchService
}

func NewFeedBatchHandler(batches *service.FeedBatchService) *FeedBatchHandler {
	return &FeedBatchHandler{service: batches}
}

func (h *FeedBatchHandler) List(c *gin.Context) {
	query, ok := bindPageQuery(c)
	if !ok {
		return
	}
	var enabled *bool
	if value := c.Query("enabled"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			respondError(c, service.NewError(service.CodeValidation, "启用状态参数无效"))
			return
		}
		enabled = &parsed
	}
	result, err := h.service.List(query, c.Query("feedType"), enabled)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *FeedBatchHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	result, err := h.service.Get(id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *FeedBatchHandler) Consumptions(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	items, total, err := h.service.Consumptions(id)
	if err != nil {
		respondError(c, err)
		return
	}
	result := dto.PageResult[model.FeedConsumption]{Items: items, Total: total, Page: 1, PageSize: 100}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *FeedBatchHandler) Create(c *gin.Context) {
	var input dto.FeedBatchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, service.NewError(service.CodeValidation, "饲料批次参数不完整或格式无效"))
		return
	}
	result, err := h.service.Create(input, actorFromContext(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *FeedBatchHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.FeedBatchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, service.NewError(service.CodeValidation, "饲料批次参数不完整或格式无效"))
		return
	}
	result, err := h.service.Update(id, input, actorFromContext(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
