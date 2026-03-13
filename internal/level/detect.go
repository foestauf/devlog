package level

import "regexp"

// Compiled patterns for level detection, ordered from most to least severe.
var patterns = []struct {
	re    *regexp.Regexp
	level string
}{
	{regexp.MustCompile(`(?i)\b(?:FATAL|PANIC)\b`), "fatal"},
	{regexp.MustCompile(`(?i)\b(?:ERROR|ERR)\b`), "error"},
	{regexp.MustCompile(`(?i)\b(?:WARN|WARNING)\b`), "warn"},
	{regexp.MustCompile(`(?i)\bINFO\b`), "info"},
	{regexp.MustCompile(`(?i)\b(?:DEBUG|TRACE)\b`), "debug"},
	// JSON structured log pattern: "level":"error" (with optional spaces).
	{regexp.MustCompile(`"level"\s*:\s*"(fatal|panic)"`), "fatal"},
	{regexp.MustCompile(`"level"\s*:\s*"error"`), "error"},
	{regexp.MustCompile(`"level"\s*:\s*"warn(?:ing)?"`), "warn"},
	{regexp.MustCompile(`"level"\s*:\s*"info"`), "info"},
	{regexp.MustCompile(`"level"\s*:\s*"(?:debug|trace)"`), "debug"},
}

// Detect examines a log line and returns the detected level.
// Returns "" if no level pattern matches.
func Detect(line string) string {
	for _, p := range patterns {
		if p.re.MatchString(line) {
			return p.level
		}
	}
	return ""
}
