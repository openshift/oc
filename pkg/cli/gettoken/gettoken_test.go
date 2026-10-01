package gettoken

import (
	"context"
	"fmt"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/openshift/oc/pkg/cli/gettoken/oidc"
	"github.com/openshift/oc/pkg/cli/gettoken/tokencache"
)

type mockAuthenticator struct {
	authCodeToken   string
	deviceCodeToken string
	refreshToken    string
	expiry          time.Time
	authCodeErr     error
	deviceCodeErr   error
	refreshErr      error
	verifyErr       error
}

func (m *mockAuthenticator) GetTokenByAuthCode(ctx context.Context, callbackAddress string, localServerReadyChan chan<- string) (string, string, time.Time, error) {
	localServerReadyChan <- "http://127.0.0.1:0/callback"
	if m.authCodeErr != nil {
		return "", "", time.Time{}, m.authCodeErr
	}
	return m.authCodeToken, m.refreshToken, m.expiry, nil
}

func (m *mockAuthenticator) GetTokenByDeviceCode(ctx context.Context, readyChan chan<- oidc.DeviceAuthInfo) (string, string, time.Time, error) {
	readyChan <- oidc.DeviceAuthInfo{
		UserCode:        "ABCD-1234",
		VerificationURI: "https://example.com/device",
	}
	if m.deviceCodeErr != nil {
		return "", "", time.Time{}, m.deviceCodeErr
	}
	return m.deviceCodeToken, m.refreshToken, m.expiry, nil
}

func (m *mockAuthenticator) Refresh(ctx context.Context, refreshToken string) (string, string, time.Time, error) {
	if m.refreshErr != nil {
		return "", "", time.Time{}, m.refreshErr
	}
	return m.authCodeToken, refreshToken, m.expiry, nil
}

func (m *mockAuthenticator) VerifyToken(ctx context.Context, token *oauth2.Token, nonce string) (string, time.Time, error) {
	if m.verifyErr != nil {
		return "", time.Time{}, m.verifyErr
	}
	idToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", time.Time{}, fmt.Errorf("no id_token")
	}
	return idToken, m.expiry, nil
}

