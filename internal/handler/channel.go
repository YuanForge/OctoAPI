package handler

import (
	"fanapi/internal/model"
	"fanapi/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// POST /admin/channels
func CreateChannel(c *gin.Context) {
	var ch model.Channel
	if err := c.ShouldBindJSON(&ch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.CreateChannel(c.Request.Context(), &ch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, ch)
}

// GET /admin/channels
func ListChannels(c *gin.Context) {
	if hasChannelListQuery(c) {
		query, parseErr := parseChannelListQuery(c)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": parseErr.Error()})
			return
		}
		result, err := service.ListChannelsPaged(c.Request.Context(), query)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"channels": result.Channels,
			"items":    result.Channels,
			"total":    result.Total,
			"page":     result.Page,
			"size":     result.Size,
		})
		return
	}

	channels, err := service.ListChannels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"channels": channels})
}

func hasChannelListQuery(c *gin.Context) bool {
	params := c.Request.URL.Query()
	for _, key := range []string{"page", "size", "page_size", "name", "display_name", "model_provider", "q", "keyword", "price_min", "price_max", "sort_by", "sort_order"} {
		if params.Has(key) {
			return true
		}
	}
	return false
}

func parseChannelListQuery(c *gin.Context) (service.ChannelListQuery, error) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	sizeRaw := c.DefaultQuery("size", c.DefaultQuery("page_size", "20"))
	size, _ := strconv.Atoi(sizeRaw)
	query := service.ChannelListQuery{
		Page:          page,
		Size:          size,
		Name:          c.Query("name"),
		DisplayName:   c.Query("display_name"),
		ModelProvider: c.Query("model_provider"),
		Keyword:       c.Query("q"),
		SortBy:        c.Query("sort_by"),
		SortOrder:     c.Query("sort_order"),
	}
	if query.Keyword == "" {
		query.Keyword = c.Query("keyword")
	}
	if priceMin, err := parseOptionalFloat(c.Query("price_min")); err != nil {
		return query, err
	} else {
		query.PriceMin = priceMin
	}
	if priceMax, err := parseOptionalFloat(c.Query("price_max")); err != nil {
		return query, err
	} else {
		query.PriceMax = priceMax
	}
	return query, nil
}

func parseOptionalFloat(raw string) (*float64, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// PUT /admin/channels/:id
func UpdateChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var ch model.Channel
	if err := c.ShouldBindJSON(&ch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ch.ID = id
	if err := service.UpdateChannel(c.Request.Context(), &ch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ch)
}

// PATCH /admin/channels/:id/active — 仅更新渠道启用状态，不影响其他字段
func PatchChannelActive(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var req struct {
		IsActive bool `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := service.PatchChannelActive(c.Request.Context(), id, req.IsActive); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /admin/channels/:id/refresh-runtime
func RefreshChannelRuntime(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}

	ch, err := service.GetChannel(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	service.InvalidateChannelCache(c.Request.Context(), id)
	service.InvalidateChannelRouteCaches(c.Request.Context(), *ch)
	if err := service.ResetChannelPoolRuntimeState(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "刷新 Redis 状态失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /admin/channels/:id
func DeleteChannel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	if err := service.DeleteChannel(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "操作失败，请稍后重试"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "渠道已删除"})
}
