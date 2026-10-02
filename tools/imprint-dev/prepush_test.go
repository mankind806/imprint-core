package main

import (
	"encoding/base64"
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
	shell     string   // absolute path of the shell that runs the hook
	shellArgs []string // arguments before the hook's path ("sh" for busybox)
	hook      string   // absolute path of .githooks/pre-push
	pathDir   string   // a directory put first on the hook's PATH, or ""
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
	return r.hookVia(nil, args, stdin, extraEnv...)
}

// hookVia runs the hook as hook does, with wrap (a command that execs its
// arguments) in front of the shell, when wrap is not empty.
func (r *ppRepo) hookVia(wrap, args []string, stdin string, extraEnv ...string) (int, string) {
	r.t.Helper()
	argv := append(append(append(append([]string{}, wrap...), r.env.shell), r.env.shellArgs...), r.env.hook)
	cmd := exec.Command(argv[0], append(argv[1:], args...)...)
	cmd.Dir = r.dir
	env := []string{"TMPDIR=" + r.t.TempDir()}
	if r.env.pathDir != "" {
		env = append(env, "PATH="+r.env.pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cmd.Env = ccIsolatedGitEnv(append(env, extraEnv...)...)
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
	wrapper := "#!/bin/sh\nexec '" + strings.Join(append(append([]string{r.env.shell}, r.env.shellArgs...), r.env.hook), "' '") + "' \"$@\"\n"
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

// ppRealBin returns the absolute path of name on PATH, for a shim to forward
// to, and skips the test when there is none.
func ppRealBin(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skip("no " + name + " on PATH to forward to: " + err.Error())
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return p
}

// ppShimPATH writes body as a sh script named name into a fresh directory and
// returns a PATH setting that puts that directory first.
func ppShimPATH(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// 11d. A check that could not run refuses the push, and IMPRINT_PUSH_ANYWAY
// does not cover it - here with a blob missing from the object store, with an
// awk that fails, and with each guard against a diff, a parent list or a work
// file that is not what the hook expects. The new commit's message holds a
// shape, so a finding is there for the override to wave through if it wrongly
// could; each case also names the guard that has to catch it.
func TestPrePushCheckCouldNotRun(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	wantCouldNotRun := func(t *testing.T, r *ppRepo, tip string, extraEnv []string, wants ...string) {
		t.Helper()
		for _, anyway := range []bool{false, true} {
			e := extraEnv
			if anyway {
				e = append(append([]string{}, extraEnv...), "IMPRINT_PUSH_ANYWAY=only a test")
			}
			code, out := r.hookNewBranch("broken", tip, e...)
			ppWantRefused(t, code, out, append([]string{"check(s) could not run", "IMPRINT_PUSH_ANYWAY does not cover it"}, wants...)...)
		}
	}
	// brokenTip seeds a repository with one new commit that adds one clean
	// line and carries a shape in its message.
	brokenTip := func(t *testing.T, label string) (*ppRepo, string) {
		t.Helper()
		r := ppSeed(t, env)
		r.git("checkout", "-q", "-b", "broken")
		r.write("shim.txt", "a clean line the shim gets in the way of\n")
		return r, r.commit("broken: write to " + addr(label, "example.invalid"))
	}
	gitBin := ppRealBin(t, "git")
	awkBin := ppRealBin(t, "awk")
	grepBin := ppRealBin(t, "grep")
	sortBin := ppRealBin(t, "sort")
	// A git that corrupts one command's output, and passes every other
	// command through.
	gitShim := func(t *testing.T, match, run string) string {
		return ppShimPATH(t, "git", "case \" $* \" in *\" "+match+" \"*) "+run+" ;; esac\nexec '"+gitBin+"' \"$@\"\n")
	}
	// An awk that runs the real one, then does after - a write that went
	// missing without awk saying so.
	awkShim := func(t *testing.T, after string) string {
		return ppShimPATH(t, "awk", "'"+awkBin+"' \"$@\"; rc=$?\n"+after+"\nexit $rc\n")
	}
	for _, c := range []struct {
		name  string
		shim  func(t *testing.T) string
		wants []string
	}{
		{"a hunk line that is not +, -, space or backslash", func(t *testing.T) string {
			return gitShim(t, "diff-tree", "'"+gitBin+"' \"$@\" | '"+awkBin+"' '{ print } /^@@ / { print \"?not a diff line\" }'; exit")
		}, []string{"into added lines"}},
		{"git prints Binary files", func(t *testing.T) string {
			return gitShim(t, "diff-tree", "'"+gitBin+"' \"$@\"; rc=$?; printf 'diff --git a/x.bin b/x.bin\\nBinary files a/x.bin and b/x.bin differ\\n'; exit $rc")
		}, []string{"could not read x.bin as text"}},
		{"a commit missing from the parent list", func(t *testing.T) string {
			return gitShim(t, "--parents", "'"+gitBin+"' \"$@\" | sed '$d'; exit")
		}, []string{"listed parents for 0 of 1 new commit(s)"}},
		{"an added-line record missing", func(t *testing.T) string {
			return awkShim(t, `if [ -n "${IMPRINT_AWKCNT:-}" ]; then n=$(cat "$IMPRINT_AWKCNT"); echo $((n + 1)) >"$IMPRINT_AWKCNT"; fi`)
		}, []string{"wrote down 1 of 2 line(s)", " adds against "}},
		{"a line missing from added", func(t *testing.T) string {
			return awkShim(t, `if [ -n "${IMPRINT_ADDED:-}" ]; then sed '$d' "$IMPRINT_ADDED" >"$IMPRINT_ADDED.cut" && mv "$IMPRINT_ADDED.cut" "$IMPRINT_ADDED"; fi`)
		}, []string{"wrote down 0 of 1 added line(s) in added"}},
		// A grep that exits 0 and writes nothing, as busybox grep does when
		// it cannot write its hits: 0 means a line matched, so the empty
		// file is a write that failed, not a clean list.
		{"a grep over the added lines that writes no hit", func(t *testing.T) string {
			return ppShimPATH(t, "grep", "for a; do last=$a; done\ncase \"${last:-}\" in */added) exit 0 ;; esac\nexec '"+grepBin+"' \"$@\"\n")
		}, []string{" in the added lines but wrote no hit down"}},
		{"a grep over the commit objects that writes no hit", func(t *testing.T) string {
			return ppShimPATH(t, "grep", "for a; do last=$a; done\ncase \"${last:-}\" in */committext) exit 0 ;; esac\nexec '"+grepBin+"' \"$@\"\n")
		}, []string{" in the commit objects but wrote no hit down"}},
		// The sort that merges the hits under your locale with those under
		// C, failing out loud, and losing a line without saying so. The
		// shape is in the commit message, so the commit objects are where it
		// fails.
		{"a sort that cannot merge the hits", func(t *testing.T) string {
			return ppShimPATH(t, "sort", "for a; do case \"$a\" in */hits.own) exit 2 ;; esac; done\nexec '"+sortBin+"' \"$@\"\n")
		}, []string{"sort could not merge the address hits in the commit objects"}},
		{"a sort that loses a merged hit", func(t *testing.T) string {
			return ppShimPATH(t, "sort", "for a; do case \"$a\" in */hits.own) '"+sortBin+"' \"$@\" | sed '$d'; exit ;; esac; done\nexec '"+sortBin+"' \"$@\"\n")
		}, []string{"sort lost some of the address hits in the commit objects while merging them"}},
		{"a sort that swaps a merged hit for another line", func(t *testing.T) string {
			return ppShimPATH(t, "sort", "for a; do case \"$a\" in */hits.own) '"+sortBin+"' \"$@\" | sed '1s/.*/1:not what grep found/'; exit ;; esac; done\nexec '"+sortBin+"' \"$@\"\n")
		}, []string{"sort lost some of the address hits in the commit objects while merging them"}},
		{"a shape loop cut short", func(t *testing.T) string {
			// A grep that empties the shape list the loops read, while the
			// first loop is on its first shape.
			return ppShimPATH(t, "grep", "for a; do last=$a; done\ncase \"${last:-}\" in */added) : >\"${last%/added}/shapes\" ;; esac\nexec '"+grepBin+"' \"$@\"\n")
		}, []string{"ran 1 of ", " shape(s) over the added lines", "ran 0 of ", " shape(s) over the commit objects"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, tip := brokenTip(t, "guard.shim")
			wantCouldNotRun(t, r, tip, []string{c.shim(t)}, c.wants...)
		})
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
		wantCouldNotRun(t, r, tip, nil)
	})
	// The first awk the hook runs reads the new commit's object, and a
	// commit nothing could read stops the push there, before any report.
	t.Run("awk fails", func(t *testing.T) {
		r, tip := brokenTip(t, "no.awk")
		shim := ppShimPATH(t, "awk", "exit 2\n")
		for _, e := range [][]string{{shim}, {shim, "IMPRINT_PUSH_ANYWAY=only a test"}} {
			code, out := r.hookNewBranch("broken", tip, e...)
			ppWantRefused(t, code, out, "awk could not read commit "+tip)
		}
	})
	// The awk that puts each hit back where it came from loses its write, and
	// exits 0 as busybox awk does when its standard output fails: only the
	// count of the lines it placed can tell. The exit 0 is forced, so
	// this holds under an awk that would report the failure itself.
	t.Run("a hit placed but not written", func(t *testing.T) {
		if fi, err := os.Stat("/dev/full"); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			t.Skip("no /dev/full here to stand in for a full disk")
		}
		r := ppSeed(t, env)
		r.git("checkout", "-q", "-b", "broken")
		r.write("lost.txt", "first line\nwrite to "+addr("lost.join", "example.invalid")+"\n")
		tip := r.commit("broken: write to " + addr("lost.message", "example.invalid"))
		shim := ppShimPATH(t, "awk", "if [ -n \"${IMPRINT_SHAPE:-}\" ]; then '"+awkBin+"' \"$@\" >/dev/full; exit 0; fi\nexec '"+awkBin+"' \"$@\"\n")
		wantCouldNotRun(t, r, tip, []string{shim}, "placed 0 of 1 address hit(s)", "placed 0 of 1 address in a commit object hit(s)")
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

// 15. GIT_DIFF_OPTS overrides -U0 even on diff-tree, and with
// diff.suppressBlankEmpty a blank context line comes out as an empty line.
// Together they shifted the line numbers in one parent's diff of a merge
// only, and the line the merge adds fell out of the intersection. With both
// set, the hook still names the line.
func TestPrePushDiffEnvironment(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	r.write("blank.txt", "a\nb\nc\n")
	r.commit("blank: three lines")
	r.pushNoVerify("origin", "main")
	r.git("checkout", "-q", "-b", "side", "main")
	r.write("blank.txt", "a\n\nb\nc\n")
	r.commit("side: a blank line, pushed")
	r.pushNoVerify("origin", "side")
	r.git("fetch", "-q", "origin")
	r.git("checkout", "-q", "-b", "feature", "main")
	r.write("feature.txt", "a clean line\n")
	r.commit("feature: clean commit before the merge")
	evil := addr("blank.context", "example.invalid")
	r.git("merge", "-q", "--no-ff", "--no-commit", "origin/side")
	r.write("blank.txt", "a\n\nb\n"+evil+"\nc\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "merge: origin/side with a line of its own")
	r.git("rev-parse", "--verify", "HEAD^2")
	r.git("config", "diff.suppressBlankEmpty", "true")
	code, out := r.hookNewBranch("blank", r.head(), "GIT_DIFF_OPTS=-u3")
	ppWantRefused(t, code, out, "address: ", ":blank.txt:4:", evil)
	ppWantChecked(t, out)
}

// 16. Refs under refs/remotes/origin/ that another remote wrote are not
// origin's word: a commit only that other remote holds is checked on its way
// to origin, and the scope line says why.
func TestPrePushTrackingNamespaceShared(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	// privateTip commits a shape only the private remote gets, then a clean
	// commit on top of it, and returns that clean commit.
	privateTip := func(r *ppRepo, secret string) string {
		r.t.Helper()
		r.git("checkout", "-q", "-b", "secret", "main")
		r.write("secret.txt", "contact "+secret+"\n")
		r.commit("secret: only on the private remote")
		r.write("feature.txt", "a clean line\n")
		return r.commit("feature: clean commit on top")
	}
	t.Run("another remote's fetch refspec", func(t *testing.T) {
		r := ppSeed(t, env)
		secret := addr("refspec.private", "example.invalid")
		tip := privateTip(r, secret)
		priv := r.newBare()
		r.git("remote", "add", "priv", priv)
		r.git("config", "--replace-all", "remote.priv.fetch", "+refs/heads/*:refs/remotes/origin/priv/*")
		r.pushNoVerify("priv", "secret~1:refs/heads/secret")
		r.git("fetch", "-q", "priv")
		r.git("rev-parse", "--verify", "refs/remotes/origin/priv/secret")
		code, out := r.hookNewBranch("feature", tip)
		ppWantRefused(t, code, out, secret, "remote priv fetches into refs/remotes/origin/", "not trusted")
		ppWantChecked(t, out)
	})
	t.Run("another remote named under origin/", func(t *testing.T) {
		r := ppSeed(t, env)
		secret := addr("name.private", "example.invalid")
		tip := privateTip(r, secret)
		priv := r.newBare()
		// A URL and no fetch refspec, so only the name says where its refs go.
		r.git("config", "remote.origin/priv.url", priv)
		r.pushNoVerify("origin/priv", "secret~1:refs/heads/secret")
		r.git("fetch", "-q", "origin/priv", "+refs/heads/*:refs/remotes/origin/priv/*")
		r.git("rev-parse", "--verify", "refs/remotes/origin/priv/secret")
		code, out := r.hookNewBranch("feature", tip)
		ppWantRefused(t, code, out, secret, "remote origin/priv stores its refs under refs/remotes/origin/", "not trusted")
		ppWantChecked(t, out)
	})
	// Refspecs on another remote that land in refs/remotes/origin/: an exact
	// destination, the same without refs/ (git puts it in front of a
	// destination without a glob that starts with remotes/), and a mirror,
	// whose glob is broader than refs/remotes/origin/. pushed is the ref the
	// private remote holds the shape under.
	for _, c := range []struct{ name, refspec, pushed string }{
		{"another remote's exact refspec destination", "+refs/heads/secret:refs/remotes/origin/secret", "refs/heads/secret"},
		{"a refspec destination without refs/", "+refs/heads/secret:remotes/origin/secret", "refs/heads/secret"},
		{"another remote's mirror refspec", "+refs/*:refs/*", "refs/remotes/origin/secret"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ppSeed(t, env)
			secret := addr("dest.private", "example.invalid")
			tip := privateTip(r, secret)
			priv := r.newBare()
			r.git("remote", "add", "priv", priv)
			r.git("config", "--replace-all", "remote.priv.fetch", c.refspec)
			// To the location, not the name: a push to priv would update its
			// tracking ref without the refs/ git puts in front on a fetch, and
			// write a file under the git directory's remotes/ folder.
			r.pushNoVerify(priv, "secret~1:"+c.pushed)
			r.git("fetch", "-q", "priv")
			r.git("rev-parse", "--verify", "refs/remotes/origin/secret")
			code, out := r.hookNewBranch("feature", tip)
			ppWantRefused(t, code, out, secret, "remote priv fetches into refs/remotes/origin/", "not trusted")
			ppWantChecked(t, out)
		})
	}
	t.Run("a remote in the legacy remotes/ folder", func(t *testing.T) {
		r := ppSeed(t, env)
		secret := addr("legacy.private", "example.invalid")
		tip := privateTip(r, secret)
		priv := r.newBare()
		r.pushNoVerify(priv, "secret~1:refs/heads/secret")
		// Neither git remote nor git config shows this remote; git 2.55.0
		// still fetches by it.
		if err := os.MkdirAll(filepath.Join(r.dir, ".git", "remotes"), 0o755); err != nil {
			t.Fatal(err)
		}
		r.write(filepath.Join(".git", "remotes", "priv"), "URL: "+priv+"\nPull: +refs/heads/*:refs/remotes/origin/p/*\n")
		r.git("fetch", "-q", "priv")
		r.git("rev-parse", "--verify", "refs/remotes/origin/p/secret")
		code, out := r.hookNewBranch("feature", tip)
		ppWantRefused(t, code, out, secret, "legacy remotes/ folder", "not trusted")
		ppWantChecked(t, out)
	})
	// With extensions.worktreeConfig on, another worktree's config.worktree
	// can define a remote, or point origin's own fetch elsewhere, where this
	// worktree's git remote and git remote get-url do not see it.
	for _, c := range []struct {
		name  string
		setup func(r *ppRepo, wt, priv string)
	}{
		{"a remote in another worktree's config.worktree", func(r *ppRepo, wt, priv string) {
			ccRunGit(r.t, wt, "config", "--worktree", "remote.priv.url", priv)
			ccRunGit(r.t, wt, "config", "--worktree", "remote.priv.fetch", "+refs/heads/*:refs/remotes/origin/p/*")
			ccRunGit(r.t, wt, "fetch", "-q", "priv")
			r.git("rev-parse", "--verify", "refs/remotes/origin/p/secret")
		}},
		{"insteadOf in another worktree's config.worktree", func(r *ppRepo, wt, priv string) {
			ccRunGit(r.t, wt, "config", "--worktree", "url."+priv+".insteadOf", r.origin)
			ccRunGit(r.t, wt, "fetch", "-q", "origin")
			r.git("rev-parse", "--verify", "refs/remotes/origin/secret")
			if got := r.git("remote", "get-url", "origin"); got != r.origin {
				r.t.Fatalf("this worktree's origin fetches from %q, want %q", got, r.origin)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ppSeed(t, env)
			secret := addr("worktree.private", "example.invalid")
			tip := privateTip(r, secret)
			priv := r.newBare()
			r.pushNoVerify(priv, "secret~1:refs/heads/secret")
			r.git("config", "extensions.worktreeConfig", "true")
			wt := filepath.Join(t.TempDir(), "other")
			r.git("worktree", "add", "-q", "--detach", wt, "main")
			c.setup(r, wt, priv)
			code, out := r.hookNewBranch("feature", tip)
			ppWantRefused(t, code, out, secret, "extensions.worktreeConfig is on", "not trusted")
			ppWantChecked(t, out)
		})
	}
}

// 17. A relative TMPDIR whose name holds "=": awk took the working files for
// variable assignments, read nothing, and let a shape through.
func TestPrePushRelativeTMPDIR(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	leak := addr("relative.tmpdir", "example.invalid")
	r.git("checkout", "-q", "-b", "rel")
	r.write("rel.txt", "first line\nwrite to "+leak+"\n")
	tip := r.commit("rel: add a contact line")
	if err := os.Mkdir(filepath.Join(r.dir, "a=b"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out := r.hookNewBranch("rel", tip, "TMPDIR=a=b")
	ppWantRefused(t, code, out, "address: ", ":rel.txt:2:", leak)
	ppWantChecked(t, out)
}

// 18. A ref line the hook cannot write down - here under a file size limit of
// zero, with the signal for it ignored so that the write fails rather than
// killing the shell - is refused, not read as nothing to push.
func TestPrePushRefListNotWritten(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	limit := []string{env.shell, "-c", `trap '' XFSZ; ulimit -f 0; exec "$@"`, "sh"}
	if out, err := exec.Command(env.shell, "-c", `trap '' XFSZ; ulimit -f 0`).CombinedOutput(); err != nil {
		t.Skipf("%s cannot ignore XFSZ and set a file size limit: %v\n%s", env.shell, err, out)
	}
	r := ppSeed(t, env)
	r.git("checkout", "-q", "-b", "limited")
	r.write("limited.txt", "a clean line\n")
	tip := r.commit("limited: one clean commit")
	code, out := r.hookVia(limit, []string{"origin", r.origin}, refLine("limited", tip, r.zero))
	ppWantRefused(t, code, out, "wrote down 0 of 1 ref line(s)")
}

// 19. A replace ref (git replace) shows a clean commit in place of one that
// adds a shape, but a push sends the original - so the hook reads the
// original.
func TestPrePushReplacedCommit(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	leak := addr("replaced.commit", "example.invalid")
	r.git("checkout", "-q", "-b", "decoy", "main")
	r.write("repl.txt", "a clean line\n")
	decoy := r.commit("repl: the clean stand-in")
	r.git("checkout", "-q", "-b", "repl", "main")
	r.write("repl.txt", "contact "+leak+"\n")
	real := r.commit("repl: the commit that is pushed")
	r.git("replace", real, decoy)
	if got := r.git("show", "repl:repl.txt"); got != "a clean line" {
		t.Fatalf("git shows %q at repl, want the stand-in's line", got)
	}
	code, out := r.hookNewBranch("repl", real)
	ppWantRefused(t, code, out, "address: ", ":repl.txt:1:", leak)
	ppWantChecked(t, out)
	// What a push sends: the original, which origin then serves.
	r.pushNoVerify("origin", "repl")
	if got := strings.TrimSpace(ccRunGit(t, r.origin, "show", "repl:repl.txt")); !strings.Contains(got, leak) {
		t.Errorf("origin serves %q at repl, want the original line with the shape", got)
	}
}

// 20. A rename (git mv) of a file that holds a shape already on origin adds
// no line: without rename detection every line of the file would read as new.
func TestPrePushRenamePushedShape(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	r.git("checkout", "-q", "-b", "moved")
	r.git("mv", "notes.txt", "moved.txt")
	tip := r.commit("notes: rename")
	code, out := r.hookNewBranch("moved", tip)
	ppWantPass(t, code, out)
	ppWantAbsent(t, out, ppSeedAddr)
}

// 21. An octopus merge (three parents): bringing in only pushed content
// passes, and a line new to all three parents is refused.
func TestPrePushOctopusMerge(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	one, two := addr("octopus.one", "example.invalid"), addr("octopus.two", "example.invalid")
	r.git("checkout", "-q", "-b", "o1", "main")
	r.write("o1.txt", "first branch "+one+"\n")
	r.commit("o1: a shape, pushed")
	r.git("checkout", "-q", "-b", "o2", "main")
	r.write("o2.txt", "second branch "+two+"\n")
	r.commit("o2: a shape, pushed")
	r.pushNoVerify("origin", "o1")
	r.pushNoVerify("origin", "o2")
	r.git("fetch", "-q", "origin")
	r.git("checkout", "-q", "-b", "feature", "main")
	r.write("feature.txt", "a clean line\n")
	r.commit("feature: clean commit before the merge")

	t.Run("only pushed content", func(t *testing.T) {
		r := r.with(t)
		r.git("checkout", "-q", "-b", "octo", "feature")
		r.git("merge", "-q", "--no-ff", "--no-edit", "origin/o1", "origin/o2")
		r.git("rev-parse", "--verify", "HEAD^3")
		code, out := r.hookNewBranch("octo", r.head())
		ppWantPass(t, code, out)
		ppWantAbsent(t, out, one, two)
	})
	t.Run("a line new to every parent", func(t *testing.T) {
		r := r.with(t)
		evil := addr("octopus.evil", "example.invalid")
		r.git("checkout", "-q", "-b", "octo-evil", "feature")
		r.git("merge", "-q", "--no-ff", "--no-commit", "origin/o1", "origin/o2")
		r.write("README.md", "seed\noctopus "+evil+"\n")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "merge: o1 and o2 with a line of its own")
		r.git("rev-parse", "--verify", "HEAD^3")
		code, out := r.hookNewBranch("octo-evil", r.head())
		ppWantRefused(t, code, out, "address: ", ":README.md:2:", evil)
		ppWantChecked(t, out)
		ppWantAbsent(t, out, one, two)
	})
}

// 22. A NUL byte on the same line as a shape: busybox awk and mawk end a
// record at a NUL, so without the hook's tr step the rest of the line would
// never reach the check.
func TestPrePushNULOnShapeLine(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	hidden := addr("same.line", "example.invalid")
	r.git("checkout", "-q", "-b", "nulline")
	r.write("nl.txt", "head\nbin\x00ary contact "+hidden+"\n")
	tip := r.commit("nulline: a NUL before a shape")
	code, out := r.hookNewBranch("nulline", tip)
	ppWantRefused(t, code, out, "address: ", ":nl.txt:2:", hidden)
	ppWantChecked(t, out)
}

// 23. A few cases under every shell and awk found on PATH: sh, dash and
// busybox sh, each with the default awk, mawk, busybox awk and original-awk.
// The tag and tree cases (28-37 below) and some commit object cases (45, 46,
// 48, 49, 53) run here too, for the seds, greps and awks those shells come
// with.
// CI runs on Ubuntu, where sh is dash and awk may be mawk; the combinations
// that ran are logged (go test -v), so the log says which were exercised. A
// combination that resolves to one already listed runs once.
func TestPrePushToolchains(t *testing.T) {
	base := ppSetup(t)
	t.Parallel()
	type tool struct {
		label string   // what the log calls it
		key   string   // the resolved binary and its arguments, to skip duplicates
		bin   string   // the path as found on PATH, which is what runs
		args  []string // arguments before the script's own
		shim  bool     // an awk other than the default, so put first on PATH
	}
	find := func(name string, args ...string) (tool, bool) {
		p, err := exec.LookPath(name)
		if err != nil {
			return tool{}, false
		}
		real := p
		if e, err := filepath.EvalSymlinks(p); err == nil {
			real = e
		}
		label := strings.Join(append([]string{name}, args...), " ")
		if b := filepath.Base(real); b != name {
			label += " (" + b + ")"
		}
		return tool{label: label, key: real + " " + strings.Join(args, " "), bin: p, args: args}, true
	}
	var shells, awks []tool
	seen := map[string]bool{}
	for _, c := range [][]string{{"sh"}, {"dash"}, {"busybox", "sh"}} {
		if tl, ok := find(c[0], c[1:]...); ok && !seen["sh:"+tl.key] {
			seen["sh:"+tl.key] = true
			shells = append(shells, tl)
		}
	}
	for _, c := range [][]string{{"awk"}, {"mawk"}, {"busybox", "awk"}, {"original-awk"}} {
		if tl, ok := find(c[0], c[1:]...); ok && !seen["awk:"+tl.key] {
			seen["awk:"+tl.key] = true
			tl.shim = c[0] != "awk"
			awks = append(awks, tl)
		}
	}
	if len(shells) == 0 || len(awks) == 0 {
		t.Skip("no shell or no awk on PATH")
	}
	grepBin := ppRealBin(t, "grep")

	r := ppSeed(t, base)
	other := addr("tc.other", "example.invalid")
	r.git("checkout", "-q", "-b", "other", "main")
	r.write("other.txt", "from the other branch "+other+"\n")
	r.commit("other: a shape, pushed")
	r.pushNoVerify("origin", "other")
	r.git("fetch", "-q", "origin")
	r.git("checkout", "-q", "-b", "feature", "main")
	r.write("feature.txt", "a clean line\n")
	clean := r.commit("feature: clean commit")
	leak := addr("tc.leak", "example.invalid")
	r.git("checkout", "-q", "-b", "leak", "main")
	r.write("leak.txt", "first line\nwrite to "+leak+"\n")
	leakTip := r.commit("leak: add a contact line")
	nulLine := addr("tc.nulline", "example.invalid")
	r.git("checkout", "-q", "-b", "nulline", "main")
	r.write("nl.txt", "head\nbin\x00ary contact "+nulLine+"\n")
	nulLineTip := r.commit("nulline: a NUL before a shape")
	r.git("checkout", "-q", "-b", "merged", "feature")
	r.git("merge", "-q", "--no-ff", "--no-edit", "origin/other")
	mergedTip := r.head()
	nulFile := addr("tc.evilnul", "example.invalid")
	r.git("checkout", "-q", "-b", "evil-nul", "feature")
	r.git("merge", "-q", "--no-ff", "--no-commit", "origin/other")
	r.write("n.bin", "bin\x00ary\ncontact "+nulFile+"\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "merge: origin/other with a NUL file of its own")
	evilNulTip := r.head()
	tagged := r.git("rev-parse", "main")
	innerShape := addr("tc.innertag", "example.invalid")
	innerTag := r.tag("tc-inner", tagged, ppAuthorName, ccTestAuthorEmail, "inner, contact "+innerShape)
	outerTag := r.tag("tc-outer", innerTag, ppAuthorName, ccTestAuthorEmail, "outer, clean")
	cleanTag := r.tag("tc-clean", tagged, ppAuthorName, ccTestAuthorEmail, "a clean tag")
	stranger := "Someone Else <" + ccTestSomeoneEmail + ">"
	rawTag := r.rawTag("object " + tagged + "\ntype commit\ntag tc-raw\ntagger " + stranger + " 1767225600 +0000")
	tree := r.git("rev-parse", "main^{tree}")
	noTagger := r.rawTag("object " + tagged + "\ntype commit\ntag tc-notagger\n\na tag that names no tagger\n")
	treeTag := r.tag("tc-ontree", tree, ppAuthorName, ccTestAuthorEmail, "a clean tag on a tree")
	latin1 := r.tag("tc-latin1", tagged, ppAuthorName, ccTestAuthorEmail, "release\n\nTel.:\xa0"+"0"+"30"+" "+"1234567")
	r.git("checkout", "-q", "-b", "latin1line", "main")
	r.write("l1.txt", "head\nTel.:\xa0"+"0"+"30"+" "+"1234567\n")
	latin1LineTip := r.commit("latin1line: a Latin-1 byte before a number")
	tcID := ppAuthorName + " <" + ccTestAuthorEmail + "> 1767225600 +0000"
	rawMsgTip := r.rawCommit("tree " + tree + "\nparent " + tagged + "\nauthor " + tcID + "\ncommitter " + tcID +
		"\n\nrawmsg: a Latin-1 byte\n\nTel.:\xa0" + "0" + "30" + " " + "1234567\n")
	// The encoding cases here hold with any grep: busybox sh runs busybox's
	// own grep, which in CI matched no capital umlaut in a place while the
	// postcode pattern held its umlauts in a bracket; the encoding cases
	// that need one are in tests 40 and 42. A declared name outside ASCII
	// shows git log's --encoding at work, and a mislabelled commit's
	// address its object read as stored. umlautplace below runs the
	// spelled-out pattern (44) under every shell.
	tcName := "J\u00fcrgen Test"
	tcDeclared := []string{"GIT_CONFIG_KEY_0=imprint.allowedIdentity", "GIT_CONFIG_VALUE_0=" + tcName + " <" + ccTestSomeoneEmail + ">"}
	r.git("checkout", "-q", "-b", "encid", "main")
	r.write("enc.txt", "a clean line\n")
	r.stage()
	r.git("-c", "user.name="+tcName, "-c", "user.email="+ccTestSomeoneEmail, "commit", "-q", "-m", "encid: a declared name outside ASCII")
	encIDTip := r.head()
	tcMislabelAddr := addr("tc.mislabel", "example.invalid")
	r.git("checkout", "-q", "-b", "mislabel", "main")
	r.write("mis.txt", "a clean line\n")
	r.stage()
	r.git("-c", "user.name="+tcName, "-c", "user.email="+ccTestSomeoneEmail, "-c", "i18n.commitEncoding=ISO-8859-1",
		"commit", "-q", "-m", "mislabel: write to "+tcMislabelAddr)
	mislabelTip := r.head()
	r.git("checkout", "-q", "-b", "umlautplace", "main")
	r.write("up.txt", "head\ndeliver to "+"10115"+" "+"\u00dcbungsstadt\n")
	umlautPlaceTip := r.commit("umlautplace: a place with a capital umlaut")
	latin1Tagger := r.rawTag("object " + tagged + "\ntype commit\ntag tc-latin1tagger\ntagger Gr\xfcn Fremd <" +
		ccTestSomeoneEmail + "> 1767225600 +0000\n\na clean tag\n")
	// Commit objects read as stored (45-49): a shape after a NUL in the
	// message and in an extra header line, a signature whose base64 holds
	// an IBAN's form, and merges of a signed-looking tag by a declared
	// tagger and by a stranger. prepare's \001 brackets and armour rule run
	// in each awk here, and every clean case before shows its identity
	// strip at work.
	objHead := "tree " + tree + "\nparent " + tagged + "\nauthor " + tcID + "\ncommitter " + tcID + "\n"
	tcNul := addr("tc.nul", "example.invalid")
	objNulTip := r.rawCommit(objHead + "\nobjnul: a clean subject\n\nbefore\x00 after " + tcNul + "\n")
	tcHdr := addr("tc.header", "example.invalid")
	objHdrTip := r.rawCommit(objHead + "x-note " + tcHdr + "\n\nobjhdr: a clean subject\n")
	objSigTip := r.rawCommit(objHead + "gpgsig " + ppArmour(" ", "") + "\nobjsig: a clean subject\n")
	// The list of encodings read in full (53), under each awk's regex.
	encReadTip := r.rawCommit(objHead + "encoding ISO-8859-1\n\nencread: a clean subject\n")
	encUnreadTip := r.rawCommit(objHead + "encoding UTF-7\n\nencunread: a clean subject\n")
	r.git("checkout", "-q", "-b", "tc-side", "main")
	r.write("tc-side.txt", "a clean line\n")
	tcSide := r.commit("tc-side: one clean commit")
	r.ppSignedLookingTag("tc-mtdeclared", tcSide, ppAuthorName+" <"+ccTestAuthorEmail+">", "a clean tag")
	r.ppSignedLookingTag("tc-mtstranger", tcSide, stranger, "a clean tag by a stranger")
	mtDeclared := r.ppMergeTag("tc-mtdeclared")
	mtStranger := r.ppMergeTag("tc-mtstranger")

	for _, sh := range shells {
		for _, aw := range awks {
			sh, aw := sh, aw
			name := sh.label + " + " + aw.label
			t.Logf("toolchain: %s", name)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := base
				env.shell, env.shellArgs = sh.bin, sh.args
				if aw.shim {
					shim := t.TempDir()
					script := "#!/bin/sh\nexec '" + strings.Join(append([]string{aw.bin}, aw.args...), "' '") + "' \"$@\"\n"
					if err := os.WriteFile(filepath.Join(shim, "awk"), []byte(script), 0o755); err != nil {
						t.Fatal(err)
					}
					env.pathDir = shim
				}
				c := *r
				c.t, c.env = t, env
				code, out := c.hookNewBranch("feature", clean)
				ppWantPass(t, code, out)
				code, out = c.hookNewBranch("leak", leakTip)
				ppWantRefused(t, code, out, "address: ", ":leak.txt:2:", leak)
				code, out = c.hookNewBranch("merged", mergedTip)
				ppWantPass(t, code, out)
				ppWantAbsent(t, out, other)
				code, out = c.hookNewBranch("evil-nul", evilNulTip)
				ppWantRefused(t, code, out, "address: ", ":n.bin:2:", nulFile)
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("nulline", nulLineTip)
				ppWantRefused(t, code, out, "address: ", ":nl.txt:2:", nulLine)
				ppWantChecked(t, out)
				code, out = c.hookNewTag("tc-clean", cleanTag)
				ppWantPass(t, code, out)
				code, out = c.hookNewTag("tc-outer", outerTag)
				ppWantRefused(t, code, out, "address in tag tc-inner: ", innerShape)
				ppWantChecked(t, out)
				code, out = c.hookNewTag("tc-raw", rawTag)
				ppWantRefused(t, code, out, "undeclared identity: "+stranger+", tagger of tag tc-raw")
				ppWantChecked(t, out)
				code, out = c.hook([]string{"origin", c.origin}, "refs/trees/t "+tree+" refs/trees/t "+c.zero+"\n")
				ppWantRefused(t, code, out, "points straight at tree "+tree)
				code, out = c.hookNewTag("tc-notagger", noTagger)
				ppWantRefused(t, code, out, "no tagger: tag tc-notagger")
				code, out = c.hookNewTag("tc-ontree", treeTag)
				ppWantRefused(t, code, out, "the tag pushed to refs/tags/tc-ontree ends in tree "+tree)
				code, out = c.hookNewTag("tc-latin1", latin1, "LC_ALL=C.UTF-8")
				ppWantRefused(t, code, out, "phone number in tag tc-latin1: 8:")
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("latin1line", latin1LineTip, "LC_ALL=C.UTF-8")
				ppWantRefused(t, code, out, "phone number: ", ":l1.txt:2:")
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("rawmsg", rawMsgTip, "LC_ALL=C.UTF-8")
				ppWantRefused(t, code, out, "phone number in a commit object: "+rawMsgTip+":8:")
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("encid", encIDTip, append([]string{"LC_ALL=C.UTF-8", "GIT_CONFIG_COUNT=2",
					"GIT_CONFIG_KEY_1=i18n.logOutputEncoding", "GIT_CONFIG_VALUE_1=ISO-8859-1"}, tcDeclared...)...)
				ppWantPass(t, code, out)
				code, out = c.hookNewBranch("mislabel", mislabelTip, append([]string{"LC_ALL=C.UTF-8", "GIT_CONFIG_COUNT=1"}, tcDeclared...)...)
				ppWantRefused(t, code, out, "address in a commit object: "+mislabelTip+":", "mislabel: write to "+tcMislabelAddr)
				ppWantChecked(t, out)
				if strings.Contains(out, "undeclared identity") {
					t.Errorf("the stored reading did not vouch for the declared name; output:\n%s", out)
				}
				for _, lc := range []string{"LC_ALL=C", "LC_ALL=C.UTF-8"} {
					code, out = c.hookNewBranch("umlautplace", umlautPlaceTip, lc)
					ppWantRefused(t, code, out, "postcode and place: ", ":up.txt:2:")
					ppWantChecked(t, out)
				}
				code, out = c.hookNewTag("tc-latin1tagger", latin1Tagger, "LC_ALL=C.UTF-8")
				ppWantRefused(t, code, out, ", tagger of tag tc-latin1tagger")
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("objnul", objNulTip, "LC_ALL=C.UTF-8")
				ppWantRefused(t, code, out, "address in a commit object: "+objNulTip+":8:before\x01 after "+tcNul)
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("objhdr", objHdrTip)
				ppWantRefused(t, code, out, "address in a commit object: "+objHdrTip+":5:x-note "+tcHdr)
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("objsig", objSigTip)
				ppWantPass(t, code, out)
				code, out = c.hookNewBranch("encread", encReadTip)
				ppWantPass(t, code, out)
				code, out = c.hookNewBranch("encunread", encUnreadTip)
				ppWantRefused(t, code, out, "unread encoding: commit "+encUnreadTip+" names the encoding \"UTF-7\"")
				ppWantChecked(t, out)
				code, out = c.hookNewBranch("merge-tc-mtdeclared", mtDeclared)
				ppWantPass(t, code, out)
				code, out = c.hookNewBranch("merge-tc-mtstranger", mtStranger, "LC_ALL=C.UTF-8")
				ppWantRefused(t, code, out, "undeclared identity: "+stranger+", tagger of the tag commit "+mtStranger+" merges")
				ppWantChecked(t, out)
				if strings.Contains(out, "in a commit object") {
					t.Errorf("the merged tag's tagger was read as a shape; output:\n%s", out)
				}

				// A file a shape grep writes to cannot be opened: a grep
				// before the shape loops - the one over the declared
				// identities - puts a directory in its place. In bash and
				// busybox sh the failed redirection returns 1, grep's "no line
				// matched", and every shape read as clean. hits.own, hits.c
				// and greperr are grep_both's grep targets; hits is where sort
				// merges their lines. Checks 2 and 3 share them, and each has
				// to say so on its own: a check 3 that read the directory as
				// clean would leave the added line's finding alone, which
				// IMPRINT_PUSH_ANYWAY waves through.
				for _, f := range []struct {
					file  string
					wants []string
				}{
					{"hits.own", []string{"could not run grep for address over the added lines", "could not run grep for address over the commit objects"}},
					{"hits.c", []string{"could not run grep for address over the added lines", "could not run grep for address over the commit objects"}},
					{"greperr", []string{"could not run grep for address over the added lines", "could not run grep for address over the commit objects"}},
					// sort runs only where a grep found something, and the
					// leak branch's message is clean, so this one is check 2's.
					{"hits", []string{"sort could not merge the address hits in the added lines"}},
				} {
					shim := t.TempDir()
					fired := filepath.Join(t.TempDir(), "fired")
					script := "#!/bin/sh\nfor a; do last=$a; done\n" +
						"case \"${last:-}\" in */allowed | */committers) d=\"${last%/*}\"; [ -d \"$d/" + f.file + "\" ] || { rm -f \"$d/" + f.file + "\" && mkdir \"$d/" + f.file + "\" && : >'" + fired + "'; } ;; esac\n" +
						"exec '" + grepBin + "' \"$@\"\n"
					if err := os.WriteFile(filepath.Join(shim, "grep"), []byte(script), 0o755); err != nil {
						t.Fatal(err)
					}
					path := shim + string(os.PathListSeparator)
					if env.pathDir != "" {
						path += env.pathDir + string(os.PathListSeparator)
					}
					for _, anyway := range []bool{false, true} {
						e := []string{"PATH=" + path + os.Getenv("PATH")}
						if anyway {
							e = append(e, "IMPRINT_PUSH_ANYWAY=only a test")
						}
						if err := os.Remove(fired); err != nil && !errors.Is(err, os.ErrNotExist) {
							t.Fatal(err)
						}
						code, out = c.hookNewBranch("leak", leakTip, e...)
						if _, err := os.Stat(fired); err != nil {
							t.Logf("%s ran no grep from PATH, so %s replaced by a directory was not exercised", sh.label, f.file)
							break
						}
						ppWantRefused(t, code, out, append([]string{"check(s) could not run", "IMPRINT_PUSH_ANYWAY does not cover it"}, f.wants...)...)
						if strings.Contains(out, "no findings") {
							t.Errorf("%s replaced by a directory: hook reports no findings; output:\n%s", f.file, out)
						}
					}
				}
			})
		}
	}
}

// 24. With POSIXLY_CORRECT set, GNU sort takes an option after a file operand
// for another file, so `sort -u FILE -o FILE` refused every push. A clean new
// commit still passes - and only through the tracking refs, since the seed
// history holds a shape - and an added shape is still refused.
func TestPrePushPOSIXLYCorrect(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	r.git("checkout", "-q", "-b", "posix")
	r.write("posix.txt", "a clean line\n")
	clean := r.commit("posix: one clean commit")
	code, out := r.hookNewBranch("posix", clean, "POSIXLY_CORRECT=1")
	ppWantPass(t, code, out)
	if !strings.Contains(out, "1 new commit(s)") {
		t.Errorf("hook report lacks %q; output:\n%s", "1 new commit(s)", out)
	}
	ppWantAbsent(t, out, ppSeedAddr)

	leak := addr("posixly.correct", "example.invalid")
	r.git("checkout", "-q", "-b", "posix-leak", "main")
	r.write("leak.txt", "first line\nwrite to "+leak+"\n")
	tip := r.commit("posix: add a contact line")
	code, out = r.hookNewBranch("posix-leak", tip, "POSIXLY_CORRECT=1")
	ppWantRefused(t, code, out, "address: ", ":leak.txt:2:", leak)
	ppWantChecked(t, out)
}

// 25. A signal ends the hook half-way. dash and busybox sh run no EXIT trap
// then, so without a trap of its own the hook would leave its working files
// behind and exit by the signal; it removes them and exits 1, which refuses
// the push. The TERM comes during the last git call the hook makes, the diff
// of the new commit against its parent: a trap that removed the files and
// returned would let the hook run on without them, to whatever exit that
// happened to reach.
// And it comes from a grep in a shape loop, which has to run in the shell that
// holds the trap: a piped loop ran in a subshell that died alone, and the hook
// reported no findings for the shapes it never ran - here the one shape the
// added line holds.
func TestPrePushSignalCleansUp(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	gitBin := ppRealBin(t, "git")
	grepBin := ppRealBin(t, "grep")
	for _, c := range []struct {
		name    string
		content string
		shim    func(t *testing.T) string
	}{
		{"a TERM during the last git call", "a clean line\n", func(t *testing.T) string {
			return ppShimPATH(t, "git", "case \" $* \" in *\" diff-tree \"*) kill -TERM \"$PPID\" ;; esac\nexec '"+gitBin+"' \"$@\"\n")
		}},
		{"a TERM from a grep in a shape loop", "first line\nvisit " + ppPostcode + "\n", func(t *testing.T) string {
			return ppShimPATH(t, "grep", "case \" $* \" in *\"/added \"*) kill -TERM \"$PPID\" ;; esac\nexec '"+grepBin+"' \"$@\"\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ppSeed(t, env)
			r.git("checkout", "-q", "-b", "sig")
			r.write("sig.txt", c.content)
			tip := r.commit("sig: one commit")
			tmp := t.TempDir()
			code, out := r.hookNewBranch("sig", tip, c.shim(t), "TMPDIR="+tmp)
			if code != 1 || strings.Contains(out, "no findings") {
				t.Errorf("hook exit %d after a TERM, want 1 and no report of no findings; output:\n%s", code, out)
			}
			left, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range left {
				t.Errorf("the hook left %s behind in TMPDIR", e.Name())
			}
		})
	}
}

// 26. A line of the hook's own report that cannot be written. A grep, at each
// shape it runs over the commit objects, swaps the findings or the errors file
// for a link to /dev/full, so every write to it fails as on a full disk. The
// verdict reads those files, and an empty one means clean: a finding or an
// error that was never written down let the push through with "no findings".
// The refusal has to come from the write, not from the check before the
// verdict that the files are still there, and no override covers it.
func TestPrePushReportNotWritten(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	if fi, err := os.Stat("/dev/full"); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skip("no /dev/full here to stand in for a full disk")
	}
	grepBin := ppRealBin(t, "grep")
	for _, c := range []struct {
		name string
		file string // the work file the shim swaps for /dev/full
		then string // what the shim does next at a grep over the commit objects
	}{
		{"a finding in a commit message", "findings", ":"},
		{"a grep that fails over the commit objects", "errors", "exit 2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ppSeed(t, env)
			r.git("checkout", "-q", "-b", "full")
			r.write("full.txt", "a clean line\n")
			tip := r.commit("full: write to " + addr("full.disk", "example.invalid"))
			shim := ppShimPATH(t, "grep", "for a; do last=$a; done\n"+
				"case \"${last:-}\" in */committext) ln -s -f /dev/full \"${last%/committext}/"+c.file+"\"; "+c.then+" ;; esac\n"+
				"exec '"+grepBin+"' \"$@\"\n")
			for _, anyway := range []bool{false, true} {
				e := []string{shim}
				if anyway {
					e = append(e, "IMPRINT_PUSH_ANYWAY=only a test")
				}
				code, out := r.hookNewBranch("full", tip, e...)
				ppWantRefused(t, code, out, "could not write the hook's report")
				if strings.Contains(out, "no findings") {
					t.Errorf("hook reports no findings; output:\n%s", out)
				}
			}
		})
	}
}

// 27. The working directory removed half-way - by a grep, at the first shape it
// runs over the commit objects, the last step before the verdict. The verdict
// read the files that were gone as empty ones and reported no findings. Which
// guard meets the gap first differs between shells, so the test names none;
// the shim leaves a mark outside the working directory, so a refusal for the
// shape alone does not pass for a guard that held.
func TestPrePushWorkGone(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	grepBin := ppRealBin(t, "grep")
	for _, c := range []struct {
		name    string
		message string
	}{
		{"a message with a shape", "gone: write to " + addr("work.gone", "example.invalid")},
		{"a clean message", "gone: one clean commit"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ppSeed(t, env)
			r.git("checkout", "-q", "-b", "gone")
			r.write("gone.txt", "a clean line\n")
			tip := r.commit(c.message)
			fired := filepath.Join(t.TempDir(), "fired")
			shim := ppShimPATH(t, "grep", "for a; do last=$a; done\n"+
				"case \"${last:-}\" in */committext) rm -rf \"${last%/committext}\"; : >'"+fired+"' ;; esac\n"+
				"exec '"+grepBin+"' \"$@\"\n")
			for _, anyway := range []bool{false, true} {
				e := []string{shim}
				if anyway {
					e = append(e, "IMPRINT_PUSH_ANYWAY=only a test")
				}
				if err := os.Remove(fired); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				code, out := r.hookNewBranch("gone", tip, e...)
				if _, err := os.Stat(fired); err != nil {
					t.Fatalf("no grep ran over the commit objects, so the working directory was never removed; output:\n%s", out)
				}
				ppWantRefused(t, code, out)
				if strings.Contains(out, "no findings") {
					t.Errorf("hook reports no findings; output:\n%s", out)
				}
			}
		})
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

// 28. A tag on a commit origin already has adds no commit, and is still
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

// 29. The tagger has to be a declared identity. GitHub's web-flow identity
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
	t.Run("declared tagger with a Latin-1 name, UTF-8 locale", func(t *testing.T) {
		r := r.with(t)
		name := "J\xfcrgen Tester"
		r.git("config", "--add", "imprint.allowedIdentity", name+" <"+ccTestSomeoneEmail+">")
		oid := r.tag("by-latin1", pushed, name, ccTestSomeoneEmail, "a clean tag by a declared Latin-1 name")
		code, out := r.hookNewTag("by-latin1", oid, "LC_ALL=C.UTF-8")
		ppWantPass(t, code, out)
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

// 30. A nested tag publishes the tags inside it: a shape only in the inner
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

// 31. A clean tag on a pushed commit passes and reads nothing else - not
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

// 32. A tag on a commit origin does not have yet brings that commit along,
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

// 33. A real git push of a tag hands the hook the tag object's id: a shape in
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

// 34. IMPRINT_PUSH_ANYWAY waves a tag finding through, as any finding. A tag
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

// 35. A ref that points at a blob or a tree, straight or through a tag, is
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
	return r.rawObject("tag", content)
}

// rawCommit writes content as a commit object, byte for byte: git commit and
// git commit-tree turn a message byte that is not valid UTF-8 into UTF-8,
// taking it for Latin-1, and so cannot make the commit some other tool can.
func (r *ppRepo) rawCommit(content string) string {
	r.t.Helper()
	return r.rawObject("commit", content)
}

func (r *ppRepo) rawObject(kind, content string) string {
	r.t.Helper()
	cmd := exec.Command("git", "-C", r.dir, "hash-object", "-t", kind, "-w", "--literally", "--stdin")
	cmd.Env = ccIsolatedGitEnv()
	cmd.Stdin = strings.NewReader(content)
	got, err := cmd.Output()
	if err != nil {
		r.t.Fatalf("git hash-object: %v", err)
	}
	return strings.TrimSpace(string(got))
}

// 36. Everything in a tag object outside the tagger's identity is read:
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

// 37. A tag object is raw bytes. A Latin-1 no-break space - not valid UTF-8 -
// right before a phone number matches no bracket expression under a UTF-8
// locale, so the shape is matched under C as well. The hook runs here under
// C.UTF-8; where that locale is missing, grep falls back to C and this case
// proves less.
func TestPrePushTagLatin1Byte(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	phone := "0" + "30" + " " + "1234567"
	oid := r.tag("latin1", r.head(), ppAuthorName, ccTestAuthorEmail, "release\n\nTel.:\xa0"+phone)
	code, out := r.hookNewTag("latin1", oid, "LC_ALL=C.UTF-8")
	ppWantRefused(t, code, out, "phone number in tag latin1: 8:")
	ppWantChecked(t, out)
	if n := strings.Count(out, "phone number in tag latin1"); n != 1 {
		t.Errorf("the hit is reported %d times, want once; output:\n%s", n, out)
	}
}

// 38. The failures of tests 25-27 and of the toolchain test's directory case,
// and lists the tag check reads changed under it, on a push of a tag alone: no new commit, so the tag check's greps are the
// only shape greps that run. A TERM from one of them, the findings or the
// errors file swapped for /dev/full, the working directory removed, a hit
// file that cannot be opened, and a grep that matches and writes nothing all
// refuse the push, with and without IMPRINT_PUSH_ANYWAY, and none reports no
// findings.
func TestPrePushTagCheckBroken(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	grepBin := ppRealBin(t, "grep")
	gitBin := ppRealBin(t, "git")
	full := false
	if fi, err := os.Stat("/dev/full"); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		full = true
	}
	shape := "release notes, write to " + addr("tag.broken", "example.invalid")
	onTagtext := func(action string) string {
		return "for a; do last=$a; done\ncase \"${last:-}\" in */tagtext) " + action + " ;; esac\nexec '" + grepBin + "' \"$@\"\n"
	}
	// The tagger check is the one grep over */allowed before the tag's shape
	// greps when no commit is new.
	onAllowed := func(file string) string {
		return "for a; do last=$a; done\ncase \"${last:-}\" in */allowed) d=\"${last%/*}\"; rm -f \"$d/" + file + "\" && mkdir \"$d/" + file + "\" ;; esac\nexec '" + grepBin + "' \"$@\"\n"
	}
	// While the hook reads imprint.allowedIdentity - after it wrote the ref
	// lists down, before the tag check reads them - git changes a work file
	// under the hook's TMPDIR. A list that is gone, empty or a directory reads
	// like one at its end, with no error.
	onConfig := func(action string) string {
		return "case \" $* \" in *\" imprint.allowedIdentity \"*) for d in \"$TMPDIR\"/*/; do " + action + "; done ;; esac\nexec '" + gitBin + "' \"$@\"\n"
	}
	for _, c := range []struct {
		name, message, shim string
		devFull             bool
		wants               []string
		tool                string // the binary the shim stands in for; grep when empty
		tree                bool   // push a tag on a tree rather than on a commit
	}{
		{name: "tags list a directory", message: shape, tool: "git",
			shim: onConfig("rm -f \"${d}tags\" && mkdir \"${d}tags\""), wants: []string{"read back 0 of 1 tag object(s)"}},
		{name: "tags list emptied", message: shape, tool: "git",
			shim: onConfig(": >\"${d}tags\""), wants: []string{"read back 0 of 1 tag object(s)"}},
		{name: "objects list a directory", message: "a clean tag on a tree", tool: "git", tree: true,
			shim: onConfig("rm -f \"${d}objects\" && mkdir \"${d}objects\""), wants: []string{"read back", "a blob or a tree"}},
		{name: "objects list removed", message: "a clean tag on a tree", tool: "git", tree: true,
			shim: onConfig("rm -f \"${d}objects\""), wants: []string{"read back", "a blob or a tree"}},
		{name: "declared identities a directory", message: "a clean tag", tool: "git",
			shim: onConfig("rm -f \"${d}allowed\" && mkdir \"${d}allowed\""), wants: []string{"grep could not read the declared identities"}},
		{"a TERM from a tag grep", shape, onTagtext("kill -TERM \"$PPID\""), false, nil, "", false},
		{"findings on a full disk", shape, onTagtext("ln -s -f /dev/full \"${last%/tagtext}/findings\""), true,
			[]string{"could not write the hook's report"}, "", false},
		{"errors on a full disk", "a clean tag", onTagtext("ln -s -f /dev/full \"${last%/tagtext}/errors\"; exit 2"), true,
			[]string{"could not write the hook's report"}, "", false},
		{"work directory removed, shape", shape, onTagtext("rm -rf \"${last%/*}\""), false, nil, "", false},
		{"work directory removed, clean", "a clean tag", onTagtext("rm -rf \"${last%/*}\""), false, nil, "", false},
		{"thits.own is a directory", shape, onAllowed("thits.own"), false,
			[]string{"could not run grep for address over tag"}, "", false},
		{"thits.C is a directory", shape, onAllowed("thits.C"), false,
			[]string{"could not run grep for address over tag"}, "", false},
		{"a grep that matches and writes nothing", shape, onTagtext("exit 0"), false,
			[]string{"but wrote no hit down"}, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.devFull && !full {
				t.Skip("no /dev/full here to stand in for a full disk")
			}
			r := ppSeed(t, env)
			target := r.head()
			if c.tree {
				target = r.git("rev-parse", "HEAD^{tree}")
			}
			oid := r.tag("broken", target, ppAuthorName, ccTestAuthorEmail, c.message)
			tool := c.tool
			if tool == "" {
				tool = "grep"
			}
			path := ppShimPATH(t, tool, c.shim)
			for _, anyway := range []bool{false, true} {
				tmp := t.TempDir()
				e := []string{path, "TMPDIR=" + tmp}
				if anyway {
					e = append(e, "IMPRINT_PUSH_ANYWAY=only a test")
				}
				code, out := r.hookNewTag("broken", oid, e...)
				if strings.HasPrefix(c.name, "a TERM") {
					// The trap removes the files and exits 1 without a word,
					// as in test 25.
					if code != 1 {
						t.Errorf("hook exit %d after a TERM, want 1; output:\n%s", code, out)
					}
				} else {
					ppWantRefused(t, code, out, c.wants...)
				}
				if strings.Contains(out, "no findings") || strings.Contains(out, "IMPRINT_PUSH_ANYWAY is set") {
					t.Errorf("hook let the push through; output:\n%s", out)
				}
				left, err := os.ReadDir(tmp)
				if err != nil {
					t.Fatal(err)
				}
				for _, l := range left {
					t.Errorf("the hook left %s behind in TMPDIR", l.Name())
				}
			}
		})
	}
}

// ppLatin1Log has git print commit metadata in Latin-1 for one hook run,
// without touching the repository's config.
var ppLatin1Log = []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=i18n.logOutputEncoding", "GIT_CONFIG_VALUE_0=ISO-8859-1"}

// 39. The lines a new commit adds are raw bytes, as the file was written. A
// Latin-1 no-break space right before a phone number matches no bracket
// expression under a UTF-8 locale, so the added lines are matched under C as
// well, as a tag object is (37). The hook runs here under C.UTF-8; where that
// locale is missing, grep falls back to C and this case proves less.
func TestPrePushAddedLineLatin1Byte(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	phone := "0" + "30" + " " + "1234567"
	r.git("checkout", "-q", "-b", "latin1")
	r.write("latin1.txt", "first line\nTel.:\xa0"+phone+"\n")
	tip := r.commit("latin1: a Latin-1 byte before a number")
	code, out := r.hookNewBranch("latin1", tip, "LC_ALL=C.UTF-8")
	ppWantRefused(t, code, out, "phone number: "+tip+":latin1.txt:2:")
	ppWantChecked(t, out)
	if n := strings.Count(out, ":latin1.txt:2:"); n != 1 {
		t.Errorf("the hit is reported %d times, want once; output:\n%s", n, out)
	}
}

// 40. git log re-encodes what it prints into i18n.logOutputEncoding, or into
// i18n.commitEncoding when that is unset. In Latin-1, a place that starts
// with an umlaut matches the pattern under neither locale, so the hook reads
// a commit's object as stored, where neither setting reaches. A message whose
// bytes are not valid UTF-8, with no encoding header to say what they are, is
// matched under C as well, and once: a commit with no encoding header is not
// read as converted. The hook runs under C.UTF-8.
func TestPrePushCommitMessageEncoding(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	phone := "0" + "30" + " " + "1234567"
	place := "10115" + " " + "\u00dcbungsstadt"
	for _, c := range []struct{ name, key string }{
		{"i18n.logOutputEncoding", "i18n.logOutputEncoding"},
		{"i18n.commitEncoding alone", "i18n.commitEncoding"},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := ppSeed(t, env)
			r.git("checkout", "-q", "-b", "enc")
			r.write("enc.txt", "a clean line\n")
			tip := r.commit("enc: deliver to " + place + "\n\nTel.:\u00a0" + phone)
			code, out := r.hookNewBranch("enc", tip, "LC_ALL=C.UTF-8",
				"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0="+c.key, "GIT_CONFIG_VALUE_0=ISO-8859-1")
			ppWantRefused(t, code, out, "postcode and place in a commit object: "+tip+":", place,
				"phone number in a commit object: "+tip+":")
			ppWantChecked(t, out)
		})
	}
	t.Run("a byte that is not valid UTF-8", func(t *testing.T) {
		t.Parallel()
		r := ppSeed(t, env)
		id := ppAuthorName + " <" + ccTestAuthorEmail + "> 1767225600 +0000"
		tip := r.rawCommit("tree " + r.git("rev-parse", "HEAD^{tree}") + "\nparent " + r.head() +
			"\nauthor " + id + "\ncommitter " + id + "\n\nraw: a Latin-1 byte\n\nTel.:\xa0" + phone + "\n")
		code, out := r.hookNewBranch("raw", tip, "LC_ALL=C.UTF-8")
		ppWantRefused(t, code, out, "phone number in a commit object: "+tip+":8:Tel.:")
		ppWantChecked(t, out)
		if n := strings.Count(out, "phone number in a commit"); n != 1 {
			t.Errorf("the hit is reported %d times, want once; output:\n%s", n, out)
		}
	})
}

// 41. The identities of the new commits reach check 1 re-encoded the same
// way. A declared name outside ASCII, printed in Latin-1, would no longer
// equal the UTF-8 name the clone declares, and its own commit would be
// refused. A stranger with such a name is still undeclared.
func TestPrePushIdentityEncoding(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	name := "J\u00fcrgen Test"
	r.git("config", "user.name", name)
	r.git("checkout", "-q", "-b", "umlaut")
	r.write("umlaut.txt", "a clean line\n")
	tip := r.commitAs(name, ccTestAuthorEmail, name, ccTestAuthorEmail, "umlaut: a declared name outside ASCII")
	code, out := r.hookNewBranch("umlaut", tip, ppLatin1Log...)
	ppWantPass(t, code, out)
	stranger := "Gr\u00fcn Fremd"
	r.write("stranger.txt", "another clean line\n")
	tip = r.commitAs(stranger, ccTestSomeoneEmail, stranger, ccTestSomeoneEmail, "stranger: an undeclared name outside ASCII")
	code, out = r.hookNewBranch("umlaut", tip, ppLatin1Log...)
	ppWantRefused(t, code, out, "undeclared identity: "+stranger+" <"+ccTestSomeoneEmail+">")
	ppWantChecked(t, out)
}

// 42. A commit's encoding header can name an encoding its bytes are not in:
// with i18n.commitEncoding set to Latin-1 and UTF-8 typed in, git commit
// writes UTF-8 under a Latin-1 header. git log converts it anyway, which
// turns the umlaut of a place into two characters no pattern holds, and a
// declared name into one nobody declared; the commit's object as stored still
// holds them, and check 1 reads its identities both ways. A commit whose
// header is right - Latin-1 bytes under a Latin-1 header - is found through
// git's conversion, which git log --pretty=raw prints with the message
// indented. Each case runs with and without the setting when the hook runs,
// under C.UTF-8.
func TestPrePushMislabelledEncoding(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	place := "10115" + " " + "Übungsstadt"
	name := "Jürgen Test"
	stranger := "Grün Fremd"
	r := ppSeed(t, env)
	r.git("config", "user.name", name)
	// commitOn commits one new file on a new branch off main, with
	// i18n.commitEncoding set to Latin-1 and the given -c settings.
	commitOn := func(branch, file string, settings []string, args ...string) string {
		r.git("checkout", "-q", "-b", branch, "main")
		r.write(file, "a clean line\n")
		r.stage()
		pre := []string{"-c", "i18n.commitEncoding=ISO-8859-1"}
		for _, kv := range settings {
			pre = append(pre, "-c", kv)
		}
		r.git(append(append(pre, "commit", "-q"), args...)...)
		return r.head()
	}
	misPlace := commitOn("misplace", "a.txt", nil, "-m", "misplace: deliver to "+place)
	misName := commitOn("misname", "b.txt", nil, "-m", "misname: a declared name outside ASCII")
	misStranger := commitOn("misstranger", "c.txt", []string{"user.name=" + stranger, "user.email=" + ccTestSomeoneEmail},
		"-m", "misstranger: an undeclared name outside ASCII")
	msgFile := filepath.Join(t.TempDir(), "msg")
	if err := os.WriteFile(msgFile, []byte("right: deliver to 10115 \xdcbungsstadt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rightPlace := commitOn("rightplace", "d.txt", nil, "-F", msgFile)
	// git takes the last of two author lines, so a declared first one must
	// not vouch for the stranger git shows.
	declared := name + " <" + ccTestAuthorEmail + "> 1767225600 +0000"
	other := "Eve Stranger <" + ccTestSomeoneEmail + "> 1767225600 +0000"
	twoAuthors := r.rawCommit("tree " + r.git("rev-parse", "main^{tree}") + "\nparent " + r.git("rev-parse", "main") +
		"\nauthor " + declared + "\nauthor " + other + "\ncommitter " + declared + "\ncommitter " + other +
		"\nencoding ISO-8859-1\n\ntwo: a declared line before a stranger's\n")
	if enc := r.git("log", "-1", "--format=%e", misPlace); enc != "ISO-8859-1" {
		t.Fatalf("the fixture commit's encoding header is %q, want ISO-8859-1", enc)
	}
	for _, c := range []struct {
		name string
		env  []string
	}{
		{"setting unset when the hook runs", []string{"LC_ALL=C.UTF-8"}},
		{"setting still on when the hook runs", []string{"LC_ALL=C.UTF-8",
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=i18n.commitEncoding", "GIT_CONFIG_VALUE_0=ISO-8859-1"}},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := r.with(t)
			code, out := r.hookNewBranch("misplace", misPlace, c.env...)
			ppWantRefused(t, code, out, "postcode and place in a commit object: "+misPlace+":", "misplace: deliver to "+place)
			ppWantChecked(t, out)
			code, out = r.hookNewBranch("misname", misName, c.env...)
			ppWantPass(t, code, out)
			code, out = r.hookNewBranch("misstranger", misStranger, c.env...)
			ppWantRefused(t, code, out, "undeclared identity: ", " <"+ccTestSomeoneEmail+">")
			ppWantChecked(t, out)
			code, out = r.hookNewBranch("rightplace", rightPlace, c.env...)
			ppWantRefused(t, code, out, "postcode and place in a commit converted to UTF-8: "+rightPlace+":7:    right: deliver to "+place)
			ppWantChecked(t, out)
			code, out = r.hookNewBranch("two", twoAuthors, c.env...)
			ppWantRefused(t, code, out, "undeclared identity: Eve Stranger <"+ccTestSomeoneEmail+">")
			ppWantChecked(t, out)
		})
	}
}

// 43. A tag object is read under C by sed as well as by grep: in a UTF-8
// locale GNU sed's .* stops at a byte that is not valid UTF-8, and a declared
// tagger whose name is stored in Latin-1 was a check that could not run.
func TestPrePushTaggerLatin1Name(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	name := "J\xfcrgen Test"
	r.git("config", "user.name", name)
	oid := r.rawTag("object " + r.head() + "\ntype commit\ntag latin1-tagger\ntagger " + name + " <" + ccTestAuthorEmail +
		"> 1767225600 +0000\n\na clean tag\n")
	code, out := r.hookNewTag("latin1-tagger", oid, "LC_ALL=C.UTF-8")
	ppWantPass(t, code, out)
	// The same tag object under two refs, the first named in Latin-1: the
	// second finds the object through the tag list, which holds that name.
	refs := "refs/tags/x\xfc " + oid + " refs/tags/x\xfc " + r.zero + "\n" + r.tagLine("latin1-tagger", oid)
	code, out = r.hook([]string{"origin", r.origin}, refs, "LC_ALL=C.UTF-8")
	ppWantPass(t, code, out)
	stranger := r.rawTag("object " + r.head() + "\ntype commit\ntag latin1-stranger\ntagger Gr\xfcn Fremd <" + ccTestSomeoneEmail +
		"> 1767225600 +0000\n\na clean tag\n")
	code, out = r.hookNewTag("latin1-stranger", stranger, "LC_ALL=C.UTF-8")
	ppWantRefused(t, code, out, "undeclared identity: ", ", tagger of tag latin1-stranger")
	ppWantChecked(t, out)
	if strings.Contains(out, "address in tag") {
		t.Errorf("the tagger's identity was read as a shape; output:\n%s", out)
	}
}

// 44. Under C a bracket expression reads a pattern's umlauts byte by byte:
// five digits before a word that starts with a lowercase umlaut matched the
// postcode pattern, and a place that starts with a capital one did not. The
// pattern spells each umlaut out as an alternative, which C reads as the same
// character a UTF-8 locale does. Each case runs under C.UTF-8 and under C.
func TestPrePushPostcodeUmlauts(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	r.git("checkout", "-q", "-b", "count", "main")
	r.write("count.txt", "head\nnur "+"12345"+" "+"übrig\n")
	countTip := r.commit("count: a number before a word")
	r.git("checkout", "-q", "-b", "place", "main")
	r.write("place.txt", "head\ndeliver to "+"10115"+" "+"Übungsstadt\n")
	placeTip := r.commit("place: a place with a capital umlaut")
	for _, lc := range []string{"LC_ALL=C.UTF-8", "LC_ALL=C"} {
		code, out := r.hookNewBranch("count", countTip, lc)
		ppWantPass(t, code, out)
		code, out = r.hookNewBranch("place", placeTip, lc)
		ppWantRefused(t, code, out, "postcode and place: "+placeTip+":place.txt:2:")
		ppWantChecked(t, out)
	}
}

// ppRawHead is the header of a commit object on top of r's HEAD, author and
// committer the declared identity, for rawCommit to take more lines after.
func (r *ppRepo) ppRawHead() string {
	r.t.Helper()
	id := ppAuthorName + " <" + ccTestAuthorEmail + "> 1767225600 +0000"
	return "tree " + r.git("rev-parse", "HEAD^{tree}") + "\nparent " + r.head() + "\nauthor " + id + "\ncommitter " + id + "\n"
}

// 45. git log stops at a NUL byte in a commit message. git's own commands
// write none, but a commit object made by hand can hold one, and a push
// publishes it: the shape after it is read from the object as stored -
// after a NUL in the body, in the subject, and on a line of its own.
func TestPrePushCommitObjectNUL(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	shape := addr("after.nul", "example.invalid")
	for _, c := range []struct {
		name, message, want string
	}{
		{"body", "nul: a clean subject\n\nbody before\x00 after " + shape + "\n", ":8:body before\x01 after " + shape},
		{"subject", "nul\x00 " + shape + "\n", ":6:nul\x01 " + shape},
		{"a line after a NUL line", "nul: a clean subject\n\n\x00\n" + shape + "\n", ":9:" + shape},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := r.with(t)
			oid := r.rawCommit(r.ppRawHead() + "\n" + c.message)
			if strings.Contains(r.git("log", "-1", "--format=%B", oid), shape) {
				t.Fatalf("git log shows the shape after the NUL, so this case proves nothing")
			}
			code, out := r.hookNewBranch("nul", oid)
			ppWantRefused(t, code, out, "address in a commit object: "+oid+c.want)
			ppWantChecked(t, out)
		})
	}
}

// 46. A header line git does not know is published with the commit - a
// local bare repository took one after the committer even with
// receive.fsckObjects on - and git log shows none of it. Every header line
// is read, but for the author's and the committer's identity, as git splits
// it: up to the first < and on to the first > after it. Whatever follows on
// the line is read, a second address included. That identity is left out
// only where check 1 has read it: on the one line of its role, and before
// any NUL. A clean header line of git's own or not passes.
func TestPrePushCommitObjectHeader(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	me := ppAuthorName + " <" + ccTestAuthorEmail + ">"
	date := " 1767225600 +0000"
	shape := addr("extra.header", "example.invalid")
	second := addr("second.address", "example.invalid")
	tree := r.git("rev-parse", "HEAD^{tree}")
	parent := "tree " + tree + "\nparent " + r.head() + "\n"
	for _, c := range []struct {
		name, object string
		wants        []string
	}{
		{"extra header line", r.ppRawHead() + "x-note mail " + shape + "\n\nhdr: a clean subject\n",
			[]string{":5:x-note mail " + shape}},
		{"no empty line", r.ppRawHead() + "x-note mail " + shape,
			[]string{":5:x-note mail " + shape}},
		{"after the identity", parent + "author " + me + " " + ppPostcode + "\ncommitter " + me + date + "\n\nafter: a clean subject\n",
			[]string{"postcode and place in a commit object: ", ":3:author " + ppPostcode}},
		{"a second address", parent + "author " + me + " <" + second + ">" + date + "\ncommitter " + me + date + "\n\nsecond: a clean subject\n",
			[]string{":3:author <" + second + ">" + date}},
		{"two author lines", parent + "author " + me + date + "\nauthor " + me + date + "\ncommitter " + me + date + "\n\ntwo: a clean subject\n",
			[]string{":3:author " + me, ":4:author " + me}},
		{"a NUL before the identity", parent + "x-n\x00ul\nauthor " + me + date + "\ncommitter " + me + date + "\n\nnul: a clean subject\n",
			[]string{":4:author " + me, ":5:committer " + me}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := r.with(t)
			oid := r.rawCommit(c.object)
			code, out := r.hookNewBranch("hdr", oid)
			wants := []string{"in a commit object: " + oid + ":"}
			ppWantRefused(t, code, out, append(wants, c.wants...)...)
			ppWantChecked(t, out)
			if strings.Contains(out, "undeclared identity") {
				t.Errorf("check 1 read an identity other than the declared one; output:\n%s", out)
			}
		})
	}
	t.Run("a clean extra header line", func(t *testing.T) {
		r := r.with(t)
		oid := r.rawCommit(r.ppRawHead() + "x-note a clean note\n\nclean: a clean subject\n")
		code, out := r.hookNewBranch("clean", oid)
		ppWantPass(t, code, out)
	})
	// git log converts the header from the encoding it names, and in UTF-7
	// +AAo- is a newline: one author line as stored is two to git, the
	// declared one last, so check 1 sees only that. As stored, the line
	// holds no identity that is declared, and none is left out.
	t.Run("a header that conversion splits", func(t *testing.T) {
		r := r.with(t)
		hidden := addr("utf7.hidden", "example.invalid")
		oid := r.rawCommit(parent + "author " + hidden + "+AAo-author " + me + " 1767225600 -0100\ncommitter " + me +
			" 1767225600 -0100\nencoding UTF-7\n\nutf7: a clean subject\n")
		if got := r.git("log", "-1", "--encoding=UTF-8", "--format=%an <%ae>", oid); got != me {
			t.Skipf("git shows the author as %q here, not as the declared identity, so the case proves nothing", got)
		}
		code, out := r.hookNewBranch("utf7", oid)
		ppWantRefused(t, code, out, "address in a commit object: "+oid+":3:author "+hidden,
			"unread encoding: commit "+oid+" names the encoding \"UTF-7\"")
		ppWantChecked(t, out)
	})
}

// ppLatin1Locale returns the name of a locale whose character set is
// ISO-8859-1, or "" when there is none here.
func ppLatin1Locale() string {
	for _, l := range []string{"de_DE.ISO-8859-1", "de_DE.iso88591", "en_US.ISO-8859-1", "en_US.iso88591"} {
		cmd := exec.Command("locale", "charmap")
		cmd.Env = []string{"LC_ALL=" + l, "PATH=" + os.Getenv("PATH")}
		if out, err := cmd.Output(); err == nil && strings.TrimSpace(string(out)) == "ISO-8859-1" {
			return l
		}
	}
	return ""
}

// 47. An encoding header that names no encoding: git takes the locale's
// character set for it, and under a Latin-1 locale it turns a UTF-8 message
// into one no pattern matches (measured with git 2.55.0, 2026-10-02). The
// object as stored still holds the message as written. Under C.UTF-8 git
// leaves it as it is; under a Latin-1 locale this case needs one installed,
// and is skipped, saying so, where there is none.
func TestPrePushEmptyEncodingHeader(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	place := "10115" + " " + "Übungsstadt"
	oid := r.rawCommit(r.ppRawHead() + "encoding \n\nenc: deliver to " + place + "\n")
	run := func(t *testing.T, locale string) {
		r := r.with(t)
		code, out := r.hookNewBranch("enc", oid, "LC_ALL="+locale)
		ppWantRefused(t, code, out, "postcode and place in a commit object: "+oid+":7:enc: deliver to "+place)
		ppWantChecked(t, out)
	}
	t.Run("C.UTF-8", func(t *testing.T) { run(t, "C.UTF-8") })
	t.Run("Latin-1", func(t *testing.T) {
		l := ppLatin1Locale()
		if l == "" {
			t.Skip("no locale with the ISO-8859-1 character set here, so git's conversion from it is not exercised")
		}
		cmd := exec.Command("git", "-C", r.dir, "log", "-1", "--encoding=UTF-8", "--format=%B", oid)
		cmd.Env = ccIsolatedGitEnv("LC_ALL=" + l)
		got, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), place) {
			t.Skip("git did not convert from " + l + "'s character set here, so the case proves nothing")
		}
		run(t, l)
		// A declared name outside ASCII under the same header: git log
		// turns it into one nobody declared, and check 1 reads the
		// identity as stored as well, since the header names no UTF-8.
		r := r.with(t)
		name := "J\u00fcrgen Test"
		id := name + " <" + ccTestAuthorEmail + "> 1767225600 +0000"
		named := r.rawCommit("tree " + r.git("rev-parse", "HEAD^{tree}") + "\nparent " + r.head() + "\nauthor " + id +
			"\ncommitter " + id + "\nencoding \n\nnamed: a clean subject\n")
		code, out := r.hookNewBranch("named", named, "LC_ALL="+l,
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=imprint.allowedIdentity", "GIT_CONFIG_VALUE_0="+name+" <"+ccTestAuthorEmail+">")
		ppWantPass(t, code, out)
	})
}

// ppIBANLine is a line made only of base64 characters that matches the IBAN
// pattern, as one in about 400 RSA-4096 signatures holds by chance.
var ppIBANLine = "DE" + "89" + "3704" + "0044" + "0532" + "0130" + "00"

// ppArmour is an armoured signature block, each line after the first
// starting with prefix, as git stores one in a header; extra goes in before
// the base64 lines.
func ppArmour(prefix, extra string) string {
	b64 := strings.Repeat("Ab+/", 16)
	return "-----BEGIN PGP SIGNATURE-----\n" + prefix + extra + "\n" + prefix + b64 + "\n" +
		prefix + ppIBANLine + "\n" + prefix + "=Ab+/\n" + prefix + "-----END PGP SIGNATURE-----\n"
}

// 48. A signed commit carries its signature in a gpgsig header. Its base64
// lines are encoded bytes, in which no shape can be read, but a run of their
// letters and digits can take the IBAN pattern's form: inside the armour,
// such a line is not matched. Every other line of the header is - an armour
// header such as Comment:, a line after the armour - and the same line in a
// header of another name is a finding.
func TestPrePushGpgsigHeader(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	comment := addr("armour.comment", "example.invalid")
	for _, c := range []struct {
		name, header string
		wants        []string // nil: the push passes
	}{
		{"gpgsig", "gpgsig " + ppArmour(" ", ""), nil},
		{"gpgsig-sha256", "gpgsig-sha256 " + ppArmour(" ", ""), nil},
		{"an armour header", "gpgsig " + ppArmour(" ", "Comment: "+comment), []string{"address in a commit object: ", ":6: Comment: " + comment}},
		{"after the armour", "gpgsig " + ppArmour(" ", "") + " " + ppIBANLine + "\n", []string{"IBAN in a commit object: ", ":11: " + ppIBANLine}},
		{"another header", "x-note " + ppArmour(" ", ""), []string{"IBAN in a commit object: ", ":8: " + ppIBANLine}},
		{"an armour that is no signature", "gpgsig -----BEGIN NOTES-----\n " + ppIBANLine + "\n -----END NOTES-----\n",
			[]string{"IBAN in a commit object: ", ":6: " + ppIBANLine}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := r.with(t)
			oid := r.rawCommit(r.ppRawHead() + c.header + "\nsig: " + c.name + "\n")
			code, out := r.hookNewBranch("sig", oid)
			if c.wants == nil {
				ppWantPass(t, code, out)
				return
			}
			ppWantRefused(t, code, out, c.wants...)
			ppWantChecked(t, out)
		})
	}
}

