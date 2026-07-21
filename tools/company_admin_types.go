package tools

// CompanyAdminGetContextInput returns the caller's resolved company context.
type CompanyAdminGetContextInput struct{}

// CompanyAdminUpdateCompanyInput updates company metadata.
type CompanyAdminUpdateCompanyInput struct {
	Name        *string `json:"name" description:"Updated company name."`
	Description *string `json:"description" description:"Updated company description."`
}

// CompanyAdminListMembersInput lists members for the caller's company.
type CompanyAdminListMembersInput struct{}

// CompanyAdminAddMemberInput adds an agent to the caller's company.
type CompanyAdminAddMemberInput struct {
	AgentID string `json:"agent_id" description:"Agent ID to add as a company member." required:"true"`
	Role    string `json:"role" description:"Optional member role label."`
}

// CompanyAdminRemoveMemberInput removes an agent from the caller's company.
type CompanyAdminRemoveMemberInput struct {
	AgentID string `json:"agent_id" description:"Agent ID to remove from the company." required:"true"`
}

// CompanyAdminSetCEOInput assigns the CEO for the caller's company.
type CompanyAdminSetCEOInput struct {
	AgentID string `json:"agent_id" description:"Agent ID that should become company CEO." required:"true"`
}

// CompanyAdminSendHeartbeatInput fans out a heartbeat message to company members.
type CompanyAdminSendHeartbeatInput struct {
	Message      string `json:"message" description:"Heartbeat message to send to targeted company members." required:"true"`
	IncludeCEO   *bool  `json:"include_ceo" description:"Include the CEO in fan-out target set. Defaults to true."`
	MemberFilter string `json:"member_filter" description:"Optional role filter; only members with matching role receive the heartbeat."`
}

// SendCompanyHeartbeatInput fans out heartbeat to members in a company.
// company_id is optional and defaults to the caller's company.
type SendCompanyHeartbeatInput struct {
	CompanyID    string `json:"company_id" description:"Optional target company ID. Defaults to caller company."`
	Message      string `json:"message" description:"Heartbeat message to send." required:"true"`
	IncludeCEO   *bool  `json:"include_ceo" description:"Include CEO in fan-out. Defaults to true."`
	MemberFilter string `json:"member_filter" description:"Optional role filter for targeted members."`
}
