package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	userrepo "github.com/4jawaly/identity-gateway/internal/modules/user/repositories"
	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
)

// startPostgres boots postgres:16, applies the real migrations and returns a GORM handle.
func startPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("testcontainers: skipped in -short mode")
	}
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("identity"),
		tcpostgres.WithUsername("identity"),
		tcpostgres.WithPassword("identity"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	driver, err := migratepg.WithInstance(sqlDB, &migratepg.Config{})
	require.NoError(t, err)
	src, err := iofs.New(os.DirFS(filepath.Join("..", "..", "..", "..", "migrations")), ".")
	require.NoError(t, err)
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	require.NoError(t, sqlDB.Close())

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	return db
}

type userRow struct {
	Sub          string
	Email        *string
	Nickname     *string
	Snapshot     *string
	IdentityHash *string
	SyncedAt     *time.Time
	UpdatedAt    time.Time
}

func getUser(t *testing.T, db *gorm.DB, sub string) *userRow {
	t.Helper()
	var r userRow
	res := db.Raw(`SELECT sub, email, nickname, snapshot::text AS snapshot, identity_hash, synced_at, updated_at FROM users WHERE sub = ?`, sub).Scan(&r)
	require.NoError(t, res.Error)
	if res.RowsAffected == 0 {
		return nil
	}
	return &r
}

func snapshotOf(t *testing.T, r *userRow) userdto.UserSyncPayload {
	t.Helper()
	var p userdto.UserSyncPayload
	require.NotNil(t, r.Snapshot)
	require.NoError(t, json.Unmarshal([]byte(*r.Snapshot), &p))
	return p
}

func parentsOf(t *testing.T, db *gorm.DB, childSub string) []string {
	t.Helper()
	var subs []string
	require.NoError(t, db.Raw(`SELECT p.sub FROM sub_accounts sa JOIN users c ON c.id = sa.child_user_id JOIN users p ON p.id = sa.parent_user_id WHERE c.sub = ? ORDER BY p.sub`, childSub).Scan(&subs).Error)
	return subs
}

func payloadFor(sub string, parents ...string) userdto.UserSyncPayload {
	return userdto.UserSyncPayload{
		Sub: sub, Email: sub + "@example.test", Nickname: "شركة " + sub,
		Roles: []string{"USER"},
		MainAccount: userdto.MainAccountPayload{
			Allowed: false, SubAccountsCount: 0, AccountID: "1", AccountTypeName: "Company", StatusName: "Active",
		},
		SubAccountOf: parents,
		Origin:       json.RawMessage(`{"sub":"` + sub + `","v":1}`),
	}
}

