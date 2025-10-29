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
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

func TestUpdateAutoIndexerStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name          string
		autoIndexer   *autoindexerv1alpha1.AutoIndexer
		cronJobs      []batchv1.CronJob
		expectError   bool
		expectedCalls int
	}{
		{
			name: "AutoIndexer without schedule",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
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
							Branch:     "main",
						},
					},
					Schedule: nil, // No schedule
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{},
			},
			cronJobs:      []batchv1.CronJob{},
			expectError:   false,
			expectedCalls: 0,
		},
		{
			name: "AutoIndexer with schedule and CronJob",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
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
							Branch:     "main",
						},
					},
					Schedule: stringPtr("0 0 * * *"),
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{},
			},
			cronJobs: []batchv1.CronJob{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cronjob",
						Namespace: "default",
						Labels: map[string]string{
							AutoIndexerNameLabel: "test-autoindexer",
						},
					},
					Status: batchv1.CronJobStatus{
						LastScheduleTime: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
					},
				},
			},
			expectError:   false,
			expectedCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{tt.autoIndexer}
			for i := range tt.cronJobs {
				objects = append(objects, &tt.cronJobs[i])
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&autoindexerv1alpha1.AutoIndexer{}).
				Build()

			recorder := record.NewFakeRecorder(10)
			reconciler := &AutoIndexerReconciler{
				Client:   client,
				Scheme:   scheme,
				Log:      logr.Discard(),
				Recorder: recorder,
			}

			ctx := context.Background()
			err := reconciler.updateAutoIndexerStatus(ctx, tt.autoIndexer)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Check if NextScheduledIndexing was set for scheduled AutoIndexers
			if tt.autoIndexer.Spec.Schedule != nil && len(tt.cronJobs) > 0 {
				if tt.autoIndexer.Status.NextScheduledIndexing == nil {
					t.Error("Expected NextScheduledIndexing to be set")
				}
			}
		})
	}
}

func TestUpdateNextScheduledRun(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	tests := []struct {
		name                   string
		autoIndexer            *autoindexerv1alpha1.AutoIndexer
		cronJobs               []batchv1.CronJob
		expectError            bool
		expectNextScheduledSet bool
	}{
		{
			name: "CronJob with LastScheduleTime",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-autoindexer",
					Namespace: "default",
				},
			},
			cronJobs: []batchv1.CronJob{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cronjob",
						Namespace: "default",
						Labels: map[string]string{
							AutoIndexerNameLabel: "test-autoindexer",
						},
					},
					Status: batchv1.CronJobStatus{
						LastScheduleTime: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
					},
				},
			},
			expectError:            false,
			expectNextScheduledSet: true,
		},
		{
			name: "CronJob without LastScheduleTime",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-autoindexer",
					Namespace: "default",
				},
			},
			cronJobs: []batchv1.CronJob{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cronjob",
						Namespace: "default",
						Labels: map[string]string{
							AutoIndexerNameLabel: "test-autoindexer",
						},
					},
					Status: batchv1.CronJobStatus{
						LastScheduleTime: nil,
					},
				},
			},
			expectError:            false,
			expectNextScheduledSet: false,
		},
		{
			name: "No CronJobs found",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-autoindexer",
					Namespace: "default",
				},
			},
			cronJobs:               []batchv1.CronJob{},
			expectError:            false,
			expectNextScheduledSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{tt.autoIndexer}
			for i := range tt.cronJobs {
				objects = append(objects, &tt.cronJobs[i])
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			reconciler := &AutoIndexerReconciler{
				Client: client,
				Scheme: scheme,
				Log:    logr.Discard(),
			}

			ctx := context.Background()
			err := reconciler.updateNextScheduledRun(ctx, tt.autoIndexer)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tt.expectNextScheduledSet {
				if tt.autoIndexer.Status.NextScheduledIndexing == nil {
					t.Error("Expected NextScheduledIndexing to be set")
				}
			} else {
				if tt.autoIndexer.Status.NextScheduledIndexing != nil {
					t.Error("Expected NextScheduledIndexing to be nil")
				}
			}
		})
	}
}

