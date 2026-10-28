package ui

import (
	"strings"
	"testing"

	"ghapk/internal/term"
)

func TestWrap(t *testing.T) {
	got := Wrap("the quick brown fox jumps over the lazy dog", 16)
	for _, l := range got {
		if term.StringWidth(l) > 16 {
			t.Errorf("line too wide: %q", l)
		}
	}
	if strings.Join(got, " ") != "the quick brown fox jumps over the lazy dog" {
		t.Errorf("lost words: %q", got)
	}
	if got := Wrap("averyveryveryverylongwordhere", 8); len(got) < 3 {
		t.Errorf("long word not cut: %q", got)
	}
	if got := Wrap("a\n\nb", 10); len(got) != 3 {
		t.Errorf("newlines: %q", got)
	}
}

func TestListScrolls(t *testing.T) {
	var l List
	l.SetN(50)
	for i := 0; i < 30; i++ {
		l.Move(1, 10)
	}
	if l.Cur != 30 || l.Off != 21 {
		t.Fatalf("cur %d off %d", l.Cur, l.Off)
	}
	l.Set(0, 10)
	if l.Off != 0 {
		t.Fatalf("off %d", l.Off)
	}
	l.Move(-5, 10)
	if l.Cur != 0 {
		t.Fatal("went negative")
	}
	l.SetN(3)
	l.Set(99, 10)
	if l.Cur != 2 {
		t.Fatalf("cur %d", l.Cur)
	}
}

func TestInputEditing(t *testing.T) {
	in := NewInput("")
	for _, r := range "hello world" {
		in.Handle(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: r})
	}
	in.Handle(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: 'w', Mod: term.ModCtrl})
	if in.String() != "hello " {
		t.Fatalf("%q", in.String())
	}
	in.Handle(term.Event{Kind: term.EvKey, Key: term.KeyLeft})
	in.Handle(term.Event{Kind: term.EvKey, Key: term.KeyBackspace})
	if in.String() != "hell " {
		t.Fatalf("%q", in.String())
	}
	in.Handle(term.Event{Kind: term.EvPaste, Text: "a\nb"})
	if !strings.Contains(in.String(), "a b") {
		t.Fatalf("paste: %q", in.String())
	}
}

func TestThemesComplete(t *testing.T) {
	for _, n := range Names() {
		th := NewTheme(n)
		if th.Name != n {
			t.Errorf("%s -> %s", n, th.Name)
		}
	}
	if NewTheme("nonsense").Name != "default" {
		t.Error("unknown theme should fall back")
	}
}

func TestFormat(t *testing.T) {
	if Bytes(1536) != "1.5 KB" || Bytes(3<<20) != "3.0 MB" {
		t.Errorf("%s %s", Bytes(1536), Bytes(3<<20))
	}
	if Plural(1, "job") != "1 job" || Plural(2, "job") != "2 jobs" {
		t.Error("plural")
	}
}
