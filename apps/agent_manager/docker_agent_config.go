package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	brokerprotocol "github.com/original-david-knight/go_wild/tools/broker"
)

func buildAgentCmd(agent *data.Agent) []string {
	// Note: The Dockerfile sets ENTRYPOINT to /usr/local/bin/agent
	// so we only need to provide arguments here, not the binary path.
	cmd := []string{
		"-agent", agent.ID,
	}

	if provider := strings.TrimSpace(agent.ModelProvider); provider != "" {
		provider = data.NormalizeLLMProvider(provider)
		cmd = append(cmd, "-provider", provider)
		if provider == data.LLMProviderOpenAI {
			cmd = append(cmd, "-openai-auth", agent.EffectiveOpenAIAuthMode())
		}
	}

	// Pass both base and smart models so the agent can toggle between them
	if agent.Model != "" {
		cmd = append(cmd, "-model", agent.Model)
	}
	if agent.SmartModel != "" {
		cmd = append(cmd, "-smart-model", agent.SmartModel)
	}
	if agent.SmartDefault {
		cmd = append(cmd, "-smart")
	}

	if agent.MaxTurns > 0 {
		cmd = append(cmd, "-max-turns", strconv.Itoa(agent.MaxTurns))
	}

	if agent.Heartbeat != "" {
		cmd = append(cmd, "-heartbeat", normalizeDuration(agent.Heartbeat))
	}

	if agent.WorkTasksTimeout != "" {
		cmd = append(cmd, "-worktasks-timeout", normalizeDuration(agent.WorkTasksTimeout))
	}

	_, _ = resolveAgentRuntimeConfig(agent.Mode, agent.WorkerContextMode, agent.ExtraFlags)

	// Parse extra flags
	if agent.ExtraFlags != "" {
		_, _, extraFlags := parseRuntimeFlags(agent.ExtraFlags)
		cmd = append(cmd, extraFlags...)
	}

	return cmd
}

func buildContainerEnv(envVars map[string]string, agentID string, brokerSecret []byte, agentEthPrivateKey ...string) []string {
	brokerEnabled := len(brokerSecret) > 0
	env := make([]string, 0, len(envVars)+5)
	authPrivateKey := ""
	if len(agentEthPrivateKey) > 0 {
		authPrivateKey = strings.TrimSpace(agentEthPrivateKey[0])
	}

	// Keys to filter from container env when broker is available.
	// Secrets are proxied through the broker; database access is denied entirely.
	// Force socket transport and Ethereum challenge auth by removing caller overrides.
	filteredKeys := map[string]bool{
		"GEMINI_API_KEY":                     true,
		"OPENAI_API_KEY":                     true,
		"OPENAI_BASE_URL":                    true,
		"OPENAI_ORG_ID":                      true,
		"OPENAI_PROJECT_ID":                  true,
		"ANTHROPIC_API_KEY":                  true,
		"ANTHROPIC_AUTH_TOKEN":               true,
		"ANTHROPIC_BASE_URL":                 true,
		"GOWILD_DATABASE_URL":                true,
		"BROKER_URL":                         true,
		"BROKER_TOKEN":                       true,
		"BROKER_SOCKET_PATH":                 true,
		brokerprotocol.AgentEthPrivateKeyEnv: true,
		brokerprotocol.BrokerOnlyEnv:         true,
	}

	for k, v := range envVars {
		if brokerEnabled && filteredKeys[k] {
			continue
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	env = append(env, "GOWILD_AGENT_ID="+agentID)
	if brokerEnabled {
		env = append(env, "BROKER_SOCKET_PATH="+brokerSocketPath())
		env = append(env, brokerprotocol.BrokerOnlyEnv+"=1")
		if authPrivateKey != "" {
			env = append(env, brokerprotocol.AgentEthPrivateKeyEnv+"="+authPrivateKey)
		}
	}
	return env
}

// buildContainerCreateConfig builds a ContainerCreateConfig from agent-specific
// configuration and broker auth. This is the bridge between the agent data model
// and the pure Docker orchestration layer in dockermgr.
func buildContainerCreateConfig(agent *data.Agent, brokerSecret []byte) dockermgr.ContainerCreateConfig {
	cmd := buildAgentCmd(agent)
	env := buildContainerEnv(agent.EnvVars(), agent.ID, brokerSecret, deriveAgentAuthPrivateKey(agent))

	labels := map[string]string{
		"gowild.agent-id": agent.ID,
	}
	if agent.ExtraFlags != "" {
		labels["gowild.extra-flags"] = agent.ExtraFlags
	}

	var extraMounts []mount.Mount
	if len(brokerSecret) > 0 {
		socketPath := brokerSocketPath()
		socketDir := filepath.Dir(socketPath)
		extraMounts = append(extraMounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   socketDir,
			Target:   socketDir,
			ReadOnly: false,
		})
	}

	return dockermgr.ContainerCreateConfig{
		AgentID:     agent.ID,
		Cmd:         cmd,
		Env:         env,
		Labels:      labels,
		MemoryBytes: dockermgr.ParseMemoryBytes(agent.MemoryLimit),
		NanoCPUs:    dockermgr.ParseCPUs(agent.CPULimit),
		ExtraMounts: extraMounts,
	}
}

func deriveAgentAuthPrivateKey(agent *data.Agent) string {
	if agent == nil || strings.TrimSpace(agent.WalletSeedPhrase) == "" {
		return ""
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(agent.WalletSeedPhrase, agentAuthDerivationIndex)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(derived.EthPrivateKey)
}
