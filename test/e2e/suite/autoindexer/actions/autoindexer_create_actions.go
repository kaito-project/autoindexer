package actions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kaito-project/autoindexer/test/e2e/manifests"
	"github.com/kaito-project/autoindexer/test/e2e/suite/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
)

const (
	DefaultGitRepoURL = "https://github.com/example/repo.git"
	DefaultGitBranch  = "main"
)

var (
	DefaultStaticURLs = []string{
		"https://raw.githubusercontent.com/kaito-project/kaito-cookbook/master/examples/ragengine-llm-d/10-K/BRK-B.pdf",
		"https://raw.githubusercontent.com/kaito-project/kaito-cookbook/master/examples/ragengine-llm-d/10-K/NVDA.pdf",
	}
)

func CreateScheduledGitAutoIndexer(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, schedule string) types.Action {
	return types.Action{
		Name: "Create Scheduled Git AutoIndexer",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Creating Scheduled Git AutoIndexer")

			gitSpec := manifests.CreateGitDataSourceSpec(DefaultGitRepoURL, DefaultGitBranch, nil, nil, nil)
			autoIndexerManifest := manifests.CreateAutoIndexerManifest(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, &schedule, gitSpec, nil, nil)

			err := testContext.Cluster.KubeClient.Create(ctx, autoIndexerManifest)
			if err != nil {
				return fmt.Errorf("failed to create scheduled git autoindexer: %w", err)
			}

			err = waitForAutoIndexerCondition(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace,
				autoindexerv1alpha1.ConditionTypeResourceStatus, "True",
				time.Second, time.Minute*5)
			if err != nil {
				return fmt.Errorf("scheduled git autoindexer did not become ready in time: %w", err)
			}

			logger.Info("Scheduled Git AutoIndexer created successfully")
			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			return deleteAutoIndexer(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace)
		},
	}
}

func CreateOneTimeGitAutoIndexer(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName string) types.Action {
	return types.Action{
		Name: "Create One-Time Git AutoIndexer",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Creating One-Time Git AutoIndexer")

			gitSpec := manifests.CreateGitDataSourceSpec(DefaultGitRepoURL, DefaultGitBranch, nil, nil, nil)
			autoIndexerManifest := manifests.CreateAutoIndexerManifest(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, nil, gitSpec, nil, nil)

			err := testContext.Cluster.KubeClient.Create(ctx, autoIndexerManifest)
			if err != nil {
				return fmt.Errorf("failed to create one-time git autoindexer: %w", err)
			}

			err = waitForAutoIndexerCondition(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace,
				autoindexerv1alpha1.ConditionTypeResourceStatus, "True",
				time.Second, time.Minute*10)
			if err != nil {
				return fmt.Errorf("one-time git autoindexer did not succeed in time: %w", err)
			}

			logger.Info("One-Time Git AutoIndexer created successfully")
			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			return deleteAutoIndexer(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace)
		},
	}
}

func CreateScheduledStaticAutoIndexer(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, schedule string) types.Action {
	return types.Action{
		Name: "Create Scheduled Static AutoIndexer",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Creating Scheduled Static AutoIndexer")

			staticSpec := manifests.CreateStaticDataSourceSpec(DefaultStaticURLs)
			autoIndexerManifest := manifests.CreateAutoIndexerManifest(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, &schedule, staticSpec, nil, nil)

			err := testContext.Cluster.KubeClient.Create(ctx, autoIndexerManifest)
			if err != nil {
				return fmt.Errorf("failed to create static scheduled AutoIndexer: %w", err)
			}

			err = waitForAutoIndexerCondition(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace,
				autoindexerv1alpha1.ConditionTypeResourceStatus, "True",
				time.Second, time.Minute*5)
			if err != nil {
				return fmt.Errorf("static scheduled autoIndexer did not become ready in time: %w", err)
			}

			logger.Info("Scheduled Static AutoIndexer created successfully")
			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			return deleteAutoIndexer(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace)
		},
	}
}

func CreateOneTimeStaticAutoIndexer(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName string) types.Action {
	return types.Action{
		Name: "Create One-Time Static AutoIndexer",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Creating One-Time Static AutoIndexer")

			staticSpec := manifests.CreateStaticDataSourceSpec(DefaultStaticURLs)
			autoIndexerManifest := manifests.CreateAutoIndexerManifest(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, nil, staticSpec, nil, nil)

			err := testContext.Cluster.KubeClient.Create(ctx, autoIndexerManifest)
			if err != nil {
				return fmt.Errorf("failed to create one-time static autoindexer: %w", err)
			}

			err = waitForAutoIndexerCondition(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace,
				autoindexerv1alpha1.ConditionTypeResourceStatus, "True",
				time.Second, time.Minute*5)
			if err != nil {
				return fmt.Errorf("one-time static autoindexer did not become ready in time: %w", err)
			}

			logger.Info("One-Time Static AutoIndexer created successfully")
			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			return deleteAutoIndexer(ctx, logger, testContext, autoIndexerName, autoIndexerNamespace)
		},
	}
}

func waitForAutoIndexerCondition(ctx context.Context, logger *slog.Logger, testContext *types.TestContext,
	autoIndexerName, autoIndexerNamespace string,
	conditionType autoindexerv1alpha1.ConditionType, expectedStatus string,
	interval time.Duration, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, interval, timeout, false, func(ctx context.Context) (bool, error) {
		logger.Info("Waiting for AutoIndexer condition", "name", autoIndexerName, "namespace", autoIndexerNamespace, "conditionType", conditionType, "expectedStatus", expectedStatus)
		var aiStatus autoindexerv1alpha1.AutoIndexer
		if err := testContext.Cluster.KubeClient.Get(ctx, client.ObjectKey{
			Name:      autoIndexerName,
			Namespace: autoIndexerNamespace,
		}, &aiStatus); err != nil {
			return false, client.IgnoreNotFound(err)
		}

		for _, condition := range aiStatus.Status.Conditions {
			if condition.Type == string(conditionType) && string(condition.Status) == string(expectedStatus) {
				logger.Info("AutoIndexer condition met", "name", autoIndexerName, "conditionType", conditionType, "status", expectedStatus)
				return true, nil
			}
		}

		return false, nil
	})
}

func deleteAutoIndexer(ctx context.Context, logger *slog.Logger, testContext *types.TestContext,
	autoIndexerName, autoIndexerNamespace string) error {
	logger.Info("Deleting AutoIndexer", "name", autoIndexerName, "namespace", autoIndexerNamespace)
	var ai autoindexerv1alpha1.AutoIndexer
	err := testContext.Cluster.KubeClient.Get(ctx, client.ObjectKey{
		Name:      autoIndexerName,
		Namespace: autoIndexerNamespace,
	}, &ai)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to get autoindexer for deletion: %w", err)
		}
		logger.Info("AutoIndexer not found, nothing to delete", "name", autoIndexerName, "namespace", autoIndexerNamespace)
		return nil
	}

	err = testContext.Cluster.KubeClient.Delete(ctx, &ai)
	if err != nil {
		return fmt.Errorf("failed to delete AutoIndexer: %w", err)
	}
	logger.Info("AutoIndexer deleted", "name", autoIndexerName, "namespace", autoIndexerNamespace)
	return nil
}
