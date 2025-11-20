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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/driftdetection"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/manifests"
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
}

// NewAutoIndexerReconciler creates a new AutoIndexer reconciler
func NewAutoIndexerReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, recorder record.EventRecorder) *AutoIndexerReconciler {
	// Create drift detector
	ragClient := driftdetection.NewRAGEngineClient(30*time.Second, 3)

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

	reconciler := &AutoIndexerReconciler{
		Client:        client,
		Scheme:        scheme,
		Log:           log,
		Recorder:      recorder,
		DriftDetector: driftDetector,
	}

	return reconciler
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

	if !autoIndexerObj.DeletionTimestamp.IsZero() {
		r.Log.Info("AutoIndexer is being deleted, cleaning up resources", "AutoIndexer", req.NamespacedName)
		return r.deleteAutoIndexer(ctx, autoIndexerObj)
	}

	r.Log.Info("AutoIndexer is being created or updated", "AutoIndexer", req.NamespacedName)
	result, err := r.addAutoIndexer(ctx, autoIndexerObj)
	if err != nil {
		return result, err
	}

	return result, nil
}

// addAutoIndexer handles the reconciliation logic for creating/updating AutoIndexer
func (r *AutoIndexerReconciler) addAutoIndexer(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) (ctrl.Result, error) {
	// Validate that referenced RAGEngine exists
	r.Log.Info("Validating referenced RAGEngine", "AutoIndexer", autoIndexerObj.Name)
	if err := r.validateRAGEngineRef(ctx, autoIndexerObj); err != nil {
		if updateErr := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue,
			"autoIndexerRAGEngineNotFound", err.Error()); updateErr != nil {
			r.Log.Error(updateErr, "failed to update autoindexer resourceready status", "autoindexer", autoIndexerObj.Name)
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: time.Minute * 5}, err
	}

	// Ensure RBAC resources exist
	r.Log.Info("Ensuring RBAC resources", "AutoIndexer", autoIndexerObj.Name)
	if err := r.ensureRBACResources(ctx, autoIndexerObj); err != nil {
		if updateErr := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue,
			"autoIndexerEnsureRBACFailed", err.Error()); updateErr != nil {
			r.Log.Error(updateErr, "failed to update autoindexer resourceready status", "autoindexer", autoIndexerObj.Name)
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, err
	}

	// Handle scheduled vs one-time execution
	if autoIndexerObj.Spec.Schedule != nil {
		// Handle scheduled execution (CronJob)
		r.Log.Info("Handling scheduled execution (CronJob)", "AutoIndexer", autoIndexerObj.Name)
		if err := r.ensureCronJob(ctx, autoIndexerObj); err != nil {
			if updateErr := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue,
				"autoIndexerEnsureCronJobFailed", err.Error()); updateErr != nil {
				r.Log.Error(updateErr, "failed to update autoindexer status", "autoindexer", autoIndexerObj.Name)
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, err
		}

		isScheduled := metav1.ConditionTrue
		if autoIndexerObj.Spec.Suspend != nil && *autoIndexerObj.Spec.Suspend {
			isScheduled = metav1.ConditionFalse
		}
		if err := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeScheduled, isScheduled,
			"Scheduled", "AutoIndexer is scheduled successfully"); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// Handle one-time execution (Job)
		r.Log.Info("Handling one-time execution (Job)", "AutoIndexer", autoIndexerObj.Name)
		if err := r.ensureJob(ctx, autoIndexerObj); err != nil {
			if updateErr := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue,
				"autoIndexerEnsureJobFailed", err.Error()); updateErr != nil {
				r.Log.Error(updateErr, "failed to update autoindexer status", "autoindexer", autoIndexerObj.Name)
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, err
		}
	}

	r.Log.Info("Checking if drift remediation job need to be created", "AutoIndexer", autoIndexerObj.Name, "namespace", autoIndexerObj.Namespace)
	if err := r.handleDriftRemediationJobCreation(ctx, autoIndexerObj); err != nil {
		r.Log.Error(err, "failed to handle drift remediation job creation", "autoindexer", autoIndexerObj.Name)
		return ctrl.Result{}, err
	}

	// Check for completed drift remediation jobs and unsuspend if necessary
	r.Log.Info("Checking for completed drift remediation jobs and unsuspending if necessary", "AutoIndexer", autoIndexerObj.Name, "namespace", autoIndexerObj.Namespace)
	if err := r.handleDriftRemediationJobCompletion(ctx, autoIndexerObj); err != nil {
		r.Log.Error(err, "failed to handle drift remediation job completion", "autoindexer", autoIndexerObj.Name)
		return ctrl.Result{}, err
	}

	r.Log.Info("AutoIndexer resources are ready", "AutoIndexer", autoIndexerObj.Name, "namespace", autoIndexerObj.Namespace)
	if err := r.updateStatusConditionIfNotMatch(ctx, autoIndexerObj, autoindexerv1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue,
		"autoIndexerResourceStatusSuccess", "autoindexer resources are ready"); err != nil {
		r.Log.Error(err, "failed to update autoindexer status", "autoindexer", autoIndexerObj.Name)
		// Don't return error here as the main reconciliation succeeded
	}

	return ctrl.Result{}, nil
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

	// Clean up owned resources (Jobs, CronJobs, etc.)
	// Wait for all owned Jobs to complete before removing the finalizer
	jobs := &batchv1.JobList{}
	if err := r.Client.List(ctx, jobs, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{
		AutoIndexerNameLabel: autoIndexerObj.Name,
	}); err != nil {
		r.Log.Error(err, "failed to list jobs for deletion wait", "autoindexer", autoIndexerObj.Name)
		return ctrl.Result{}, err
	}
	for _, job := range jobs.Items {
		// If job is not completed or failed, requeue
		if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			r.Log.Info("Waiting for Job to complete before deleting AutoIndexer", "job", job.Name, "autoindexer", autoIndexerObj.Name)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	r.Log.Info("AutoIndexer deleted successfully", "autoindexer", autoIndexerObj.Name, "namespace", autoIndexerObj.Namespace)
	return ctrl.Result{}, nil
}

