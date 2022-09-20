package priority

import (
	"golang.org/x/sys/windows"
	"syscall"
)

const RealtimePriorityClass = 0x00000100

func Elevate() error {
	// https://docs.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getcurrentprocess
	// This returns a pseudo handle, which need not be closed when it is no longer needed.
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return err
	}

	err = windows.SetPriorityClass(windows.Handle(handle), RealtimePriorityClass)
	return err
}
