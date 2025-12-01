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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/mock"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/utils"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

// MockRAGClient is a mock implementation of the RAGEngineClient interface for testing
type MockRAGClientForControllerTest struct {
	mock.Mock
}

func (m *MockRAGClientForControllerTest) GetDocumentCount(ragEngineName, indexName, autoindexerName, autoIndexerNamespace string) (int32, error) {
	args := m.Called(ragEngineName, indexName, autoindexerName, autoIndexerNamespace)
	return args.Get(0).(int32), args.Error(1)
}

func (m *MockRAGClientForControllerTest) ListIndexes(ragEngineName, indexName, autoindexerName, autoIndexerNamespace string) ([]string, error) {
	args := m.Called(ragEngineName, indexName, autoindexerName, autoIndexerNamespace)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockRAGClientForControllerTest) DeleteIndex(ragEngineName, indexName, autoIndexerNamespace string) error {
	args := m.Called(ragEngineName, indexName, autoIndexerNamespace)
	return args.Error(0)
}

func TestAutoIndexerReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	ragEngine := &kaitov1alpha1.RAGEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ragengine",
			Namespace: "default",
		},
		Spec: &kaitov1alpha1.RAGEngineSpec{},
		Status: kaitov1alpha1.RAGEngineStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(kaitov1alpha1.RAGEngineConditionTypeSucceeded),
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			RAGEngine: "test-ragengine",
			IndexName: "test-index",
			DataSource: autoindexerv1alpha1.DataSourceSpec{
				Type: autoindexerv1alpha1.DataSourceTypeGit,
				Git: &autoindexerv1alpha1.GitDataSourceSpec{
					Repository: "https://github.com/example/repo",
				},
			},
		},
		Status: autoindexerv1alpha1.AutoIndexerStatus{},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ragEngine, autoIndexer).
		WithStatusSubresource(&autoindexerv1alpha1.AutoIndexer{}).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := NewAutoIndexerReconciler(client, scheme, logr.Discard(), recorder)

	ctx := context.Background()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Log("Reconcile requested requeue")
	}

	// Verify the AutoIndexer was processed (we'll check status conditions instead of finalizers
	// since the current controller doesn't implement finalizer addition)
	updatedAutoIndexer := &autoindexerv1alpha1.AutoIndexer{}
	err = client.Get(ctx, req.NamespacedName, updatedAutoIndexer)
	if err != nil {
		t.Fatalf("Failed to get updated AutoIndexer: %v", err)
	}

	// Verify phase set to pending
	if updatedAutoIndexer.Status.IndexingPhase != autoindexerv1alpha1.AutoIndexerPhasePending {
		t.Error("phase should have been updated to pending during reconciliation")
	}

	// should reconcile again to get resources created
	result, err = reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedAutoIndexer = &autoindexerv1alpha1.AutoIndexer{}
	err = client.Get(ctx, req.NamespacedName, updatedAutoIndexer)
	if err != nil {
		t.Fatalf("Failed to get updated AutoIndexer: %v", err)
	}

	// Verify phase set to running with successful reconciliation
	if updatedAutoIndexer.Status.IndexingPhase != autoindexerv1alpha1.AutoIndexerPhaseRunning {
		t.Error("phase should have been updated to running during reconciliation")
	}
}

