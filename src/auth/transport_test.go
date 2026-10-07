package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnsureServiceSessionStoresTokenAndExpiry(t *testing.T) {
	privateKeyPEM, _, err := GenerateKeyPairPEM()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("serviceId") != "service-1" {
			t.Fatalf("unexpected service id: %s", r.URL.Query().Get("serviceId"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"sessionToken":"session-token","expiresAt":%q}`, expiresAt.Format(time.RFC3339))
	}))
	defer server.Close()

	serviceID := "service-1"
	sessionURL := server.URL + "/api/m2m/session/open"
	transport := &AuthTransport{Transport: http.DefaultTransport}
	transport.ConfigureRuntimeAuth(&sessionURL, &serviceID, &privateKeyPEM)

	if err := transport.ensureServiceSession(); err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	if transport.AccessToken == nil || *transport.AccessToken != "session-token" {
		t.Fatalf("unexpected access token: %#v", transport.AccessToken)
	}
	if transport.SessionExpiry == nil || !transport.SessionExpiry.Equal(expiresAt) {
		t.Fatalf("unexpected session expiry: %#v", transport.SessionExpiry)
	}
}

func TestSignedHeadersForWebsocketUsesServiceSession(t *testing.T) {
	privateKeyPEM, _, err := GenerateKeyPairPEM()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"sessionToken":"websocket-token","expiresAt":%q}`, expiresAt.Format(time.RFC3339))
	}))
	defer server.Close()

	serviceID := "service-1"
	sessionURL := server.URL + "/api/m2m/session/open"
	transport := &AuthTransport{Transport: http.DefaultTransport}
	transport.ConfigureRuntimeAuth(&sessionURL, &serviceID, &privateKeyPEM)

	headers, err := transport.SignedHeadersForWebsocket()
	if err != nil {
		t.Fatalf("signed websocket headers: %v", err)
	}
	if got := headers.Get("Authorization"); got != "Bearer websocket-token" {
		t.Fatalf("authorization header = %q", got)
	}
}
