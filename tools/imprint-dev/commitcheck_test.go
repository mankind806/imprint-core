package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addr builds an email address at runtime from its local part and domain.
// Every test address in this file and in commitcheck_integration_test.go
// goes through it (or through the emailUser/emailDomain split in ccTestConf
// below), so neither .go source file itself carries an address-shaped
// literal: .githooks/pre-push's own address check scans every pushed
// commit's whole tree, not just what changed, and would otherwise flag both
// files on every future push, forever, once this lands on main.
func addr(user, domain string) string { return user + "@" + domain }

// Shared test addresses, also used by commitcheck_integration_test.go.
var (
	ccTestAuthorEmail     = addr("author", "example.invalid")
	ccTestSomeoneEmail    = addr("someone", "example.invalid")
	ccTestBotEmail        = addr("bot", "example.invalid")
	ccTestWebFlowEmail    = addr("noreply", "github.com")
	ccTestMaintainerEmail = addr("maintainer", "example.invalid")
)

// ccTestOwnerLogin is the GitHub login ccTestConf binds "Test Author" to:
// on a pull_request event, only a commit whose author or committer is
// "Test Author" <ccTestAuthorEmail> *and* whose --pr-author is this login
// passes directly; matching the name and email alone is exactly what an
// attacker forking the repository can fake in their own commits.
const ccTestOwnerLogin = "test-owner"

// ccTestConf is a fictitious config: never this repository's own identities,
// so these tests never depend on nor leak them. Same emailUser/emailDomain
// split as .imprint/commit.conf, for the same reason.
const ccTestConf = `{
  "authors": [
    {"name": "Test Author", "login": "test-owner", "emailUser": "author", "emailDomain": "example.invalid"}
  ],
  "committers": [
    {"name": "Test Author", "login": "test-owner", "emailUser": "author", "emailDomain": "example.invalid"},
    {"name": "GitHub", "emailUser": "noreply", "emailDomain": "github.com", "mergeCommitter": true}
  ],
  "botAuthors": [
    {"name": "dependabot[bot]", "emailUser": "bot", "emailDomain": "example.invalid", "prAuthor": "dependabot[bot]"}
  ]
}
`

func ccTestConfParsed(t *testing.T) ccConf {
	t.Helper()
	var c ccConf
	if err := json.Unmarshal([]byte(ccTestConf), &c); err != nil {
		t.Fatal(err)
	}
	c.resolveEmails()
	return c
}

