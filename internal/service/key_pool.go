package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fanapi/internal/cache"
	"fanapi/internal/db"
	"fanapi/internal/model"
)

const (
	// exhaustedTTL 是三方 Key 被标记为耗尽后的自动恢复时间。
	// 超过此时间后该 Key 会重新参与分配轮转。
	exhaustedTTL = time.Hour

	assignKeyFmt    = "pool:assign:%d:%d" // Redis 键格式：pool:assign:{pool_id}:{entity_id}
	exhaustedKeyFmt = "pool:exhausted:%d" // Redis 键格式：pool:exhausted:{pool_key_id}
)

var (
	ErrPoolNoAvailableKeys  = errors.New("pool has no available keys")
	ErrPoolAllKeysExhausted = errors.New("pool keys exhausted")
	ErrPoolAllKeysTried     = errors.New("pool keys tried")
)

type poolKeyStateError struct {
	kind error
	msg  string
}

func (e poolKeyStateError) Error() string { return e.msg }

func (e poolKeyStateError) Unwrap() error { return e.kind }

func newPoolKeyStateError(kind error, format string, args ...interface{}) error {
	return poolKeyStateError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

func clearPoolAssignments(ctx context.Context, poolID int64) {
	if poolID <= 0 {
		return
	}
	pattern := fmt.Sprintf(assignKeyFmt, poolID, 0)
	pattern = strings.TrimSuffix(pattern, "0") + "*"
	iter := cache.Client.Scan(ctx, 0, pattern, 200).Iterator()
	for iter.Next(ctx) {
		cache.Client.Del(ctx, iter.Val())
	}
}

func clearPoolKeyExhaustedState(ctx context.Context, keyIDs ...int64) {
	if len(keyIDs) == 0 {
		return
	}
	redisKeys := make([]string, 0, len(keyIDs))
	seen := make(map[int64]struct{}, len(keyIDs))
	for _, keyID := range keyIDs {
		if keyID <= 0 {
			continue
		}
		if _, ok := seen[keyID]; ok {
			continue
		}
		seen[keyID] = struct{}{}
		redisKeys = append(redisKeys, fmt.Sprintf(exhaustedKeyFmt, keyID))
	}
	if len(redisKeys) > 0 {
		cache.Client.Del(ctx, redisKeys...)
	}
}

func resetPoolRuntimeState(ctx context.Context, poolID int64) error {
	if poolID <= 0 {
		return nil
	}
	var keys []model.PoolKey
	if err := db.Engine.Where("pool_id = ?", poolID).Cols("id").Find(&keys); err != nil {
		return err
	}
	keyIDs := make([]int64, 0, len(keys))
	for i := range keys {
		keyIDs = append(keyIDs, keys[i].ID)
	}
	clearPoolAssignments(ctx, poolID)
	clearPoolKeyExhaustedState(ctx, keyIDs...)
	return nil
}

func ResetPoolRuntimeState(ctx context.Context, poolID int64) error {
	return resetPoolRuntimeState(ctx, poolID)
}

func ResetPoolKeyRuntimeState(ctx context.Context, poolID int64, keyID int64) {
	clearPoolAssignments(ctx, poolID)
	clearPoolKeyExhaustedState(ctx, keyID)
}

func ResetChannelPoolRuntimeState(ctx context.Context, channelID int64) error {
	if channelID <= 0 {
		return nil
	}

	var poolIDs []int64
	if err := db.Engine.Table("key_pools").
		Where("channel_id = ?", channelID).
		Cols("id").
		Find(&poolIDs); err != nil {
		return err
	}

	for _, poolID := range poolIDs {
		if err := resetPoolRuntimeState(ctx, poolID); err != nil {
			return err
		}
	}
	return nil
}

// GetOrAssignPoolKey 返回分配给 entityID 的三方 PoolKey（Sticky Assignment）。
//
// entityID 取值规则：
//   - LLM 场景：优先使用 api_key_id，若无则使用 user_id
//   - 异步任务场景：使用 user_id
//
// 分配策略：
//  1. 若 entityID 已有分配且该 Key 未耗尽 → 直接复用（保证上下文延续 / 缓存命中）
//  2. 若已分配但 Key 已耗尽，或尚未分配 → 按 priority ASC, id ASC 选择第一个可用 Key 并绑定
func GetOrAssignPoolKey(ctx context.Context, poolID, entityID int64) (*model.PoolKey, error) {
	// 检查号池本身是否启用
	pool := &model.KeyPool{}
	found, err := db.Engine.ID(poolID).Get(pool)
	if err != nil {
		return nil, fmt.Errorf("号池 %d: 读取失败: %w", poolID, err)
	}
	if !found {
		return nil, fmt.Errorf("号池 %d 不存在", poolID)
	}
	if !pool.IsActive {
		return nil, fmt.Errorf("号池 %d 已停用", poolID)
	}

	assignKey := fmt.Sprintf(assignKeyFmt, poolID, entityID)

	// 1. 查询当前分配
	assignedIDStr, err := cache.Client.Get(ctx, assignKey).Result()
	if err == nil {
		var assignedID int64
		fmt.Sscanf(assignedIDStr, "%d", &assignedID)

		exhaustedKey := fmt.Sprintf(exhaustedKeyFmt, assignedID)
		exhausted, _ := cache.Client.Exists(ctx, exhaustedKey).Result()
		if exhausted == 0 {
			// 已分配且未耗尽 → 直接复用
			key := &model.PoolKey{}
			found, dbErr := db.Engine.Where("id = ? AND is_active = true", assignedID).Get(key)
			if dbErr == nil && found {
				return key, nil
			}
		}
	}

	// 2. 尚未分配 or 当前分配已耗尽 → 重新轮转
	key, err := rotatePoolKey(ctx, poolID, entityID, assignKey, 0)
	if err == nil {
		return key, nil
	}
	return nil, err
}

// MarkExhaustedAndRotate 将 poolKeyID 标记为耗尽（带 TTL），同时为 entityID 轮转到下一可用 Key。
// 用于检测到上游 429 / 配额不足时主动触发轮转。
func MarkExhaustedAndRotate(ctx context.Context, poolID, poolKeyID, entityID int64) (*model.PoolKey, error) {
	exhaustedKey := fmt.Sprintf(exhaustedKeyFmt, poolKeyID)
	cache.Client.Set(ctx, exhaustedKey, 1, exhaustedTTL)

	assignKey := fmt.Sprintf(assignKeyFmt, poolID, entityID)
	return rotatePoolKey(ctx, poolID, entityID, assignKey, poolKeyID)
}

// RotatePoolKeySkipping 在当前请求内轮转到下一个可用 Key，不会把已试 Key 标记为耗尽。
// 用于 521/504 这类上游网关错误：本次请求先试同池其它 Key，全部失败后再交给渠道级重试。
func RotatePoolKeySkipping(ctx context.Context, poolID, entityID int64, skipKeyIDs []int64) (*model.PoolKey, error) {
	skip := make(map[int64]bool, len(skipKeyIDs))
	for _, id := range skipKeyIDs {
		if id > 0 {
			skip[id] = true
		}
	}

	var keys []model.PoolKey
	if err := db.Engine.Where("pool_id = ? AND is_active = true", poolID).
		OrderBy("priority ASC, id ASC").Find(&keys); err != nil {
		return nil, fmt.Errorf("key pool %d: db error: %w", poolID, err)
	}
	if len(keys) == 0 {
		return nil, newPoolKeyStateError(ErrPoolNoAvailableKeys, "号池 %d 暂无可用 Key", poolID)
	}

	assignKey := fmt.Sprintf(assignKeyFmt, poolID, entityID)
	for i := range keys {
		k := &keys[i]
		if skip[k.ID] {
			continue
		}
		exhaustedKey := fmt.Sprintf(exhaustedKeyFmt, k.ID)
		exists, _ := cache.Client.Exists(ctx, exhaustedKey).Result()
		if exists == 0 {
			cache.Client.Set(ctx, assignKey, fmt.Sprintf("%d", k.ID), 0)
			return k, nil
		}
	}
	return nil, newPoolKeyStateError(ErrPoolAllKeysTried, "号池 %d 的可用 Key 均已尝试", poolID)
}

// rotatePoolKey 从池中选择第一个未耗尽的可用 Key（跳过 skipKeyID），并写入分配记录。
func rotatePoolKey(ctx context.Context, poolID, _ int64, assignKey string, skipKeyID int64) (*model.PoolKey, error) {
	var keys []model.PoolKey
	if err := db.Engine.Where("pool_id = ? AND is_active = true", poolID).
		OrderBy("priority ASC, id ASC").Find(&keys); err != nil {
		return nil, fmt.Errorf("key pool %d: db error: %w", poolID, err)
	}
	if len(keys) == 0 {
		return nil, newPoolKeyStateError(ErrPoolNoAvailableKeys, "号池 %d 暂无可用 Key", poolID)
	}

	for i := range keys {
		k := &keys[i]
		if k.ID == skipKeyID {
			continue
		}
		exhaustedKey := fmt.Sprintf(exhaustedKeyFmt, k.ID)
		exists, _ := cache.Client.Exists(ctx, exhaustedKey).Result()
		if exists == 0 {
			// 绑定分配（无过期时间 = Sticky，永久持有直到主动轮转）
			cache.Client.Set(ctx, assignKey, fmt.Sprintf("%d", k.ID), 0)
			return k, nil
		}
	}
	return nil, newPoolKeyStateError(ErrPoolAllKeysExhausted, "号池 %d 的所有 Key 已耗尽", poolID)
}

// ---- 管理接口 ----

// ListKeyPools 返回号池列表（管理端，不过滤 is_active，软删除通过前端展示状态区分）。
// channelID > 0 时按渠道过滤，否则返回全部号池。
func ListKeyPools(ctx context.Context, channelID int64) ([]model.KeyPool, error) {
	pools := make([]model.KeyPool, 0)
	var err error
	if channelID > 0 {
		err = db.Engine.Where("channel_id = ?", channelID).OrderBy("id DESC").Find(&pools)
	} else {
		err = db.Engine.OrderBy("id DESC").Find(&pools)
	}
	return pools, err
}

// CreateKeyPool 创建一个新号池。
func CreateKeyPool(ctx context.Context, pool *model.KeyPool) error {
	_, err := db.Engine.Insert(pool)
	if err == nil {
		clearPoolAssignments(ctx, pool.ID)
	}
	return err
}

// ToggleKeyPool 切换号池启用/停用状态。
func ToggleKeyPool(ctx context.Context, poolID int64) error {
	pool := &model.KeyPool{}
	found, err := db.Engine.ID(poolID).Get(pool)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("号池 %d 不存在", poolID)
	}
	_, err = db.Engine.ID(poolID).Cols("is_active").Update(&model.KeyPool{IsActive: !pool.IsActive})
	if err == nil {
		_ = resetPoolRuntimeState(ctx, poolID)
	}
	return err
}

