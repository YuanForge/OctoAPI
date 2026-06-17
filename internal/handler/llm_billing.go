package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"fanapi/internal/billing"
	"fanapi/internal/model"
	"fanapi/internal/service"

	"github.com/gin-gonic/gin"
)

// llmSettle 执行结算：与预扣金额对比，退还多扣或补扣差额，并写计费流水。
// usageData 为精确或估算的 {prompt_tokens, completion_tokens}；
// 为 nil 时（连接在任何输出前断开）全额退款。
func llmSettle(c *gin.Context, ch *model.Channel, reqData, usageData map[string]interface{},
	totalHold, userID, channelID, apiKeyIDVal, poolKeyIDVal int64, corrID string, userGroup string) {
	ctx := c.Request.Context()
	upstreamCostHold, _ := billing.CalcUpstreamCost(ch, reqData)

	// 非 token 计费（image/video/audio/count/custom）：预扣即精确值，上游成功即结算完毕，不依赖 usageData。
	// 例外：billing_type=image 且响应中检测到实际图片数量（image_count），按实际图片数调差。
	if ch.BillingType != "token" {
		if ch.BillingType == "image" && usageData != nil {
			var imgCount int64
			switch v := usageData["image_count"].(type) {
			case int64:
				imgCount = v
			case float64:
				imgCount = int64(v)
			}
			if imgCount > 0 {
				// 预扣时使用的图片数量（来自请求 n 字段，默认 1）
				preCount := int64(1)
				switch v := reqData["n"].(type) {
				case float64:
					if v > 0 {
						preCount = int64(v)
					}
				case int64:
					if v > 0 {
						preCount = v
					}
				}
				if imgCount != preCount {
					// 计算单张图片的价格：将 reqData 中 n 改为 1 后调用 CalcForUser
					singleReq := make(map[string]interface{}, len(reqData)+1)
					for k, v := range reqData {
						singleReq[k] = v
					}
					singleReq["n"] = float64(1)
					costPerImage, _, _ := billing.CalcForUser(ch, singleReq, userGroup)
					delta := imgCount - preCount
					if delta > 0 {
						extraCost := costPerImage * delta
						mcCharged, generalCharged, chargeErr := llmChargeExtra(c, userID, extraCost)
						charged := mcCharged + generalCharged
						if charged > 0 {
							metrics := model.JSON{
								"reason":      "image_count_adjust",
								"image_count": imgCount,
								"pre_count":   preCount,
							}
							if chargeErr != nil || charged < extraCost {
								metrics["partial"] = true
								metrics["charge_error"] = fmt.Sprint(chargeErr)
								metrics["attempted_cost"] = extraCost
							}
							recordLLMChargeTx(ctx, c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID,
								"settle", charged, 0, mcCharged, metrics)
						}
					} else {
						refundAmt := costPerImage * (-delta)
						refunded, mcRefunded := llmRefundCredits(c, userID, refundAmt)
						recordLLMRefundTx(ctx, c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID,
							refunded, 0, mcRefunded, model.JSON{
								"reason":      "image_count_adjust",
								"image_count": imgCount,
								"pre_count":   preCount,
							})
					}
				}
			}
		}
		enqueueLLMLogPatch(corrID, []string{"status", "usage", "error_msg"}, model.LLMLog{Status: "ok", Usage: model.JSON(usageData), ErrorMsg: ""})
		return
	}

	if usageData == nil {
		if totalHold > 0 {
			refunded, mcRefunded := llmRefundCredits(c, userID, totalHold)
			recordLLMRefundTx(ctx, c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, refunded, scaleRefundCost(upstreamCostHold, refunded, totalHold), mcRefunded, model.JSON{"reason": "no_output"})
		}
		enqueueLLMLogPatch(corrID, []string{"status"}, model.LLMLog{Status: "refunded"})
		return
	}
	respData := map[string]interface{}{"usage": usageData}
	actualCost, settleErr := billing.CalcActualCostForUser(ch, reqData, respData, userGroup)
	actualUpstreamCost, _ := billing.CalcActualUpstreamCost(ch, reqData, respData)
	if settleErr == nil {
		inputFromResponse, _ := ch.BillingConfig["input_from_response"].(bool)
		if !inputFromResponse {
			// 分离结算：预扣已扣除估算输入费用，此处结算差额（输出 + 缓存折扣调整）。
			// delta = actualCost - totalHold
			//   > 0：实际费用超出预扣（有输出/补扣），需再扣差额
			//   < 0：实际费用低于预扣（高缓存命中率导致输入成本降低），需退还差额
			//   = 0：刚好持平，无需操作
			outputCost := actualCost - totalHold
			outputUpstreamCost := actualUpstreamCost - upstreamCostHold
			if outputCost < 0 {
				// 实际费用低于预扣：退还多扣部分（常见于 Prompt Cache 命中率较高的场景）
				refundAmt := -outputCost
				refunded, mcRefunded := llmRefundCredits(c, userID, refundAmt)
				upstreamRefund := int64(0)
				if outputUpstreamCost < 0 {
					upstreamRefund = -outputUpstreamCost
				}
				recordLLMRefundTx(ctx, c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, refunded, scaleRefundCost(upstreamRefund, refunded, refundAmt), mcRefunded, model.JSON{
					"actual_cost": actualCost,
					"held":        totalHold,
					"usage":       usageData,
					"reason":      "cache_discount",
				})
			} else {
				mcCharged := int64(0)
				generalCharged := int64(0)
				var chargeErr error
				if outputCost > 0 {
					mcCharged, generalCharged, chargeErr = llmChargeExtra(c, userID, outputCost)
				}
				charged := mcCharged + generalCharged
				upstreamSettle := outputUpstreamCost
				if upstreamSettle < 0 {
					upstreamSettle = 0
				}
				metrics := model.JSON{
					"actual_cost": actualCost,
					"held":        totalHold,
					"usage":       usageData,
				}
				if chargeErr != nil || charged < outputCost {
					metrics["partial"] = true
					metrics["charge_error"] = fmt.Sprint(chargeErr)
					metrics["attempted_cost"] = outputCost
				}
				recordLLMChargeTx(ctx, c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, "settle", charged, upstreamSettle, mcCharged, metrics)
			}
		} else {
			// input_from_response=true 或非 token 类型：预扣为估算，结算修正差额。
			// 预扣已从 DB 扣除 totalHold，此处补充差额使总扣款等于实际费用。
			delta := totalHold - actualCost
			if delta > 0 {
				// 实际费用低于预估：退还多扣部分
				refunded, mcRefunded := llmRefundCredits(c, userID, delta)
				upstreamDelta := upstreamCostHold - actualUpstreamCost
				if upstreamDelta < 0 {
					upstreamDelta = 0
				}
				recordLLMRefundTx(ctx, c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, refunded, scaleRefundCost(upstreamDelta, refunded, delta), mcRefunded, model.JSON{
					"actual_cost": actualCost,
					"held":        totalHold,
					"usage":       usageData,
				})
			} else if delta < 0 {
				// 实际费用高于预估：补扣差额
				extraCost := -delta
				mcCharged, generalCharged, chargeErr := llmChargeExtra(c, userID, extraCost)
				charged := mcCharged + generalCharged
				upstreamExtra := actualUpstreamCost - upstreamCostHold
				if upstreamExtra < 0 {
					upstreamExtra = 0
				}
				metrics := model.JSON{
					"actual_cost": actualCost,
					"held":        totalHold,
					"usage":       usageData,
				}
				if chargeErr != nil || charged < extraCost {
					metrics["partial"] = true
					metrics["charge_error"] = fmt.Sprint(chargeErr)
					metrics["attempted_cost"] = extraCost
				}
				if charged > 0 {
					recordLLMChargeTx(ctx, c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, "settle", charged, upstreamExtra, mcCharged, metrics)
				}
			}
		}
	}
	enqueueLLMLogPatch(corrID, []string{"status", "usage", "error_msg"}, model.LLMLog{Status: "ok", Usage: model.JSON(usageData), ErrorMsg: ""})
}

