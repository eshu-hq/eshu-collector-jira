// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	collector "github.com/eshu-hq/eshu-collector-jira"
)

const (
	seededBinaryName   = "collector"
	seededManifestName = "manifest.yaml"
	// imageRefEnv supplies the digest-pinned image reference when --image-ref
	// is not given; the flag wins.
	imageRefEnv = "COLLECTOR_IMAGE_REF"
)

var (
	imageRefPattern          = regexp.MustCompile(`^[^@\s]+@sha256:[0-9a-f]{64}$`)
	manifestImageLinePattern = regexp.MustCompile(`(?m)^([ \t]*image:[ \t]*)\S+[ \t]*$`)
)

// runSeed implements `collector seed --dest DIR [--image-ref REF@sha256:DIGEST]`.
// It writes DIR/collector (this binary, mode 0755) and DIR/manifest.yaml (the
// embedded manifest, mode 0644). Re-running it is safe: files are replaced
// atomically with identical content. --image-ref, or the COLLECTOR_IMAGE_REF
// environment variable when the flag is absent, stamps the digest-pinned image
// reference the operator deployed into the manifest's artifact entry, because
// an image cannot embed its own digest. Without either, the repository
// placeholder is written unchanged.
func runSeed(args []string, stdout io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dest := flags.String("dest", "", "directory to write the collector binary and manifest into")
	imageRef := flags.String("image-ref", "", "optional digest-pinned image reference (name@sha256:<64 hex>) to record in the manifest artifact; defaults to $"+imageRefEnv)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("seed: unexpected argument %q", flags.Arg(0))
	}
	executable, err := os.Executable()
	if err != nil {
		return errors.New("seed: resolve own executable")
	}
	manifest, err := manifestFor(collector.ManifestYAML, firstNonBlank(*imageRef, getenv(imageRefEnv)))
	if err != nil {
		return err
	}
	digest, err := seed(*dest, executable, manifest)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "seeded %s sha256:%s\n", filepath.Join(*dest, seededBinaryName), digest)
	return err
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// manifestFor returns the manifest bytes to seed, stamping the image reference into the
// artifact entry when one is given.
func manifestFor(manifest []byte, image string) ([]byte, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return manifest, nil
	}
	if !imageRefPattern.MatchString(image) {
		return nil, errors.New("seed: --image-ref must be a digest-pinned reference (name@sha256:<64 hex>)")
	}
	if len(manifestImageLinePattern.FindAllIndex(manifest, -1)) != 1 {
		return nil, errors.New("seed: manifest must declare exactly one artifact image to stamp")
	}
	// ReplaceAllFunc, not a template, so a reference holding "$" is literal.
	return manifestImageLinePattern.ReplaceAllFunc(manifest, func(line []byte) []byte {
		prefix := manifestImageLinePattern.FindSubmatch(line)[1]
		return append(append([]byte(nil), prefix...), image...)
	}), nil
}

// seed copies the executable and writes the manifest into dest, returning the
// hex SHA-256 of the seeded binary.
func seed(dest string, executable string, manifest []byte) (string, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", errors.New("seed: --dest is required")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", errors.New("seed: create destination directory")
	}
	digest, err := copyExecutable(executable, filepath.Join(dest, seededBinaryName))
	if err != nil {
		return "", err
	}
	if err := writeAtomic(filepath.Join(dest, seededManifestName), 0o644, func(w io.Writer) error {
		_, err := w.Write(manifest)
		return err
	}); err != nil {
		return "", errors.New("seed: write manifest")
	}
	return digest, nil
}

func copyExecutable(source string, target string) (string, error) {
	in, err := os.Open(source) // #nosec G304 -- the path is this process's own executable
	if err != nil {
		return "", errors.New("seed: open own executable")
	}
	defer func() { _ = in.Close() }()
	hash := sha256.New()
	if err := writeAtomic(target, 0o755, func(w io.Writer) error {
		_, err := io.Copy(io.MultiWriter(w, hash), in)
		return err
	}); err != nil {
		return "", errors.New("seed: write binary")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// writeAtomic writes through a temporary file in the target directory and
// renames it into place, so a concurrent reader never sees a partial file.
func writeAtomic(target string, mode os.FileMode, write func(io.Writer) error) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".seed-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err = write(tmp); err != nil {
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}
