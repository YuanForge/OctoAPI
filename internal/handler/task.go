package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"fanapi/internal/db"
	"fanapi/internal/model"
	"fanapi/internal/script"
	"fanapi/internal/service"

	"github.com/gin-gonic/gin"
)

// GetTaskBilling 查询任务计费明细
// @Summary      查询任务计费明细
// @Description  返回指定任务的全部计费流水及汇总（净扣费、是否已退款）。
// @Tags         任务
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id   path      int  true  "任务 ID"
// @Success      200  {object}  object{transactions=[]model.BillingTransaction,total_charged=int,total_refunded=int,net_charged=int,refunded=bool}
// @Failure      404  {object}  object  "任务不存在"
// @Router       /v1/tasks/{id}/billing [get]
func GetTaskBilling(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务 ID 格式错误"})
		return
	}
	userID := c.MustGet("user_id").(int64)

	task := &model.Task{}
	found, err := db.Engine.Where("id = ? AND user_id = ?", id, userID).
		Cols("id", "corr_id", "credits_charged", "status").Get(task)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败，请稍后重试"})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	var txs []model.BillingTransaction
	if task.CorrID != "" {
		if err := db.Engine.Where("user_id = ? AND corr_id = ?", userID, task.CorrID).
			Cols("id", "corr_id", "type", "credits", "balance_after", "metrics", "created_at").
			Asc("id").Find(&txs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败，请稍后重试"})
			return
		}
	}

	var totalCharged, totalRefunded int64
	for _, tx := range txs {
		switch tx.Type {
		case "charge", "hold", "settle":
			totalCharged += tx.Credits
		case "refund":
			totalRefunded += tx.Credits
		}
	}
	netCharged := totalCharged - totalRefunded

	c.JSON(http.StatusOK, gin.H{
		"transactions":   txs,
		"total_charged":  totalCharged,
		"total_refunded": totalRefunded,
		"net_charged":    netCharged,
		"refunded":       totalRefunded > 0,
	})
}

// GetTask 查询任务结果
// @Summary      查询任务结果
// @Description  轮询图片/视频/音频/音乐任务结果。code=150 进行中，200 成功，500 失败。
// @Tags         任务
// @Produce      json
// @Security     ApiKeyAuth
// @Param        id   path      int  true  "任务 ID"
// @Success      200  {object}  model.TaskResult
// @Failure      404  {object}  object  "任务不存在"
// @Router       /v1/tasks/{id} [get]
func GetTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务 ID 格式错误"})
		return
	}
	userID := c.MustGet("user_id").(int64)

	task := &model.Task{}
	found, err := db.Engine.Where("id = ? AND user_id = ?", id, userID).Get(task)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败，请稍后重试"})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	rewrite := getTaskResultURLRewrite()

	switch task.Status {
	case "pending":
		c.JSON(http.StatusOK, gin.H{"code": 150, "status": 0, "msg": "排队中"})
	case "processing":
		c.JSON(http.StatusOK, gin.H{"code": 150, "status": 1, "msg": "生成中"})
	case "done":
		result := task.Result
		if rewrite != nil && len(task.Result) > 0 {
			result = rewriteJSONStrings(cloneJSON(task.Result), rewrite)
		}
		out := gin.H{}
		for _, k := range []string{"code", "status", "msg", "url"} {
			if v, ok := result[k]; ok {
				out[k] = v
			}
		}
		c.JSON(http.StatusOK, out)
	case "failed":
		c.JSON(http.StatusOK, gin.H{"code": 500, "status": 3, "msg": service.UserFacingErrorMessage(task.ErrorMsg)})
	default:
		c.JSON(http.StatusOK, gin.H{"code": 150, "status": 0, "msg": task.Status})
	}
}

