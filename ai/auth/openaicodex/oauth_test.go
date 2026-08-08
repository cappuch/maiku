package openaicodex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFlexibleSeconds(t *testing.T) {
	cases := map[string]float64{
		`5`:      5,
		`"5"`:    5,
		`"5.5"`:  5.5,
		`"0.75"`: 0.75,
	}
	for in, want := range cases {
		var s flexibleSeconds
		if err := json.Unmarshal([]byte(in), &s); err != nil {
			t.Errorf("unmarshal %s: %v", in, err)
			continue
		}
		if float64(s) != want {
			t.Errorf("unmarshal %s = %v, want %v", in, float64(s), want)
		}
	}

	// Invalid input must error, not silently coerce.
	var s flexibleSeconds
	if err := json.Unmarshal([]byte(`"abc"`), &s); err == nil {
		t.Error("expected error for non-numeric string")
	}
	if err := json.Unmarshal([]byte(`null`), &s); err == nil {
		t.Error("expected error for null")
	}
}

func TestFlexibleSecondsInterval(t *testing.T) {
	// End-to-end: the device code response with a string interval.
	body := []byte(`{"device_auth_id":"da_1","user_code":"ABCD-EFGH","interval":"5"}`)
	var parsed struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Interval     flexibleSeconds `json:"interval"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	interval := time.Duration(float64(parsed.Interval) * float64(time.Second))
	if interval != 5*time.Second {
		t.Fatalf("interval = %v, want 5s", interval)
	}
}
