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
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/driftdetection"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/manifests"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/utils"
	"github.com/kaito-project/autoindexer/pkg/clients/ragengine"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

const (
	AutoIndexerHashAnnotation = "autoindexer.kaito.sh/hash"
	AutoIndexerNameLabel      = "autoindexer.kaito.sh/name"
)

// AutoIndexerReconciler reconciles an AutoIndexer object
type AutoIndexerReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	DriftDetector driftdetection.DriftDetector
	RAGClient     driftdetection.RAGEngineClient
}

// NewAutoIndexerReconciler creates a new AutoIndexer reconciler
func NewAutoIndexerReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, recorder record.EventRecorder) *AutoIndexerReconciler {
	// Create drift detector
	ragClient := ragengine.NewRAGEngineClient(30*time.Second, 3)

	// Configure drift detection
	driftConfig := driftdetection.DefaultDriftDetectionConfig()

	// Allow configuration via environment variables
	if intervalEnv := os.Getenv("DRIFT_DETECTION_INTERVAL"); intervalEnv != "" {
		if interval, err := time.ParseDuration(intervalEnv); err == nil {
			driftConfig.CheckInterval = interval
		}
	}

	if enabledEnv := os.Getenv("DRIFT_DETECTION_ENABLED"); enabledEnv == "false" {
		driftConfig.Enabled = false
	}

	// Create drift detector
	driftDetector := driftdetection.NewDriftDetector(
		client,
		ragClient,
		driftConfig,
		log.WithName("drift-detector"),
	)

	return &AutoIndexerReconciler{
		Client:        client,
		Scheme:        scheme,
		Log:           log,
		Recorder:      recorder,
		DriftDetector: driftDetector,
		RAGClient:     ragClient,
	}
}

