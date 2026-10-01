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

// ppWantChecked fails the test when the hook reports a check that could not
// run: a shape found only because a check broke is found for the wrong reason.
func ppWantChecked(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "could not run") {
		t.Errorf("hook reports a check that could not run; output:\n%s", out)
	}
}

// installHook makes git itself run the hook on a push from r, through the
// test's shell.
func (r *ppRepo) installHook() {
	r.t.Helper()
	hooks := r.t.TempDir()
	wrapper := "#!/bin/sh\nexec '" + r.env.shell + "' '" + r.env.hook + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte(wrapper), 0o755); err != nil {
		r.t.Fatal(err)
	}
	r.git("config", "core.hooksPath", hooks)
}

// realPush runs git push with the installed hook and returns git's error and
// combined output.
func (r *ppRepo) realPush(args ...string) (error, string) {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir, "push"}, args...)...)
	cmd.Env = ccIsolatedGitEnv("TMPDIR=" + r.t.TempDir())
	out, err := cmd.CombinedOutput()
	return err, string(out)
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

	r.installHook()
	err, out := r.realPush("origin", "feature")
	if err != nil {
		t.Fatalf("git push: %v, want success; output:\n%s", err, out)
	}
	if !strings.Contains(out, "1 new commit(s)") {
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
// when the commit set is empty). The shape is in the message as well: git
// log's fallback to HEAD shows in identities and messages, not in the diffs.
func TestPrePushBranchAtPushedCommit(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	local := addr("unpushed.head", "example.invalid")
	r.write("local.txt", "unpushed "+local+"\n")
	r.commit("local: unpushed commit with a shape, " + local)
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
	ppWantChecked(t, out)
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
// that is in neither parent ("evil merge") is - also in a file that holds a
// NUL byte, in the merge or in only one parent, or that .gitattributes marks
// binary, where a combined diff prints no line at all.
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
	r.git("checkout", "-q", "-b", "nulside", "main")
	r.write("mixed.txt", "bin\x00ary\nshared line\n")
	r.commit("nulside: a file with a NUL byte, pushed")
	r.pushNoVerify("origin", "nulside")
	r.git("fetch", "-q", "origin")

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
	t.Run("evil merge adds a file with a NUL byte", func(t *testing.T) {
		r := r.with(t)
		hidden := addr("evil.nul", "example.invalid")
		r.git("checkout", "-q", "-b", "evil-nul", "feature")
		r.git("merge", "-q", "--no-ff", "--no-commit", "origin/other")
		r.write("n.bin", "bin\x00ary\ncontact "+hidden+"\n")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "merge: origin/other with a NUL file of its own")
		r.git("rev-parse", "--verify", "HEAD^2")
		code, out := r.hookNewBranch("evil-nul", r.head())
		ppWantRefused(t, code, out, "address: ", ":n.bin:2:", hidden)
		ppWantChecked(t, out)
	})
	t.Run("evil merge adds a file marked binary", func(t *testing.T) {
		r := r.with(t)
		hidden := addr("evil.attr", "example.invalid")
		r.git("checkout", "-q", "-b", "evil-attr", "feature")
		r.git("merge", "-q", "--no-ff", "--no-commit", "origin/other")
		r.write(".gitattributes", "*.dat binary\n")
		r.write("z.dat", "plain text\ncontact "+hidden+"\n")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "merge: origin/other with a binary-marked file")
		r.git("rev-parse", "--verify", "HEAD^2")
		code, out := r.hookNewBranch("evil-attr", r.head())
		ppWantRefused(t, code, out, "address: ", ":z.dat:2:", hidden)
		ppWantChecked(t, out)
	})
	t.Run("merge where one parent's version holds a NUL", func(t *testing.T) {
		r := r.with(t)
		hidden := addr("one.parent", "example.invalid")
		r.git("checkout", "-q", "-b", "one-nul", "feature")
		r.write("mixed.txt", "shared line\n")
		r.commit("one-nul: the text version of mixed.txt")
		r.git("merge", "-q", "--no-ff", "--no-commit", "-s", "ours", "origin/nulside")
		r.write("mixed.txt", "shared line\ncontact "+hidden+"\n")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "merge: origin/nulside, keeping the text version plus a line")
		r.git("rev-parse", "--verify", "HEAD^2")
		code, out := r.hookNewBranch("one-nul", r.head())
		ppWantRefused(t, code, out, "address: ", ":mixed.txt:2:", hidden)
		ppWantChecked(t, out)
	})
}

