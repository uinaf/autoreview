package provider

import (
	"regexp"
	"strings"
)

func missingCapabilities(output string, required []string) []string {
	missing := make([]string, 0)
	for _, capability := range required {
		if !containsCapability(output, capability) {
			missing = append(missing, capability)
		}
	}
	return missing
}

func optionSupports(help, option, value string) bool {
	lines := strings.Split(help, "\n")
	for index, line := range lines {
		if !containsCapability(line, option) {
			continue
		}
		section := line
		for next := index + 1; next < len(lines); next++ {
			trimmed := strings.TrimSpace(lines[next])
			if strings.HasPrefix(trimmed, "-") {
				break
			}
			section += "\n" + lines[next]
		}
		if containsCapability(section, value) {
			return true
		}
	}
	return false
}

func containsCapability(output, capability string) bool {
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(capability) + `([^A-Za-z0-9_-]|$)`)
	return pattern.MatchString(output)
}
