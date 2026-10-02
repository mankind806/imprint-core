// Command imprint-dev keeps the imprint plugin tree honest.
//
//	imprint-dev gen   [--root dir]
//	imprint-dev check [--root dir] [--sarif file] [--release] [--today YYYY-MM-DD]
//	imprint-dev measure [--log file] [--projects dir] [--target 5m] [--format table|json]
//	imprint-dev setup (--plan | --check) [--root dir] [--repo path]
//	imprint-dev setup --apply --root dir [--repo path]
//	imprint-dev commit-check --range spec [--root dir] [--pr-author login] [--pr-title text]
//	    [--pr-body-file file] [--same-repo] [--sarif file]
//	imprint-dev hook-typesafe-check [--endpoint url] [--timeout sec]
//	imprint-dev hook-skill-suggestion [--endpoint url] [--timeout sec]
//	imprint-dev judge --gate name [--host h] [--deadline-ms N] [--no-ui] [--source s] [--endpoint url]
//	imprint-dev judge (--list | --version)
//	imprint-dev version | --version
//
// gen writes the SessionStart and SubagentStart hook payloads, and rules/AGENTS.md,
// from their one canonical source, hooks/kernkarte.md. check runs the invariants a
// program can decide over the plugin tree and reports every finding. measure reports the
// subagent runs that hooks/log-subagent.sh logged: duration, model, effort and
// whether a run went over the target; it exits 0 whatever it reports. setup compares
// (--plan, --check) or applies (--apply) setup inventory entries; what it may read,
// show and write is bounded by the path checks in setup.go; exit 0 all good, 1 drift
// (--check) or a refused/failed entry (--apply), 2 on usage, invalid inventory or the
// agent env guard. commit-check
// checks a range of new commits, and on a pull request its body, against
// .imprint/commit.conf and CONTRIBUTING.md's rules for commit messages (N78, N79,
// N96); see commitcheck.go. hook-typesafe-check runs opt-in advisory checks on
// memory and register writes via TypeSafe System One. hook-skill-suggestion
// suggests relevant imprint skills for user prompts via TypeSafe System One.
// judge is the shared Jev core (docs/judge.md): it reads one event as JSON on
// stdin and writes one verdict allow|warn|ask|block as JSON on stdout, with the
// gate's questions, thresholds and fail mode from judge/gates.json; see judge.go.
// version prints the build's version and VCS data.
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
  imprint-dev setup (--plan | --check) [--root dir] [--repo path]
  imprint-dev setup --apply --root dir [--repo path]
  imprint-dev commit-check --range spec [--root dir] [--pr-author login] [--pr-title text]
      [--pr-body-file file] [--same-repo] [--sarif file]
  imprint-dev hook-typesafe-check [--endpoint url] [--timeout sec]
  imprint-dev hook-skill-suggestion [--endpoint url] [--timeout sec]
  imprint-dev judge --gate name [--host claude|codex|agy|pi] [--deadline-ms N] [--no-ui]
      [--source hook|bench|test] [--endpoint url] [--registry file]
  imprint-dev judge (--list | --version)
  imprint-dev version | --version

gen      writes hooks/session-start.json, hooks/subagent-start.json and rules/AGENTS.md from hooks/kernkarte.md
check    checks the plugin tree; exit 0 all good, 1 violation, 2 the check could not run;
         an overdue re-check date warns, and fails only with --release
measure  reports subagent runs from the hook's log (default $CLAUDE_PLUGIN_DATA/subagent-log.jsonl);
         an overrun is reported, not a failure: exit 0, or 2 if the log cannot be read
setup    --plan/--check compare setup inventory entries against their targets without writing,
         --apply (explicit --root that is the imprint plugin) writes only allowlisted targets of
         the plugin's own inventory (--repo: status only); rights targets are only printed;
         exit 0 all good, 1 drift, a refused/failed entry or an unreadable --repo inventory
         (--check), 2 on usage, invalid inventory, plugin identity or env guard
commit-check   checks a range of new commits (and, with --pr-body-file, a pull request's
         body) against .imprint/commit.conf and CONTRIBUTING.md's commit-message rules;
         exit 0 no findings, 1 a finding, 2 the check could not run
hook-typesafe-check  evaluates memory/register file writes via TypeSafe System One for missing
         provenance (knowledge-keeping); exit 0 always (fail-open)
hook-skill-suggestion  suggests relevant imprint skills for user prompts via TypeSafe System One;
         exit 0 always (fail-open)
judge    the Jev core: one event as JSON on stdin, one verdict (allow, warn, ask, block) as JSON
         on stdout, one line in $IMPRINT_JUDGE_LOG or ${XDG_STATE_HOME:-~/.local/state}/imprint/judge.jsonl;
         exit 0 once the verdict is written (also when the call, its flags or its input failed:
         the gate's fail mode decides); 1 only for an unparsable flag or --list/--version without
         a registry; never 2
version  prints the version and the VCS data built into the binary
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
	case "setup":
		return runSetup(args[1:], stdout, stderr)
	case "commit-check":
		return runCommitCheck(args[1:], stdout, stderr)
	case "hook-typesafe-check":
		return runHookTypesafeCheck(args[1:], stdin, stdout, stderr)
	case "hook-skill-suggestion":
		return runHookSkillSuggestion(args[1:], stdin, stdout, stderr)
	case "judge":
		return runJudge(args[1:], stdin, stdout, stderr)
	case "version", "--version":
		return runVersion(args[1:], stdout, stderr)
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
