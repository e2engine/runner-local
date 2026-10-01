package errors

import (
	"github.com/ygrebnov/errorc"
)

var (
	ErrNilConfig                = errorc.New("config is nil")
	ErrNilTestJobsChannel       = errorc.New("test jobs channel is nil")
	ErrNilTestWorker            = errorc.New("test worker is nil")
	ErrNilStateUpdater          = errorc.New("state updater is nil")
	ErrNilEnvironmentController = errorc.New("environment controller is nil")
	ErrNotStarted               = errorc.New("local runner has not started yet")
)
