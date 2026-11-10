package ghapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Class says what kind of failure a `gh api` call ended in. The UI uses it to
// pick a plain-language message and a next step instead of showing gh's raw
// stderr, and the retry loop uses it to decide what is worth trying again.
type Class string

const (
	ClassAuth      Class = "auth"      // 401: gh is signed out or the token is dead
	ClassRateLimit Class = "ratelimit" // 429, or 403 with "rate limit"
	ClassForbidden Class = "forbidden" // 403: token lacks scope / no access
	ClassMissing   Class = "missing"   // 404
	ClassGone      Class = "gone"      // 410: expired artifact
	ClassServer    Class = "server"    // 5xx
	ClassNetwork   Class = "network"   // cannot reach GitHub
	ClassTimeout   Class = "timeout"   // gh took too long
	ClassOther     Class = "other"
)

// Error is a classified `gh api` failure. Its Error text keeps the long-standing
// "gh api <endpoint>: <message>" shape so logs and older checks keep working;
// Title and Hint are what the interface shows people.
type Error struct {
	Class    Class
	Status   int    // HTTP status when gh reported one, else 0
	Endpoint string // API path without its query string
	Msg      string // first line of gh's own message
	Err      error  // underlying process error, when any
}

func (e *Error) Error() string {
	msg := e.Msg
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	return "gh api " + e.Endpoint + ": " + msg
}

func (e *Error) Unwrap() error { return e.Err }

// Title is a short, plain-language description of what went wrong.
func (e *Error) Title() string {
	switch e.Class {
	case ClassAuth:
		return "GitHub says you are signed out"
	case ClassRateLimit:
		return "GitHub request limit reached"
	case ClassForbidden:
		return "GitHub refused access"
	case ClassMissing:
		return "Not found on GitHub"
	case ClassGone:
		return "No longer available (expired)"
	case ClassServer:
		return "GitHub had a server error"
	case ClassNetwork:
		return "Cannot reach GitHub"
	case ClassTimeout:
		return "GitHub took too long to answer"
	}
	if e.Msg != "" {
		return e.Msg
	}
	return "Request failed"
}

// Hint is the most useful next step for this failure. For a 404 on an Actions
// endpoint it says so explicitly: the repository itself is reachable, so a
// stale branch/workflow setting is far more likely than a typo in the repo.
func (e *Error) Hint() string {
	switch e.Class {
	case ClassAuth:
		return "Run: gh auth login"
	case ClassRateLimit:
		return "Wait for the limit to reset, then press r. Cached answers keep costing nothing."
	case ClassForbidden:
		return "Private repos need the repo scope: gh auth refresh -s repo,workflow"
	case ClassMissing:
		if strings.Contains(e.Endpoint, "/actions/workflows/") {
			return "That workflow is not in this repo. Clear the workflow filter or pick one from the list."
		}
		if strings.Contains(e.Endpoint, "/actions/") {
			return "The repository exists, so a saved branch or workflow setting is probably stale. Clear the filters."
		}
		return "Check the owner/name, and that gh is signed in to an account with access."
	case ClassGone:
		return "GitHub deletes artifacts after the retention period. Re-run the workflow for a fresh one."
	case ClassServer:
		return "Usually brief. Wait a minute and press r."
	case ClassNetwork, ClassTimeout:
		return "Check your connection and press r to retry."
	}
	return ""
}

// Retryable reports whether trying the same request again can plausibly help.
// A timeout is deliberately not retried: each attempt already waited the full
// deadline, and three of those would freeze the screen for minutes.
func (e *Error) Retryable() bool { return e.Class == ClassServer || e.Class == ClassNetwork }

var httpStatusRe = regexp.MustCompile(`(?i)\bHTTP\s+(\d{3})\b`)

// classify turns gh's stderr (and the process error) into an *Error.
func classify(endpoint, stderr string, runErr error, timedOut bool) *Error {
	msg := firstLine(stderr)
	e := &Error{Class: ClassOther, Endpoint: stripQuery(endpoint), Msg: msg, Err: runErr}
	if e.Msg == "" && runErr != nil {
		e.Msg = runErr.Error()
	}
	if m := httpStatusRe.FindStringSubmatch(stderr); m != nil {
		e.Status, _ = strconv.Atoi(m[1])
	}
	low := strings.ToLower(stderr)
	switch {
	case timedOut:
		e.Class = ClassTimeout
	case e.Status == 401 || strings.Contains(low, "gh auth login") || strings.Contains(low, "authentication required"):
		e.Class = ClassAuth
	case e.Status == 429 || (e.Status == 403 && strings.Contains(low, "rate limit")) || strings.Contains(low, "secondary rate limit"):
		e.Class = ClassRateLimit
	case e.Status == 403:
		e.Class = ClassForbidden
	case e.Status == 404:
		e.Class = ClassMissing
	case e.Status == 410:
		e.Class = ClassGone
	case e.Status >= 500 && e.Status < 600:
		e.Class = ClassServer
	case connectionFailure(stderr):
		e.Class = ClassNetwork
	}
	return e
}

// logLine is the compact form written to the activity log.
func (e *Error) logLine() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s (HTTP %d) %s", e.Class, e.Status, e.Msg)
	}
	return fmt.Sprintf("%s %s", e.Class, e.Msg)
}

func stripQuery(endpoint string) string {
	if i := strings.IndexByte(endpoint, '?'); i >= 0 {
		return endpoint[:i]
	}
	return endpoint
}

// AsError returns the classified error inside err, if there is one.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// IsClass reports whether err is a classified failure of class c.
func IsClass(err error, c Class) bool {
	e, ok := AsError(err)
	return ok && e.Class == c
}

// Describe splits any error into a short title and an optional hint, ready for
// a toast or an error panel. Unclassified errors keep their own text.
func Describe(err error) (title, hint string) {
	if err == nil {
		return "", ""
	}
	if e, ok := AsError(err); ok {
		return e.Title(), e.Hint()
	}
	if errors.Is(err, context.Canceled) {
		return "Cancelled", ""
	}
	var nf *WorkflowNotFoundError
	if errors.As(err, &nf) {
		return nf.Error(), "Clear the workflow filter, or pick one of the listed workflows."
	}
	return strings.TrimSpace(err.Error()), ""
}

// WorkflowNotFoundError means a workflow name from the settings matches nothing
// in the repository. Known lists what does exist so the message can suggest it.
type WorkflowNotFoundError struct {
	Name  string
	Known []string
}

func (e *WorkflowNotFoundError) Error() string {
	s := fmt.Sprintf("no workflow matching %q", e.Name)
	if len(e.Known) > 0 {
		k := e.Known
		more := ""
		if len(k) > 4 {
			k, more = k[:4], fmt.Sprintf(" +%d more", len(e.Known)-4)
		}
		s += " (this repo has: " + strings.Join(k, ", ") + more + ")"
	}
	return s
}
