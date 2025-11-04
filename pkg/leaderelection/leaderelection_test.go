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

package leaderelection_test

import (
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/gke-labs/multicluster-leader-election/api/v1alpha1"
	"github.com/gke-labs/multicluster-leader-election/pkg/leaderelection"
	"github.com/gke-labs/multicluster-leader-election/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newLease(renewTime time.Time, duration int32) *v1alpha1.MultiClusterLease {
	return &v1alpha1.MultiClusterLease{
		Spec: v1alpha1.MultiClusterLeaseSpec{
			RenewTime:            &metav1.MicroTime{Time: renewTime},
			LeaseDurationSeconds: &duration,
		},
	}
}

func TestAcquireOrRenew_CreateLeaseSuccess(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now(), 30)

	info, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)
	assert.True(t, info.Acquired)
	assert.Equal(t, "candidate-1", *info.HolderIdentity)
	assert.NotNil(t, info.LeaseTransitions)
	assert.Equal(t, int32(1), *info.LeaseTransitions)
}

func TestAcquireOrRenew_CreateLeaseRace(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now(), 30)

	// Simulate another process creating the lease first
	existingLease := &storage.LeaseObject{
		Data: storage.LeaseData{
			HolderIdentity:       "other-candidate",
			RenewTime:            time.Now(),
			LeaseDurationSeconds: 30,
			LeaseTransitions:     1,
		},
	}
	err := store.CreateLease(existingLease)
	require.NoError(t, err)

	info, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)
	assert.False(t, info.Acquired)
	assert.Equal(t, "other-candidate", *info.HolderIdentity)
	assert.NotNil(t, info.LeaseTransitions)
	assert.Equal(t, int32(1), *info.LeaseTransitions)
}

func TestAcquireOrRenew_RenewLeaseSuccess(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now(), 30)

	// First, acquire the lease
	info1, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)
	assert.Equal(t, int32(1), *info1.LeaseTransitions)

	// Then, renew it
	info2, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)
	assert.True(t, info2.Acquired)
	assert.Equal(t, "candidate-1", *info2.HolderIdentity)
	assert.Equal(t, int32(1), *info2.LeaseTransitions)
}

func TestAcquireOrRenew_AcquireExpiredLease(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now(), 30)

	// Create an expired lease
	expiredLease := &storage.LeaseObject{
		Data: storage.LeaseData{
			HolderIdentity:       "other-candidate",
			RenewTime:            time.Now().Add(-60 * time.Second),
			LeaseDurationSeconds: 30,
			LeaseTransitions:     5,
		},
	}
	err := store.CreateLease(expiredLease)
	require.NoError(t, err)

	info, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)
	assert.True(t, info.Acquired)
	assert.Equal(t, "candidate-1", *info.HolderIdentity)
	assert.Equal(t, int32(6), *info.LeaseTransitions)
}

func TestAcquireOrRenew_LeaseNotExpired(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now(), 30)

	// Create a valid lease held by someone else
	validLease := &storage.LeaseObject{
		Data: storage.LeaseData{
			HolderIdentity:       "other-candidate",
			RenewTime:            time.Now(),
			LeaseDurationSeconds: 30,
		},
	}
	err := store.CreateLease(validLease)
	require.NoError(t, err)

	info, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)
	assert.False(t, info.Acquired)
	assert.Equal(t, "other-candidate", *info.HolderIdentity)
}

func TestAcquireOrRenew_UpdateLeaseRace(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now(), 30)

	// Acquire the lease
	_, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)

	// Simulate another process updating the lease
	leaseInStore, err := store.ReadLease()
	require.NoError(t, err)
	leaseInStore.Data.HolderIdentity = "other-candidate"
	err = store.UpdateLease(leaseInStore)
	require.NoError(t, err)

	// Try to renew, should fail and return the new holder
	info, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	require.NoError(t, err)
	assert.False(t, info.Acquired)
	assert.Equal(t, "other-candidate", *info.HolderIdentity)
}

func TestAcquireOrRenew_StaleCandidate(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now().Add(-20*time.Second), 30) // Stale renewTime

	_, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "candidate lease is stale")
}

func TestAcquireOrRenew_BackendError(t *testing.T) {
	store := storage.NewInMemoryStorage()
	elector := leaderelection.NewLeaderElector(store)
	lease := newLease(time.Now(), 30)

	// Inject an error
	injectedErr := errors.New("backend failure")
	store.InjectError(injectedErr)

	_, err := elector.AcquireOrRenew(context.Background(), lease, "candidate-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), injectedErr.Error())
}
