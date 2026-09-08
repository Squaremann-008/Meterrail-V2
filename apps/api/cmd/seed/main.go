// Command seed populates a development database with a coherent set of
// records: a user with a wallet, an organization, a scheduled session and a
// handful of notifications. It is idempotent, so re-running it is safe.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/meterrail/api/internal/config"
	"github.com/meterrail/api/internal/database"
	"github.com/meterrail/api/internal/logging"
	"github.com/meterrail/api/internal/models"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	force := flag.Bool("force", false, "allow seeding a production database")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Seed data in production would create real-looking accounts nobody owns.
	if cfg.App.IsProduction() && !*force {
		return errors.New("refusing to seed a production database without -force")
	}

	logger := logging.New(cfg.App.LogLevel, cfg.App.LogFormat, "seed", cfg.App.Environment)
	ctx := context.Background()

	db, err := database.Open(ctx, cfg.Database, logger)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close(db) }()

	// Seeding an unmigrated database produces confusing "relation does not
	// exist" errors, so make sure the schema is there first.
	if err := database.Migrate(ctx, db, logger); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := seedUser(tx)
		if err != nil {
			return err
		}
		org, err := seedOrganization(tx, user)
		if err != nil {
			return err
		}
		if err := seedSession(tx, user, org); err != nil {
			return err
		}
		if err := seedNotifications(tx, user); err != nil {
			return err
		}

		logger.Info("seed complete",
			slog.String("user_id", user.ID.String()),
			slog.String("organization_id", org.ID.String()),
		)
		return nil
	})
}

// seedUser creates (or reuses) the demo account. The Dynamic id is a fixed
// sentinel so re-running never forks a second user.
func seedUser(tx *gorm.DB) (*models.User, error) {
	const dynamicID = "seed-dynamic-user-0001"
	email := "demo@meterrail.local"

	var user models.User
	err := tx.First(&user, "dynamic_user_id = ?", dynamicID).Error
	if err == nil {
		return &user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	user = models.User{
		DynamicUserID: dynamicID,
		Email:         &email,
		DisplayName:   "Demo Operator",
		IsActive:      true,
	}
	if err := tx.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("create seed user: %w", err)
	}

	now := time.Now().UTC()
	wallet := models.Wallet{
		UserID:     user.ID,
		Address:    models.NormalizeAddress(models.ChainEVM, "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"),
		Chain:      models.ChainEVM,
		ChainID:    ptr(int64(1)),
		Provider:   "seed",
		IsPrimary:  true,
		VerifiedAt: &now,
	}
	if err := tx.Create(&wallet).Error; err != nil {
		return nil, fmt.Errorf("create seed wallet: %w", err)
	}

	return &user, nil
}

func seedOrganization(tx *gorm.DB, owner *models.User) (*models.Organization, error) {
	const slug = "demo-org"

	var org models.Organization
	err := tx.First(&org, "slug = ?", slug).Error
	if err == nil {
		return &org, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	org = models.Organization{
		Slug:        slug,
		Name:        "Demo Organization",
		Description: "Seeded workspace for local development.",
		OwnerID:     owner.ID,
	}
	if err := tx.Create(&org).Error; err != nil {
		return nil, fmt.Errorf("create seed organization: %w", err)
	}

	membership := models.Membership{
		UserID:         owner.ID,
		OrganizationID: org.ID,
		Role:           models.RoleOwner,
	}
	if err := tx.Create(&membership).Error; err != nil {
		return nil, fmt.Errorf("create seed membership: %w", err)
	}

	return &org, nil
}

func seedSession(tx *gorm.DB, host *models.User, org *models.Organization) error {
	const channel = "mr-seedchannel01"

	var existing int64
	if err := tx.Model(&models.Session{}).Where("channel_name = ?", channel).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	scheduled := time.Now().UTC().Add(time.Hour)
	session := models.Session{
		HostID:          host.ID,
		OrganizationID:  &org.ID,
		ChannelName:     channel,
		Title:           "Weekly sync",
		Description:     "Seeded Agora session for local development.",
		Status:          models.SessionScheduled,
		MaxParticipants: 16,
		ScheduledFor:    &scheduled,
	}
	if err := tx.Create(&session).Error; err != nil {
		return fmt.Errorf("create seed session: %w", err)
	}
	return nil
}

func seedNotifications(tx *gorm.DB, user *models.User) error {
	var existing int64
	if err := tx.Model(&models.Notification{}).Where("user_id = ?", user.ID).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	notifications := []models.Notification{
		{
			UserID: user.ID, Channel: models.NotifyInApp, Kind: "welcome",
			Title: "Welcome to Meterrail", Body: "Your seeded account is ready.",
		},
		{
			UserID: user.ID, Channel: models.NotifyInApp, Kind: "session.scheduled",
			Title: "Weekly sync scheduled", Body: "Starts in an hour.",
		},
	}
	if err := tx.Create(&notifications).Error; err != nil {
		return fmt.Errorf("create seed notifications: %w", err)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
