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
	"os"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/manifests"
)

// DriftReconciler handles reconciliation of drift detection results
type DriftReconciler struct {
	client client.Client
	logger logr.Logger
}

// NewDriftReconciler creates a new drift reconciler
func NewDriftReconciler(client client.Client, logger logr.Logger) *DriftReconciler {
	return &DriftReconciler{
		client: client,
		logger: logger,
	}
}

// ReconcileDrift handles a drift detection result
func (r *DriftReconciler) ReconcileDrift(result DriftDetectionResult) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch result.Action {
	case DriftActionTriggerJob:
		return r.triggerJob(ctx, result)
	case DriftActionUpdateStatus:
		return r.updateStatus(ctx, result)
	case DriftActionNone:
		return nil
	default:
		return fmt.Errorf("unknown drift action: %s", result.Action)
	}
}

// triggerJob creates a new indexing job for one-time AutoIndexers
func (r *DriftReconciler) triggerJob(ctx context.Context, result DriftDetectionResult) error {
	// Get the AutoIndexer object
	autoIndexer := &autoindexerv1alpha1.AutoIndexer{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Name:      result.AutoIndexerName,
		Namespace: result.AutoIndexerNamespace,
	}, autoIndexer); err != nil {
		return fmt.Errorf("failed to get AutoIndexer: %w", err)
	}

	// For scheduled AutoIndexers, suspend them during drift remediation
	var originalSuspendState bool
	if autoIndexer.Spec.Schedule != nil {
		// Store original suspend state
		if autoIndexer.Spec.Suspend != nil {
			originalSuspendState = *autoIndexer.Spec.Suspend
		} else {
			originalSuspendState = false
		}

		// Only suspend if not already suspended
		if !originalSuspendState {
			suspend := true
			autoIndexer.Spec.Suspend = &suspend

			// Add annotation to track the original state and that we need to unsuspend after remediation
			if autoIndexer.Annotations == nil {
				autoIndexer.Annotations = make(map[string]string)
			}
			autoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation-suspended"] = "true"
			r.logger.Info("Suspended AutoIndexer for drift remediation",
				"autoindexer", result.AutoIndexerName,
				"namespace", result.AutoIndexerNamespace,
				"original-suspend-state", originalSuspendState)
		}
	}

	// Add annotation to track the autoindexer is under drift remediation
	if autoIndexer.Annotations == nil {
		autoIndexer.Annotations = make(map[string]string)
	}
	autoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation"] = "true"

	// Update the AutoIndexer to suspend it
	if err := r.client.Update(ctx, autoIndexer); err != nil {
		return fmt.Errorf("failed to update AutoIndexer during drift remediation: %w", err)
	}

	// Generate a unique job name with timestamp
	timestamp := time.Now().Format("20060102-150405")
	jobName := fmt.Sprintf("%s-drift-remediation-%s", autoIndexer.Name, timestamp)

	// Generate the Job manifest
	config := manifests.JobConfig{
		AutoIndexer:        autoIndexer,
		JobName:            jobName,
		JobType:            "drift-remediation",
		Image:              r.getImageConfig().GetImage(),
		ImagePullPolicy:    "Always",
		ServiceAccountName: manifests.GenerateServiceAccountName(autoIndexer),
	}

	job := manifests.GenerateIndexingJobManifest(config)

	// Add drift remediation specific labels
	if job.Labels == nil {
		job.Labels = make(map[string]string)
	}
	job.Labels["autoindexer.kaito.sh/drift-remediation"] = "true"

	// Add the same labels to the pod template
	if job.Spec.Template.Labels == nil {
		job.Spec.Template.Labels = make(map[string]string)
	}
	job.Spec.Template.Labels["autoindexer.kaito.sh/drift-remediation"] = "true"

	// Set the AutoIndexer as the owner of the Job
	if err := controllerutil.SetControllerReference(autoIndexer, job, r.client.Scheme()); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the job
	if err := r.client.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create drift remediation job: %w", err)
	}

	r.logger.Info("Created drift remediation job",
		"job", jobName,
		"autoindexer", result.AutoIndexerName,
		"namespace", result.AutoIndexerNamespace,
		"expected-count", result.ExpectedCount,
		"actual-count", result.ActualCount)

	// Update the AutoIndexer status to indicate drift remediation (best effort)
	if err := r.updateStatusWithDriftRemediation(ctx, autoIndexer, result); err != nil {
		r.logger.Error(err, "failed to update AutoIndexer status for drift remediation, but job creation succeeded",
			"autoindexer", result.AutoIndexerName,
			"namespace", result.AutoIndexerNamespace)
		// Don't fail the whole operation if only the status update fails
	}

	return nil
}

