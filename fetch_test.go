package goproxy

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMarshalInfo(t *testing.T) {
	info := struct {
		Version string
		Time    time.Time
	}{"v1.0.0", time.Now()}

	got := marshalInfo(info.Version, info.Time)

	info.Time = info.Time.UTC()

	want, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("unexpected error %q", err)
	}

	if got != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
