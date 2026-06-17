package model

import "time"

// BalanceSyncJob records a persisted DB->Redis balance delta that must be
// retried after the PostgreSQL transaction has already committed.
type BalanceSyncJob struct {
	ID        int64     `xorm:"pk autoincr 'id'" json:"id"`
	UserID    int64     `xorm:"notnull index 'user_id'" json:"user_id"`
	Delta     int64     `xorm:"notnull 'delta'" json:"delta"`
	Reason    string    `xorm:"'reason'" json:"reason"`
	CorrID    string    `xorm:"'corr_id'" json:"corr_id"`
	Status    string    `xorm:"notnull default('pending') index 'status'" json:"status"`
	Attempts  int64     `xorm:"notnull default(0) 'attempts'" json:"attempts"`
	LastError string    `xorm:"text 'last_error'" json:"last_error"`
	CreatedAt time.Time `xorm:"created 'created_at'" json:"created_at"`
	UpdatedAt time.Time `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (*BalanceSyncJob) TableName() string { return "balance_sync_jobs" }
