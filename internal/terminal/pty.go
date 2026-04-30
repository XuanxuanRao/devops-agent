package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type PtyFile interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}

type PtyProcess struct {
	File   PtyFile
	PID    int
	Ref    string
	Wait   func() error
	Signal func(sig os.Signal) error
}

type PtyFactory interface {
	Start(spec StartSpec) (*PtyProcess, error)
	Resize(file PtyFile, cols, rows int) error
}

type StartSpec struct {
	SessionID string
	Shell     string
	Cwd       string
	Env       []string
	Cols      int
	Rows      int
	Title     string
	Stdout    *os.File
}

type RealPtyFactory struct{}

func NewRealPtyFactory() *RealPtyFactory {
	return &RealPtyFactory{}
}

func (f *RealPtyFactory) Start(spec StartSpec) (*PtyProcess, error) {
	if spec.Shell == "" {
		return nil, fmt.Errorf("shell is required")
	}

	cmd := exec.Command(spec.Shell)
	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}
	cmd.Env = append(os.Environ(), spec.Env...)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(spec.Cols),
		Rows: uint16(spec.Rows),
	})
	if err != nil {
		return nil, err
	}

	return &PtyProcess{
		File: ptmx,
		PID:  cmd.Process.Pid,
		Ref:  fmt.Sprintf("pty_%d", cmd.Process.Pid),
		Wait: cmd.Wait,
		Signal: func(sig os.Signal) error {
			raw, ok := sig.(syscall.Signal)
			if !ok {
				return fmt.Errorf("unsupported signal type %T", sig)
			}

			pgid, err := syscall.Getpgid(cmd.Process.Pid)
			if err != nil {
				return err
			}
			return syscall.Kill(-pgid, raw)
		},
	}, nil
}

func (f *RealPtyFactory) Resize(file PtyFile, cols, rows int) error {
	ptmx, ok := file.(*os.File)
	if !ok {
		return fmt.Errorf("real pty resize requires *os.File, got %T", file)
	}

	return pty.Setsize(ptmx, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}
