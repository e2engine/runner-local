package api

import (
	"context"
	nativeerrors "errors"
	"time"

	"github.com/e2engine/core/execute"
	"github.com/e2engine/core/execute/persist"
	"github.com/e2engine/core/execute/runtime"
	"github.com/e2engine/core/model"
	coreerrors "github.com/e2engine/core/pkg/errors"
	"github.com/e2engine/core/pkg/keys"
	"github.com/e2engine/core/pkg/log"
	"github.com/ygrebnov/errorc"
	"github.com/ygrebnov/workers"

	"github.com/e2engine/runner-local/pkg/config"
	"github.com/e2engine/runner-local/pkg/errors"
)

type Service struct {
	cfg    *config.Config
	logger log.Logger

	testWorker testWorker

	tests <-chan execute.TestJob

	testResults <-chan *execute.TestExecutionResult
	errs        <-chan error

	environment  environmentController
	stateUpdater stateUpdater
}

type settings struct {
	logger       log.Logger
	tests        <-chan execute.TestJob
	stateUpdater stateUpdater
	environment  environmentController
	testWorker   testWorker
}

type Option func(*settings)

func WithLogger(logger log.Logger) Option {
	return func(settings *settings) {
		if logger != nil {
			settings.logger = logger
		}
	}
}

func WithTestJobsChannel(jobs <-chan execute.TestJob) Option {
	return func(settings *settings) {
		if jobs != nil {
			settings.tests = jobs
		}
	}
}

func WithTestWorker(worker testWorker) Option {
	return func(settings *settings) {
		if worker != nil {
			settings.testWorker = worker
		}
	}
}

func WithEnvironmentController(
	controller environmentController,
) Option {
	return func(settings *settings) {
		if controller != nil {
			settings.environment = controller
		}
	}
}

func WithStateUpdater(stateUpdater stateUpdater) Option {
	return func(settings *settings) {
		if stateUpdater != nil {
			settings.stateUpdater = stateUpdater
		}
	}
}

type stateUpdater interface {
	SetTestRunning(ctx context.Context, executionID string, startedAt time.Time) error
	SetTestSuiteRunning(ctx context.Context, executionID string, startedAt time.Time) error
	SetTestCompleted(ctx context.Context, result execute.TestExecutionResult) (bool, error)
	SetTestSuiteCompleted(ctx context.Context, result execute.TestSuiteExecutionResult) (bool, error)
}

var _ stateUpdater = (*persist.Updater)(nil)

type environmentController interface {
	Acquire(
		ctx context.Context,
		instanceID string,
		env *model.Environment,
	) (*runtime.EnvironmentInstance, bool, error)

	Release(
		ctx context.Context,
		instanceID string,
	) error

	ReleaseAll(ctx context.Context) error
}

var _ environmentController = (*runtime.EnvironmentController)(nil)

type testWorker = execute.Worker[execute.TestJob, execute.TestExecutionResult]

func getSettings(opts ...Option) (*settings, error) {
	s := &settings{}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	var err error
	if s.logger == nil {
		s.logger, err = log.NewSilentLogger()
		if err != nil {
			return nil, errorc.With(
				coreerrors.ErrCannotInitializeLogger, errorc.Error(keys.Cause, err),
			)
		}
	}

	return s, nil
}

func NewService(cfg *config.Config, opts ...Option) (*Service, error) {
	s, err := getSettings(opts...)
	if err != nil {
		return nil, err
	}

	if cfg == nil {
		return nil, errors.ErrNilConfig
	}

	if s.tests == nil {
		return nil, errors.ErrNilTestJobsChannel
	}

	if s.testWorker == nil {
		return nil, errors.ErrNilTestWorker
	}

	if s.stateUpdater == nil {
		return nil, errors.ErrNilStateUpdater
	}

	if s.environment == nil {
		return nil, errors.ErrNilEnvironmentController
	}

	return &Service{
		cfg:          cfg,
		logger:       s.logger.With(log.String(keys.Component, "local_runner")),
		testWorker:   s.testWorker,
		tests:        s.tests,
		stateUpdater: s.stateUpdater,
		environment:  s.environment,
	}, nil
}

//nolint:gocritic // workers.MapStream requires a value-taking callback; TestJob is the stream item type.
func (s *Service) executeTest(
	ctx context.Context,
	job execute.TestJob,
) (*execute.TestExecutionResult, error) {
	instance, created, acquireErr := s.environment.Acquire(
		ctx,
		job.GetEnvironmentInstanceID(),
		&job.Environment,
	)
	if acquireErr != nil {
		return nil, acquireErr
	}

	job.Runtime.Calls = instance.Calls

	if job.TestSuiteExecutionID != "" && created {
		if err := s.stateUpdater.SetTestSuiteRunning(
			ctx,
			job.TestSuiteExecutionID,
			time.Now().UTC(),
		); err != nil {
			return nil, err
		}
	}

	if err := s.stateUpdater.SetTestRunning(
		ctx,
		job.ExecutionID,
		time.Now().UTC(),
	); err != nil {
		return nil, err
	}

	result, executeErr := s.testWorker.Execute(ctx, job)
	if executeErr != nil {
		return &execute.TestExecutionResult{
			ExecutionID:          job.ExecutionID,
			TestSuiteExecutionID: job.TestSuiteExecutionID,
			Status:               model.ExecutionStatusError,
			FinishedAt:           time.Now().UTC(),
			Summary: &model.TestExecutionSummary{
				Error: executeErr.Error(),
			},
		}, nil
	}

	return result, nil
}

func (s *Service) StartTestExecution(ctx context.Context) error {
	testResults, errs, err := workers.MapStream(
		ctx,
		s.tests,
		s.executeTest,
		workers.WithFixedPool(uint(s.cfg.WorkersPoolSize)),
	)
	if err != nil {
		return err
	}

	s.testResults = testResults
	s.errs = errs

	return nil
}

func (s *Service) Run(ctx context.Context) (runErr error) {
	if s.testResults == nil || s.errs == nil {
		return errors.ErrNotStarted
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			s.cfg.CleanupTimeout,
		)
		defer cancel()
		if err := s.environment.ReleaseAll(cleanupCtx); err != nil {
			runErr = nativeerrors.Join(runErr, err)
		}
	}()

	results := s.testResults
	errs := s.errs

	for results != nil || errs != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}

			if err != nil {
				return err
			}

		case result, ok := <-results:
			if !ok {
				results = nil
				continue
			}

			completed, err := s.stateUpdater.SetTestCompleted(ctx, *result)
			if err != nil {
				return err
			}

			if completed {
				if err := s.environment.Release(
					ctx,
					result.GetEnvironmentInstanceID(),
				); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
