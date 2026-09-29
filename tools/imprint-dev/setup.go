package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

type setupEntry struct {
	ID           string `json:"id"`
	Typ          string `json:"typ"`
	Quelle       string `json:"quelle"`
	Ziel         string `json:"ziel"`
	Rechte       *bool  `json:"rechte"`
	Beschreibung string `json:"beschreibung"`
}

var validTyps = map[string]bool{
	"shim":           true,
	"githook":        true,
	"copy":           true,
	"rechte-vorlage": true,
	"marketplace":    true,
	"mcp":            true,
	"systemd":        true,
}

func parseInventory(data []byte) ([]setupEntry, error) {
	var entries []setupEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	for i, e := range entries {
		if strings.TrimSpace(e.ID) == "" {
			return nil, fmt.Errorf("entry %d: missing or empty 'id'", i+1)
		}
		if !validTyps[e.Typ] {
			return nil, fmt.Errorf("entry %d (%s): invalid 'typ' %q", i+1, e.ID, e.Typ)
		}
		if strings.TrimSpace(e.Quelle) == "" {
			return nil, fmt.Errorf("entry %d (%s): missing or empty 'quelle'", i+1, e.ID)
		}
		if strings.TrimSpace(e.Ziel) == "" {
			return nil, fmt.Errorf("entry %d (%s): missing or empty 'ziel'", i+1, e.ID)
		}
		if e.Rechte == nil {
			return nil, fmt.Errorf("entry %d (%s): missing 'rechte'", i+1, e.ID)
		}
		if strings.TrimSpace(e.Beschreibung) == "" {
			return nil, fmt.Errorf("entry %d (%s): missing or empty 'beschreibung'", i+1, e.ID)
		}
	}
	return entries, nil
}

func expandZiel(ziel string) string {
	home := os.Getenv("HOME")
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	s := ziel
	s = strings.ReplaceAll(s, "${XDG_CONFIG_HOME}", xdg)
	s = strings.ReplaceAll(s, "$XDG_CONFIG_HOME", xdg)
	s = strings.ReplaceAll(s, "${HOME}", home)
	s = strings.ReplaceAll(s, "$HOME", home)
	return filepath.Clean(s)
}

type evalResult struct {
	entry    setupEntry
	status   string
	reason   string
	diffText string
}

func evaluateEntry(e setupEntry, qPath, zPath string) evalResult {
	res := evalResult{entry: e}

	qData, qErr := os.ReadFile(qPath)
	if qErr != nil {
		res.status = "nicht-prüfbar"
		res.reason = fmt.Sprintf("quelle unreadable: %v", qErr)
		return res
	}

	zData, zErr := os.ReadFile(zPath)
	if zErr != nil {
		if errors.Is(zErr, fs.ErrNotExist) {
			res.status = "fehlt"
			return res
		}
		if zInfo, statErr := os.Stat(zPath); statErr == nil && zInfo.IsDir() {
			res.status = "nicht-prüfbar"
			res.reason = "ziel is a directory"
			return res
		}
		res.status = "nicht-prüfbar"
		res.reason = fmt.Sprintf("ziel unreadable: %v", zErr)
		return res
	}

	if bytes.Equal(qData, zData) {
		res.status = "gleich"
		return res
	}

	res.status = "abweichend"
	res.diffText = generateDiff(string(qData), string(zData))
	return res
}

func generateDiff(qContent, zContent string) string {
	qLines := splitLinesString(qContent)
	zLines := splitLinesString(zContent)
	edits := diffLines(qLines, zLines)
	if len(edits) > 15 {
		edits = append(edits[:15], "... (diff truncated)")
	}
	return strings.Join(edits, "\n")
}

func splitLinesString(s string) []string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func diffLines(srcLines, dstLines []string) []string {
	m, n := len(srcLines), len(dstLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			if srcLines[i] == dstLines[j] {
				dp[i+1][j+1] = dp[i][j] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i+1][j+1] = dp[i+1][j]
			} else {
				dp[i+1][j+1] = dp[i][j+1]
			}
		}
	}
	var edits []string
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && srcLines[i-1] == dstLines[j-1] {
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			edits = append(edits, "+ "+dstLines[j-1])
			j--
		} else if i > 0 && (j == 0 || dp[i][j-1] < dp[i-1][j]) {
			edits = append(edits, "- "+srcLines[i-1])
			i--
		}
	}
	for k := 0; k < len(edits)/2; k++ {
		edits[k], edits[len(edits)-1-k] = edits[len(edits)-1-k], edits[k]
	}
	return edits
}

func runSetup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	plan := fs.Bool("plan", false, "compare setup inventory entries against target files without writing")
	root := fs.String("root", ".", "plugin root directory")
	repo := fs.String("repo", "", "optional repository path")

	if done, code := parseFlags(fs, args, stderr); done {
		if code != exitOK {
			fmt.Fprint(stderr, usageText)
		}
		return code
	}

	if !*plan {
		fmt.Fprintln(stderr, "imprint-dev setup: --plan flag is required")
		fmt.Fprint(stderr, usageText)
		return exitError
	}

	rootInvPath := filepath.Join(*root, "setup", "inventar.json")
	rootData, err := os.ReadFile(rootInvPath)
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev setup: cannot read %s: %v\n%s", rootInvPath, err, usageText)
		return exitError
	}

	rootEntries, err := parseInventory(rootData)
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev setup: invalid inventory %s: %v\n%s", rootInvPath, err, usageText)
		return exitError
	}

	type processedItem struct {
		invRoot string
		entry   setupEntry
	}

	var items []processedItem
	for _, e := range rootEntries {
		items = append(items, processedItem{invRoot: *root, entry: e})
	}

	var repoNote string
	if *repo != "" {
		repoInvPath := filepath.Join(*repo, ".imprint", "setup.json")
		repoData, rErr := os.ReadFile(repoInvPath)
		if rErr != nil {
			repoNote = fmt.Sprintf("note: repo setup inventory at %s is missing or unreadable (%v)", repoInvPath, rErr)
		} else {
			repoEntries, pErr := parseInventory(repoData)
			if pErr != nil {
				fmt.Fprintf(stderr, "imprint-dev setup: invalid repo inventory %s: %v\n%s", repoInvPath, pErr, usageText)
				return exitError
			}
			for _, e := range repoEntries {
				items = append(items, processedItem{invRoot: *repo, entry: e})
			}
		}
	}

	var results []evalResult
	for _, item := range items {
		e := item.entry
		qPath := filepath.Join(item.invRoot, filepath.FromSlash(e.Quelle))
		zPath := expandZiel(e.Ziel)

		res := evaluateEntry(e, qPath, zPath)
		results = append(results, res)
	}

	if repoNote != "" {
		fmt.Fprintln(stdout, repoNote)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\ttyp\tstatus\tbeschreibung")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.entry.ID, r.entry.Typ, r.status, r.entry.Beschreibung)
	}
	tw.Flush()

	for _, r := range results {
		if r.status == "nicht-prüfbar" && r.reason != "" {
			fmt.Fprintf(stdout, "\n[%s] nicht-prüfbar: %s\n", r.entry.ID, r.reason)
		} else if r.status == "abweichend" && r.diffText != "" {
			fmt.Fprintf(stdout, "\n--- diff for %s (quelle vs ziel) ---\n%s\n", r.entry.ID, r.diffText)
		}
	}

	return exitOK
}
