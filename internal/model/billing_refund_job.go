package model

import "time"

// BillingRefundJob is a durable retry record for a refund that could not be
// committed immediately. It prevents failed requests from losing their refund
// when the first billing transaction attempt hits a transient error.
type BillingRefundJob struct {
	ID                  int64     `xorm:"pk autoincr 'id'" json:"id"`
	UserID              int64     `xorm:"notnull index 'user_id'" json:"user_id"`
	ChannelID           int64     `xorm:"'channel_id'" json:"channel_id"`
	APIKeyID            int64     `xorm:"'api_key_id'" json:"api_key_id"`
	PoolKeyID           int64     `xorm:"notnull default(0) 'pool_key_id'" json:"pool_key_id"`
	CorrID              string    `xorm:"'corr_id'" json:"corr_id"`
	Credits             int64     `xorm:"notnull 'credits'" json:"credits"`
	Cost                int64     `xorm:"notnull default(0) 'cost'" json:"cost"`
	ModelCreditRefunded int64     `xorm:"notnull default(0) 'model_credit_refunded'" json:"model_credit_refunded"`
	Metrics             JSON      `xorm:"jsonb 'metrics'" json:"metrics"`
	DedupeKey           string    `xorm:"notnull default('') 'dedupe_key'" json:"dedupe_key"`
	Status              string    `xorm:"notnull default('pending') index 'status'" json:"status"`
	Attempts            int64     `xorm:"notnull default(0) 'attempts'" json:"attempts"`
	NextRunAt           time.Time `xorm:"notnull index 'next_run_at'" json:"next_run_at"`
	LastError           string    `xorm:"text 'last_error'" json:"last_error"`
	CreatedAt           time.Time `xorm:"created 'created_at'" json:"created_at"`
	UpdatedAt           time.Time `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (*BillingRefundJob) TableName() string { return "billing_refund_jobs" }