// llmRefundAndAbort 退款并终止请求（上游失败时调用）。
// corrID 不为空时同步更新 LLMLog 的错误状态。
// llmRefundCredits 按优先级退款：优先退回通用余额，再退专属模型积分（与扣款顺序相反）。
// 调用时自动更新 gin context 中记录的已扣款数量，保证多次退款不会重复退回。

// llmChargeExtra 结算补扣：优先消耗专属模型积分，不足部分再扣通用余额。
// 同步更新 gin context 中记录的已扣款数量，保证后续退款计算正确。
// 返回从专属模型积分、通用余额中实际扣除的数量，供 WriteTx 记录。
func llmChargeExtra(c *gin.Context, userID, amount int64) (modelExtraCharged, generalExtraCharged int64, err error) {
	if amount <= 0 {
		return 0, 0, nil
	}
	ctx := c.Request.Context()

	if rk, ok := c.Get("model_credit_routing_key"); ok {
		if routingKey, ok := rk.(string); ok && routingKey != "" {
			modelExtraCharged, _ = billing.ChargeModelCredit(ctx, userID, routingKey, amount)
		}
	}

	generalExtraCharged = amount - modelExtraCharged
	if generalExtraCharged > 0 {
		if chargeErr := billing.Charge(ctx, userID, generalExtraCharged); chargeErr != nil {
			if modelExtraCharged > 0 {
				if rk, ok := c.Get("model_credit_routing_key"); ok {
					if routingKey, ok := rk.(string); ok && routingKey != "" {
						_ = billing.RefundModelCredit(ctx, userID, routingKey, modelExtraCharged)
					}
				}
			}
			return 0, 0, chargeErr
		}
	}

	// 更新 context 中的累计扣款记录，供后续退款使用
	if modelExtraCharged > 0 {
		mc := int64(0)
		if v, ok := c.Get("model_credit_charged"); ok {
			if val, ok := v.(int64); ok {
				mc = val
			}
		}
		c.Set("model_credit_charged", mc+modelExtraCharged)
	}
	if generalExtraCharged > 0 {
		gc := int64(0)
		if v, ok := c.Get("model_credit_general_charged"); ok {
			if val, ok := v.(int64); ok {
				gc = val
			}
		}
		c.Set("model_credit_general_charged", gc+generalExtraCharged)
	}
	return modelExtraCharged, generalExtraCharged, nil
}

