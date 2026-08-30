package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type URLPolicy struct {
	resolver    Resolver
	allowHTTP   bool
	allowedPort map[uint16]struct{}
}

func NewURLPolicy(resolver Resolver, allowHTTP bool, allowedPorts []int) (*URLPolicy, error) {
	if resolver == nil || len(allowedPorts) == 0 {
		return nil, errors.New("webhook resolver and allowed ports are required")
	}
	ports := make(map[uint16]struct{}, len(allowedPorts))
	for _, port := range allowedPorts {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid webhook port %d", port)
		}
		ports[uint16(port)] = struct{}{}
	}
	return &URLPolicy{resolver: resolver, allowHTTP: allowHTTP, allowedPort: ports}, nil
}

func (p *URLPolicy) Resolve(ctx context.Context, rawURL string) (*url.URL, []netip.Addr, error) {
	endpoint, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, nil, errors.New("webhook endpoint is not a valid absolute URL")
	}
	if endpoint.Scheme != "https" && (!p.allowHTTP || endpoint.Scheme != "http") {
		return nil, nil, errors.New("webhook endpoint must use HTTPS")
	}
	if endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, nil, errors.New("webhook endpoint must not contain credentials or fragments")
	}
	port, err := endpointPort(endpoint)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := p.allowedPort[port]; !ok {
		return nil, nil, fmt.Errorf("webhook endpoint port %d is not allowed", port)
	}

	host := strings.TrimSuffix(endpoint.Hostname(), ".")
	if host == "" {
		return nil, nil, errors.New("webhook endpoint host is required")
	}
	addresses, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve webhook endpoint: %w", err)
	}
	if len(addresses) == 0 {
		return nil, nil, errors.New("webhook endpoint resolved to no addresses")
	}
	for _, address := range addresses {
		if !publicAddress(address) {
			return nil, nil, fmt.Errorf("webhook endpoint resolved to prohibited address %s", address)
		}
	}
	return endpoint, addresses, nil
}

func endpointPort(endpoint *url.URL) (uint16, error) {
	if endpoint.Port() == "" {
		if endpoint.Scheme == "https" {
			return 443, nil
		}
		return 80, nil
	}
	port, err := strconv.ParseUint(endpoint.Port(), 10, 16)
	if err != nil || port == 0 {
		return 0, errors.New("webhook endpoint has an invalid port")
	}
	return uint16(port), nil
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsValid() && address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsUnspecified()
}

var _ Resolver = (*net.Resolver)(nil)
