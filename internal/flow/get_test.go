package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
)

func scanRuns(n int) []ghapi.Run {
	runs := make([]ghapi.Run, n)
	for i := range runs {
		runs[i] = ghapi.Run{ID: int64(i + 1), RunNumber: i + 1}
	}
	return runs
}

func firstArtifact(_ ghapi.Run, arts []ghapi.Artifact) (ghapi.Artifact, bool) {
	if len(arts) == 0 {
		return ghapi.Artifact{}, false
	}
	return arts[0], true
}

func TestScanArtifactsNewestMatchWithBoundedConcurrency(t *testing.T) {
	var inFlight, maxInFlight int32
	fetch := func(_ context.Context, _ string, runID int64) ([]ghapi.Artifact, error) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			peak := atomic.LoadInt32(&maxInFlight)
			if cur <= peak || atomic.CompareAndSwapInt32(&maxInFlight, peak, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		if runID == 2 {
			return []ghapi.Artifact{{ID: 20, Name: "app-arm64-debug.apk"}}, nil
		}
		return nil, nil
	}
	art, run, empty, unseen, err := scanArtifacts(context.Background(), "owner/repo", scanRuns(10), fetch, firstArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != 2 || art.ID != 20 || empty != 1 || unseen != 0 {
		t.Fatalf("run=%d art=%d empty=%d unseen=%d, want newest match on run 2", run.ID, art.ID, empty, unseen)
	}
	if maxInFlight > artifactScanConcurrency {
		t.Fatalf("max in flight = %d, want <= %d", maxInFlight, artifactScanConcurrency)
	}
	if maxInFlight < 2 {
		t.Fatalf("requests were not parallel (max in flight = %d)", maxInFlight)
	}
}

func TestScanArtifactsCountsUnreadableRunsAndStopsEarly(t *testing.T) {
	var calls int32
	fetch := func(_ context.Context, _ string, runID int64) ([]ghapi.Artifact, error) {
		atomic.AddInt32(&calls, 1)
		if runID == 1 {
			return nil, errors.New("temporary failure")
		}
		if runID == 3 {
			return []ghapi.Artifact{{ID: 30, Name: "app.apk"}}, nil
		}
		return nil, nil
	}
	art, run, empty, unseen, err := scanArtifacts(context.Background(), "owner/repo", scanRuns(20), fetch, firstArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != 3 || art.ID != 30 || empty != 1 || unseen != 1 {
		t.Fatalf("run=%d art=%d empty=%d unseen=%d, want run 3 after one failure and one empty run", run.ID, art.ID, empty, unseen)
	}
	if got := atomic.LoadInt32(&calls); got != artifactScanConcurrency {
		t.Fatalf("fetched %d runs, want only the first batch (%d)", got, artifactScanConcurrency)
	}
}

func TestScanArtifactsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int32
	fetch := func(context.Context, string, int64) ([]ghapi.Artifact, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	}
	if _, _, _, _, err := scanArtifacts(ctx, "owner/repo", scanRuns(3), fetch, firstArtifact); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("made %d requests after cancellation", calls)
	}
}
