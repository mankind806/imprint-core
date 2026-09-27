// commit-check enforces the identity and message rules behind N78, N79 and
// N96 for a range of new commits and, on a pull-request event, for the
// pull-request text that becomes the squash-merge commit message.
//
//	imprint-dev commit-check --root . --range base..head [--pr-author login] [--pr-title text]
//	    [--pr-body-file file] [--same-repo] [--sarif file]
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
	// Login is the GitHub login that has to have opened the pull request
	// for this identity to count as this commit's author or committer on a
	// pull_request event (prAuthor != ""). A commit's own author/committer
	// name and email are exactly as spoofable as any other git metadata —
	// anyone can `git config user.name`/`user.email` to anything, in their
	// own fork, on their own commits — so matching them alone is never
	// proof of who actually opened the pull request; only the login GitHub
	// itself reports for the event is. On a push (prAuthor == ""), Login is
	// not checked: pushing already needs write access, which is git's own
	// access control, not this file's. Left empty, the identity is never
	// trusted on a pull_request event, only on a push.
	Login string `json:"login,omitempty"`
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
	// is no pull-request context to check a PR author against. What actually
	// ties this identity string to a genuine GitHub-performed merge is the
	// repository's ruleset (PR-only, squash-merge-only), not this git
	// metadata by itself, which remains as spoofable as any other; a fork PR
	// cannot make its own commits carry this identity before such a merge
	// happens, because nothing but GitHub's own merge action produces it.
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

// identityStatus reports whether name/email matches one of ids directly
// (matched), and, when it does, whether it is trusted here (loginOK).
// loginOK is unconditional on a push (prAuthor == "") and on a same-repo
// pull request (sameRepo == true): a push already needs write access, and a
// pull request whose head branch lives in this repository, not a fork
// (github.event.pull_request.head.repo.full_name == github.repository),
// can only carry commits from someone who already has write access here too
// — Dependabot's own branches, the owner's own branches, or a commit the
// owner added straight onto someone else's such branch. Only a fork pull
// request, where anyone can set their own git config to anything on their
// own commits, falls back to checking the matched entry's own Login against
// prAuthor. See ccIdentity.Login for more on why a name/email match alone is
// never enough there.
func identityStatus(ids []ccIdentity, name, email, prAuthor string, sameRepo bool) (matched, loginOK bool) {
	for _, id := range ids {
		if id.Name == name && id.Email == email {
			if prAuthor == "" || sameRepo {
				return true, true
			}
			return true, id.Login != "" && id.Login == prAuthor
		}
	}
	return false, false
}

func (c ccConf) authorIdentityStatus(name, email, prAuthor string, sameRepo bool) (matched, loginOK bool) {
	return identityStatus(c.Authors, name, email, prAuthor, sameRepo)
}

func (c ccConf) committerIdentityStatus(name, email, prAuthor string, sameRepo bool) (matched, loginOK bool) {
	return identityStatus(c.Committers, name, email, prAuthor, sameRepo)
}

