package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func discoverExecutable(name, repository string, environment []string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("provider executable is required")
	}
	if strings.ContainsRune(name, filepath.Separator) {
		path, err := filepath.Abs(name)
		if err != nil {
			return "", fmt.Errorf("resolve provider executable: %w", err)
		}
		return validateExecutable(path, repository)
	}
	pathValue := environmentValue(environment, "PATH")
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" {
			directory = "."
		}
		candidate, err := filepath.Abs(filepath.Join(directory, name))
		if err != nil {
			continue
		}
		path, err := validateExecutable(candidate, repository)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("provider executable %q was not found outside the reviewed repository", name)
}

func validateExecutable(path, repository string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return "", fmt.Errorf("resolve reviewed repository: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve reviewed repository: %w", err)
	}
	if pathInside(root, resolved) {
		return "", fmt.Errorf("provider executable %q is inside the reviewed repository", resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("provider executable %q is not an executable regular file", resolved)
	}
	return resolved, nil
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
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
