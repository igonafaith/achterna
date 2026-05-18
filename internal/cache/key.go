package cache

import (
	"fmt"
	"net/http"
	"sort"
)

// DeriveKey returns a cache key in the form METHOD:host:path?sorted_query.
func DeriveKey(r *http.Request) string {
	return fmt.Sprintf("%s:%s%s?%s",
		r.Method,
		r.Host,
		r.URL.Path,
		sortQuery(r.URL.Query()),
	)
}

func sortQuery(vals map[string][]string) string {
	if len(vals) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf []byte
	first := true
	for _, k := range keys {
		sort.Strings(vals[k])
		for _, v := range vals[k] {
			if !first {
				buf = append(buf, '&')
			}
			buf = append(buf, k...)
			buf = append(buf, '=')
			buf = append(buf, v...)
			first = false
		}
	}
	return string(buf)
}
