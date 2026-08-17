package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zzzcws/bmanga-core/internal/catalog"
	"github.com/zzzcws/bmanga-core/internal/prototype"
)

func main() {
	configPath := flag.String("config", "config/libraries.json", "path to the library configuration JSON")
	flag.Parse()
	if err := run(context.Background(), *configPath, os.Stdout); err != nil {
		fatal(err)
	}
}

func run(ctx context.Context, configPath string, output io.Writer) error {
	config, err := catalog.LoadConfig(configPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(config.Database), 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	server, err := prototype.NewServer(config.Database)
	if err != nil {
		return fmt.Errorf("initialize application schema: %w", err)
	}
	if err := server.Close(); err != nil {
		return fmt.Errorf("close application schema initializer: %w", err)
	}
	summary, err := catalog.Scan(ctx, config)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return err
	}
	return nil
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "bmanga-scan:", err)
	os.Exit(1)
}
