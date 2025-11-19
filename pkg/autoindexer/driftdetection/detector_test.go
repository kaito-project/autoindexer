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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

func TestNewDriftDetector(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	ragClient := &MockRAGEngineClient{}
	config := DefaultDriftDetectionConfig()
	logger := logr.Discard()

	reconcilerFunc := func(result DriftDetectionResult) error {
		return nil
	}

	detector := NewDriftDetector(client, ragClient, config, logger, reconcilerFunc)
	assert.NotNil(t, detector)

	impl, ok := detector.(*DriftDetectorImpl)
	assert.True(t, ok)
	assert.NotNil(t, impl.client)
	assert.NotNil(t, impl.ragClient)
	assert.Equal(t, config, impl.config)
	assert.NotNil(t, impl.reconcilerFunc)
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
		{autoindexerv1alpha1.AutoIndexerPhaseCompleted, true},
		{autoindexerv1alpha1.AutoIndexerPhaseFailed, true},
		{autoindexerv1alpha1.AutoIndexerPhasePending, false},
		{autoindexerv1alpha1.AutoIndexerPhaseRunning, false},
		{autoindexerv1alpha1.AutoIndexerPhaseRetrying, false},
		{autoindexerv1alpha1.AutoIndexerPhaseUnknown, false},
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
