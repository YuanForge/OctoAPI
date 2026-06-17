package billing

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"fanapi/internal/cache"
	"fanapi/internal/db"
	"fanapi/internal/model"

	"github.com/redis/go-redis/v9"
	"xorm.io/xorm"
)

const quotaKeyFmt = "billing:quota:%d"
const quotaLeaseLockNamespace int64 = 20260618
const quotaLeaseMinTopUpCredits int64 = 1_000_000

var quotaLeaseTTL = 30 * time.Minute
var quotaLeaseReclaimGrace = 2 * time.Minute

func quotaKey(userID int64) string {
	return fmt.Sprintf(quotaKeyFmt, userID)
}

func quotaLeaseExpiresAt() time.Time {
	return time.Now().Add(quotaLeaseTTL)
}

func reserveQuota(ctx context.Context, userID, needed int64, reason string) error {
	if needed <= 0 {
		return nil
	}

	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return err
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1, $2)", quotaLeaseLockNamespace, userID); err != nil {
		_ = sess.Rollback()
		return err
	}

	rows, err := sess.QueryString("SELECT balance FROM users WHERE id = $1 FOR UPDATE", userID)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	if len(rows) == 0 {
		_ = sess.Rollback()
		return fmt.Errorf("用户不存在")
	}
	balance, _ := strconv.ParseInt(rows[0]["balance"], 10, 64)
	if balance < needed {
		_ = sess.Rollback()
		return fmt.Errorf("余额不足")
	}

	reserve := quotaLeaseMinTopUpCredits
	if needed > reserve {
		reserve = needed
	}
	if balance < reserve {
		reserve = balance
	}
	if reserve < needed {
		_ = sess.Rollback()
		return fmt.Errorf("余额不足")
	}

	if _, err := sess.Exec("UPDATE users SET balance = balance - $1 WHERE id = $2 AND balance >= $1", reserve, userID); err != nil {
		_ = sess.Rollback()
		return err
	}

	expiresAt := quotaLeaseExpiresAt()
	var lease model.BillingQuotaLease
	found, err := sess.Where("user_id = ? AND status = ? AND expires_at > ?", userID, "active", time.Now()).Desc("id").Get(&lease)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	if found {
		lease.ReservedCredits += reserve
		lease.RemainingCredits += reserve
		lease.ExpiresAt = expiresAt
		if _, err := sess.ID(lease.ID).Cols("reserved_credits", "remaining_credits", "expires_at", "updated_at").Update(&lease); err != nil {
			_ = sess.Rollback()
			return err
		}
	} else {
		lease = model.BillingQuotaLease{
			UserID:           userID,
			ReservedCredits:  reserve,
			RemainingCredits: reserve,
			Status:           "active",
			Reason:           reason,
			ExpiresAt:        expiresAt,
		}
		if _, err := sess.Insert(&lease); err != nil {
			_ = sess.Rollback()
			return err
		}
	}

	if err := sess.Commit(); err != nil {
		return err
	}

	key := quotaKey(userID)
	if err := cache.Client.IncrBy(ctx, key, reserve).Err(); err != nil {
		if releaseErr := releaseReservedQuota(ctx, userID, reserve, "redis_reserve_failed"); releaseErr != nil {
			log.Printf("[quota] release reserve after redis failure failed user=%d reserve=%d err=%v", userID, reserve, releaseErr)
		}
		return err
	}
	_ = cache.Client.Expire(ctx, key, quotaLeaseTTL).Err()
	InvalidateBalanceCache(ctx, userID)
	return nil
}

func releaseReservedQuota(ctx context.Context, userID, credits int64, reason string) error {
	if credits <= 0 {
		return nil
	}
	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return err
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1, $2)", quotaLeaseLockNamespace, userID); err != nil {
		_ = sess.Rollback()
		return err
	}
	rows, err := sess.QueryString(`
WITH target AS (
    SELECT id, LEAST(remaining_credits, $1)::bigint AS release_amount
    FROM billing_quota_leases
    WHERE user_id = $2 AND status = 'active'
    ORDER BY id DESC
    LIMIT 1
    FOR UPDATE
),
updated_lease AS (
    UPDATE billing_quota_leases l
    SET reserved_credits = GREATEST(0, reserved_credits - target.release_amount),
        remaining_credits = remaining_credits - target.release_amount,
        updated_at = NOW()
    FROM target
    WHERE l.id = target.id AND target.release_amount > 0
    RETURNING target.release_amount
),
updated_user AS (
    UPDATE users
    SET balance = balance + (SELECT release_amount FROM updated_lease)
    WHERE id = $2 AND EXISTS (SELECT 1 FROM updated_lease)
    RETURNING balance
)
SELECT release_amount FROM updated_lease`, credits, userID)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	released := int64(0)
	if len(rows) > 0 {
		released, _ = strconv.ParseInt(rows[0]["release_amount"], 10, 64)
	}
	if err := sess.Commit(); err != nil {
		return err
	}
	InvalidateBalanceCache(ctx, userID)
	if reason != "" && released > 0 {
		log.Printf("[quota] released reserve user=%d credits=%d requested=%d reason=%s", userID, released, credits, reason)
	}
	return nil
}

