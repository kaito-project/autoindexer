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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
)

// NewDriftDetector creates a new drift detector
func NewDriftDetector(
	client client.Client,
	ragClient RAGEngineClient,
	config DriftDetectionConfig,
	logger logr.Logger,
) DriftDetector {
	return &DriftDetectorImpl{
		client:    client,
		ragClient: ragClient,
		config:    config,
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
}

// Start begins the drift detection process
func (d *DriftDetectorImpl) Start(stopCh <-chan struct{}) error {
	if !d.config.Enabled {
		d.logger.Info("Drift detection is disabled")
		return nil
	}

	d.logger.Info("Starting drift detection", "interval", d.config.CheckInterval)
	d.ticker = time.NewTicker(d.config.CheckInterval)

	go d.run(stopCh)
	return nil
}

// Stop stops the drift detection process
func (d *DriftDetectorImpl) Stop() error {
	d.logger.Info("Stopping drift detection")

	if d.ticker != nil {
		d.ticker.Stop()
	}

	close(d.stopCh)
	return nil
}

// run is the main drift detection loop
func (d *DriftDetectorImpl) run(stopCh <-chan struct{}) {
	defer func() {
		if d.ticker != nil {
			d.ticker.Stop()
		}
	}()

	// Run initial check
	d.performDriftCheck()

	for {
		select {
		case <-d.ticker.C:
			d.performDriftCheck()
		case <-stopCh:
			d.logger.Info("Drift detection stopped")
			return
		case <-d.stopCh:
			d.logger.Info("Drift detection stopped via internal signal")
			return
		}
	}
}

// performDriftCheck checks all AutoIndexer objects for drift
func (d *DriftDetectorImpl) performDriftCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), d.config.RequestTimeout*2)
	defer cancel()

	d.logger.V(1).Info("Performing drift check")

	// List all AutoIndexer objects
	autoIndexers := &autoindexerv1alpha1.AutoIndexerList{}
	if err := d.client.List(ctx, autoIndexers); err != nil {
		d.logger.Error(err, "Failed to list AutoIndexer objects")
		return
	}

	d.logger.V(1).Info("Found AutoIndexer objects", "count", len(autoIndexers.Items))

	// Check each AutoIndexer for drift
	for _, autoIndexer := range autoIndexers.Items {
		result := d.checkAutoIndexerDrift(ctx, &autoIndexer)

		// Log the result
		if result.Error != nil {
			d.logger.Error(result.Error, "Drift check failed",
				"autoindexer", result.AutoIndexerName,
				"namespace", result.AutoIndexerNamespace)
		} else if result.DriftDetected {
			d.logger.Info("Drift detected",
				"autoindexer", result.AutoIndexerName,
				"namespace", result.AutoIndexerNamespace,
				"expected", result.ExpectedCount,
				"actual", result.ActualCount,
				"action", result.Action)
		} else {
			d.logger.Info("No drift detected",
				"autoindexer", result.AutoIndexerName,
				"namespace", result.AutoIndexerNamespace,
				"count", result.ActualCount)
		}

		// Call the reconciler function if drift is detected
		if result.DriftDetected {
			if err := d.setDriftDetected(ctx, &autoIndexer); err != nil {
				d.logger.Error(err, "Failed to set drift detected annotation for AutoIndexer",
					"autoindexer", result.AutoIndexerName,
					"namespace", result.AutoIndexerNamespace)
			}
		}
	}
}

// checkAutoIndexerDrift checks a single AutoIndexer for drift
func (d *DriftDetectorImpl) checkAutoIndexerDrift(ctx context.Context, autoIndexer *autoindexerv1alpha1.AutoIndexer) DriftDetectionResult {
	result := DriftDetectionResult{
		AutoIndexerName:      autoIndexer.Name,
		AutoIndexerNamespace: autoIndexer.Namespace,
		ExpectedCount:        autoIndexer.Status.NumOfDocumentInIndex,
		DriftDetected:        false,
		Action:               DriftActionNone,
	}

	// Skip if AutoIndexer is not in a stable state
	if !d.isAutoIndexerInStableState(autoIndexer) {
		d.logger.V(1).Info("Skipping drift check for AutoIndexer not in stable state",
			"autoindexer", autoIndexer.Name,
			"namespace", autoIndexer.Namespace,
			"phase", autoIndexer.Status.IndexingPhase)
		return result
	}

	if autoIndexer.Status.NumOfDocumentInIndex == 0 {
		d.logger.V(1).Info("Skipping drift check for AutoIndexer with no indexed documents",
			"autoindexer", autoIndexer.Name,
			"namespace", autoIndexer.Namespace)
		return result
	}

	// Skip if AutoIndexer is suspended
	if autoIndexer.Spec.Suspend != nil && *autoIndexer.Spec.Suspend {
		d.logger.V(1).Info("Skipping drift check for suspended AutoIndexer",
			"autoindexer", autoIndexer.Name,
			"namespace", autoIndexer.Namespace)
		return result
	}

	// Get actual document count from RAG engine
	actualCount, err := d.ragClient.GetDocumentCount(autoIndexer.Spec.RAGEngine, autoIndexer.Spec.IndexName, autoIndexer.Name, autoIndexer.Namespace)
	if err != nil {
		result.Error = fmt.Errorf("failed to get document count from RAG engine: %w", err)
		return result
	}

	result.ActualCount = actualCount

	// Check for drift
	if result.ExpectedCount != actualCount {
		d.logger.V(1).Info("Document count changed, drift detected",
			"autoindexer", autoIndexer.Name,
			"namespace", autoIndexer.Namespace,
			"expected", result.ExpectedCount,
			"actual", actualCount)

		result.DriftDetected = true
		result.Action = d.determineDriftAction(autoIndexer)
	}

	return result
}

// isAutoIndexerInStableState checks if the AutoIndexer is in a stable state for drift checking
func (d *DriftDetectorImpl) isAutoIndexerInStableState(autoIndexer *autoindexerv1alpha1.AutoIndexer) bool {
	switch autoIndexer.Status.IndexingPhase {
	case autoindexerv1alpha1.AutoIndexerPhaseScheduled:
		return true
	case autoindexerv1alpha1.AutoIndexerPhasePending,
		autoindexerv1alpha1.AutoIndexerPhaseRunning,
		autoindexerv1alpha1.AutoIndexerPhaseSuspended,
		autoindexerv1alpha1.AutoIndexerPhaseDriftRemediation,
		autoindexerv1alpha1.AutoIndexerPhaseFailed,
		autoindexerv1alpha1.AutoIndexerPhaseCompleted:
		// Skip actively running or pending indexers
		return false
	default:
		// Unknown phase - skip for safety
		return false
	}
}

// determineDriftAction determines what action to take when drift is detected
func (d *DriftDetectorImpl) determineDriftAction(autoIndexer *autoindexerv1alpha1.AutoIndexer) DriftAction {
	// Create a job to remediate drift
	return DriftActionTriggerJob
}

func (d *DriftDetectorImpl) setDriftDetected(ctx context.Context, autoIndexer *autoindexerv1alpha1.AutoIndexer) error {
	// Add annotation to indicate drift remediation is in progress
	if autoIndexer.Annotations == nil {
		autoIndexer.Annotations = make(map[string]string)
	}
	autoIndexer.Annotations["autoindexer.kaito.sh/drift-detected"] = "true"
	err := d.client.Update(ctx, autoIndexer)
	if err != nil {
		return fmt.Errorf("failed to set annotation for AutoIndexer: %w", err)
	}
	return nil
}
