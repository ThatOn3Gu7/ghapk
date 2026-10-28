// Command shot drives the TUI without a terminal and writes each requested
// frame as an HTML page, so the look can be checked (and screenshotted).
//
//	shot script.txt outdir
//
// Script lines: size W H · key NAME · text STRING · click X Y · wheel X Y up|down
// · pump MS · sleep MS · shot NAME · print · set KEY VALUE
package main

import (
	"bufio"
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"
	"time"

	"ghapk/internal/engine"
	"ghapk/internal/term"
	"ghapk/internal/tui"
)

var keys = map[string]term.Event{
	"enter": {Kind: term.EvKey, Key: term.KeyEnter}, "esc": {Kind: term.EvKey, Key: term.KeyEsc},
	"tab": {Kind: term.EvKey, Key: term.KeyTab}, "up": {Kind: term.EvKey, Key: term.KeyUp},
	"down": {Kind: term.EvKey, Key: term.KeyDown}, "left": {Kind: term.EvKey, Key: term.KeyLeft},
	"right": {Kind: term.EvKey, Key: term.KeyRight}, "space": {Kind: term.EvKey, Key: term.KeyRune, Rune: ' '},
	"backspace": {Kind: term.EvKey, Key: term.KeyBackspace}, "pgdn": {Kind: term.EvKey, Key: term.KeyPgDn},
	"pgup": {Kind: term.EvKey, Key: term.KeyPgUp},
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: shot script.txt outdir")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	out := os.Args[2]
	os.MkdirAll(out, 0o755)
	eng := engine.New(map[string]string{})
	w, h := 100, 32
	app := tui.New(eng, w, h, tui.Options{})
	started := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.Fields(line)
		switch fs[0] {
		case "size":
			w, _ = strconv.Atoi(fs[1])
			h, _ = strconv.Atoi(fs[2])
			app.Resize(w, h)
		case "start":
			app.Start()
			started = true
		case "pump":
			ms, _ := strconv.Atoi(fs[1])
			app.Pump(time.Duration(ms) * time.Millisecond)
		case "sleep":
			ms, _ := strconv.Atoi(fs[1])
			time.Sleep(time.Duration(ms) * time.Millisecond)
		case "key":
			for _, k := range fs[1:] {
				if ev, ok := keys[k]; ok {
					app.Event(ev)
				} else if strings.HasPrefix(k, "ctrl-") {
					app.Event(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: rune(k[5]), Mod: term.ModCtrl})
				} else {
					for _, r := range k {
						app.Event(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: r})
					}
				}
				app.Frame() // lay out between keys, as the real loop does
			}
		case "text":
			for _, r := range strings.TrimPrefix(line, "text ") {
				app.Event(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: r})
				app.Frame()
			}
		case "click":
			x, _ := strconv.Atoi(fs[1])
			y, _ := strconv.Atoi(fs[2])
			app.Frame()
			app.Event(term.Event{Kind: term.EvMouse, X: x, Y: y, Button: term.BtnLeft, Action: term.MousePress})
		case "wheel":
			x, _ := strconv.Atoi(fs[1])
			y, _ := strconv.Atoi(fs[2])
			b := term.BtnWheelDown
			if fs[3] == "up" {
				b = term.BtnWheelUp
			}
			app.Frame()
			app.Event(term.Event{Kind: term.EvMouse, X: x, Y: y, Button: b, Action: term.MouseWheel})
		case "print":
			fmt.Print(app.Frame().String())
		case "shot":
			if !started {
				app.Start()
				started = true
			}
			b := app.Frame()
			fmt.Printf("== %s (%dx%d)\n", fs[1], b.W, b.H)
			os.WriteFile(out+"/"+fs[1]+".html", []byte(toHTML(b)), 0o644)
			os.WriteFile(out+"/"+fs[1]+".txt", []byte(b.String()), 0o644)
		}
		if app.Quit() {
			fmt.Println("(app quit)")
			return
		}
	}
}

func toHTML(b *term.Buffer) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><meta charset=utf-8><body style="margin:0;background:#0d1117"><pre id=t style="margin:0;padding:6px;display:inline-block;background:#0d1117;color:#c9d1d9;font:15px/1.25 'DejaVu Sans Mono',monospace">`)
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			c := b.Cells[y*b.W+x]
			if c.W == 0 {
				continue
			}
			st := c.Style
			fg, bg := st.FG, st.BG
			if st.Attr&term.Reverse != 0 {
				fg, bg = bg, fg
			}
			css := ""
			if r, g, bl, ok := fg.RGB8(); ok {
				css += fmt.Sprintf("color:rgb(%d,%d,%d);", r, g, bl)
			} else if st.Attr&term.Reverse != 0 {
				css += "color:#0d1117;"
			}
			if r, g, bl, ok := bg.RGB8(); ok {
				css += fmt.Sprintf("background:rgb(%d,%d,%d);", r, g, bl)
			} else if st.Attr&term.Reverse != 0 {
				css += "background:#c9d1d9;"
			}
			if st.Attr&term.Bold != 0 {
				css += "font-weight:bold;"
			}
			if st.Attr&term.Dim != 0 {
				css += "opacity:.55;"
			}
			if st.Attr&term.Italic != 0 {
				css += "font-style:italic;"
			}
			if st.Attr&term.Underline != 0 {
				css += "text-decoration:underline;"
			}
			r := c.R
			if r == 0 {
				r = ' '
			}
			s := html.EscapeString(string(r))
			if c.W == 2 {
				s = `<span style="display:inline-block;width:2ch">` + s + `</span>`
			}
			if css != "" {
				sb.WriteString(`<span style="` + css + `">` + s + `</span>`)
			} else {
				sb.WriteString(s)
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("</pre>")
	return sb.String()
}
