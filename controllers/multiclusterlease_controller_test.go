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

package controllers

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/gke-labs/multicluster-leader-election/api/v1alpha1"
)

// TestReconcile_PanicOnGCSAuthError specifically tests the panic condition.
// It sets up a reconciler with a GCS client that is guaranteed to fail authentication.
// With the buggy code, this test will pass because it expects a panic.
// After the fix is applied, this test will FAIL because the panic is gone.
// We will then update the test to assert that no panic occurs.
func TestReconcile_PanicOnGCSAuthError(t *testing.T) {
	// TDD Step 1: Write a test that fails (by panicking)
	// We expect a panic, so we recover from it to make the test pass.
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic, but we expected it to. The bug might be fixed. Please update this test to assert for no panic.")
		} else {
			t.Log("Successfully caught expected panic.")
		}
	}()

	// Arrange: Set up a scenario that will cause a panic
	ctx := context.Background()
	log := ctrl.Log.WithName("test")

	// Create a fake k8s client and add our scheme
	scheme := BuildScheme()
	fakeKubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create a GCS client that will always fail auth
	gcsClient, err := storage.NewClient(ctx, option.WithoutAuthentication())
	require.NoError(t, err)

	// Create the reconciler with the failing GCS client
	reconciler := NewMultiClusterLeaseReconciler(
		fakeKubeClient,
		log,
		gcsClient,
		"non-existent-bucket",
	)

	// Create a sample MultiClusterLease object for the reconciler to process
	leaseName := "test-lease"
	leaseNamespace := "test-ns"
	holderID := "test-holder"
	now := metav1.MicroTime{Time: time.Now()}

	mcl := &v1alpha1.MultiClusterLease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: leaseNamespace,
		},
		Spec: v1alpha1.MultiClusterLeaseSpec{
			HolderIdentity:       &holderID,
			RenewTime:            &now,
			LeaseDurationSeconds: int32Ptr(15),
		},
	}
	require.NoError(t, fakeKubeClient.Create(ctx, mcl))

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      leaseName,
			Namespace: leaseNamespace,
		},
	}

	// Act: Call the Reconcile method. This should panic with the buggy code.
	_, _ = reconciler.Reconcile(ctx, req)
}

func int32Ptr(i int32) *int32 {
	return &i
}
