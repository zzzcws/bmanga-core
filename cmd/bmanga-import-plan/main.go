package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/zzzcws/bmanga-core/internal/importplan"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "bmanga-import-plan:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if stdout == nil || stderr == nil {
		return errors.New("stdout and stderr are required")
	}
	limits := importplan.DefaultLimits()
	flags := flag.NewFlagSet("bmanga-import-plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "explicit security root containing both intake and library")
	intake := flags.String("intake", "", "intake directory, absolute or relative to --root")
	library := flags.String("library", "", "existing library directory, absolute or relative to --root")
	flags.IntVar(&limits.MaxFiles, "max-files", limits.MaxFiles, "maximum regular files inspected per tree")
	flags.IntVar(&limits.MaxEntries, "max-entries", limits.MaxEntries, "maximum directory entries inspected per tree")
	flags.Int64Var(&limits.MaxFileBytes, "max-file-bytes", limits.MaxFileBytes, "maximum bytes hashed for one file")
	flags.Int64Var(&limits.MaxTotalBytes, "max-total-bytes", limits.MaxTotalBytes, "maximum total bytes hashed per tree")
	flags.IntVar(&limits.MaxDepth, "max-depth", limits.MaxDepth, "maximum directory depth below each selected tree")
	flags.IntVar(&limits.MaxPathBytes, "max-path-bytes", limits.MaxPathBytes, "maximum relative path length in bytes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("positional arguments are not accepted")
	}
	plan, err := importplan.Build(ctx, importplan.Options{
		Root:    *root,
		Intake:  *intake,
		Library: *library,
		Limits:  limits,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(plan); err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	return nil
}
