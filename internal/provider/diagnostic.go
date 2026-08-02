package provider

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maximumDiagnosticRunes = 4000

func sanitizeDiagnostic(text string, environment []string) string {
	secretValues := make([]string, 0)
	seen := map[string]struct{}{}
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if !found || value == "" || !sensitiveEnvironmentName(name) {
			continue
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			secretValues = append(secretValues, value)
		}
	}
	sort.Slice(secretValues, func(left, right int) bool {
		return len(secretValues[left]) > len(secretValues[right])
	})
	replacements := make([]string, 0, len(secretValues)*2)
	for _, value := range secretValues {
		replacements = append(replacements, value, "[redacted]")
	}
	if len(replacements) != 0 {
		text = strings.NewReplacer(replacements...).Replace(text)
	}
	text = strings.ToValidUTF8(text, "�")
	var sanitized strings.Builder
	for _, character := range text {
		switch {
		case character == '\n' || character == '\t':
			sanitized.WriteRune(character)
		case unicode.IsControl(character) || unicode.Is(unicode.Cf, character):
			if character <= 0xff {
				fmt.Fprintf(&sanitized, "\\x%02x", character)
			} else {
				fmt.Fprintf(&sanitized, "\\u%04x", character)
			}
		default:
			sanitized.WriteRune(character)
		}
	}
	value := sanitized.String()
	if utf8.RuneCountInString(value) <= maximumDiagnosticRunes {
		return strings.TrimSpace(value)
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maximumDiagnosticRunes])) + "…[truncated]"
}

func sensitiveEnvironmentName(name string) bool {
	name = strings.ToUpper(name)
	for _, marker := range []string{
		"AUTH", "BEARER", "COOKIE", "CRED", "DSN", "KEY", "OAUTH", "PASS",
		"PAT", "PRIVATE", "PROXY", "SECRET", "SESSION", "TOKEN", "URI", "URL",
	} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}
