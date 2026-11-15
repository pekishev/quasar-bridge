package update

import "os/exec"

// Updater execution is Linux-only; Windows hosts build and run routing tests.
func detachProcess(cmd *exec.Cmd) {}
