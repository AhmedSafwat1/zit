// Package app holds the dependency container shared by both binaries.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/4jawaly/identity-gateway/internal/config"
	"github.com/4jawaly/identity-gateway/pkg/cache"
	"github.com/4jawaly/identity-gateway/pkg/queue"
	"github.com/4jawaly/identity-gateway/pkg/zitadel"
)

// Container owns every process-wide dependency. Fields are nil when the
// binary did not ask for them.
type Container struct {
	Cfg     *config.Config
	Log     zerolog.Logger
	DB      *gorm.DB
	Redis   redis.UniversalClient
	Store   cache.Store
	Pub     queue.Publisher
	Zitadel *zitadel.Client

	closers []func(context.Context) error
}

// Option builds one dependency. Options run in the order given, so
// WithConfig and WithLogger must come before anything that needs them.
type Option func(context.Context, *Container) error

const bootTimeout = 10 * time.Second

// New applies opts in order. Every dependency option pings its target and
// fails fast; on failure whatever already opened is closed again.
func New(opts ...Option) (*Container, error) {
	c := &Container{Log: zerolog.Nop()}
	ctx, cancel := context.WithTimeout(context.Background(), bootTimeout)
	defer cancel()
	for _, o := range opts {
		if err := o(ctx, c); err != nil {
			_ = c.Close(context.Background())
			return nil, err
		}
	}
	return c, nil
}

func (c *Container) addCloser(f func(context.Context) error) { c.closers = append(c.closers, f) }

// Close releases dependencies in reverse construction order.
func (c *Container) Close(ctx context.Context) error {
	var errs []error
	for i := len(c.closers) - 1; i >= 0; i-- {
		if err := c.closers[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	c.closers = nil
	return errors.Join(errs...)
}

func (c *Container) needCfg() error {
	if c.Cfg == nil {
		return errors.New("app: WithConfig must be applied first")
	}
	return nil
}

// WithConfig sets the configuration.
func WithConfig(cfg *config.Config) Option {
	return func(_ context.Context, c *Container) error { c.Cfg = cfg; return nil }
}

// WithLogger sets the logger.
func WithLogger(l zerolog.Logger) Option {
	return func(_ context.Context, c *Container) error { c.Log = l; return nil }
}

// WithDatabase opens Postgres and pings it. Only the worker uses it.
func WithDatabase() Option {
	return func(ctx context.Context, c *Container) error {
		if err := c.needCfg(); err != nil {
			return err
		}
		dc := c.Cfg.Database
		if !dc.Enabled() {
			return errors.New("app: DB_HOST is empty")
		}
		db, err := gorm.Open(postgres.Open(dc.DSN()), &gorm.Config{
			Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
			SkipDefaultTransaction: true,
			PrepareStmt:            true,
		})
		if err != nil {
			return fmt.Errorf("open postgres: %w", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			return fmt.Errorf("postgres pool: %w", err)
		}
		sqlDB.SetMaxOpenConns(dc.MaxOpenConns)
		sqlDB.SetMaxIdleConns(dc.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(dc.ConnMaxLifetime)
		if err := sqlDB.PingContext(ctx); err != nil {
			_ = sqlDB.Close()
			return fmt.Errorf("ping postgres: %w", err)
		}
		c.DB = db
		c.addCloser(func(context.Context) error { return sqlDB.Close() })
		return nil
	}
}

// WithRedis opens the Redis client, pings it and builds the Store.
func WithRedis() Option {
	return func(ctx context.Context, c *Container) error {
		if err := c.needCfg(); err != nil {
			return err
		}
		rc := c.Cfg.Redis
		rdb := redis.NewClient(&redis.Options{
			Addr:         rc.Addr,
			Password:     rc.Password,
			DB:           rc.DB,
			PoolSize:     rc.PoolSize,
			MinIdleConns: rc.MinIdleConns,
			ReadTimeout:  rc.ReadTimeout,
			WriteTimeout: rc.ReadTimeout,
			DialTimeout:  rc.DialTimeout,
			PoolTimeout:  rc.ReadTimeout, // shed load instead of queueing when the pool is exhausted
			// The request context deadline bounds every command, so a slow
			// Redis never holds an HTTP goroutine past the handler timeout.
			ContextTimeoutEnabled: true,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			_ = rdb.Close()
			return fmt.Errorf("ping redis: %w", err)
		}
		c.Redis = rdb
		c.Store = cache.NewRedis(rdb,
			cache.WithJitter(c.Cfg.Cache.TTLJitter),
			cache.WithKeyPrefix(c.Cfg.Cache.KeyPrefix),
		)
		c.addCloser(func(context.Context) error { return rdb.Close() })
		return nil
	}
}

// WithPublisher dials RabbitMQ, declares the topology and keeps a confirm-mode publisher.
func WithPublisher() Option {
	return func(ctx context.Context, c *Container) error {
		if err := c.needCfg(); err != nil {
			return err
		}
		qc := c.Cfg.Queue
		if qc.URL == "" {
			return errors.New("app: QUEUE_URL is empty")
		}
		pub, err := queue.NewPublisher(ctx, qc.URL, qc.Exchange,
			[]queue.Binding{{Queue: queue.QueueUserSync, RoutingKey: queue.RoutingUserSync}},
			queue.WithPublishTimeout(qc.PublishTimeout),
			queue.WithConfirm(qc.Confirm),
			queue.WithPublisherLogger(c.Log.With().Str("component", "publisher").Logger()),
		)
		if err != nil {
			return err
		}
		c.Pub = pub
		c.addCloser(pub.Close)
		return nil
	}
}

// WithZitadel builds the userinfo client on an already-wrapped http client
// and checks OIDC discovery so a wrong issuer dies at boot.
func WithZitadel(httpClient *http.Client) Option {
	return func(ctx context.Context, c *Container) error {
		if err := c.needCfg(); err != nil {
			return err
		}
		zc := c.Cfg.Zitadel
		client := zitadel.NewClient(httpClient, zc.Issuer,
			zitadel.WithLogger(c.Log.With().Str("component", "zitadel").Logger()),
			zitadel.WithRetries(zc.Retries),
			zitadel.WithUserinfoPath(zc.UserinfoPath),
		)
		if err := client.Ping(ctx); err != nil {
			return err
		}
		c.Zitadel = client
		return nil
	}
}

// Ready pings every dependency this container holds. Used by /ready.
func (c *Container) Ready(ctx context.Context) error {
	var errs []error
	if c.Redis != nil {
		if err := c.Redis.Ping(ctx).Err(); err != nil {
			errs = append(errs, fmt.Errorf("redis: %w", err))
		}
	}
	if c.DB != nil {
		if sqlDB, err := c.DB.DB(); err != nil {
			errs = append(errs, err)
		} else if err := sqlDB.PingContext(ctx); err != nil {
			errs = append(errs, fmt.Errorf("postgres: %w", err))
		}
	}
	return errors.Join(errs...)
}
