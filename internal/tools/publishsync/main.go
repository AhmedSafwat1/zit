// Command publishsync publishes one synthetic user.sync envelope. Dev-only:
// proves the worker path end to end without a real Zitadel token.
//
//	go run ./internal/tools/publishsync -sub 900000000000000001 -parent 900000000000000002
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/4jawaly/identity-gateway/internal/config"
	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/modules/user/jobs/sync_user"
	"github.com/4jawaly/identity-gateway/pkg/queue"
)

func main() {
	sub := flag.String("sub", "900000000000000001", "subject")
	parents := flag.String("parent", "", "comma-separated parent subs")
	status := flag.String("status", "Active", "status_name")
	flag.Parse()

	if err := run(*sub, *parents, *status); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(sub, parents, status string) error {
	cfg := config.MustLoad()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pub, err := queue.NewPublisher(ctx, cfg.Queue.URL, cfg.Queue.Exchange,
		[]queue.Binding{sync_user.Binding}, queue.WithPublisherLogger(zerolog.Nop()))
	if err != nil {
		return err
	}
	defer func() { _ = pub.Close(ctx) }()

	var parentSubs []string
	if parents != "" {
		parentSubs = strings.Split(parents, ",")
	}
	p := dto.UserSyncPayload{
		Sub: sub, Email: sub + "@dev.local", Nickname: "dev " + sub,
		Roles: []string{"USER"}, Issuer: cfg.Zitadel.Issuer,
		MainAccount:  dto.MainAccountPayload{StatusName: status, AccountID: "1"},
		SubAccountOf: parentSubs,
		Origin:       json.RawMessage(`{"sub":"` + sub + `","dev":true}`),
	}
	e, err := queue.NewEnvelope(sync_user.EventType, sync_user.SchemaVersion, cfg.App.Producer(), p)
	if err != nil {
		return err
	}
	if err := pub.Publish(ctx, sync_user.EventType, e); err != nil {
		return err
	}
	fmt.Println("published", e.EventID, "sub", sub, "hash", p.Hash())
	return nil
}