func TestValidateGrantType(t *testing.T) {
	testCases := []struct {
		name          string
		grantType     string
		expectedError string
	}{
		{
			name:      "empty grant-type is valid",
			grantType: "",
		},
		{
			name:      "authorization-code is valid",
			grantType: "authorization-code",
		},
		{
			name:      "device-code is valid",
			grantType: "device-code",
		},
		{
			name:          "unsupported grant-type is invalid",
			grantType:     "implicit",
			expectedError: `unsupported --grant-type "implicit", supported values are "authorization-code" and "device-code"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := &GetTokenOptions{
				IssuerURL: "https://example.com",
				ClientID:  "test-client",
				GrantType: tc.grantType,
			}
			err := o.Validate()
			if tc.expectedError == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error %q, got nil", tc.expectedError)
				} else if err.Error() != tc.expectedError {
					t.Errorf("expected error %q, got: %v", tc.expectedError, err)
				}
			}
		})
	}
}

func TestEffectiveGrantType(t *testing.T) {
	testCases := []struct {
		name     string
		grant    string
		expected string
	}{
		{name: "empty defaults to authorization-code", grant: "", expected: "authorization-code"},
		{name: "authorization-code", grant: "authorization-code", expected: "authorization-code"},
		{name: "device-code", grant: "device-code", expected: "device-code"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			o := &GetTokenOptions{GrantType: tc.grant}
			if got := o.effectiveGrantType(); got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestDoInitialAuthDispatch(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour)
	streams := genericiooptions.NewTestIOStreamsDiscard()

	t.Run("dispatches to auth code when grant-type is empty", func(t *testing.T) {
		mock := &mockAuthenticator{
			authCodeToken: "auth-code-id-token",
			refreshToken:  "auth-code-refresh",
			expiry:        expiry,
		}
		o := &GetTokenOptions{
			GrantType:             "",
			authenticator:         mock,
			authenticationTimeout: 5 * time.Minute,
			IOStreams:             streams,
		}
		idToken, _, _, err := o.doInitialAuth(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if idToken != "auth-code-id-token" {
			t.Errorf("expected auth-code-id-token, got %q", idToken)
		}
	})

	t.Run("dispatches to device code when grant-type is device-code", func(t *testing.T) {
		mock := &mockAuthenticator{
			deviceCodeToken: "device-code-id-token",
			refreshToken:    "device-refresh",
			expiry:          expiry,
		}
		o := &GetTokenOptions{
			GrantType:             "device-code",
			authenticator:         mock,
			authenticationTimeout: 5 * time.Minute,
			IOStreams:             streams,
		}
		idToken, _, _, err := o.doInitialAuth(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if idToken != "device-code-id-token" {
			t.Errorf("expected device-code-id-token, got %q", idToken)
		}
	})
}

func TestDoDeviceCodeOutputsInfo(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour)
	streams, _, _, errOut := genericiooptions.NewTestIOStreams()

	mock := &mockAuthenticator{
		deviceCodeToken: "device-id-token",
		refreshToken:    "device-refresh",
		expiry:          expiry,
	}
	o := &GetTokenOptions{
		GrantType:             "device-code",
		authenticator:         mock,
		authenticationTimeout: 5 * time.Minute,
		IOStreams:             streams,
	}

	idToken, refreshToken, _, err := o.doDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if idToken != "device-id-token" {
		t.Errorf("expected id token %q, got %q", "device-id-token", idToken)
	}
	if refreshToken != "device-refresh" {
		t.Errorf("expected refresh token %q, got %q", "device-refresh", refreshToken)
	}

	output := errOut.String()
	if len(output) == 0 {
		t.Fatal("expected output to ErrOut, got nothing")
	}
	expectedStrings := []string{
		"https://example.com/device",
		"ABCD-1234",
		"To authenticate",
	}
	for _, s := range expectedStrings {
		if !contains(output, s) {
			t.Errorf("expected ErrOut to contain %q, got: %s", s, output)
		}
	}
}

func TestDoDeviceCodeError(t *testing.T) {
	streams := genericiooptions.NewTestIOStreamsDiscard()

	mock := &mockAuthenticator{
		deviceCodeErr: fmt.Errorf("provider does not support device code"),
	}
	o := &GetTokenOptions{
		GrantType:             "device-code",
		authenticator:         mock,
		authenticationTimeout: 5 * time.Minute,
		IOStreams:             streams,
	}

	_, _, _, err := o.doDeviceCode(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "device code flow error") {
		t.Errorf("expected error to contain 'device code flow error', got: %v", err)
	}
}

func TestGetTokenWithDeviceCodeNilCache(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour)
	streams := genericiooptions.NewTestIOStreamsDiscard()

	mock := &mockAuthenticator{
		deviceCodeToken: "device-token",
		refreshToken:    "device-refresh",
		expiry:          expiry,
	}
	o := &GetTokenOptions{
		GrantType:             "device-code",
		authenticator:         mock,
		authenticationTimeout: 5 * time.Minute,
		IOStreams:             streams,
	}

	alreadyValid, idToken, refreshToken, _, err := o.getToken(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alreadyValid {
		t.Error("expected alreadyValid=false for nil cache")
	}
	if idToken != "device-token" {
		t.Errorf("expected %q, got %q", "device-token", idToken)
	}
	if refreshToken != "device-refresh" {
		t.Errorf("expected %q, got %q", "device-refresh", refreshToken)
	}
}

func TestGetTokenFallsBackToDeviceCodeAfterRefreshFailure(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour)
	streams := genericiooptions.NewTestIOStreamsDiscard()

	mock := &mockAuthenticator{
		deviceCodeToken: "new-device-token",
		refreshToken:    "new-refresh",
		expiry:          expiry,
		refreshErr:      fmt.Errorf("refresh token expired"),
	}
	o := &GetTokenOptions{
		GrantType:             "device-code",
		authenticator:         mock,
		authenticationTimeout: 5 * time.Minute,
		IOStreams:             streams,
	}

	cache := &tokencache.Set{
		RefreshToken: "expired-refresh-token",
	}

	alreadyValid, idToken, _, _, err := o.getToken(context.Background(), cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alreadyValid {
		t.Error("expected alreadyValid=false")
	}
	if idToken != "new-device-token" {
		t.Errorf("expected %q, got %q", "new-device-token", idToken)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
