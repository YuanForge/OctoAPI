package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fanapi/internal/db"
	"fanapi/internal/model"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────
//  渠道管理扩展
// ─────────────────────────────────────────────

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

// ─────────────────────────────────────────────
//  用户管理扩展
// ─────────────────────────────────────────────

// GET /admin/users/:id/portrait  用户消费画像
func GetUserPortrait(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	engine := db.Engine
	since := time.Now().AddDate(0, 0, -30)

	// 30 天每日消费（credits）
	type dayRow struct {
		Day    string  `json:"day" xorm:"day"`
		Amount float64 `json:"amount" xorm:"amount"`
	}
	var daily []dayRow
	engine.SQL(
		`SELECT TO_CHAR(DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai'), 'MM-DD') AS day,
		        COALESCE(SUM(credits), 0)::float8 AS amount
		 FROM billing_transactions WHERE user_id=$1 AND created_at>=$2 AND type IN ('charge','settle')
		 GROUP BY DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai')
		 ORDER BY DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai')`,
		id, since,
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

// ─────────────────────────────────────────────
//  账单扩展
// ─────────────────────────────────────────────

// GET /admin/transactions/aggregate  多维聚合
func GetTransactionAggregate(c *gin.Context) {
	dim := c.DefaultQuery("dim", "day") // day/user/channel/model
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	startAt := c.Query("start_at")
	endAt := c.Query("end_at")

	engine := db.Engine
	args := []interface{}{}
	where := "type IN ('charge','settle')"
	if startAt != "" {
		t, _ := parseDateTime(startAt, false)
		if !t.IsZero() {
			where += fmt.Sprintf(" AND created_at >= $%d", len(args)+1)
			args = append(args, t)
		}
	}
	if endAt != "" {
		t, _ := parseDateTime(endAt, true)
		if !t.IsZero() {
			where += fmt.Sprintf(" AND created_at <= $%d", len(args)+1)
			args = append(args, t)
		}
	}

	type aggRow struct {
		Key     string  `json:"key" xorm:"key"`
		Revenue float64 `json:"revenue" xorm:"revenue"`
		Cost    float64 `json:"cost" xorm:"cost"`
		Profit  float64 `json:"profit" xorm:"profit"`
		Calls   int64   `json:"calls" xorm:"calls"`
	}

	var selectExpr, groupExpr string
	switch dim {
	case "user":
		selectExpr = "user_id::text AS key"
		groupExpr = "user_id"
	case "channel":
		selectExpr = "channel_id::text AS key"
		groupExpr = "channel_id"
	case "model":
		// join with llm_logs by corr_id – too expensive; use metrics->>'model'
		selectExpr = "COALESCE(metrics->>'model', 'unknown') AS key"
		groupExpr = "COALESCE(metrics->>'model', 'unknown')"
	default: // day
		selectExpr = "TO_CHAR(DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai'), 'YYYY-MM-DD') AS key"
		groupExpr = "DATE_TRUNC('day', created_at AT TIME ZONE 'Asia/Shanghai')"
	}

	whereExpr := where
	if whereExpr != "" {
		whereExpr = "WHERE " + whereExpr
	}
	sql := fmt.Sprintf(
		`SELECT %s,
		        SUM(credits)::float8 AS revenue,
		        SUM(cost)::float8 AS cost,
		        SUM(credits-cost)::float8 AS profit,
		        COUNT(*) AS calls
		 FROM billing_transactions %s
		 GROUP BY %s ORDER BY revenue DESC LIMIT %d OFFSET %d`,
		selectExpr, whereExpr, groupExpr, size, (page-1)*size,
	)
	var rows []aggRow
	if err := engine.SQL(sql, args...).Find(&rows); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows, "dim": dim})
}

