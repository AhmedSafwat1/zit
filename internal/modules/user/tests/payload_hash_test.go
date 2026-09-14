package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
)

func TestUserSyncPayload_Hash(t *testing.T) {
	t.Parallel()

	base := samplePayload()
	tests := []struct {
		name     string
		mutate   func(p *sync_user.UserSyncPayload)
		wantSame bool
	}{
		{name: "identical payload hashes the same", mutate: func(*sync_user.UserSyncPayload) {}, wantSame: true},
		{name: "role order does not matter", mutate: func(p *sync_user.UserSyncPayload) { p.Roles = []string{"USER", "ADMIN"} }, wantSame: false},
		{name: "roles in either order hash the same", mutate: func(p *sync_user.UserSyncPayload) {
			p.Roles = []string{"ADMIN"}
		}, wantSame: true},
		{name: "sub_account_of order does not matter", mutate: func(p *sync_user.UserSyncPayload) {
			p.SubAccountOf = []string{"355646368934527195"}
		}, wantSame: true},
		{name: "origin bytes are not part of the hash", mutate: func(p *sync_user.UserSyncPayload) { p.Origin = []byte(`{"other":1}`) }, wantSame: true},
		{name: "account type name is not part of the hash", mutate: func(p *sync_user.UserSyncPayload) { p.MainAccount.AccountTypeName = "x" }, wantSame: true},
		{name: "status change changes the hash", mutate: func(p *sync_user.UserSyncPayload) { p.MainAccount.StatusName = "Suspended" }, wantSame: false},
		{name: "sub accounts count change changes the hash", mutate: func(p *sync_user.UserSyncPayload) { p.MainAccount.SubAccountsCount = 35 }, wantSame: false},
		{name: "nickname change changes the hash", mutate: func(p *sync_user.UserSyncPayload) { p.Nickname = "x" }, wantSame: false},
		{name: "email change changes the hash", mutate: func(p *sync_user.UserSyncPayload) { p.Email = "x@y" }, wantSame: false},
		{name: "new parent changes the hash", mutate: func(p *sync_user.UserSyncPayload) { p.SubAccountOf = append(p.SubAccountOf, "9") }, wantSame: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := samplePayload()
			tt.mutate(&p)
			if tt.wantSame {
				assert.Equal(t, base.Hash(), p.Hash())
			} else {
				assert.NotEqual(t, base.Hash(), p.Hash())
			}
			assert.Len(t, p.Hash(), 64)
		})
	}
}

func TestUserSyncPayload_Hash_OrderIndependent(t *testing.T) {
	t.Parallel()
	a := dto.UserSyncPayload{Sub: "1", Roles: []string{"b", "a", "c"}, SubAccountOf: []string{"3", "1", "2"}}
	b := dto.UserSyncPayload{Sub: "1", Roles: []string{"c", "b", "a"}, SubAccountOf: []string{"1", "2", "3"}}
	assert.Equal(t, a.Hash(), b.Hash())
}
