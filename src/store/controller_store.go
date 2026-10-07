package store

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"doorservice/dto"
)

type ControllerStore struct {
	mu    sync.RWMutex
	items map[string]dto.AccessControllerDTO
}

func NewControllerStore() *ControllerStore {
	return &ControllerStore{items: make(map[string]dto.AccessControllerDTO)}
}

func (s *ControllerStore) Upsert(controller dto.AccessControllerDTO) error {
	if controller.ID == "" {
		return errors.New("controller id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[controller.ID] = controller
	return nil
}

func (s *ControllerStore) Get(id string) (dto.AccessControllerDTO, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	controller, ok := s.items[id]
	return controller, ok
}

// FindByAddress returns the stored controller of the given type at ip:port.
func (s *ControllerStore) FindByAddress(controllerType, ipAddress string, port int) (dto.AccessControllerDTO, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, controller := range s.items {
		if strings.EqualFold(controller.Type, controllerType) &&
			controller.IPAddress == ipAddress && controller.Port == port {
			return controller, true
		}
	}
	return dto.AccessControllerDTO{}, false
}

func (s *ControllerStore) GetAll() []dto.AccessControllerDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]dto.AccessControllerDTO, 0, len(s.items))
	for _, controller := range s.items {
		items = append(items, controller)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (s *ControllerStore) ReplaceAll(controllers []dto.AccessControllerDTO) error {
	next := make(map[string]dto.AccessControllerDTO, len(controllers))
	for _, controller := range controllers {
		if controller.ID == "" {
			return errors.New("controller id is required")
		}
		next[controller.ID] = controller
	}
	s.mu.Lock()
	s.items = next
	s.mu.Unlock()
	return nil
}