func ccHasRule(findings []ccFinding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func ccFindRule(findings []ccFinding, rule string) (ccFinding, bool) {
	for _, f := range findings {
		if f.Rule == rule {
			return f, true
		}
	}
	return ccFinding{}, false
}

func TestCommitCheckIdentity(t *testing.T) {
	conf := ccTestConfParsed(t)
	author := ccTestAuthorEmail
	someone := ccTestSomeoneEmail
	webFlow := ccTestWebFlowEmail
	bot := ccTestBotEmail
	cases := []struct {
		name                       string
		authorName, authorEmail    string
		commName, commEmail        string
		prAuthor                   string
		sameRepo                   bool
		wantAuthorOK, wantCommitOK bool
	}{
		{
			name:       "allowed identity, no PR context (push)",
			authorName: "Test Author", authorEmail: author,
			commName: "Test Author", commEmail: author,
			wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "foreign author",
			authorName: "Someone Else", authorEmail: someone,
			commName: "Test Author", commEmail: author,
			wantAuthorOK: false, wantCommitOK: true,
		},
		{
			name:       "foreign committer",
			authorName: "Test Author", authorEmail: author,
			commName: "Someone Else", commEmail: someone,
			wantAuthorOK: true, wantCommitOK: false,
		},
		{
			name:       "web-flow committer (squash merge, push)",
			authorName: "Test Author", authorEmail: author,
			commName: "GitHub", commEmail: webFlow,
			wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "dependabot without pr-author and without web-flow committer",
			authorName: "dependabot[bot]", authorEmail: bot,
			commName: "dependabot[bot]", commEmail: bot,
			wantAuthorOK: false, wantCommitOK: false,
		},
		{
			name:       "dependabot on push, squash-merged by web-flow (no PR context)",
			authorName: "dependabot[bot]", authorEmail: bot,
			commName: "GitHub", commEmail: webFlow,
			wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "dependabot on its own, still-open PR (author == committer == bot)",
			authorName: "dependabot[bot]", authorEmail: bot,
			commName: "dependabot[bot]", commEmail: bot,
			prAuthor: "dependabot[bot]", wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "dependabot with the wrong pr-author",
			authorName: "dependabot[bot]", authorEmail: bot,
			commName: "dependabot[bot]", commEmail: bot,
			prAuthor: "someone-else", wantAuthorOK: false, wantCommitOK: false,
		},
		{
			// The vulnerability item 2 closes: a fork can set its own git
			// config to any name and email it likes and open a pull request
			// from its own, unrelated GitHub account. Name+email matching
			// the owner is never proof that the owner opened the pull
			// request; only --pr-author, which the workflow fills in from
			// the event GitHub itself sent, is.
			name:       "a pull request opened by someone else, with the owner's identity spoofed onto the commit",
			authorName: "Test Author", authorEmail: author,
			commName: "Test Author", commEmail: author,
			prAuthor: "evil", wantAuthorOK: false, wantCommitOK: false,
		},
		{
			name:       "the same identity, but the pull request really was opened by its bound login",
			authorName: "Test Author", authorEmail: author,
			commName: "Test Author", commEmail: author,
			prAuthor: ccTestOwnerLogin, wantAuthorOK: true, wantCommitOK: true,
		},
		{
			// N1: a real Dependabot pull request's own commits, measured
			// against cli/cli #14487 and actions/checkout #2578 (gh api
			// .../pulls/N/commits): author dependabot[bot], committer
			// already the web-flow identity, even before any merge.
			// Dependabot's branches live in this repository, not a fork.
			name:       "a real dependabot pull request: bot author, web-flow committer already, same-repo",
			authorName: "dependabot[bot]", authorEmail: bot,
			commName: "GitHub", commEmail: webFlow,
			prAuthor: "dependabot[bot]", sameRepo: true, wantAuthorOK: true, wantCommitOK: true,
		},
		{
			// N2: a web-UI edit, an "Update branch" merge, or an accepted
			// Copilot suggestion on the owner's own pull request all commit
			// as the owner with the web-flow identity as committer. The
			// register allows web-flow at a merge; this is that same
			// identity, same-repo, before the pull request has merged.
			name:       "a web-flow committer on the owner's own, same-repo pull request",
			authorName: "Test Author", authorEmail: author,
			commName: "GitHub", commEmail: webFlow,
			prAuthor: ccTestOwnerLogin, sameRepo: true, wantAuthorOK: true, wantCommitOK: true,
		},
		{
			// N3: the owner pushes a fix commit straight onto someone
			// else's (here Dependabot's) still-open, same-repo pull
			// request; --pr-author still names the pull request's own
			// opener, not this commit's author.
			name:       "the owner's own fix commit on a dependabot pull request",
			authorName: "Test Author", authorEmail: author,
			commName: "Test Author", commEmail: author,
			prAuthor: "dependabot[bot]", sameRepo: true, wantAuthorOK: true, wantCommitOK: true,
		},
		{
			// The same spoofed-identity attempt as above, but now flagged
			// as coming from a fork (sameRepo stays false): a fork pull
			// request still needs the login binding regardless.
			name:       "a fork pull request still cannot pass by spoofing the owner's identity",
			authorName: "Test Author", authorEmail: author,
			commName: "Test Author", commEmail: author,
			prAuthor: "evil", sameRepo: false, wantAuthorOK: false, wantCommitOK: false,
		},
		{
			// Same-repo trust covers only identities already in
			// .imprint/commit.conf; a genuinely unrecognized identity is
			// still refused even from a same-repo branch (a collaborator
			// nobody has added, say).
			name:       "a same-repo commit from an unrecognized identity is still refused",
			authorName: "Someone Else", authorEmail: someone,
			commName: "Someone Else", commEmail: someone,
			prAuthor: "someone-else", sameRepo: true, wantAuthorOK: false, wantCommitOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ccCommit{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				AuthorName: tc.authorName, AuthorEmail: tc.authorEmail,
				CommitName: tc.commName, CommitEmail: tc.commEmail, Message: "Text\n"}
			found := checkOneCommit(conf, c, tc.prAuthor, tc.sameRepo)
			if got := ccHasRule(found, ccRuleAuthor); got == tc.wantAuthorOK {
				t.Errorf("author finding %v, want author-OK=%v", got, tc.wantAuthorOK)
			}
			if got := ccHasRule(found, ccRuleCommitter); got == tc.wantCommitOK {
				t.Errorf("committer finding %v, want committer-OK=%v", got, tc.wantCommitOK)
			}
		})
	}

	t.Run("the spoofed-identity finding names the real fix, not a generic mismatch", func(t *testing.T) {
		c := ccCommit{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AuthorName: "Test Author", AuthorEmail: author,
			CommitName: "Test Author", CommitEmail: author, Message: "Text\n"}
		found := checkOneCommit(conf, c, "evil", false)
		f, ok := ccFindRule(found, ccRuleAuthor)
		if !ok {
			t.Fatalf("findings %v, want %s", found, ccRuleAuthor)
		}
		if !strings.Contains(f.Message, "external contributions need an entry in .imprint/commit.conf added by the owner") {
			t.Errorf("message %q does not name the fix", f.Message)
		}
	})
}

