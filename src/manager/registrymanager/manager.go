package registrymanager

import (
	"context"
	"sort"
	"sync"
	"time"

	"doorservice/api"
	"doorservice/dto"
	"doorservice/store"
)

type ServiceState string

const (
	StateStarting ServiceState = "STARTING"
	StateDegraded ServiceState = "DEGRADED"
	StateReady    ServiceState = "READY"
	StateStopping ServiceState = "STOPPING"
)

type HydrationReport struct {
	ControllersLoaded int      `json:"controllersLoaded"`
	DoorsLoaded       int      `json:"doorsLoaded"`
	SkippedDoorIDs    []string `json:"skippedDoorIds"`
}

type Backoff func(attempt int) time.Duration

type Option func(*Manager)

type Manager struct {
	mu          sync.RWMutex
	controllers *store.ControllerStore
	doors       *store.DoorStore
	state       ServiceState
	backoff     Backoff
}

func New(controllers *store.ControllerStore, doors *store.DoorStore, options ...Option) *Manager {
	if controllers == nil {
		controllers = store.NewControllerStore()
	}
	if doors == nil {
		doors = store.NewDoorStore()
	}
	m := &Manager{
		controllers: controllers,
		doors:       doors,
		state:       StateStarting,
		backoff:     DefaultBackoff,
	}
	for _, option := range options {
		option(m)
	}
	return m
}

func WithBackoff(backoff Backoff) Option {
	return func(m *Manager) {
		if backoff != nil {
			m.backoff = backoff
		}
	}
}

func DefaultBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Second * time.Duration(1<<attempt)
}

func (m *Manager) Hydrate(data api.DoorServiceData) HydrationReport {
	controllerIDs := make(map[string]struct{}, len(data.AccessControllers))
	controllers := make([]dto.AccessControllerDTO, 0, len(data.AccessControllers))
	for _, controller := range data.AccessControllers {
		if controller.ID == "" || !controller.Activate {
			continue
		}
		controllerIDs[controller.ID] = struct{}{}
		controllers = append(controllers, controller)
	}

	doors := make([]dto.DoorDTO, 0, len(data.Doors))
	skippedDoorIDs := make([]string, 0)
	for _, door := range data.Doors {
		if door.ID == "" || !door.Activate {
			continue
		}
		if door.AccessControllerID != "" {
			if _, ok := controllerIDs[door.AccessControllerID]; !ok {
				skippedDoorIDs = append(skippedDoorIDs, door.ID)
				continue
			}
		}
		doors = append(doors, door)
	}
	sort.Strings(skippedDoorIDs)

	m.mu.Lock()
	_ = m.controllers.ReplaceAll(controllers)
	_ = m.doors.ReplaceAll(doors)
	m.state = StateReady
	m.mu.Unlock()

	return HydrationReport{
		ControllersLoaded: len(controllers),
		DoorsLoaded:       len(doors),
		SkippedDoorIDs:    skippedDoorIDs,
	}
}

func (m *Manager) Bootstrap(
	ctx context.Context,
	fetch func(context.Context) (api.DoorServiceData, error),
) error {
	for attempt := 0; ; attempt++ {
		data, err := fetch(ctx)
		if err == nil {
			m.Hydrate(data)
			return nil
		}
		m.setState(StateDegraded)

		delay := m.backoff(attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			m.setState(StateStopping)
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *Manager) WithSnapshot(
	operation func(*store.ControllerStore, *store.DoorStore) error,
) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return operation(m.controllers, m.doors)
}

func (m *Manager) State() ServiceState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) Stop() {
	m.setState(StateStopping)
}

func (m *Manager) setState(state ServiceState) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}
