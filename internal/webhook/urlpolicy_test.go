package webhook

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type staticResolver struct {
	addresses map[string][]netip.Addr
	err       error
}

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.addresses[host], nil
}

func TestURLPolicyResolveAllowsOnlyPublicHTTPS(t *testing.T) {
	resolver := staticResolver{addresses: map[string][]netip.Addr{
		"hooks.example.com":   {netip.MustParseAddr("93.184.216.34")},
		"mixed.example.com":   {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")},
		"private.example.com": {netip.MustParseAddr("10.0.0.8")},
	}}
	policy, err := NewURLPolicy(resolver, false, []int{443, 8443})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "public HTTPS", url: "https://hooks.example.com/events"},
		{name: "allow-listed TLS port", url: "https://hooks.example.com:8443/events"},
		{name: "plain HTTP", url: "http://hooks.example.com/events", wantErr: true},
		{name: "private DNS", url: "https://private.example.com/events", wantErr: true},
		{name: "mixed DNS answer", url: "https://mixed.example.com/events", wantErr: true},
		{name: "metadata IP", url: "https://169.254.169.254/latest/meta-data", wantErr: true},
		{name: "loopback IPv6", url: "https://[::1]/events", wantErr: true},
		{name: "credentials", url: "https://user:pass@hooks.example.com/events", wantErr: true},
		{name: "blocked port", url: "https://hooks.example.com:9443/events", wantErr: true},
		{name: "relative URL", url: "/events", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := policy.Resolve(t.Context(), test.url)
			if (err != nil) != test.wantErr {
				t.Fatalf("Resolve() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestURLPolicyResolveReportsDNSFailure(t *testing.T) {
	policy, err := NewURLPolicy(staticResolver{err: errors.New("DNS unavailable")}, false, []int{443})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := policy.Resolve(t.Context(), "https://hooks.example.com/events"); err == nil {
		t.Fatal("Resolve() ignored DNS failure")
	}
}

func TestNewURLPolicyValidatesPorts(t *testing.T) {
	for _, ports := range [][]int{nil, {0}, {65536}} {
		if _, err := NewURLPolicy(staticResolver{}, false, ports); err == nil {
			t.Fatalf("NewURLPolicy(%v) accepted invalid ports", ports)
		}
	}
}