func quotaRemaining(ctx context.Context, userID int64) (int64, error) {
	val, err := cache.Client.Get(ctx, quotaKey(userID)).Int64()
	if err == nil {
		return val, nil
	}
	if err != redis.Nil {
		return 0, err
	}
	return SyncQuotaToRedis(ctx, userID)
}

// SyncQuotaToRedis rebuilds the hot quota key from the active DB lease.
func SyncQuotaToRedis(ctx context.Context, userID int64) (int64, error) {
	var row struct {
		ID               int64     `xorm:"id"`
		RemainingCredits int64     `xorm:"remaining_credits"`
		ExpiresAt        time.Time `xorm:"expires_at"`
	}
	found, err := db.Engine.SQL(`
SELECT id, remaining_credits, expires_at
FROM billing_quota_leases
WHERE user_id = $1 AND status = 'active'
ORDER BY id DESC
LIMIT 1`, userID).Get(&row)
	if err != nil {
		return 0, err
	}
	key := quotaKey(userID)
	if !found || row.RemainingCredits <= 0 {
		_ = cache.Client.Del(ctx, key).Err()
		return 0, nil
	}
	now := time.Now()
	reclaimAt := row.ExpiresAt.Add(quotaLeaseReclaimGrace)
	if !reclaimAt.After(now) {
		if err := reclaimQuotaLease(ctx, row.ID); err != nil {
			return 0, err
		}
		_ = cache.Client.Del(ctx, key).Err()
		return 0, nil
	}
	ttl := time.Until(row.ExpiresAt)
	if ttl <= 0 {
		ttl = time.Until(reclaimAt)
	}
	if err := cache.Client.Set(ctx, key, row.RemainingCredits, ttl).Err(); err != nil {
		return 0, err
	}
	return row.RemainingCredits, nil
}

func ensureQuota(ctx context.Context, userID, credits int64) error {
	if credits <= 0 {
		return nil
	}
	remaining, err := quotaRemaining(ctx, userID)
	if err != nil {
		return err
	}
	if remaining >= credits {
		return nil
	}
	return reserveQuota(ctx, userID, credits-remaining, "charge")
}

func ensureRefundQuotaLease(ctx context.Context, userID int64) error {
	var lease model.BillingQuotaLease
	found, err := db.Engine.Where("user_id = ? AND status = ? AND expires_at > ?", userID, "active", time.Now()).Desc("id").Get(&lease)
	if err != nil {
		return err
	}
	if found {
		return nil
	}

	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return err
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1, $2)", quotaLeaseLockNamespace, userID); err != nil {
		_ = sess.Rollback()
		return err
	}
	found, err = sess.Where("user_id = ? AND status = ? AND expires_at > ?", userID, "active", time.Now()).Desc("id").Get(&lease)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	if !found {
		lease = model.BillingQuotaLease{
			UserID:           userID,
			ReservedCredits:  0,
			RemainingCredits: 0,
			Status:           "active",
			Reason:           "refund",
			ExpiresAt:        quotaLeaseExpiresAt(),
		}
		if _, err := sess.Insert(&lease); err != nil {
			_ = sess.Rollback()
			return err
		}
	}
	return sess.Commit()
}

// ApplyQuotaDelta compensates Redis quota after a pre-applied charge/refund
// transaction fails to persist.
func ApplyQuotaDelta(ctx context.Context, userID, delta int64) error {
	if delta == 0 {
		return nil
	}
	key := quotaKey(userID)
	if delta > 0 {
		exists, err := cache.Client.Exists(ctx, key).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			_, err = SyncQuotaToRedis(ctx, userID)
			return err
		}
		if err := cache.Client.IncrBy(ctx, key, delta).Err(); err != nil {
			return err
		}
		_ = cache.Client.Expire(ctx, key, quotaLeaseTTL).Err()
		return nil
	}

	amount := -delta
	exists, err := cache.Client.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		_, err = SyncQuotaToRedis(ctx, userID)
		return err
	}
	result, err := luaCharge.Run(ctx, cache.Client, []string{key}, amount).Int64()
	if err != nil {
		return err
	}
	if result < 0 {
		return fmt.Errorf("授权额度补偿失败")
	}
	_ = cache.Client.Expire(ctx, key, quotaLeaseTTL).Err()
	return nil
}

// ReleasePreAppliedQuota reverts a Redis-only charge before its billing
// transaction has been persisted. It must not update the DB lease because the
// matching charge was never mirrored into billing_quota_leases.
func ReleasePreAppliedQuota(ctx context.Context, userID, credits int64) error {
	if credits <= 0 {
		return nil
	}
	return ApplyQuotaDelta(ctx, userID, credits)
}

