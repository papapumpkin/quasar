package forge

import "testing"

func TestParsePRNumber(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"https://github.com/owner/repo/pull/42", 42, true},
		{"  https://github.com/owner/repo/pull/123  \n", 123, true},
		{"https://github.com/owner/repo/issues/42", 42, true}, // trailing-segment numeric is enough
		{"", 0, false},
		{"https://github.com/owner/repo/", 0, false},
		{"not a url", 0, false},
		{"https://github.com/owner/repo/pull/abc", 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, ok := parsePRNumber(tc.in)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("parsePRNumber(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestOpenPRRejectsEmptyInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts PROpts
		want string
	}{
		{"no head", PROpts{WorkDir: "/tmp", Title: "t"}, "Head"},
		{"no title", PROpts{WorkDir: "/tmp", Head: "quasar/x"}, "Title"},
		{"no workdir", PROpts{Head: "quasar/x", Title: "t"}, "WorkDir"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := OpenPR(t.Context(), tc.opts)
			if err == nil {
				t.Fatalf("OpenPR(%+v) err = nil, want error mentioning %q", tc.opts, tc.want)
			}
			if !contains(err.Error(), tc.want) {
				t.Errorf("OpenPR err = %q, want it to mention %q", err.Error(), tc.want)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