// ppSignedLookingTag writes a tag object on target whose message ends in an
// armoured block, which is all git looks for before it records a merge of
// the tag in a mergetag header, and points refs/tags/NAME at it. An empty
// tagger leaves the tagger line out.
func (r *ppRepo) ppSignedLookingTag(name, target, tagger, message string) string {
	r.t.Helper()
	head := "object " + target + "\ntype commit\ntag " + name + "\n"
	if tagger != "" {
		head += "tagger " + tagger + " 1767225600 +0000\n"
	}
	oid := r.rawTag(head + "\n" + message + "\n" + ppArmour("", ""))
	r.git("update-ref", "refs/tags/"+name, oid)
	return oid
}

// ppMergeTag merges the tag NAME into a new branch off main with git merge,
// which copies the tag into the merge's mergetag header, and returns the
// merge. The message is given: with --no-edit, git also writes the tag's
// signature and gpg's report on it into the message, as lines that start
// with "#" (measured with git 2.55.0, 2026-10-02), and those are a message.
func (r *ppRepo) ppMergeTag(name string) string {
	r.t.Helper()
	r.git("checkout", "-q", "-b", "merge-"+name, "main")
	r.git("merge", "-q", "--no-ff", "-m", "merge: tag "+name, name)
	if !strings.Contains(r.git("cat-file", "commit", "HEAD"), "\nmergetag object ") {
		r.t.Fatalf("git merge %s recorded no mergetag header", name)
	}
	return r.head()
}

