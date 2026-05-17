package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fanapi/internal/config"
	"fanapi/internal/model"
	"fanapi/internal/mq"
	"fanapi/internal/notify"
	"fanapi/internal/service"

	"github.com/nats-io/nats.go"
)

// StartWorkers 根据 WorkerConfig 订阅 NATS 任务主题。
//
// 默认（未配置）：订阅 "task.>"  ，consumer 名为 "workers-all"。
// 专用 Worker 示例（在 config.yaml 中添加）：
//
//	worker:
//	  subjects:
//	    - "task.video.*"
//	    - "task.audio.*"
func StartWorkers(cfg config.WorkerConfig) error {
	// 清理上次运行遗留的失效 Consumer，再进行订阅。
	// 只应在 Worker 进程中运行——如在服务器进程中运行会杀死服务器的 result-proc Consumer。
	mq.PurgeConsumers()

	if cfg.MaxConcurrent > 0 {
		log.Printf("[script worker] max concurrent tasks: %d", cfg.MaxConcurrent)
	}

	subjects := cfg.Subjects
	if len(subjects) == 0 {
		subjects = []string{"task.>"}
	}
	for _, subj := range subjects {
		consumer := subjectToConsumer(subj)
		if _, err := mq.QueueSubscribe(subj, consumer, handleTask, cfg.MaxConcurrent); err != nil {
			return fmt.Errorf("subscribe %s: %w", subj, err)
		}
		log.Printf("[script worker] subscribed to %s (consumer: %s)", subj, consumer)
	}
	return nil
}

func subjectToConsumer(subject string) string {
	s := strings.TrimPrefix(subject, "task.")
	s = strings.TrimSuffix(s, ".*")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, ">", "all")
	s = strings.ReplaceAll(s, "*", "any")
	return "workers-" + s
}

// natsMaxPayload 是 NATS 消息发布的保守最大字节数（略低于服务端限制，留出序列化开销）。
// NATS 服务端已配置 max_payload = 60MB；此处设 55MB 作为软限制，保留 5MB 余量。
const natsMaxPayload = 55 * 1024 * 1024 // 55 MB

func handleTask(msg *nats.Msg) {
	var job model.TaskJob
	if err := json.Unmarshal(msg.Data, &job); err != nil {
		log.Printf("[worker] bad message: %v", err)
		_ = msg.Term()
		return
	}

	result := execJob(context.Background(), &job)

	subject := fmt.Sprintf("result.%d", job.TaskID)
	data, _ := json.Marshal(result)

	// NATS 服务端有最大消息大小限制（默认 1MB）。
	// 上游若返回 base64 内联图片等大体积数据，响应可能超限。
	// 策略：先去掉调试用的 UpstreamResponse；仍超限则整体标为失败并立即 Term，
	// 避免消息被无限重投（每次都会失败）直到 MaxDeliver 耗尽后任务永久卡死。
	if len(data) > natsMaxPayload {
		log.Printf("[worker] task %d: result too large (%d bytes), stripping upstream_response", job.TaskID, len(data))
		result.UpstreamResponse = nil
		data, _ = json.Marshal(result)
	}
	if len(data) > natsMaxPayload {
		log.Printf("[worker] task %d: result still too large (%d bytes), marking failed", job.TaskID, len(data))
		result.Outcome = model.OutcomeFailed
		result.ErrorMsg = fmt.Sprintf("上游响应体过大（超过 %d KB），无法经由消息队列传输；请在渠道的 response_script 中提取 URL 而非透传整个响应", natsMaxPayload/1024)
		result.Result = nil
		result.UpstreamResponse = nil
		data, _ = json.Marshal(result)
	}

	// 先发布结果再 ACK——若发布失败则消息会被重新投递，Worker 将重试。
	if err := mq.PublishResult(subject, data); err != nil {
		log.Printf("[worker] task %d: failed to publish result: %v", job.TaskID, err)
		_ = msg.Term() // 立即终止，不再重投（同样的载荷会一直失败）
		return
	}
	_ = msg.Ack()
}

