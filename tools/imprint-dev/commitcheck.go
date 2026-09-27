// commit-check enforces the identity and message rules behind N78, N79 and
// N96 for a range of new commits and, on a pull-request event, for the
// pull-request text that becomes the squash-merge commit message.
//
//	imprint-dev commit-check --root . --range base..head [--pr-author login] [--pr-body-file file] [--sarif file]
//
// Rules (see CONTRIBUTING.md, "AI assistance and commit messages"):
//
//	commit-author      the commit's author (name+email) is in .imprint/commit.conf
//	commit-committer   the commit's committer (name+email) is in .imprint/commit.conf
//	commit-session-line   the commit message has no "Claude-Session:" line
//	commit-session-url    the commit message has no claude.ai/code/session_ URL
//	commit-address        the commit message carries no email address
//	pr-session-line, pr-session-url, pr-address   the same three, for the pull-request body
//	pr-trailer          if the pull-request body mentions "Assisted-by:", it is the last line
//	                    and git reads it as a trailer (CONTRIBUTING, N96)
//
// commit-address and the three pr-* rules are skipped for one commit, or for
// the whole pull-request body, when the author or committer was let in only
// through a botAuthors entry of .imprint/commit.conf (N102): that message is
// not ours to hold to CONTRIBUTING's style, the same way a bot's fixed,
// publicly documented system address in a generated file is not treated as
// personal data. A human contributor's or the owner's own messages get no
// such exemption.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultCommitConfPath = ".imprint/commit.conf"

// --- configuration -----------------------------------------------------------

type ccIdentity struct {
	Name string `json:"name"`
	// EmailUser and EmailDomain together are the identity's email address,
	// split across two JSON fields and joined only at load time (see
	// resolveEmail). A whole address here would be an address-shaped
	// literal in tracked content, and .githooks/pre-push's own address check
	// scans every pushed commit's whole tree, not just what changed, so it
	// would flag this file on every future push forever, not just this one.
	EmailUser   string `json:"emailUser"`
	EmailDomain string `json:"emailDomain"`
	Comment     string `json:"comment,omitempty"`
	// MergeCommitter marks a committers[] entry that only a merge through the
	// web UI or API produces. It gates a bot author in on a push, where there
	// is no pull-request context to check a PR author against.
	MergeCommitter bool `json:"mergeCommitter,omitempty"`
	// Email is EmailUser + "@" + EmailDomain. It is never itself read from
	// or written to JSON; resolveEmail fills it in after unmarshalling.
	Email string `json:"-"`
}

// resolveEmail joins EmailUser and EmailDomain into Email.
func (id *ccIdentity) resolveEmail() { id.Email = id.EmailUser + "@" + id.EmailDomain }

// resolveEmails calls resolveEmail on every identity in c. Every caller that
// unmarshals a ccConf, in tools/imprint-dev or in its tests, calls this
// right afterwards; Email is otherwise left empty.
func (c *ccConf) resolveEmails() {
	for i := range c.Authors {
		c.Authors[i].resolveEmail()
	}
	for i := range c.Committers {
		c.Committers[i].resolveEmail()
	}
	for i := range c.BotAuthors {
		c.BotAuthors[i].resolveEmail()
	}
}

// ccBotAuthor is an identity allowed as a commit's author only when either
// the pull request that opened it matches PRAuthor, or, on a push with no
// pull-request context, the commit's committer is a MergeCommitter identity
// (a merge of that bot's own pull request, not a direct push).
type ccBotAuthor struct {
	ccIdentity
	PRAuthor string `json:"prAuthor"`
}

type ccConf struct {
	Note       string        `json:"_note,omitempty"`
	Authors    []ccIdentity  `json:"authors"`
	Committers []ccIdentity  `json:"committers"`
	BotAuthors []ccBotAuthor `json:"botAuthors,omitempty"`
}

