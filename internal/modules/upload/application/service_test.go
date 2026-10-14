package application

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/modules/upload/contract"
)

type memoryStore struct {
	data []byte
}

func (s *memoryStore) Save(input contract.StoreInput) (string, error) {
	data, err := io.ReadAll(input.Source)
	if err != nil {
		return "", err
	}
	s.data = data
	return "/uploads/" + input.Scene + "/" + input.Year + "/" + input.Month + "/" + input.Filename, nil
}

func TestUploadServiceSaveFileAllowsArchiveForTelegramScene(t *testing.T) {
	policy := Policy{
		MaxSize:           10 * 1024 * 1024,
		AllowedTypes:      []string{"image/jpeg", "image/png"},
		AllowedExtensions: []string{".jpg", ".png"},
	}
	store := &memoryStore{}
	service := NewService(policy, store)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "demo.zip")
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	if _, err := part.Write([]byte("fake zip content")); err != nil {
		t.Fatalf("write form content failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}

	reader := multipart.NewReader(&body, writer.Boundary())
	form, err := reader.ReadForm(1024 * 1024)
	if err != nil {
		t.Fatalf("read form failed: %v", err)
	}
	files := form.File["file"]
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d", len(files))
	}

	result, err := service.SaveFileWithMeta(files[0], "telegram")
	if err != nil {
		t.Fatalf("save file failed: %v", err)
	}
	savedPath := result.URL
	if filepath.Ext(savedPath) != ".zip" {
		t.Fatalf("expected .zip saved path, got %s", savedPath)
	}
	if string(store.data) != "fake zip content" {
		t.Fatalf("stored content got %q", store.data)
	}
}

