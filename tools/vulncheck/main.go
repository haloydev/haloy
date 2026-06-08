package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

var allowedFindings = []allowedFinding{
	{OSV: "GO-2026-4883", Module: "github.com/docker/docker"},
	{OSV: "GO-2026-4887", Module: "github.com/docker/docker"},
}

type allowedFinding struct {
	OSV    string
	Module string
}

type finding struct {
	OSV          string       `json:"osv"`
	FixedVersion string       `json:"fixed_version"`
	Trace        []traceEntry `json:"trace"`
}

type traceEntry struct {
	Module   string `json:"module"`
	Package  string `json:"package"`
	Function string `json:"function"`
}

type vulncheckRecord struct {
	Finding *finding `json:"finding"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	textOutput, textErr := runGovulncheck("./...")
	os.Stdout.Write(textOutput)
	if textErr == nil {
		return nil
	}

	jsonOutput, jsonErr := runGovulncheck("-format", "json", "./...")
	if jsonErr != nil {
		return fmt.Errorf("govulncheck failed and JSON report could not be parsed: %w", jsonErr)
	}

	unexpected, allowed, err := evaluateFindings(bytes.NewReader(jsonOutput), allowedFindings)
	if err != nil {
		return fmt.Errorf("parse govulncheck JSON: %w", err)
	}
	if len(unexpected) > 0 {
		return fmt.Errorf("govulncheck found unallowlisted actionable vulnerabilities: %s", strings.Join(unexpected, ", "))
	}
	if len(allowed) > 0 {
		fmt.Fprintf(os.Stderr, "govulncheck: ignoring known no-fix Docker advisories: %s\n", strings.Join(allowed, ", "))
	}
	return nil
}

func runGovulncheck(args ...string) ([]byte, error) {
	cmd := exec.Command("govulncheck", args...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func evaluateFindings(r io.Reader, allowlist []allowedFinding) ([]string, []string, error) {
	decoder := json.NewDecoder(r)
	unexpected := map[string]struct{}{}
	allowed := map[string]struct{}{}

	for {
		var record vulncheckRecord
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if record.Finding == nil || !record.Finding.actionable() {
			continue
		}

		if record.Finding.allowedBy(allowlist) {
			allowed[record.Finding.OSV] = struct{}{}
			continue
		}
		unexpected[record.Finding.OSV] = struct{}{}
	}

	return sortedKeys(unexpected), sortedKeys(allowed), nil
}

func (f finding) actionable() bool {
	for _, entry := range f.Trace {
		if entry.Package != "" || entry.Function != "" {
			return true
		}
	}
	return false
}

func (f finding) allowedBy(allowlist []allowedFinding) bool {
	if f.FixedVersion != "" {
		return false
	}
	for _, allowed := range allowlist {
		if f.OSV != allowed.OSV {
			continue
		}
		for _, entry := range f.Trace {
			if entry.Module == allowed.Module {
				return true
			}
		}
	}
	return false
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