// execJob 执行一个 TaskJob 并返回 WorkerResult，永不返回 nil。
func execJob(ctx context.Context, job *model.TaskJob) *model.WorkerResult {
	base := &model.WorkerResult{
		TaskID:          job.TaskID,
		TaskType:        job.TaskType,
		UserID:          job.UserID,
		APIKeyID:        job.APIKeyID,
		CorrID:          job.CorrID,
		CreditsCharged:  job.CreditsCharged,
		ChannelID:       job.ChannelID,
		PoolKeyID:       job.PoolKeyID,
		RetryCount:      job.RetryCount,
		Payload:         job.Payload, // 保留下来以便服务器在 OutcomeRateLimited / 稳定密钥重试时重新发布
		RetryChannelIDs: job.RetryChannelIDs,
	}

	fail := func(msg string) *model.WorkerResult {
		base.Outcome = model.OutcomeFailed
		base.ErrorMsg = msg
		return base
	}

	// 应用 request_script
	payload := job.Payload
	if job.RequestScript != "" {
		mapped, err := RunMapRequest(job.RequestScript, payload, job.PoolKeyValue)
		if err != nil {
			return fail("request mapping error: " + err.Error())
		}
		payload = mapped
	}

	// 记录上游请求（调试用），包含目标 URL 和请求头
	upstreamReq := make(map[string]interface{})
	for k, v := range payload {
		upstreamReq[k] = v
	}
	initialReq := make(map[string]interface{})
	for k, v := range payload {
		initialReq[k] = v
	}
	// 计算实际 URL（含 {model} 和 {{pool_key}} 替换），方便管理端排障
	targetURLForLog := job.BaseURL
	if modelVal, ok := job.Payload["model"].(string); ok && modelVal != "" {
		targetURLForLog = strings.ReplaceAll(targetURLForLog, "{model}", modelVal)
	}
	// URL 里也做 {{}} / {{pool_key}} 替换（实际请求 URL 记录）
	targetURLForLog = ResolveHeaderValue(targetURLForLog, job.PoolKeyValue)
	upstreamReq["_url"] = targetURLForLog
	upstreamReq["_method"] = job.Method
	upstreamReq["_initial_request"] = initialReq
	// 合并渠道配置的请求头（完整替换后记录，含完整 Key）
	headersForLog := make(map[string]interface{})
	for k, v := range job.Headers {
		if sv, ok := v.(string); ok {
			headersForLog[k] = ResolveHeaderValue(sv, job.PoolKeyValue)
		} else {
			headersForLog[k] = v
		}
	}
	headersForLog["Content-Type"] = "application/json"
	upstreamReq["_headers"] = headersForLog
	base.UpstreamRequest = upstreamReq

	// 调用上游 HTTP
	respData, statusCode, err := callUpstream(job, payload)
	if err != nil {
		return fail("upstream error: " + err.Error())
	}

	// 429: report rate_limited so server can rotate key and retry (once)
	if statusCode == http.StatusTooManyRequests {
		if job.PoolKeyID > 0 && job.RetryCount < 1 {
			base.Outcome = model.OutcomeRateLimited
			return base
		}
		return fail("upstream rate limited")
	}

	upstreamResp := make(map[string]interface{})
	for k, v := range respData {
		upstreamResp[k] = v
	}
	base.UpstreamResponse = upstreamResp

	// 应用 response_script
	if job.ResponseScript != "" {
		mapped, err := RunMapResponse(job.ResponseScript, respData)
		if err != nil {
			return fail("response mapping error: " + err.Error())
		}
		respData = mapped
	}

	// 检查是否有异步上游任务 ID
	upstreamTaskID, _ := respData["upstream_task_id"].(string)
	if upstreamTaskID == "" {
		if v, ok := respData["id"].(string); ok && job.QueryURL != "" {
			upstreamTaskID = v
		}
	}
	if upstreamTaskID != "" {
		base.Outcome = model.OutcomeAsync
		base.UpstreamTaskID = upstreamTaskID
		return base
	}

	// 错误检测（error_script 或内置识别逻辑）
	errMsg, isErr := "", false
	fatalErr := false
	if job.ErrorScript != "" {
		var scriptErr error
		errMsg, fatalErr, scriptErr = RunCheckError(job.ErrorScript, respData)
		if scriptErr != nil {
			log.Printf("[worker] task %d: error_script failed: %v", job.TaskID, scriptErr)
		}
		isErr = errMsg != ""
	} else {
		errMsg, isErr = DetectUpstreamError(respData)
	}
	if fatalErr {
		if err := service.PatchChannelActive(context.Background(), job.ChannelID, false); err != nil {
			log.Printf("[worker] task %d: disable channel %d failed: %v", job.TaskID, job.ChannelID, err)
		} else {
			channelName := fmt.Sprintf("channel-%d", job.ChannelID)
			go func(name string, id int64, reason string) {
				defer func() { recover() }()
				if err := notify.SendLarkChannelDisabled(name, id, reason); err != nil {
					log.Printf("[lark notify] failed: %v", err)
				}
			}(channelName, job.ChannelID, errMsg)
		}
	}
	if isErr {
		return fail(errMsg)
	}

	// response_script 返回 status=3 表示业务失败（兼容 goja 导出的 int64/float64/int）
	if statusVal := toIntVal(respData["status"]); statusVal == 3 {
		return fail("upstream failed: " + fmt.Sprintf("%v", respData["msg"]))
	}

	result := make(map[string]interface{})
	for k, v := range respData {
		result[k] = v
	}
	base.Outcome = model.OutcomeDone
	base.Result = result
	return base
}

