// Package filter matches slash-separated source-relative paths.
package filter

import (
	"fmt"
	"path"
)

// Matcher applies include and exclude glob patterns. Excludes take priority.
// Patterns use path.Match syntax; they do not support recursive ** matching.
type Matcher struct {
	includes []string
	excludes []string
}

func New(includes, excludes []string) (Matcher, error) {
	for _, pattern := range append(append([]string{}, includes...), excludes...) {
		if _, err := path.Match(pattern, ""); err != nil {
			return Matcher{}, fmt.Errorf("invalid glob %q: %w", pattern, err)
		}
	}
	return Matcher{includes: includes, excludes: excludes}, nil
}

func (m Matcher) Include(name string) bool {
	if m.Excluded(name) {
		return false
	}
	return len(m.includes) == 0 || matches(m.includes, name)
}

// Excluded reports whether an exclude pattern matches name.
func (m Matcher) Excluded(name string) bool { return matches(m.excludes, name) }

func matches(patterns []string, name string) bool {
	for _, pattern := range patterns {
		ok, _ := path.Match(pattern, name)
		if ok {
			return true
		}
	}
	return false
}
