package handler

import (
	"bufio"
	"bytes"
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
	"fanapi/internal/notify"
	"fanapi/internal/protocol"
	"fanapi/internal/script"
	"fanapi/internal/service"
	"fanapi/internal/upstream"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	protocolOpenAI    = "openai"
	protocolClaude    = "claude"
	protocolGemini    = "gemini"
	protocolResponses = "responses"
	protocolRealtime  = "realtime"

	responsesOperationCompact = "compact"
	maxPoolKeyExhaustRetries  = 8
)

// OpenAIModels returns an OpenAI-compatible model list for clients that discover
// available models through GET /v1/models.
//
// @Summary      OpenAI 兼容模型列表
// @Description  返回当前 API Key 可用的 LLM 模型列表；data[].id 可填入对话请求的 model 字段作为 routing_model。
// @Tags         LLM
// @Produce      json
// @Security     ApiKeyAuth
// @Success      200  {object}  model.OpenAIModelListResponse
// @Failure      500  {object}  model.APIErrorResponse  "内部错误"
// @Router       /v1/models [get]
func OpenAIModels(c *gin.Context) {
	var channels []model.Channel
	if err := db.Engine.Where("is_active = true AND type = ?", "llm").
		Cols("id", "model", "display_name", "created_at").
		OrderBy("id ASC").
		Find(&channels); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type modelInfo struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	seen := make(map[string]bool)
	data := make([]modelInfo, 0, len(channels))
	for _, ch := range channels {
		id := service.ChannelRoutingKey(ch)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		created := int64(0)
		if !ch.CreatedAt.IsZero() {
			created = ch.CreatedAt.Unix()
		}
		data = append(data, modelInfo{
			ID:      id,
			Object:  "model",
			Created: created,
			OwnedBy: "fanapi",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}

func effectiveProtocol(ch *model.Channel) string {
	if ch.Protocol == "" {
		return protocolOpenAI
	}
	return ch.Protocol
}

// LLMProxy 处理 POST /v1/chat/completions（OpenAI 标准格式）。
// 客户端发送 OpenAI 格式请求，收到 OpenAI 格式 SSE 响应。
//
// @Summary      OpenAI 兼容对话（Chat Completions）
// @Description  发送 OpenAI 格式对话请求，支持流式（SSE）和非流式；将 model 字段填写渠道的 routing_model，服务端自动路由到真实上游模型。
// @Tags         LLM
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.OpenAIChatCompletionRequest  true  "OpenAI Chat Completions 请求体；model 填渠道名称（routing_model）"
// @Success      200   {object}  model.OpenAIChatCompletionResponse  "OpenAI 格式响应；stream=true 时为 text/event-stream SSE 流"
// @Failure      400   {object}  model.APIErrorResponse  "参数错误"
// @Failure      402   {object}  model.APIErrorResponse  "余额不足"
// @Failure      503   {object}  model.APIErrorResponse  "无可用渠道"
// @Router       /v1/chat/completions [post]
func LLMProxy(c *gin.Context) {
	c.Set("client_proto", protocolOpenAI)
	llmProxy(c)
}

// ClaudeProxy 处理 POST /v1/messages（Anthropic Claude 原生格式）。
// 客户端发送 Claude 原生格式请求，收到 Claude 原生格式 SSE 响应。
//
// @Summary      Anthropic Claude 原生对话
// @Description  发送 Anthropic Messages API 格式请求，支持流式（SSE）；model 填渠道的 routing_model。
// @Tags         LLM
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.ClaudeMessagesRequest  true  "Claude Messages 请求体；model 填渠道名称（routing_model）"
// @Success      200   {object}  model.ClaudeMessagesResponse  "Claude 格式响应；stream=true 时为 text/event-stream SSE 流"
// @Failure      400   {object}  model.APIErrorResponse  "参数错误"
// @Failure      402   {object}  model.APIErrorResponse  "余额不足"
// @Router       /v1/messages [post]
func ClaudeProxy(c *gin.Context) {
	c.Set("client_proto", protocolClaude)
	llmProxy(c)
}

// GeminiProxy 处理 POST /v1/gemini（Gemini generateContent 兼容格式，非原生路径）。
// 客户端发送 Gemini generateContent 风格请求，收到 Gemini 风格响应。
// 如需兼容 Google AI SDK 原生 URL 路径，请使用 /v1beta/models/{path}。
//
// @Summary      Gemini generateContent 兼容接口（非原生路径）
// @Description  接收 Gemini generateContent 风格请求体并按统一路由转发；这不是 Google Gemini 的原生 URL 路径。若需兼容 Google AI SDK 原生路径，请使用 /v1beta/models/{path}。
// @Tags         LLM
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        channel_id  query     int     false  "渠道 ID（兼容旧版）"
// @Param        body        body      model.GeminiGenerateContentRequest  true   "Gemini generateContent 风格请求体；model 可省略并由 channel_id 指定"
// @Success      200   {object}  model.GeminiGenerateContentResponse  "Gemini 风格响应"
// @Failure      400   {object}  model.APIErrorResponse  "参数错误"
// @Failure      402   {object}  model.APIErrorResponse  "余额不足"
// @Router       /v1/gemini [post]
func GeminiProxy(c *gin.Context) {
	c.Set("client_proto", protocolGemini)
	llmProxy(c)
}

// GeminiNativeProxy 处理 POST /v1beta/models/{model}:generateContent 和
// POST /v1beta/models/{model}:streamGenerateContent（Google Gemini SDK 原生路径格式）。
//
// 兼容官方 Google AI SDK，只需将 SDK 的 baseURL 指向本服务即可。
// model 从 URL 路径中自动提取并注入请求体，stream 由 URL action 自动推断，
// 无需客户端修改任何请求体字段。
//
// @Summary      Gemini 原生路径兼容（Google AI SDK）
// @Description  兼容 Google Gemini SDK 原生路径格式：非流式访问 :generateContent，流式访问 :streamGenerateContent?alt=sse。model 从 URL 路径自动提取。
// @Tags         LLM
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        path  path   string  true   "{model}:generateContent 或 {model}:streamGenerateContent"
// @Param        alt   query  string  false  "流式 SSE 参数；streamGenerateContent 通常传 sse"
// @Param        body  body   model.GeminiGenerateContentRequest  true  "Gemini generateContent 请求体；model 和 stream 会从 URL 自动注入"
// @Success      200   {object}  model.GeminiGenerateContentResponse  "Gemini 格式响应；流式时为 text/event-stream SSE"
// @Failure      400   {object}  model.APIErrorResponse  "参数错误"
// @Failure      402   {object}  model.APIErrorResponse  "余额不足"
// @Router       /v1beta/models/{path} [post]
func GeminiNativeProxy(c *gin.Context) {
	// Gin wildcard 路由 /v1beta/models/*path 捕获的值形如 /gemini-2.5-flash:generateContent
	path := strings.TrimPrefix(c.Param("path"), "/")

	// 解析模型名和 action（格式：{model}:{action}）
	parts := strings.SplitN(path, ":", 2)
	modelName := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	// 流式判断：action 为 streamGenerateContent，或查询参数 alt=sse
	isStream := strings.HasPrefix(action, "streamGenerateContent") || c.Query("alt") == "sse"

	// 读取请求体，注入 model 和 stream 字段后替换
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	if len(bodyBytes) == 0 {
		bodyBytes = []byte("{}")
	}
	var reqData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体 JSON 格式错误"})
		return
	}

	// 注入 model（用于渠道路由）
	if modelName != "" {
		reqData["model"] = modelName
	}
	// 注入 stream（llmProxy 在协议转换前读取此字段）
	if isStream {
		reqData["stream"] = true
	}

	newBody, _ := json.Marshal(reqData)
	c.Request.Body = io.NopCloser(bytes.NewReader(newBody))
	c.Request.ContentLength = int64(len(newBody))

	c.Set("client_proto", protocolGemini)
	llmProxy(c)
}

// ResponsesProxy 处理 POST /v1/responses（OpenAI Responses API，Codex CLI 使用）。
// 客户端发送 Responses API 格式请求，收到 Responses API 格式响应（同步或 SSE）。
//
// 请求格式：
//
//	{
//	  "model": "codex-mini-latest",
//	  "input": "hello" / [{"role":"user","content":"hello"}],
//	  "instructions": "You are helpful.",  // 等价于 system 消息
//	  "stream": true,
//	  "max_output_tokens": 4096
//	}
//
// @Summary      OpenAI Responses API（Codex CLI 兼容）
// @Description  兼容 OpenAI Responses API（POST /v1/responses），Codex CLI 默认使用此接口。将 Responses API 格式请求转换为 Chat Completions 格式转发上游，并将响应转换回 Responses API 格式。
// @Tags         LLM
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.ResponsesRequest  true  "Responses API 请求体；model 填渠道名称（routing_model）"
// @Success      200   {object}  model.ResponsesResponse  "Responses API 格式响应；stream=true 时为 text/event-stream SSE 流"
// @Failure      400   {object}  model.APIErrorResponse  "参数错误"
// @Failure      402   {object}  model.APIErrorResponse  "余额不足"
// @Failure      503   {object}  model.APIErrorResponse  "无可用渠道"
// @Router       /v1/responses [post]
func ResponsesProxy(c *gin.Context) {
	c.Set("client_proto", protocolResponses)
	llmProxy(c)
}

// ResponsesCompactProxy 处理 POST /v1/responses/compact。
// Codex 在执行对话压缩时会请求该兼容端点；请求体仍按 Responses API 代理链路处理。
//
// @Summary      OpenAI Responses API 对话压缩兼容
// @Description  兼容 Codex 等客户端的 POST /v1/responses/compact；仅选择 protocol=responses 的上游渠道，并按 Responses API 格式返回。
// @Tags         LLM
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        body  body      model.ResponsesRequest  true  "Responses API 请求体；model 填渠道名称（routing_model）"
// @Success      200   {object}  model.ResponsesResponse  "Responses API 格式响应；stream=true 时为 text/event-stream SSE 流"
// @Failure      400   {object}  model.APIErrorResponse  "参数错误"
// @Failure      402   {object}  model.APIErrorResponse  "余额不足"
// @Failure      503   {object}  model.APIErrorResponse  "无可用渠道"
// @Router       /v1/responses/compact [post]
func ResponsesCompactProxy(c *gin.Context) {
	c.Set("client_proto", protocolResponses)
	c.Set("responses_operation", responsesOperationCompact)
	llmProxy(c)
}

// llmProxy 是三条 LLM 路由的共同实现。
// 支持：
//   - 多渠道负载均衡（加权随机 + 优先级 + 错误率自动屏蔽）
//   - 稳定密钥：按售价升序尝试，失败自动切换更贵的渠道
//   - 格式互转（OpenAI ↔ Claude / Gemini）
//   - 认证扩展（bearer / query_param / basic / sigv4）
//   - 用户分组定价
//   - 失败自动重试（最多 3 个不同渠道）
func llmProxy(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)
	apiKeyID, _ := c.Get("api_key_id")
	var apiKeyIDVal int64
	if apiKeyID != nil {
		apiKeyIDVal = apiKeyID.(int64)
	}

	// 获取用户 group（用于分组定价）
	var userGroup string
	if raw, ok := c.Get("user_group"); ok {
		userGroup, _ = raw.(string)
	}

	// 获取密钥类型（稳定密钥使用价格升序路由）
	keyType, _ := c.Get("key_type")
	isStable := keyType == "stable"

	channelIDStr := c.Query("channel_id")
	responsesOperation := getResponsesOperation(c)
	isResponsesCompact := responsesOperation == responsesOperationCompact

	// 余额前置检查：通用余额 <= 0 时直接拒绝，无论模型定价是否为 0
	if bal, balErr := billing.GetBalance(c.Request.Context(), userID); balErr == nil && bal <= 0 {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": "余额不足，请充值后继续使用"})
		return
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}
	// 保存原始请求体字节，供 passthrough_body 模式使用（在任何解析或修改之前）
	c.Set("raw_body", bodyBytes)
	var reqData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体 JSON 格式错误"})
		return
	}

	// 渠道选择
	var ch *model.Channel
	var triedIDs []int64
	var stableChannels []model.Channel // 稳定密钥：按价格排好序的渠道列表

	if channelIDStr != "" {
		// 直接指定 channel_id，不走负载均衡
		channelID, parseErr := strconv.ParseInt(channelIDStr, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id 格式错误"})
			return
		}
		ch, err = service.GetChannel(c.Request.Context(), channelID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if isResponsesCompact && effectiveProtocol(ch) != protocolResponses {
			c.JSON(http.StatusBadRequest, gin.H{"error": "对话压缩需要选择 protocol=responses 的渠道"})
			return
		}
	} else {
		routingModel, _ := reqData["model"].(string)
		if routingModel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请在请求体 model 字段填写模型名称，或通过 channel_id 参数指定渠道"})
			return
		}
		if isStable {
			// 稳定密钥：获取按价格升序排列的渠道列表
			if isResponsesCompact {
				stableChannels, err = service.SelectChannelStableForUserByProtocol(c.Request.Context(), routingModel, protocolResponses, userGroup)
			} else {
				stableChannels, err = service.SelectChannelStableForUser(c.Request.Context(), routingModel, userGroup)
			}
			if err != nil {
				// 兜底：按 name 精确查找（兼容旧行为）
				ch, err = service.GetChannelByName(c.Request.Context(), routingModel)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在: " + routingModel})
					return
				}
				if isResponsesCompact && effectiveProtocol(ch) != protocolResponses {
					c.JSON(http.StatusNotFound, gin.H{"error": "对话压缩需要可用的 protocol=responses 渠道: " + routingModel})
					return
				}
			} else {
				ch = &stableChannels[0]
			}
		} else {
			if isResponsesCompact {
				ch, err = service.SelectChannelByProtocol(c.Request.Context(), routingModel, protocolResponses)
			} else {
				ch, err = service.SelectChannel(c.Request.Context(), routingModel)
			}
			if err != nil {
				// 兜底：按 name 精确查找（兼容旧行为）
				ch, err = service.GetChannelByName(c.Request.Context(), routingModel)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在: " + routingModel})
					return
				}
				if isResponsesCompact && effectiveProtocol(ch) != protocolResponses {
					c.JSON(http.StatusNotFound, gin.H{"error": "对话压缩需要可用的 protocol=responses 渠道: " + routingModel})
					return
				}
			}
		}
	}

	llmProxyWithChannel(c, ch, reqData, userID, apiKeyIDVal, userGroup, triedIDs, stableChannels)
}

