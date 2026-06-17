package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"fanapi/internal/billing"
	"fanapi/internal/db"
	"fanapi/internal/model"
	"fanapi/internal/mq"
	"fanapi/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// requestBaseURL 从请求中推断站点根 URL（如 "https://example.com"），
// 优先使用 X-Forwarded-Proto 以兼容反向代理场景。
func requestBaseURL(c *gin.Context) string {
	scheme := "https"
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if c.Request.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + c.Request.Host
}

// expandReferImages 将 refer_images 中以 "/" 开头的本地路径补全为完整 URL。
// 已是完整 URL（http/https 开头）的条目保持不变。
func expandReferImages(images []string, baseURL string) []string {
	if len(images) == 0 {
		return images
	}
	expanded := make([]string, len(images))
	for i, img := range images {
		if strings.HasPrefix(img, "/") {
			expanded[i] = baseURL + img
		} else {
			expanded[i] = img
		}
	}
	return expanded
}

func expandReferVideos(videos []string, baseURL string) []string {
	if len(videos) == 0 {
		return videos
	}
	expanded := make([]string, len(videos))
	for i, video := range videos {
		if strings.HasPrefix(video, "/") {
			expanded[i] = baseURL + video
		} else {
			expanded[i] = video
		}
	}
	return expanded
}

// bindImageRequest 将请求 body 解析为 ImageRequest。
// 先按结构体绑定固定字段（做必填校验），再将原始 JSON 中其余字段写入 Extra，
// Extra 字段经 ToMap() 合并后透传给 JS 映射脚本。
func bindImageRequest(bodyBytes []byte) (*model.ImageRequest, error) {
	var req model.ImageRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return nil, err
	}
	req.Size = strings.ToLower(strings.TrimSpace(req.Size))
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	var raw map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &raw)
	known := map[string]bool{"model": true, "prompt": true, "size": true, "aspect_ratio": true, "refer_images": true, "n": true}
	req.Extra = make(map[string]interface{})
	for k, v := range raw {
		if !known[k] {
			req.Extra[k] = v
		}
	}
	return &req, nil
}

// bindVideoRequest 将请求 body 解析为 VideoRequest 并合并 Extra 字段。
func bindVideoRequest(bodyBytes []byte) (*model.VideoRequest, error) {
	var req model.VideoRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return nil, err
	}
	req.Size = strings.ToLower(strings.TrimSpace(req.Size))
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	var raw map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &raw)
	known := map[string]bool{"model": true, "prompt": true, "size": true, "aspect_ratio": true, "duration": true, "refer_images": true, "refer_videos": true}
	req.Extra = make(map[string]interface{})
	for k, v := range raw {
		if !known[k] {
			req.Extra[k] = v
		}
	}
	return &req, nil
}

// bindAudioRequest 将请求 body 解析为 AudioRequest 并合并 Extra 字段。
func bindAudioRequest(bodyBytes []byte) (*model.AudioRequest, error) {
	var req model.AudioRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return nil, err
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	var raw map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &raw)
	known := map[string]bool{"model": true, "input": true, "voice": true, "duration": true}
	req.Extra = make(map[string]interface{})
	for k, v := range raw {
		if !known[k] {
			req.Extra[k] = v
		}
	}
	return &req, nil
}

