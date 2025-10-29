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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
)

// updateAutoIndexerStatus updates the AutoIndexer status based on job/cronjob states
func (r *AutoIndexerReconciler) updateAutoIndexerStatus(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	// Update next scheduled run for CronJobs
	if autoIndexerObj.Spec.Schedule != nil {
		if err := r.updateNextScheduledRun(ctx, autoIndexerObj); err != nil {
			klog.ErrorS(err, "failed to update next scheduled run", "autoindexer", klog.KObj(autoIndexerObj))
		}
	}

	return nil
}

// updateNextScheduledRun calculates and updates the next scheduled run time
func (r *AutoIndexerReconciler) updateNextScheduledRun(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	// Get CronJob
	cronJobs := &batchv1.CronJobList{}
	if err := r.Client.List(ctx, cronJobs, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{
		AutoIndexerNameLabel: autoIndexerObj.Name,
	}); err != nil {
		return err
	}

	if len(cronJobs.Items) > 0 {
		cronJob := cronJobs.Items[0]
		if cronJob.Status.LastScheduleTime != nil {
			// Calculate next run based on schedule
			// This is a simplified calculation - in practice, you'd want to use a proper cron parser
			nextRun := cronJob.Status.LastScheduleTime.Add(time.Hour) // Placeholder logic
			autoIndexerObj.Status.NextScheduledIndexing = &metav1.Time{Time: nextRun}
		}
	}

	return nil
}

// setAutoIndexerCondition sets a condition on the AutoIndexer status
func (r *AutoIndexerReconciler) setAutoIndexerCondition(autoIndexerObj *autoindexerv1alpha1.AutoIndexer, conditionType autoindexerv1alpha1.ConditionType, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	// Find and update existing condition or append new one
	for i, existingCondition := range autoIndexerObj.Status.Conditions {
		if existingCondition.Type == condition.Type {
			// Only update if the status changed to avoid unnecessary updates
			if existingCondition.Status != condition.Status {
				autoIndexerObj.Status.Conditions[i] = condition
			}
			return
		}
	}

	// Add new condition
	autoIndexerObj.Status.Conditions = append(autoIndexerObj.Status.Conditions, condition)
}

// getAutoIndexerCondition gets a condition by type from the AutoIndexer status
func (r *AutoIndexerReconciler) getAutoIndexerCondition(autoIndexerObj *autoindexerv1alpha1.AutoIndexer, conditionType autoindexerv1alpha1.ConditionType) *metav1.Condition {
	for _, condition := range autoIndexerObj.Status.Conditions {
		if condition.Type == string(conditionType) {
			return &condition
		}
	}
	return nil
}

// isAutoIndexerReady checks if the AutoIndexer is in a ready state
func (r *AutoIndexerReconciler) isAutoIndexerReady(autoIndexerObj *autoindexerv1alpha1.AutoIndexer) bool {
	condition := r.getAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

// recordAutoIndexerEvent records an event for the AutoIndexer
func (r *AutoIndexerReconciler) recordAutoIndexerEvent(autoIndexerObj *autoindexerv1alpha1.AutoIndexer, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(autoIndexerObj, eventType, reason, message)
	}
}

// handleJobFailure handles job failure scenarios
func (r *AutoIndexerReconciler) handleJobFailure(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer, job *batchv1.Job, err error) error {
	// Record failure
	errorMessage := fmt.Sprintf("Indexing job %s failed: %v", job.Name, err)
	r.recordAutoIndexerEvent(autoIndexerObj, "Warning", "JobFailed", errorMessage)

	// Update condition
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue, "JobFailed", errorMessage)

	return nil
}

// handleJobSuccess handles successful job completion
func (r *AutoIndexerReconciler) handleJobSuccess(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer, job *batchv1.Job) error {
	// Record success
	successMessage := fmt.Sprintf("Indexing job %s completed successfully", job.Name)
	r.recordAutoIndexerEvent(autoIndexerObj, "Normal", "JobSucceeded", successMessage)

	// Update condition
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeSucceeded, metav1.ConditionTrue, "JobSucceeded", successMessage)
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionFalse, "JobSucceeded", "")

	return nil
}
