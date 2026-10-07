package store

import (
	"context"
	"errors"
	"sync"

	"doorservice/manager"
)

type ClientEntry struct {
	Adapter     manager.DoorAdapter
	OperationMu *sync.Mutex
}

type clientSlot struct {
	buildMu     sync.Mutex
	operationMu sync.Mutex
	adapter     manager.DoorAdapter
}

type ClientStore struct {
	mu    sync.Mutex
	slots map[string]*clientSlot
}

func NewClientStore() *ClientStore {
	return &ClientStore{slots: make(map[string]*clientSlot)}
}

func (s *ClientStore) GetOrCreate(
	key string,
	build func() (manager.DoorAdapter, error),
) (ClientEntry, error) {
	if key == "" {
		return ClientEntry{}, errors.New("client key is required")
	}
	if build == nil {
		return ClientEntry{}, errors.New("client builder is required")
	}

	s.mu.Lock()
	slot := s.slots[key]
	if slot == nil {
		slot = &clientSlot{}
		s.slots[key] = slot
	}
	s.mu.Unlock()

	slot.buildMu.Lock()
	defer slot.buildMu.Unlock()
	if slot.adapter == nil {
		adapter, err := build()
		if err != nil {
			return ClientEntry{}, err
		}
		if adapter == nil {
			return ClientEntry{}, errors.New("client builder returned nil adapter")
		}
		slot.adapter = adapter
	}
	return ClientEntry{Adapter: slot.adapter, OperationMu: &slot.operationMu}, nil
}

func (s *ClientStore) WithController(
	ctx context.Context,
	key string,
	build func() (manager.DoorAdapter, error),
	operation func(manager.DoorAdapter) error,
) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if operation == nil {
		return errors.New("client operation is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := s.GetOrCreate(key, build)
	if err != nil {
		return err
	}
	entry.OperationMu.Lock()
	defer entry.OperationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return operation(entry.Adapter)
}
