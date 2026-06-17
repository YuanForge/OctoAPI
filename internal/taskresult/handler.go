package taskresult

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"fanapi/internal/billing"
	"fanapi/internal/config"
	"fanapi/internal/db"
	"fanapi/internal/model"
	"fanapi/internal/mq"
	"fanapi/internal/service"

	"github.com/nats-io/nats.go"
)

const maxPoolKeyExhaustRetries = 8

// StartResultProcessor 订阅 RESULTS JetStream 流。
// 只应在 API 服务器进程中调用。
func StartResultProcessor(_ config.WorkerConfig) error {
	if _, err := mq.QueueSubscribe("result.>", "result-proc", handleResult, 0); err != nil {
		return fmt.Errorf("subscribe results: %w", err)
	}
	log.Println("[result-proc] subscribed to result.>")
	return nil
}

func handleResult(msg *nats.Msg) {
	var res model.WorkerResult
	if err := json.Unmarshal(msg.Data, &res); err != nil {
		log.Printf("[result-proc] bad message: %v", err)
		_ = msg.Term()
		return
	}

	ctx := context.Background()

	upstreamReq := toJSON(res.UpstreamRequest)
	upstreamResp := toJSON(res.UpstreamResponse)

	switch res.Outcome {

	case model.OutcomeDone:
		result := toJSON(res.Result)
		if res.TaskType == "image" {
			if ch, err := service.GetChannel(ctx, res.ChannelID); err == nil {
				result = convertResultURLs(result, ch.BaseURL)
			}
		}
		enqueueDoneUpdate(doneItem{
			msg:          msg,
			taskID:       res.TaskID,
			status:       "done",
			progress:     100,
			result:       result,
			upstreamReq:  upstreamReq,
			upstreamResp: upstreamResp,
		})
		return // ACK 由批量写入器处理

	case model.OutcomeAsync:
		enqueueDoneUpdate(doneItem{
			msg:            msg,
			taskID:         res.TaskID,
			status:         "processing",
			upstreamTaskID: res.UpstreamTaskID,
			upstreamReq:    upstreamReq,
		})
		log.Printf("[result-proc] task %d async, upstream_task_id=%s", res.TaskID, res.UpstreamTaskID)
		return // ACK 由批量写入器处理

	case model.OutcomeRateLimited:
		triedKeyIDs := appendResultPoolKeyID(res.PoolRetryKeyIDs, res.PoolKeyID)
		retryErrMsg := ""
		if res.PoolKeyID <= 0 {
			retryErrMsg = "pool key retry unavailable"
		} else if len(triedKeyIDs) >= maxPoolKeyExhaustRetries {
			retryErrMsg = "pool key exhausted after retry"
		} else {
			ch, err := service.GetChannel(ctx, res.ChannelID)
			if err != nil {
				retryErrMsg = "rate limited + channel load failed: " + err.Error()
			} else {
				newKey, err := service.MarkExhaustedAndRotate(ctx, ch.KeyPoolID, res.PoolKeyID, res.UserID)
				if err == nil && newKey != nil {
					job := &model.TaskJob{
						TaskID:          res.TaskID,
						TaskType:        res.TaskType,
						UserID:          res.UserID,
						APIKeyID:        res.APIKeyID,
						CorrID:          res.CorrID,
						CreditsCharged:  res.CreditsCharged,
						ChannelID:       res.ChannelID,
						BaseURL:         ch.BaseURL,
						Method:          ch.Method,
						Headers:         ch.Headers,
						TimeoutMs:       ch.TimeoutMs,
						QueryTimeoutMs:  ch.QueryTimeoutMs,
						RequestScript:   ch.RequestScript,
						ResponseScript:  ch.ResponseScript,
						ErrorScript:     ch.ErrorScript,
						QueryURL:        ch.QueryURL,
						QueryMethod:     ch.QueryMethod,
						QueryScript:     ch.QueryScript,
						PoolKeyID:       newKey.ID,
						PoolKeyValue:    newKey.Value,
						PoolKeyBaseURL:  newKey.BaseURLOverride,
						Payload:         res.Payload,
						RetryCount:      res.RetryCount + 1,
						PoolRetryKeyIDs: triedKeyIDs,
						RetryChannelIDs: res.RetryChannelIDs, // 透传：429 轮转 Key 重试若仍失败，仍可触发稳定密钥换渠道重试
					}
					data, _ := json.Marshal(job)
					subject := fmt.Sprintf("task.%s.%d", res.TaskType, res.ChannelID)
					if pubErr := mq.Publish(subject, data); pubErr != nil {
						saveAndFail(ctx, res, upstreamReq, upstreamResp, "rate limited, retry publish failed")
					}
					_ = msg.Ack()
					return
				}
				retryErrMsg = "pool key rotation failed: " + fmt.Sprint(err)
			}
		}
		res.PoolRetryKeyIDs = triedKeyIDs
		if res.ErrorMsg == "" {
			res.ErrorMsg = retryErrMsg
		}
		fallthrough

	case model.OutcomePoolKeyRetry:
		poolRetryChannel, channelErr := service.GetChannel(ctx, res.ChannelID)
		if channelErr == nil && poolRetryChannel.KeyPoolID > 0 && res.PoolKeyID > 0 {
			triedKeyIDs := appendResultPoolKeyID(res.PoolRetryKeyIDs, res.PoolKeyID)
			newKey, rotateErr := service.RotatePoolKeySkipping(ctx, poolRetryChannel.KeyPoolID, res.UserID, triedKeyIDs)
			if rotateErr == nil && newKey != nil {
				job := &model.TaskJob{
					TaskID:          res.TaskID,
					TaskType:        res.TaskType,
					UserID:          res.UserID,
					APIKeyID:        res.APIKeyID,
					CorrID:          res.CorrID,
					CreditsCharged:  res.CreditsCharged,
					ChannelID:       res.ChannelID,
					BaseURL:         poolRetryChannel.BaseURL,
					Method:          poolRetryChannel.Method,
					Headers:         poolRetryChannel.Headers,
					TimeoutMs:       poolRetryChannel.TimeoutMs,
					QueryTimeoutMs:  poolRetryChannel.QueryTimeoutMs,
					RequestScript:   poolRetryChannel.RequestScript,
					ResponseScript:  poolRetryChannel.ResponseScript,
					ErrorScript:     poolRetryChannel.ErrorScript,
					QueryURL:        poolRetryChannel.QueryURL,
					QueryMethod:     poolRetryChannel.QueryMethod,
					QueryScript:     poolRetryChannel.QueryScript,
					PoolKeyID:       newKey.ID,
					PoolKeyValue:    newKey.Value,
					PoolKeyBaseURL:  newKey.BaseURLOverride,
					Payload:         res.Payload,
					RetryCount:      res.RetryCount,
					PoolRetryKeyIDs: triedKeyIDs,
					RetryChannelIDs: res.RetryChannelIDs,
				}
				updateProcessingTask(ctx, res.TaskID, upstreamReq, upstreamResp)
				data, _ := json.Marshal(job)
				subject := fmt.Sprintf("task.%s.%d", res.TaskType, res.ChannelID)
				if publishErr := mq.Publish(subject, data); publishErr != nil {
					saveAndFail(ctx, res, upstreamReq, upstreamResp, "pool key retry publish failed: "+publishErr.Error())
				}
				_ = msg.Ack()
				return
			}
			res.PoolRetryKeyIDs = triedKeyIDs
			if res.ErrorMsg == "" {
				res.ErrorMsg = "pool key retry exhausted: " + fmt.Sprint(rotateErr)
			}
		} else {
			if channelErr != nil {
				res.ErrorMsg = "pool key retry channel load failed: " + channelErr.Error()
			} else if res.ErrorMsg == "" {
				res.ErrorMsg = "pool key retry unavailable"
			}
		}
		fallthrough

	case model.OutcomeFailed:
		if len(upstreamReq) > 0 || len(upstreamResp) > 0 {
			db.Engine.Where("id = ?", res.TaskID).
				Cols("upstream_request", "upstream_response").
				Update(&model.Task{UpstreamRequest: upstreamReq, UpstreamResponse: upstreamResp})
		}

		// 稳定密钥：若还有备用渠道，换渠道重试而不是直接失败
		if len(res.RetryChannelIDs) > 0 {
			nextChannelID := res.RetryChannelIDs[0]
			remaining := res.RetryChannelIDs[1:]
			nextCh, chErr := service.GetChannel(ctx, nextChannelID)
			if chErr == nil {
				log.Printf("[result-proc] task %d failed on channel %d, retrying with channel %d (stable key)", res.TaskID, res.ChannelID, nextChannelID)

				// 退回当前渠道的费用
				if res.CreditsCharged > 0 {
					var chargeTx model.BillingTransaction
					upstreamCostOld := int64(0)
					mcChargedOld := int64(0)
					routingKeyOld := ""
					if found, _ := db.Engine.Where("corr_id = ? AND type = ?", res.CorrID, "charge").Get(&chargeTx); found {
						upstreamCostOld = chargeTx.Cost
						mcChargedOld = chargeTx.ModelCreditCharged
						if rk, ok := chargeTx.Metrics["routing_key"].(string); ok {
							routingKeyOld = rk
						}
					}
					if mcChargedOld > 0 && routingKeyOld != "" {
						if err := billing.RefundModelCredit(ctx, res.UserID, routingKeyOld, mcChargedOld); err != nil {
							log.Printf("[result-proc] task %d: refund old model credit failed: %v", res.TaskID, err)
							mcChargedOld = 0
						}
					}
					generalRefundOld := res.CreditsCharged - chargeTx.ModelCreditCharged
					refundedOld := mcChargedOld
					if generalRefundOld > 0 {
						if err := billing.Refund(ctx, res.UserID, generalRefundOld); err != nil {
							log.Printf("[result-proc] task %d: refund old general balance failed: %v", res.TaskID, err)
						} else {
							refundedOld += generalRefundOld
						}
					}
					if refundedOld <= 0 {
						failTaskDB(ctx, res.TaskID, res.UserID, res.ChannelID, res.APIKeyID, res.CorrID, 0, "retry refund failed")
						_ = msg.Ack()
						return
					}
					if err := service.WriteTx(ctx, res.UserID, res.ChannelID, res.APIKeyID, res.PoolKeyID, res.CorrID, "refund", refundedOld, scaleCost(upstreamCostOld, refundedOld, res.CreditsCharged), mcChargedOld, model.JSON{
						"task_id":     res.TaskID,
						"routing_key": routingKeyOld,
						"reason":      "stable_key_channel_retry",
					}); err != nil {
						log.Printf("[result-proc] task %d: write retry refund tx failed: %v", res.TaskID, err)
						revertTaskRefund(ctx, res.UserID, res.TaskID, res.CreditsCharged, mcChargedOld, routingKeyOld)
						failTaskDB(ctx, res.TaskID, res.UserID, res.ChannelID, res.APIKeyID, res.CorrID, 0, "billing refund transaction failed: "+err.Error())
						_ = msg.Ack()
						return
					}
					updateTaskCreditsCharged(ctx, res.TaskID, res.CreditsCharged-refundedOld)
				}

				var userGroup string
				var task model.Task
				if found, _ := db.Engine.ID(res.TaskID).Cols("request").Get(&task); found {
					// 从任务请求中恢复 user_group（通过 user 表查询）
					var user model.User
					if ufound, _ := db.Engine.ID(res.UserID).Cols("group").Get(&user); ufound {
						userGroup = user.Group
					}
				}
				newCost, _, calcErr := billing.CalcForUser(nextCh, res.Payload, userGroup)
				newUpstreamCost, _ := billing.CalcUpstreamCost(nextCh, res.Payload)
				if calcErr != nil {
					log.Printf("[result-proc] task %d: calc cost for retry channel %d failed: %v, marking failed", res.TaskID, nextChannelID, calcErr)
					failTaskDB(ctx, res.TaskID, res.UserID, res.ChannelID, res.APIKeyID, res.CorrID, 0, res.ErrorMsg)
					_ = msg.Ack()
					return
				}
				// 路由键：取原始 payload 中的模型字段（未被渠道覆盖）。
				// 当前重试或第一次请求均使用同一个路由键。
				newRoutingKey := ""
				if rk, ok := res.Payload["model"].(string); ok {
					newRoutingKey = rk
				}
				var newModelCreditCharged int64
				if newCost > 0 {
					if newRoutingKey != "" {
						newModelCreditCharged, _ = billing.ChargeModelCredit(ctx, res.UserID, newRoutingKey, newCost)
					}
					generalNewCharge := newCost - newModelCreditCharged
					if generalNewCharge > 0 {
						if chargeErr := billing.Charge(ctx, res.UserID, generalNewCharge); chargeErr != nil {
							log.Printf("[result-proc] task %d: charge for retry channel %d failed: %v, marking failed", res.TaskID, nextChannelID, chargeErr)
							if newModelCreditCharged > 0 {
								_ = billing.RefundModelCredit(ctx, res.UserID, newRoutingKey, newModelCreditCharged)
							}
							failTaskDB(ctx, res.TaskID, res.UserID, res.ChannelID, res.APIKeyID, res.CorrID, 0, res.ErrorMsg)
							_ = msg.Ack()
							return
						}
					}
				}
				// 写新的扣费流水
				newCorrID := res.CorrID + "_r" + fmt.Sprintf("%d", nextChannelID)
				if err := service.WriteTx(ctx, res.UserID, nextChannelID, res.APIKeyID, 0, newCorrID, "charge", newCost, newUpstreamCost, newModelCreditCharged, model.JSON{
					"task_id":      res.TaskID,
					"retry_of":     res.ChannelID,
					"routing_key":  newRoutingKey,
					"stable_retry": true,
				}); err != nil {
					log.Printf("[result-proc] task %d: write retry charge tx failed: %v", res.TaskID, err)
					revertTaskCharge(ctx, res.UserID, res.TaskID, newCost, newModelCreditCharged, newRoutingKey)
					failTaskDB(ctx, res.TaskID, res.UserID, res.ChannelID, res.APIKeyID, res.CorrID, 0, "billing charge transaction failed: "+err.Error())
					_ = msg.Ack()
					return
				}

				// 更新 DB 中的渠道、费用和剩余重试列表
				// remaining 可能为空，使用 Cols() 强制写入空数组，避免后续异步失败时再次重试已尝试的渠道
				db.Engine.Where("id = ?", res.TaskID).Cols("channel_id", "credits_charged", "corr_id", "status", "retry_channel_ids").
					Update(&model.Task{
						ChannelID:       nextChannelID,
						CreditsCharged:  newCost,
						CorrID:          newCorrID,
						Status:          "processing",
						RetryChannelIDs: model.Int64Array(remaining),
					})

				// 分配号池 Key
				var poolKeyID int64
				var poolKeyValue string
				var poolKeyBaseURL string
				if nextCh.KeyPoolID > 0 {
					pk, pkErr := service.GetOrAssignPoolKey(ctx, nextCh.KeyPoolID, res.UserID)
					if pkErr == nil {
						poolKeyID = pk.ID
						poolKeyValue = pk.Value
						poolKeyBaseURL = pk.BaseURLOverride
					}
				}

				// 重新发布到新渠道
				retryJob := &model.TaskJob{
					TaskID:          res.TaskID,
					TaskType:        res.TaskType,
					UserID:          res.UserID,
					APIKeyID:        res.APIKeyID,
					CorrID:          newCorrID,
					CreditsCharged:  newCost,
					ChannelID:       nextChannelID,
					BaseURL:         nextCh.BaseURL,
					Method:          nextCh.Method,
					Headers:         nextCh.Headers,
					TimeoutMs:       nextCh.TimeoutMs,
					QueryTimeoutMs:  nextCh.QueryTimeoutMs,
					RequestScript:   nextCh.RequestScript,
					ResponseScript:  nextCh.ResponseScript,
					ErrorScript:     nextCh.ErrorScript,
					QueryURL:        nextCh.QueryURL,
					QueryMethod:     nextCh.QueryMethod,
					QueryScript:     nextCh.QueryScript,
					PoolKeyID:       poolKeyID,
					PoolKeyValue:    poolKeyValue,
					PoolKeyBaseURL:  poolKeyBaseURL,
					Payload:         res.Payload,
					RetryChannelIDs: remaining,
				}
				data, _ := json.Marshal(retryJob)
				subject := fmt.Sprintf("task.%s.%d", res.TaskType, nextChannelID)
				if pubErr := mq.Publish(subject, data); pubErr != nil {
					log.Printf("[result-proc] task %d: retry publish to channel %d failed: %v", res.TaskID, nextChannelID, pubErr)
					failTaskDB(ctx, res.TaskID, res.UserID, nextChannelID, res.APIKeyID, newCorrID, newCost, "retry publish failed: "+pubErr.Error())
				}
				_ = msg.Ack()
				return
			}
			log.Printf("[result-proc] task %d: could not load retry channel %d: %v, trying remaining", res.TaskID, nextChannelID, chErr)
		}

		failTaskDB(ctx, res.TaskID, res.UserID, res.ChannelID, res.APIKeyID, res.CorrID, res.CreditsCharged, res.ErrorMsg)

	default:
		log.Printf("[result-proc] unknown outcome %q for task %d", res.Outcome, res.TaskID)
	}

	_ = msg.Ack()
}

