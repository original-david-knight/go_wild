package broker

import (
	agentauth "github.com/original-david-knight/go_wild/agent_auth"
)

const (
	AuthChallengePath = "/broker/v1/auth/challenge"
	AuthVerifyPath    = "/broker/v1/auth/verify"

	AgentEthPrivateKeyEnv = "GOWILD_AGENT_ETH_PRIVATE_KEY"
	BrokerOnlyEnv         = "GOWILD_BROKER_ONLY"
)

type AuthChallengeRequest = agentauth.ChallengeRequest
type AuthChallengeResponse = agentauth.Challenge
type AuthVerifyRequest = agentauth.VerifyRequest
type AuthVerifyResponse = agentauth.VerifyResponse

func BuildSignInMessage(ch AuthChallengeResponse) (string, error) {
	return agentauth.BuildSignInMessage(ch)
}
