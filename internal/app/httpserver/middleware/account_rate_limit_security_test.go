package middleware

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountRateLimitRejectsUnparseableAndOversizedBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"normal", "padded", "trailing_json"} {
		t.Run(mode, func(t *testing.T) {
			r := gin.New()
			_ = r.SetTrustedProxies(nil)
			r.Use(BodySizeLimitMiddleware(8<<20, 32<<20))
			r.Use(RateLimitMiddleware(nil, RateLimitRule{WindowSeconds: 900, MaxRequests: 20, BlockSeconds: 900}, KeyByIP))
			r.Use(RateLimitMiddleware(nil, RateLimitRule{WindowSeconds: 900, MaxRequests: 20, BlockSeconds: 900}, KeyByJSONField("email")))
			handled := 0
			r.POST("/login", func(c *gin.Context) {
				var req struct {
					Email    string `json:"email" binding:"required"`
					Password string `json:"password" binding:"required"`
				}
				if c.ShouldBindJSON(&req) != nil {
					c.AbortWithStatus(400)
					return
				}
				handled++
				c.Status(200)
			})
			last := 0
			for i := 1; i <= 21; i++ {
				body := `{"email":"target@example.test","password":"wrong"}`
				if mode == "padded" {
					body += strings.Repeat(" ", 65<<10)
				}
				if mode == "trailing_json" {
					body += `{}`
				}
				req := httptest.NewRequest("POST", "/login", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.RemoteAddr = fmt.Sprintf("203.0.113.%d:1234", i)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				last = w.Code
			}
			wantStatus, wantHandled := 429, 20
			if mode == "padded" {
				wantStatus, wantHandled = 413, 0
			}
			if mode == "trailing_json" {
				wantStatus, wantHandled = 400, 0
			}
			if last != wantStatus || handled != wantHandled {
				t.Fatalf("status=%d handled=%d; want %d/%d", last, handled, wantStatus, wantHandled)
			}
		})
	}
}
