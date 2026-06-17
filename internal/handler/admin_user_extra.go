package handler

import (
	"fanapi/internal/db"
	"fanapi/internal/model"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

// GET /admin/users/:id/portrait  用户消费画像
func GetUserPortrait(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	engine := db.Engine
	since := time.Now().AddDate(0, 0, -30)

	// 30 天每日消费（CNY）
	type dayRow struct {
		Day    string  `json:"day" xorm:"day"`
		Amount float64 `json:"amount" xorm:"amount"`
	}
	var daily []dayRow
	engine.SQL(
		`SELECT TO_CHAR(DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai'), 'MM-DD') AS day,
		        COALESCE(SUM(CASE WHEN type IN ('charge','hold','settle') THEN credits WHEN type = 'refund' THEN -credits ELSE 0 END), 0)::float8 / $3 AS amount
		 FROM billing_transactions WHERE user_id=$1 AND created_at>=$2 AND type IN ('charge','hold','settle','refund')
		 GROUP BY DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai')
		 ORDER BY DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai')`,
		id, since, creditsPerCNY,
	).Find(&daily)

	// TOP5 模型
	type modelRow struct {
		Model string `json:"model" xorm:"model"`
		Calls int64  `json:"calls" xorm:"calls"`
	}
	var topModels []modelRow
	engine.SQL(
		`SELECT model, COUNT(*) AS calls FROM llm_logs WHERE user_id=$1 AND created_at>=$2 AND status='ok'
		 GROUP BY model ORDER BY calls DESC LIMIT 5`,
		id, since,
	).Find(&topModels)

	// API Key 列表
	var keys []model.APIKey
	engine.Where("user_id=?", id).OrderBy("created_at DESC").Find(&keys)

	// 风控标签
	var labels []model.RiskLabel
	engine.Where("user_id=?", id).OrderBy("created_at DESC").Find(&labels)

	c.JSON(http.StatusOK, gin.H{
		"daily_spend": daily,
		"top_models":  topModels,
		"api_keys":    keys,
		"risk_labels": labels,
	})
}

// POST /admin/users/:id/risk-labels  手动添加风控标签
func AddRiskLabel(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var req struct {
		Label  string `json:"label"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	adminID := getAdminID(c)
	label := &model.RiskLabel{UserID: userID, Label: req.Label, Reason: req.Reason, CreatedBy: adminID}
	if _, err := db.Engine.Insert(label); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, label)
}

// DELETE /admin/risk-labels/:id  删除风控标签
func DeleteRiskLabel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	db.Engine.Delete(&model.RiskLabel{ID: id})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /admin/users/:id/operation-log  用户操作日志（取 billing_transactions 中手动操作部分）
func GetUserOperationLog(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	engine := db.Engine
	type opRow struct {
		Type      string    `json:"type" xorm:"type"`
		Credits   int64     `json:"credits" xorm:"credits"`
		CreatedAt time.Time `json:"created_at" xorm:"created_at"`
	}
	var ops []opRow
	engine.SQL(
		`SELECT type, credits, created_at FROM billing_transactions
		 WHERE user_id=$1 AND type IN ('recharge','refund','adjust')
		 ORDER BY created_at DESC LIMIT 100`,
		id,
	).Find(&ops)

	// 审计日志（操作人维度）
	var audits []model.AdminAuditLog
	engine.Where("resource_type='user' AND resource_id=?", id).
		OrderBy("created_at DESC").Limit(50).Find(&audits)

	c.JSON(http.StatusOK, gin.H{"transactions": ops, "audits": audits})
}

// GET /admin/users/:id/referrals  用户邀请关系
func GetUserReferrals(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}

	var target model.User
	found, err := db.Engine.ID(id).Cols("id", "inviter_id").Get(&target)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	type referralUser struct {
		ID        int64     `json:"id" xorm:"id"`
		Username  string    `json:"username" xorm:"username"`
		Email     string    `json:"email,omitempty" xorm:"email"`
		CreatedAt time.Time `json:"created_at" xorm:"created_at"`
	}

	var inviter *referralUser
	if target.InviterID != nil {
		row := referralUser{}
		found, err := db.Engine.SQL(
			`SELECT id, username, COALESCE(email, '') AS email, created_at
			 FROM users
			 WHERE id = $1`,
			*target.InviterID,
		).Get(&row)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询邀请人失败"})
			return
		}
		if found {
			inviter = &row
		}
	}

	var invitees []referralUser
	if err := db.Engine.SQL(
		`SELECT id, username, COALESCE(email, '') AS email, created_at
		 FROM users
		 WHERE inviter_id = $1
		 ORDER BY created_at DESC, id DESC
		 LIMIT 500`,
		id,
	).Find(&invitees); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询被邀请人失败"})
		return
	}
	if invitees == nil {
		invitees = []referralUser{}
	}
	inviteeCount, _ := db.Engine.Where("inviter_id = ?", id).Count(new(model.User))

	c.JSON(http.StatusOK, gin.H{
		"inviter":       inviter,
		"inviter_id":    target.InviterID,
		"invitees":      invitees,
		"invitee_count": inviteeCount,
	})
}

// GET /admin/api-keys  平台维度 API Key 总览
func AdminListAPIKeys(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	status := c.Query("status") // active/inactive/revoked
	userID := c.Query("user_id")

	engine := db.Engine
	sess := engine.Table("api_keys").
		Select("api_keys.id, api_keys.user_id, api_keys.name, api_keys.key_type, api_keys.is_active, api_keys.last_used_at, api_keys.created_at, users.email AS user_email").
		Join("LEFT", "users", "users.id = api_keys.user_id").
		OrderBy("api_keys.created_at DESC")

	if userID != "" {
		sess = sess.Where("api_keys.user_id=?", userID)
	}
	switch status {
	case "active":
		sess = sess.Where("api_keys.is_active=true")
	case "inactive":
		sess = sess.Where("api_keys.is_active=false AND api_keys.last_used_at > NOW()-INTERVAL '7 days'")
	}

	type keyRow struct {
		model.APIKey `xorm:"extends"`
		UserEmail    string `json:"user_email" xorm:"user_email"`
	}
	var keys []keyRow
	countSess := engine.Table("api_keys").Join("LEFT", "users", "users.id = api_keys.user_id")
	if userID != "" {
		countSess = countSess.Where("api_keys.user_id=?", userID)
	}
	switch status {
	case "active":
		countSess = countSess.Where("api_keys.is_active=true")
	case "inactive":
		countSess = countSess.Where("api_keys.is_active=false AND api_keys.last_used_at > NOW()-INTERVAL '7 days'")
	}
	total, _ := countSess.Count()
	sess.Limit(size, (page-1)*size).Find(&keys)

	c.JSON(http.StatusOK, gin.H{"keys": keys, "total": total})
}

// 用户状态中文
func userStatusText(status string) string {
	switch status {
	case "active":
		return "正常"
	case "frozen":
		return "已冻结"
	default:
		return status
	}
}

// PATCH /admin/api-keys/:id/revoke  吊销 API Key
func RevokeAPIKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	db.Engine.ID(id).Update(&model.APIKey{IsActive: false})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
