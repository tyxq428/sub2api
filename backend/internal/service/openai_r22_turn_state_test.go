package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestR22TurnStateSameTurnRetryKeepsOpaqueEcho(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 7, "sess-r22-retry")
	c.Request.Header.Set("x-codex-turn-metadata", `{"turn_id":"turn-r22-a"}`)
	upstream := http.Header{"X-Codex-Turn-State": []string{"blob-r22-a"}}
	svc.relayOpenAICodexTurnState(c, &Account{ID: 42}, upstream)

	echo := http.Header{"X-Codex-Turn-State": []string{"blob-r22-a"}}
	svc.guardOpenAICodexTurnStateEcho(c, &Account{ID: 42}, echo)
	require.Equal(t, "blob-r22-a", echo.Get("x-codex-turn-state"))
}

func TestR22TurnStateNextTurnStripsPreviousOpaqueEcho(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 7, "sess-r22-next")
	c.Request.Header.Set("x-codex-turn-metadata", `{"turn_id":"turn-r22-a"}`)
	upstream := http.Header{"X-Codex-Turn-State": []string{"blob-r22-a"}}
	svc.relayOpenAICodexTurnState(c, &Account{ID: 42}, upstream)

	c.Request.Header.Set("x-codex-turn-metadata", `{"turn_id":"turn-r22-b"}`)
	echo := http.Header{"X-Codex-Turn-State": []string{"blob-r22-a"}}
	svc.guardOpenAICodexTurnStateEcho(c, &Account{ID: 42}, echo)
	require.Empty(t, echo.Get("x-codex-turn-state"))
}

func TestR22TurnStateTurnIDFallsBackToExplicitHeaderOnly(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 7, "sess-r22-fallback")
	c.Request.Header.Set("turn-id", "turn-header")
	require.Equal(t, "turn-header", openAICodexTurnStateTurnID(c))

	c.Request.Header.Set("x-codex-turn-metadata", `{"turn_id":"turn-canonical"}`)
	require.Equal(t, "turn-canonical", openAICodexTurnStateTurnID(c))

	c.Request.Header.Set("x-codex-turn-metadata", "{malformed")
	require.Equal(t, "turn-header", openAICodexTurnStateTurnID(c))
}
