// @title           FANAPI
// @version         1.0
// @description     LLM 对话 · 图片 / 视频 / 音频生成 · 任务查询
// @host            localhost:8080
// @schemes         http https
// @securityDefinitions.apikey ApiKeyAuth
// @in              header
// @name            X-API-Key
package main

import (
	"context"
	"fmt"
	"log"

	_ "fanapi/docs"
	"fanapi/internal/billing"
	"fanapi/internal/cache"
	"fanapi/internal/config"
	"fanapi/internal/db"
	"fanapi/internal/handler"
	"fanapi/internal/middleware"
	"fanapi/internal/mq"
	"fanapi/internal/service"
	"fanapi/internal/taskresult"
	"fanapi/pkg/mailer"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := db.Init(&cfg.DB, true); err != nil {
		log.Fatalf("db: %v", err)
	}
	log.Println("db connected")

	if err := cache.Init(&cfg.Redis); err != nil {
		log.Fatalf("redis: %v", err)
	}
	log.Println("redis connected")

	// 启动时清除渠道缓存，确保 poller/worker 使用 DB 中最新的脚本和配置
	if keys, err := cache.Client.Keys(context.Background(), "channel:*").Result(); err == nil && len(keys) > 0 {
		cache.Client.Del(context.Background(), keys...)
		log.Printf("cleared %d channel cache keys on startup", len(keys))
	}

	if err := mq.Init(&cfg.NATS); err != nil {
		log.Fatalf("nats: %v", err)
	}
	log.Println("nats connected")
	if err := mq.EnsureStream(); err != nil {
		log.Fatalf("nats ensure stream: %v", err)
	}

	_ = billing.SyncBalanceToRedis // 预留：可在启动时手动同步余额到 Redis

	// 启动结果处理器：订阅 RESULTS 流，写入 DB 并完成计费结算
	if err := taskresult.StartResultProcessor(cfg.Worker); err != nil {
		log.Fatalf("result processor: %v", err)
	}

	// 启动异步任务轮询器（轮询 DB 中含 upstream_task_id 的 processing 状态任务）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	taskresult.StartBatchWriter(ctx)
	taskresult.StartPoller(ctx)

	// 启动上游余额自动同步与低余额告警（每 10 秒）
	handler.StartUpstreamBalanceMonitor(ctx)
	handler.StartUpstreamCostMonitor(ctx)

	// 启动 OCPC 定时上报调度器
	service.StartOcpcScheduler(ctx)

	m := mailer.New(&cfg.SMTP)
	authH := handler.NewAuthHandler(&cfg.Server, m)
	vendorH := handler.NewVendorHandler(&cfg.Server)

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Static("/uploads", "uploads")

	// 健康检查（无需认证）
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })

	// Swagger 接口文档静态资源
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	// OpenAPI spec（动态替换域名）
	r.GET("/openapi.json", handler.SwaggerJSON)
	r.GET("/openapi-user.json", handler.UserSwaggerJSON)

	// API 文档页面（无需认证）
	r.GET("/docs", handler.APIDocs)

	// 公开接口（无需认证）
	r.GET("/public/channels", authH.ListModels)
	r.GET("/public/settings", handler.GetPublicSettings)

	// Epay 回调（无需用户认证，Epay 平台回调）
	r.GET("/pay/epay/callback", handler.EpayCallback)
	r.POST("/pay/epay/callback", handler.EpayCallback)

	// 中台支付回调（无需用户认证，支付中台回调）
	r.POST("/pay/apply/notify", handler.PayApplyNotify)
	// 收钱吧支付回调（无需用户认证，收钱吧回调）
	r.POST("/pay/shouqianba/notify", handler.ShouqianbaNotify)

	// 公开认证路由（注册/登录/发验证码等）
	auth := r.Group("/auth")
	{
		auth.POST("/send-code", authH.SendCode)
		auth.POST("/register", authH.Register)
		auth.POST("/login", authH.Login)
		auth.POST("/forgot-password", authH.ForgotPassword)
		auth.POST("/reset-password", authH.ResetPassword)
	}

	// 号商认证路由（公开）
	vendorAuth := r.Group("/vendor/auth")
	{
		vendorAuth.POST("/register", vendorH.Register)
		vendorAuth.POST("/login", vendorH.Login)
	}

	// 号商门户（需要 vendor JWT）
	vendorPortal := r.Group("/vendor")
	vendorPortal.Use(middleware.VendorAuth(&cfg.Server))
	{
		vendorPortal.GET("/profile", vendorH.GetProfile)
		vendorPortal.GET("/keys", vendorH.GetPoolKeys)
		vendorPortal.POST("/keys", vendorH.SubmitKey)
		vendorPortal.GET("/pools", vendorH.GetSubmittablePools)
	}

	// 需认证的用户路由（JWT 或 API Key）
	authed := r.Group("/")
	authed.Use(middleware.Auth(&cfg.Server))
	{
		authed.POST("/upload/image", handler.UploadImage)
		user := authed.Group("/user")
		{
			user.GET("/profile", authH.GetProfile)
			user.GET("/balance", authH.GetBalance)
			user.GET("/transactions", authH.GetTransactions)
			user.GET("/stats", authH.GetUserStats)
			user.GET("/model-credits", authH.GetModelCredits)
			user.GET("/channels", authH.ListModels)
			user.GET("/apikeys", authH.ListAPIKeys)
			user.POST("/apikeys", authH.CreateAPIKey)
			user.DELETE("/apikeys/:id", authH.DeleteAPIKey)
			user.PUT("/password", authH.ChangePassword)
			user.POST("/bind-email", authH.BindEmail)
			user.POST("/reference-images", handler.UploadReferenceImage)
			user.POST("/cards/redeem", handler.RedeemCard)
			user.GET("/cards/redeem-history", handler.GetRedeemHistory)
			user.GET("/payment-orders", handler.GetUserPaymentOrders)
			user.GET("/invite", handler.GetInviteInfo)
			user.GET("/invite/list", handler.GetInviteeList)
			user.POST("/invite/convert", handler.ConvertFrozenBalance)
			user.GET("/payment-qr", handler.GetPaymentQR)
			user.PUT("/payment-qr", handler.SavePaymentQR)
			user.POST("/withdraw", handler.SubmitWithdraw)
			user.GET("/withdraw/history", handler.ListWithdrawHistory)
			user.GET("/coupons/validate", handler.ValidateCoupon)
		}

		// 管理员路由（JWT 或 API Key + admin 角色）
		admin := authed.Group("/admin")
		admin.Use(middleware.Admin())
		{
			admin.POST("/channels", handler.CreateChannel)
			admin.GET("/channels", handler.ListChannels)
			admin.PUT("/channels/:id", handler.UpdateChannel)
			admin.PATCH("/channels/:id/active", handler.PatchChannelActive)
			admin.DELETE("/channels/:id", handler.DeleteChannel)
			// 号池管理
			admin.GET("/key-pools", handler.ListKeyPools)
			admin.POST("/key-pools", handler.CreateKeyPool)
			admin.DELETE("/key-pools/:id", handler.DeleteKeyPool)
			admin.PATCH("/key-pools/:id/toggle", handler.ToggleKeyPool)
			admin.PATCH("/key-pools/:id/vendor-toggle", handler.ToggleVendorSubmittable)
			admin.GET("/key-pools/:id/keys", handler.ListPoolKeys)
			admin.POST("/key-pools/:id/keys", handler.AddPoolKey)
			admin.POST("/key-pools/:id/keys/import", handler.ImportPoolKeys)
			admin.POST("/key-pools/:id/sync-upstream", handler.SyncKeyPoolFromUpstream)
			admin.GET("/key-pools/:id/channels", handler.GetKeyPoolChannels)
			admin.DELETE("/pool-keys/:id", handler.RemovePoolKey)
			admin.PATCH("/pool-keys/:id", handler.UpdatePoolKey)
			admin.PATCH("/pool-keys/:id/vendor", handler.AdminSetPoolKeyVendor)
			admin.GET("/users", handler.ListUsers)
			admin.POST("/users", handler.CreateUser)
			admin.POST("/users/batch", handler.BatchUpdateUsers)
			admin.DELETE("/users/:id", handler.DeleteUser)
			admin.POST("/users/:id/recharge", handler.Recharge)
			admin.POST("/users/:id/model-credits", handler.GrantModelCredit)
			admin.GET("/users/:id/model-credits", handler.AdminListModelCredits)
			admin.PUT("/users/:id/password", handler.ResetUserPassword)
			admin.PUT("/users/:id/group", handler.SetUserGroup)
			admin.PUT("/users/:id/role", handler.SetUserRole)
			admin.PUT("/users/:id/rebate-ratio", handler.SetUserRebateRatio)
			admin.PATCH("/users/:id/freeze", handler.FreezeUser)
			admin.GET("/transactions", handler.ListAllTransactions)
			admin.GET("/tasks", handler.ListTasks)
			admin.GET("/tasks/:id", handler.GetAdminTask)
			admin.GET("/cleanup/preview", handler.AdminPreviewCleanup)
			admin.POST("/cleanup/run", handler.AdminRunCleanup)
			admin.GET("/stats", handler.GetAdminStats)
			admin.GET("/stats/trend", handler.GetAdminStatsTrend)
			admin.GET("/stats/top", handler.GetAdminStatsTop)
			// 卡密管理
			admin.POST("/cards/generate", handler.GenerateCards)
			admin.GET("/cards", handler.ListCards)
			admin.DELETE("/cards/:id", handler.DeleteCard)
			// LLM 日志
			admin.GET("/llm-logs", handler.AdminListLLMLogs)
			admin.GET("/llm-logs/:id", handler.AdminGetLLMLog)
			// 系统设置
			admin.GET("/settings", handler.GetSettings)
			admin.PUT("/settings", handler.UpdateSettings)
			admin.POST("/verify-password", handler.AdminVerifyPassword)
			// 管理员个人信息 & 权限
			admin.GET("/me", handler.GetAdminMe)
			// OCPC 转化上报 + 平台账户管理
			admin.POST("/ocpc/upload", handler.TriggerOcpcUpload)
			admin.GET("/ocpc/schedule", handler.GetOcpcSchedule)
			admin.PUT("/ocpc/schedule", handler.UpdateOcpcSchedule)
			admin.GET("/ocpc/platforms", handler.ListOcpcPlatforms)
			admin.POST("/ocpc/platforms", handler.CreateOcpcPlatform)
			admin.PUT("/ocpc/platforms/:id", handler.UpdateOcpcPlatform)
			admin.DELETE("/ocpc/platforms/:id", handler.DeleteOcpcPlatform)
			admin.PATCH("/ocpc/platforms/:id/toggle", handler.ToggleOcpcPlatform)
			// 号商管理
			admin.GET("/vendors", handler.AdminListVendors)
			admin.PATCH("/vendors/:id", handler.AdminUpdateVendor)
			// 提现管理
			admin.GET("/withdrawals", handler.AdminListWithdrawals)
			admin.GET("/withdrawals/pending-count", handler.AdminPendingWithdrawCount)
			admin.POST("/withdrawals/:id/approve", handler.AdminApproveWithdrawal)
			admin.POST("/withdrawals/:id/reject", handler.AdminRejectWithdrawal)
			admin.POST("/withdrawals/:id/cs-approve", handler.AdminCsApproveWithdrawal)
			admin.POST("/withdrawals/:id/proof", handler.AdminUploadWithdrawalProof)

			// ── Superpower 扩展路由 ──────────────────────────────────
			// 渠道批量 + 健康 + 变更日志
			admin.POST("/channels/batch", handler.BatchUpdateChannels)
			admin.GET("/channels/:id/health", handler.GetChannelHealth)
			admin.GET("/channels/:id/logs", handler.ListChannelLogs)
			admin.GET("/channels/:id/upstream-cost", handler.PreviewChannelUpstreamCost)
			admin.POST("/channels/:id/sync-upstream-cost", handler.SyncChannelUpstreamCost)

			// 用户画像 + 风控标签 + API Key 总览
			admin.GET("/users/:id/referrals", handler.GetUserReferrals)
			admin.GET("/users/:id/portrait", handler.GetUserPortrait)
			admin.GET("/users/:id/operation-log", handler.GetUserOperationLog)
			admin.POST("/users/:id/risk-labels", handler.AddRiskLabel)
			admin.DELETE("/risk-labels/:id", handler.DeleteRiskLabel)
			admin.GET("/api-keys", handler.AdminListAPIKeys)
			admin.PATCH("/api-keys/:id/revoke", handler.RevokeAPIKey)

			// 账单聚合 + 手动调账
			admin.GET("/transactions/aggregate", handler.GetTransactionAggregate)
			admin.POST("/transactions/adjust", handler.AdjustTransaction)

			// 卡密批次
			admin.GET("/cards/batches", handler.ListCardBatches)
			admin.POST("/cards/:id/void", handler.VoidCard)
			admin.POST("/cards/batches/:batch_id/void", handler.VoidCardBatch)

			// 审计日志
			admin.GET("/audit", handler.ListAuditLogs)

			// 通知中心
			admin.GET("/notifications", handler.ListNotifications)
			admin.POST("/notifications", handler.CreateNotification)
			admin.POST("/notifications/:id/send", handler.SendNotification)
			admin.DELETE("/notifications/:id", handler.DeleteNotification)

			// 告警中心
			admin.GET("/alerts", handler.ListAlerts)
			admin.PATCH("/alerts/:id/ack", handler.AckAlert)
			admin.PATCH("/alerts/:id/resolve", handler.ResolveAlert)

			// 数据导出中心
			admin.GET("/exports", handler.ListExportTasks)
			admin.POST("/exports", handler.CreateExportTask)

			// 上游平台管理
			admin.GET("/upstream-platforms", handler.ListUpstreamPlatforms)
			admin.POST("/upstream-platforms", handler.CreateUpstreamPlatform)
			admin.PUT("/upstream-platforms/:id", handler.UpdateUpstreamPlatform)
			admin.DELETE("/upstream-platforms/:id", handler.DeleteUpstreamPlatform)
			admin.GET("/upstream-platforms/:id/models", handler.GetUpstreamModels)
			admin.GET("/upstream-platforms/:id/channel-bindings/preview", handler.PreviewUpstreamPlatformChannelBindings)
			admin.POST("/upstream-platforms/:id/bind-channels", handler.BindUpstreamPlatformChannels)
			admin.POST("/upstream-platforms/:id/sync-balance", handler.SyncUpstreamPlatformBalance)
			admin.POST("/upstream-platforms/:id/sync-channels", handler.SyncUpstreamPlatformChannels)
			admin.POST("/upstream-platforms/:id/api-keys", handler.CreateUpstreamAPIKey)
			admin.POST("/channels/batch-from-upstream", handler.BatchCreateChannelsFromUpstream)

			// RBAC 角色管理
			admin.GET("/roles", handler.ListRoles)
			admin.POST("/roles", handler.CreateRole)
			admin.PUT("/roles/:id", handler.UpdateRole)
			admin.DELETE("/roles/:id", handler.DeleteRole)

			// 管理员账号 & 角色分配
			admin.GET("/admins", handler.ListAdminUsers)
			admin.PUT("/admins/:id/roles", handler.SetAdminRoles)

			// 优惠券管理
			admin.GET("/coupons", handler.ListCoupons)
			admin.POST("/coupons", handler.CreateCoupon)
			admin.DELETE("/coupons/:id", handler.VoidCoupon)
			admin.GET("/coupons/:id/uses", handler.ListCouponUses)

			// 客户充值明细
			admin.GET("/payments", handler.AdminListPaymentOrders)

			// 系统设置操作日志
			admin.GET("/settings/logs", handler.ListSettingLogs)
		}

		// Epay 充值（需要 JWT 认证）
		authed.POST("/pay/epay/create", handler.CreateEpayOrder)

		// 中台支付（需要 JWT 认证）
		authed.POST("/pay/apply/create", handler.CreatePayApplyOrder)
		authed.POST("/pay/shouqianba/create", handler.CreateShouqianbaOrder)
		authed.GET("/pay/order/status", handler.GetPaymentOrderStatus)

		// 客服端路由（JWT + agent 或 admin 角色）
		agentGrp := authed.Group("/agent")
		agentGrp.Use(middleware.Agent())
		{
			agentGrp.GET("/users", handler.AgentListUsers)
			agentGrp.POST("/users/:id/recharge", handler.AgentRecharge)
			agentGrp.GET("/invite", handler.AgentGetInvite)
			agentGrp.PUT("/wechat-qr", handler.AgentUpdateWechatQR)
		}

		// 用户任务查询（支持 JWT 或 API Key）
		authed.GET("/v1/tasks", handler.ListUserTasks)
		authed.DELETE("/v1/tasks/history", handler.DeleteUserTaskHistory)
		authed.GET("/v1/tasks/:id", handler.GetTask)
		authed.GET("/v1/tasks/:id/billing", handler.GetTaskBilling)
		authed.GET("/v1/llm-logs", handler.UserListLLMLogs)
		authed.GET("/v1/llm-logs/:id", handler.UserGetLLMLog)
		authed.GET("/v1/conversations", handler.ListConversations)
		authed.POST("/v1/conversations", handler.SaveConversation)
		authed.DELETE("/v1/conversations/:id", handler.DeleteConversation)

		// 公开 API（需要 API Key）
		v1 := authed.Group("/v1")
		v1.Use(middleware.APIKeyOnly())
		{
			v1.GET("/models", handler.OpenAIModels)                      // OpenAI 兼容模型列表
			v1.POST("/chat/completions", handler.LLMProxy)               // OpenAI 兼容格式
			v1.POST("/messages", handler.ClaudeProxy)                    // Claude 原生格式
			v1.POST("/responses", handler.ResponsesProxy)                // OpenAI Responses API（SSE / 同步）
			v1.POST("/responses/compact", handler.ResponsesCompactProxy) // Codex 对话压缩兼容
			v1.GET("/responses", handler.ResponsesWSProxy)               // OpenAI Responses API（WebSocket 双向流）
			v1.POST("/gemini", handler.GeminiProxy)                      // Gemini 原生格式
			v1.POST("/image", handler.CreateImageTask)
			v1.POST("/video", handler.CreateVideoTask)
			v1.POST("/audio", handler.CreateAudioTask)
			v1.POST("/music", handler.CreateMusicTask) // Suno 音乐生成
		}

		// Gemini SDK 原生路径兼容（/v1beta/models/{model}:generateContent）
		v1beta := authed.Group("/v1beta")
		v1beta.Use(middleware.APIKeyOnly())
		{
			v1beta.POST("/models/*path", handler.GeminiNativeProxy)
		}
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server: %v", err)
	}
}
