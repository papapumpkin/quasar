package sensors

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistry_RegisterAndBuildSensor(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.RegisterSensor("fake", func() (Sensor, error) {
		return &fakeSensor{name: "fake"}, nil
	})

	s, err := reg.BuildSensor("fake")
	if err != nil {
		t.Fatalf("BuildSensor returned error: %v", err)
	}
	if s.Name() != "fake" {
		t.Errorf("built sensor Name() = %q, want fake", s.Name())
	}
}

func TestRegistry_BuildUnknownSensor(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	if _, err := reg.BuildSensor("missing"); err == nil {
		t.Fatal("expected error for unregistered sensor, got nil")
	}
}

func TestRegistry_SensorConstructorErrorPropagates(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	reg := NewRegistry()
	reg.RegisterSensor("fake", func() (Sensor, error) {
		return nil, sentinel
	})

	_, err := reg.BuildSensor("fake")
	if !errors.Is(err, sentinel) {
		t.Fatalf("BuildSensor error = %v, want wrap of %v", err, sentinel)
	}
}

func TestRegistry_NilSensorConstructorPanics(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil sensor constructor, got none")
		}
	}()
	reg.RegisterSensor("nil", nil)
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

func TestRegistry_BuildForge_NilConfig(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	var gotCfg map[string]any
	reg.RegisterForge("fake", func(cfg map[string]any, _ SecretResolver) (Forge, error) {
		gotCfg = cfg
		return &fakeForge{name: "fake"}, nil
	})

	if _, err := reg.BuildForge("fake", nil, nil); err != nil {
		t.Fatalf("BuildForge returned error: %v", err)
	}
	if gotCfg == nil {
		t.Error("forge constructor received nil cfg; want non-nil empty map")
	}
}

func TestRegistry_DuplicateSensorPanics(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.RegisterSensor("dup", func() (Sensor, error) {
		return &fakeSensor{name: "dup"}, nil
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

	reg.RegisterSensor("dup", func() (Sensor, error) {
		return &fakeSensor{name: "dup"}, nil
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

	first := Default()
	if first == nil {
		t.Fatal("Default() returned nil")
	}
	if second := Default(); first != second {
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