// ApplyQuotaLeaseTx mirrors a persisted billing transaction into the DB lease.
func ApplyQuotaLeaseTx(sess *xorm.Session, userID int64, txType string, generalCredits int64) error {
	if userID <= 0 || generalCredits <= 0 {
		return nil
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1, $2)", quotaLeaseLockNamespace, userID); err != nil {
		return err
	}
	expiresAt := quotaLeaseExpiresAt()
	switch txType {
	case "charge", "settle", "hold":
		rows, err := sess.QueryString(`
UPDATE billing_quota_leases
SET remaining_credits = remaining_credits - $1,
    expires_at = $2,
    updated_at = NOW()
WHERE id = (
    SELECT id FROM billing_quota_leases
    WHERE user_id = $3 AND status = 'active'
    ORDER BY id DESC
    LIMIT 1
)
AND remaining_credits >= $1
RETURNING remaining_credits`, generalCredits, expiresAt, userID)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("授权额度不足或不存在")
		}
	case "refund":
		rows, err := sess.QueryString(`
UPDATE billing_quota_leases
SET remaining_credits = remaining_credits + $1,
    expires_at = $2,
    updated_at = NOW()
WHERE id = (
    SELECT id FROM billing_quota_leases
    WHERE user_id = $3 AND status = 'active'
    ORDER BY id DESC
    LIMIT 1
)
RETURNING remaining_credits`, generalCredits, expiresAt, userID)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			lease := model.BillingQuotaLease{
				UserID:           userID,
				ReservedCredits:  0,
				RemainingCredits: generalCredits,
				Status:           "active",
				Reason:           "refund",
				ExpiresAt:        expiresAt,
			}
			if _, err := sess.Insert(&lease); err != nil {
				return err
			}
		}
	}
	return nil
}

// SpendableBalance returns free DB balance plus active authorized quota.
func SpendableBalance(ctx context.Context, userID int64) (int64, error) {
	var row struct {
		Balance int64 `xorm:"balance"`
	}
	found, err := db.Engine.Context(ctx).SQL(`
SELECT u.balance + COALESCE((
    SELECT SUM(remaining_credits)
    FROM billing_quota_leases
    WHERE user_id = u.id AND status = 'active' AND expires_at > NOW()
), 0) AS balance
FROM users u
WHERE u.id = $1`, userID).Get(&row)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("用户不存在")
	}
	return row.Balance, nil
}

func SpendableBalanceTx(sess *xorm.Session, userID int64) (int64, error) {
	rows, err := sess.QueryString(`
SELECT u.balance + COALESCE((
    SELECT SUM(remaining_credits)
    FROM billing_quota_leases
    WHERE user_id = u.id AND status = 'active' AND expires_at > NOW()
), 0) AS balance
FROM users u
WHERE u.id = $1`, userID)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("用户不存在")
	}
	balance, _ := strconv.ParseInt(rows[0]["balance"], 10, 64)
	return balance, nil
}

func InvalidateBalanceCache(ctx context.Context, userID int64) {
	if userID <= 0 {
		return
	}
	_ = cache.Client.Del(ctx, balanceKey(userID)).Err()
}

func ReclaimExpiredQuotaLeases(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var leases []model.BillingQuotaLease
	if err := db.Engine.Context(ctx).
		Where("status = ? AND expires_at < ?", "active", time.Now().Add(-quotaLeaseReclaimGrace)).
		Asc("expires_at").
		Limit(limit).
		Find(&leases); err != nil {
		return 0, err
	}

	reclaimed := 0
	for _, lease := range leases {
		if err := reclaimQuotaLease(ctx, lease.ID); err != nil {
			return reclaimed, err
		}
		reclaimed++
	}
	return reclaimed, nil
}

func reclaimQuotaLease(ctx context.Context, leaseID int64) error {
	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return err
	}

	var lease model.BillingQuotaLease
	found, err := sess.ID(leaseID).Get(&lease)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	if !found || lease.Status != "active" || lease.ExpiresAt.Add(quotaLeaseReclaimGrace).After(time.Now()) {
		return sess.Commit()
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1, $2)", quotaLeaseLockNamespace, lease.UserID); err != nil {
		_ = sess.Rollback()
		return err
	}
	found, err = sess.ID(leaseID).Get(&lease)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	if !found || lease.Status != "active" || lease.ExpiresAt.Add(quotaLeaseReclaimGrace).After(time.Now()) {
		return sess.Commit()
	}

	remaining := lease.RemainingCredits
	if remaining > 0 {
		if _, err := sess.Exec("UPDATE users SET balance = balance + $1 WHERE id = $2", remaining, lease.UserID); err != nil {
			_ = sess.Rollback()
			return err
		}
	}
	lease.RemainingCredits = 0
	lease.Status = "expired"
	if _, err := sess.ID(lease.ID).Cols("remaining_credits", "status", "updated_at").Update(&lease); err != nil {
		_ = sess.Rollback()
		return err
	}
	if err := sess.Commit(); err != nil {
		return err
	}
	_ = cache.Client.Del(ctx, quotaKey(lease.UserID)).Err()
	InvalidateBalanceCache(ctx, lease.UserID)
	return nil
}
