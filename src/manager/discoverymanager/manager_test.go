package discoverymanager

import (
	"context"
	"sync"
	"testing"
	"time"

	"doorservice/dto"
	"doorservice/manager"
	"doorservice/store"

	"github.com/stretchr/testify/require"
)

func TestExpandIPv4RangeIsInclusiveAndRejectsReversedRange(t *testing.T) {
	ips, err := expandIPv4Range("192.168.1.10", "192.168.1.12")
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.1.10", "192.168.1.11", "192.168.1.12"}, ips)

	_, err = expandIPv4Range("192.168.1.12", "192.168.1.10")
	require.Error(t, err)
}

func TestExpandIPv4RangeStopsAtConfiguredLimit(t *testing.T) {
	_, err := expandIPv4RangeLimited("10.0.0.1", "10.0.0.3", 2)
	require.Error(t, err)
}

func TestStartEmitsOneTerminalResultPerIPAndDoneOnce(t *testing.T) {
	adapter := &fakeAdapter{}
	controllers := store.NewControllerStore()
	doors := store.NewDoorStore()
	m := New(adapter, controllers, doors, store.NewJobStore(), 2)

	var mu sync.Mutex
	var results []dto.DiscoveryResult
	done := 0
	err := m.Start(
		context.Background(),
		"req-1",
		dto.DiscoveryDoorInfoDTO{FromIP: "192.168.1.10", ToIP: "192.168.1.12", Ports: []int{80}},
		func(result dto.DiscoveryResult) {
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		},
		func() {
			mu.Lock()
			done++
			mu.Unlock()
		},
	)
	require.NoError(t, err)
	m.Wait("req-1")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, results, 3)
	require.Equal(t, 1, done)
	require.LessOrEqual(t, adapter.MaxConcurrent(), 2)
	require.Len(t, controllers.GetAll(), 3)
	require.Len(t, doors.GetAll(), 3)
}

func TestStartUsesONVIFPortFallbackAndCredentialCartesianProduct(t *testing.T) {
	adapter := &fakeAdapter{acceptUsername: "operator", acceptPassword: "second", acceptPort: 8080}
	m := New(adapter, store.NewControllerStore(), store.NewDoorStore(), store.NewJobStore(), 1)
	onvifPort := 8080

	var result dto.DiscoveryResult
	require.NoError(t, m.Start(
		context.Background(),
		"req-credentials",
		dto.DiscoveryDoorInfoDTO{
			Usernames: []string{"admin", "operator"},
			Passwords: []string{"first", "second"},
			FromIP:    "10.0.0.5",
			ONVIFPort: &onvifPort,
		},
		func(got dto.DiscoveryResult) { result = got },
		nil,
	))
	m.Wait("req-credentials")

	require.True(t, result.State)
	require.Equal(t, 8080, result.Port)
	require.Equal(t, "operator", result.Username)
	require.Equal(t, "second", result.Password)
	require.Equal(t, 4, adapter.Attempts())
}

func TestStartUsesPortsAndCredentialsFromSingleInput(t *testing.T) {
	adapter := &fakeAdapter{acceptUsername: "operator", acceptPassword: "second", acceptPort: 8080}
	m := New(adapter, store.NewControllerStore(), store.NewDoorStore(), store.NewJobStore(), 1)

	var result dto.DiscoveryResult
	require.NoError(t, m.Start(
		context.Background(),
		"req-overlap",
		dto.DiscoveryDoorInfoDTO{
			Usernames: []string{"admin", "operator"}, Passwords: []string{"first", "second"},
			FromIP: "10.0.0.5", Ports: []int{80, 8080},
		},
		func(got dto.DiscoveryResult) { result = got },
		nil,
	))
	m.Wait("req-overlap")

	require.True(t, result.State)
	require.Equal(t, 8080, result.Port)
	require.Equal(t, "operator", result.Username)
}

