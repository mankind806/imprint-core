package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests run the real .githooks/pre-push - like hookscript_test.go, an
// exception to the rule that no test reads the repository - against throwaway
// repositories with a local bare repository as their origin. The hook is run
// the way git runs it: through a shell, two arguments (remote name and
// location), the ref list on standard input. IMPRINT_PREPUSH_TEST_SHELL names
// a different shell (dash, say) for a local run; without git or the shell on
// PATH the tests are skipped, and say so.
//
// Every personal-data shape in this file is built at runtime (addr from
// commitcheck_test.go, ppPostcode below), never written as one literal: this
// file is itself pushed through the hook it tests.

// ppPostcode is a postcode-and-place fixture, split so no source line holds it.
var ppPostcode = "10115" + " " + "Musterstadt"

// ppSeedAddr is the address the seed history carries in notes.txt, line 3.
var ppSeedAddr = addr("seed.fixture", "example.invalid")

const ppAuthorName = "Test Author"

type ppHookEnv struct {
	shell string // absolute path of the shell that runs the hook
	hook  string // absolute path of .githooks/pre-push
}

// ppSetup resolves git, the shell and the hook path before a test calls
// t.Parallel, and skips the test when git or the shell is missing.
func ppSetup(t *testing.T) ppHookEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH, so the pre-push hook cannot be exercised here: " + err.Error())
	}
	shell := os.Getenv("IMPRINT_PREPUSH_TEST_SHELL")
	if shell == "" {
		shell = "sh"
	}
	sh, err := exec.LookPath(shell)
	if err != nil {
		t.Skip("no " + shell + " on PATH, so the pre-push hook cannot be run here: " + err.Error())
	}
	hook, err := filepath.Abs(filepath.Join("..", "..", ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hook); err != nil {
		t.Fatal(err)
	}
	return ppHookEnv{shell: sh, hook: hook}
}

type ppRepo struct {
	t      *testing.T
	env    ppHookEnv
	dir    string
	origin string // the bare repository behind the remote named origin
	zero   string // the all-zero object id git sends for "nothing there yet"
}

// ppSeed builds a repository whose main is already on origin (pushed without
// hooks, then fetched, so refs/remotes/origin/main exists). Its history holds
// a shape fixture (ppSeedAddr in notes.txt, line 3) and a commit whose
// committer is GitHub's web-flow identity - neither may count against a push
// that only adds commits on top.
func ppSeed(t *testing.T, env ppHookEnv) *ppRepo {
	t.Helper()
	r := &ppRepo{t: t, env: env, dir: ccNewTestRepo(t)}
	r.git("config", "user.name", ppAuthorName)
	r.git("config", "user.email", ccTestAuthorEmail)
	r.write("README.md", "seed\n")
	r.write("notes.txt", "line one\nline two\ncontact "+ppSeedAddr+"\n")
	r.commit("seed: fixtures with a shape")
	r.write("merged.txt", "merged through the web flow\n")
	r.commitAs(ppAuthorName, ccTestAuthorEmail, "GitHub", ccTestWebFlowEmail, "seed: squash merge with the web-flow committer")
	r.origin = r.newBare()
	r.git("remote", "add", "origin", r.origin)
	r.pushNoVerify("origin", "main")
	r.git("fetch", "-q", "origin")
	r.zero = strings.Repeat("0", len(r.head()))
	return r
}

// with returns a copy of r bound to a subtest's t, so a failing helper calls
// FailNow on the goroutine that runs that subtest.
func (r *ppRepo) with(t *testing.T) *ppRepo { c := *r; c.t = t; return &c }

func (r *ppRepo) git(args ...string) string {
	r.t.Helper()
	return strings.TrimSpace(ccRunGit(r.t, r.dir, args...))
}

func (r *ppRepo) head() string { r.t.Helper(); return r.git("rev-parse", "HEAD") }

func (r *ppRepo) newBare() string {
	r.t.Helper()
	bare := r.t.TempDir()
	ccRunGit(r.t, bare, "init", "-q", "--bare")
	return bare
}

