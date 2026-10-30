package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"researchos/backend/internal/research"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	allowMissing := flag.Bool("allow-missing", false, "continue with missing historical files, retaining their references and backing up saved run records")
	checkOnly := flag.Bool("check-only", false, "verify bucket write/read/delete access without migrating data")
	remove := flag.Bool("delete-local", false, "remove local research files only after every uploaded object verifies")
	flag.Parse()
	cfg, err := research.LoadConfig()
	if err != nil {
		return err
	}
	cfg.StorageBackend = "r2"
	objects, err := research.NewObjectStorage(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = research.CheckObjectStorage(ctx, objects); err != nil {
		return err
	}
	if *checkOnly {
		fmt.Println("PASS: R2 upload, download, SHA-256 verification, and delete in bucket " + cfg.R2Bucket)
		return nil
	}
	store, err := research.OpenStoreWithMigrationURL(cfg.DatabaseURL, cfg.DatabaseURLUnpooled)
	if err != nil {
		return err
	}
	defer store.Close()
	var report research.StorageMigrationReport
	if *allowMissing {
		report, err = research.MigrateArtifactsAllowMissing(ctx, store, cfg.DataDir, objects, *remove)
	} else {
		report, err = research.MigrateArtifacts(ctx, store, cfg.DataDir, objects, *remove)
	}
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(report)
	fmt.Println(string(raw))
	fmt.Println("Verified. Set STORAGE_BACKEND=r2 in the root .env, then start the API.")
	return nil
}