// 11b. The remote's old tip alone - a location for the remote name, so no
// tracking refs - keeps the history it reaches out of the check.
func TestPrePushOldTipOnly(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	r.write("ontop.txt", "a clean line on top of the pushed tip\n")
	tip := r.commit("ontop: one clean commit")
	code, out := r.hook([]string{r.origin, r.origin}, refLine("main", tip, pushed))
	ppWantPass(t, code, out)
	if !strings.Contains(out, "1 new commit(s)") {
		t.Errorf("hook report lacks %q; output:\n%s", "1 new commit(s)", out)
	}
	ppWantAbsent(t, out, ppSeedAddr)
}

// 11c. A push that goes to remote.origin.pushurl rather than to the URL the
// tracking refs were fetched from does not trust them: a commit only the
// fetch repository holds is checked, and its shape refuses a real push.
func TestPrePushPushURLElsewhere(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pub := r.newBare()
	r.git("config", "remote.origin.pushurl", pub)
	r.git("checkout", "-q", "-b", "feature")
	r.write("feature.txt", "a clean line for the push repository\n")
	r.commit("feature: clean commit on top of the fetch repository")

	r.installHook()
	err, out := r.realPush("origin", "feature")
	if err == nil {
		t.Fatalf("git push succeeded, want the hook to refuse it; output:\n%s", out)
	}
	ppWantRefused(t, 1, out, ppSeedAddr, "not trusted")
	ppWantChecked(t, out)
	cmd := exec.Command("git", "-C", pub, "rev-parse", "-q", "--verify", "refs/heads/feature")
	cmd.Env = ccIsolatedGitEnv()
	if got, err := cmd.Output(); err == nil {
		t.Errorf("the push repository has feature at %s, want no such ref", strings.TrimSpace(string(got)))
	}
}

