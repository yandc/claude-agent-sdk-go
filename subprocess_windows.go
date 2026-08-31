//go:build windows

package claudeagent

import (
	"os/exec"
	"syscall"
)

const (
	windowsSubprocessCreateNewProcessGroup  = 0x00000200
	windowsSubprocessCreateDefaultErrorMode = 0x04000000
	windowsSubprocessCreateNoWindow         = 0x08000000
)

func windowsSubprocessCreationFlags() uint32 {
	return windowsSubprocessCreateNewProcessGroup | windowsSubprocessCreateDefaultErrorMode | windowsSubprocessCreateNoWindow
}

func configurePlatformSubprocessCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsSubprocessCreationFlags(),
	}
}

func killPlatformSubprocess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
