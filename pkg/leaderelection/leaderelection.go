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
	"errors"
	"fmt"
	"time"

	gstorage "cloud.google.com/go/storage"
	v1alpha1 "github.com/gke-labs/multicluster-leader-election/api/v1alpha1"
	"github.com/gke-labs/multicluster-leader-election/pkg/storage"
	ctrl "sigs.k8s.io/controller-runtime"
)

// LeaderElector handles the actual leader election logic using a pluggable storage backend.
type LeaderElector struct {
	storage storage.Storage
}

// NewLeaderElector creates a new LeaderElector instance.
func NewLeaderElector(storage storage.Storage) *LeaderElector {
	return &LeaderElector{
		storage: storage,
	}
}

// LeaseInfo contains the current state of a lease from the backend.
type LeaseInfo struct {
	// Acquired indicates whether this identity successfully acquired or renewed the lease.
	Acquired bool
	// HolderIdentity is the identity of the current lease holder (may be empty if no holder).
	HolderIdentity *string
	// RenewTime is when the lease was last renewed (may be nil if no lease exists).
	RenewTime *time.Time
	// LeaseTransitions is the number of times leadership has changed (may be nil if no lease exists).
	LeaseTransitions *int32
}

// AcquireOrRenew attempts to acquire or renew the lease. It is designed
// to be robust against race conditions.
func (le *LeaderElector) AcquireOrRenew(ctx context.Context, lease *v1alpha1.MultiClusterLease, identity string) (*LeaseInfo, error) {
	log := ctrl.Log.WithName("leaderelector").WithValues("candidate", identity)

	// Check if the candidate is alive by checking its renewTime.
	if lease.Spec.RenewTime == nil {
		log.Info("candidate has no renew time")
		return &LeaseInfo{Acquired: false}, fmt.Errorf("candidate has no renew time")
	}

	// TODO: Make the staleness check configurable
	if time.Since(lease.Spec.RenewTime.Time) > 15*time.Second {
		log.Info("candidate lease is stale")
		// Return an error immediately, as the local candidate is stale.
		// We don't need to read from the backend if the local state is invalid.
		return &LeaseInfo{Acquired: false}, fmt.Errorf("candidate lease is stale")
	}

	// 1. Try to read the existing lease from the storage backend.
	log.Info("reading lease")
	leaseObject, err := le.storage.ReadLease()
	if err != nil {
		if !errors.Is(err, gstorage.ErrObjectNotExist) {
			log.Error(err, "failed to read lease")
			return nil, fmt.Errorf("failed to read lease: %w", err)
		}

		// 2. The lease does not exist. Try to create it.
		log.Info("lease object does not exist, attempting to create")
		newLease := &storage.LeaseObject{
			Data: storage.LeaseData{
				HolderIdentity:       identity,
				RenewTime:            time.Now(),
				LeaseDurationSeconds: int(*lease.Spec.LeaseDurationSeconds),
				LeaseTransitions:     1,
			},
		}
		createErr := le.storage.CreateLease(newLease)
		if createErr == nil {
			// Success! We are the first and only leader.
			log.Info("successfully created new lease")
			return &LeaseInfo{
				Acquired:         true,
				HolderIdentity:   &newLease.Data.HolderIdentity,
				RenewTime:        &newLease.Data.RenewTime,
				LeaseTransitions: &newLease.Data.LeaseTransitions,
			}, nil
		}

		// We lost the creation race. The object now exists.
		log.Info("lost creation race, re-reading lease")
		leaseObject, err = le.storage.ReadLease()
		if err != nil {
			log.Error(err, "failed to read lease after losing creation race")
			return nil, fmt.Errorf("failed to read lease after losing creation race: %w", err)
		}
	}

	// 3. The lease exists. Check if we can acquire it.
	log.Info("successfully read lease", "holder", leaseObject.Data.HolderIdentity, "renewTime", leaseObject.Data.RenewTime)
	leaseDuration := time.Duration(leaseObject.Data.LeaseDurationSeconds) * time.Second
	leaseExpired := time.Since(leaseObject.Data.RenewTime) > leaseDuration
	log.Info("checking lease expiration", "isExpired", leaseExpired)

	if leaseObject.Data.HolderIdentity == identity || leaseExpired {
		// We are the holder or the lease is expired. Try to update.
		log.Info("attempting to acquire or renew lease")

		if leaseObject.Data.HolderIdentity != identity && leaseObject.Data.HolderIdentity != "" {
			leaseObject.Data.LeaseTransitions++
			log.Info("incrementing lease transitions", "newTransitions", leaseObject.Data.LeaseTransitions)
		}

		leaseObject.Data.HolderIdentity = identity
		leaseObject.Data.RenewTime = time.Now()
		leaseObject.Data.LeaseDurationSeconds = int(*lease.Spec.LeaseDurationSeconds)

		updateErr := le.storage.UpdateLease(leaseObject)
		if updateErr != nil {
			// We lost an update race. Re-read to get the latest state.
			log.Info("lost update race, re-reading lease")
			leaseObject, err = le.storage.ReadLease()
			if err != nil {
				log.Error(err, "failed to read lease after losing update race")
				return nil, fmt.Errorf("failed to read lease after losing update race: %w", err)
			}
			return &LeaseInfo{
				Acquired:         false,
				HolderIdentity:   &leaseObject.Data.HolderIdentity,
				RenewTime:        &leaseObject.Data.RenewTime,
				LeaseTransitions: &leaseObject.Data.LeaseTransitions,
			}, nil
		}

		// Successfully updated. We are the leader.
		log.Info("successfully acquired or renewed lease")
		// After a successful update, re-read the lease to get the authoritative state.
		leaseObject, err = le.storage.ReadLease()
		if err != nil {
			return nil, fmt.Errorf("failed to read lease after successful update: %w", err)
		}
		return &LeaseInfo{
			Acquired:         true,
			HolderIdentity:   &leaseObject.Data.HolderIdentity,
			RenewTime:        &leaseObject.Data.RenewTime,
			LeaseTransitions: &leaseObject.Data.LeaseTransitions,
		}, nil
	}

	// 4. The lease is held by someone else and is not expired.
	log.Info("lease is held by another identity and is not expired")
	return &LeaseInfo{
		Acquired:         false,
		HolderIdentity:   &leaseObject.Data.HolderIdentity,
		RenewTime:        &leaseObject.Data.RenewTime,
		LeaseTransitions: &leaseObject.Data.LeaseTransitions,
	}, nil
}
