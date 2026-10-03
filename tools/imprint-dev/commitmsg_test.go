package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the real .githooks/commit-msg and the real
// tools/typesafe/bin/ts-commit-check behind it - like prepush_test.go, an
// exception to the rule that no test reads the repository. git itself runs the
// hook (core.hooksPath), in throwaway repositories, the way it does in a clone
// that opted in. No TypeSafe key reaches ts-commit-check: the environment is
// built from scratch without TYPESAFE_API_KEY, and a secret-tool that finds
// nothing comes first on PATH, so only the local alarm decides and nothing
// leaves the machine. Without git, sh or python3 on PATH the tests are
// skipped, and say so.
//
// The leak-shaped values are built at runtime, never written as one literal:
// this file is itself committed through the hook it tests.

var (
	cmShapeA    = "auth: \"" + "ghp_" + "4f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e" + "\""
	cmShapeB    = "auth: \"" + "ghp_" + "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f" + "\""
	cmShapeName = "ghp_" + "4f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e" + ".txt"
)

// cmNoOwnLines is what ts-commit-check prints for a merge that adds no line
// new against every parent: proof the merge mode ran, not the normal one.
const cmNoOwnLines = "keine Zeile hinzu, die gegen jeden Elternteil neu ist"

type cmRepo struct {
	t   *testing.T
	dir string
	env []string
}

// cmSetup resolves the hook and the tool, puts a wrapper for the tool and a
// secret-tool that finds nothing on PATH, and creates a repository with
// core.hooksPath on the real .githooks. It skips the test when git, sh or
// python3 is missing.
func cmSetup(t *testing.T) *cmRepo {
	t.Helper()
	for _, tool := range []string{"git", "sh", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool + " on PATH, so the commit-msg hook cannot be run here: " + err.Error())
		}
	}
	hooks, err := filepath.Abs(filepath.Join("..", "..", ".githooks"))
	if err != nil {
		t.Fatal(err)
	}
	tool, err := filepath.Abs(filepath.Join("..", "..", "tools", "typesafe", "bin", "ts-commit-check"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(hooks, "commit-msg"), tool} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	cmWriteExec(t, filepath.Join(bin, "ts-commit-check"),
		"#!/bin/sh\nexec python3 '"+strings.ReplaceAll(tool, "'", `'\''`)+"' \"$@\"\n")
	cmWriteExec(t, filepath.Join(bin, "secret-tool"), "#!/bin/sh\nexit 1\n")
	r := &cmRepo{t: t, dir: t.TempDir()}
	r.env = ccIsolatedGitEnv("PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PYTHONDONTWRITEBYTECODE=1")
	r.git("init", "-q", "-b", "main")
	r.git("config", "user.name", ppAuthorName)
	r.git("config", "user.email", ccTestAuthorEmail)
	r.git("config", "core.hooksPath", hooks)
	return r
}

func cmWriteExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// in returns a copy of r that runs git in dir, a linked worktree of r.
func (r *cmRepo) in(dir string) *cmRepo { c := *r; c.dir = dir; return &c }

func (r *cmRepo) run(args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *cmRepo) git(args ...string) string {
	r.t.Helper()
	out, err := r.run(args...)
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

func (r *cmRepo) write(path, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, path), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *cmRepo) head() string { r.t.Helper(); return r.git("rev-parse", "HEAD") }

// commit stages everything and commits through the hook, which has to pass.
func (r *cmRepo) commit(msg string) { r.t.Helper(); r.git("add", "-A"); r.mustPass("commit", "-q", "-m", msg) }

// commitNoVerify stages everything and commits past the hook: the way a line
// already published on main arrives, through a squash merge on the server.
func (r *cmRepo) commitNoVerify(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--no-verify", "-m", msg)
}

