package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

// ginxBadRequest constructs a 400-style error with a printf message. We
// return errors (rather than calling ginx.Bomb directly inside validate)
// so handlers can decide how to surface them — usually
// ginx.NewRender(c).Data(nil, err), which produces the standard {dat,err}
// envelope the front-end expects.
func ginxBadRequest(format string, args ...interface{}) error {
	return errors.New(fmt.Sprintf(format, args...))
}

// decodeJSONInt64List parses a stored "[1,2,3]" column into []int64. Empty
// or malformed input yields nil so callers see "no scope" semantics.
func decodeJSONInt64List(s string) []int64 {
	if s == "" {
		return nil
	}
	var out []int64
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// decodeJSONIntList parses a stored "[1,2]" column into []int. Same
// not-found / malformed convention as decodeJSONInt64List.
func decodeJSONIntList(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// urlParamID extracts the :id path param as int64. Wrapper around
// ginx.UrlParamInt64 that errors with a clearer message when the param is
// missing or non-numeric, since "id" is the most common URL param across
// every CRUD handler.
func urlParamID(c *gin.Context) int64 {
	v := c.Param("id")
	if v == "" {
		ginx.Bomb(400, "missing id in URL")
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		ginx.Bomb(400, "invalid id %q", v)
	}
	return id
}

// encodeJSONList serializes a []int64 / []int / []string as a JSON array
// string for storage in CSV-style columns. This mirrors how N9e's native
// AlertMute persists DatasourceIds — see models/alert_mute.go DB2FE/FE2DB.
//
// We accept any value rather than overload three functions because the
// handlers all receive interface{} from JSON unmarshal anyway.
func encodeJSONList(v interface{}) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
