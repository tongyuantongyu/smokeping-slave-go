package priority

import "syscall"

func Elevate() error {
	pid := syscall.Getpid()

	err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, -20)
	return err
}
