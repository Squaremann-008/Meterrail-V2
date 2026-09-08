package models

import (
	"time"

	"github.com/google/uuid"
)

// OnchainEvent mirrors a record produced by the Envio HyperIndex indexer. The
// indexer writes to its own database; the sync job copies the slice this API
// serves so reads do not depend on the indexer being up.
type OnchainEvent struct {
	Base
	// EnvioID is the indexer's primary key, used to dedupe on re-sync.
	EnvioID         string     `gorm:"size:255;uniqueIndex;not null" json:"envioId"`
	ChainID         int64      `gorm:"not null;index:idx_onchain_chain_block,priority:1" json:"chainId"`
	BlockNumber     int64      `gorm:"not null;index:idx_onchain_chain_block,priority:2" json:"blockNumber"`
	BlockTimestamp  time.Time  `gorm:"not null;index" json:"blockTimestamp"`
	TransactionHash string     `gorm:"size:80;not null;index" json:"transactionHash"`
	LogIndex        int        `gorm:"not null" json:"logIndex"`
	ContractAddress string     `gorm:"size:80;not null;index" json:"contractAddress"`
	EventName       string     `gorm:"size:128;not null;index" json:"eventName"`
	Payload         JSONMap    `gorm:"type:jsonb" json:"payload"`
	UserID          *uuid.UUID `gorm:"type:uuid;index" json:"userId,omitempty"`
	ProcessedAt     *time.Time `json:"processedAt,omitempty"`
}

func (OnchainEvent) TableName() string { return "onchain_events" }

// IndexerCheckpoint remembers how far the sync job has read per chain so each
// run only pulls the tail.
type IndexerCheckpoint struct {
	Base
	ChainID          int64     `gorm:"uniqueIndex;not null" json:"chainId"`
	LastBlockNumber  int64     `gorm:"not null;default:0" json:"lastBlockNumber"`
	LastSyncedAt     time.Time `json:"lastSyncedAt"`
	LastEventEnvioID string    `gorm:"size:255" json:"lastEventEnvioId,omitempty"`
}

func (IndexerCheckpoint) TableName() string { return "indexer_checkpoints" }
