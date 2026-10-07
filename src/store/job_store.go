package store

import (
	"context"
	"errors"
	"sync"
)

type JobStore struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func NewJobStore() *JobStore {
	return &JobStore{cancel: make(map[string]context.CancelFunc)}
}

func (s *JobStore) Add(requestID string, cancel context.CancelFunc) error {
	if requestID == "" {
		return errors.New("request id is required")
	}
	if cancel == nil {
		return errors.New("cancel function is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.cancel[requestID]; exists {
		return errors.New("request id already exists")
	}
	s.cancel[requestID] = cancel
	return nil
}

func (s *JobStore) Cancel(requestID string) bool {
	s.mu.Lock()
	cancel, ok := s.cancel[requestID]
	if ok {
		delete(s.cancel, requestID)
	}
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (s *JobStore) Remove(requestID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cancel[requestID]; !ok {
		return false
	}
	delete(s.cancel, requestID)
	return true
}

func (s *JobStore) Has(requestID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.cancel[requestID]
	return ok
}
