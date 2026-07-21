package main

import "testing"

func TestBrokerSearchHandlerCredential_UsesEnvWhenUnset(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "env-key")

	h := &BrokerSearchHandler{}
	apiKey := h.credential()

	if apiKey != "env-key" {
		t.Fatalf("apiKey = %q, want env-key", apiKey)
	}
}

func TestBrokerSearchHandlerCredential_PrefersConfiguredValue(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "env-key")

	h := &BrokerSearchHandler{
		apiKey: "configured-key",
	}
	apiKey := h.credential()

	if apiKey != "configured-key" {
		t.Fatalf("apiKey = %q, want configured-key", apiKey)
	}
}