func TestSetAutoIndexerCondition(t *testing.T) {
	tests := []struct {
		name               string
		autoIndexer        *autoindexerv1alpha1.AutoIndexer
		conditionType      autoindexerv1alpha1.ConditionType
		status             metav1.ConditionStatus
		reason             string
		message            string
		expectedConditions int
		expectUpdate       bool
	}{
		{
			name: "Add new condition",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{},
				},
			},
			conditionType:      autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded,
			status:             metav1.ConditionTrue,
			reason:             "TestReason",
			message:            "Test message",
			expectedConditions: 1,
			expectUpdate:       true,
		},
		{
			name: "Update existing condition with status change",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded),
							Status:  metav1.ConditionFalse,
							Reason:  "OldReason",
							Message: "Old message",
						},
					},
				},
			},
			conditionType:      autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded,
			status:             metav1.ConditionTrue,
			reason:             "NewReason",
			message:            "New message",
			expectedConditions: 1,
			expectUpdate:       true,
		},
		{
			name: "No update when status unchanged",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded),
							Status:             metav1.ConditionTrue,
							Reason:             "OldReason",
							Message:            "Old message",
							LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
						},
					},
				},
			},
			conditionType:      autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded,
			status:             metav1.ConditionTrue,
			reason:             "NewReason",
			message:            "New message",
			expectedConditions: 1,
			expectUpdate:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &AutoIndexerReconciler{}

			// Store original condition for comparison
			var originalCondition *metav1.Condition
			for _, condition := range tt.autoIndexer.Status.Conditions {
				if condition.Type == string(tt.conditionType) {
					originalCondition = &condition
					break
				}
			}

			reconciler.setAutoIndexerCondition(tt.autoIndexer, tt.conditionType, tt.status, tt.reason, tt.message)

			if len(tt.autoIndexer.Status.Conditions) != tt.expectedConditions {
				t.Errorf("Expected %d conditions, got %d", tt.expectedConditions, len(tt.autoIndexer.Status.Conditions))
			}

			// Find the condition
			var foundCondition *metav1.Condition
			for _, condition := range tt.autoIndexer.Status.Conditions {
				if condition.Type == string(tt.conditionType) {
					foundCondition = &condition
					break
				}
			}

			if foundCondition == nil {
				t.Error("Expected condition not found")
				return
			}

			if foundCondition.Status != tt.status {
				t.Errorf("Expected status %v, got %v", tt.status, foundCondition.Status)
			}

			if tt.expectUpdate {
				// When update is expected, check that reason and message are updated
				if foundCondition.Reason != tt.reason {
					t.Errorf("Expected reason %v, got %v", tt.reason, foundCondition.Reason)
				}

				if foundCondition.Message != tt.message {
					t.Errorf("Expected message %v, got %v", tt.message, foundCondition.Message)
				}

				// Check if LastTransitionTime was set
				if foundCondition.LastTransitionTime.IsZero() {
					t.Error("Expected LastTransitionTime to be set")
				}
			} else {
				// When no update is expected, check that reason and message remain unchanged
				if originalCondition != nil {
					if foundCondition.Reason != originalCondition.Reason {
						t.Errorf("Expected reason to remain %v, got %v", originalCondition.Reason, foundCondition.Reason)
					}

					if foundCondition.Message != originalCondition.Message {
						t.Errorf("Expected message to remain %v, got %v", originalCondition.Message, foundCondition.Message)
					}

					if !foundCondition.LastTransitionTime.Equal(&originalCondition.LastTransitionTime) {
						t.Error("Expected LastTransitionTime to remain unchanged")
					}
				}
			}
		})
	}
}

func TestGetAutoIndexerCondition(t *testing.T) {
	tests := []struct {
		name          string
		autoIndexer   *autoindexerv1alpha1.AutoIndexer
		conditionType autoindexerv1alpha1.ConditionType
		expectNil     bool
	}{
		{
			name: "Condition exists",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			conditionType: autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded,
			expectNil:     false,
		},
		{
			name: "Condition does not exist",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{},
				},
			},
			conditionType: autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded,
			expectNil:     true,
		},
		{
			name: "Different condition exists",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(autoindexerv1alpha1.AutoIndexerConditionTypeError),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			conditionType: autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded,
			expectNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &AutoIndexerReconciler{}
			condition := reconciler.getAutoIndexerCondition(tt.autoIndexer, tt.conditionType)

			if tt.expectNil && condition != nil {
				t.Error("Expected nil condition but got one")
			}
			if !tt.expectNil && condition == nil {
				t.Error("Expected condition but got nil")
			}
			if !tt.expectNil && condition != nil {
				if condition.Type != string(tt.conditionType) {
					t.Errorf("Expected condition type %v, got %v", tt.conditionType, condition.Type)
				}
			}
		})
	}
}

func TestIsAutoIndexerReady(t *testing.T) {
	tests := []struct {
		name        string
		autoIndexer *autoindexerv1alpha1.AutoIndexer
		expectReady bool
	}{
		{
			name: "AutoIndexer is ready",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(autoindexerv1alpha1.ConditionTypeResourceStatus),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectReady: true,
		},
		{
			name: "AutoIndexer is not ready",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(autoindexerv1alpha1.ConditionTypeResourceStatus),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expectReady: false,
		},
		{
			name: "No ResourceStatus condition",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectReady: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &AutoIndexerReconciler{}
			ready := reconciler.isAutoIndexerReady(tt.autoIndexer)

			if ready != tt.expectReady {
				t.Errorf("Expected ready=%v, got ready=%v", tt.expectReady, ready)
			}
		})
	}
}

