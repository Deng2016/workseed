//go:build windows

package main

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

const instanceMutexName = `Local\Workseed.SingleInstance`

func acquireSingleInstance() (release func(), acquired bool, err error) {
	name, err := windows.UTF16PtrFromString(instanceMutexName)
	if err != nil {
		return nil, false, fmt.Errorf("encode mutex name: %w", err)
	}

	handle, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("create mutex: %w", err)
	}

	return func() {
		_ = windows.CloseHandle(handle)
	}, true, nil
}
