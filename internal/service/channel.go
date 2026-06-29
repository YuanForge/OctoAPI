package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	billingcalc "fanapi/internal/billing"
	"fanapi/internal/cache"
	"fanapi/internal/db"
	"fanapi/internal/model"
)

const channelCacheTTL = 10 * time.Minute

// ─────────────────────────────────────────────────────────────────────────────
// 基础 CRUD + 单次渠道查询
// ─────────────────────────────────────────────────────────────────────────────

// GetChannel 通过 ID 加载渠道，使用 Redis 作为缓存层。
func GetChannel(ctx context.Context, channelID int64) (*model.Channel, error) {
	cacheKey := fmt.Sprintf("channel:%d", channelID)

	data, err := cache.Client.Get(ctx, cacheKey).Bytes()
	if err == nil {
		ch := &model.Channel{}
		if jsonErr := json.Unmarshal(data, ch); jsonErr == nil {
			return ch, nil
		}
	}

	ch := &model.Channel{}
	found, err := db.Engine.Where("id = ? AND is_active = true", channelID).Get(ch)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("渠道不存在")
	}

	if b, jsonErr := json.Marshal(ch); jsonErr == nil {
		cache.Client.Set(ctx, cacheKey, b, channelCacheTTL)
	}
	return ch, nil
}

// InvalidateChannelCache 删除渠道对应的 Redis 缓存。
func InvalidateChannelCache(ctx context.Context, channelID int64) {
	cache.Client.Del(ctx, fmt.Sprintf("channel:%d", channelID))
}

func invalidateChannelRouteCaches(ctx context.Context, channels ...model.Channel) {
	keys := make(map[string]struct{})
	for _, ch := range channels {
		if ch.ID > 0 {
			keys[fmt.Sprintf("channel:%d", ch.ID)] = struct{}{}
		}
		if ch.Name != "" {
			keys[fmt.Sprintf("channel:name:%s", ch.Name)] = struct{}{}
		}
		if ch.Model != "" {
			keys[legacyChannelModelCachePrefix+ch.Model] = struct{}{}
			keys[channelModelCachePrefix+ch.Model] = struct{}{}
		}
		if routeKey := ChannelRoutingKey(ch); routeKey != "" {
			keys[legacyChannelModelCachePrefix+routeKey] = struct{}{}
			keys[channelModelCachePrefix+routeKey] = struct{}{}
		}
	}
	if len(keys) == 0 {
		return
	}
	args := make([]string, 0, len(keys))
	for key := range keys {
		args = append(args, key)
	}
	cache.Client.Del(ctx, args...)
}

func InvalidateChannelRouteCaches(ctx context.Context, channels ...model.Channel) {
	invalidateChannelRouteCaches(ctx, channels...)
}

// ListChannels 返回所有渠道（管理员接口）。
func ListChannels(ctx context.Context) ([]model.Channel, error) {
	var channels []model.Channel
	err := db.Engine.OrderBy("priority ASC, id DESC").Find(&channels)
	for index := range channels {
		channels[index].ModelProvider = EffectiveModelProvider(channels[index])
	}
	return channels, err
}

// CreateChannel 插入一个新渠道。
const channelListCreditsPerCNY = 1_000_000.0

type ChannelListQuery struct {
	Page          int
	Size          int
	Name          string
	DisplayName   string
	ModelProvider string
	Keyword       string
	PriceMin      *float64
	PriceMax      *float64
	SortBy        string
	SortOrder     string
}

type ChannelListResult struct {
	Channels []model.Channel
	Total    int64
	Page     int
	Size     int
}