func TestCommitCheckSessionAndAddressPatterns(t *testing.T) {
	conf := ccTestConfParsed(t)
	author := ccTestAuthorEmail
	allowedCommit := func(message string) ccCommit {
		return ccCommit{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AuthorName: "Test Author", AuthorEmail: author,
			CommitName: "Test Author", CommitEmail: author, Message: message}
	}
	cases := []struct {
		name    string
		message string
		rule    string
	}{
		{"session line", "Text\n\nClaude-Session: https://claude.ai/code/session_abc123\n", ccRuleSessionLine},
		{"session line, mixed case", "Text\n\nCLAUDE-SESSION:  something\n", ccRuleSessionLine},
		{"session URL without a trailer", "Text with a link https://claude.ai/code/session_abc123 in prose.\n", ccRuleSessionURL},
		{"session line with underscore/hyphen between the words", "Text\n\nclaude_session: x\n", ccRuleSessionLine},
		{"session line with a hyphen and no space before the colon", "Text\n\nclaude-session:x\n", ccRuleSessionLine},
		{"session URL, percent-encoded underscore", "Text: https://claude.ai/code/session%5Fabc\n", ccRuleSessionURL},
		{"session URL under claude.com", "Text: https://claude.com/code/session_abc\n", ccRuleSessionURL},
		{"session line split by a zero-width space", "Text\n\nClaude​-Session: x\n", ccRuleSessionLine},
		{"session URL with a Markdown-escaped underscore", `Text: https://claude.ai/code/session\_abc` + "\n", ccRuleSessionURL},
		{"session line after a lone CR (old Mac line ending)", "Text\r\rClaude-Session: x\r", ccRuleSessionLine},
		{"an address in the message", "Text.\n\nSigned-off-by: Someone <" + ccTestSomeoneEmail + ">\n", ccRuleAddress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := checkOneCommit(conf, allowedCommit(tc.message), "", false)
			if !ccHasRule(found, tc.rule) {
				t.Errorf("want rule %s, got %v", tc.rule, found)
			}
		})
	}

	t.Run("a clean message has no finding at all", func(t *testing.T) {
		found := checkOneCommit(conf, allowedCommit("A plain, unassisted commit message.\n"), "", false)
		if len(found) != 0 {
			t.Errorf("findings %v, want none", found)
		}
	})

	t.Run("a bot's own commit message keeps its own address (N102)", func(t *testing.T) {
		bot := ccCommit{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			AuthorName: "dependabot[bot]", AuthorEmail: ccTestBotEmail,
			CommitName: "GitHub", CommitEmail: ccTestWebFlowEmail,
			Message: "Bump x from 1 to 2.\n\nSigned-off-by: dependabot[bot] <" + addr("support", "example.invalid") + ">\n"}
		found := checkOneCommit(conf, bot, "", false)
		if ccHasRule(found, ccRuleAddress) {
			t.Errorf("bot commit's own address should be exempt, got %v", found)
		}
	})

	t.Run("a human commit message is never exempt from the address rule for an address nobody declared", func(t *testing.T) {
		found := checkOneCommit(conf, allowedCommit("Text.\n\nContact "+ccTestSomeoneEmail+".\n"), "", false)
		if !ccHasRule(found, ccRuleAddress) {
			t.Errorf("want %s, got %v", ccRuleAddress, found)
		}
	})

	t.Run("an address .imprint/commit.conf itself already declares is not a finding", func(t *testing.T) {
		// Item 5: the owner's own noreply address, quoted in their own
		// Signed-off-by line, adds nothing .imprint/commit.conf and every
		// commit's own metadata do not already carry.
		found := checkOneCommit(conf, allowedCommit("Text.\n\nSigned-off-by: Test Author <"+author+">\n"), "", false)
		if ccHasRule(found, ccRuleAddress) {
			t.Errorf("a configured address should be exempt, got %v", found)
		}
	})

	t.Run("a *different* address alongside a configured one is still a finding", func(t *testing.T) {
		msg := "Text.\n\nSigned-off-by: Test Author <" + author + ">\nCc: " + ccTestSomeoneEmail + "\n"
		found := checkOneCommit(conf, allowedCommit(msg), "", false)
		if !ccHasRule(found, ccRuleAddress) {
			t.Errorf("want %s, got %v", ccRuleAddress, found)
		}
	})
}

