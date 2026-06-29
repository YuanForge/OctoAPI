package handler

import (
	"fanapi/internal/db"
	"fanapi/internal/model"
	"fanapi/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

// POST /user/apikeys  (requires auth)
func (h *AuthHandler) CreateAPIKey(c *gin.Context) {
	var req struct {
		Name    string `json:"name" binding:"required,max=64"`
		KeyType string `json:"key_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if getSettingValue("show_low_price_key") == "false" {
		req.KeyType = "stable"
	} else if req.KeyType != "stable" {
		req.KeyType = "low_price"
	}
	userID := c.MustGet("user_id").(int64)
	rawKey, err := service.GenerateAPIKey(c.Request.Context(), userID, req.Name, req.KeyType, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"key": rawKey, "note": "store this key safely, it will not be shown again"})
}

// GET /user/apikeys
func (h *AuthHandler) ListAPIKeys(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)
	var keys []model.APIKey
	if err := db.Engine.Where("user_id = ?", userID).
		Cols("id", "name", "key_hash", "raw_key_enc", "key_type", "is_active", "last_used_at", "created_at").
		Desc("id").
		Find(&keys); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	spendStats, err := loadAPIKeySpendStats(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type apiKeyItem struct {
		ID            int64       `json:"id"`
		Name          string      `json:"name"`
		KeyType       string      `json:"key_type"`
		KeyPrefix     string      `json:"key_prefix"`
		RawKey        string      `json:"raw_key"`
		Viewable      bool        `json:"viewable"`
		IsActive      bool        `json:"is_active"`
		TotalConsumed int64       `json:"total_consumed"`
		TodayConsumed int64       `json:"today_consumed"`
		LastUsedAt    interface{} `json:"last_used_at"`
		CreatedAt     interface{} `json:"created_at"`
	}

	items := make([]apiKeyItem, 0, len(keys))
	for _, k := range keys {
		rawKey := ""
		viewable := false
		if k.RawKeyEnc != "" {
			if decrypted, err := service.DecryptAPIKey(k.RawKeyEnc, h.cfg.JWTSecret); err == nil {
				rawKey = decrypted
				viewable = true
			}
		}
		prefix := ""
		if len(k.KeyHash) >= 12 {
			prefix = k.KeyHash[:12]
		} else {
			prefix = k.KeyHash
		}
		keyType := k.KeyType
		if keyType == "" {
			keyType = "low_price"
		}
		stats := spendStats[k.ID]
		items = append(items, apiKeyItem{
			ID:            k.ID,
			Name:          k.Name,
			KeyType:       keyType,
			KeyPrefix:     prefix,
			RawKey:        rawKey,
			Viewable:      viewable,
			IsActive:      k.IsActive,
			TotalConsumed: stats.TotalConsumed,
			TodayConsumed: stats.TodayConsumed,
			LastUsedAt:    k.LastUsedAt,
			CreatedAt:     k.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"api_keys": items})
}

type apiKeySpendStats struct {
	TotalConsumed int64
	TodayConsumed int64
}

func loadAPIKeySpendStats(userID int64) (map[int64]apiKeySpendStats, error) {
	rows, err := db.Engine.QueryString(`
		SELECT api_key_id,
			COALESCE(SUM(CASE
				WHEN type IN ('charge','hold','settle','consume') THEN credits
				WHEN type = 'refund' THEN -credits
				ELSE 0 END), 0) AS total_consumed,
			COALESCE(SUM(CASE
				WHEN created_at >= CURRENT_DATE THEN
					CASE
						WHEN type IN ('charge','hold','settle','consume') THEN credits
						WHEN type = 'refund' THEN -credits
						ELSE 0
					END
				ELSE 0 END), 0) AS today_consumed
		FROM billing_transactions
		WHERE user_id = ? AND api_key_id > 0
		GROUP BY api_key_id`, userID)
	if err != nil {
		return nil, err
	}
	stats := make(map[int64]apiKeySpendStats, len(rows))
	for _, row := range rows {
		keyID := parseInt64Field(row, "api_key_id")
		if keyID <= 0 {
			continue
		}
		total := parseInt64Field(row, "total_consumed")
		today := parseInt64Field(row, "today_consumed")
		if total < 0 {
			total = 0
		}
		if today < 0 {
			today = 0
		}
		stats[keyID] = apiKeySpendStats{TotalConsumed: total, TodayConsumed: today}
	}
	return stats, nil
}

func parseInt64Field(row map[string]string, key string) int64 {
	v, _ := strconv.ParseInt(row[key], 10, 64)
	return v
}

// DELETE /user/apikeys/:id
func (h *AuthHandler) DeleteAPIKey(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)
	keyID := strings.TrimSpace(c.Param("id"))
	affected, err := db.Engine.Where("id = ? AND user_id = ?", keyID, userID).
		Delete(&model.APIKey{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "API Key 不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "API Key 已删除"})
}