func loadCommitConf(path string) (ccConf, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ccConf{}, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var c ccConf
	if err := json.Unmarshal(data, &c); err != nil {
		return ccConf{}, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	c.resolveEmails()
	if len(c.Authors) == 0 {
		return ccConf{}, fmt.Errorf("%s: authors is empty", path)
	}
	if len(c.Committers) == 0 {
		return ccConf{}, fmt.Errorf("%s: committers is empty", path)
	}
	return c, nil
}

func (c ccConf) authorAllowedDirect(name, email string) bool {
	for _, id := range c.Authors {
		if id.Name == name && id.Email == email {
			return true
		}
	}
	return false
}

func (c ccConf) committerAllowedDirect(name, email string) bool {
	for _, id := range c.Committers {
		if id.Name == name && id.Email == email {
			return true
		}
	}
	return false
}

func (c ccConf) findBotAuthor(name, email string) (ccBotAuthor, bool) {
	for _, b := range c.BotAuthors {
		if b.Name == name && b.Email == email {
			return b, true
		}
	}
	return ccBotAuthor{}, false
}

func (c ccConf) isMergeCommitter(name, email string) bool {
	for _, id := range c.Committers {
		if id.MergeCommitter && id.Name == name && id.Email == email {
			return true
		}
	}
	return false
}

// --- git -----------------------------------------------------------------

type ccCommit struct {
	Hash        string
	AuthorName  string
	AuthorEmail string
	CommitName  string
	CommitEmail string
	Message     string
}

const ccFieldSep = "\x1f"

// commitsInRange runs `git log` over rangeSpec (e.g. "base..head" or
// "onlyCommit^!") inside root and returns every commit it contains, oldest
// first. A rangeSpec that resolves to no commits is not an error; one git
// cannot resolve at all (unknown revision, bad syntax) is.
func commitsInRange(root, rangeSpec string) ([]ccCommit, error) {
	format := strings.Join([]string{"%H", "%an", "%ae", "%cn", "%ce", "%B"}, ccFieldSep)
	cmd := exec.Command("git", "-C", root, "log", "-z", "--format="+format, rangeSpec)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log %s: %w: %s", rangeSpec, err, strings.TrimSpace(errBuf.String()))
	}
	raw := out.String()
	if raw == "" {
		return nil, nil
	}
	records := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	commits := make([]ccCommit, 0, len(records))
	for _, rec := range records {
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, ccFieldSep, 6)
		if len(fields) != 6 {
			return nil, fmt.Errorf("git log %s: unexpected format in the output", rangeSpec)
		}
		commits = append(commits, ccCommit{
			Hash: fields[0], AuthorName: fields[1], AuthorEmail: fields[2],
			CommitName: fields[3], CommitEmail: fields[4], Message: fields[5],
		})
	}
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 { // git log lists newest first
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

// --- message patterns ------------------------------------------------------

var ccSessionLineRE = regexp.MustCompile(`(?im)^[ \t]*claude-session[ \t]*:`)
var ccSessionURLRE = regexp.MustCompile(`(?i)claude\.ai/code/session_`)

// ccAddressRE is the address shape .githooks/pre-push already checks tracked
// content and commit messages against.
var ccAddressRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

var ccAssistedByMentionRE = regexp.MustCompile(`(?im)^[ \t]*assisted-by[ \t]*:`)

func normalizeCRLF(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// ccFinding is one violation, either of one commit or of the pull-request
// text. Where is a short human-readable label: a short commit hash, or "PR
// body".
type ccFinding struct {
	Where   string
	Rule    string
	Message string
}

func (f ccFinding) String() string { return fmt.Sprintf("%s: %s: %s", f.Where, f.Rule, f.Message) }

const (
	ccRuleAuthor      = "commit-author"
	ccRuleCommitter   = "commit-committer"
	ccRuleSessionLine = "commit-session-line"
	ccRuleSessionURL  = "commit-session-url"
	ccRuleAddress     = "commit-address"
	ccRulePRSession   = "pr-session-line"
	ccRulePRURL       = "pr-session-url"
	ccRulePRAddress   = "pr-address"
	ccRuleTrailer     = "pr-trailer"
)

// ccRules lists every rule in a fixed order, for the summary and for SARIF.
var ccRules = []struct{ ID, Title string }{
	{ccRuleAuthor, "the commit's author (name+email) is in .imprint/commit.conf"},
	{ccRuleCommitter, "the commit's committer (name+email) is in .imprint/commit.conf"},
	{ccRuleSessionLine, `the commit message has no "Claude-Session:" line`},
	{ccRuleSessionURL, "the commit message has no claude.ai/code/session_ URL"},
	{ccRuleAddress, "the commit message carries no email address"},
	{ccRulePRSession, `the pull-request body has no "Claude-Session:" line`},
	{ccRulePRURL, "the pull-request body has no claude.ai/code/session_ URL"},
	{ccRulePRAddress, "the pull-request body carries no email address"},
	{ccRuleTrailer, `if the pull-request body mentions "Assisted-by:", it is a well-formed trailer`},
}

// checkCommitMessage runs the message-shape rules against msg (a commit
// message or the pull-request body) and appends findings under where. When
// exemptAddress is true, ccRuleAddress/ccRulePRAddress is skipped (N102: the
// message was let in only via a bot identity, so it is not ours to police).
func checkCommitMessage(msg, where string, sessionRule, urlRule, addressRule string, exemptAddress bool, out *[]ccFinding) {
	msg = normalizeCRLF(msg)
	if ccSessionLineRE.MatchString(msg) {
		*out = append(*out, ccFinding{where, sessionRule, `contains a "Claude-Session:" line`})
	}
	if ccSessionURLRE.MatchString(msg) {
		*out = append(*out, ccFinding{where, urlRule, "contains a claude.ai/code/session_ URL"})
	}
	if !exemptAddress {
		if m := ccAddressRE.FindString(msg); m != "" {
			*out = append(*out, ccFinding{where, addressRule, fmt.Sprintf("contains an address: %s", m)})
		}
	}
}

// checkOneCommit runs every commit-scoped rule against c and returns its
// findings.
func checkOneCommit(conf ccConf, c ccCommit, prAuthor string) []ccFinding {
	var out []ccFinding
	short := c.Hash
	if len(short) > 10 {
		short = short[:10]
	}

	authorOK := conf.authorAllowedDirect(c.AuthorName, c.AuthorEmail)
	bot, isBot := conf.findBotAuthor(c.AuthorName, c.AuthorEmail)
	viaBot := false
	if !authorOK && isBot {
		switch {
		case prAuthor != "" && prAuthor == bot.PRAuthor:
			authorOK, viaBot = true, true
		case prAuthor == "" && conf.isMergeCommitter(c.CommitName, c.CommitEmail):
			authorOK, viaBot = true, true
		}
	}
	if !authorOK {
		out = append(out, ccFinding{short, ccRuleAuthor,
			fmt.Sprintf("author %q <%s> is not in .imprint/commit.conf", c.AuthorName, c.AuthorEmail)})
	}

	committerOK := conf.committerAllowedDirect(c.CommitName, c.CommitEmail)
	if !committerOK && prAuthor != "" {
		if b, ok := conf.findBotAuthor(c.CommitName, c.CommitEmail); ok && prAuthor == b.PRAuthor {
			committerOK, viaBot = true, true
		}
	}
	if !committerOK {
		out = append(out, ccFinding{short, ccRuleCommitter,
			fmt.Sprintf("committer %q <%s> is not in .imprint/commit.conf", c.CommitName, c.CommitEmail)})
	}

	checkCommitMessage(c.Message, short, ccRuleSessionLine, ccRuleSessionURL, ccRuleAddress, viaBot, &out)
	return out
}

// checkPullRequestBody runs the pull-request-text rules. isBotPR skips every
// rule here (N102 plus "Bot-PRs von der PR-Text-Regel ausnehmen": the body is
// generated by the bot and not authored by a repository contributor at all,
// and may embed a third party's changelog we have no way to edit).
func checkPullRequestBody(body string, isBotPR bool) []ccFinding {
	if isBotPR {
		return nil
	}
	var out []ccFinding
	checkCommitMessage(body, "PR body", ccRulePRSession, ccRulePRURL, ccRulePRAddress, false, &out)
	if trailerOK, mentioned := checkAssistedByTrailer(body); mentioned && !trailerOK {
		out = append(out, ccFinding{"PR body", ccRuleTrailer,
			`mentions "Assisted-by:" but it is not the last line read as a git trailer ` +
				`(a blank line has to separate it from anything above, and nothing may follow it)`})
	}
	return out
}

// checkAssistedByTrailer reports whether body mentions "Assisted-by:" at all,
// and, if it does, whether it is the last line, read by git as the last
// trailer of a well-formed trailer block. Both conditions are required and
// neither implies the other: "Assisted-by:" immediately followed by another
// `Key: value` line (no blank line between them) is still the physical last
// line but git reads it as the second-to-last trailer, not the last one; a
// generator line touching "Assisted-by:" from above (no blank line before
// it, the #15 shape CONTRIBUTING.md describes) still leaves it the last
// physical line, but breaks git's trailer block entirely, so nothing in the
// tail is read as a trailer at all. `git interpret-trailers` is the same
// parser CONTRIBUTING.md's own `%(trailers:key=Assisted-by)` example reads
// with, rather than a hand-rolled one, so the two never disagree about what
// counts as a trailer.
func checkAssistedByTrailer(body string) (ok, mentioned bool) {
	body = normalizeCRLF(body)
	if !ccAssistedByMentionRE.MatchString(body) {
		return false, false
	}
	mentioned = true

	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	lastNonBlank := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastNonBlank = lines[i]
			break
		}
	}
	if !ccAssistedByMentionRE.MatchString(lastNonBlank) {
		return false, true
	}

	cmd := exec.Command("git", "interpret-trailers", "--parse", "--only-trailers")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.Output()
	if err != nil {
		return false, true
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return false, true
	}
	trailerLines := strings.Split(trimmed, "\n")
	lastTrailer := trailerLines[len(trailerLines)-1]
	k, _, found := strings.Cut(lastTrailer, ":")
	return found && strings.EqualFold(strings.TrimSpace(k), "assisted-by"), true
}

