package shave

import "os/exec"

// runCmd runs an external program, returning stdout, stderr and an exit code.
// All commands that shell out (kubectl, git) share this single implementation.
func runCmd(name string, args ...string) (string, string, int) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err == nil {
		return string(out), "", 0
	}
	estr := err.Error()
	if ee, ok := err.(*exec.ExitError); ok {
		estr = string(ee.Stderr)
	}
	return string(out), estr, 1
}
