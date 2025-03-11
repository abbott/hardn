package utils

import "strings"

// Capitalize returns the string with the first letter capitalized
func Capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
