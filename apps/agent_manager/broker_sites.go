package main

import (
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// BrokerSitesHandler handles static site publishing operations.
// Directories are copied from agent containers and uploaded to agent_net.
type BrokerSitesHandler struct {
	db     gowild_data.Database
	docker *dockermgr.DockerManager
}

// NewBrokerSitesHandler creates a new sites broker handler.
func NewBrokerSitesHandler(db gowild_data.Database, docker *dockermgr.DockerManager) *BrokerSitesHandler {
	return &BrokerSitesHandler{
		db:     db,
		docker: docker,
	}
}