// 11d. A check that could not run refuses the push, and IMPRINT_PUSH_ANYWAY
// does not cover it - here with a blob missing from the object store, and
// with an awk that fails. The new commit's message holds a shape, so a
// finding is there for the override to wave through if it wrongly could.
func TestPrePushCheckCouldNotRun(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	wantCouldNotRun := func(t *testing.T, r *ppRepo, tip string, extraEnv ...string) {
		t.Helper()
		for _, anyway := range []bool{false, true} {
			e := extraEnv
			if anyway {
				e = append(append([]string{}, extraEnv...), "IMPRINT_PUSH_ANYWAY=only a test")
			}
			code, out := r.hookNewBranch("broken", tip, e...)
			ppWantRefused(t, code, out, "check(s) could not run", "IMPRINT_PUSH_ANYWAY does not cover it")
		}
	}
	t.Run("missing blob", func(t *testing.T) {
		r := ppSeed(t, env)
		r.git("checkout", "-q", "-b", "broken")
		r.write("lost.txt", "a line whose blob goes missing\n")
		tip := r.commit("broken: write to " + addr("lost.blob", "example.invalid"))
		blob := r.git("rev-parse", tip+":lost.txt")
		loose := filepath.Join(r.dir, ".git", "objects", blob[:2], blob[2:])
		if err := os.Remove(loose); err != nil {
			t.Fatalf("removing the loose object: %v", err)
		}
		// git diff reads an unchanged checked-out file in place of its blob,
		// so the copy in the working tree has to go as well.
		if err := os.Remove(filepath.Join(r.dir, "lost.txt")); err != nil {
			t.Fatal(err)
		}
		wantCouldNotRun(t, r, tip)
	})
	t.Run("awk fails", func(t *testing.T) {
		r := ppSeed(t, env)
		shim := t.TempDir()
		if err := os.WriteFile(filepath.Join(shim, "awk"), []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		r.git("checkout", "-q", "-b", "broken")
		r.write("shim.txt", "a clean line no awk will read\n")
		tip := r.commit("broken: write to " + addr("no.awk", "example.invalid"))
		wantCouldNotRun(t, r, tip, "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"))
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

// tag makes an annotated tag NAME at TARGET, tagged by taggerName and
// taggerEmail, and returns the tag object's id. git takes the tagger from the
// committer identity. Names have to be distinct: dates are fixed, and the
// name is part of the object.
func (r *ppRepo) tag(name, target, taggerName, taggerEmail, message string) string {
	r.t.Helper()
	cmd := exec.Command("git", "-C", r.dir, "tag", "-a", "-m", message, name, target)
	cmd.Env = ccIsolatedGitEnv("GIT_COMMITTER_NAME="+taggerName, "GIT_COMMITTER_EMAIL="+taggerEmail)
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git tag %s: %v\n%s", name, err, out)
	}
	oid := r.git("rev-parse", "refs/tags/"+name)
	if typ := r.git("cat-file", "-t", oid); typ != "tag" {
		r.t.Fatalf("refs/tags/%s points at a %s, want a tag object", name, typ)
	}
	return oid
}

// tagLine is the line git pipes into the hook for a new tag NAME at OID.
func (r *ppRepo) tagLine(name, oid string) string {
	return "refs/tags/" + name + " " + oid + " refs/tags/" + name + " " + r.zero + "\n"
}

// hookNewTag runs the hook for a push of a tag origin does not have yet.
func (r *ppRepo) hookNewTag(name, oid string, extraEnv ...string) (int, string) {
	r.t.Helper()
	return r.hook([]string{"origin", r.origin}, r.tagLine(name, oid), extraEnv...)
}

// 15. A tag on a commit origin already has adds no commit, and is still
// read: a shape in its message is refused - any of the shapes.
func TestPrePushTagMessage(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	for _, tc := range []struct{ name, shape, text string }{
		{"msg-address", "address", "release notes, write to " + addr("tag.message", "example.invalid")},
		{"msg-postcode", "postcode and place", "release notes, shipped from " + ppPostcode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := r.with(t)
			oid := r.tag(tc.name, pushed, ppAuthorName, ccTestAuthorEmail, "first line\n\n"+tc.text)
			code, out := r.hookNewTag(tc.name, oid)
			ppWantRefused(t, code, out, tc.shape+" in tag "+tc.name+": ", tc.text,
				"0 new commit(s), 1 tag object(s)")
			ppWantChecked(t, out)
		})
	}
}

// 16. The tagger has to be a declared identity. GitHub's web-flow identity
// is let in as a merge's committer only, so as a tagger it is undeclared.
func TestPrePushTagger(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	t.Run("declared tagger", func(t *testing.T) {
		r := r.with(t)
		oid := r.tag("by-author", pushed, ppAuthorName, ccTestAuthorEmail, "a clean tag by the declared identity")
		code, out := r.hookNewTag("by-author", oid)
		ppWantPass(t, code, out)
		if !strings.Contains(out, "0 undeclared tagger(s)") {
			t.Errorf("hook report lacks %q; output:\n%s", "0 undeclared tagger(s)", out)
		}
	})
	t.Run("undeclared tagger", func(t *testing.T) {
		r := r.with(t)
		oid := r.tag("by-stranger", pushed, "Someone Else", ccTestSomeoneEmail, "a clean tag by a stranger")
		code, out := r.hookNewTag("by-stranger", oid)
		ppWantRefused(t, code, out,
			"undeclared identity: Someone Else <"+ccTestSomeoneEmail+">, tagger of tag by-stranger")
	})
	t.Run("web-flow tagger", func(t *testing.T) {
		r := r.with(t)
		oid := r.tag("by-web-flow", pushed, "GitHub", ccTestWebFlowEmail, "a clean tag by the web flow")
		code, out := r.hookNewTag("by-web-flow", oid)
		ppWantRefused(t, code, out,
			"undeclared identity: GitHub <"+ccTestWebFlowEmail+">, tagger of tag by-web-flow")
	})
}

// 17. A nested tag publishes the tags inside it: a shape only in the inner
// tag's message, or a stranger only as the inner tagger, is refused although
// the outer tag is clean.
func TestPrePushNestedTag(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	t.Run("shape in the inner message", func(t *testing.T) {
		r := r.with(t)
		inner := addr("inner.tag", "example.invalid")
		innerOid := r.tag("inner-shape", pushed, ppAuthorName, ccTestAuthorEmail, "inner, contact "+inner)
		outerOid := r.tag("outer-shape", innerOid, ppAuthorName, ccTestAuthorEmail, "outer, clean")
		code, out := r.hookNewTag("outer-shape", outerOid)
		ppWantRefused(t, code, out, "address in tag inner-shape: ", inner, "2 tag object(s)")
	})
	t.Run("stranger as the inner tagger", func(t *testing.T) {
		r := r.with(t)
		innerOid := r.tag("inner-stranger", pushed, "Someone Else", ccTestSomeoneEmail, "inner, by a stranger")
		outerOid := r.tag("outer-stranger", innerOid, ppAuthorName, ccTestAuthorEmail, "outer, by the author")
		code, out := r.hookNewTag("outer-stranger", outerOid)
		ppWantRefused(t, code, out, "tagger of tag inner-stranger")
	})
}

// 18. A clean tag on a pushed commit passes and reads nothing else - not
// the unpushed commit on HEAD that carries a shape in its file and message.
func TestPrePushCleanTagOnPushedCommit(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	local := addr("unpushed.tagged", "example.invalid")
	r.write("local.txt", "unpushed "+local+"\n")
	r.commit("local: unpushed commit with a shape, " + local)
	oid := r.tag("clean", pushed, ppAuthorName, ccTestAuthorEmail, "a clean release tag")
	code, out := r.hookNewTag("clean", oid)
	ppWantPass(t, code, out)
	for _, w := range []string{"0 new commit(s), 1 tag object(s)", "ran: annotated tags"} {
		if !strings.Contains(out, w) {
			t.Errorf("hook report lacks %q; output:\n%s", w, out)
		}
	}
	ppWantAbsent(t, out, local, ppSeedAddr)
}

// 19. A tag on a commit origin does not have yet brings that commit along,
// and the commit is checked as on any other ref.
func TestPrePushTagOnNewCommit(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	carried := addr("tag.carries", "example.invalid")
	r.write("carried.txt", "a line the tag brings along: "+carried+"\n")
	tip := r.commit("carried: a commit only the tag points at")
	oid := r.tag("carrier", tip, ppAuthorName, ccTestAuthorEmail, "a clean tag on a new commit")
	code, out := r.hookNewTag("carrier", oid)
	ppWantRefused(t, code, out, ":carried.txt:1:", carried, "1 new commit(s), 1 tag object(s)")
}

// 20. A real git push of a tag hands the hook the tag object's id: a shape in
// the message stops the push, and origin does not get the tag.
func TestPrePushTagRealPush(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	leak := addr("real.push", "example.invalid")
	r.tag("leaky", pushed, ppAuthorName, ccTestAuthorEmail, "notes, contact "+leak)
	r.tag("fine", pushed, ppAuthorName, ccTestAuthorEmail, "notes, nothing to see")
	r.installHook()

	err, out := r.realPush("origin", "refs/tags/leaky")
	if err == nil {
		t.Fatalf("git push succeeded, want the hook to refuse it; output:\n%s", out)
	}
	ppWantRefused(t, 1, out, "address in tag leaky: ", leak)
	cmd := exec.Command("git", "-C", r.origin, "rev-parse", "-q", "--verify", "refs/tags/leaky")
	cmd.Env = ccIsolatedGitEnv()
	if got, err := cmd.Output(); err == nil {
		t.Errorf("origin has tag leaky at %s, want no such ref", strings.TrimSpace(string(got)))
	}

	err, out = r.realPush("origin", "refs/tags/fine")
	if err != nil {
		t.Fatalf("git push of a clean tag: %v, want success; output:\n%s", err, out)
	}
	if !strings.Contains(out, "1 tag object(s)") {
		t.Errorf("hook report lacks %q; output:\n%s", "1 tag object(s)", out)
	}
}

// 21. IMPRINT_PUSH_ANYWAY waves a tag finding through, as any finding. A tag
// object the hook cannot read - an inner tag missing from the object store, a
// tagger line it cannot parse - refuses the push, and no override covers it.
func TestPrePushTagAnywayAndUnreadable(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	pushed := r.head()
	anyway := "IMPRINT_PUSH_ANYWAY=only a test"
	t.Run("finding with a reason", func(t *testing.T) {
		r := r.with(t)
		oid := r.tag("anyway", pushed, ppAuthorName, ccTestAuthorEmail, "notes, contact "+addr("tag.anyway", "example.invalid"))
		code, out := r.hookNewTag("anyway", oid, anyway)
		ppWantPass(t, code, out)
		if !strings.Contains(out, "only a test") {
			t.Errorf("output does not echo the reason; output:\n%s", out)
		}
	})
	t.Run("inner tag missing", func(t *testing.T) {
		r := r.with(t)
		innerOid := r.tag("lost-inner", pushed, ppAuthorName, ccTestAuthorEmail, "an inner tag that goes missing")
		outerOid := r.tag("lost-outer", innerOid, ppAuthorName, ccTestAuthorEmail, "the outer tag of a lost one")
		r.git("tag", "-d", "lost-inner")
		if err := os.Remove(filepath.Join(r.dir, ".git", "objects", innerOid[:2], innerOid[2:])); err != nil {
			t.Fatalf("removing the loose object: %v", err)
		}
		for _, e := range [][]string{nil, {anyway}} {
			code, out := r.hookNewTag("lost-outer", outerOid, e...)
			ppWantRefused(t, code, out, "could not read the type of "+innerOid)
		}
	})
	t.Run("tagger line without an identity", func(t *testing.T) {
		r := r.with(t)
		raw := "object " + pushed + "\ntype commit\ntag odd\ntagger nobody in brackets 1767225600 +0000\n\na tag no git would write\n"
		cmd := exec.Command("git", "-C", r.dir, "hash-object", "-t", "tag", "-w", "--literally", "--stdin")
		cmd.Env = ccIsolatedGitEnv()
		cmd.Stdin = strings.NewReader(raw)
		got, err := cmd.Output()
		if err != nil {
			t.Fatalf("git hash-object: %v", err)
		}
		oid := strings.TrimSpace(string(got))
		for _, e := range [][]string{nil, {anyway}} {
			code, out := r.hookNewTag("odd", oid, e...)
			ppWantRefused(t, code, out, "could not read the tagger of tag odd", "IMPRINT_PUSH_ANYWAY does not cover it")
		}
	})
}

// 22. A ref that points at a blob or a tree, straight or through a tag, is
// a finding: the hook does not read the content, and rev-list would list
// nothing for it. IMPRINT_PUSH_ANYWAY with a reason lets it through; a real
// push of a blob under refs/tags/ is stopped before origin gets it.
func TestPrePushBlobOrTreeRef(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	cmd := exec.Command("git", "-C", r.dir, "hash-object", "-w", "--stdin")
	cmd.Env = ccIsolatedGitEnv()
	cmd.Stdin = strings.NewReader("a loose blob, contact " + addr("in.blob", "example.invalid") + "\n")
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("git hash-object: %v", err)
	}
	blob := strings.TrimSpace(string(got))
	tree := r.git("rev-parse", "HEAD^{tree}")
	tagBlob := r.tag("on-blob", blob, ppAuthorName, ccTestAuthorEmail, "a clean tag on a blob")
	tagTree := r.tag("on-tree", tree, ppAuthorName, ccTestAuthorEmail, "a clean tag on a tree")
	line := func(ref, oid string) string { return ref + " " + oid + " " + ref + " " + r.zero + "\n" }

	for _, tc := range []struct{ name, ref, oid, want string }{
		{"blob", "refs/tags/b", blob, "unread content: refs/tags/b points straight at blob " + blob},
		{"tree", "refs/trees/t", tree, "unread content: refs/trees/t points straight at tree " + tree},
		{"tag on a blob", "refs/tags/on-blob", tagBlob, "unread content: the tag pushed to refs/tags/on-blob ends in blob " + blob},
		{"tag on a tree", "refs/tags/on-tree", tagTree, "unread content: the tag pushed to refs/tags/on-tree ends in tree " + tree},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := r.with(t)
			code, out := r.hook([]string{"origin", r.origin}, line(tc.ref, tc.oid))
			ppWantRefused(t, code, out, tc.want, "refs to a blob or a tree - 1 ref(s)")
			ppWantChecked(t, out)
			code, out = r.hook([]string{"origin", r.origin}, line(tc.ref, tc.oid), "IMPRINT_PUSH_ANYWAY=pushed on purpose")
			ppWantPass(t, code, out)
		})
	}
	t.Run("real push of a blob under refs/tags", func(t *testing.T) {
		r := r.with(t)
		r.installHook()
		err, out := r.realPush("origin", blob+":refs/tags/blob")
		if err == nil {
			t.Fatalf("git push succeeded, want the hook to refuse it; output:\n%s", out)
		}
		ppWantRefused(t, 1, out, "points straight at blob "+blob)
		cmd := exec.Command("git", "-C", r.origin, "rev-parse", "-q", "--verify", "refs/tags/blob")
		cmd.Env = ccIsolatedGitEnv()
		if got, err := cmd.Output(); err == nil {
			t.Errorf("origin has refs/tags/blob at %s, want no such ref", strings.TrimSpace(string(got)))
		}
	})
}