//+kubebuilder:rbac:groups=autoindexer.kaito.sh,resources=autoindexers,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=autoindexer.kaito.sh,resources=autoindexers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AutoIndexerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	autoIndexerObj := &autoindexerv1alpha1.AutoIndexer{}
	if err := r.Client.Get(ctx, req.NamespacedName, autoIndexerObj); err != nil {
		if !apierrors.IsNotFound(err) {
			r.Log.Error(err, "failed to get AutoIndexer", "AutoIndexer", req.Name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	r.Log.Info("Reconciling", "AutoIndexer", req.NamespacedName)

	if autoIndexerObj.DeletionTimestamp.IsZero() {
		if err := r.ensureFinalizer(ctx, autoIndexerObj); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		r.Log.Info("AutoIndexer is being deleted, cleaning up resources", "AutoIndexer", req.NamespacedName)
		return r.deleteAutoIndexer(ctx, autoIndexerObj)
	}

	r.Log.Info("AutoIndexer is being created or updated", "AutoIndexer", req.NamespacedName)
	return r.syncAutoIndexer(ctx, autoIndexerObj)
}

func (r *AutoIndexerReconciler) ensureFinalizer(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	if !controllerutil.ContainsFinalizer(autoIndexerObj, utils.AutoIndexerFinalizer) {
		patch := client.MergeFrom(autoIndexerObj.DeepCopy())
		controllerutil.AddFinalizer(autoIndexerObj, utils.AutoIndexerFinalizer)
		if err := r.Client.Patch(ctx, autoIndexerObj, patch); err != nil {
			r.Log.Error(err, "failed to ensure the finalizer to the autoindexer", "autoindexer", klog.KObj(autoIndexerObj))
			return err
		}
	}
	return nil
}

// addAutoIndexer handles the reconciliation logic for creating/updating AutoIndexer
func (r *AutoIndexerReconciler) syncAutoIndexer(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) (ctrl.Result, error) {
	existingAutoIndexerObj := autoIndexerObj.DeepCopy()

	// Always ensure required resources exist and are up to date
	// This handles user modifications and deleted resources(rbac, service account, etc)
	if err := r.ensureRequiredResources(ctx, autoIndexerObj); err != nil {
		r.Log.Error(err, "failed to ensure required resources", "autoindexer", autoIndexerObj.Name)
		return r.patchAndReturn(ctx, autoIndexerObj, existingAutoIndexerObj, ctrl.Result{}, err)
	}

	var err error
	// Handle reconciliation based on current phase
	switch autoIndexerObj.Status.IndexingPhase {
	case autoindexerv1alpha1.AutoIndexerPhasePending:
		err = r.handlePendingPhase(ctx, autoIndexerObj)
	case autoindexerv1alpha1.AutoIndexerPhaseScheduled:
		err = r.handleScheduledPhase(ctx, autoIndexerObj)
	case autoindexerv1alpha1.AutoIndexerPhaseSuspended:
		err = r.handleSuspendedPhase(ctx, autoIndexerObj)
	case autoindexerv1alpha1.AutoIndexerPhaseRunning:
		err = r.handleRunningPhase(ctx, autoIndexerObj)
	case autoindexerv1alpha1.AutoIndexerPhaseCompleted:
		err = r.handleCompletedPhase(ctx, autoIndexerObj)
	case autoindexerv1alpha1.AutoIndexerPhaseFailed:
		err = r.handleFailedPhase(ctx, autoIndexerObj)
	case autoindexerv1alpha1.AutoIndexerPhaseDriftRemediation:
		err = r.handleDriftRemediationPhase(ctx, autoIndexerObj)
	default:
		// Unknown phase, set to Pending
		r.Log.Info("Unknown phase, setting to Pending", "AutoIndexer", autoIndexerObj.Name, "phase", autoIndexerObj.Status.IndexingPhase)
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhasePending, "PhaseReset", "Unknown phase, resetting to Pending", nil)
	}

	// If an error occurred during phase handling, log and set condition
	if err != nil {
		r.Log.Error(err, "failed to reconcile", "autoindexer", autoIndexerObj.Name, "phase", autoIndexerObj.Status.IndexingPhase)
		r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue, "ReconcileFailed", err.Error())
		return r.patchAndReturn(ctx, autoIndexerObj, existingAutoIndexerObj, ctrl.Result{}, err)
	}

	// Clear error condition on successful reconciliation
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionFalse, "ReconcileSucceeded", "")
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ReconcileSucceeded", "")
	return r.patchAndReturn(ctx, autoIndexerObj, existingAutoIndexerObj, ctrl.Result{}, nil)
}

// getJobStatus returns counts of running, failed, and completed jobs for the AutoIndexer
func (r *AutoIndexerReconciler) getJobStatus(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) (running, failed, completed int, latestJobFailed bool, err error) {
	jobs := &batchv1.JobList{}
	listOpts := []client.ListOption{
		client.InNamespace(autoIndexerObj.Namespace),
		client.MatchingLabels{
			"autoindexer.kaito.sh/name": autoIndexerObj.Name,
		},
	}

	if err := r.Client.List(ctx, jobs, listOpts...); err != nil {
		return 0, 0, 0, false, fmt.Errorf("failed to list jobs: %w", err)
	}

	latestJobFailed = false
	lastJobTimestamp := time.Time{}
	for _, job := range jobs.Items {
		if lastJobTimestamp.IsZero() && !job.Status.StartTime.IsZero() {
			lastJobTimestamp = job.Status.StartTime.Time
		}
		if job.Status.Active > 0 {
			running++
			if job.Status.StartTime != nil && job.Status.StartTime.Time.After(lastJobTimestamp) {
				latestJobFailed = false
				lastJobTimestamp = job.Status.StartTime.Time
			}
		} else if job.Status.Failed > 0 {
			failed++
			if job.Status.StartTime != nil && job.Status.StartTime.Time.After(lastJobTimestamp) {
				latestJobFailed = true
				lastJobTimestamp = job.Status.StartTime.Time
			}
		} else if job.Status.Succeeded > 0 {
			completed++
			if job.Status.StartTime != nil && job.Status.StartTime.Time.After(lastJobTimestamp) {
				latestJobFailed = false
				lastJobTimestamp = job.Status.StartTime.Time
			}
		}
	}

	return running, failed, completed, latestJobFailed, nil
}

