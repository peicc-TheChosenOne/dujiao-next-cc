package contract

import (
	"context"
	"net"
	"strings"
	"time"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
)

type HostnameState struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	SSL      struct {
		Status string `json:"status"`
	} `json:"ssl"`
}

type HostnameProvider interface {
	Create(context.Context, string) (HostnameState, error)
	Get(context.Context, string) (HostnameState, error)
	Delete(context.Context, string) error
}

type DomainProvisioningStore interface {
	ManagementStore
	ListDomainsForProvisioning(time.Time, time.Time, int) ([]resellerdomain.Domain, error)
	ClaimDomainProvisioning(uint, string, time.Time, time.Time) (bool, error)
}

type DomainDNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func ValidProvisioningHost(host string) bool {
	if len(host) > 253 || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}
