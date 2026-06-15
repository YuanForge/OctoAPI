package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fanapi/internal/cache"
	"fanapi/internal/db"
	"fanapi/internal/model"
)

const (
	keyPoolSyncLockFmt  = "pool:sync_lock:%d"
	newAPITokenPageSize = 100
	newAPITokenMaxPages = 5
)

var keyPoolHTTPClient = &http.Client{Timeout: 15 * time.Second}

type KeyPoolSyncResult struct {
	PoolID             int64  `json:"pool_id"`
	PlatformID         int64  `json:"platform_id"`
	Group              string `json:"group,omitempty"`
	Listed             int    `json:"listed"`
	Imported           int    `json:"imported"`
	Reactivated        int    `json:"reactivated"`
	Skipped            int    `json:"skipped"`
	CreatedUpstream    int    `json:"created_upstream"`
	SkippedByLock      bool   `json:"skipped_by_lock,omitempty"`
	CreatedUpstreamKey string `json:"-"`
}

type keyPoolSyncConfig struct {
	Pool     model.KeyPool
	Channel  model.Channel
	Platform model.UpstreamPlatform
	Group    string
}

type newAPITokenRecord struct {
	ID     string
	Key    string
	Group  string
	Active bool
}

// SyncKeyPoolFromUpstream imports active New API tokens into the local pool.
// It does not create upstream tokens.
func SyncKeyPoolFromUpstream(ctx context.Context, poolID int64) (KeyPoolSyncResult, error) {
	return syncKeyPoolFromUpstream(ctx, poolID)
}

func EnsureKeyPoolFromUpstream(ctx context.Context, poolID int64) (KeyPoolSyncResult, error) {
	return syncKeyPoolFromUpstream(ctx, poolID)
}

func syncKeyPoolFromUpstream(ctx context.Context, poolID int64) (KeyPoolSyncResult, error) {
	lockKey := fmt.Sprintf(keyPoolSyncLockFmt, poolID)
	locked := false
	if cache.Client != nil {
		ok, err := cache.Client.SetNX(ctx, lockKey, 1, 30*time.Second).Result()
		if err == nil && !ok {
			select {
			case <-ctx.Done():
				return KeyPoolSyncResult{}, ctx.Err()
			case <-time.After(800 * time.Millisecond):
				return KeyPoolSyncResult{PoolID: poolID, SkippedByLock: true}, nil
			}
		}
		locked = err == nil && ok
	}
	if locked {
		defer cache.Client.Del(context.Background(), lockKey)
	}

	cfg, err := resolveKeyPoolSyncConfig(ctx, poolID)
	if err != nil {
		return KeyPoolSyncResult{PoolID: poolID}, err
	}
	result := KeyPoolSyncResult{
		PoolID:     poolID,
		PlatformID: cfg.Platform.ID,
		Group:      cfg.Group,
	}

	tokens, err := listNewAPITokens(ctx, cfg.Platform)
	if err != nil {
		return result, err
	}
	for _, token := range tokens {
		if !token.Active {
			result.Skipped++
			continue
		}
		if token.Key == "" || isMaskedAPIKey(token.Key) {
			result.Skipped++
			continue
		}
		if cfg.Group != "" && token.Group != "" && token.Group != cfg.Group {
			result.Skipped++
			continue
		}
		result.Listed++
		created, reactivated, err := upsertSyncedPoolKey(ctx, poolID, token.Key)
		if err != nil {
			return result, err
		}
		switch {
		case created:
			result.Imported++
		case reactivated:
			result.Reactivated++
		default:
			result.Skipped++
		}
	}

	return result, nil
}

