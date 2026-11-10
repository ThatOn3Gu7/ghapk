// Package version extracts and compares version strings found in artifact and
// APK names, with the same rules the original script used: dates and build
// stamps are not versions, a v-prefixed token wins, and a pre-release tag sorts
// below the release.
package version

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	vPrefixed = regexp.MustCompile(`[vV][0-9]+(?:\.[0-9]+){1,3}`)
	plain     = regexp.MustCompile(`[0-9]+(?:\.[0-9]+){1,3}`)
	yearish   = regexp.MustCompile(`^[0-9]{4,}$`)
	dmy       = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}\.[0-9]{4}$`)
)

// Extract returns the version in s ("1.2.3"), or "" if there is none.
// "MyApp_2024.06_debug.apk" has none: 2024.06 is a date. "app-v1.2.3" is 1.2.3.
func Extract(s string) string {
	if m := vPrefixed.FindString(s); m != "" {
		return m[1:]
	}
	for _, m := range plain.FindAllString(s, -1) {
		first := strings.SplitN(m, ".", 2)[0]
		if yearish.MatchString(first) || dmy.MatchString(m) {
			continue
		}
		return m
	}
	return ""
}

// Compare returns 1 if a > b, -1 if a < b and 0 if equal. Missing components
// count as 0 ("1.2" == "1.2.0"). A component with a tag ("3-rc1") sorts below
// the same number without one.
func Compare(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		xn, xt := split(x)
		yn, yt := split(y)
		if xn != yn {
			if xn > yn {
				return 1
			}
			return -1
		}
		if xt == yt {
			continue
		}
		switch {
		case xt == "":
			return 1
		case yt == "":
			return -1
		case xt > yt:
			return 1
		default:
			return -1
		}
	}
	return 0
}

func split(c string) (int64, string) {
	i := 0
	for i < len(c) && c[i] >= '0' && c[i] <= '9' {
		i++
	}
	var n int64
	if i > 0 {
		n, _ = strconv.ParseInt(c[:i], 10, 64)
	}
	return n, c[i:]
}