func TestStopCancelsOnlyMatchingJobAndStillCompletesOnce(t *testing.T) {
	adapter := &fakeAdapter{block: make(chan struct{})}
	m := New(adapter, store.NewControllerStore(), store.NewDoorStore(), store.NewJobStore(), 1)

	var mu sync.Mutex
	var results []dto.DiscoveryResult
	done := 0
	require.NoError(t, m.Start(
		context.Background(),
		"req-cancel",
		dto.DiscoveryDoorInfoDTO{FromIP: "10.0.0.1", ToIP: "10.0.0.3", Ports: []int{80}},
		func(result dto.DiscoveryResult) {
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		},
		func() {
			mu.Lock()
			done++
			mu.Unlock()
		},
	))

	require.Eventually(t, func() bool { return adapter.Attempts() > 0 }, time.Second, time.Millisecond)
	require.False(t, m.Stop("another-request"))
	require.True(t, m.Stop("req-cancel"))
	m.Wait("req-cancel")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, results, 3)
	require.Equal(t, 1, done)
	for _, result := range results {
		require.Equal(t, "cancelled", result.ResultLevel)
		require.Equal(t, manager.ErrorDiscoveryCancelled, result.ErrorCode)
	}
}

func TestStartPropagatesAIBoxScopeToDiscoveryResult(t *testing.T) {
	m := New(&fakeAdapter{}, store.NewControllerStore(), store.NewDoorStore(), store.NewJobStore(), 1)

	var result dto.DiscoveryResult
	require.NoError(t, m.Start(
		context.Background(),
		"req-aibox",
		dto.DiscoveryDoorInfoDTO{
			AIBoxID: "00000000-0000-0000-0000-000000000552",
			FromIP:  "10.0.0.8", ToIP: "10.0.0.8", Ports: []int{80},
		},
		func(got dto.DiscoveryResult) { result = got },
		nil,
	))
	m.Wait("req-aibox")

	require.Equal(t, "00000000-0000-0000-0000-000000000552", result.AIBoxID)
	require.NotNil(t, result.Controller)
	require.Equal(t, result.AIBoxID, result.Controller.AIBoxID)
	require.Len(t, result.Doors, 1)
	require.Equal(t, result.AIBoxID, result.Doors[0].AIBoxID)
}

type fakeAdapter struct {
	mu             sync.Mutex
	concurrent     int
	maxConcurrent  int
	attempts       int
	acceptUsername string
	acceptPassword string
	acceptPort     int
	block          chan struct{}
}

func (f *fakeAdapter) Connect(ctx context.Context, controller dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	f.mu.Lock()
	f.attempts++
	f.concurrent++
	if f.concurrent > f.maxConcurrent {
		f.maxConcurrent = f.concurrent
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.concurrent--
		f.mu.Unlock()
	}()

	if f.block != nil {
		select {
		case <-ctx.Done():
			return dto.AccessControllerDTO{}, ctx.Err()
		case <-f.block:
		}
	}
	if f.acceptUsername != "" && (controller.Username != f.acceptUsername || controller.Password != f.acceptPassword) {
		return dto.AccessControllerDTO{}, manager.NewServiceError(manager.ErrorAuthFailed, "authentication failed", nil)
	}
	if f.acceptPort != 0 && controller.Port != f.acceptPort {
		return dto.AccessControllerDTO{}, manager.NewServiceError(manager.ErrorDeviceUnreachable, "device unreachable", nil)
	}
	controller.Type = "ONVIF"
	controller.Activate = true
	return controller, nil
}

func (f *fakeAdapter) DiscoverDoors(_ context.Context, controller dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	return []dto.DoorDTO{{
		ID:                 controller.ID + ":door-1",
		DoorType:           "ONVIF",
		AccessControllerID: controller.ID,
		Activate:           true,
		AIBoxID:            controller.AIBoxID,
	}}, nil
}

func (f *fakeAdapter) OpenDoor(context.Context, dto.DoorDTO) error  { return nil }
func (f *fakeAdapter) CloseDoor(context.Context, dto.DoorDTO) error { return nil }

func (f *fakeAdapter) Attempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

func (f *fakeAdapter) MaxConcurrent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxConcurrent
}
