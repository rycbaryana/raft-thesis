package main

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logger := newLogger(cfg.LogLevel)

	runtime, err := buildNode(cfg, logger)
	if err != nil {
		log.Fatalf("build node: %v", err)
	}
	runtime.Serve()

	log.Printf("---------------------------------------")
	log.Printf("NODE %d STARTED at %s", cfg.ID, cfg.Addr)
	if len(cfg.Bootstrap) > 0 {
		log.Printf("Founding bootstrap members: %v", cfg.Bootstrap)
	} else {
		log.Printf("Joiner mode: empty config; waiting for AddNode from a leader")
	}
	log.Printf("---------------------------------------")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down node...")
	runtime.Close()
}
