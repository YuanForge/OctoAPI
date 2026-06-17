package handler

import (
	"fanapi/internal/db"
	"fanapi/internal/model"
	"fanapi/internal/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"strconv"
)

// PUT /admin/users/:id/password — 管理员重置任意用户密码
func ResetUserPassword(c *gin.Context) {
	targetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var req struct {
		Password string `json:"password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}
	affected, err := db.Engine.ID(targetID).Cols("password_hash").Update(&model.User{PasswordHash: string(hash)})
	if err != nil || affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "密码已重置"})
}

// GET /admin/users — 用户列表（分页 + 复合筛选）
// 支持参数: email(模糊), uid, status(active/frozen), group, balance_min, balance_max,
//
//	created_after, created_before (YYYY-MM-DD 或 RFC3339)
func ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}

	type adminUserRow struct {
		ID           int64    `json:"id"`
		Username     string   `json:"username"`
		Email        *string  `json:"email"`
		Role         string   `json:"role"`
		Group        string   `json:"group"`
		Balance      int64    `json:"balance"`
		IsActive     bool     `json:"is_active"`
		FrozenReason string   `json:"frozen_reason,omitempty"`
		RebateRatio  *float64 `json:"rebate_ratio,omitempty"`
		CreatedAt    string   `json:"created_at"`
		InviteCount  int64    `json:"invite_count"`
		TotalSpent   int64    `json:"total_spent"`
	}

	// 构建 WHERE 条件
	whereClauses := []string{"1=1"}
	args := []interface{}{}
	argIdx := 1
	balanceExpr := "u.balance + COALESCE((SELECT SUM(remaining_credits) FROM billing_quota_leases WHERE user_id = u.id AND status = 'active' AND expires_at > NOW()), 0)"

	if email := c.Query("email"); email != "" {
		whereClauses = append(whereClauses, "u.email ILIKE $"+strconv.Itoa(argIdx))
		args = append(args, "%"+email+"%")
		argIdx++
	}
	if uid := c.Query("uid"); uid != "" {
		whereClauses = append(whereClauses, "u.id = $"+strconv.Itoa(argIdx))
		args = append(args, uid)
		argIdx++
	}
	if status := c.Query("status"); status != "" {
		switch status {
		case "active":
			whereClauses = append(whereClauses, "u.is_active = true")
		case "frozen":
			whereClauses = append(whereClauses, "u.is_active = false")
		}
	}
	if group := c.Query("group"); group != "" {
		whereClauses = append(whereClauses, `u."group" = $`+strconv.Itoa(argIdx))
		args = append(args, group)
		argIdx++
	}
	if balMin := c.Query("balance_min"); balMin != "" {
		if v, err := strconv.ParseInt(balMin, 10, 64); err == nil {
			whereClauses = append(whereClauses, balanceExpr+" >= $"+strconv.Itoa(argIdx))
			args = append(args, v*1_000_000)
			argIdx++
		}
	}
	if balMax := c.Query("balance_max"); balMax != "" {
		if v, err := strconv.ParseInt(balMax, 10, 64); err == nil {
			whereClauses = append(whereClauses, balanceExpr+" <= $"+strconv.Itoa(argIdx))
			args = append(args, v*1_000_000)
			argIdx++
		}
	}
	if createdAfter, err := parseDateTime(c.Query("created_after"), false); err == nil && !createdAfter.IsZero() {
		whereClauses = append(whereClauses, "u.created_at >= $"+strconv.Itoa(argIdx))
		args = append(args, createdAfter)
		argIdx++
	}
	if createdBefore, err := parseDateTime(c.Query("created_before"), true); err == nil && !createdBefore.IsZero() {
		whereClauses = append(whereClauses, "u.created_at <= $"+strconv.Itoa(argIdx))
		args = append(args, createdBefore)
		argIdx++
	}

	where := ""
	for i, clause := range whereClauses {
		if i == 0 {
			where = "WHERE " + clause
		} else {
			where += " AND " + clause
		}
	}

	limitArg := argIdx
	offsetArg := argIdx + 1
	args = append(args, size, (page-1)*size)

	sql := `
SELECT
  u.id, u.username, u.email, u.role, u."group", ` + balanceExpr + ` AS balance, u.is_active, u.frozen_reason, u.rebate_ratio, u.created_at,
  COALESCE((SELECT COUNT(*) FROM users WHERE inviter_id = u.id), 0) AS invite_count,
  COALESCE((SELECT SUM(CASE WHEN type IN ('charge','hold','settle') THEN credits WHEN type = 'refund' THEN -credits ELSE 0 END) FROM billing_transactions WHERE user_id = u.id), 0) AS total_spent
FROM users u
` + where + `
ORDER BY u.id DESC
LIMIT $` + strconv.Itoa(limitArg) + ` OFFSET $` + strconv.Itoa(offsetArg)

	rows, err := db.Engine.SQL(sql, args...).QueryString()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]adminUserRow, 0, len(rows))
	for _, r := range rows {
		id, _ := strconv.ParseInt(r["id"], 10, 64)
		balance, _ := strconv.ParseInt(r["balance"], 10, 64)
		inviteCount, _ := strconv.ParseInt(r["invite_count"], 10, 64)
		totalSpent, _ := strconv.ParseInt(r["total_spent"], 10, 64)
		isActive := r["is_active"] == "true" || r["is_active"] == "t" || r["is_active"] == "1"
		row := adminUserRow{
			ID:           id,
			Username:     r["username"],
			Role:         r["role"],
			Group:        r["group"],
			Balance:      balance,
			IsActive:     isActive,
			FrozenReason: r["frozen_reason"],
			CreatedAt:    r["created_at"],
			InviteCount:  inviteCount,
			TotalSpent:   totalSpent,
		}
		if email, ok := r["email"]; ok && email != "" {
			row.Email = &email
		}
		if ratioStr, ok := r["rebate_ratio"]; ok && ratioStr != "" {
			ratio, err := strconv.ParseFloat(ratioStr, 64)
			if err == nil {
				row.RebateRatio = &ratio
			}
		}
		result = append(result, row)
	}

	// 计算过滤后的总数
	countSQL := "SELECT COUNT(*) FROM users u " + where
	countArgs := args[:len(args)-2]
	total, _ := db.Engine.SQL(countSQL, countArgs...).Count()
	c.JSON(http.StatusOK, gin.H{"users": result, "total": total})
}

// PUT /admin/users/:id/group — 设置用户定价分组
func SetUserGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var req struct {
		Group string `json:"group"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := db.Engine.ID(id).Cols("group").Update(&model.User{Group: req.Group}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "group updated"})
}

