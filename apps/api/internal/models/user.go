package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return true
	}
	return false
}

// Rank orders roles so permission checks can ask "at least admin".
func (r Role) Rank() int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleMember:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// User is the local mirror of a Dynamic identity. DynamicUserID is the join key
// against the `sub` claim of the verified JWT.
type User struct {
	Base
	DynamicUserID string     `gorm:"size:128;uniqueIndex;not null" json:"dynamicUserId"`
	Email         *string    `gorm:"size:320;uniqueIndex" json:"email,omitempty"`
	Username      *string    `gorm:"size:64;uniqueIndex" json:"username,omitempty"`
	DisplayName   string     `gorm:"size:128" json:"displayName"`
	AvatarURL     string     `gorm:"size:1024" json:"avatarUrl,omitempty"`
	IsActive      bool       `gorm:"not null;default:true;index" json:"isActive"`
	LastSeenAt    *time.Time `json:"lastSeenAt,omitempty"`

	Wallets     []Wallet     `gorm:"constraint:OnDelete:CASCADE" json:"wallets,omitempty"`
	Memberships []Membership `gorm:"constraint:OnDelete:CASCADE" json:"memberships,omitempty"`
}

func (User) TableName() string { return "users" }

type WalletChain string

const (
	ChainEVM    WalletChain = "evm"
	ChainSolana WalletChain = "solana"
)

// Wallet is a verified wallet linked to a user by Dynamic.
type Wallet struct {
	Base
	UserID     uuid.UUID   `gorm:"type:uuid;not null;index" json:"userId"`
	Address    string      `gorm:"size:128;not null;uniqueIndex:idx_wallets_chain_address,priority:2" json:"address"`
	Chain      WalletChain `gorm:"size:32;not null;uniqueIndex:idx_wallets_chain_address,priority:1" json:"chain"`
	ChainID    *int64      `json:"chainId,omitempty"`
	Provider   string      `gorm:"size:64" json:"provider,omitempty"`
	IsPrimary  bool        `gorm:"not null;default:false" json:"isPrimary"`
	VerifiedAt *time.Time  `json:"verifiedAt,omitempty"`

	User *User `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}

func (Wallet) TableName() string { return "wallets" }

// NormalizeAddress lowercases EVM addresses so lookups are case-insensitive.
// Solana addresses are base58 and case-significant, so they are left alone.
func NormalizeAddress(chain WalletChain, address string) string {
	address = strings.TrimSpace(address)
	if chain == ChainEVM {
		return strings.ToLower(address)
	}
	return address
}
