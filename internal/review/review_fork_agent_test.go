package review

import "testing"

func TestForkSettingsWithProfile(t *testing.T) {
	base := ForkSettings{Model: "sonnet", ReasoningEffort: "high", MaxSteps: 8}
	tests := []struct {
		name    string
		profile ForkSettings
		want    ForkSettings
	}{
		{name: "profile-with-no-fields-keeps-the-base-model-effort-and-steps", profile: ForkSettings{}, want: base},
		{name: "light-lowers-effort", profile: ForkSettings{Model: "haiku", ReasoningEffort: "low"}, want: ForkSettings{Model: "haiku", ReasoningEffort: "low", MaxSteps: 8}},
		{name: "heavy-adds-steps", profile: ForkSettings{MaxSteps: 16}, want: ForkSettings{Model: "sonnet", ReasoningEffort: "high", MaxSteps: 16}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base.withProfile(tt.profile); got != tt.want {
				t.Fatalf("withProfile = %+v; want %+v", got, tt.want)
			}
		})
	}
}
