package denoise

import (
	"sort"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
)

const (
	// keyPairSep separates "k=v" pairs in an incident key.
	// Chosen because pipe is unlikely to appear in a label value.
	keyPairSep = "|"
	// keyKVSep separates a key from its value within one pair.
	keyKVSep = "="
	// keyMissingValue is the sentinel used when a dimension label is absent
	// from the event. Using a fixed marker keeps two events with the same
	// missing-label still grouping together (they share the same "<absent>"),
	// instead of fanning out into separate incidents.
	keyMissingValue = "<absent>"
)

// BuildIncidentKey produces a stable, deterministic string identifier from
// the values of the given dimension labels on the event.
//
// Properties:
//   - Sorted dimensions: input ["b","a"] yields the same key as ["a","b"].
//   - Missing labels resolve to a sentinel (keyMissingValue), so absent dims
//     are still groupable instead of splintering into singleton incidents.
//   - Empty dimensions list yields an empty string — caller should reject.
//
// Examples:
//
//	dims=["service","cluster"], event.tags={service:order, cluster:prod, host:db01}
//	  → "cluster=prod|service=order"
//
//	dims=["service","cluster"], event.tags={service:order}
//	  → "cluster=<absent>|service=order"
func BuildIncidentKey(event *models.AlertCurEvent, dimensions []string) string {
	if event == nil || len(dimensions) == 0 {
		return ""
	}

	dims := make([]string, len(dimensions))
	copy(dims, dimensions)
	sort.Strings(dims)

	var b strings.Builder
	for i, dim := range dims {
		if i > 0 {
			b.WriteString(keyPairSep)
		}
		b.WriteString(dim)
		b.WriteString(keyKVSep)
		if v, ok := event.TagsMap[dim]; ok && v != "" {
			b.WriteString(v)
		} else {
			b.WriteString(keyMissingValue)
		}
	}
	return b.String()
}

// DimensionValuesJSON returns the dimension label values as a map suitable
// for JSON serialization into CustomIncident.DimensionValues.
//
// Unlike BuildIncidentKey (which is for fast equality lookup), this function
// preserves the original dimension order for human-readable display.
func DimensionValuesMap(event *models.AlertCurEvent, dimensions []string) map[string]string {
	out := make(map[string]string, len(dimensions))
	if event == nil {
		return out
	}
	for _, dim := range dimensions {
		if v, ok := event.TagsMap[dim]; ok {
			out[dim] = v
		} else {
			out[dim] = keyMissingValue
		}
	}
	return out
}
