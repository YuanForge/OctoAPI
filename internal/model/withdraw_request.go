package model

import "time"

// WithdrawRequest 积分提现申请。
type WithdrawRequest struct {
	ID          int64  `xorm:"pk autoincr 'id'" json:"id"`
	UserID      int64  `xorm:"notnull 'user_id'" json:"user_id"`
	Amount      int64  `xorm:"notnull 'amount'" json:"amount"`                                  // 微单位积分
	Status      string `xorm:"notnull default('pending') 'status'" json:"status"`               // pending/approved/rejected
	ReviewStage string `xorm:"notnull default('cs_review') 'review_stage'" json:"review_stage"` // cs_review/finance_review/completed
	PaymentType string `xorm:"notnull default('') 'payment_type'" json:"payment_type"`          // wechat/alipay
	PaymentQR   string `xorm:"notnull default('') 'payment_qr'" json:"payment_qr"`              // 收款码快照
	ProofURL    string `xorm:"notnull default('') 'proof_url'" json:"proof_url,omitempty"`      // 财务打款凭证图片
	ProofNote   string `xorm:"notnull default('') 'proof_note'" json:"proof_note,omitempty"`    // 财务凭证备注
	AdminRemark string `xorm:"notnull default('') 'admin_remark'" json:"admin_remark,omitempty"`
	// 客服初审
	CsReviewerID int64      `xorm:"notnull default(0) 'cs_reviewer_id'" json:"cs_reviewer_id,omitempty"`
	CsReviewedAt *time.Time `xorm:"'cs_reviewed_at'" json:"cs_reviewed_at,omitempty"`
	// 财务复审
	FinanceReviewerID int64      `xorm:"notnull default(0) 'finance_reviewer_id'" json:"finance_reviewer_id,omitempty"`
	FinanceReviewedAt *time.Time `xorm:"'finance_reviewed_at'" json:"finance_reviewed_at,omitempty"`
	CreatedAt         time.Time  `xorm:"created 'created_at'" json:"created_at"`
	UpdatedAt         time.Time  `xorm:"updated 'updated_at'" json:"updated_at"`

	// 关联字段（查询时 JOIN 填充，不入库）
	Username string `xorm:"username" json:"username,omitempty"`
}

func (*WithdrawRequest) TableName() string { return "withdraw_requests" }
