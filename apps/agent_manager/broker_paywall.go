package main

import (
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// BrokerPaywallHandler handles crypto paywall operations.
// Files are uploaded to agent_net (the public server) rather than stored locally.
type BrokerPaywallHandler struct {
	db     gowild_data.Database
	docker *dockermgr.DockerManager
}

// NewBrokerPaywallHandler creates a new paywall broker handler.
func NewBrokerPaywallHandler(db gowild_data.Database, docker *dockermgr.DockerManager) *BrokerPaywallHandler {
	return &BrokerPaywallHandler{
		db:     db,
		docker: docker,
	}
}
