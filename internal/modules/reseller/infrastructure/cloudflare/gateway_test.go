package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testGateway(t *testing.T, handler http.HandlerFunc) *Gateway {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/quota") {
			fmt.Fprint(w, `{"success":true,"result":{"used":0}}`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	gateway := New("test-zone", "test-token", "", 100)
	gateway.baseURL = server.URL
	return gateway
}

func TestCreateUsesHTTPValidationAndRecoversDuplicate(t *testing.T) {
	posts := 0
	g := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing authentication")
		}
		if r.Method == http.MethodPost {
			posts++
			var input struct {
				Hostname string
				SSL      struct{ Method, Type string }
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input.Hostname != "shop.example.test" || input.SSL.Method != "http" || input.SSL.Type != "dv" {
				t.Errorf("incorrect validation request: %+v", input)
			}
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"success":false,"errors":[{"message":"test-token provider detail"}]}`)
			return
		}
		if r.URL.Query().Get("hostname") != "shop.example.test" {
			t.Error("missing exact hostname filter")
		}
		fmt.Fprint(w, `{"success":true,"result":[{"id":"existing","hostname":"other.example.test"},{"id":"matched","hostname":"shop.example.test"}]}`)
	})
	state, err := g.Create(context.Background(), "shop.example.test")
	if err != nil || state.ID != "matched" || posts != 1 {
		t.Fatalf("duplicate recovery failed: %v %+v", err, state)
	}
}

func TestProviderErrorsAreRedactedAndDeleteIsIdempotent(t *testing.T) {
	for _, code := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			g := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				fmt.Fprint(w, "test-token private upstream detail")
			})
			_, err := g.Get(context.Background(), "hostname-id")
			if err == nil || strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe provider error: %v", err)
			}
			if code == 404 && g.Delete(context.Background(), "hostname-id") != nil {
				t.Error("deleting a missing hostname must succeed")
			}
		})
	}
}

func TestGatewayRejectsRedirectsAndInvalidResponses(t *testing.T) {
	leaked := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer destination.Close()
	g := testGateway(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	if _, err := g.Get(context.Background(), "id"); err == nil || leaked {
		t.Fatal("redirect must not forward credentials")
	}
	for _, body := range []string{`{"success":true,"result":{"id":"wrong","hostname":"shop.example.test"}}`, strings.Repeat("x", 1024*1024+1)} {
		g := testGateway(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := g.Get(context.Background(), "id"); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}

func TestGatewayHonorsCancellation(t *testing.T) {
	called := false
	g := testGateway(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Create(ctx, "shop.example.test"); err == nil || called {
		t.Fatal("cancelled request was sent")
	}
}
