package webhook

import (
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestSenderSignsAndLimitsResponse(t *testing.T) {
	policy, err := NewURLPolicy(staticResolver{addresses: map[string][]netip.Addr{
		"hooks.example.com": {netip.MustParseAddr("93.184.216.34")},
	}}, false, []int{443})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSender(policy, 5)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("subscriber-secret")
	timestamp := time.Unix(1_700_000_000, 0)
	body := []byte(`{"id":"event-1"}`)
	sender.transport = func(endpoint *url.URL, addresses []netip.Addr) http.RoundTripper {
		if endpoint.Hostname() != "hooks.example.com" || len(addresses) != 1 {
			t.Fatalf("transport endpoint = %v, addresses = %v", endpoint, addresses)
		}
		return roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("request method/Content-Type = %s/%s", request.Method, request.Header.Get("Content-Type"))
			}
			if request.Header.Get(HeaderDeliveryID) != "delivery-1" || request.Header.Get(HeaderEventType) != "platform.identity.user.changed.v1" {
				t.Fatalf("webhook identity headers are missing")
			}
			if !VerifySignature(secret, timestamp, body, request.Header.Get(HeaderSignature)) {
				t.Fatalf("webhook signature is invalid")
			}
			return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader("123456789")), Header: make(http.Header)}, nil
		})
	}

	result, err := sender.Send(t.Context(), SendRequest{
		EndpointURL: "https://hooks.example.com/events", DeliveryID: "delivery-1",
		EventType: "platform.identity.user.changed.v1", Body: body, Secret: secret, Timestamp: timestamp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Successful() || result.Body != "12345" || !result.BodyTruncated {
		t.Fatalf("Send() result = %+v", result)
	}
}

func TestSenderRejectsInvalidRequestBeforeNetwork(t *testing.T) {
	policy, err := NewURLPolicy(staticResolver{}, false, []int{443})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSender(policy, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Send(t.Context(), SendRequest{}); err == nil {
		t.Fatal("Send() accepted an incomplete request")
	}
}
