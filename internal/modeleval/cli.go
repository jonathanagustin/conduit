package modeleval

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// DefaultCorpusDir is the committed corpus, relative to the repository root.
const DefaultCorpusDir = "docs/evidence/model-evals"

const usage = `usage: conduit-eval <replay|record|live> [flags]

  replay   answer every prompt from the committed transcripts and fail when a
           prompt has no recording or a score is below the committed baseline
  record   call the configured provider, write its transcripts and its
           baseline score file, and print the change against the old baseline
  live     call the configured provider and score it; write nothing unless
           --out is given

record and live read the Hub's own variables: CONDUIT_MODEL_BASE_URL (with
CONDUIT_MODEL_NAME, CONDUIT_MODEL_API_KEY_FILE). Host model commands are unsupported.

flags:
  --corpus DIR   corpus directory (default ` + DefaultCorpusDir + `)
  --model ID     replay: the model id to replay (default: the corpus gateModel)
  --out FILE     also write the score report to FILE
`

// Main runs the conduit-eval command line and returns the exit status:
// 0 pass, 1 gate failure, 2 usage or configuration error.
func Main(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("conduit-eval "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	corpusDir := flags.String("corpus", DefaultCorpusDir, "corpus directory")
	modelID := flags.String("model", "", "model id to replay")
	out := flags.String("out", "", "write the score report to this file")
	if mode != ModeReplay && mode != ModeRecord && mode != ModeLive {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() > 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "conduit-eval:", err)
		return 2
	}
	corpus, err := LoadCorpus(*corpusDir)
	if err != nil {
		return fail(err)
	}
	options := Options{Mode: mode, ModelID: *modelID}
	if mode == ModeReplay {
		if options.ModelID == "" {
			options.ModelID = corpus.GateModel
		}
		if options.ModelID == "" {
			return fail(errors.New("replay needs --model or a corpus gateModel"))
		}
		options.Transcripts, err = LoadTranscripts(TranscriptDir(corpus.Dir, options.ModelID))
		if err != nil {
			return fail(err)
		}
	} else {
		if *modelID != "" {
			return fail(errors.New("--model applies to replay only; record and live take the id from the configured provider"))
		}
		provider, err := ProviderFromEnv(getenv)
		if err != nil {
			return fail(err)
		}
		options.Live, options.ModelID = provider.Model, provider.ID
	}
	outcome, err := Run(ctx, corpus, options)
	if err != nil {
		return fail(err)
	}
	report := outcome.Report
	printSummary(stdout, report)
	if *out != "" {
		if err := WriteReport(*out, report); err != nil {
			return fail(err)
		}
	}
	baselinePath := BaselinePath(corpus.Dir, options.ModelID)
	baseline, baselineErr := LoadReport(baselinePath)
	switch mode {
	case ModeReplay:
		if baselineErr != nil {
			return fail(fmt.Errorf("replay needs a committed baseline: %w", baselineErr))
		}
		if failures := Gate(report, baseline); len(failures) > 0 {
			fmt.Fprintln(stderr, "conduit-eval: replay gate FAILED")
			for _, failure := range failures {
				fmt.Fprintln(stderr, "  -", failure)
			}
			return 1
		}
		fmt.Fprintf(stdout, "replay gate passed: every prompt has a recording and no score is below %s\n", baselinePath)
	case ModeRecord:
		if baselineErr == nil {
			printDelta(stdout, report, baseline)
		} else if !errors.Is(baselineErr, os.ErrNotExist) {
			return fail(baselineErr)
		}
		dir := TranscriptDir(corpus.Dir, options.ModelID)
		if err := WriteTranscripts(dir, outcome.Transcripts); err != nil {
			return fail(err)
		}
		if err := WriteReport(baselinePath, report); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "wrote %d transcript(s) to %s and the baseline to %s; review the change before committing\n", len(outcome.Transcripts), dir, baselinePath)
	case ModeLive:
		if baselineErr == nil {
			printDelta(stdout, report, baseline)
		}
	}
	return 0
}

func printSummary(out io.Writer, report Report) {
	fmt.Fprintf(out, "model %s  corpus %s  skill %s (%s)  prompts %d (set %s)\n",
		report.ModelID, report.CorpusVersion, report.SkillVersion, short(report.SkillSHA256), len(report.PromptSHA256), short(report.PromptSetSHA256))
	for _, dimension := range Dimensions() {
		score := report.Dimensions[dimension]
		fmt.Fprintf(out, "  %-22s %3d/%-3d %.4f\n", dimension, score.Passed, score.Total, score.Value)
	}
	fmt.Fprintf(out, "  %-22s %3d/%-3d %.4f\n", "aggregate", report.Aggregate.Passed, report.Aggregate.Total, report.Aggregate.Value)
	for _, item := range report.Cases {
		for _, assertion := range item.Assertions {
			if !assertion.Pass {
				fmt.Fprintf(out, "  FAIL %s/%s: %s\n", item.ID, assertion.ID, assertion.Detail)
			}
		}
	}
}

func printDelta(out io.Writer, current, baseline Report) {
	fmt.Fprintln(out, "change against the committed baseline:")
	for _, dimension := range append(Dimensions(), "aggregate") {
		now, was := current.Dimensions[dimension], baseline.Dimensions[dimension]
		if dimension == "aggregate" {
			now, was = current.Aggregate, baseline.Aggregate
		}
		fmt.Fprintf(out, "  %-22s %d/%d -> %d/%d (%+.4f)\n", dimension, was.Passed, was.Total, now.Passed, now.Total, now.Value-was.Value)
	}
	for _, line := range Regressions(current, baseline) {
		fmt.Fprintln(out, " ", line)
	}
}

func short(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}
