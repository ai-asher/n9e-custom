package bootstrap

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/toolkits/pkg/logger"
)

// parseInt64CSV decodes a column that may carry either a JSON array
// ("[1,2,3]") or a comma/space-separated string ("1,2,3" / "1 2 3").
// Both shapes are observed across N9e's various conversion helpers, so we
// accept both rather than gambling on which our raw rows use.
//
// Empty input — or the literal JSON null "null" — yields nil (treated as
// "no scope = match all" by the matchers downstream). The "null" case
// matters because Go's json.Marshal of a nil []int64 produces the string
// "null", not "[]", so a struct with a nil slice round-trips through the
// API + DB and lands here unchanged.
func parseInt64CSV(s string) []int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return nil
	}
	if s[0] == '[' {
		var out []int64
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			logger.Warningf("custom/bootstrap: bad int64 list JSON %q: %v", s, err)
			return nil
		}
		return out
	}
	parts := splitOnAny(s, ", ")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			logger.Warningf("custom/bootstrap: bad int64 %q in list %q: %v", p, s, err)
			continue
		}
		out = append(out, v)
	}
	return out
}

// parseIntCSV is parseInt64CSV's narrower sibling for int columns
// (severity lists). Same dual-format handling, plus tolerance for the
// JSON-null marshaling artifact described above.
func parseIntCSV(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return nil
	}
	if s[0] == '[' {
		var out []int
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			logger.Warningf("custom/bootstrap: bad int list JSON %q: %v", s, err)
			return nil
		}
		return out
	}
	parts := splitOnAny(s, ", ")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			logger.Warningf("custom/bootstrap: bad int %q in list %q: %v", p, s, err)
			continue
		}
		out = append(out, v)
	}
	return out
}

// splitOnAny splits s on every byte present in seps. Avoids importing a
// regex or strings.FieldsFunc for a 4-line helper.
func splitOnAny(s, seps string) []string {
	out := make([]string, 0, 4)
	last := 0
	for i := 0; i < len(s); i++ {
		if strings.ContainsRune(seps, rune(s[i])) {
			out = append(out, s[last:i])
			last = i + 1
		}
	}
	out = append(out, s[last:])
	return out
}
