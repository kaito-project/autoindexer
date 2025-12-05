package suite

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	autoindexerTests "github.com/kaito-project/autoindexer/test/e2e/suite/autoindexer/tests"
	"github.com/kaito-project/autoindexer/test/e2e/suite/types"
	"github.com/kaito-project/autoindexer/test/e2e/utils"
	"golang.org/x/sync/errgroup"
)

func RunE2ETestSuite(ctx context.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	testContext := &types.TestContext{
		Cluster: utils.TestingCluster,
	}

	err := utils.GetClusterClient(utils.TestingCluster)
	if err != nil {
		return err
	}

	// Add Tests Here
	allTests := []types.Test{
		autoindexerTests.NewAutoIndexerOneTimeStaticTest("static-one-time-ai", "default", "ragengine", "static-one-time-index", 5789), // To run test concurrently, make sure the local port forwards are different
		autoindexerTests.NewAutoIndexerSchedulesStaticTest("static-scheduled-ai", "default", "ragengine-static-scheduled", "static-scheduled-index", "*/5 * * * *", 5790),
	}

	concurrentTests := []types.Test{}
	syncTests := []types.Test{}
	for _, test := range allTests {
		if test.CanRunConcurrently() {
			concurrentTests = append(concurrentTests, test)
		} else {
			syncTests = append(syncTests, test)
		}
	}

	errorGroup, gCtx := errgroup.WithContext(ctx)
	for _, test := range concurrentTests {
		t := test // capture the current test in the loop variable
		errorGroup.Go(func() error {
			testCtx, cancel := context.WithTimeout(gCtx, 30*time.Minute)
			defer cancel()
			if err := t.Run(testCtx, logger.With("test", t.GetName()), testContext); err != nil {
				return fmt.Errorf("failed to run test %s: %w", t.GetName(), err)
			}
			return nil
		})
	}

	if err := errorGroup.Wait(); err != nil {
		return fmt.Errorf("failed to run concurrent tests: %w", err)
	}

	for _, test := range syncTests {
		testCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		if err := test.Run(testCtx, logger.With("test", test.GetName()), testContext); err != nil {
			return fmt.Errorf("failed to run test %s: %w", test.GetName(), err)
		}
	}

	return nil
}
