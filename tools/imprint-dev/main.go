// Command imprint-dev keeps the imprint plugin tree honest.
//
//	imprint-dev gen   [--root dir]
//	imprint-dev check [--root dir] [--sarif file] [--release] [--today YYYY-MM-DD]
//	imprint-dev measure [--log file] [--projects dir] [--target 5m] [--format table|json]
//	imprint-dev commit-check --range spec [--root dir] [--pr-author login] [--pr-title text]
//	    [--pr-body-file file] [--same-repo] [--sarif file]
//	imprint-dev hook-typesafe-check [--endpoint url] [--timeout sec]
//
// gen writes the SessionStart and SubagentStart hook payloads, and rules/AGENTS.md,
// from their one canonical source, hooks/kernkarte.md. check runs the invariants a
// program can decide over the plugin tree and reports every finding. measure reports the
// subagent runs that hooks/log-subagent.sh logged: duration, model, effort and
// whether a run went over the target; it exits 0 whatever it reports. commit-check
// checks a range of new commits, and on a pull request its body, against
// .imprint/commit.conf and CONTRIBUTING.md's rules for commit messages (N78, N79,
// N96); see commitcheck.go. hook-typesafe-check runs opt-in advisory checks on
// memory and register writes via TypeSafe System One.
//
// An overdue re-check date is a warning, which leaves the exit code alone,
// unless --release is given: overdue dates block a release, never the ordinary
// test run. --today sets the day they are measured against; it defaults to the
// local date.
//
// Exit codes: 0 all good, 1 at least one violation, 2 the tool itself could not
// run (unreadable root, an I/O error, an unwritable SARIF file, bad usage).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	exitOK        = 0
	exitViolation = 1
	exitError     = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = `usage:
  imprint-dev gen   [--root dir]
  imprint-dev check [--root dir] [--sarif file] [--release] [--today YYYY-MM-DD]
  imprint-dev measure [--log file] [--projects dir] [--target 5m] [--format table|json]
  imprint-dev commit-check --range spec [--root dir] [--pr-author login] [--pr-title text]
      [--pr-body-file file] [--same-repo] [--sarif file]
  imprint-dev hook-typesafe-check [--endpoint url] [--timeout sec]

gen      writes hooks/session-start.json, hooks/subagent-start.json and rules/AGENTS.md from hooks/kernkarte.md
check    checks the plugin tree; exit 0 all good, 1 violation, 2 the check could not run;
         an overdue re-check date warns, and fails only with --release
measure  reports subagent runs from the hook's log (default $CLAUDE_PLUGIN_DATA/subagent-log.jsonl);
         an overrun is reported, not a failure: exit 0, or 2 if the log cannot be read
commit-check   checks a range of new commits (and, with --pr-body-file, a pull request's
         body) against .imprint/commit.conf and CONTRIBUTING.md's commit-message rules;
         exit 0 no findings, 1 a finding, 2 the check could not run
hook-typesafe-check  evaluates memory/register file writes via TypeSafe System One for missing
         provenance (knowledge-keeping); exit 0 always (fail-open)
`

func run(args []string, stdout, stderr io.Writer) int {
	return runWithStdin(args, os.Stdin, stdout, stderr)
}

func runWithStdin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitError
	}
	switch args[0] {
	case "gen":
		return runGen(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stderr)
	case "measure":
		return runMeasure(args[1:], stdout, stderr)
	case "commit-check":
		return runCommitCheck(args[1:], stdout, stderr)
	case "hook-typesafe-check":
		return runHookTypesafeCheck(args[1:], stdin, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	default:
		fmt.Fprintf(stderr, "imprint-dev: unknown subcommand %q\n%s", args[0], usageText)
		return exitError
	}
}

// parseFlags parses a subcommand's flags. It returns done=true with the exit
// code when the caller should stop: on -h, on a bad flag, or on a stray argument.
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) (done bool, code int) {
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, exitOK
		}
		return true, exitError
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "imprint-dev %s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		return true, exitError
	}
	return false, 0
}

func runGen(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	root := fs.String("root", ".", "plugin root")
	if done, code := parseFlags(fs, args, stderr); done {
		return code
	}
	written, err := writeGenerated(*root)
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev gen: %v\n", err)
		return exitError
	}
	for _, p := range written {
		fmt.Fprintf(stdout, "imprint-dev gen: wrote %s\n", p)
	}
	return exitOK
}

func runCheck(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	root := fs.String("root", ".", "plugin root")
	sarifPath := fs.String("sarif", "", "also write SARIF 2.1.0 to this file")
	release := fs.Bool("release", false, "a release run: an overdue re-check date is a violation, not a warning")
	today := fs.String("today", "", "the date re-check dates are measured against, YYYY-MM-DD (default: the local date)")
	if done, code := parseFlags(fs, args, stderr); done {
		return code
	}
	if *today == "" {
		*today = time.Now().Format(dateLayout)
	} else if _, err := time.Parse(dateLayout, *today); err != nil {
		fmt.Fprintf(stderr, "imprint-dev check: --today %q is not a date in the form YYYY-MM-DD\n", *today)
		return exitError
	}

	e := &env{Root: *root, Today: *today, Release: *release}
	rep := runChecks(e)
	printSummary(stderr, rep)
	if *sarifPath != "" {
		if err := writeSARIF(*sarifPath, rep); err != nil {
			fmt.Fprintf(stderr, "imprint-dev check: could not write SARIF: %v\n", err)
			return exitError
		}
	}
	return rep.exitCode()
}
