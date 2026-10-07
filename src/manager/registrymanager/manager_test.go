package registrymanager_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"doorservice/api"
	"doorservice/dto"
	"doorservice/manager/registrymanager"
	"doorservice/store"

	"github.com/stretchr/testify/require"
)

func TestHydrateLoadsControllersBeforeDoorsAndSkipsBrokenReferences(t *testing.T) {
	controllers := store.NewControllerStore()
	doors := store.NewDoorStore()
	m := registrymanager.New(controllers, doors)

	report := m.Hydrate(api.DoorServiceData{
		AccessControllers: []dto.AccessControllerDTO{{ID: "c1", Activate: true}},
		Doors: []dto.DoorDTO{
			{ID: "d1", AccessControllerID: "c1", Activate: true},
			{ID: "d2", AccessControllerID: "missing", Activate: true},
			{ID: "d3", Activate: false},
		},
	})

	require.Equal(t, 1, report.ControllersLoaded)
	require.Equal(t, 1, report.DoorsLoaded)
	require.Equal(t, []string{"d2"}, report.SkippedDoorIDs)
	require.Equal(t, registrymanager.StateReady, m.State())
	require.NotNil(t, mustController(t, controllers, "c1"))
	require.NotNil(t, mustDoor(t, doors, "d1"))
}

func TestBootstrapStaysDegradedUntilFetchSucceeds(t *testing.T) {
	m := registrymanager.New(
		store.NewControllerStore(),
		store.NewDoorStore(),
		registrymanager.WithBackoff(func(int) time.Duration { return 0 }),
	)
	attempts := 0

	err := m.Bootstrap(context.Background(), func(context.Context) (api.DoorServiceData, error) {
		attempts++
		if attempts == 1 {
			return api.DoorServiceData{}, errors.New("vms unavailable")
		}
		require.Equal(t, registrymanager.StateDegraded, m.State())
		return api.DoorServiceData{
			AccessControllers: []dto.AccessControllerDTO{{ID: "c1", Activate: true}},
		}, nil
	})

	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	require.Equal(t, registrymanager.StateReady, m.State())
}

func mustController(t *testing.T, controllers *store.ControllerStore, id string) *dto.AccessControllerDTO {
	t.Helper()
	controller, ok := controllers.Get(id)
	require.True(t, ok)
	return &controller
}

func mustDoor(t *testing.T, doors *store.DoorStore, id string) *dto.DoorDTO {
	t.Helper()
	door, ok := doors.Get(id)
	require.True(t, ok)
	return &door
}
