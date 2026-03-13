package level

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"2024-01-01 ERROR something broke", "error"},
		{"[ERR] connection refused", "error"},
		{"WARN: disk almost full", "warn"},
		{"WARNING: deprecated API", "warn"},
		{"INFO server started on :8080", "info"},
		{"DEBUG processing request id=42", "debug"},
		{"TRACE entering function", "debug"},
		{"FATAL out of memory", "fatal"},
		{"PANIC: runtime error", "fatal"},
		{`{"level":"error","msg":"bad request"}`, "error"},
		{`{"level": "warn", "msg": "slow query"}`, "warn"},
		{`{"level":"info","msg":"started"}`, "info"},
		{`{"level":"debug","msg":"details"}`, "debug"},
		{"just a normal log line", ""},
		{"", ""},
		{"information about the system", ""}, // INFO should match word boundary only — wait, INFO is in "information"... let me check
	}

	for _, tt := range tests {
		got := Detect(tt.line)
		if got != tt.want {
			t.Errorf("Detect(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestDetectWordBoundary(t *testing.T) {
	// "information" should NOT match INFO because \b is after INFO in "information"
	// Actually \bINFO\b won't match "information" because there's no boundary after INFO.
	got := Detect("information about errors in the system")
	if got != "" {
		// "information" contains INFO at start but no word boundary after.
		// "errors" contains ERROR at start? No — ERR is in "errors" with \bERR\b matching.
		// Actually \bERR\b would match "errors" because ERR is followed by 'o' which... hmm.
		// \b matches between word and non-word. E-R-R-o: the boundary is before E and there's
		// no boundary between R and o since both are word chars. So \bERR\b requires boundary
		// after last R, but 'o' is a word char, so no boundary. Good.
		t.Errorf("expected empty, got %q", got)
	}
}
