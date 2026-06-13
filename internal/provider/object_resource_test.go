// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import "testing"

func TestSubsetMatches(t *testing.T) {
	cases := []struct {
		name        string
		prior, cfg  string
		wantMatched bool
	}{
		{
			name:        "config subset of full device object — match (0-diff)",
			prior:       `{".id":"*1","address":"192.168.88.1/24","interface":"bridge","network":"192.168.88.0","dynamic":"false"}`,
			cfg:         `{"address":"192.168.88.1/24","interface":"bridge"}`,
			wantMatched: true,
		},
		{
			name:        "declared key drifted — no match (update)",
			prior:       `{".id":"*1","address":"192.168.88.1/24","interface":"ether1"}`,
			cfg:         `{"address":"192.168.88.1/24","interface":"bridge"}`,
			wantMatched: false,
		},
		{
			name:        "declared key missing on device — no match",
			prior:       `{".id":"*1","address":"192.168.88.1/24"}`,
			cfg:         `{"address":"192.168.88.1/24","interface":"bridge"}`,
			wantMatched: false,
		},
		{
			name:        "key order / whitespace insensitive — match",
			prior:       `{"name":"gw","comment":"core"}`,
			cfg:         "{\n  \"comment\": \"core\",\n  \"name\": \"gw\"\n}",
			wantMatched: true,
		},
		{
			name:        "RouterOS string-encoded values compared as strings — match",
			prior:       `{".id":"*3","disabled":"false","mtu":"1500"}`,
			cfg:         `{"disabled":"false","mtu":"1500"}`,
			wantMatched: true,
		},
		{
			name:        "string-encoded value drift — no match",
			prior:       `{".id":"*3","mtu":"1500"}`,
			cfg:         `{"mtu":"1480"}`,
			wantMatched: false,
		},
		{
			name:        "list value compared in order — match",
			prior:       `{".id":"*1","dns-servers":["1.1.1.1","8.8.8.8"]}`,
			cfg:         `{"dns-servers":["1.1.1.1","8.8.8.8"]}`,
			wantMatched: true,
		},
		{
			name:        "invalid prior JSON — no match (fall back to diff)",
			prior:       `not json`,
			cfg:         `{"a":1}`,
			wantMatched: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subsetMatches(tc.prior, tc.cfg); got != tc.wantMatched {
				t.Fatalf("subsetMatches() = %v, want %v", got, tc.wantMatched)
			}
		})
	}
}

func TestNormPath(t *testing.T) {
	for in, want := range map[string]string{
		"ip/address":         "/ip/address",
		"/ip/address":        "/ip/address",
		" system/identity ":  "/system/identity",
		"/system/identity":   "/system/identity",
		"ip/address/":        "/ip/address",
		"/interface/vlan/*1": "/interface/vlan/*1",
	} {
		if got := normPath(in); got != want {
			t.Errorf("normPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestItemPath(t *testing.T) {
	cases := []struct {
		menu, id, want string
	}{
		{"ip/address", "*1", "/ip/address/*1"},
		{"/interface/vlan", "*A", "/interface/vlan/*A"},
		{"ip/dhcp-server/lease", "*F", "/ip/dhcp-server/lease/*F"},
	}
	for _, tc := range cases {
		if got := itemPath(tc.menu, tc.id); got != tc.want {
			t.Errorf("itemPath(%q,%q) = %q, want %q", tc.menu, tc.id, got, tc.want)
		}
	}
}

func TestExtractID(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"asterisk id", `{".id":"*7","address":"10.0.0.1/24"}`, "*7"},
		{"named id", `{".id":"bridge1","name":"bridge1"}`, "bridge1"},
		{"no id field", `{"address":"10.0.0.1/24"}`, ""},
		{"not an object (array)", `[{".id":"*1"}]`, ""},
		{"invalid json", `nope`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractID([]byte(tc.raw)); got != tc.want {
				t.Errorf("extractID(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestIsEmptyResponse(t *testing.T) {
	for in, want := range map[string]bool{
		"":               true,
		"  ":             true,
		"[]":             true,
		"{}":             true,
		"null":           true,
		`[{".id":"*1"}]`: false,
		`{".id":"*1"}`:   false,
		"\n  []  \n":     true,
	} {
		if got := isEmptyResponse([]byte(in)); got != want {
			t.Errorf("isEmptyResponse(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCompactJSON(t *testing.T) {
	out, err := compactJSON([]byte("{\n \"b\": 2,\n \"a\": 1\n}"))
	if err != nil {
		t.Fatal(err)
	}
	// json.Marshal of a map sorts keys; whitespace is removed.
	if out != `{"a":1,"b":2}` {
		t.Fatalf("compactJSON = %q", out)
	}
}
