package entity

import "testing"

func TestTableNames(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
	}{
		{"Notification", Notification{}.TableName()},
		{"Template", Template{}.TableName()},
		{"Preference", Preference{}.TableName()},
		{"DeviceToken", DeviceToken{}.TableName()},
	}

	want := map[string]string{
		"Notification": "notification.notifications",
		"Template":     "notification.templates",
		"Preference":   "notification.preferences",
		"DeviceToken":  "notification.device_tokens",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.tableName != want[tt.name] {
				t.Errorf("%s.TableName() = %q, want %q", tt.name, tt.tableName, want[tt.name])
			}
		})
	}
}
