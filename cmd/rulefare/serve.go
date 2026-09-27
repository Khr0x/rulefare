package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/Khr0x/rulefare/internal/httpapi"
	"github.com/Khr0x/rulefare/internal/platform"
)

// serve runs the platform until ctx is cancelled (SIGINT/SIGTERM in main).
// It is kept apart from the rules subcommands so they never load platform
// configuration or open network listeners.
func serve(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "serve takes no arguments; configure it with RULEFARE_* environment variables")
		fmt.Fprint(stderr, usage)
		return 2
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	cfg, err := platform.LoadConfig(getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 1
	}
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("listen failed", "addr", cfg.HTTPAddr, "error", err)
		return 1
	}
	logger.Info("listening", "addr", ln.Addr().String())
	if err := httpapi.Serve(ctx, ln, httpapi.NewHandler(), cfg.ShutdownTimeout); err != nil {
		logger.Error("server stopped with error", "error", err)
		return 1
	}
	logger.Info("stopped")
	return 0
}
