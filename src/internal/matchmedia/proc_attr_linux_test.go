//go:build linux

package matchmedia

import (
	"syscall"
	"testing"
)

func TestChildSysProcAttrPdeathsig(t *testing.T) {
	attr := childSysProcAttr()
	if attr == nil {
		t.Fatal("nil SysProcAttr")
	}
	if !attr.Setpgid {
		t.Fatal("expected Setpgid")
	}
	if attr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("Pdeathsig=%v want SIGTERM", attr.Pdeathsig)
	}
}
