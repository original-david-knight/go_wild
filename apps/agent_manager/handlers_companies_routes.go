package main

import (
	"net/http"
	"strings"
)

type companyCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request)
type companyHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string)
type companyMemberHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID, agentID string)
type companyKnowledgeEntryHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID, entryID string)

var companyCollectionHandlers = map[string]companyCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.listCompanies(w, r)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request) {
		h.createCompany(w, r)
	},
}

var companyHandlers = map[string]companyHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.getCompany(w, r, companyID)
	},
	http.MethodPatch: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.updateCompany(w, r, companyID)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.deleteCompany(w, r, companyID)
	},
}

var companyMembersCollectionHandlers = map[string]companyHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.listCompanyMembers(w, r, companyID)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.addCompanyMember(w, r, companyID)
	},
}

var companyMemberHandlers = map[string]companyMemberHandlerFunc{
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID, agentID string) {
		h.removeCompanyMember(w, r, companyID, agentID)
	},
	http.MethodPatch: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID, agentID string) {
		h.updateCompanyMember(w, r, companyID, agentID)
	},
}

var companyWebhookHandlers = map[string]companyHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.getCompanyWebhooks(w, r, companyID)
	},
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.putCompanyWebhook(w, r, companyID)
	},
}

var companyKnowledgeCollectionHandlers = map[string]companyHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.listCompanyKnowledge(w, r, companyID)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID string) {
		h.addCompanyKnowledge(w, r, companyID)
	},
}

var companyKnowledgeEntryHandlers = map[string]companyKnowledgeEntryHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID, entryID string) {
		h.getCompanyKnowledge(w, r, companyID, entryID)
	},
	http.MethodPatch: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID, entryID string) {
		h.updateCompanyKnowledge(w, r, companyID, entryID)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, companyID, entryID string) {
		h.deleteCompanyKnowledge(w, r, companyID, entryID)
	},
}

func isCompanyCollectionMethod(method string) bool {
	_, ok := companyCollectionHandlers[method]
	return ok
}

func isCompanyMethod(method string) bool {
	_, ok := companyHandlers[method]
	return ok
}

func isCompanyMembersCollectionMethod(method string) bool {
	_, ok := companyMembersCollectionHandlers[method]
	return ok
}

func isCompanyMemberMethod(method string) bool {
	_, ok := companyMemberHandlers[method]
	return ok
}

func isCompanyWebhookMethod(method string) bool {
	_, ok := companyWebhookHandlers[method]
	return ok
}

func isCompanyKnowledgeCollectionMethod(method string) bool {
	_, ok := companyKnowledgeCollectionHandlers[method]
	return ok
}

func isCompanyKnowledgeEntryMethod(method string) bool {
	_, ok := companyKnowledgeEntryHandlers[method]
	return ok
}

// handleCompanies handles GET /api/companies and POST /api/companies.
func (h *Handlers) handleCompanies(w http.ResponseWriter, r *http.Request) {
	if !isCompanyCollectionMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := companyCollectionHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r)
}

// handleCompany routes /api/companies/{id} and sub-paths.
func (h *Handlers) handleCompany(w http.ResponseWriter, r *http.Request) {
	route, err := parseCompanyRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	companyID := route.companyID

	if len(route.parts) == 1 {
		if !isCompanyMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := companyHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r, companyID)
		return
	}

	action := route.action
	if !isCompanyAction(action) {
		writeError(w, http.StatusNotFound, "unknown company action")
		return
	}
	handler, ok := companyActionHandlers[action]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown company action")
		return
	}
	if handler(h, w, r, route.parts, companyID) {
		return
	}

	writeError(w, http.StatusNotFound, "unknown company action")
}

type companyActionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool

var companyActionHandlers = map[string]companyActionHandlerFunc{
	"members":          handleCompanyMembersAction,
	"ceo":              handleCompanyCEOAction,
	"webhooks":         handleCompanyWebhooksAction,
	"public-endpoints": handleCompanyPublicEndpointsAction,
	"shopify":          handleCompanyShopifyAction,
	"polymarket":       handleCompanyPolymarketAction,
	"topdawg":          handleCompanyTopDawgAction,
	"cjdropshipping":   handleCompanyCJDropshippingAction,
	"amazon":           handleCompanyAmazonAction,
	"missions":         handleCompanyMissionsAction,
	"knowledge":        handleCompanyKnowledgeAction,
}

func isCompanyAction(action string) bool {
	_, ok := companyActionHandlers[action]
	return ok
}

func handleCompanyMembersAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	if len(parts) == 2 {
		if !isCompanyMembersCollectionMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler, ok := companyMembersCollectionHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler(h, w, r, companyID)
		return true
	}
	agentID := strings.TrimSpace(parts[2])
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return true
	}
	if !isCompanyMemberMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	handler, ok := companyMemberHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	handler(h, w, r, companyID, agentID)
	return true
}

func handleCompanyCEOAction(h *Handlers, w http.ResponseWriter, r *http.Request, _ []string, companyID string) bool {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	h.setCompanyCEO(w, r, companyID)
	return true
}

func handleCompanyWebhooksAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	if len(parts) == 2 {
		if !isCompanyWebhookMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler, ok := companyWebhookHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler(h, w, r, companyID)
		return true
	}
	if len(parts) == 3 && parts[2] == "rotate-key" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		h.rotateCompanyWebhookKey(w, r, companyID)
		return true
	}
	return false
}

func handleCompanyPublicEndpointsAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		h.getCompanyPublicEndpoints(w, r, companyID)
		return true
	}
	return false
}

func handleCompanyShopifyAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	return h.handleCompanyConnectionRoute(w, r, parts, companyID, companyConnectionRouteHandlers{
		get:  h.getCompanyShopify,
		put:  h.putCompanyShopify,
		del:  h.deleteCompanyShopify,
		test: h.testCompanyShopify,
	})
}

func handleCompanyPolymarketAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	if h.handleCompanyConnectionRoute(w, r, parts, companyID, companyConnectionRouteHandlers{
		get: h.getCompanyPolymarket,
		put: h.putCompanyPolymarket,
		del: h.deleteCompanyPolymarket,
	}) {
		return true
	}

	if len(parts) == 3 {
		switch parts[2] {
		case "portfolio":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return true
			}
			h.getCompanyPolymarketPortfolio(w, r, companyID)
			return true
		case "notes":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return true
			}
			h.listCompanyPolymarketNotes(w, r, companyID)
			return true
		case "sell":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return true
			}
			h.sellCompanyPolymarketPosition(w, r, companyID)
			return true
		case "exit":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return true
			}
			h.exitCompanyPolymarketPosition(w, r, companyID)
			return true
		case "cancel":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return true
			}
			h.cancelCompanyPolymarketOrder(w, r, companyID)
			return true
		}
	}

	return false
}

func handleCompanyTopDawgAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	return h.handleCompanyConnectionRoute(w, r, parts, companyID, companyConnectionRouteHandlers{
		get:  h.getCompanyTopDawg,
		put:  h.putCompanyTopDawg,
		del:  h.deleteCompanyTopDawg,
		test: h.testCompanyTopDawg,
	})
}

func handleCompanyCJDropshippingAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	return h.handleCompanyConnectionRoute(w, r, parts, companyID, companyConnectionRouteHandlers{
		get:  h.getCompanyCJDropshipping,
		put:  h.putCompanyCJDropshipping,
		del:  h.deleteCompanyCJDropshipping,
		test: h.testCompanyCJDropshipping,
	})
}

func handleCompanyAmazonAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	return h.handleCompanyConnectionRoute(w, r, parts, companyID, companyConnectionRouteHandlers{
		get:  h.getCompanyAmazon,
		put:  h.putCompanyAmazon,
		del:  h.deleteCompanyAmazon,
		test: h.testCompanyAmazon,
	})
}

func handleCompanyMissionsAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	subPath := ""
	if len(parts) > 2 {
		subPath = strings.Join(parts[2:], "/")
	}
	h.handleMissions(w, r, companyID, subPath)
	return true
}

func handleCompanyKnowledgeAction(h *Handlers, w http.ResponseWriter, r *http.Request, parts []string, companyID string) bool {
	// /api/companies/{id}/knowledge
	if len(parts) == 2 {
		if !isCompanyKnowledgeCollectionMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler, ok := companyKnowledgeCollectionHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler(h, w, r, companyID)
		return true
	}
	// /api/companies/{id}/knowledge/{entry_id}
	if len(parts) == 3 {
		entryID := strings.TrimSpace(parts[2])
		if entryID == "" {
			writeError(w, http.StatusBadRequest, "entry_id is required")
			return true
		}
		if !isCompanyKnowledgeEntryMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler, ok := companyKnowledgeEntryHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler(h, w, r, companyID, entryID)
		return true
	}
	return false
}

type companyConnectionRouteHandlers struct {
	get  func(http.ResponseWriter, *http.Request, string)
	put  func(http.ResponseWriter, *http.Request, string)
	del  func(http.ResponseWriter, *http.Request, string)
	test func(http.ResponseWriter, *http.Request, string)
}

type companyConnectionMethodHandlerFunc func(companyConnectionRouteHandlers, http.ResponseWriter, *http.Request, string)

var companyConnectionMethodHandlers = map[string]companyConnectionMethodHandlerFunc{
	http.MethodGet: func(handlers companyConnectionRouteHandlers, w http.ResponseWriter, r *http.Request, companyID string) {
		handlers.get(w, r, companyID)
	},
	http.MethodPut: func(handlers companyConnectionRouteHandlers, w http.ResponseWriter, r *http.Request, companyID string) {
		handlers.put(w, r, companyID)
	},
	http.MethodDelete: func(handlers companyConnectionRouteHandlers, w http.ResponseWriter, r *http.Request, companyID string) {
		handlers.del(w, r, companyID)
	},
}

func isCompanyConnectionMethod(method string) bool {
	_, ok := companyConnectionMethodHandlers[method]
	return ok
}

func (h *Handlers) handleCompanyConnectionRoute(
	w http.ResponseWriter,
	r *http.Request,
	parts []string,
	companyID string,
	handlers companyConnectionRouteHandlers,
) bool {
	if len(parts) == 2 {
		if !isCompanyConnectionMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler, ok := companyConnectionMethodHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handler(handlers, w, r, companyID)
		return true
	}

	if len(parts) == 3 && parts[2] == "test" && handlers.test != nil {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return true
		}
		handlers.test(w, r, companyID)
		return true
	}

	return false
}
