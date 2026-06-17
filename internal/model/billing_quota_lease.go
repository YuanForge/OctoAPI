package model

import "time"

// BillingQuotaLease reserves DB balance into a Redis-consumable quota bucket.
// users.balance remains the free balance; active lease remaining credits are
// already authorized and can be consumed on the hot path.
type BillingQuotaLease struct {
	ID               int64     `xorm:"pk autoincr 'id'" json:"id"`
	UserID           int64     `xorm:"notnull index 'user_id'" json:"user_id"`
	ReservedCredits  int64     `xorm:"notnull default(0) 'reserved_credits'" json:"reserved_credits"`
	RemainingCredits int64     `xorm:"notnull default(0) 'remaining_credits'" json:"remaining_credits"`
	Status           string    `xorm:"notnull default('active') index 'status'" json:"status"`
	Reason           string    `xorm:"notnull default('') 'reason'" json:"reason"`
	ExpiresAt        time.Time `xorm:"notnull index 'expires_at'" json:"expires_at"`
	CreatedAt        time.Time `xorm:"created 'created_at'" json:"created_at"`
	UpdatedAt        time.Time `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (*BillingQuotaLease) TableName() string { return "billing_quota_leases" }
