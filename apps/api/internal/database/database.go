// Package database opens the GORM connection to Postgres and exposes the
// migration entrypoints.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/meterrail/api/internal/config"
)

// Open dials Postgres and applies the pool settings. It pings before returning
// so a bad DATABASE_URL fails at boot instead of on first request.
func Open(ctx context.Context, cfg config.Database, logger *slog.Logger) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger:                                   newGormLogger(logger, cfg.SlowThreshold),
		SkipDefaultTransaction:                   true,
		PrepareStmt:                              true,
		DisableForeignKeyConstraintWhenMigrating: false,
		NowFunc:                                  func() time.Time { return time.Now().UTC() },
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: cfg.URL,
		// Neon's and Supabase's transaction-mode poolers do not support the
		// extended protocol's prepared-statement cache across connections.
		PreferSimpleProtocol: false,
	}), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("resolve sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	logger.Info("database connected",
		slog.Int("max_open_conns", cfg.MaxOpenConns),
		slog.Int("max_idle_conns", cfg.MaxIdleConns),
	)
	return db, nil
}

// Close releases the underlying pool.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Health runs a cheap round trip used by the readiness probe.
func Health(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// newGormLogger bridges GORM's logger interface onto slog.
func newGormLogger(logger *slog.Logger, slow time.Duration) gormlogger.Interface {
	return gormlogger.New(
		slogWriter{logger: logger},
		gormlogger.Config{
			SlowThreshold:             slow,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			Colorful:                  false,
		},
	)
}

type slogWriter struct{ logger *slog.Logger }

func (w slogWriter) Printf(format string, args ...any) {
	w.logger.Warn("gorm", slog.String("message", fmt.Sprintf(format, args...)))
}
