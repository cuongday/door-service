package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"commonkit/database"
	"doorservice/config"
	"doorservice/db"
	servicews "doorservice/websocket"

	"github.com/stretchr/testify/require"
)

func TestLoadBuildsServerVMSURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"isBox":false,"apiHost":"127.0.0.1","apiPort":58080,"apiSecure":false}`), 0o600))

	loaded, err := config.Load(path)
	require.NoError(t, err)
	base := loaded.GetConfig()
	require.Equal(t, "http://127.0.0.1:58080", base.VmsAPIURL)
	require.Equal(t, "ws://127.0.0.1:58080", base.VmsWSURL)
}

func TestStartRegistersDoorServiceWhenCacheIsEmpty(t *testing.T) {
	apiClient := &fakeServiceAPI{
		created: db.ServiceModel{
			BaseModel:       database.BaseModel{ID: "service-1"},
			Name:            config.ServiceName,
			Type:            config.ServiceType,
			RbmqPrivateHost: "rabbitmq.internal",
			RbmqPrivatePort: 5672,
		},
	}
	base := config.NewBaseConfig(config.ConfigJSON{APIHost: "vms.local", APIPort: 58080}, apiClient, servicews.New())
	base.DatabasePath = filepath.Join(t.TempDir(), "service.db")
	server := config.NewServerConfig(base)

	require.NoError(t, server.Start(context.Background()))
	require.Equal(t, config.ServiceName, apiClient.registration.Name)
	require.Equal(t, config.ServiceType, apiClient.registration.Type)
	require.Equal(t, "DOOR_service-1", base.RbmqQueuePrefix)
}

func TestStartReplacesStaleCachedRegistration(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "service.db")
	connection, err := database.SetupDatabase(databasePath)
	require.NoError(t, err)
	_, err = db.New(connection).Save(db.ServiceModel{
		BaseModel: database.BaseModel{ID: "stale-service"},
		Type:      config.ServiceType,
	})
	require.NoError(t, err)

	apiClient := &fakeServiceAPI{created: db.ServiceModel{
		BaseModel: database.BaseModel{ID: "replacement-service"},
		Type:      config.ServiceType,
	}}
	base := config.NewBaseConfig(config.ConfigJSON{APIHost: "vms.local", APIPort: 58080}, apiClient, servicews.New())
	base.DatabasePath = databasePath
	base.RetryDelay = time.Millisecond
	server := config.NewServerConfig(base)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	require.NoError(t, server.Start(ctx))
	require.Equal(t, "stale-service", apiClient.readServiceID)
	require.Equal(t, "replacement-service", base.ServiceID)
}

type fakeServiceAPI struct {
	created       db.ServiceModel
	registration  db.ServiceModel
	readServiceID string
	services      []db.ServiceModel
}

func (f *fakeServiceAPI) ReadService(_ context.Context, serviceID string) ([]db.ServiceModel, error) {
	f.readServiceID = serviceID
	return f.services, nil
}

func (f *fakeServiceAPI) CreateService(_ context.Context, service db.ServiceModel) (db.ServiceModel, error) {
	f.registration = service
	return f.created, nil
}

func TestApplyRegisteredServiceBuildsDoorQueueAndWebsocketURL(t *testing.T) {
	ws := servicews.New()
	base := config.NewBaseConfig(config.ConfigJSON{}, nil, ws)
	base.VmsWSURL = "ws://vms.local:58080"

	base.ApplyRegisteredService(db.ServiceModel{
		BaseModel:       database.BaseModel{ID: "service-1"},
		Type:            "DOOR",
		RbmqPrivateHost: "rabbitmq.internal",
		RbmqPrivatePort: 5672,
	})

	require.Equal(t, "service-1", base.ServiceID)
	require.Equal(t, "DOOR_service-1", base.RbmqQueuePrefix)
	require.Equal(t, "rabbitmq.internal", base.RbmqHost)
	require.Equal(t, 5672, base.RbmqPort)
	require.Equal(t, "ws://vms.local:58080/service/service-1", ws.URL())
}