func resolveKeyPoolSyncConfig(ctx context.Context, poolID int64) (keyPoolSyncConfig, error) {
	var pool model.KeyPool
	found, err := db.Engine.ID(poolID).Get(&pool)
	if err != nil {
		return keyPoolSyncConfig{}, fmt.Errorf("读取号池失败: %w", err)
	}
	if !found {
		return keyPoolSyncConfig{}, fmt.Errorf("号池 %d 不存在", poolID)
	}
	if !pool.IsActive {
		return keyPoolSyncConfig{}, fmt.Errorf("号池 %d 已停用", poolID)
	}

	var ch model.Channel
	found, err = db.Engine.ID(pool.ChannelID).Get(&ch)
	if err != nil {
		return keyPoolSyncConfig{}, fmt.Errorf("读取号池渠道失败: %w", err)
	}
	if !found {
		return keyPoolSyncConfig{}, fmt.Errorf("号池 %d 绑定的渠道不存在", poolID)
	}

	platformID := jsonInt64Value(ch.BillingConfig["upstream_platform_id"])
	var platform model.UpstreamPlatform
	if platformID > 0 {
		found, err = db.Engine.ID(platformID).Get(&platform)
	} else if baseURL := strings.TrimSpace(jsonStringValue(ch.BillingConfig["upstream_base_url"])); baseURL != "" {
		found, err = db.Engine.Where("base_url = ?", strings.TrimRight(baseURL, "/")).Get(&platform)
	}
	if err != nil {
		return keyPoolSyncConfig{}, fmt.Errorf("读取上游平台失败: %w", err)
	}
	if !found {
		return keyPoolSyncConfig{}, errors.New("号池渠道未绑定可同步的上游平台")
	}
	if !platform.IsActive {
		return keyPoolSyncConfig{}, fmt.Errorf("上游平台 %s 已停用", platform.Name)
	}
	if normalizePlatformType(platform.PlatformType) != "newapi" {
		return keyPoolSyncConfig{}, fmt.Errorf("上游平台 %s 不是 New API 类型，暂不支持自动同步号池 Key", platform.Name)
	}
	if strings.TrimSpace(platform.SystemTokenEnc) == "" {
		return keyPoolSyncConfig{}, fmt.Errorf("上游平台 %s 未配置系统访问令牌", platform.Name)
	}
	if strings.TrimSpace(platform.UpstreamUserID) == "" {
		return keyPoolSyncConfig{}, fmt.Errorf("上游平台 %s 未配置 New-Api-User", platform.Name)
	}

	group := strings.TrimSpace(jsonStringValue(ch.BillingConfig["upstream_group"]))
	if group == "" {
		group = strings.TrimSpace(platform.UpstreamGroup)
	}

	return keyPoolSyncConfig{
		Pool:     pool,
		Channel:  ch,
		Platform: platform,
		Group:    group,
	}, nil
}

func listNewAPITokens(ctx context.Context, p model.UpstreamPlatform) ([]newAPITokenRecord, error) {
	out := make([]newAPITokenRecord, 0, newAPITokenPageSize)
	for page := 1; page <= newAPITokenMaxPages; page++ {
		items, err := fetchNewAPITokenPage(ctx, p, page, newAPITokenPageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) < newAPITokenPageSize {
			break
		}
	}
	return out, nil
}

func fetchNewAPITokenPage(ctx context.Context, p model.UpstreamPlatform, page, size int) ([]newAPITokenRecord, error) {
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/api/token/?p=" + strconv.Itoa(page) + "&size=" + strconv.Itoa(size)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	applyNewAPIAdminHeaders(req, p)
	resp, err := keyPoolHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求上游 token 列表失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("上游 token 列表响应 %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	decoder := json.NewDecoder(bytes.NewReader(respBody))
	decoder.UseNumber()
	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析上游 token 列表失败: %w", err)
	}
	if success, ok := raw["success"].(bool); ok && !success {
		msg := jsonStringValue(raw["message"])
		if msg == "" {
			msg = jsonStringValue(raw["error"])
		}
		if msg == "" {
			msg = "上游 token 列表返回失败"
		}
		return nil, errors.New(msg)
	}

	return extractNewAPITokenRecords(raw["data"]), nil
}

func createNewAPIKey(ctx context.Context, p model.UpstreamPlatform, name, group string) (string, error) {
	body := map[string]interface{}{
		"name":                 name,
		"remain_quota":         int64(-1),
		"unlimited_quota":      true,
		"expired_time":         int64(-1),
		"model_limits_enabled": false,
		"model_limits":         "",
		"group":                group,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/api/token", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	applyNewAPIAdminHeaders(req, p)
	req.Header.Set("Content-Type", "application/json")

	resp, err := keyPoolHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求上游创建 Key 失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("上游创建 Key 响应 %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	decoder := json.NewDecoder(bytes.NewReader(respBody))
	decoder.UseNumber()
	var result map[string]interface{}
	if err := decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("解析上游创建 Key 响应失败: %w", err)
	}
	if success, ok := result["success"].(bool); ok && !success {
		msg := jsonStringValue(result["message"])
		if msg == "" {
			msg = jsonStringValue(result["error"])
		}
		if msg == "" {
			msg = "上游创建 Key 失败"
		}
		return "", errors.New(msg)
	}
	if key, ok := result["data"].(string); ok && strings.TrimSpace(key) != "" {
		return key, nil
	}
	if data, ok := result["data"].(map[string]interface{}); ok {
		for _, field := range []string{"key", "token", "value", "api_key"} {
			if key := jsonStringValue(data[field]); key != "" {
				return key, nil
			}
		}
	}
	return "", errors.New("上游未返回 API Key")
}

func applyNewAPIAdminHeaders(req *http.Request, p model.UpstreamPlatform) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.SystemTokenEnc))
	req.Header.Set("New-Api-User", strings.TrimSpace(p.UpstreamUserID))
}