func callUpstream(job *model.TaskJob, payload map[string]interface{}) (map[string]interface{}, int, error) {
	// 允许 requestScript 通过保留字段动态覆盖请求地址/方法/头，
	// 例如：
	//   { _url: "https://api.example.com/v1/images/edits", _method: "POST", _headers: {...}, _body_type: "multipart/form-data" }
	// 这些控制字段不会进入实际请求体。
	bodyPayload := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		if k == "_url" || k == "_method" || k == "_headers" || k == "_body_type" || k == "_form_fields" || k == "_files" {
			continue
		}
		bodyPayload[k] = v
	}

	bodyType := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["_body_type"])))
	body, contentType, err := buildUpstreamBody(job, payload, bodyPayload, bodyType)
	if err != nil {
		return nil, 0, err
	}

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	// 支持 requestScript 动态指定 _url；否则回退到渠道 base_url。
	targetURL := job.BaseURL
	if v, ok := payload["_url"].(string); ok && strings.TrimSpace(v) != "" {
		targetURL = v
	}
	// 支持 {model} 和 {{}} / {{pool_key}} 占位符，将请求载荷中的模型名 / 号池 Key 注入 URL
	if modelVal, ok := job.Payload["model"].(string); ok && modelVal != "" {
		targetURL = strings.ReplaceAll(targetURL, "{model}", modelVal)
	}
	targetURL = ResolveHeaderValue(targetURL, job.PoolKeyValue)

	method := job.Method
	if v, ok := payload["_method"].(string); ok && strings.TrimSpace(v) != "" {
		method = v
	}
	req, err := http.NewRequest(method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	// 应用渠道 Header，支持 {{}} / {{pool_key}} 占位符替换
	headers := make(map[string]interface{}, len(job.Headers)+1)
	for k, v := range job.Headers {
		headers[k] = v
	}
	if extra, ok := payload["_headers"].(map[string]interface{}); ok {
		for k, v := range extra {
			headers[k] = v
		}
	}
	for k, v := range headers {
		if sv, ok := v.(string); ok {
			req.Header.Set(k, ResolveHeaderValue(sv, job.PoolKeyValue))
		}
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("upstream response not JSON: %w", err)
	}
	return result, resp.StatusCode, nil
}

