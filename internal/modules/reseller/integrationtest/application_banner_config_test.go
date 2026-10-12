package integrationtest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	resellerpresenter "github.com/dujiao-next/internal/modules/reseller/transport/http/presenter"
)

func bannerConfigTestService(t *testing.T) (*resellerapp.SiteConfigService, uint) {
	t.Helper()
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, t.Name()+"@example.test")
	profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	return resellerapp.NewSiteConfigService(resellergormstore.New(db)), user.ID
}

func TestNormalizeResellerBannerModesAndLinks(t *testing.T) {
	svc, userID := bannerConfigTestService(t)
	for _, mode := range []string{"", "inherit", "hidden"} {
		row, err := svc.UpdateUserSiteConfig(context.Background(), userID, resellerapp.ResellerSiteConfigInput{
			Banner: &resellerapp.ResellerBannerInput{Mode: mode, LinkType: "internal"},
		})
		if err != nil || len(row.BannerJSON) != 1 {
			t.Fatalf("inactive banner must not require an image or destination: %v %+v", err, row)
		}
	}
	for _, link := range []struct{ typ, value string }{
		{"none", "javascript:alert(1)"},
		{"internal", "/products?category_id=1"},
		{"external", "https://example.test/help"},
	} {
		row, err := svc.UpdateUserSiteConfig(context.Background(), userID, resellerapp.ResellerSiteConfigInput{
			Banner: &resellerapp.ResellerBannerInput{
				Mode: "custom", Image: " /uploads/reseller/hero.png ", LinkType: link.typ, LinkValue: link.value,
				Title: resellerapp.LocalizedTextInput{"zh-CN": " 我的店铺 ", "fr-FR": "discard"}, OpenInNewTab: true,
			},
		})
		if err != nil {
			t.Fatalf("valid custom banner rejected: %v", err)
		}
		out := row.BannerJSON
		if out["image"] != "/uploads/reseller/hero.png" || !reflect.DeepEqual(resellerSiteConfigTestMap(out["title"]), map[string]interface{}{"zh-CN": "我的店铺", "zh-TW": "", "en-US": ""}) {
			t.Fatalf("banner image and localized text not normalized: %+v", out)
		}
		if link.typ == "none" && (out["link_value"] != "" || out["open_in_new_tab"] != false) {
			t.Fatalf("no-link banner retained a destination: %+v", out)
		}
	}
}

func TestNormalizeResellerBannerRejectsUnsafeInput(t *testing.T) {
	svc, userID := bannerConfigTestService(t)
	for _, input := range []resellerapp.ResellerBannerInput{
		{Mode: "unknown"},
		{Mode: "custom"},
		{Mode: "custom", Image: "javascript:alert(1)"},
		{Mode: "custom", Image: "/uploads/hero.png", MobileImage: "data:image/png;base64,abc"},
		{Mode: "custom", Image: "/uploads/hero.png", LinkType: "external", LinkValue: "javascript:alert(1)"},
		{Mode: "custom", Image: "/uploads/hero.png", LinkType: "internal", LinkValue: "//example.test"},
		{Mode: "custom", Image: "/uploads/hero.png", LinkType: "internal", LinkValue: "/\\example.test"},
		{Mode: "custom", Image: "/uploads/hero.png", LinkType: "internal", LinkValue: "https://example.test"},
		{Mode: "custom", Image: "/uploads/hero.png", LinkType: "external", LinkValue: "/products"},
		{Mode: "custom", Image: "/uploads/hero.png", LinkType: "unknown", LinkValue: "/products"},
	} {
		_, err := svc.UpdateUserSiteConfig(context.Background(), userID, resellerapp.ResellerSiteConfigInput{Banner: &input})
		var fieldErr *resellerapp.ResellerSiteConfigFieldError
		if !errors.As(err, &fieldErr) {
			t.Fatalf("invalid banner accepted: %+v, error=%v", input, err)
		}
	}
}

func TestResellerBannerConfigIsolationAndLegacyUpdates(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	repo := resellergormstore.New(db)
	svc := resellerapp.NewSiteConfigService(repo)
	users := []string{"banner-alice@example.test", "banner-bob@example.test"}
	profiles := make([]resellerdomain.Profile, 0, len(users))
	for _, email := range users {
		user := seedResellerManagementUser(t, db, email)
		profile := resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, SettlementStatus: resellerdomain.SettlementStatusNormal}
		if err := db.Create(&profile).Error; err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, profile)
	}
	ctx := context.Background()
	base := map[string]interface{}{"brand": map[string]interface{}{"site_name": "Main"}}
	for index, profile := range profiles {
		image := "/uploads/reseller/" + users[index] + ".png"
		row, err := svc.UpdateUserSiteConfig(ctx, profile.UserID, resellerapp.ResellerSiteConfigInput{
			Banner: &resellerapp.ResellerBannerInput{Mode: "custom", Image: image, LinkType: "none"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if resellerpresenter.NewResellerSiteConfigResp(row).Banner["image"] != image || resellerpresenter.NewAdminResellerSiteConfigResp(row).Banner["image"] != image {
			t.Fatal("site-config responses must include the saved banner")
		}
	}
	for index, profile := range profiles {
		out, err := svc.ApplyPublicConfigOverlay(ctx, ResellerTenantContext(users[index], profile.ID, profile.UserID, users[index]), base)
		if err != nil {
			t.Fatal(err)
		}
		if resellerSiteConfigTestMap(out["reseller_banner"])["image"] != "/uploads/reseller/"+users[index]+".png" {
			t.Fatalf("wrong reseller banner: %+v", out)
		}
	}
	alice := profiles[0]
	_, before, _, err := svc.GetUserSiteConfig(alice.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{false, true} {
		input := resellerapp.ResellerSiteConfigInput{SiteName: "Legacy client"}
		if admin {
			_, err = svc.UpdateAdminSiteConfig(ctx, alice.ID, input)
		} else {
			_, err = svc.UpdateUserSiteConfig(ctx, alice.UserID, input)
		}
		if err != nil {
			t.Fatal(err)
		}
		_, after, _, err := svc.GetUserSiteConfig(alice.UserID)
		if err != nil || !reflect.DeepEqual(before.BannerJSON, after.BannerJSON) {
			t.Fatalf("omitted banner must survive legacy updates: %v %+v", err, after)
		}
	}
	for _, mode := range []string{"hidden", "inherit"} {
		_, err := svc.UpdateUserSiteConfig(ctx, alice.UserID, resellerapp.ResellerSiteConfigInput{Banner: &resellerapp.ResellerBannerInput{Mode: mode}})
		if err != nil {
			t.Fatal(err)
		}
		out, err := svc.ApplyPublicConfigOverlay(ctx, ResellerTenantContext("alice.example.test", alice.ID, alice.UserID, "alice.example.test"), base)
		if err != nil || resellerSiteConfigTestMap(out["reseller_banner"])["mode"] != mode {
			t.Fatalf("wrong banner mode: %v %+v", err, out)
		}
	}
	main, err := svc.ApplyPublicConfigOverlay(ctx, resellercontract.MainTenantContext("main.example.test"), base)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := main["reseller_banner"]; exists || !reflect.DeepEqual(base["brand"], main["brand"]) {
		t.Fatalf("reseller banner altered the main store: %+v", main)
	}
}
