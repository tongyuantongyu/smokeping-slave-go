package priority

import (
	"golang.org/x/sys/windows"
	"syscall"
)

const RealtimePriorityClass = 0x00000100

func Elevate() error {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return err
	}

	err = windows.SetPriorityClass(windows.Handle(handle), RealtimePriorityClass)
	return err
}
