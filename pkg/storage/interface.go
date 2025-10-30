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

import "time"

// LeaseData represents the data stored in the global lease object.
type LeaseData struct {
	HolderIdentity       string    `json:"holderIdentity"`
	RenewTime            time.Time `json:"renewTime"`
	LeaseDurationSeconds int       `json:"leaseDurationSeconds"`
	LeaseTransitions     int32     `json:"leaseTransitions"`
}

// LeaseObject represents the full lease object, including metadata needed for compare-and-swap.
type LeaseObject struct {
	Data            LeaseData
	ResourceVersion string // For GCS, this will be the object's Generation.
}

// Storage is the interface for a pluggable storage backend for the global lock.
type Storage interface {
	// ReadLease reads the current lease object from the backend.
	ReadLease() (*LeaseObject, error)
	// UpdateLease atomically updates the lease object using a compare-and-swap mechanism.
	UpdateLease(lease *LeaseObject) error
	// CreateLease creates the lease object for the first time.
	CreateLease(lease *LeaseObject) error
	// DeleteLease removes the lease object.
	DeleteLease(lease *LeaseObject) error
}

// StorageFactory creates storage backends for a given lease object.
type StorageFactory interface {
	NewStorage(leaseObjectName string) Storage
}
