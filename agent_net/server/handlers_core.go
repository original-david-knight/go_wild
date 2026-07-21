package server

import "github.com/original-david-knight/go_wild/agent_net"

// Handlers contains HTTP handlers for the agent network API.
type Handlers struct {
	service  *gowild_agent_net.Service
	treasury gowild_agent_net.TreasuryAddresses
	wsHub    *WSHub
}

// NewHandlers creates new API handlers.
func NewHandlers(service *gowild_agent_net.Service, treasury gowild_agent_net.TreasuryAddresses, wsHub *WSHub) *Handlers {
	return &Handlers{
		service:  service,
		treasury: treasury,
		wsHub:    wsHub,
	}
}
