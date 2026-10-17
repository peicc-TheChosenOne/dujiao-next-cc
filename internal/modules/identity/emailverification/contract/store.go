package emailverificationcontract

import (
	"time"

	emailverificationdomain "github.com/dujiao-next/internal/modules/identity/emailverification/domain"
)

type Store interface {
	Create(*emailverificationdomain.Code) error
	GetLatest(email, purpose string) (*emailverificationdomain.Code, error)
	// MarkVerified 把未使用的验证码标记为已使用；返回 false 表示已被其他请求抢先使用。
	MarkVerified(id uint, verifiedAt time.Time) (bool, error)
	// ReserveAttempt 原子地占用一次校验机会；maxAttempts <= 0 表示不限次数。
	// 返回 false 表示次数已用尽或验证码已被使用。
	ReserveAttempt(id uint, maxAttempts int) (bool, error)
}
