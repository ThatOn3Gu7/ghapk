package ghub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"
)

// ErrStalled is returned when a download received no bytes for too long.
var ErrStalled = errors.New("download stalled")

// Progress is told how many bytes have arrived.
type Progress func(done int64)

// Stream runs `gh <args>` and copies its stdout into dest, calling prog as
// bytes arrive. If no byte arrives for stall (0 = never), the process is killed
// and ErrStalled is returned. The file is written as given; the caller moves it
// into place once it has verified it.
func (c *Client) Stream(ctx context.Context, dest string, stall time.Duration, prog Progress, args ...string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := c.command(cctx, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	var errBuf limitedBuf
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return err
	}
	waitErr := make(chan error, 1)
	go func() {
		e := cmd.Wait()
		pw.CloseWithError(e) // readers see the exit status as the stream error
		waitErr <- e
	}()

	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	stalled := make(chan struct{})
	if stall > 0 {
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-cctx.Done():
					return
				case <-t.C:
					if time.Since(time.Unix(0, last.Load())) > stall {
						close(stalled)
						cancel()
						return
					}
				}
			}
		}()
	}

	buf := make([]byte, 64*1024)
	var done int64
	var copyErr error
	for {
		n, rerr := pr.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				copyErr = werr
				cancel()
				break
			}
			done += int64(n)
			last.Store(time.Now().UnixNano())
			if prog != nil {
				prog(done)
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				copyErr = rerr
			}
			break
		}
	}
	pr.Close()
	<-waitErr
	select {
	case <-stalled:
		return ErrStalled
	default:
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if copyErr != nil {
		cls, st := Classify(1, errBuf.String())
		if cls == ClassUnknown && st == 0 {
			return fmt.Errorf("gh failed: %s", firstLine(errBuf.String(), copyErr.Error()))
		}
		return &Error{Class: cls, Status: st, Msg: errBuf.String()}
	}
	return f.Sync()
}

// DownloadArtifact streams an artifact's zip via `gh api`.
func (c *Client) DownloadArtifact(ctx context.Context, repo string, id int64, dest string, stall time.Duration, prog Progress) error {
	return c.Stream(ctx, dest, stall, prog, "api", fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", repo, id))
}

// DownloadAsset streams a release asset.
func (c *Client) DownloadAsset(ctx context.Context, repo string, id int64, dest string, stall time.Duration, prog Progress) error {
	return c.Stream(ctx, dest, stall, prog, "api", "-H", "Accept: application/octet-stream",
		fmt.Sprintf("repos/%s/releases/assets/%d", repo, id))
}

// DownloadViaRunDownload is the last-resort fallback: `gh run download` unpacks
// the artifact itself into dir.
func (c *Client) DownloadViaRunDownload(ctx context.Context, repo string, runID int64, name, dir string) error {
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_, se, rc := c.Run(ctx, 15*time.Minute, "run", "download", strconv.FormatInt(runID, 10), "-R", repo, "--name", name, "--dir", dir)
	if rc != 0 {
		cls, st := Classify(rc, se)
		return &Error{Class: cls, Status: st, Msg: firstLine(se, "gh run download failed")}
	}
	return nil
}

// DownloadViaCurl streams the artifact with curl, giving it the token through
// stdin so it never appears on a command line or in the process list.
func (c *Client) DownloadViaCurl(ctx context.Context, repo string, id int64, dest string, stall time.Duration, prog Progress) error {
	curl, err := exec.LookPath("curl")
	if err != nil {
		return err
	}
	tok := c.Token(ctx)
	if tok == "" {
		return errors.New("no token for curl")
	}
	base := "https://api.github.com"
	if c.Host != "" && c.Host != "github.com" {
		base = "https://" + c.Host + "/api/v3"
	}
	url := fmt.Sprintf("%s/repos/%s/actions/artifacts/%d/zip", base, repo, id)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(cctx, curl, "-sS", "--fail", "--location", "--retry", "2",
		"--connect-timeout", "15", "--config", "-",
		"-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28",
		"-o", dest, url)
	cmd.Stdin = stringsReader("header = \"Authorization: Bearer " + tok + "\"\n")
	var eb limitedBuf
	cmd.Stderr = &eb
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var lastSize int64 = -1
	lastMove := time.Now()
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case e := <-done:
			if e != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("curl: %s", firstLine(eb.String(), e.Error()))
			}
			if st, err := os.Stat(dest); err == nil && prog != nil {
				prog(st.Size())
			}
			return nil
		case <-t.C:
			if st, err := os.Stat(dest); err == nil {
				if st.Size() != lastSize {
					lastSize, lastMove = st.Size(), time.Now()
					if prog != nil {
						prog(st.Size())
					}
				}
			}
			if stall > 0 && time.Since(lastMove) > stall {
				cancel()
				<-done
				return ErrStalled
			}
		}
	}
}
