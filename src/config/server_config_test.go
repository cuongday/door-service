package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"commonkit/database"
	"doorservice/api"
	"doorservice/auth"
	"doorservice/db"
	doorwebsocket "doorservice/websocket"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestServerRegistrationUsesDirectM2MURLAndBootstrapSecret(t *testing.T) {
	const bootstrapSecret = "test-bootstrap-secret"

	type receivedRequest struct {
		method          string
		path            string
		bootstrapSecret string
		payload         map[string]any
	}
	received := make(chan receivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- receivedRequest{
			method:          r.Method,
			path:            r.URL.Path,
			bootstrapSecret: r.Header.Get("X-M2M-Bootstrap-Secret"),
			payload:         payload,
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	transport := &auth.AuthTransport{Transport: http.DefaultTransport}
	base := &BaseConfig{
		ConfigJSON: ConfigJSON{
			APIHost:         serverURL.Hostname(),
			APIPort:         mustPort(t, serverURL),
			BootstrapSecret: bootstrapSecret,
		},
		API:                 api.New("", transport),
		logger:              zap.NewNop(),
		SigningPublicKeyPEM: "door-public-key",
	}
	config := newServerConfigWithBase(base)

	config.update()
	require.False(t, base.createAndSaveService())
	request := <-received
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/m2m/service", request.path)
	require.Equal(t, bootstrapSecret, request.bootstrapSecret)
	require.Equal(t, "door-public-key", request.payload["publicKeyPem"])
	require.NotContains(t, request.payload, "publicKey")
}

func TestExistingServiceMissingPublicKeyIsPatched(t *testing.T) {
	const bootstrapSecret = "test-bootstrap-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		require.Equal(t, "/api/m2m/service", r.URL.Path)
		require.Equal(t, bootstrapSecret, r.Header.Get("X-M2M-Bootstrap-Secret"))
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "service-1", payload["id"])
		require.Equal(t, ServiceType, payload["type"])
		require.Equal(t, "door-public-key", payload["publicKeyPem"])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &auth.AuthTransport{Transport: http.DefaultTransport}
	base := &BaseConfig{
		API:                 api.New(server.URL, transport),
		ConfigJSON:          ConfigJSON{BootstrapSecret: bootstrapSecret},
		SigningPublicKeyPEM: "door-public-key",
		logger:              zap.NewNop(),
	}
	newServerConfigWithBase(base)
	service := &db.ServiceModel{BaseModel: database.BaseModel{ID: "service-1"}}

	require.NoError(t, base.ensureServicePublicKey(service))
	require.Equal(t, "door-public-key", service.PublicKeyPem)
}

func TestServerCallbackUsesM2MWebsocketPath(t *testing.T) {
	ws := doorwebsocket.New()
	base := &BaseConfig{
		ServiceID: "service-1",
		VmsWSURL:  "ws://vms.example",
		Websocket: ws,
	}
	config := newServerConfigWithBase(base)

	config.onServiceRegisteredCallback(&db.ServiceModel{})

	require.Equal(t, "ws://vms.example/api/m2m/service/service-1", ws.URL())
}

func mustPort(t *testing.T, value *url.URL) int {
	t.Helper()
	port := value.Port()
	require.NotEmpty(t, port)
	parsed, err := strconv.Atoi(port)
	require.NoError(t, err)
	return parsed
}
