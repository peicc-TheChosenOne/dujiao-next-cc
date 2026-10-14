package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateChecksProviderQuotaAndRecoversExistingHostname(t *testing.T) {
	for _, mode := range []string{"full", "existing", "malformed", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					t.Error("quota check must prevent POST")
					return
				}
				if strings.HasSuffix(r.URL.Path, "/quota") {
					switch mode {
					case "malformed":
						fmt.Fprint(w, `{"success":true,"result":{}}`)
					case "unavailable":
						w.WriteHeader(503)
					default:
						fmt.Fprint(w, `{"success":true,"result":{"used":100}}`)
					}
					return
				}
				if mode == "existing" {
					fmt.Fprint(w, `{"success":true,"result":[{"id":"existing","hostname":"shop.example.test"}]}`)
				} else {
					fmt.Fprint(w, `{"success":true,"result":[]}`)
				}
			}))
			defer server.Close()
			gateway := New("zone", "test-token", "", 100)
			gateway.baseURL = server.URL
			state, err := gateway.Create(context.Background(), "shop.example.test")
			if mode == "existing" {
				if err != nil || state.ID != "existing" {
					t.Fatalf("recovery failed: %v %+v", err, state)
				}
			} else {
				var coded interface{ ProvisioningCode() string }
				if !errors.As(err, &coded) {
					t.Fatalf("quota failure not rejected: %v", err)
				}
				if mode == "full" && coded.ProvisioningCode() != "quota_exceeded" {
					t.Fatalf("unexpected quota error %v", err)
				}
			}
			if posts != 0 {
				t.Fatal("quota failure created a billable hostname")
			}
		})
	}
}
func TestCreateDoesNotExceedProviderQuotaOnSubsequentRequests(t *testing.T) {
	used, posts := 99, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/quota") {
			fmt.Fprintf(w, `{"success":true,"result":{"used":%d}}`, used)
			return
		}
		if r.Method == "POST" {
			var input struct{ Hostname string }
			_ = json.NewDecoder(r.Body).Decode(&input)
			used++
			posts++
			fmt.Fprintf(w, `{"success":true,"result":{"id":"created","hostname":%q}}`, input.Hostname)
			return
		}
		fmt.Fprint(w, `{"success":true,"result":[]}`)
	}))
	defer server.Close()
	gateway := New("zone", "test-token", "", 100)
	gateway.baseURL = server.URL
	if _, err := gateway.Create(context.Background(), "first.example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Create(context.Background(), "second.example.test"); err == nil {
		t.Fatal("next request must be refused at budget limit")
	}
	if used != 100 || posts != 1 {
		t.Fatalf("used=%d posts=%d", used, posts)
	}
}
