package middleware

import (
	"io"
	"net/http"
	"time"

	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/upstream"

	"github.com/gin-gonic/gin"
)

const upstreamUserIDKey = "upstream_user_id"
const upstreamCredentialIDKey = "upstream_credential_id"

// UpstreamCredentialStore 只暴露签名鉴权链路所需的凭证能力。
type UpstreamCredentialStore interface {
	GetByApiKey(apiKey string) (*apicredentialdomain.ApiCredential, error)
	TouchLastUsedAt(id uint, at time.Time) error
}

// UpstreamAPIAuthMiddleware 上游 API 签名鉴权中间件
func UpstreamAPIAuthMiddleware(credRepo UpstreamCredentialStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader(upstream.HeaderApiKey)
		timestampStr := c.GetHeader(upstream.HeaderTimestamp)
		signature := c.GetHeader(upstream.HeaderSignature)

		if apiKey == "" || timestampStr == "" || signature == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error_code": "missing_auth_headers", "error_message": "missing authentication headers"})
			return
		}

		timestamp, err := upstream.ParseTimestamp(timestampStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error_code": "invalid_timestamp", "error_message": "invalid timestamp"})
			return
		}

		if !upstream.IsTimestampValid(timestamp) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error_code": "timestamp_expired", "error_message": "timestamp expired"})
			return
		}

		cred, err := credRepo.GetByApiKey(apiKey)
		if err != nil {
			logger.Errorw("upstream_auth_db_error", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"ok": false, "error_code": "internal_error", "error_message": "internal error"})
			return
		}
		if cred == nil || cred.Status != constants.ApiCredentialStatusApproved || !cred.IsActive {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"ok": false, "error_code": "invalid_api_key", "error_message": "api key is invalid or disabled"})
			return
		}
		if cred.User == nil || cred.User.Status != constants.UserStatusActive {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"ok": false, "error_code": "user_disabled", "error_message": "user account is disabled"})
			return
		}

		// 读取 body 用于签名验证（限制最大 10MB 防止内存耗尽）
		var body []byte
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)
			body, err = io.ReadAll(c.Request.Body)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"ok": false, "error_code": "bad_request", "error_message": "failed to read request body"})
				return
			}
			// 重置 body 供后续 handler 读取
			c.Request.Body = io.NopCloser(
				&bodyReader{data: body},
			)
		}

		method := c.Request.Method
		path := c.Request.URL.Path

		if !upstream.Verify(cred.ApiSecret, method, path, signature, timestamp, body) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error_code": "invalid_signature", "error_message": "signature verification failed"})
			return
		}
		if !claimSignedRequest(c, "upstream", cred.ApiKey, signature) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error_code": "request_replayed", "error_message": "request signature has already been used"})
			return
		}

		// 更新最后使用时间（异步、只写单列、60s 节流）
		now := time.Now()
		if cred.LastUsedAt == nil || now.Sub(*cred.LastUsedAt) > time.Minute {
			credID := cred.ID
			go func() {
				if updateErr := credRepo.TouchLastUsedAt(credID, now); updateErr != nil {
					logger.Warnw("upstream_auth_update_last_used_failed", "error", updateErr)
				}
			}()
		}

		// 将凭证信息存入 context
		c.Set(upstreamUserIDKey, cred.UserID)
		c.Set(upstreamCredentialIDKey, cred.ID)
		c.Set("upstream_api_key", cred.ApiKey)

		c.Next()
	}
}

// signedRequestReplayTTL 覆盖签名时间戳允许的偏差范围（前后各 MaxTimestampSkew 秒）。
const signedRequestReplayTTL = 2*upstream.MaxTimestampSkew*time.Second + time.Second

// claimSignedRequest 对会改变状态的签名请求做一次性占用：同一签名在有效期内只能使用一次，
// 截获的写请求无法被重放。GET/HEAD 不占用：签名不含 query，同一秒内合法的并发查询会共用签名。
func claimSignedRequest(c *gin.Context, scope, key, signature string) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return cache.ClaimOnce(c.Request.Context(), "replay:"+scope+":"+key+":"+signature, signedRequestReplayTTL)
}

// bodyReader 实现 io.Reader，用于重置 body
type bodyReader struct {
	data   []byte
	offset int
}

func (r *bodyReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
