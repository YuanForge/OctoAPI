package model

import "time"

// CardBatch 卡密批次
type CardBatch struct {
	ID        int64     `xorm:"pk autoincr 'id'" json:"id"`
	BatchID   string    `xorm:"notnull unique 'batch_id'" json:"batch_id"`
	Note      string    `xorm:"text 'note'" json:"note"`
	Credits   int64     `xorm:"notnull 'credits'" json:"credits"`
	Count     int       `xorm:"notnull 'count'" json:"count"`
	CreatedBy int64     `xorm:"default(0) 'created_by'" json:"created_by"`
	CreatedAt time.Time `xorm:"created 'created_at'" json:"created_at"`
}

func (*CardBatch) TableName() string { return "card_batches" }

// ChannelLog 渠道变更日志
type ChannelLog struct {
	ID        int64     `xorm:"pk autoincr 'id'" json:"id"`
	ChannelID int64     `xorm:"notnull index 'channel_id'" json:"channel_id"`
	AdminID   int64     `xorm:"default(0) 'admin_id'" json:"admin_id"`
	Field     string    `xorm:"'field'" json:"field"`
	OldVal    string    `xorm:"text 'old_val'" json:"old_val"`
	NewVal    string    `xorm:"text 'new_val'" json:"new_val"`
	CreatedAt time.Time `xorm:"created 'created_at'" json:"created_at"`
}

func (*ChannelLog) TableName() string { return "channel_logs" }

// AdminAuditLog 全局审计日志
type AdminAuditLog struct {
	ID           int64     `xorm:"pk autoincr 'id'" json:"id"`
	AdminID      int64     `xorm:"notnull index 'admin_id'" json:"admin_id"`
	AdminEmail   string    `xorm:"'admin_email'" json:"admin_email"`
	Action       string    `xorm:"'action'" json:"action"`               // create/update/delete/batch
	ResourceType string    `xorm:"'resource_type'" json:"resource_type"` // user/channel/card...
	ResourceID   int64     `xorm:"default(0) 'resource_id'" json:"resource_id"`
	Summary      string    `xorm:"text 'summary'" json:"summary"`
	Detail       JSON      `xorm:"jsonb 'detail'" json:"detail"`
	IP           string    `xorm:"'ip'" json:"ip"`
	UA           string    `xorm:"text 'ua'" json:"ua"`
	CreatedAt    time.Time `xorm:"created 'created_at'" json:"created_at"`
}

func (*AdminAuditLog) TableName() string { return "admin_audit_logs" }

// Notification 通知
type Notification struct {
	ID          int64      `xorm:"pk autoincr 'id'" json:"id"`
	Title       string     `xorm:"'title'" json:"title"`
	Content     string     `xorm:"text 'content'" json:"content"`
	TargetType  string     `xorm:"'target_type'" json:"target_type"` // all/group/user
	TargetValue string     `xorm:"'target_value'" json:"target_value"`
	Status      string     `xorm:"notnull default('draft') 'status'" json:"status"` // draft/sent/scheduled
	CreatedBy   int64      `xorm:"'created_by'" json:"created_by"`
	SendAt      *time.Time `xorm:"'send_at'" json:"send_at"`
	SentAt      *time.Time `xorm:"'sent_at'" json:"sent_at"`
	CreatedAt   time.Time  `xorm:"created 'created_at'" json:"created_at"`
}

func (*Notification) TableName() string { return "notifications" }

// Coupon 优惠券批次
type Coupon struct {
	ID            int64      `xorm:"pk autoincr 'id'" json:"id"`
	Code          string     `xorm:"unique 'code'" json:"code"`
	Type          string     `xorm:"'type'" json:"type"` // discount/rebate/gift
	Title         string     `xorm:"'title'" json:"title"`
	DiscountType  string     `xorm:"'discount_type'" json:"discount_type"` // amount/percent
	DiscountValue int64      `xorm:"'discount_value'" json:"discount_value"`
	MinAmount     int64      `xorm:"'min_amount'" json:"min_amount"`
	MaxDiscount   int64      `xorm:"'max_discount'" json:"max_discount"`
	TotalCount    int        `xorm:"'total_count'" json:"total_count"`
	UsedCount     int        `xorm:"'used_count'" json:"used_count"`
	PerUserLimit  int        `xorm:"default(1) 'per_user_limit'" json:"per_user_limit"`
	ValidFrom     *time.Time `xorm:"'valid_from'" json:"valid_from"`
	ValidUntil    *time.Time `xorm:"'valid_until'" json:"valid_until"`
	CreatedBy     int64      `xorm:"'created_by'" json:"created_by"`
	CreatedAt     time.Time  `xorm:"created 'created_at'" json:"created_at"`
}

func (*Coupon) TableName() string { return "coupons" }

// CouponUse 优惠券使用记录
type CouponUse struct {
	ID        int64     `xorm:"pk autoincr 'id'" json:"id"`
	CouponID  int64     `xorm:"notnull index 'coupon_id'" json:"coupon_id"`
	UserID    int64     `xorm:"notnull index 'user_id'" json:"user_id"`
	Discount  int64     `xorm:"notnull 'discount'" json:"discount"`
	CreatedAt time.Time `xorm:"created 'created_at'" json:"created_at"`
}

func (*CouponUse) TableName() string { return "coupon_uses" }

// RiskLabel 风控标签
type RiskLabel struct {
	ID        int64     `xorm:"pk autoincr 'id'" json:"id"`
	UserID    int64     `xorm:"notnull index 'user_id'" json:"user_id"`
	Label     string    `xorm:"'label'" json:"label"` // same_ip_multi/wool/high_consume/custom:xxx
	Reason    string    `xorm:"text 'reason'" json:"reason"`
	CreatedBy int64     `xorm:"default(0) 'created_by'" json:"created_by"`
	CreatedAt time.Time `xorm:"created 'created_at'" json:"created_at"`
}

