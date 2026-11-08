package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"ofam.dev/oden"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "user.name=Family Test", "-c", "user.email=family@example.invalid", "-c", "commit.gpgSign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := realTempDir(t)
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "status.renames", "true")
	for _, name := range []string{"tracked.txt", "rename.txt", "delete.txt"} {
		put(t, filepath.Join(dir, name), "base\n")
	}
	put(t, filepath.Join(dir, ".gitignore"), "ignored/\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-qm", "initial")
	return dir
}

func TestStatusCountsPathsRatherThanStatusColumns(t *testing.T) {
	dir := newRepo(t)
	clean := inspect(context.Background(), Repo{Name: "example", Path: dir})
	if clean.Status != "clean" || clean.Tracked != 4 || clean.Changed != 0 || clean.Branch != "main" {
		t.Fatalf("clean: %+v", clean)
	}
	put(t, filepath.Join(dir, "tracked.txt"), "staged\n")
	runGit(t, dir, "add", "tracked.txt")
	put(t, filepath.Join(dir, "tracked.txt"), "worktree\n")
	runGit(t, dir, "mv", "rename.txt", "renamed\nname.txt")
	if err := os.Remove(filepath.Join(dir, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "new", "one.txt"), "one")
	put(t, filepath.Join(dir, "new", "two\nname.txt"), "two")
	put(t, filepath.Join(dir, "ignored", "cache"), "ignored")
	index := filepath.Join(dir, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	s := inspect(context.Background(), Repo{Name: "example", Path: dir})
	if s.Status != "dirty" || s.Tracked != 4 || s.Changed != 5 || s.Staged != 2 || s.Unstaged != 2 || s.Untracked != 2 || s.Conflicts != 0 {
		t.Fatalf("dirty: %+v", s)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("inspection changed Git index: %v", err)
	}
}

func TestConflictsAndWorktrees(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "linked")
	runGit(t, dir, "worktree", "add", "-qb", "linked", worktree)
	s := inspect(context.Background(), Repo{Name: "linked", Path: worktree})
	if s.Status != "clean" || s.Tracked != 4 || s.Branch != "linked" {
		t.Fatalf("linked worktree: %+v", s)
	}
	put(t, filepath.Join(worktree, "tracked.txt"), "linked change\n")
	runGit(t, worktree, "add", "tracked.txt")
	runGit(t, worktree, "commit", "-qm", "linked change")
	put(t, filepath.Join(dir, "tracked.txt"), "main change\n")
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-qm", "main change")
	merge := exec.Command("git", "-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "user.name=Family Test", "-c", "user.email=family@example.invalid", "-c", "commit.gpgSign=false", "merge", "--no-ff", "linked")
	if err := merge.Run(); err == nil {
		t.Fatal("expected a merge conflict")
	}
	s = inspect(context.Background(), Repo{Name: "main", Path: dir})
	if s.Status != "dirty" || s.Tracked != 4 || s.Changed != 1 || s.Conflicts != 1 || s.Staged != 0 || s.Unstaged != 0 {
		t.Fatalf("conflict: %+v", s)
	}
}

func TestStatusReposFromManifestDeduplicated(t *testing.T) {
	tl, _, _ := tool(t)
	repo := filepath.Join(tl.Home, "proj", "project")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tl.SaveManifest([]Entry{{Name: "named", Path: "~/proj/project"}, {Name: "alias", Path: repo}}); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(repo)
	repos, err := tl.statusRepos(nil)
	if err != nil || len(repos) != 1 || repos[0].Path != real {
		t.Fatalf("manifest repos: %+v, %v", repos, err)
	}
	explicit, err := tl.statusRepos([]string{repo})
	if err != nil || len(explicit) != 1 || explicit[0].Path != real || explicit[0].Name != "project" {
		t.Fatalf("explicit directories replace the manifest: %+v, %v", explicit, err)
	}
}

// report runs status --json ARGS with a fresh tool and returns the report.
func report(t *testing.T, args ...string) (Report, error) {
	t.Helper()
	tl, out, _ := tool(t)
	err := tl.Main(append([]string{"status", "--json"}, args...))
	var r Report
	if jerr := json.Unmarshal(out.Bytes(), &r); jerr != nil {
		t.Fatalf("%v: %q", jerr, out)
	}
	return r, err
}

func TestFilteredReportRetainsErrorsAndFullSummary(t *testing.T) {
	clean, dirty, ordinary := newRepo(t), newRepo(t), realTempDir(t)
	put(t, filepath.Join(dirty, "new.txt"), "new")
	missing := filepath.Join(t.TempDir(), "missing")
	rep, err := report(t, clean, "-d", dirty, ordinary, missing)
	if oden.ExitCode(err) != 1 {
		t.Fatalf("unavailable paths must produce nonzero exit: %v", err)
	}
	if len(rep.Repos) != 3 || rep.Summary.Total != 4 || rep.Summary.Dirty != 1 || rep.Summary.Clean != 1 || rep.Summary.Errors != 2 || rep.Summary.Changed != 1 {
		t.Fatalf("filtered report: %+v", rep)
	}
	states := map[string]string{}
	for _, repo := range rep.Repos {
		states[repo.Path] = repo.Status
	}
	if states[dirty] != "dirty" || states[ordinary] != "not-repo" || states[missing] != "missing" || states[clean] != "" {
		t.Fatalf("filter hid an error or included a clean repository: %+v", states)
	}
	rep, err = report(t, "--", ordinary, missing)
	if oden.ExitCode(err) != 1 || len(rep.Repos) != 2 || rep.Summary.Errors != 2 {
		t.Fatalf("unavailable report: %+v, %v", rep, err)
	}
}

func TestGitEnvironmentAndUnbornBranch(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "fresh")
	put(t, filepath.Join(dir, "new.txt"), "new")
	runGit(t, dir, "add", ".")
	other := newRepo(t)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))
	s := inspect(context.Background(), Repo{Name: "fresh", Path: dir})
	if s.Status != "dirty" || s.Branch != "fresh" || s.Tracked != 1 || s.Staged != 1 || s.Changed != 1 {
		t.Fatalf("unborn repository or Git environment isolation: %+v", s)
	}
}

func TestReportFlagAliasesAndDelimiter(t *testing.T) {
	dir := newRepo(t)
	put(t, filepath.Join(dir, "new.txt"), "new")
	for _, option := range []string{"-d", "--dirty"} {
		rep, err := report(t, dir, option)
		if err != nil || len(rep.Repos) != 1 || rep.Repos[0].Changed != 1 {
			t.Fatalf("%s: %+v, %v", option, rep, err)
		}
	}
	parent := realTempDir(t)
	for _, name := range []string{"-c", "-h"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(parent)
	rep, err := report(t, "-d", "--", "-c", "-h")
	if oden.ExitCode(err) != 1 || rep.Summary.Total != 2 || rep.Summary.Errors != 2 {
		t.Fatalf("literal directory report: %+v, %v", rep, err)
	}
}

func TestStatusAheadBehind(t *testing.T) {
	upstream := newRepo(t)
	clone := realTempDir(t)
	runGit(t, clone, "clone", "-q", upstream, ".")
	put(t, filepath.Join(upstream, "more.txt"), "more")
	runGit(t, upstream, "add", ".")
	runGit(t, upstream, "commit", "-qm", "more")
	runGit(t, clone, "fetch", "-q")
	s := inspect(context.Background(), Repo{Name: "c", Path: clone})
	if s.Status != "clean" || s.Branch != "main" || s.Upstream != "origin/main" || s.Behind != 1 || s.Ahead != 0 {
		t.Fatalf("behind: %+v", s)
	}
}
