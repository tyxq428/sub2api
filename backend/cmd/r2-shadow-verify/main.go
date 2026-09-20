// r2-shadow-verify is a staging-only verifier for the Codex R2 shadow observer.
//
// It intentionally refuses to run unless current_database() matches the
// expected staging database. It sends no model request and persists only
// pseudonymized synthetic shadow rows, which it removes before exit.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codexidentity"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func cleanup(ctx context.Context, db *sql.DB, accountID int64) {
	for _, query := range []string{
		"DELETE FROM codex_r2_shadow_edges WHERE account_id=$1",
		"DELETE FROM codex_r2_shadow_entities WHERE account_id=$1",
		"DELETE FROM codex_r2_shadow_daily WHERE account_id=$1",
	} {
		_, _ = db.ExecContext(ctx, query, accountID)
	}
}

func syntheticSnapshot() (http.Header, []byte) {
	const (
		install = "44444444-4444-4444-8444-444444444444"
		session = "0199a1b2-c3d4-4e5f-8a9b-111111111111"
		thread  = "0199a1b2-c3d4-4e5f-8a9b-222222222222"
		turn    = "0199a1b2-c3d4-4e5f-8a9b-333333333333"
		window  = "0199a1b2-c3d4-4e5f-8a9b-555555555555"
		parent  = "0199a1b2-c3d4-4e5f-8a9b-666666666666"
		root    = "0199a1b2-c3d4-4e5f-8a9b-777777777777"
	)
	metadataBytes, err := json.Marshal(map[string]any{
		"installation_id": install, "session_id": session, "thread_id": thread, "turn_id": turn,
		"window_id": window, "context_window_id": window, "forked_from_thread_id": parent,
		"parent_thread_id": parent, "parent_turn_id": turn, "root_turn_id": root,
		"request_kind": "turn", "subagent_kind": "worker", "thread_source": "cli", "turn_trigger": "user",
	})
	must(err)
	metadata := string(metadataBytes)
	headers := http.Header{
		"User-Agent":               {"codex-tui/0.154.0 (Windows 10.0.26200; x86_64) WindowsTerminal"},
		"Originator":               {"codex-tui"},
		"Version":                  {"0.154.0"},
		"Conversation-Id":          {session},
		"Conversation_Id":          {session},
		"Session-Id":               {session},
		"Session_Id":               {session},
		"Thread-Id":                {thread},
		"Turn-Id":                  {turn},
		"X-Client-Request-Id":      {thread},
		"X-Codex-Beta-Features":    {"feature-a,feature-b"},
		"X-Codex-Installation-Id":  {install},
		"X-Codex-Parent-Thread-Id": {parent},
		"X-Codex-Turn-Metadata":    {metadata},
		"X-Codex-Window-Id":        {window},
		"X-Openai-Subagent":        {"worker"},
	}
	body, err := json.Marshal(map[string]any{
		"prompt_cache_key": session,
		"client_metadata": map[string]any{
			"installation_id": install, "x-codex-installation-id": install,
			"session_id": session, "session-id": session,
			"thread_id": thread, "thread-id": thread,
			"turn_id": turn, "turn-id": turn,
			"window_id": window, "x-codex-window-id": window,
			"x-client-request-id": thread, "x-codex-parent-thread-id": parent,
			"x-openai-subagent": "worker", "x-codex-turn-metadata": metadata,
		},
	})
	must(err)
	return headers, body
}