func upsertSyncedPoolKey(ctx context.Context, poolID int64, value string) (created bool, reactivated bool, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, false, nil
	}
	var existing model.PoolKey
	found, err := db.Engine.Where("pool_id = ? AND value = ?", poolID, value).Get(&existing)
	if err != nil {
		return false, false, err
	}
	if found {
		if !existing.IsActive {
			existing.IsActive = true
			if _, err := db.Engine.ID(existing.ID).Cols("is_active").Update(&existing); err != nil {
				return false, false, err
			}
			return false, true, nil
		}
		return false, false, nil
	}
	key := &model.PoolKey{
		PoolID:   poolID,
		Value:    value,
		IsActive: true,
	}
	_, err = db.Engine.Insert(key)
	return err == nil, false, err
}

func extractNewAPITokenRecords(v interface{}) []newAPITokenRecord {
	rows := extractObjectRows(v)
	out := make([]newAPITokenRecord, 0, len(rows))
	for _, row := range rows {
		record := newAPITokenRecord{
			ID:     firstString(row, "id", "token_id"),
			Key:    firstString(row, "key", "token", "value", "api_key", "apiKey"),
			Group:  firstString(row, "group", "group_name", "groupName"),
			Active: tokenRecordActive(row),
		}
		out = append(out, record)
	}
	return out
}

func extractObjectRows(v interface{}) []map[string]interface{} {
	switch data := v.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(data))
		for _, item := range data {
			if row, ok := item.(map[string]interface{}); ok {
				out = append(out, row)
			}
		}
		return out
	case map[string]interface{}:
		for _, field := range []string{"items", "list", "rows", "tokens", "records", "data"} {
			if rows := extractObjectRows(data[field]); len(rows) > 0 {
				return rows
			}
		}
	}
	return nil
}

func tokenRecordActive(row map[string]interface{}) bool {
	for _, field := range []string{"disabled", "deleted"} {
		if b, ok := row[field].(bool); ok && b {
			return false
		}
	}
	for _, field := range []string{"enabled", "is_active", "active"} {
		if b, ok := row[field].(bool); ok {
			return b
		}
	}
	if status, ok := row["status"]; ok {
		switch v := status.(type) {
		case bool:
			return v
		case json.Number:
			n, _ := v.Int64()
			return n == 1
		case float64:
			return int64(v) == 1
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			if s == "disabled" || s == "deleted" || s == "expired" || s == "false" || s == "0" || s == "2" {
				return false
			}
			return true
		}
	}
	if expiredAt := jsonInt64Value(row["expired_time"]); expiredAt > 0 && expiredAt <= time.Now().Unix() {
		return false
	}
	if rawRemain, ok := row["remain_quota"]; ok {
		remain := jsonInt64Value(rawRemain)
		unlimited, _ := row["unlimited_quota"].(bool)
		if remain <= 0 && !unlimited {
			return false
		}
	}
	return true
}

func firstString(row map[string]interface{}, fields ...string) string {
	for _, field := range fields {
		if s := strings.TrimSpace(jsonStringValue(row[field])); s != "" {
			return s
		}
	}
	return ""
}

func isMaskedAPIKey(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.Contains(value, "*") || strings.Contains(strings.ToLower(value), "redacted")
}

func normalizePlatformType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "openai"
	}
	return value
}

func jsonStringValue(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case fmt.Stringer:
		return x.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func jsonInt64Value(v interface{}) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case int32:
		return int64(x)
	case float64:
		return int64(x)
	case float32:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		return 0
	}
}
