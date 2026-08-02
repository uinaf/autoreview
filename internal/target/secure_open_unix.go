//go:build darwin || linux

package target

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func openRegularFile(root, relative string) (*os.File, os.FileInfo, error) {
	return walkRegularFile(root, relative, false)
}

func validateRegularOrMissingFile(root, relative string) error {
	file, _, err := walkRegularFile(root, relative, true)
	if file != nil {
		_ = file.Close()
	}
	return err
}

func walkRegularFile(root, relative string, allowMissingFinal bool) (*os.File, os.FileInfo, error) {
	current, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	components := strings.Split(relative, "/")
	for index, component := range components {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if index+1 < len(components) {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(current, component, flags, 0)
		_ = unix.Close(current)
		if openErr != nil {
			if allowMissingFinal && index+1 == len(components) && errors.Is(openErr, unix.ENOENT) {
				return nil, nil, nil
			}
			return nil, nil, fmt.Errorf("no-follow open: %w", openErr)
		}
		current = next
	}
	file := os.NewFile(uintptr(current), relative)
	if file == nil {
		_ = unix.Close(current)
		return nil, nil, fmt.Errorf("wrap opened file")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("not a regular file")
	}
	return file, info, nil
}