// createTask 是图片/视频/音频任务的通用创建逻辑。
// reqData 是平台标准格式的 map（由 bind* + ToMap() 产生），包含 size、aspect_ratio 等字段。
// 计费在此精确完成（size+aspect_ratio 已知）；worker 内执行 request_script 再转为 vendor 格式。
func createTask(c *gin.Context, taskType string, reqData map[string]interface{}) {
	userID := c.MustGet("user_id").(int64)
	apiKeyID, _ := c.Get("api_key_id")
	var apiKeyIDVal int64
	if apiKeyID != nil {
		apiKeyIDVal = apiKeyID.(int64)
	}

	// 获取密钥类型（稳定密钥使用售价升序路由）
	keyType, _ := c.Get("key_type")
	isStable := keyType == "stable"
	var userGroup string
	if raw, ok := c.Get("user_group"); ok {
		userGroup, _ = raw.(string)
	}

	// 余额前置检查：通用余额 <= 0 时直接拒绝，无论模型定价是否为 0。
	if bal, balErr := billing.GetBalance(c.Request.Context(), userID); balErr == nil && bal <= 0 {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": "余额不足，请充值后继续使用"})
		return
	}

	// 渠道解析：优先 channel_id 查询参数（兼容旧客户端），否则用 reqData["model"] 按渠道名路由。
	var ch *model.Channel
	var stableChannels []model.Channel // 稳定密钥：按价格排序的候选列表

	if channelIDStr := c.Query("channel_id"); channelIDStr != "" {
		channelID, parseErr := strconv.ParseInt(channelIDStr, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id 格式错误"})
			return
		}
		var chErr error
		ch, chErr = service.GetChannel(c.Request.Context(), channelID)
		if chErr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": chErr.Error()})
			return
		}
	} else {
		routingModel, _ := reqData["model"].(string)
		if routingModel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请指定 model 或 channel_id"})
			return
		}
		if isStable {
			var chErr error
			stableChannels, chErr = service.SelectChannelStableForUser(c.Request.Context(), routingModel, userGroup)
			if chErr != nil {
				// 兜底：按 name 精确查找（稳定密钥优先保证可用性）
				chSingle, nameErr := service.GetChannelByName(c.Request.Context(), routingModel)
				if nameErr != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在: " + routingModel})
					return
				}
				stableChannels = []model.Channel{*chSingle}
			}
			ch = &stableChannels[0]
		} else {
			var chErr error
			ch, chErr = service.SelectChannel(c.Request.Context(), routingModel)
			if chErr != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在: " + routingModel})
				return
			}
		}
	}
	channelID := ch.ID

	// 捕获用户传入的路由键（用于专属模型积分扣减），必须在模型名覆盖之前保存
	routingKey, _ := reqData["model"].(string)

	// 用渠道配置的真实模型名覆盖用户传入的路由键。
	if ch.Model != "" {
		reqData["model"] = ch.Model
	}

	// 精确计费：图片/视频/音频在请求时参数已全部已知，无需两阶段结算
	cost, _, calcErr := billing.CalcForUser(ch, reqData, userGroup)
	if calcErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "计费计算失败，请稍后重试"})
		return
	}
	// 计算上游进价成本（用于记录利润，不影响用户扣费）
	upstreamCost, _ := billing.CalcUpstreamCost(ch, reqData)

	var modelCreditCharged int64
	if cost > 0 {
		// 优先消耗专属模型积分，不足部分再扣通用余额
		if routingKey != "" {
			modelCreditCharged, _ = billing.ChargeModelCredit(c.Request.Context(), userID, routingKey, cost)
		}
		generalCharge := cost - modelCreditCharged
		if generalCharge > 0 {
			if chargeErr := billing.Charge(c.Request.Context(), userID, generalCharge); chargeErr != nil {
				// 退回已扣的模型积分
				if modelCreditCharged > 0 {
					_ = billing.RefundModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged)
				}
				c.JSON(http.StatusPaymentRequired, gin.H{"error": chargeErr.Error()})
				return
			}
		}
	}

	// 解析号池 Key（在任务写入前获取，以便后续所有流水记录携带 pool_key_id）
	var poolKeyID int64
	var poolKeyValue string
	var poolKeyBaseURL string
	if ch.KeyPoolID > 0 {
		pk, pkErr := service.GetOrAssignPoolKey(c.Request.Context(), ch.KeyPoolID, userID)
		if pkErr != nil {
			if cost > 0 {
				if err := billing.ReleasePreAppliedQuota(c.Request.Context(), userID, cost-modelCreditCharged); err != nil {
					log.Printf("[proxy-billing] release pre-applied quota failed user_id=%d credits=%d err=%v", userID, cost-modelCreditCharged, err)
				}
				if modelCreditCharged > 0 {
					_ = billing.RefundModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged)
				}
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "号池分配失败: " + pkErr.Error()})
			return
		}
		poolKeyID = pk.ID
		poolKeyValue = pk.Value
		poolKeyBaseURL = pk.BaseURLOverride
	}

	// 将平台标准格式原样存入 DB，方便排障；vendor 格式只在 worker 内转换
	reqJSON := model.JSON{}
	for k, v := range reqData {
		reqJSON[k] = v
	}
	// 稳定密钥：将剩余待试渠道 ID（跳过当前渠道）按价格升序存入 RetryChannelIDs，
	// 同步路径由 result-proc 直接消费 WorkerResult 中的字段；异步路径由 poller 从 task 表读取。
	var retryChannelIDs []int64
	if len(stableChannels) > 0 {
		for i := range stableChannels {
			if stableChannels[i].ID != channelID {
				retryChannelIDs = append(retryChannelIDs, stableChannels[i].ID)
			}
		}
	} else if routingKey != "" {
		excluded := []int64{channelID}
		for len(retryChannelIDs) < 2 {
			nextCh, err := service.SelectChannelByWeight(c.Request.Context(), routingKey, excluded...)
			if err != nil || nextCh == nil {
				break
			}
			retryChannelIDs = append(retryChannelIDs, nextCh.ID)
			excluded = append(excluded, nextCh.ID)
		}
	}

	corrID := uuid.New().String()
	task := &model.Task{
		UserID:          userID,
		ChannelID:       channelID,
		APIKeyID:        apiKeyIDVal,
		Type:            taskType,
		Status:          "pending",
		Request:         reqJSON,
		CreditsCharged:  cost,
		CorrID:          corrID,
		RetryChannelIDs: model.Int64Array(retryChannelIDs),
	}
	if _, err := db.Engine.Insert(task); err != nil {
		if cost > 0 {
			if err := billing.ReleasePreAppliedQuota(c.Request.Context(), userID, cost-modelCreditCharged); err != nil {
				log.Printf("[proxy-billing] release pre-applied quota failed user_id=%d credits=%d err=%v", userID, cost-modelCreditCharged, err)
			}
			if modelCreditCharged > 0 {
				_ = billing.RefundModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged)
			}
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建任务失败，请稍后重试"})
		return
	}

	// 写计费流水（routing_key 存入 metrics，供 failTaskDB 退款时使用）
	if err := service.WriteTx(c.Request.Context(), userID, channelID, apiKeyIDVal, poolKeyID, corrID, "charge", cost, upstreamCost, modelCreditCharged, model.JSON{
		"task_id":     task.ID,
		"type":        taskType,
		"routing_key": routingKey,
	}); err != nil {
		db.Engine.Where("id = ?", task.ID).Cols("status", "error_msg").Update(&model.Task{Status: "failed", ErrorMsg: "billing transaction error"})
		if modelCreditCharged > 0 {
			_ = billing.RefundModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "计费流水写入失败，请稍后重试"})
		return
	}

	natSubject := fmt.Sprintf("task.%s.%d", taskType, channelID)
	job := &model.TaskJob{
		TaskID:          task.ID,
		TaskType:        taskType,
		UserID:          userID,
		APIKeyID:        apiKeyIDVal,
		CorrID:          corrID,
		CreditsCharged:  cost,
		ChannelID:       channelID,
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
		PoolKeyID:       poolKeyID,
		PoolKeyValue:    poolKeyValue,
		PoolKeyBaseURL:  poolKeyBaseURL,
		Payload:         reqData,
		RetryChannelIDs: retryChannelIDs,
	}
	msgBytes, _ := json.Marshal(job)
	if pubErr := mq.Publish(natSubject, msgBytes); pubErr != nil {
		db.Engine.Where("id = ?", task.ID).Cols("status", "error_msg").Update(&model.Task{Status: "failed", ErrorMsg: "publish error"})
		if cost > 0 {
			refunded := int64(0)
			generalRefund := cost - modelCreditCharged
			if generalRefund > 0 {
				if err := billing.Refund(c.Request.Context(), userID, generalRefund); err != nil {
					log.Printf("[proxy-billing] refund general balance failed user_id=%d task_id=%d credits=%d err=%v",
						userID, task.ID, generalRefund, err)
				} else {
					refunded += generalRefund
				}
			}
			if modelCreditCharged > 0 {
				if err := billing.RefundModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged); err != nil {
					log.Printf("[proxy-billing] refund model credit failed user_id=%d task_id=%d credits=%d err=%v",
						userID, task.ID, modelCreditCharged, err)
					modelCreditCharged = 0
				} else {
					refunded += modelCreditCharged
				}
			}
			if refunded <= 0 {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "任务投递失败，退款失败，请联系管理员"})
				return
			}
			if err := service.WriteTx(c.Request.Context(), userID, channelID, apiKeyIDVal, poolKeyID, corrID, "refund", refunded, scaleRefundCost(upstreamCost, refunded, cost), modelCreditCharged, model.JSON{
				"task_id":     task.ID,
				"routing_key": routingKey,
				"reason":      "publish error",
			}); err != nil {
				log.Printf("[proxy-billing] write publish-error refund tx failed user_id=%d task_id=%d corr_id=%s err=%v",
					userID, task.ID, corrID, err)
				if modelCreditCharged > 0 {
					if charged, chargeErr := billing.ChargeModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged); chargeErr != nil {
						log.Printf("[proxy-billing] revert model refund failed user_id=%d task_id=%d credits=%d err=%v",
							userID, task.ID, modelCreditCharged, chargeErr)
					} else if charged != modelCreditCharged {
						log.Printf("[proxy-billing] revert model refund partial user_id=%d task_id=%d expected=%d charged=%d",
							userID, task.ID, modelCreditCharged, charged)
					}
				}
			}
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务投递失败，请稍后重试"})
		return
	}

	// 标记为处理中（worker 收到后即开始执行）
	db.Engine.Where("id = ?", task.ID).Cols("status").Update(&model.Task{Status: "processing"})

	c.JSON(http.StatusAccepted, gin.H{"task_id": task.ID})
}

