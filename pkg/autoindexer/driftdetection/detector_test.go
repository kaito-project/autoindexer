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
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
)

// MockRAGEngineClient implements RAGEngineClient for testing
type MockRAGEngineClient struct {
	mock.Mock
}

func (m *MockRAGEngineClient) GetDocumentCount(ragEngineEndpoint, indexName, autoindexerName, autoIndexerNamespace string) (int32, error) {
	args := m.Called(ragEngineEndpoint, indexName, autoindexerName, autoIndexerNamespace)
	return args.Get(0).(int32), args.Error(1)
}

func (m *MockRAGEngineClient) ListIndexes(ragEngineEndpoint, indexName, autoindexerName, autoIndexerNamespace string) ([]string, error) {
	args := m.Called(ragEngineEndpoint, indexName, autoindexerName, autoIndexerNamespace)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockRAGEngineClient) DeleteIndex(ragEngineEndpoint, indexName, autoIndexerNamespace string) error {
	args := m.Called(ragEngineEndpoint, indexName, autoIndexerNamespace)
	return args.Error(0)
}

func TestNewDriftDetector(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	ragClient := &MockRAGEngineClient{}
	config := DefaultDriftDetectionConfig()
	logger := logr.Discard()

	detector := NewDriftDetector(client, ragClient, config, logger)
	assert.NotNil(t, detector)

	impl, ok := detector.(*DriftDetectorImpl)
	assert.True(t, ok)
	assert.NotNil(t, impl.client)
	assert.NotNil(t, impl.ragClient)
	assert.Equal(t, config, impl.config)
}

func TestDriftDetector_DetermineDriftAction(t *testing.T) {
	detector := &DriftDetectorImpl{}

	// Test scheduled AutoIndexer
	scheduledAutoIndexer := &autoindexerv1alpha1.AutoIndexer{
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			Schedule: stringPtr("0 0 * * *"), // Daily
		},
	}

	action := detector.determineDriftAction(scheduledAutoIndexer)
	assert.Equal(t, DriftActionTriggerJob, action)

	// Test one-time AutoIndexer
	oneTimeAutoIndexer := &autoindexerv1alpha1.AutoIndexer{
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			Schedule: nil,
		},
	}

	action = detector.determineDriftAction(oneTimeAutoIndexer)
	assert.Equal(t, DriftActionTriggerJob, action)
}

func TestDriftDetector_IsAutoIndexerInStableState(t *testing.T) {
	detector := &DriftDetectorImpl{}

	testCases := []struct {
		phase    autoindexerv1alpha1.AutoIndexerPhase
		expected bool
	}{
		{autoindexerv1alpha1.AutoIndexerPhaseCompleted, false},
		{autoindexerv1alpha1.AutoIndexerPhaseFailed, false},
		{autoindexerv1alpha1.AutoIndexerPhasePending, false},
		{autoindexerv1alpha1.AutoIndexerPhaseRunning, false},
		{autoindexerv1alpha1.AutoIndexerPhaseScheduled, true},
		{autoindexerv1alpha1.AutoIndexerPhaseSuspended, false},
		{autoindexerv1alpha1.AutoIndexerPhaseDriftRemediation, false},
	}

	for _, tc := range testCases {
		autoIndexer := &autoindexerv1alpha1.AutoIndexer{
			Status: autoindexerv1alpha1.AutoIndexerStatus{
				IndexingPhase: tc.phase,
			},
		}

		result := detector.isAutoIndexerInStableState(autoIndexer)
		assert.Equal(t, tc.expected, result, "Phase %s should return %v", tc.phase, tc.expected)
	}
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}

func TestDriftDetector_Start(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		expectedResult bool
	}{
		{
			name:           "start with drift detection enabled",
			enabled:        true,
			expectedResult: false, // no error
		},
		{
			name:           "start with drift detection disabled",
			enabled:        false,
			expectedResult: false, // no error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().Build()
			ragClient := &MockRAGEngineClient{}
			config := DriftDetectionConfig{
				Enabled:        tt.enabled,
				CheckInterval:  1 * time.Second,
				MaxRetries:     1,
				RequestTimeout: 1 * time.Second,
			}

			detector := NewDriftDetector(client, ragClient, config, logr.Discard())

			stopCh := make(chan struct{})
			err := detector.Start(stopCh)

			if tt.expectedResult {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Clean up
			detector.Stop()
			close(stopCh)
		})
	}
}

