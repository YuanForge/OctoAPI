package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"fanapi/internal/billing"
	"fanapi/internal/db"
	"fanapi/internal/model"

	"github.com/gin-gonic/gin"
)

// GetInviteInfo 返回当前用户的邀请码、已邀请人数、冻结积分余额。
//
// @Summary      查询邀请信息
// @Description  返回邀请码、邀请人数及冻结积分（待解冻返佣）
// @Tags         邀请
// @Security     BearerAuth
// @Success      200  {object}  object{invite_code=string,invite_count=int,frozen_balance=int}
// @Router       /user/invite [get]
func GetInviteInfo(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)

	var user model.User
	if found, err := db.Engine.ID(userID).Cols("invite_code", "frozen_balance").Get(&user); err != nil || !found {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取用户信息失败"})
		return
	}

	// 兼容旧账号：若邀请码为空则自动生成并持久化
	if user.InviteCode == "" {
		code := generateInviteCode()
		if n, err := db.Engine.Exec(
			"UPDATE users SET invite_code = $1 WHERE id = $2 AND (invite_code IS NULL OR invite_code = '')",
			code, userID,
		); err == nil {
			if rows, _ := n.RowsAffected(); rows > 0 {
				user.InviteCode = code
			}
		}
		// 若并发导致 UPDATE 未命中（其他实例已写入），重新读一次
		if user.InviteCode == "" {
			db.Engine.ID(userID).Cols("invite_code").Get(&user) //nolint:errcheck
		}
	}

	count, _ := db.Engine.Where("inviter_id = ?", userID).Count(&model.User{})

	c.JSON(http.StatusOK, gin.H{
		"invite_code":    user.InviteCode,
		"invite_count":   count,
		"frozen_balance": user.FrozenBalance,
	})
}

// ConvertFrozenBalance 将冻结积分转为可用积分。
//
// @Summary      解冻积分
// @Description  将指定数量的冻结返佣积分转换为可用余额
// @Tags         邀请
// @Security     BearerAuth
// @Param        body  body      object{amount=int}  true  "解冻数量（单位：积分，0 表示全部）"
// @Success      200   {object}  object{converted=int,available_balance=int}
// @Router       /user/invite/convert [post]
func ConvertFrozenBalance(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)

	var req struct {
		Amount int64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user model.User
	if found, err := db.Engine.ID(userID).Cols("frozen_balance").Get(&user); err != nil || !found {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取用户信息失败"})
		return
	}

	toConvert := req.Amount
	if toConvert <= 0 || toConvert > user.FrozenBalance {
		toConvert = user.FrozenBalance
	}
	if toConvert <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无可用冻结积分"})
		return
	}

	// 原子操作：减少 frozen_balance，增加可用余额。Redis 只作为缓存，失败不影响账务。
	n, err := db.Engine.Exec(
		"UPDATE users SET frozen_balance = frozen_balance - $1, balance = balance + $1 WHERE id = $2 AND frozen_balance >= $1",
		toConvert, userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解冻失败，请稍后重试"})
		return
	}
	affected, _ := n.RowsAffected()
	if affected == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "冻结积分不足"})
		return
	}

	billing.InvalidateBalanceCache(c.Request.Context(), userID)

	newBalance, _ := billing.GetBalance(c.Request.Context(), userID)
	c.JSON(http.StatusOK, gin.H{
		"converted":         toConvert,
		"available_balance": newBalance,
	})
}

// generateInviteCode 生成 16 位十六进制邀请码（本包内使用）。
func generateInviteCode() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

// GetInviteeList 返回当前用户邀请的用户列表，包含用户名、累计充值额、累计消费额。
// GET /user/invite/list
func GetInviteeList(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)

	type inviteeRow struct {
		ID            int64     `json:"id" xorm:"id"`
		Username      string    `json:"username" xorm:"username"`
		TotalRecharge float64   `json:"total_recharge" xorm:"total_recharge"`
		TotalSpend    float64   `json:"total_spend" xorm:"total_spend"`
		CreatedAt     time.Time `json:"created_at" xorm:"created_at"`
	}

	var rows []inviteeRow
	err := db.Engine.SQL(`
		SELECT u.id, u.username,
		       COALESCE((SELECT SUM(amount) FROM payment_orders WHERE user_id = u.id AND status = 'paid'), 0)       AS total_recharge,
		       COALESCE((SELECT SUM(CASE WHEN type IN ('charge','hold','settle') THEN credits WHEN type = 'refund' THEN -credits ELSE 0 END) FROM billing_transactions WHERE user_id = u.id), 0) / 1000000.0 AS total_spend,
		       u.created_at
		FROM users u
		WHERE u.inviter_id = $1
		ORDER BY u.created_at DESC
		LIMIT 200
	`, userID).Find(&rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	if rows == nil {
		rows = []inviteeRow{}
	}
	c.JSON(http.StatusOK, gin.H{"invitees": rows})
}
