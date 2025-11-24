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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
)

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