func TestDriftDetector_Stop(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	ragClient := &MockRAGEngineClient{}
	config := DefaultDriftDetectionConfig()

	detector := NewDriftDetector(client, ragClient, config, logr.Discard())

	// Start first
	stopCh := make(chan struct{})
	err := detector.Start(stopCh)
	assert.NoError(t, err)

	// Then stop
	err = detector.Stop()
	assert.NoError(t, err)

	close(stopCh)
}

func TestDriftDetector_CheckAutoIndexerDrift(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name              string
		autoIndexer       *autoindexerv1alpha1.AutoIndexer
		mockDocumentCount int32
		mockError         error
		expectedDrift     bool
		expectedError     bool
	}{
		{
			name: "no drift when counts match",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseCompleted,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 10,
			mockError:         nil,
			expectedDrift:     false,
			expectedError:     false,
		},
		{
			name: "drift when counts differ",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseScheduled,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 5,
			mockError:         nil,
			expectedDrift:     true,
			expectedError:     false,
		},
		{
			name: "skip drift check when remediation policy is Ignore",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
					DriftRemediationPolicy: &autoindexerv1alpha1.DriftRemediationPolicy{
						Strategy: autoindexerv1alpha1.DriftRemediationStrategyIgnore,
					},
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseScheduled,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 5,
			mockError:         nil,
			expectedDrift:     false, // Should skip drift check when policy is Ignore
			expectedError:     false,
		},
		{
			name: "detect drift when remediation policy is Auto",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
					DriftRemediationPolicy: &autoindexerv1alpha1.DriftRemediationPolicy{
						Strategy: autoindexerv1alpha1.DriftRemediationStrategyAuto,
					},
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseScheduled,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 5,
			mockError:         nil,
			expectedDrift:     true, // Should detect drift when policy is Auto
			expectedError:     false,
		},
		{
			name: "detect drift when remediation policy is Manual",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
					DriftRemediationPolicy: &autoindexerv1alpha1.DriftRemediationPolicy{
						Strategy: autoindexerv1alpha1.DriftRemediationStrategyManual,
					},
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseScheduled,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 5,
			mockError:         nil,
			expectedDrift:     true, // Should detect drift when policy is Manual
			expectedError:     false,
		},
		{
			name: "detect drift when no remediation policy set (default behavior)",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
					// No DriftRemediationPolicy set
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseScheduled,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 5,
			mockError:         nil,
			expectedDrift:     true, // Should detect drift when no policy is set
			expectedError:     false,
		},
		{
			name: "skip when suspended",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
					Suspend:   &[]bool{true}[0],
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseCompleted,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 5,
			mockError:         nil,
			expectedDrift:     false, // Should skip drift check
			expectedError:     false,
		},
		{
			name: "skip when not in stable state",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase:        autoindexerv1alpha1.AutoIndexerPhaseRunning,
					NumOfDocumentInIndex: 10,
				},
			},
			mockDocumentCount: 5,
			mockError:         nil,
			expectedDrift:     false, // Should skip drift check
			expectedError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			ragClient := &MockRAGEngineClient{}
			config := DefaultDriftDetectionConfig()

			// Set up mock expectations
			ragClient.On("GetDocumentCount",
				tt.autoIndexer.Spec.RAGEngine,
				tt.autoIndexer.Spec.IndexName,
				tt.autoIndexer.Name,
				tt.autoIndexer.Namespace).Return(tt.mockDocumentCount, tt.mockError).Maybe()

			detector := &DriftDetectorImpl{
				client:    client,
				ragClient: ragClient,
				config:    config,
				logger:    logr.Discard(),
				stopCh:    make(chan struct{}),
			}

			ctx := context.Background()
			result := detector.checkAutoIndexerDrift(ctx, tt.autoIndexer)

			assert.Equal(t, tt.autoIndexer.Name, result.AutoIndexerName)
			assert.Equal(t, tt.autoIndexer.Namespace, result.AutoIndexerNamespace)
			assert.Equal(t, tt.expectedDrift, result.DriftDetected)

			if tt.expectedError {
				assert.Error(t, result.Error)
			} else {
				assert.NoError(t, result.Error)
			}

			ragClient.AssertExpectations(t)
		})
	}
}

