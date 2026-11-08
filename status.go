package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Repo is one directory a status report inspects.
type Repo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type RepoStatus struct {
	Repo
	Status    string `json:"status"`
	Branch    string `json:"branch,omitempty"`
	Upstream  string `json:"upstream,omitempty"`
	Ahead     int    `json:"ahead,omitempty"`
	Behind    int    `json:"behind,omitempty"`
	Tracked   int    `json:"tracked"`
	Changed   int    `json:"changed"`
	Staged    int    `json:"staged"`
	Unstaged  int    `json:"unstaged"`
	Untracked int    `json:"untracked"`
	Conflicts int    `json:"conflicts"`
	Error     string `json:"error,omitempty"`
}

type Summary struct {
	Total     int `json:"total"`
	Clean     int `json:"clean"`
	Dirty     int `json:"dirty"`
	Errors    int `json:"errors"`
	Tracked   int `json:"tracked"`
	Changed   int `json:"changed"`
	Staged    int `json:"staged"`
	Unstaged  int `json:"unstaged"`
	Untracked int `json:"untracked"`
	Conflicts int `json:"conflicts"`
}

type Report struct {
	Repos   []RepoStatus `json:"repos"`
	Summary Summary      `json:"summary"`
}

func collect(ctx context.Context, repos []Repo) Report {
	r := Report{Repos: make([]RepoStatus, 0, len(repos))}
	for _, repo := range repos {
		s := inspect(ctx, repo)
		r.Repos = append(r.Repos, s)
		r.Summary.Total++
		switch s.Status {
		case "clean":
			r.Summary.Clean++
		case "dirty":
			r.Summary.Dirty++
		default:
			r.Summary.Errors++
			continue
		}
		r.Summary.Tracked += s.Tracked
		r.Summary.Changed += s.Changed
		r.Summary.Staged += s.Staged
		r.Summary.Unstaged += s.Unstaged
		r.Summary.Untracked += s.Untracked
		r.Summary.Conflicts += s.Conflicts
	}
	return r
}

func inspect(ctx context.Context, repo Repo) RepoStatus {
	s := RepoStatus{Repo: repo}
	info, err := os.Stat(repo.Path)
	if os.IsNotExist(err) {
		s.Status, s.Error = "missing", "directory does not exist"
		return s
	}
	if err != nil {
		s.Status, s.Error = "error", err.Error()
		return s
	}
	if !info.IsDir() {
		s.Status, s.Error = "not-repo", "not a directory"
		return s
	}
	if _, err := os.Stat(filepath.Join(repo.Path, ".git")); err != nil {
		if os.IsNotExist(err) {
			s.Status, s.Error = "not-repo", "not a repository root (.git is absent)"
		} else {
			s.Status, s.Error = "error", err.Error()
		}
		return s
	}
	data, err := gitOutput(ctx, repo.Path, "status", "--porcelain=v1", "-z", "--branch", "--untracked-files=all", "--ignore-submodules=none")
	if err == nil {
		err = parseStatus(data, &s)
	}
	if err == nil {
		data, err = gitOutput(ctx, repo.Path, "ls-files", "--cached", "--deduplicate", "-z")
		if err == nil {
			s.Tracked = bytes.Count(data, []byte{0})
		}
	}
	if err != nil {
		s.Status, s.Error = "error", err.Error()
		return s
	}
	s.Status = "clean"
	if s.Changed != 0 {
		s.Status = "dirty"
	}
	return s
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir, "-c", "core.fsmonitor=false"}, args...)...)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func parseStatus(data []byte, s *RepoStatus) error {
	for len(data) > 0 {
		record, rest, ok := bytes.Cut(data, []byte{0})
		if !ok || len(record) < 3 {
			return fmt.Errorf("invalid Git status record")
		}
		data = rest
		if bytes.HasPrefix(record, []byte("## ")) {
			branch := strings.TrimPrefix(string(record[3:]), "No commits yet on ")
			branch = strings.TrimPrefix(branch, "Initial commit on ")
			branch, track, _ := strings.Cut(branch, "...")
			s.Branch = branch
			upstream, counts, _ := strings.Cut(track, " [")
			s.Upstream = upstream
			for _, part := range strings.Split(strings.TrimSuffix(counts, "]"), ", ") {
				if n, ok := strings.CutPrefix(part, "ahead "); ok {
					s.Ahead, _ = strconv.Atoi(n)
				} else if n, ok := strings.CutPrefix(part, "behind "); ok {
					s.Behind, _ = strconv.Atoi(n)
				}
			}
			continue
		}
		x, y := record[0], record[1]
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			_, data, ok = bytes.Cut(data, []byte{0})
			if !ok {
				return fmt.Errorf("missing Git rename source")
			}
		}
		if x == '!' && y == '!' {
			continue
		}
		s.Changed++
		switch string(record[:2]) {
		case "??":
			s.Untracked++
		case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
			s.Conflicts++
		default:
			if x != ' ' {
				s.Staged++
			}
			if y != ' ' {
				s.Unstaged++
			}
		}
	}
	return nil
}
