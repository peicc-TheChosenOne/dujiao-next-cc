package callbackclient

import (
	"net"
	"net/http"
	"time"

	"github.com/dujiao-next/internal/shared/netguard"
)

// newSafeHTTPClient 返回用于向下游回调地址发起请求的 HTTP 客户端：
// 拨号时解析域名并只连接公网地址，不跟随重定向。
func newSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           netguard.PublicDialContext(dialer),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
