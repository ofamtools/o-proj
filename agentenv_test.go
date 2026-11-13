package main

import (
	"os"
	"testing"
)

// TestMain clears the agent-harness and pane markers the framework reads to
// detect an agent caller and its pane and session (MTM_PANE_ID, MTM_SESSION),
// so the suite runs the same inside an agent session or an mtm pane as
// outside it. Tests that need an identity set their own environment.
func TestMain(m *testing.M) {
	for _, key := range []string{
		"OCHAZUKE_ACTOR", "OCHAZUKE_HARNESS", "MTM_HARNESS", "TUIOS_HARNESS", "AGENT_HARNESS",
		"CODEX_THREAD_ID", "CODEX_CI", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID",
		"CLAUDE_CODE_ENTRYPOINT", "PI_SESSION_ID",
		"MTM_PANE_ID", "MTM_SESSION", "MTM_AGENT_NAME", "TUIOS_PANE_ID", "TUIOS_SESSION", "TUIOS_AGENT_NAME",
	} {
		os.Unsetenv(key)
	}
	os.Exit(m.Run())
}
