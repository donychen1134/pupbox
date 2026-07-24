package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donychen1134/pupbox/internal/replay"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "collect":
		err = collect(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pupbox-replay:", err)
		os.Exit(1)
	}
}

func collect(args []string) error {
	flags := flag.NewFlagSet("collect", flag.ContinueOnError)
	server := flags.String("server", "", "Pupbox server base URL")
	output := flags.String("out", "", "private output directory")
	limit := flags.Int("limit", 50, "number of recent events to inspect (max 200)")
	feedback := flags.String("feedback", "all", "all, rated, good, missed, or too_long")
	groupGap := flags.Duration("group-gap", 5*time.Minute, "start a new synthetic session after this gap")
	tokenEnv := flags.String("token-env", "PUPBOX_ACCESS_TOKEN", "environment variable containing the access token")
	timeout := flags.Duration("timeout", 30*time.Second, "per-request timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*server) == "" {
		return errors.New("--server is required")
	}
	if *output == "" {
		defaultOutput, err := defaultCorpusDir()
		if err != nil {
			return err
		}
		*output = defaultOutput
	}
	result, err := replay.Collect(context.Background(), replay.CollectOptions{
		ServerURL: *server,
		Token:     os.Getenv(*tokenEnv),
		OutputDir: *output,
		Limit:     *limit,
		Feedback:  *feedback,
		GroupGap:  *groupGap,
		Client:    &http.Client{Timeout: *timeout},
		Log:       os.Stderr,
	})
	if err != nil {
		return err
	}
	fmt.Printf("corpus=%s collected=%d skipped=%d\n", result.OutputDir, result.Collected, result.Skipped)
	return nil
}

func run(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	server := flags.String("server", "", "target Pupbox server base URL")
	corpus := flags.String("corpus", "", "private corpus directory")
	reportPath := flags.String("report", "", "report JSON path")
	tokenEnv := flags.String("token-env", "PUPBOX_ACCESS_TOKEN", "environment variable containing the access token")
	timeout := flags.Duration("timeout", 90*time.Second, "per-recording timeout")
	redactText := flags.Bool("redact-text", false, "omit transcripts and replies from the report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*server) == "" || strings.TrimSpace(*corpus) == "" {
		return errors.New("--server and --corpus are required")
	}
	report, path, err := replay.Run(context.Background(), replay.RunOptions{
		ServerURL:  *server,
		Token:      os.Getenv(*tokenEnv),
		CorpusDir:  *corpus,
		Report:     *reportPath,
		RedactText: *redactText,
		Client:     &http.Client{Timeout: *timeout},
		Log:        os.Stderr,
	})
	if err != nil {
		return err
	}
	fmt.Printf("report=%s total=%d passed=%d failed=%d stt_p50=%dms stt_p90=%dms total_p50=%dms total_p90=%dms\n",
		path, report.Summary.Total, report.Summary.Passed, report.Summary.Failed,
		report.Summary.STTP50MS, report.Summary.STTP90MS,
		report.Summary.TotalP50MS, report.Summary.TotalP90MS)
	if report.Summary.Failed > 0 {
		return fmt.Errorf("%d replay case(s) failed; report was written", report.Summary.Failed)
	}
	return nil
}

func defaultCorpusDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	name := time.Now().Format("20060102-150405")
	return filepath.Join(home, ".local", "share", "pupbox", "replay", name), nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  pupbox-replay collect --server URL [--out DIR] [--limit 50]
  pupbox-replay run --server URL --corpus DIR [--report FILE]

The access token is read from PUPBOX_ACCESS_TOKEN by default. Raw recordings,
transcripts, and reports are private artifacts and must not be committed.`)
}
