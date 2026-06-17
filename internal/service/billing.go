package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"fanapi/internal/billing"
	"fanapi/internal/db"
	"fanapi/internal/model"
)

// WriteTx 写入一条计费流水，并在需要时同步余额。
// poolKeyID 为本次请求使用的号池 Key ID（0 表示未使用号池）。
// cost 为支付给上游的进价成本（若暂不记录可传 0）。
// modelCreditCharged 为本次从专属模型积分中扣除的数量（0 表示全部来自通用余额）。
// DB 仅更新通用余额（users.balance），模型积分存储在 user_model_credits 表中。
//
// 余额策略：
//   - "hold"/"settle"/"charge"/"refund"：调用方必须先成功操作 Redis 授权额度；
//     WriteTx 在同一 DB 事务中写流水并同步扣减/返还 billing_quota_leases.remaining_credits。
//   - "recharge"/"adjust"：先在 DB 原子更新 users.balance，再增量同步余额缓存。
func WriteTx(ctx context.Context, userID, channelID, apiKeyID, poolKeyID int64, corrID, txType string, credits, cost, modelCreditCharged int64, metrics model.JSON) error {
	if metrics == nil {
		metrics = model.JSON{}
	}
	taskID := metricInt64(metrics, "task_id")
	llmLogID := metricInt64(metrics, "llm_log_id")
	skipRedisSync := metricBool(metrics, "skip_redis_sync")
	refundRetryJob := txType == "refund" && metricBool(metrics, "refund_retry_job")
	refundDedupeKey := ""
	if txType == "refund" {
		refundDedupeKey = ensureRefundDedupeKey(userID, corrID, credits, cost, modelCreditCharged, metrics)
	}
	tx := &model.BillingTransaction{
		UserID:             userID,
		ChannelID:          channelID,
		APIKeyID:           apiKeyID,
		PoolKeyID:          poolKeyID,
		CorrID:             corrID,
		Type:               txType,
		Credits:            credits,
		ModelCreditCharged: modelCreditCharged,
		Cost:               cost,
		Metrics:            metrics,
		LLMLogID:           llmLogID,
		TaskID:             taskID,
	}

	// DB 仅反映通用余额变化；专属模型积分变化记录在 user_model_credits 表。
	generalCredits := credits - modelCreditCharged
	if generalCredits < 0 {
		generalCredits = 0
	}

	var delta int64
	redisPreApplied := false
	switch txType {
	case "charge", "settle", "hold":
		delta = -generalCredits
		redisPreApplied = true
	case "refund", "recharge":
		delta = generalCredits
		redisPreApplied = txType == "refund"
	case "adjust":
		delta = credits
	}
	var balanceSyncJob *model.BalanceSyncJob

	compensateRedis := func(reason string) {
		if skipRedisSync || !redisPreApplied || delta == 0 {
			return
		}
		if err := billing.ApplyQuotaDelta(context.Background(), userID, -delta); err != nil {
			log.Printf("[billing] quota compensation failed user=%d type=%s corr_id=%s delta=%d reason=%s err=%v",
				userID, txType, corrID, -delta, reason, err)
		}
	}
	handlePreAppliedFailure := func(reason string, cause error) error {
		if txType == "refund" && redisPreApplied && !refundRetryJob && !skipRedisSync && credits > 0 {
			if err := enqueueRefundJob(context.Background(), userID, channelID, apiKeyID, poolKeyID, corrID, credits, cost, modelCreditCharged, metrics, refundDedupeKey, cause); err == nil {
				log.Printf("[billing] refund tx deferred user=%d corr_id=%s credits=%d reason=%s err=%v",
					userID, corrID, credits, reason, cause)
				if n, processErr := ProcessBillingRefundJobs(context.Background(), 10); processErr != nil {
					log.Printf("[billing] immediate refund job retry failed after %d jobs user=%d corr_id=%s err=%v",
						n, userID, corrID, processErr)
				}
				return nil
			} else {
				log.Printf("[billing] enqueue refund job failed user=%d corr_id=%s credits=%d reason=%s err=%v queue_err=%v",
					userID, corrID, credits, reason, cause, err)
			}
		}
		compensateRedis(reason)
		return cause
	}

	// 将余额更新与流水插入包在同一事务，避免余额已改但流水缺失
	sess := db.Engine.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		return handlePreAppliedFailure("begin", err)
	}
	if !redisPreApplied && !skipRedisSync && delta != 0 {
		if _, err := sess.Exec("SELECT pg_advisory_xact_lock($1, $2)", int64(20260617), userID); err != nil {
			if rbErr := sess.Rollback(); rbErr != nil {
				log.Printf("[billing] rollback failed: %v", rbErr)
			}
			return err
		}
	}

	if redisPreApplied {
		if txType == "refund" && refundRetryJob && refundDedupeKey != "" {
			if exists, err := refundTxExists(sess, refundDedupeKey); err != nil {
				if rbErr := sess.Rollback(); rbErr != nil {
					log.Printf("[billing] rollback failed: %v", rbErr)
				}
				return handlePreAppliedFailure("dedupe_lookup", err)
			} else if exists {
				if rbErr := sess.Rollback(); rbErr != nil {
					log.Printf("[billing] rollback failed: %v", rbErr)
				}
				billing.InvalidateBalanceCache(context.Background(), userID)
				return nil
			}
		}
		if err := billing.ApplyQuotaLeaseTx(sess, userID, txType, generalCredits); err != nil {
			if rbErr := sess.Rollback(); rbErr != nil {
				log.Printf("[billing] rollback failed: %v", rbErr)
			}
			return handlePreAppliedFailure("quota_lease_update", err)
		}
		if bal, err := billing.SpendableBalanceTx(sess, userID); err == nil {
			tx.BalanceAfter = bal
		}
	} else if delta != 0 {
		rows, err := sess.QueryString(
			"UPDATE users SET balance = balance + $1 WHERE id = $2 AND balance + $1 >= 0 RETURNING balance",
			delta, userID,
		)
		if err != nil {
			if rbErr := sess.Rollback(); rbErr != nil {
				log.Printf("[billing] rollback failed: %v", rbErr)
			}
			compensateRedis("update_error")
			return err
		}
		if len(rows) == 0 {
			if rbErr := sess.Rollback(); rbErr != nil {
				log.Printf("[billing] rollback failed: %v", rbErr)
			}
			compensateRedis("update_no_rows")
			return fmt.Errorf("用户余额不足或用户不存在")
		}
		if bal, err := billing.SpendableBalanceTx(sess, userID); err == nil {
			tx.BalanceAfter = bal
		} else if balStr, ok := rows[0]["balance"]; ok {
			tx.BalanceAfter, _ = strconv.ParseInt(balStr, 10, 64)
		}
	}

	if _, err := sess.Insert(tx); err != nil {
		if rbErr := sess.Rollback(); rbErr != nil {
			log.Printf("[billing] rollback failed: %v", rbErr)
		}
		return handlePreAppliedFailure("insert_tx", err)
	}

	if !skipRedisSync && !redisPreApplied && delta != 0 {
		balanceSyncJob = &model.BalanceSyncJob{
			UserID: userID,
			Delta:  delta,
			Reason: txType,
			CorrID: corrID,
			Status: "pending",
		}
		if _, err := sess.Insert(balanceSyncJob); err != nil {
			if rbErr := sess.Rollback(); rbErr != nil {
				log.Printf("[billing] rollback failed: %v", rbErr)
			}
			return err
		}
	}

	if err := sess.Commit(); err != nil {
		return handlePreAppliedFailure("commit", err)
	}

	billing.InvalidateBalanceCache(context.Background(), userID)

	// 未预先操作 Redis 的 DB 余额变更（如充值、手动调账）在事务成功后用增量同步到 Redis 缓存。
	if !skipRedisSync && !redisPreApplied && delta != 0 {
		if balanceSyncJob == nil {
			log.Printf("[billing] missing balance sync job user=%d type=%s corr_id=%s delta=%d",
				userID, txType, corrID, delta)
			return nil
		}
		if err := billing.ApplyBalanceSyncJob(context.Background(), *balanceSyncJob); err != nil {
			log.Printf("[billing] apply db delta to redis failed user=%d type=%s corr_id=%s delta=%d err=%v",
				userID, txType, corrID, delta, err)
		} else if _, err := db.Engine.ID(balanceSyncJob.ID).Cols("status").Update(&model.BalanceSyncJob{Status: "done"}); err != nil {
			log.Printf("[billing] mark db->redis balance sync done failed user=%d type=%s corr_id=%s job_id=%d err=%v",
				userID, txType, corrID, balanceSyncJob.ID, err)
		}
	}

	// 消费类交易触发邀请返佣和号商收益：
	//   hold    — 输入费预扣（input_from_response=false 时精确，=true 时为估算）
	//   settle  — 输出费或差额补扣
	//   charge  — 图片/视频/音频一次性扣费
	//   refund  — 退款时反向扣回已发放的返佣/收益（传负值）
	switch txType {
	case "charge", "settle", "hold":
		go applyPostBillingHooks(userID, poolKeyID, credits, cost)
	case "refund":
		go applyPostBillingHooks(userID, poolKeyID, -credits, -cost)
	}
	return nil
}