func TestUploadServiceSaveFileSVG(t *testing.T) {
	policy := Policy{
		MaxSize:           10 * 1024 * 1024,
		AllowedTypes:      []string{"image/jpeg", "image/png", "image/svg+xml"},
		AllowedExtensions: []string{".jpg", ".png", ".svg"},
	}
	store := &memoryStore{}
	svc := NewService(policy, store)

	safeSVG := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40" fill="red"/></svg>`

	t.Run("safe SVG upload succeeds", func(t *testing.T) {
		fh := createMultipartFile(t, "icon.svg", []byte(safeSVG))
		result, err := svc.SaveFileWithMeta(fh, "common")
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		path := result.URL
		if filepath.Ext(path) != ".svg" {
			t.Fatalf("expected .svg extension, got %s", path)
		}
		if string(store.data) != safeSVG {
			t.Fatalf("stored SVG mismatch: %q", store.data)
		}
	})

	t.Run("SVG with script tag is rejected", func(t *testing.T) {
		malicious := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
		fh := createMultipartFile(t, "bad.svg", []byte(malicious))
		_, err := svc.SaveFileWithMeta(fh, "common")
		if err == nil {
			t.Fatal("expected error for SVG with script tag")
		}
		if !strings.Contains(err.Error(), "<script>") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("SVG with event handler is rejected", func(t *testing.T) {
		malicious := `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><circle cx="50" cy="50" r="40"/></svg>`
		fh := createMultipartFile(t, "bad2.svg", []byte(malicious))
		_, err := svc.SaveFileWithMeta(fh, "common")
		if err == nil {
			t.Fatal("expected error for SVG with event handler")
		}
		if !strings.Contains(err.Error(), "onload") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("SVG with javascript protocol is rejected", func(t *testing.T) {
		malicious := `<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:alert(1)"><circle cx="50" cy="50" r="40"/></a></svg>`
		fh := createMultipartFile(t, "bad3.svg", []byte(malicious))
		_, err := svc.SaveFileWithMeta(fh, "common")
		if err == nil {
			t.Fatal("expected error for SVG with javascript protocol")
		}
	})

	t.Run("SVG with foreignObject is rejected", func(t *testing.T) {
		malicious := `<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body xmlns="http://www.w3.org/1999/xhtml"><div>hello</div></body></foreignObject></svg>`
		fh := createMultipartFile(t, "bad4.svg", []byte(malicious))
		_, err := svc.SaveFileWithMeta(fh, "common")
		if err == nil {
			t.Fatal("expected error for SVG with foreignObject")
		}
	})

	t.Run("SVG with XML declaration", func(t *testing.T) {
		xmlSVG := `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="blue"/></svg>`
		fh := createMultipartFile(t, "xml.svg", []byte(xmlSVG))
		result, err := svc.SaveFileWithMeta(fh, "common")
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		path := result.URL
		if filepath.Ext(path) != ".svg" {
			t.Fatalf("expected .svg extension, got %s", path)
		}
	})
}

func TestIsSVGContent(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{"svg tag", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`, true},
		{"xml declaration", `<?xml version="1.0"?><svg></svg>`, true},
		{"not svg", `<html><body></body></html>`, false},
		{"plain text", `hello world`, false},
		{"empty", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSVGContent([]byte(tt.input))
			if got != tt.expect {
				t.Errorf("isSVGContent(%q) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}

func TestValidateSVGSafety(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"safe svg", `<svg xmlns="http://www.w3.org/2000/svg"><circle cx="50" cy="50" r="40"/></svg>`, false},
		{"script tag", `<svg><script>alert(1)</script></svg>`, true},
		{"onclick", `<svg onclick="alert(1)"></svg>`, true},
		{"javascript href", `<svg><a href="javascript:void(0)"></a></svg>`, true},
		{"data uri html", `<svg><image href="data:text/html,<h1>hi</h1>"/></svg>`, true},
		{"foreignObject", `<svg><foreignObject></foreignObject></svg>`, true},
		{"onload tab before equals", "<svg xmlns=\"http://www.w3.org/2000/svg\" onload\t=\"alert(1)\"></svg>", true},
		{"onload newline before equals", "<svg onload\n=\"alert(1)\"></svg>", true},
		{"onload double space", `<svg onload  ="alert(1)"></svg>`, true},
		{"unlisted event attr onbegin", `<svg><set attributeName="x" onbegin="alert(1)"/></svg>`, true},
		{"entity encoded javascript", `<svg><a href="&#106;avascript:alert(1)"></a></svg>`, true},
		{"xlink href javascript", `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="javascript:alert(1)"></a></svg>`, true},
		{"xml-stylesheet pi", `<?xml-stylesheet href="http://evil/x.css"?><svg></svg>`, true},
		{"entity declaration", `<!DOCTYPE svg [<!ENTITY x "y">]><svg>&x;</svg>`, true},
		{"malformed xml", `<svg onload="alert(1)"`, true},
		{"safe with doctype and style", `<?xml version="1.0"?><!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd"><svg xmlns="http://www.w3.org/2000/svg"><style>.a{fill:red}</style><rect class="a" width="10" height="10"/></svg>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSVGSafety([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSVGSafety() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// createMultipartFile 辅助函数：创建模拟的 multipart 文件
func createMultipartFile(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form content failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	reader := multipart.NewReader(&body, writer.Boundary())
	form, err := reader.ReadForm(1024 * 1024)
	if err != nil {
		t.Fatalf("read form failed: %v", err)
	}
	files := form.File["file"]
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d", len(files))
	}
	return files[0]
}

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newPNGOnlyUploadService(store contract.Store) *Service {
	return NewService(Policy{
		MaxSize:           1 << 20,
		AllowedTypes:      []string{"image/png"},
		AllowedExtensions: []string{".png"},
	}, store)
}

func TestTelegramSceneRejectsExecutableContent(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  []byte
	}{
		{name: "html page", filename: "x.html", content: []byte("<html><script>alert(1)</script></html>")},
		{name: "svg image", filename: "x.svg", content: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{name: "html disguised as pdf", filename: "x.pdf", content: []byte("<!DOCTYPE html><script>alert(1)</script>")},
		{name: "no extension", filename: "payload", content: []byte("plain")},
	}
	for _, tc := range cases {
		store := &memoryStore{}
		_, err := newPNGOnlyUploadService(store).SaveFileWithMeta(createMultipartFile(t, tc.filename, tc.content), "telegram")
		var validation *contract.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("%s: error = %v, want a validation error", tc.name, err)
		}
		if store.data != nil {
			t.Errorf("%s: rejected upload was stored", tc.name)
		}
	}
}

func TestTelegramSceneAcceptsDocumentsAndImages(t *testing.T) {
	for _, tc := range []struct {
		filename string
		content  []byte
	}{
		{filename: "manual.pdf", content: []byte("%PDF-1.4\n1 0 obj\n")},
		{filename: "cover.png", content: testPNGBytes(t)},
	} {
		store := &memoryStore{}
		result, err := newPNGOnlyUploadService(store).SaveFileWithMeta(createMultipartFile(t, tc.filename, tc.content), "telegram")
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tc.filename, err)
		}
		if filepath.Ext(result.URL) != filepath.Ext(tc.filename) || !bytes.Equal(store.data, tc.content) {
			t.Fatalf("%s: upload was not stored as-is (url=%s)", tc.filename, result.URL)
		}
	}
}

func TestRegularSceneStillUsesConfiguredPolicy(t *testing.T) {
	service := newPNGOnlyUploadService(&memoryStore{})
	if _, err := service.SaveFileWithMeta(createMultipartFile(t, "photo.png", testPNGBytes(t)), "product"); err != nil {
		t.Fatalf("allowed png rejected: %v", err)
	}
	if _, err := service.SaveFileWithMeta(createMultipartFile(t, "manual.pdf", []byte("%PDF-1.4\n")), "product"); err == nil {
		t.Fatal("product scene must keep rejecting extensions outside the configured policy")
	}
}
