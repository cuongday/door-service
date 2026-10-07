package commandmanager

import (
	"context"
	"sync"
	"testing"

	"doorservice/dto"
	serviceManager "doorservice/manager"
	"doorservice/manager/registrymanager"
	"doorservice/store"

	"github.com/stretchr/testify/require"
)

func TestOpenDoorResolvesDoorAndUsesFactory(t *testing.T) {
	doors := store.NewDoorStore()
	controllers := store.NewControllerStore()
	require.NoError(t, controllers.Upsert(dto.AccessControllerDTO{ID: "c1", Type: "HIKVISION"}))
	require.NoError(t, doors.Upsert(dto.DoorDTO{ID: "d1", AccessControllerID: "c1", DoorType: "HIKVISION"}))
	adapter := &recordingAdapter{}
	m := New(doors, controllers, staticFactory{adapter: adapter}, WithClientStore(store.NewClientStore()))

	result := m.Execute(context.Background(), dto.DoorCommand{DoorID: "d1", Command: "OPEN_DOOR"})

	require.True(t, result.Success)
	require.Equal(t, "d1", adapter.Opened().ID)
}

func TestCommandUsesCurrentControllerConnectionInsteadOfDoorCopy(t *testing.T) {
	doors := store.NewDoorStore()
	controllers := store.NewControllerStore()
	index := 1
	// The door was registered before the controller moved to HTTPS with a new password.
	require.NoError(t, doors.Upsert(dto.DoorDTO{
		ID: "d1", AccessControllerID: "c1", DoorType: "HIKVISION", DoorIndex: &index,
		IPAddress: "10.0.0.1", Port: 80, Username: "admin", Password: "old",
	}))
	require.NoError(t, controllers.Upsert(dto.AccessControllerDTO{
		ID: "c1", Type: "HIKVISION", IPAddress: "10.0.0.2", Port: 443,
		Username: "admin", Password: "new", TLS: true, VerifyTLS: false,
	}))
	adapter := &recordingAdapter{}
	m := New(doors, controllers, staticFactory{adapter: adapter})

	result := m.Execute(context.Background(), dto.DoorCommand{DoorID: "d1", Command: "OPEN_DOOR"})

	require.True(t, result.Success)
	opened := adapter.Opened()
	require.Equal(t, "10.0.0.2", opened.IPAddress)
	require.Equal(t, 443, opened.Port)
	require.Equal(t, "new", opened.Password)
	require.Equal(t, true, opened.ProtocolMetadata["tls"])
	require.Equal(t, false, opened.ProtocolMetadata["verifyTls"])
	stored, _ := doors.Get("d1")
	require.Equal(t, "old", stored.Password)
}

func TestExecuteReturnsStableErrorForUnknownDoor(t *testing.T) {
	m := New(store.NewDoorStore(), store.NewControllerStore(), staticFactory{adapter: &recordingAdapter{}})

	result := m.Execute(context.Background(), dto.DoorCommand{DoorID: "missing", Command: "CLOSE_DOOR"})

	require.False(t, result.Success)
	require.Equal(t, serviceManager.ErrorDoorNotFound, result.ErrorCode)
}

func TestExecuteReturnsRegistryNotReadyBeforeBootstrapForUnknownDoor(t *testing.T) {
	m := New(
		store.NewDoorStore(),
		store.NewControllerStore(),
		staticFactory{adapter: &recordingAdapter{}},
		WithRegistry(staticRegistryState(registrymanager.StateDegraded)),
	)

	result := m.Execute(context.Background(), dto.DoorCommand{DoorID: "missing", Command: "OPEN_DOOR"})

	require.False(t, result.Success)
	require.Equal(t, serviceManager.ErrorRegistryNotReady, result.ErrorCode)
}

func TestFactorySelectsSupportedProtocols(t *testing.T) {
	onvif := &recordingAdapter{}
	hikvision := &recordingAdapter{}
	factory := NewFactory(onvif, hikvision, nil, nil)

	got, err := factory.ForController(dto.AccessControllerDTO{Type: "ONVIF"})
	require.NoError(t, err)
	require.Same(t, onvif, got)
	got, err = factory.ForDoor(dto.DoorDTO{DoorType: "HIKVISION"})
	require.NoError(t, err)
	require.Same(t, hikvision, got)
	_, err = factory.ForDoor(dto.DoorDTO{DoorType: "UNKNOWN"})
	require.Error(t, err)
}

type staticFactory struct {
	adapter serviceManager.DoorAdapter
}

type staticRegistryState registrymanager.ServiceState

func (s staticRegistryState) State() registrymanager.ServiceState {
	return registrymanager.ServiceState(s)
}

func (f staticFactory) ForDoor(dto.DoorDTO) (serviceManager.DoorAdapter, error) {
	return f.adapter, nil
}

func (f staticFactory) ForController(dto.AccessControllerDTO) (serviceManager.DoorAdapter, error) {
	return f.adapter, nil
}

type recordingAdapter struct {
	mu     sync.Mutex
	opened dto.DoorDTO
	closed dto.DoorDTO
}

func (a *recordingAdapter) Connect(_ context.Context, controller dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	return controller, nil
}

func (a *recordingAdapter) DiscoverDoors(context.Context, dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	return nil, nil
}

func (a *recordingAdapter) OpenDoor(_ context.Context, door dto.DoorDTO) error {
	a.mu.Lock()
	a.opened = door
	a.mu.Unlock()
	return nil
}

func (a *recordingAdapter) CloseDoor(_ context.Context, door dto.DoorDTO) error {
	a.mu.Lock()
	a.closed = door
	a.mu.Unlock()
	return nil
}

func (a *recordingAdapter) Opened() dto.DoorDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opened
}
