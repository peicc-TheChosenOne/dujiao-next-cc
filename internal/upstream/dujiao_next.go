package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/shared/netguard"

	"github.com/google/uuid"
)

// upstreamHTTPError 上游返回非 200 时的结构化错误
type upstreamHTTPError struct {
	Status  int
	Code    string
	Message string
	Body    string
}

func (e *upstreamHTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("upstream responded with status %d (%s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("upstream responded with status %d: %s", e.Status, e.Body)
}

// extractUpstreamErrorCode 从错误链中提取 upstreamHTTPError.Code
func extractUpstreamErrorCode(err error) string {
	var ue *upstreamHTTPError
	if errors.As(err, &ue) {
		return ue.Code
	}
	return ""
}

// DujiaoNextAdapter Dujiao-Next 协议适配器
type DujiaoNextAdapter struct {
	baseURL     string
	apiKey      string
	apiSecret   string
	uploadsDir  string
	client      *http.Client
	imageClient *http.Client
}

// NewDujiaoNextAdapter 创建 Dujiao-Next 适配器
func NewDujiaoNextAdapter(conn *siteconnectiondomain.Connection, uploadsDir string) *DujiaoNextAdapter {
	return &DujiaoNextAdapter{
		baseURL:    strings.TrimRight(conn.BaseURL, "/"),
		apiKey:     conn.ApiKey,
		apiSecret:  conn.ApiSecret,
		uploadsDir: uploadsDir,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		imageClient: newImageDownloadClient(),
	}
}

const (
	// maxUpstreamImageBytes 单张上游图片的大小上限，与默认 upload.max_size 一致
	maxUpstreamImageBytes = 10 << 20
	// maxUpstreamImageRedirects 允许图片地址跳转的次数，每一跳都经过公网地址校验
	maxUpstreamImageRedirects = 3
)

// upstreamImageExtensions 按文件头识别出的可落盘图片类型；扩展名由内容决定，
// 不取自上游 URL，避免 .html 等文件以页面形式从本站源下发。
var upstreamImageExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// newImageDownloadClient 返回下载上游图片用的客户端。图片地址由上游站点控制，
// 拨号期只允许公网地址；不走环境代理，因为经代理转发时无法校验真实目标。
func newImageDownloadClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext:           netguard.PublicDialContext(dialer),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxUpstreamImageRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// Ping 连接测试
func (a *DujiaoNextAdapter) Ping(ctx context.Context) (*PingResult, error) {
	var result struct {
		OK bool `json:"ok"`
		PingResult
	}
	if err := a.doRequest(ctx, http.MethodPost, "/api/v1/upstream/ping", nil, &result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("ping failed")
	}
	return &result.PingResult, nil
}

// ListCategories 拉取上游分类列表
func (a *DujiaoNextAdapter) ListCategories(ctx context.Context) (*CategoryListResult, error) {
	var result struct {
		OK         bool               `json:"ok"`
		Categories []UpstreamCategory `json:"categories"`
	}
	if err := a.doRequest(ctx, http.MethodGet, "/api/v1/upstream/categories", nil, &result); err != nil {
		// 旧版上游不支持分类 API，返回空列表
		var ue *upstreamHTTPError
		if errors.As(err, &ue) && ue.Status == http.StatusNotFound {
			return &CategoryListResult{Supported: false, Categories: []UpstreamCategory{}}, nil
		}
		return nil, err
	}
	return &CategoryListResult{Supported: true, Categories: result.Categories}, nil
}

// ListProducts 拉取上游商品列表
func (a *DujiaoNextAdapter) ListProducts(ctx context.Context, opts ListProductsOpts) (*ProductListResult, error) {
	path := fmt.Sprintf("/api/v1/upstream/products?page=%d&page_size=%d", opts.Page, opts.PageSize)
	if opts.UpdatedAfter != nil {
		path += "&updated_after=" + opts.UpdatedAfter.Format(time.RFC3339)
	}
	if opts.IncludeInactive {
		path += "&include_inactive=true"
	}
	var result ProductListResult
	if err := a.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProduct 获取单个商品详情
// 上游已删除（软删）→ 返回 ErrUpstreamProductDeleted
// 旧版上游对下架商品也返回 404 product_unavailable → 返回 ErrUpstreamProductUnavailable
// 新版上游下架商品改为 200 + is_active=false，调用方应根据 IsActive 字段判断
func (a *DujiaoNextAdapter) GetProduct(ctx context.Context, productID uint) (*UpstreamProduct, error) {
	path := fmt.Sprintf("/api/v1/upstream/products/%d", productID)
	var result struct {
		OK      bool            `json:"ok"`
		Product UpstreamProduct `json:"product"`
	}
	if err := a.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		// 解析上游返回的 error_code 归一化为哨兵错误
		switch extractUpstreamErrorCode(err) {
		case "product_deleted", "product_not_found":
			return nil, ErrUpstreamProductDeleted
		case "product_unavailable":
			return nil, ErrUpstreamProductUnavailable
		}
		return nil, err
	}
	return &result.Product, nil
}

// CreateOrder 发起采购单
func (a *DujiaoNextAdapter) CreateOrder(ctx context.Context, req CreateUpstreamOrderReq) (*CreateUpstreamOrderResp, error) {
	var result CreateUpstreamOrderResp
	if err := a.doRequest(ctx, http.MethodPost, "/api/v1/upstream/orders", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetOrder 查询上游订单状态
func (a *DujiaoNextAdapter) GetOrder(ctx context.Context, orderID uint) (*UpstreamOrderDetail, error) {
	path := fmt.Sprintf("/api/v1/upstream/orders/%d", orderID)
	var result UpstreamOrderDetail
	if err := a.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelOrder 取消采购单
func (a *DujiaoNextAdapter) CancelOrder(ctx context.Context, orderID uint) error {
	path := fmt.Sprintf("/api/v1/upstream/orders/%d/cancel", orderID)
	var result struct {
		OK bool `json:"ok"`
	}
	if err := a.doRequest(ctx, http.MethodPost, path, nil, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("cancel order failed")
	}
	return nil
}

// DownloadImage 下载图片到本地
func (a *DujiaoNextAdapter) DownloadImage(ctx context.Context, imageURL string) (string, error) {
	fullURL, err := a.resolveImageURL(imageURL)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}

	resp, err := a.imageClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxUpstreamImageBytes {
		return "", fmt.Errorf("download image: size %d exceeds %d bytes", resp.ContentLength, maxUpstreamImageBytes)
	}

	// 按文件头确定类型与扩展名，非图片内容直接拒绝
	head := make([]byte, 512)
	n, err := io.ReadFull(resp.Body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("download image: %w", err)
	}
	head = head[:n]
	contentType := http.DetectContentType(head)
	ext, ok := upstreamImageExtensions[contentType]
	if !ok {
		return "", fmt.Errorf("download image: unsupported content type %q", contentType)
	}

	filename := uuid.New().String() + ext
	dir := filepath.Join(a.uploadsDir, "upstream")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create uploads dir: %w", err)
	}

	filePath := filepath.Join(dir, filename)
	f, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}

	body := io.LimitReader(io.MultiReader(bytes.NewReader(head), resp.Body), maxUpstreamImageBytes+1)
	written, err := io.Copy(f, body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && written > maxUpstreamImageBytes {
		err = fmt.Errorf("size exceeds %d bytes", maxUpstreamImageBytes)
	}
	if err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("write file: %w", err)
	}

	// 返回相对路径
	return "/uploads/upstream/" + filename, nil
}

// resolveImageURL 把上游给出的图片地址规范为绝对 http(s) URL，相对路径按上游站点补全。
func (a *DujiaoNextAdapter) resolveImageURL(imageURL string) (string, error) {
	raw := strings.TrimSpace(imageURL)
	if strings.HasPrefix(raw, "/") {
		raw = a.baseURL + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("download image: unsupported url %q", imageURL)
	}
	return parsed.String(), nil
}

// doRequest 发送签名请求
func (a *DujiaoNextAdapter) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
	}

	// 签名用的 path 不含 query string
	signPath := path
	if idx := strings.Index(path, "?"); idx > 0 {
		signPath = path[:idx]
	}

	timestamp := time.Now().Unix()
	signature := Sign(a.apiSecret, method, signPath, timestamp, bodyBytes)

	url := a.baseURL + path
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set(HeaderApiKey, a.apiKey)
	req.Header.Set(HeaderTimestamp, fmt.Sprintf("%d", timestamp))
	req.Header.Set(HeaderSignature, signature)
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Warnw("upstream_request_error",
			"method", method, "path", path,
			"status", resp.StatusCode, "body", string(respBody))
		// 尝试解析结构化错误响应
		var errPayload struct {
			ErrorCode    string `json:"error_code"`
			ErrorMessage string `json:"error_message"`
		}
		_ = json.Unmarshal(respBody, &errPayload)
		return &upstreamHTTPError{
			Status:  resp.StatusCode,
			Code:    errPayload.ErrorCode,
			Message: errPayload.ErrorMessage,
			Body:    string(respBody),
		}
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
