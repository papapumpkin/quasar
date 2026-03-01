package loop

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// FindingID computes a deterministic identifier for a finding based on its
// severity and description. The ID is a short hex prefix of a SHA-256 hash,
// stable across cycles so the same logical finding can be tracked.
func FindingID(severity, description string) string {
	h := sha256.New()
	h.Write([]byte(severity))
	h.Write([]byte(":"))
	h.Write([]byte(strings.TrimSpace(description)))
	return fmt.Sprintf("f-%x", h.Sum(nil)[:6])
}