// TestCommitCheckAssistedBy covers item 1's reading of N96: pure handiwork
// needs no Assisted-by line, but any of the four AI-assistance markers
// forces the rule.
func TestCommitCheckAssistedBy(t *testing.T) {
	conf := ccTestConfParsed(t)
	author := ccTestAuthorEmail
	commitWith := func(message string) ccCommit {
		return ccCommit{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AuthorName: "Test Author", AuthorEmail: author,
			CommitName: "Test Author", CommitEmail: author, Message: message}
	}

	t.Run("pure handiwork, no AI marker at all, is not a finding", func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the off-by-one in the range check.\n"), "", false)
		if ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("plain handiwork should need no Assisted-by line, got %v", found)
		}
	})

	t.Run("a well-formed Assisted-by trailer is not a finding", func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the check.\n\nAssisted-by: some tool\n"), "", false)
		if ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("findings %v, want none", found)
		}
	})

	t.Run("Co-Authored-By naming Claude, with no Assisted-by line, is a finding", func(t *testing.T) {
		msg := "Fix the check.\n\nCo-Authored-By: Claude <" + addr("noreply", "example.invalid") + ">\n"
		found := checkOneCommit(conf, commitWith(msg), "", false)
		if !ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("want %s, got %v", ccRuleAssistedBy, found)
		}
	})

	t.Run("Co-Authored-By naming Copilot, with no Assisted-by line, is a finding", func(t *testing.T) {
		msg := "Fix the check.\n\nCo-Authored-By: Copilot <" + addr("noreply", "example.invalid") + ">\n"
		found := checkOneCommit(conf, commitWith(msg), "", false)
		if !ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("want %s, got %v", ccRuleAssistedBy, found)
		}
	})

	t.Run("Co-Authored-By naming a human, not an AI tool, needs no Assisted-by line", func(t *testing.T) {
		msg := "Fix the check.\n\nCo-Authored-By: Pat Reviewer <" + addr("pat", "example.invalid") + ">\n"
		found := checkOneCommit(conf, commitWith(msg), "", false)
		if ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("findings %v, want none (no AI tool named)", found)
		}
	})

	t.Run(`"Generated with Claude", with no Assisted-by line, is a finding`, func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the check.\n\nGenerated with Claude.\n"), "", false)
		if !ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("want %s, got %v", ccRuleAssistedBy, found)
		}
	})

	// N5: "Generated with/by/using" on its own also matches a code
	// generator's own unrelated boilerplate; naming no AI anywhere in the
	// same sentence, it is not AI disclosure and needs no Assisted-by line.
	for _, msg := range []string{
		"Code generated by protoc-gen-go. DO NOT EDIT.\n",
		`Code generated by "stringer -type=Pill"; DO NOT EDIT.` + "\n",
		"Code generated by MockGen. DO NOT EDIT.\n",
	} {
		t.Run("N5: "+strings.SplitN(msg, "\n", 2)[0]+" names no AI and needs no Assisted-by line", func(t *testing.T) {
			found := checkOneCommit(conf, commitWith(msg), "", false)
			if ccHasRule(found, ccRuleAssistedBy) {
				t.Errorf("findings %v, want none (a code generator's own boilerplate names no AI)", found)
			}
		})
	}

	t.Run("N5: the canonical \"Generated with [Claude Code]\" line, with no Assisted-by line, is a finding", func(t *testing.T) {
		msg := "Fix the check.\n\nGenerated with [Claude Code](https://claude.com/claude-code)\n"
		found := checkOneCommit(conf, commitWith(msg), "", false)
		if !ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("want %s, got %v", ccRuleAssistedBy, found)
		}
	})

	t.Run(`N5: "Generated using Claude", with no Assisted-by line, is a finding`, func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the check.\n\nGenerated using Claude.\n"), "", false)
		if !ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("want %s, got %v", ccRuleAssistedBy, found)
		}
	})

	t.Run("an empty Assisted-by value is a finding, worded as fill-in-or-remove", func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the check.\n\nAssisted-by:\n"), "", false)
		f, ok := ccFindRule(found, ccRuleAssistedBy)
		if !ok {
			t.Fatalf("findings %v, want %s", found, ccRuleAssistedBy)
		}
		if !strings.Contains(f.Message, "fill in") || !strings.Contains(f.Message, "remove") {
			t.Errorf("message %q does not read as fill-in-or-remove", f.Message)
		}
	})

	t.Run("a bot's own commit needs no Assisted-by line either", func(t *testing.T) {
		bot := ccCommit{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			AuthorName: "dependabot[bot]", AuthorEmail: ccTestBotEmail,
			CommitName: "GitHub", CommitEmail: ccTestWebFlowEmail,
			Message: "Bump x.\n\nGenerated with dependabot-core.\n"}
		found := checkOneCommit(conf, bot, "", false)
		if ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("findings %v, want none (bot commit is exempt)", found)
		}
	})

	// N6: a value made only of zero-width characters (and/or NBSP, already
	// whitespace) reads as empty to a person but is not empty as bytes.
	t.Run("a zero-width-only Assisted-by value is a finding, worded as fill-in-or-remove", func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the check.\n\nAssisted-by: ​ ​\n"), "", false)
		f, ok := ccFindRule(found, ccRuleAssistedBy)
		if !ok {
			t.Fatalf("findings %v, want %s", found, ccRuleAssistedBy)
		}
		if !strings.Contains(f.Message, "fill in") || !strings.Contains(f.Message, "remove") {
			t.Errorf("message %q does not read as fill-in-or-remove", f.Message)
		}
	})

	// N6: the robot emoji followed by a variation selector (U+FE0F, forcing
	// emoji presentation — common when copied out of some renderers) is
	// still the same 🤖 line.
	t.Run("a 🤖 line with a trailing variation selector still counts as the marker", func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the check.\n\n\U0001F916️ Generated with a tool\n\nAssisted-by: some tool\n"), "", false)
		if ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("findings %v, want none (well-formed trailer after the marker)", found)
		}
	})

	t.Run(`"Generated by Claude", with no Assisted-by line, is a finding`, func(t *testing.T) {
		found := checkOneCommit(conf, commitWith("Fix the check.\n\nGenerated by Claude.\n"), "", false)
		if !ccHasRule(found, ccRuleAssistedBy) {
			t.Errorf("want %s, got %v", ccRuleAssistedBy, found)
		}
	})

	for _, tool := range []string{"Cursor", "Gemini", "Codex", "ChatGPT", "OpenAI"} {
		t.Run("Co-Authored-By naming "+tool+", with no Assisted-by line, is a finding", func(t *testing.T) {
			msg := "Fix the check.\n\nCo-Authored-By: " + tool + " <" + addr("noreply", "example.invalid") + ">\n"
			found := checkOneCommit(conf, commitWith(msg), "", false)
			if !ccHasRule(found, ccRuleAssistedBy) {
				t.Errorf("want %s, got %v", ccRuleAssistedBy, found)
			}
		})
	}
}

