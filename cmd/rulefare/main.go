package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Khr0x/rulefare/engine"
)

const maxJSONBytes = 8 << 20 // Per file; bound allocation before decoding.

const usage = `Usage:
  rulefare rules validate --schema FILE --ruleset FILE [--json]
  rulefare rules evaluate --schema FILE --ruleset FILE --context FILE --layer NAME --at RFC3339
  rulefare serve

serve reads RULEFARE_HTTP_ADDR (default 127.0.0.1:8080) and
RULEFARE_SHUTDOWN_TIMEOUT (default 15s), and stops on SIGINT/SIGTERM.

Exit codes: 0 success (including NO_MATCH), 1 input/evaluation/I/O failure, 2 usage error.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") ||
		len(args) == 2 && args[0] == "rules" && (args[1] == "--help" || args[1] == "-h") {
		if _, err := io.WriteString(stdout, usage); err != nil {
			return fail(stderr, err)
		}
		return 0
	}
	if len(args) >= 1 && args[0] == "serve" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return serve(ctx, args[1:], os.Getenv, stderr)
	}
	if len(args) < 2 || args[0] != "rules" || (args[1] != "validate" && args[1] != "evaluate") {
		fmt.Fprint(stderr, usage)
		return 2
	}
	command := args[1]
	flags := flag.NewFlagSet("rulefare rules "+command, flag.ContinueOnError)
	var flagOutput bytes.Buffer
	flags.SetOutput(&flagOutput)
	schemaPath := flags.String("schema", "", "schema JSON file (required)")
	rulesPath := flags.String("ruleset", "", "ruleset JSON file (required)")
	var jsonOutput bool
	var contextPath, layer, at string
	if command == "validate" {
		flags.BoolVar(&jsonOutput, "json", false, "write ValidationReport as JSON")
	} else {
		flags.StringVar(&contextPath, "context", "", "context JSON object file (required)")
		flags.StringVar(&layer, "layer", "", "rule layer (required)")
		flags.StringVar(&at, "at", "", "effective timestamp in RFC3339 format (required)")
	}
	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, err := io.Copy(stdout, &flagOutput); err != nil {
				return fail(stderr, err)
			}
			return 0
		}
		io.Copy(stderr, &flagOutput)
		return 2
	}
	if flags.NArg() != 0 || *schemaPath == "" || *rulesPath == "" ||
		command == "evaluate" && (contextPath == "" || layer == "" || at == "") {
		fmt.Fprintln(stderr, "required flags are missing or unexpected positional arguments were supplied")
		fmt.Fprint(stderr, usage)
		return 2
	}
	var effectiveAt time.Time
	if command == "evaluate" {
		var err error
		effectiveAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			fmt.Fprintln(stderr, "--at must be an RFC3339 timestamp with a timezone")
			return 2
		}
	}

	var schema engine.Schema
	if err := readJSON(*schemaPath, &schema); err != nil {
		return fail(stderr, err)
	}
	var rules engine.RuleSet
	if err := readJSON(*rulesPath, &rules); err != nil {
		return fail(stderr, err)
	}
	program, report := engine.Compile(schema, rules)
	if command == "validate" {
		if err := writeReport(stdout, report, jsonOutput); err != nil {
			return fail(stderr, err)
		}
		if !report.Valid() {
			return 1
		}
		return 0
	}
	if !report.Valid() {
		if err := writeReport(stderr, report, true); err != nil {
			return fail(stderr, err)
		}
		return 1
	}
	var context map[string]any
	if err := readJSON(contextPath, &context); err != nil {
		return fail(stderr, err)
	}
	result, evalErr := program.Evaluate(engine.Evaluation{
		Layer: layer, EffectiveAt: effectiveAt, Context: context,
	})
	if err := writeJSON(stdout, result); err != nil {
		return fail(stderr, err)
	}
	if evalErr != nil {
		var invalid *engine.InvalidEvaluationError
		if errors.As(evalErr, &invalid) {
			if err := writeReport(stderr, invalid.Report, true); err != nil {
				return fail(stderr, err)
			}
			return 1
		}
		return fail(stderr, evalErr)
	}
	return 0
}

// readJSON bounds each document and accepts exactly one non-null object. The
// decoder rejects unknown struct fields and preserves integers for the engine.
func readJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxJSONBytes+1))
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	if len(data) > maxJSONBytes {
		return fmt.Errorf("%q exceeds the %d-byte JSON limit", path, maxJSONBytes)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("%q must contain a JSON object", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		// Decoder errors (notably time parsing) may contain input values.
		return fmt.Errorf("%q: invalid JSON, field type, or unknown field", path)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%q must contain exactly one JSON object", path)
	}
	return nil
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeReport(out io.Writer, report engine.ValidationReport, asJSON bool) error {
	if asJSON {
		return writeJSON(out, report)
	}
	if report.Valid() {
		_, err := fmt.Fprintln(out, "Valid ruleset.")
		return err
	}
	for _, issue := range report.Issues {
		if _, err := fmt.Fprintf(out, "%s %s: %s\n", issue.Code, issue.Path, issue.Message); err != nil {
			return err
		}
	}
	return nil
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "rulefare:", err)
	return 1
}
