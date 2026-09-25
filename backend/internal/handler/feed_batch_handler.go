package handler

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
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

func parseEnabled(value string) *bool {
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func (h *FeedBatchHandler) List(c *gin.Context) {
	query, ok := bindPageQuery(c)
	if !ok {
		return
	}
	result, err := h.service.List(query, c.Query("feedType"), parseEnabled(c.Query("enabled")))
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

func (h *FeedBatchHandler) Available(c *gin.Context) {
	result, err := h.service.Available(c.Query("feedType"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"items": result}})
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

func (h *FeedBatchHandler) SetEnabled(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.FeedBatchEnabledInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, service.NewError(service.CodeValidation, "启用状态参数无效"))
		return
	}
	result, err := h.service.SetEnabled(id, input.Enabled, actorFromContext(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *FeedBatchHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(id, actorFromContext(c)); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
