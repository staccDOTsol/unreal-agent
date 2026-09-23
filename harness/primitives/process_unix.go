//go:build unix

package primitives

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func signalProcessGroup(pid int, signal syscall.Signal) error {
	return syscall.Kill(-pid, signal)
}

func setProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func openProcessCapture(path string, stream string) (*os.File, os.FileInfo, error) {
	var descriptor int
	err := retryEINTR(func() error {
		var openErr error
		descriptor, openErr = unix.Open(
			path,
			unix.O_WRONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC,
			0,
		)
		return openErr
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open process %s capture %q: %w", stream, path, err)
	}

	file := os.NewFile(uintptr(descriptor), path)
	info, err := file.Stat()
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("inspect process %s capture %q: %w", stream, path, err),
			closeProcessFile(file),
		)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.Join(
			fmt.Errorf("open process %s capture %q: path is not a regular file", stream, path),
			closeProcessFile(file),
		)
	}
	if err := unix.SetNonblock(descriptor, false); err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("open process %s capture %q: set blocking: %w", stream, path, err),
			closeProcessFile(file),
		)
	}
	return file, info, nil
}

func truncateProcessCapture(file *os.File, path string, stream string) error {
	if file == nil {
		return nil
	}
	if err := retryEINTR(func() error { return unix.Ftruncate(int(file.Fd()), 0) }); err != nil {
		return errors.Join(
			fmt.Errorf("truncate process %s capture %q: %w", stream, path, err),
			closeProcessFile(file),
		)
	}
	return nil
}

func processGroupExists(processGroupID int) (bool, error) {
	err := syscall.Kill(-processGroupID, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}

func drainProcessOutput(
	ctx context.Context,
	request ProcessStartRequest,
	stream ProcessStream,
	reader *os.File,
	offset *int64,
	events chan<- PrimitiveEvent,
) error {
	fileDescriptor := int(reader.Fd())
	if err := unix.SetNonblock(fileDescriptor, true); err != nil {
		return fmt.Errorf("drain process %s: set nonblocking: %w", processStreamName(stream), err)
	}

	buffer := make([]byte, ProcessOutputChunkSize)
	for {
		count, readErr := unix.Read(fileDescriptor, buffer)
		if count > 0 {
			if !sendProcessOutput(ctx, request, stream, *offset, buffer[:count], events) {
				return nil
			}
			*offset += int64(count)
		}
		if errors.Is(readErr, unix.EINTR) {
			continue
		}
		if errors.Is(readErr, unix.EAGAIN) || errors.Is(readErr, unix.EWOULDBLOCK) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("drain process %s: %w", processStreamName(stream), readErr)
		}
		if count == 0 {
			return nil
		}
	}
}
