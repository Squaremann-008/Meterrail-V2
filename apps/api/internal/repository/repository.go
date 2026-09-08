// Package repository holds the data-access layer. Handlers never touch *gorm.DB
// directly; every query lives behind one of these types so caching and
// pagination stay consistent.
package repository

import (
	"errors"

	"gorm.io/gorm"
)

// ErrNotFound normalises GORM's sentinel so callers do not import gorm.
var ErrNotFound = errors.New("repository: not found")

// wrap converts driver errors into package-level sentinels.
func wrap(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// Repositories bundles every repository so wiring is a single struct.
type Repositories struct {
	Users         *UserRepository
	Organizations *OrganizationRepository
	Media         *MediaRepository
	Sessions      *SessionRepository
	Onchain       *OnchainRepository
	Notifications *NotificationRepository
	Audit         *AuditRepository
}

func New(db *gorm.DB) *Repositories {
	return &Repositories{
		Users:         &UserRepository{db: db},
		Organizations: &OrganizationRepository{db: db},
		Media:         &MediaRepository{db: db},
		Sessions:      &SessionRepository{db: db},
		Onchain:       &OnchainRepository{db: db},
		Notifications: &NotificationRepository{db: db},
		Audit:         &AuditRepository{db: db},
	}
}

// Page is the cursor-free pagination envelope used by list endpoints.
type Page struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Normalize clamps user-supplied paging into a safe range.
func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = 25
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// apply attaches LIMIT/OFFSET to a query.
func (p Page) apply(q *gorm.DB) *gorm.DB {
	n := p.Normalize()
	return q.Limit(n.Limit).Offset(n.Offset)
}
