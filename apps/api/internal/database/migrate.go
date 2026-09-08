package database

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/meterrail/api/internal/models"
)

// Migrate runs GORM's AutoMigrate across every model plus the extensions and
// hand-written indexes that AutoMigrate cannot express.
func Migrate(ctx context.Context, db *gorm.DB, logger *slog.Logger) error {
	db = db.WithContext(ctx)

	// gen_random_uuid() and trigram search both come from extensions.
	for _, ext := range []string{"pgcrypto", "pg_trgm"} {
		if err := db.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %q", ext)).Error; err != nil {
			// Managed Postgres (Neon/Supabase) may not grant extension
			// creation. Log and continue: nothing here is load-bearing.
			logger.Warn("could not create extension",
				slog.String("extension", ext), slog.String("error", err.Error()))
		}
	}

	if err := db.AutoMigrate(models.All()...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}

	for _, stmt := range postMigrationStatements() {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("post-migration %q: %w", stmt, err)
		}
	}

	logger.Info("migrations applied", slog.Int("models", len(models.All())))
	return nil
}

// postMigrationStatements holds partial and expression indexes that AutoMigrate
// has no struct-tag equivalent for.
func postMigrationStatements() []string {
	return []string{
		// Unread notification lookups are the hot path on the bell icon.
		`CREATE INDEX IF NOT EXISTS idx_notifications_unread
		   ON notifications (user_id, created_at DESC)
		 WHERE read_at IS NULL AND deleted_at IS NULL`,

		// Only one live session per channel at a time.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_live_channel
		   ON sessions (channel_name)
		 WHERE status = 'live' AND deleted_at IS NULL`,

		// Fuzzy org search by name.
		`CREATE INDEX IF NOT EXISTS idx_organizations_name_trgm
		   ON organizations USING gin (name gin_trgm_ops)`,

		// Reaping orphaned R2 objects scans pending uploads by age.
		`CREATE INDEX IF NOT EXISTS idx_media_pending_created
		   ON media_assets (created_at)
		 WHERE status = 'pending' AND deleted_at IS NULL`,

		// The Envio sync job pages by (chain, block, log index).
		`CREATE INDEX IF NOT EXISTS idx_onchain_cursor
		   ON onchain_events (chain_id, block_number DESC, log_index DESC)`,
	}
}

// Drop tears the schema down. Guarded by the caller; never call in production.
func Drop(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Migrator().DropTable(models.All()...)
}