func buildUpstreamBody(job *model.TaskJob, payload, bodyPayload map[string]interface{}, bodyType string) ([]byte, string, error) {
	if bodyType == "" || bodyType == "json" || bodyType == "application/json" {
		body, err := json.Marshal(bodyPayload)
		return body, "application/json", err
	}

	if bodyType == "multipart/form-data" || bodyType == "multipart" || bodyType == "form-data" {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		fields := map[string]interface{}{}
		if rawFields, ok := payload["_form_fields"].(map[string]interface{}); ok && len(rawFields) > 0 {
			for k, v := range rawFields {
				fields[k] = v
			}
		} else {
			for k, v := range bodyPayload {
				if k == "refer_images" {
					continue
				}
				fields[k] = v
			}
		}
		for k, v := range fields {
			if err := writer.WriteField(k, fmt.Sprint(v)); err != nil {
				_ = writer.Close()
				return nil, "", err
			}
		}

		files := map[string]interface{}{}
		if rawFiles, ok := payload["_files"].(map[string]interface{}); ok && len(rawFiles) > 0 {
			for k, v := range rawFiles {
				files[k] = v
			}
		} else if refs, ok := payload["refer_images"].([]interface{}); ok && len(refs) > 0 {
			files["image"] = refs[0]
		} else if refs, ok := payload["refer_images"].([]string); ok && len(refs) > 0 {
			files["image"] = refs[0]
		}

		for field, raw := range files {
			urls := normalizeToStrings(raw)
			for idx, rawURL := range urls {
				fileBytes, fileName, fileType, err := readUploadSource(strings.TrimSpace(ResolveHeaderValue(rawURL, job.PoolKeyValue)))
				if err != nil {
					_ = writer.Close()
					return nil, "", fmt.Errorf("multipart file %s[%d] load error: %w", field, idx, err)
				}
				partName := field
				if len(urls) > 1 {
					partName = fmt.Sprintf("%s[%d]", field, idx)
				}
				headers := make(textproto.MIMEHeader)
				headers.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, partName, fileName))
				if fileType == "" {
					fileType = mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName)))
				}
				if fileType == "" {
					fileType = http.DetectContentType(fileBytes)
				}
				headers.Set("Content-Type", fileType)
				part, err := writer.CreatePart(headers)
				if err != nil {
					_ = writer.Close()
					return nil, "", err
				}
				if _, err := part.Write(fileBytes); err != nil {
					_ = writer.Close()
					return nil, "", err
				}
			}
		}

		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), writer.FormDataContentType(), nil
	}

	body, err := json.Marshal(bodyPayload)
	return body, "application/json", err
}

func normalizeToStrings(v interface{}) []string {
	switch vv := v.(type) {
	case string:
		if strings.TrimSpace(vv) == "" {
			return nil
		}
		return []string{vv}
	case []string:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func readUploadSource(src string) ([]byte, string, string, error) {
	if src == "" {
		return nil, "", "", fmt.Errorf("empty source")
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		resp, err := http.Get(src)
		if err != nil {
			return nil, "", "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			return nil, "", "", fmt.Errorf("download failed: %s", string(b))
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", "", err
		}
		fileName := filenameFromURL(src)
		if fileName == "" {
			fileName = "upload"
		}
		return data, fileName, resp.Header.Get("Content-Type"), nil
	}

	localPath := strings.TrimPrefix(src, "/")
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, "", "", err
	}
	fileName := filepath.Base(localPath)
	if fileName == "" || fileName == "." || fileName == string(filepath.Separator) {
		fileName = "upload"
	}
	return data, fileName, mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName))), nil
}

func filenameFromURL(rawURL string) string {
	idx := strings.LastIndex(rawURL, "/")
	if idx < 0 || idx+1 >= len(rawURL) {
		return ""
	}
	name := rawURL[idx+1:]
	if name == "" {
		return ""
	}
	return name
}

// DetectUpstreamError 检测常见厂商错误响应格式。
// 如识别到错误，返回错误消息和 true。
//
// 支持的格式：
//   - OpenAI / 通用：{"error": {"message": "...", "code": "..."}}
//   - 字符串错误：{"error": "some message"}
//   - 自定义 code+msg：{"code": "InvalidParameter", "message": "..."}
func DetectUpstreamError(resp map[string]interface{}) (string, bool) {
	if errVal, ok := resp["error"]; ok && errVal != nil {
		switch e := errVal.(type) {
		case map[string]interface{}:
			msg, _ := e["message"].(string)
			code, _ := e["code"].(string)
			switch {
			case code != "" && msg != "":
				return code + ": " + msg, true
			case msg != "":
				return msg, true
			case code != "":
				return code, true
			}
		case string:
			if e != "" {
				return e, true
			}
		}
		return "upstream returned error", true
	}

	codeVal, hasCode := resp["code"]
	msgStr, _ := resp["message"].(string)
	if hasCode && msgStr != "" {
		switch c := codeVal.(type) {
		case string:
			if c != "" {
				return c + ": " + msgStr, true
			}
		case float64:
			if c < 200 || c >= 300 {
				return fmt.Sprintf("code %d: %s", int(c), msgStr), true
			}
		}
	}

	return "", false
}

// toIntVal 从 goja 脚本导出的值中提取整数，兼容 int64/float64/int/int32。
func toIntVal(v interface{}) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case float64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	}
	return 0
}