// rawTag writes CONTENT as a tag object, as git hash-object --literally takes
// it, and returns its id: the hook has to read tag objects that no git
// command would write, since a push carries them all the same.
func (r *ppRepo) rawTag(content string) string {
	r.t.Helper()
	cmd := exec.Command("git", "-C", r.dir, "hash-object", "-t", "tag", "-w", "--literally", "--stdin")
	cmd.Env = ccIsolatedGitEnv()
	cmd.Stdin = strings.NewReader(content)
	got, err := cmd.Output()
	if err != nil {
		r.t.Fatalf("git hash-object: %v", err)
	}
	return strings.TrimSpace(string(got))
}

// 23. Everything in a tag object outside the tagger's identity is read:
// an extra header line, a tag whose header never ends (git reads no message
// from it), CRLF line ends, and what follows the identity on the tagger
// line. A tagger line with no newline after it is still checked, and a tag
// with no tagger at all is a finding.
func TestPrePushTagObjectOutsideMessage(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	head := "object " + r.head() + "\ntype commit\n"
	me := ppAuthorName + " <" + ccTestAuthorEmail + ">"
	stranger := "Someone Else <" + ccTestSomeoneEmail + ">"
	for _, tc := range []struct {
		name, body string
		wants      []string
	}{
		{"extra-header", "tagger " + me + " 1767225600 +0000\nx-note mail " + addr("extra.header", "example.invalid") + "\n\na clean message\n",
			[]string{"address in tag extra-header: 5:", addr("extra.header", "example.invalid")}},
		{"no-blank-line", "tagger " + me + " 1767225600 +0000\nrelease, mail " + addr("no.blank", "example.invalid") + "\n",
			[]string{"address in tag no-blank-line: 5:", addr("no.blank", "example.invalid")}},
		{"crlf", "tagger " + me + " 1767225600 +0000\r\n\r\nrelease, mail " + addr("crlf.lines", "example.invalid") + "\r\n",
			[]string{"address in tag crlf: 6:", addr("crlf.lines", "example.invalid")}},
		{"tagger-date", "tagger " + me + " " + ppPostcode + "\n\na clean message\n",
			[]string{"postcode and place in tag tagger-date: 4:tagger " + ppPostcode}},
		{"tagger-no-newline", "tagger " + stranger + " 1767225600 +0000",
			[]string{"undeclared identity: " + stranger + ", tagger of tag tagger-no-newline"}},
		{"no-tagger", "\na tag that names no tagger\n",
			[]string{"no tagger: tag no-tagger names nobody"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := r.with(t)
			oid := r.rawTag(head + "tag " + tc.name + "\n" + tc.body)
			code, out := r.hookNewTag(tc.name, oid)
			ppWantRefused(t, code, out, tc.wants...)
			ppWantChecked(t, out)
		})
	}
}
