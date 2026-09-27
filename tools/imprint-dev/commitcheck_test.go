package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// ccTestConf is a fictitious config: never this repository's own identities,
// so these tests never depend on nor leak them.
const ccTestConf = `{
  "authors": [
    {"name": "Test Author", "email": "author@example.invalid"}
  ],
  "committers": [
    {"name": "Test Author", "email": "author@example.invalid"},
    {"name": "GitHub", "email": "noreply@github.com", "mergeCommitter": true}
  ],
  "botAuthors": [
    {"name": "dependabot[bot]", "email": "bot@example.invalid", "prAuthor": "dependabot[bot]"}
  ]
}
`

func ccTestConfParsed(t *testing.T) ccConf {
	t.Helper()
	var c ccConf
	if err := json.Unmarshal([]byte(ccTestConf), &c); err != nil {
		t.Fatal(err)
	}
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

func TestCommitCheckIdentity(t *testing.T) {
	conf := ccTestConfParsed(t)
	cases := []struct {
		name                       string
		authorName, authorEmail    string
		commName, commEmail        string
		prAuthor                   string
		wantAuthorOK, wantCommitOK bool
	}{
		{
			name:       "allowed identity, no PR context",
			authorName: "Test Author", authorEmail: "author@example.invalid",
			commName: "Test Author", commEmail: "author@example.invalid",
			wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "foreign author",
			authorName: "Someone Else", authorEmail: "someone@example.invalid",
			commName: "Test Author", commEmail: "author@example.invalid",
			wantAuthorOK: false, wantCommitOK: true,
		},
		{
			name:       "foreign committer",
			authorName: "Test Author", authorEmail: "author@example.invalid",
			commName: "Someone Else", commEmail: "someone@example.invalid",
			wantAuthorOK: true, wantCommitOK: false,
		},
		{
			name:       "web-flow committer (squash merge)",
			authorName: "Test Author", authorEmail: "author@example.invalid",
			commName: "GitHub", commEmail: "noreply@github.com",
			wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "dependabot without pr-author and without web-flow committer",
			authorName: "dependabot[bot]", authorEmail: "bot@example.invalid",
			commName: "dependabot[bot]", commEmail: "bot@example.invalid",
			wantAuthorOK: false, wantCommitOK: false,
		},
		{
			name:       "dependabot on push, squash-merged by web-flow (no PR context)",
			authorName: "dependabot[bot]", authorEmail: "bot@example.invalid",
			commName: "GitHub", commEmail: "noreply@github.com",
			wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "dependabot on its own, still-open PR (author == committer == bot)",
			authorName: "dependabot[bot]", authorEmail: "bot@example.invalid",
			commName: "dependabot[bot]", commEmail: "bot@example.invalid",
			prAuthor: "dependabot[bot]", wantAuthorOK: true, wantCommitOK: true,
		},
		{
			name:       "dependabot with the wrong pr-author",
			authorName: "dependabot[bot]", authorEmail: "bot@example.invalid",
			commName: "dependabot[bot]", commEmail: "bot@example.invalid",
			prAuthor: "someone-else", wantAuthorOK: false, wantCommitOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ccCommit{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				AuthorName: tc.authorName, AuthorEmail: tc.authorEmail,
				CommitName: tc.commName, CommitEmail: tc.commEmail, Message: "Text\n"}
			found := checkOneCommit(conf, c, tc.prAuthor)
			if got := ccHasRule(found, ccRuleAuthor); got == tc.wantAuthorOK {
				t.Errorf("author finding %v, want author-OK=%v", got, tc.wantAuthorOK)
			}
			if got := ccHasRule(found, ccRuleCommitter); got == tc.wantCommitOK {
				t.Errorf("committer finding %v, want committer-OK=%v", got, tc.wantCommitOK)
			}
		})
	}
}

