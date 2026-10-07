package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"doorservice/api"

	"github.com/stretchr/testify/require"
)

func TestDoorServiceDataContractFixture(t *testing.T) {
	payload, err := os.ReadFile("../tests/contracts/door_service_data.json")
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "DOOR", r.URL.Query().Get("type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	data, err := api.New(server.URL, server.Client()).ReadServiceData(context.Background(), "service-1")
	require.NoError(t, err)
	require.Len(t, data.AccessControllers, 2)
	require.Equal(t, "controller-hikvision", data.AccessControllers[0].ID)
	require.True(t, data.AccessControllers[0].VerifyTLS)
	require.Len(t, data.Doors, 2)
	require.Equal(t, 1, *data.Doors[0].DoorIndex)
	require.Equal(t, "http://192.168.1.20/onvif/door_control", data.Doors[1].ONVIFEndpoint)
	require.Equal(t, "door_1", data.Doors[1].ONVIFDoorToken)
}
