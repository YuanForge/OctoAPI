package billing

import (
	"context"
	"fmt"

	"fanapi/internal/cache"
	"fanapi/internal/db"

	"github.com/redis/go-redis/v9"
)

const modelCreditKeyFmt = "user:model_credit:%d:%s"

func modelCreditKey(userID int64, modelName string) string {
	return fmt.Sprintf(modelCreditKeyFmt, userID, modelName)
}

// SyncModelCreditToRedis 将 DB 中的模型专属积分同步到 Redis。
func SyncModelCreditToRedis(ctx context.Context, userID int64, modelName string) (int64, error) {
	var result struct{ Credits int64 }
	found, err := db.Engine.SQL(
		"SELECT credits FROM user_model_credits WHERE user_id = ? AND model_name = ?",
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

// luaChargeModel 原子地扣减模型积分，余额不足时返回 -1，键不存在时返回 -2。
var luaChargeModel = redis.NewScript(`
local bal = tonumber(redis.call("GET", KEYS[1]))
if not bal then return -2 end
if bal < tonumber(ARGV[1]) then return -1 end
return redis.call("DECRBY", KEYS[1], ARGV[1])
`)

// ChargeModelCredit 优先扣减用户的模型专属积分，返回实际扣减量。
// 若模型积分不足以覆盖全部 credits，则全额扣减模型积分余额并返回实际扣减量，
// 剩余部分由调用方继续从通用余额扣除。
// 若无模型积分（余额为 0 或键不存在），返回 0。
func ChargeModelCredit(ctx context.Context, userID int64, modelName string, credits int64) (charged int64, err error) {
	if credits <= 0 {
		return 0, nil
	}
	key := modelCreditKey(userID, modelName)

	// 确保 Redis 键存在
	existing, getErr := cache.Client.Get(ctx, key).Int64()
	if getErr != nil {
		existing, err = SyncModelCreditToRedis(ctx, userID, modelName)
		if err != nil {
			return 0, err
		}
	}
	if existing <= 0 {
		return 0, nil
	}

	// 实际能扣的量：min(existing, credits)
	toCharge := credits
	if existing < credits {
		toCharge = existing
	}

	result, err := luaChargeModel.Run(ctx, cache.Client, []string{key}, toCharge).Int64()
	if err != nil {
		return 0, err
	}
	if result == -2 {
		// 键消失了（极罕见），重试一次
		existing, _ = SyncModelCreditToRedis(ctx, userID, modelName)
		if existing <= 0 {
			return 0, nil
		}
		toCharge = credits
		if existing < credits {
			toCharge = existing
		}
		result, err = luaChargeModel.Run(ctx, cache.Client, []string{key}, toCharge).Int64()
		if err != nil || result < 0 {
			return 0, err
		}
	}
	if result == -1 {
		// 并发下被其他请求耗尽，退化到 0
		return 0, nil
	}
	// 同步 DB（非关键路径，异步执行）
	go func() {
		db.Engine.Exec(
			"UPDATE user_model_credits SET credits = GREATEST(0, credits - $1), updated_at = NOW() WHERE user_id = $2 AND model_name = $3",
			toCharge, userID, modelName,
		)
	}()
	return toCharge, nil
}

// RefundModelCredit 退回模型专属积分（用于退款场景）。
func RefundModelCredit(ctx context.Context, userID int64, modelName string, credits int64) error {
	if credits <= 0 {
		return nil
	}
	key := modelCreditKey(userID, modelName)
	// 确保键存在再 IncrBy
	if _, err := cache.Client.Get(ctx, key).Int64(); err != nil {
		if _, syncErr := SyncModelCreditToRedis(ctx, userID, modelName); syncErr != nil {
			return syncErr
		}
	}
	if err := cache.Client.IncrBy(ctx, key, credits).Err(); err != nil {
		return err
	}
	go func() {
		db.Engine.Exec(
			"UPDATE user_model_credits SET credits = credits + $1, updated_at = NOW() WHERE user_id = $2 AND model_name = $3",
			credits, userID, modelName,
		)
	}()
	return nil
}

// AddModelCredit 增加模型专属积分（管理员赠送）。DB 是权威数据，Redis 随后同步。
func AddModelCredit(ctx context.Context, userID int64, modelName string, credits int64) error {
	if credits <= 0 {
		return fmt.Errorf("积分数量必须大于 0")
	}
	// upsert
	_, err := db.Engine.Exec(`
		INSERT INTO user_model_credits (user_id, model_name, credits, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (user_id, model_name)
		DO UPDATE SET credits = user_model_credits.credits + $3, updated_at = NOW()
	`, userID, modelName, credits)
	if err != nil {
		return err
	}
	// 同步 Redis
	_, err = SyncModelCreditToRedis(ctx, userID, modelName)
	return err
}
