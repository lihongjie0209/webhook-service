package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type SendRequest struct {
	EndpointURL string
	DeliveryID  string
	EventType   string
	Body        []byte
	Secret      []byte
	Timestamp   time.Time
}

type SendResult struct {
	StatusCode    int
	Body          string
	BodyTruncated bool
}

func (r SendResult) Successful() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

type Sender struct {
	policy           *URLPolicy
	maxResponseBytes int64
	transport        func(*url.URL, []netip.Addr) http.RoundTripper
}

func NewSender(policy *URLPolicy, maxResponseBytes int64) (*Sender, error) {
	if policy == nil || maxResponseBytes <= 0 {
		return nil, errors.New("webhook URL policy and positive response limit are required")
	}
	sender := &Sender{policy: policy, maxResponseBytes: maxResponseBytes}
	sender.transport = sender.pinnedTransport
	return sender, nil
}

func (s *Sender) Send(ctx context.Context, request SendRequest) (SendResult, error) {
	if request.DeliveryID == "" || request.EventType == "" || len(request.Secret) == 0 || request.Timestamp.IsZero() {
		return SendResult{}, errors.New("webhook delivery ID, event type, secret, and timestamp are required")
	}
	endpoint, addresses, err := s.policy.Resolve(ctx, request.EndpointURL)
	if err != nil {
		return SendResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(request.Body))
	if err != nil {
		return SendResult{}, fmt.Errorf("create webhook request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("User-Agent", "microservice-platform-webhook/1.0")
	httpRequest.Header.Set(HeaderDeliveryID, request.DeliveryID)
	httpRequest.Header.Set(HeaderEventType, request.EventType)
	httpRequest.Header.Set(HeaderTimestamp, strconv.FormatInt(request.Timestamp.Unix(), 10))
	httpRequest.Header.Set(HeaderSignature, Sign(request.Secret, request.Timestamp, request.Body))

	client := &http.Client{
		Transport:     s.transport(endpoint, addresses),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return SendResult{}, fmt.Errorf("send webhook request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	limited := io.LimitReader(response.Body, s.maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return SendResult{}, fmt.Errorf("read webhook response: %w", err)
	}
	result := SendResult{StatusCode: response.StatusCode}
	if int64(len(body)) > s.maxResponseBytes {
		body = body[:s.maxResponseBytes]
		result.BodyTruncated = true
	}
	result.Body = string(body)
	return result, nil
}

func (s *Sender) pinnedTransport(endpoint *url.URL, addresses []netip.Addr) http.RoundTripper {
	port, _ := endpointPort(endpoint)
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Hostname()}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var failures []error
		for _, address := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), strconv.Itoa(int(port))))
			if err == nil {
				return connection, nil
			}
			failures = append(failures, err)
		}
		return nil, errors.Join(failures...)
	}
	return otelhttp.NewTransport(transport)
}
