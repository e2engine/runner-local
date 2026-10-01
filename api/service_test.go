package api

import (
	"context"
	nativeerrors "errors"
	"testing"
	"time"

	"github.com/e2engine/core/execute"
	"github.com/e2engine/core/execute/call"
	"github.com/e2engine/core/execute/runtime"
	"github.com/e2engine/core/model"

	"github.com/e2engine/runner-local/pkg/config"
	"github.com/e2engine/runner-local/pkg/errors"
)

type testWorkerFunc func(
	context.Context,
	execute.TestJob,
) (*execute.TestExecutionResult, error)

func (f testWorkerFunc) Execute(
	ctx context.Context,
	job execute.TestJob,
) (*execute.TestExecutionResult, error) {
	return f(ctx, job)
}

type stateUpdaterMock struct {
	setTestRunning        func(context.Context, string, time.Time) error
	setTestSuiteRunning   func(context.Context, string, time.Time) error
	setTestCompleted      func(context.Context, execute.TestExecutionResult) (bool, error)
	setTestSuiteCompleted func(context.Context, execute.TestSuiteExecutionResult) (bool, error)
}

func (s *stateUpdaterMock) SetTestRunning(
	ctx context.Context,
	executionID string,
	startedAt time.Time,
) error {
	if s.setTestRunning != nil {
		return s.setTestRunning(ctx, executionID, startedAt)
	}

	return nil
}

func (s *stateUpdaterMock) SetTestSuiteRunning(
	ctx context.Context,
	executionID string,
	startedAt time.Time,
) error {
	if s.setTestSuiteRunning != nil {
		return s.setTestSuiteRunning(ctx, executionID, startedAt)
	}

	return nil
}

func (s *stateUpdaterMock) SetTestCompleted(
	ctx context.Context,
	result execute.TestExecutionResult,
) (bool, error) {
	if s.setTestCompleted != nil {
		return s.setTestCompleted(ctx, result)
	}

	return false, nil
}

func (s *stateUpdaterMock) SetTestSuiteCompleted(
	ctx context.Context,
	result execute.TestSuiteExecutionResult,
) (bool, error) {
	if s.setTestSuiteCompleted != nil {
		return s.setTestSuiteCompleted(ctx, result)
	}

	return false, nil
}

type environmentControllerMock struct {
	acquire    func(context.Context, string, *model.Environment) (*runtime.EnvironmentInstance, bool, error)
	release    func(context.Context, string) error
	releaseAll func(context.Context) error
}

func (e *environmentControllerMock) Acquire(
	ctx context.Context,
	instanceID string,
	env *model.Environment,
) (*runtime.EnvironmentInstance, bool, error) {
	if e.acquire != nil {
		return e.acquire(ctx, instanceID, env)
	}

	return &runtime.EnvironmentInstance{}, false, nil
}

func (e *environmentControllerMock) Release(
	ctx context.Context,
	instanceID string,
) error {
	if e.release != nil {
		return e.release(ctx, instanceID)
	}

	return nil
}

func (e *environmentControllerMock) ReleaseAll(ctx context.Context) error {
	if e.releaseAll != nil {
		return e.releaseAll(ctx)
	}

	return nil
}

