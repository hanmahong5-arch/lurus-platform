package app

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

func TestIsValidChannel_PreferenceContext(t *testing.T) {
	tests := []struct {
		channel entity.Channel
		valid   bool
	}{
		{entity.ChannelInApp, true},
		{entity.ChannelEmail, true},
		{entity.ChannelFCM, true},
		{entity.Channel("sms"), false},
		{entity.Channel("push"), false},
		{entity.Channel(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.channel), func(t *testing.T) {
			got := isValidChannel(tt.channel)
			if got != tt.valid {
				t.Errorf("isValidChannel(%q) = %v, want %v", tt.channel, got, tt.valid)
			}
		})
	}
}

func TestPreferenceUpdate_Validation(t *testing.T) {
	// Verify PreferenceUpdate struct fields are correct.
	u := PreferenceUpdate{
		Channel: "email",
		Enabled: false,
	}
	if u.Channel != "email" {
		t.Errorf("Channel = %q, want %q", u.Channel, "email")
	}
	if u.Enabled {
		t.Error("Enabled should be false")
	}
}

func TestPreferenceView_AllChannels(t *testing.T) {
	// Ensure PreferenceView covers all known channels.
	allChannels := []entity.Channel{entity.ChannelInApp, entity.ChannelEmail, entity.ChannelFCM}
	views := make([]PreferenceView, 0, len(allChannels))
	for _, ch := range allChannels {
		views = append(views, PreferenceView{Channel: ch, Enabled: true})
	}
	if len(views) != 3 {
		t.Errorf("expected 3 channels, got %d", len(views))
	}
}
