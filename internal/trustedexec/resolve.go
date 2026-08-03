package trustedexec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Resolve(name, configuredPath, repository string, environment []string) (string, error) {
	boundaries, err := repositoryBoundaries(repository)
	if err != nil {
		return "", err
	}
	if configuredPath != "" && strings.ContainsRune(configuredPath, filepath.Separator) {
		path, err := validate(configuredPath, boundaries)
		if err != nil {
			return "", fmt.Errorf("trusted %s executable: %w", name, err)
		}
		return path, nil
	}
	executable := configuredPath
	if executable == "" {
		executable = name
	}
	for _, directory := range filepath.SplitList(environmentValue(environment, "PATH")) {
		if !filepath.IsAbs(directory) {
			continue
		}
		path, err := validate(filepath.Join(directory, executable), boundaries)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("trusted %s executable %q was not found outside the reviewed repository", name, executable)
}

func GitEnvironment() []string {
	return []string{
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"LANG=C",
		"LC_ALL=C",
	}
}

func repositoryBoundaries(repository string) ([]string, error) {
	if repository == "" {
		repository = "."
	}
	absolute, err := filepath.Abs(repository)
	if err != nil {
		return nil, fmt.Errorf("resolve reviewed repository: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve reviewed repository: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("inspect reviewed repository: %w", err)
	}
	if !info.IsDir() {
		absolute = filepath.Dir(absolute)
		resolved = filepath.Dir(resolved)
	}
	boundaries := make([]string, 0, 2)
	for _, root := range []string{absolute, resolved} {
		boundary, err := outermostWorktree(root)
		if err != nil {
			return nil, err
		}
		if boundary != "" && (len(boundaries) == 0 || boundary != boundaries[0]) {
			boundaries = append(boundaries, boundary)
		}
	}
	if len(boundaries) == 0 {
		return nil, fmt.Errorf("reviewed path is not inside a Git worktree")
	}
	return boundaries, nil
}

func outermostWorktree(root string) (string, error) {
	var boundary string
	for directory := root; ; directory = filepath.Dir(directory) {
		if _, err := os.Lstat(filepath.Join(directory, ".git")); err == nil {
			boundary = directory
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect reviewed repository boundary: %w", err)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return boundary, nil
}

func validate(path string, boundaries []string) (string, error) {
	original, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(original)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	for _, boundary := range boundaries {
		originalInside, err := pathInside(boundary, original)
		if err != nil {
			return "", err
		}
		resolvedInside, err := pathInside(boundary, resolved)
		if err != nil {
			return "", err
		}
		if originalInside || resolvedInside {
			return "", fmt.Errorf("executable %q is inside the reviewed repository", resolved)
		}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("executable %q is not an executable regular file", resolved)
	}
	return resolved, nil
}

func pathInside(root, candidate string) (bool, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false, err
	}
	for current := candidate; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return false, err
		}
		if os.SameFile(rootInfo, info) {
			return true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
	}
}

func environmentValue(environment []string, name string) string {
	for index := len(environment) - 1; index >= 0; index-- {
		key, value, found := strings.Cut(environment[index], "=")
		if found && key == name {
			return value
		}
	}
	return ""
}
