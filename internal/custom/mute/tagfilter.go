package mute

import (
	"github.com/ccfos/nightingale/v6/models"
)

// matchAllTagFilters reports whether every filter in `filters` matches `tags`.
// Empty filters returns true — caller must treat empty filters as
// "no tag constraint".
//
// Why duplicated: see internal/custom/suppress/tagfilter.go for the
// rationale. Mute uses identical TagFilter semantics; a shared common
// package was rejected to keep coupling low while the modules stabilize.
func matchAllTagFilters(tags map[string]string, filters []models.TagFilter) bool {
	for i := range filters {
		if !matchSingleTagFilter(tags, &filters[i]) {
			return false
		}
	}
	return true
}

func matchSingleTagFilter(tags map[string]string, f *models.TagFilter) bool {
	val, present := tags[f.Key]

	switch f.Func {
	case "==":
		if !present {
			return false
		}
		s, ok := f.Value.(string)
		return ok && val == s
	case "!=":
		if !present {
			return true
		}
		s, ok := f.Value.(string)
		return !ok || val != s
	case "=~":
		if !present || f.Regexp == nil {
			return false
		}
		return f.Regexp.MatchString(val)
	case "!~":
		if !present {
			return true
		}
		if f.Regexp == nil {
			return true
		}
		return !f.Regexp.MatchString(val)
	case "in":
		if !present {
			return false
		}
		_, ok := f.Vset[val]
		return ok
	case "not in":
		if !present {
			return true
		}
		_, ok := f.Vset[val]
		return !ok
	default:
		return false
	}
}
