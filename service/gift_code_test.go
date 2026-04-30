package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestGiftCodePrimaryNotifyAvailable(t *testing.T) {
	tests := []struct {
		name        string
		notifyType  string
		setting     dto.UserSetting
		emailToUse  string
		expectReady bool
	}{
		{
			name:        "email requires resolved email",
			notifyType:  dto.NotifyTypeEmail,
			emailToUse:  "creator@example.com",
			expectReady: true,
		},
		{
			name:       "webhook requires url",
			notifyType: dto.NotifyTypeWebhook,
			setting: dto.UserSetting{
				WebhookUrl: "https://example.com/hook",
			},
			expectReady: true,
		},
		{
			name:       "gotify requires url and token",
			notifyType: dto.NotifyTypeGotify,
			setting: dto.UserSetting{
				GotifyUrl:   "https://gotify.example.com",
				GotifyToken: "token",
			},
			expectReady: true,
		},
		{
			name:        "bark without url is unavailable",
			notifyType:  dto.NotifyTypeBark,
			expectReady: false,
		},
		{
			name:       "gotify without token is unavailable",
			notifyType: dto.NotifyTypeGotify,
			setting: dto.UserSetting{
				GotifyUrl: "https://gotify.example.com",
			},
			expectReady: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ready := giftCodePrimaryNotifyAvailable(
				testCase.notifyType,
				testCase.setting,
				testCase.emailToUse,
			)
			if ready != testCase.expectReady {
				t.Fatalf("expected %v, got %v", testCase.expectReady, ready)
			}
		})
	}
}

func TestGiftCodeNotificationTextEscapesHTML(t *testing.T) {
	input := ` <img src=x onerror="alert(1)">谢谢<script>alert(2)</script> `
	got := giftCodeNotificationText(input)
	want := `&lt;img src=x onerror=&#34;alert(1)&#34;&gt;谢谢&lt;script&gt;alert(2)&lt;/script&gt;`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
