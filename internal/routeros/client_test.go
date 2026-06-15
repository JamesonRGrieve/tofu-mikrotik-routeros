// SPDX-License-Identifier: AGPL-3.0-or-later
package routeros

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Set must map a settings (singleton) menu to the RouterOS `set` command:
// POST /rest/<menu>/set with the body. A bare PATCH on the menu is rejected by
// RouterOS ("missing or invalid resource identifier"), so this mapping is the fix.
func TestSetUsesSetCommand(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(Config{Host: strings.TrimPrefix(srv.URL, "http://"), Scheme: "http"})
	if _, err := c.Set("/system/identity", []byte(`{"name":"x"}`)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/rest/system/identity/set" {
		t.Errorf("path = %q, want /rest/system/identity/set", gotPath)
	}
	if gotBody != `{"name":"x"}` {
		t.Errorf("body = %q, want the declared JSON", gotBody)
	}
}

// FindByName must return the .id of the collection item matching the name (so
// Create can adopt a pre-existing built-in), and "" when none matches.
func TestFindByName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{".id":"*1","name":"memory"},{".id":"*4","name":"disk"}]`))
	}))
	defer srv.Close()
	c := NewClient(Config{Host: strings.TrimPrefix(srv.URL, "http://"), Scheme: "http"})

	for _, tc := range []struct{ name, want string }{
		{"disk", "*4"},
		{"memory", "*1"},
		{"absent", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := c.FindByName("/system/logging/action", tc.name)
			if err != nil {
				t.Fatalf("FindByName: %v", err)
			}
			if id != tc.want {
				t.Errorf("id = %q, want %q", id, tc.want)
			}
		})
	}
}
