package main

import (
	"io"
	"testing"

	"ofam.dev/ochazuke/rulecheck"
)

// TestRules runs ochazuke's hiddenkeys check over o-proj's own default
// rows: a hidden row takes no key that did something at any place and mode
// without it. It walks the runtime's places and testdata/rules/places.json
// (the family's recorded noun places: noun, ancestors, modal, with the "proj."
// namespace dropped from each noun, since the key picker recorded them as mtm
// mounts them and this runtime names them without it). The other
// rulecheck checks stay with their own tests (TestNoNewNameBranches,
// TestNoNewRefusals, TestLexicon).
//
// serial: the checks pass RULECHECK_UPDATE on with t.Setenv.
func TestRules(t *testing.T) {
	home := t.TempDir()
	tool := &Tool{Stdout: io.Discard, Stderr: io.Discard, Home: home, Prog: "o-proj", Env: []string{"HOME=" + home}}
	sh, err := tool.shell()
	if err != nil {
		t.Fatal(err)
	}
	places, err := rulecheck.LoadPlaces("testdata/rules/places.json")
	if err != nil {
		t.Fatal(err)
	}
	const own = "has its own test (TestNoNewNameBranches, TestNoNewRefusals, TestLexicon) or is not adopted here yet"
	rulecheck.Check(t, rulecheck.Config{
		Runtime: sh.Runtime,
		Places:  places,
		Root:    ".",
		Dirs:    []string{"."},
		Off: map[string]string{
			"deadkeys": own, "names": own, "refusals": own, "lexicon": own, "nouns": own,
		},
	})
}