// 49. A merge of a signed tag carries the tag in its mergetag header, read as
// the tag check reads a tag: its tagger has to be a declared identity, and is
// left out of the shapes, everything up to the last ">" on the line, as
// written; the rest is matched, but for the base64 lines of the signature git
// would find, the last one. A line of the message that starts with "tagger "
// is text. A tagger line with no ">" is a check that could not run, and a
// merged tag with no tagger brings no identity along.
func TestPrePushMergetagHeader(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	me := ppAuthorName + " <" + ccTestAuthorEmail + ">"
	stranger := "Someone Else <" + ccTestSomeoneEmail + ">"
	shape := addr("merged.tag", "example.invalid")
	r.git("checkout", "-q", "-b", "side", "main")
	r.write("side.txt", "a clean line\n")
	side := r.commit("side: one clean commit")
	r.ppSignedLookingTag("t-declared", side, me, "a clean tag")
	r.ppSignedLookingTag("t-stranger", side, stranger, "a clean tag by a stranger")
	r.ppSignedLookingTag("t-shape", side, me, "release, contact "+shape)
	declared := r.ppMergeTag("t-declared")
	strangerMerge := r.ppMergeTag("t-stranger")
	shapeMerge := r.ppMergeTag("t-shape")

	code, out := r.hookNewBranch("merge-t-declared", declared)
	ppWantPass(t, code, out)
	if !strings.Contains(out, "1 tagger(s) of merged tags, 0 undeclared") {
		t.Errorf("the merged tag's tagger was not checked; output:\n%s", out)
	}
	code, out = r.hookNewBranch("merge-t-stranger", strangerMerge)
	ppWantRefused(t, code, out, "undeclared identity: "+stranger+", tagger of the tag commit "+strangerMerge+" merges")
	ppWantChecked(t, out)
	if strings.Contains(out, "in a commit object") {
		t.Errorf("the merged tag's tagger was read as a shape; output:\n%s", out)
	}
	code, out = r.hookNewBranch("merge-t-shape", shapeMerge)
	ppWantRefused(t, code, out, "address in a commit object: "+shapeMerge+":", " release, contact "+shape)
	ppWantChecked(t, out)

	// Unlike ppIBANLine, which the armour every tag here ends in holds too,
	// so a finding for it says which armour was read.
	fakeIBAN := "DE" + "02" + "1203" + "0000" + "0000" + "2020" + "51"
	for _, c := range []struct {
		name, tagger, message string
		wants                 []string // nil: the push passes
	}{
		{"t-before", me, "release notes\n" + ppIBANLine, []string{"IBAN in a commit object: ", " " + ppIBANLine}},
		{"t-fake", me, "-----BEGIN PGP SIGNATURE-----\n" + fakeIBAN + "\n-----END PGP SIGNATURE-----\nmore notes",
			[]string{"IBAN in a commit object: ", " " + fakeIBAN}},
		{"t-taggerline", me, "tagger " + stranger, []string{"address in a commit object: ", " tagger " + stranger}},
		{"t-twoaddr", me + " <" + shape + ">", "a clean tag", []string{"undeclared identity: " + me + " <" + shape + ">, tagger of the tag commit "}},
		{"t-tab", "\t" + me, "a clean tag", []string{"undeclared identity: \t" + me + ", tagger of the tag commit "}},
		{"t-none", "", "a clean tag with no tagger", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := r.with(t)
			r.ppSignedLookingTag(c.name, side, c.tagger, c.message)
			merge := r.ppMergeTag(c.name)
			code, out := r.hookNewBranch("merge-"+c.name, merge)
			if c.wants == nil {
				ppWantPass(t, code, out)
				return
			}
			ppWantRefused(t, code, out, c.wants...)
			ppWantChecked(t, out)
			if n := strings.Count(out, "IBAN in a commit object"); n > 1 {
				t.Errorf("the IBAN-shaped line is reported %d times, want once: the signature git finds holds one too; output:\n%s", n, out)
			}
		})
	}

	// A merge written under a Latin-1 header, of a tag whose tagger is the
	// declared name outside ASCII: the tag holds it in UTF-8, git log turns
	// it into one nobody declared, and the tagger as stored vouches for it.
	t.Run("t-latin1", func(t *testing.T) {
		r := r.with(t)
		name := "J\u00f6rg T\u00e4ster"
		r.ppSignedLookingTag("t-latin1", side, name+" <"+ccTestAuthorEmail+">", "a clean tag")
		r.git("checkout", "-q", "-b", "merge-t-latin1", "main")
		r.git("-c", "i18n.commitEncoding=ISO-8859-1", "merge", "-q", "--no-ff", "-m", "merge: tag t-latin1", "t-latin1")
		merge := r.head()
		if enc := r.git("log", "-1", "--format=%e", merge); enc != "ISO-8859-1" {
			t.Fatalf("the merge's encoding header is %q, want ISO-8859-1", enc)
		}
		code, out := r.hookNewBranch("merge-t-latin1", merge,
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=imprint.allowedIdentity", "GIT_CONFIG_VALUE_0="+name+" <"+ccTestAuthorEmail+">")
		ppWantPass(t, code, out)
	})

	// A tagger line that ends no identity in ">", made by hand.
	r.git("checkout", "-q", "main")
	mt := "mergetag object " + side + "\n type commit\n tag t-broken\n tagger nobody at all\n \n a clean tag\n"
	oid := r.rawCommit("tree " + r.git("rev-parse", "main^{tree}") + "\nparent " + r.head() + "\nparent " + side +
		"\nauthor " + me + " 1767225600 +0000\ncommitter " + me + " 1767225600 +0000\n" + mt + "\nbroken: merge\n")
	code, out = r.hookNewBranch("broken", oid)
	ppWantRefused(t, code, out, "check(s) could not run", "could not read the tagger of the tag commit "+oid+" merges")
}

