package suppress

import (
	"github.com/ccfos/nightingale/v6/models"
)

// matchAllTagFilters reports whether every filter in `filters` matches `tags`.
// Empty filters returns true (caller-defined "match everything" semantics).
//
// Why duplicated from internal/custom/denoise/tagfilter.go:
//
//	Lifting this into a shared internal/custom/common package would create
//	two-way pressure (suppress and denoise both depending on common, common
//	potentially needing types from each). The cost of a 70-line duplicate
//	with identical semantics is much lower than that coupling. If the
//	matchers diverge in the future, that is the signal to consolidate.
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
