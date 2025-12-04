package actions

import (
	"context"
	"fmt"
	"log/slog"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	"github.com/kaito-project/autoindexer/test/e2e/suite/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FuncValidateAutoIndexerStatus func(ctx context.Context, logger *slog.Logger, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error

func ValidateAutoIndexerStatus(autoIndexerName, autoIndexerNamespace string, validateFunc FuncValidateAutoIndexerStatus) types.Action {
	return types.Action{
		Name: "Validate AutoIndexer Status",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			logger.Info("Validating AutoIndexer Status")

			autoIndexerObj := &autoindexerv1alpha1.AutoIndexer{}
			err := testContext.Cluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      autoIndexerName,
				Namespace: autoIndexerNamespace,
			}, autoIndexerObj)
			if err != nil {
				return fmt.Errorf("failed to get autoindexer for status validation: %w", err)
			}

			return validateFunc(ctx, logger, autoIndexerObj)
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.TestContext) error {
			// No cleanup needed for status validation
			return nil
		},
	}
}
