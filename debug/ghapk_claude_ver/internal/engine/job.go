package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ghapk/internal/apk"
	"ghapk/internal/device"
	"ghapk/internal/exit"
)

// StepState is where one pipeline step stands.
type StepState int

const (
	StepPending StepState = iota
	StepRunning
	StepOK
	StepWarn
	StepSkip
	StepFail
)

// Step ids, in pipeline order.
const (
	StDownload = "download"
	StVerify   = "verify"
	StUnpack   = "unpack"
	StValidate = "validate"
	StSave     = "save"
	StInstall  = "install"
	StConfirm  = "confirm"
)

// Step is one line of the job's progress rail.
type Step struct {
	ID     string
	Title  string
	State  StepState
	Detail string
	Start  time.Time
	End    time.Time
}

// JobState is the overall state.
type JobState int

const (
	JobRunning JobState = iota
	JobAsking
	JobDone
	JobFailed
	JobCancelled
)

// Progress describes a transfer in flight.
type Progress struct {
	Active bool
	Done   int64
	Total  int64
	Rate   float64 // bytes per second, smoothed
	ETA    time.Duration
	Method string
}

// Fraction is 0..1, or -1 when the total is unknown.
func (p Progress) Fraction() float64 {
	if p.Total <= 0 {
		return -1
	}
	f := float64(p.Done) / float64(p.Total)
	if f > 1 {
		f = 1
	}
	return f
}

// Result is what a finished job produced.
type Result struct {
	APK          string
	Copy         string
	Size         int64
	SHA          string
	Info         apk.Info
	Cert         apk.Cert
	HaveInfo     bool
	HandedOff    bool
	Method       string
	Verified     bool
	NotLanded    bool
	InstalledNow int64
	Diag         *device.Diagnosis
	Digest       string // "match", "mismatch", "none"
	Elapsed      time.Duration
}

// Question is a decision the job needs from a person.
type Question struct {
	Kind    string
	Title   string
	Body    string
	Options []string
	Default int
	Danger  bool // a wrong answer is costly: never auto-answered "yes"
}

// Prompter answers questions. Ask returns the chosen option index, or -1 to
// cancel the job. It must return when ctx is cancelled.
type Prompter interface {
	Ask(ctx context.Context, q Question) int
}

// JobOpts tunes one run.
type JobOpts struct {
	NoInstall bool // stop after saving the APK
	Yes       bool // take the best choice at every non-dangerous decision
}

// Job is one download-and-install run.
type Job struct {
	ID      int
	Src     Source
	Opts    JobOpts
	Created time.Time

	e      *Engine
	mu     sync.Mutex
	steps  []Step
	log    []string
	state  JobState
	prog   Progress
	res    Result
	err    error
	code   int
	q      *Question
	cancel context.CancelFunc
	done   chan struct{}
	notify func()
	prompt Prompter
	work   string
}

// JobView is an immutable snapshot for rendering.
type JobView struct {
	ID       int
	Src      Source
	Steps    []Step
	Log      []string
	State    JobState
	Prog     Progress
	Res      Result
	Err      error
	Code     int
	Question *Question
	Created  time.Time
}

// Snapshot copies the job's state.
func (j *Job) Snapshot() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := JobView{ID: j.ID, Src: j.Src, Steps: append([]Step(nil), j.steps...), Log: append([]string(nil), j.log...),
		State: j.state, Prog: j.prog, Res: j.res, Err: j.err, Code: j.code, Created: j.Created}
	if j.q != nil {
		q := *j.q
		v.Question = &q
	}
	return v
}

// Running reports whether the job is still in progress.
func (j *Job) Running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == JobRunning || j.state == JobAsking
}

// Cancel stops the job.
func (j *Job) Cancel() {
	j.mu.Lock()
	c := j.cancel
	j.mu.Unlock()
	if c != nil {
		c()
	}
}

// Wait blocks until the job ends.
func (j *Job) Wait() { <-j.done }

var jobSeq struct {
	sync.Mutex
	n int
}

