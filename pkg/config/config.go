package config

import (
	"time"

	coreworker "github.com/e2engine/core/execute/worker"
)

type Config struct {
	WorkersPoolSize int                `yaml:"workers_pool_size" env:"WORKERS_POOL_SIZE" default:"8" validate:"min(1)"`
	CleanupTimeout  time.Duration      `yaml:"cleanup_timeout" env:"CLEANUP_TIMEOUT" default:"30s" validate:"min(1s)"`
	Worker          *coreworker.Config `yaml:"worker" env:"WORKER" default:"dive" validateElem:"dive"`
}
