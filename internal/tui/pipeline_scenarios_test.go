package tui

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func signedJobAPK(t *testing.T, file string, code int32) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, err := z.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(jobTestAXML("com.example.app", "1.0", code, 23, 34))
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	eocd := bytes.LastIndex(raw, []byte{0x50, 0x4b, 0x05, 0x06})
	cd := int(binary.LittleEndian.Uint32(raw[eocd+16:]))
	lp := func(b []byte) []byte {
		var o bytes.Buffer
		_ = binary.Write(&o, binary.LittleEndian, uint32(len(b)))
		o.Write(b)
		return o.Bytes()
	}
	signed := lp(append(lp(nil), lp(lp(der))...))
	value := lp(lp(signed))
	var pair bytes.Buffer
	_ = binary.Write(&pair, binary.LittleEndian, uint64(4+len(value)))
	_ = binary.Write(&pair, binary.LittleEndian, uint32(0xf05368c0))
	pair.Write(value)
	var block bytes.Buffer
	size := uint64(pair.Len() + 24)
	_ = binary.Write(&block, binary.LittleEndian, size)
	block.Write(pair.Bytes())
	_ = binary.Write(&block, binary.LittleEndian, size)
	_ = binary.Write(&block, binary.LittleEndian, uint64(0x20676953204b5041))
	_ = binary.Write(&block, binary.LittleEndian, uint64(0x3234206b636f6c42))
	tail := append([]byte(nil), raw[cd:]...)
	binary.LittleEndian.PutUint32(tail[eocd-cd+16:], uint32(cd+block.Len()))
	final := append(append(append([]byte(nil), raw[:cd]...), block.Bytes()...), tail...)
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, final, 0600); err != nil {
		t.Fatal(err)
	}
}

func scriptedJob(id int, spec jobSpec, answers ...int) (jobFinishedMsg, []jobQuestionMsg) {
	ch := make(chan tea.Msg, 64)
	go runJob(id, context.Background(), spec, ch)
	var result jobFinishedMsg
	var questions []jobQuestionMsg
	for msg := range ch {
		switch v := msg.(type) {
		case jobQuestionMsg:
			questions = append(questions, v)
			answer := 0
			if len(answers) > 0 {
				answer = answers[0]
				answers = answers[1:]
			}
			v.reply <- answer
		case jobFinishedMsg:
			result = v
		}
	}
	return result, questions
}

func TestSameVersionChoiceKeepsAPKOrReinstalls(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "saved.apk")
	jobTestAPK(t, path, 10)
	t.Setenv("INSTALL_MARKER", filepath.Join(root, "installed"))
	fakeCommand(t, bin, "adb", `case "$*" in *get-state*) echo device;; *ro.product.cpu.abilist*) echo arm64-v8a;; *ro.build.version.sdk*) echo 34;; *dumpsys*) echo 'versionCode=10 versionName=older-name';; *install*) echo yes > "$INSTALL_MARKER"; echo Success;; esac`)
	s := config.Defaults()
	s.PackageName = "com.example.app"
	spec := jobSpec{repo: "one/repo", settings: s, install: true, savedPath: path}
	r, q := scriptedJob(1, spec)
	if r.err != nil || len(q) != 1 || q[0].title != "Already installed" || q[0].options[0] != "Keep APK, skip install" {
		t.Fatalf("result=%+v questions=%+v", r, q)
	}
	if _, err := os.Stat(filepath.Join(root, "installed")); !os.IsNotExist(err) {
		t.Fatal("default launched installer")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("APK not kept")
	}
	r, q = scriptedJob(2, spec, 1)
	if r.err != nil || r.status != "installed" || len(q) != 1 {
		t.Fatalf("result=%+v questions=%+v", r, q)
	}
	if _, err := os.Stat(filepath.Join(root, "installed")); err != nil {
		t.Fatal("reinstall not launched")
	}
}