func (r *ppRepo) pushNoVerify(remote, refspec string) {
	r.t.Helper()
	r.git("push", "-q", "--no-verify", remote, refspec)
}

func (r *ppRepo) write(path, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, path), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// stage adds everything and fails the test if nothing is staged:
// ccCommitAs passes --allow-empty, and an empty commit would make every
// "passes" case here pass for nothing.
func (r *ppRepo) stage() {
	r.t.Helper()
	r.git("add", "-A")
	cmd := exec.Command("git", "-C", r.dir, "diff", "--cached", "--quiet")
	cmd.Env = ccIsolatedGitEnv()
	if err := cmd.Run(); err == nil {
		r.t.Fatal("nothing staged; the fixture commit would be empty")
	}
}

// commit stages and commits as the declared identity. Every message in a
// repository has to be distinct: dates are fixed, so two otherwise equal
// commits would share an id.
func (r *ppRepo) commit(message string) string {
	r.t.Helper()
	return r.commitAs(ppAuthorName, ccTestAuthorEmail, ppAuthorName, ccTestAuthorEmail, message)
}

func (r *ppRepo) commitAs(authorName, authorEmail, commName, commEmail, message string) string {
	r.t.Helper()
	r.stage()
	return ccCommitAs(r.t, r.dir, authorName, authorEmail, commName, commEmail, message)
}

// refLine is one line of what git pipes into a pre-push hook.
func refLine(branch, localOid, remoteOid string) string {
	return "refs/heads/" + branch + " " + localOid + " refs/heads/" + branch + " " + remoteOid + "\n"
}

// hook runs the pre-push hook in the repository with args and stdin and
// returns its exit code and combined output.
func (r *ppRepo) hook(args []string, stdin string, extraEnv ...string) (int, string) {
	r.t.Helper()
	cmd := exec.Command(r.env.shell, append([]string{r.env.hook}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = ccIsolatedGitEnv(append([]string{"TMPDIR=" + r.t.TempDir()}, extraEnv...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	case err != nil:
		r.t.Fatalf("running the hook: %v\n%s", err, out)
	}
	return 0, string(out)
}

// hookNewBranch runs the hook for a push of a branch origin does not have yet.
func (r *ppRepo) hookNewBranch(branch, tip string, extraEnv ...string) (int, string) {
	r.t.Helper()
	return r.hook([]string{"origin", r.origin}, refLine(branch, tip, r.zero), extraEnv...)
}

func ppWantPass(t *testing.T, code int, out string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("hook exit %d, want 0 (push proceeds); output:\n%s", code, out)
	}
}

func ppWantRefused(t *testing.T, code int, out string, wants ...string) {
	t.Helper()
	if code != 1 || !strings.Contains(out, "refusing the push") {
		t.Fatalf("hook exit %d, want 1 and a refused push; output:\n%s", code, out)
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q; output:\n%s", w, out)
		}
	}
}

func ppWantAbsent(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(out, u) {
			t.Errorf("output names %q, which is already on the remote; output:\n%s", u, out)
		}
	}
}

// 1. A real git push of a new branch on top of a history that holds a shape
// and a web-flow committer: only the one new commit is checked.
func TestPrePushNewBranchRealPush(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	r.git("checkout", "-q", "-b", "feature")
	r.write("feature.txt", "a clean line\n")
	tip := r.commit("feature: one clean commit")

	hooks := t.TempDir()
	wrapper := "#!/bin/sh\nexec '" + env.shell + "' '" + env.hook + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("config", "core.hooksPath", hooks)

	cmd := exec.Command("git", "-C", r.dir, "push", "origin", "feature")
	cmd.Env = ccIsolatedGitEnv("TMPDIR=" + t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git push: %v, want success; output:\n%s", err, out)
	}
	if !strings.Contains(string(out), "1 new commit(s)") {
		t.Errorf("hook report lacks %q; output:\n%s", "1 new commit(s)", out)
	}
	if got := strings.TrimSpace(ccRunGit(t, r.origin, "rev-parse", "--verify", "refs/heads/feature")); got != tip {
		t.Errorf("origin has feature at %s, want %s", got, tip)
	}
}

