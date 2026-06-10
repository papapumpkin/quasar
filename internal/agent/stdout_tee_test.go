package agent

import (
	"bytes"
	"context"
	"testing"
)

func TestStdoutTeeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithStdoutTee(context.Background(), &buf)
	w := StdoutTee(ctx)
	if w == nil {
		t.Fatal("StdoutTee returned nil after WithStdoutTee")
	}
	if w != &buf {
		t.Fatalf("StdoutTee returned a different writer than was set")
	}
}

func TestStdoutTeeNilWriterIgnored(t *testing.T) {
	base := context.Background()
	ctx := WithStdoutTee(base, nil)
	if ctx != base {
		t.Error("WithStdoutTee(nil) should return ctx unchanged")
	}
	if StdoutTee(ctx) != nil {
		t.Error("StdoutTee should be nil when no tee was set")
	}
}

func TestStdoutTeeAbsent(t *testing.T) {
	if StdoutTee(context.Background()) != nil {
		t.Error("StdoutTee should be nil on a bare context")
	}
}