// 50. With log.showSignature on, git log prints what verifying a signature
// says among its own lines - "No signature" for an SSH one it cannot read -
// and check 1 read those as identities. The hook asks git log not to, and
// counts two identity lines a commit: a git that prints one more is refused.
func TestPrePushShowSignature(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	armour := "-----BEGIN SSH SIGNATURE-----\n \n " + strings.Repeat("Ab+/", 16) + "\n -----END SSH SIGNATURE-----\n"
	oid := r.rawCommit(r.ppRawHead() + "gpgsig " + armour + "\nsigned: a clean subject\n")
	cmd := exec.Command("git", "-C", r.dir, "-c", "log.showSignature=true", "log", "-1", "--format=%H", oid)
	cmd.Env = ccIsolatedGitEnv()
	got, _ := cmd.Output()
	if strings.Count(string(got), "\n") < 2 {
		t.Skipf("git printed nothing for the signature here (%q), so the setting is not exercised", got)
	}
	code, out := r.hookNewBranch("signed", oid, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=log.showSignature", "GIT_CONFIG_VALUE_0=true")
	ppWantPass(t, code, out)
	gitBin := ppRealBin(t, "git")
	shim := ppShimPATH(t, "git", "case \" $* \" in *\" --format=%H A \"*) '"+gitBin+"' \"$@\"; rc=$?; echo extra; exit $rc ;; esac\nexec '"+gitBin+"' \"$@\"\n")
	code, out = r.hookNewBranch("signed", oid, shim)
	ppWantRefused(t, code, out, "git log listed 3 identity line(s) for 1 new commit(s), not two each.")

	// A message read as converted goes through git log as well: a commit
	// signed with a real SSH key whose header names Latin-1, and a signer
	// git knows, make git print "Good ... signature for" the signer's
	// principal, an address, among the message's lines.
	t.Run("a message read as converted", func(t *testing.T) {
		if _, err := exec.LookPath("ssh-keygen"); err != nil {
			t.Skip("no ssh-keygen here, so no commit can be signed: " + err.Error())
		}
		r := ppSeed(t, env)
		dir := t.TempDir()
		key := filepath.Join(dir, "key")
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "test", "-f", key).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen: %v\n%s", err, out)
		}
		pub, err := os.ReadFile(key + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		signers := filepath.Join(dir, "allowed_signers")
		if err := os.WriteFile(signers, []byte(ccTestAuthorEmail+" "+string(pub)), 0o644); err != nil {
			t.Fatal(err)
		}
		r.git("checkout", "-q", "-b", "sshsigned")
		r.write("signed.txt", "a clean line\n")
		r.stage()
		r.git("-c", "gpg.format=ssh", "-c", "user.signingkey="+key, "-c", "i18n.commitEncoding=ISO-8859-1",
			"commit", "-q", "-S", "-m", "sshsigned: a clean subject")
		tip := r.head()
		show := []string{"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=log.showSignature", "GIT_CONFIG_VALUE_0=true",
			"GIT_CONFIG_KEY_1=gpg.ssh.allowedSignersFile", "GIT_CONFIG_VALUE_1=" + signers}
		cmd := exec.Command("git", "-C", r.dir, "log", "-1", "--encoding=UTF-8", "--format=%B", tip)
		cmd.Env = ccIsolatedGitEnv(show...)
		got, _ := cmd.Output()
		if !strings.Contains(string(got), ccTestAuthorEmail) {
			t.Skipf("git printed no signer for the signature here (%q), so the setting is not exercised", got)
		}
		code, out := r.hookNewBranch("sshsigned", tip, show...)
		ppWantPass(t, code, out)
	})
}