// updateStatus updates the IndexingPhase of the AutoIndexer
func (r *AutoIndexerReconciler) updateStatus(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer, newPhase autoindexerv1alpha1.AutoIndexerPhase, reason, message string, newConditions []metav1.Condition) {
	autoIndexerObj.Status.IndexingPhase = newPhase
	for _, cond := range newConditions {
		found := false
		for i, existingCond := range autoIndexerObj.Status.Conditions {
			if existingCond.Type == cond.Type {
				autoIndexerObj.Status.Conditions[i] = cond
				found = true
				break
			}
		}
		if !found {
			autoIndexerObj.Status.Conditions = append(autoIndexerObj.Status.Conditions, cond)
		}
	}

	r.Log.Info("Updated AutoIndexer phase on struct", "AutoIndexer", autoIndexerObj.Name, "newPhase", newPhase, "reason", reason, "message", message)
}

// handlePendingPhase handles the Pending phase - determines next phase based on configuration
// Note: Resource creation is now handled in ensureRequiredResources() which runs on every reconciliation
func (r *AutoIndexerReconciler) handlePendingPhase(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	r.Log.Info("Handling Pending phase", "AutoIndexer", autoIndexerObj.Name)

	// Determine next phase based on configuration
	if autoIndexerObj.Spec.Schedule != nil {
		// Scheduled AutoIndexer - move to appropriate phase based on suspend state
		if autoIndexerObj.Spec.Suspend != nil && *autoIndexerObj.Spec.Suspend {
			r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseSuspended, "Suspended", "AutoIndexer is suspended", nil)
		} else {
			r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseScheduled, "Scheduled", "AutoIndexer is scheduled and ready", nil)
		}
	} else {
		// One-time execution - create job and move to Running phase
		if err := r.ensureJob(ctx, autoIndexerObj); err != nil {
			r.Log.Error(err, "failed to create job for one-time execution", "AutoIndexer", autoIndexerObj.Name)
			r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseFailed, "JobCreationFailed", err.Error(), []metav1.Condition{
				{
					Type:               string(autoindexerv1alpha1.AutoIndexerConditionTypeError),
					Status:             metav1.ConditionTrue,
					Reason:             "JobCreationFailed",
					Message:            "Failed to ensure indexing job",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: autoIndexerObj.Generation,
				},
				{
					Type:               string(autoindexerv1alpha1.ConditionTypeResourceStatus),
					Status:             metav1.ConditionFalse,
					Reason:             "JobCreationFailed",
					Message:            "Failed to ensure indexing job",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: autoIndexerObj.Generation,
				},
			})
			return err
		}
		// Move to Running phase as job is starting
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseRunning, "JobStarted", "Indexing job has been started", nil)
	}

	return nil
}

// handleScheduledPhase handles the Scheduled phase - monitors for new jobs and validates state
func (r *AutoIndexerReconciler) handleScheduledPhase(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	r.Log.Info("Handling Scheduled phase", "AutoIndexer", autoIndexerObj.Name)

	// Check if suspend state has changed
	if autoIndexerObj.Spec.Suspend != nil && *autoIndexerObj.Spec.Suspend {
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseSuspended, "Suspended", "AutoIndexer has been suspended", nil)
		return nil
	}

	// Check for running jobs (validation should have caught this, but double-check)
	runningJobs, _, _, _, err := r.getJobStatus(ctx, autoIndexerObj)
	if err != nil {
		r.Log.Error(err, "failed to get job status", "AutoIndexer", autoIndexerObj.Name)
		return err
	}

	if runningJobs > 0 {
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseRunning, "JobsRunning", fmt.Sprintf("%d jobs are now running", runningJobs), nil)
		return nil
	}

	if autoIndexerObj.Annotations[utils.AutoIndexerDriftDetectedAnnotation] == "true" {
		if autoIndexerObj.Spec.DriftRemediationPolicy == nil || autoIndexerObj.Spec.DriftRemediationPolicy.Strategy == autoindexerv1alpha1.DriftRemediationStrategyIgnore {
			// realistically we should never hit this since drift detection won't set the annotation if strategy is Ignore
			r.Log.Info("Drift remediation strategy is Ignore, skipping remediation", "AutoIndexer", autoIndexerObj.Name)
			return nil
		}
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseDriftRemediation, "DriftDetected", "Drift detected, entering remediation phase", nil)
	}

	return nil
}

