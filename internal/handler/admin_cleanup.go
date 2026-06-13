package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fanapi/internal/db"
	"fanapi/internal/model"

	"github.com/gin-gonic/gin"
)

const (
	cleanupMinRetentionDays = 30
	cleanupDefaultBatchSize = 1000
	cleanupMaxRowsPerRun    = 50000
)

type cleanupTargetConfig struct {
	Table    string
	Label    string
	Statuses []string
}

var cleanupTargets = map[string]cleanupTargetConfig{
	"tasks": {
		Table:    "tasks",
		Label:    "历史任务",
		Statuses: []string{"done", "failed"},
	},
	"llm_logs": {
		Table:    "llm_logs",
		Label:    "LLM 调用日志",
		Statuses: []string{"ok", "error", "refunded"},
	},
}

// AdminPreviewCleanup 预估可清理的数据量，不执行删除。
func AdminPreviewCleanup(c *gin.Context) {
	if !requireAdminPermission(c, "tasks:write") {
		return
	}
	target, cfg, ok := parseCleanupTarget(c)
	if !ok {
		return
	}
	retentionDays, ok := parseRetentionDays(c.Query("retention_days"), c)
	if !ok {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	total, err := countCleanupRows(cfg, cutoff)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "预估清理数量失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"target":         target,
		"target_label":   cfg.Label,
		"retention_days": retentionDays,
		"cutoff":         cutoff.Format(time.RFC3339),
		"count":          total,
		"statuses":       cfg.Statuses,
	})
}

// AdminRunCleanup 分批物理删除已结束的历史数据。
func AdminRunCleanup(c *gin.Context) {
	if !requireAdminPermission(c, "tasks:write") {
		return
	}
	var req struct {
		Target        string `json:"target"`
		RetentionDays int    `json:"retention_days"`
		Confirm       string `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}
	target, cfg, ok := normalizeCleanupTarget(req.Target)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的清理类型"})
		return
	}
	if strings.TrimSpace(req.Confirm) != "确认清理" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入确认清理"})
		return
	}
	retentionDays, ok := validateRetentionDays(req.RetentionDays, c)
	if !ok {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	before, err := countCleanupRows(cfg, cutoff)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计清理数量失败"})
		return
	}

	deleted, err := deleteCleanupRows(cfg, cutoff, cleanupDefaultBatchSize, cleanupMaxRowsPerRun)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清理失败，请稍后重试"})
		return
	}
	remaining, _ := countCleanupRows(cfg, cutoff)
	hasMore := remaining > 0

	writeCleanupAudit(c, target, cfg, retentionDays, cutoff, before, deleted, remaining)
	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"target":         target,
		"target_label":   cfg.Label,
		"retention_days": retentionDays,
		"cutoff":         cutoff.Format(time.RFC3339),
		"matched":        before,
		"deleted":        deleted,
		"remaining":      remaining,
		"has_more":       hasMore,
		"statuses":       cfg.Statuses,
	})
}

func parseCleanupTarget(c *gin.Context) (string, cleanupTargetConfig, bool) {
	target := c.Query("target")
	normalized, cfg, ok := normalizeCleanupTarget(target)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的清理类型"})
		return "", cleanupTargetConfig{}, false
	}
	return normalized, cfg, true
}

func normalizeCleanupTarget(target string) (string, cleanupTargetConfig, bool) {
	normalized := strings.TrimSpace(target)
	cfg, ok := cleanupTargets[normalized]
	return normalized, cfg, ok
}

func parseRetentionDays(raw string, c *gin.Context) (int, bool) {
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "保留天数格式错误"})
		return 0, false
	}
	return validateRetentionDays(days, c)
}

func validateRetentionDays(days int, c *gin.Context) (int, bool) {
	if days < cleanupMinRetentionDays {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("保留天数不能少于 %d 天", cleanupMinRetentionDays)})
		return 0, false
	}
	return days, true
}

func countCleanupRows(cfg cleanupTargetConfig, cutoff time.Time) (int64, error) {
	return db.Engine.Table(cfg.Table).
		In("status", stringSliceToInterfaces(cfg.Statuses)...).
		And("created_at < ?", cutoff).
		Count()
}

func deleteCleanupRows(cfg cleanupTargetConfig, cutoff time.Time, batchSize, maxRows int64) (int64, error) {
	var total int64
	for total < maxRows {
		limit := batchSize
		if maxRows-total < limit {
			limit = maxRows - total
		}
		sql := fmt.Sprintf(`
WITH doomed AS (
	SELECT id FROM %s
	WHERE status IN (%s) AND created_at < $1
	ORDER BY id
	LIMIT %d
)
DELETE FROM %s
WHERE id IN (SELECT id FROM doomed)`,
			cfg.Table,
			postgresPlaceholders(2, len(cfg.Statuses)),
			limit,
			cfg.Table,
		)
		args := []interface{}{cutoff}
		args = append(args, stringSliceToInterfaces(cfg.Statuses)...)
		result, err := db.Engine.Exec(append([]interface{}{sql}, args...)...)
		if err != nil {
			return total, err
		}
		affected, _ := result.RowsAffected()
		total += affected
		if affected == 0 || affected < limit {
			break
		}
	}
	return total, nil
}

func postgresPlaceholders(start, count int) string {
	parts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		parts = append(parts, fmt.Sprintf("$%d", start+i))
	}
	return strings.Join(parts, ",")
}

func stringSliceToInterfaces(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func requireAdminPermission(c *gin.Context, permission string) bool {
	adminID := getAdminID(c)
	if adminID <= 0 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无访问权限"})
		return false
	}
	var userRoles []model.AdminUserRole
	if err := db.Engine.Where("admin_id = ?", adminID).Find(&userRoles); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "权限校验失败"})
		return false
	}
	if len(userRoles) == 0 {
		return true
	}
	roleIDs := make([]interface{}, 0, len(userRoles))
	for _, ur := range userRoles {
		roleIDs = append(roleIDs, ur.RoleID)
	}
	var roles []model.AdminRole
	if err := db.Engine.In("id", roleIDs...).Cols("permissions").Find(&roles); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "权限校验失败"})
		return false
	}
	for _, role := range roles {
		for _, p := range role.Permissions {
			if p == "*" || p == permission {
				return true
			}
		}
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要任务管理权限"})
	return false
}

func writeCleanupAudit(c *gin.Context, target string, cfg cleanupTargetConfig, retentionDays int, cutoff time.Time, matched, deleted, remaining int64) {
	adminID := getAdminID(c)
	var user model.User
	_, _ = db.Engine.ID(adminID).Cols("email", "username").Get(&user)
	adminEmail := user.Username
	if user.Email != nil && *user.Email != "" {
		adminEmail = *user.Email
	}
	audit := &model.AdminAuditLog{
		AdminID:      adminID,
		AdminEmail:   adminEmail,
		Action:       "cleanup",
		ResourceType: target,
		Summary:      fmt.Sprintf("清理%s：删除 %d 条，保留最近 %d 天", cfg.Label, deleted, retentionDays),
		Detail: model.JSON{
			"target":         target,
			"target_label":   cfg.Label,
			"statuses":       cfg.Statuses,
			"retention_days": retentionDays,
			"cutoff":         cutoff.Format(time.RFC3339),
			"matched":        matched,
			"deleted":        deleted,
			"remaining":      remaining,
		},
		IP: c.ClientIP(),
		UA: c.Request.UserAgent(),
	}
	_, _ = db.Engine.Insert(audit)
}