// updateStatus updates the AutoIndexer status to reflect detected drift
func (r *DriftReconciler) updateStatus(ctx context.Context, result DriftDetectionResult) error {
	// Get the AutoIndexer object
	autoIndexer := &autoindexerv1alpha1.AutoIndexer{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Name:      result.AutoIndexerName,
		Namespace: result.AutoIndexerNamespace,
	}, autoIndexer); err != nil {
		return fmt.Errorf("failed to get AutoIndexer: %w", err)
	}

	existingAutoIndexer := autoIndexer.DeepCopy()
	// Update status to reflect the actual count
	autoIndexer.Status.NumOfDocumentInIndex = result.ActualCount

	// Add or update drift condition
	r.updateDriftCondition(autoIndexer, result)

	// Update the status
	if err := r.client.Status().Patch(ctx, autoIndexer, client.MergeFrom(existingAutoIndexer)); err != nil {
		return fmt.Errorf("failed to update AutoIndexer status: %w", err)
	}

	r.logger.Info("Updated AutoIndexer status with drift information",
		"autoindexer", result.AutoIndexerName,
		"namespace", result.AutoIndexerNamespace,
		"expected-count", result.ExpectedCount,
		"actual-count", result.ActualCount)

	return nil
}

// updateStatusWithDriftRemediation updates the status for drift remediation jobs
func (r *DriftReconciler) updateStatusWithDriftRemediation(ctx context.Context, autoIndexer *autoindexerv1alpha1.AutoIndexer, result DriftDetectionResult) error {
	// Refetch the AutoIndexer to get the latest version after spec updates
	freshAutoIndexer := &autoindexerv1alpha1.AutoIndexer{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Name:      result.AutoIndexerName,
		Namespace: result.AutoIndexerNamespace,
	}, freshAutoIndexer); err != nil {
		return fmt.Errorf("failed to get fresh AutoIndexer for status update: %w", err)
	}

	existingFreshAutoIndexer := freshAutoIndexer.DeepCopy()

	// Update indexing phase to indicate drift remediation
	freshAutoIndexer.Status.IndexingPhase = autoindexerv1alpha1.AutoIndexerPhaseRunning

	// Add drift remediation condition
	r.addDriftRemediationCondition(freshAutoIndexer, result)

	// Update the status
	if err := r.client.Status().Patch(ctx, freshAutoIndexer, client.MergeFrom(existingFreshAutoIndexer)); err != nil {
		return fmt.Errorf("failed to update AutoIndexer status for drift remediation: %w", err)
	}

	return nil
}

// updateDriftCondition adds or updates the drift detection condition
func (r *DriftReconciler) updateDriftCondition(autoIndexer *autoindexerv1alpha1.AutoIndexer, result DriftDetectionResult) {
	condition := metav1.Condition{
		Type:               string(autoindexerv1alpha1.AutoIndexerConditionTypeDriftDetected),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "DocumentCountMismatch",
		Message: fmt.Sprintf("Document count drift detected: expected %d, actual %d",
			result.ExpectedCount, result.ActualCount),
	}

	// Find and update existing condition or append new one
	updated := false
	for i := range autoIndexer.Status.Conditions {
		if autoIndexer.Status.Conditions[i].Type == condition.Type {
			autoIndexer.Status.Conditions[i] = condition
			updated = true
			break
		}
	}

	if !updated {
		autoIndexer.Status.Conditions = append(autoIndexer.Status.Conditions, condition)
	}
}

// addDriftRemediationCondition adds a condition indicating drift remediation is in progress
func (r *DriftReconciler) addDriftRemediationCondition(autoIndexer *autoindexerv1alpha1.AutoIndexer, result DriftDetectionResult) {
	condition := metav1.Condition{
		Type:               string(autoindexerv1alpha1.AutoIndexerConditionTypeDriftRemediation),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "RemediationJobCreated",
		Message: fmt.Sprintf("Drift remediation job created due to document count mismatch: expected %d, actual %d",
			result.ExpectedCount, result.ActualCount),
	}

	// Find and update existing condition or append new one
	updated := false
	for i := range autoIndexer.Status.Conditions {
		if autoIndexer.Status.Conditions[i].Type == condition.Type {
			autoIndexer.Status.Conditions[i] = condition
			updated = true
			break
		}
	}

	if !updated {
		autoIndexer.Status.Conditions = append(autoIndexer.Status.Conditions, condition)
	}
}

// getImageConfig returns the image configuration for jobs
func (r *DriftReconciler) getImageConfig() ImageConfig {
	return ImageConfig{
		RegistryName: getEnv("PRESET_AUTO_INDEXER_REGISTRY_NAME", "mcr.microsoft.com/aks/kaito"),
		ImageName:    getEnv("PRESET_AUTO_INDEXER_IMAGE_NAME", "kaito-autoindexer"),
		ImageTag:     getEnv("PRESET_AUTO_INDEXER_IMAGE_TAG", "0.6.0"),
	}
}

// Helper structs and functions (duplicated from controller for simplicity)
type ImageConfig struct {
	RegistryName string
	ImageName    string
	ImageTag     string
}

func (ic ImageConfig) GetImage() string {
	return fmt.Sprintf("%s/%s:%s", ic.RegistryName, ic.ImageName, ic.ImageTag)
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
