package neutron

import (
	"reflect"
	"testing"
)

func TestExtractDeclarations(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want []Declaration
	}{
		{
			name: "pulls symbols from Files and Solution sections",
			spec: `+++
id = "phase-x"
+++

## Problem

We need a new sensor. ` + "`func Ignored()`" + ` lives here but Problem is not scanned.

## Solution

Introduce a ` + "`type Sensor interface`" + ` and a method:

` + "```go" + `
func (b *Budget) CheckBefore() error
` + "```" + `

## Files

- ` + "`internal/sensors/sensor.go`" + ` — define ` + "`type Sensor interface`" + ` and ` + "`func NewSensor`" + `
`,
			want: []Declaration{
				{Kind: "type", Name: "Sensor"},
				{Kind: "func", Name: "CheckBefore"},
				{Kind: "func", Name: "NewSensor"},
			},
		},
		{
			name: "deduplicates repeated symbols across sections",
			spec: `## Solution
func Build() error

## Files
func Build() error`,
			want: []Declaration{{Kind: "func", Name: "Build"}},
		},
		{
			name: "ignores unexported names",
			spec: `## Solution
func helper() {}
type widget struct{}`,
			want: nil,
		},
		{
			name: "no target sections",
			spec: `## Problem
func ShouldNotMatch() {}`,
			want: nil,
		},
		{
			name: "captures var and const",
			spec: `## Solution
var DefaultTimeout = 5
const MaxWorkers = 4`,
			want: []Declaration{
				{Kind: "var", Name: "DefaultTimeout"},
				{Kind: "const", Name: "MaxWorkers"},
			},
		},
		{
			name: "empty spec",
			spec: "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractDeclarations(tt.spec)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractDeclarations() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
