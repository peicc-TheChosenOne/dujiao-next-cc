package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	"github.com/dujiao-next/internal/upstream"

	"github.com/gin-gonic/gin"
)

func TestResolveAllowedOrigin(t *testing.T) {
	got := resolveAllowedOrigin("https://example.com", []string{"*"})
	if got != "*" {
		t.Fatalf("wildcard must return * instead of echoing the origin, got %s", got)
	}

	got = resolveAllowedOrigin("https://a.example.com", []string{"https://a.example.com", "https://b.example.com"})
	if got != "https://a.example.com" {
		t.Fatalf("allow-list should return matched origin, got %s", got)
	}

	got = resolveAllowedOrigin("https://a.example.com", []string{"*", "https://a.example.com"})
	if got != "https://a.example.com" {
		t.Fatalf("explicit origin should take precedence over wildcard, got %s", got)
	}

	got = resolveAllowedOrigin("https://x.example.com", []string{"https://a.example.com"})
	if got != "" {
		t.Fatalf("unmatched origin should be empty, got %s", got)
	}
}

func TestCORSMiddlewareOnlyAllowsCredentialsForExplicitOrigins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name            string
		allowedOrigins  []string
		wantOrigin      string
		wantCredentials string
	}{
		{name: "wildcard", allowedOrigins: []string{"*"}, wantOrigin: "*", wantCredentials: ""},
		{name: "explicit", allowedOrigins: []string{"https://shop.example.com"}, wantOrigin: "https://shop.example.com", wantCredentials: "true"},
	} {
		r := gin.New()
		r.Use(CORSMiddleware(config.CORSConfig{AllowedOrigins: tc.allowedOrigins, AllowCredentials: true}))
		r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Origin", "https://shop.example.com")
		r.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != tc.wantOrigin {
			t.Errorf("%s: Allow-Origin = %q, want %q", tc.name, got, tc.wantOrigin)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != tc.wantCredentials {
			t.Errorf("%s: Allow-Credentials = %q, want %q", tc.name, got, tc.wantCredentials)
		}
	}
}

func TestBodySizeLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodySizeLimitMiddleware(16, 64))
	r.POST("/echo", func(c *gin.Context) {
		var payload map[string]interface{}
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
	})

	send := func(body, contentType string, chunked bool) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if chunked {
			req.ContentLength = -1
		}
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := send(`{"a":"b"}`, "application/json", false); code != http.StatusOK {
		t.Fatalf("small body = %d, want 200", code)
	}
	if code := send(`{"a":"`+strings.Repeat("x", 32)+`"}`, "application/json", false); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared oversized body = %d, want 413", code)
	}
	if code := send(`{"a":"`+strings.Repeat("x", 32)+`"}`, "application/json", true); code != http.StatusBadRequest {
		t.Fatalf("streamed oversized body = %d, want the handler to stop reading at the limit", code)
	}
	if code := send(strings.Repeat("x", 32), "multipart/form-data; boundary=x", false); code == http.StatusRequestEntityTooLarge {
		t.Fatal("multipart body under its own limit must not be rejected as too large")
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"request_id": getRequestID(c)})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(requestIDHeader, "req-123")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	if w.Header().Get(requestIDHeader) != "req-123" {
		t.Fatalf("response request id want req-123 got %s", w.Header().Get(requestIDHeader))
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp["request_id"] != "req-123" {
		t.Fatalf("context request id want req-123 got %s", resp["request_id"])
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w2, req2)
	generated := w2.Header().Get(requestIDHeader)
	if generated == "" {
		t.Fatalf("generated request id should not be empty")
	}
	if resp := strings.TrimSpace(generated); resp == "" {
		t.Fatalf("generated request id should not be blank")
	}
}

func TestJWTAuthMiddlewareMissingSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(JWTAuthMiddleware("", nil))
	r.GET("/admin/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	var resp struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("status_code want 401 got %d", resp.StatusCode)
	}
}

type fakeUpstreamCredentialStore struct {
	cred *apicredentialdomain.ApiCredential
}

func (s fakeUpstreamCredentialStore) GetByApiKey(string) (*apicredentialdomain.ApiCredential, error) {
	return s.cred, nil
}

func (fakeUpstreamCredentialStore) TouchLastUsedAt(uint, time.Time) error { return nil }

func TestUpstreamAPIAuthRejectsReplayedWriteRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	cred := &apicredentialdomain.ApiCredential{
		ID: 1, UserID: 7, ApiKey: "replay-key-" + t.Name(), ApiSecret: "replay-secret",
		Status: constants.ApiCredentialStatusApproved, IsActive: true, LastUsedAt: &now,
		User: &userdomain.User{Status: constants.UserStatusActive},
	}
	r := gin.New()
	r.Use(UpstreamAPIAuthMiddleware(fakeUpstreamCredentialStore{cred: cred}))
	r.POST("/api/v1/upstream/orders", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/v1/upstream/products", func(c *gin.Context) { c.Status(http.StatusOK) })

	signed := func(method, path, body string) func() int {
		timestamp := time.Now().Unix()
		signature := upstream.Sign(cred.ApiSecret, method, path, timestamp, []byte(body))
		return func() int {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set(upstream.HeaderApiKey, cred.ApiKey)
			req.Header.Set(upstream.HeaderTimestamp, strconv.FormatInt(timestamp, 10))
			req.Header.Set(upstream.HeaderSignature, signature)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w.Code
		}
	}

	createOrder := signed(http.MethodPost, "/api/v1/upstream/orders", `{"sku_id":1}`)
	if code := createOrder(); code != http.StatusOK {
		t.Fatalf("first signed POST = %d, want 200", code)
	}
	if code := createOrder(); code != http.StatusUnauthorized {
		t.Fatalf("replayed signed POST = %d, want 401", code)
	}

	listProducts := signed(http.MethodGet, "/api/v1/upstream/products", "")
	for i := 0; i < 2; i++ {
		if code := listProducts(); code != http.StatusOK {
			t.Fatalf("signed GET #%d = %d, want 200", i+1, code)
		}
	}
}

func TestUploadsSecurityHeadersMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	for _, name := range []string{"photo.png", "page.html", "PAGE.HTML", "icon.svg", "manual.pdf"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	r := gin.New()
	r.Group("/uploads", UploadsSecurityHeadersMiddleware()).Static("/", root)

	for _, tc := range []struct {
		file       string
		attachment bool
	}{
		{file: "photo.png", attachment: false},
		{file: "page.html", attachment: true},
		{file: "PAGE.HTML", attachment: true},
		{file: "icon.svg", attachment: true},
		{file: "manual.pdf", attachment: true},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/"+tc.file, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.file, w.Code)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", tc.file, got)
		}
		gotAttachment := w.Header().Get("Content-Disposition") == "attachment"
		gotSandbox := strings.HasPrefix(w.Header().Get("Content-Security-Policy"), "sandbox")
		if gotAttachment != tc.attachment || gotSandbox != tc.attachment {
			t.Errorf("%s: attachment=%v sandbox=%v, want both %v", tc.file, gotAttachment, gotSandbox, tc.attachment)
		}
	}
}
