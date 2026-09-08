package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/meterrail/api/internal/cache"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/models"
	"github.com/meterrail/api/internal/repository"
)

// syncBatchSize is how many indexed rows one sync pass pulls per round trip.
const syncBatchSize = 500

type OnchainService struct{ deps Deps }

func (s *OnchainService) List(ctx context.Context, f repository.OnchainFilter) ([]models.OnchainEvent, int64, error) {
	if f.Address != "" {
		f.Address = strings.ToLower(f.Address)
	}
	return s.deps.Repos.Onchain.List(ctx, f)
}

// Sync pulls everything the indexer has produced past our checkpoint for one
// chain and mirrors it locally. It loops until a short page arrives, so a
// backlog drains in a single run.
func (s *OnchainService) Sync(ctx context.Context, chainID int64, batchSize int) (int, error) {
	if s.deps.Envio == nil {
		return 0, httpx.ServiceUnavailable("envio indexer")
	}
	if batchSize <= 0 {
		batchSize = syncBatchSize
	}

	checkpoint, err := s.deps.Repos.Onchain.Checkpoint(ctx, chainID)
	if err != nil {
		return 0, fmt.Errorf("read checkpoint for chain %d: %w", chainID, err)
	}

	cursor := checkpoint.LastBlockNumber
	total := 0

	for {
		transfers, err := s.deps.Envio.TransfersAfter(ctx, chainID, cursor, batchSize)
		if err != nil {
			return total, fmt.Errorf("fetch transfers after block %d: %w", cursor, err)
		}
		if len(transfers) == 0 {
			break
		}

		events := make([]models.OnchainEvent, 0, len(transfers))
		for i := range transfers {
			t := &transfers[i]
			event := models.OnchainEvent{
				EnvioID:         t.ID,
				ChainID:         t.ChainID,
				BlockNumber:     t.BlockNumber,
				BlockTimestamp:  t.BlockTime(),
				TransactionHash: t.TxHash,
				LogIndex:        t.LogIndex,
				ContractAddress: strings.ToLower(t.Contract),
				EventName:       "Transfer",
				Payload: models.JSONMap{
					"from":  strings.ToLower(t.From),
					"to":    strings.ToLower(t.To),
					"value": t.Value,
				},
			}
			// Attribute the event to a local account when the recipient is a
			// wallet we know about.
			if user, err := s.deps.Repos.Users.ByWallet(ctx, models.ChainEVM, t.To); err == nil {
				event.UserID = &user.ID
			} else if !errors.Is(err, repository.ErrNotFound) {
				s.deps.Logger.Warn("wallet attribution failed", slog.String("error", err.Error()))
			}
			events = append(events, event)
		}

		if _, err := s.deps.Repos.Onchain.UpsertBatch(ctx, events); err != nil {
			return total, fmt.Errorf("persist %d events: %w", len(events), err)
		}

		last := transfers[len(transfers)-1]
		if err := s.deps.Repos.Onchain.SaveCheckpoint(ctx, chainID, last.BlockNumber, last.ID); err != nil {
			return total, fmt.Errorf("save checkpoint: %w", err)
		}

		total += len(transfers)
		cursor = last.BlockNumber

		// A short page means we have caught up with the indexer.
		if len(transfers) < batchSize {
			break
		}
	}

	if total > 0 {
		s.deps.Logger.Info("indexer sync complete",
			slog.Int64("chain_id", chainID), slog.Int("events", total), slog.Int64("block", cursor))
		// New events invalidate every cached listing for this chain.
		if _, err := s.deps.Cache.DeletePrefix(ctx, fmt.Sprintf("onchain:%d", chainID)); err != nil {
			s.deps.Logger.Warn("onchain cache eviction failed", slog.String("error", err.Error()))
		}
	}
	return total, nil
}

// IndexerStatus reports the indexer's head against our mirrored checkpoint so
// operators can see sync lag. Cached briefly because the dashboard polls it.
func (s *OnchainService) IndexerStatus(ctx context.Context) (any, error) {
	if s.deps.Envio == nil {
		return nil, httpx.ServiceUnavailable("envio indexer")
	}

	type chainStatus struct {
		ChainID       int64      `json:"chainId"`
		IndexerHead   int64      `json:"indexerHead"`
		MirroredBlock int64      `json:"mirroredBlock"`
		LagBlocks     int64      `json:"lagBlocks"`
		LastSyncedAt  *time.Time `json:"lastSyncedAt,omitempty"`
	}

	return cache.Remember(ctx, s.deps.Cache, "onchain:status", 15*time.Second,
		func(ctx context.Context) ([]chainStatus, error) {
			heads, err := s.deps.Envio.SyncStatus(ctx)
			if err != nil {
				return nil, err
			}
			checkpoints, err := s.deps.Repos.Onchain.Checkpoints(ctx)
			if err != nil {
				return nil, err
			}

			mirrored := make(map[int64]models.IndexerCheckpoint, len(checkpoints))
			for i := range checkpoints {
				mirrored[checkpoints[i].ChainID] = checkpoints[i]
			}

			out := make([]chainStatus, 0, len(heads))
			for _, head := range heads {
				status := chainStatus{ChainID: head.ChainID, IndexerHead: head.BlockHeight}
				if cp, ok := mirrored[head.ChainID]; ok {
					status.MirroredBlock = cp.LastBlockNumber
					status.LagBlocks = head.BlockHeight - cp.LastBlockNumber
					synced := cp.LastSyncedAt
					status.LastSyncedAt = &synced
				} else {
					status.LagBlocks = head.BlockHeight
				}
				out = append(out, status)
			}
			return out, nil
		})
}
