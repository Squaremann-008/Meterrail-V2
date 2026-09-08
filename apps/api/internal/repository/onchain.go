package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/meterrail/api/internal/models"
)

type OnchainRepository struct{ db *gorm.DB }

// UpsertBatch writes a page of indexed events idempotently. Re-running a sync
// over the same range is a no-op, which is what makes the checkpoint safe to
// rewind after a reorg.
func (r *OnchainRepository) UpsertBatch(ctx context.Context, events []models.OnchainEvent) (int64, error) {
	if len(events) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "envio_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"payload", "block_timestamp", "user_id", "updated_at"}),
	}).CreateInBatches(events, 200)
	return res.RowsAffected, wrap(res.Error)
}

type OnchainFilter struct {
	ChainID   *int64
	EventName string
	Contract  string
	Address   string
	Page      Page
}

func (r *OnchainRepository) List(ctx context.Context, f OnchainFilter) ([]models.OnchainEvent, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.OnchainEvent{})
	if f.ChainID != nil {
		q = q.Where("chain_id = ?", *f.ChainID)
	}
	if f.EventName != "" {
		q = q.Where("event_name = ?", f.EventName)
	}
	if f.Contract != "" {
		q = q.Where("contract_address = ?", f.Contract)
	}
	if f.Address != "" {
		// Match either side of a transfer inside the jsonb payload.
		q = q.Where("payload->>'from' = ? OR payload->>'to' = ?", f.Address, f.Address)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap(err)
	}

	var events []models.OnchainEvent
	err := f.Page.apply(q).
		Order("block_number DESC, log_index DESC").
		Find(&events).Error
	return events, total, wrap(err)
}

// Checkpoint reads the sync cursor for a chain, creating it at zero on first
// run so the caller always gets a usable value.
func (r *OnchainRepository) Checkpoint(ctx context.Context, chainID int64) (*models.IndexerCheckpoint, error) {
	var cp models.IndexerCheckpoint
	err := r.db.WithContext(ctx).First(&cp, "chain_id = ?", chainID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cp = models.IndexerCheckpoint{ChainID: chainID}
		if err := r.db.WithContext(ctx).Create(&cp).Error; err != nil {
			return nil, wrap(err)
		}
		return &cp, nil
	}
	return &cp, wrap(err)
}

func (r *OnchainRepository) SaveCheckpoint(ctx context.Context, chainID, blockNumber int64, lastEnvioID string) error {
	return wrap(r.db.WithContext(ctx).Model(&models.IndexerCheckpoint{}).
		Where("chain_id = ?", chainID).
		Updates(map[string]any{
			"last_block_number":   blockNumber,
			"last_event_envio_id": lastEnvioID,
			"last_synced_at":      time.Now().UTC(),
		}).Error)
}

func (r *OnchainRepository) Checkpoints(ctx context.Context) ([]models.IndexerCheckpoint, error) {
	var cps []models.IndexerCheckpoint
	err := r.db.WithContext(ctx).Order("chain_id ASC").Find(&cps).Error
	return cps, wrap(err)
}
