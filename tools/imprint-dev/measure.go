package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// measure reads the log that hooks/log-subagent.sh writes, pairs each
// SubagentStop with the latest SubagentStart of the same agent_id, and reports
// one row per run: when it started, agent type, the model(s) its transcript
// records, effort, duration and whether the duration went over the target.
// Quality is a column the tool cannot fill; the lead enters it by hand.

const logFileName = "subagent-log.jsonl"

// logEntry is one line of the hook's log.
type logEntry struct {
	TS             string `json:"ts"`
	Event          string `json:"hook_event_name"`
	AgentID        string `json:"agent_id"`
	AgentType      string `json:"agent_type"`
	SessionID      string `json:"session_id"`
	Effort         string `json:"effort"`
	Model          string `json:"model"`
	TranscriptPath string `json:"agent_transcript_path"`

	time time.Time
}

// measuredRun is one subagent run: a start, a stop, or both.
type measuredRun struct {
	AgentID    string   `json:"agent_id"`
	AgentType  string   `json:"agent_type"`
	SessionID  string   `json:"session_id"`
	Start      string   `json:"start,omitempty"`
	Stop       string   `json:"stop,omitempty"`
	Seconds    *int64   `json:"duration_seconds"`
	OverTarget *bool    `json:"over_target"`
	Models     []string `json:"models"`
	Effort     string   `json:"effort"`
	Transcript string   `json:"transcript"` // found, missing, not recorded
	Quality    string   `json:"quality"`    // never filled by the tool

	start, stop    time.Time
	transcriptPath string
}

const (
	transcriptFound       = "found"
	transcriptMissing     = "missing"
	transcriptNotRecorded = "not recorded"
	unknownModel          = "unknown"
)

// readLog parses the log. Lines that are not a start or stop with an agent_id,
// an agent_type and a parseable ts are skipped and counted.
func readLog(r io.Reader) (entries []logEntry, skipped int, err error) {
	br := bufio.NewReader(r)
	for {
		line, rerr := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var e logEntry
			ok := json.Unmarshal(line, &e) == nil &&
				(e.Event == "SubagentStart" || e.Event == "SubagentStop") &&
				e.AgentID != "" && e.AgentType != ""
			if ok {
				e.time, err = time.Parse(time.RFC3339, e.TS)
				ok = err == nil
				err = nil
			}
			if ok {
				entries = append(entries, e)
			} else {
				skipped++
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return nil, 0, rerr
		}
	}
	return entries, skipped, nil
}

// pairRuns pairs each stop with the latest start of the same agent_id before
// it. A start that a later start replaces before any stop is dropped: a resumed
// agent starts again under the same id. A start with no stop and a stop with
// no start still become rows, so that a gap in the log shows.
func pairRuns(entries []logEntry, target time.Duration) []measuredRun {
	sorted := make([]logEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].time.Before(sorted[j].time) })

	pending := map[string]logEntry{}
	var runs []measuredRun
	for _, e := range sorted {
		if e.Event == "SubagentStart" {
			pending[e.AgentID] = e
			continue
		}
		r := measuredRun{AgentID: e.AgentID, AgentType: e.AgentType, SessionID: e.SessionID,
			Stop: e.TS, stop: e.time, Effort: e.Effort, transcriptPath: e.TranscriptPath}
		if e.Model != "" {
			r.Models = []string{e.Model}
		}
		if s, ok := pending[e.AgentID]; ok {
			delete(pending, e.AgentID)
			r.Start, r.start = s.TS, s.time
			if s.Model != "" && s.Model != e.Model {
				r.Models = append([]string{s.Model}, r.Models...)
			}
			if r.Effort == "" {
				r.Effort = s.Effort
			}
			d := int64(e.time.Sub(s.time) / time.Second)
			over := e.time.Sub(s.time) > target
			r.Seconds, r.OverTarget = &d, &over
		}
		runs = append(runs, r)
	}
	for _, s := range pending {
		var models []string
		if s.Model != "" {
			models = []string{s.Model}
		}
		runs = append(runs, measuredRun{AgentID: s.AgentID, AgentType: s.AgentType, SessionID: s.SessionID,
			Start: s.TS, start: s.time, Effort: s.Effort, Models: models})
	}
	sort.SliceStable(runs, func(i, j int) bool {
		a, b := runs[i].first(), runs[j].first()
		if !a.Equal(b) {
			return a.Before(b)
		}
		return runs[i].AgentID < runs[j].AgentID
	})
	return runs
}

func (r measuredRun) first() time.Time {
	if !r.start.IsZero() {
		return r.start
	}
	return r.stop
}

// transcriptInfo reads only known metadata shapes: Claude assistant.message.model
// and top-level effort, or Codex turn_context.payload.model/effort. Rollout formats
// are not stable APIs; unknown records supply no model or effort.
func transcriptInfo(path string) (models, efforts []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	seenM, seenE := map[string]bool{}, map[string]bool{}
	br := bufio.NewReader(f)
	for {
		line, rerr := br.ReadBytes('\n')
		var l struct {
			Type    string `json:"type"`
			Effort  any    `json:"effort"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
			Payload struct {
				Model  string `json:"model"`
				Effort any    `json:"effort"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &l) == nil {
			var model string
			var effort any
			switch l.Type {
			case "assistant":
				model, effort = l.Message.Model, l.Effort
			case "turn_context":
				model, effort = l.Payload.Model, l.Payload.Effort
			}
			if model != "" && !seenM[model] {
				seenM[model] = true
				models = append(models, model)
			}
			if ef, ok := effort.(string); ok && ef != "" && !seenE[ef] {
				seenE[ef] = true
				efforts = append(efforts, ef)
			}
		}
		if errors.Is(rerr, io.EOF) {
			return models, efforts, nil
		}
		if rerr != nil {
			return nil, nil, rerr
		}
	}
}

