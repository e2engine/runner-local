package api

import (
	"context"
	nativeerrors "errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/e2engine/core/execute"
	"github.com/e2engine/core/execute/call"
	"github.com/e2engine/core/execute/runtime"
	"github.com/e2engine/core/model"
	coreerrors "github.com/e2engine/core/pkg/errors"

	"github.com/e2engine/runner-local/pkg/config"
)

func TestServiceSharedEnvironmentCallStoreLimit(t *testing.T) {
	const suiteID = "suite-1"

	store := call.NewStoreFactory(&call.Config{
		MaxCalls:         1,
		MaxRecordedBytes: 1024,
	}).New()

	jobs := make(chan execute.TestJob, 2)

	for _, id := range []string{"test-1", "test-2"} {
		jobs <- execute.TestJob{
			ExecutionID:          id,
			TestSuiteExecutionID: suiteID,
		}
	}
	close(jobs)

	var acquires atomic.Int32
	var releases atomic.Int32

	results := make(map[string]execute.TestExecutionResult)

	environment := &environmentControllerMock{
		acquire: func(
			_ context.Context,
			instanceID string,
			_ *model.Environment,
		) (*runtime.EnvironmentInstance, bool, error) {
			if instanceID != suiteID {
				t.Errorf(
					"expected environment instance %q, got %q",
					suiteID,
					instanceID,
				)
			}

			created := acquires.Add(1) == 1

			return &runtime.EnvironmentInstance{
				Calls: store,
			}, created, nil
		},

		release: func(
			_ context.Context,
			instanceID string,
		) error {
			releases.Add(1)

			if instanceID != suiteID {
				t.Errorf(
					"expected release of %q, got %q",
					suiteID,
					instanceID,
				)
			}

			return nil
		},
	}

	worker := testWorkerFunc(func(
		_ context.Context,
		job execute.TestJob,
	) (*execute.TestExecutionResult, error) {
		if job.Runtime.Calls != store {
			t.Error("expected shared call store")
		}

		err := job.Runtime.Calls.Record(call.Call{
			TestExecutionID: job.ExecutionID,
			ServiceID:       "service-1",
		})
		if err != nil {
			return nil, err
		}

		return &execute.TestExecutionResult{
			ExecutionID:          job.ExecutionID,
			TestSuiteExecutionID: suiteID,
			Status:               model.ExecutionStatusPassed,
		}, nil
	})

	updater := &stateUpdaterMock{
		setTestCompleted: func(
			_ context.Context,
			result execute.TestExecutionResult,
		) (bool, error) {
			results[result.ExecutionID] = result

			// Release the shared environment only after
			// both test executions have completed.
			return len(results) == 2, nil
		},
	}

	service := &Service{
		cfg: &config.Config{
			WorkersPoolSize: 1,
			CleanupTimeout:  time.Second,
		},
		testWorker:   worker,
		tests:        jobs,
		stateUpdater: updater,
		environment:  environment,
	}

	if err := service.StartTestExecution(t.Context()); err != nil {
		t.Fatalf("StartTestExecution failed: %v", err)
	}

	if err := service.Run(t.Context()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if got := acquires.Load(); got != 2 {
		t.Errorf("expected two acquires, got %d", got)
	}

	if got := releases.Load(); got != 1 {
		t.Errorf("expected one release, got %d", got)
	}

	if len(results) != 2 {
		t.Fatalf("expected two results, got %d", len(results))
	}

	recorded := store.Get(call.Filter{})
	if len(recorded) != 1 {
		t.Fatalf("expected one recorded call, got %d", len(recorded))
	}

	winnerID := recorded[0].TestExecutionID
	winner, ok := results[winnerID]
	if !ok {
		t.Fatalf("no result for recorded call from %q", winnerID)
	}

	if winner.Status != model.ExecutionStatusPassed {
		t.Errorf(
			"expected recording test %q to pass, got %s",
			winnerID,
			winner.Status,
		)
	}

	loserID := "test-1"
	if winnerID == "test-1" {
		loserID = "test-2"
	} else if winnerID != "test-2" {
		t.Fatalf("unexpected recorded execution ID %q", winnerID)
	}

	loser := results[loserID]
	if loser.Status != model.ExecutionStatusError {
		t.Errorf(
			"expected rejected test %q to error, got %s",
			loserID,
			loser.Status,
		)
	}

	if loser.Summary == nil {
		t.Fatal("expected rejected test execution summary")
	}

	if loser.Summary.Error != coreerrors.ErrCallStoreLimitExceeded.Error() {
		t.Errorf(
			"expected summary error %q, got %q",
			coreerrors.ErrCallStoreLimitExceeded.Error(),
			loser.Summary.Error,
		)
	}

	if !nativeerrors.Is(
		store.Err(),
		coreerrors.ErrCallStoreLimitExceeded,
	) {
		t.Errorf(
			"expected shared store to exceed limit, got %v",
			store.Err(),
		)
	}
}