// GET /admin/tasks
func ListTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}

	query := db.Engine.Desc("id")
	if taskID := c.Query("task_id"); taskID != "" {
		query = query.Where("id = ?", taskID)
	}
	if userID := c.Query("user_id"); userID != "" {
		query = query.And("user_id = ?", userID)
	}
	if status := c.Query("status"); status != "" {
		query = query.And("status = ?", status)
	}
	if taskType := c.Query("type"); taskType != "" {
		query = query.And("type = ?", taskType)
	}
	if startAt := c.Query("start_at"); startAt != "" {
		query = query.And("created_at >= ?", startAt)
	}
	if endAt := c.Query("end_at"); endAt != "" {
		query = query.And("created_at <= ?", endAt)
	}

	var tasks []model.Task
	total, err := query.Cols("id", "user_id", "channel_id", "api_key_id", "type", "status",
		"progress", "upstream_task_id",
		"error_msg", "credits_charged", "corr_id", "created_at", "updated_at").
		Limit(size, (page-1)*size).FindAndCount(&tasks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败，请稍后重试"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "total": total})
}

// ListUserTasks 查询当前用户的任务列表
// @Summary      查询任务列表
// @Description  分页获取当前 API Key 对应用户的历史任务。
// @Tags         任务
// @Produce      json
// @Security     ApiKeyAuth
// @Param        page      query     int     false  "页码（默认 1）"
// @Param        size      query     int     false  "每页条数（默认 20，最大 100）"
// @Param        status    query     string  false  "状态过滤：pending/processing/done/failed"
// @Param        type      query     string  false  "任务类型过滤：image/video/audio/music"
// @Param        task_id   query     int     false  "按 task_id 精确查询"
// @Param        start_at  query     string  false  "创建时间起（2006-01-02 15:04:05）"
// @Param        end_at    query     string  false  "创建时间止"
// @Success      200  {object}  object{tasks=[]model.TaskResult,total=int}
// @Router       /v1/tasks [get]
func ListUserTasks(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}

	query := db.Engine.Where("user_id = ? AND user_deleted = false", userID).Desc("id")
	if status := c.Query("status"); status != "" {
		query = query.And("status = ?", status)
	}
	if taskType := c.Query("type"); taskType != "" {
		query = query.And("type = ?", taskType)
	}
	if taskID := c.Query("task_id"); taskID != "" {
		query = query.And("id = ?", taskID)
	}
	if startAt := c.Query("start_at"); startAt != "" {
		query = query.And("created_at >= ?", startAt)
	}
	if endAt := c.Query("end_at"); endAt != "" {
		query = query.And("created_at <= ?", endAt)
	}

	var tasks []model.Task
	total, err := query.Cols("id", "user_id", "channel_id", "api_key_id", "type", "status",
		"progress", "upstream_task_id",
		"error_msg", "credits_charged", "corr_id", "request", "result", "created_at", "updated_at").
		Limit(size, (page-1)*size).FindAndCount(&tasks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败，请稍后重试"})
		return
	}

	results := make([]model.TaskResult, 0, len(tasks))
	rewrite := getTaskResultURLRewrite()
	for i := range tasks {
		results = append(results, buildTaskResult(&tasks[i], rewrite))
	}
	c.JSON(http.StatusOK, gin.H{"tasks": results, "total": total})
}

// DELETE /v1/tasks/history
// 清空当前用户的任务历史记录，软删除已完成/失败的历史任务（不影响管理员查看）。
func DeleteUserTaskHistory(c *gin.Context) {
	userID := c.MustGet("user_id").(int64)
	taskType := c.Query("type")

	query := db.Engine.Where("user_id = ? AND status IN ('done', 'failed') AND user_deleted = false", userID)
	if taskType != "" {
		query = query.And("type = ?", taskType)
	}

	n, err := query.Cols("user_deleted").Update(&model.Task{UserDeleted: true})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清空失败，请稍后重试"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": n})
}

// GET /admin/tasks/:id
func GetAdminTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务 ID 格式错误"})
		return
	}
	task := &model.Task{}
	found, err := db.Engine.Where("id = ?", id).Get(task)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败，请稍后重试"})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	applyAdminResultProxyRewrite(task)
	enrichAdminUpstreamRequest(task)
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// applyAdminResultProxyRewrite 按系统配置重写管理端任务结果中的 URL 前缀。
// 仅影响返回值，不改写数据库中的原始 task.result。
func applyAdminResultProxyRewrite(task *model.Task) {
	if task == nil || len(task.Result) == 0 {
		return
	}
	rewrite := getTaskResultURLRewrite()
	if rewrite == nil {
		return
	}

	copied := cloneJSON(task.Result)
	task.Result = rewriteJSONStrings(copied, rewrite)
}

