package gettoken

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/int128/oauth2dev"
	"golang.org/x/oauth2"

	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/openshift/oc/pkg/cli/gettoken/oidc"
	"github.com/openshift/oc/pkg/cli/gettoken/tokencache"
)

type mockAuthenticator struct {
	authCodeToken      string
	deviceCodeToken    string
	refreshToken       string
	expiry             time.Time
	deviceAuthResponse *oauth2dev.AuthorizationResponse
	authCodeErr        error
	deviceAuthErr      error
	deviceCodeErr      error
	refreshErr         error
	verifyErr          error
}

var _ oidc.Authenticator = &mockAuthenticator{}

func (m *mockAuthenticator) GetTokenByAuthCode(ctx context.Context, callbackAddress string, localServerReadyChan chan<- string) (string, string, time.Time, error) {
	localServerReadyChan <- "http://127.0.0.1:0/callback"
	if m.authCodeErr != nil {
		return "", "", time.Time{}, m.authCodeErr
	}
	return m.authCodeToken, m.refreshToken, m.expiry, nil
}

func (m *mockAuthenticator) GetDeviceAuthorization(ctx context.Context) (*oauth2dev.AuthorizationResponse, error) {
	if m.deviceAuthErr != nil {
		return nil, m.deviceAuthErr
	}
	if m.deviceAuthResponse != nil {
		return m.deviceAuthResponse, nil
	}
	return &oauth2dev.AuthorizationResponse{
		DeviceCode:      "device-code-1",
		UserCode:        "ABCD-1234",
		VerificationURI: "https://example.com/device",
	}, nil
}

func (m *mockAuthenticator) ExchangeDeviceCode(ctx context.Context, authResponse *oauth2dev.AuthorizationResponse) (string, string, time.Time, error) {
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
			name:      "authcode is valid",
			grantType: "authcode",
		},
		{
			name:      "device-code is valid",
			grantType: "device-code",
		},
		{
			name:          "unsupported grant-type is invalid",
			grantType:     "implicit",
			expectedError: `unsupported --grant-type "implicit", supported values are "authcode" and "device-code"`,
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
		{name: "empty defaults to authcode", grant: "", expected: "authcode"},
		{name: "authcode", grant: "authcode", expected: "authcode"},
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

func TestDoDeviceCode(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour)
	errTest := errors.New("test error")

	testCases := []struct {
		name                 string
		authenticator        *mockAuthenticator
		autoOpenBrowser      bool
		expectedIDToken      string
		expectedRefreshToken string
		expectedOutput       string
		expectedErr          error
	}{
		{
			name: "server returns verification_uri_complete",
			authenticator: &mockAuthenticator{
				deviceAuthResponse: &oauth2dev.AuthorizationResponse{
					DeviceCode:              "device-code-1",
					UserCode:                "code123",
					VerificationURI:         "https://example.com/verification",
					VerificationURIComplete: "https://example.com/verificationComplete?code=code123",
				},
				deviceCodeToken: "test-id-token",
				refreshToken:    "test-refresh-token",
				expiry:          expiry,
			},
			expectedIDToken:      "test-id-token",
			expectedRefreshToken: "test-refresh-token",
			expectedOutput: "Please enter the following code when asked in your browser: code123\n" +
				"Please visit the following URL in your browser: https://example.com/verificationComplete?code=code123\n",
		},
		{
			name: "server returns verification_uri",
			authenticator: &mockAuthenticator{
				deviceAuthResponse: &oauth2dev.AuthorizationResponse{
					DeviceCode:      "device-code-1",
					UserCode:        "code123",
					VerificationURI: "https://example.com/verification",
				},
				deviceCodeToken: "test-id-token",
				refreshToken:    "test-refresh-token",
				expiry:          expiry,
			},
			expectedIDToken:      "test-id-token",
			expectedRefreshToken: "test-refresh-token",
			expectedOutput: "Please enter the following code when asked in your browser: code123\n" +
				"Please visit the following URL in your browser: https://example.com/verification\n",
		},
		{
			name: "server returns verification_url",
			authenticator: &mockAuthenticator{
				deviceAuthResponse: &oauth2dev.AuthorizationResponse{
					DeviceCode:      "device-code-1",
					UserCode:        "code123",
					VerificationURL: "https://example.com/verificationURL",
				},
				deviceCodeToken: "test-id-token",
				refreshToken:    "test-refresh-token",
				expiry:          expiry,
			},
			expectedIDToken:      "test-id-token",
			expectedRefreshToken: "test-refresh-token",
			expectedOutput: "Please enter the following code when asked in your browser: code123\n" +
				"Please visit the following URL in your browser: https://example.com/verificationURL\n",
		},
		{
			name: "browser is not opened for a non-HTTP(S) verification URI",
			authenticator: &mockAuthenticator{
				deviceAuthResponse: &oauth2dev.AuthorizationResponse{
					DeviceCode:              "device-code-1",
					UserCode:                "code123",
					VerificationURIComplete: "file:///etc/passwd",
				},
				deviceCodeToken: "test-id-token",
				refreshToken:    "test-refresh-token",
				expiry:          expiry,
			},
			autoOpenBrowser:      true,
			expectedIDToken:      "test-id-token",
			expectedRefreshToken: "test-refresh-token",
			expectedOutput: "Please enter the following code when asked in your browser: code123\n" +
				"error: refusing to open a non-HTTP(S) URL in the browser\n\n" +
				"Please visit the following URL in your browser manually: file:///etc/passwd\n",
		},
		{
			name: "server returns no verification URI",
			authenticator: &mockAuthenticator{
				deviceAuthResponse: &oauth2dev.AuthorizationResponse{
					DeviceCode: "device-code-1",
					UserCode:   "code123",
				},
			},
			expectedErr: errors.New("no verification URI in the authorization response"),
		},
		{
			name: "authorization error",
			authenticator: &mockAuthenticator{
				deviceAuthErr: errTest,
			},
			expectedErr: fmt.Errorf("authorization error: %w", errTest),
		},
		{
			name: "error when exchanging the device code",
			authenticator: &mockAuthenticator{
				deviceCodeErr: errTest,
			},
			expectedOutput: "Please enter the following code when asked in your browser: ABCD-1234\n" +
				"Please visit the following URL in your browser: https://example.com/device\n",
			expectedErr: fmt.Errorf("unable to exchange device code: %w", errTest),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			streams, _, _, errOut := genericiooptions.NewTestIOStreams()
			o := &GetTokenOptions{
				GrantType:             "device-code",
				AutoOpenBrowser:       tc.autoOpenBrowser,
				authenticator:         tc.authenticator,
				authenticationTimeout: 5 * time.Minute,
				IOStreams:             streams,
			}

			idToken, refreshToken, gotExpiry, err := o.doDeviceCode(context.Background())
			if tc.expectedErr != nil {
				if err == nil || err.Error() != tc.expectedErr.Error() {
					t.Fatalf("expected error %q, got: %v", tc.expectedErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.expectedIDToken, idToken); diff != "" {
				t.Errorf("unexpected id token (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.expectedRefreshToken, refreshToken); diff != "" {
				t.Errorf("unexpected refresh token (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.authenticator.expiry, gotExpiry); diff != "" {
				t.Errorf("unexpected expiry (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.expectedOutput, errOut.String()); diff != "" {
				t.Errorf("unexpected output (-want +got):\n%s", diff)
			}
		})
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