func (*RiskLabel) TableName() string { return "risk_labels" }

// UpstreamPlatform 上游平台
type UpstreamPlatform struct {
	ID                     int64      `xorm:"pk autoincr 'id'" json:"id"`
	Name                   string     `xorm:"'name'" json:"name"`
	PlatformType           string     `xorm:"notnull default('openai') 'platform_type'" json:"platform_type"` // openai / newapi / sub2api
	BaseURL                string     `xorm:"text 'base_url'" json:"base_url"`
	APIKeyEnc              string     `xorm:"text 'api_key_enc'" json:"-"`
	SystemTokenEnc         string     `xorm:"text 'system_token_enc'" json:"-"`
	UpstreamUserID         string     `xorm:"notnull default('') 'upstream_user_id'" json:"upstream_user_id"`
	UpstreamGroup          string     `xorm:"notnull default('') 'upstream_group'" json:"upstream_group"`
	Balance                int64      `xorm:"'balance'" json:"balance"` // credits
	BalanceAmount          float64    `xorm:"notnull default(0) 'balance_amount'" json:"balance_amount"`
	BalanceCurrency        string     `xorm:"notnull default('CNY') 'balance_currency'" json:"balance_currency"`
	BalanceSyncedAt        *time.Time `xorm:"'balance_synced_at'" json:"balance_synced_at"`
	BalanceAlertThreshold  float64    `xorm:"notnull default(0) 'balance_alert_threshold'" json:"balance_alert_threshold"`
	BalanceAlertNotified   bool       `xorm:"notnull default(false) 'balance_alert_notified'" json:"balance_alert_notified"`
	BalanceAlertNotifiedAt *time.Time `xorm:"'balance_alert_notified_at'" json:"balance_alert_notified_at"`
	IsActive               bool       `xorm:"notnull default(true) 'is_active'" json:"is_active"`
	Note                   string     `xorm:"text 'note'" json:"note"`
	CreatedAt              time.Time  `xorm:"created 'created_at'" json:"created_at"`
	UpdatedAt              time.Time  `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (*UpstreamPlatform) TableName() string { return "upstream_platforms" }

// Alert 告警记录
type Alert struct {
	ID           int64      `xorm:"pk autoincr 'id'" json:"id"`
	Type         string     `xorm:"'type'" json:"type"` // channel_error/fail_rate/profit_negative/balance_low
	ResourceType string     `xorm:"'resource_type'" json:"resource_type"`
	ResourceID   int64      `xorm:"default(0) 'resource_id'" json:"resource_id"`
	Message      string     `xorm:"text 'message'" json:"message"`
	Status       string     `xorm:"notnull default('open') 'status'" json:"status"` // open/acked/resolved
	AckedBy      *int64     `xorm:"'acked_by'" json:"acked_by"`
	AckedAt      *time.Time `xorm:"'acked_at'" json:"acked_at"`
	ResolvedAt   *time.Time `xorm:"'resolved_at'" json:"resolved_at"`
	Detail       JSON       `xorm:"jsonb 'detail'" json:"detail"`
	CreatedAt    time.Time  `xorm:"created 'created_at'" json:"created_at"`
}

func (*Alert) TableName() string { return "alerts" }

// ExportTask 数据导出任务
type ExportTask struct {
	ID        int64      `xorm:"pk autoincr 'id'" json:"id"`
	Name      string     `xorm:"'name'" json:"name"`
	Type      string     `xorm:"'type'" json:"type"` // transactions/users/cards/llm_logs
	Params    JSON       `xorm:"jsonb 'params'" json:"params"`
	Status    string     `xorm:"notnull default('pending') 'status'" json:"status"` // pending/processing/done/failed
	Progress  int        `xorm:"default(0) 'progress'" json:"progress"`
	FileURL   string     `xorm:"text 'file_url'" json:"file_url"`
	FileSize  int64      `xorm:"default(0) 'file_size'" json:"file_size"`
	ErrorMsg  string     `xorm:"text 'error_msg'" json:"error_msg"`
	CreatedBy int64      `xorm:"'created_by'" json:"created_by"`
	ExpiresAt *time.Time `xorm:"'expires_at'" json:"expires_at"`
	CreatedAt time.Time  `xorm:"created 'created_at'" json:"created_at"`
	UpdatedAt time.Time  `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (*ExportTask) TableName() string { return "export_tasks" }

// AdminRole RBAC 角色
type AdminRole struct {
	ID          int64       `xorm:"pk autoincr 'id'" json:"id"`
	Name        string      `xorm:"notnull unique 'name'" json:"name"`
	Label       string      `xorm:"'label'" json:"label"`
	Permissions JSONStrings `xorm:"jsonb 'permissions'" json:"permissions"` // ["channel:read", ...]
	IsBuiltin   bool        `xorm:"default(false) 'is_builtin'" json:"is_builtin"`
	CreatedAt   time.Time   `xorm:"created 'created_at'" json:"created_at"`
}

func (*AdminRole) TableName() string { return "admin_roles" }

// AdminUserRole 管理员与角色的绑定
type AdminUserRole struct {
	ID        int64     `xorm:"pk autoincr 'id'" json:"id"`
	AdminID   int64     `xorm:"notnull index 'admin_id'" json:"admin_id"`
	RoleID    int64     `xorm:"notnull 'role_id'" json:"role_id"`
	CreatedAt time.Time `xorm:"created 'created_at'" json:"created_at"`
}

func (*AdminUserRole) TableName() string { return "admin_user_roles" }