// configuredEmails collects every address .imprint/commit.conf itself
// declares (as an author, a committer, or a bot). A message may quote one
// of these — the owner's own Signed-off-by with their own noreply address,
// say — without that quoting itself becoming an address-rule finding: the
// address is already public in every commit's own metadata, and this file
// is the one canonical place declaring it. Any other address still is one.
func (c ccConf) configuredEmails() map[string]bool {
	m := map[string]bool{}
	add := func(email string) {
		if email != "" {
			m[email] = true
		}
	}
	for _, id := range c.Authors {
		add(id.Email)
	}
	for _, id := range c.Committers {
		add(id.Email)
	}
	for _, b := range c.BotAuthors {
		add(b.Email)
	}
	return m
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

// isShallowClone reports whether root is a shallow git clone. A shallow
// clone can make a range like base..head resolve against a history that
// was never actually fetched, silently checking fewer commits than were
// really pushed; the caller aborts rather than risk that fail-open.
func isShallowClone(root string) (bool, error) {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--is-shallow-repository")
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git rev-parse --is-shallow-repository: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return strings.TrimSpace(out.String()) == "true", nil
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

// ccSessionLineRE and ccSessionURLRE are matched against normalizeForSession's
// output, not the raw message: [\s_-]* between "claude" and "session", and
// the %5f alternative for the URL's underscore, are exactly the two
// variants measured getting past a plain "claude-session" / "session_"
// match while still reading as the real thing to a human.
var ccSessionLineRE = regexp.MustCompile(`(?im)^[ \t]*claude[\s_-]*session[ \t]*:`)
var ccSessionURLRE = regexp.MustCompile(`(?i)claude\.(ai|com)/code/session(_|%5f)`)

// ccAddressRE is the address shape .githooks/pre-push already checks tracked
// content and commit messages against.
var ccAddressRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

var ccAssistedByMentionRE = regexp.MustCompile(`(?im)^[ \t]*assisted-by[ \t]*:`)
var ccAssistedByLineRE = regexp.MustCompile(`(?i)^[ \t]*assisted-by[ \t]*:[ \t]*(.*)$`)

// ccRobotLineRE matches the robot emoji's own code point regardless of a
// following variation selector (U+FE0F, forcing emoji presentation) or
// anything else on the line: the substring is there either way.
var ccRobotLineRE = regexp.MustCompile(`\x{1F916}`)
var ccGeneratedWithRE = regexp.MustCompile(`(?i)generated (with|by)`)

// ccCoAuthoredAIRE names every AI tool or lab CONTRIBUTING.md's own examples
// and this repository's own history have used in a Co-Authored-By trailer;
// a human co-author's own name never matches, so a genuine review credit
// keeps needing no Assisted-by line.
var ccCoAuthoredAIRE = regexp.MustCompile(`(?im)^[ \t]*co-authored-by[ \t]*:.*\b(claude|copilot|cursor|gemini|codex|chatgpt|openai)\b`)

// ccZeroWidthRE matches characters with no visible width that would
// otherwise split up "claude-session" or a session URL for a plain string
// match while a human reading the rendered text sees it as one unbroken
// word: zero-width space/non-joiner/joiner, the word joiner, and a BOM.
var ccZeroWidthRE = regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{2060}\x{FEFF}]`)

// ccMarkdownEscapeRE matches a backslash escaping one ASCII punctuation
// character, CommonMark's own escape rule (the same one GitHub's Markdown
// renderer follows) — "claude\_session\_x" reads as "claude_session_x" once
// rendered, or once unescaped here.
var ccMarkdownEscapeRE = regexp.MustCompile(`\\([\x21-\x2F\x3A-\x40\x5B-\x60\x7B-\x7E])`)

func normalizeCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n") // a lone CR is still a line break, not a character
}

// normalizeForSession undoes the shapes measured getting a session marker
// past a plain string match while it still reads as one to a person:
// CRLF/lone-CR line endings, zero-width characters spliced into the words,
// and a Markdown backslash-escape in front of a punctuation character.
// Unicode normalization (e.g. full-width Latin letters folding to ASCII)
// would close more of these, but needs a library beyond the standard one
// this module is built against, so it stays open (see CONTRIBUTING.md).
func normalizeForSession(s string) string {
	s = normalizeCRLF(s)
	s = ccZeroWidthRE.ReplaceAllString(s, "")
	return ccMarkdownEscapeRE.ReplaceAllString(s, "$1")
}

// hasAIAssistanceMarker reports whether text already discloses AI
// assistance by one of the shapes N96 names: a robot-emoji line, a
// "Generated with" line, a Co-Authored-By trailer naming Claude or Copilot,
// or any Assisted-by mention at all. Pure human handiwork that names none
// of these owes the repository no Assisted-by line (CONTRIBUTING.md, "AI
// assistance and commit messages": "if an AI tool helped, say so").
func hasAIAssistanceMarker(text string) bool {
	return ccRobotLineRE.MatchString(text) || ccGeneratedWithRE.MatchString(text) ||
		ccCoAuthoredAIRE.MatchString(text) || ccAssistedByMentionRE.MatchString(text)
}

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
	ccRuleAssistedBy  = "commit-assisted-by"
	ccRulePRSession   = "pr-session-line"
	ccRulePRURL       = "pr-session-url"
	ccRulePRAddress   = "pr-address"
	ccRuleTrailer     = "pr-trailer"
)

// ccRules lists every rule in a fixed order, for the summary and for SARIF.
var ccRules = []struct{ ID, Title string }{
	{ccRuleAuthor, "the commit's author (name+email), and, on a pull request, the login that opened it, are in .imprint/commit.conf"},
	{ccRuleCommitter, "the commit's committer (name+email), and, on a pull request, the login that opened it, are in .imprint/commit.conf"},
	{ccRuleSessionLine, `the commit message has no "Claude-Session:" line`},
	{ccRuleSessionURL, "the commit message has no claude.ai/code/session_ URL"},
	{ccRuleAddress, "the commit message carries no email address CONTRIBUTING.md and .imprint/commit.conf do not already declare"},
	{ccRuleAssistedBy, `if the commit message discloses AI assistance at all, its last line is a well-formed, non-empty "Assisted-by:" trailer`},
	{ccRulePRSession, `the pull-request body has no "Claude-Session:" line`},
	{ccRulePRURL, "the pull-request body has no claude.ai/code/session_ URL"},
	{ccRulePRAddress, "the pull-request body carries no email address CONTRIBUTING.md and .imprint/commit.conf do not already declare"},
	{ccRuleTrailer, `if the pull-request body discloses AI assistance at all, its last line is a well-formed, non-empty "Assisted-by:" trailer, and a 🤖 line, if any, is immediately followed by a blank line and then it`},
}

// checkCommitMessage runs the session-link and address rules against msg (a
// commit message or the pull-request body) and appends findings under
// where. When exemptAddress is true, the address rule is skipped (N102: the
// message was let in only via a bot identity, so it is not ours to police).
// allowedAddrs exempts an address CONTRIBUTING.md itself already declares
// (an owner's or a bot's own noreply address, quoted rather than leaked).
func checkCommitMessage(msg, where string, sessionRule, urlRule, addressRule string, exemptAddress bool, allowedAddrs map[string]bool, out *[]ccFinding) {
	forSession := normalizeForSession(msg)
	if ccSessionLineRE.MatchString(forSession) {
		*out = append(*out, ccFinding{where, sessionRule, `contains a "Claude-Session:" line`})
	}
	if ccSessionURLRE.MatchString(forSession) {
		*out = append(*out, ccFinding{where, urlRule, "contains a claude.ai/code/session_ URL"})
	}
	if !exemptAddress {
		for _, m := range ccAddressRE.FindAllString(normalizeCRLF(msg), -1) {
			if !allowedAddrs[m] {
				*out = append(*out, ccFinding{where, addressRule, fmt.Sprintf("contains an address: %s", m)})
				break
			}
		}
	}
}

// checkOneCommit runs every commit-scoped rule against c and returns its
// findings. sameRepo is only ever true on a pull_request event, and only
// when the pull request's head branch lives in this repository rather than
// a fork; see identityStatus.
func checkOneCommit(conf ccConf, c ccCommit, prAuthor string, sameRepo bool) []ccFinding {
	var out []ccFinding
	short := c.Hash
	if len(short) > 10 {
		short = short[:10]
	}

	authorMatched, authorLoginOK := conf.authorIdentityStatus(c.AuthorName, c.AuthorEmail, prAuthor, sameRepo)
	authorOK := authorMatched && authorLoginOK
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
		msg := fmt.Sprintf("author %q <%s> is not in .imprint/commit.conf", c.AuthorName, c.AuthorEmail)
		if authorMatched && !authorLoginOK {
			msg = "external contributions need an entry in .imprint/commit.conf added by the owner"
		}
		out = append(out, ccFinding{short, ccRuleAuthor, msg})
	}

	committerMatched, committerLoginOK := conf.committerIdentityStatus(c.CommitName, c.CommitEmail, prAuthor, sameRepo)
	committerOK := committerMatched && committerLoginOK
	if !committerOK && prAuthor != "" {
		if b, ok := conf.findBotAuthor(c.CommitName, c.CommitEmail); ok && prAuthor == b.PRAuthor {
			committerOK, viaBot = true, true
		}
	}
	if !committerOK {
		msg := fmt.Sprintf("committer %q <%s> is not in .imprint/commit.conf", c.CommitName, c.CommitEmail)
		if committerMatched && !committerLoginOK {
			msg = "external contributions need an entry in .imprint/commit.conf added by the owner"
		}
		out = append(out, ccFinding{short, ccRuleCommitter, msg})
	}

	checkCommitMessage(c.Message, short, ccRuleSessionLine, ccRuleSessionURL, ccRuleAddress, viaBot, conf.configuredEmails(), &out)
	if !viaBot {
		if msg := checkAssistedByRule(c.Message, false); msg != "" {
			out = append(out, ccFinding{short, ccRuleAssistedBy, msg})
		}
	}
	return out
}

// checkPullRequestBody runs all four pull-request-text rules: the session
// line, the session URL, the address, and the Assisted-by trailer. isBotPR —
// true when the caller's --pr-author matches a configured botAuthors
// entry's PRAuthor, a condition on the pull request as a whole, not on any
// one commit's identity the way checkOneCommit's viaBot is — skips every
// one of the four (N102 plus "Bot-PRs von der PR-Text-Regel ausnehmen": the
// body is generated by the bot and not authored by a repository
// contributor at all, and may embed a third party's changelog we have no
// way to edit).
func checkPullRequestBody(conf ccConf, text string, isBotPR bool) []ccFinding {
	if isBotPR {
		return nil
	}
	var out []ccFinding
	checkCommitMessage(text, "PR text", ccRulePRSession, ccRulePRURL, ccRulePRAddress, false, conf.configuredEmails(), &out)
	if msg := checkAssistedByRule(text, true); msg != "" {
		out = append(out, ccFinding{"PR text", ccRuleTrailer, msg})
	}
	return out
}

// checkAssistedByRule implements N96's reading of "AI assistance and
// commit messages": pure human handiwork that names no AI feature owes no
// Assisted-by line, but the moment text discloses one at all — a robot-emoji
// line, "Generated with", a Co-Authored-By naming Claude or Copilot, or any
// Assisted-by mention — its own last non-blank line has to be a non-empty
// "Assisted-by: <value>" that git itself reads as the last trailer of a
// well-formed block (`git interpret-trailers`, the same parser
// CONTRIBUTING.md's own `%(trailers:key=Assisted-by)` example reads with,
// rather than a hand-rolled one, so the two never disagree about what
// counts as a trailer). requireRobotSequence additionally demands, only for
// the pull-request body: if a 🤖 line appears anywhere, the text's very
// last three lines are that 🤖 line, one blank line, then Assisted-by —
// CONTRIBUTING.md's own required shape for the squash-merge commit message.
// Returns "" for no finding.
func checkAssistedByRule(text string, requireRobotSequence bool) string {
	text = normalizeCRLF(text)
	if !hasAIAssistanceMarker(text) {
		return ""
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	last := lines[len(lines)-1]
	m := ccAssistedByLineRE.FindStringSubmatch(last)
	if m == nil {
		return `discloses AI assistance (a 🤖 line, "Generated with", an AI Co-Authored-By trailer, or an Assisted-by mention) ` +
			`but its last non-blank line is not "Assisted-by: <value>"`
	}
	// A value made only of zero-width characters (NBSP is already
	// whitespace to TrimSpace) reads as empty to a person but is not empty
	// as bytes, so it is stripped before judging it empty.
	if strings.TrimSpace(ccZeroWidthRE.ReplaceAllString(m[1], "")) == "" {
		return `has an empty "Assisted-by:" line; fill in the tool or model name, or remove the line`
	}

	cmd := exec.Command("git", "interpret-trailers", "--parse", "--only-trailers")
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.Output()
	trimmed := strings.TrimSpace(string(out))
	if err != nil || trimmed == "" {
		return `has an "Assisted-by:" line that git does not read as a trailer at all ` +
			`(a blank line has to separate it from anything above)`
	}
	trailerLines := strings.Split(trimmed, "\n")
	if k, _, found := strings.Cut(trailerLines[len(trailerLines)-1], ":"); !found || !strings.EqualFold(strings.TrimSpace(k), "assisted-by") {
		return `has "Assisted-by:" as its last line, but git reads a later trailer as the last one ` +
			`(nothing, not even another trailer, may follow it)`
	}

	if requireRobotSequence && ccRobotLineRE.MatchString(text) {
		if len(lines) < 3 || !ccRobotLineRE.MatchString(lines[len(lines)-3]) || strings.TrimSpace(lines[len(lines)-2]) != "" {
			return `contains a 🤖 line, but the text's last three lines are not exactly that line, ` +
				`one blank line, then "Assisted-by:"`
		}
	}
	return ""
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
	conf := fs.String("conf", defaultCommitConfPath, "path to commit.conf: relative to root, or absolute to read it from anywhere else (e.g. the base commit, not the pull request's own tree)")
	rangeSpec := fs.String("range", "", "commit range to check, e.g. base..head or onlyCommit^! (required)")
	prAuthor := fs.String("pr-author", "", "login of the pull-request author; leave empty on a push")
	prBodyFile := fs.String("pr-body-file", "", "file holding the pull-request body to check; omitted on a push")
	prTitle := fs.String("pr-title", "", "the pull request's own title, checked together with --pr-body-file as title+blank+body, the shape a squash merge turns them into")
	sameRepo := fs.Bool("same-repo", false, "the pull request's head branch lives in this repository, not a fork "+
		"(github.event.pull_request.head.repo.full_name == github.repository); ignored on a push")
	sarifPath := fs.String("sarif", "", "also write SARIF 2.1.0 to this file")
	if done, code := parseFlags(fs, args, stderr); done {
		return code
	}
	if *rangeSpec == "" {
		fmt.Fprintln(stderr, "imprint-dev commit-check: --range is required (e.g. base..head or onlyCommit^!)")
		return exitError
	}

	findings, err := checkCommitCheckRange(*root, *conf, *rangeSpec, *prAuthor, *prBodyFile, *prTitle, *sameRepo)
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
// pull-request text (its title, then a blank line, then its body — the
// shape a squash merge turns them into).
func checkCommitCheckRange(root, confRelPath, rangeSpec, prAuthor, prBodyFile, prTitle string, sameRepo bool) ([]ccFinding, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(absRoot); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("root %q is not a readable directory", root)
	}
	if shallow, err := isShallowClone(absRoot); err != nil {
		return nil, fmt.Errorf("could not tell whether %s is a shallow clone: %w", root, err)
	} else if shallow {
		return nil, fmt.Errorf("%s is a shallow git clone; refusing to run rather than risk a range silently "+
			"resolving against history that was never fetched (checkout needs fetch-depth: 0)", root)
	}
	confPath := confRelPath
	if !filepath.IsAbs(confPath) {
		confPath = filepath.Join(absRoot, filepath.FromSlash(confRelPath))
	}
	conf, err := loadCommitConf(confPath)
	if err != nil {
		return nil, err
	}
	commits, err := commitsInRange(absRoot, rangeSpec)
	if err != nil {
		return nil, err
	}
	var out []ccFinding
	for _, c := range commits {
		out = append(out, checkOneCommit(conf, c, prAuthor, sameRepo)...)
	}
	if prBodyFile != "" {
		body, err := os.ReadFile(prBodyFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read --pr-body-file %s: %w", prBodyFile, err)
		}
		text := string(body)
		if prTitle != "" {
			text = prTitle + "\n\n" + text
		}
		_, isBotPR := findBotAuthorByPRAuthor(conf, prAuthor)
		out = append(out, checkPullRequestBody(conf, text, isBotPR)...)
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