// 2. A new commit that adds an address is refused, with path and line named.
func TestPrePushAddedShapeRefused(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	leak := addr("leak.two", "example.invalid")
	r.git("checkout", "-q", "-b", "leak")
	r.write("leak.txt", "first line\nwrite to "+leak+"\n")
	tip := r.commit("leak: add a contact line")
	code, out := r.hookNewBranch("leak", tip)
	ppWantRefused(t, code, out, "address: ", ":leak.txt:2:", leak)
}

// 3. A clean line added to a file that already holds a shape (already on
// origin) passes: only added lines count, not their context.
func TestPrePushCleanLineInFileWithPushedShape(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	r.git("checkout", "-q", "-b", "append")
	r.write("notes.txt", "line one\nline two\ncontact "+ppSeedAddr+"\nline four, clean\n")
	tip := r.commit("notes: append a clean line")
	code, out := r.hookNewBranch("append", tip)
	ppWantPass(t, code, out)
	ppWantAbsent(t, out, ppSeedAddr)
}

// 4. A shape added in one pushed commit and removed in the next is still
// published - its blob stays reachable - and is refused.
func TestPrePushShapeAddedThenRemoved(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	gone := addr("removed.later", "example.invalid")
	r.git("checkout", "-q", "-b", "draft")
	r.write("draft.txt", "draft\ncall "+gone+"\n")
	r.commit("draft: add a contact")
	r.write("draft.txt", "draft\n")
	tip := r.commit("draft: remove the contact again")
	code, out := r.hookNewBranch("draft", tip)
	ppWantRefused(t, code, out, "address: ", ":draft.txt:2:", gone)
}

// 5. An update push after a rebase onto an origin/main that gained a shape
// and a web-flow commit: the upstream commits are on origin already and are
// not checked again.
func TestPrePushUpdateAfterRebase(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	upstream := addr("upstream.merge", "example.invalid")
	r.git("checkout", "-q", "-b", "feature")
	r.write("feature.txt", "first clean line\n")
	old := r.commit("feature: first clean commit")
	r.pushNoVerify("origin", "feature")

	r.git("checkout", "-q", "main")
	r.write("upstream.txt", "upstream contact "+upstream+"\n")
	r.commitAs(ppAuthorName, ccTestAuthorEmail, "GitHub", ccTestWebFlowEmail, "upstream: squash merge carrying a shape")
	r.pushNoVerify("origin", "main")
	r.git("fetch", "-q", "origin")

	r.git("checkout", "-q", "feature")
	r.git("rebase", "-q", "origin/main")
	tip := r.head()
	if tip == old {
		t.Fatal("rebase left the branch tip unchanged")
	}
	code, out := r.hook([]string{"origin", r.origin}, refLine("feature", tip, old))
	ppWantPass(t, code, out)
	ppWantAbsent(t, out, upstream, ppSeedAddr)
}

// 6. A destination with no tracking refs - a configured remote never fetched,
// or a location given in place of a remote name - gets the full history
// checked, shapes in old commits included.
func TestPrePushRemoteWithoutTrackingRefs(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	mirror := r.newBare()
	r.git("remote", "add", "mirror", mirror)
	r.git("checkout", "-q", "-b", "feature")
	r.write("feature.txt", "a clean line\n")
	tip := r.commit("feature: clean commit for the mirror")

	t.Run("remote name without tracking refs", func(t *testing.T) {
		r := r.with(t)
		code, out := r.hook([]string{"mirror", mirror}, refLine("feature", tip, r.zero))
		ppWantRefused(t, code, out, ppSeedAddr)
	})
	t.Run("location as the remote name", func(t *testing.T) {
		r := r.with(t)
		code, out := r.hook([]string{r.origin, r.origin}, refLine("feature", tip, r.zero))
		ppWantRefused(t, code, out, ppSeedAddr)
	})
}

