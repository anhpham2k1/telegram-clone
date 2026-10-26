package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

type terminalAuth struct {
	reader   *bufio.Reader
	phone    string
	password string
	mu       sync.Mutex
}

func newTerminalAuth(phone, password string) *terminalAuth {
	return &terminalAuth{
		reader:   bufio.NewReader(os.Stdin),
		phone:    phone,
		password: password,
	}
}

func (a *terminalAuth) Phone(context.Context) (string, error) {
	if a.phone == "" {
		return "", errors.New("TG_PHONE is empty")
	}
	return a.phone, nil
}

func (a *terminalAuth) Code(context.Context, *tg.AuthSentCode) (string, error) {
	return a.read("Telegram login code: ")
}

func (a *terminalAuth) Password(context.Context) (string, error) {
	if a.password != "" {
		return a.password, nil
	}
	return a.read("2FA password: ")
}

func (a *terminalAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return nil
}

func (a *terminalAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("this account has no Telegram profile; log in once with an official client first")
}

func (a *terminalAuth) read(prompt string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	fmt.Fprint(os.Stderr, prompt)
	line, err := a.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