// updateStatusConditionIfNotMatch updates the status condition if it doesn't match
func (r *AutoIndexerReconciler) updateStatusConditionIfNotMatch(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer, conditionType autoindexerv1alpha1.ConditionType, status metav1.ConditionStatus, reason, message string) error {
	// Find existing condition
	existingAutoIndexerObj := autoIndexerObj.DeepCopy()
	var existingCondition *metav1.Condition
	for i := range autoIndexerObj.Status.Conditions {
		if autoIndexerObj.Status.Conditions[i].Type == string(conditionType) {
			existingCondition = &autoIndexerObj.Status.Conditions[i]
			break
		}
	}

	// Check if update is needed
	if existingCondition != nil && existingCondition.Status == status && existingCondition.Reason == reason && existingCondition.Message == message {
		return nil // No update needed
	}

	// Update or add condition
	newCondition := metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	if existingCondition != nil {
		*existingCondition = newCondition
	} else {
		autoIndexerObj.Status.Conditions = append(autoIndexerObj.Status.Conditions, newCondition)
	}

	// Update status
	if err := r.Client.Status().Patch(ctx, autoIndexerObj, client.MergeFrom(existingAutoIndexerObj)); err != nil {
		return fmt.Errorf("failed to update autoindexer status: %w", err)
	}

	return nil
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

func (r *AutoIndexerReconciler) handleDriftRemediationJobCreation(ctx context.Context, autoIndexer *autoindexerv1alpha1.AutoIndexer) error {
	// Only check if this AutoIndexer was in drift remediation
	if autoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation"] != "true" {
		return nil
	}

	// If a drift remediation job was already created, skip creating another one
	if autoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation-created"] == "true" {
		return nil
	}

	if autoIndexer.Annotations == nil {
		autoIndexer.Annotations = make(map[string]string)
	}
	autoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation-created"] = "true"

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
			autoIndexer.Annotations["autoindexer.kaito.sh/drift-remediation-suspended"] = "true"
			r.Log.Info("Suspended AutoIndexer for drift remediation",
				"autoindexer", autoIndexer.Name,
				"namespace", autoIndexer.Namespace,
				"original-suspend-state", originalSuspendState)
		}
	}

	// Update the AutoIndexer to add annotations and suspend if needed
	if err := r.Client.Update(ctx, autoIndexer); err != nil {
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
		Image:              manifests.GetJobImageConfig().GetImage(),
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
	if err := controllerutil.SetControllerReference(autoIndexer, job, r.Client.Scheme()); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the job
	if err := r.Client.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create drift remediation job: %w", err)
	}

	r.Log.Info("Created drift remediation job",
		"job", jobName,
		"autoindexer", autoIndexer.Name,
		"namespace", autoIndexer.Namespace)

	// Update the AutoIndexer status to indicate drift remediation (best effort)
	if err := r.updateStatusWithDriftRemediation(ctx, autoIndexer); err != nil {
		r.Log.Error(err, "failed to update AutoIndexer status for drift remediation, but job creation succeeded",
			"autoindexer", autoIndexer.Name,
			"namespace", autoIndexer.Namespace)
		// Don't fail the whole operation if only the status update fails
	}

	return nil
}

// updateStatusWithDriftRemediation updates the status for drift remediation jobs
func (r *AutoIndexerReconciler) updateStatusWithDriftRemediation(ctx context.Context, autoIndexer *autoindexerv1alpha1.AutoIndexer) error {
	// Refetch the AutoIndexer to get the latest version after spec updates
	freshAutoIndexer := &autoindexerv1alpha1.AutoIndexer{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      autoIndexer.Name,
		Namespace: autoIndexer.Namespace,
	}, freshAutoIndexer); err != nil {
		return fmt.Errorf("failed to get fresh AutoIndexer for status update: %w", err)
	}

	existingFreshAutoIndexer := freshAutoIndexer.DeepCopy()

	// Update indexing phase to indicate drift remediation
	freshAutoIndexer.Status.IndexingPhase = autoindexerv1alpha1.AutoIndexerPhaseRunning

	// Add drift remediation condition
	r.addDriftRemediationCondition(freshAutoIndexer)
	r.updateDriftCondition(freshAutoIndexer)

	// Update the status
	if err := r.Client.Status().Patch(ctx, freshAutoIndexer, client.MergeFrom(existingFreshAutoIndexer)); err != nil {
		return fmt.Errorf("failed to update AutoIndexer status for drift remediation: %w", err)
	}

	return nil
}

