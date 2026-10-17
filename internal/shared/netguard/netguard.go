// Package netguard 为指向外部可控地址的出站请求提供拨号期地址校验，防止 SSRF 打到内网。
package netguard

import (
	"context"
	"errors"
	"net"
)

// ErrForbiddenAddress 目标域名解析到的地址全部不是公网地址。
var ErrForbiddenAddress = errors.New("target resolves to a forbidden address")

// PublicDialContext 返回只连接公网地址的拨号函数：拨号时解析域名并逐个校验，
// 实际连接的就是校验过的 IP，因此不受 DNS 重绑定影响。
func PublicDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error = ErrForbiddenAddress
		for _, ip := range ips {
			if !IsPublicIP(ip.IP) {
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
}

// IsPublicIP 判断地址是否可作为出站目标：排除回环、私网、链路本地、组播与运营商级 NAT 地址。
func IsPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	// 100.64.0.0/10（运营商级 NAT，阿里云元数据 100.100.100.200 也在此段）
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 {
		return false
	}
	return true
}
