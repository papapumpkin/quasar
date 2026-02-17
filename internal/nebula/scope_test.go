package nebula

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateScopeOverlaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		phases    []PhaseSpec
		wantCount int
		wantErr   error
		wantMsg   string // substring in error message
	}{
		{
			name: "overlapping scopes no dependency",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
			wantMsg:   `phases "a" and "b"`,
		},
		{
			name: "overlapping scopes with dependency",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}, DependsOn: []string{"b"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 0,
		},
		{
			name: "overlapping scopes with allow_scope_overlap on first",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}, AllowScopeOverlap: true},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 0,
		},
		{
			name: "overlapping scopes with allow_scope_overlap on second",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}, AllowScopeOverlap: true},
			},
			wantCount: 0,
		},
		{
			name: "unscoped phases skip checking",
			phases: []PhaseSpec{
				{ID: "a", Title: "A"},
				{ID: "b", Title: "B"},
			},
			wantCount: 0,
		},
		{
			name: "one scoped one unscoped",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B"},
			},
			wantCount: 0,
		},
		{
			name: "non-overlapping scopes",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/api/"}},
				{ID: "b", Title: "B", Scope: []string{"cmd/"}},
			},
			wantCount: 0,
		},
		{
			name: "exact match scopes",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/api/handler.go"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/handler.go"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "directory containment parent contains child",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/middleware/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "error message includes phase IDs and pattern",
			phases: []PhaseSpec{
				{ID: "alpha", Title: "A", Scope: []string{"internal/"}},
				{ID: "beta", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
			wantMsg:   `phases "alpha" and "beta"`,
		},
		{
			name: "three phases two overlapping",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}},
				{ID: "b", Title: "B", Scope: []string{"cmd/"}},
				{ID: "c", Title: "C", Scope: []string{"internal/loop/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "transitive dependency prevents overlap error",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/"}, DependsOn: []string{"b"}},
				{ID: "b", Title: "B", DependsOn: []string{"c"}},
				{ID: "c", Title: "C", Scope: []string{"internal/api/"}},
			},
			wantCount: 0,
		},
		{
			name: "glob star pattern overlap",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/*.go"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api.go"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "glob doublestar pattern overlap",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/**/*.go"}},
				{ID: "b", Title: "B", Scope: []string{"internal/api/"}},
			},
			wantCount: 1,
			wantErr:   ErrScopeOverlap,
		},
		{
			name: "three phases A-B overlap B-C overlap A-C no overlap",
			phases: []PhaseSpec{
				{ID: "a", Title: "A", Scope: []string{"internal/api/"}},
				{ID: "b", Title: "B", Scope: []string{"internal/"}},
				{ID: "c", Title: "C", Scope: []string{"internal/loop/"}},
			},
			wantCount: 2,
			wantErr:   ErrScopeOverlap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := validateScopeOverlaps(tt.phases)
			if len(errs) != tt.wantCount {
				t.Fatalf("got %d errors, want %d: %v", len(errs), tt.wantCount, errs)
			}
			if tt.wantErr != nil && len(errs) > 0 {
				if !errors.Is(errs[0].Err, tt.wantErr) {
					t.Errorf("got error %v, want %v", errs[0].Err, tt.wantErr)
				}
			}
			if tt.wantMsg != "" && len(errs) > 0 {
				if !strings.Contains(errs[0].Err.Error(), tt.wantMsg) {
					t.Errorf("error %q does not contain %q", errs[0].Err.Error(), tt.wantMsg)
				}
			}
		})
	}
}

func TestScopesOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{"exact match", []string{"foo.go"}, []string{"foo.go"}, true},
		{"dir contains", []string{"internal/"}, []string{"internal/api/"}, true},
		{"no overlap", []string{"cmd/"}, []string{"internal/"}, false},
		{"multiple patterns one overlaps", []string{"cmd/", "internal/"}, []string{"pkg/", "internal/api/"}, true},
		{"glob star vs literal", []string{"cmd/*.go"}, []string{"cmd/root.go"}, true},
		{"doublestar glob vs literal", []string{"api/**/*.proto"}, []string{"api/v1/service.proto"}, true},
		{"exact scope both sides", []string{"internal/"}, []string{"internal/"}, true},
		{"empty a", nil, []string{"internal/"}, false},
		{"empty b", []string{"internal/"}, nil, false},
		{"both empty", nil, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, got := scopesOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("scopesOverlap(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestPatternsOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"exact match", "internal/api/handler.go", "internal/api/handler.go", true},
		{"parent contains child", "internal", "internal/api", true},
		{"child contains parent", "internal/api", "internal", true},
		{"trailing slash parent", "internal/", "internal/api/", true},
		{"no overlap", "cmd/", "internal/", false},
		{"glob star matches literal", "internal/*.go", "internal/main.go", true},
		{"glob star no match", "internal/*.go", "cmd/main.go", false},
		{"doublestar containment", "internal/**/*.go", "internal/api/handler.go", true},
		{"doublestar different dirs", "internal/**", "cmd/**", false},
		{"sibling dirs", "internal/api/", "internal/loop/", false},
		{"same dir incompatible extensions", "internal/*.go", "internal/*.ts", false},
		{"same dir compatible globs", "internal/*.go", "internal/*.go", true},
		{"same dir wildcard vs extension", "internal/*", "internal/*.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := patternsOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("patternsOverlap(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