func getTaskResultURLRewrite() func(string) string {
	// 优先读取多规则 JSON（[{"from":"...","to":"..."}]）
	rulesRaw := strings.TrimSpace(getSettingValue("result_url_proxy_rules"))
	type ruleEntry struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	var rules []ruleEntry
	if rulesRaw != "" {
		_ = json.Unmarshal([]byte(rulesRaw), &rules)
	}
	// 兼容旧单对配置
	if len(rules) == 0 {
		from := strings.TrimSpace(getSettingValue("result_url_proxy_from"))
		to := strings.TrimSpace(getSettingValue("result_url_proxy_to"))
		if from != "" && to != "" {
			rules = []ruleEntry{{From: from, To: to}}
		}
	}
	if len(rules) == 0 {
		return nil
	}
	// 编译全部规则，组合为复合重写函数
	var fns []func(string) string
	for _, r := range rules {
		f := strings.TrimRight(strings.TrimSpace(r.From), "/")
		t := strings.TrimRight(strings.TrimSpace(r.To), "/")
		if f == "" || t == "" {
			continue
		}
		fns = append(fns, compilePrefixRewrite(f, t))
	}
	if len(fns) == 0 {
		return nil
	}
	return func(s string) string {
		for _, fn := range fns {
			s = fn(s)
		}
		return s
	}
}

func cloneJSON(src model.JSON) model.JSON {
	if src == nil {
		return nil
	}
	dst := make(model.JSON, len(src))
	for k, v := range src {
		dst[k] = cloneAny(v)
	}
	return dst
}

func cloneAny(v interface{}) interface{} {
	switch vv := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(vv))
		for k, x := range vv {
			m[k] = cloneAny(x)
		}
		return m
	case []interface{}:
		a := make([]interface{}, len(vv))
		for i := range vv {
			a[i] = cloneAny(vv[i])
		}
		return a
	default:
		return vv
	}
}

func compilePrefixRewrite(from, to string) func(string) string {
	pat := "^" + regexp.QuoteMeta(from) + "(/|$)"
	re := regexp.MustCompile(pat)
	return func(s string) string {
		if !re.MatchString(s) {
			return s
		}
		return re.ReplaceAllString(s, to+"$1")
	}
}

func rewriteJSONStrings(src model.JSON, rewrite func(string) string) model.JSON {
	if src == nil {
		return nil
	}
	out := make(model.JSON, len(src))
	for k, v := range src {
		out[k] = rewriteAny(v, rewrite)
	}
	return out
}

func rewriteAny(v interface{}, rewrite func(string) string) interface{} {
	switch vv := v.(type) {
	case string:
		return rewrite(vv)
	case map[string]interface{}:
		m := make(map[string]interface{}, len(vv))
		for k, x := range vv {
			m[k] = rewriteAny(x, rewrite)
		}
		return m
	case []interface{}:
		a := make([]interface{}, len(vv))
		for i := range vv {
			a[i] = rewriteAny(vv[i], rewrite)
		}
		return a
	default:
		return vv
	}
}

