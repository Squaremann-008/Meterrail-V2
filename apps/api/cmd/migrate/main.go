// Command migrate applies (or tears down) the database schema.
//
//	migrate            apply migrations
//	migrate -action=status  print the current table list
//	migrate -action=reset   drop and recreate everything (never in production)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/meterrail/api/internal/config"
	"github.com/meterrail/api/internal/database"
	"github.com/meterrail/api/internal/logging"
	"github.com/meterrail/api/internal/models"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	action := flag.String("action", "up", "up | status | reset")
	force := flag.Bool("force", false, "allow a destructive action in a production environment")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.App.LogLevel, cfg.App.LogFormat, "migrate", cfg.App.Environment)
	ctx := context.Background()

	db, err := database.Open(ctx, cfg.Database, logger)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	switch *action {
	case "up":
		return database.Migrate(ctx, db, logger)

	case "status":
		migrator := db.Migrator()
		for _, model := range models.All() {
			table := db.NamingStrategy.TableName(fmt.Sprintf("%T", model))
			exists := migrator.HasTable(model)
			status := "missing"
			if exists {
				status = "present"
			}
			logger.Info("table", slog.String("model", fmt.Sprintf("%T", model)),
				slog.String("table", table), slog.String("status", status))
		}
		return nil

	case "reset":
		// Dropping every table is unrecoverable, so production needs an
		// explicit -force in addition to the flag itself.
		if cfg.App.IsProduction() && !*force {
			return fmt.Errorf("refusing to reset a production database without -force")
		}
		logger.Warn("dropping all tables")
		if err := database.Drop(ctx, db); err != nil {
			return fmt.Errorf("drop tables: %w", err)
		}
		return database.Migrate(ctx, db, logger)

	default:
		return fmt.Errorf("unknown action %q: expected up, status or reset", *action)
	}
}