func ListChannelsPaged(ctx context.Context, query ChannelListQuery) (*ChannelListResult, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Size < 1 {
		query.Size = 20
	}
	if query.Size > 100 {
		query.Size = 100
	}

	var channels []model.Channel
	if err := db.Engine.OrderBy("priority ASC, id DESC").Find(&channels); err != nil {
		return nil, err
	}

	filtered := make([]model.Channel, 0, len(channels))
	nameFilter := strings.ToLower(strings.TrimSpace(query.Name))
	displayNameFilter := strings.ToLower(strings.TrimSpace(query.DisplayName))
	modelProviderFilter := strings.ToLower(strings.TrimSpace(query.ModelProvider))
	keywordFilter := strings.ToLower(strings.TrimSpace(query.Keyword))
	for _, ch := range channels {
		ch.ModelProvider = EffectiveModelProvider(ch)
		if nameFilter != "" && !containsFold(ch.Name, nameFilter) {
			continue
		}
		if displayNameFilter != "" && !containsFold(ch.DisplayName, displayNameFilter) {
			continue
		}
		if modelProviderFilter != "" && !containsFold(EffectiveModelProvider(ch), modelProviderFilter) {
			continue
		}
		if keywordFilter != "" && !channelMatchesKeyword(ch, keywordFilter) {
			continue
		}
		priceCNY := channelBasePrice(ch) / channelListCreditsPerCNY
		if query.PriceMin != nil && priceCNY < *query.PriceMin {
			continue
		}
		if query.PriceMax != nil && priceCNY > *query.PriceMax {
			continue
		}
		filtered = append(filtered, ch)
	}

	if strings.EqualFold(query.SortBy, "price") {
		desc := strings.EqualFold(query.SortOrder, "desc")
		sort.SliceStable(filtered, func(i, j int) bool {
			leftPrice := channelBasePrice(filtered[i])
			rightPrice := channelBasePrice(filtered[j])
			if leftPrice != rightPrice {
				if desc {
					return leftPrice > rightPrice
				}
				return leftPrice < rightPrice
			}
			return filtered[i].ID > filtered[j].ID
		})
	}

	total := int64(len(filtered))
	start := (query.Page - 1) * query.Size
	if start >= len(filtered) {
		return &ChannelListResult{Channels: []model.Channel{}, Total: total, Page: query.Page, Size: query.Size}, nil
	}
	end := start + query.Size
	if end > len(filtered) {
		end = len(filtered)
	}
	return &ChannelListResult{Channels: filtered[start:end], Total: total, Page: query.Page, Size: query.Size}, nil
}

func containsFold(value, needleLower string) bool {
	return strings.Contains(strings.ToLower(value), needleLower)
}

func channelMatchesKeyword(ch model.Channel, needleLower string) bool {
	return containsFold(ch.Name, needleLower) ||
		containsFold(ch.DisplayName, needleLower) ||
		containsFold(EffectiveModelProvider(ch), needleLower) ||
		containsFold(ch.Model, needleLower) ||
		containsFold(ch.Type, needleLower) ||
		containsFold(ch.Protocol, needleLower)
}

func EffectiveModelProvider(ch model.Channel) string {
	if provider := strings.TrimSpace(ch.ModelProvider); provider != "" {
		return provider
	}
	return InferModelProvider(ch.Model, ch.DisplayName, ch.Name, nativeProtocolProviderHint(ch.Protocol))
}

func nativeProtocolProviderHint(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "claude":
		return "anthropic"
	case "gemini":
		return "google"
	default:
		return ""
	}
}

func InferModelProvider(values ...string) string {
	normalized := strings.ToLower(strings.Join(values, " "))
	if normalized == "" {
		return ""
	}

	if strings.Contains(normalized, "anthropic") || strings.Contains(normalized, "claude") {
		return "Anthropic"
	}
	if strings.Contains(normalized, "google") || strings.Contains(normalized, "gemini") {
		return "Google"
	}
	if strings.Contains(normalized, "deepseek") {
		return "DeepSeek"
	}
	if strings.Contains(normalized, "qwen") || strings.Contains(normalized, "tongyi") || strings.Contains(normalized, "通义") {
		return "Alibaba"
	}
	if strings.Contains(normalized, "suno") {
		return "Suno"
	}
	if strings.Contains(normalized, "kling") || strings.Contains(normalized, "可灵") {
		return "Kuaishou"
	}
	if strings.Contains(normalized, "midjourney") || strings.Contains(normalized, "mj-") {
		return "Midjourney"
	}
	if strings.Contains(normalized, "openai") ||
		strings.Contains(normalized, "gpt") ||
		strings.Contains(normalized, "dall-e") ||
		strings.Contains(normalized, "whisper") ||
		strings.Contains(normalized, "tts") ||
		hasOpenAIReasoningModelPrefix(normalized) {
		return "OpenAI"
	}
	return ""
}

