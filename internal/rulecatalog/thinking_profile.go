package rulecatalog

import (
	"fmt"
	"strings"
)

// ThinkingProfile picks the fork settings of a rule: a light rule runs cheap, a heavy rule reads more.
// The user config profiles table maps each profile to its model, effort, and steps.
type ThinkingProfile string

// ThinkingProfile values; a rule with no profile is medium.
const (
	ThinkingProfileLight  ThinkingProfile = "light"
	ThinkingProfileMedium ThinkingProfile = "medium"
	ThinkingProfileHeavy  ThinkingProfile = "heavy"
)

// ParseThinkingProfile maps a rule profile string; an empty string means medium.
func ParseThinkingProfile(text string) (ThinkingProfile, error) {
	switch profile := ThinkingProfile(strings.ToLower(text)); profile {
	case "":
		return ThinkingProfileMedium, nil
	case ThinkingProfileLight, ThinkingProfileMedium, ThinkingProfileHeavy:
		return profile, nil
	}
	return "", fmt.Errorf("review rule profile %q is not light, medium, or heavy", text)
}