// POST /admin/transactions/adjust  手动调账
func AdjustTransaction(c *gin.Context) {
	var req struct {
		UserID  int64  `json:"user_id"`
		Type    string `json:"type"`    // adjust/recharge/refund
		Credits int64  `json:"credits"` // 正负均可
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.UserID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id 不能为空"})
		return
	}
	if len(req.Reason) < 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason 至少 5 个字符"})
		return
	}
	if req.Type == "" {
		req.Type = "adjust"
	}

	engine := db.Engine
	sess := engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 获取用户当前余额
	type balRow struct {
		Balance int64 `xorm:"balance_credits"`
	}
	var user balRow
	if found, err := sess.SQL("SELECT balance_credits FROM users WHERE id=$1 FOR UPDATE", req.UserID).Get(&user); err != nil || !found {
		sess.Rollback()
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户不存在"})
		return
	}

	newBalance := user.Balance + req.Credits
	if newBalance < 0 {
		newBalance = 0
	}
	if _, err := sess.Exec("UPDATE users SET balance_credits=$1 WHERE id=$2", newBalance, req.UserID); err != nil {
		sess.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tx := &model.BillingTransaction{
		UserID:       req.UserID,
		Type:         req.Type,
		Credits:      req.Credits,
		BalanceAfter: newBalance,
		Metrics:      model.JSON{"reason": req.Reason, "admin_id": getAdminID(c)},
	}
	if _, err := sess.Insert(tx); err != nil {
		sess.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sess.Commit()
	c.JSON(http.StatusOK, gin.H{"ok": true, "balance_after": newBalance, "transaction_id": tx.ID})
}

// ─────────────────────────────────────────────
//  卡密批次管理
// ─────────────────────────────────────────────

// GET /admin/cards/batches  批次列表
func ListCardBatches(c *gin.Context) {
	engine := db.Engine
	type batchRow struct {
		ID        int64     `json:"id" xorm:"id"`
		BatchID   string    `json:"batch_id" xorm:"batch_id"`
		Note      string    `json:"note" xorm:"note"`
		Credits   int64     `json:"credits" xorm:"credits"`
		Count     int       `json:"count" xorm:"count"`
		CreatedBy int64     `json:"created_by" xorm:"created_by"`
		CreatedAt time.Time `json:"created_at" xorm:"created_at"`
		Used      int       `json:"used" xorm:"used"`
	}
	var rows []batchRow
	_ = engine.SQL(
		`SELECT cb.*, COUNT(c.id) FILTER (WHERE c.status='used') AS used
		 FROM card_batches cb
		 LEFT JOIN cards c ON c.batch_id = cb.batch_id
		 GROUP BY cb.id ORDER BY cb.created_at DESC LIMIT 100`,
	).Find(&rows)

	// 历史兼容：旧数据可能仅存在 cards.batch_id，而 card_batches 为空。
	if len(rows) == 0 {
		_ = engine.SQL(
			`SELECT
				MIN(c.id) AS id,
				c.batch_id AS batch_id,
				MAX(c.note) AS note,
				MAX(c.credits) AS credits,
				COUNT(*) AS count,
				0 AS created_by,
				MIN(c.created_at) AS created_at,
				COUNT(*) FILTER (WHERE c.status='used') AS used
			 FROM cards c
			 WHERE c.batch_id IS NOT NULL AND c.batch_id <> ''
			 GROUP BY c.batch_id
			 ORDER BY MIN(c.created_at) DESC
			 LIMIT 100`,
		).Find(&rows)
	}
	c.JSON(http.StatusOK, gin.H{"batches": rows})
}

// POST /admin/cards/:id/void  作废单张卡密
func VoidCard(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	engine := db.Engine
	var card model.Card
	if found, err := engine.ID(id).Get(&card); err != nil || !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡密不存在"})
		return
	}
	if card.Status == "used" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "已使用的卡密不可作废"})
		return
	}
	engine.ID(id).Update(&model.Card{Status: "voided"})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /admin/cards/batches/:batch_id/void  批量作废整批次未用卡密
