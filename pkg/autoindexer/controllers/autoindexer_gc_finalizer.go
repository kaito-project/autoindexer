package controllers

import (
	"context"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/utils"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// garbageCollectAutoIndexer remove finalizer associated with autoindexer object.
func (r *AutoIndexerReconciler) garbageCollectAutoIndexer(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) (ctrl.Result, error) {
	r.Log.Info("garbageCollectAutoIndexer", "autoindexer", klog.KObj(autoIndexerObj))

	// We need to remove the index within the RAG engine before removing the finalizer
	// First wait for any running jobs to complete
	jobs := &batchv1.JobList{}
	if err := r.Client.List(ctx, jobs, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{
		AutoIndexerNameLabel: autoIndexerObj.Name,
	}); err != nil {
		r.Log.Error(err, "failed to list jobs for deletion wait", "autoindexer", klog.KObj(autoIndexerObj))
		return ctrl.Result{}, err
	}
	for _, job := range jobs.Items {
		// If job is active, wait for it to complete
		if job.Status.Active != 0 {
			r.Log.Info("Waiting for Job to complete before deleting AutoIndexer", "job", job.Name, "autoindexer", klog.KObj(autoIndexerObj))
			return ctrl.Result{}, nil
		}
	}

	err := r.RAGClient.DeleteIndex(autoIndexerObj.Spec.RAGEngine, autoIndexerObj.Spec.IndexName, autoIndexerObj.Namespace)
	if err != nil {
		r.Log.Error(err, "failed to delete index in RAG engine during AutoIndexer deletion", "autoindexer", klog.KObj(autoIndexerObj))
		return ctrl.Result{}, err
	}
	r.Log.Info("Successfully deleted index in RAG engine", "autoindexer", klog.KObj(autoIndexerObj))

	if controllerutil.RemoveFinalizer(autoIndexerObj, utils.AutoIndexerFinalizer) {
		if updateErr := r.Update(ctx, autoIndexerObj, &client.UpdateOptions{}); updateErr != nil {
			r.Log.Error(updateErr, "failed to remove the finalizer from the autoindexer",
				"autoindexer", klog.KObj(autoIndexerObj))
			return ctrl.Result{}, updateErr
		}
		r.Log.Info("successfully removed the autoindexer finalizers", "autoindexer", klog.KObj(autoIndexerObj))
	}

	return ctrl.Result{}, nil
}
