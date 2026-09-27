package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ccNewTestRepo creates an isolated git repository in a fresh temp dir: no
// user config, no system config, no hooks from outside the test, so it never
// picks up this machine's real git identity.
func ccNewTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ccRunGit(t, dir, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(dir, ".imprint"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".imprint", "commit.conf"), []byte(ccTestConf), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func ccIsolatedGitEnv(extra ...string) []string {
	env := []string{
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
		"HOME=/nonexistent-commit-check-test",
		"PATH=" + os.Getenv("PATH"),
	}
	return append(env, extra...)
}

func ccRunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = ccIsolatedGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func ccCommitAs(t *testing.T, dir, authorName, authorEmail, commName, commEmail, message string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "commit", "-q", "--allow-empty", "-m", message)
	cmd.Env = ccIsolatedGitEnv(
		"GIT_AUTHOR_NAME="+authorName, "GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+commName, "GIT_COMMITTER_EMAIL="+commEmail,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return strings.TrimSpace(ccRunGit(t, dir, "rev-parse", "HEAD"))
}

// checkCommitCheckRangeSameRepo is checkCommitCheckRange with sameRepo true
// and no PR title, for the N1-N3 same-repo cases.
func checkCommitCheckRangeSameRepo(root, confRelPath, rangeSpec, prAuthor, prBodyFile, prTitle string) ([]ccFinding, error) {
	return checkCommitCheckRange(root, confRelPath, rangeSpec, prAuthor, prBodyFile, prTitle, true)
}

// checkCommitCheckRangeTitle is checkCommitCheckRange with a PR title set
// and sameRepo true (item N7 is about the title, not about same-repo trust;
// true here just keeps the commits themselves out of the way of the
// assertion).
func checkCommitCheckRangeTitle(root, confRelPath, rangeSpec, prAuthor, prBodyFile, prTitle string) ([]ccFinding, error) {
	return checkCommitCheckRange(root, confRelPath, rangeSpec, prAuthor, prBodyFile, prTitle, true)
}

// TestCommitCheckRangeIntegration covers the task's required cases end to
// end through a real git repository.
func TestCommitCheckRangeIntegration(t *testing.T) {
	t.Run("allowed: no findings", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Allowed commit")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none", findings)
		}
	})

	t.Run("foreign author", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Someone Else", ccTestSomeoneEmail, "Test Author", ccTestAuthorEmail, "Foreign author")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleAuthor) {
			t.Fatalf("findings %v, want %s", findings, ccRuleAuthor)
		}
	})

	t.Run("a session trailer line in a commit message", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail,
			"Feature\n\nClaude-Session: https://claude.ai/code/session_xyz\n")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleSessionLine) {
			t.Fatalf("findings %v, want %s", findings, ccRuleSessionLine)
		}
	})

	t.Run("an address in a commit message", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		// Not the owner's own configured address (item 5 exempts that one);
		// some other address is still a finding.
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail,
			"Feature\n\nSigned-off-by: Someone Else <"+ccTestSomeoneEmail+">\n")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleAddress) {
			t.Fatalf("findings %v, want %s", findings, ccRuleAddress)
		}
	})

	t.Run("the owner's own configured address in a commit message is not a finding (item 5)", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail,
			"Feature\n\nSigned-off-by: Test Author <"+ccTestAuthorEmail+">\n")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none (a configured address is not a leak)", findings)
		}
	})

	t.Run("web-flow committer at a squash merge", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "GitHub", ccTestWebFlowEmail, "Squash merge")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none (the web-flow committer is allowed)", findings)
		}
	})

	t.Run("a dependabot PR: allowed only with the matching --pr-author", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "dependabot[bot]", ccTestBotEmail, "dependabot[bot]", ccTestBotEmail, "chore(deps): bump x")

		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleAuthor) {
			t.Fatalf("without --pr-author, want %s, got %v", ccRuleAuthor, findings)
		}

		findings, err = checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "dependabot[bot]", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("with the matching --pr-author, want no findings, got %v", findings)
		}
	})

	t.Run("push to main after a dependabot squash merge, no PR context left", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "dependabot[bot]", ccTestBotEmail, "GitHub", ccTestWebFlowEmail, "chore(deps): bump x (#1)")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none (bot author + web-flow committer is allowed on push)", findings)
		}
	})

	t.Run("push to main, only the new commit via the onlyCommit^! form", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		ccCommitAs(t, dir, "Someone Else", ccTestSomeoneEmail, "Someone Else", ccTestSomeoneEmail, "An old, already-accepted commit")
		head := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "The new commit")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, head+"^!", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none: only the new commit is in range, the old foreign one is history (N79)", findings)
		}
	})

	t.Run("empty range is not an error", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		head := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, head+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none", findings)
		}
	})

	t.Run("an unresolvable range is an error", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		if _, err := checkCommitCheckRange(dir, defaultCommitConfPath, "no-such-ref..HEAD", "", "", "", false); err == nil {
			t.Fatal("want an error for an unresolvable range")
		}
	})

	t.Run("a PR body with an address fails even when the commits are clean", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Clean commit")
		body := filepath.Join(t.TempDir(), "body.txt")
		if err := os.WriteFile(body, []byte("Text.\n\nContact "+ccTestSomeoneEmail+".\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", ccTestOwnerLogin, body, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRulePRAddress) {
			t.Fatalf("findings %v, want %s", findings, ccRulePRAddress)
		}
	})

	t.Run("item 2: a foreign pull request cannot pass by spoofing the owner's git identity", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		// Anyone forking the repository can set these exact values in their
		// own clone; nothing about them proves who opened the pull request.
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Spoofed commit")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "evil", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleAuthor) || !ccHasRule(findings, ccRuleCommitter) {
			t.Fatalf("findings %v, want both %s and %s", findings, ccRuleAuthor, ccRuleCommitter)
		}
	})

	t.Run("item 2: the owner's real pull request, opened under their own login, passes", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "The owner's own commit")
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", ccTestOwnerLogin, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none", findings)
		}
	})

	t.Run("N1: a real dependabot pull request (bot author, web-flow committer, same-repo) passes", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		// Measured against cli/cli #14487 and actions/checkout #2578 (gh
		// api .../pulls/N/commits): a real, still-open Dependabot pull
		// request's own commits already carry the web-flow identity as
		// committer, not dependabot[bot] itself.
		ccCommitAs(t, dir, "dependabot[bot]", ccTestBotEmail, "GitHub", ccTestWebFlowEmail, "chore(deps): bump x")
		findings, err := checkCommitCheckRangeSameRepo(dir, defaultCommitConfPath, base+"..HEAD", "dependabot[bot]", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none", findings)
		}
	})

	t.Run("N2: a web-flow committer on the owner's own, same-repo pull request passes", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		// A web-UI edit, an "Update branch" merge, or an accepted Copilot
		// suggestion on the owner's own pull request: author is the owner,
		// committer is web-flow, before any merge.
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "GitHub", ccTestWebFlowEmail, "Edited through the web UI")
		findings, err := checkCommitCheckRangeSameRepo(dir, defaultCommitConfPath, base+"..HEAD", ccTestOwnerLogin, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none", findings)
		}
	})

	t.Run("N3: the owner's own fix commit on a dependabot pull request passes", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "dependabot[bot]", ccTestBotEmail, "GitHub", ccTestWebFlowEmail, "chore(deps): bump x")
		// The owner pushes straight onto Dependabot's own, same-repo
		// branch; --pr-author still names the pull request's own opener.
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Fix the lockfile too")
		findings, err := checkCommitCheckRangeSameRepo(dir, defaultCommitConfPath, base+"..HEAD", "dependabot[bot]", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none", findings)
		}
	})

	t.Run("a fork pull request still cannot pass by spoofing the owner's identity, even after N1-N3", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Spoofed commit")
		// Not same-repo: same-repo trust is exactly the fix N1-N3 asked
		// for, so this has to keep failing without it.
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "evil", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleAuthor) || !ccHasRule(findings, ccRuleCommitter) {
			t.Fatalf("findings %v, want both %s and %s", findings, ccRuleAuthor, ccRuleCommitter)
		}
	})

	t.Run("a same-repo commit from an unrecognized identity is still refused", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Someone Else", ccTestSomeoneEmail, "Someone Else", ccTestSomeoneEmail, "Not in .imprint/commit.conf at all")
		findings, err := checkCommitCheckRangeSameRepo(dir, defaultCommitConfPath, base+"..HEAD", "someone-else", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleAuthor) || !ccHasRule(findings, ccRuleCommitter) {
			t.Fatalf("findings %v, want both %s and %s", findings, ccRuleAuthor, ccRuleCommitter)
		}
	})

	t.Run("N7: an AI marker in the PR title alone forces the Assisted-by rule on the body", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Clean commit")
		body := filepath.Join(t.TempDir(), "body.txt")
		if err := os.WriteFile(body, []byte("Just a plain body.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		findings, err := checkCommitCheckRangeTitle(dir, defaultCommitConfPath, base+"..HEAD", ccTestOwnerLogin, body,
			"Generated with a tool: fix the thing")
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRuleTrailer) {
			t.Fatalf("findings %v, want %s (the title alone names an AI feature)", findings, ccRuleTrailer)
		}
	})

	t.Run("N7: the title's own Assisted-by, last after title+blank+body, satisfies the rule", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Clean commit")
		body := filepath.Join(t.TempDir(), "body.txt")
		if err := os.WriteFile(body, []byte("Some detail.\n\nAssisted-by: some tool\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		findings, err := checkCommitCheckRangeTitle(dir, defaultCommitConfPath, base+"..HEAD", ccTestOwnerLogin, body,
			"Fix the thing")
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none", findings)
		}
	})

	t.Run("N7: an address in the PR title alone is a finding", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Clean commit")
		body := filepath.Join(t.TempDir(), "body.txt")
		if err := os.WriteFile(body, []byte("Plain body.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		findings, err := checkCommitCheckRangeTitle(dir, defaultCommitConfPath, base+"..HEAD", ccTestOwnerLogin, body,
			"Fix the thing, contact "+ccTestSomeoneEmail)
		if err != nil {
			t.Fatal(err)
		}
		if !ccHasRule(findings, ccRulePRAddress) {
			t.Fatalf("findings %v, want %s", findings, ccRulePRAddress)
		}
	})

	t.Run("item 4: an absolute --conf reads the config from anywhere, not only under --root", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "Someone Else", ccTestSomeoneEmail, "Someone Else", ccTestSomeoneEmail, "Would fail against the tree's own conf")
		// A base-commit conf that, unlike the tree's own copy, allows
		// "Someone Else" — standing in for `git show base:.imprint/commit.conf`
		// read into a file outside the checkout, the mechanism
		// check.yml uses so a pull request cannot rewrite the list it is
		// itself checked against.
		altConf := `{"authors":[{"name":"Someone Else","emailUser":"someone","emailDomain":"example.invalid"}],` +
			`"committers":[{"name":"Someone Else","emailUser":"someone","emailDomain":"example.invalid"}]}`
		confFile := filepath.Join(t.TempDir(), "base-commit.conf")
		if err := os.WriteFile(confFile, []byte(altConf), 0o644); err != nil {
			t.Fatal(err)
		}
		findings, err := checkCommitCheckRange(dir, confFile, base+"..HEAD", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none (the absolute conf, not the tree's own, decided this)", findings)
		}
	})

	t.Run("item 7: a shallow clone is refused rather than checked incompletely", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "First")
		ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Second")
		shallow := t.TempDir()
		cmd := exec.Command("git", "clone", "-q", "--depth", "1", "--branch", "main", "file://"+dir, shallow)
		cmd.Env = ccIsolatedGitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git clone --depth 1: %v\n%s", err, out)
		}
		if _, err := checkCommitCheckRange(shallow, defaultCommitConfPath, "HEAD^!", "", "", "", false); err == nil {
			t.Fatal("want an error refusing to run against a shallow clone")
		} else if !strings.Contains(err.Error(), "shallow") {
			t.Fatalf("error %q does not mention the shallow clone", err)
		}
	})

	t.Run("a dependabot PR body is exempt even with third-party content", func(t *testing.T) {
		dir := ccNewTestRepo(t)
		base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
		ccCommitAs(t, dir, "dependabot[bot]", ccTestBotEmail, "dependabot[bot]", ccTestBotEmail, "chore(deps): bump x")
		body := filepath.Join(t.TempDir(), "body.txt")
		if err := os.WriteFile(body, []byte("Bumps x.\n\nSee "+ccTestMaintainerEmail+" for details.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		findings, err := checkCommitCheckRange(dir, defaultCommitConfPath, base+"..HEAD", "dependabot[bot]", body, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings %v, want none (dependabot PR body is exempt)", findings)
		}
	})
}