// llmRefundCredits 退款：优先退通用余额，再退专属模型积分（与扣款顺序相反）。
// 返回实际退款总额和其中的专属模型积分数量，供 WriteTx 记录。
func llmRefundCredits(c *gin.Context, userID, amount int64) (refunded, modelRefunded int64) {
	if amount <= 0 {
		return 0, 0
	}
	ctx := c.Request.Context()

	// 读取本次请求的扣款记录
	modelCharged := int64(0)
	if mc, ok := c.Get("model_credit_charged"); ok {
		if v, ok := mc.(int64); ok {
			modelCharged = v
		}
	}
	generalCharged := int64(0)
	if gc, ok := c.Get("model_credit_general_charged"); ok {
		if v, ok := gc.(int64); ok {
			generalCharged = v
		}
	}

	// 优先退通用余额，再退模型积分
	generalRefund := int64(0)
	modelRefund := int64(0)
	if amount <= generalCharged {
		generalRefund = amount
	} else {
		generalRefund = generalCharged
		modelRefund = amount - generalCharged
		if modelRefund > modelCharged {
			modelRefund = modelCharged
		}
	}

	if generalRefund > 0 {
		if err := billing.Refund(ctx, userID, generalRefund); err != nil {
			log.Printf("[llm-billing] refund general balance failed user_id=%d credits=%d err=%v", userID, generalRefund, err)
		} else {
			refunded += generalRefund
			c.Set("model_credit_general_charged", generalCharged-generalRefund)
		}
	}
	if modelRefund > 0 {
		if rk, ok := c.Get("model_credit_routing_key"); ok {
			if routingKey, ok := rk.(string); ok && routingKey != "" {
				if err := billing.RefundModelCredit(ctx, userID, routingKey, modelRefund); err != nil {
					log.Printf("[llm-billing] refund model credit failed user_id=%d routing_key=%s credits=%d err=%v", userID, routingKey, modelRefund, err)
				} else {
					refunded += modelRefund
					modelRefunded = modelRefund
					c.Set("model_credit_charged", modelCharged-modelRefund)
				}
			}
		}
	}
	return refunded, modelRefunded
}

