package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"researchos/backend/internal/research"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Kairo stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := research.LoadConfig()
	if err != nil {
		return err
	}
	objects, err := research.NewObjectStorage(cfg)
	if err != nil {
		return err
	}
	store, err := research.OpenStoreWithMigrationURL(cfg.DatabaseURL, cfg.DatabaseURLUnpooled)
	if err != nil {
		return err
	}
	defer store.Close()
	if err = store.Recover(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	probeCode := research.WorkerProbeCode()
	var probe *exec.Cmd
	if cfg.WorkerMode == "docker" {
		probe = exec.CommandContext(ctx, "docker", "run", "--rm", "--pull=never", "--network", "none", "--read-only", "--memory", "768m", "--cpus", "1", "--pids-limit", "64", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "65534:65534", "--tmpfs", "/tmp:size=32m,nosuid,nodev", "-e", "OPENBLAS_NUM_THREADS=1", cfg.WorkerImage, "python3", "-I", "-c", probeCode, "--capabilities")
	} else {
		probe = exec.CommandContext(ctx, cfg.Python, "-I", "-c", probeCode, "--capabilities")
	}
	probeOutput, probeErr := probe.Output()
	cancel()
	var capabilities research.WorkerCapabilities
	if probeErr == nil {
		probeErr = json.Unmarshal(probeOutput, &capabilities)
	}
	registry := research.NewRegistry(cfg.Python, cfg.WorkerMode, slices.Contains(capabilities.Modules, "scipy"))
	registry.SetAvailableModules(capabilities.Modules)
	registry.SetLanguages(capabilities.Languages)
	registry.SetWorkerAvailability(probeErr == nil, "Worker probe failed; check Python 3.10+, or the Docker daemon and locally built worker image")
	model := research.NewModelClient(cfg.ModelProvider, cfg.ModelURL, cfg.ModelName, cfg.ModelKey)
	executor := research.PythonExecutor{Python: cfg.Python, Mode: cfg.WorkerMode, Image: cfg.WorkerImage}
	jobs := research.NewJobs(store, registry, executor, model, cfg.DataDir, cfg.WorkerMode, cfg.WorkerImage, cfg.Workers)
	jobs.Objects = objects
	jobs.Start()
	defer jobs.Close()
	app := &research.Server{Objects: objects, Config: cfg, Store: store, Registry: registry, Model: model, Jobs: jobs}
	server := &http.Server{Addr: cfg.Addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	stopped := make(chan error, 1)
	go func() {
		slog.Info("Kairo API listening", "address", cfg.Addr, "model_provider", cfg.ModelProvider, "model", cfg.ModelName, "worker_mode", cfg.WorkerMode, "env_file", cfg.EnvFile)
		stopped <- server.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case err = <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals:
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err = server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}
	return nil
}
