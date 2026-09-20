package codexwire

import "testing"

func BenchmarkParseTurnMetadata(b *testing.B) {
	raw := `{"installation_id":"11111111-1111-4111-8111-111111111111","session_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","thread_id":"0199a1b2-c3d4-7e5f-8a9b-111111111111","turn_id":"0199a1b2-c3d4-7e5f-8a9b-222222222222","analytics_enabled":false,"window_number":9007199254740993,"workspaces":[{"root":"/synthetic"}],"future_opaque":{"n":9007199254740993}}`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseTurnMetadata(raw); err != nil {
			b.Fatal(err)
		}
	}
}
