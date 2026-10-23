// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector is the Eshu Jira collector entrypoint. With no arguments
// (or --sdk-stdio) it serves one claim: it reads one collector-sdk/v1alpha1
// request from stdin and writes one result to stdout. With the seed
// subcommand it writes its own binary and manifest into a directory so an
// init container can install the component without a shell.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	collector "github.com/eshu-hq/eshu-collector-jira"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "collector: %v\n", err)
		os.Exit(1)
	}
}

// run dispatches on the first argument: "seed" installs the binary and
// manifest, anything else serves one claim.
func run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
) error {
	if len(args) > 0 && args[0] == "seed" {
		return runSeed(args[1:], stdout, getenv)
	}
	return runClaim(ctx, args, stdin, stdout, stderr, getenv, time.Now)
}

func runClaim(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
	now func() time.Time,
) error {
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	// --sdk-stdio is the documented process-adapter argument. Serving one
	// claim from stdin is the only claim mode, so the flag only has to parse.
	flags.Bool("sdk-stdio", true, "read one collector SDK host request from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	request, err := collector.ParseRequest(stdin)
	if err != nil {
		return err
	}
	handler := collector.Handler{Getenv: getenv, Now: now, Log: stderr}
	result := handler.Handle(ctx, request)
	encoded, err := json.Marshal(result)
	if err != nil {
		return errors.New("encode result")
	}
	if _, err := stdout.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}
