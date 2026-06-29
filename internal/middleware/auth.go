package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"fanapi/internal/config"
	"fanapi/internal/db"
	"fanapi/internal/model"
	"fanapi/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// applyAPIKeyAuth 将已验证的 API Key 写入 gin 上下文，并检查账户冻结状态。
// 返回 false 表示已向客户端写入错误响应，调用方应立即 return。
func applyAPIKeyAuth(c *gin.Context, apiKey *model.APIKey) bool {
	user := &model.User{}
	if found, _ := db.Engine.ID(apiKey.UserID).Cols("group", "is_active").Get(user); found {
		if !user.IsActive {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "账户已被冻结，请联系管理员"})
			return false
		}
		c.Set("user_group", user.Group)
	}
	c.Set("user_id", apiKey.UserID)
	c.Set("api_key_id", apiKey.ID)
	c.Set("key_type", effectiveAPIKeyType(apiKey.KeyType))
	c.Set("auth_type", "apikey")
	return true
}

func effectiveAPIKeyType(keyType string) string {
	if lowPriceKeysDisabled() {
		return "stable"
	}
	if keyType == "stable" {
		return "stable"
	}
	return "low_price"
}

func lowPriceKeysDisabled() bool {
	var setting model.SystemSetting
	found, _ := db.Engine.Where("key = ?", "show_low_price_key").Cols("value").Get(&setting)
	return found && strings.EqualFold(strings.TrimSpace(setting.Value), "false")
}

// Auth supports both X-API-Key header and Authorization: Bearer JWT.
func Auth(cfg *config.ServerConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Try API Key first
		rawKey := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if rawKey == "" {
			rawKey = strings.TrimSpace(c.GetHeader("X-Goog-Api-Key"))
		}
		// Gemini official clients usually send key in query for native route:
		// /v1beta/models/{model}:generateContent?key=...
		if rawKey == "" && strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") {
			rawKey = strings.TrimSpace(c.Query("key"))
		}
		if rawKey != "" {
			apiKey, err := service.LookupAPIKey(c.Request.Context(), rawKey)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "API Key 无效"})
				return
			}
			if !applyAPIKeyAuth(c, apiKey) {
				return
			}
			c.Next()
			return
		}

		// Try JWT Bearer
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			// First try to validate as API Key (supports "Authorization: Bearer sk-xxx")
			if apiKey, err := service.LookupAPIKey(c.Request.Context(), tokenStr); err == nil {
				if !applyAPIKeyAuth(c, apiKey) {
					return
				}
				c.Next()
				return
			}

			// Fall back to JWT
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(cfg.JWTSecret), nil
			})
			if err != nil || !token.Valid {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已过期，请重新登录"})
				return
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录凭证异常，请重新登录"})
				return
			}
			sub, ok := claims["sub"].(float64)
			if !ok {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录凭证异常，请重新登录"})
				return
			}
			userID := int64(sub)
			role, _ := claims["role"].(string)
			group, _ := claims["group"].(string)
			// 检查账户冻结状态（与 API Key 路径对称）
			{
				user := &model.User{}
				if found, _ := db.Engine.ID(userID).Cols("group", "is_active").Get(user); found {
					if !user.IsActive {
						c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "账户已被冻结，请联系管理员"})
						return
					}
					group = user.Group
				}
			}
			c.Set("user_id", userID)
			c.Set("role", role)
			c.Set("user_group", group)
			c.Set("auth_type", "jwt")

			// 兼容用户端旧 API Key（无 raw_key）：允许 JWT 携带选中的 key_id 调用 /v1 受限接口。
			selectedKeyIDRaw := strings.TrimSpace(c.GetHeader("X-API-Key-Id"))
			if selectedKeyIDRaw != "" {
				if selectedKeyID, parseErr := strconv.ParseInt(selectedKeyIDRaw, 10, 64); parseErr == nil && selectedKeyID > 0 {
					var selectedKey model.APIKey
					if found, _ := db.Engine.Where("id = ? AND user_id = ? AND is_active = true", selectedKeyID, userID).
						Cols("id", "key_type").
						Get(&selectedKey); found {
						keyType := selectedKey.KeyType
						c.Set("api_key_id", selectedKey.ID)
						c.Set("key_type", effectiveAPIKeyType(keyType))
						c.Set("auth_type", "jwt_with_key")
					}
				}
			}
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
	}
}

// APIKeyOnly rejects requests that are not authenticated via API Key.
func APIKeyOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		authType, _ := c.Get("auth_type")
		if authType != "apikey" && authType != "jwt_with_key" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "此接口仅支持 API Key 认证"})
			return
		}
		c.Next()
	}
}
