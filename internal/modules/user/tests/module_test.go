package tests

import (
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/4jawaly/identity-gateway/internal/modules/user"
	usersvc "github.com/4jawaly/identity-gateway/internal/modules/user/services"
	"github.com/4jawaly/identity-gateway/internal/modules/user/tests/mocks"
	"github.com/4jawaly/identity-gateway/pkg/queue"
)

func TestAPIModule_Routes(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	svc, _, _, _ := newTestService(t, usersvc.WithSyncBudget(time.Second))
	m := user.NewAPI(svc)
	assert.Equal(t, "user", m.Name())

	r := gin.New()
	m.RegisterRoutes(r.Group("/api/v1"))
	got := []string{}
	for _, ri := range r.Routes() {
		got = append(got, ri.Method+" "+ri.Path)
	}
	sort.Strings(got)
	assert.Equal(t, []string{"POST /api/v1/users/introspect"}, got)
}

func TestWorkerModule_Consumers(t *testing.T) {
	t.Parallel()
	m := user.NewWorker(usersvc.NewSyncService(mocks.NewUserRepository(t)))
	assert.Equal(t, "user", m.Name())

	c := queue.NewConsumer("amqp://unused", "zit.events")
	m.RegisterConsumers(c)
	b := c.Bindings()
	require.Len(t, b, 1)
	assert.Equal(t, queue.QueueUserSync, b[0].Queue)
	assert.Equal(t, "user.sync", b[0].RoutingKey)
}
