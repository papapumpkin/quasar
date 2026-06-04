package integrations

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistry_RegisterAndBuildTicketSource(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.RegisterTicketSource("fake", func(cfg map[string]any, _ SecretResolver) (TicketSource, error) {
		return &fakeSource{name: "fake"}, nil
	})

	src, err := reg.BuildTicketSource("fake", map[string]any{"repo": "x/y"}, OSSecretResolver{})
	if err != nil {
		t.Fatalf("BuildTicketSource returned error: %v", err)
	}
	if src.Name() != "fake" {
		t.Errorf("built source Name() = %q, want fake", src.Name())
	}
}

func TestRegistry_BuildTicketSource_NilConfig(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	var gotCfg map[string]any
	reg.RegisterTicketSource("fake", func(cfg map[string]any, _ SecretResolver) (TicketSource, error) {
		gotCfg = cfg
		return &fakeSource{name: "fake"}, nil
	})

	if _, err := reg.BuildTicketSource("fake", nil, nil); err != nil {
		t.Fatalf("BuildTicketSource returned error: %v", err)
	}
	if gotCfg == nil {
		t.Error("constructor received nil cfg; want non-nil empty map")
	}
}

func TestRegistry_BuildUnknownSource(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	if _, err := reg.BuildTicketSource("missing", nil, nil); err == nil {
		t.Fatal("expected error for unregistered source, got nil")
	}
}

func TestRegistry_ConstructorErrorPropagates(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	reg := NewRegistry()
	reg.RegisterTicketSource("fake", func(map[string]any, SecretResolver) (TicketSource, error) {
		return nil, sentinel
	})

	_, err := reg.BuildTicketSource("fake", nil, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("BuildTicketSource error = %v, want wrap of %v", err, sentinel)
	}
}

func TestRegistry_RegisterForgeAndBuild(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.RegisterForge("fake", func(map[string]any, SecretResolver) (Forge, error) {
		return &fakeForge{name: "fake"}, nil
	})

	forge, err := reg.BuildForge("fake", nil, nil)
	if err != nil {
		t.Fatalf("BuildForge returned error: %v", err)
	}
	if forge.Name() != "fake" {
		t.Errorf("built forge Name() = %q, want fake", forge.Name())
	}
}

func TestRegistry_DuplicateTicketSourcePanics(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.RegisterTicketSource("dup", func(map[string]any, SecretResolver) (TicketSource, error) {
		return &fakeSource{name: "dup"}, nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration, got none")
		}
		if !strings.Contains(toString(r), "already registered") {
			t.Errorf("panic message = %q, want it to mention 'already registered'", toString(r))
		}
	}()

	reg.RegisterTicketSource("dup", func(map[string]any, SecretResolver) (TicketSource, error) {
		return &fakeSource{name: "dup"}, nil
	})
}

func TestRegistry_DuplicateForgePanics(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.RegisterForge("dup", func(map[string]any, SecretResolver) (Forge, error) {
		return &fakeForge{name: "dup"}, nil
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate forge registration, got none")
		}
	}()

	reg.RegisterForge("dup", func(map[string]any, SecretResolver) (Forge, error) {
		return &fakeForge{name: "dup"}, nil
	})
}

func TestDefaultRegistryIsStable(t *testing.T) {
	t.Parallel()

	if Default() == nil {
		t.Fatal("Default() returned nil")
	}
	if Default() != Default() {
		t.Error("Default() returned different instances across calls")
	}
}

// toString renders a recovered panic value for assertion.
func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return ""
}
