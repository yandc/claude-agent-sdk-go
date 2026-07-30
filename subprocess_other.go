//go:build !windows

package claudeagent

import "os/exec"

func configurePlatformSubprocessCommand(cmd *exec.Cmd) {
}