func getResponsesOperation(c *gin.Context) string {
	raw, ok := c.Get("responses_operation")
	if !ok {
		return ""
	}
	op, _ := raw.(string)
	return op
}

func shouldConvertRequestBody(clientProto, channelProto string, reqData map[string]interface{}) bool {
	if clientProto != channelProto {
		return true
	}
	if clientProto == protocolResponses && channelProto == protocolResponses {
		if msgs, ok := reqData["messages"].([]interface{}); ok && len(msgs) > 0 {
			return true
		}
	}
	return false
}

func isPoolKeyRetryStatus(statusCode int) bool {
	return statusCode == http.StatusGatewayTimeout || statusCode == 521
}

func isPoolKeyExhaustStatus(statusCode int) bool {
	return statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests
}

func appendPoolKeyID(ids []int64, id int64) []int64 {
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

func persistLLMUpstreamHeaders(corrID string, sentHeaders map[string]string) {
	if sentHeaders == nil {
		return
	}
	go func() {
		m := make(map[string]interface{}, len(sentHeaders))
		for k, v := range sentHeaders {
			m[k] = v
		}
		enqueueLLMLogPatch(corrID, []string{"upstream_headers"}, model.LLMLog{
			UpstreamHeaders: model.JSON(m),
		})
	}()
}

// llmProxyWithChannel 执行实际的上游请求，支持失败重试（换渠道）。
// stableChannels 非空时使用稳定模式（价格升序），否则使用正常负载均衡。
func llmProxyWithChannel(c *gin.Context, ch *model.Channel, reqData map[string]interface{},
	userID, apiKeyIDVal int64, userGroup string, triedIDs []int64, stableChannels []model.Channel) {

	const maxRetries = 3

	channelID := ch.ID
	triedIDs = append(triedIDs, channelID)

	// 捕获用户传入的路由键（用于专属模型积分扣减），必须在模型名覆盖之前保存
	routingKey, _ := reqData["model"].(string)

	// 用渠道配置的真实模型名覆盖用户传入的路由键
	if ch.Model != "" {
		reqData["model"] = ch.Model
	}
	// 在协议转换前保存模型名（Gemini 转换后 body 不含 model 字段，但 URL 替换需要用到）
	resolvedModel, _ := reqData["model"].(string)

	proto := effectiveProtocol(ch)
	responsesOperation := getResponsesOperation(c)
	isResponsesCompact := responsesOperation == responsesOperationCompact
	if isResponsesCompact && proto != protocolResponses {
		c.JSON(http.StatusBadRequest, gin.H{"error": "对话压缩需要 protocol=responses 的上游渠道"})
		return
	}

	// 获取客户端协议（由 LLMProxy/ClaudeProxy/GeminiProxy 写入 context）
	clientProto := protocolOpenAI
	if cp, ok := c.Get("client_proto"); ok {
		if s, ok := cp.(string); ok && s != "" {
			clientProto = s
		}
	}

	// isStream 必须在协议转换前读取（Gemini 转换后 body 不含 stream 字段）
	isStream, _ := reqData["stream"].(bool)

	// 保存原始客户端格式请求，用于：
	// 1. 计费估算（billing 读取 messages 字段，此字段在 Gemini 转换后不存在）
	// 2. 换渠道重试（下一渠道需要原始格式重新转换，而不是已转换格式）
	origReqData := make(map[string]interface{}, len(reqData))
	for k, v := range reqData {
		origReqData[k] = v
	}

	// 客户端格式 ≠ 渠道格式时，需要请求格式转换链：
	//   客户端格式 → OpenAI（若客户端本身就是 OpenAI 则跳过） → 渠道格式（若渠道本身是 OpenAI 则跳过）
	// 特例：客户端和渠道都是 responses 协议时，仍需归一化转换：
	//   客户端可能以 messages 格式（兼容 chat/completions）调用 /v1/responses，
	//   必须经 responsesToOpenAI → openAIToResponsesRequest 将 messages 转换为合法的 input 字段，
	//   否则原始 messages 体直接发往上游 Responses API 会导致 422/502。
	// passthrough_body=true 时跳过所有转换，直接使用原始请求体字节。
	needsConversion := !isResponsesCompact && shouldConvertRequestBody(clientProto, proto, reqData)
	if !ch.PassthroughBody && needsConversion && ch.RequestScript == "" {
		working := reqData
		// Step 1: 客户端格式 → OpenAI
		if clientProto != protocolOpenAI {
			norm, normErr := protocol.NormalizeClientRequest(working, clientProto)
			if normErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式转换错误: " + normErr.Error()})
				return
			}
			norm["model"] = resolvedModel
			working = norm
		}
		// Step 2: OpenAI → 渠道格式
		if proto != protocolOpenAI {
			conv, convErr := protocol.ConvertRequest(working, proto)
			if convErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "请求格式转换错误: " + convErr.Error()})
				return
			}
			working = conv
		}
		reqData = working
	}

	// 1. 号池 Sticky Key 分配（在 request_script 之前，以便脚本可用 poolKey 变量）
	entityID := apiKeyIDVal
	if entityID == 0 {
		entityID = userID
	}
	var poolKey *model.PoolKey
	if ch.KeyPoolID > 0 {
		pk, pkErr := service.GetOrAssignPoolKey(c.Request.Context(), ch.KeyPoolID, entityID)
		if pkErr != nil {
			service.RecordChannelError(c.Request.Context(), channelID)
			if len(triedIDs) < maxRetries {
				if nextCh := selectNextChannel(c.Request.Context(), routingKey, triedIDs, stableChannels, isResponsesCompact); nextCh != nil {
					llmProxyWithChannel(c, nextCh, origReqData, userID, apiKeyIDVal, userGroup, triedIDs, stableChannels)
					return
				}
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "key pool error: " + pkErr.Error()})
			return
		}
		poolKey = pk
	}
	poolKeyValue := ""
	if poolKey != nil {
		poolKeyValue = poolKey.Value
	}

	// 2. request_script（JS）映射（有脚本时跳过自动协议转换，由脚本自行处理）
	// passthrough_body=true 时也跳过脚本，确保请求体不被任何逻辑修改。
	mappedReq := reqData
	if !ch.PassthroughBody && ch.RequestScript != "" {
		mapped, scriptErr := script.RunMapRequest(ch.RequestScript, reqData, poolKeyValue)
		if scriptErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "入参映射错误: " + scriptErr.Error()})
			return
		}
		mappedReq = mapped
	}

	// 3a. Claude 渠道：补全必填字段（max_tokens 为 Claude API 强制要求，无论客户端格式如何）
	// passthrough_body=true 时跳过，避免破坏原始请求体的完整性签名。
	if !ch.PassthroughBody && proto == protocolClaude {
		if _, ok := mappedReq["max_tokens"]; !ok {
			mappedReq["max_tokens"] = 4096
		}
	}

	// 3b. 流式注入 include_usage（OpenAI 协议专用）
	// passthrough_body=true 时跳过，避免修改原始请求体。
	if !ch.PassthroughBody && isStream && proto == protocolOpenAI {
		mappedReq["stream"] = true
		if _, hasOpts := mappedReq["stream_options"]; !hasOpts {
			mappedReq["stream_options"] = map[string]interface{}{"include_usage": true}
		} else if opts, ok := mappedReq["stream_options"].(map[string]interface{}); ok {
			opts["include_usage"] = true
		}
	}

	// 4. 计算预扣金额（含用户分组定价）
	// 使用原始客户端格式请求（origReqData）：Gemini 转换后不含 messages 字段，会导致 token 估算为 0
	inputHold, outputHold, calcErr := billing.CalcForUser(ch, origReqData, userGroup)
	if calcErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "计费计算错误: " + calcErr.Error()})
		return
	}
	totalHold := inputHold + outputHold
	upstreamCostHold, _ := billing.CalcUpstreamCost(ch, origReqData)

	var modelCreditCharged int64
	var generalCreditCharged int64
	if totalHold > 0 {
		// 优先扣减专属模型积分，不足部分再扣通用余额
		if routingKey != "" {
			modelCreditCharged, _ = billing.ChargeModelCredit(c.Request.Context(), userID, routingKey, totalHold)
		}
		generalCreditCharged = totalHold - modelCreditCharged

		if generalCreditCharged > 0 {
			balanceBefore, balanceErr := billing.GetBalance(c.Request.Context(), userID)
			if balanceErr != nil {
				log.Printf("[llm-hold] get balance failed user_id=%d channel_id=%d channel=%q model=%q input_hold=%d output_hold=%d total_hold=%d err=%v",
					userID, channelID, ch.Name, resolvedModel, inputHold, outputHold, totalHold, balanceErr)
			} else {
				log.Printf("[llm-hold] try charge user_id=%d channel_id=%d channel=%q model=%q input_hold=%d output_hold=%d total_hold=%d balance_before=%d model_credit_charged=%d",
					userID, channelID, ch.Name, resolvedModel, inputHold, outputHold, totalHold, balanceBefore, modelCreditCharged)
			}
			if chargeErr := billing.Charge(c.Request.Context(), userID, generalCreditCharged); chargeErr != nil {
				// 退回已扣的模型积分
				if modelCreditCharged > 0 {
					_ = billing.RefundModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged)
				}
				if balanceErr != nil {
					log.Printf("[llm-hold] charge failed user_id=%d channel_id=%d channel=%q model=%q input_hold=%d output_hold=%d total_hold=%d err=%v",
						userID, channelID, ch.Name, resolvedModel, inputHold, outputHold, totalHold, chargeErr)
				} else {
					log.Printf("[llm-hold] charge failed user_id=%d channel_id=%d channel=%q model=%q input_hold=%d output_hold=%d total_hold=%d balance_before=%d err=%v",
						userID, channelID, ch.Name, resolvedModel, inputHold, outputHold, totalHold, balanceBefore, chargeErr)
				}
				c.JSON(http.StatusPaymentRequired, gin.H{"error": chargeErr.Error()})
				return
			}
		}
		// 记录扣款信息，供后续退款时优先还原
		c.Set("model_credit_routing_key", routingKey)
		c.Set("model_credit_charged", modelCreditCharged)
		c.Set("model_credit_general_charged", generalCreditCharged)
	}

	poolKeyIDVal := int64(0)
	if poolKey != nil {
		poolKeyIDVal = poolKey.ID
	}

	corrID := uuid.New().String()
	c.Header("X-Corr-Id", corrID)
	if totalHold > 0 {
		if err := service.WriteTx(c.Request.Context(), userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, "hold", totalHold, upstreamCostHold, modelCreditCharged, model.JSON{
			"input_hold":  inputHold,
			"output_hold": outputHold,
			"user_group":  userGroup,
		}); err != nil {
			if modelCreditCharged > 0 {
				_ = billing.RefundModelCredit(c.Request.Context(), userID, routingKey, modelCreditCharged)
			}
			log.Printf("[llm-hold] write transaction failed user_id=%d channel_id=%d corr_id=%s err=%v", userID, channelID, corrID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "计费流水写入失败，请稍后重试"})
			return
		}
	}

	// 5. 写入 LLM 请求日志
	modelName := resolvedModel
	// 预先计算实际上游 URL（与 sendLLMRequest 中逻辑保持一致）
	poolKeyBaseURL := ""
	if poolKey != nil {
		poolKeyBaseURL = poolKey.BaseURLOverride
	}
	upstreamURL := resolveLLMTargetURL(upstream.BaseURLForPoolKey(ch.BaseURL, poolKeyBaseURL), modelName, isStream, responsesOperation)
	upstreamMethod := ch.Method
	if upstreamMethod == "" {
		upstreamMethod = "POST"
	}
	inputPricePer1M, outputPricePer1M := resolveTokenPriceMetaValue(ch, userGroup)
	llmLog := &model.LLMLog{
		UserID:                 userID,
		ChannelID:              channelID,
		APIKeyID:               apiKeyIDVal,
		CorrID:                 corrID,
		Model:                  modelName,
		InputPricePer1MTokens:  inputPricePer1M,
		OutputPricePer1MTokens: outputPricePer1M,
		IsStream:               isStream,
		Transport: func() string {
			if isStream {
				return "sse"
			}
			return "http"
		}(),
		UpstreamURL:     upstreamURL,
		UpstreamMethod:  upstreamMethod,
		UpstreamRequest: model.JSON(mappedReq),
		ClientRequest:   model.JSON(origReqData), // 用户原始请求（协议转换前）
		Status:          "pending",
	}
	enqueueLLMLogInsert(*llmLog)

	// 6. 号池 Key 已在步骤1分配，直接发送上游请求
	// 7. 发送上游请求
	triedPoolKeyIDs := make([]int64, 0, 4)
	if poolKey != nil {
		triedPoolKeyIDs = appendPoolKeyID(triedPoolKeyIDs, poolKey.ID)
	}
	sentHeaders, resp, err := sendLLMRequest(c, ch, mappedReq, poolKey, proto, resolvedModel, isStream, responsesOperation)
	persistLLMUpstreamHeaders(corrID, sentHeaders)

	if err == nil && resp != nil {
		// 401/403/429 视为当前号池 Key 失效或额度不可用：临时摘除该 Key，并在同池内换 Key 重试。
		for err == nil && resp != nil && isPoolKeyExhaustStatus(resp.StatusCode) &&
			ch.KeyPoolID > 0 && poolKey != nil && len(triedPoolKeyIDs) < maxPoolKeyExhaustRetries {
			resp.Body.Close()
			newKey, rotErr := service.MarkExhaustedAndRotate(c.Request.Context(), ch.KeyPoolID, poolKey.ID, entityID)
			if rotErr != nil || newKey == nil {
				break
			}
			poolKey = newKey
			poolKeyIDVal = newKey.ID // 更新 poolKeyIDVal，确保后续结算流水关联正确的号商
			triedPoolKeyIDs = appendPoolKeyID(triedPoolKeyIDs, newKey.ID)
			sentHeaders, resp, err = sendLLMRequest(c, ch, mappedReq, poolKey, proto, resolvedModel, isStream, responsesOperation)
			persistLLMUpstreamHeaders(corrID, sentHeaders)
			if err != nil {
				service.RecordChannelError(c.Request.Context(), channelID)
				llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, 0, "上游请求失败(重试): "+err.Error())
				return
			}
		}

		// 521/504 先在同号池内尝试其它 Key；池内都不可用后再交给原有渠道级重试策略。
		for err == nil && resp != nil && isPoolKeyRetryStatus(resp.StatusCode) && ch.KeyPoolID > 0 && poolKey != nil {
			newKey, rotErr := service.RotatePoolKeySkipping(c.Request.Context(), ch.KeyPoolID, entityID, triedPoolKeyIDs)
			if rotErr != nil || newKey == nil {
				break
			}
			resp.Body.Close()
			poolKey = newKey
			poolKeyIDVal = newKey.ID
			triedPoolKeyIDs = appendPoolKeyID(triedPoolKeyIDs, newKey.ID)
			sentHeaders, resp, err = sendLLMRequest(c, ch, mappedReq, poolKey, proto, resolvedModel, isStream, responsesOperation)
			persistLLMUpstreamHeaders(corrID, sentHeaders)
		}
	}

	if err != nil {
		service.RecordChannelError(c.Request.Context(), channelID)
		// 尝试换渠道重试
		if len(triedIDs) < maxRetries {
			if nextCh := selectNextChannel(c.Request.Context(), routingKey, triedIDs, stableChannels, isResponsesCompact); nextCh != nil {
				// 退回已扣的 hold
				if totalHold > 0 {
					if !refundLLMHoldForRetry(c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, totalHold, upstreamCostHold, "channel_retry") {
						return
					}
					enqueueLLMLogPatch(corrID, []string{"status", "error_msg"}, model.LLMLog{Status: "error", ErrorMsg: "channel retry"})
				}
				llmProxyWithChannel(c, nextCh, origReqData, userID, apiKeyIDVal, userGroup, triedIDs, stableChannels)
				return
			}
		}
		llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, 0, "上游请求失败: "+err.Error())
		return
	}

	if resp == nil {
		llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, 0, "上游请求失败: empty response")
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyErr, _ := io.ReadAll(resp.Body)
		service.RecordChannelError(c.Request.Context(), channelID)

		// 跑 error_script 识别业务错误（含 fatal=余额不足等永久故障）。
		// 4xx body 通常是 JSON，5xx 也常带结构化错误；非 JSON 时跳过。
		var bizErr string
		var fatal bool
		var bodyJSON map[string]interface{}
		if json.Unmarshal(bodyErr, &bodyJSON) == nil && bodyJSON != nil {
			if ch.ErrorScript != "" {
				bizErr, fatal, _ = script.RunCheckError(ch.ErrorScript, bodyJSON)
			}
			if bizErr == "" {
				if detected, ok := script.DetectUpstreamError(bodyJSON); ok {
					bizErr = detected
				}
			}
		}
		if fatal {
			_ = service.PatchChannelActive(c.Request.Context(), channelID, false)
			log.Printf("[llm] disable channel id=%d fatal_err=%q status=%d", channelID, bizErr, resp.StatusCode)
			go func(name string, id int64, reason string) {
				// 防止通知失败阻塞主流程
				defer func() { recover() }()
				if err := notify.SendLarkChannelDisabled(name, id, reason); err != nil {
					log.Printf("[lark notify] failed: %v", err)
				}
			}(ch.Name, ch.ID, bizErr)
		}

		// 5xx 总是尝试换渠道；4xx 仅在 error_script 命中时换。
		// 号池 Key 级错误在同池重试后仍存在时，也交给渠道级重试。
		poolKeyRetryExhausted := ch.KeyPoolID > 0 && poolKey != nil && isPoolKeyExhaustStatus(resp.StatusCode)
		shouldRetry := resp.StatusCode >= 500 || bizErr != "" || poolKeyRetryExhausted
		if shouldRetry && len(triedIDs) < maxRetries {
			if nextCh := selectNextChannel(c.Request.Context(), routingKey, triedIDs, stableChannels, isResponsesCompact); nextCh != nil {
				if totalHold > 0 {
					if !refundLLMHoldForRetry(c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, totalHold, upstreamCostHold, "channel_retry") {
						return
					}
					retryMsg := "channel retry"
					if bizErr != "" {
						retryMsg = "channel retry: " + bizErr
					} else if poolKeyRetryExhausted {
						retryMsg = fmt.Sprintf("channel retry: pool key returned %d", resp.StatusCode)
					}
					enqueueLLMLogPatch(corrID, []string{"status", "error_msg"}, model.LLMLog{Status: "error", ErrorMsg: retryMsg})
				}
				llmProxyWithChannel(c, nextCh, origReqData, userID, apiKeyIDVal, userGroup, triedIDs, stableChannels)
				return
			}
		}

		abortMsg := bizErr
		if abortMsg == "" {
			abortMsg = fmt.Sprintf("上游返回 %d: %s", resp.StatusCode, string(bodyErr))
		}
		llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, resp.StatusCode, abortMsg)
		return
	}

	service.RecordChannelSuccess(c.Request.Context(), channelID)

	// ---- 同步响应 ----
	if !isStream {
		respBytes, _ := io.ReadAll(resp.Body)
		if !isResponsesCompact {
			if converted, detected, convErr := protocol.ConvertSSEToSyncResponse(respBytes, proto); detected {
				if convErr != nil {
					service.RecordChannelError(c.Request.Context(), channelID)
					llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, http.StatusOK, "上游响应格式错误: "+convErr.Error())
					return
				}
				respBytes = converted
			}
		}

		// 先解析原始上游响应（格式与 proto 一致），用于 usage 提取、error_script 检测和日志记录。
		// 必须在协议转换之前解析，否则 Claude/Gemini 的 usage 字段名会被改写为 OpenAI 格式，
		// 导致 NormalizeUsage 找不到对应字段而返回 nil，触发错误的全额退款。
		var origRespJSON map[string]interface{}
		_ = json.Unmarshal(respBytes, &origRespJSON)

		if origRespJSON != nil {
			// 200 但 body 内含 error：优先跑 error_script，未命中时走通用 OpenAI error 检测。
			bizErr := ""
			fatal := false
			if ch.ErrorScript != "" {
				if detected, fatalDetected, scriptErr := script.RunCheckError(ch.ErrorScript, origRespJSON); scriptErr == nil {
					bizErr = detected
					fatal = fatalDetected
				}
			}
			if bizErr == "" {
				if detected, ok := script.DetectUpstreamError(origRespJSON); ok {
					bizErr = detected
				}
			}
			if bizErr != "" {
				service.RecordChannelError(c.Request.Context(), channelID)
				if fatal {
					_ = service.PatchChannelActive(c.Request.Context(), channelID, false)
					log.Printf("[llm] disable channel id=%d fatal_err=%q status=200", channelID, bizErr)
				}
				if len(triedIDs) < maxRetries {
					if nextCh := selectNextChannel(c.Request.Context(), routingKey, triedIDs, stableChannels, isResponsesCompact); nextCh != nil {
						if totalHold > 0 {
							if !refundLLMHoldForRetry(c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, totalHold, upstreamCostHold, "channel_retry") {
								return
							}
							enqueueLLMLogPatch(corrID, []string{"status", "error_msg"}, model.LLMLog{Status: "error", ErrorMsg: "channel retry: " + bizErr})
						}
						llmProxyWithChannel(c, nextCh, origReqData, userID, apiKeyIDVal, userGroup, triedIDs, stableChannels)
						return
					}
				}
				llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, http.StatusOK, bizErr)
				return
			}
		}

		// 从原始上游响应提取 usage（使用渠道原始格式，在任何协议转换之前）
		syncUsage := protocol.NormalizeUsage(origRespJSON, proto)

		// 响应格式转换链：渠道格式 → OpenAI → 客户端格式
		// 客户端格式 == 渠道格式时直接透传；有 response_script 时跳过自动转换。
		if clientProto != proto && ch.ResponseScript == "" {
			// Step 1: 渠道格式 → OpenAI
			if proto != protocolOpenAI {
				if converted, convErr := protocol.ConvertSyncResponse(respBytes, proto); convErr == nil {
					respBytes = converted
				}
			}
			// Step 2: OpenAI → 客户端格式
			if clientProto != protocolOpenAI {
				if converted, convErr := protocol.ConvertResponseToClient(respBytes, clientProto); convErr == nil {
					respBytes = converted
				}
			}
		}

		c.Data(http.StatusOK, "application/json", respBytes)

		var clientRespJSON map[string]interface{}
		_ = json.Unmarshal(respBytes, &clientRespJSON)
		enqueueLLMLogPatch(corrID, []string{"upstream_status", "upstream_response", "client_response"}, model.LLMLog{
			UpstreamStatus:   http.StatusOK,
			UpstreamResponse: model.JSON(origRespJSON),
			ClientResponse:   model.JSON(clientRespJSON),
		})

		llmSettle(c, ch, origReqData, syncUsage, totalHold, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, userGroup)
		return
	}

	// ---- 流式 SSE 响应 ----
	// error_script 业务错误检测：许多上游在额度耗尽时即使请求了流式也会立即以 JSON 返回错误。
	// 在写出任何响应字节前 peek 第一行，若匹配 error_script 则换渠道重试。
	if ch.ErrorScript != "" {
		peekBuf := bufio.NewReader(resp.Body)
		firstLineBytes, peekErr := peekBuf.ReadBytes('\n')
		if (peekErr == nil || len(firstLineBytes) > 0) && len(firstLineBytes) > 0 {
			firstLine := strings.TrimRight(string(firstLineBytes), "\r\n")
			checkSrc := strings.TrimPrefix(firstLine, "data: ")
			if checkSrc != "" && checkSrc != "[DONE]" {
				var firstJSON map[string]interface{}
				if json.Unmarshal([]byte(checkSrc), &firstJSON) == nil {
					bizErr := ""
					fatal := false
					if detected, fatalDetected, scriptErr := script.RunCheckError(ch.ErrorScript, firstJSON); scriptErr == nil {
						bizErr = detected
						fatal = fatalDetected
					}
					if bizErr == "" {
						if detected, ok := script.DetectUpstreamError(firstJSON); ok {
							bizErr = detected
						}
					}
					if bizErr != "" {
						service.RecordChannelError(c.Request.Context(), channelID)
						if fatal {
							_ = service.PatchChannelActive(c.Request.Context(), channelID, false)
							log.Printf("[llm] disable channel id=%d fatal_err=%q status=200(stream)", channelID, bizErr)
						}
						if len(triedIDs) < maxRetries {
							if nextCh := selectNextChannel(c.Request.Context(), routingKey, triedIDs, stableChannels, isResponsesCompact); nextCh != nil {
								if totalHold > 0 {
									if !refundLLMHoldForRetry(c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, totalHold, upstreamCostHold, "channel_retry") {
										return
									}
									enqueueLLMLogPatch(corrID, []string{"status", "error_msg"}, model.LLMLog{Status: "error", ErrorMsg: "channel retry: " + bizErr})
								}
								llmProxyWithChannel(c, nextCh, origReqData, userID, apiKeyIDVal, userGroup, triedIDs, stableChannels)
								return
							}
						}
						llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, http.StatusOK, bizErr)
						return
					}
				}
			}
			// 第一行正常：将其拼回，后续 scanner 照常读取
			resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(string(firstLineBytes)), peekBuf))
		}
	} else {
		peekBuf := bufio.NewReader(resp.Body)
		firstLineBytes, peekErr := peekBuf.ReadBytes('\n')
		if (peekErr == nil || len(firstLineBytes) > 0) && len(firstLineBytes) > 0 {
			firstLine := strings.TrimRight(string(firstLineBytes), "\r\n")
			checkSrc := strings.TrimPrefix(firstLine, "data: ")
			if checkSrc != "" && checkSrc != "[DONE]" {
				var firstJSON map[string]interface{}
				if json.Unmarshal([]byte(checkSrc), &firstJSON) == nil {
					if bizErr, ok := script.DetectUpstreamError(firstJSON); ok {
						service.RecordChannelError(c.Request.Context(), channelID)
						if len(triedIDs) < maxRetries {
							if nextCh := selectNextChannel(c.Request.Context(), routingKey, triedIDs, stableChannels, isResponsesCompact); nextCh != nil {
								if totalHold > 0 {
									if !refundLLMHoldForRetry(c, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, totalHold, upstreamCostHold, "channel_retry") {
										return
									}
									enqueueLLMLogPatch(corrID, []string{"status", "error_msg"}, model.LLMLog{Status: "error", ErrorMsg: "channel retry: " + bizErr})
								}
								llmProxyWithChannel(c, nextCh, origReqData, userID, apiKeyIDVal, userGroup, triedIDs, stableChannels)
								return
							}
						}
						llmRefundAndAbort(c, corrID, userID, totalHold, upstreamCostHold, poolKeyIDVal, http.StatusOK, bizErr)
						return
					}
				}
			}
			resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(string(firstLineBytes)), peekBuf))
		}
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	usage := &usageState{protocol: proto}
	// SSE 格式转换器：客户端格式 != 渠道格式时需要转换 SSE 流
	var sseConv protocol.SSEConverter
	if clientProto != proto {
		sseConv = protocol.NewSSEConverter(proto, clientProto)
	}
	var responsesFilter *responsesPassthroughSSEFilter
	if sseConv == nil && clientProto == protocolResponses && proto == protocolResponses {
		responsesFilter = &responsesPassthroughSSEFilter{}
	}

	// 收集上游原始 SSE 行用于日志，超过 200KB 后停止收集但继续推流。
	const maxSSELogBytes = 200 * 1024
	var rawSSELines []string
	var rawSSEBytes int

	scanner := bufio.NewScanner(resp.Body)
	// Gemini thinking 模型的单个 SSE data: 行可能远超 64 KB（Go Scanner 默认 token 上限），
	// 超出会使 scanner.Scan() 停止并静默截断流。此处将上限提升至 10 MB。
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var streamReadErr error
	c.Stream(func(w io.Writer) bool {
		if !scanner.Scan() {
			streamReadErr = scanner.Err()
			if sseConv != nil {
				for _, l := range sseConv.Flush() {
					fmt.Fprintf(w, "%s\n", l)
				}
			} else if responsesFilter != nil {
				for _, l := range responsesFilter.Flush() {
					fmt.Fprintf(w, "%s\n", l)
				}
			}
			return false
		}
		line := scanner.Text()
		usage.processLine(line) // 始终用上游格式解析，保证计费准确
		if rawSSEBytes < maxSSELogBytes {
			rawSSELines = append(rawSSELines, line)
			rawSSEBytes += len(line) + 1
		}
		if sseConv != nil {
			for _, l := range sseConv.Convert(line) {
				fmt.Fprintf(w, "%s\n", l)
			}
		} else if responsesFilter != nil {
			for _, l := range responsesFilter.Convert(line) {
				fmt.Fprintf(w, "%s\n", l)
			}
		} else {
			fmt.Fprintf(w, "%s\n", line)
		}
		return true
	})

	enqueueLLMLogPatch(corrID, []string{"upstream_status", "upstream_response", "client_response"}, model.LLMLog{
		UpstreamStatus:   http.StatusOK,
		UpstreamResponse: model.JSON{"lines": rawSSELines},
		ClientResponse:   buildStreamClientResponse(rawSSELines, proto),
	})

	if streamReadErr != nil {
		service.RecordChannelError(c.Request.Context(), channelID)
		log.Printf("[llm] stream read error corr_id=%s channel_id=%d err=%v", corrID, channelID, streamReadErr)
		enqueueLLMLogPatch(corrID, []string{"status", "error_msg"}, model.LLMLog{Status: "error", ErrorMsg: streamReadErr.Error()})
	}

	llmSettle(c, ch, origReqData, usage.normalized(origReqData), totalHold, userID, channelID, apiKeyIDVal, poolKeyIDVal, corrID, userGroup)
}
