package preview

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

func Open(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", absPath)
	case "linux":
		cmd = exec.Command("xdg-open", absPath)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", absPath)
	default:
		return fmt.Errorf("opening browser is not supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}
