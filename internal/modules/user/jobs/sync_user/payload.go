// Package sync_user is the user.sync job: the API-side notifier that
// publishes it and the worker-side consumer that applies it.
package sync_user

import (
	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/pkg/queue"
)

// SchemaVersion is the version of UserSyncPayload carried in the envelope.
const SchemaVersion = 1

// EventType is the envelope event type and the routing key.
const EventType = "user.sync"

// Binding is the queue this job consumes from.
var Binding = queue.Binding{Queue: queue.QueueUserSync, RoutingKey: EventType}

// UserSyncPayload is the job payload. The type lives in dto so services can
// reference it without importing jobs; this alias keeps the job package the
// place people look for it.
type UserSyncPayload = userdto.UserSyncPayload

// MainAccountPayload is the account block of the payload.
type MainAccountPayload = userdto.MainAccountPayload
