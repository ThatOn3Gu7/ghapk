package ghapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoreRateLimit(t *testing.T) {
	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(gh, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	write(`printf '%s' '{"resources":{"core":{"remaining":42,"limit":100,"reset":1700000000}}}'`)
	got, err := CoreRateLimit(context.Background())
	if err != nil || got.Remaining != 42 || got.Limit != 100 || !got.Reset.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("%+v %v", got, err)
	}
	write(`printf '%s' '{"resources":{"core":{}}}'`)
	if _, err = CoreRateLimit(context.Background()); err == nil {
		t.Fatal("missing values must be unknown")
	}
	write(`exit 1`)
	if _, err = CoreRateLimit(context.Background()); err == nil {
		t.Fatal("failed gh must not return a valid rate")
	}
}