func TestCommitCheckPullRequestBody(t *testing.T) {
	conf := ccTestConfParsed(t)

	t.Run("a clean body with a well-formed trailer passes", func(t *testing.T) {
		body := "Summary of the change.\n\nMore detail.\n\nAssisted-by: some tool\n"
		found := checkPullRequestBody(conf, body, false)
		if len(found) != 0 {
			t.Errorf("findings %v, want none", found)
		}
	})

	t.Run("pure handiwork, no AI marker, needs no trailer at all", func(t *testing.T) {
		found := checkPullRequestBody(conf, "Just a plain body, written by hand.\n", false)
		if ccHasRule(found, ccRuleTrailer) {
			t.Errorf("no marker should not trip the trailer rule, got %v", found)
		}
	})

	t.Run("a session line in the body is a finding", func(t *testing.T) {
		found := checkPullRequestBody(conf, "Text\n\nClaude-Session: https://claude.ai/code/session_x\n", false)
		if !ccHasRule(found, ccRulePRSession) {
			t.Errorf("want %s, got %v", ccRulePRSession, found)
		}
	})

	t.Run("an address in the body is a finding", func(t *testing.T) {
		body := "Text.\n\nContact " + ccTestSomeoneEmail + ".\n"
		found := checkPullRequestBody(conf, body, false)
		if !ccHasRule(found, ccRulePRAddress) {
			t.Errorf("want %s, got %v", ccRulePRAddress, found)
		}
	})

	t.Run("the owner's own configured address in the body is not a finding", func(t *testing.T) {
		body := "Text.\n\nSigned-off-by: Test Author <" + ccTestAuthorEmail + ">\n\nAssisted-by: some tool\n"
		found := checkPullRequestBody(conf, body, false)
		if ccHasRule(found, ccRulePRAddress) {
			t.Errorf("findings %v, want no address finding", found)
		}
	})

	t.Run("Co-Authored-By naming Claude, with no Assisted-by line, is a finding", func(t *testing.T) {
		body := "Summary.\n\nCo-Authored-By: Claude <" + addr("noreply", "example.invalid") + ">\n"
		found := checkPullRequestBody(conf, body, false)
		if !ccHasRule(found, ccRuleTrailer) {
			t.Errorf("want %s, got %v", ccRuleTrailer, found)
		}
	})

	t.Run("Assisted-by present but not the last trailer line is a finding", func(t *testing.T) {
		// The #15 shape from CONTRIBUTING.md: a line touching Assisted-by
		// with no blank line before it breaks git's own trailer block.
		body := "Summary.\n\n\U0001F916 Generated with a tool\nAssisted-by: some tool\n"
		found := checkPullRequestBody(conf, body, false)
		if !ccHasRule(found, ccRuleTrailer) {
			t.Errorf("want %s, got %v", ccRuleTrailer, found)
		}
	})

	t.Run("Assisted-by immediately followed by another trailer is a finding", func(t *testing.T) {
		// git still parses both as trailers with no blank line between them,
		// but Assisted-by is then the second-to-last one, not the last.
		body := "Summary.\n\nAssisted-by: some tool\nReviewed-by: someone\n"
		found := checkPullRequestBody(conf, body, false)
		if !ccHasRule(found, ccRuleTrailer) {
			t.Errorf("want %s, got %v", ccRuleTrailer, found)
		}
	})

	t.Run("Assisted-by followed by a blank line and more prose is a finding", func(t *testing.T) {
		body := "Summary.\n\nAssisted-by: some tool\n\nOne more line after it.\n"
		found := checkPullRequestBody(conf, body, false)
		if !ccHasRule(found, ccRuleTrailer) {
			t.Errorf("want %s, got %v", ccRuleTrailer, found)
		}
	})

	t.Run("an empty Assisted-by value in the body is a finding", func(t *testing.T) {
		found := checkPullRequestBody(conf, "Summary.\n\nAssisted-by: \n", false)
		f, ok := ccFindRule(found, ccRuleTrailer)
		if !ok {
			t.Fatalf("findings %v, want %s", found, ccRuleTrailer)
		}
		if !strings.Contains(f.Message, "fill in") {
			t.Errorf("message %q does not read as fill-in-or-remove", f.Message)
		}
	})

	t.Run("a generator line separated by a blank line still parses", func(t *testing.T) {
		body := "Summary.\n\n\U0001F916 Generated with a tool\n\nAssisted-by: some tool\n"
		found := checkPullRequestBody(conf, body, false)
		if ccHasRule(found, ccRuleTrailer) {
			t.Errorf("a blank line before Assisted-by should parse, got %v", found)
		}
	})

	t.Run("a 🤖 line present but not immediately before Assisted-by is a finding", func(t *testing.T) {
		// The 🤖 line is there, and the trailer itself is well-formed, but
		// something sits between them: CONTRIBUTING.md's required shape for
		// the pull-request body specifically is the 🤖 line, one blank
		// line, then Assisted-by, with nothing else in between.
		body := "Summary.\n\n\U0001F916 Generated with a tool\n\nOne more paragraph.\n\nAssisted-by: some tool\n"
		found := checkPullRequestBody(conf, body, false)
		if !ccHasRule(found, ccRuleTrailer) {
			t.Errorf("want %s, got %v", ccRuleTrailer, found)
		}
	})

	t.Run("a recognised bot PR body is exempt from every PR-text rule, verified against real findings", func(t *testing.T) {
		// Item 8: prove the exemption actually suppresses findings, rather
		// than merely exercising an early return that says nothing about
		// the checks it bypasses.
		body := "Bumps x.\n\nContact " + ccTestSomeoneEmail + ".\n\nClaude-Session: https://claude.ai/code/session_x\n"
		withoutExemption := checkPullRequestBody(conf, body, false)
		if len(withoutExemption) == 0 {
			t.Fatalf("test body produces no findings even without the exemption; it proves nothing")
		}
		if !ccHasRule(withoutExemption, ccRulePRAddress) || !ccHasRule(withoutExemption, ccRulePRSession) {
			t.Fatalf("findings %v, want both %s and %s", withoutExemption, ccRulePRAddress, ccRulePRSession)
		}
		withExemption := checkPullRequestBody(conf, body, true)
		if len(withExemption) != 0 {
			t.Errorf("findings %v, want none (bot PR is exempt)", withExemption)
		}
	})
}

