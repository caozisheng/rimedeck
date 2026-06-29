package daemon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeAgentExecutionTimeoutDefault(t *testing.T) {
	t.Parallel()

	fallback := 20 * time.Minute
	if got := decodeAgentExecutionTimeout(nil, fallback, quietLogger()); got != fallback {
		t.Fatalf("timeout = %s, want %s", got, fallback)
	}
	if got := decodeAgentExecutionTimeout(json.RawMessage(`{"mode":"gateway"}`), fallback, quietLogger()); got != fallback {
		t.Fatalf("timeout = %s, want %s", got, fallback)
	}
}

func TestDecodeAgentExecutionTimeoutMinutes(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"execution":{"timeout_minutes":45}}`)
	if got := decodeAgentExecutionTimeout(raw, 20*time.Minute, quietLogger()); got != 45*time.Minute {
		t.Fatalf("timeout = %s, want 45m", got)
	}
}

func TestDecodeAgentExecutionTimeoutFractionalMinutes(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"execution":{"timeout_minutes":1.5}}`)
	if got := decodeAgentExecutionTimeout(raw, 20*time.Minute, quietLogger()); got != 90*time.Second {
		t.Fatalf("timeout = %s, want 90s", got)
	}
}

func TestDecodeAgentExecutionTimeoutInvalid(t *testing.T) {
	t.Parallel()

	fallback := 20 * time.Minute
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"execution":{"timeout_minutes":0}}`),
		json.RawMessage(`{"execution":{"timeout_minutes":-1}}`),
		json.RawMessage(`{"execution":{"timeout_minutes":2000}}`),
		json.RawMessage(`{"execution":{"timeout_minutes":"45"}}`),
		json.RawMessage(`{"execution":`),
	} {
		if got := decodeAgentExecutionTimeout(raw, fallback, quietLogger()); got != fallback {
			t.Fatalf("timeout for %s = %s, want %s", raw, got, fallback)
		}
	}
}