// 7. A new branch at a commit origin already has adds nothing, even while
// HEAD carries an unpushed commit with a shape (guards a fallback to HEAD
// when the commit set is empty).
func TestPrePushBranchAtPushedCommit(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	local := addr("unpushed.head", "example.invalid")
	r.write("local.txt", "unpushed "+local+"\n")
	r.commit("local: unpushed commit with a shape")
	code, out := r.hookNewBranch("alias", pushed)
	ppWantPass(t, code, out)
	if !strings.Contains(out, "0 new commit(s)") {
		t.Errorf("hook report lacks %q; output:\n%s", "0 new commit(s)", out)
	}
	ppWantAbsent(t, out, local, ppSeedAddr)
}

// 8. Commit messages: a shape in a new commit's message is refused; one in
// a message already on origin is not checked again.
func TestPrePushCommitMessages(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	t.Run("new commit's message", func(t *testing.T) {
		r := ppSeed(t, env)
		inMsg := addr("message.new", "example.invalid")
		r.git("checkout", "-q", "-b", "msg")
		r.write("clean.txt", "nothing to see\n")
		tip := r.commit("msg: reach me at " + inMsg)
		code, out := r.hookNewBranch("msg", tip)
		ppWantRefused(t, code, out, inMsg)
	})
	t.Run("pushed commit's message", func(t *testing.T) {
		r := ppSeed(t, env)
		r.write("old.txt", "an old clean file\n")
		r.commit("old: deliver to " + ppPostcode)
		r.pushNoVerify("origin", "main")
		r.git("fetch", "-q", "origin")
		r.git("checkout", "-q", "-b", "next")
		r.write("next.txt", "a clean line\n")
		tip := r.commit("next: clean commit on top")
		code, out := r.hookNewBranch("next", tip)
		ppWantPass(t, code, out)
		ppWantAbsent(t, out, ppPostcode)
	})
}

// 9. A file holding NUL bytes is still scanned.
func TestPrePushFileWithNULBytes(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	hidden := addr("behind.nul", "example.invalid")
	r.git("checkout", "-q", "-b", "binary")
	r.write("blob.bin", "bin\x00ary\x01\ncontact "+hidden+"\n")
	tip := r.commit("binary: add a file with NUL bytes")
	code, out := r.hookNewBranch("binary", tip)
	ppWantRefused(t, code, out, "address: ", ":blob.bin:2:", hidden)
}

// 10. GitHub's web-flow identity is accepted as committer only.
func TestPrePushWebFlowIdentity(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	webFlow := "GitHub <" + ccTestWebFlowEmail + ">"
	t.Run("web-flow committer, declared author", func(t *testing.T) {
		r := r.with(t)
		r.git("checkout", "-q", "-b", "wf-committer", "main")
		r.write("wf1.txt", "squash merged\n")
		tip := r.commitAs(ppAuthorName, ccTestAuthorEmail, "GitHub", ccTestWebFlowEmail, "wf: committed by the web flow")
		code, out := r.hookNewBranch("wf-committer", tip)
		ppWantPass(t, code, out)
	})
	t.Run("web-flow author", func(t *testing.T) {
		r := r.with(t)
		r.git("checkout", "-q", "-b", "wf-author", "main")
		r.write("wf2.txt", "authored as the web flow\n")
		tip := r.commitAs("GitHub", ccTestWebFlowEmail, ppAuthorName, ccTestAuthorEmail, "wf: authored as the web flow")
		code, out := r.hookNewBranch("wf-author", tip)
		ppWantRefused(t, code, out, "undeclared identity: "+webFlow)
	})
}