func TestCommitCheckCLIExitCodes(t *testing.T) {
	dir := ccNewTestRepo(t)
	base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
	ccCommitAs(t, dir, "Someone Else", ccTestSomeoneEmail, "Test Author", ccTestAuthorEmail, "Foreign author")

	t.Run("0 without findings", func(t *testing.T) {
		code, out, _ := runCLI(t, "commit-check", "--root", dir, "--range", "HEAD..HEAD")
		if code != exitOK || !strings.Contains(out, "0 finding(s)") {
			t.Fatalf("code %d, output %q", code, out)
		}
	})
	t.Run("1 with a finding", func(t *testing.T) {
		code, out, _ := runCLI(t, "commit-check", "--root", dir, "--range", base+"..HEAD")
		if code != exitViolation || !strings.Contains(out, ccRuleAuthor) {
			t.Fatalf("code %d, output %q", code, out)
		}
	})
	t.Run("2 without --range", func(t *testing.T) {
		if code, _, errs := runCLI(t, "commit-check", "--root", dir); code != exitError || errs == "" {
			t.Fatalf("code %d, stderr %q", code, errs)
		}
	})
	t.Run("2 with a missing config", func(t *testing.T) {
		if code, _, _ := runCLI(t, "commit-check", "--root", dir, "--range", "HEAD..HEAD", "--conf", "does-not-exist.json"); code != exitError {
			t.Fatalf("code %d, want %d", code, exitError)
		}
	})
	t.Run("2 with an unreadable --pr-body-file", func(t *testing.T) {
		if code, _, _ := runCLI(t, "commit-check", "--root", dir, "--range", "HEAD..HEAD", "--pr-body-file", "does-not-exist.txt"); code != exitError {
			t.Fatalf("code %d, want %d", code, exitError)
		}
	})
}

