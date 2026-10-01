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
		{"a shape loop cut short", func(t *testing.T) string {
			// A grep that empties the shape list the loops read, while the
			// first loop is on its first shape.
			return ppShimPATH(t, "grep", "for a; do last=$a; done\ncase \"${last:-}\" in */added) : >\"${last%/added}/shapes\" ;; esac\nexec '"+grepBin+"' \"$@\"\n")
		}, []string{"ran 1 of ", " shape(s) over the added lines", "ran 0 of ", " shape(s) over the commit messages"}},
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
	t.Run("awk fails", func(t *testing.T) {
		r, tip := brokenTip(t, "no.awk")
		wantCouldNotRun(t, r, tip, []string{ppShimPATH(t, "awk", "exit 2\n")})
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
// the push. The TERM comes during the last git call the hook makes, the one
// that reads the messages: a trap that removed the files and returned would
// let the hook run on without them, to whatever exit that happened to reach.
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
			return ppShimPATH(t, "git", "case \" $* \" in *\" --format=%h|%s%n%b \"*) kill -TERM \"$PPID\" ;; esac\nexec '"+gitBin+"' \"$@\"\n")
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
