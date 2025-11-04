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

package storage_test

import (
	"testing"
	"time"

	gstorage "cloud.google.com/go/storage"
	"github.com/gke-labs/multicluster-leader-election/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorage_ReadWriteCycle(t *testing.T) {
	store := storage.NewInMemoryStorage()
	lease := &storage.LeaseObject{
		Data: storage.LeaseData{
			HolderIdentity:       "test-holder",
			RenewTime:            time.Now(),
			LeaseDurationSeconds: 30,
		},
	}

	// Create
	err := store.CreateLease(lease)
	require.NoError(t, err)
	assert.Equal(t, "1", lease.ResourceVersion)

	// Read
	readLease, err := store.ReadLease()
	require.NoError(t, err)
	assert.Equal(t, lease.Data.HolderIdentity, readLease.Data.HolderIdentity)
	assert.Equal(t, "1", readLease.ResourceVersion)

	// Update
	readLease.Data.HolderIdentity = "new-holder"
	err = store.UpdateLease(readLease)
	require.NoError(t, err)
	assert.Equal(t, "2", readLease.ResourceVersion)

	// Read again
	readLease2, err := store.ReadLease()
	require.NoError(t, err)
	assert.Equal(t, "new-holder", readLease2.Data.HolderIdentity)
	assert.Equal(t, "2", readLease2.ResourceVersion)
}

func TestStorage_CreateConflict(t *testing.T) {
	store := storage.NewInMemoryStorage()
	lease := &storage.LeaseObject{Data: storage.LeaseData{HolderIdentity: "holder1"}}
	err := store.CreateLease(lease)
	require.NoError(t, err)

	lease2 := &storage.LeaseObject{Data: storage.LeaseData{HolderIdentity: "holder2"}}
	err = store.CreateLease(lease2)
	assert.Error(t, err)
}

func TestStorage_UpdateConflict(t *testing.T) {
	store := storage.NewInMemoryStorage()
	lease := &storage.LeaseObject{Data: storage.LeaseData{HolderIdentity: "holder1"}}
	err := store.CreateLease(lease)
	require.NoError(t, err)

	lease1, err := store.ReadLease()
	require.NoError(t, err)
	lease2, err := store.ReadLease()
	require.NoError(t, err)

	lease1.Data.HolderIdentity = "new-holder1"
	err = store.UpdateLease(lease1)
	require.NoError(t, err)

	lease2.Data.HolderIdentity = "new-holder2"
	err = store.UpdateLease(lease2)
	assert.Error(t, err, "second update should fail due to resource version mismatch")
}

func TestStorage_DeleteLease(t *testing.T) {
	store := storage.NewInMemoryStorage()
	lease := &storage.LeaseObject{Data: storage.LeaseData{HolderIdentity: "holder1"}}
	err := store.CreateLease(lease)
	require.NoError(t, err)

	readLease, err := store.ReadLease()
	require.NoError(t, err)

	err = store.DeleteLease(readLease)
	require.NoError(t, err)

	_, err = store.ReadLease()
	assert.Equal(t, gstorage.ErrObjectNotExist, err)
}

func TestStorage_ReadNonExistentLease(t *testing.T) {
	store := storage.NewInMemoryStorage()
	_, err := store.ReadLease()
	assert.Equal(t, gstorage.ErrObjectNotExist, err)
}
