//go:build darwin

package term

import (
	"os"
	"syscall"
	"unsafe"
)

type termState struct{ t syscall.Termios }

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

func isTTY(f *os.File) bool {
	var t syscall.Termios
	return ioctl(f.Fd(), syscall.TIOCGETA, unsafe.Pointer(&t)) == nil
}

func makeRaw(f *os.File) (*termState, error) {
	var old syscall.Termios
	if err := ioctl(f.Fd(), syscall.TIOCGETA, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(f.Fd(), syscall.TIOCSETA, unsafe.Pointer(&raw)); err != nil {
		return nil, err
	}
	return &termState{old}, nil
}

func restore(f *os.File, s *termState) {
	_ = ioctl(f.Fd(), syscall.TIOCSETA, unsafe.Pointer(&s.t))
}

func termSize(f *os.File) (w, h int, err error) {
	var ws struct{ Row, Col, X, Y uint16 }
	if err = ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}