// updateDriftCondition adds or updates the drift detection condition
func (r *AutoIndexerReconciler) updateDriftCondition(autoIndexer *autoindexerv1alpha1.AutoIndexer) {
	condition := metav1.Condition{
		Type:               string(autoindexerv1alpha1.AutoIndexerConditionTypeDriftDetected),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "DocumentCountMismatch",
		Message:            fmt.Sprintf("Document count drift detected"),
		ObservedGeneration: autoIndexer.Generation,
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
func (r *AutoIndexerReconciler) addDriftRemediationCondition(autoIndexer *autoindexerv1alpha1.AutoIndexer) {
	condition := metav1.Condition{
		Type:               string(autoindexerv1alpha1.AutoIndexerConditionTypeDriftRemediation),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "RemediationJobCreated",
		Message:            fmt.Sprintf("Drift remediation job created due to document count mismatch"),
		ObservedGeneration: autoIndexer.Generation,
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

// handleDriftRemediationJobCompletion checks for completed drift remediation jobs and unsuspends the AutoIndexer if necessary
func (r *AutoIndexerReconciler) handleDriftRemediationJobCompletion(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	// Only check if this AutoIndexer was in drift remediation
	if autoIndexerObj.Annotations["autoindexer.kaito.sh/drift-remediation"] != "true" {
		return nil
	}

	// Get all jobs owned by this AutoIndexer that are drift remediation jobs
	jobs := &batchv1.JobList{}
	listOpts := []client.ListOption{
		client.InNamespace(autoIndexerObj.Namespace),
		client.MatchingLabels{
			"autoindexer.kaito.sh/name":              autoIndexerObj.Name,
			"autoindexer.kaito.sh/drift-remediation": "true",
		},
	}

	if err := r.Client.List(ctx, jobs, listOpts...); err != nil {
		return fmt.Errorf("failed to list drift remediation jobs: %w", err)
	}

	// Check if any drift remediation jobs are still running
	r.Log.Info("Checking drift remediation jobs for running status", "AutoIndexer", autoIndexerObj.Name, "namespace", autoIndexerObj.Namespace, "jobCount", len(jobs.Items))
	hasRunningJobs := false
	for _, job := range jobs.Items {
		// Job is still running if it hasn't succeeded or failed
		if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			hasRunningJobs = true
			break
		}
	}

	// If no drift remediation jobs are running, we can unsuspend the AutoIndexer
	if !hasRunningJobs && len(jobs.Items) > 0 {
		return r.unsuspendAfterDriftRemediation(ctx, autoIndexerObj)
	}

	return nil
}

// unsuspendAfterDriftRemediation restores the original suspension state after drift remediation completes
func (r *AutoIndexerReconciler) unsuspendAfterDriftRemediation(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
	existingAutoIndexerObj := autoIndexerObj.DeepCopy()

	// Update the suspend state to the original value
	if autoIndexerObj.Annotations["autoindexer.kaito.sh/drift-remediation-suspended"] == "true" {
		suspend := false
		autoIndexerObj.Spec.Suspend = &suspend
		delete(autoIndexerObj.Annotations, "autoindexer.kaito.sh/drift-remediation-suspended")
	}

	// Remove the drift remediation annotations
	if autoIndexerObj.Annotations != nil {
		delete(autoIndexerObj.Annotations, "autoindexer.kaito.sh/drift-remediation")
		delete(autoIndexerObj.Annotations, "autoindexer.kaito.sh/drift-remediation-created")
	}

	// Update the AutoIndexer
	if err := r.Client.Update(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to unsuspend AutoIndexer after drift remediation: %w", err)
	}

	// Update the drift remediation condition to indicate completion
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeDriftRemediation, metav1.ConditionFalse, "RemediationCompleted", "Drift remediation completed, AutoIndexer resumed")
	r.setAutoIndexerCondition(autoIndexerObj, autoindexerv1alpha1.AutoIndexerConditionTypeDriftDetected, metav1.ConditionFalse, "RemediationCompleted", "Drift remediation completed, AutoIndexer resumed")

	// Update status (best effort - don't fail if status update fails)
	if err := r.Client.Status().Patch(ctx, autoIndexerObj, client.MergeFrom(existingAutoIndexerObj)); err != nil {
		r.Log.Error(err, "failed to update AutoIndexer status after drift remediation completion, but unsuspension succeeded",
			"autoindexer", autoIndexerObj.Name,
			"namespace", autoIndexerObj.Namespace)
		// Don't return error since the main operation (unsuspending) succeeded
	}

	r.Log.Info("Unsuspended AutoIndexer after drift remediation completion",
		"autoindexer", autoIndexerObj.Name,
		"namespace", autoIndexerObj.Namespace)

	return nil
}
