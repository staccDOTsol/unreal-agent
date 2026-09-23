//go:build windows

package primitives

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func normalizeProcessGroupSignalError(_ int, err error) error {
	return err
}

func signalProcessGroup(pid int, _ syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	err = process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return syscall.ESRCH
	}
	return err
}

func setProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func openProcessCapture(path string, stream string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open process %s capture %q: %w", stream, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("open process %s capture %q: path is not a regular file", stream, path)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open process %s capture %q: %w", stream, path, err)
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("inspect process %s capture %q: %w", stream, path, err),
			closeProcessFile(file),
		)
	}
	if !stat.Mode().IsRegular() {
		return nil, nil, errors.Join(
			fmt.Errorf("open process %s capture %q: path is not a regular file", stream, path),
			closeProcessFile(file),
		)
	}
	return file, stat, nil
}

func truncateProcessCapture(file *os.File, path string, stream string) error {
	if file == nil {
		return nil
	}
	if err := file.Truncate(0); err != nil {
		return errors.Join(
			fmt.Errorf("truncate process %s capture %q: %w", stream, path, err),
			closeProcessFile(file),
		)
	}
	return nil
}

func processGroupExists(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return true, nil
		}
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, err
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false, err
	}
	const stillActive = 259
	return code == stillActive, nil
}

func drainProcessOutput(
	ctx context.Context,
	request ProcessStartRequest,
	stream ProcessStream,
	reader *os.File,
	offset *int64,
	events chan<- PrimitiveEvent,
) error {
	if err := reader.SetReadDeadline(time.Now()); err != nil {
		return fmt.Errorf("drain process %s: set nonblocking: %w", processStreamName(stream), err)
	}
	buffer := make([]byte, ProcessOutputChunkSize)
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if !sendProcessOutput(ctx, request, stream, *offset, buffer[:count], events) {
				return nil
			}
			*offset += int64(count)
		}
		if errors.Is(readErr, io.EOF) || os.IsTimeout(readErr) {
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
