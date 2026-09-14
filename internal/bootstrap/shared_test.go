package bootstrap

import (
	"net/http"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/4jawaly/identity-gateway/internal/app"
	"github.com/4jawaly/identity-gateway/internal/config"
	cmocks "github.com/4jawaly/identity-gateway/internal/mocks"
	"github.com/4jawaly/identity-gateway/pkg/cache"
	"github.com/4jawaly/identity-gateway/pkg/zitadel"
)

func apiContainer(t *testing.T) *app.Container {
	t.Helper()
	return &app.Container{
		Log: zerolog.Nop(), Cfg: &config.Config{},
		Store:   cache.NewMemory(),
		Pub:     cmocks.NewPublisher(t),
		Zitadel: zitadel.NewClient(http.DefaultClient, "https://zauth.example"),
	}
}

func workerContainer() *app.Container {
	return &app.Container{Log: zerolog.Nop(), Cfg: &config.Config{}, DB: &gorm.DB{}}
}

func TestNewAPIShared(t *testing.T) {
	t.Parallel()
	s := newAPIShared(apiContainer(t))
	require.NotNil(t, s.UserSvc)
	require.NotNil(t, s.UserCache)
	assert.True(t, s.Authenticator() != nil)
}

func TestNewConsumerShared(t *testing.T) {
	t.Parallel()
	s := newConsumerShared(workerContainer())
	require.NotNil(t, s.UserRepo)
	require.NotNil(t, s.SyncSvc)
	assert.True(t, s.UserRepository() != nil)
}

func TestModuleLists(t *testing.T) {
	t.Parallel()
	api := APIModules(apiContainer(t))
	require.Len(t, api, 1)
	assert.Equal(t, "user", api[0].Name())

	worker := WorkerModules(workerContainer())
	require.Len(t, worker, 1)
	assert.Equal(t, "user", worker[0].Name())
}