func TestCommitCheckSessionAndAddressPatterns(t *testing.T) {
	conf := ccTestConfParsed(t)
	allowedCommit := func(message string) ccCommit {
		return ccCommit{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AuthorName: "Test Author", AuthorEmail: "author@example.invalid",
			CommitName: "Test Author", CommitEmail: "author@example.invalid", Message: message}
	}
	cases := []struct {
		name    string
		message string
		rule    string
	}{
		{"session line", "Text\n\nClaude-Session: https://claude.ai/code/session_abc123\n", ccRuleSessionLine},
		{"session line, mixed case", "Text\n\nCLAUDE-SESSION:  something\n", ccRuleSessionLine},
		{"session URL without a trailer", "Text with a link https://claude.ai/code/session_abc123 in prose.\n", ccRuleSessionURL},
		{"an address in the message", "Text.\n\nSigned-off-by: Someone <someone@example.invalid>\n", ccRuleAddress},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := checkOneCommit(conf, allowedCommit(tc.message), "")
			if !ccHasRule(found, tc.rule) {
				t.Errorf("want rule %s, got %v", tc.rule, found)
			}
		})
	}

	t.Run("a clean message has no finding at all", func(t *testing.T) {
		found := checkOneCommit(conf, allowedCommit("A plain commit message.\n\nAssisted-by: some tool\n"), "")
		if len(found) != 0 {
			t.Errorf("findings %v, want none", found)
		}
	})

	t.Run("a bot's own commit message keeps its own address (N102)", func(t *testing.T) {
		bot := ccCommit{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			AuthorName: "dependabot[bot]", AuthorEmail: "bot@example.invalid",
			CommitName: "GitHub", CommitEmail: "noreply@github.com",
			Message: "Bump x from 1 to 2.\n\nSigned-off-by: dependabot[bot] <support@example.invalid>\n"}
		found := checkOneCommit(conf, bot, "")
		if ccHasRule(found, ccRuleAddress) {
			t.Errorf("bot commit's own address should be exempt, got %v", found)
		}
	})

	t.Run("a human commit message is never exempt from the address rule", func(t *testing.T) {
		found := checkOneCommit(conf, allowedCommit("Text.\n\nSigned-off-by: Test Author <author@example.invalid>\n"), "")
		if !ccHasRule(found, ccRuleAddress) {
			t.Errorf("want %s, got %v", ccRuleAddress, found)
		}
	})
}

func TestCommitCheckPullRequestBody(t *testing.T) {
	t.Run("a clean body with a well-formed trailer passes", func(t *testing.T) {
		body := "Summary of the change.\n\nMore detail.\n\nAssisted-by: some tool\n"
		found := checkPullRequestBody(body, false)
		if len(found) != 0 {
			t.Errorf("findings %v, want none", found)
		}
	})

	t.Run("a session line in the body is a finding", func(t *testing.T) {
		found := checkPullRequestBody("Text\n\nClaude-Session: https://claude.ai/code/session_x\n", false)
		if !ccHasRule(found, ccRulePRSession) {
			t.Errorf("want %s, got %v", ccRulePRSession, found)
		}
	})

	t.Run("an address in the body is a finding", func(t *testing.T) {
		found := checkPullRequestBody("Text.\n\nContact someone@example.invalid.\n", false)
		if !ccHasRule(found, ccRulePRAddress) {
			t.Errorf("want %s, got %v", ccRulePRAddress, found)
		}
	})

	t.Run("no Assisted-by mention is not a finding", func(t *testing.T) {
		found := checkPullRequestBody("Just a plain body.\n", false)
		if ccHasRule(found, ccRuleTrailer) {
			t.Errorf("no mention should not trip the trailer rule, got %v", found)
		}
	})

	t.Run("Assisted-by present but not the last trailer line is a finding", func(t *testing.T) {
		// The #15 shape from CONTRIBUTING.md: a line touching Assisted-by
		// with no blank line before it breaks git's own trailer block.
		body := "Summary.\n\n\U0001F916 Generated with a tool\nAssisted-by: some tool\n"
		found := checkPullRequestBody(body, false)
		if !ccHasRule(found, ccRuleTrailer) {
			t.Errorf("want %s, got %v", ccRuleTrailer, found)
		}
	})

	t.Run("Assisted-by followed by another line is a finding", func(t *testing.T) {
		body := "Summary.\n\nAssisted-by: some tool\n\nOne more line after it.\n"
		found := checkPullRequestBody(body, false)
		if !ccHasRule(found, ccRuleTrailer) {
			t.Errorf("want %s, got %v", ccRuleTrailer, found)
		}
	})

	t.Run("a generator line separated by a blank line still parses", func(t *testing.T) {
		body := "Summary.\n\n\U0001F916 Generated with a tool\n\nAssisted-by: some tool\n"
		found := checkPullRequestBody(body, false)
		if ccHasRule(found, ccRuleTrailer) {
			t.Errorf("a blank line before Assisted-by should parse, got %v", found)
		}
	})

	t.Run("a recognised bot PR body is exempt from every PR-text rule", func(t *testing.T) {
		body := "Bumps x.\n\nContact someone@example.invalid.\n\nClaude-Session: https://claude.ai/code/session_x\n"
		found := checkPullRequestBody(body, true)
		if len(found) != 0 {
			t.Errorf("findings %v, want none (bot PR is exempt)", found)
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
