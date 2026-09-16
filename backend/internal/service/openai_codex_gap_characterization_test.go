package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func gapRestoreIdentityGlobals(t *testing.T) {
	t.Helper()
	enabled := codexIdentityEnforcement.Load()
	codexCanonicalUAMu.RLock()
	resolver := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	t.Cleanup(func() {
		SetCodexCanonicalUserAgentResolver(resolver)
		SetCodexIdentityEnforcementEnabled(enabled)
	})
}

// Characterization, not new identity logic: v0.2.5 already supports an exec
// override and derives the effective version from the canonical resolver.
func TestCodexGapExecIdentityUsesExistingOverride(t *testing.T) {
	gapRestoreIdentityGlobals(t)
	SetCodexIdentityEnforcementEnabled(true)
	SetCodexCanonicalUserAgentResolver(func() string {
		return "codex-tui/0.154.0 (Ubuntu 24.04.3; x86_64) xterm-256color"
	})
	h := make(http.Header)
	h.Set("originator", "codex_vscode")
	h.Set("user-agent", "codex_vscode/0.151.0 (Windows; x86_64) vscode")
	enforceCodexIdentityHeadersWithUA(h, "codex_exec/0.151.0 (Ubuntu 24.04.3; x86_64) xterm-256color")
	require.Equal(t, "codex_exec", h.Get("originator"))
	require.Equal(t, "0.154.0", h.Get("version"))
	require.Equal(t, "codex_exec/0.154.0 (Ubuntu 24.04.3; x86_64) xterm-256color", h.Get("user-agent"))
}

func TestCodexGapConvergenceOffIsIndependentOfIdentity(t *testing.T) {
	gapRestoreIdentityGlobals(t)
	SetCodexIdentityEnforcementEnabled(true)
	canonical := "codex_exec/0.154.0 (Ubuntu 24.04.3; x86_64) xterm-256color"
	SetCodexCanonicalUserAgentResolver(func() string { return canonical })
	account := gapQuotaAccount(nil, nil)
	require.Equal(t, codexFingerprintOff, account.GetCodexFingerprintMode())
	require.Nil(t, resolveCodexFingerprintIDs(account, "synthetic-session", account.GetCodexFingerprintMode()))
	h := make(http.Header)
	h.Set("originator", "codex_vscode")
	h.Set("user-agent", "codex_vscode/0.151.0 (Windows; x86_64) vscode")
	enforceCodexIdentityHeaders(h)
	require.Equal(t, canonical, h.Get("user-agent"))
	require.Equal(t, "codex_exec", h.Get("originator"))
}
