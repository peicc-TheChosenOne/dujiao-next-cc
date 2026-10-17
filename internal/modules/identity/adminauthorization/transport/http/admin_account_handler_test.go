package adminauthzhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	admindomain "github.com/dujiao-next/internal/modules/identity/admin/domain"

	"github.com/gin-gonic/gin"
)

type fakeAdminDirectory struct {
	admins  map[uint]*admindomain.Admin
	nextID  uint
	created []*admindomain.Admin
	updated []*admindomain.Admin
	deleted []uint
}

func newFakeAdminDirectory(admins ...admindomain.Admin) *fakeAdminDirectory {
	directory := &fakeAdminDirectory{admins: map[uint]*admindomain.Admin{}, nextID: 100}
	for i := range admins {
		admin := admins[i]
		directory.admins[admin.ID] = &admin
	}
	return directory
}

func (d *fakeAdminDirectory) List() ([]admindomain.Admin, error) {
	list := make([]admindomain.Admin, 0, len(d.admins))
	for _, admin := range d.admins {
		list = append(list, *admin)
	}
	return list, nil
}

func (d *fakeAdminDirectory) GetByID(id uint) (*admindomain.Admin, error) {
	admin, ok := d.admins[id]
	if !ok {
		return nil, nil
	}
	clone := *admin
	return &clone, nil
}

func (d *fakeAdminDirectory) GetByUsername(username string) (*admindomain.Admin, error) {
	for _, admin := range d.admins {
		if strings.EqualFold(admin.Username, username) {
			clone := *admin
			return &clone, nil
		}
	}
	return nil, nil
}

func (d *fakeAdminDirectory) Create(admin *admindomain.Admin) error {
	d.nextID++
	admin.ID = d.nextID
	d.created = append(d.created, admin)
	return nil
}

func (d *fakeAdminDirectory) Update(admin *admindomain.Admin) error {
	d.updated = append(d.updated, admin)
	return nil
}

func (d *fakeAdminDirectory) Delete(id uint) error {
	d.deleted = append(d.deleted, id)
	return nil
}

func (d *fakeAdminDirectory) Count() (int64, error) {
	return int64(len(d.admins)), nil
}

type fakeRolePolicyService struct{ RolePolicyService }

func (fakeRolePolicyService) SetAdminRoles(uint, []string) error { return nil }

type fakePasswordService struct{}

func (fakePasswordService) ValidatePassword(string) error { return nil }

func (fakePasswordService) HashPassword(password string) (string, error) {
	return "hash:" + password, nil
}

type fakeAuthStateCache struct{}

func (fakeAuthStateCache) SetAdminAuthState(context.Context, *admindomain.Admin) error { return nil }

func (fakeAuthStateCache) DelAdminAuthState(context.Context, uint) error { return nil }

var (
	superAccount  = admindomain.Admin{ID: 1, Username: "root", IsSuper: true}
	systemAccount = admindomain.Admin{ID: 2, Username: "sysadmin"}
	staffAccount  = admindomain.Admin{ID: 3, Username: "staff"}
)

// callAuthzAdmin 以指定操作者身份调用 handler，返回业务状态码。
func callAuthzAdmin(t *testing.T, handler gin.HandlerFunc, operatorIsSuper bool, targetID, body string) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if targetID != "" {
		c.Params = gin.Params{{Key: "id", Value: targetID}}
	}
	c.Set("admin_id", systemAccount.ID)
	c.Set("username", systemAccount.Username)
	c.Set("admin_is_super", operatorIsSuper)
	handler(c)

	var payload struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return payload.StatusCode
}

func newAccountHandler(directory *fakeAdminDirectory) *AdminHandler {
	return NewAdminHandler(fakeRolePolicyService{}, directory, fakePasswordService{}, fakeAuthStateCache{}, nil)
}

func TestCreateAuthzAdminRequiresSuperOperatorForSuperAccount(t *testing.T) {
	for _, body := range []string{
		`{"username":"newsuper","password":"Passw0rd!","is_super":true}`,
		`{"username":"admin","password":"Passw0rd!"}`,
	} {
		directory := newFakeAdminDirectory(systemAccount)
		if code := callAuthzAdmin(t, newAccountHandler(directory).CreateAuthzAdmin, false, "", body); code != 403 {
			t.Errorf("create %s by non-super = %d, want 403", body, code)
		}
		if len(directory.created) != 0 {
			t.Errorf("create %s by non-super persisted %d admins", body, len(directory.created))
		}
	}

	directory := newFakeAdminDirectory(superAccount)
	body := `{"username":"newsuper","password":"Passw0rd!","is_super":true}`
	if code := callAuthzAdmin(t, newAccountHandler(directory).CreateAuthzAdmin, true, "", body); code != 0 {
		t.Fatalf("create super by super = %d, want 0", code)
	}
	if len(directory.created) != 1 || !directory.created[0].IsSuper {
		t.Fatalf("super operator should create a super admin, got %+v", directory.created)
	}
}

func TestUpdateAuthzAdminBlocksNonSuperEscalation(t *testing.T) {
	cases := []struct {
		name   string
		target string
		body   string
	}{
		{name: "grant self super", target: "2", body: `{"is_super":true}`},
		{name: "change super password", target: "1", body: `{"password":"Passw0rd!"}`},
		{name: "revoke super", target: "1", body: `{"is_super":false}`},
		{name: "rename to protected username", target: "3", body: `{"username":"admin"}`},
	}
	for _, tc := range cases {
		directory := newFakeAdminDirectory(superAccount, systemAccount, staffAccount)
		if code := callAuthzAdmin(t, newAccountHandler(directory).UpdateAuthzAdmin, false, tc.target, tc.body); code != 403 {
			t.Errorf("%s by non-super = %d, want 403", tc.name, code)
		}
		if len(directory.updated) != 0 {
			t.Errorf("%s by non-super persisted %d updates", tc.name, len(directory.updated))
		}
	}
}

func TestUpdateAuthzAdminAllowsLegitimateChanges(t *testing.T) {
	directory := newFakeAdminDirectory(superAccount, systemAccount, staffAccount)
	if code := callAuthzAdmin(t, newAccountHandler(directory).UpdateAuthzAdmin, false, "3", `{"password":"Passw0rd!"}`); code != 0 {
		t.Fatalf("non-super updating a normal admin = %d, want 0", code)
	}

	directory = newFakeAdminDirectory(superAccount, systemAccount, staffAccount)
	if code := callAuthzAdmin(t, newAccountHandler(directory).UpdateAuthzAdmin, true, "2", `{"is_super":true}`); code != 0 {
		t.Fatalf("super granting super = %d, want 0", code)
	}
	if len(directory.updated) != 1 || !directory.updated[0].IsSuper {
		t.Fatalf("super operator should grant super, got %+v", directory.updated)
	}
}

func TestDeleteAuthzAdminRequiresSuperOperatorForSuperAccount(t *testing.T) {
	directory := newFakeAdminDirectory(superAccount, systemAccount, staffAccount)
	if code := callAuthzAdmin(t, newAccountHandler(directory).DeleteAuthzAdmin, false, "1", ""); code != 403 {
		t.Fatalf("non-super deleting super = %d, want 403", code)
	}
	if len(directory.deleted) != 0 {
		t.Fatalf("non-super deleted %v", directory.deleted)
	}

	if code := callAuthzAdmin(t, newAccountHandler(directory).DeleteAuthzAdmin, false, "3", ""); code != 0 {
		t.Fatalf("non-super deleting a normal admin = %d, want 0", code)
	}
}
