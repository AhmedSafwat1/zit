// Package tests is the black-box suite of the user module.
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/internal/modules/user/tests/mocks"
	"github.com/4jawaly/identity-gateway/pkg/zitadel"
)

const testIssuer = "https://zauth.4ja.org"

// runInline executes post-response work synchronously so tests can assert it.
func runInline(f func()) { f() }

// newTestService builds a UserService on fresh mocks with an inline runner.
func newTestService(t *testing.T, opts ...usersvc.ServiceOption) (*usersvc.UserService, *mocks.Cache, *mocks.ZitadelClient, *mocks.SyncNotifier) {
	t.Helper()
	c := mocks.NewCache(t)
	z := mocks.NewZitadelClient(t)
	n := mocks.NewSyncNotifier(t)
	base := []usersvc.ServiceOption{usersvc.WithSyncRunner(runInline), usersvc.WithIssuer(testIssuer)}
	svc := usersvc.NewUserService(z, c, n, append(base, opts...)...)
	return svc, c, z, n
}

// loadFixture reads testdata/<name> and decodes it into T.
func loadFixture[T any](t *testing.T, name string) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(readFixture(t, name), &v))
	return v
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// loadClaims parses a userinfo fixture the way the Zitadel client does.
func loadClaims(t *testing.T, name string) *zitadel.Claims {
	t.Helper()
	c, err := zitadel.ParseClaims(readFixture(t, name))
	require.NoError(t, err)
	return c
}

// adminReport is the expected report for userinfo_admin.json, spelled out
// by hand so the test does not validate the mapper with itself.
func adminReport() *userdto.IntrospectResponse {
	return &userdto.IntrospectResponse{
		OK:       true,
		Identity: userdto.Identity{Sub: "355646255235338042", Email: "admin@rawazin.example", Nickname: "شركة روازن الدولية"},
		Roles:    []string{"ADMIN", "jawaly_ai_super_admin"},
		IsAdmin:  true,
		MainAccount: userdto.MainAccount{
			Allowed: true, SubAccountsCount: 34, AccountID: "1", StatusName: "Active",
		},
		IsSubAccount: true,
		SubAccountOf: []string{"355646368934527195", "355646755028599003"},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
