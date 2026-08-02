//go:build darwin || linux

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func readUntrustedConfigFile(path string) ([]byte, error) {
	directory, name := filepath.Split(path)
	root, err := unix.Open(filepath.Clean(directory), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	descriptor, openErr := unix.Openat(root, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	_ = unix.Close(root)
	if openErr != nil {
		return nil, openErr
	}
	return readOpenedConfig(descriptor, path)
}

func readTrustedConfigFile(path, trustedRoot, repositoryRoot string) ([]byte, error) {
	relative, err := filepath.Rel(trustedRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, fmt.Errorf("trusted XDG config must be beneath the account home")
	}
	resolvedRoot, err := filepath.EvalSymlinks(trustedRoot)
	if err != nil {
		return nil, err
	}
	resolvedPath := filepath.Join(resolvedRoot, relative)
	if pathContained(repositoryRoot, resolvedPath) {
		return nil, fmt.Errorf("trusted XDG config must be outside the repository")
	}
	current, err := unix.Open(resolvedRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	if err := validateTrustedDescriptor(current, false); err != nil {
		_ = unix.Close(current)
		return nil, err
	}
	components := strings.Split(filepath.ToSlash(relative), "/")
	for index, component := range components {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if index+1 < len(components) {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(current, component, flags, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return nil, fmt.Errorf("open trusted XDG config: %w", openErr)
		}
		if err := validateTrustedDescriptor(next, index+1 == len(components)); err != nil {
			_ = unix.Close(next)
			return nil, err
		}
		current = next
	}
	return readOpenedConfig(current, resolvedPath)
}

func validateTrustedDescriptor(descriptor int, final bool) error {
	var info unix.Stat_t
	if err := unix.Fstat(descriptor, &info); err != nil {
		return err
	}
	mode := uint32(info.Mode)
	if final {
		if info.Uid != uint32(os.Geteuid()) {
			return fmt.Errorf("trusted XDG config must be owned by the current user")
		}
		if mode&unix.S_IFMT != unix.S_IFREG || mode&0o022 != 0 {
			return fmt.Errorf("trusted XDG config must be a non-writable regular file")
		}
		return nil
	}
	if info.Uid != 0 && info.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("trusted XDG config directory has an unexpected owner")
	}
	if mode&0o022 != 0 && mode&unix.S_ISVTX == 0 {
		return fmt.Errorf("trusted XDG config directory is group or world writable")
	}
	return nil
}

func pathContained(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func readOpenedConfig(descriptor int, path string) ([]byte, error) {
	if descriptor < 0 {
		return nil, errors.New("invalid config descriptor")
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, fmt.Errorf("wrap config file")
	}
	defer func() { _ = file.Close() }()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("config is not a regular file")
	}
	if before.Size() > maximumConfigBytes {
		return nil, fmt.Errorf("config exceeds %d bytes", maximumConfigBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumConfigBytes+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(content)) != before.Size() || int64(len(content)) > maximumConfigBytes {
		return nil, fmt.Errorf("config changed or exceeded its limit while reading")
	}
	return content, nil
}
