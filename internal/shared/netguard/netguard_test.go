package netguard

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestIsPublicIP(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false}, {"10.1.2.3", false}, {"192.168.1.1", false}, {"172.16.0.9", false},
		{"169.254.169.254", false}, {"100.64.0.1", false}, {"100.100.100.200", false}, {"0.0.0.0", false},
		{"::1", false}, {"fd00::1", false}, {"::ffff:127.0.0.1", false},
		{"8.8.8.8", true}, {"1.1.1.1", true}, {"2606:4700::1111", true},
	} {
		if got := IsPublicIP(net.ParseIP(tc.ip)); got != tc.want {
			t.Errorf("IsPublicIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestPublicDialContextRefusesNonPublicTargets(t *testing.T) {
	dial := PublicDialContext(&net.Dialer{Timeout: time.Second})
	for _, addr := range []string{"127.0.0.1:80", "[::1]:80", "10.0.0.1:6379", "100.100.100.200:80"} {
		conn, err := dial(context.Background(), "tcp", addr)
		if conn != nil {
			_ = conn.Close()
		}
		if !errors.Is(err, ErrForbiddenAddress) {
			t.Errorf("dial %s error = %v, want ErrForbiddenAddress", addr, err)
		}
	}
}
