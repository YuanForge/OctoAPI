package handler

import (
	"fanapi/internal/db"
	"fanapi/internal/model"
	"fanapi/internal/service"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

// POST /admin/channels/batch  批量启停 / 批量倍率
func BatchUpdateChannels(c *gin.Context) {
	var req struct {
		Action    string  `json:"action"` // toggle_active / set_rate
		IDs       []int64 `json:"ids"`
		IsActive  bool    `json:"is_active"`
		RateRatio float64 `json:"rate_ratio"` // 倍率，仅 set_rate 时有效
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ids 长度须在 1-200 之间"})
		return
	}
	engine := db.Engine
	var affected []model.Channel
	if err := engine.In("id", req.IDs).Cols("id", "name", "model", "display_name").Find(&affected); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	switch req.Action {
	case "toggle_active":
		_, err := engine.Exec(
			"UPDATE channels SET is_active=$1, updated_at=NOW() WHERE id = ANY($2::bigint[])",
			req.IsActive, fmt.Sprintf("{%s}", joinInt64s(req.IDs)),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case "set_rate":
		if req.RateRatio <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "rate_ratio 须大于 0"})
			return
		}
		// 更新 billing_config.rate_ratio（JSON merge）
		_, err := engine.Exec(
			`UPDATE channels SET billing_config = billing_config || jsonb_build_object('rate_ratio', $1::float8),
			 updated_at=NOW() WHERE id = ANY($2::bigint[])`,
			req.RateRatio, fmt.Sprintf("{%s}", joinInt64s(req.IDs)),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的 action"})
		return
	}
	service.InvalidateChannelRouteCaches(c.Request.Context(), affected...)
	c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(req.IDs)})
}

// GET /admin/channels/:id/health  渠道 24h 健康统计
func GetChannelHealth(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	engine := db.Engine
	since := time.Now().Add(-24 * time.Hour)

	type healthRow struct {
		Status string `json:"status"`
		Cnt    int64  `json:"count"`
	}
	var rows []healthRow
	if err := engine.SQL(
		`SELECT status, COUNT(*) AS cnt FROM llm_logs WHERE channel_id=$1 AND created_at>=$2 GROUP BY status`,
		id, since,
	).Find(&rows); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var total, ok int64
	for _, r := range rows {
		total += r.Cnt
		if r.Status == "ok" {
			ok += r.Cnt
		}
	}
	successRate := 0.0
	if total > 0 {
		successRate = float64(ok) / float64(total) * 100
	}

	// P50/P99 延迟（仅 ok 状态）
	type latRow struct {
		P50 float64 `json:"p50"`
		P99 float64 `json:"p99"`
	}
	var lat latRow
	engine.SQL(
		`SELECT COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (updated_at-created_at))*1000), 0) AS p50,
		        COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (updated_at-created_at))*1000), 0) AS p99
		 FROM llm_logs WHERE channel_id=$1 AND created_at>=$2 AND status='ok'`,
		id, since,
	).Get(&lat)

	// 失败原因 TOP5
	type errRow struct {
		Msg string `json:"msg" xorm:"error_msg"`
		Cnt int64  `json:"count" xorm:"cnt"`
	}
	var errs []errRow
	engine.SQL(
		`SELECT SUBSTRING(error_msg, 1, 80) AS error_msg, COUNT(*) AS cnt
		 FROM llm_logs WHERE channel_id=$1 AND created_at>=$2 AND error_msg!=''
		 GROUP BY SUBSTRING(error_msg, 1, 80) ORDER BY cnt DESC LIMIT 5`,
		id, since,
	).Find(&errs)

	c.JSON(http.StatusOK, gin.H{
		"total":        total,
		"ok":           ok,
		"success_rate": successRate,
		"p50_ms":       lat.P50,
		"p99_ms":       lat.P99,
		"top_errors":   errs,
	})
}

// GET /admin/channels/:id/logs  渠道变更日志
func ListChannelLogs(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	engine := db.Engine
	var logs []model.ChannelLog
	if err := engine.Where("channel_id=?", id).OrderBy("created_at DESC").Limit(100).Find(&logs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// helper：将 []int64 转为逗号分隔字符串（用于 PostgreSQL ANY 占位符）
func joinInt64s(ids []int64) string {
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += strconv.FormatInt(id, 10)
	}
	return s
}