func TestSignatureClashRequiresTwoConfirmationsBeforeUninstall(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "signed.apk")
	signedJobAPK(t, path, 101)
	t.Setenv("TERMUX_VERSION", "1")
	t.Setenv("UNINSTALL_MARKER", filepath.Join(root, "uninstalled"))
	t.Setenv("INSTALL_MARKER", filepath.Join(root, "installed"))
	fakeCommand(t, bin, "termux-open", `echo yes > "$INSTALL_MARKER"`)
	fakeCommand(t, bin, "rish", fmt.Sprintf(`case "$*" in *'-c id'*) echo 'uid=2000(shell)';; *'get-current-user'*) echo %d;; *'list packages'*) echo 'package:com.example.app';; *'dumpsys package'*) echo 'versionCode=100 cert sha256: %s';; *'pm uninstall'*) echo yes > "$UNINSTALL_MARKER"; echo Success;; esac`, os.Getuid()/100000, strings.Repeat("a", 64)))
	s := config.Defaults()
	s.PackageName = "com.example.app"
	s.VerifyInstall = false
	spec := jobSpec{repo: "one/repo", settings: s, install: true, savedPath: path}
	r, q := scriptedJob(1, spec)
	if r.err != nil || len(q) != 1 || q[0].title != "Signing key mismatch" || len(q[0].options) != 3 {
		t.Fatalf("%+v %+v", r, q)
	}
	if _, err := os.Stat(filepath.Join(root, "uninstalled")); !os.IsNotExist(err) {
		t.Fatal("default uninstalled")
	}
	r, q = scriptedJob(2, spec, 2, 0)
	if r.err != nil || len(q) != 2 || q[1].title != "Erase app data?" || !strings.Contains(q[1].body[0], "com.example.app") {
		t.Fatalf("%+v %+v", r, q)
	}
	if _, err := os.Stat(filepath.Join(root, "uninstalled")); !os.IsNotExist(err) {
		t.Fatal("second cancel uninstalled")
	}
	r, q = scriptedJob(3, spec, 2, 1)
	if r.err != nil || r.status != "launched" || len(q) != 2 {
		t.Fatalf("%+v %+v", r, q)
	}
	if _, err := os.Stat(filepath.Join(root, "uninstalled")); err != nil {
		t.Fatal("confirmed uninstall missing")
	}
	if _, err := os.Stat(filepath.Join(root, "installed")); err != nil {
		t.Fatal("installer not launched")
	}
}

func TestPhoneQuestionScrollAndTouch(t *testing.T) {
	m := buildModel(t, 32, 18)
	m.startJob(jobSpec{})
	q := jobQuestionMsg{id: 1, title: "Signing key mismatch", body: []string{strings.Repeat("Read this warning before proceeding. ", 24)}, options: []string{"Cancel", "Install anyway", "Uninstall first"}, danger: 2, reply: make(chan int, 1)}
	mm, _ := m.Update(q)
	m = mm.(Model)
	if lipgloss.Width(m.modalBox()) > m.width || lipgloss.Height(m.modalBox()) > m.height {
		t.Fatalf("question does not fit: %s", m.modalBox())
	}
	if m.jobQuestionMaxScroll() == 0 {
		t.Fatal("long warning not scrollable")
	}
	// Selecting an action before reading advances the warning rather than accepting it.
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(Model)
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	select {
	case <-q.reply:
		t.Fatal("accepted hidden warning")
	default:
	}
	for m.jobQuestionScroll < m.jobQuestionMaxScroll() {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = mm.(Model)
	}
	lines := strings.Split(m.modalLayer(), "\n")
	x, y := -1, -1
	for row, line := range lines {
		plain := components.StripANSI(line)
		if p := strings.Index(plain, "Install anyway"); p >= 0 {
			x = components.VisibleWidth(plain[:p])
			y = row
			break
		}
	}
	if x < 0 || m.confirmButtonHit(x, y, m.confirm.Options) != 1 {
		t.Fatalf("option not tappable: %s", m.modalLayer())
	}
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	m = mm.(Model)
	select {
	case got := <-q.reply:
		if got != 1 {
			t.Fatalf("choice=%d", got)
		}
	default:
		t.Fatal("touch did not select")
	}
	for _, j := range m.jobs {
		j.cancel()
	}
}

func TestDowngradeAsksAndPassesExplicitADBFlag(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "old.apk")
	jobTestAPK(t, path, 10)
	marker := filepath.Join(root, "args")
	t.Setenv("INSTALL_MARKER", marker)
	fakeCommand(t, bin, "adb", `case "$*" in *get-state*) echo device;; *ro.product.cpu.abilist*) echo arm64-v8a;; *ro.build.version.sdk*) echo 34;; *dumpsys*) echo 'versionCode=50 versionName=5.0';; *install*) echo "$*" > "$INSTALL_MARKER"; echo Success;; esac`)
	s := config.Defaults()
	s.PackageName = "com.example.app"
	s.Downgrade = "auto"
	spec := jobSpec{repo: "one/repo", settings: s, install: true, savedPath: path}
	r, q := scriptedJob(1, spec)
	if r.err != nil || len(q) != 1 || q[0].title != "Downgrade warning" {
		t.Fatalf("%+v %+v", r, q)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("default installed downgrade")
	}
	r, _ = scriptedJob(2, spec, 1)
	data, err := os.ReadFile(marker)
	if err != nil || r.err != nil || !strings.Contains(string(data), "install -r -d") {
		t.Fatalf("result=%+v args=%s err=%v", r, data, err)
	}
	s.Downgrade = "never"
	spec.settings = s
	_ = os.Remove(marker)
	r, q = scriptedJob(3, spec, 1)
	if r.err == nil || len(q) != 0 {
		t.Fatalf("blocked downgrade: %+v %+v", r, q)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("blocked downgrade installed")
	}
}

