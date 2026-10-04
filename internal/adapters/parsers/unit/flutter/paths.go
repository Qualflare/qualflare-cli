package flutter

import (
	"net/url"
	"path"
	"strings"
)

// normalizePath strips a file:// prefix (decoding percent escapes) and turns
// backslashes into slashes so Windows runners compare like the others.
func normalizePath(p string) string {
	if rest, ok := strings.CutPrefix(p, "file://"); ok {
		if dec, err := url.PathUnescape(rest); err == nil {
			rest = dec
		}
		p = rest
	}
	return strings.ReplaceAll(p, "\\", "/")
}

func isTestDir(segment string) bool {
	return segment == "test" || segment == "integration_test"
}

// projectRoot returns the Flutter project root implied by the suite paths: the
// longest directory prefix shared by all of them that ends just before a test/
// or integration_test/ segment. It returns "" when there is none.
func projectRoot(suitePaths []string) string {
	var common []string
	for i, p := range suitePaths {
		segs := strings.Split(path.Dir(normalizePath(p)), "/")
		if i == 0 {
			common = segs
			continue
		}
		n := 0
		for n < len(common) && n < len(segs) && common[n] == segs[n] {
			n++
		}
		common = common[:n]
	}
	// The paths may diverge exactly at the test directories (test/ vs
	// integration_test/), leaving the root as the whole shared prefix.
	if len(suitePaths) > 0 {
		diverge := true
		for _, p := range suitePaths {
			segs := strings.Split(normalizePath(p), "/")
			if len(segs) <= len(common) || !isTestDir(segs[len(common)]) {
				diverge = false
				break
			}
		}
		if diverge {
			return strings.Join(common, "/")
		}
	}
	for i := len(common) - 1; i >= 0; i-- {
		if isTestDir(common[i]) {
			return strings.Join(common[:i], "/")
		}
	}
	return ""
}

// relativePath makes p relative to root. A path outside root, or with no
// test/ or integration_test/ segment right after it, falls back to its base name.
func relativePath(p, root string) string {
	p = normalizePath(p)
	if root != "" {
		if rest, ok := strings.CutPrefix(p, root+"/"); ok {
			first, _, _ := strings.Cut(rest, "/")
			if isTestDir(first) {
				return rest
			}
		}
	}
	return path.Base(p)
}