// handleSuspendedPhase handles the Suspended phase - waits for unsuspend
func (r *AutoIndexerReconciler) handleSuspendedPhase(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	r.Log.Info("Handling Suspended phase", "AutoIndexer", autoIndexerObj.Name)

	// Check if suspend state has changed
	if autoIndexerObj.Spec.Suspend == nil || !*autoIndexerObj.Spec.Suspend {
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseScheduled, "Unsuspended", "AutoIndexer has been unsuspended", nil)
	}

	// Remain suspended, check again later
	return nil
}

// handleRunningPhase handles the Running phase - monitors job completion
func (r *AutoIndexerReconciler) handleRunningPhase(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	r.Log.Info("Handling Running phase", "AutoIndexer", autoIndexerObj.Name)

	runningJobs, failedJobs, completedJobs, latestJobFailed, err := r.getJobStatus(ctx, autoIndexerObj)
	if err != nil {
		r.Log.Error(err, "failed to get job status", "AutoIndexer", autoIndexerObj.Name)
		return err
	}

	// If no jobs are running, determine next phase
	if runningJobs == 0 {
		if failedJobs > 0 && latestJobFailed && autoIndexerObj.Spec.Schedule == nil {
			// Non-scheduled AutoIndexer with failed jobs
			r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseFailed, "JobsFailed", fmt.Sprintf("%d jobs have failed", failedJobs), nil)
		} else if completedJobs > 0 {
			if autoIndexerObj.Spec.Schedule != nil {
				// Scheduled AutoIndexer - go back to scheduled phase
				r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseScheduled, "JobsCompleted", "Jobs completed, waiting for next schedule", nil)
			} else {
				// Non-scheduled AutoIndexer - mark as completed
				r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhaseCompleted, "JobsCompleted", "All jobs completed successfully", nil)
			}
		}
	}

	return nil
}

// handleCompletedPhase handles the Completed phase - waits for reset to Pending
func (r *AutoIndexerReconciler) handleCompletedPhase(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	r.Log.Info("Handling Completed phase", "AutoIndexer", autoIndexerObj.Name)

	return nil
}

// handleFailedPhase handles the Failed phase - waits for intervention or reset
func (r *AutoIndexerReconciler) handleFailedPhase(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	r.Log.Info("Handling Failed phase", "AutoIndexer", autoIndexerObj.Name)

	return nil
}

