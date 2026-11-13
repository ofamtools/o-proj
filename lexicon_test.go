package main

import (
	"strings"

	"io"
	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
	"testing"

	"ofam.dev/ochazuke/lexiconcheck"
	"ofam.dev/ochazuke/scaffoldcheck"
)

// o-proj's registered operations are held to the lexicon's naming rules
// (lexicon plan): one word has one part of speech, nouns are singular, a
// namespace is not a noun or action word, a key has at most three parts.
// testdata/lexicon-words.tsv is a copy of ochazuke's word list.
func TestLexicon(t *testing.T) {
	home := t.TempDir()
	tool := &Tool{Stdout: io.Discard, Stderr: io.Discard, Home: home, Prog: "o-proj", Env: []string{"HOME=" + home}}
	sh, err := tool.shell()
	if err != nil {
		t.Fatal(err)
	}
	lexiconcheck.Check(t, lexiconcheck.Config{
		Operations: &sh.Runtime.Operations,
		MainNouns:  map[string]string{"proj": "project"},
		Words:      "testdata/lexicon-words.tsv",
		Baseline:   "testdata/lexiconcheck.txt",
	})
}

// The manifest's git status is a computed prop of the project collection
// (project status), not a status verb.
func TestProjectStatusIsAProp(t *testing.T) {
	tl, out, _ := tool(t)
	if err := tl.Main([]string{"project", "status"}); err != nil || !strings.Contains(out.String(), "summary") {
		t.Fatalf("project status %q %v", out, err)
	}
	if _, ok := tl.mustShell(t).Runtime.Operations.Find(ochazuke.Key("proj.project.status")); ok {
		t.Fatal("project status is still a verb")
	}
}

func (t *Tool) mustShell(tt *testing.T) *ocli.Shell {
	tt.Helper()
	sh, err := t.shell()
	if err != nil {
		tt.Fatal(err)
	}
	return sh
}

// The project noun is served by the generic scaffold from its declaration:
// a label, the archive role, the standard verbs and their keys (an empty
// baseline: nothing is excused).
func TestProjectScaffold(t *testing.T) {
	tl, _, _ := tool(t)
	scaffoldcheck.Check(t, scaffoldcheck.Config{Runtime: tl.mustShell(t).Runtime, Baseline: "testdata/scaffoldcheck.txt"})
}