func TestDifferentVariantQueriesActualAPKPackage(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "app.apk")
	jobTestAPK(t, path, 10)
	t.Setenv("QUERY_MARKER", filepath.Join(root, "queries"))
	fakeCommand(t, bin, "adb", `case "$*" in *get-state*) echo device;; *ro.product.cpu.abilist*) echo arm64-v8a;; *ro.build.version.sdk*) echo 34;; *dumpsys*) echo "$*" >> "$QUERY_MARKER"; echo 'Unable to find package';; esac`)
	s := config.Defaults()
	s.PackageName = "com.example.app.debug"
	r, q := scriptedJob(1, jobSpec{repo: "one/repo", settings: s, install: true, savedPath: path})
	if r.err != nil || len(q) != 1 || q[0].title != "Package name mismatch" {
		t.Fatalf("%+v %+v", r, q)
	}
	calls, err := os.ReadFile(filepath.Join(root, "queries"))
	if err != nil || !strings.Contains(string(calls), "dumpsys package com.example.app") || strings.Contains(string(calls), "com.example.app.debug") {
		t.Fatalf("queried wrong app: %s %v", calls, err)
	}
}

func TestEqualRankedAPKsAskWhichToExtract(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "two.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range []string{"first-debug.apk", "second-debug.apk"} {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = w.Write([]byte("test apk " + name))
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	t.Setenv("JOB_ZIP", path)
	fakeCommand(t, bin, "gh", `cat "$JOB_ZIP"`)
	s := config.Defaults()
	s.VerifyDigest = false
	r, q := scriptedJob(1, jobSpec{repo: "one/repo", settings: s, choice: planChoice{art: ghapi.Artifact{ID: 1, Name: "android"}}}, 1, 2)
	if r.err != nil || len(q) != 2 || q[0].title != "Several APKs" || !strings.Contains(q[1].body[0], "second-debug.apk") || !strings.Contains(r.path, "second-debug") {
		t.Fatalf("result=%+v questions=%+v", r, q)
	}
}

func TestQuestionFooterAndLongPackageStaySafeOnPhone(t *testing.T) {
	m := buildModel(t, 32, 18)
	m.startJob(jobSpec{})
	q := jobQuestionMsg{id: 1, title: "Erase app data?", body: []string{"Uninstall " + strings.Repeat("a", 200) + ".example? Data will be erased."}, options: []string{"Cancel", "Uninstall and install"}, danger: 1, reply: make(chan int, 1)}
	mm, _ := m.Update(q)
	m = mm.(Model)
	if lipgloss.Height(m.modalBox()) > m.height || lipgloss.Width(m.modalBox()) > m.width {
		t.Fatalf("overflow: %s", m.modalBox())
	}
	// Footer is an actual touch target, not merely a hint. Default is Cancel.
	lines := strings.Split(m.modalLayer(), "\n")
	x, y := -1, -1
	for row, line := range lines {
		p := components.StripANSI(line)
		if i := strings.Index(p, "Esc cancel"); i >= 0 {
			x = components.VisibleWidth(p[:i])
			y = row
			break
		}
	}
	if x < 0 {
		t.Fatalf("footer missing: %s", m.modalLayer())
	}
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	m = mm.(Model)
	select {
	case got := <-q.reply:
		if got != 0 {
			t.Fatalf("choice %d", got)
		}
	default:
		t.Fatal("cancel hint did not cancel")
	}
	for _, j := range m.jobs {
		j.cancel()
	}
}

func TestUnknownBaselineCannotVerifyInteractiveInstall(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "saved.apk")
	jobTestAPK(t, path, 10)
	t.Setenv("TERMUX_VERSION", "1")
	t.Setenv("INSTALL_MARKER", filepath.Join(root, "opened"))
	fakeCommand(t, bin, "termux-open", `echo opened > "$INSTALL_MARKER"`)
	fakeCommand(t, bin, "dumpsys", `if [ -f "$INSTALL_MARKER" ]; then echo 'versionCode=10'; else echo 'service temporarily unavailable'; fi`)
	s := config.Defaults()
	s.PackageName = "com.example.app"
	r, q := scriptedJob(1, jobSpec{repo: "one/repo", settings: s, install: true, savedPath: path})
	if r.err != nil || r.status != "launched" || len(q) != 0 {
		t.Fatalf("unknown baseline must stay unconfirmed: %+v %+v", r, q)
	}
}
