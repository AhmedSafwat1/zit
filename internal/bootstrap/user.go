package bootstrap

import (
	"github.com/4jawaly/identity-gateway/internal/modules/user"
)

// userAPI wires the HTTP half of the user module.
func userAPI(s *APIShared) *user.APIModule {
	return user.NewAPI(s.UserSvc)
}

// userConsumer wires the queue half of the user module.
func userConsumer(s *ConsumerShared) *user.WorkerModule {
	return user.NewWorker(s.SyncSvc)
}