// 51. The lists the commit objects are read into are counted, as the added
// lines are (11d): an awk that loses a line of the text, of its locations or
// of the merged taggers without saying so, or reports no count, stops the
// push before any check reads them - for the objects as stored and for a
// commit read as converted. IMPRINT_PUSH_ANYWAY does not reach that far.
func TestPrePushCommitObjectCounts(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	awkBin := ppRealBin(t, "awk")
	r := ppSeed(t, env)
	me := ppAuthorName + " <" + ccTestAuthorEmail + ">"
	plain := r.rawCommit(r.ppRawHead() + "\nplain: a clean subject\n")
	latin1 := r.rawCommit(r.ppRawHead() + "encoding ISO-8859-1\n\nlatin1: a clean subject\n")
	r.git("checkout", "-q", "-b", "side", "main")
	r.write("side.txt", "a clean line\n")
	side := r.commit("side: one clean commit")
	r.ppSignedLookingTag("t-count", side, me, "a clean tag")
	merge := r.ppMergeTag("t-count")
	// after runs once the real awk has, when cond holds.
	shim := func(t *testing.T, cond, after string) string {
		return ppShimPATH(t, "awk", "'"+awkBin+"' \"$@\"; rc=$?\nif "+cond+"; then "+after+"; fi\nexit $rc\n")
	}
	dropLast := func(v string) string {
		return "sed '$d' \"$" + v + "\" >\"$" + v + ".cut\" && mv \"$" + v + ".cut\" \"$" + v + "\""
	}
	objects := `[ -n "${IMPRINT_COMMITLOC:-}" ]`
	converted := `case "${IMPRINT_COMMITLOC:-}" in */convloc) true ;; *) false ;; esac`
	// git listing the identities out of step with the commits: each
	// commit's committer line before its author line, or the committer
	// line named for another commit.
	gitBin := ppRealBin(t, "git")
	for _, c := range []struct{ name, prog string }{
		{"identities swapped", "NR % 2 == 1 { a = $0; next } { print; print a }"},
		{"a committer line for another commit", "NR % 2 == 0 { sub(/^[0-9a-f]+/, \"0\") } { print }"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := r.with(t)
			s := ppShimPATH(t, "git", "case \" $* \" in *\" --format=%H A \"*) '"+gitBin+"' \"$@\" | '"+awkBin+"' '"+c.prog+"'; exit ;; esac\nexec '"+gitBin+"' \"$@\"\n")
			code, out := r.hookNewBranch("count", plain, s)
			ppWantRefused(t, code, out, "the identities git listed are out of step at commit "+plain)
		})
	}
	for _, c := range []struct {
		name, tip, cond, after, want string
	}{
		{"a location lost", plain, objects, dropLast("IMPRINT_COMMITLOC"), " line(s) of the new commits' objects in commitloc."},
		{"no count", plain, objects, `echo x >"$IMPRINT_OBJCNT"`, "awk reported no count of what it read from commit " + plain},
		{"a merged tagger lost", merge, objects + ` && [ -s "$IMPRINT_MTTAGGERS" ]`, `: >"$IMPRINT_MTTAGGERS"`,
			"wrote down 0 of 1 line(s) of the new commits' objects in mttaggers."},
		{"a converted location lost", latin1, converted, dropLast("IMPRINT_COMMITLOC"), " line(s) of the new commits' objects in convloc."},
		{"a line of text lost", plain, objects, `d="${IMPRINT_COMMITLOC%/*}"; case "$IMPRINT_COMMITLOC" in */commitloc) sed '$d' "$d/committext" >"$d/committext.cut" && mv "$d/committext.cut" "$d/committext" ;; esac`,
			" line(s) of the new commits' objects in committext."},
		{"a converted line of text lost", latin1, converted, `d="${IMPRINT_COMMITLOC%/*}"; sed '$d' "$d/convtext" >"$d/convtext.cut" && mv "$d/convtext.cut" "$d/convtext"`,
			" line(s) of the new commits' objects in convtext."},
		{"no converted count", latin1, converted, `echo x >"$IMPRINT_OBJCNT"`, "awk reported no count of what it read from commit " + latin1 + " as converted."},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := r.with(t)
			s := shim(t, c.cond, c.after)
			for _, e := range [][]string{{s}, {s, "IMPRINT_PUSH_ANYWAY=only a test"}} {
				code, out := r.hookNewBranch("count", c.tip, e...)
				ppWantRefused(t, code, out, c.want)
			}
		})
	}
}