// handleDriftRemediationPhase handles the DriftRemediation phase - orchestrates drift remediation
func (r *AutoIndexerReconciler) handleDriftRemediationPhase(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	r.Log.Info("Handling DriftRemediation phase", "AutoIndexer", autoIndexerObj.Name)
	driftDetected := autoIndexerObj.Annotations[utils.AutoIndexerDriftDetectedAnnotation]

	if autoIndexerObj.Spec.DriftRemediationPolicy != nil && autoIndexerObj.Spec.DriftRemediationPolicy.Strategy == autoindexerv1alpha1.DriftRemediationStrategyIgnore {
		r.Log.Info("Drift remediation strategy is Ignore, exiting DriftRemediation phase", "AutoIndexer", autoIndexerObj.Name)
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhasePending, "DriftRemediationIgnored", "Drift remediation strategy is Ignore, resetting to Pending", nil)
		return nil
	}

	if driftDetected != "true" {
		if autoIndexerObj.Spec.Suspend != nil && *autoIndexerObj.Spec.Suspend {
			if err := r.setSuspendedState(ctx, autoIndexerObj, false); err != nil {
				r.Log.Error(err, "failed to unsuspend AutoIndexer after drift remediation", "AutoIndexer", autoIndexerObj.Name)
				return err
			}
			r.Log.Info("AutoIndexer unsuspended after drift remediation", "AutoIndexer", autoIndexerObj.Name)
			return nil
		}
		r.Log.Info("No drift detected annotation found, exiting DriftRemediation phase", "AutoIndexer", autoIndexerObj.Name)
		r.updateStatus(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerPhasePending, "NoDriftDetected", "No drift detected, resetting to Pending", nil)
		return nil
	}

	if autoIndexerObj.Spec.DriftRemediationPolicy == nil || autoIndexerObj.Spec.DriftRemediationPolicy.Strategy == autoindexerv1alpha1.DriftRemediationStrategyManual {
		r.Log.Info("Drift remediation strategy is Manual, waiting for user intervention", "AutoIndexer", autoIndexerObj.Name)
		return nil
	}

	if autoIndexerObj.Spec.Suspend == nil || (autoIndexerObj.Spec.Suspend != nil && !*autoIndexerObj.Spec.Suspend) {
		if err := r.setSuspendedState(ctx, autoIndexerObj, true); err != nil {
			r.Log.Error(err, "failed to suspend AutoIndexer during drift remediation", "AutoIndexer", autoIndexerObj.Name)
			return err
		}
		r.Log.Info("AutoIndexer suspended during drift remediation", "AutoIndexer", autoIndexerObj.Name)
		return nil
	}

	lastRemediationTime := autoIndexerObj.Annotations[utils.AutoIndexerLastDriftRemediatedAnnotation]
	lastDetectionTime := autoIndexerObj.Annotations[utils.AutoIndexerLastDriftDetectedAnnotation]
	if lastDetectionTime != "" && lastRemediationTime != "" {
		if tDetect, err := time.Parse(time.RFC3339, lastDetectionTime); err == nil {
			if tRemediate, err := time.Parse(time.RFC3339, lastRemediationTime); err == nil {
				if tRemediate.After(tDetect) {
					// drift already remediated
					r.Log.Info("Drift was already remediated since last detection, waiting for detector to update annotations", "AutoIndexer", autoIndexerObj.Name)
					return nil
				}
			}
		}
	}

	runningJobs, _, _, _, err := r.getJobStatus(ctx, autoIndexerObj)
	if err != nil {
		r.Log.Error(err, "failed to get job status", "AutoIndexer", autoIndexerObj.Name)
		return err
	}
	if runningJobs > 0 {
		r.Log.Info("indexing job is still running", "AutoIndexer", autoIndexerObj.Name)
		return nil
	}

	indexes, err := r.RAGClient.ListIndexes(autoIndexerObj.Spec.RAGEngine, autoIndexerObj.Spec.IndexName, autoIndexerObj.Name, autoIndexerObj.Namespace)
	if err != nil {
		r.Log.Error(err, "failed to list indexes", "AutoIndexer", autoIndexerObj.Name)
		return err
	}

	indexFound := false
	for _, index := range indexes {
		if strings.EqualFold(index, autoIndexerObj.Spec.IndexName) {
			indexFound = true
			break
		}
	}

	if indexFound {
		err = r.RAGClient.DeleteIndex(autoIndexerObj.Spec.RAGEngine, autoIndexerObj.Spec.IndexName, autoIndexerObj.Namespace)
		if err != nil {
			r.Log.Error(err, "failed to delete index during drift remediation", "AutoIndexer", autoIndexerObj.Name)
			return err
		}
		r.Log.Info("Deleted drifted index", "AutoIndexer", autoIndexerObj.Name)
	}

	if autoIndexerObj.Spec.DataSource.Type == autoindexerv1alpha1.DataSourceTypeGit {
		emptyCommit := ""
		autoIndexerObj.Status.LastIndexedCommit = &emptyCommit
		autoIndexerObj.Status.NumOfDocumentInIndex = 0
		r.Log.Info("Reset LastIndexedCommit due to drift remediation", "AutoIndexer", autoIndexerObj.Name)
	}

	// Update annotation to mark drift as cleared
	// Next reconciliations will detect no drift and move back to Pending phase
	existingAutoIndexerObj := autoIndexerObj.DeepCopy()
	autoIndexerObj.Annotations[utils.AutoIndexerLastDriftRemediatedAnnotation] = time.Now().Format(time.RFC3339)
	return r.updateAnnotations(ctx, autoIndexerObj, existingAutoIndexerObj)
}

