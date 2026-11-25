//go:build verification && linux

// A pseudo-terminal for the checks of the full-screen view, opened with the
// standard library only (/dev/ptmx, unlock, the slave's number, TIOCSWINSZ),
// and the terminal mode read through termios. See README.md.
package verification

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

type pty struct {
	master *os.File // the terminal's side: what the tool writes is read here, keys are written here
	slave  *os.File // the tool's side: its stdin, stdout and stderr
	name   string   // /dev/pts/N
}

// fdCall runs fn with the file's descriptor without switching the file to
// blocking mode (os.File.Fd would), so that Close still ends a pending Read.
func fdCall(f *os.File, fn func(fd uintptr) error) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := rc.Control(func(fd uintptr) { inner = fn(fd) }); err != nil {
		return err
	}
	return inner
}

func ioctl(fd, req, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); errno != 0 {
		return errno
	}
	return nil
}

// openPTY opens a new pseudo-terminal of cols x rows.
func openPTY(cols, rows int) (*pty, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var n uint32
	err = fdCall(m, func(fd uintptr) error {
		var unlock int32
		if err := ioctl(fd, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
			return fmt.Errorf("unlock: %w", err)
		}
		if err := ioctl(fd, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
			return fmt.Errorf("slave number: %w", err)
		}
		return nil
	})
	if err != nil {
		m.Close()
		return nil, err
	}
	name := fmt.Sprintf("/dev/pts/%d", n)
	s, err := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		m.Close()
		return nil, err
	}
	p := &pty{master: m, slave: s, name: name}
	if err := p.setSize(cols, rows); err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

func (p *pty) setSize(cols, rows int) error {
	ws := [4]uint16{uint16(rows), uint16(cols), 0, 0} // struct winsize: row, col, xpixel, ypixel
	return fdCall(p.slave, func(fd uintptr) error {
		return ioctl(fd, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
	})
}

// termios reads the terminal's mode (TCGETS) from the slave side.
func (p *pty) termios() (syscall.Termios, error) {
	var t syscall.Termios
	err := fdCall(p.slave, func(fd uintptr) error {
		return ioctl(fd, syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	})
	return t, err
}

func fmtTermios(t syscall.Termios) string {
	return fmt.Sprintf("iflag=%#o oflag=%#o cflag=%#o lflag=%#o ispeed=%d ospeed=%d cc=%v",
		t.Iflag, t.Oflag, t.Cflag, t.Lflag, t.Ispeed, t.Ospeed, t.Cc)
}

func (p *pty) close() {
	p.master.Close()
	p.slave.Close()
}

// ttyAttr makes the child the leader of a new session with the slave (its fd
// 0) as its controlling terminal.
func ttyAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
}