func TestNewService(t *testing.T) {
	validConfig := &config.Config{
		WorkersPoolSize: 1,
		CleanupTimeout:  time.Second * 30,
	}

	validJobs := make(chan execute.TestJob)

	validWorker := testWorkerFunc(func(
		ctx context.Context,
		job execute.TestJob,
	) (*execute.TestExecutionResult, error) {
		return &execute.TestExecutionResult{}, nil
	})

	validStateUpdater := &stateUpdaterMock{}
	validEnvironment := &environmentControllerMock{}

	tests := []struct {
		name        string
		config      *config.Config
		jobs        <-chan execute.TestJob
		worker      testWorker
		updater     stateUpdater
		environment environmentController
		expectedErr error
	}{
		{
			name:        "valid",
			config:      validConfig,
			jobs:        validJobs,
			worker:      validWorker,
			updater:     validStateUpdater,
			environment: validEnvironment,
		},
		{
			name:        "nil config",
			jobs:        validJobs,
			worker:      validWorker,
			updater:     validStateUpdater,
			environment: validEnvironment,
			expectedErr: errors.ErrNilConfig,
		},
		{
			name:        "nil test jobs channel",
			config:      validConfig,
			worker:      validWorker,
			updater:     validStateUpdater,
			environment: validEnvironment,
			expectedErr: errors.ErrNilTestJobsChannel,
		},
		{
			name:        "nil test worker",
			config:      validConfig,
			jobs:        validJobs,
			updater:     validStateUpdater,
			environment: validEnvironment,
			expectedErr: errors.ErrNilTestWorker,
		},
		{
			name:        "nil state updater",
			config:      validConfig,
			jobs:        validJobs,
			worker:      validWorker,
			environment: validEnvironment,
			expectedErr: errors.ErrNilStateUpdater,
		},
		{
			name:        "nil environment controller",
			config:      validConfig,
			jobs:        validJobs,
			worker:      validWorker,
			updater:     validStateUpdater,
			expectedErr: errors.ErrNilEnvironmentController,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewService(
				tt.config,
				WithTestJobsChannel(tt.jobs),
				WithTestWorker(tt.worker),
				WithStateUpdater(tt.updater),
				WithEnvironmentController(tt.environment),
			)

			if !nativeerrors.Is(err, tt.expectedErr) {
				t.Fatalf(
					"expected error %v, got %v",
					tt.expectedErr,
					err,
				)
			}

			if tt.expectedErr != nil {
				if service != nil {
					t.Fatal("expected nil service")
				}

				return
			}

			if service == nil {
				t.Fatal("expected service")
			}
		})
	}
}