// validateRAGEngineRef validates that the referenced RAGEngine exists
func (r *AutoIndexerReconciler) validateRAGEngineRef(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	ragEngine := &kaitov1alpha1.RAGEngine{}
	ragEngineKey := client.ObjectKey{
		Name:      autoIndexerObj.Spec.RAGEngine,
		Namespace: autoIndexerObj.Namespace,
	}

	// If namespace is not specified in the ref, use the AutoIndexer's namespace
	if ragEngineKey.Namespace == "" {
		ragEngineKey.Namespace = autoIndexerObj.Namespace
	}

	if err := r.Client.Get(ctx, ragEngineKey, ragEngine); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("referenced RAGEngine %s/%s not found", ragEngineKey.Namespace, ragEngineKey.Name)
		}
		return fmt.Errorf("failed to get referenced RAGEngine: %w", err)
	}

	return nil
}

// ensureRequiredResources ensures all required resources exist and are up to date
// This runs on every reconciliation to handle user modifications and deleted resources
func (r *AutoIndexerReconciler) ensureRequiredResources(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	// Validate that referenced RAGEngine exists
	if err := r.validateRAGEngineRef(ctx, autoIndexerObj); err != nil {
		r.Log.Error(err, "RAGEngine validation failed", "AutoIndexer", autoIndexerObj.Name)
		r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue, "RAGEngineNotFound", err.Error())
		r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionFalse, "RAGEngineNotFound", "Referenced RAGEngine not found")
		return err
	}

	// Always ensure RBAC resources exist and are up to date
	if err := r.ensureRBACResources(ctx, autoIndexerObj); err != nil {
		r.Log.Error(err, "RBAC resources failed", "AutoIndexer", autoIndexerObj.Name)
		r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue, "RBACFailed", err.Error())
		r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionFalse, "RBACFailed", "Failed to ensure RBAC resources")
		return err
	}

	// Ensure Job/CronJob resources based on schedule configuration
	if autoIndexerObj.Spec.Schedule != nil {
		// Ensure CronJob exists and is up to date
		if err := r.ensureCronJob(ctx, autoIndexerObj); err != nil {
			r.Log.Error(err, "CronJob resources failed", "AutoIndexer", autoIndexerObj.Name)
			r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionTrue, "CronJobFailed", err.Error())
			r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionFalse, "CronJobFailed", "Failed to ensure CronJob resources")
			return err
		}
	}

	// Clear any previous resource-related error conditions
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeError, metav1.ConditionFalse, "ResourcesEnsured", "")
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesEnsured", "All required resources are ensured")

	return nil
}

// ensureRBACResources creates or updates RBAC resources for AutoIndexer jobs
func (r *AutoIndexerReconciler) ensureRBACResources(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	// Ensure ServiceAccount
	if err := r.ensureServiceAccount(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to ensure ServiceAccount: %w", err)
	}

	// Ensure Role
	if err := r.ensureRole(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to ensure Role: %w", err)
	}

	// Ensure RoleBinding
	if err := r.ensureRoleBinding(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to ensure RoleBinding: %w", err)
	}

	return nil
}

