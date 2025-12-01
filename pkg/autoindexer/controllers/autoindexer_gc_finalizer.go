package controllers

import (
	"context"
	"time"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/pkg/autoindexer/utils"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// garbageCollectRAGEngine remove finalizer associated with ragengine object.
func (r *AutoIndexerReconciler) garbageCollectRAGEngine(ctx context.Context, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) (ctrl.Result, error) {
	r.Log.Info("garbageCollectAutoIndexer", "autoindexer", klog.KObj(autoIndexerObj))

	// We need to remove the index within the RAG engine before removing the finalizer
	// First wait for any running jobs to complete
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

	err := r.RAGClient.DeleteIndex(autoIndexerObj.Spec.RAGEngine, autoIndexerObj.Spec.IndexName, autoIndexerObj.Namespace)
	if err != nil {
		r.Log.Error(err, "failed to delete index in RAG engine during AutoIndexer deletion", "autoindexer", klog.KObj(autoIndexerObj))
		return ctrl.Result{}, err
	}
	r.Log.Info("Successfully deleted index in RAG engine", "autoindexer", klog.KObj(autoIndexerObj))

	if controllerutil.RemoveFinalizer(autoIndexerObj, utils.AutoIndexerFinalizer) {
		if updateErr := r.Update(ctx, autoIndexerObj, &client.UpdateOptions{}); updateErr != nil {
			klog.ErrorS(updateErr, "failed to remove the finalizer from the autoindexer",
				"autoindexer", klog.KObj(autoIndexerObj))
			return ctrl.Result{}, updateErr
		}
		klog.InfoS("successfully removed the autoindexer finalizers", "autoindexer", klog.KObj(autoIndexerObj))
	}

	return ctrl.Result{}, nil
}
