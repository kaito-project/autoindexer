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
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DriftDetectionConfig holds configuration for the drift detection system
type DriftDetectionConfig struct {
	// CheckInterval defines how often to check for drift (default: 5 minutes)
	CheckInterval time.Duration
	// Enabled determines whether drift detection is active
	Enabled bool
	// MaxRetries defines maximum retries for RAG engine requests
	MaxRetries int
	// RequestTimeout defines timeout for RAG engine requests
	RequestTimeout time.Duration
}

// DefaultDriftDetectionConfig returns default configuration
func DefaultDriftDetectionConfig() DriftDetectionConfig {
	return DriftDetectionConfig{
		CheckInterval:  5 * time.Minute,
		Enabled:        true,
		MaxRetries:     3,
		RequestTimeout: 30 * time.Second,
	}
}

// RAGEngineClient defines the interface for interacting with RAG engines
type RAGEngineClient interface {
	// GetDocumentCount returns the number of documents in the given index
	// filtered by the autoindexer metadata
	GetDocumentCount(ragEngineEndpoint, indexName, autoindexerName, autoIndexerNamespace string) (int32, error)
}

// DriftDetector defines the interface for drift detection
type DriftDetector interface {
	// Start begins the drift detection process
	Start(stopCh <-chan struct{}) error
	// Stop stops the drift detection process
	Stop() error
}

// DriftAction represents the action to take when drift is detected
type DriftAction string

const (
	// DriftActionTriggerJob triggers a new indexing job for one-time AutoIndexers
	DriftActionTriggerJob DriftAction = "TriggerJob"
	// DriftActionNone no action needed
	DriftActionNone DriftAction = "None"
)

// DriftDetectionResult represents the result of drift detection for an AutoIndexer
type DriftDetectionResult struct {
	// AutoIndexerName is the name of the AutoIndexer
	AutoIndexerName string
	// AutoIndexerNamespace is the namespace of the AutoIndexer
	AutoIndexerNamespace string
	// ExpectedCount is the count from AutoIndexer status
	ExpectedCount int32
	// ActualCount is the count from RAG engine
	ActualCount int32
	// DriftDetected indicates if drift was detected
	DriftDetected bool
	// Action is the recommended action to take
	Action DriftAction
	// Error contains any error that occurred during detection
	Error error
}

// DriftDetectorImpl implements the DriftDetector interface
type DriftDetectorImpl struct {
	client         client.Client
	ragClient      RAGEngineClient
	config         DriftDetectionConfig
	logger         logr.Logger
	stopCh         chan struct{}
	ticker         *time.Ticker
	reconcilerFunc func(result DriftDetectionResult) error
}
