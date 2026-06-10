package cockpit

import "testing"

func TestCockpitConfigDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.Enabled {
		t.Error("cockpit must default to disabled")
	}
	if c.Addr != "127.0.0.1:7330" {
		t.Errorf("addr = %q, want 127.0.0.1:7330", c.Addr)
	}
}
