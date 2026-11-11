package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"example.com/meshwork-prototype/meshwork"
)

type outputFile struct {
	path, tokenType string
}

// bindProcess maps local ports to files in a fresh working directory per firing.
// The caller supplies a trusted argv and paths relative to that directory.
func bindProcess(store artifactStore, logs io.Writer, argv []string, inputs map[string]string, outputs map[string]outputFile) meshwork.Compute {
	return func(ctx context.Context, tokens map[string]meshwork.Token) (map[string]meshwork.Token, error) {
		if len(tokens) != len(inputs) {
			return nil, fmt.Errorf("process has %d input tokens for %d file bindings", len(tokens), len(inputs))
		}
		dir, err := os.MkdirTemp(string(store), "attempt-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		for port, path := range inputs {
			token, ok := tokens[port]
			if !ok || !filepath.IsLocal(path) {
				return nil, fmt.Errorf("invalid process input binding %q to %q", port, path)
			}
			if err := store.materialize(token, filepath.Join(dir, path)); err != nil {
				return nil, fmt.Errorf("stage input %s: %w", port, err)
			}
		}

		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = logs, logs
		cmd.WaitDelay = time.Second
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("%s: %w", argv[0], err)
		}

		result := make(map[string]meshwork.Token, len(outputs))
		for port, file := range outputs {
			if !filepath.IsLocal(file.path) {
				return nil, fmt.Errorf("invalid process output binding %q to %q", port, file.path)
			}
			token, err := store.capture(filepath.Join(dir, file.path), file.tokenType)
			if err != nil {
				return nil, fmt.Errorf("capture output %s: %w", port, err)
			}
			result[port] = token
		}
		return result, nil
	}
}