// DeleteKeyPool 删除号池及其所有 Key。
func DeleteKeyPool(ctx context.Context, poolID int64) error {
	_ = resetPoolRuntimeState(ctx, poolID)
	if _, err := db.Engine.Where("pool_id = ?", poolID).Delete(&model.PoolKey{}); err != nil {
		return err
	}
	_, err := db.Engine.ID(poolID).Delete(&model.KeyPool{})
	return err
}

// ListPoolKeys 返回号池内所有 Key（含排序，供管理界面展示）。
func ListPoolKeys(ctx context.Context, poolID int64) ([]model.PoolKey, error) {
	keys := make([]model.PoolKey, 0)
	err := db.Engine.Where("pool_id = ?", poolID).OrderBy("priority ASC, id DESC").Find(&keys)
	return keys, err
}

// AddPoolKey 向号池添加一个三方 Key。
func AddPoolKey(ctx context.Context, key *model.PoolKey) error {
	_, err := db.Engine.Insert(key)
	if err == nil {
		clearPoolAssignments(ctx, key.PoolID)
		clearPoolKeyExhaustedState(ctx, key.ID)
	}
	return err
}

// RemovePoolKey 删除号池中的一个 Key。
func RemovePoolKey(ctx context.Context, keyID int64) error {
	var key model.PoolKey
	found, err := db.Engine.ID(keyID).Cols("id", "pool_id").Get(&key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	_, err = db.Engine.ID(keyID).Delete(&model.PoolKey{})
	if err == nil {
		clearPoolAssignments(ctx, key.PoolID)
		clearPoolKeyExhaustedState(ctx, keyID)
	}
	return err
}
