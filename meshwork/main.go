package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"example.com/meshwork-prototype/linker"
	"example.com/meshwork-prototype/meshwork"
)

const (
	rawCSVPath     = "data/raw/house_data.csv"
	cleanedCSVPath = "data/processed/cleaned_house_data.csv"
	rawCSVType     = "RawHousingCSV"
	cleanedCSVType = "CleanedHousingCSV"
)

type artifactReport struct {
	SHA256    json.RawMessage `json:"sha256"`
	TokenHash string          `json:"token_hash"`
}

func run(ctx context.Context, args []string, out, logs io.Writer) error {
	flags := flag.NewFlagSet("meshwork", flag.ContinueOnError)
	flags.SetOutput(logs)
	name := flags.String("scenario", "clean", "scenario name (clean or reproduce)")
	input := flags.String("input", rawCSVPath, "raw housing CSV")
	output := flags.String("output", cleanedCSVPath, "path to publish the cleaned CSV")
	python := flags.String("python", "python", "Python executable with pandas and NumPy installed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}

	linked, err := linker.Link([]linker.Repository{{Name: "house-price", Root: "meshwork"}})
	if err != nil {
		return err
	}
	var scenario linker.Scenario
	for _, candidate := range linked.Scenarios {
		if candidate.Name == *name {
			if scenario.Name != "" {
				return fmt.Errorf("ambiguous scenario %q", *name)
			}
			scenario = candidate
		}
	}
	if scenario.Name == "" {
		return fmt.Errorf("unknown scenario %q", *name)
	}
	rawPlace := scenario.Graph + "/raw_csv"
	cleanedPlace := scenario.Graph + "/cleaned_csv"
	if _, seeded := scenario.Marking[cleanedPlace]; seeded {
		return fmt.Errorf("scenario %s must start with an empty cleaned_csv place", scenario.Name)
	}

	interpreter, err := exec.LookPath(*python)
	if err != nil {
		return err
	}
	interpreter, err = filepath.Abs(interpreter)
	if err != nil {
		return err
	}
	script, err := filepath.Abs("src/data/run_processing.py")
	if err != nil {
		return err
	}
	workspace, err := os.MkdirTemp("", "house-price-meshwork-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	store := artifactStore(workspace)
	raw, err := store.capture(*input, rawCSVType)
	if err != nil {
		return fmt.Errorf("capture raw CSV: %w", err)
	}
	if want, ok := scenario.Marking[rawPlace]; ok && want.Hash != raw.Hash {
		return fmt.Errorf("scenario %s requires raw CSV digest %s, got %s", scenario.Name, want.Data, raw.Data)
	}
	scenario.Marking[rawPlace] = raw
	implementations := map[string]meshwork.Compute{
		"house-data.process": bindProcess(store, logs, []string{interpreter, script},
			map[string]string{"raw": rawCSVPath},
			map[string]outputFile{"cleaned": {path: cleanedCSVPath, tokenType: cleanedCSVType}}),
	}
	runtime, err := linker.RunScenario(ctx, linked.Definitions[scenario.Graph], scenario, implementations)
	if err != nil {
		return err
	}
	cleaned, ok := runtime.Token(cleanedPlace)
	if !ok {
		return errors.New("scenario completed without a cleaned CSV")
	}
	if err := store.materialize(cleaned, *output); err != nil {
		return fmt.Errorf("publish cleaned CSV: %w", err)
	}
	return json.NewEncoder(out).Encode(struct {
		Scenario string         `json:"scenario"`
		Raw      artifactReport `json:"raw"`
		Cleaned  artifactReport `json:"cleaned"`
	}{scenario.Name, artifactReport{raw.Data, raw.Hash}, artifactReport{cleaned.Data, cleaned.Hash}})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
