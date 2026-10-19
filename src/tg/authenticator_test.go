package tg

import (
	"testing"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"
)

func TestNormalizePhone(t *testing.T) {
	require.Equal(t, "+79991234567", normalizePhone("+7 999 123-45-67"))
	require.Equal(t, "+79991234567", normalizePhone("79991234567"))
	require.Equal(t, "", normalizePhone(""))
}

func TestDescribeSentCode(t *testing.T) {
	t.Run("app with next type", func(t *testing.T) {
		sent := &tg.AuthSentCode{Type: &tg.AuthSentCodeTypeApp{Length: 5}}
		sent.SetNextType(&tg.AuthCodeTypeSMS{})
		act := describeSentCode(sent)
		require.Contains(t, act, "5-digit")
		require.Contains(t, act, "service chat")
		require.Contains(t, act, "resend")
		require.Contains(t, act, "Sms")
	})
	t.Run("sms", func(t *testing.T) {
		act := describeSentCode(&tg.AuthSentCode{Type: &tg.AuthSentCodeTypeSMS{Length: 6}})
		require.Contains(t, act, "SMS")
		require.NotContains(t, act, "resend")
	})
}