// ppUTF7 encodes text as one UTF-7 shifted run: "+", the UTF-16BE bytes in
// base64 without padding, "-".
func ppUTF7(text string) string {
	var b []byte
	for _, r := range text {
		b = append(b, byte(r>>8), byte(r))
	}
	return "+" + base64.RawStdEncoding.EncodeToString(b) + "-"
}

// 52. A header that names an encoding other than UTF-8 is converted by git
// log before it reads the identities, and a conversion can make lines the
// object as stored does not hold: in UTF-7, an extra header line held an
// author line of its own once converted, and git showed that one. The
// stored reading of a declared author vouches only for an identity git shows
// that differs from it in nothing but characters outside ASCII, so a
// stranger is undeclared. A shape git shows only in the converted header is
// found in the commit as git log --pretty=raw prints it. A name stored in
// Latin-1 under a Latin-1 header is still declared, and its identity left
// out of the shapes.
func TestPrePushConvertedIdentity(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	me := ppAuthorName + " <" + ccTestAuthorEmail + ">"
	stranger := "Evil Stranger <" + addr("utf7.author", "example.invalid") + ">"
	date := " 1767225600 -0100"
	head := "tree " + r.git("rev-parse", "HEAD^{tree}") + "\nparent " + r.head() + "\n"
	t.Run("a line the conversion makes", func(t *testing.T) {
		r := r.with(t)
		oid := r.rawCommit(head + "author " + me + date + "\ncommitter " + me + date + "\nx-junk " + ppUTF7("\nauthor "+stranger+date) +
			"\nencoding UTF-7\n\nutf7: a clean subject\n")
		if got := r.git("log", "-1", "--encoding=UTF-8", "--format=%an <%ae>", oid); got != stranger {
			t.Skipf("git shows the author as %q here, not as the converted line, so the case proves nothing", got)
		}
		code, out := r.hookNewBranch("utf7", oid)
		ppWantRefused(t, code, out, "undeclared identity: "+stranger, "unread encoding: commit "+oid)
		ppWantChecked(t, out)
	})
	// A shape git shows only once it has converted the header: in an extra
	// line, after the committer's identity, and on a line of the signature
	// that is base64 as stored and a phone number to git. git log
	// --pretty=raw starts with a "commit" line, so each line is one further
	// down than in the object.
	phone := ppUTF7("0" + "30" + " " + "1234567")
	for _, c := range []struct{ name, header, want string }{
		{"in an extra line", "committer " + me + date + "\nx-note call " + phone + "\n", ":6:x-note call 0"},
		{"after the identity", "committer " + me + date + " call " + phone + "\n", ":5:committer" + date + " call 0"},
		{"in the armour", "committer " + me + date + "\ngpgsig -----BEGIN SSH SIGNATURE-----\n " + strings.TrimSuffix(phone, "-") +
			"\n -----END SSH SIGNATURE-----\n", ":7: 0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := r.with(t)
			oid := r.rawCommit(head + "author " + me + date + "\n" + c.header + "encoding UTF-7\n\nutf7: " + c.name + "\n")
			cmd := exec.Command("git", "-C", r.dir, "log", "-1", "--encoding=UTF-8", "--pretty=raw", oid)
			cmd.Env = ccIsolatedGitEnv()
			got, err := cmd.Output()
			if err != nil || !strings.Contains(string(got), "0"+"30"+" "+"1234567") {
				t.Skipf("git does not show the converted phone number here (%v, %q), so the case proves nothing", err, got)
			}
			code, out := r.hookNewBranch("utf7h", oid)
			ppWantRefused(t, code, out, "phone number in a commit converted to UTF-8: "+oid+c.want, "unread encoding: commit "+oid)
			ppWantChecked(t, out)
		})
	}
	t.Run("a name stored in Latin-1", func(t *testing.T) {
		r := r.with(t)
		name := "J\u00f6rg T\u00e4ster"
		stored := "J\xf6rg T\xe4ster <" + ccTestAuthorEmail + ">"
		oid := r.rawCommit(head + "author " + stored + date + "\ncommitter " + stored + date +
			"\nencoding ISO-8859-1\n\nlatin1: a clean subject\n")
		code, out := r.hookNewBranch("latin1", oid, "LC_ALL=C.UTF-8",
			"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=imprint.allowedIdentity", "GIT_CONFIG_VALUE_0="+name+" <"+ccTestAuthorEmail+">")
		ppWantPass(t, code, out)
	})
}

