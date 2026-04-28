package denoise

import (
	"encoding/json"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ormx"
)

// parseTagFilters decodes a JSONArr-encoded []TagFilter (the same shape used
// by native AlertMute.Tags) and runs the upstream's regex/set compilation
// step so subsequent matches can reuse the compiled forms.
//
// We cannot reuse models.ParseTagFilter directly on the cached rule because
// the rule struct stores the JSON bytes, not a parsed slice — every Process
// call would otherwise re-allocate. Calls of this helper are cheap because
// rules are loaded into memsto once per refresh interval.
func parseTagFilters(raw ormx.JSONArr) ([]models.TagFilter, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var filters []models.TagFilter
	if err := json.Unmarshal(raw, &filters); err != nil {
		return nil, err
	}
	return models.ParseTagFilter(filters)
}

// matchAllTagFilters reports whether every filter in `filters` passes
// against `tags`. Reuses the matching primitives the upstream native code
// uses (so we behave identically on edge cases like missing keys, regex
// anchoring, "in" semantics, etc.) by funneling through alert/common.
//
// Implementation note: we duplicate a tiny amount of logic from
// alert/common/match.go intentionally. Importing alert/common from here
// would form a cycle (alert/* depends on internal/custom for hook
// registration), so we keep the matcher self-contained.
func matchAllTagFilters(tags map[string]string, filters []models.TagFilter) bool {
	for i := range filters {
		if !matchSingleTagFilter(tags, &filters[i]) {
			return false
		}
	}
	return true
}

// matchSingleTagFilter implements the filter operators supported by N9e's
// native TagFilter:
//
//	"=="      string equality
//	"!="      string inequality
//	"=~"      regex match (filter.Regexp pre-compiled by ParseTagFilter)
//	"!~"      regex non-match
//	"in"      value is in a fixed set (filter.Vset)
//	"not in"  value is not in the set
//
// A missing key is treated as not matching for "==", "=~", "in", and as
// matching for the negative operators — same convention as upstream.
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