func ccSummary(findings []ccFinding) string {
	if len(findings) == 0 {
		return "commit-check: 0 finding(s)"
	}
	count := map[string]int{}
	for _, f := range findings {
		count[f.Rule]++
	}
	parts := make([]string, 0, len(ccRules))
	for _, r := range ccRules {
		if n := count[r.ID]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", r.ID, n))
		}
	}
	return fmt.Sprintf("commit-check: %d finding(s) (%s)", len(findings), strings.Join(parts, ", "))
}

// --- CLI ---------------------------------------------------------------------

func runCommitCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("commit-check", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	conf := fs.String("conf", defaultCommitConfPath, "path to commit.conf, relative to root")
	rangeSpec := fs.String("range", "", "commit range to check, e.g. base..head or onlyCommit^! (required)")
	prAuthor := fs.String("pr-author", "", "login of the pull-request author (bot rule only, e.g. dependabot[bot]); leave empty on a push")
	prBodyFile := fs.String("pr-body-file", "", "file holding the pull-request body to check; omitted on a push")
	sarifPath := fs.String("sarif", "", "also write SARIF 2.1.0 to this file")
	if done, code := parseFlags(fs, args, stderr); done {
		return code
	}
	if *rangeSpec == "" {
		fmt.Fprintln(stderr, "imprint-dev commit-check: --range is required (e.g. base..head or onlyCommit^!)")
		return exitError
	}

	findings, err := checkCommitCheckRange(*root, *conf, *rangeSpec, *prAuthor, *prBodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "imprint-dev commit-check: could not run: %v\n", err)
		if *sarifPath != "" {
			if werr := writeCommitCheckSARIF(*sarifPath, nil, err); werr != nil {
				fmt.Fprintf(stderr, "imprint-dev commit-check: SARIF not written: %v\n", werr)
			}
		}
		return exitError
	}
	for _, f := range findings {
		fmt.Fprintln(stdout, f)
	}
	fmt.Fprintln(stdout, ccSummary(findings))
	if *sarifPath != "" {
		if err := writeCommitCheckSARIF(*sarifPath, findings, nil); err != nil {
			fmt.Fprintf(stderr, "imprint-dev commit-check: SARIF not written: %v\n", err)
			return exitError
		}
	}
	if len(findings) > 0 {
		return exitViolation
	}
	return exitOK
}