func TestServiceExecuteTest(t *testing.T) {
	errAcquire := nativeerrors.New("acquire failed")
	errSetSuiteRunning := nativeerrors.New("set suite running failed")
	errSetTestRunning := nativeerrors.New("set test running failed")
	errWorker := nativeerrors.New("worker failed")

	const (
		executionID      = "execution-1"
		suiteExecutionID = "suite-execution-1"
	)

	tests := []struct {
		name string

		suiteExecutionID string
		created          bool

		acquireErr         error
		setSuiteRunningErr error
		setTestRunningErr  error
		workerErr          error

		expectSuiteRunning bool
		expectTestRunning  bool
		expectWorker       bool
		expectErr          error
		expectErrorResult  bool
	}{
		{
			name:              "standalone test",
			expectTestRunning: true,
			expectWorker:      true,
		},
		{
			name:               "first test in suite",
			suiteExecutionID:   suiteExecutionID,
			created:            true,
			expectSuiteRunning: true,
			expectTestRunning:  true,
			expectWorker:       true,
		},
		{
			name:              "subsequent test in suite",
			suiteExecutionID:  suiteExecutionID,
			created:           false,
			expectTestRunning: true,
			expectWorker:      true,
		},
		{
			name:             "acquire fails",
			suiteExecutionID: suiteExecutionID,
			acquireErr:       errAcquire,
			expectErr:        errAcquire,
		},
		{
			name:               "set suite running fails",
			suiteExecutionID:   suiteExecutionID,
			created:            true,
			setSuiteRunningErr: errSetSuiteRunning,
			expectSuiteRunning: true,
			expectErr:          errSetSuiteRunning,
		},
		{
			name:              "set test running fails",
			suiteExecutionID:  suiteExecutionID,
			setTestRunningErr: errSetTestRunning,
			expectTestRunning: true,
			expectErr:         errSetTestRunning,
		},
		{
			name:              "worker fails",
			suiteExecutionID:  suiteExecutionID,
			expectTestRunning: true,
			expectWorker:      true,
			workerErr:         errWorker,
			expectErrorResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				suiteRunningCalled bool
				testRunningCalled  bool
				workerCalled       bool
			)

			calls := &call.Store{}

			environment := &environmentControllerMock{
				acquire: func(
					ctx context.Context,
					instanceID string,
					env *model.Environment,
				) (*runtime.EnvironmentInstance, bool, error) {
					if tt.acquireErr != nil {
						return nil, false, tt.acquireErr
					}

					expectedInstanceID := executionID
					if tt.suiteExecutionID != "" {
						expectedInstanceID = tt.suiteExecutionID
					}

					if instanceID != expectedInstanceID {
						t.Fatalf(
							"expected instance ID %q, got %q",
							expectedInstanceID,
							instanceID,
						)
					}

					return &runtime.EnvironmentInstance{
						Calls: calls,
					}, tt.created, nil
				},
			}

			updater := &stateUpdaterMock{
				setTestSuiteRunning: func(
					ctx context.Context,
					id string,
					startedAt time.Time,
				) error {
					suiteRunningCalled = true

					if id != suiteExecutionID {
						t.Fatalf(
							"expected suite execution ID %q, got %q",
							suiteExecutionID,
							id,
						)
					}

					if startedAt.IsZero() {
						t.Fatal("expected suite startedAt")
					}

					return tt.setSuiteRunningErr
				},

				setTestRunning: func(
					ctx context.Context,
					id string,
					startedAt time.Time,
				) error {
					testRunningCalled = true

					if id != executionID {
						t.Fatalf(
							"expected execution ID %q, got %q",
							executionID,
							id,
						)
					}

					if startedAt.IsZero() {
						t.Fatal("expected test startedAt")
					}

					return tt.setTestRunningErr
				},
			}

			expectedResult := &execute.TestExecutionResult{
				ExecutionID:          executionID,
				TestSuiteExecutionID: tt.suiteExecutionID,
				Status:               model.ExecutionStatusPassed,
			}

			worker := testWorkerFunc(func(
				ctx context.Context,
				job execute.TestJob,
			) (*execute.TestExecutionResult, error) {
				workerCalled = true

				if job.Runtime.Calls != calls {
					t.Fatal("expected environment calls to be assigned to job runtime")
				}

				if tt.workerErr != nil {
					return nil, tt.workerErr
				}

				return expectedResult, nil
			})

			service := &Service{
				testWorker:   worker,
				stateUpdater: updater,
				environment:  environment,
			}

			job := execute.TestJob{
				ExecutionID:          executionID,
				TestSuiteExecutionID: tt.suiteExecutionID,
				Environment:          model.Environment{},
			}

			result, err := service.executeTest(context.Background(), job)

			if !nativeerrors.Is(err, tt.expectErr) {
				t.Fatalf("expected error %v, got %v", tt.expectErr, err)
			}

			if suiteRunningCalled != tt.expectSuiteRunning {
				t.Errorf(
					"expected SetTestSuiteRunning called %v, got %v",
					tt.expectSuiteRunning,
					suiteRunningCalled,
				)
			}

			if testRunningCalled != tt.expectTestRunning {
				t.Errorf(
					"expected SetTestRunning called %v, got %v",
					tt.expectTestRunning,
					testRunningCalled,
				)
			}

			if workerCalled != tt.expectWorker {
				t.Errorf(
					"expected worker called %v, got %v",
					tt.expectWorker,
					workerCalled,
				)
			}

			if tt.expectErr != nil {
				if result != nil {
					t.Fatal("expected nil result")
				}

				return
			}

			if tt.expectErrorResult {
				if result == nil {
					t.Fatal("expected result")
				}

				if result.ExecutionID != executionID {
					t.Errorf(
						"expected execution ID %q, got %q",
						executionID,
						result.ExecutionID,
					)
				}

				if result.TestSuiteExecutionID != tt.suiteExecutionID {
					t.Errorf(
						"expected suite execution ID %q, got %q",
						tt.suiteExecutionID,
						result.TestSuiteExecutionID,
					)
				}

				if result.Status != model.ExecutionStatusError {
					t.Errorf(
						"expected status %q, got %q",
						model.ExecutionStatusError,
						result.Status,
					)
				}

				if result.FinishedAt.IsZero() {
					t.Error("expected FinishedAt")
				}

				if result.Summary == nil {
					t.Fatal("expected summary")
				}

				if result.Summary.Error != errWorker.Error() {
					t.Errorf(
						"expected summary error %q, got %q",
						errWorker.Error(),
						result.Summary.Error,
					)
				}

				return
			}

			if result != expectedResult {
				t.Fatal("expected worker result to be returned unchanged")
			}
		})
	}
}

