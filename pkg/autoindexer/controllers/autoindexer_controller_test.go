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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

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
				Type: autoindexerv1alpha1.DataSourceTypeGitHub,
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

	// Verify at least one condition was set during reconciliation
	if len(updatedAutoIndexer.Status.Conditions) == 0 {
		t.Error("Expected status conditions to be set during reconciliation")
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
			Name:      "test-autoindexer",
			Namespace: "default",
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
	reconciler := NewAutoIndexerReconciler(client, scheme, logr.Discard(), recorder)

	ctx := context.Background()

	result, err := reconciler.deleteAutoIndexer(ctx, autoIndexer)
	if err != nil {
		t.Fatalf("deleteAutoIndexer failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("deleteAutoIndexer should not request requeue when all jobs are completed")
	}
}

func TestAutoIndexerReconciler_updateStatusConditionIfNotMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
		Status: autoindexerv1alpha1.AutoIndexerStatus{
			Conditions: []metav1.Condition{},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer).
		WithStatusSubresource(&autoindexerv1alpha1.AutoIndexer{}).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := NewAutoIndexerReconciler(client, scheme, logr.Discard(), recorder)

	ctx := context.Background()

	// Test adding a new condition
	err := reconciler.updateStatusConditionIfNotMatch(ctx, autoIndexer, autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded, metav1.ConditionTrue, "TestReason", "Test message")
	if err != nil {
		t.Fatalf("updateStatusConditionIfNotMatch failed: %v", err)
	}

	// Verify condition was added
	if len(autoIndexer.Status.Conditions) != 1 {
		t.Fatalf("Expected 1 condition, got %d", len(autoIndexer.Status.Conditions))
	}

	condition := autoIndexer.Status.Conditions[0]
	if condition.Type != string(autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded) {
		t.Errorf("Expected condition type %s, got %s", autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded, condition.Type)
	}
	if condition.Status != metav1.ConditionTrue {
		t.Errorf("Expected condition status True, got %s", condition.Status)
	}
	if condition.Reason != "TestReason" {
		t.Errorf("Expected condition reason TestReason, got %s", condition.Reason)
	}
	if condition.Message != "Test message" {
		t.Errorf("Expected condition message 'Test message', got %s", condition.Message)
	}
}