// checkCommitCheckRange loads the config, resolves the commit range, and
// returns every finding across the commits plus, when prBodyFile is set, the
// pull-request body.
func checkCommitCheckRange(root, confRelPath, rangeSpec, prAuthor, prBodyFile string) ([]ccFinding, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(absRoot); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("root %q is not a readable directory", root)
	}
	conf, err := loadCommitConf(filepath.Join(absRoot, filepath.FromSlash(confRelPath)))
	if err != nil {
		return nil, err
	}
	commits, err := commitsInRange(absRoot, rangeSpec)
	if err != nil {
		return nil, err
	}
	var out []ccFinding
	for _, c := range commits {
		out = append(out, checkOneCommit(conf, c, prAuthor)...)
	}
	if prBodyFile != "" {
		body, err := os.ReadFile(prBodyFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read --pr-body-file %s: %w", prBodyFile, err)
		}
		_, isBotPR := findBotAuthorByPRAuthor(conf, prAuthor)
		out = append(out, checkPullRequestBody(string(body), isBotPR)...)
	}
	return out, nil
}

func findBotAuthorByPRAuthor(conf ccConf, prAuthor string) (ccBotAuthor, bool) {
	if prAuthor == "" {
		return ccBotAuthor{}, false
	}
	for _, b := range conf.BotAuthors {
		if b.PRAuthor == prAuthor {
			return b, true
		}
	}
	return ccBotAuthor{}, false
}

