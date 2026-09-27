package upgrade

import "strings"

// GentleAIVersion returns the version gentle-ai itself upgraded to, without a
// leading "v", when the report contains a successful gentle-ai upgrade. The
// running binary is then stale and must be restarted before syncing.
func (r UpgradeReport) GentleAIVersion() (string, bool) {
	for _, result := range r.Results {
		if result.ToolName == "gentle-ai" && result.Status == UpgradeSucceeded {
			return strings.TrimPrefix(result.NewVersion, "v"), true
		}
	}
	return "", false
}