func TestCommitCheckSummary(t *testing.T) {
	if s := ccSummary(nil); !strings.Contains(s, "0 finding(s)") {
		t.Errorf("ccSummary(nil) = %q", s)
	}
	f := []ccFinding{{Where: "a", Rule: ccRuleAuthor, Message: "x"}, {Where: "b", Rule: ccRuleAuthor, Message: "y"}}
	if s := ccSummary(f); !strings.Contains(s, "2 finding(s)") || !strings.Contains(s, "commit-author 2") {
		t.Errorf("ccSummary = %q", s)
	}
}

// TestCommitCheckLoadsTheRealConf is item 8: one test loads this
// repository's own .imprint/commit.conf, not only the fictitious one, so a
// schema change here is caught even if nobody remembers to update the test
// fixture in step.
func TestCommitCheckLoadsTheRealConf(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(wd)) // tools/imprint-dev -> tools -> repo root
	conf, err := loadCommitConf(filepath.Join(root, defaultCommitConfPath))
	if err != nil {
		t.Fatalf("loading the real %s: %v", defaultCommitConfPath, err)
	}
	if len(conf.Authors) == 0 || conf.Authors[0].Email == "" || !strings.Contains(conf.Authors[0].Email, "@") {
		t.Fatalf("real conf's first author did not resolve an email: %+v", conf.Authors)
	}
	if len(conf.BotAuthors) == 0 {
		t.Fatalf("real conf has no bot authors; Dependabot is expected to be listed")
	}
}