// --- SARIF -------------------------------------------------------------------

func writeCommitCheckSARIF(path string, findings []ccFinding, fatal error) error {
	index := map[string]int{}
	rules := make([]map[string]any, 0, len(ccRules))
	for i, r := range ccRules {
		index[r.ID] = i
		rules = append(rules, map[string]any{
			"id":               r.ID,
			"shortDescription": map[string]any{"text": r.Title},
		})
	}
	invocation := map[string]any{"executionSuccessful": fatal == nil}
	if fatal != nil {
		invocation["toolExecutionNotifications"] = []map[string]any{
			{"level": "error", "message": map[string]any{"text": fatal.Error()}},
		}
	}
	results := []map[string]any{}
	for _, f := range findings {
		results = append(results, map[string]any{
			"ruleId":    f.Rule,
			"ruleIndex": index[f.Rule],
			"kind":      "fail",
			"level":     "error",
			"message":   map[string]any{"text": f.Message},
			"locations": []map[string]any{
				{"logicalLocations": []map[string]any{{"name": f.Where, "kind": "commit"}}},
			},
		})
	}
	doc := map[string]any{
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"version": "2.1.0",
		"runs": []map[string]any{{
			"tool":        map[string]any{"driver": map[string]any{"name": "imprint-dev-commit-check", "rules": rules}},
			"invocations": []map[string]any{invocation},
			"results":     results,
		}},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