func (r *cmRepo) merging() bool {
	r.t.Helper()
	_, err := r.run("rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

func (r *cmRepo) parents() int {
	r.t.Helper()
	return len(strings.Fields(r.git("rev-list", "--parents", "-n", "1", "HEAD"))) - 1
}

// mustPass runs a git command that commits through the hook and fails the
// test unless it succeeded and ts-commit-check actually ran: without a key it
// prints a Hinweis line on every pass, and a hook that found no
// ts-commit-check would pass silently.
func (r *cmRepo) mustPass(args ...string) string {
	r.t.Helper()
	out, err := r.run(args...)
	if err != nil {
		r.t.Fatalf("git %v was refused: %v\n%s", args, err, out)
	}
	if !strings.Contains(out, "Hinweis:") {
		r.t.Fatalf("git %v passed, but ts-commit-check printed nothing, so it did not run:\n%s", args, out)
	}
	return out
}

// mustRefuse runs a git command that commits through the hook and fails the
// test unless the local alarm refused it - not an error, which exits 1 as
// well - and HEAD stayed where it was.
func (r *cmRepo) mustRefuse(args ...string) string {
	r.t.Helper()
	head := r.head()
	out, err := r.run(args...)
	if err == nil {
		r.t.Fatalf("git %v passed, want it refused:\n%s", args, out)
	}
	if !strings.Contains(out, "ABBRUCH") || strings.Contains(out, "Fehler:") {
		r.t.Fatalf("git %v failed, but not with the leak refusal:\n%s", args, out)
	}
	if got := r.head(); got != head {
		r.t.Fatalf("HEAD moved from %s to %s although the commit was refused", head, got)
	}
	return out
}

// cmDiverged builds main and a branch feature that both moved on from one
// base. feature edits shared.txt at the top; main appends cmShapeA to
// shared.txt and adds a file named cmShapeName, both past the hook. HEAD is
// left on feature, so merging main brings in only lines main already has.
func cmDiverged(t *testing.T) *cmRepo {
	t.Helper()
	r := cmSetup(t)
	r.write("shared.txt", "one\ntwo\nthree\n")
	r.commit("base")
	r.git("checkout", "-q", "-b", "feature")
	r.write("shared.txt", "one, edited on feature\ntwo\nthree\n")
	r.write("feature.txt", "feature work\n")
	r.commit("feature work")
	r.git("checkout", "-q", "main")
	r.write("shared.txt", "one\ntwo\nthree\n"+cmShapeA+"\n")
	r.write(cmShapeName, "named like a key, published on main\n")
	r.commitNoVerify("fixtures, already on main")
	r.git("checkout", "-q", "feature")
	return r
}

// cmConflict builds main and feature that set the same line of conf.txt
// differently - main to cmShapeA, past the hook - and starts merging main into
// feature, which stops on the conflict before any hook runs.
func cmConflict(t *testing.T) *cmRepo {
	t.Helper()
	r := cmSetup(t)
	r.write("conf.txt", "head\nvalue = 0\ntail\n")
	r.commit("base")
	r.git("checkout", "-q", "-b", "feature")
	r.write("conf.txt", "head\nvalue = 1\ntail\n")
	r.commit("feature value")
	r.git("checkout", "-q", "main")
	r.write("conf.txt", "head\n"+cmShapeA+"\ntail\n")
	r.commitNoVerify("main value, already on main")
	r.git("checkout", "-q", "feature")
	if out, err := r.run("merge", "--no-edit", "main"); err == nil {
		t.Fatalf("the merge was meant to stop on a conflict:\n%s", out)
	}
	if !r.merging() {
		t.Fatal("no MERGE_HEAD after the conflicting merge")
	}
	return r
}

func TestCommitMsgMergeOfParentLinesPasses(t *testing.T) {
	r := cmDiverged(t)
	t.Parallel()
	// Against feature, the merge adds cmShapeA and the file cmShapeName; the
	// whole-diff check refused exactly that. Against main it adds only
	// feature's own edit, which feature already carries.
	out := r.mustPass("merge", "--no-edit", "main")
	if !strings.Contains(out, cmNoOwnLines) {
		t.Fatalf("the merge passed, but not as a merge with no lines of its own:\n%s", out)
	}
	if n := r.parents(); n != 2 {
		t.Fatalf("HEAD has %d parent(s), want a merge commit with 2", n)
	}
}

func TestCommitMsgConflictResolvedToOneSidePasses(t *testing.T) {
	r := cmConflict(t)
	t.Parallel()
	r.git("checkout", "--theirs", "conf.txt")
	r.git("add", "conf.txt")
	out := r.mustPass("commit", "--no-edit")
	if !strings.Contains(out, cmNoOwnLines) {
		t.Fatalf("the resolution took main's line verbatim, yet the merge counted lines of its own:\n%s", out)
	}
	if n := r.parents(); n != 2 {
		t.Fatalf("HEAD has %d parent(s), want a merge commit with 2", n)
	}
}

func TestCommitMsgConflictResolutionWithNewLeakRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		commit []string
	}{
		{"staged, then commit", []string{"commit", "--no-edit"}},
		{"commit -a", []string{"commit", "-a", "--no-edit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := cmConflict(t)
			t.Parallel()
			// Neither feature's line nor main's: new against both parents.
			r.write("conf.txt", "head\n"+cmShapeB+"\ntail\n")
			if tc.commit[1] != "-a" {
				r.git("add", "conf.txt")
			}
			r.mustRefuse(tc.commit...)
			if !r.merging() {
				t.Fatal("MERGE_HEAD is gone, so the refused merge cannot be finished")
			}
		})
	}
}

func TestCommitMsgEvilMergeRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		add  func(r *cmRepo)
	}{
		{"line in a file both parents have", func(r *cmRepo) {
			r.write("feature.txt", "feature work\n"+cmShapeB+"\n")
		}},
		{"file name no parent has", func(r *cmRepo) {
			r.write("x-"+cmShapeName, "an unremarkable line\n")
		}},
		{"line in a file that is not UTF-8", func(r *cmRepo) {
			// A Latin-1 byte must not turn the whole diff into nothing.
			r.write("latin1.txt", "caf\xe9\n"+cmShapeB+"\n")
		}},
		// Python's str.splitlines splits on these too; git does not, so to git
		// each is one line, whose rest must still be read.
		{"line after a lone CR", func(r *cmRepo) {
			r.write("feature.txt", "feature work\nx\r"+cmShapeB+"\n")
		}},
		{"line after a form feed", func(r *cmRepo) {
			r.write("feature.txt", "feature work\nx\f"+cmShapeB+"\n")
		}},
		{"line after U+2028", func(r *cmRepo) {
			r.write("feature.txt", "feature work\nx "+cmShapeB+"\n")
		}},
		{"lone CR before a line that starts with diff", func(r *cmRepo) {
			r.write("feature.txt", "feature work\nx\rdiff y\n"+cmShapeB+"\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := cmDiverged(t)
			t.Parallel()
			r.git("merge", "--no-commit", "--no-ff", "main")
			tc.add(r)
			r.git("add", "-A")
			r.mustRefuse("commit", "--no-edit")
		})
	}
}

func TestCommitMsgOctopusMergeReadsEveryParent(t *testing.T) {
	r := cmSetup(t)
	t.Parallel()
	r.write("base.txt", "base\n")
	r.commit("base")
	r.git("branch", "b1")
	r.git("branch", "b2")
	r.write("main.txt", "main work\n")
	r.commit("main work")
	r.git("checkout", "-q", "b1")
	r.write("b1.txt", "b1 work\n")
	r.commit("b1 work")
	r.git("checkout", "-q", "b2")
	r.write("b2.txt", cmShapeA+"\n")
	r.commitNoVerify("b2 fixture, already published")
	r.git("checkout", "-q", "main")
	// cmShapeA is new against main and against b1: only b2, the second
	// MERGE_HEAD entry, has it.
	out := r.mustPass("merge", "--no-edit", "b1", "b2")
	if !strings.Contains(out, cmNoOwnLines) {
		t.Fatalf("the octopus merge passed, but not as a merge with no lines of its own:\n%s", out)
	}
	if n := r.parents(); n != 3 {
		t.Fatalf("HEAD has %d parent(s), want an octopus merge with 3", n)
	}
}

func TestCommitMsgMergeInLinkedWorktree(t *testing.T) {
	r := cmDiverged(t)
	t.Parallel()
	r.git("checkout", "-q", "main")
	wt := r.in(filepath.Join(t.TempDir(), "wt"))
	r.git("worktree", "add", "-q", "-b", "feature2", wt.dir, "feature")
	// MERGE_HEAD lives under .git/worktrees/<name>/ here, not under .git/.
	wt.git("merge", "--no-commit", "--no-ff", "main")
	wt.write("feature.txt", "feature work\n"+cmShapeB+"\n")
	wt.git("add", "-A")
	wt.mustRefuse("commit", "--no-edit")
	wt.write("feature.txt", "feature work\n")
	wt.git("add", "-A")
	out := wt.mustPass("commit", "--no-edit")
	if !strings.Contains(out, cmNoOwnLines) {
		t.Fatalf("the merge in a linked worktree passed, but not as a merge:\n%s", out)
	}
	if n := wt.parents(); n != 2 {
		t.Fatalf("HEAD has %d parent(s), want a merge commit with 2", n)
	}
}

func TestCommitMsgNormalCommitsCheckTheWholeDiff(t *testing.T) {
	t.Run("plain commit", func(t *testing.T) {
		r := cmSetup(t)
		t.Parallel()
		r.write("base.txt", "base\n")
		r.commit("base")
		r.write("conf.txt", cmShapeA+"\n")
		r.git("add", "-A")
		r.mustRefuse("commit", "-m", "add conf")
	})
	t.Run("merge --squash", func(t *testing.T) {
		// A squash leaves no MERGE_HEAD: its commit has one parent, and every
		// line it brings in is checked as that commit's own.
		r := cmDiverged(t)
		t.Parallel()
		r.git("merge", "--squash", "main")
		if r.merging() {
			t.Fatal("git merge --squash left a MERGE_HEAD; this test assumes it does not")
		}
		r.mustRefuse("commit", "-m", "squash main")
	})
}
