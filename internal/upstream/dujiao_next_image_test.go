package upstream

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
)

var testPNGHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// newLoopbackImageAdapter 换用 httptest 自带客户端：生产图片客户端会拒绝回环地址。
func newLoopbackImageAdapter(t *testing.T, server *httptest.Server) (*DujiaoNextAdapter, string) {
	t.Helper()
	dir := t.TempDir()
	adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: server.URL}, dir)
	adapter.imageClient = server.Client()
	return adapter, dir
}

func storedUpstreamFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "upstream"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read upstream dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestDownloadImageNamesFileByDetectedType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(testPNGHeader)
	}))
	t.Cleanup(server.Close)
	adapter, dir := newLoopbackImageAdapter(t, server)

	localPath, err := adapter.DownloadImage(context.Background(), "/uploads/evil.html")
	if err != nil {
		t.Fatalf("DownloadImage() error = %v", err)
	}
	if !strings.HasPrefix(localPath, "/uploads/upstream/") || !strings.HasSuffix(localPath, ".png") {
		t.Fatalf("local path = %q, want /uploads/upstream/*.png", localPath)
	}
	stored, err := os.ReadFile(filepath.Join(dir, "upstream", filepath.Base(localPath)))
	if err != nil || !bytes.Equal(stored, testPNGHeader) {
		t.Fatalf("stored file = %q, err = %v", stored, err)
	}
}

func TestDownloadImageRejectsNonImageContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("<html><script>alert(1)</script></html>"))
	}))
	t.Cleanup(server.Close)
	adapter, dir := newLoopbackImageAdapter(t, server)

	if _, err := adapter.DownloadImage(context.Background(), server.URL+"/cover.png"); err == nil {
		t.Fatal("DownloadImage() accepted HTML content")
	}
	if files := storedUpstreamFiles(t, dir); len(files) != 0 {
		t.Fatalf("rejected download left files: %v", files)
	}
}

func TestDownloadImageRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(testPNGHeader)
		writer.(http.Flusher).Flush() // 分块传输，不提前暴露 Content-Length
		_, _ = writer.Write(make([]byte, maxUpstreamImageBytes))
	}))
	t.Cleanup(server.Close)
	adapter, dir := newLoopbackImageAdapter(t, server)

	if _, err := adapter.DownloadImage(context.Background(), "/uploads/huge.png"); err == nil {
		t.Fatal("DownloadImage() accepted a body above the size limit")
	}
	if files := storedUpstreamFiles(t, dir); len(files) != 0 {
		t.Fatalf("oversized download left files: %v", files)
	}
}

func TestDownloadImageRejectsNonHTTPURL(t *testing.T) {
	adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: "https://upstream.example"}, t.TempDir())
	for _, raw := range []string{"file:///etc/passwd", "gopher://127.0.0.1:6379/_INFO", "javascript:alert(1)", ""} {
		if _, err := adapter.DownloadImage(context.Background(), raw); err == nil {
			t.Errorf("DownloadImage(%q) should fail", raw)
		}
	}
}

func TestDownloadImageDefaultClientRefusesLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		t.Errorf("loopback image target must not be reached")
		_, _ = writer.Write(testPNGHeader)
	}))
	t.Cleanup(server.Close)
	adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: server.URL}, t.TempDir())

	if _, err := adapter.DownloadImage(context.Background(), "/uploads/cover.png"); err == nil {
		t.Fatal("DownloadImage() with the default client should refuse a loopback target")
	}
}
