package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	autoindexerv1alpha1 "github.com/kaito-project/autoindexer/api/v1alpha1"
	aiActions "github.com/kaito-project/autoindexer/test/e2e/suite/autoindexer/actions"
	ragActions "github.com/kaito-project/autoindexer/test/e2e/suite/ragengine/actions"
	"github.com/kaito-project/autoindexer/test/e2e/suite/types"
)

type AutoIndexerTest struct {
	types.BaseTest
}

func NewAutoIndexerOneTimeStaticTest(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName string, portForwardLocalPort int) types.Test {
	return AutoIndexerTest{
		BaseTest: types.BaseTest{
			Name:            "AutoIndexer One-Time Static Test",
			Description:     "Tests one-time static autoindexer functionality",
			RunConcurrently: true,
			Actions: []types.Action{
				ragActions.CreateRAGEngine(ragEngineName, autoIndexerNamespace),
				aiActions.CreateOneTimeStaticAutoIndexer(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName),
				aiActions.WaitForAutoIndexerJobCompletion(autoIndexerName, autoIndexerNamespace, 10*time.Minute),
				ragActions.StartPortForward(ragEngineName, autoIndexerNamespace, portForwardLocalPort),
				ragActions.ValidateIndexExistsInRAGEngine(ragEngineName, autoIndexerNamespace, indexName),
				aiActions.ValidateAutoIndexerStatus(autoIndexerName, autoIndexerNamespace, func(ctx context.Context, logger *slog.Logger, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
					statusBytes, _ := json.Marshal(&autoIndexerObj.Status)
					logger.Info("Validating AutoIndexer status for one-time static autoindexer", "autoIndexerName", autoIndexerName, "autoIndexerStatus", string(statusBytes))

					if autoIndexerObj.Status.IndexingPhase != autoindexerv1alpha1.AutoIndexerPhaseCompleted {
						return fmt.Errorf("expected autoindexer phase status to be 'Completed', got '%s'", autoIndexerObj.Status.IndexingPhase)
					}

					if autoIndexerObj.Status.LastIndexingTimestamp == nil {
						return fmt.Errorf("expected last indexing time to be set, but it was nil")
					}

					if autoIndexerObj.Status.LastIndexingDurationSeconds == 0 {
						return fmt.Errorf("expected last indexing duration to be greater than 0, but it was 0")
					}

					// if autoIndexerObj.Status.NumOfDocumentInIndex == 0 {
					// 	return fmt.Errorf("expected number of documents in index to be greater than 0, but it was 0")
					// }

					if autoIndexerObj.Status.SuccessfulIndexingCount == 0 {
						return fmt.Errorf("expected successful indexing count to be greater than 0, but it was 0")
					}

					if autoIndexerObj.Status.ErrorIndexingCount != 0 {
						return fmt.Errorf("expected error indexing count to be 0, but it was %d", autoIndexerObj.Status.ErrorIndexingCount)
					}

					logger.Info("AutoIndexer status validated successfully", "autoIndexerName", autoIndexerName, "autoIndexerNamespace", autoIndexerNamespace)
					return nil
				}),
			},
		},
	}
}