// ensureServiceAccount creates or updates a ServiceAccount for AutoIndexer jobs
func (r *AutoIndexerReconciler) ensureServiceAccount(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	serviceAccount := manifests.GenerateServiceAccountManifest(autoIndexerObj)

	// Check if ServiceAccount already exists
	existingSA := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: serviceAccount.Name, Namespace: serviceAccount.Namespace}, existingSA)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the ServiceAccount
			r.Log.Info("Creating ServiceAccount", "serviceaccount", serviceAccount.Name, "namespace", serviceAccount.Namespace, "autoindexer", autoIndexerObj.Name)
			return r.Create(ctx, serviceAccount)
		}
		return err
	}

	// Update the existing ServiceAccount if needed
	if !hasOwnerReference(existingSA, autoIndexerObj) {
		r.Log.Info("Updating ServiceAccount", "serviceaccount", serviceAccount.Name, "namespace", serviceAccount.Namespace, "autoindexer", autoIndexerObj.Name)
		existingSA.OwnerReferences = serviceAccount.OwnerReferences
		existingSA.Labels = serviceAccount.Labels
		return r.Update(ctx, existingSA)
	}

	return nil
}

// ensureRole creates or updates a Role for AutoIndexer jobs
func (r *AutoIndexerReconciler) ensureRole(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	role := manifests.GenerateRoleManifest(autoIndexerObj)

	// Check if Role already exists
	existingRole := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, existingRole)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the Role
			r.Log.Info("Creating Role", "role", role.Name, "namespace", role.Namespace, "autoindexer", autoIndexerObj.Name)
			return r.Create(ctx, role)
		}
		return err
	}

	// Update the existing Role if needed
	if !reflect.DeepEqual(existingRole.Rules, role.Rules) || !hasOwnerReference(existingRole, autoIndexerObj) {
		r.Log.Info("Updating Role", "role", role.Name, "namespace", role.Namespace, "autoindexer", autoIndexerObj.Name)
		existingRole.Rules = role.Rules
		existingRole.OwnerReferences = role.OwnerReferences
		existingRole.Labels = role.Labels
		return r.Update(ctx, existingRole)
	}

	return nil
}

// ensureRoleBinding creates or updates a RoleBinding for AutoIndexer jobs
func (r *AutoIndexerReconciler) ensureRoleBinding(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	roleBinding := manifests.GenerateRoleBindingManifest(autoIndexerObj)

	// Check if RoleBinding already exists
	existingRB := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, existingRB)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the RoleBinding
			r.Log.Info("Creating RoleBinding", "rolebinding", roleBinding.Name, "namespace", roleBinding.Namespace, "autoindexer", autoIndexerObj.Name)
			return r.Create(ctx, roleBinding)
		}
		return err
	}

	// Update the existing RoleBinding if needed
	if !reflect.DeepEqual(existingRB.Subjects, roleBinding.Subjects) ||
		!reflect.DeepEqual(existingRB.RoleRef, roleBinding.RoleRef) ||
		!hasOwnerReference(existingRB, autoIndexerObj) {
		r.Log.Info("Updating RoleBinding", "rolebinding", roleBinding.Name, "namespace", roleBinding.Namespace, "autoindexer", autoIndexerObj.Name)
		existingRB.Subjects = roleBinding.Subjects
		existingRB.RoleRef = roleBinding.RoleRef
		existingRB.OwnerReferences = roleBinding.OwnerReferences
		existingRB.Labels = roleBinding.Labels
		return r.Update(ctx, existingRB)
	}

	return nil
}

