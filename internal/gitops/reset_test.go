package gitops

import (
	"context"
	"errors"
	"testing"
)

func TestResetHard(t *testing.T) {
	t.Parallel()

	t.Run("resets on a feature branch", func(t *testing.T) {
		t.Parallel()
		var sawReset bool
		fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
			if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--abbrev-ref" {
				return []byte("quasar/fix\n"), nil, nil
			}
			if len(args) >= 3 && args[0] == "reset" && args[1] == "--hard" {
				sawReset = true
				if args[2] != "deadbeef" {
					t.Errorf("reset sha = %q, want deadbeef", args[2])
				}
			}
			return nil, nil, nil
		}}
		c := NewWithRunner(".", fr.run)
		if err := c.ResetHard(context.Background(), "deadbeef"); err != nil {
			t.Fatalf("ResetHard: %v", err)
		}
		if !sawReset {
			t.Error("the hard reset was not executed on a feature branch")
		}
	})

	t.Run("refuses on a protected base branch", func(t *testing.T) {
		t.Parallel()
		var sawReset bool
		fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
			if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--abbrev-ref" {
				return []byte("main\n"), nil, nil
			}
			if len(args) >= 1 && args[0] == "reset" {
				sawReset = true
			}
			return nil, nil, nil
		}}
		c := NewWithRunner(".", fr.run)
		err := c.ResetHard(context.Background(), "deadbeef")
		if !errors.Is(err, ErrUnsafeRef) {
			t.Errorf("err = %v, want ErrUnsafeRef", err)
		}
		if sawReset {
			t.Error("the perimeter must NOT run a hard reset on a protected base branch")
		}
	})
}
