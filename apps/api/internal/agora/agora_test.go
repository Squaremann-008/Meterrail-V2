package agora

import (
	"testing"
	"time"

	"github.com/meterrail/api/internal/config"
)

func testService(t *testing.T) *Service {
	t.Helper()

	svc, err := New(config.Agora{
		AppID:          "0123456789abcdef0123456789abcdef",
		AppCertificate: "fedcba9876543210fedcba9876543210",
		TokenTTL:       time.Hour,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New(config.Agora{AppID: "only-an-app-id"}); err == nil {
		t.Fatal("expected New to reject a config with no app certificate")
	}
}

func TestRTCTokenIsSignedAndScoped(t *testing.T) {
	svc := testService(t)

	token, err := svc.RTCToken("channel-1", 42, RolePublisher)
	if err != nil {
		t.Fatalf("RTCToken: %v", err)
	}

	if token.Token == "" {
		t.Error("expected a non-empty token")
	}
	// AccessToken2 credentials are prefixed with their version.
	if len(token.Token) < 3 || token.Token[:3] != "007" {
		t.Errorf("expected an AccessToken2 (007…) credential, got %q", token.Token)
	}
	if token.Channel != "channel-1" {
		t.Errorf("Channel = %q, want channel-1", token.Channel)
	}
	if token.UID != 42 {
		t.Errorf("UID = %d, want 42", token.UID)
	}
	if !token.ExpiresAt.After(time.Now().UTC()) {
		t.Error("expected ExpiresAt to be in the future")
	}
}

func TestRTCTokenRejectsEmptyChannel(t *testing.T) {
	if _, err := testService(t).RTCToken("", 1, RoleSubscriber); err == nil {
		t.Fatal("expected an empty channel to be rejected")
	}
}

// Two roles over the same channel and uid must produce different credentials,
// otherwise the publish privilege would not actually be enforced.
func TestRTCTokenVariesByRole(t *testing.T) {
	svc := testService(t)

	publisher, err := svc.RTCToken("channel-1", 42, RolePublisher)
	if err != nil {
		t.Fatalf("publisher token: %v", err)
	}
	subscriber, err := svc.RTCToken("channel-1", 42, RoleSubscriber)
	if err != nil {
		t.Fatalf("subscriber token: %v", err)
	}

	if publisher.Token == subscriber.Token {
		t.Error("publisher and subscriber tokens should differ")
	}
}

func TestRTMTokenRequiresUserID(t *testing.T) {
	if _, err := testService(t).RTMToken(""); err == nil {
		t.Fatal("expected an empty user id to be rejected")
	}
}

func TestDeriveUIDIsStableAndNonZero(t *testing.T) {
	const seed = "6f1a1f0e-0000-7000-8000-000000000001"

	first := DeriveUID(seed)
	if first != DeriveUID(seed) {
		t.Error("DeriveUID must be deterministic for a given seed")
	}
	// uid 0 tells the Agora SDK to allocate its own, which would break the
	// binding between the token and the joining client.
	if first == 0 {
		t.Error("DeriveUID must never return 0")
	}
	if DeriveUID("a different user") == first {
		t.Error("distinct seeds should not collide on this input")
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		in      string
		want    Role
		wantErr bool
	}{
		{"publisher", RolePublisher, false},
		{"subscriber", RoleSubscriber, false},
		{"", RoleSubscriber, false}, // default to the least privileged role
		{"admin", "", true},
	}

	for _, tt := range tests {
		got, err := ParseRole(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseRole(%q) expected an error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRole(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseRole(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
