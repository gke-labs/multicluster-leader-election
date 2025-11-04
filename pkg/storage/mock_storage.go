// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"errors"
	"strconv"
	"sync"

	gstorage "cloud.google.com/go/storage"
)

// InMemoryStorage is a simple in-memory implementation of the Storage interface for testing.
type InMemoryStorage struct {
	mu    sync.Mutex
	lease *LeaseObject
	err   error // for injecting errors
}

// NewInMemoryStorage creates a new InMemoryStorage.
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{}
}

func (s *InMemoryStorage) ReadLease() (*LeaseObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.lease == nil {
		return nil, gstorage.ErrObjectNotExist
	}
	// Return a copy to prevent race conditions
	leaseCopy := *s.lease
	return &leaseCopy, nil
}

func (s *InMemoryStorage) UpdateLease(lease *LeaseObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.lease == nil {
		return gstorage.ErrObjectNotExist
	}
	if s.lease.ResourceVersion != lease.ResourceVersion {
		return errors.New("resource version mismatch")
	}
	currentVersion, _ := strconv.Atoi(s.lease.ResourceVersion)
	lease.ResourceVersion = strconv.Itoa(currentVersion + 1)
	s.lease = lease
	return nil
}

func (s *InMemoryStorage) CreateLease(lease *LeaseObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.lease != nil {
		return errors.New("object already exists")
	}
	lease.ResourceVersion = "1"
	s.lease = lease
	return nil
}

func (s *InMemoryStorage) DeleteLease(lease *LeaseObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.lease == nil {
		return gstorage.ErrObjectNotExist
	}
	if s.lease.ResourceVersion != lease.ResourceVersion {
		return errors.New("resource version mismatch")
	}
	s.lease = nil
	return nil
}

// InjectError allows injecting an error for testing error paths.
func (s *InMemoryStorage) InjectError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// InMemoryStorageFactory creates InMemoryStorage instances.
// It manages a collection of in-memory lease objects, keyed by their name.
type InMemoryStorageFactory struct {
	mu       sync.Mutex
	storages map[string]*InMemoryStorage
}

// NewInMemoryStorageFactory creates a new InMemoryStorageFactory.
func NewInMemoryStorageFactory() *InMemoryStorageFactory {
	return &InMemoryStorageFactory{
		storages: make(map[string]*InMemoryStorage),
	}
}

// NewStorage returns the InMemoryStorage instance for the given lease object name.
// It creates a new instance if one doesn't exist.
func (f *InMemoryStorageFactory) NewStorage(leaseObjectName string) Storage {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.storages[leaseObjectName]; !exists {
		f.storages[leaseObjectName] = NewInMemoryStorage()
	}
	return f.storages[leaseObjectName]
}

// GetStorage returns the InMemoryStorage instance for the given lease object name,
// allowing direct manipulation for testing (e.g., injecting errors or pre-populating data).
func (f *InMemoryStorageFactory) GetStorage(leaseObjectName string) *InMemoryStorage {
	// Re-use NewStorage logic to ensure it exists
	return f.NewStorage(leaseObjectName).(*InMemoryStorage)
}
