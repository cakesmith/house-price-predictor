package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/meshwork"
)

func TestFailedProcessKeepsInputForRetry(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("retry fixture requires sh")
	}
	linked, err := linker.Link([]linker.Repository{{Name: "house-price", Root: "."}})
	if err != nil {
		t.Fatal(err)
	}
	graph := linked.Definitions["/house-price-training"]
	if len(graph.Transitions) != 1 {
		t.Fatalf("expected one linked transition, got %d", len(graph.Transitions))
	}
	transition := graph.Transitions[0].Name
	const rawPlace = "/house-price-training/raw_csv"
	const cleanedPlace = "/house-price-training/cleaned_csv"
	const inputData = "price\n100\n"
	store := artifactStore(t.TempDir())
	dir := t.TempDir()
	source := filepath.Join(dir, "raw.csv")
	ready := filepath.Join(dir, "ready")
	if err := os.WriteFile(source, []byte(inputData), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := store.capture(source, rawCSVType)
	if err != nil {
		t.Fatal(err)
	}
	command := []string{shell, "-c", `
		test ! -e cleaned.csv || exit 99
		if test ! -e "$1"; then
			printf 'partial\n' > cleaned.csv
			exit 23
		fi
		cat raw.csv > cleaned.csv
	`, "clean", ready}
	compute := bindProcess(store, io.Discard, command,
		map[string]string{"raw": "raw.csv"},
		map[string]outputFile{"cleaned": {path: "cleaned.csv", tokenType: cleanedCSVType}})
	runtime, err := graph.Instantiate(map[string]meshwork.Compute{"house-data.process": compute})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Put(rawPlace, raw); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var exit *exec.ExitError
	if err := runtime.Fire(ctx, transition); !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("expected subprocess exit 23, got %v", err)
	}
	if kept, ok := runtime.Token(rawPlace); !ok || kept.Hash != raw.Hash {
		t.Fatal("failed firing changed the input marking")
	}
	if _, ok := runtime.Token(cleanedPlace); ok {
		t.Fatal("failed firing committed an output token")
	}

	if err := os.WriteFile(source, []byte("changed after capture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Fire(ctx, transition); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.Token(rawPlace); ok {
		t.Fatal("successful retry did not consume the input")
	}
	cleaned, ok := runtime.Token(cleanedPlace)
	if !ok || cleaned.Type != cleanedCSVType {
		t.Fatal("successful retry did not commit the expected output type")
	}
	dest := filepath.Join(dir, "result.csv")
	if err := store.materialize(cleaned, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != inputData {
		t.Fatalf("retry output = %q, %v, want captured bytes %q", got, err, inputData)
	}
	if err := runtime.Put(rawPlace, raw); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Fire(ctx, transition); !errors.Is(err, meshwork.ErrNotEnabled) {
		t.Fatalf("expected an occupied output to block firing, got %v", err)
	}
}

func TestSuccessfulProcessRequiresDeclaredOutput(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("process fixture requires sh")
	}
	compute := bindProcess(artifactStore(t.TempDir()), io.Discard, []string{shell, "-c", "exit 0"}, nil,
		map[string]outputFile{"cleaned": {path: "cleaned.csv", tokenType: cleanedCSVType}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outputs, err := compute(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "capture output cleaned") || outputs != nil {
		t.Fatalf("expected missing-output error, got %v, %v", outputs, err)
	}
}