func TestServiceRunNotStarted(t *testing.T) {
	service := &Service{}

	err := service.Run(context.Background())

	if !nativeerrors.Is(err, errors.ErrNotStarted) {
		t.Fatalf(
			"expected error %v, got %v",
			errors.ErrNotStarted,
			err,
		)
	}
}

func TestServiceRunWorkerError(t *testing.T) {
	workerErr := nativeerrors.New("worker stream failed")

	results := make(chan *execute.TestExecutionResult)
	errs := make(chan error, 1)
	errs <- workerErr
	close(errs)

	releaseAllCalls := 0

	service := &Service{
		cfg: &config.Config{
			CleanupTimeout: time.Second,
		},
		testResults: results,
		errs:        errs,
		environment: &environmentControllerMock{
			releaseAll: func(ctx context.Context) error {
				releaseAllCalls++

				return nil
			},
		},
	}

	err := service.Run(context.Background())

	if !nativeerrors.Is(err, workerErr) {
		t.Fatalf(
			"expected error %v, got %v",
			workerErr,
			err,
		)
	}

	if releaseAllCalls != 1 {
		t.Fatalf(
			"expected ReleaseAll called once, got %d",
			releaseAllCalls,
		)
	}
}

func TestServiceRunContextCanceled(t *testing.T) {
	results := make(chan *execute.TestExecutionResult)
	errs := make(chan error)

	releaseAllCalls := 0

	service := &Service{
		cfg: &config.Config{
			CleanupTimeout: time.Second,
		},
		testResults: results,
		errs:        errs,
		environment: &environmentControllerMock{
			releaseAll: func(ctx context.Context) error {
				releaseAllCalls++

				return nil
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.Run(ctx)

	if !nativeerrors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected error %v, got %v",
			context.Canceled,
			err,
		)
	}

	if releaseAllCalls != 1 {
		t.Fatalf(
			"expected ReleaseAll called once, got %d",
			releaseAllCalls,
		)
	}
}

func TestServiceLifecycle(t *testing.T) {
	const (
		executionID      = "execution-1"
		suiteExecutionID = "suite-execution-1"
	)

	errSetTestCompleted := nativeerrors.New("set test completed failed")
	errRelease := nativeerrors.New("release failed")
	errReleaseAll := nativeerrors.New("release all failed")

	tests := []struct {
		name string

		suiteExecutionID string
		completed        bool

		setTestCompletedErr error
		releaseErr          error
		releaseAllErr       error

		expectedReleaseCalls int
		expectedErr          error
	}{
		{
			name:                 "standalone test completed",
			completed:            true,
			expectedReleaseCalls: 1,
		},
		{
			name:                 "suite test completed",
			suiteExecutionID:     suiteExecutionID,
			completed:            true,
			expectedReleaseCalls: 1,
		},
		{
			name:                 "suite not completed",
			suiteExecutionID:     suiteExecutionID,
			completed:            false,
			expectedReleaseCalls: 0,
		},
		{
			name:                "set test completed fails",
			completed:           true,
			setTestCompletedErr: errSetTestCompleted,
			expectedErr:         errSetTestCompleted,
		},
		{
			name:                 "release fails",
			completed:            true,
			releaseErr:           errRelease,
			expectedReleaseCalls: 1,
			expectedErr:          errRelease,
		},
		{
			name:                 "release all fails",
			completed:            true,
			releaseAllErr:        errReleaseAll,
			expectedReleaseCalls: 1,
			expectedErr:          errReleaseAll,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := make(chan execute.TestJob, 1)

			var (
				workerCalls           int
				setTestCompletedCalls int
				releaseCalls          int
				releaseAllCalls       int
			)

			worker := testWorkerFunc(func(
				ctx context.Context,
				job execute.TestJob,
			) (*execute.TestExecutionResult, error) {
				workerCalls++

				return &execute.TestExecutionResult{
					ExecutionID:          job.ExecutionID,
					TestSuiteExecutionID: job.TestSuiteExecutionID,
					Status:               model.ExecutionStatusPassed,
					FinishedAt:           time.Now().UTC(),
				}, nil
			})

			updater := &stateUpdaterMock{
				setTestCompleted: func(
					ctx context.Context,
					result execute.TestExecutionResult,
				) (bool, error) {
					setTestCompletedCalls++

					if result.ExecutionID != executionID {
						t.Errorf(
							"expected execution ID %q, got %q",
							executionID,
							result.ExecutionID,
						)
					}

					if result.TestSuiteExecutionID != tt.suiteExecutionID {
						t.Errorf(
							"expected suite execution ID %q, got %q",
							tt.suiteExecutionID,
							result.TestSuiteExecutionID,
						)
					}

					return tt.completed, tt.setTestCompletedErr
				},
			}

			environment := &environmentControllerMock{
				acquire: func(
					ctx context.Context,
					instanceID string,
					env *model.Environment,
				) (*runtime.EnvironmentInstance, bool, error) {
					return &runtime.EnvironmentInstance{
						Calls: &call.Store{},
					}, false, nil
				},

				release: func(
					ctx context.Context,
					instanceID string,
				) error {
					releaseCalls++

					expectedInstanceID := executionID
					if tt.suiteExecutionID != "" {
						expectedInstanceID = tt.suiteExecutionID
					}

					if instanceID != expectedInstanceID {
						t.Errorf(
							"expected instance ID %q, got %q",
							expectedInstanceID,
							instanceID,
						)
					}

					return tt.releaseErr
				},

				releaseAll: func(ctx context.Context) error {
					releaseAllCalls++

					return tt.releaseAllErr
				},
			}

			service := &Service{
				cfg: &config.Config{
					WorkersPoolSize: 1,
					CleanupTimeout:  time.Second * 30,
				},
				testWorker:   worker,
				tests:        jobs,
				stateUpdater: updater,
				environment:  environment,
			}

			ctx := context.Background()

			if err := service.StartTestExecution(ctx); err != nil {
				t.Fatalf("StartTestExecution failed: %v", err)
			}

			jobs <- execute.TestJob{
				ExecutionID:          executionID,
				TestSuiteExecutionID: tt.suiteExecutionID,
				Environment:          model.Environment{},
			}

			close(jobs)

			err := service.Run(ctx)

			if !nativeerrors.Is(err, tt.expectedErr) {
				t.Errorf(
					"expected error %v, got %v",
					tt.expectedErr,
					err,
				)
			}

			if workerCalls != 1 {
				t.Errorf(
					"expected worker called once, got %d",
					workerCalls,
				)
			}

			if setTestCompletedCalls != 1 {
				t.Errorf(
					"expected SetTestCompleted called once, got %d",
					setTestCompletedCalls,
				)
			}

			if releaseCalls != tt.expectedReleaseCalls {
				t.Errorf(
					"expected Release called %d times, got %d",
					tt.expectedReleaseCalls,
					releaseCalls,
				)
			}

			if releaseAllCalls != 1 {
				t.Errorf(
					"expected ReleaseAll called once, got %d",
					releaseAllCalls,
				)
			}
		})
	}
}
