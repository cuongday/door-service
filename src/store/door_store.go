package store

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"doorservice/dto"
)

type DoorStore struct {
	mu           sync.RWMutex
	items        map[string]dto.DoorDTO
	byController map[string]map[string]struct{}
	byONVIF      map[string]string
	byDoorIndex  map[string]string
}

func NewDoorStore() *DoorStore {
	return &DoorStore{
		items:        make(map[string]dto.DoorDTO),
		byController: make(map[string]map[string]struct{}),
		byONVIF:      make(map[string]string),
		byDoorIndex:  make(map[string]string),
	}
}

func (s *DoorStore) Upsert(door dto.DoorDTO) error {
	if door.ID == "" {
		return errors.New("door id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.items[door.ID]; ok {
		s.removeIndexes(current)
	}
	door = cloneDoor(door)
	s.items[door.ID] = door
	s.addIndexes(door)
	return nil
}

func (s *DoorStore) Get(id string) (dto.DoorDTO, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	door, ok := s.items[id]
	return cloneDoor(door), ok
}

func (s *DoorStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	door, ok := s.items[id]
	if !ok {
		return false
	}
	s.removeIndexes(door)
	delete(s.items, id)
	return true
}

func (s *DoorStore) GetAll() []dto.DoorDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collectSorted(s.items)
}

func (s *DoorStore) GetByControllerID(controllerID string) []dto.DoorDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.byController[controllerID]
	doors := make(map[string]dto.DoorDTO, len(ids))
	for id := range ids {
		doors[id] = s.items[id]
	}
	return s.collectSorted(doors)
}

func (s *DoorStore) GetByONVIFToken(endpoint, token string) (dto.DoorDTO, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byONVIF[onvifKey(endpoint, token)]
	if !ok {
		return dto.DoorDTO{}, false
	}
	return cloneDoor(s.items[id]), true
}

func (s *DoorStore) GetByControllerDoorIndex(controllerID string, index int) (dto.DoorDTO, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byDoorIndex[doorIndexKey(controllerID, index)]
	if !ok {
		return dto.DoorDTO{}, false
	}
	return cloneDoor(s.items[id]), true
}

func (s *DoorStore) ReplaceAll(doors []dto.DoorDTO) error {
	next := NewDoorStore()
	for _, door := range doors {
		if err := next.Upsert(door); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.items = next.items
	s.byController = next.byController
	s.byONVIF = next.byONVIF
	s.byDoorIndex = next.byDoorIndex
	s.mu.Unlock()
	return nil
}

func (s *DoorStore) addIndexes(door dto.DoorDTO) {
	if door.AccessControllerID != "" {
		ids := s.byController[door.AccessControllerID]
		if ids == nil {
			ids = make(map[string]struct{})
			s.byController[door.AccessControllerID] = ids
		}
		ids[door.ID] = struct{}{}
	}
	if door.ONVIFEndpoint != "" && door.ONVIFDoorToken != "" {
		s.byONVIF[onvifKey(door.ONVIFEndpoint, door.ONVIFDoorToken)] = door.ID
	}
	if door.AccessControllerID != "" && door.DoorIndex != nil {
		s.byDoorIndex[doorIndexKey(door.AccessControllerID, *door.DoorIndex)] = door.ID
	}
}

func (s *DoorStore) removeIndexes(door dto.DoorDTO) {
	if ids := s.byController[door.AccessControllerID]; ids != nil {
		delete(ids, door.ID)
		if len(ids) == 0 {
			delete(s.byController, door.AccessControllerID)
		}
	}
	if door.ONVIFEndpoint != "" && door.ONVIFDoorToken != "" {
		delete(s.byONVIF, onvifKey(door.ONVIFEndpoint, door.ONVIFDoorToken))
	}
	if door.AccessControllerID != "" && door.DoorIndex != nil {
		delete(s.byDoorIndex, doorIndexKey(door.AccessControllerID, *door.DoorIndex))
	}
}

func (s *DoorStore) collectSorted(items map[string]dto.DoorDTO) []dto.DoorDTO {
	doors := make([]dto.DoorDTO, 0, len(items))
	for _, door := range items {
		doors = append(doors, cloneDoor(door))
	}
	sort.Slice(doors, func(i, j int) bool { return doors[i].ID < doors[j].ID })
	return doors
}

func onvifKey(endpoint, token string) string {
	return endpoint + "\x00" + token
}

func doorIndexKey(controllerID string, index int) string {
	return fmt.Sprintf("%s\x00%d", controllerID, index)
}

func cloneDoor(door dto.DoorDTO) dto.DoorDTO {
	if door.DoorIndex != nil {
		index := *door.DoorIndex
		door.DoorIndex = &index
	}
	door.ProtocolMetadata = cloneMetadata(door.ProtocolMetadata)
	return door
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	clone := make(map[string]any, len(metadata))
	for key, value := range metadata {
		clone[key] = cloneMetadataValue(value)
	}
	return clone
}

func cloneMetadataValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMetadata(typed)
	case []any:
		clone := make([]any, len(typed))
		for i, item := range typed {
			clone[i] = cloneMetadataValue(item)
		}
		return clone
	default:
		return typed
	}
}
