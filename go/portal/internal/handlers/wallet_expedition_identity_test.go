package handlers

import (
	"testing"

	"github.com/senderzz/portal-service/internal/auth"
)

func TestExpeditionWalletUserIDUsesNativePortalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user *auth.PortalUser
		want int64
	}{
		{
			name: "legacy-linked producer still uses native portal id",
			user: &auth.PortalUser{ID: 15, WPUserID: 21},
			want: 15,
		},
		{
			name: "native producer uses native portal id",
			user: &auth.PortalUser{ID: 51},
			want: 51,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := expeditionWalletUserID(tt.user); got != tt.want {
				t.Fatalf("expeditionWalletUserID() = %d, want %d", got, tt.want)
			}
		})
	}
}
