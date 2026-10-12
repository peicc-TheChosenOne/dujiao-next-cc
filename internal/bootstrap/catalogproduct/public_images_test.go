package catalogproductbootstrap

import (
	"errors"
	"reflect"
	"testing"

	productapp "github.com/dujiao-next/internal/modules/catalog/product/application"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/jsonslice"
	"strings"
)

type imageProductRepo struct{ products []productdomain.Product }

func (r *imageProductRepo) List(productcontract.ListFilter) ([]productdomain.Product, int64, error) {
	return append([]productdomain.Product{}, r.products...), int64(len(r.products)), nil
}

func TestPublicProductDetailImageReplacesBodyOnlyForItsStorefront(t *testing.T) {
	content := jsonmap.JSON{"zh-CN": "总店正文", "zh-TW": "總店正文", "en-US": "Platform body"}
	product := productdomain.Product{ID: 1, Slug: "one", TitleJSON: jsonmap.JSON{"zh-CN": "商品名称"}, DescriptionJSON: jsonmap.JSON{"zh-CN": "商品简介"}, ContentJSON: content, Images: jsonslice.Strings{"platform-cover"}, SKUs: []productdomain.ProductSKU{{ID: 5, SKUCode: "original"}}}
	repo := &imageProductRepo{[]productdomain.Product{product}}
	reader := &imageScopeReader{configs: map[uint][]resellerdomain.ProductImages{10: {{ProductID: 1, DetailImage: "/uploads/detail.png"}}}}
	adapter := publicProductAdapter{products: productapp.NewService(productapp.Options{Products: repo}), hidden: reader, images: reader}
	shop := reseller.ResellerTenantContext("shop.example.test", 10, 101, "")
	detail, err := adapter.GetPublicBySlugForTenant(shop, "one")
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"zh-CN", "zh-TW", "en-US"} {
		body, _ := detail.ContentJSON[lang].(string)
		if strings.Count(body, "<img ") != 1 || !strings.Contains(body, `src="/uploads/detail.png"`) || strings.Contains(body, "总店正文") {
			t.Fatal(body)
		}
	}
	if !reflect.DeepEqual(detail.TitleJSON, product.TitleJSON) || !reflect.DeepEqual(detail.DescriptionJSON, product.DescriptionJSON) || detail.SKUs[0].SKUCode != "original" || detail.Images[0] != "platform-cover" {
		t.Fatal("metadata/gallery changed")
	}
	for _, tenant := range []reseller.TenantContext{reseller.MainTenantContext("main.example.test"), reseller.ResellerTenantContext("other.example.test", 20, 202, "")} {
		detail, err = adapter.GetPublicBySlugForTenant(tenant, "one")
		if err != nil || !reflect.DeepEqual(detail.ContentJSON, content) {
			t.Fatalf("other tenant: %+v %v", detail, err)
		}
	}
	rows, _, err := adapter.ListPublicForTenant(shop, "", "", 1, 20)
	if err != nil || !reflect.DeepEqual(rows[0].ContentJSON, content) {
		t.Fatal("detail override affected list body")
	}
	reader.configs[10][0].DetailImage = ""
	repo.products[0].ContentJSON = jsonmap.JSON{"zh-CN": "总店新正文"}
	detail, err = adapter.GetPublicBySlugForTenant(shop, "one")
	if err != nil || detail.ContentJSON["zh-CN"] != "总店新正文" {
		t.Fatalf("restore did not follow platform: %+v %v", detail, err)
	}
	reader.configs[10][0].DetailImage = `/uploads/a" onerror="alert(1).png`
	detail, err = adapter.GetPublicBySlugForTenant(shop, "one")
	if err != nil || strings.Contains(detail.ContentJSON["zh-CN"].(string), `" onerror="`) {
		t.Fatal("unsafe HTML attribute")
	}
}
func (r *imageProductRepo) GetBySlug(slug string, _ bool) (*productdomain.Product, error) {
	for i := range r.products {
		if r.products[i].Slug == slug {
			return &r.products[i], nil
		}
	}
	return nil, productcontract.ErrNotFound
}
func (r *imageProductRepo) GetAdminByID(string) (*productdomain.Product, error) { return nil, nil }

type imageScopeReader struct {
	calls   int
	ids     []uint
	configs map[uint][]resellerdomain.ProductImages
	err     error
}

func (r *imageScopeReader) ListProductImages(id uint, ids []uint) ([]resellerdomain.ProductImages, error) {
	r.calls++
	r.ids = append([]uint{}, ids...)
	return r.configs[id], r.err
}
func (*imageScopeReader) ListHiddenProductIDs(uint) ([]uint, error) { return nil, nil }

func TestPublicProductImagesListDetailAndTenantIsolation(t *testing.T) {
	repo := &imageProductRepo{[]productdomain.Product{{ID: 1, Slug: "one", Images: jsonslice.Strings{"platform-1"}}, {ID: 2, Slug: "two", Images: jsonslice.Strings{"platform-2"}}}}
	reader := &imageScopeReader{configs: map[uint][]resellerdomain.ProductImages{
		10: {{ProductID: 1, Images: jsonslice.Strings{"shop-10", "cover-2"}}, {ProductID: 2, Images: jsonslice.Strings{}}},
		20: {{ProductID: 1, Images: jsonslice.Strings{"shop-20"}}},
	}}
	adapter := publicProductAdapter{products: productapp.NewService(productapp.Options{Products: repo}), hidden: reader, images: reader}
	main := reseller.MainTenantContext("main.example.test")
	shop10 := reseller.ResellerTenantContext("ten.example.test", 10, 101, "")
	shop20 := reseller.ResellerTenantContext("twenty.example.test", 20, 202, "")
	for _, tc := range []struct {
		tenant reseller.TenantContext
		first  string
	}{{main, "platform-1"}, {shop10, "shop-10"}, {shop20, "shop-20"}} {
		before := reader.calls
		rows, total, err := adapter.ListPublicForTenant(tc.tenant, "", "", 1, 20)
		if err != nil || total != 2 || rows[0].Images[0] != tc.first || rows[1].Images[0] != "platform-2" {
			t.Fatalf("list: %+v %v", rows, err)
		}
		if tc.tenant.IsReseller() {
			if reader.calls-before != 1 || !reflect.DeepEqual(reader.ids, []uint{1, 2}) {
				t.Fatal("list did not use one scoped batch read")
			}
		} else if reader.calls != before {
			t.Fatal("main queried custom images")
		}
		product, err := adapter.GetPublicBySlugForTenant(tc.tenant, "one")
		if err != nil || product.Images[0] != tc.first {
			t.Fatalf("detail: %+v %v", product, err)
		}
	}
	if repo.products[0].Images[0] != "platform-1" {
		t.Fatal("query mutated platform product")
	}
	repo.products[1].Images = jsonslice.Strings{"updated-platform"}
	product, err := adapter.GetPublicBySlugForTenant(shop10, "two")
	if err != nil || product.Images[0] != "updated-platform" {
		t.Fatalf("empty custom config did not follow platform: %+v %v", product, err)
	}
	reader.err = errors.New("image store unavailable")
	if _, _, err := adapter.ListPublicForTenant(shop10, "", "", 1, 20); err == nil {
		t.Fatal("image lookup failure ignored")
	}
}