// ensureCronJob creates or updates a CronJob for scheduled indexing
func (r *AutoIndexerReconciler) ensureCronJob(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	// Generate the CronJob manifest
	config := manifests.JobConfig{
		AutoIndexer:        autoIndexerObj,
		JobName:            fmt.Sprintf("%s-cronjob", autoIndexerObj.Name),
		JobType:            "scheduled-indexing",
		Image:              manifests.GetJobImageConfig().GetImage(),
		ImagePullPolicy:    "Always",
		ServiceAccountName: manifests.GenerateServiceAccountName(autoIndexerObj),
	}

	cronJob := manifests.GenerateIndexingCronJobManifest(config)

	// Set the AutoIndexer as the owner of the CronJob
	if err := controllerutil.SetControllerReference(autoIndexerObj, cronJob, r.Scheme); err != nil {
		return err
	}

	// Check if CronJob already exists
	existingCronJob := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: cronJob.Name, Namespace: cronJob.Namespace}, existingCronJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the CronJob
			r.Log.Info("Creating CronJob", "cronjob", cronJob.Name, "namespace", cronJob.Namespace, "autoindexer", autoIndexerObj.Name)
			return r.Create(ctx, cronJob)
		}
		return err
	}

	// Update the existing CronJob if needed
	if !equalCronJobs(existingCronJob, cronJob) {
		r.Log.Info("Updating CronJob", "cronjob", cronJob.Name, "namespace", cronJob.Namespace, "autoindexer", autoIndexerObj.Name)
		existingCronJob.Spec = cronJob.Spec
		return r.Update(ctx, existingCronJob)
	}

	return nil
}

// ensureJob creates or updates a Job for one-time indexing
func (r *AutoIndexerReconciler) ensureJob(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	// Generate the Job manifest
	config := manifests.JobConfig{
		AutoIndexer:        autoIndexerObj,
		JobName:            fmt.Sprintf("%s-job", autoIndexerObj.Name),
		JobType:            "one-time-indexing",
		Image:              manifests.GetJobImageConfig().GetImage(),
		ImagePullPolicy:    "Always",
		ServiceAccountName: manifests.GenerateServiceAccountName(autoIndexerObj),
	}

	job := manifests.GenerateIndexingJobManifest(config)

	// Set the AutoIndexer as the owner of the Job
	if err := controllerutil.SetControllerReference(autoIndexerObj, job, r.Scheme); err != nil {
		return err
	}

	// Check if Job already exists
	existingJob := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, existingJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the Job
			r.Log.Info("Creating Job", "job", job.Name, "namespace", job.Namespace, "autoindexer", autoIndexerObj.Name)
			return r.Create(ctx, job)
		}
		return err
	}

	// Jobs are immutable once created, so we don't update existing ones
	r.Log.Info("Job already exists", "job", existingJob.Name, "namespace", existingJob.Namespace, "autoindexer", autoIndexerObj.Name)
	return nil
}

// deleteAutoIndexer handles cleanup when AutoIndexer is being deleted
func (r *AutoIndexerReconciler) deleteAutoIndexer(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) (ctrl.Result, error) {
	r.Log.Info("Deleting AutoIndexer", "autoindexer", autoIndexerObj.Name, "namespace", autoIndexerObj.Namespace)

	return r.garbageCollectAutoIndexer(ctx, autoIndexerObj)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutoIndexerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("AutoIndexer")

	// Add a hook to start drift detection when the manager starts
	if err := mgr.Add(&driftDetectorRunnable{
		driftDetector: r.DriftDetector,
		logger:        r.Log.WithName("drift-detector-runnable"),
	}); err != nil {
		return fmt.Errorf("failed to add drift detector runnable: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&autoindexerv1alpha1.AutoIndexer{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5}).
		Complete(r)
}

// driftDetectorRunnable implements manager.Runnable to start drift detection
type driftDetectorRunnable struct {
	driftDetector driftdetection.DriftDetector
	logger        logr.Logger
}

func (d *driftDetectorRunnable) Start(ctx context.Context) error {
	d.logger.Info("Starting drift detector")
	return d.driftDetector.Start(ctx.Done())
}

// equalCronJobs compares two CronJob specs for equality
func equalCronJobs(existing, desired *batchv1.CronJob) bool {
	return reflect.DeepEqual(existing.Spec, desired.Spec)
}

// hasOwnerReference checks if the resource has an owner reference to the given object
func hasOwnerReference(obj metav1.Object, owner metav1.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}
