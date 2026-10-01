//go:build !linux

package matchmedia

import "syscall"

func childSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
