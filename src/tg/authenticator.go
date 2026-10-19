package tg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/gotd/td/telegram/auth"
	tgapi "github.com/gotd/td/tg"
)

const resendKeyword = "resend"

type ResendCodeFunc func(ctx context.Context, phone string, phoneCodeHash string) (tgapi.AuthSentCodeClass, error)

type terminalAuthenticator struct {
	phone  string
	resend ResendCodeFunc
}

func NewTerminalAuthenticator(phone string) auth.UserAuthenticator {
	return &terminalAuthenticator{phone: normalizePhone(phone)}
}

func (a *terminalAuthenticator) Phone(ctx context.Context) (string, error) {
	if a.phone != "" {
		return a.phone, nil
	}
	raw, err := prompt("Phone number (international format, e.g. +79991234567)", huh.EchoModeNormal)
	if err != nil {
		return "", err
	}
	a.phone = normalizePhone(raw)
	return a.phone, nil
}

func (a *terminalAuthenticator) Password(ctx context.Context) (string, error) {
	return prompt("Two-factor password", huh.EchoModePassword)
}

func (a *terminalAuthenticator) Code(ctx context.Context, sentCode *tgapi.AuthSentCode) (string, error) {
	for {
		description := describeSentCode(sentCode)
		fmt.Fprintln(os.Stderr, description)
		code, err := prompt("Code from Telegram (or type \""+resendKeyword+"\" to request it another way)", huh.EchoModeNormal)
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(code, resendKeyword) {
			return strings.ReplaceAll(code, "-", ""), nil
		}
		if a.resend == nil {
			fmt.Fprintln(os.Stderr, "resend is not available")
			continue
		}
		resent, err := a.resend(ctx, a.phone, sentCode.PhoneCodeHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "resend failed: %v\n", err)
			continue
		}
		next, ok := resent.(*tgapi.AuthSentCode)
		if !ok {
			return "", fmt.Errorf("unexpected resend response %T", resent)
		}
		sentCode = next
	}
}

func (a *terminalAuthenticator) AcceptTermsOfService(ctx context.Context, tos tgapi.HelpTermsOfService) error {
	return nil
}

func (a *terminalAuthenticator) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up is not supported, use an existing account")
}

func describeSentCode(sentCode *tgapi.AuthSentCode) string {
	var where string
	switch t := sentCode.Type.(type) {
	case *tgapi.AuthSentCodeTypeApp:
		where = fmt.Sprintf("Telegram sent a %d-digit code to your other logged-in Telegram apps: open the \"Telegram\" service chat (not SMS)", t.Length)
	case *tgapi.AuthSentCodeTypeSMS:
		where = fmt.Sprintf("Telegram sent a %d-digit code via SMS", t.Length)
	case *tgapi.AuthSentCodeTypeCall:
		where = fmt.Sprintf("Telegram is calling you, the %d-digit code is dictated in the call", t.Length)
	case *tgapi.AuthSentCodeTypeFlashCall:
		where = "Telegram is making a flash call, enter the calling number's last digits"
	case *tgapi.AuthSentCodeTypeMissedCall:
		where = fmt.Sprintf("Telegram is making a missed call from a number starting with %s, enter its last %d digits", t.Prefix, t.Length)
	case *tgapi.AuthSentCodeTypeEmailCode:
		where = fmt.Sprintf("Telegram sent a %d-digit code to your login email %s", t.Length, t.EmailPattern)
	case *tgapi.AuthSentCodeTypeFragmentSMS:
		where = fmt.Sprintf("the code is available at %s (Fragment)", t.URL)
	case *tgapi.AuthSentCodeTypeSetUpEmailRequired:
		where = "Telegram requires setting up a login email for this account first; do it in an official Telegram app"
	default:
		where = fmt.Sprintf("Telegram sent the code via %s", sentCode.Type.TypeName())
	}
	nextType, hasNext := sentCode.GetNextType()
	if hasNext {
		where += fmt.Sprintf("; if nothing arrives, type \"%s\" to get it via %s", resendKeyword, strings.TrimPrefix(nextType.TypeName(), "auth.codeType"))
	}
	return where
}

func normalizePhone(raw string) string {
	var sb strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return ""
	}
	return "+" + sb.String()
}

func prompt(title string, mode huh.EchoMode) (string, error) {
	var value string
	err := huh.NewInput().
		Title(title).
		EchoMode(mode).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("value is required")
			}
			return nil
		}).
		Value(&value).
		Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}