// applyPostBillingHooks 在消费发生后异步处理：
//  1. 邀请返佣：若用户有邀请人，按比例将 credits 加入邀请人的冻结余额
//  2. 号商收益：若本次请求使用了号商的 Key，按比例计入号商可提现余额
//
// credits/cost 可为负值（refund 场景），表示回扣已发放的返佣/收益，不会使余额低于 0。
func metricInt64(metrics model.JSON, key string) int64 {
	if metrics == nil {
		return 0
	}
	switch v := metrics[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

func metricBool(metrics model.JSON, key string) bool {
	if metrics == nil {
		return false
	}
	switch v := metrics[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

func applyPostBillingHooks(userID, poolKeyID, credits, cost int64) {
	ctx := context.Background()

	// ── 邀请返佣 ─────────────────────────────────────────────────────────
	if credits != 0 {
		var inviterID int64
		var rebateRatio *float64
		rows, err := db.Engine.QueryString(
			"SELECT inviter_id, rebate_ratio FROM users WHERE id = $1", userID,
		)
		if err == nil && len(rows) > 0 {
			if s := rows[0]["inviter_id"]; s != "" {
				inviterID, _ = strconv.ParseInt(s, 10, 64)
			}
			if s := rows[0]["rebate_ratio"]; s != "" {
				var r float64
				if _, err2 := fmt.Sscanf(s, "%f", &r); err2 == nil {
					rebateRatio = &r
				}
			}
		}

		if inviterID > 0 {
			ratio := getRebateRatio(ctx, rebateRatio)
			if ratio > 0 {
				rebateCredits := int64(float64(credits) * ratio)
				if rebateCredits != 0 {
					var sql string
					if rebateCredits > 0 {
						sql = "UPDATE users SET frozen_balance = frozen_balance + $1 WHERE id = $2"
					} else {
						// 回扣：floor 0，不允许透支冻结余额
						sql = "UPDATE users SET frozen_balance = GREATEST(0, frozen_balance + $1) WHERE id = $2"
					}
					if _, err := db.Engine.Exec(sql, rebateCredits, inviterID); err != nil {
						log.Printf("[billing] apply inviter rebate failed user=%d inviter=%d err=%v", userID, inviterID, err)
					}
				}
			}
		}
	}

	// ── 号商收益 ──────────────────────────────────────────────────────────
	if poolKeyID > 0 && cost != 0 {
		rows, err := db.Engine.QueryString(
			"SELECT vendor_id FROM pool_keys WHERE id = $1", poolKeyID,
		)
		if err == nil && len(rows) > 0 {
			vendorIDStr := rows[0]["vendor_id"]
			if vendorIDStr != "" {
				vendorID, _ := strconv.ParseInt(vendorIDStr, 10, 64)
				if vendorID > 0 {
					commission := getVendorCommission(ctx, vendorID)
					// 号商到手 = cost * (1 - commission)；负值时回扣
					vendorEarns := int64(float64(cost) * (1 - commission))
					if vendorEarns != 0 {
						var sql string
						if vendorEarns > 0 {
							sql = "UPDATE vendors SET balance = balance + $1 WHERE id = $2"
						} else {
							// 回扣：floor 0
							sql = "UPDATE vendors SET balance = GREATEST(0, balance + $1) WHERE id = $2"
						}
						if _, err2 := db.Engine.Exec(sql, vendorEarns, vendorID); err2 != nil {
							log.Printf("[billing] apply vendor earning failed vendor=%d err=%v", vendorID, err2)
						}
					}
				}
			}
		}
	}
}

// getRebateRatio 返回有效的返佣比例：优先使用用户个人设置，否则读取系统默认值。
func getRebateRatio(_ context.Context, userRatio *float64) float64 {
	if userRatio != nil {
		return *userRatio
	}
	s := &model.SystemSetting{}
	if found, _ := db.Engine.Where("key = ?", "default_rebate_ratio").Get(s); found && s.Value != "" {
		var r float64
		if _, err := fmt.Sscanf(s.Value, "%f", &r); err == nil {
			return r
		}
	}
	return 0
}

// getVendorCommission 返回有效的平台手续费比例：优先使用号商个人设置，否则读取系统默认值。
func getVendorCommission(_ context.Context, vendorID int64) float64 {
	rows, _ := db.Engine.QueryString("SELECT commission_ratio FROM vendors WHERE id = $1", vendorID)
	if len(rows) > 0 && rows[0]["commission_ratio"] != "" {
		var r float64
		if _, err := fmt.Sscanf(rows[0]["commission_ratio"], "%f", &r); err == nil {
			return r
		}
	}
	s := &model.SystemSetting{}
	if found, _ := db.Engine.Where("key = ?", "default_vendor_commission").Get(s); found && s.Value != "" {
		var r float64
		if _, err := fmt.Sscanf(s.Value, "%f", &r); err == nil {
			return r
		}
	}
	return 0
}

// GetBalance 从 DB 返回用户的当前余额。
func GetBalance(ctx context.Context, userID int64) (int64, error) {
	if balance, err := billing.GetBalance(ctx, userID); err == nil {
		return balance, nil
	}
	return GetDBBalance(ctx, userID)
}

// GetDBBalance 从 PostgreSQL 返回用户的当前余额快照。
func GetDBBalance(ctx context.Context, userID int64) (int64, error) {
	user := &model.User{}
	found, err := db.Engine.Where("id = ?", userID).Cols("balance").Get(user)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("用户不存在")
	}
	return user.Balance, nil
}

// Recharge 为用户增加 credits（管理员操作）。
// 余额更新已在 WriteTx 内完成，请勿在此处重复更新 DB。
func Recharge(ctx context.Context, userID, adminID, credits int64) error {
	return WriteTx(ctx, userID, 0, 0, 0, "", "recharge", credits, 0, 0, nil)
}

// GrantModelCredit 为用户赠送指定模型的专属积分（管理员操作）。
// modelName 为渠道的路由键（display_name 非空时为 display_name，否则为 model）。
func GrantModelCredit(ctx context.Context, userID int64, modelName string, credits int64) error {
	return billing.AddModelCredit(ctx, userID, modelName, credits)
}

// ListModelCredits 返回用户所有模型专属积分记录。
func ListModelCredits(ctx context.Context, userID int64) ([]model.UserModelCredit, error) {
	var records []model.UserModelCredit
	err := db.Engine.Where("user_id = ? AND credits > 0", userID).
		OrderBy("model_name").Find(&records)
	return records, err
}

// ListTransactions 返回用户的分页计费历史。corrID/taskID 非空时分别按对应字段过滤。
func ListTransactions(ctx context.Context, userID int64, page, pageSize int, corrID, taskID string) ([]model.BillingTransaction, error) {
	var txs []model.BillingTransaction
	sess := db.Engine.Where("user_id = ?", userID)
	if corrID != "" {
		sess.And("corr_id = ?", corrID)
	}
	if taskID != "" {
		if id, err := strconv.ParseInt(taskID, 10, 64); err == nil {
			sess.And("task_id = ?", id)
		} else {
			sess.And("1 = 0")
		}
	}
	err := sess.Desc("created_at").
		Limit(pageSize, (page-1)*pageSize).
		Find(&txs)
	return txs, err
}

// CountTransactions 返回用户的计费记录总数。corrID/taskID 非空时分别按对应字段过滤。
func CountTransactions(ctx context.Context, userID int64, corrID, taskID string) (int64, error) {
	sess := db.Engine.Where("user_id = ?", userID)
	if corrID != "" {
		sess.And("corr_id = ?", corrID)
	}
	if taskID != "" {
		if id, err := strconv.ParseInt(taskID, 10, 64); err == nil {
			sess.And("task_id = ?", id)
		} else {
			sess.And("1 = 0")
		}
	}
	count, err := sess.Count(&model.BillingTransaction{})
	return count, err
}

func ensureRefundDedupeKey(userID int64, corrID string, credits, cost, modelCreditCharged int64, metrics model.JSON) string {
	if metrics == nil {
		metrics = model.JSON{}
	}
	if key, _ := metrics["refund_dedupe_key"].(string); key != "" {
		return key
	}
	clean := model.JSON{}
	for k, v := range metrics {
		switch k {
		case "refund_dedupe_key", "refund_retry_job", "skip_redis_sync", "last_error":
			continue
		default:
			clean[k] = v
		}
	}
	payload := map[string]interface{}{
		"user_id":              userID,
		"corr_id":              corrID,
		"credits":              credits,
		"cost":                 cost,
		"model_credit_charged": modelCreditCharged,
		"metrics":              clean,
	}
	b, _ := json.Marshal(payload)
	sum := sha1.Sum(b)
	key := hex.EncodeToString(sum[:])
	metrics["refund_dedupe_key"] = key
	return key
}

func refundTxExists(sess interface {
	QueryString(...interface{}) ([]map[string]string, error)
}, dedupeKey string) (bool, error) {
	if dedupeKey == "" {
		return false, nil
	}
	rows, err := sess.QueryString(
		"SELECT id FROM billing_transactions WHERE type = 'refund' AND metrics->>'refund_dedupe_key' = $1 LIMIT 1",
		dedupeKey,
	)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func cloneMetrics(metrics model.JSON) model.JSON {
	out := model.JSON{}
	for k, v := range metrics {
		out[k] = v
	}
	return out
}

func enqueueRefundJob(ctx context.Context, userID, channelID, apiKeyID, poolKeyID int64, corrID string, credits, cost, modelCreditRefunded int64, metrics model.JSON, dedupeKey string, cause error) error {
	if credits <= 0 {
		return nil
	}
	if metrics == nil {
		metrics = model.JSON{}
	}
	if dedupeKey == "" {
		dedupeKey = ensureRefundDedupeKey(userID, corrID, credits, cost, modelCreditRefunded, metrics)
	}
	if dedupeKey != "" {
		var existing model.BillingRefundJob
		found, err := db.Engine.Context(ctx).
			Where("dedupe_key = ? AND dedupe_key != ''", dedupeKey).
			Get(&existing)
		if err != nil {
			return err
		}
		if found {
			if existing.Status == "done" || existing.Status == "pending" {
				return nil
			}
			patch := &model.BillingRefundJob{
				Status:    "pending",
				NextRunAt: time.Now(),
				LastError: fmt.Sprint(cause),
			}
			_, err = db.Engine.Context(ctx).
				ID(existing.ID).
				Cols("status", "next_run_at", "last_error", "updated_at").
				Update(patch)
			return err
		}
	}
	jobMetrics := cloneMetrics(metrics)
	jobMetrics["refund_retry_job"] = true
	jobMetrics["skip_redis_sync"] = true
	if dedupeKey != "" {
		jobMetrics["refund_dedupe_key"] = dedupeKey
	}
	if cause != nil {
		jobMetrics["deferred_error"] = cause.Error()
	}
	job := &model.BillingRefundJob{
		UserID:              userID,
		ChannelID:           channelID,
		APIKeyID:            apiKeyID,
		PoolKeyID:           poolKeyID,
		CorrID:              corrID,
		Credits:             credits,
		Cost:                cost,
		ModelCreditRefunded: modelCreditRefunded,
		Metrics:             jobMetrics,
		DedupeKey:           dedupeKey,
		Status:              "pending",
		NextRunAt:           time.Now(),
		LastError:           fmt.Sprint(cause),
	}
	_, err := db.Engine.Context(ctx).Insert(job)
	if err != nil && dedupeKey != "" {
		var existing model.BillingRefundJob
		found, lookupErr := db.Engine.Context(ctx).
			Where("dedupe_key = ? AND dedupe_key != ''", dedupeKey).
			Get(&existing)
		if lookupErr != nil {
			return lookupErr
		}
		if found {
			return nil
		}
	}
	return err
}

// StartBillingRefundJobWorker retries refund transactions that were safely
// applied to the hot quota path but failed to persist on the first attempt.
func StartBillingRefundJobWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		log.Println("[billing-refund] refund retry worker started")
		processBillingRefundJobs(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processBillingRefundJobs(ctx)
			}
		}
	}()
}

func processBillingRefundJobs(ctx context.Context) {
	if n, err := ProcessBillingRefundJobs(ctx, 100); err != nil {
		log.Printf("[billing-refund] process refund jobs failed after %d jobs: %v", n, err)
	}
}

func ProcessBillingRefundJobs(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var jobs []model.BillingRefundJob
	if err := db.Engine.Context(ctx).
		Where("status = ? AND next_run_at <= ?", "pending", time.Now()).
		Asc("id").
		Limit(limit).
		Find(&jobs); err != nil {
		return 0, err
	}

	processed := 0
	for _, job := range jobs {
		if err := processBillingRefundJob(ctx, job); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func processBillingRefundJob(ctx context.Context, job model.BillingRefundJob) error {
	if job.DedupeKey != "" {
		rows, err := db.Engine.Context(ctx).QueryString(
			"SELECT id FROM billing_transactions WHERE type = 'refund' AND metrics->>'refund_dedupe_key' = $1 LIMIT 1",
			job.DedupeKey,
		)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			_, err = db.Engine.Context(ctx).
				ID(job.ID).
				Cols("status", "attempts", "last_error", "updated_at").
				Update(&model.BillingRefundJob{
					Status:    "done",
					Attempts:  job.Attempts + 1,
					LastError: "",
				})
			return err
		}
	}

	metrics := cloneMetrics(job.Metrics)
	metrics["refund_retry_job"] = true
	metrics["skip_redis_sync"] = true
	if job.DedupeKey != "" {
		metrics["refund_dedupe_key"] = job.DedupeKey
	}
	err := WriteTx(ctx, job.UserID, job.ChannelID, job.APIKeyID, job.PoolKeyID, job.CorrID, "refund", job.Credits, job.Cost, job.ModelCreditRefunded, metrics)
	if err != nil {
		attempts := job.Attempts + 1
		backoff := time.Duration(attempts*attempts) * time.Second
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
		_, updateErr := db.Engine.Context(ctx).
			ID(job.ID).
			Cols("attempts", "next_run_at", "last_error", "updated_at").
			Update(&model.BillingRefundJob{
				Attempts:  attempts,
				NextRunAt: time.Now().Add(backoff),
				LastError: err.Error(),
			})
		if updateErr != nil {
			return updateErr
		}
		return nil
	}
	_, err = db.Engine.Context(ctx).
		ID(job.ID).
		Cols("status", "attempts", "last_error", "updated_at").
		Update(&model.BillingRefundJob{
			Status:    "done",
			Attempts:  job.Attempts + 1,
			LastError: "",
		})
	return err
}