func recordLLMChargeTx(ctx context.Context, c *gin.Context, userID, channelID, apiKeyIDVal, poolKeyIDVal int64, corrID, txType string, credits, upstreamCost, modelCharged int64, metrics model.JSON) bool {
	if credits <= 0 {
		return true
	}
	if err := service.WriteTx(ctx, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, txType, credits, upstreamCost, modelCharged, metrics); err != nil {
		log.Printf("[llm-billing] write %s tx failed user_id=%d corr_id=%s credits=%d model_credit=%d err=%v",
			txType, userID, corrID, credits, modelCharged, err)
		revertLLMCharge(ctx, c, userID, credits, modelCharged)
		return false
	}
	return true
}

func recordLLMRefundTx(ctx context.Context, c *gin.Context, userID, channelID, apiKeyIDVal, poolKeyIDVal int64, corrID string, credits, upstreamCost, modelRefunded int64, metrics model.JSON) bool {
	if credits <= 0 {
		return true
	}
	if err := service.WriteTx(ctx, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, "refund", credits, upstreamCost, modelRefunded, metrics); err != nil {
		log.Printf("[llm-billing] write refund tx failed user_id=%d corr_id=%s credits=%d model_credit=%d err=%v",
			userID, corrID, credits, modelRefunded, err)
		revertLLMRefund(ctx, c, userID, credits, modelRefunded)
		return false
	}
	return true
}

func scaleRefundCost(cost, refunded, requested int64) int64 {
	if cost <= 0 || refunded <= 0 || requested <= 0 || refunded >= requested {
		if refunded <= 0 {
			return 0
		}
		return cost
	}
	return cost * refunded / requested
}

func revertLLMCharge(ctx context.Context, c *gin.Context, userID, credits, modelCharged int64) {
	generalCharged := credits - modelCharged
	if generalCharged > 0 {
		// WriteTx already compensates the Redis general balance for failed pre-applied charges.
		adjustContextCounter(c, "model_credit_general_charged", -generalCharged)
	}
	if modelCharged > 0 {
		if routingKey := getLLMRoutingKey(c); routingKey != "" {
			if err := billing.RefundModelCredit(ctx, userID, routingKey, modelCharged); err != nil {
				log.Printf("[llm-billing] revert model charge failed user_id=%d routing_key=%s credits=%d err=%v", userID, routingKey, modelCharged, err)
			}
			adjustContextCounter(c, "model_credit_charged", -modelCharged)
		}
	}
}

