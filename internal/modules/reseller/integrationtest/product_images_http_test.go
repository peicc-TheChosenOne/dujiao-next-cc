package integrationtest

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	userhttp "github.com/dujiao-next/internal/modules/reseller/transport/http/user"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestProductImagesHTTPUsesAuthenticatedIdentityAndReturnsConflict(t *testing.T) {
	f := newImageFixture(t)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id, err := strconv.ParseUint(c.GetHeader("X-Test-User"), 10, 64); err == nil {
			c.Set("user_id", uint(id))
		}
	})
	userhttp.RegisterUserProductImagesRoutes(r.Group("/images"), userhttp.NewUserProductImagesHandler(f.service))
	path := "/images/product-images/" + strconv.Itoa(int(f.product.ID))
	request := func(method, path string, body io.Reader, contentType string, userID uint) (int, map[string]interface{}) {
		t.Helper()
		req := httptest.NewRequest(method, path, body)
		if userID > 0 {
			req.Header.Set("X-Test-User", strconv.Itoa(int(userID)))
		}
		req.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var result map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode %s: %v", w.Body.String(), err)
		}
		return w.Code, result
	}
	_, result := request(http.MethodGet, path, nil, "", 0)
	if result["status_code"] != float64(401) {
		t.Fatal(result)
	}
	file := imageFile(t, "custom.png", 0)
	source, err := file.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "custom.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, source); err != nil {
		t.Fatal(err)
	}
	op := uuid.NewString()
	_ = w.WriteField("revision", "0")
	_ = w.WriteField("operation_id", op)
	_ = w.WriteField("reseller_id", "99999")
	_ = w.Close()
	code, result := request(http.MethodPost, path+"/upload", &body, w.FormDataContentType(), f.user.ID)
	if code != 200 || result["status_code"] != float64(0) {
		t.Fatal(code, result)
	}
	data := result["data"].(map[string]interface{})
	if data["revision"] != float64(1) || data["operation_id"] != op || data["image_source"] != "custom" {
		t.Fatal(data)
	}
	_, other := request(http.MethodGet, path, nil, "", f.other.ID)
	if other["data"].(map[string]interface{})["image_source"] != "platform" {
		t.Fatal(other)
	}
	payload, _ := json.Marshal(map[string]interface{}{"images": []string{}, "revision": 0, "operation_id": uuid.NewString()})
	code, result = request(http.MethodPut, path, bytes.NewReader(payload), "application/json", f.user.ID)
	if code != http.StatusConflict || result["status_code"] != float64(409) {
		t.Fatal(code, result)
	}
	payload, _ = json.Marshal(map[string]interface{}{"images": []string{}, "revision": 1, "operation_id": uuid.NewString()})
	_, result = request(http.MethodPut, path, bytes.NewReader(payload), "application/json", f.user.ID)
	if result["status_code"] != float64(0) || result["data"].(map[string]interface{})["image_source"] != "platform" {
		t.Fatal(result)
	}
	_, result = request(http.MethodGet, "/images/product-images?page=1&page_size=1&keyword=service", nil, "", f.user.ID)
	if result["status_code"] != float64(0) || len(result["data"].([]interface{})) != 1 {
		t.Fatal(result)
	}
}

func TestProductImagesHTTPDetailUploadSingleFileAndReset(t *testing.T) {
	f := newImageFixture(t)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", f.user.ID) })
	userhttp.RegisterUserProductImagesRoutes(r.Group("/images"), userhttp.NewUserProductImagesHandler(f.service))
	base := "/images/product-images/" + strconv.Itoa(int(f.product.ID)) + "/detail"
	upload := func(count int, revision string) map[string]interface{} {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for i := 0; i < count; i++ {
			file := imageFile(t, "detail.png", 0)
			source, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			part, err := writer.CreateFormFile("file", "detail.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(part, source); err != nil {
				t.Fatal(err)
			}
			_ = source.Close()
		}
		_ = writer.WriteField("revision", revision)
		_ = writer.WriteField("operation_id", uuid.NewString())
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, base+"/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var result map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := upload(2, "0"); result["status_code"] != float64(400) {
		t.Fatal("multiple files accepted", result)
	}
	first := upload(1, "0")
	if first["status_code"] != float64(0) {
		t.Fatal(first)
	}
	data := first["data"].(map[string]interface{})
	if data["custom_detail_image"] == "" || data["detail_image_source"] != "custom" || len(data["custom_images"].([]interface{})) != 0 {
		t.Fatal(data)
	}
	second := upload(1, "1")
	if second["status_code"] != float64(0) {
		t.Fatal(second)
	}
	if second["data"].(map[string]interface{})["custom_detail_image"] == data["custom_detail_image"] {
		t.Fatal("detail was not replaced")
	}
	payload, _ := json.Marshal(map[string]interface{}{"revision": 2, "operation_id": uuid.NewString(), "detail_image": "https://external.example/forbidden.png"})
	req := httptest.NewRequest(http.MethodPut, base, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	reset := result["data"].(map[string]interface{})
	if result["status_code"] != float64(0) || reset["custom_detail_image"] != "" || reset["detail_image_source"] != "platform" || reset["revision"] != float64(3) {
		t.Fatal(result)
	}
}