func TestAutoIndexerReconciler_HandleDriftRemediationJobCompletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	t.Run("unsuspend_after_drift_remediation_completion", func(t *testing.T) {
		schedule := "0 0 * * *"
		suspend := true

		// Create AutoIndexer that was suspended for drift remediation
		autoIndexer := &autoindexerv1alpha1.AutoIndexer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer",
				Namespace: "default",
				Annotations: map[string]string{
					"autoindexer.kaito.sh/drift-remediation-suspended": "true",
					"autoindexer.kaito.sh/original-suspend-state":      "false",
				},
			},
			Spec: autoindexerv1alpha1.AutoIndexerSpec{
				RAGEngine: "test-rag",
				IndexName: "test-index",
				Schedule:  &schedule,
				Suspend:   &suspend, // Currently suspended
				DataSource: autoindexerv1alpha1.DataSourceSpec{
					Type: autoindexerv1alpha1.DataSourceTypeGitHub,
					Git: &autoindexerv1alpha1.GitDataSourceSpec{
						Repository: "test/repo",
						Branch:     "main",
					},
				},
			},
		}

		// Create a completed drift remediation job
		completedJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer-drift-remediation-123",
				Namespace: "default",
				Labels: map[string]string{
					"autoindexer.kaito.sh/name":              "test-autoindexer",
					"autoindexer.kaito.sh/drift-remediation": "true",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(autoIndexer, autoindexerv1alpha1.GroupVersion.WithKind("AutoIndexer")),
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1, // Job completed successfully
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(autoIndexer, completedJob).Build()
		reconciler := &AutoIndexerReconciler{
			Client: fakeClient,
			Log:    logr.Discard(),
		}

		err := reconciler.handleDriftRemediationJobCompletion(context.Background(), autoIndexer)
		if err != nil {
			t.Fatalf("handleDriftRemediationJobCompletion failed: %v", err)
		}

		// Verify AutoIndexer is unsuspended
		var updatedAutoIndexer autoindexerv1alpha1.AutoIndexer
		err = fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      "test-autoindexer",
			Namespace: "default",
		}, &updatedAutoIndexer)
		if err != nil {
			t.Fatalf("Failed to get updated AutoIndexer: %v", err)
		}

		if updatedAutoIndexer.Spec.Suspend == nil || *updatedAutoIndexer.Spec.Suspend {
			t.Errorf("Expected AutoIndexer to be unsuspended, but Suspend is %v", updatedAutoIndexer.Spec.Suspend)
		}

		// Verify annotations are removed
		if updatedAutoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation-suspended"] != "" {
			t.Errorf("Expected drift-remediation-suspended annotation to be removed")
		}

		if updatedAutoIndexer.Annotations["autoindexer.kaito.sh/original-suspend-state"] != "" {
			t.Errorf("Expected original-suspend-state annotation to be removed")
		}
	})

	t.Run("dont_unsuspend_if_jobs_still_running", func(t *testing.T) {
		schedule := "0 0 * * *"
		suspend := true

		// Create AutoIndexer that was suspended for drift remediation
		autoIndexer := &autoindexerv1alpha1.AutoIndexer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer",
				Namespace: "default",
				Annotations: map[string]string{
					"autoindexer.kaito.sh/drift-remediation-suspended": "true",
					"autoindexer.kaito.sh/original-suspend-state":      "false",
				},
			},
			Spec: autoindexerv1alpha1.AutoIndexerSpec{
				RAGEngine: "test-rag",
				IndexName: "test-index",
				Schedule:  &schedule,
				Suspend:   &suspend, // Currently suspended
				DataSource: autoindexerv1alpha1.DataSourceSpec{
					Type: autoindexerv1alpha1.DataSourceTypeGitHub,
					Git: &autoindexerv1alpha1.GitDataSourceSpec{
						Repository: "test/repo",
						Branch:     "main",
					},
				},
			},
		}

		// Create a running drift remediation job (not completed)
		runningJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer-drift-remediation-123",
				Namespace: "default",
				Labels: map[string]string{
					"autoindexer.kaito.sh/name":              "test-autoindexer",
					"autoindexer.kaito.sh/drift-remediation": "true",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(autoIndexer, autoindexerv1alpha1.GroupVersion.WithKind("AutoIndexer")),
				},
			},
			Status: batchv1.JobStatus{
				// No Succeeded or Failed - job is still running
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(autoIndexer, runningJob).Build()
		reconciler := &AutoIndexerReconciler{
			Client: fakeClient,
			Log:    logr.Discard(),
		}

		err := reconciler.handleDriftRemediationJobCompletion(context.Background(), autoIndexer)
		if err != nil {
			t.Fatalf("handleDriftRemediationJobCompletion failed: %v", err)
		}

		// Verify AutoIndexer remains suspended since job is still running
		var updatedAutoIndexer autoindexerv1alpha1.AutoIndexer
		err = fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      "test-autoindexer",
			Namespace: "default",
		}, &updatedAutoIndexer)
		if err != nil {
			t.Fatalf("Failed to get updated AutoIndexer: %v", err)
		}

		if updatedAutoIndexer.Spec.Suspend == nil || !*updatedAutoIndexer.Spec.Suspend {
			t.Errorf("Expected AutoIndexer to remain suspended while jobs are running, but Suspend is %v", updatedAutoIndexer.Spec.Suspend)
		}

		// Verify annotations remain
		if updatedAutoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation-suspended"] != "true" {
			t.Errorf("Expected drift-remediation-suspended annotation to remain while jobs are running")
		}
	})

	t.Run("no_action_if_not_suspended_for_drift_remediation", func(t *testing.T) {
		schedule := "0 0 * * *"

		// Create AutoIndexer that was NOT suspended for drift remediation
		autoIndexer := &autoindexerv1alpha1.AutoIndexer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer",
				Namespace: "default",
				// No drift remediation annotations
			},
			Spec: autoindexerv1alpha1.AutoIndexerSpec{
				RAGEngine: "test-rag",
				IndexName: "test-index",
				Schedule:  &schedule,
				// Not suspended
				DataSource: autoindexerv1alpha1.DataSourceSpec{
					Type: autoindexerv1alpha1.DataSourceTypeGitHub,
					Git: &autoindexerv1alpha1.GitDataSourceSpec{
						Repository: "test/repo",
						Branch:     "main",
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(autoIndexer).Build()
		reconciler := &AutoIndexerReconciler{
			Client: fakeClient,
			Log:    logr.Discard(),
		}

		err := reconciler.handleDriftRemediationJobCompletion(context.Background(), autoIndexer)
		if err != nil {
			t.Fatalf("handleDriftRemediationJobCompletion failed: %v", err)
		}

		// Verify nothing changed since it wasn't suspended for drift remediation
		var updatedAutoIndexer autoindexerv1alpha1.AutoIndexer
		err = fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      "test-autoindexer",
			Namespace: "default",
		}, &updatedAutoIndexer)
		if err != nil {
			t.Fatalf("Failed to get updated AutoIndexer: %v", err)
		}

		// Should remain not suspended
		if updatedAutoIndexer.Spec.Suspend != nil && *updatedAutoIndexer.Spec.Suspend {
			t.Errorf("Expected AutoIndexer to remain unsuspended, but Suspend is %v", updatedAutoIndexer.Spec.Suspend)
		}
	})
}
