package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"polaris/views/index"

	"github.com/delaneyj/toolbelt"
	"github.com/delaneyj/toolbelt/embeddednats"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/starskey-io/starskey"
)

func SetupRoutes(ctx context.Context, logger *slog.Logger, router chi.Router) (cleanup func() error, err error) {
	// Initialize Starskey
	skey, err := starskey.Open(&starskey.Config{
		Permission:        0755,
		Directory:         "db/todos",
		FlushThreshold:    (1024 * 1024) * 24, // 24MB
		MaxLevel:          3,
		SizeFactor:        10,
		BloomFilter:       true,
		Logging:           true,
		Compression:       true,
		CompressionOption: starskey.S2Compression,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open starskey: %w", err)
	}

	natsPort, err := getFreeNatsPort()
	if err != nil {
		return nil, fmt.Errorf("failed to get free port: %w", err)
	}

	ns, err := embeddednats.New(ctx, embeddednats.WithNATSServerOptions(&natsserver.Options{
		Port: natsPort,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedded NATS server: %w", err)
	}

	ns.WaitForServer()
	logger.Info("NATS server is up", "port", natsPort)

	var once sync.Once
	cleanup = func() error {
		var cleanupErr error
		once.Do(func() {
			cleanupErr = errors.Join(ns.Close(), skey.Close())
		})
		return cleanupErr
	}

	sessionStore := sessions.NewCookieStore([]byte("session-secret"))
	sessionStore.MaxAge(int(24 * time.Hour / time.Second))

	if err := errors.Join(
		index.SetupIndexRoute(router, sessionStore, skey, ns),
	); err != nil {
		return cleanup, fmt.Errorf("error setting up routes: %w", err)
	}

	return cleanup, nil
}

func getFreeNatsPort() (int, error) {
	if p, ok := os.LookupEnv("NATS_PORT"); ok {
		natsPort, err := strconv.Atoi(p)
		if err != nil {
			return 0, err
		}
		if isPortFree(natsPort) {
			return natsPort, nil
		}
	}
	return toolbelt.FreePort()
}

func isPortFree(port int) bool {
	address := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