// transcriptIndex maps agent-<id>.jsonl file names found under dir to their
// paths; it is built only when --projects is given and a run needs it.
func transcriptIndex(dir string) (map[string]string, error) {
	idx := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil // an unreadable corner does not stop the search
		}
		name := de.Name()
		if de.Type().IsRegular() && strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
			if _, dup := idx[name]; !dup {
				idx[name] = p
			}
		}
		return nil
	})
	return idx, err
}

// resolveModels fills Models, Transcript and, where the log has none, Effort.
func resolveModels(runs []measuredRun, projects string) error {
	var idx map[string]string
	for i := range runs {
		r := &runs[i]
		path := r.transcriptPath
		if path != "" {
			if _, err := os.Stat(path); err != nil {
				path = ""
			}
		}
		if path == "" && projects != "" {
			if idx == nil {
				var err error
				if idx, err = transcriptIndex(projects); err != nil {
					return fmt.Errorf("--projects: %w", err)
				}
			}
			path = idx["agent-"+r.AgentID+".jsonl"]
		}
		switch {
		case path == "" && r.transcriptPath == "":
			r.Transcript = transcriptNotRecorded
		case path == "":
			r.Transcript = transcriptMissing
		}
		if path == "" {
			if len(r.Models) == 0 {
				r.Models = []string{unknownModel}
			}
			continue
		}
		models, efforts, err := transcriptInfo(path)
		if err != nil {
			r.Transcript = transcriptMissing
			if len(r.Models) == 0 {
				r.Models = []string{unknownModel}
			}
			continue
		}
		r.Transcript = transcriptFound
		if len(r.Models) == 0 {
			r.Models = models
		}
		if len(r.Models) == 0 {
			r.Models = []string{unknownModel}
		}
		if r.Effort == "" {
			r.Effort = strings.Join(efforts, ",")
		}
	}
	return nil
}

func formatDuration(s *int64) string {
	if s == nil {
		return "-"
	}
	return (time.Duration(*s) * time.Second).String()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func writeTable(w io.Writer, runs []measuredRun, target time.Duration) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "started (UTC)\tagent_type\tmodel(s)\teffort\tduration\tover target\tquality\tagent")
	over, open, noStart := 0, 0, 0
	for _, r := range runs {
		started := "-"
		if !r.start.IsZero() {
			started = r.start.UTC().Format("2006-01-02 15:04")
		}
		var overText string
		switch {
		case r.OverTarget != nil && *r.OverTarget:
			overText = "yes"
			over++
		case r.OverTarget != nil:
			overText = "no"
		case r.Stop == "":
			overText = "no stop"
			open++
		default:
			overText = "no start"
			noStart++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", started, r.AgentType, strings.Join(r.Models, ","),
			orDash(r.Effort), formatDuration(r.Seconds), overText, "", shortID(r.AgentID))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%d run(s); %d over the target of %s; %d without a stop; %d without a start. Quality is entered by hand.\n",
		len(runs), over, target, open, noStart)
	return err
}

func runMeasure(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("measure", flag.ContinueOnError)
	logPath := fs.String("log", "", "the hook's log (default $CLAUDE_PLUGIN_DATA/"+logFileName+")")
	projects := fs.String("projects", "", "directory to search for agent-<id>.jsonl when a logged transcript path is absent or gone")
	target := fs.Duration("target", 5*time.Minute, "duration above which a run is marked over target")
	format := fs.String("format", "table", "table or json")
	if done, code := parseFlags(fs, args, stderr); done {
		return code
	}
	if *format != "table" && *format != "json" {
		fmt.Fprintf(stderr, "imprint-dev measure: --format must be table or json, not %q\n", *format)
		return exitError
	}
	if *logPath == "" {
		dir := os.Getenv("CLAUDE_PLUGIN_DATA")
		if dir == "" {
			fmt.Fprintln(stderr, "imprint-dev measure: --log is required when CLAUDE_PLUGIN_DATA is not set")
			return exitError
		}
		*logPath = filepath.Join(dir, logFileName)
	}
	f, err := os.Open(*logPath)
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev measure: cannot read the log: %v\n", err)
		return exitError
	}
	entries, skipped, err := readLog(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev measure: cannot read the log: %v\n", err)
		return exitError
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "imprint-dev measure: skipped %d line(s) that are not a start or stop with agent_id, agent_type and ts\n", skipped)
	}
	runs := pairRuns(entries, *target)
	if err := resolveModels(runs, *projects); err != nil {
		fmt.Fprintf(stderr, "imprint-dev measure: %v\n", err)
		return exitError
	}
	if *format == "json" {
		if runs == nil {
			runs = []measuredRun{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(runs)
	} else {
		err = writeTable(stdout, runs, *target)
	}
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev measure: %v\n", err)
		return exitError
	}
	return exitOK
}
