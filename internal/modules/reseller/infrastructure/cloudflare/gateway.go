package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
)

type Gateway struct {
	client                       *http.Client
	baseURL, zone, token, origin string
	maxHostnames                 int
	createMu                     sync.Mutex
}
type providerError string

func (e providerError) Error() string            { return string(e) }
func (e providerError) ProvisioningCode() string { return string(e) }

func New(zone, token, origin string, maxHostnames int) *Gateway {
	return &Gateway{client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, baseURL: "https://api.cloudflare.com/client/v4", zone: zone, token: token, origin: origin, maxHostnames: maxHostnames}
}

func (g *Gateway) Create(ctx context.Context, host string) (resellercontract.HostnameState, error) {
	var result resellercontract.HostnameState
	if !resellercontract.ValidProvisioningHost(host) {
		return result, providerError("validation_failed")
	}
	g.createMu.Lock()
	defer g.createMu.Unlock()
	var quota struct {
		Used *int `json:"used"`
	}
	if err := g.request(ctx, http.MethodGet, "/quota", nil, &quota); err != nil {
		return result, err
	}
	if quota.Used == nil || *quota.Used < 0 || g.maxHostnames < 1 {
		return result, providerError("validation_failed")
	}
	if *quota.Used >= g.maxHostnames {
		// 丢失创建响应的已有对象仍可恢复，达到额度时不再发送创建请求。
		if existing, err := g.findHostname(ctx, host); err == nil {
			return existing, nil
		}
		return result, providerError("quota_exceeded")
	}
	input := map[string]any{"hostname": host, "ssl": map[string]string{"method": "http", "type": "dv"}}
	if g.origin != "" {
		input["custom_origin_server"] = g.origin
	}
	err := g.request(ctx, http.MethodPost, "", input, &result)
	if err != nil && err.Error() == "validation_failed" {
		// A successful remote create may precede a lost response or a failed local commit.
		if existing, lookupErr := g.findHostname(ctx, host); lookupErr == nil {
			return existing, nil
		}
	}
	if err == nil && (result.ID == "" || result.Hostname != host) {
		err = providerError("validation_failed")
	}
	return result, err
}

func (g *Gateway) findHostname(ctx context.Context, host string) (resellercontract.HostnameState, error) {
	var rows []resellercontract.HostnameState
	if err := g.request(ctx, http.MethodGet, "?hostname="+url.QueryEscape(host), nil, &rows); err != nil {
		return resellercontract.HostnameState{}, err
	}
	for _, row := range rows {
		if row.Hostname == host && row.ID != "" {
			return row, nil
		}
	}
	return resellercontract.HostnameState{}, providerError("not_found")
}

func (g *Gateway) Get(ctx context.Context, id string) (resellercontract.HostnameState, error) {
	var result resellercontract.HostnameState
	err := g.request(ctx, http.MethodGet, "/"+url.PathEscape(id), nil, &result)
	if err == nil && (result.ID != id || result.Hostname == "") {
		err = providerError("validation_failed")
	}
	return result, err
}

func (g *Gateway) Delete(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	err := g.request(ctx, http.MethodDelete, "/"+url.PathEscape(id), nil, nil)
	if err != nil && err.Error() == "not_found" {
		return nil
	}
	return err
}

func (g *Gateway) request(ctx context.Context, method, suffix string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return providerError("validation_failed")
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+"/zones/"+url.PathEscape(g.zone)+"/custom_hostnames"+suffix, body)
	if err != nil {
		return providerError("api_unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(req)
	if err != nil {
		return providerError("api_unavailable")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 401, 403:
		return providerError("access_denied")
	case 404:
		return providerError("not_found")
	case 429:
		return providerError("rate_limited")
	}
	if response.StatusCode >= 500 {
		return providerError("api_unavailable")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerError("validation_failed")
	}
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 || json.Unmarshal(data, &envelope) != nil || !envelope.Success {
		return providerError("validation_failed")
	}
	if output != nil && json.Unmarshal(envelope.Result, output) != nil {
		return providerError("validation_failed")
	}
	return nil
}