// 11. Merges: lines a merge takes over from a parent are not new; a line
// that is in neither parent ("evil merge") is.
func TestPrePushMerge(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	other := addr("other.branch", "example.invalid")
	r.git("checkout", "-q", "-b", "other", "main")
	r.write("other.txt", "from the other branch "+other+"\n")
	r.commit("other: a shape, pushed")
	r.pushNoVerify("origin", "other")
	r.git("fetch", "-q", "origin")
	r.git("checkout", "-q", "-b", "feature", "main")
	r.write("feature.txt", "a clean line\n")
	r.commit("feature: clean commit before the merge")

	t.Run("merge of a pushed branch", func(t *testing.T) {
		r := r.with(t)
		r.git("checkout", "-q", "-b", "merged", "feature")
		r.git("merge", "-q", "--no-ff", "--no-edit", "origin/other")
		r.git("rev-parse", "--verify", "HEAD^2")
		code, out := r.hookNewBranch("merged", r.head())
		ppWantPass(t, code, out)
		ppWantAbsent(t, out, other)
	})
	t.Run("evil merge", func(t *testing.T) {
		r := r.with(t)
		evil := addr("evil.merge", "example.invalid")
		r.git("checkout", "-q", "-b", "evil", "feature")
		r.git("merge", "-q", "--no-ff", "--no-commit", "origin/other")
		r.write("README.md", "seed\nevil "+evil+"\n")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "merge: origin/other with a line of its own")
		r.git("rev-parse", "--verify", "HEAD^2")
		code, out := r.hookNewBranch("evil", r.head())
		ppWantRefused(t, code, out, evil)
	})
}

// 12. IMPRINT_PUSH_ANYWAY lets a push with findings through and echoes why.
func TestPrePushAnywayWithReason(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	r.git("checkout", "-q", "-b", "anyway")
	r.write("anyway.txt", "contact "+addr("pushed.anyway", "example.invalid")+"\n")
	tip := r.commit("anyway: a shape pushed on purpose")
	reason := "fixture reviewed by hand"
	code, out := r.hookNewBranch("anyway", tip, "IMPRINT_PUSH_ANYWAY="+reason)
	ppWantPass(t, code, out)
	if !strings.Contains(out, reason) {
		t.Errorf("output does not echo the reason %q; output:\n%s", reason, out)
	}
}

// 13. An empty ref list from git (two arguments, no terminal) passes; a run
// without arguments is refused.
func TestPrePushEmptyRefList(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	t.Run("two arguments, empty list", func(t *testing.T) {
		r := r.with(t)
		code, out := r.hook([]string{"origin", r.origin}, "")
		ppWantPass(t, code, out)
	})
	t.Run("no arguments", func(t *testing.T) {
		r := r.with(t)
		code, out := r.hook(nil, "")
		if code == 0 {
			t.Fatalf("hook exit 0 without arguments, want a refusal; output:\n%s", out)
		}
	})
}

// 14. The hook's web-flow committer identity is the one .imprint/commit.conf
// marks as mergeCommitter.
func TestPrePushWebFlowMatchesCommitConf(t *testing.T) {
	t.Parallel()
	hookPath := filepath.Join("..", "..", ".githooks", "pre-push")
	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	field := func(name string) string {
		m := regexp.MustCompile(`(?m)^` + name + `='([^']*)'$`).FindSubmatch(hook)
		if m == nil {
			t.Errorf("the hook has no line %s='...'", name)
			return ""
		}
		return string(m[1])
	}
	name, user, domain := field("web_flow_name"), field("web_flow_user"), field("web_flow_domain")

	confPath := filepath.Join("..", "..", ".imprint", "commit.conf")
	raw, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	conf, err := parseCommitConf(raw, confPath)
	if err != nil {
		t.Fatal(err)
	}
	var merge []ccIdentity
	for _, c := range conf.Committers {
		if c.MergeCommitter {
			merge = append(merge, c)
		}
	}
	if len(merge) != 1 {
		t.Fatalf("commit.conf has %d mergeCommitter entries, want exactly 1", len(merge))
	}
	if t.Failed() {
		return
	}
	m := merge[0]
	if name != m.Name || user != m.EmailUser || domain != m.EmailDomain {
		t.Errorf("hook web flow %q / %q / %q, commit.conf mergeCommitter %q / %q / %q",
			name, user, domain, m.Name, m.EmailUser, m.EmailDomain)
	}
}
