package netns

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/sirupsen/logrus"
)

type Dialer struct {
	Log        *logrus.Logger
	BinaryPath string
}

func NewDialer(log *logrus.Logger, binaryPath string) *Dialer {
	return &Dialer{
		Log:        log,
		BinaryPath: binaryPath,
	}
}

type NamespaceConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (nc *NamespaceConn) Read(p []byte) (n int, err error) {
	return nc.stdout.Read(p)
}

func (nc *NamespaceConn) Write(p []byte) (n int, err error) {
	return nc.stdin.Write(p)
}

func (nc *NamespaceConn) Close() error {
	nc.stdin.Close()
	nc.stdout.Close()
	return nc.cmd.Process.Kill()
}

func (d *Dialer) Dial(slotName string, addr string) (io.ReadWriteCloser, error) {
	cmd := exec.Command("ip", "netns", "exec", slotName, d.BinaryPath, "dial", "--addr", addr)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe for %s: %w", slotName, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe for %s: %w", slotName, err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("dial %s via %s: %w", addr, slotName, err)
	}

	return &NamespaceConn{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}, nil
}