func VoidCardBatch(c *gin.Context) {
	batchID := c.Param("batch_id")
	engine := db.Engine

	// 优先按 card_batch_id（数字）关联查询；兼容旧数据（batch_id 字符串）
	if batchIDInt, err := strconv.ParseInt(batchID, 10, 64); err == nil {
		// 新数据：按 card_batch_id 作废
		res, err := engine.Exec(
			"UPDATE cards SET status='voided' WHERE card_batch_id=$1 AND status='unused'", batchIDInt,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		n, _ := res.RowsAffected()
		c.JSON(http.StatusOK, gin.H{"ok": true, "voided": n})
	} else {
		// 旧数据：按 batch_id（字符串）作废
		res, err := engine.Exec(
			"UPDATE cards SET status='voided' WHERE batch_id=$1 AND status='unused'", batchID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		n, _ := res.RowsAffected()
		c.JSON(http.StatusOK, gin.H{"ok": true, "voided": n})
	}
}

// ─────────────────────────────────────────────
//  提现凭证
// ─────────────────────────────────────────────

// POST /admin/withdrawals/:id/proof  上传打款凭证
func AdminUploadWithdrawalProof(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var req struct {
		ProofURL  string `json:"proof_url"`
		ProofNote string `json:"proof_note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 兼容未执行迁移的生产库：按需补齐字段，避免上传凭证时报列不存在。
	_, _ = db.Engine.Exec("ALTER TABLE withdraw_requests ADD COLUMN IF NOT EXISTS proof_url TEXT NOT NULL DEFAULT ''")
	_, _ = db.Engine.Exec("ALTER TABLE withdraw_requests ADD COLUMN IF NOT EXISTS proof_note TEXT NOT NULL DEFAULT ''")
	_, err = db.Engine.Exec(
		"UPDATE withdraw_requests SET proof_url=$1, proof_note=$2, updated_at=NOW() WHERE id=$3",
		req.ProofURL, req.ProofNote, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─────────────────────────────────────────────
//  全局审计日志
// ─────────────────────────────────────────────

// GET /admin/audit  审计日志列表
func ListAuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	engine := db.Engine
	sess := engine.Table("admin_audit_logs").OrderBy("created_at DESC")
	if v := c.Query("admin_id"); v != "" {
		sess = sess.Where("admin_id=?", v)
	}
	if v := c.Query("resource_type"); v != "" {
		sess = sess.Where("resource_type=?", v)
	}
	if v := c.Query("action"); v != "" {
		sess = sess.Where("action=?", v)
	}
	countSess := engine.Table("admin_audit_logs")
	if v := c.Query("admin_id"); v != "" {
		countSess = countSess.Where("admin_id=?", v)
	}
	if v := c.Query("resource_type"); v != "" {
		countSess = countSess.Where("resource_type=?", v)
	}
	if v := c.Query("action"); v != "" {
		countSess = countSess.Where("action=?", v)
	}
	total, _ := countSess.Count(&model.AdminAuditLog{})
	var logs []model.AdminAuditLog
	sess.Limit(size, (page-1)*size).Find(&logs)
	c.JSON(http.StatusOK, gin.H{"logs": logs, "total": total})
}

// helper：从 context 取管理员 ID（middleware 注入 "user_id"）
func getAdminID(c *gin.Context) int64 {
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

// ─────────────────────────────────────────────
//  通知中心
// ─────────────────────────────────────────────

// GET /admin/notifications
func ListNotifications(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	engine := db.Engine
	sess := engine.Table("notifications").OrderBy("created_at DESC")
	if s := c.Query("status"); s != "" {
		sess = sess.Where("status=?", s)
	}
	countSess := engine.Table("notifications")
	if s := c.Query("status"); s != "" {
		countSess = countSess.Where("status=?", s)
	}
	total, _ := countSess.Count(&model.Notification{})
	var items []model.Notification
	sess.Limit(size, (page-1)*size).Find(&items)
	c.JSON(http.StatusOK, gin.H{"notifications": items, "total": total})
}

// POST /admin/notifications
func CreateNotification(c *gin.Context) {
	var n model.Notification
	if err := c.ShouldBindJSON(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n.CreatedBy = getAdminID(c)
	if n.Status == "" {
		n.Status = "draft"
	}
	if _, err := db.Engine.Insert(&n); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, n)
}

// POST /admin/notifications/:id/send  立即发送
func SendNotification(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	now := time.Now()
	db.Engine.Exec(
		"UPDATE notifications SET status='sent', sent_at=$1 WHERE id=$2 AND status='draft'",
		now, id,
	)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /admin/notifications/:id
func DeleteNotification(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	db.Engine.Delete(&model.Notification{ID: id})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─────────────────────────────────────────────
//  告警中心
// ─────────────────────────────────────────────

// GET /admin/alerts
func ListAlerts(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	engine := db.Engine
	sess := engine.Table("alerts").OrderBy("created_at DESC")
	if s := c.Query("status"); s != "" {
		sess = sess.Where("status=?", s)
	}
	if t := c.Query("type"); t != "" {
		sess = sess.Where("type=?", t)
	}
	countSess := engine.Table("alerts")
	if s := c.Query("status"); s != "" {
		countSess = countSess.Where("status=?", s)
	}
	if t := c.Query("type"); t != "" {
		countSess = countSess.Where("type=?", t)
	}
	total, _ := countSess.Count(&model.Alert{})
	var items []model.Alert
	sess.Limit(size, (page-1)*size).Find(&items)
	c.JSON(http.StatusOK, gin.H{"alerts": items, "total": total})
}

// PATCH /admin/alerts/:id/ack  确认告警
func AckAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	adminID := getAdminID(c)
	now := time.Now()
	db.Engine.Exec(
		"UPDATE alerts SET status='acked', acked_by=$1, acked_at=$2 WHERE id=$3 AND status='open'",
		adminID, now, id,
	)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PATCH /admin/alerts/:id/resolve  解决告警
func ResolveAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	now := time.Now()
	db.Engine.Exec(
		"UPDATE alerts SET status='resolved', resolved_at=$1 WHERE id=$2",
		now, id,
	)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─────────────────────────────────────────────
//  数据导出中心
// ─────────────────────────────────────────────

// GET /admin/exports
func ListExportTasks(c *gin.Context) {
	adminID := getAdminID(c)
	var tasks []model.ExportTask
	db.Engine.Where("created_by=?", adminID).OrderBy("created_at DESC").Limit(50).Find(&tasks)
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// POST /admin/exports  创建导出任务（异步，直接标记为 pending）
func CreateExportTask(c *gin.Context) {
	var req struct {
		Name   string     `json:"name"`
		Type   string     `json:"type"`
		Params model.JSON `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	task := &model.ExportTask{
		Name:      req.Name,
		Type:      req.Type,
		Params:    req.Params,
		Status:    "pending",
		CreatedBy: getAdminID(c),
		ExpiresAt: &expires,
	}
	if _, err := db.Engine.Insert(task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 在后台 goroutine 中执行导出
	go runExportTask(task.ID, c.Request.Host)
	c.JSON(http.StatusCreated, task)
}

// runExportTask 在后台执行导出任务，生成 CSV 文件。
func runExportTask(taskID int64, host string) {
	fail := func(msg string) {
		db.Engine.ID(taskID).Cols("status", "error_msg").Update(&model.ExportTask{
			Status:   "failed",
			ErrorMsg: msg,
		})
	}

	var task model.ExportTask
	if found, _ := db.Engine.ID(taskID).Get(&task); !found {
		return
	}

	// 标记为处理中
	db.Engine.ID(taskID).Cols("status").Update(&model.ExportTask{Status: "processing"})

	// 根据类型执行查询并构建 CSV 行
	var headers []string
	var rows [][]string

	switch task.Type {
	case "transactions":
		type row struct {
			ID        int64     `xorm:"id"`
			UserID    int64     `xorm:"user_id"`
			Type      string    `xorm:"type"`
			Credits   int64     `xorm:"credits"`
			Cost      int64     `xorm:"cost"`
			CorrID    string    `xorm:"corr_id"`
			CreatedAt time.Time `xorm:"created_at"`
		}
		var records []row
		db.Engine.Table("billing_transactions").OrderBy("id DESC").Limit(100000).Find(&records)
		headers = []string{"ID", "用户ID", "类型", "积分变动(CNY)", "成本(CNY)", "关联ID", "时间"}
		for _, r := range records {
			rows = append(rows, []string{
				fmt.Sprintf("%d", r.ID),
				fmt.Sprintf("%d", r.UserID),
				r.Type,
				fmt.Sprintf("%.6f", float64(r.Credits)/1_000_000),
				fmt.Sprintf("%.6f", float64(r.Cost)/1_000_000),
				r.CorrID,
				r.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
	case "billing":
		type row struct {
			ID        int64     `xorm:"id"`
			UserID    int64     `xorm:"user_id"`
			Type      string    `xorm:"type"`
			Credits   int64     `xorm:"credits"`
			Cost      int64     `xorm:"cost"`
			CorrID    string    `xorm:"corr_id"`
			CreatedAt time.Time `xorm:"created_at"`
		}
		var records []row
		db.Engine.Table("billing_transactions").OrderBy("id DESC").Limit(100000).Find(&records)
		headers = []string{"ID", "用户ID", "类型", "积分变动(CNY)", "成本(CNY)", "关联ID", "时间"}
		for _, r := range records {
			rows = append(rows, []string{
				fmt.Sprintf("%d", r.ID),
				fmt.Sprintf("%d", r.UserID),
				r.Type,
				fmt.Sprintf("%.6f", float64(r.Credits)/1_000_000),
				fmt.Sprintf("%.6f", float64(r.Cost)/1_000_000),
				r.CorrID,
				r.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
	case "users":
		var records []model.User
		db.Engine.Table("users").OrderBy("id ASC").Limit(100000).Find(&records)
		headers = []string{"ID", "用户名", "邮箱", "余额(CNY)", "角色", "状态", "注册时间"}
		for _, r := range records {
			email := ""
			if r.Email != nil {
				email = *r.Email
			}
			status := "正常"
			if !r.IsActive {
				status = "已冻结"
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", r.ID),
				r.Username,
				email,
				fmt.Sprintf("%.6f", float64(r.Balance)/1_000_000),
				r.Role,
				status,
				r.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
	case "payments":
		type row struct {
			ID         int64     `xorm:"id"`
			UserID     int64     `xorm:"user_id"`
			OutTradeNo string    `xorm:"out_trade_no"`
			Amount     float64   `xorm:"amount"`
			Credits    int64     `xorm:"credits"`
			Status     string    `xorm:"status"`
			PayChannel string    `xorm:"pay_channel"`
			PayFlat    int       `xorm:"pay_flat"`
			CreatedAt  time.Time `xorm:"created_at"`
		}
		var records []row
		db.Engine.Table("payment_orders").OrderBy("id DESC").Limit(100000).Find(&records)
		headers = []string{"ID", "用户ID", "订单号", "金额(元)", "到账金额(¥)", "状态", "支付渠道", "时间"}
		for _, r := range records {
			statusZH := r.Status
			switch r.Status {
			case "paid":
				statusZH = "已支付"
			case "pending":
				statusZH = "待支付"
			case "failed":
				statusZH = "失败"
			case "refunded":
				statusZH = "已退款"
			}
			payChannelZH := r.PayChannel
			switch r.PayChannel {
			case "wechat":
				payChannelZH = "微信支付"
			case "alipay":
				payChannelZH = "支付宝"
			case "epay":
				payChannelZH = "Epay"
			case "":
				if r.PayFlat == 1 {
					payChannelZH = "微信支付"
				} else if r.PayFlat == 2 {
					payChannelZH = "支付宝"
				} else {
					payChannelZH = "-"
				}
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", r.ID),
				fmt.Sprintf("%d", r.UserID),
				r.OutTradeNo,
				fmt.Sprintf("%.2f", r.Amount),
				fmt.Sprintf("%.2f", float64(r.Credits)/1_000_000),
				statusZH,
				payChannelZH,
				r.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
	case "llm_logs":
		type row struct {
			ID        int64     `xorm:"id"`
			UserID    int64     `xorm:"user_id"`
			ChannelID int64     `xorm:"channel_id"`
			Model     string    `xorm:"model"`
			Status    string    `xorm:"status"`
			CorrID    string    `xorm:"corr_id"`
			ErrorMsg  string    `xorm:"error_msg"`
			CreatedAt time.Time `xorm:"created_at"`
		}
		var records []row
		db.Engine.Table("llm_logs").OrderBy("id DESC").Limit(100000).Find(&records)
		headers = []string{"ID", "用户ID", "渠道ID", "模型", "状态", "CorrID", "错误信息", "时间"}
		for _, r := range records {
			rows = append(rows, []string{
				fmt.Sprintf("%d", r.ID),
				fmt.Sprintf("%d", r.UserID),
				fmt.Sprintf("%d", r.ChannelID),
				r.Model,
				r.Status,
				r.CorrID,
				r.ErrorMsg,
				r.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
	default:
		fail("不支持的导出类型: " + task.Type)
		return
	}

	// 写入 CSV 文件
	exportDir := filepath.Join("uploads", "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		fail("创建导出目录失败: " + err.Error())
		return
	}
	filename := fmt.Sprintf("export_%d_%d.csv", taskID, time.Now().Unix())
	fullPath := filepath.Join(exportDir, filename)
	f, err := os.Create(fullPath)
	if err != nil {
		fail("创建文件失败: " + err.Error())
		return
	}
	// UTF-8 BOM 让 Excel 正常显示中文
	f.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(f)
	w.Write(headers)
	w.WriteAll(rows)
	w.Flush()
	f.Close()

	if err := w.Error(); err != nil {
		fail("写入 CSV 失败: " + err.Error())
		return
	}

	info, _ := os.Stat(fullPath)
	fileSize := int64(0)
	if info != nil {
		fileSize = info.Size()
	}
	fileURL := fmt.Sprintf("/uploads/exports/%s", filename)

	db.Engine.ID(taskID).Cols("status", "progress", "file_url", "file_size").Update(&model.ExportTask{
		Status:   "done",
		Progress: 100,
		FileURL:  fileURL,
		FileSize: fileSize,
	})
}

// ─────────────────────────────────────────────
//  上游平台管理
// ─────────────────────────────────────────────

// GET /admin/upstream-platforms
func ListUpstreamPlatforms(c *gin.Context) {
	var items []model.UpstreamPlatform
	db.Engine.OrderBy("created_at DESC").Find(&items)
	c.JSON(http.StatusOK, gin.H{"platforms": items})
}

// POST /admin/upstream-platforms
func CreateUpstreamPlatform(c *gin.Context) {
	var p model.UpstreamPlatform
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 简单存储（生产中 APIKey 应加密）
	if _, err := db.Engine.Insert(&p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p.APIKeyEnc = "" // 不返回
	c.JSON(http.StatusCreated, p)
}

// PUT /admin/upstream-platforms/:id
func UpdateUpstreamPlatform(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var p model.UpstreamPlatform
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p.ID = id
	db.Engine.ID(id).AllCols().Update(&p)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /admin/upstream-platforms/:id
func DeleteUpstreamPlatform(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	db.Engine.Delete(&model.UpstreamPlatform{ID: id})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /admin/upstream-platforms/:id/models  拉取上游可用模型列表
func GetUpstreamModels(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var p model.UpstreamPlatform
	if found, _ := db.Engine.ID(id).Get(&p); !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "平台不存在"})
		return
	}
	baseURL := p.BaseURL
	if baseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "平台未配置 Base URL"})
		return
	}
	apiKey := p.APIKeyEnc // stored as plaintext for now

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", baseURL+"/v1/models", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "请求上游失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("上游响应 %d", resp.StatusCode)})
		return
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "解析上游响应失败: " + err.Error()})
		return
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

// POST /admin/channels/batch-from-upstream  从上游平台一键批量创建渠道
func BatchCreateChannelsFromUpstream(c *gin.Context) {
	var req struct {
		PlatformID int64    `json:"platform_id"`
		Models     []string `json:"models"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.PlatformID == 0 || len(req.Models) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "platform_id 和 models 为必填"})
		return
	}
	var p model.UpstreamPlatform
	if found, _ := db.Engine.ID(req.PlatformID).Get(&p); !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "平台不存在"})
		return
	}

	created := 0
	for _, modelName := range req.Models {
		ch := &model.Channel{
			Name:        p.Name + " - " + modelName,
			Model:       modelName,
			Type:        "llm",
			BaseURL:     p.BaseURL + "/v1/chat/completions",
			Method:      "POST",
			BillingType: "token",
			Protocol:    "openai",
			IsActive:    true,
			Weight:      1,
		}
		// Set Authorization header
		if p.APIKeyEnc != "" {
			ch.Headers = model.JSON{"Authorization": "Bearer " + p.APIKeyEnc}
		}
		if _, err := db.Engine.Insert(ch); err == nil {
			created++
		}
	}
	c.JSON(http.StatusCreated, gin.H{"created": created})
}

// ─────────────────────────────────────────────
//  管理员个人信息 & 权限
// ─────────────────────────────────────────────

// GET /admin/me  返回当前管理员的基本信息和合并后的权限集
func GetAdminMe(c *gin.Context) {
	userID, _ := c.Get("user_id")
	var user model.User
	if found, _ := db.Engine.ID(userID).Cols("id", "username", "email", "role").Get(&user); !found {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
		return
	}

	// 查询该管理员是否有显式角色绑定
	// 有绑定 → 使用绑定角色的权限集合（受限管理员）
	// 无绑定 → 视为超级管理员，返回 ["*"]
	var userRoles []model.AdminUserRole
	db.Engine.Where("admin_id = ?", userID).Find(&userRoles)
	if len(userRoles) == 0 {
		// 未绑定任何角色 → 超级管理员
		c.JSON(http.StatusOK, gin.H{
			"user_id":     user.ID,
			"username":    user.Username,
			"email":       user.Email,
			"role":        user.Role,
			"permissions": []string{"*"},
		})
		return
	}

	roleIDs := make([]interface{}, 0, len(userRoles))
	for _, ur := range userRoles {
		roleIDs = append(roleIDs, ur.RoleID)
	}

	var roles []model.AdminRole
	db.Engine.Table("admin_roles").In("id", roleIDs...).Cols("permissions").Find(&roles)

	seen := map[string]bool{}
	perms := []string{}
	for _, r := range roles {
		for _, p := range r.Permissions {
			if !seen[p] {
				seen[p] = true
				perms = append(perms, p)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":     user.ID,
		"username":    user.Username,
		"email":       user.Email,
		"role":        user.Role,
		"permissions": perms,
	})
}

// ─────────────────────────────────────────────
//  号池绑定关系
// ─────────────────────────────────────────────

// GET /admin/key-pools/:id/channels  获取引用该号池的所有渠道
func GetKeyPoolChannels(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var channels []model.Channel
	db.Engine.Where("key_pool_id = ?", id).Find(&channels)
	c.JSON(http.StatusOK, gin.H{"channels": channels})
}

// ─────────────────────────────────────────────
//  RBAC 角色管理
// ─────────────────────────────────────────────

// GET /admin/roles
func ListRoles(c *gin.Context) {
	var roles []model.AdminRole
	db.Engine.OrderBy("id ASC").Find(&roles)
	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

// POST /admin/roles
func CreateRole(c *gin.Context) {
	var r model.AdminRole
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	r.IsBuiltin = false
	if _, err := db.Engine.Insert(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, r)
}

// PUT /admin/roles/:id
func UpdateRole(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var r model.AdminRole
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	r.ID = id
	if _, err := db.Engine.ID(id).Cols("label", "permissions").Update(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /admin/roles/:id
func DeleteRole(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	// 内置角色不允许删除
	var r model.AdminRole
	if found, _ := db.Engine.ID(id).Cols("is_builtin").Get(&r); !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色不存在"})
		return
	}
	if r.IsBuiltin {
		c.JSON(http.StatusForbidden, gin.H{"error": "内置角色不允许删除"})
		return
	}
	// 先清理绑定关系，再删除角色，避免孤儿数据
	db.Engine.Where("role_id = ?", id).Delete(&model.AdminUserRole{})
	db.Engine.Delete(&model.AdminRole{ID: id})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─────────────────────────────────────────────
//  管理员账号管理
// ─────────────────────────────────────────────

// GET /admin/admins  列出所有管理员账号及其角色
func ListAdminUsers(c *gin.Context) {
	var users []model.User
	db.Engine.Where("role = ?", "admin").
		Cols("id", "username", "email", "created_at").
		OrderBy("id ASC").Find(&users)

	// 批量查询每位管理员绑定的角色
	type adminItem struct {
		ID        int64    `json:"id"`
		Username  string   `json:"username"`
		Email     *string  `json:"email"`
		RoleIDs   []int64  `json:"role_ids"`
		RoleNames []string `json:"role_names"`
	}

	if len(users) == 0 {
		c.JSON(http.StatusOK, gin.H{"admins": []adminItem{}})
		return
	}

	adminIDs := make([]interface{}, 0, len(users))
	for _, u := range users {
		adminIDs = append(adminIDs, u.ID)
	}

	var userRoles []model.AdminUserRole
	db.Engine.In("admin_id", adminIDs...).Find(&userRoles)

	// 收集需要查询的 role_ids
	roleIDSet := map[int64]bool{}
	for _, ur := range userRoles {
		roleIDSet[ur.RoleID] = true
	}
	roleIDSlice := make([]interface{}, 0, len(roleIDSet))
	for rid := range roleIDSet {
		roleIDSlice = append(roleIDSlice, rid)
	}

	roleMap := map[int64]string{}
	if len(roleIDSlice) > 0 {
		var roles []model.AdminRole
		db.Engine.In("id", roleIDSlice...).Cols("id", "name").Find(&roles)
		for _, r := range roles {
			roleMap[r.ID] = r.Name
		}
	}

	// 按 admin_id 分组
	rolesOf := map[int64][]int64{}
	for _, ur := range userRoles {
		rolesOf[ur.AdminID] = append(rolesOf[ur.AdminID], ur.RoleID)
	}

	items := make([]adminItem, 0, len(users))
	for _, u := range users {
		rIDs := rolesOf[u.ID]
		rNames := make([]string, 0, len(rIDs))
		for _, rid := range rIDs {
			if name, ok := roleMap[rid]; ok {
				rNames = append(rNames, name)
			}
		}
		items = append(items, adminItem{
			ID:        u.ID,
			Username:  u.Username,
			Email:     u.Email,
			RoleIDs:   rIDs,
			RoleNames: rNames,
		})
	}
	c.JSON(http.StatusOK, gin.H{"admins": items})
}

// PUT /admin/admins/:id/roles  设置管理员绑定的角色（全量替换）
func SetAdminRoles(c *gin.Context) {
	adminID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}

	var req struct {
		RoleIDs []int64 `json:"role_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 校验被操作的用户确实是 admin 角色
	var target model.User
	found, _ := db.Engine.ID(adminID).Cols("id", "role").Get(&target)
	if !found || target.Role != "admin" {
		c.JSON(http.StatusNotFound, gin.H{"error": "管理员不存在"})
		return
	}

	// 在事务内全量替换：先删全部旧绑定，再逐条插入
	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "事务开启失败"})
		return
	}
	if _, err := sess.Where("admin_id = ?", adminID).Delete(&model.AdminUserRole{}); err != nil {
		_ = sess.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, rid := range req.RoleIDs {
		if _, err := sess.Insert(&model.AdminUserRole{AdminID: adminID, RoleID: rid}); err != nil {
			_ = sess.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err := sess.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─────────────────────────────────────────────
//  优惠券管理
// ─────────────────────────────────────────────

// GET /admin/coupons
func ListCoupons(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	engine := db.Engine
	total, _ := engine.Count(&model.Coupon{})
	var items []model.Coupon
	engine.OrderBy("created_at DESC").Limit(size, (page-1)*size).Find(&items)
	c.JSON(http.StatusOK, gin.H{"coupons": items, "total": total})
}

// POST /admin/coupons
func CreateCoupon(c *gin.Context) {
	var req struct {
		Code          string `json:"code"`
		Type          string `json:"type"`
		Title         string `json:"title"`
		DiscountType  string `json:"discount_type"`
		DiscountValue int64  `json:"discount_value"`
		MinAmount     int64  `json:"min_amount"`
		MaxDiscount   int64  `json:"max_discount"`
		TotalCount    int    `json:"total_count"`
		PerUserLimit  int    `json:"per_user_limit"`
		ValidFrom     string `json:"valid_from"`
		ValidUntil    string `json:"valid_until"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var validFrom *time.Time
	if t, err := parseDateTime(req.ValidFrom, false); err == nil && !t.IsZero() {
		validFrom = &t
	}
	var validUntil *time.Time
	if t, err := parseDateTime(req.ValidUntil, true); err == nil && !t.IsZero() {
		validUntil = &t
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		code = fmt.Sprintf("CPN%d%d", time.Now().Unix(), rand.Intn(10000))
	}
	cp := &model.Coupon{
		Code:          code,
		Type:          req.Type,
		Title:         req.Title,
		DiscountType:  req.DiscountType,
		DiscountValue: req.DiscountValue,
		MinAmount:     req.MinAmount,
		MaxDiscount:   req.MaxDiscount,
		TotalCount:    req.TotalCount,
		PerUserLimit:  req.PerUserLimit,
		ValidFrom:     validFrom,
		ValidUntil:    validUntil,
		CreatedBy:     getAdminID(c),
	}
	if _, err := db.Engine.Insert(cp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, cp)
}

// DELETE /admin/coupons/:id  作废整批优惠券
func VoidCoupon(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	// 将有效期设为过去，达到作废效果
	db.Engine.Exec("UPDATE coupons SET valid_until=NOW()-INTERVAL '1 second' WHERE id=$1", id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /admin/coupons/:id/uses  优惠券使用记录
func ListCouponUses(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var uses []model.CouponUse
	db.Engine.Where("coupon_id=?", id).OrderBy("created_at DESC").Limit(100).Find(&uses)
	c.JSON(http.StatusOK, gin.H{"uses": uses})
}

// ─────────────────────────────────────────────
//  客户充值明细
// ─────────────────────────────────────────────

// GET /admin/payments  管理员查看所有支付订单
func AdminListPaymentOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	engine := db.Engine
	sess := engine.Table("payment_orders").
		Select("payment_orders.*, users.email AS user_email").
		Join("LEFT", "users", "users.id = payment_orders.user_id").
		OrderBy("payment_orders.created_at DESC")

	if s := c.Query("status"); s != "" {
		sess = sess.Where("payment_orders.status=?", s)
	}
	if uid := c.Query("user_id"); uid != "" {
		sess = sess.Where("payment_orders.user_id=?", uid)
	}
	if email := c.Query("email"); email != "" {
		sess = sess.Where("users.email ILIKE ?", "%"+email+"%")
	}
	if pf := c.Query("pay_flat"); pf != "" {
		sess = sess.Where("payment_orders.pay_flat=?", pf)
	}
	if pc := c.Query("pay_channel"); pc != "" {
		sess = sess.Where("payment_orders.pay_channel=?", pc)
	}
	startAt := c.Query("start_at")
	endAt := c.Query("end_at")
	if startAt != "" {
		t, _ := parseDateTime(startAt, false)
		if !t.IsZero() {
			sess = sess.Where("payment_orders.created_at>=?", t)
		}
	}
	if endAt != "" {
		t, _ := parseDateTime(endAt, true)
		if !t.IsZero() {
			sess = sess.Where("payment_orders.created_at<=?", t)
		}
	}

	type orderRow struct {
		model.PaymentOrder `xorm:"extends"`
		UserEmail          string `json:"user_email" xorm:"user_email"`
	}
	countSess := engine.Table("payment_orders").Join("LEFT", "users", "users.id = payment_orders.user_id")
	if s := c.Query("status"); s != "" {
		countSess = countSess.Where("payment_orders.status=?", s)
	}
	if uid := c.Query("user_id"); uid != "" {
		countSess = countSess.Where("payment_orders.user_id=?", uid)
	}
	if email := c.Query("email"); email != "" {
		countSess = countSess.Where("users.email ILIKE ?", "%"+email+"%")
	}
	if pf := c.Query("pay_flat"); pf != "" {
		countSess = countSess.Where("payment_orders.pay_flat=?", pf)
	}
	if pc := c.Query("pay_channel"); pc != "" {
		countSess = countSess.Where("payment_orders.pay_channel=?", pc)
	}
	if startAt != "" {
		if t, _ := parseDateTime(startAt, false); !t.IsZero() {
			countSess = countSess.Where("payment_orders.created_at>=?", t)
		}
	}
	if endAt != "" {
		if t, _ := parseDateTime(endAt, true); !t.IsZero() {
			countSess = countSess.Where("payment_orders.created_at<=?", t)
		}
	}
	total, _ := countSess.Count(&model.PaymentOrder{})
	var rows []orderRow
	sess.Limit(size, (page-1)*size).Find(&rows)
	c.JSON(http.StatusOK, gin.H{"orders": rows, "total": total})
}

// ─────────────────────────────────────────────
//  系统设置操作日志（通过审计日志查询）
// ─────────────────────────────────────────────

// GET /admin/settings/logs
func ListSettingLogs(c *gin.Context) {
	var logs []model.AdminAuditLog
	db.Engine.Where("resource_type='settings'").OrderBy("created_at DESC").Limit(100).Find(&logs)
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}