// POST /admin/users — 管理员/运营创建用户账号
func CreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required,min=3,max=32"`
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=8"`
		Role     string `json:"role"` // 默认 "user"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role := req.Role
	if role == "" {
		role = "user"
	}
	allowedRoles := map[string]bool{"user": true, "agent": true, "admin": true, "operator": true}
	if !allowedRoles[role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色值无效"})
		return
	}

	// 检查邮箱唯一性
	if exists, _ := db.Engine.Where("email = ?", req.Email).Exist(new(model.User)); exists {
		c.JSON(http.StatusConflict, gin.H{"error": "该邮箱已被注册"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}
	emailVal := req.Email
	inviteCode := service.GenerateInviteCode()
	user := &model.User{
		Username:     req.Username,
		Email:        &emailVal,
		PasswordHash: string(hash),
		Role:         role,
		IsActive:     true,
		InviteCode:   inviteCode,
	}
	if _, err := db.Engine.Insert(user); err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "用户名或邮箱已被占用"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败，请稍后重试"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": user.ID, "username": user.Username, "email": user.Email})
}

// DELETE /admin/users/:id — 管理员硬删除用户（同时删除其所有 API Key）
// 仅 admin 角色可操作，operator 无此权限。
func DeleteUser(c *gin.Context) {
	// 只允许 admin 删除
	if role, _ := c.Get("role"); role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅管理员可删除用户"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}

	// 软验证：不允许删除 admin 账户，防止误删
	target := &model.User{}
	found, _ := db.Engine.ID(id).Cols("role").Get(target)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	if target.Role == "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "不能删除管理员账户"})
		return
	}

	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "事务开启失败"})
		return
	}

	// 删除该用户的所有 API Key（硬删除）
	if _, err := sess.Where("user_id = ?", id).Delete(new(model.APIKey)); err != nil {
		sess.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除 API Key 失败"})
		return
	}
	// 硬删除用户
	if _, err := sess.ID(id).Delete(new(model.User)); err != nil {
		sess.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除用户失败"})
		return
	}
	if err := sess.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "事务提交失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "用户已删除"})
}

// isUniqueViolation 判断数据库错误是否为唯一约束冲突。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "duplicate") || contains(msg, "unique")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// PATCH /admin/users/:id/freeze — 冻结或解冻账户
// 冻结后：用户无法登录，其 API Key 也无法使用。
func FreezeUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}
	var req struct {
		Freeze bool   `json:"freeze"`
		Reason string `json:"reason"` // 冻结原因（解冻时可忽略）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	reason := ""
	if req.Freeze {
		reason = req.Reason
	}
	affected, err := db.Engine.ID(id).Cols("is_active", "frozen_reason").Update(&model.User{IsActive: !req.Freeze, FrozenReason: reason})
	if err != nil || affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	msg := "账户已冻结"
	if !req.Freeze {
		msg = "账户已解冻"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}

// POST /admin/users/batch — 批量操作用户
// body: { "action": "freeze"|"unfreeze"|"set_group", "ids": [1,2,3], "group": "vip", "reason": "..." }
func BatchUpdateUsers(c *gin.Context) {
	var req struct {
		Action string  `json:"action" binding:"required"`
		IDs    []int64 `json:"ids" binding:"required,min=1"`
		Group  string  `json:"group"`
		Reason string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.IDs) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "单次批量不超过 200 个"})
		return
	}

	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "事务开启失败"})
		return
	}

	switch req.Action {
	case "freeze":
		if _, err := sess.In("id", req.IDs).Cols("is_active", "frozen_reason").
			Update(&model.User{IsActive: false, FrozenReason: req.Reason}); err != nil {
			sess.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case "unfreeze":
		if _, err := sess.In("id", req.IDs).Cols("is_active", "frozen_reason").
			Update(&model.User{IsActive: true, FrozenReason: ""}); err != nil {
			sess.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case "set_group":
		if req.Group == "" {
			sess.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{"error": "set_group 操作需要提供 group 值"})
			return
		}
		if _, err := sess.In("id", req.IDs).Cols("group").
			Update(&model.User{Group: req.Group}); err != nil {
			sess.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	default:
		sess.Rollback()
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的 action: " + req.Action})
		return
	}

	if err := sess.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "事务提交失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "批量操作成功", "count": len(req.IDs)})
}