func revertLLMRefund(ctx context.Context, c *gin.Context, userID, credits, modelRefunded int64) {
	generalRefunded := credits - modelRefunded
	if generalRefunded > 0 {
		// WriteTx compensates the Redis general quota when a refund cannot be queued.
		adjustContextCounter(c, "model_credit_general_charged", generalRefunded)
	}
	if modelRefunded > 0 {
		if routingKey := getLLMRoutingKey(c); routingKey != "" {
			if charged, err := billing.ChargeModelCredit(ctx, userID, routingKey, modelRefunded); err != nil {
				log.Printf("[llm-billing] revert model refund failed user_id=%d routing_key=%s credits=%d err=%v", userID, routingKey, modelRefunded, err)
			} else if charged != modelRefunded {
				log.Printf("[llm-billing] revert model refund partial user_id=%d routing_key=%s expected=%d charged=%d", userID, routingKey, modelRefunded, charged)
			} else {
				adjustContextCounter(c, "model_credit_charged", modelRefunded)
			}
		}
	}
}

func getLLMRoutingKey(c *gin.Context) string {
	if rk, ok := c.Get("model_credit_routing_key"); ok {
		if routingKey, ok := rk.(string); ok {
			return routingKey
		}
	}
	return ""
}

func adjustContextCounter(c *gin.Context, key string, delta int64) {
	current := int64(0)
	if v, ok := c.Get(key); ok {
		if val, ok := v.(int64); ok {
			current = val
		}
	}
	next := current + delta
	if next < 0 {
		next = 0
	}
	c.Set(key, next)
}

func refundLLMHoldForRetry(c *gin.Context, userID, channelID, apiKeyIDVal, poolKeyIDVal int64, corrID string, totalHold, upstreamCostHold int64, reason string) bool {
	if totalHold <= 0 {
		return true
	}
	refunded, mcRefunded := llmRefundCredits(c, userID, totalHold)
	if refunded <= 0 {
		msg := fmt.Sprintf("重试前退款失败，预扣未退回：corr_id=%s user_id=%d hold=%d", corrID, userID, totalHold)
		log.Printf("[llm-billing] %s", msg)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重试前退款失败，请稍后重试"})
		return false
	}
	if !recordLLMRefundTx(c.Request.Context(), c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, refunded, scaleRefundCost(upstreamCostHold, refunded, totalHold), mcRefunded, model.JSON{"reason": reason}) {
		log.Printf("[llm-billing] retry refund tx failed corr_id=%s user_id=%d refunded=%d hold=%d", corrID, userID, refunded, totalHold)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重试前退款流水写入失败，请稍后重试"})
		return false
	}
	if refunded != totalHold {
		log.Printf("[llm-billing] retry refund partial corr_id=%s user_id=%d refunded=%d hold=%d", corrID, userID, refunded, totalHold)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重试前预扣未全额退回，请稍后重试"})
		return false
	}
	return true
}

func llmRefundAndAbort(c *gin.Context, corrID string, userID, credits, upstreamCost, poolKeyIDVal int64, upstreamStatus int, errMsg string) {
	userMsg := service.UserFacingErrorMessage(errMsg)
	if userMsg != errMsg {
		log.Printf("[llm] request %s failed: %s", corrID, errMsg)
	}
	if credits > 0 {
		refunded, mcRefunded := llmRefundCredits(c, userID, credits)
		recordLLMRefundTx(c.Request.Context(), c, userID, 0, 0, poolKeyIDVal, corrID, refunded, scaleRefundCost(upstreamCost, refunded, credits), mcRefunded, model.JSON{"reason": "upstream_error"})
	}
	if corrID != "" {
		enqueueLLMLogPatch(corrID, []string{"status", "upstream_status", "error_msg"}, model.LLMLog{Status: "error", UpstreamStatus: upstreamStatus, ErrorMsg: errMsg})
	}
	// 根据错误类型返回语义准确的 HTTP 状态码：
	// - 上游超时（context deadline exceeded / Client.Timeout）→ 504 Gateway Timeout
	// - 其他上游失败 → 502 Bad Gateway
	statusCode := http.StatusBadGateway
	if strings.Contains(errMsg, "context deadline exceeded") || strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "Timeout") {
		statusCode = http.StatusGatewayTimeout
	}
	c.JSON(statusCode, gin.H{"error": userMsg})
}
