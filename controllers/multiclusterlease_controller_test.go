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
	"errors"
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
	mclstorage "github.com/gke-labs/multicluster-leader-election/pkg/storage"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
)

// TestReconcile_PanicOnGCSAuthError specifically tests the panic condition.
// It sets up a reconciler with a GCS client that is guaranteed to fail authentication.
// With the buggy code, this test will pass because it expects a panic.
// After the fix is applied, this test will FAIL because the panic is gone.
// We will then update the test to assert that no panic occurs.
func TestReconcile_PanicOnGCSAuthError(t *testing.T) {
	// Arrange: Set up a scenario that will cause a panic
	ctx := context.Background()
	log := ctrl.Log.WithName("test")

	// Create a fake k8s client and add our scheme
	scheme := BuildScheme()
	fakeKubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create a GCS client that will always fail auth
	gcsClient, err := storage.NewClient(ctx, option.WithoutAuthentication())
	require.NoError(t, err)

	gcsStorageFactory := mclstorage.NewGCSStorageFactory(gcsClient, "non-existent-bucket")

	// Create the reconciler with the failing GCS client
	reconciler := NewMultiClusterLeaseReconciler(
		fakeKubeClient,
		log,
		gcsStorageFactory,
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

	_, _ = reconciler.Reconcile(ctx, req)
}

func int32Ptr(i int32) *int32 {
	return &i
}

func TestReconcile_AcquireLease(t *testing.T) {
	leaseName := "test-lease"
	nsName := "test-ns-acquire"
	identity := "candidate-1"
	duration := int32(30)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	err := k8sClient.Create(context.Background(), ns)
	require.NoError(t, err)
	defer func() {
		err := k8sClient.Delete(context.Background(), ns)
		assert.NoError(t, err)
	}()

	lease := &v1alpha1.MultiClusterLease{
		ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: nsName},
		Spec: v1alpha1.MultiClusterLeaseSpec{
			HolderIdentity:       &identity,
			LeaseDurationSeconds: &duration,
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	err = k8sClient.Create(context.Background(), lease)
	require.NoError(t, err)

	lookupKey := types.NamespacedName{Name: leaseName, Namespace: nsName}
	var updatedLease v1alpha1.MultiClusterLease

	// Use require.Eventually to wait for the status to be updated.
	require.Eventually(t, func() bool {
		err := k8sClient.Get(context.Background(), lookupKey, &updatedLease)
		if err != nil {
			return false
		}
		return updatedLease.Status.GlobalHolderIdentity != nil
	}, 10*time.Second, 250*time.Millisecond)

	assert.Equal(t, identity, *updatedLease.Status.GlobalHolderIdentity)
	healthyCond := meta.FindStatusCondition(updatedLease.Status.Conditions, string(v1alpha1.ConditionTypeBackendHealthy))
	require.NotNil(t, healthyCond)
	assert.Equal(t, metav1.ConditionTrue, healthyCond.Status)
}

func TestReconcile_LeaseHeldByOther(t *testing.T) {
	leaseName := "test-lease-held"
	nsName := "test-ns-held"
	identity := "candidate-1"
	otherIdentity := "other-candidate"
	duration := int32(30)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	err := k8sClient.Create(context.Background(), ns)
	require.NoError(t, err)
	defer func() {
		err := k8sClient.Delete(context.Background(), ns)
		assert.NoError(t, err)
	}()

	lookupKey := types.NamespacedName{Name: leaseName, Namespace: nsName}

	// Pre-populate the storage backend using the shared factory
	factory := reconciler.StorageFactory.(*mclstorage.InMemoryStorageFactory)
	mockStorage := factory.GetStorage(lookupKey.String())
	err = mockStorage.CreateLease(&mclstorage.LeaseObject{
		Data: mclstorage.LeaseData{
			HolderIdentity:       otherIdentity,
			RenewTime:            time.Now(),
			LeaseDurationSeconds: int(duration),
		},
	})
	require.NoError(t, err)

	lease := &v1alpha1.MultiClusterLease{
		ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: nsName},
		Spec: v1alpha1.MultiClusterLeaseSpec{
			HolderIdentity:       &identity,
			LeaseDurationSeconds: &duration,
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	err = k8sClient.Create(context.Background(), lease)
	require.NoError(t, err)

	var updatedLease v1alpha1.MultiClusterLease

	require.Eventually(t, func() bool {
		err := k8sClient.Get(context.Background(), lookupKey, &updatedLease)
		if err != nil {
			return false
		}
		return updatedLease.Status.GlobalHolderIdentity != nil && *updatedLease.Status.GlobalHolderIdentity == otherIdentity
	}, 10*time.Second, 250*time.Millisecond)
}

func TestReconcile_BackendError(t *testing.T) {
	leaseName := "test-lease-backend-error"
	nsName := "test-ns-backend-error"
	identity := "candidate-1"
	duration := int32(30)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	err := k8sClient.Create(context.Background(), ns)
	require.NoError(t, err)
	defer func() {
		err := k8sClient.Delete(context.Background(), ns)
		assert.NoError(t, err)
	}()

	lookupKey := types.NamespacedName{Name: leaseName, Namespace: nsName}

	// Inject an error into the mock storage using the shared factory
	factory := reconciler.StorageFactory.(*mclstorage.InMemoryStorageFactory)
	mockStorage := factory.GetStorage(lookupKey.String())
	mockStorage.InjectError(errors.New("permanent backend failure"))

	lease := &v1alpha1.MultiClusterLease{
		ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: nsName},
		Spec: v1alpha1.MultiClusterLeaseSpec{
			HolderIdentity:       &identity,
			LeaseDurationSeconds: &duration,
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	err = k8sClient.Create(context.Background(), lease)
	require.NoError(t, err)

	var updatedLease v1alpha1.MultiClusterLease

	require.Eventually(t, func() bool {
		err := k8sClient.Get(context.Background(), lookupKey, &updatedLease)
		if err != nil {
			return false
		}
		cond := meta.FindStatusCondition(updatedLease.Status.Conditions, string(v1alpha1.ConditionTypeBackendHealthy))
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 10*time.Second, 250*time.Millisecond)
}

func TestReconcile_ObjectDeletion(t *testing.T) {
	leaseName := "test-lease-deletion"
	nsName := "test-ns-deletion"
	identity := "candidate-1"
	duration := int32(30)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	err := k8sClient.Create(context.Background(), ns)
	require.NoError(t, err)
	defer func() {
		// Namespace will be deleted by the test itself
	}()

	lease := &v1alpha1.MultiClusterLease{
		ObjectMeta: metav1.ObjectMeta{
			Name:       leaseName,
			Namespace:  nsName,
			Finalizers: []string{"multiclusterlease.core.cnrm.cloud.google.com/finalizer"},
		},
		Spec: v1alpha1.MultiClusterLeaseSpec{
			HolderIdentity:       &identity,
			LeaseDurationSeconds: &duration,
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	err = k8sClient.Create(context.Background(), lease)
	require.NoError(t, err)

	lookupKey := types.NamespacedName{Name: leaseName, Namespace: nsName}

	// Wait for the reconciler to add the finalizer and set initial status
	require.Eventually(t, func() bool {
		var fetchedLease v1alpha1.MultiClusterLease
		err := k8sClient.Get(context.Background(), lookupKey, &fetchedLease)
		if err != nil {
			return false
		}
		return len(fetchedLease.ObjectMeta.Finalizers) > 0 && meta.IsStatusConditionTrue(fetchedLease.Status.Conditions, string(v1alpha1.ConditionTypeBackendHealthy))
	}, 10*time.Second, 250*time.Millisecond)

	// Delete the lease to trigger finalizer logic
	err = k8sClient.Delete(context.Background(), lease)
	require.NoError(t, err)

	lookupKey = types.NamespacedName{Name: leaseName, Namespace: nsName}
	// Eventually, the finalizer should be removed
	require.Eventually(t, func() bool {
		var fetchedLease v1alpha1.MultiClusterLease
		err := k8sClient.Get(context.Background(), lookupKey, &fetchedLease)
		if apierrors.IsNotFound(err) {
			return true // Object is gone, so finalizer is implicitly removed
		}
		if err != nil {
			return false
		}
		return len(fetchedLease.ObjectMeta.Finalizers) == 0
	}, 10*time.Second, 250*time.Millisecond)
}

func TestReconcile_HolderIdentityNotSet(t *testing.T) {
	leaseName := "test-lease-no-holder"
	nsName := "test-ns-no-holder"
	duration := int32(30)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	err := k8sClient.Create(context.Background(), ns)
	require.NoError(t, err)
	defer func() {
		err := k8sClient.Delete(context.Background(), ns)
		assert.NoError(t, err)
	}()

	lease := &v1alpha1.MultiClusterLease{
		ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: nsName},
		Spec: v1alpha1.MultiClusterLeaseSpec{
			LeaseDurationSeconds: &duration,
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	err = k8sClient.Create(context.Background(), lease)
	require.NoError(t, err)

	// Give the reconciler a moment to NOT act
	time.Sleep(2 * time.Second)

	lookupKey := types.NamespacedName{Name: leaseName, Namespace: nsName}
	var updatedLease v1alpha1.MultiClusterLease
	err = k8sClient.Get(context.Background(), lookupKey, &updatedLease)
	require.NoError(t, err)

	assert.Nil(t, updatedLease.Status.GlobalHolderIdentity)
	assert.Empty(t, updatedLease.Status.Conditions)
}
