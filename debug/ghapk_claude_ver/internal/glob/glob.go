// Package glob implements bash-style glob matching with extglob, the way the
// original ghapk script matched artifact and file names: case-insensitive,
// anchored, and `*` also crosses `/`.
//
// Supported syntax: *  ?  [abc]  [a-z]  [!x]  [^x]  and the extglob groups
// @(a|b)  ?(a|b)  *(a|b)  +(a|b)  !(a|b).
package glob

import "strings"

type kind int

const (
	kLit   kind = iota
	kAny        // ?
	kStar       // *
	kClass      // [...]
	kGroup      // extglob group
)

type node struct {
	k    kind
	lit  rune
	set  []rng
	neg  bool
	op   byte // '@' '?' '*' '+' '!'
	alts [][]node
}

type rng struct{ lo, hi rune }

// Pattern is a compiled glob.
type Pattern struct{ nodes []node }

// Compile parses a pattern. Matching is case-insensitive, so the pattern is
// lowered up front. A malformed pattern never fails: broken syntax degrades to
// literal text, which is what bash does too.
func Compile(p string) *Pattern {
	r := []rune(strings.ToLower(strings.TrimSpace(p)))
	n, _ := parse(r, 0, false)
	return &Pattern{nodes: n}
}

func parse(r []rune, i int, inGroup bool) ([]node, int) {
	var out []node
	for i < len(r) {
		c := r[i]
		if inGroup && (c == '|' || c == ')') {
			return out, i
		}
		switch {
		case c == '*' && i+1 < len(r) && r[i+1] == '(':
			g, ni, ok := parseGroup(r, i+2, '*')
			if ok {
				out, i = append(out, g), ni
				continue
			}
			out = append(out, node{k: kStar})
			i++
		case (c == '@' || c == '?' || c == '+' || c == '!') && i+1 < len(r) && r[i+1] == '(':
			g, ni, ok := parseGroup(r, i+2, byte(c))
			if ok {
				out, i = append(out, g), ni
				continue
			}
			if c == '?' {
				out = append(out, node{k: kAny})
			} else {
				out = append(out, node{k: kLit, lit: c})
			}
			i++
		case c == '*':
			for i < len(r) && r[i] == '*' && !(i+1 < len(r) && r[i+1] == '(') {
				i++
			}
			out = append(out, node{k: kStar})
		case c == '?':
			out = append(out, node{k: kAny})
			i++
		case c == '[':
			nd, ni, ok := parseClass(r, i)
			if ok {
				out, i = append(out, nd), ni
				continue
			}
			out = append(out, node{k: kLit, lit: c})
			i++
		case c == '\\' && i+1 < len(r):
			out = append(out, node{k: kLit, lit: r[i+1]})
			i += 2
		default:
			out = append(out, node{k: kLit, lit: c})
			i++
		}
	}
	return out, i
}

func parseGroup(r []rune, i int, op byte) (node, int, bool) {
	var alts [][]node
	for {
		seq, ni := parse(r, i, true)
		alts = append(alts, seq)
		i = ni
		if i >= len(r) {
			return node{}, 0, false // unterminated
		}
		if r[i] == '|' {
			i++
			continue
		}
		// r[i] == ')'
		return node{k: kGroup, op: op, alts: alts}, i + 1, true
	}
}

func parseClass(r []rune, i int) (node, int, bool) {
	j := i + 1
	nd := node{k: kClass}
	if j < len(r) && (r[j] == '!' || r[j] == '^') {
		nd.neg = true
		j++
	}
	first := true
	for j < len(r) {
		if r[j] == ']' && !first {
			return nd, j + 1, true
		}
		first = false
		lo := r[j]
		if j+2 < len(r) && r[j+1] == '-' && r[j+2] != ']' {
			nd.set = append(nd.set, rng{lo, r[j+2]})
			j += 3
			continue
		}
		nd.set = append(nd.set, rng{lo, lo})
		j++
	}
	return node{}, 0, false
}

// Match reports whether the whole of s matches the pattern.
func (p *Pattern) Match(s string) bool {
	rs := []rune(strings.ToLower(s))
	return m(p.nodes, rs, 0, func(j int) bool { return j == len(rs) })
}

// m matches nodes against s[j:], calling k with the end position of every way
// the nodes can match. Continuation-passing keeps backtracking simple.
func m(ns []node, s []rune, j int, k func(int) bool) bool {
	if len(ns) == 0 {
		return k(j)
	}
	n, rest := ns[0], ns[1:]
	switch n.k {
	case kLit:
		return j < len(s) && s[j] == n.lit && m(rest, s, j+1, k)
	case kAny:
		return j < len(s) && m(rest, s, j+1, k)
	case kClass:
		if j >= len(s) {
			return false
		}
		in := false
		for _, r := range n.set {
			if s[j] >= r.lo && s[j] <= r.hi {
				in = true
				break
			}
		}
		return in != n.neg && m(rest, s, j+1, k)
	case kStar:
		for e := len(s); e >= j; e-- {
			if m(rest, s, e, k) {
				return true
			}
		}
		return false
	case kGroup:
		return group(n, rest, s, j, k)
	}
	return false
}

func group(n node, rest []node, s []rune, j int, k func(int) bool) bool {
	one := func(from int, cont func(int) bool) bool {
		for _, alt := range n.alts {
			if m(alt, s, from, cont) {
				return true
			}
		}
		return false
	}
	switch n.op {
	case '@':
		return one(j, func(e int) bool { return m(rest, s, e, k) })
	case '?':
		if m(rest, s, j, k) {
			return true
		}
		return one(j, func(e int) bool { return m(rest, s, e, k) })
	case '*', '+':
		var rep func(from int, count int) bool
		rep = func(from int, count int) bool {
			if (n.op == '*' || count > 0) && m(rest, s, from, k) {
				return true
			}
			return one(from, func(e int) bool {
				if e == from { // no progress: stop an endless loop
					return false
				}
				return rep(e, count+1)
			})
		}
		return rep(j, 0)
	case '!':
		for e := j; e <= len(s); e++ {
			matched := false
			for _, alt := range n.alts {
				if m(alt, s[:e], j, func(x int) bool { return x == e }) {
					matched = true
					break
				}
			}
			if !matched && m(rest, s, e, k) {
				return true
			}
		}
		return false
	}
	return false
}

// MatchAny reports whether s matches at least one of the comma-separated glob
// patterns in the list. An empty list matches nothing.
func MatchAny(s string, pats []*Pattern) bool {
	for _, p := range pats {
		if p.Match(s) {
			return true
		}
	}
	return false
}

// SplitList turns "a, b,,c" into compiled patterns, dropping empties.
func SplitList(list string) []*Pattern {
	var out []*Pattern
	for _, f := range strings.Split(list, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, Compile(f))
		}
	}
	return out
}
