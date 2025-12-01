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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gke-labs/multicluster-leader-election/api/v1alpha1"
	"github.com/gke-labs/multicluster-leader-election/pkg/storage"
	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	ctx        context.Context
	cancel     context.CancelFunc
	testLog    logr.Logger
	reconciler *MultiClusterLeaseReconciler
)

func TestMain(m *testing.M) {
	ctx, cancel = context.WithCancel(context.Background())
	testLog = zap.New(zap.WriteTo(writer{}), zap.UseDevMode(true))
	log.SetLogger(testLog)

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	if err != nil {
		testLog.Error(err, "failed to start test environment")
		panic(err)
	}

	err = v1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(err)
	}

	// Start the controller manager
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		panic(err)
	}

	reconciler = &MultiClusterLeaseReconciler{
		Client:         k8sManager.GetClient(),
		Log:            ctrl.Log.WithName("controllers").WithName("MultiClusterLease"),
		StorageFactory: storage.NewInMemoryStorageFactory(),
	}
	err = reconciler.SetupWithManager(k8sManager)
	if err != nil {
		panic(err)
	}

	go func() {
		err = k8sManager.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	// Run tests
	code := m.Run()

	// Teardown
	cancel()
	err = testEnv.Stop()
	if err != nil {
		testLog.Error(err, "failed to stop test environment")
	}

	// Exit
	if code != 0 {
		// Propagate failure
		fmt.Printf("Tests failed with exit code %d\n", code)
	}
}

// writer is a helper to redirect envtest logs to the testing framework
type writer struct{}

func (w writer) Write(p []byte) (n int, err error) {
	fmt.Print(string(p))
	return len(p), nil
}