func NewAutoIndexerSchedulesStaticTest(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, schedule string, portForwardLocalPort int) types.Test {
	//initialRunAutoIndexerStatus := autoindexerv1alpha1.AutoIndexerStatus{}

	return AutoIndexerTest{
		BaseTest: types.BaseTest{
			Name:            "AutoIndexer Scheduled Static Test",
			Description:     "Tests scheduled static autoindexer functionality",
			RunConcurrently: true,
			Actions: []types.Action{
				ragActions.CreateRAGEngine(ragEngineName, autoIndexerNamespace),
				aiActions.CreateScheduledStaticAutoIndexer(autoIndexerName, autoIndexerNamespace, ragEngineName, indexName, schedule),
				aiActions.WaitForAutoIndexerJobCompletion(autoIndexerName, autoIndexerNamespace, 10*time.Minute),
				ragActions.StartPortForward(ragEngineName, autoIndexerNamespace, portForwardLocalPort),
				ragActions.ValidateIndexExistsInRAGEngine(ragEngineName, autoIndexerNamespace, indexName),
				aiActions.ValidateAutoIndexerStatus(autoIndexerName, autoIndexerNamespace, func(ctx context.Context, logger *slog.Logger, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
					if autoIndexerObj.Status.IndexingPhase != autoindexerv1alpha1.AutoIndexerPhaseScheduled && autoIndexerObj.Status.IndexingPhase != autoindexerv1alpha1.AutoIndexerPhaseRunning {
						return fmt.Errorf("expected autoindexer phase status to be 'Scheduled' or 'Running', got '%s'", autoIndexerObj.Status.IndexingPhase)
					}

					if autoIndexerObj.Status.LastIndexingTimestamp == nil {
						return fmt.Errorf("expected last indexing time to be set, but it was nil")
					}

					if autoIndexerObj.Status.LastIndexingDurationSeconds == 0 {
						return fmt.Errorf("expected last indexing duration to be greater than 0, but it was 0")
					}

					// Can only tests this against rag v0.8.0+
					// if autoIndexerObj.Status.NumOfDocumentInIndex == 0 {
					// 	return fmt.Errorf("expected number of documents in index to be greater than 0, but it was 0")
					// }

					if autoIndexerObj.Status.SuccessfulIndexingCount == 0 {
						return fmt.Errorf("expected successful indexing count to be greater than 0, but it was 0")
					}

					if autoIndexerObj.Status.ErrorIndexingCount != 0 {
						return fmt.Errorf("expected error indexing count to be 0, but it was %d", autoIndexerObj.Status.ErrorIndexingCount)
					}

					// initialRunAutoIndexerStatus = autoIndexerObj.Status
					logger.Info("AutoIndexer status validated successfully", "autoIndexerName", autoIndexerName, "autoIndexerNamespace", autoIndexerNamespace)
					return nil
				}),
				aiActions.WaitForAutoIndexerJobStart(autoIndexerName, autoIndexerNamespace, 10*time.Minute),
				aiActions.WaitForAutoIndexerJobCompletion(autoIndexerName, autoIndexerNamespace, 10*time.Minute),
				aiActions.SuspendAutoIndexerJobs(autoIndexerName, autoIndexerNamespace),
				ragActions.ValidateIndexExistsInRAGEngine(ragEngineName, autoIndexerNamespace, indexName),
				aiActions.ValidateAutoIndexerStatus(autoIndexerName, autoIndexerNamespace, func(ctx context.Context, logger *slog.Logger, autoIndexerObj *autoindexerv1alpha1.AutoIndexer) error {
					if autoIndexerObj.Status.IndexingPhase != autoindexerv1alpha1.AutoIndexerPhaseSuspended {
						return fmt.Errorf("expected autoindexer phase status to be 'Suspended', got '%s'", autoIndexerObj.Status.IndexingPhase)
					}

					if autoIndexerObj.Status.LastIndexingTimestamp == nil {
						return fmt.Errorf("expected last indexing time to be set, but it was nil")
					}

					if autoIndexerObj.Status.LastIndexingDurationSeconds == 0 {
						return fmt.Errorf("expected last indexing duration to be greater than 0, but it was 0")
					}

					// Can only tests this against rag v0.8.0+
					// if autoIndexerObj.Status.NumOfDocumentInIndex == 0 || autoIndexerObj.Status.NumOfDocumentInIndex != initialRunAutoIndexerStatus.NumOfDocumentInIndex {
					// 	return fmt.Errorf("expected number of documents in index(%d) to equal initial run number of documents(%d)", initialRunAutoIndexerStatus.NumOfDocumentInIndex, autoIndexerObj.Status.NumOfDocumentInIndex)
					// }

					if autoIndexerObj.Status.SuccessfulIndexingCount < 2 {
						return fmt.Errorf("expected successful indexing count to be at least 2, but it was %d", autoIndexerObj.Status.SuccessfulIndexingCount)
					}

					if autoIndexerObj.Status.ErrorIndexingCount != 0 {
						return fmt.Errorf("expected error indexing count to be 0, but it was %d", autoIndexerObj.Status.ErrorIndexingCount)
					}

					logger.Info("AutoIndexer status validated successfully", "autoIndexerName", autoIndexerName, "autoIndexerNamespace", autoIndexerNamespace)
					return nil
				}),
			},
		},
	}
}
