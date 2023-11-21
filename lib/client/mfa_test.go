// Copyright 2023 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/gravitational/teleport/api/client/proto"
	wanpb "github.com/gravitational/teleport/api/types/webauthn"
	"github.com/gravitational/teleport/api/utils/prompt"
	wancli "github.com/gravitational/teleport/lib/auth/webauthncli"
	wantypes "github.com/gravitational/teleport/lib/auth/webauthntypes"
	"github.com/gravitational/teleport/lib/client/mfa"
)

// TestPromptMFAChallenge_usingNonRegisteredDevice tests a specific MFA scenario
// where the user picks a non-registered security key.
// See api_login_test.go and/or TeleportClient tests for more general
// authentication tests.
func TestPromptMFAChallenge_usingNonRegisteredDevice(t *testing.T) {
	oldStdin := prompt.Stdin()
	t.Cleanup(func() {
		prompt.SetStdin(oldStdin)
	})

	const proxyAddr = "example.com"
	ctx := context.Background()

	// The Webauthn challenge below looks like a typical MFA challenge.
	challengeWebauthnOnly := &proto.MFAAuthenticateChallenge{
		WebauthnChallenge: &wanpb.CredentialAssertion{
			PublicKey: &wanpb.PublicKeyCredentialRequestOptions{
				Challenge: []byte{1, 2, 3, 4, 5}, // arbitrary
				RpId:      "example.com",
				AllowCredentials: []*wanpb.CredentialDescriptor{
					{
						Type: string(protocol.PublicKeyCredentialType),
						Id:   []byte{5, 5, 5, 5, 5}, // arbitrary
					},
				},
				UserVerification: string(protocol.VerificationDiscouraged),
			},
		},
	}

	challengeWebauthnOTP := &proto.MFAAuthenticateChallenge{
		TOTP:              &proto.TOTPChallenge{}, // non-nil enables OTP prompt
		WebauthnChallenge: challengeWebauthnOnly.WebauthnChallenge,
	}

	tests := []struct {
		name            string
		challenge       *proto.MFAAuthenticateChallenge
		customizePrompt func(p *mfa.PromptConfig)
	}{
		{
			name:      "webauthn only",
			challenge: challengeWebauthnOnly,
		},
		{
			name:      "webauthn and OTP",
			challenge: challengeWebauthnOTP,
			customizePrompt: func(p *mfa.PromptConfig) {
				p.AllowStdinHijack = true // required for OTP+WebAuthn prompt.
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test := test
			t.Parallel()

			// Set a timeout so the test won't block forever.
			// We don't expect to hit the timeout for any of the test cases.
			ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()

			// Prompt never has any input.
			prompt.SetStdin(prompt.NewFakeReader().AddReply(func(ctx context.Context) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			}))

			promptConfig := mfa.NewPromptConfig(proxyAddr)
			promptConfig.WebauthnSupported = true
			promptConfig.WebauthnLoginFunc = func(ctx context.Context, origin string, assertion *wantypes.CredentialAssertion, prompt wancli.LoginPrompt, opts *wancli.LoginOpts) (*proto.MFAAuthenticateResponse, string, error) {
				return nil, "", wancli.ErrUsingNonRegisteredDevice
			}

			if test.customizePrompt != nil {
				test.customizePrompt(promptConfig)
			}

			_, err := mfa.NewCLIPrompt(promptConfig, os.Stderr).Run(ctx, test.challenge)
			if !errors.Is(err, wancli.ErrUsingNonRegisteredDevice) {
				t.Errorf("PromptMFAChallenge returned err=%q, want %q", err, wancli.ErrUsingNonRegisteredDevice)
			}
		})
	}
}