package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// openBrowser opens the given URL in the default browser.
// Errors are logged to stderr but are not fatal — this is a best-effort
// convenience for interactive use. Headless environments silently skip.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		// Unsupported platform — skip silently.
		return
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[web] could not open browser: %v\n", err)
	}
}
