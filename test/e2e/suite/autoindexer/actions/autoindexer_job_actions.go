package actions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/test/e2e/suite/types"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AutoIndexerNameLabel = "autoindexer.kaito.sh/name"
)

func WaitForAutoIndexerJobCompletion(autoIndexerName, autoIndexerNamespace string, timeout time.Duration) types.Action {
	return types.Action{
		Name: "Wait for AutoIndexer Job Completion",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			return wait.PollUntilContextTimeout(ctx, 10*time.Second, timeout, false, func(ctx context.Context) (bool, error) {
				logger.Info("Checking for AutoIndexer job completion", "autoindexer", autoIndexerName)
				jobs := &batchv1.JobList{}
				if err := testContext.Cluster.KubeClient.List(ctx, jobs, client.InNamespace(autoIndexerNamespace), client.MatchingLabels{
					AutoIndexerNameLabel: autoIndexerName,
				}); err != nil {
					return false, fmt.Errorf("failed to list autoindexer jobs: %w", err)
				}

				if len(jobs.Items) == 0 {
					logger.Info("No jobs found for autoindexer yet", "autoindexer", autoIndexerName)
					return false, nil
				}

				anyActiveJobs := false
				completedJobsFound := false
				for _, job := range jobs.Items {
					if job.Status.Active > 0 {
						anyActiveJobs = true
					}
					if job.Status.Succeeded > 0 {
						completedJobsFound = true
					}
					if job.Status.Failed > 0 {
						return false, fmt.Errorf("autoindexer job %s failed", job.Name)
					}
				}

				if completedJobsFound && !anyActiveJobs {
					logger.Info("AutoIndexer job completed", "autoindexer", autoIndexerName)
					return true, nil
				}

				logger.Info("AutoIndexer job still running", "autoindexer", autoIndexerName)
				return false, nil
			})
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			// No cleanup needed for waiting action
			return nil
		},
	}
}

func WaitForAutoIndexerJobStart(autoIndexerName, autoIndexerNamespace string, timeout time.Duration) types.Action {
	return types.Action{
		Name: "Wait for AutoIndexer Job Start",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			return wait.PollUntilContextTimeout(ctx, 10*time.Second, timeout, false, func(ctx context.Context) (bool, error) {
				logger.Info("Checking for AutoIndexer job start", "autoindexer", autoIndexerName)
				jobs := &batchv1.JobList{}
				if err := testContext.Cluster.KubeClient.List(ctx, jobs, client.InNamespace(autoIndexerNamespace), client.MatchingLabels{
					AutoIndexerNameLabel: autoIndexerName,
				}); err != nil {
					return false, fmt.Errorf("failed to list autoindexer jobs: %w", err)
				}

				if len(jobs.Items) == 0 {
					logger.Info("No jobs found for autoindexer yet", "autoindexer", autoIndexerName)
					return false, nil
				}

				for _, job := range jobs.Items {
					if job.Status.Active > 0 {
						logger.Info("AutoIndexer job started", "job", job.Name, "autoindexer", autoIndexerName)
						return true, nil
					}
				}

				logger.Info("AutoIndexer job not started yet", "autoindexer", autoIndexerName)
				return false, nil
			})
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			// No cleanup needed for waiting action
			return nil
		},
	}
}

func SuspendAutoIndexerJobs(autoIndexerName, autoIndexerNamespace string) types.Action {
	return types.Action{
		Name: "Suspend AutoIndexer Jobs",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Suspending AutoIndexer Jobs", "autoindexer", autoIndexerName)
			autoIndexerObj := &autoindexerv1alpha1.AutoIndexer{}
			err := testContext.Cluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      autoIndexerName,
				Namespace: autoIndexerNamespace,
			}, autoIndexerObj)
			if err != nil {
				return fmt.Errorf("failed to get autoindexer for status validation: %w", err)
			}
			suspended := true
			autoIndexerObj.Spec.Suspend = &suspended
			if err := testContext.Cluster.KubeClient.Update(ctx, autoIndexerObj); err != nil {
				return fmt.Errorf("failed to update autoindexer to suspend jobs: %w", err)
			}

			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			// No cleanup needed for suspend action
			return nil
		},
	}
}

func ResumeAutoIndexerJobs(autoIndexerName, autoIndexerNamespace string) types.Action {
	return types.Action{
		Name: "Resume AutoIndexer Jobs",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Resuming AutoIndexer Jobs", "autoindexer", autoIndexerName)
			autoIndexerObj := &autoindexerv1alpha1.AutoIndexer{}
			err := testContext.Cluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      autoIndexerName,
				Namespace: autoIndexerNamespace,
			}, autoIndexerObj)
			if err != nil {
				return fmt.Errorf("failed to get autoindexer for status validation: %w", err)
			}
			suspended := false
			autoIndexerObj.Spec.Suspend = &suspended
			if err := testContext.Cluster.KubeClient.Update(ctx, autoIndexerObj); err != nil {
				return fmt.Errorf("failed to update autoindexer to suspend jobs: %w", err)
			}

			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			// No cleanup needed for suspend action
			return nil
		},
	}
}
