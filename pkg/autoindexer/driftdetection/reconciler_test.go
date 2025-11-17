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

package driftdetection

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
)

func TestDriftReconciler_TriggerJob_ScheduledAutoIndexer_Suspension(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	schedule := "0 0 * * *"

	// Test case 1: AutoIndexer is not suspended initially - should be suspended during remediation
	t.Run("suspend_not_initially_suspended", func(t *testing.T) {
		autoIndexer := &autoindexerv1alpha1.AutoIndexer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer",
				Namespace: "default",
			},
			Spec: autoindexerv1alpha1.AutoIndexerSpec{
				RAGEngine: "test-rag",
				IndexName: "test-index",
				Schedule:  &schedule,
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
		reconciler := NewDriftReconciler(fakeClient, klog.NewKlogr())

		result := DriftDetectionResult{
			AutoIndexerName:      "test-autoindexer",
			AutoIndexerNamespace: "default",
			Action:               DriftActionTriggerJob,
			ExpectedCount:        100,
			ActualCount:          50,
		}

		err := reconciler.ReconcileDrift(result)
		if err != nil {
			t.Fatalf("ReconcileDrift failed: %v", err)
		}

		// Verify AutoIndexer is suspended
		var updatedAutoIndexer autoindexerv1alpha1.AutoIndexer
		err = fakeClient.Get(context.Background(), client.ObjectKey{
			Name:      "test-autoindexer",
			Namespace: "default",
		}, &updatedAutoIndexer)
		if err != nil {
			t.Fatalf("Failed to get updated AutoIndexer: %v", err)
		}

		if updatedAutoIndexer.Spec.Suspend == nil || !*updatedAutoIndexer.Spec.Suspend {
			t.Errorf("Expected AutoIndexer to be suspended, but Suspend is %v", updatedAutoIndexer.Spec.Suspend)
		}

		// Verify annotations are set
		if updatedAutoIndexer.Annotations["autoindexer.kaito.io/drift-remediation-suspended"] != "true" {
			t.Errorf("Expected drift-remediation-suspended annotation to be true")
		}

		if updatedAutoIndexer.Annotations["autoindexer.kaito.io/original-suspend-state"] != "false" {
			t.Errorf("Expected original-suspend-state annotation to be false")
		}

		// Verify job was created with drift remediation label
		jobs := &batchv1.JobList{}
		err = fakeClient.List(context.Background(), jobs, client.InNamespace("default"))
		if err != nil {
			t.Fatalf("Failed to list jobs: %v", err)
		}

		if len(jobs.Items) != 1 {
			t.Errorf("Expected 1 job to be created, but got %d", len(jobs.Items))
		} else {
			job := jobs.Items[0]
			if job.Labels["autoindexer.kaito.io/drift-remediation"] != "true" {
				t.Errorf("Expected job to have drift-remediation label set to true")
			}
		}
	})

	// Test case 2: AutoIndexer is already suspended - should not change suspension state
	t.Run("dont_suspend_already_suspended", func(t *testing.T) {
		suspend := true
		autoIndexer := &autoindexerv1alpha1.AutoIndexer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer-suspended",
				Namespace: "default",
			},
			Spec: autoindexerv1alpha1.AutoIndexerSpec{
				RAGEngine: "test-rag",
				IndexName: "test-index",
				Schedule:  &schedule,
				Suspend:   &suspend,
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
		reconciler := NewDriftReconciler(fakeClient, klog.NewKlogr())

		result := DriftDetectionResult{
			AutoIndexerName:      "test-autoindexer-suspended",
			AutoIndexerNamespace: "default",
			Action:               DriftActionTriggerJob,
			ExpectedCount:        100,
			ActualCount:          50,
		}

		err := reconciler.ReconcileDrift(result)
		if err != nil {
			t.Fatalf("ReconcileDrift failed: %v", err)
		}

		// Verify AutoIndexer remains suspended but no annotations are added since it was already suspended
		var updatedAutoIndexer autoindexerv1alpha1.AutoIndexer
		err = fakeClient.Get(context.Background(), client.ObjectKey{
			Name:      "test-autoindexer-suspended",
			Namespace: "default",
		}, &updatedAutoIndexer)
		if err != nil {
			t.Fatalf("Failed to get updated AutoIndexer: %v", err)
		}

		if updatedAutoIndexer.Spec.Suspend == nil || !*updatedAutoIndexer.Spec.Suspend {
			t.Errorf("Expected AutoIndexer to remain suspended, but Suspend is %v", updatedAutoIndexer.Spec.Suspend)
		}

		// Verify no drift remediation annotations are set since it was already suspended
		if updatedAutoIndexer.Annotations["autoindexer.kaito.io/drift-remediation-suspended"] == "true" {
			t.Errorf("Did not expect drift-remediation-suspended annotation to be set for already suspended AutoIndexer")
		}
	})

	// Test case 3: One-time AutoIndexer (no schedule) - should not be suspended
	t.Run("onetime_not_suspended", func(t *testing.T) {
		autoIndexer := &autoindexerv1alpha1.AutoIndexer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-autoindexer-onetime",
				Namespace: "default",
			},
			Spec: autoindexerv1alpha1.AutoIndexerSpec{
				RAGEngine: "test-rag",
				IndexName: "test-index",
				// No schedule - one-time job
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
		reconciler := NewDriftReconciler(fakeClient, klog.NewKlogr())

		result := DriftDetectionResult{
			AutoIndexerName:      "test-autoindexer-onetime",
			AutoIndexerNamespace: "default",
			Action:               DriftActionTriggerJob,
			ExpectedCount:        100,
			ActualCount:          50,
		}

		err := reconciler.ReconcileDrift(result)
		if err != nil {
			t.Fatalf("ReconcileDrift failed: %v", err)
		}

		// Verify AutoIndexer is not suspended (since it's a one-time job)
		var updatedAutoIndexer autoindexerv1alpha1.AutoIndexer
		err = fakeClient.Get(context.Background(), client.ObjectKey{
			Name:      "test-autoindexer-onetime",
			Namespace: "default",
		}, &updatedAutoIndexer)
		if err != nil {
			t.Fatalf("Failed to get updated AutoIndexer: %v", err)
		}

		// Should not be suspended (or if it is suspended, it shouldn't have been changed)
		if updatedAutoIndexer.Spec.Suspend != nil && *updatedAutoIndexer.Spec.Suspend {
			t.Errorf("One-time AutoIndexer should not be suspended")
		}

		// Verify no drift remediation annotations are set
		if updatedAutoIndexer.Annotations["autoindexer.kaito.io/drift-remediation-suspended"] == "true" {
			t.Errorf("One-time AutoIndexer should not have drift-remediation-suspended annotation")
		}
	})
}
