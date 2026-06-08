package neutron

import (
	"reflect"
	"testing"
)

func TestDetectDeletions(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want []string
	}{
		{
			name: "removed function with no replacement is a deletion",
			diff: `--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,3 @@
 package foo
-func OldHelper() error {
-	return nil
-}
 func Keep() {}`,
			want: []string{"OldHelper"},
		},
		{
			name: "removed type and const",
			diff: `-type LegacyConfig struct{}
-const MaxRetries = 3
+type Config struct{}`,
			want: []string{"LegacyConfig", "MaxRetries"},
		},
		{
			name: "signature change is not a deletion (name re-added)",
			diff: `-func Poll() error
+func Poll(ctx context.Context) error`,
			want: nil,
		},
		{
			name: "method receiver is skipped, method name captured",
			diff: `-func (b *Budget) CheckBefore() error {`,
			want: []string{"CheckBefore"},
		},
		{
			name: "indented (nested) declaration is ignored",
			diff: `-	func inner() {}`,
			want: nil,
		},
		{
			name: "file headers are not treated as deletions",
			diff: `--- a/types.go
+++ b/types.go`,
			want: nil,
		},
		{
			name: "no declarations at all",
			diff: `-	x := 1
+	x := 2`,
			want: nil,
		},
		{
			name: "empty diff",
			diff: "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetectDeletions(tt.diff)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DetectDeletions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectTouchedSymbols(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want []Touched
	}{
		{
			name: "added function with trailing brace trimmed",
			diff: `--- a/foo.go
+++ b/foo.go
@@ -1 +1,3 @@
+func Poll(ctx context.Context) error {
+	return nil
+}`,
			want: []Touched{{Name: "Poll", Signature: "func Poll(ctx context.Context) error"}},
		},
		{
			name: "type and method receiver",
			diff: `+type Sensor interface {
+func (b *Budget) CheckBefore() error {`,
			want: []Touched{
				{Name: "Sensor", Signature: "type Sensor interface"},
				{Name: "CheckBefore", Signature: "func (b *Budget) CheckBefore() error"},
			},
		},
		{
			name: "file headers and removed lines ignored",
			diff: `+++ b/x.go
-func Removed() {}
 func Context() {}`,
			want: nil,
		},
		{
			name: "first occurrence wins for duplicates",
			diff: `+var Conf = 1
+var Conf = 2`,
			want: []Touched{{Name: "Conf", Signature: "var Conf = 1"}},
		},
		{
			name: "empty diff",
			diff: "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetectTouchedSymbols(tt.diff)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DetectTouchedSymbols() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