// enrichAdminUpstreamRequest 在历史数据未记录首发请求时，按当前渠道配置兜底重建，
// 以便管理端同时看到“首次POST请求体”和“轮询GET请求体”。
func enrichAdminUpstreamRequest(task *model.Task) {
	if task == nil {
		return
	}
	req := task.UpstreamRequest
	if len(req) == 0 {
		return
	}
	if _, ok := req["_initial_request"]; ok {
		return
	}

	pollOnly := false
	if m, ok := req["method"].(string); ok && strings.EqualFold(m, "GET") {
		pollOnly = true
	}
	if m, ok := req["_method"].(string); ok && strings.EqualFold(m, "GET") {
		pollOnly = true
	}
	if _, ok := req["_poll_request"]; ok {
		pollOnly = true
	}
	if !pollOnly {
		return
	}

	// 保存当前轮询信息，避免被后续覆盖。
	if _, ok := req["_poll_request"]; !ok {
		poll := model.JSON{}
		if v, ok := req["_url"]; ok {
			poll["_url"] = v
		}
		if v, ok := req["_headers"]; ok {
			poll["_headers"] = v
		}
		if v, ok := req["method"]; ok {
			poll["method"] = v
		}
		if v, ok := req["_method"]; ok {
			poll["_method"] = v
		}
		if v, ok := req["query"]; ok {
			poll["query"] = v
		}
		if len(poll) > 0 {
			req["_poll_request"] = poll
		}
	}

	ch, err := service.GetChannel(context.Background(), task.ChannelID)
	if err != nil || ch == nil {
		// 至少把用户原始请求回填，避免首发请求体区域为空。
		req["_initial_request"] = task.Request
		task.UpstreamRequest = req
		return
	}

	payload := map[string]interface{}{}
	for k, v := range task.Request {
		payload[k] = v
	}
	initialPayload := payload
	if ch.RequestScript != "" {
		if mapped, mapErr := script.RunMapRequest(ch.RequestScript, payload, ""); mapErr == nil {
			initialPayload = mapped
		}
	}

	initialReq := model.JSON{}
	for k, v := range initialPayload {
		initialReq[k] = v
	}
	req["_initial_request"] = initialReq

	method := ch.Method
	if method == "" {
		method = "POST"
	}
	req["_method"] = method

	if ch.BaseURL != "" {
		targetURL := ch.BaseURL
		if modelVal, ok := task.Request["model"].(string); ok && modelVal != "" {
			targetURL = strings.ReplaceAll(targetURL, "{model}", modelVal)
		}
		targetURL = script.ResolveHeaderValue(targetURL, "")
		req["_url"] = targetURL
	}

	headers := model.JSON{}
	for k, v := range ch.Headers {
		headers[k] = v
	}
	headers["Content-Type"] = "application/json"
	req["_headers"] = headers

	task.UpstreamRequest = req
}

// buildTaskResult 根据 task 状态组装标准 TaskResult。
// done 状态直接从 task.Result 里读取（response_script 已映射好），
// 其余状态由平台合成，不依赖上游响应。
func buildTaskResult(task *model.Task, rewrite func(string) string) model.TaskResult {
	result := task.Result
	if rewrite != nil && len(task.Result) > 0 {
		result = rewriteJSONStrings(cloneJSON(task.Result), rewrite)
	}

	base := model.TaskResult{
		TaskID:         task.ID,
		TaskType:       task.Type,
		ChannelID:      task.ChannelID,
		CreditsCharged: task.CreditsCharged,
		CreatedAt:      task.CreatedAt,
		Request:        task.Request, // 原始请求参数
		Result:         result,       // 映射后的响应结果
	}
	switch task.Status {
	case "pending":
		base.Code = 150
		base.Status = 0
		base.Msg = "排队中"
		return base

	case "processing":
		base.Code = 150
		base.Status = 1
		base.Msg = "生成中"
		return base

	case "done":
		t := task.UpdatedAt
		base.FinishedAt = &t
		code := 200
		if v, ok := result["code"]; ok {
			if n, ok := toInt(v); ok {
				code = n
			}
		}
		statusVal := 2
		if v, ok := result["status"]; ok {
			if n, ok := toInt(v); ok {
				statusVal = n
			}
		}
		url, _ := result["url"].(string)
		// 若 response_script 把 url 映射为数组（如 gpt-image-2），提取第一个元素作为顶层 url
		// 并把完整数组放入 Items，兼容多图场景
		if url == "" {
			if arr, ok := result["url"].([]interface{}); ok && len(arr) > 0 {
				if s, ok := arr[0].(string); ok {
					url = s
				}
				base.Items = arr
			}
		}
		msg, _ := result["msg"].(string)
		base.Code = code
		base.Status = statusVal
		base.URL = url
		base.Msg = msg
		// 多结果任务（如音乐每次生成两首）
		if items, ok := result["items"]; ok {
			if arr, ok := items.([]interface{}); ok {
				base.Items = arr
			}
		}
		return base

	case "failed":
		t := task.UpdatedAt
		base.FinishedAt = &t
		base.Code = 500
		base.Status = 3
		base.Msg = service.UserFacingErrorMessage(task.ErrorMsg)
		return base

	default:
		base.Code = 150
		base.Status = 0
		base.Msg = task.Status
		return base
	}
}

// toInt 将 JSON 数值（float64）或 int 类型安全转换为 int。
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
