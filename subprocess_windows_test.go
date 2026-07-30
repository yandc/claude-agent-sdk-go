//go:build windows

package claudeagent

import "testing"

func TestWindowsSubprocessCreationFlagsSuppressConsoleWindow(t *testing.T) {
	const (
		createNewProcessGroup  = 0x00000200
		createDefaultErrorMode = 0x04000000
		createNoWindow         = 0x08000000
	)

	flags := windowsSubprocessCreationFlags()
	for name, flag := range map[string]uint32{
		"CREATE_NEW_PROCESS_GROUP":  createNewProcessGroup,
		"CREATE_DEFAULT_ERROR_MODE": createDefaultErrorMode,
		"CREATE_NO_WINDOW":          createNoWindow,
	} {
		if flags&flag != flag {
			t.Fatalf("%s missing from creation flags %#x", name, flags)
		}
	}
}
