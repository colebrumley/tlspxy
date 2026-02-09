package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/colebrumley/tlspxy/internal/loadtest"
)

func main() {
	scenario := flag.String("scenario", "all", "scenario name or \"all\"")
	list := flag.Bool("list", false, "list available scenarios and exit")
	concurrency := flag.Int("concurrency", 0, "override worker count (0 = use scenario default)")
	duration := flag.Duration("duration", 0, "override test duration (0 = use scenario default)")
	payload := flag.Int("payload", 0, "override payload size in bytes (0 = use scenario default)")
	jsonOutput := flag.Bool("json", false, "output as JSON")
	certDir := flag.String("certdir", "", "path to test certs (default: auto-detect)")
	flag.Parse()

	if *list {
		printScenarioList()
		return
	}

	// Resolve cert directory.
	dir := *certDir
	if dir == "" {
		var err error
		dir, err = findCertDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	// Select scenarios.
	scenarios := selectScenarios(*scenario)
	if len(scenarios) == 0 {
		fmt.Fprintf(os.Stderr, "Error: unknown scenario %q\n", *scenario)
		fmt.Fprintf(os.Stderr, "Run with -list to see available scenarios.\n")
		os.Exit(1)
	}

	// Run scenarios and collect results.
	var results []loadtest.Result
	var failed bool

	for _, s := range scenarios {
		// Apply overrides.
		if *concurrency > 0 {
			s.Concurrency = *concurrency
		}
		if *duration > 0 {
			s.Duration = *duration
		}
		if *payload > 0 {
			s.PayloadSize = *payload
		}

		fmt.Fprintf(os.Stderr, "Running %s...\n", s.Name)

		result, err := loadtest.RunScenario(s, dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running %s: %v\n", s.Name, err)
			failed = true
			continue
		}

		results = append(results, *result)

		if !*jsonOutput {
			loadtest.PrintText(os.Stdout, *result)
			fmt.Println()
		}

		// Check pass/fail.
		if s.Validate != nil {
			if err := s.Validate(result); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", s.Name, err)
				failed = true
			}
		} else if result.ErrorRate > 0.01 {
			fmt.Fprintf(os.Stderr, "FAIL %s: error rate %.2f%% exceeds 1%%\n", s.Name, result.ErrorRate*100)
			failed = true
		}
	}

	// JSON output.
	if *jsonOutput && len(results) > 0 {
		if err := loadtest.PrintJSON(os.Stdout, results); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON: %v\n", err)
			os.Exit(1)
		}
	}

	// Summary for multiple scenarios in text mode.
	if !*jsonOutput && len(results) > 1 {
		loadtest.PrintSummary(os.Stdout, results)
	}

	if failed {
		os.Exit(1)
	}
}

// printScenarioList prints a table of available scenarios and exits.
func printScenarioList() {
	scenarios := loadtest.ListScenarios()
	fmt.Printf("%-25s %-8s %s\n", "Name", "Mode", "Description")
	fmt.Println(strings.Repeat("-", 70))
	for _, s := range scenarios {
		fmt.Printf("%-25s %-8s %s\n", s.Name, s.Harness.Mode, s.Description)
	}
}

// selectScenarios returns the scenarios matching the given name, or all if "all".
func selectScenarios(name string) []loadtest.Scenario {
	all := loadtest.ListScenarios()
	if name == "all" {
		return all
	}
	for _, s := range all {
		if s.Name == name {
			return []loadtest.Scenario{s}
		}
	}
	return nil
}

// findCertDir walks up from the current working directory looking for go.mod,
// then returns contrib/testdata/certs/ relative to the module root.
func findCertDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			certDir := filepath.Join(dir, "contrib", "testdata", "certs")
			if _, err := os.Stat(certDir); err == nil {
				return certDir, nil
			}
			return "", fmt.Errorf("found go.mod at %s but no contrib/testdata/certs/ directory", dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find go.mod in any parent directory")
}
