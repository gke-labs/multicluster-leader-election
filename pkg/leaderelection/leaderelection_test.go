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

package leaderelection

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/gke-labs/multicluster-leader-election/api/v1alpha1"
	"github.com/gke-labs/multicluster-leader-election/pkg/storage"
)

func TestAcquireOrRenew(t *testing.T) {
	ctx := context.Background()
	s := storage.NewFakeStorage()
	leaseKey := "test-lease"
	le := NewLeaderElector(s, leaseKey)

	identity := "pod-1"
	now := time.Now()
	mcl := &v1alpha1.MultiClusterLease{
		Spec: v1alpha1.MultiClusterLeaseSpec{
			HolderIdentity:       &identity,
			RenewTime:            &metav1.MicroTime{Time: now},
			LeaseDurationSeconds: int32Ptr(15),
		},
	}

	// 1. Initial acquisition
	info, err := le.AcquireOrRenew(ctx, mcl, identity)
	require.NoError(t, err)
	require.True(t, info.Acquired)
	require.Equal(t, identity, *info.HolderIdentity)
	require.Equal(t, int32(1), *info.LeaseTransitions)

	// 2. Renewal
	info, err = le.AcquireOrRenew(ctx, mcl, identity)
	require.NoError(t, err)
	require.True(t, info.Acquired)
	require.Equal(t, identity, *info.HolderIdentity)
	require.Equal(t, int32(1), *info.LeaseTransitions)

	// 3. Different identity tries to acquire (fails because not expired)
	otherIdentity := "pod-2"
	mclOther := &v1alpha1.MultiClusterLease{
		Spec: v1alpha1.MultiClusterLeaseSpec{
			HolderIdentity:       &otherIdentity,
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
			LeaseDurationSeconds: int32Ptr(15),
		},
	}
	info, err = le.AcquireOrRenew(ctx, mclOther, otherIdentity)
	require.NoError(t, err)
	require.False(t, info.Acquired)
	require.Equal(t, identity, *info.HolderIdentity)

	// 4. Wait for expiration and failover
	// Manually expire the lease in storage
	obj, err := s.ReadLease(ctx, leaseKey)
	require.NoError(t, err)
	obj.Data.RenewTime = time.Now().Add(-20 * time.Second)
	_, err = s.UpdateLease(ctx, leaseKey, obj)
	require.NoError(t, err)

	info, err = le.AcquireOrRenew(ctx, mclOther, otherIdentity)
	require.NoError(t, err)
	require.True(t, info.Acquired)
	require.Equal(t, otherIdentity, *info.HolderIdentity)
	require.Equal(t, int32(2), *info.LeaseTransitions)
}

func int32Ptr(i int32) *int32 {
	return &i
}
