package device

import (
	"context"
	"runtime"
	"time"
)

func hostOS() string { return runtime.GOOS }

func newCmdTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