func hasOpenAIReasoningModelPrefix(value string) bool {
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '/' || r == '_' || r == ':' || r == ',' || r == ';' || r == '|'
	}) {
		if strings.HasPrefix(token, "o1") || strings.HasPrefix(token, "o3") || strings.HasPrefix(token, "o4") {
			return true
		}
	}
	return false
}

func CreateChannel(ctx context.Context, ch *model.Channel) error {
	if ch.BillingType == "custom" {
		return fmt.Errorf("custom 自定义脚本计费已停用")
	}
	_, err := db.Engine.Insert(ch)
	if err == nil {
		invalidateChannelRouteCaches(ctx, *ch)
	}
	return err
}

// GetChannelByName 通过 Name 字段加载渠道，Name 即路由模型名。
// 缓存键为 "channel:name:{name}"。
// 保留向后兼容；新路由逻辑请使用 SelectChannel。
func GetChannelByName(ctx context.Context, name string) (*model.Channel, error) {
	cacheKey := fmt.Sprintf("channel:name:%s", name)

	data, err := cache.Client.Get(ctx, cacheKey).Bytes()
	if err == nil {
		ch := &model.Channel{}
		if jsonErr := json.Unmarshal(data, ch); jsonErr == nil {
			return ch, nil
		}
	}

	ch := &model.Channel{}
	found, err := db.Engine.Where("name = ? AND is_active = true", name).Get(ch)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("渠道 \"%s\" 不存在", name)
	}

	if b, jsonErr := json.Marshal(ch); jsonErr == nil {
		cache.Client.Set(ctx, cacheKey, b, channelCacheTTL)
	}
	return ch, nil
}

// PatchChannelActive 仅更新渠道的 is_active 字段，避免覆盖其他字段。
func PatchChannelActive(ctx context.Context, id int64, isActive bool) error {
	// 先读取路由相关字段，用于删除模型/展示名路由列表缓存，避免启停后仍命中旧列表。
	var old model.Channel
	_, _ = db.Engine.ID(id).Cols("id", "name", "model", "display_name").Get(&old)

	_, err := db.Engine.ID(id).Cols("is_active").Update(&model.Channel{IsActive: isActive})
	if err == nil {
		invalidateChannelRouteCaches(ctx, old)
	}
	return err
}

// UpdateChannel 更新渠道并删除缓存。
// 改名/改模型场景下，旧 name/model 对应的缓存键也必须失效，否则将残留 stale
// 数据直至 TTL 过期（最多 channelCacheTTL）。
func UpdateChannel(ctx context.Context, ch *model.Channel) error {
	if ch.BillingType == "custom" {
		return fmt.Errorf("custom 自定义脚本计费已停用")
	}
	// 先读旧记录，用于失效旧 name/model/display_name 缓存键
	var old model.Channel
	_, _ = db.Engine.ID(ch.ID).Cols("id", "name", "model", "display_name").Get(&old)

	_, err := db.Engine.Where("id = ?", ch.ID).AllCols().Update(ch)
	if err == nil {
		invalidateChannelRouteCaches(ctx, old, *ch)
	}
	return err
}

