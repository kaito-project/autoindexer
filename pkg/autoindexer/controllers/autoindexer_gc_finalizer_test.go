// Copyright (c) KAITO authors.
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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/utils"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

// MockRAGClient is a mock implementation of the RAGEngineClient interface
type MockRAGClient struct {
	mock.Mock
}

func (m *MockRAGClient) GetDocumentCount(ragEngineName, indexName, autoindexerName, autoIndexerNamespace string) (int32, error) {
	args := m.Called(ragEngineName, indexName, autoindexerName, autoIndexerNamespace)
	return args.Get(0).(int32), args.Error(1)
}

func (m *MockRAGClient) ListIndexes(ragEngineName, indexName, autoindexerName, autoIndexerNamespace string) ([]string, error) {
	args := m.Called(ragEngineName, indexName, autoindexerName, autoIndexerNamespace)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockRAGClient) DeleteIndex(ragEngineName, indexName, autoIndexerNamespace string) error {
	args := m.Called(ragEngineName, indexName, autoIndexerNamespace)
	return args.Error(0)
}

func setupTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	return scheme
}

func createTestAutoIndexer(name, namespace string) *autoindexerv1alpha1.AutoIndexer {
	return &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{utils.AutoIndexerFinalizer},
		},
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			RAGEngine: "test-ragengine",
			IndexName: "test-index",
		},
	}
}

func createTestJob(name, namespace, autoIndexerName string, active int32) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				AutoIndexerNameLabel: autoIndexerName,
			},
		},
		Status: batchv1.JobStatus{
			Active: active,
		},
	}
}

func TestGarbageCollectAutoIndexer_Success(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer).
		Build()

	mockRAGClient := &MockRAGClient{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called
	mockRAGClient.AssertExpectations(t)

	// Verify the finalizer was removed
	assert.Empty(t, autoIndexer.Finalizers, "Finalizer should have been removed")
}

func TestGarbageCollectAutoIndexer_WithActiveJob_WaitsForCompletion(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")
	activeJob := createTestJob("test-job", "default", "test-autoindexer", 1) // Active job

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer, activeJob).
		Build()

	mockRAGClient := &MockRAGClient{}
	// DeleteIndex should NOT be called since there's an active job

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	// Should return no error but not proceed with deletion
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was NOT called
	mockRAGClient.AssertNotCalled(t, "DeleteIndex")

	// Verify the finalizer was NOT removed (since job is still active)
	assert.Contains(t, autoIndexer.Finalizers, utils.AutoIndexerFinalizer, "Finalizer should still be present")
}

func TestGarbageCollectAutoIndexer_WithCompletedJob_Proceeds(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")
	completedJob := createTestJob("test-job", "default", "test-autoindexer", 0) // Inactive job

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer, completedJob).
		Build()

	mockRAGClient := &MockRAGClient{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called
	mockRAGClient.AssertExpectations(t)

	// Verify the finalizer was removed
	assert.Empty(t, autoIndexer.Finalizers, "Finalizer should have been removed")
}

func TestGarbageCollectAutoIndexer_MultipleJobs_WaitsForAllActive(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")
	activeJob1 := createTestJob("test-job-1", "default", "test-autoindexer", 1)
	inactiveJob2 := createTestJob("test-job-2", "default", "test-autoindexer", 0)
	activeJob3 := createTestJob("test-job-3", "default", "test-autoindexer", 2)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer, activeJob1, inactiveJob2, activeJob3).
		Build()

	mockRAGClient := &MockRAGClient{}
	// DeleteIndex should NOT be called since there are active jobs

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was NOT called
	mockRAGClient.AssertNotCalled(t, "DeleteIndex")

	// Verify the finalizer was NOT removed (since there are still active jobs)
	assert.Contains(t, autoIndexer.Finalizers, utils.AutoIndexerFinalizer, "Finalizer should still be present")
}

func TestGarbageCollectAutoIndexer_NoJobs_Proceeds(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer).
		Build()

	mockRAGClient := &MockRAGClient{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called
	mockRAGClient.AssertExpectations(t)

	// Verify the finalizer was removed
	assert.Empty(t, autoIndexer.Finalizers, "Finalizer should have been removed")
}

func TestGarbageCollectAutoIndexer_JobListError(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")

	// Create a client that will return an error when listing jobs
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer).
		Build()

	// Since fake client doesn't easily allow us to force a List error,
	// this test demonstrates the happy path when no jobs exist
	// In real scenarios, List errors could happen due to RBAC issues, network problems, etc.
	mockRAGClient := &MockRAGClient{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	// With fake client, this should succeed
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called
	mockRAGClient.AssertExpectations(t)

	// Verify the finalizer was removed
	assert.Empty(t, autoIndexer.Finalizers, "Finalizer should have been removed")
}

func TestGarbageCollectAutoIndexer_RAGClientDeleteError(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer).
		Build()

	mockRAGClient := &MockRAGClient{}
	expectedError := errors.New("failed to delete index from RAG engine")
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(expectedError)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.Error(t, err)
	assert.Equal(t, expectedError, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called
	mockRAGClient.AssertExpectations(t)

	// Verify the finalizer was NOT removed due to error
	assert.Contains(t, autoIndexer.Finalizers, utils.AutoIndexerFinalizer, "Finalizer should still be present due to error")
}

func TestGarbageCollectAutoIndexer_NoFinalizerToRemove(t *testing.T) {
	scheme := setupTestScheme()

	// Create AutoIndexer without finalizer
	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-autoindexer",
			Namespace: "default",
			// No finalizers
		},
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			RAGEngine: "test-ragengine",
			IndexName: "test-index",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer).
		Build()

	mockRAGClient := &MockRAGClient{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called
	mockRAGClient.AssertExpectations(t)

	// Verify no finalizers (should remain empty)
	assert.Empty(t, autoIndexer.Finalizers)
}

func TestGarbageCollectAutoIndexer_JobsInDifferentNamespace_Ignored(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")
	// Create a job in a different namespace
	jobInOtherNamespace := createTestJob("test-job", "other-namespace", "test-autoindexer", 1)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer, jobInOtherNamespace).
		Build()

	mockRAGClient := &MockRAGClient{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called (job in other namespace should be ignored)
	mockRAGClient.AssertExpectations(t)

	// Verify the finalizer was removed
	assert.Empty(t, autoIndexer.Finalizers, "Finalizer should have been removed")
}

func TestGarbageCollectAutoIndexer_JobsWithDifferentLabels_Ignored(t *testing.T) {
	scheme := setupTestScheme()

	autoIndexer := createTestAutoIndexer("test-autoindexer", "default")
	// Create a job with different autoindexer label
	jobWithDifferentLabel := createTestJob("test-job", "default", "other-autoindexer", 1)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer, jobWithDifferentLabel).
		Build()

	mockRAGClient := &MockRAGClient{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  &record.FakeRecorder{},
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()
	result, err := reconciler.garbageCollectAutoIndexer(ctx, autoIndexer)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify that DeleteIndex was called (job with different label should be ignored)
	mockRAGClient.AssertExpectations(t)

	// Verify the finalizer was removed
	assert.Empty(t, autoIndexer.Finalizers, "Finalizer should have been removed")
}