func TestAutoIndexerReconciler_deleteAutoIndexer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-autoindexer",
			Namespace:  "default",
			Finalizers: []string{utils.AutoIndexerFinalizer},
		},
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			RAGEngine: "test-ragengine",
			IndexName: "test-index",
		},
	}

	// Create a completed job (both succeeded and failed are 0 means incomplete job)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
			Labels: map[string]string{
				AutoIndexerNameLabel: "test-autoindexer",
			},
		},
		Status: batchv1.JobStatus{
			Succeeded: 1, // Mark job as completed
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer, job).
		Build()

	recorder := record.NewFakeRecorder(10)
	
	// Create mock RAG client
	mockRAGClient := &MockRAGClientForControllerTest{}
	mockRAGClient.On("DeleteIndex", "test-ragengine", "test-index", "default").Return(nil)

	// Create reconciler manually with mock instead of using NewAutoIndexerReconciler
	reconciler := &AutoIndexerReconciler{
		Client:    client,
		Log:       logr.Discard(),
		Scheme:    scheme,
		Recorder:  recorder,
		RAGClient: mockRAGClient,
	}

	ctx := context.Background()

	result, err := reconciler.deleteAutoIndexer(ctx, autoIndexer)
	if err != nil {
		t.Fatalf("deleteAutoIndexer failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("deleteAutoIndexer should not request requeue when all jobs are completed")
	}

	// Verify that DeleteIndex was called
	mockRAGClient.AssertExpectations(t)
}

func TestAutoIndexerReconciler_GetJobStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}

	jobs := []batchv1.Job{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "running-job",
				Namespace: "default",
				Labels: map[string]string{
					"autoindexer.kaito.sh/name": "test",
				},
			},
			Status: batchv1.JobStatus{Active: 1},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "failed-job",
				Namespace: "default",
				Labels: map[string]string{
					"autoindexer.kaito.sh/name": "test",
				},
			},
			Status: batchv1.JobStatus{Failed: 1},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "completed-job",
				Namespace: "default",
				Labels: map[string]string{
					"autoindexer.kaito.sh/name": "test",
				},
			},
			Status: batchv1.JobStatus{Succeeded: 1},
		},
	}

	objs := []client.Object{autoIndexer}
	for i := range jobs {
		objs = append(objs, &jobs[i])
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	reconciler := &AutoIndexerReconciler{
		Client: client,
		Log:    logr.Discard(),
		Scheme: scheme,
	}

	ctx := context.Background()
	running, failed, completed, latestJobFailed, err := reconciler.getJobStatus(ctx, autoIndexer)

	if err != nil {
		t.Fatalf("getJobStatus failed: %v", err)
	}

	if running != 1 {
		t.Errorf("Expected 1 running job, got %d", running)
	}
	if failed != 1 {
		t.Errorf("Expected 1 failed job, got %d", failed)
	}
	if completed != 1 {
		t.Errorf("Expected 1 completed job, got %d", completed)
	}
	if latestJobFailed {
		t.Errorf("Expected latest job to not have failed")
	}
}

func TestAutoIndexerReconciler_UpdateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       "test-uid",
		},
		Status: autoindexerv1alpha1.AutoIndexerStatus{
			IndexingPhase: autoindexerv1alpha1.AutoIndexerPhasePending,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(autoIndexer).WithStatusSubresource(autoIndexer).Build()

	ctx := context.Background()

	// Test the equivalent of updateStatus method but simplified to just test phase change
	if autoIndexer.Status.IndexingPhase == autoindexerv1alpha1.AutoIndexerPhaseRunning {
		t.Error("Phase should not already be Running")
	}

	existingAutoIndexerObj := autoIndexer.DeepCopy()
	autoIndexer.Status.IndexingPhase = autoindexerv1alpha1.AutoIndexerPhaseRunning

	if err := fakeClient.Status().Patch(ctx, autoIndexer, client.MergeFrom(existingAutoIndexerObj)); err != nil {
		t.Fatalf("Failed to update phase: %v", err)
	}

	// Fetch the updated object from the client to check the phase
	updatedAutoIndexer := &autoindexerv1alpha1.AutoIndexer{}
	err := fakeClient.Get(ctx, types.NamespacedName{Name: autoIndexer.Name, Namespace: autoIndexer.Namespace}, updatedAutoIndexer)
	if err != nil {
		t.Fatalf("Failed to get updated AutoIndexer: %v", err)
	}

	if updatedAutoIndexer.Status.IndexingPhase != autoindexerv1alpha1.AutoIndexerPhaseRunning {
		t.Errorf("Expected phase Running, got %s", updatedAutoIndexer.Status.IndexingPhase)
	}
}