// 53. A commit whose header names an encoding other than UTF-8 is read as
// stored and as git converts it. That covers what git shows only where a
// conversion makes an ASCII character from the same byte alone, and never a
// newline or a NUL; any other encoding - UTF-7, UTF-16, EBCDIC - is a
// finding. The commits here hold ASCII alone, so every encoding reads them
// the same, and only the name decides.
func TestPrePushUnreadEncoding(t *testing.T) {
	env := ppSetup(t)
	t.Parallel()
	r := ppSeed(t, env)
	id := ppAuthorName + " <" + ccTestAuthorEmail + "> 1767225600 -0100"
	head := "tree " + r.git("rev-parse", "HEAD^{tree}") + "\nparent " + r.head() + "\nauthor " + id + "\ncommitter " + id + "\n"
	for _, enc := range []string{"ISO-8859-1", "iso8859-15", "latin1", "utf8", "", "windows-1252", "CP1251", "KOI8-R",
		"EUC-JP", "GBK", "GB18030", "Big5", "Shift_JIS", "CP932"} {
		t.Run("reads "+enc, func(t *testing.T) {
			r := r.with(t)
			oid := r.rawCommit(head + "encoding " + enc + "\n\nreadable: " + enc + "\n")
			code, out := r.hookNewBranch("enc", oid)
			ppWantPass(t, code, out)
		})
	}
	for _, enc := range []string{"UTF-7", "UTF-16BE", "IBM037", "ISO-2022-JP"} {
		t.Run("does not read "+enc, func(t *testing.T) {
			r := r.with(t)
			oid := r.rawCommit(head + "encoding " + enc + "\n\nunread: " + enc + "\n")
			code, out := r.hookNewBranch("enc", oid)
			ppWantRefused(t, code, out, "unread encoding: commit "+oid+" names the encoding \""+enc+"\", which this hook does not read in full")
		})
	}
	// An escaped NUL ends git's converted text, and what follows it is
	// read nowhere but here.
	t.Run("an escaped NUL", func(t *testing.T) {
		r := r.with(t)
		oid := r.rawCommit(head + "encoding UTF-7\n\nnul: x+AAA-" + ppUTF7("call "+"0"+"30"+" "+"1234567") + "\n")
		code, out := r.hookNewBranch("enc", oid)
		ppWantRefused(t, code, out, "unread encoding: commit "+oid)
	})
}
