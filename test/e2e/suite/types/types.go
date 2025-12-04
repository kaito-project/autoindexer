package types

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kaito-project/autoindexer/test/e2e/suite/ragengine/client"
	"github.com/kaito-project/autoindexer/test/e2e/utils"
	"k8s.io/client-go/tools/portforward"
)

type TestContext struct {
	Cluster       *utils.Cluster
	RAGClient     *client.RAGEngineClient
	PortForwarder *portforward.PortForwarder
}

type TestFunc func(ctx context.Context, logger *slog.Logger, testContext *TestContext) error
type Action struct {
	Name        string
	RunFunc     TestFunc
	CleanupFunc TestFunc
}

type Test interface {
	GetName() string
	GetDescription() string
	CanRunConcurrently() bool
	Run(ctx context.Context, logger *slog.Logger, testContext *TestContext) error
}

type BaseTest struct {
	Name            string
	Description     string
	RunConcurrently bool
	Actions         []Action
}

func (t BaseTest) GetName() string {
	return t.Name
}

func (t BaseTest) GetDescription() string {
	return t.Description
}

func (t BaseTest) CanRunConcurrently() bool {
	return t.RunConcurrently
}

// Run executes the test by running all actions in sequence.
// If a run action fails, it will stop executing further run actions and run cleanup actions.
func (t BaseTest) Run(ctx context.Context, logger *slog.Logger, testContext *TestContext) error {
	logger = logger.With("test", t.Name)

	if testContext == nil {
		return fmt.Errorf("test context cannot be nil for test %s", t.Name)
	}

	var err error
	for _, action := range t.Actions {
		if action.RunFunc == nil {
			logger.Error("action run function is nil", "action", action.Name)
			return fmt.Errorf("action run function is nil for action %s in test %s", action.Name, t.Name)
		}

		if err = action.RunFunc(ctx, logger.With("action", action.Name, "stage", "run"), testContext); err != nil {
			logger.Error("failed to run test action", "action", action.Name, "error", err)
			err = fmt.Errorf("test %s failed at action %s: %w", t.Name, action.Name, err)
			break
		}
	}

	// Cleanup actions should always run and not fail if the test failed or was not run.
	// Should run in reverse order of the run actions to properly clean up dependent resources.
	var cleanupErrs error
	for idx := len(t.Actions) - 1; idx >= 0; idx-- {
		action := t.Actions[idx]
		if action.CleanupFunc == nil {
			continue
		}

		if err := action.CleanupFunc(ctx, logger.With("action", action.Name, "stage", "cleanup"), testContext); err != nil {
			cleanupErrs = errors.Join(cleanupErrs, fmt.Errorf("test %s cleanup failed at action %s: %w", t.Name, action.Name, err))
			logger.Error("failed to run cleanup action", "action", action.Name, "error", err)
		}
	}

	allErrs := errors.Join(err, cleanupErrs)

	if allErrs != nil {
		return allErrs
	}

	return nil
}