// TODO: Fix this test - fake client status update issue similar to controller tests
/*
func TestDriftDetector_SetStatusToDriftRemediation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)

	autoIndexer := &autoindexerv1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ai",
			Namespace: "default",
		},
		Spec: autoindexerv1alpha1.AutoIndexerSpec{
			RAGEngine: "test-rag",
			IndexName: "test-index",
		},
		Status: autoindexerv1alpha1.AutoIndexerStatus{
			IndexingPhase: autoindexerv1alpha1.AutoIndexerPhaseCompleted,
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(autoIndexer).WithStatusSubresource(autoIndexer).Build()
	ragClient := &MockRAGEngineClient{}
	config := DefaultDriftDetectionConfig()

	detector := &DriftDetectorImpl{
		client:    client,
		ragClient: ragClient,
		config:    config,
		logger:    logr.Discard(),
		stopCh:    make(chan struct{}),
	}

	ctx := context.Background()
	err := detector.setStatusToDriftRemediation(ctx, autoIndexer)
	assert.NoError(t, err)

	// Verify the phase was updated - need to fetch the updated object
	var updatedAutoIndexer autoindexerv1alpha1.AutoIndexer
	err = client.Get(ctx, types.NamespacedName{Name: "test-ai", Namespace: "default"}, &updatedAutoIndexer)
	assert.NoError(t, err)
	assert.Equal(t, autoindexerv1alpha1.AutoIndexerPhaseDriftRemediation, updatedAutoIndexer.Status.IndexingPhase)
}
*/

func TestDriftDetector_SetDriftDetectedAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name        string
		autoIndexer *autoindexerv1alpha1.AutoIndexer
		expectError bool
	}{
		{
			name: "set annotations on AutoIndexer without existing annotations",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase: autoindexerv1alpha1.AutoIndexerPhaseScheduled,
				},
			},
			expectError: false,
		},
		{
			name: "set annotations on AutoIndexer with existing annotations",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
					Annotations: map[string]string{
						"existing-annotation": "value",
					},
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase: autoindexerv1alpha1.AutoIndexerPhaseScheduled,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.autoIndexer).Build()
			ragClient := &MockRAGEngineClient{}
			config := DefaultDriftDetectionConfig()

			detector := &DriftDetectorImpl{
				client:    client,
				ragClient: ragClient,
				config:    config,
				logger:    logr.Discard(),
				stopCh:    make(chan struct{}),
			}

			ctx := context.Background()
			err := detector.setDriftDetected(ctx, tt.autoIndexer)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				
				// Verify annotations were set
				assert.Equal(t, "true", tt.autoIndexer.Annotations["autoindexer.kaito.sh/drift-detected"])
				assert.NotEmpty(t, tt.autoIndexer.Annotations["autoindexer.kaito.sh/last-drift-detected"])
				
				// Verify existing annotations are preserved
				if existingValue, exists := tt.autoIndexer.Annotations["existing-annotation"]; exists {
					assert.Equal(t, "value", existingValue)
				}
			}
		})
	}
}

func TestDriftDetector_SetDriftRemediatedAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = autoindexerv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name               string
		autoIndexer        *autoindexerv1alpha1.AutoIndexer
		expectError        bool
		shouldUpdateClient bool
	}{
		{
			name: "clear drift annotation when it exists and is true",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
					Annotations: map[string]string{
						"autoindexer.kaito.sh/drift-detected": "true",
					},
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase: autoindexerv1alpha1.AutoIndexerPhaseScheduled,
				},
			},
			expectError:        false,
			shouldUpdateClient: true,
		},
		{
			name: "no update when annotation doesn't exist",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase: autoindexerv1alpha1.AutoIndexerPhaseScheduled,
				},
			},
			expectError:        false,
			shouldUpdateClient: false,
		},
		{
			name: "no update when annotation is already false",
			autoIndexer: &autoindexerv1alpha1.AutoIndexer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ai",
					Namespace: "default",
					Annotations: map[string]string{
						"autoindexer.kaito.sh/drift-detected": "false",
					},
				},
				Spec: autoindexerv1alpha1.AutoIndexerSpec{
					RAGEngine: "test-rag",
					IndexName: "test-index",
				},
				Status: autoindexerv1alpha1.AutoIndexerStatus{
					IndexingPhase: autoindexerv1alpha1.AutoIndexerPhaseScheduled,
				},
			},
			expectError:        false,
			shouldUpdateClient: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.autoIndexer).Build()
			ragClient := &MockRAGEngineClient{}
			config := DefaultDriftDetectionConfig()

			detector := &DriftDetectorImpl{
				client:    client,
				ragClient: ragClient,
				config:    config,
				logger:    logr.Discard(),
				stopCh:    make(chan struct{}),
			}

			ctx := context.Background()
			err := detector.setDriftRemediated(ctx, tt.autoIndexer)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				
				if tt.shouldUpdateClient {
					// Verify annotation was set to false
					assert.Equal(t, "false", tt.autoIndexer.Annotations["autoindexer.kaito.sh/drift-detected"])
				}
			}
		})
	}
}