func main() {
	ctx := context.Background()
	dsn := envOr("R2_VERIFY_POSTGRES_DSN",
		"host=/var/run/postgresql user=sub2api_r1 dbname=sub2api_r1_staging sslmode=disable")
	expectedDB := envOr("R2_VERIFY_EXPECTED_DATABASE", "sub2api_r1_staging")
	accountID, err := strconv.ParseInt(envOr("R2_VERIFY_ACCOUNT_ID", "1"), 10, 64)
	must(err)
	if accountID <= 0 {
		panic("R2_VERIFY_ACCOUNT_ID must be positive")
	}

	db, err := sql.Open("postgres", dsn)
	must(err)
	defer func() { _ = db.Close() }()
	must(db.PingContext(ctx))
	var actualDB string
	must(db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actualDB))
	if actualDB != expectedDB {
		panic(fmt.Sprintf("refusing non-staging database: got %q want %q", actualDB, expectedDB))
	}
	var accountExists bool
	must(db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND deleted_at IS NULL)", accountID).Scan(&accountExists))
	if !accountExists {
		panic("staging verifier account does not exist")
	}

	cleanup(ctx, db, accountID)
	defer cleanup(ctx, db, accountID)
	headers, body := syntheticSnapshot()
	raw := codexidentity.Capture(headers, body, codexidentity.DefaultLimits())
	profile, ok := codexidentity.ProfileByID(codexidentity.Codex0154ProfileID)
	if !ok {
		panic("codex 0.154 profile missing")
	}
	mapper, err := codexidentity.NewMapper(codexidentity.MappingScope{
		Secret:    []byte("01234567890123456789012345678901"),
		AuthScope: "api-key:9001", CredentialScope: "chatgpt:synthetic-staging",
		Algorithm: "hmac-sha256-v1", KeyEpoch: "staging-r2-e1",
	})
	must(err)
	plan, err := codexidentity.BuildPlan(raw, profile, mapper, codexidentity.BuildOptions{RequireValidatedClient: true})
	must(err)
	must(plan.Validate())
	projectedHeaders, projectedBody, err := codexidentity.ProjectHTTP(headers, body, plan)
	must(err)
	proposed := codexidentity.Capture(projectedHeaders, projectedBody, codexidentity.DefaultLimits())
	state := service.NewCodexR2StateService(db, nil)

	oldObserver, err := codexidentity.NewObserver(codexidentity.ObserverOptions{
		Mode: codexidentity.ModeShadow, Secret: []byte("01234567890123456789012345678901"),
		QueueCapacity: 16, MaxEventBytes: 4096, BatchSize: 1, FlushInterval: time.Second, Sink: state,
	})
	must(err)
	oldResult := oldObserver.RecordForAccount(accountID, raw, codexidentity.StageInput,
		codexidentity.PurposeInference, profile.ID, "observed")
	oldObserver.Close()
	fmt.Printf("old_limit_result=%s old_oversize=%d\n", oldResult, oldObserver.Stats().DroppedOversize)
	if oldResult != codexidentity.RecordDroppedOversize {
		panic("fixture no longer reproduces the old 4 KiB drop")
	}

	writeErrors := 0
	observer, err := codexidentity.NewObserver(codexidentity.ObserverOptions{
		Mode: codexidentity.ModeShadow, Secret: []byte("01234567890123456789012345678901"),
		QueueCapacity: 16, MaxEventBytes: 16 * 1024, BatchSize: 1, FlushInterval: time.Second, Sink: state,
		OnWriteError: func(error) { writeErrors++ },
	})
	must(err)
	for _, item := range []struct {
		snapshot codexidentity.RawSnapshot
		stage    codexidentity.Stage
		result   string
	}{
		{raw, codexidentity.StageInput, "observed"},
		{proposed, codexidentity.StageProposed, "planned"},
		{raw, codexidentity.StageActual, "prepared"},
	} {
		result := observer.RecordForAccount(accountID, item.snapshot, item.stage,
			codexidentity.PurposeInference, profile.ID, item.result)
		if result != codexidentity.RecordEnqueued {
			panic("fixed observer did not enqueue: " + string(result))
		}
	}
	observer.Close()
	stats := observer.Stats()
	fmt.Printf("new_stats submitted=%d enqueued=%d written=%d oversize=%d full=%d write_errors=%d callback_errors=%d\n",
		stats.Submitted, stats.Enqueued, stats.Written, stats.DroppedOversize, stats.DroppedFull, stats.WriteErrors, writeErrors)
	if stats.Written != 3 || stats.DroppedOversize != 0 || stats.DroppedFull != 0 || stats.WriteErrors != 0 || writeErrors != 0 {
		os.Exit(2)
	}

	rows, err := db.QueryContext(ctx,
		"SELECT stage,result,total_events FROM codex_r2_shadow_daily WHERE account_id=$1 ORDER BY stage,result", accountID)
	must(err)
	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	for rows.Next() {
		var stage, result string
		var total int64
		must(rows.Scan(&stage, &result, &total))
		fmt.Printf("row %s/%s=%d\n", stage, result, total)
		seen[stage+"/"+result] = total > 0
	}
	must(rows.Err())
	for _, key := range []string{"input/observed", "proposed_v2/planned", "actual/prepared"} {
		if !seen[key] {
			panic("missing " + key)
		}
	}
}
