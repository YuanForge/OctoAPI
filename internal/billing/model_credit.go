package billing

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"

	"fanapi/internal/cache"
	"fanapi/internal/db"
)

const modelCreditKeyFmt = "user:model_credit:%d:%s"
const modelCreditLockNamespace int64 = 20260619

func modelCreditKey(userID int64, modelName string) string {
	return fmt.Sprintf(modelCreditKeyFmt, userID, modelName)
}

func modelCreditLockID(userID int64, modelName string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strconv.FormatInt(userID, 10)))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(modelName))
	return int64(h.Sum64())
}

// SyncModelCreditToRedis 将 DB 中的模型专属积分同步到 Redis。
func SyncModelCreditToRedis(ctx context.Context, userID int64, modelName string) (int64, error) {
	var result struct{ Credits int64 }
	found, err := db.Engine.SQL(
		"SELECT COALESCE(SUM(credits), 0) AS credits FROM user_model_credits WHERE user_id = ? AND model_name = ?",
		userID, modelName,
	).Get(&result)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	key := modelCreditKey(userID, modelName)
	cache.Client.Set(ctx, key, result.Credits, 0)
	return result.Credits, nil
}

// GetModelCredit 返回用户某模型的专属积分余额，缓存未命中时从 DB 同步。
func GetModelCredit(ctx context.Context, userID int64, modelName string) (int64, error) {
	key := modelCreditKey(userID, modelName)
	val, err := cache.Client.Get(ctx, key).Int64()
	if err == nil {
		return val, nil
	}
	return SyncModelCreditToRedis(ctx, userID, modelName)
}

// ChargeModelCredit 优先扣减用户的模型专属积分，返回实际扣减量。
// 若模型积分不足以覆盖全部 credits，则全额扣减模型积分余额并返回实际扣减量，
// 剩余部分由调用方继续从通用余额扣除。
// 若无模型积分（余额为 0 或键不存在），返回 0。
func ChargeModelCredit(ctx context.Context, userID int64, modelName string, credits int64) (charged int64, err error) {
	if credits <= 0 {
		return 0, nil
	}
	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return 0, err
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1)", modelCreditLockID(userID, modelName)); err != nil {
		_ = sess.Rollback()
		return 0, err
	}
	rows, err := sess.QueryString(`
SELECT id, credits
FROM user_model_credits
WHERE user_id = $1 AND model_name = $2 AND credits > 0
ORDER BY id
FOR UPDATE`, userID, modelName)
	if err != nil {
		_ = sess.Rollback()
		return 0, err
	}
	remaining := credits
	for _, row := range rows {
		if remaining <= 0 {
			break
		}
		id, _ := strconv.ParseInt(row["id"], 10, 64)
		available, _ := strconv.ParseInt(row["credits"], 10, 64)
		if id <= 0 || available <= 0 {
			continue
		}
		toCharge := available
		if toCharge > remaining {
			toCharge = remaining
		}
		if _, err := sess.Exec(
			"UPDATE user_model_credits SET credits = credits - $1, updated_at = NOW() WHERE id = $2 AND credits >= $1",
			toCharge, id,
		); err != nil {
			_ = sess.Rollback()
			return 0, err
		}
		charged += toCharge
		remaining -= toCharge
	}
	if err := sess.Commit(); err != nil {
		return 0, err
	}
	_, _ = SyncModelCreditToRedis(ctx, userID, modelName)
	return charged, nil
}

// RefundModelCredit 退回模型专属积分（用于退款场景）。
func RefundModelCredit(ctx context.Context, userID int64, modelName string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return err
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1)", modelCreditLockID(userID, modelName)); err != nil {
		_ = sess.Rollback()
		return err
	}
	rows, err := sess.QueryString(`
SELECT id
FROM user_model_credits
WHERE user_id = $1 AND model_name = $2
ORDER BY id
LIMIT 1
FOR UPDATE`, userID, modelName)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	if len(rows) == 0 {
		if _, err := sess.Exec(
			"INSERT INTO user_model_credits (user_id, model_name, credits, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())",
			userID, modelName, credits,
		); err != nil {
			_ = sess.Rollback()
			return err
		}
	} else {
		id, _ := strconv.ParseInt(rows[0]["id"], 10, 64)
		if _, err := sess.Exec(
			"UPDATE user_model_credits SET credits = credits + $1, updated_at = NOW() WHERE id = $2",
			credits, id,
		); err != nil {
			_ = sess.Rollback()
			return err
		}
	}
	if err := sess.Commit(); err != nil {
		return err
	}
	_, err = SyncModelCreditToRedis(ctx, userID, modelName)
	return err
}

// AddModelCredit 增加模型专属积分（管理员赠送）。DB 是权威数据，Redis 随后同步。
func AddModelCredit(ctx context.Context, userID int64, modelName string, credits int64) error {
	if credits <= 0 {
		return fmt.Errorf("积分数量必须大于 0")
	}
	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return err
	}
	if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1)", modelCreditLockID(userID, modelName)); err != nil {
		_ = sess.Rollback()
		return err
	}
	rows, err := sess.QueryString(`
SELECT id
FROM user_model_credits
WHERE user_id = $1 AND model_name = $2
ORDER BY id
LIMIT 1
FOR UPDATE`, userID, modelName)
	if err != nil {
		_ = sess.Rollback()
		return err
	}
	if len(rows) == 0 {
		if _, err = sess.Exec(
			"INSERT INTO user_model_credits (user_id, model_name, credits, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())",
			userID, modelName, credits,
		); err != nil {
			_ = sess.Rollback()
			return err
		}
	} else {
		id, _ := strconv.ParseInt(rows[0]["id"], 10, 64)
		if _, err = sess.Exec(
			"UPDATE user_model_credits SET credits = credits + $1, updated_at = NOW() WHERE id = $2",
			credits, id,
		); err != nil {
			_ = sess.Rollback()
			return err
		}
	}
	if err := sess.Commit(); err != nil {
		return err
	}
	_, err = SyncModelCreditToRedis(ctx, userID, modelName)
	return err
}
