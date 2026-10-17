package domain

import (
	"time"

	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	"github.com/dujiao-next/internal/shared/money"
)

// Profile 分销商资料。
type Profile struct {
	LoginSecurity        *LoginSecurityInfo `gorm:"-" json:"login_security,omitempty"`
	ID                   uint               `gorm:"primarykey" json:"id"`
	UserID               uint               `gorm:"not null;uniqueIndex" json:"user_id"`
	Status               string             `gorm:"type:varchar(32);index;not null;default:'pending_review'" json:"status"`
	ApplyReason          string             `gorm:"type:text" json:"apply_reason,omitempty"`
	RejectReason         string             `gorm:"type:text" json:"reject_reason,omitempty"`
	DefaultMarkupPercent money.Amount       `gorm:"type:decimal(10,2);not null;default:0" json:"default_markup_percent"`
	MaxMarkupPercent     money.Amount       `gorm:"type:decimal(10,2);not null;default:0" json:"max_markup_percent"`
	SettlementStatus     string             `gorm:"type:varchar(32);index;not null;default:'normal'" json:"settlement_status"`
	ReviewedBy           *uint              `gorm:"index" json:"reviewed_by,omitempty"`
	ReviewedAt           *time.Time         `gorm:"index" json:"reviewed_at,omitempty"`
	CreatedAt            time.Time          `gorm:"index" json:"created_at"`
	UpdatedAt            time.Time          `gorm:"index" json:"updated_at"`
	DeletedAt            *time.Time         `gorm:"index" json:"-"`

	User *userdomain.User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (Profile) TableName() string { return "reseller_profiles" }

type LoginSecurityInfo struct {
	Required     bool       `json:"required"`
	Bound        bool       `json:"bound"`
	BindingState string     `json:"binding_state"`
	BoundAt      *time.Time `json:"bound_at,omitempty"`
	Action       string     `json:"action"`
	Reason       string     `json:"reason"`
	AdminID      uint       `json:"admin_id"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

func (p *Profile) PopulateLoginSecurity() {
	if p.User == nil {
		return
	}
	state := "unbound"
	if p.User.ResellerTOTPResetAt != nil {
		state = "reset_pending"
	}
	if p.User.TOTPEnabledAt != nil {
		state = "bound"
	}
	p.LoginSecurity = &LoginSecurityInfo{Required: !p.User.ResellerTOTPExempt, Bound: p.User.TOTPEnabledAt != nil, BindingState: state, BoundAt: p.User.TOTPEnabledAt, Action: p.User.ResellerSecurityAction, Reason: p.User.ResellerSecurityReason, AdminID: p.User.ResellerSecurityAdminID, UpdatedAt: p.User.ResellerSecurityUpdatedAt}
}
