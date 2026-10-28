package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type RateLimit struct {
	Remaining int
	Limit     int
	Reset     time.Time
}

// CoreRateLimit uses gh's existing authentication; failures remain unknown.
func CoreRateLimit(parent context.Context) (RateLimit, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "api", "rate_limit")
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return RateLimit{}, err
	}
	var data struct {
		Resources struct {
			Core struct {
				Remaining int   `json:"remaining"`
				Limit     int   `json:"limit"`
				Reset     int64 `json:"reset"`
			} `json:"core"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(out.Bytes(), &data); err != nil {
		return RateLimit{}, err
	}
	c := data.Resources.Core
	if c.Limit <= 0 || c.Reset <= 0 || c.Remaining < 0 {
		return RateLimit{}, fmt.Errorf("invalid rate limit response")
	}
	return RateLimit{Remaining: c.Remaining, Limit: c.Limit, Reset: time.Unix(c.Reset, 0)}, nil
}
