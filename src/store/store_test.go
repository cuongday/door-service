package store_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"doorservice/dto"
	"doorservice/manager"
	"doorservice/store"
)

func TestDoorStoreUpsertAndGetByID(t *testing.T) {
	s := store.NewDoorStore()
	door := dto.DoorDTO{ID: "door-1", AccessControllerID: "controller-1", DoorIndex: intPtr(2)}
	if err := s.Upsert(door); err != nil {
		t.Fatalf("upsert door: %v", err)
	}

	got, ok := s.Get("door-1")
	if !ok || got.AccessControllerID != "controller-1" || got.DoorIndex == nil || *got.DoorIndex != 2 {
		t.Fatalf("unexpected door: %#v, ok=%v", got, ok)
	}
}

func TestDoorStoreRejectsEmptyID(t *testing.T) {
	s := store.NewDoorStore()
	if err := s.Upsert(dto.DoorDTO{}); err == nil {
		t.Fatal("expected empty id error")
	}
}

func TestDoorStoreCopiesMetadataAndBuildsNativeIndexes(t *testing.T) {
	s := store.NewDoorStore()
	index := 3
	door := dto.DoorDTO{
		ID:                 "door-1",
		DoorType:           "HIKVISION",
		AccessControllerID: "controller-1",
		DoorIndex:          &index,
		ONVIFEndpoint:      "http://panel/onvif/door_control",
		ONVIFDoorToken:     "door-token-1",
		ProtocolMetadata:   map[string]any{"supportsAccessDoor": true},
	}
	if err := s.Upsert(door); err != nil {
		t.Fatalf("upsert door: %v", err)
	}

	door.ProtocolMetadata["supportsAccessDoor"] = false
	got, _ := s.Get("door-1")
	got.ProtocolMetadata["supportsAccessDoor"] = false
	again, _ := s.Get("door-1")
	if again.ProtocolMetadata["supportsAccessDoor"] != true {
		t.Fatalf("metadata escaped store copy: %#v", again.ProtocolMetadata)
	}

	byController := s.GetByControllerID("controller-1")
	if len(byController) != 1 || byController[0].ID != "door-1" {
		t.Fatalf("unexpected controller index: %#v", byController)
	}
	byIndex, ok := s.GetByControllerDoorIndex("controller-1", 3)
	if !ok || byIndex.ID != "door-1" {
		t.Fatalf("unexpected door-index lookup: %#v, ok=%v", byIndex, ok)
	}
	byToken, ok := s.GetByONVIFToken("http://panel/onvif/door_control", "door-token-1")
	if !ok || byToken.ID != "door-1" {
		t.Fatalf("unexpected token lookup: %#v, ok=%v", byToken, ok)
	}
}

func TestDoorStoreDeleteRemovesDoorAndIndexesIdempotently(t *testing.T) {
	s := store.NewDoorStore()
	index := 4
	door := dto.DoorDTO{
		ID:                 "door-delete",
		AccessControllerID: "controller-delete",
		DoorIndex:          &index,
		ONVIFEndpoint:      "http://panel/onvif/door_control",
		ONVIFDoorToken:     "door-token-delete",
	}
	if err := s.Upsert(door); err != nil {
		t.Fatalf("upsert door: %v", err)
	}

	if !s.Delete(door.ID) {
		t.Fatal("first delete should remove the door")
	}
	if _, ok := s.Get(door.ID); ok {
		t.Fatal("deleted door remains in primary store")
	}
	if got := s.GetByControllerID(door.AccessControllerID); len(got) != 0 {
		t.Fatalf("controller index still contains deleted door: %#v", got)
	}
	if _, ok := s.GetByONVIFToken(door.ONVIFEndpoint, door.ONVIFDoorToken); ok {
		t.Fatal("ONVIF index still contains deleted door")
	}
	if s.Delete(door.ID) {
		t.Fatal("second delete should be idempotent")
	}
}

func TestControllerStoreReplaceAllDoesNotMutateOnInvalidInput(t *testing.T) {
	s := store.NewControllerStore()
	if err := s.Upsert(dto.AccessControllerDTO{ID: "controller-1"}); err != nil {
		t.Fatalf("upsert controller: %v", err)
	}
	if err := s.ReplaceAll([]dto.AccessControllerDTO{{ID: ""}}); err == nil {
		t.Fatal("expected invalid replacement error")
	}
	if _, ok := s.Get("controller-1"); !ok {
		t.Fatal("invalid replacement mutated the existing snapshot")
	}
}

func TestClientStoreGetOrCreateBuildsOncePerKey(t *testing.T) {
	s := store.NewClientStore()
	var builds atomic.Int32
	const callers = 20

	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry, err := s.GetOrCreate("controller-1", func() (manager.DoorAdapter, error) {
				builds.Add(1)
				return fakeAdapter{}, nil
			})
			if err != nil {
				errs <- err
				return
			}
			if entry.Adapter == nil || entry.OperationMu == nil {
				errs <- errors.New("incomplete client entry")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if builds.Load() != 1 {
		t.Fatalf("adapter built %d times", builds.Load())
	}
}

func TestJobStoreCancelIsIdempotent(t *testing.T) {
	s := store.NewJobStore()
	var calls atomic.Int32
	if err := s.Add("request-1", func() { calls.Add(1) }); err != nil {
		t.Fatalf("add job: %v", err)
	}
	if !s.Cancel("request-1") {
		t.Fatal("first cancel should find the job")
	}
	if s.Cancel("request-1") {
		t.Fatal("second cancel should be a no-op")
	}
	if calls.Load() != 1 {
		t.Fatalf("cancel called %d times", calls.Load())
	}
}

type fakeAdapter struct{}

func (fakeAdapter) Connect(context.Context, dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	return dto.AccessControllerDTO{}, nil
}

func (fakeAdapter) DiscoverDoors(context.Context, dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	return nil, nil
}

func (fakeAdapter) OpenDoor(context.Context, dto.DoorDTO) error { return nil }

func (fakeAdapter) CloseDoor(context.Context, dto.DoorDTO) error { return nil }

func intPtr(v int) *int { return &v }
