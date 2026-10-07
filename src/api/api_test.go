package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"doorservice/api"
	"doorservice/db"

	"github.com/stretchr/testify/require"
)

func TestReadServiceDataDecodesControllersBeforeDoors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/service/data", r.URL.Path)
		require.Equal(t, "service-1", r.URL.Query().Get("id"))
		require.Equal(t, "DOOR", r.URL.Query().Get("type"))

		_, err := io.WriteString(w, `{"id":"service-1","type":"DOOR","accessControllers":[{"id":"controller-1","type":"HIKVISION","activate":true}],"doors":[{"id":"door-1","accessControllerId":"controller-1","activate":true}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()

	client := api.New(server.URL, server.Client())
	data, err := client.ReadServiceData(context.Background(), "service-1")
	require.NoError(t, err)
	require.Len(t, data.AccessControllers, 1)
	require.Len(t, data.Doors, 1)
	require.Equal(t, "controller-1", data.AccessControllers[0].ID)
	require.Equal(t, "door-1", data.Doors[0].ID)
}

func TestCreateServicePostsDoorRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/service", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var payload db.ServiceModel
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "DOOR_SERVICE", payload.Name)
		require.Equal(t, "DOOR", payload.Type)
		require.Equal(t, "door-host", payload.HostName)

		w.Header().Set("Content-Type", "application/json")
		_, err := io.WriteString(w, `{"id":"service-1","name":"DOOR_SERVICE","type":"DOOR","rbmqPrivateHost":"rabbitmq","rbmqPrivatePort":5672}`)
		require.NoError(t, err)
	}))
	defer server.Close()

	client := api.New(server.URL, server.Client())
	service, err := client.CreateService(context.Background(), db.ServiceModel{
		Name:      "DOOR_SERVICE",
		Type:      "DOOR",
		HostName:  "door-host",
		IPAddress: "192.168.1.10",
		Heartbeat: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Equal(t, "service-1", service.ID)
	require.Equal(t, "rabbitmq", service.RbmqPrivateHost)
}

func TestReadServiceReturnsRegisteredRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/service", r.URL.Path)
		require.Equal(t, "service-1", r.URL.Query().Get("ids"))
		_, err := io.WriteString(w, `[{"id":"service-1","name":"DOOR_SERVICE","type":"DOOR"}]`)
		require.NoError(t, err)
	}))
	defer server.Close()

	client := api.New(server.URL, server.Client())
	services, err := client.ReadService(context.Background(), "service-1")
	require.NoError(t, err)
	require.Len(t, services, 1)
	require.Equal(t, "service-1", services[0].ID)
}