// CreateImageTask 创建图片生成任务
// @Summary      创建图片生成任务
// @Description  异步任务，提交后返回 task_id；通过 GET /v1/tasks/:id 轮询结果。
// @Tags         媒体生成
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.ImageRequest  true  "图片生成参数"
// @Success      202   {object}  object{task_id=int}  "任务已接受"
// @Failure      400   {object}  object  "参数错误"
// @Failure      402   {object}  object  "余额不足"
// @Router       /v1/image [post]
func CreateImageTask(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	req, err := bindImageRequest(bodyBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ReferImages = expandReferImages(req.ReferImages, requestBaseURL(c))
	createTask(c, "image", req.ToMap())
}

// CreateVideoTask 创建视频生成任务
// @Summary      创建视频生成任务
// @Description  异步任务，提交后返回 task_id；通过 GET /v1/tasks/:id 轮询结果。
// @Tags         媒体生成
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.VideoRequest  true  "视频生成参数"
// @Success      202   {object}  object{task_id=int}  "任务已接受"
// @Failure      400   {object}  object  "参数错误"
// @Failure      402   {object}  object  "余额不足"
// @Router       /v1/video [post]
func CreateVideoTask(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	req, err := bindVideoRequest(bodyBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ReferImages = expandReferImages(req.ReferImages, requestBaseURL(c))
	req.ReferVideos = expandReferVideos(req.ReferVideos, requestBaseURL(c))
	createTask(c, "video", req.ToMap())
}

// CreateAudioTask 创建音频/TTS 任务
// @Summary      创建音频生成（TTS）任务
// @Description  异步任务，提交后返回 task_id；通过 GET /v1/tasks/:id 轮询结果。
// @Tags         媒体生成
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.AudioRequest  true  "音频生成参数"
// @Success      202   {object}  object{task_id=int}  "任务已接受"
// @Failure      400   {object}  object  "参数错误"
// @Failure      402   {object}  object  "余额不足"
// @Router       /v1/audio [post]
func CreateAudioTask(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	req, err := bindAudioRequest(bodyBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	createTask(c, "audio", req.ToMap())
}

// bindMusicRequest 将请求 body 解析为 MusicRequest 并合并 Extra 字段。
func bindMusicRequest(bodyBytes []byte) (*model.MusicRequest, error) {
	var req model.MusicRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return nil, err
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	var raw map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &raw)
	known := map[string]bool{
		"model": true, "input_type": true, "mv_version": true, "make_instrumental": true,
		"gpt_description_prompt": true, "prompt": true, "tags": true, "title": true,
		"continue_clip_id": true, "continue_at": true, "cover_clip_id": true,
		"task": true, "metadata_params": true, "callback_url": true,
	}
	req.Extra = make(map[string]interface{})
	for k, v := range raw {
		if !known[k] {
			req.Extra[k] = v
		}
	}
	return &req, nil
}

// CreateMusicTask 创建 Suno 音乐生成任务
// @Summary      创建音乐生成任务（Suno）
// @Description  异步任务，每次生成 2 首；提交后返回 task_id，通过 GET /v1/tasks/:id 轮询结果（items 数组）。
// @Description
// @Description  ## 创作模式说明
// @Description
// @Description  | input_type | 说明 |
// @Description  |-----------|------|
// @Description  | `10` | 灵感模式：填写 `gpt_description_prompt`，平台自动生成歌词 |
// @Description  | `20` | 自定义模式：手动填写 `prompt`（歌词）、`tags`（风格）、`title` |
// @Description
// @Description  ---
// @Description
// @Description  ### 续写模式（Extend）
// @Description
// @Description  在已有音频基础上续写，`continue_clip_id` 填目标音频 URL 或 clip ID，`continue_at` 为续写起始时间（秒）。
// @Description
// @Description  ```json
// @Description  {
// @Description    "model": "suno",
// @Description    "mv_version": "chirp-v5",
// @Description    "input_type": "20",
// @Description    "make_instrumental": false,
// @Description    "prompt": "[Verse 1]\n小狗汪汪叫\n...",
// @Description    "tags": "",
// @Description    "title": "为你歌唱",
// @Description    "continue_clip_id": "https://cdn1.suno.ai/7c395650-62f2-4c4f-8b68-cf55b874c96c.mp3",
// @Description    "continue_at": "27"
// @Description  }
// @Description  ```
// @Description
// @Description  ---
// @Description
// @Description  ### 添加人声（Add Vocals / overpainting）
// @Description
// @Description  给纯音乐轨道添加人声。`task` 设为 `overpainting`，`metadata_params` 中指定目标音频及起止时间（秒）。
// @Description
// @Description  ```json
// @Description  {
// @Description    "model": "suno",
// @Description    "mv_version": "chirp-v4-5+",
// @Description    "input_type": "20",
// @Description    "make_instrumental": false,
// @Description    "prompt": "[Verse 1]\nUsah lepas kau pergi\n...",
// @Description    "tags": "pop,female voice",
// @Description    "title": "Hi,melancholic",
// @Description    "task": "overpainting",
// @Description    "metadata_params": {
// @Description      "overpainting_clip_id": "https://cdn1.suno.ai/21ae9c64-86ab-435a-b810-ed62727caf0a.mp3",
// @Description      "overpainting_start_s": 0,
// @Description      "overpainting_end_s": 57.9
// @Description    }
// @Description  }
// @Description  ```
// @Description
// @Description  ---
// @Description
// @Description  ### 添加伴奏（Add Instrumental / underpainting）
// @Description
// @Description  给人声轨道添加纯音乐伴奏。`task` 设为 `underpainting`，`make_instrumental` 设为 `true`，`prompt` 留空，`metadata_params` 中指定目标音频及起止时间（秒）。
// @Description
// @Description  ```json
// @Description  {
// @Description    "model": "suno",
// @Description    "mv_version": "chirp-v4-5+",
// @Description    "input_type": "20",
// @Description    "make_instrumental": true,
// @Description    "prompt": "",
// @Description    "tags": "pop,female voice",
// @Description    "title": "Hi,melancholic",
// @Description    "task": "underpainting",
// @Description    "metadata_params": {
// @Description      "underpainting_clip_id": "https://cdn1.suno.ai/21ae9c64-86ab-435a-b810-ed62727caf0a.mp3",
// @Description      "underpainting_start_s": 0,
// @Description      "underpainting_end_s": 57.9
// @Description    }
// @Description  }
// @Description  ```
// @Tags         媒体生成
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.MusicRequest  true  "Suno 音乐生成参数"
// @Success      202   {object}  object{task_id=int}  "任务已接受"
// @Failure      400   {object}  object  "参数错误"
// @Failure      402   {object}  object  "余额不足"
// @Router       /v1/music [post]
func CreateMusicTask(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	req, err := bindMusicRequest(bodyBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	createTask(c, "music", req.ToMap())
}