// NewJob prepares a job for src. Call Start to run it.
func (e *Engine) NewJob(src Source, opts JobOpts) *Job {
	jobSeq.Lock()
	jobSeq.n++
	id := jobSeq.n
	jobSeq.Unlock()
	j := &Job{ID: id, Src: src, Opts: opts, Created: time.Now(), e: e, done: make(chan struct{})}
	titles := []struct{ id, t string }{
		{StDownload, "Download"}, {StVerify, "Verify checksum"}, {StUnpack, "Unpack"},
		{StValidate, "Validate APK"}, {StSave, "Save"}, {StInstall, "Hand to installer"}, {StConfirm, "Confirm on device"},
	}
	for _, t := range titles {
		j.steps = append(j.steps, Step{ID: t.id, Title: t.t})
	}
	return j
}

// Start runs the job in the background. onChange is called (from any
// goroutine) whenever the snapshot changes; p answers questions.
func (j *Job) Start(parent context.Context, p Prompter, onChange func()) {
	ctx, cancel := context.WithCancel(parent)
	j.mu.Lock()
	j.cancel, j.prompt, j.notify = cancel, p, onChange
	j.mu.Unlock()
	go func() {
		defer close(j.done)
		defer cancel()
		start := time.Now()
		j.run(ctx)
		j.mu.Lock()
		j.res.Elapsed = time.Since(start)
		if ctx.Err() != nil && j.state != JobFailed {
			j.state = JobCancelled
			j.code = exit.Interrupted
		} else if j.state == JobRunning || j.state == JobAsking {
			j.state = JobDone
		}
		j.mu.Unlock()
		j.changed()
	}()
}

// ---- internals used by the pipeline ------------------------------------------

func (j *Job) changed() {
	j.mu.Lock()
	f := j.notify
	j.mu.Unlock()
	if f != nil {
		f()
	}
}

func (j *Job) logf(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	j.mu.Lock()
	j.log = append(j.log, s)
	j.mu.Unlock()
	j.e.Log.Logf("job %d: %s", j.ID, s)
	j.changed()
}

func (j *Job) step(id string) *Step {
	for i := range j.steps {
		if j.steps[i].ID == id {
			return &j.steps[i]
		}
	}
	return nil
}

func (j *Job) begin(id, detail string) {
	j.mu.Lock()
	if s := j.step(id); s != nil {
		s.State, s.Detail, s.Start = StepRunning, detail, time.Now()
	}
	j.mu.Unlock()
	j.changed()
}

func (j *Job) detail(id, detail string) {
	j.mu.Lock()
	if s := j.step(id); s != nil {
		s.Detail = detail
	}
	j.mu.Unlock()
	j.changed()
}

func (j *Job) finish(id string, st StepState, detail string) {
	j.mu.Lock()
	if s := j.step(id); s != nil {
		s.State, s.End = st, time.Now()
		if detail != "" {
			s.Detail = detail
		}
	}
	j.mu.Unlock()
	j.changed()
}

// fail ends the job with an error and the exit code a script would see.
func (j *Job) fail(id string, code int, err error) {
	j.finish(id, StepFail, err.Error())
	j.mu.Lock()
	j.state, j.err, j.code = JobFailed, err, code
	j.mu.Unlock()
	j.logf("failed: %v", err)
}

func (j *Job) setProg(p Progress) {
	j.mu.Lock()
	j.prog = p
	j.mu.Unlock()
	j.changed()
}

// ask puts a question to the person and waits.
func (j *Job) ask(ctx context.Context, q Question) int {
	if j.prompt == nil {
		return q.Default
	}
	j.mu.Lock()
	j.q, j.state = &q, JobAsking
	j.mu.Unlock()
	j.changed()
	a := j.prompt.Ask(ctx, q)
	j.mu.Lock()
	j.q = nil
	if j.state == JobAsking {
		j.state = JobRunning
	}
	j.mu.Unlock()
	j.changed()
	return a
}

// ExitCode is the process exit code for the job.
func (v JobView) ExitCode() int {
	switch v.State {
	case JobDone:
		if v.Res.NotLanded {
			return exit.Install
		}
		return exit.OK
	case JobCancelled:
		return exit.Interrupted
	}
	if v.Code != 0 {
		return v.Code
	}
	return exit.API
}