// saveAndFail 一次性写入上游字段并将任务标记为失败。
func saveAndFail(ctx context.Context, res model.WorkerResult, req, resp model.JSON, msg string) {
	if len(req) > 0 || len(resp) > 0 {
		db.Engine.Where("id = ?", res.TaskID).
			Cols("upstream_request", "upstream_response").
			Update(&model.Task{UpstreamRequest: req, UpstreamResponse: resp})
	}
	failTaskDB(ctx, res.TaskID, res.UserID, res.ChannelID, res.APIKeyID, res.CorrID, res.CreditsCharged, msg)
}

// failTaskDB 将任务标记为失败并退还 credits。
// 幂等操作：通过条件更新 (status != 'failed') 保持幂等。
func updateProcessingTask(ctx context.Context, taskID int64, req, resp model.JSON) {
	task := &model.Task{Status: "processing"}
	cols := []string{"status"}
	if len(req) > 0 {
		task.UpstreamRequest = req
		cols = append(cols, "upstream_request")
	}
	if len(resp) > 0 {
		task.UpstreamResponse = resp
		cols = append(cols, "upstream_response")
	}
	db.Engine.Context(ctx).Where("id = ?", taskID).Cols(cols...).Update(task)
}

func appendResultPoolKeyID(ids []int64, id int64) []int64 {
	if id <= 0 {
		return ids
	}
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func failTaskDB(ctx context.Context, taskID, userID, channelID, apiKeyID int64, corrID string, credits int64, errMsg string) {
	log.Printf("[result-proc] task %d failed: %s", taskID, errMsg)
	userMsg := service.UserFacingErrorMessage(errMsg)
	n, _ := db.Engine.
		Where("id = ? AND status != ?", taskID, "failed").
		Cols("status", "error_msg").
		Update(&model.Task{Status: "failed", ErrorMsg: errMsg})
	if n == 0 {
		return
	}
	if credits <= 0 {
		return
	}

	// 从原收费流水中查询上游成本、号池、模型积分专属部分及路由键，
	// 保证退款流水与收费流水对称。
	var chargeTx model.BillingTransaction
	upstreamCost := int64(0)
	poolKeyID := int64(0)
	mcCharged := int64(0)
	routingKey := ""
	if found, _ := db.Engine.Where("corr_id = ? AND type = ?", corrID, "charge").Get(&chargeTx); found {
		upstreamCost = chargeTx.Cost
		poolKeyID = chargeTx.PoolKeyID
		mcCharged = chargeTx.ModelCreditCharged
		if rk, ok := chargeTx.Metrics["routing_key"].(string); ok {
			routingKey = rk
		}
	}

	// 优先退还专属模型积分，剩余退还通用余额
	if mcCharged > 0 && routingKey != "" {
		if err := billing.RefundModelCredit(ctx, userID, routingKey, mcCharged); err != nil {
			log.Printf("[result-proc] task %d: refund model credit failed: %v", taskID, err)
			mcCharged = 0
		}
	}
	generalRefund := credits - chargeTx.ModelCreditCharged
	refunded := mcCharged
	if generalRefund > 0 {
		if err := billing.Refund(ctx, userID, generalRefund); err != nil {
			log.Printf("[result-proc] task %d: refund (Redis) failed: %v", taskID, err)
		} else {
			refunded += generalRefund
		}
	}
	if refunded <= 0 {
		log.Printf("[result-proc] task %d: no credits refunded to user %d", taskID, userID)
		return
	}
	if err := service.WriteTx(ctx, userID, channelID, apiKeyID, poolKeyID, corrID, "refund", refunded, scaleCost(upstreamCost, refunded, credits), mcCharged, model.JSON{
		"task_id":     taskID,
		"routing_key": routingKey,
		"reason":      userMsg,
	}); err != nil {
		log.Printf("[result-proc] task %d: write refund tx failed: %v", taskID, err)
		revertTaskRefund(ctx, userID, taskID, credits, mcCharged, routingKey)
		return
	}
	updateTaskCreditsCharged(ctx, taskID, credits-refunded)
	log.Printf("[result-proc] task %d: refunded %d credits (model_credit=%d) to user %d", taskID, credits, mcCharged, userID)
}

func updateTaskCreditsCharged(ctx context.Context, taskID, credits int64) {
	if credits < 0 {
		credits = 0
	}
	if _, err := db.Engine.Context(ctx).
		Where("id = ?", taskID).
		Cols("credits_charged").
		Update(&model.Task{CreditsCharged: credits}); err != nil {
		log.Printf("[result-proc] task %d: update credits_charged failed: %v", taskID, err)
	}
}

func revertTaskRefund(ctx context.Context, userID, taskID, credits, modelRefunded int64, routingKey string) {
	if modelRefunded > 0 && routingKey != "" {
		charged, err := billing.ChargeModelCredit(ctx, userID, routingKey, modelRefunded)
		if err != nil {
			log.Printf("[result-proc] task %d: revert model refund failed user=%d routing_key=%s credits=%d err=%v",
				taskID, userID, routingKey, modelRefunded, err)
		} else if charged != modelRefunded {
			log.Printf("[result-proc] task %d: revert model refund partial user=%d routing_key=%s expected=%d charged=%d",
				taskID, userID, routingKey, modelRefunded, charged)
		}
	}
}

func revertTaskCharge(ctx context.Context, userID, taskID, credits, modelCharged int64, routingKey string) {
	if modelCharged > 0 && routingKey != "" {
		if err := billing.RefundModelCredit(ctx, userID, routingKey, modelCharged); err != nil {
			log.Printf("[result-proc] task %d: revert model charge failed user=%d routing_key=%s credits=%d err=%v",
				taskID, userID, routingKey, modelCharged, err)
		}
	}
}

func scaleCost(cost, actual, requested int64) int64 {
	if cost <= 0 || actual <= 0 || requested <= 0 {
		return 0
	}
	if actual >= requested {
		return cost
	}
	return cost * actual / requested
}

func toJSON(m map[string]interface{}) model.JSON {
	j := model.JSON{}
	for k, v := range m {
		j[k] = v
	}
	return j
}