func TestCommitCheckSARIF(t *testing.T) {
	dir := ccNewTestRepo(t)
	base := ccCommitAs(t, dir, "Test Author", ccTestAuthorEmail, "Test Author", ccTestAuthorEmail, "Base")
	ccCommitAs(t, dir, "Someone Else", ccTestSomeoneEmail, "Test Author", ccTestAuthorEmail, "Foreign author")
	out := filepath.Join(t.TempDir(), "out.sarif")

	code, _, stderr := runCLI(t, "commit-check", "--root", dir, "--range", base+"..HEAD", "--sarif", out)
	if code != exitViolation {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Invocations []struct {
				ExecutionSuccessful bool `json:"executionSuccessful"`
			} `json:"invocations"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				RuleIndex int    `json:"ruleIndex"`
				Kind      string `json:"kind"`
				Level     string `json:"level"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &log); err != nil {
		t.Fatal(err)
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 {
		t.Fatalf("version %q, %d runs", log.Version, len(log.Runs))
	}
	r := log.Runs[0]
	if r.Tool.Driver.Name != "imprint-dev-commit-check" {
		t.Fatalf("driver %q", r.Tool.Driver.Name)
	}
	if len(r.Tool.Driver.Rules) != len(ccRules) {
		t.Fatalf("%d rules, want %d", len(r.Tool.Driver.Rules), len(ccRules))
	}
	if !r.Invocations[0].ExecutionSuccessful {
		t.Fatalf("invocations %+v", r.Invocations)
	}
	found := false
	for _, res := range r.Results {
		if res.RuleID == ccRuleAuthor && res.Kind == "fail" && res.Level == "error" {
			if r.Tool.Driver.Rules[res.RuleIndex].ID != ccRuleAuthor {
				t.Errorf("ruleIndex %d does not point at %s", res.RuleIndex, ccRuleAuthor)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no %s result in %+v", ccRuleAuthor, r.Results)
	}
}

func TestCommitCheckSARIFWhenTheCheckCannotRun(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.sarif")
	code, _, _ := runCLI(t, "commit-check", "--root", filepath.Join(t.TempDir(), "nowhere"), "--range", "HEAD..HEAD", "--sarif", out)
	if code != exitError {
		t.Fatalf("exit %d", code)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"executionSuccessful": false`) {
		t.Fatalf("SARIF does not record the failed run:\n%s", raw)
	}
}
