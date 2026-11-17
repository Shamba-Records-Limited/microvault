package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMobileConfigValidate(t *testing.T) {
	valid := func() MobileConfig {
		return MobileConfig{
			SMSProvider: SMSProviderMobileSasa,
			MobileSasa: MobileSasaConfig{
				APIToken:    "mbs_token",
				SenderID:    "MICROVAULT",
				CallbackURL: "https://core.example/api/v1/mobile/sms/mobilesasa/delivery?token=t",
			},
		}
	}
	tests := []struct {
		name    string
		mutate  func(*MobileConfig)
		wantErr string
	}{
		{name: "mobile sasa complete"},
		{name: "africastalking needs nothing extra", mutate: func(c *MobileConfig) { c.SMSProvider = SMSProviderAfricasTalking; c.MobileSasa = MobileSasaConfig{} }},
		{name: "unknown provider", mutate: func(c *MobileConfig) { c.SMSProvider = "twilio" }, wantErr: "SMS_PROVIDER"},
		{name: "no token", mutate: func(c *MobileConfig) { c.MobileSasa.APIToken = "" }, wantErr: "MOBILESASA_API_TOKEN"},
		{name: "no sender", mutate: func(c *MobileConfig) { c.MobileSasa.SenderID = "" }, wantErr: "MOBILESASA_SENDER_ID"},
		{name: "plain http base url", mutate: func(c *MobileConfig) { c.MobileSasa.BaseURL = "http://api.mobilesasa.com" }, wantErr: "MOBILESASA_BASE_URL"},
		{name: "relative callback", mutate: func(c *MobileConfig) { c.MobileSasa.CallbackURL = "/delivery" }, wantErr: "MOBILESASA_CALLBACK_URL"},
		{name: "callback too long", mutate: func(c *MobileConfig) { c.MobileSasa.CallbackURL = "https://x.example/" + strings.Repeat("a", 250) }, wantErr: "at most 255"},
		{name: "no callback is allowed", mutate: func(c *MobileConfig) { c.MobileSasa.CallbackURL = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			if tt.mutate != nil {
				tt.mutate(&c)
			}
			err := c.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NotContains(t, err.Error(), "token=t", "the callback URL carries a secret")
		})
	}
}
