// Package routeutil holds small helpers shared across HTTP adapters that
// route on top of `http.ServeMux`-style `{name}` wildcards (stdlib, chi).
// Gin/Fiber/Echo use `:name` natively and don't need this.
package routeutil

import "strings"

// TranslateColonToBrace rewrites the shared `:name` path convention to the
// `{name}` form expected by Go 1.22+ `http.ServeMux` and chi, and returns
// the param names in order of appearance. A bare `:` or a `:` mid-segment
// (e.g. `/literal-:colon/x`) is left untouched.
func TranslateColonToBrace(path string) (string, []string) {
	segments := strings.Split(path, "/")
	var names []string
	for i, seg := range segments {
		if strings.HasPrefix(seg, ":") && len(seg) > 1 {
			name := seg[1:]
			names = append(names, name)
			segments[i] = "{" + name + "}"
		}
	}
	return strings.Join(segments, "/"), names
}