func TestRecordAutoIndexerEvent(t *testing.T) {
	tests := []struct {
		name        string
		hasRecorder bool
		eventType   string
		reason      string
		message     string
	}{
		{
			name:        "With recorder",
			hasRecorder: true,
			eventType:   "Normal",
			reason:      "TestReason",
			message:     "Test message",
		},
		{
			name:        "Without recorder",
			hasRecorder: false,
			eventType:   "Warning",
			reason:      "TestReason",
			message:     "Test message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			autoIndexer := &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-autoindexer",
					Namespace: "default",
				},
			}

			var recorder record.EventRecorder
			if tt.hasRecorder {
				recorder = record.NewFakeRecorder(10)
			}

			reconciler := &AutoIndexerReconciler{
				Recorder: recorder,
			}

			// This should not panic even without a recorder
			reconciler.recordAutoIndexerEvent(autoIndexer, tt.eventType, tt.reason, tt.message)

			if tt.hasRecorder {
				fakeRecorder := recorder.(*record.FakeRecorder)
				select {
				case event := <-fakeRecorder.Events:
					expectedEvent := tt.eventType + " " + tt.reason + " " + tt.message
					if event != expectedEvent {
						t.Errorf("Expected event %q, got %q", expectedEvent, event)
					}
				default:
					t.Error("Expected event to be recorded")
				}
			}
		})
	}
}

func TestHandleJobFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
		Status: autoindexerv1alpha1.AutoIndexerStatus{
			Conditions: []metav1.Condition{},
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
	}

	recorder := record.NewFakeRecorder(10)
	reconciler := &AutoIndexerReconciler{
		Recorder: recorder,
	}

	ctx := context.Background()
	testError := fmt.Errorf("test job failure")

	err := reconciler.handleJobFailure(ctx, autoIndexer, job, testError)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Check if error condition was set
	errorCondition := reconciler.getAutoIndexerCondition(autoIndexer, autoindexerv1alpha1.AutoIndexerConditionTypeError)
	if errorCondition == nil {
		t.Error("Expected error condition to be set")
	} else {
		if errorCondition.Status != metav1.ConditionTrue {
			t.Errorf("Expected error condition status to be True, got %v", errorCondition.Status)
		}
		if errorCondition.Reason != "JobFailed" {
			t.Errorf("Expected reason 'JobFailed', got %v", errorCondition.Reason)
		}
	}

	// Check if event was recorded
	select {
	case event := <-recorder.Events:
		if !contains(event, "JobFailed") {
			t.Errorf("Expected event to contain 'JobFailed', got: %s", event)
		}
	default:
		t.Error("Expected event to be recorded")
	}
}

func TestHandleJobSuccess(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
		Status: autoindexerv1alpha1.AutoIndexerStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(autoindexerv1alpha1.AutoIndexerConditionTypeError),
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
	}

	recorder := record.NewFakeRecorder(10)
	reconciler := &AutoIndexerReconciler{
		Recorder: recorder,
	}

	ctx := context.Background()

	err := reconciler.handleJobSuccess(ctx, autoIndexer, job)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Check if success condition was set
	successCondition := reconciler.getAutoIndexerCondition(autoIndexer, autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded)
	if successCondition == nil {
		t.Error("Expected success condition to be set")
	} else {
		if successCondition.Status != metav1.ConditionTrue {
			t.Errorf("Expected success condition status to be True, got %v", successCondition.Status)
		}
		if successCondition.Reason != "JobSucceeded" {
			t.Errorf("Expected reason 'JobSucceeded', got %v", successCondition.Reason)
		}
	}

	// Check if error condition was cleared
	errorCondition := reconciler.getAutoIndexerCondition(autoIndexer, autoindexerv1alpha1.AutoIndexerConditionTypeError)
	if errorCondition == nil {
		t.Error("Expected error condition to exist")
	} else {
		if errorCondition.Status != metav1.ConditionFalse {
			t.Errorf("Expected error condition status to be False, got %v", errorCondition.Status)
		}
	}

	// Check if event was recorded
	select {
	case event := <-recorder.Events:
		if !contains(event, "JobSucceeded") {
			t.Errorf("Expected event to contain 'JobSucceeded', got: %s", event)
		}
	default:
		t.Error("Expected event to be recorded")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsHelper(s, substr))))
}

func containsHelper(s, substr string) bool {
	for i := 1; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Helper function to return string pointer
func stringPtr(s string) *string {
	return &s
}
