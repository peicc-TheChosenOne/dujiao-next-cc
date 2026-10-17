package integrationtest

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"reflect"
	"sync"
	"testing"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellerstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/dujiao-next/internal/modules/reseller/transport/http/presenter"
	uploadapp "github.com/dujiao-next/internal/modules/upload/application"
	uploadcontract "github.com/dujiao-next/internal/modules/upload/contract"
	uploadlocal "github.com/dujiao-next/internal/modules/upload/infrastructure/localstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/jsonslice"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type imageFixture struct {
	db       *gorm.DB
	store    *resellerstore.Store
	service  *resellerapp.ProductImageService
	uploader *uploadapp.Service
	user     userdomain.User
	profile  resellerdomain.Profile
	other    userdomain.User
	product  productdomain.Product
}

func TestAdminProductImagesIndependentListSourcesAndLifecycle(t *testing.T) {
	f := newImageFixture(t)
	// 图片查询不得依赖定价记录。
	if err := f.db.Where("reseller_id = ?", f.profile.ID).Delete(&resellerdomain.ProductSetting{}).Error; err != nil {
		t.Fatal(err)
	}
	state, err := f.service.UploadAdminProductImage(f.profile.ID, f.product.ID, imageFile(t, "a.png", 0), 0, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.service.UploadAdminProductImage(f.profile.ID, f.product.ID, imageFile(t, "b.png", 0), state.Config.Revision, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	images := []string{state.Config.Images[1], state.Config.Images[0]}
	state, err = f.service.UpdateAdminProductImages(f.profile.ID, f.product.ID, images, state.Config.Revision, uuid.NewString())
	if err != nil || !reflect.DeepEqual([]string(state.Config.Images), images) {
		t.Fatalf("reorder: %+v %v", state, err)
	}
	state, err = f.service.UploadAdminProductImage(f.profile.ID, f.product.ID, imageFile(t, "detail.png", 0), state.Config.Revision, uuid.NewString(), true)
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := f.service.ListAdminProductImages(resellercontract.ProductImageAdminFilter{Page: 1, PageSize: 1, ImageSource: "custom", DetailSource: "custom"})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Config.ResellerID != f.profile.ID {
		t.Fatalf("custom list: %+v %d %v", rows, total, err)
	}
	rows, total, err = f.service.ListAdminProductImages(resellercontract.ProductImageAdminFilter{Page: 2, PageSize: 1})
	if err != nil || total != 1 || len(rows) != 0 {
		t.Fatalf("pagination: %d %d %v", len(rows), total, err)
	}
	if _, err := f.service.UpdateAdminProductImages(f.profile.ID, f.product.ID, nil, 0, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesChanged) {
		t.Fatalf("stale update: %v", err)
	}
	if _, err := f.service.UpdateAdminProductImages(f.profile.ID, f.product.ID, []string{"https://example.test/foreign.png"}, state.Config.Revision, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesInvalid) {
		t.Fatalf("foreign image: %v", err)
	}
	state, err = f.service.UpdateAdminProductImages(f.profile.ID, f.product.ID, []string{}, state.Config.Revision, uuid.NewString())
	if err != nil || state.Config.DetailImage == "" {
		t.Fatalf("independent gallery reset: %+v %v", state, err)
	}
	rows, total, err = f.service.ListAdminProductImages(resellercontract.ProductImageAdminFilter{ImageSource: "platform", DetailSource: "custom"})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("detail-only list: %+v %d %v", rows, total, err)
	}
	userState, err := f.service.GetUserProductImages(f.user.ID, f.product.ID)
	if err != nil || userState.Config.DetailImage != state.Config.DetailImage {
		t.Fatalf("user/admin parity: %+v %v", userState, err)
	}
	other, err := f.service.GetUserProductImages(f.other.ID, f.product.ID)
	if err != nil || len(other.Config.Images) != 0 || other.Config.DetailImage != "" {
		t.Fatalf("tenant isolation: %+v %v", other, err)
	}
	state, err = f.service.ResetAdminProductDetailImage(f.profile.ID, f.product.ID, state.Config.Revision, uuid.NewString())
	if err != nil || state.Config.ID == 0 || state.Config.Revision != 6 {
		t.Fatalf("reset preserves version: %+v %v", state, err)
	}
	_, total, err = f.service.ListAdminProductImages(resellercontract.ProductImageAdminFilter{})
	if err != nil || total != 0 {
		t.Fatalf("empty overrides omitted: %d %v", total, err)
	}
	rows, total, err = f.service.ListAdminProductImages(resellercontract.ProductImageAdminFilter{Scope: "all", ResellerID: f.profile.ID, ProductID: f.product.ID})
	if err != nil || total != 1 || rows[0].Config.Revision != state.Config.Revision {
		t.Fatalf("all active products: %+v %d %v", rows, total, err)
	}
	for _, input := range []resellercontract.ProductImageAdminFilter{{Scope: "all"}, {Scope: "invalid"}, {ImageSource: "invalid"}, {DetailSource: "invalid"}} {
		if _, _, err := f.service.ListAdminProductImages(input); !errors.Is(err, resellercontract.ErrProductImagesInvalid) {
			t.Fatalf("invalid filter %+v: %v", input, err)
		}
	}
}

func TestAdminProductImagesReadonlyInactiveAndValidation(t *testing.T) {
	f := newImageFixture(t)
	for _, file := range []*multipart.FileHeader{imageFile(t, "oversize.png", int(resellerapp.ProductImageMaxSize)+1), imageFileBytes(t, "fake.png", []byte("not an image"))} {
		if _, err := f.service.UploadAdminProductImage(f.profile.ID, f.product.ID, file, 0, uuid.NewString(), false); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	if err := f.db.Model(&f.profile).Update("status", resellerdomain.ProfileStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	state, err := f.service.GetAdminProductImages(f.profile.ID, f.product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if presenter.NewAdminResellerProductImagesResp(state.Product, state.Config, state.Profile).CanEdit {
		t.Fatal("disabled reseller is editable")
	}
	if _, err := f.service.UploadAdminProductImage(f.profile.ID, f.product.ID, imageFile(t, "a.png", 0), 0, uuid.NewString(), false); !errors.Is(err, resellercontract.ErrProfileInactive) {
		t.Fatalf("disabled write: %v", err)
	}
	if err := f.db.Model(&f.profile).Update("status", resellerdomain.ProfileStatusActive).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&f.product).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	state, err = f.service.GetAdminProductImages(f.profile.ID, f.product.ID)
	if err != nil || presenter.NewAdminResellerProductImagesResp(state.Product, state.Config, state.Profile).CanEdit {
		t.Fatalf("inactive product read: %+v %v", state, err)
	}
	if _, err := f.service.UploadAdminProductImage(f.profile.ID, f.product.ID, imageFile(t, "a.png", 0), 0, uuid.NewString(), false); err == nil {
		t.Fatal("inactive product write accepted")
	}
}

func TestProductImagesDetailReplacementIsolationAndGalleryIndependence(t *testing.T) {
	f := newImageFixture(t)
	platformContent := jsonmap.JSON{"zh-CN": "总店详情", "zh-TW": "總店詳情", "en-US": "Platform details"}
	if err := f.db.Model(&f.product).Update("content_json", platformContent).Error; err != nil {
		t.Fatal(err)
	}
	state, err := f.service.UploadUserProductDetailImage(f.user.ID, f.product.ID, imageFile(t, "detail.png", 512000), 0, uuid.NewString())
	if err != nil || state.Config.DetailImage == "" || len(state.Config.Images) != 0 {
		t.Fatalf("first detail: %+v %v", state, err)
	}
	first := state.Config.DetailImage
	other, err := f.service.GetUserProductImages(f.other.ID, f.product.ID)
	if err != nil || other.Config.DetailImage != "" {
		t.Fatalf("other shop: %+v %v", other, err)
	}
	state, err = f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "gallery.png", 0), 1, uuid.NewString())
	if err != nil || state.Config.DetailImage != first || len(state.Config.Images) != 1 {
		t.Fatalf("gallery upload: %+v %v", state, err)
	}
	gallery := state.Config.Images[0]
	state, err = f.service.UploadUserProductDetailImage(f.user.ID, f.product.ID, imageFile(t, "replacement.png", 0), 2, uuid.NewString())
	if err != nil || state.Config.DetailImage == first || state.Config.Images[0] != gallery {
		t.Fatalf("replacement: %+v %v", state, err)
	}
	replacement := state.Config.DetailImage
	if _, err := f.service.ResetUserProductDetailImage(f.user.ID, f.product.ID, 2, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesChanged) {
		t.Fatalf("stale detail reset: %v", err)
	}
	state, err = f.service.UpdateUserProductImages(f.user.ID, f.product.ID, []string{}, 3, uuid.NewString())
	if err != nil || len(state.Config.Images) != 0 || state.Config.DetailImage != replacement {
		t.Fatalf("gallery reset: %+v %v", state, err)
	}
	state, err = f.service.ResetUserProductDetailImage(f.user.ID, f.product.ID, 4, uuid.NewString())
	if err != nil || state.Config.DetailImage != "" || state.Config.Revision != 5 {
		t.Fatalf("detail reset: %+v %v", state, err)
	}
	resp := presenter.NewResellerProductImagesResp(state.Product, state.Config)
	if resp.DetailImageSource != "platform" || resp.CustomDetailImage != "" {
		t.Fatal(resp)
	}
	for revision := uint64(5); revision < 10; revision++ {
		if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "gallery.png", 0), revision, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	state, err = f.service.UploadUserProductDetailImage(f.user.ID, f.product.ID, imageFile(t, "detail.png", 0), 10, uuid.NewString())
	if err != nil || len(state.Config.Images) != 5 || state.Config.DetailImage == "" {
		t.Fatalf("full gallery detail upload: %+v %v", state, err)
	}
	pricing := resellerapp.NewProductSettingService(f.store, productgormstore.NewProductStore(f.db))
	if err := pricing.ResetUserProductSetting(f.user.ID, f.product.ID, 0); err != nil {
		t.Fatal(err)
	}
	state, err = f.service.GetUserProductImages(f.user.ID, f.product.ID)
	if err != nil || state.Config.DetailImage == "" || len(state.Config.Images) != 5 {
		t.Fatalf("pricing reset: %+v %v", state, err)
	}
	state, err = f.service.ResetUserProductDetailImage(f.user.ID, f.product.ID, 11, uuid.NewString())
	if err != nil || len(state.Config.Images) != 5 || state.Config.DetailImage != "" {
		t.Fatalf("independent reset: %+v %v", state, err)
	}
	var main productdomain.Product
	if err := f.db.First(&main, f.product.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(main.ContentJSON, platformContent) {
		t.Fatal("platform content overwritten")
	}
	rows, _, err := f.service.ListUserProductImages(f.user.ID, resellerapp.ProductSettingUserListInput{Page: 1, PageSize: 20})
	if err != nil || rows[0].Config.Revision != 12 || rows[0].Config.DetailImage != "" {
		t.Fatalf("list recovery: %+v %v", rows, err)
	}
}

func TestProductImagesDetailValidationAndConcurrentGalleryChange(t *testing.T) {
	f := newImageFixture(t)
	for _, tc := range []struct {
		name string
		size int
	}{{"large.png", 512001}, {"invalid.svg", 0}} {
		if _, err := f.service.UploadUserProductDetailImage(f.user.ID, f.product.ID, imageFile(t, tc.name, tc.size), 0, uuid.NewString()); err == nil {
			t.Fatalf("accepted %s", tc.name)
		}
	}
	barrier := synchronizedImageUploader{f.uploader, make(chan struct{}, 2), make(chan struct{})}
	svc := resellerapp.NewProductImageService(f.store, barrier)
	results := make(chan error, 2)
	file1, file2 := imageFile(t, "detail.png", 0), imageFile(t, "gallery.png", 0)
	go func() {
		_, err := svc.UploadUserProductDetailImage(f.user.ID, f.product.ID, file1, 0, uuid.NewString())
		results <- err
	}()
	go func() {
		_, err := svc.UploadUserProductImage(f.user.ID, f.product.ID, file2, 0, uuid.NewString())
		results <- err
	}()
	<-barrier.arrived
	<-barrier.arrived
	close(barrier.release)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, resellercontract.ErrProductImagesChanged) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	state, err := f.service.GetUserProductImages(f.user.ID, f.product.ID)
	if err != nil || success != 1 || conflict != 1 || state.Config.Revision != 1 {
		t.Fatalf("cross-operation race: %+v %v", state, err)
	}
	if state.Config.DetailImage == "" {
		state, err = f.service.UploadUserProductDetailImage(f.user.ID, f.product.ID, file1, 1, uuid.NewString())
	} else {
		state, err = f.service.UploadUserProductImage(f.user.ID, f.product.ID, file2, 1, uuid.NewString())
	}
	if err != nil || len(state.Config.Images) != 1 || state.Config.DetailImage == "" {
		t.Fatalf("retry lost other image: %+v %v", state, err)
	}
	if err := f.db.Model(&f.profile).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.UploadUserProductDetailImage(f.user.ID, f.product.ID, file1, 2, uuid.NewString()); !errors.Is(err, resellercontract.ErrProfileInactive) {
		t.Fatalf("inactive: %v", err)
	}
}

func TestProductImagesDetailMigrationPreservesExistingGalleryAndRevision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Exec(`CREATE TABLE reseller_product_images (id INTEGER PRIMARY KEY, reseller_id INTEGER NOT NULL, product_id INTEGER NOT NULL, images JSON NOT NULL, revision INTEGER NOT NULL, operation_id VARCHAR(36) NOT NULL, created_at DATETIME, updated_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	op := uuid.NewString()
	if err := db.Exec(`INSERT INTO reseller_product_images (id,reseller_id,product_id,images,revision,operation_id) VALUES (1,10,20,?,7,?)`, jsonslice.Strings{"/uploads/existing.png"}, op).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&resellerdomain.ProductImages{}); err != nil {
		t.Fatal(err)
	}
	var config resellerdomain.ProductImages
	if err := db.First(&config, 1).Error; err != nil {
		t.Fatal(err)
	}
	if config.DetailImage != "" || config.Revision != 7 || config.OperationID != op || !reflect.DeepEqual(config.Images, jsonslice.Strings{"/uploads/existing.png"}) {
		t.Fatal(config)
	}
}

func newImageFixture(t *testing.T) imageFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&userdomain.User{}, &categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}, &resellerdomain.Profile{}, &resellerdomain.ProductSetting{}, &resellerdomain.ProductImages{}); err != nil {
		t.Fatal(err)
	}
	user, profile, product, _ := seedResellerProductSettingServiceData(t, db)
	product.Images = jsonslice.Strings{"/uploads/platform.png", "/uploads/platform-2.png"}
	if err := db.Model(&product).Update("images", product.Images).Error; err != nil {
		t.Fatal(err)
	}
	other := userdomain.User{Email: uuid.NewString() + "@example.test", PasswordHash: "hash", Status: "active"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&resellerdomain.Profile{UserID: other.ID, Status: resellerdomain.ProfileStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	uploader := uploadapp.NewService(uploadapp.Policy{MaxSize: resellerapp.ProductImageMaxSize, AllowedTypes: []string{"image/png", "image/jpeg", "image/gif", "image/webp"}, AllowedExtensions: []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}}, uploadlocal.New(t.TempDir()))
	store := resellerstore.New(db)
	return imageFixture{db, store, resellerapp.NewProductImageService(store, uploader), uploader, user, profile, other, product}
}

func imageFile(t *testing.T, name string, size int) *multipart.FileHeader {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if size > data.Len() {
		data.Write(make([]byte, size-data.Len()))
	}
	return imageFileBytes(t, name, data.Bytes())
}

func imageFileBytes(t *testing.T, name string, data []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&body, w.Boundary()).ReadForm(1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form.File["file"][0]
}

func TestProductImagesSupportedFormatsAndInvalidContent(t *testing.T) {
	f := newImageFixture(t)
	var jpg, animated bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&animated, img, nil); err != nil {
		t.Fatal(err)
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	for revision, tc := range []struct {
		name string
		data []byte
	}{{"a.JPG", jpg.Bytes()}, {"a.gif", animated.Bytes()}, {"a.webp", webp}} {
		if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFileBytes(t, tc.name, tc.data), uint64(revision), uuid.NewString()); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	badWebP := append([]byte("RIFF\x0c\x00\x00\x00WEBPVP8X"), 0xff, 0xff, 0xff, 0xff)
	for _, tc := range []struct {
		name string
		data []byte
	}{{"fake.png", []byte("not an image")}, {"huge-chunk.webp", badWebP}} {
		if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFileBytes(t, tc.name, tc.data), 3, uuid.NewString()); err == nil {
			t.Fatalf("accepted %s", tc.name)
		}
	}
}

func TestProductImagesLifecycleIsolationAndPricing(t *testing.T) {
	f := newImageFixture(t)
	state, err := f.service.GetUserProductImages(f.user.ID, f.product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Config.Revision != 0 || len(state.Config.Images) != 0 {
		t.Fatal(state)
	}
	resp := presenter.NewResellerProductImagesResp(state.Product, state.Config)
	if resp.ImageSource != "platform" || !reflect.DeepEqual(resp.EffectiveImages, []string(f.product.Images)) {
		t.Fatal(resp)
	}
	var own []string
	for revision := uint64(0); revision < 3; revision++ {
		state, err = f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "custom.png", 0), revision, uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		own = append(own, state.Config.Images[len(state.Config.Images)-1])
	}
	if !reflect.DeepEqual([]string(state.Config.Images), own) {
		t.Fatal(state.Config)
	}
	other, err := f.service.GetUserProductImages(f.other.ID, f.product.ID)
	if err != nil || len(other.Config.Images) != 0 {
		t.Fatalf("other shop: %+v %v", other, err)
	}
	other, err = f.service.UploadUserProductImage(f.other.ID, f.product.ID, imageFile(t, "other.png", 0), 0, uuid.NewString())
	if err != nil || other.Config.Images[0] == own[0] {
		t.Fatalf("other upload: %+v %v", other, err)
	}
	state, err = f.service.UpdateUserProductImages(f.user.ID, f.product.ID, []string{own[2], own[0], own[1]}, 3, uuid.NewString())
	if err != nil || state.Config.Images[0] != own[2] {
		t.Fatalf("cover/reorder: %+v %v", state, err)
	}
	state, err = f.service.UpdateUserProductImages(f.user.ID, f.product.ID, []string{own[2]}, 4, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	// 定价保存和恢复均不能影响图片；图片恢复也不能删除定价配置。
	pricing := resellerapp.NewProductSettingService(f.store, productgormstore.NewProductStore(f.db))
	_, err = pricing.SaveUserProductSettings(f.user.ID, f.product.ID, resellerapp.ProductSettingSaveInput{Settings: []resellerapp.ProductSettingInput{{IsListed: true, PricingMode: resellerdomain.PricingModeInherit}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = pricing.ResetUserProductSetting(f.user.ID, f.product.ID, 0); err != nil {
		t.Fatal(err)
	}
	state, err = f.service.GetUserProductImages(f.user.ID, f.product.ID)
	if err != nil || state.Config.Revision != 5 || len(state.Config.Images) != 1 {
		t.Fatalf("pricing reset: %+v %v", state, err)
	}
	if _, err = f.store.UpsertSetting(resellerdomain.ProductSetting{ResellerID: f.profile.ID, ProductID: f.product.ID, IsListed: true, PricingMode: resellerdomain.PricingModeInherit}); err != nil {
		t.Fatal(err)
	}
	state, err = f.service.UpdateUserProductImages(f.user.ID, f.product.ID, []string{}, 5, uuid.NewString())
	if err != nil || state.Config.Revision != 6 || len(state.Config.Images) != 0 {
		t.Fatalf("reset: %+v %v", state, err)
	}
	var priceCount int64
	f.db.Model(&resellerdomain.ProductSetting{}).Where("reseller_id = ? AND deleted_at IS NULL", f.profile.ID).Count(&priceCount)
	if priceCount != 1 {
		t.Fatal("image reset changed pricing")
	}
	if err := f.db.Model(&f.product).Update("images", jsonslice.Strings{"/uploads/new-platform.png"}).Error; err != nil {
		t.Fatal(err)
	}
	state, err = f.service.GetUserProductImages(f.user.ID, f.product.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp = presenter.NewResellerProductImagesResp(state.Product, state.Config)
	if resp.ImageSource != "platform" || resp.EffectiveImages[0] != "/uploads/new-platform.png" || resp.Revision != 6 {
		t.Fatal(resp)
	}
	if _, err := f.service.UpdateUserProductImages(f.user.ID, f.product.ID, []string{}, 0, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesChanged) {
		t.Fatalf("reset lost version: %v", err)
	}
	var main productdomain.Product
	if err := f.db.First(&main, f.product.ID).Error; err != nil {
		t.Fatal(err)
	}
	if main.Images[0] != "/uploads/new-platform.png" {
		t.Fatal("platform overwritten")
	}
	rows, total, err := f.service.ListUserProductImages(f.user.ID, resellerapp.ProductSettingUserListInput{Page: 1, PageSize: 1, Keyword: "服务"})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("search: %v %d %v", rows, total, err)
	}
}

func TestProductImagesValidationAndScope(t *testing.T) {
	f := newImageFixture(t)
	for _, tc := range []struct {
		name string
		size int
	}{{"too-large.png", 512001}, {"invalid.svg", 0}, {"invalid.txt", 0}} {
		if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, tc.name, tc.size), 0, uuid.NewString()); err == nil {
			t.Fatalf("accepted %s", tc.name)
		}
	}
	state, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "boundary.png", 512000), 0, uuid.NewString())
	if err != nil {
		t.Fatalf("500KB boundary: %v", err)
	}
	for _, images := range [][]string{{"https://outside.example/a.png"}, {"/uploads/platform.png"}, {state.Config.Images[0], state.Config.Images[0]}} {
		if _, err := f.service.UpdateUserProductImages(f.user.ID, f.product.ID, images, 1, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesInvalid) {
			t.Fatalf("accepted %v: %v", images, err)
		}
	}
	if _, err := f.service.UpdateUserProductImages(f.other.ID, f.product.ID, []string{state.Config.Images[0]}, 0, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesInvalid) {
		t.Fatalf("cross-shop image: %v", err)
	}
	for revision := uint64(1); revision < 5; revision++ {
		state, err = f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "a.png", 0), revision, uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "six.png", 0), 5, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesLimit) {
		t.Fatalf("sixth accepted: %v", err)
	}
	if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "stale.png", 0), 4, uuid.NewString()); !errors.Is(err, resellercontract.ErrProductImagesChanged) {
		t.Fatalf("stale accepted: %v", err)
	}
	if _, err := f.service.GetUserProductImages(99999, f.product.ID); !errors.Is(err, resellercontract.ErrNotOpened) {
		t.Fatalf("unopened: %v", err)
	}
	if err := f.db.Model(&f.profile).Update("status", "disabled").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.UpdateUserProductImages(f.user.ID, f.product.ID, []string{}, 5, uuid.NewString()); !errors.Is(err, resellercontract.ErrProfileInactive) {
		t.Fatalf("inactive: %v", err)
	}
	if err := f.db.Model(&f.profile).Update("status", resellerdomain.ProfileStatusActive).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&categorydomain.Category{}).Where("id = ?", f.product.CategoryID).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "inactive.png", 0), 5, uuid.NewString()); err == nil {
		t.Fatal("inactive category accepted")
	}
}

type synchronizedImageUploader struct {
	uploader *uploadapp.Service
	arrived  chan struct{}
	release  chan struct{}
}

func (u synchronizedImageUploader) SaveFileWithMeta(file *multipart.FileHeader, scene string) (*uploadcontract.Result, error) {
	u.arrived <- struct{}{}
	<-u.release
	return u.uploader.SaveFileWithMeta(file, scene)
}

func TestProductImagesConcurrentUploadsCannotOverwriteOrExceedLimit(t *testing.T) {
	for _, initialCount := range []int{0, 4} {
		t.Run(string(rune('0'+initialCount)), func(t *testing.T) {
			f := newImageFixture(t)
			for revision := uint64(0); revision < uint64(initialCount); revision++ {
				if _, err := f.service.UploadUserProductImage(f.user.ID, f.product.ID, imageFile(t, "a.png", 0), revision, uuid.NewString()); err != nil {
					t.Fatal(err)
				}
			}
			barrier := synchronizedImageUploader{f.uploader, make(chan struct{}, 2), make(chan struct{})}
			svc := resellerapp.NewProductImageService(f.store, barrier)
			files := []*multipart.FileHeader{imageFile(t, "one.png", 0), imageFile(t, "two.png", 0)}
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, file := range files {
				wg.Add(1)
				go func(file *multipart.FileHeader) {
					defer wg.Done()
					_, err := svc.UploadUserProductImage(f.user.ID, f.product.ID, file, uint64(initialCount), uuid.NewString())
					results <- err
				}(file)
			}
			<-barrier.arrived
			<-barrier.arrived
			close(barrier.release)
			wg.Wait()
			close(results)
			success, conflict := 0, 0
			for err := range results {
				if err == nil {
					success++
				} else if errors.Is(err, resellercontract.ErrProductImagesChanged) {
					conflict++
				} else {
					t.Fatal(err)
				}
			}
			state, err := f.service.GetUserProductImages(f.user.ID, f.product.ID)
			if err != nil || success != 1 || conflict != 1 || len(state.Config.Images) != initialCount+1 {
				t.Fatalf("success=%d conflict=%d state=%+v err=%v", success, conflict, state, err)
			}
		})
	}
}
