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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

// GCSStorage implements the Storage interface using Google Cloud Storage.
type GCSStorage struct {
	client     *storage.Client
	bucketName string
	objectName string
}

// NewGCSStorage creates a new GCSStorage instance.
func NewGCSStorage(client *storage.Client, bucketName, objectName string) *GCSStorage {
	return &GCSStorage{
		client:     client,
		bucketName: bucketName,
		objectName: fmt.Sprintf("leases/%s", objectName),
	}
}

// GCSStorageFactory creates GCSStorage instances.
type GCSStorageFactory struct {
	Client     *storage.Client
	BucketName string
}

// NewGCSStorageFactory creates a new GCSStorageFactory.
func NewGCSStorageFactory(client *storage.Client, bucketName string) *GCSStorageFactory {
	return &GCSStorageFactory{
		Client:     client,
		BucketName: bucketName,
	}
}

// NewStorage creates a new Storage backend for a specific lease object.
func (f *GCSStorageFactory) NewStorage(leaseObjectName string) Storage {
	return NewGCSStorage(f.Client, f.BucketName, leaseObjectName)
}

// ReadLease reads the current lease object from GCS.
func (g *GCSStorage) ReadLease() (*LeaseObject, error) {
	obj := g.getObjectHandle()
	attrs, err := obj.Attrs(context.Background())
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("getting object attrs: %w", err)
	}

	leaseObject := &LeaseObject{
		ResourceVersion: strconv.FormatInt(attrs.Generation, 10),
	}

	// Read the object content
	reader, err := obj.NewReader(context.Background())
	if err != nil {
		return nil, fmt.Errorf("creating object reader: %w", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading lease object: %w", err)
	}

	// If the object is empty, return default values
	if len(content) == 0 {
		return leaseObject, nil
	}

	// Unmarshal the JSON data
	if err := json.Unmarshal(content, &leaseObject.Data); err != nil {
		return nil, fmt.Errorf("unmarshalling lease data: %w", err)
	}

	return leaseObject, nil
}

// UpdateLease atomically updates the lease object in GCS.
func (g *GCSStorage) UpdateLease(lease *LeaseObject) error {
	generation, err := strconv.ParseInt(lease.ResourceVersion, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid resource version: %w", err)
	}

	obj := g.getObjectHandle()

	// Marshal the lease data to JSON
	content, err := json.Marshal(lease.Data)
	if err != nil {
		return fmt.Errorf("marshalling lease data: %w", err)
	}

	// Create a conditional writer
	w := obj.If(storage.Conditions{GenerationMatch: generation}).NewWriter(context.Background())
	w.ContentType = "application/json"
	w.CacheControl = "no-cache, no-store, must-revalidate"

	// Write the JSON data
	if _, err := w.Write(content); err != nil {
		w.Close()
		if isGCSPreconditionError(err) {
			return fmt.Errorf("precondition failed while writing lease data: %w", err)
		}
		return fmt.Errorf("writing lease data: %w", err)
	}

	// Close the writer to complete the operation
	if err := w.Close(); err != nil {
		if isGCSPreconditionError(err) {
			return fmt.Errorf("precondition failed while closing writer: %w", err)
		}
		return fmt.Errorf("closing writer: %w", err)
	}

	return nil
}

// CreateLease creates the lease object in GCS for the first time.
func (g *GCSStorage) CreateLease(lease *LeaseObject) error {
	obj := g.getObjectHandle()

	// Marshal the lease data to JSON
	content, err := json.Marshal(lease.Data)
	if err != nil {
		return fmt.Errorf("marshalling new lease data: %w", err)
	}

	// Use a precondition that the object doesn't exist
	w := obj.If(storage.Conditions{DoesNotExist: true}).NewWriter(context.Background())
	w.ContentType = "application/json"
	w.CacheControl = "no-cache, no-store, must-revalidate"

	// Write the JSON data
	if _, err := w.Write(content); err != nil {
		w.Close() // Close the writer even on error
		return err
	}

	// Close the writer to complete the operation
	if err := w.Close(); err != nil {
		return err
	}

	return nil
}

// DeleteLease removes the lease object from GCS.
func (g *GCSStorage) DeleteLease(lease *LeaseObject) error {
	generation, err := strconv.ParseInt(lease.ResourceVersion, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid resource version: %w", err)
	}
	obj := g.getObjectHandle()
	return obj.If(storage.Conditions{GenerationMatch: generation}).Delete(context.Background())
}

// getObjectHandle returns a handle to the GCS object for this lease.
func (g *GCSStorage) getObjectHandle() *storage.ObjectHandle {
	bucket := g.client.Bucket(g.bucketName)
	return bucket.Object(g.objectName)
}

// isGCSPreconditionError checks if an error is a GCS precondition failure.
func isGCSPreconditionError(err error) bool {
	var gErr *googleapi.Error
	if errors.As(err, &gErr) {
		return gErr.Code == http.StatusPreconditionFailed || gErr.Code == http.StatusConflict
	}
	return false
}
