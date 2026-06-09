package routeutil

import (
	"reflect"
	"testing"
)

func TestTranslateColonToBrace(t *testing.T) {
	tests := []struct {
		in        string
		wantPath  string
		wantNames []string
	}{
		{"/health", "/health", nil},
		{"/hello/:name", "/hello/{name}", []string{"name"}},
		{"/users/:id/posts/:postID", "/users/{id}/posts/{postID}", []string{"id", "postID"}},
		// A colon mid-segment must not be treated as a param marker.
		{"/literal-:colon/x", "/literal-:colon/x", nil},
		// Bare ":" (no name after it) must not be translated.
		{"/:", "/:", nil},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			gotPath, gotNames := TranslateColonToBrace(tt.in)
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if !reflect.DeepEqual(gotNames, tt.wantNames) {
				t.Errorf("names = %v, want %v", gotNames, tt.wantNames)
			}
		})
	}
}
