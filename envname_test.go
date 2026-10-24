// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// pastedSecrets are values an operator might paste into token_env, email_env,
// jql_env, or provider instead of an environment variable name. The first is
// shaped like a valid variable name, so a name check alone cannot catch it;
// the others fail the name check by character set or length.
var pastedSecrets = []string{
	"ghp_SUPERSECRETTOKEN123abcdef",
	"ATATT3xFfGF0-secret=value/with+symbols",
	"bot@example.invalid",
	strings.Repeat("A", maxEnvNameLength+1),
	"has space token",
}

func TestEnvVarNamesAreValidatedBeforeUse(t *testing.T) {
	t.Parallel()

	for name, valid := range map[string]bool{
		"JIRA_API_TOKEN": true,
		"_x1":            true,
		// Literal lengths, not maxEnvNameLength, so raising the cap fails here.
		strings.Repeat("A", 128): true,
		strings.Repeat("A", 129): false,
		"":                       false,
		"1LEADING_DIGIT":         false,
		"HAS-DASH":               false,
		"HAS=EQUALS":             false,
		"HAS SPACE":              false,
		"line\nbreak":            false,
	} {
		if got := validEnvName(name); got != valid {
			t.Fatalf("validEnvName(%q) = %t, want %t", name, got, valid)
		}
	}
}

func TestParseSettingsNeverEchoesAPastedSecret(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"token_env", "email_env", "jql_env", "provider"} {
		for _, secret := range pastedSecrets {
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				config := validConfig()
				if key == "jql_env" {
					delete(config, "jql")
				}
				config[key] = secret
				var looked []string
				getenv := func(name string) string {
					looked = append(looked, name)
					return testEnv()(name)
				}
				_, err := ParseSettings(config, "scope", getenv)
				if err == nil {
					// An unresolved email_env named like a valid variable is
					// accepted (the email is optional); nothing is echoed.
					if key == "email_env" && validEnvName(strings.TrimSpace(secret)) {
						return
					}
					t.Fatal("ParseSettings() error = nil, want rejection")
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error text echoes the pasted value: %v", err)
				}
				if !validEnvName(strings.TrimSpace(secret)) {
					for _, name := range looked {
						if name == secret {
							t.Fatalf("an invalid name reached the environment lookup: %q", name)
						}
					}
					if key != "provider" && !strings.Contains(err.Error(), "is not a valid environment variable name") {
						t.Fatalf("error = %v, want the fixed invalid-name message", err)
					}
				}
			})
		}
	}
}

func TestHandleNeverEchoesAPastedSecretToResultOrLog(t *testing.T) {
	t.Parallel()

	for _, secret := range pastedSecrets {
		t.Run("token_env", func(t *testing.T) {
			t.Parallel()
			result, transport, logs := runScenario(t, "basic", func(_ *replayScenario, r *Request) {
				r.Config["token_env"] = secret
			})
			if result.State != sdk.ResultTerminal || len(result.Facts) != 0 ||
				result.Statuses[0].FailureClass != FailureInvalidConfig {
				t.Fatalf("result = %+v, want terminal %s", result, FailureInvalidConfig)
			}
			if len(transport.sortedCalls()) != 0 {
				t.Fatalf("a rejected config must not reach Jira: %v", transport.sortedCalls())
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{string(encoded), logs} {
				if strings.Contains(text, secret) {
					t.Fatalf("output echoes the pasted value: %s", text)
				}
			}
		})
	}
}

// tokenShapedKey is a config key an operator could paste a credential into by
// mistake. An unknown key is rejected, and the rejection must not quote it.
const tokenShapedKey = "ghp_SUPERSECRETKEYNAME123abcdef"

func TestUnknownConfigKeyIsNeverEchoed(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(map[string]any){
		"top level": func(c map[string]any) { c[tokenShapedKey] = 1 },
		"in limits": func(c map[string]any) { c["limits"] = map[string]any{tokenShapedKey: 1} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := validConfig()
			mutate(config)
			_, err := ParseSettings(config, "scope", testEnv())
			if err == nil {
				t.Fatal("ParseSettings() error = nil, want rejection")
			}
			if strings.Contains(err.Error(), tokenShapedKey) {
				t.Fatalf("error text echoes the unknown key: %v", err)
			}
			if !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error = %v, want it to say the field is unknown", err)
			}

			result, _, logs := runScenario(t, "basic", func(_ *replayScenario, r *Request) {
				mutate(r.Config)
			})
			encoded, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			for _, text := range []string{string(encoded), logs} {
				if strings.Contains(text, tokenShapedKey) {
					t.Fatalf("output echoes the unknown key: %s", text)
				}
			}
		})
	}
}

func TestParseRequestNeverEchoesAnUnknownField(t *testing.T) {
	t.Parallel()

	input := requestJSON(t, func(d map[string]any) { d[tokenShapedKey] = true })
	_, err := ParseRequest(strings.NewReader(input))
	if err == nil {
		t.Fatal("ParseRequest() error = nil, want rejection")
	}
	if strings.Contains(err.Error(), tokenShapedKey) {
		t.Fatalf("error text echoes the unknown field: %v", err)
	}
}
