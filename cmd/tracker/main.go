// Command tracker is the whole application: HTTP API, embedded frontend,
// migrations and a few operational subcommands in one binary.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // TRACKER_TIMEZONE must work on hosts without zoneinfo

	"tracker/internal/api"
	"tracker/internal/auth"
	"tracker/internal/config"
	"tracker/internal/database"
	"tracker/internal/project"
	"tracker/internal/story"
	"tracker/internal/user"
	"tracker/internal/velocity"
	"tracker/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: tracker [command]

Commands:
  serve               run the server (default)
  migrate             apply database migrations and exit
  backup <file>       write a consistent database snapshot (safe while running)
  check <file>        verify a database snapshot before restoring it
  reset-password <email>   set a new password read from stdin; signs the user out everywhere
  healthcheck         query /health of the local server; exit status reflects health
  version             print the version

Configuration comes from TRACKER_* environment variables; see deploy/tracker.env.example.
`

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = withDB(func(ctx context.Context, _ *config.Config, db *database.DB) error {
			v, err := db.SchemaVersion(ctx)
			if err == nil {
				fmt.Printf("database is at schema version %d\n", v)
			}
			return err
		})
	case "backup":
		if len(os.Args) != 3 {
			err = errors.New("usage: tracker backup <destination-file>")
			break
		}
		err = withDB(func(ctx context.Context, _ *config.Config, db *database.DB) error {
			return db.Backup(ctx, os.Args[2])
		})
	case "check":
		if len(os.Args) != 3 {
			err = errors.New("usage: tracker check <database-file>")
			break
		}
		var v int64
		if v, err = database.VerifySQLiteFile(context.Background(), os.Args[2]); err == nil {
			fmt.Printf("ok: schema version %d\n", v)
		}
	case "reset-password":
		if len(os.Args) != 3 {
			err = errors.New("usage: tracker reset-password <email>   (new password on stdin)")
			break
		}
		err = withDB(func(ctx context.Context, cfg *config.Config, db *database.DB) error {
			fmt.Fprint(os.Stderr, "New password: ")
			line, rerr := bufio.NewReader(os.Stdin).ReadString('\n')
			if rerr != nil && line == "" {
				return rerr
			}
			svc := auth.NewService(db, auth.Options{Secret: cfg.SessionSecret})
			if err := svc.SetPassword(ctx, os.Args[2], strings.TrimRight(line, "\r\n")); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "password updated; existing sessions were revoked")
			return nil
		})
	case "healthcheck":
		err = healthcheck()
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tracker:", err)
		os.Exit(1)
	}
}

// withDB loads the configuration, opens and migrates the database, runs fn.
func withDB(fn func(ctx context.Context, cfg *config.Config, db *database.DB) error) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := cfg.Prepare(); err != nil {
		return err
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.DatabaseDriver, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return fn(ctx, cfg, db)
}

func serve() error {
	started := time.Now()
	return withDB(func(ctx context.Context, cfg *config.Config, db *database.DB) error {
		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
		slog.SetDefault(logger)

		srv := &api.Server{
			Auth:           auth.NewService(db, auth.Options{Secret: cfg.SessionSecret, AllowRegistration: cfg.AllowRegistration}),
			Users:          user.NewService(db),
			Projects:       project.NewService(db, nil),
			Stories:        story.NewService(db, cfg.Location, nil),
			Velocity:       velocity.NewService(db, cfg.Location, nil),
			DB:             db,
			Logger:         logger,
			PublicURL:      cfg.PublicURL,
			TrustedProxies: cfg.TrustedProxies,
			Version:        version,
			Timezone:       cfg.Location.String(),
			Frontend:       web.Dist(),
		}
		httpServer := &http.Server{
			Addr:              cfg.Addr,
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		}

		ln, err := net.Listen("tcp", cfg.Addr)
		if err != nil {
			return err
		}
		logger.Info("tracker started",
			"version", version,
			"addr", ln.Addr().String(),
			"public_url", cfg.PublicURL.String(),
			"database_driver", cfg.DatabaseDriver,
			"database_url", cfg.DatabaseURL,
			"allow_registration", cfg.AllowRegistration,
			"timezone", cfg.Location.String(),
			"startup_ms", time.Since(started).Milliseconds(),
		)

		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		errc := make(chan error, 1)
		go func() { errc <- httpServer.Serve(ln) }()

		select {
		case err := <-errc:
			return err
		case <-ctx.Done():
		}
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	})
}

// healthcheck lets minimal containers (no curl, no shell) probe the server.
func healthcheck() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	host, port, _ := net.SplitHostPort(cfg.Addr)
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: HTTP %d", resp.StatusCode)
	}
	fmt.Println("ok")
	return nil
}
