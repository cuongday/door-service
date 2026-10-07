package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"commonkit/util/httputil"
	"commonkit/util/jsonutil"
)

const (
	nonceHeader     = "X-M2M-Nonce"
	signatureHeader = "X-M2M-Signature"
)

var authOnce sync.Once
var authTransport *AuthTransport

type AuthTransport struct {
	mu sync.Mutex

	ServiceID         *string
	SessionURL        *string
	SigningPrivateKey *string
	RegistrationToken *string
	RegistrationKey   *string
	AccessToken       *string
	SessionExpiry     *time.Time
	Transport         http.RoundTripper
}

type SessionResponseData struct {
	SessionToken string    `json:"sessionToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func GetAuthTransport() *AuthTransport {
	authOnce.Do(func() {
		authTransport = &AuthTransport{
			Transport: http.DefaultTransport,
		}
	})
	return authTransport
}

func (m *AuthTransport) ConfigureRegistrationAuth(token, tokenKey *string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RegistrationToken = token
	m.RegistrationKey = tokenKey
	m.ServiceID = nil
	m.SessionURL = nil
	m.SigningPrivateKey = nil
	m.AccessToken = nil
	m.SessionExpiry = nil
}

func (m *AuthTransport) ConfigureRuntimeAuth(sessionURL, serviceID, signingPrivateKey *string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SessionURL = sessionURL
	m.ServiceID = serviceID
	m.SigningPrivateKey = signingPrivateKey
	m.RegistrationToken = nil
	m.RegistrationKey = nil
	m.AccessToken = nil
	m.SessionExpiry = nil
}

func (m *AuthTransport) InvalidateSession() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AccessToken = nil
	m.SessionExpiry = nil
}

func (m *AuthTransport) SignedHeadersForWebsocket() (http.Header, error) {
	headers := make(http.Header)
	if m.serviceSessionEnabled() {
		if err := m.ensureServiceSession(); err != nil {
			return nil, err
		}
		headers.Set("Authorization", "Bearer "+*m.AccessToken)
		return headers, nil
	}
	m.addRegistrationHeaders(headers)
	return headers, nil
}

func (m *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.serviceSessionEnabled() {
		if err := m.ensureServiceSession(); err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+*m.AccessToken)
		resp, err := m.Transport.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			m.InvalidateSession()
			if err := m.ensureServiceSession(); err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+*m.AccessToken)
			return m.Transport.RoundTrip(req)
		}
		return resp, nil
	}

	m.addRegistrationHeaders(req.Header)
	return m.Transport.RoundTrip(req)
}

func (m *AuthTransport) addRegistrationHeaders(headers http.Header) {
	if m.RegistrationToken == nil || m.RegistrationKey == nil {
		return
	}
	if *m.RegistrationToken == "" || *m.RegistrationKey == "" {
		return
	}
	headers.Set(*m.RegistrationKey, *m.RegistrationToken)
}

func (m *AuthTransport) serviceSessionEnabled() bool {
	return m.ServiceID != nil && *m.ServiceID != "" &&
		m.SessionURL != nil && *m.SessionURL != "" &&
		m.SigningPrivateKey != nil && *m.SigningPrivateKey != ""
}

func (m *AuthTransport) serviceSessionUsable() bool {
	return m.AccessToken != nil && m.SessionExpiry != nil && m.SessionExpiry.After(time.Now().Add(30*time.Second))
}

func (m *AuthTransport) ensureServiceSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.serviceSessionUsable() {
		return nil
	}
	if !m.serviceSessionEnabled() {
		return errors.New("service session is not configured")
	}

	nonce, err := randomNonce()
	if err != nil {
		return err
	}
	signature, err := SignSessionOpen(*m.SigningPrivateKey, *m.ServiceID, nonce)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, *m.SessionURL+"?serviceId="+*m.ServiceID, nil)
	if err != nil {
		return err
	}
	req.Header.Set(nonceHeader, nonce)
	req.Header.Set(signatureHeader, signature)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := httputil.ReadResponseBody(resp)
		return fmt.Errorf("m2m session request failed: %d %s", resp.StatusCode, string(body))
	}

	body, err := httputil.ReadResponseBody(resp)
	if err != nil {
		return err
	}
	data, err := jsonutil.Unmarshal[SessionResponseData](body)
	if err != nil {
		return err
	}
	if data == nil || data.SessionToken == "" || data.ExpiresAt.IsZero() {
		return errors.New("m2m session response is incomplete")
	}
	m.AccessToken = &data.SessionToken
	expiresAt := data.ExpiresAt
	m.SessionExpiry = &expiresAt
	return nil
}

func randomNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.New("generate nonce: " + err.Error())
	}
	return hex.EncodeToString(buf), nil
}