// DeleteChannel 永久删除数据库中的渠道。
func DeleteChannel(ctx context.Context, channelID int64) error {
	// 先加载渠道的 name/model/display_name，以便删除相关缓存条目。
	var ch model.Channel
	_, _ = db.Engine.ID(channelID).Cols("id", "name", "model", "display_name").Get(&ch)
	_, err := db.Engine.Where("id = ?", channelID).Delete(new(model.Channel))
	if err == nil {
		if ch.ID == 0 {
			ch.ID = channelID
		}
		invalidateChannelRouteCaches(ctx, ch)
	}
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// 负载均衡渠道选择
// ─────────────────────────────────────────────────────────────────────────────

const (
	channelModelListTTL           = 30 * time.Second
	channelModelCachePrefix       = "channel:model:v2:"
	legacyChannelModelCachePrefix = "channel:model:"
	// 错误率窗口：当渠道在 errRateWindow 内的错误率超过 errRateThreshold（需满足最少 errRateMinReqs 次请求）时跳过该渠道。
	errRateWindow    = 5 * time.Minute
	errRateThreshold = 0.5 // 错误率 50%
	errRateMinReqs   = 5   // 触发错误率过滤的最小请求数
)

// ChannelRoutingKey 返回用户请求时应填写的 model 值。
// 自定义展示名存在时，它就是对外路由键；否则使用标准模型名。
func ChannelRoutingKey(ch model.Channel) string {
	if displayName := strings.TrimSpace(ch.DisplayName); displayName != "" {
		return displayName
	}
	return strings.TrimSpace(ch.Model)
}

// SelectChannelStable 返回按售价升序排列的可用渠道列表。
// 稳定密钥使用此函数：先尝试最便宜的渠道，失败后依次尝试更贵的渠道。
func SelectChannelStable(ctx context.Context, modelName string, excludeIDs ...int64) ([]model.Channel, error) {
	return SelectChannelStableForUser(ctx, modelName, "", excludeIDs...)
}

// SelectChannelStableForUser 返回按当前用户可见售价升序排列的可用渠道列表。
// 同价时优先级高者优先，再按 ID 升序稳定排序。
func SelectChannelStableForUser(ctx context.Context, modelName, userGroup string, excludeIDs ...int64) ([]model.Channel, error) {
	channels, err := listChannelsByModel(ctx, modelName)
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("无可用渠道: %s", modelName)
	}

	excluded := make(map[int64]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		excluded[id] = true
	}

	var candidates []model.Channel
	for _, ch := range channels {
		if !excluded[ch.ID] && ch.IsActive {
			candidates = append(candidates, ch)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("所有渠道均已尝试或不可用")
	}

	// 按售价升序排列（最便宜的优先）；同价时优先级高者优先，再按 ID 升序稳定排序。
	sort.Slice(candidates, func(i, j int) bool {
		return stableChannelLess(candidates[i], candidates[j], userGroup)
	})
	return candidates, nil
}

// SelectChannelStableForUserByProtocol 返回指定协议的可用渠道，并按当前用户可见售价升序排列。
func SelectChannelStableForUserByProtocol(ctx context.Context, modelName, protocol, userGroup string, excludeIDs ...int64) ([]model.Channel, error) {
	channels, err := listChannelsByModel(ctx, modelName)
	if err != nil {
		return nil, err
	}
	channels = filterChannelsByProtocol(channels, protocol)
	if len(channels) == 0 {
		return nil, fmt.Errorf("无可用 %s 协议渠道: %s", protocol, modelName)
	}

	excluded := make(map[int64]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		excluded[id] = true
	}

	var candidates []model.Channel
	for _, ch := range channels {
		if !excluded[ch.ID] && ch.IsActive {
			candidates = append(candidates, ch)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("所有 %s 协议渠道均已尝试或不可用", protocol)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return stableChannelLess(candidates[i], candidates[j], userGroup)
	})
	return candidates, nil
}

func stableChannelLess(left, right model.Channel, userGroup string) bool {
	leftPrice := channelBasePriceForGroup(left, userGroup)
	rightPrice := channelBasePriceForGroup(right, userGroup)
	if leftPrice != rightPrice {
		return leftPrice < rightPrice
	}
	if left.Priority != right.Priority {
		return left.Priority > right.Priority
	}
	return left.ID < right.ID
}

// channelBasePrice 提取渠道的基础售价用于排序比较。
func channelBasePrice(ch model.Channel) float64 {
	return channelBasePriceForGroup(ch, "")
}

func channelBasePriceForGroup(ch model.Channel, userGroup string) float64 {
	cfg := billingcalc.EffectivePricingConfig(map[string]interface{}(ch.BillingConfig), userGroup)
	switch ch.BillingType {
	case "token":
		return mapFloat64(cfg, "input_price_per_1m_tokens") + mapFloat64(cfg, "output_price_per_1m_tokens")
	case "image":
		if raw, ok := cfg["size_prices"]; ok {
			if sp, ok := raw.(map[string]interface{}); ok {
				var min float64 = -1
				for _, v := range sp {
					if p, ok := toFloat64(v); ok && p > 0 && (min < 0 || p < min) {
						min = p
					}
				}
				if min >= 0 {
					return min
				}
			}
		}
		if sp := mapFloat64(cfg, "default_size_price"); sp > 0 {
			return sp
		}
		return mapFloat64(cfg, "base_price")
	case "count":
		if pricePerCall := mapFloat64(cfg, "price_per_call"); pricePerCall > 0 {
			return pricePerCall
		}
		return mapFloat64(cfg, "price_per_count")
	case "audio":
		return mapFloat64(cfg, "price_per_second")
	case "video":
		return mapFloat64(cfg, "price_per_second")
	default:
		return 0
	}
}

func applyChannelGroupPricing(cfg map[string]interface{}, group string) map[string]interface{} {
	if group == "" || cfg == nil {
		return cfg
	}
	pricingGroups, ok := cfg["pricing_groups"].(map[string]interface{})
	if !ok {
		return cfg
	}
	overrides, ok := pricingGroups[group].(map[string]interface{})
	if !ok {
		return cfg
	}
	merged := make(map[string]interface{}, len(cfg))
	for key, value := range cfg {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

func mapFloat64(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	f, _ := toFloat64(v)
	return f
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}

// SelectChannel 使用以下策略选取最优渠道：
//  1. 按优先级降序排序（选最高优先级组）
//  2. 错误率过滤（跳过近 5 分钟内错误率 >50% 的渠道）
//  3. 最高可用优先级组内按权重随机选取
//
// excludeIDs 允许调用方厒除已失败的渠道（用于重试）。
func SelectChannel(ctx context.Context, modelName string, excludeIDs ...int64) (*model.Channel, error) {
	channels, err := listChannelsByModel(ctx, modelName)
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("无可用渠道: %s", modelName)
	}

	excluded := make(map[int64]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		excluded[id] = true
	}

	// 删除已排除和不健康的渠道
	var candidates []model.Channel
	for _, ch := range channels {
		if excluded[ch.ID] {
			continue
		}
		if isChannelUnhealthy(ctx, ch.ID) {
			continue
		}
		candidates = append(candidates, ch)
	}

	// 若所有健康渠道均已厒除，则回退至所有未厒除渠道
	if len(candidates) == 0 {
		for _, ch := range channels {
			if !excluded[ch.ID] {
				candidates = append(candidates, ch)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("所有渠道均不可用，请稍后重试")
	}

	// 按优先级降序排序，选取最高优先级组
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority > candidates[j].Priority
	})
	topPriority := candidates[0].Priority
	var topTier []model.Channel
	for _, ch := range candidates {
		if ch.Priority == topPriority {
			topTier = append(topTier, ch)
		}
	}

	// 在最高优先级组内按权重随机选取
	selected := weightedRandom(topTier)
	return selected, nil
}

// SelectChannelByProtocol 使用与 SelectChannel 相同的策略，但只在指定协议渠道中选择。
func SelectChannelByProtocol(ctx context.Context, modelName, protocol string, excludeIDs ...int64) (*model.Channel, error) {
	channels, err := listChannelsByModel(ctx, modelName)
	if err != nil {
		return nil, err
	}
	channels = filterChannelsByProtocol(channels, protocol)
	if len(channels) == 0 {
		return nil, fmt.Errorf("无可用 %s 协议渠道: %s", protocol, modelName)
	}

	excluded := make(map[int64]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		excluded[id] = true
	}

	var candidates []model.Channel
	for _, ch := range channels {
		if excluded[ch.ID] {
			continue
		}
		if isChannelUnhealthy(ctx, ch.ID) {
			continue
		}
		candidates = append(candidates, ch)
	}

	if len(candidates) == 0 {
		for _, ch := range channels {
			if !excluded[ch.ID] {
				candidates = append(candidates, ch)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("所有 %s 协议渠道均不可用，请稍后重试", protocol)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority > candidates[j].Priority
	})
	topPriority := candidates[0].Priority
	var topTier []model.Channel
	for _, ch := range candidates {
		if ch.Priority == topPriority {
			topTier = append(topTier, ch)
		}
	}

	return weightedRandom(topTier), nil
}

// SelectChannelByWeight 用于重试场景：跳过优先级分组，直接对所有未排除的健康渠道
// 做加权随机选取。当健康渠道全部排除后回退到全部未排除渠道，保证重试不会空手而归。
func SelectChannelByWeight(ctx context.Context, modelName string, excludeIDs ...int64) (*model.Channel, error) {
	channels, err := listChannelsByModel(ctx, modelName)
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("无可用渠道: %s", modelName)
	}

	excluded := make(map[int64]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		excluded[id] = true
	}

	var candidates []model.Channel
	for _, ch := range channels {
		if excluded[ch.ID] {
			continue
		}
		if isChannelUnhealthy(ctx, ch.ID) {
			continue
		}
		candidates = append(candidates, ch)
	}

	// 健康渠道全部排除时，回退到所有未排除渠道（保证重试有渠道可用）
	if len(candidates) == 0 {
		for _, ch := range channels {
			if !excluded[ch.ID] {
				candidates = append(candidates, ch)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("所有渠道均不可用，请稍后重试")
	}

	return weightedRandom(candidates), nil
}

// RecordChannelSuccess 记录一次成功请求用于错误率统计。
func RecordChannelSuccess(ctx context.Context, channelID int64) {
	recordChannelEvent(ctx, channelID, "ok")
}

// RecordChannelError 记录一次失败请求用于错误率统计。
func RecordChannelError(ctx context.Context, channelID int64) {
	recordChannelEvent(ctx, channelID, "err")
}

// ─────────────────────────────────────────────────────────────────────────────
// 内部辅助函数
// ─────────────────────────────────────────────────────────────────────────────

func listChannelsByModel(ctx context.Context, modelName string) ([]model.Channel, error) {
	modelName = strings.TrimSpace(modelName)
	cacheKey := channelModelCachePrefix + modelName
	data, err := cache.Client.Get(ctx, cacheKey).Bytes()
	if err == nil {
		var channels []model.Channel
		if jsonErr := json.Unmarshal(data, &channels); jsonErr == nil {
			return channels, nil
		}
	}

	// 有 display_name 时只按 display_name 路由；未设置 display_name 时才按标准模型名路由。
	var channels []model.Channel
	err = db.Engine.Where("((TRIM(display_name) != '' AND TRIM(display_name) = ?) OR (TRIM(display_name) = '' AND model = ?)) AND is_active = true", modelName, modelName).
		OrderBy("priority DESC, id ASC").Find(&channels)
	if err != nil {
		return nil, err
	}
	if b, jsonErr := json.Marshal(channels); jsonErr == nil {
		cache.Client.Set(ctx, cacheKey, b, channelModelListTTL)
	}
	return channels, nil
}

func filterChannelsByProtocol(channels []model.Channel, protocol string) []model.Channel {
	if protocol == "" {
		return channels
	}
	filtered := make([]model.Channel, 0, len(channels))
	for _, ch := range channels {
		chProto := ch.Protocol
		if chProto == "" {
			chProto = "openai"
		}
		if chProto == protocol {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}

func weightedRandom(channels []model.Channel) *model.Channel {
	if len(channels) == 1 {
		return &channels[0]
	}
	total := 0
	for _, ch := range channels {
		w := ch.Weight
		if w <= 0 {
			w = 1
		}
		total += w
	}
	r := rand.Intn(total)
	for i, ch := range channels {
		w := ch.Weight
		if w <= 0 {
			w = 1
		}
		r -= w
		if r < 0 {
			return &channels[i]
		}
	}
	return &channels[0]
}

// isChannelUnhealthy 当渠道近期错误率超过 errRateThreshold 时返回 true。
// 使用每个渠道每个时间窗口对应的两个 Redis 计数器。
func isChannelUnhealthy(ctx context.Context, channelID int64) bool {
	window := time.Now().Truncate(errRateWindow).Unix()
	okKey := fmt.Sprintf("ch_stat:%d:%d:ok", channelID, window)
	errKey := fmt.Sprintf("ch_stat:%d:%d:err", channelID, window)

	okStr, _ := cache.Client.Get(ctx, okKey).Result()
	errStr, _ := cache.Client.Get(ctx, errKey).Result()
	okCount, _ := strconv.ParseInt(okStr, 10, 64)
	errCount, _ := strconv.ParseInt(errStr, 10, 64)
	total := okCount + errCount
	if total < errRateMinReqs {
		return false
	}
	return float64(errCount)/float64(total) > errRateThreshold
}

func recordChannelEvent(ctx context.Context, channelID int64, event string) {
	window := time.Now().Truncate(errRateWindow).Unix()
	key := fmt.Sprintf("ch_stat:%d:%d:%s", channelID, window, event)
	cache.Client.Incr(ctx, key)
	// TTL = 2 个窗口周期，确保旧数据干净过期
	cache.Client.Expire(ctx, key, errRateWindow*2)
}
