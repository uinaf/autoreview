package provider

import (
	"regexp"
	"strings"
)

func missingCapabilities(output string, required []string) []string {
	missing := make([]string, 0)
	for _, capability := range required {
		if !strings.Contains(output, capability) {
			missing = append(missing, capability)
		}
	}
	return missing
}

func optionSupports(help, option, value string) bool {
	optionPattern := regexp.MustCompile(regexp.QuoteMeta(option) + `([^A-Za-z0-9_-]|$)`)
	valuePattern := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(value) + `([^A-Za-z0-9_-]|$)`)
	lines := strings.Split(help, "\n")
	for index, line := range lines {
		if !optionPattern.MatchString(line) {
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
		if valuePattern.MatchString(section) {
			return true
		}
	}
	return false
}