func TestSyncService_Sync(t *testing.T) {
	t.Parallel()
	db := startPostgres(t)
	svc := usersvc.NewSyncService(userrepo.NewUserRepository(db))
	ctx := context.Background()
	t0 := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	t2 := t1.Add(time.Minute)

	tests := []struct {
		name string
		run  func(t *testing.T, sub string)
	}{
		{
			name: "fresh insert stores columns, snapshot and hash",
			run: func(t *testing.T, sub string) {
				p := payloadFor(sub, "parent-"+sub)
				require.NoError(t, svc.Sync(ctx, p, t0))
				row := getUser(t, db, sub)
				require.NotNil(t, row)
				require.NotNil(t, row.Email)
				assert.Equal(t, sub+"@example.test", *row.Email)
				require.NotNil(t, row.Nickname)
				assert.Equal(t, "شركة "+sub, *row.Nickname)
				require.NotNil(t, row.IdentityHash)
				assert.Equal(t, p.Hash(), *row.IdentityHash)
				require.NotNil(t, row.SyncedAt)
				assert.True(t, t0.Equal(*row.SyncedAt))
				snap := snapshotOf(t, row)
				assert.Equal(t, []string{"USER"}, snap.Roles)
				assert.Equal(t, []string{"parent-" + sub}, snap.SubAccountOf)
				assert.Equal(t, "Active", snap.MainAccount.StatusName)
				assert.JSONEq(t, string(p.Origin), string(snap.Origin), "raw claims kept inside the snapshot")
			},
		},
		{
			name: "replaying the same message is a no-op",
			run: func(t *testing.T, sub string) {
				require.NoError(t, svc.Sync(ctx, payloadFor(sub), t0))
				before := getUser(t, db, sub)
				require.NoError(t, svc.Sync(ctx, payloadFor(sub), t0))
				after := getUser(t, db, sub)
				assert.Equal(t, before.UpdatedAt, after.UpdatedAt)
				assert.Equal(t, before.SyncedAt, after.SyncedAt)
			},
		},
		{
			name: "out-of-order older message is rejected by the stale guard",
			run: func(t *testing.T, sub string) {
				newer := payloadFor(sub, "parent-"+sub)
				newer.Nickname = "newer"
				require.NoError(t, svc.Sync(ctx, newer, t1))

				older := payloadFor(sub)
				older.Nickname = "older"
				require.NoError(t, svc.Sync(ctx, older, t0))

				row := getUser(t, db, sub)
				assert.Equal(t, "newer", *row.Nickname)
				assert.True(t, t1.Equal(*row.SyncedAt))
				assert.Equal(t, []string{"parent-" + sub}, parentsOf(t, db, sub), "edges from the newer message survive")
			},
		},
		{
			name: "newer message with an unchanged identity hash does not rewrite the row",
			run: func(t *testing.T, sub string) {
				require.NoError(t, svc.Sync(ctx, payloadFor(sub), t0))
				before := getUser(t, db, sub)

				same := payloadFor(sub)
				same.Origin = json.RawMessage(`{"sub":"` + sub + `","v":2}`) // not part of the hash
				require.NoError(t, svc.Sync(ctx, same, t1))
				after := getUser(t, db, sub)
				assert.Equal(t, before.UpdatedAt, after.UpdatedAt)
				assert.True(t, t0.Equal(*after.SyncedAt), "synced_at untouched")
				assert.Equal(t, *before.Snapshot, *after.Snapshot)
			},
		},
		{
			name: "unknown parents get placeholder rows with only a sub, plus edges",
			run: func(t *testing.T, sub string) {
				p1, p2 := "p1-"+sub, "p2-"+sub
				require.NoError(t, svc.Sync(ctx, payloadFor(sub, p1, p2, p1), t0))
				for _, p := range []string{p1, p2} {
					row := getUser(t, db, p)
					require.NotNil(t, row, p)
					assert.Nil(t, row.Email)
					assert.Nil(t, row.Nickname)
					assert.Nil(t, row.Snapshot)
					assert.Nil(t, row.IdentityHash)
					assert.Nil(t, row.SyncedAt, "placeholder = never synced")
				}
				assert.Equal(t, []string{p1, p2}, parentsOf(t, db, sub))
			},
		},
		{
			name: "placeholder parent becomes real when it syncs itself",
			run: func(t *testing.T, sub string) {
				parent := "p-" + sub
				require.NoError(t, svc.Sync(ctx, payloadFor(sub, parent), t1))
				require.Nil(t, getUser(t, db, parent).SyncedAt)

				pp := payloadFor(parent)
				pp.MainAccount.Allowed = true
				pp.MainAccount.SubAccountsCount = 1
				require.NoError(t, svc.Sync(ctx, pp, t0)) // older than the child's message, still accepted
				row := getUser(t, db, parent)
				require.NotNil(t, row.SyncedAt)
				require.NotNil(t, row.Email)
				snap := snapshotOf(t, row)
				assert.True(t, snap.IsMain())
				assert.Equal(t, []string{parent}, parentsOf(t, db, sub), "edge is preserved")
			},
		},
		{
			name: "edge added on a later message",
			run: func(t *testing.T, sub string) {
				p1, p2 := "p1-"+sub, "p2-"+sub
				require.NoError(t, svc.Sync(ctx, payloadFor(sub, p1), t0))
				require.NoError(t, svc.Sync(ctx, payloadFor(sub, p1, p2), t1))
				assert.Equal(t, []string{p1, p2}, parentsOf(t, db, sub))
			},
		},
		{
			name: "edge removed on a later message",
			run: func(t *testing.T, sub string) {
				p1, p2 := "p1-"+sub, "p2-"+sub
				require.NoError(t, svc.Sync(ctx, payloadFor(sub, p1, p2), t0))
				require.NoError(t, svc.Sync(ctx, payloadFor(sub, p2), t1))
				assert.Equal(t, []string{p2}, parentsOf(t, db, sub))
				require.NoError(t, svc.Sync(ctx, payloadFor(sub), t2))
				assert.Empty(t, parentsOf(t, db, sub))
			},
		},
		{
			name: "self reference in sub_account_of is ignored",
			run: func(t *testing.T, sub string) {
				require.NoError(t, svc.Sync(ctx, payloadFor(sub, sub), t0))
				assert.Empty(t, parentsOf(t, db, sub))
				assert.Empty(t, snapshotOf(t, getUser(t, db, sub)).SubAccountOf)
			},
		},
		{
			name: "missing email keeps the existing one",
			run: func(t *testing.T, sub string) {
				require.NoError(t, svc.Sync(ctx, payloadFor(sub), t0))
				noEmail := payloadFor(sub)
				noEmail.Email = ""
				require.NoError(t, svc.Sync(ctx, noEmail, t1))
				row := getUser(t, db, sub)
				require.NotNil(t, row.Email)
				assert.Equal(t, sub+"@example.test", *row.Email)
			},
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t, "sub-"+string(rune('a'+i)))
		})
	}
}
