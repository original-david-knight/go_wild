package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

const legacyCompanyMethodToolNamePrefix = "company_method_"

type companyMethodToolSpec struct {
	ToolName         string
	Method           string
	Description      string
	ProviderAgentIDs []string
	InputSchema      any
	OutputSchema     any
	providerMethods  map[string]string
}

func (s companyMethodToolSpec) asMap() map[string]any {
	legacyAliases := []string{}
	legacy := strings.TrimSpace(legacyCompanyMethodToolName(s.Method))
	if legacy != "" && legacy != strings.TrimSpace(s.ToolName) {
		legacyAliases = append(legacyAliases, legacy)
	}

	item := map[string]any{
		"tool_name":            s.ToolName,
		"method":               s.Method,
		"description":          s.Description,
		"provider_agent_ids":   append([]string(nil), s.ProviderAgentIDs...),
		"provider_agent_count": len(s.ProviderAgentIDs),
		"legacy_tool_names":    legacyAliases,
	}
	if s.InputSchema != nil {
		item["input_schema"] = s.InputSchema
	}
	if s.OutputSchema != nil {
		item["output_schema"] = s.OutputSchema
	}
	return item
}

func companyMethodToolName(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return "method"
	}
	return method
}

func legacyCompanyMethodToolName(method string) string {
	method = strings.TrimSpace(method)
	slug := strings.ToLower(method)
	slug = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, slug)
	for strings.Contains(slug, "__") {
		slug = strings.ReplaceAll(slug, "__", "_")
	}
	slug = strings.Trim(slug, "_")
	if slug == "" {
		slug = "method"
	}
	if len(slug) > 28 {
		slug = slug[:28]
	}

	sum := sha1.Sum([]byte(method))
	digest := hex.EncodeToString(sum[:])[:10]
	return legacyCompanyMethodToolNamePrefix + slug + "_" + digest
}

func normalizeLegacyCompanyMethod(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return ""
	}
	if !strings.HasPrefix(method, legacyCompanyMethodToolNamePrefix) {
		return method
	}
	trimmed := strings.TrimPrefix(method, legacyCompanyMethodToolNamePrefix)
	lastUnderscore := strings.LastIndex(trimmed, "_")
	if lastUnderscore <= 0 || lastUnderscore+1 >= len(trimmed) {
		return method
	}
	slug := strings.TrimSpace(trimmed[:lastUnderscore])
	digest := strings.TrimSpace(trimmed[lastUnderscore+1:])
	if slug == "" || len(digest) != 10 || !isHexLower(digest) {
		return method
	}
	if legacyCompanyMethodToolName(slug) != method {
		return method
	}
	return slug
}

func isHexLower(value string) bool {
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func (s companyMethodToolSpec) targetMethodForProvider(providerAgentID string) string {
	providerAgentID = strings.TrimSpace(providerAgentID)
	if providerAgentID != "" && s.providerMethods != nil {
		if method := strings.TrimSpace(s.providerMethods[providerAgentID]); method != "" {
			return method
		}
	}
	return strings.TrimSpace(s.Method)
}

func listCompanyMethodTools(ctx context.Context, db gowild_data.Database, agentID string) ([]companyMethodToolSpec, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return []companyMethodToolSpec{}, nil
	}

	peerMemberSet, err := companyPeerMemberSet(ctx, db, agentID)
	if err != nil {
		return nil, err
	}
	if len(peerMemberSet) == 0 {
		return []companyMethodToolSpec{}, nil
	}

	methodProviders, err := collectCompanyMethodProviders(ctx, db, peerMemberSet)
	if err != nil {
		return nil, err
	}
	if len(methodProviders) == 0 {
		return []companyMethodToolSpec{}, nil
	}

	methodByName, err := listA2AMethodDefinitionsByName(ctx, db)
	if err != nil {
		return nil, err
	}
	return buildCompanyMethodToolSpecs(methodProviders, methodByName), nil
}

func companyPeerMemberSet(ctx context.Context, db gowild_data.Database, agentID string) (map[string]struct{}, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, db, agentID)
	if err != nil {
		return nil, err
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return map[string]struct{}{}, nil
	}
	members, err := data.ListCompanyMembers(ctx, db, strings.TrimSpace(member.CompanyID))
	if err != nil {
		return nil, err
	}
	peerSet := make(map[string]struct{}, len(members))
	for _, m := range members {
		id := strings.TrimSpace(m.AgentID)
		if id == "" || id == agentID {
			continue
		}
		peerSet[id] = struct{}{}
	}
	return peerSet, nil
}

func collectCompanyMethodProviders(ctx context.Context, db gowild_data.Database, peerMemberSet map[string]struct{}) (map[string]map[string]string, error) {
	rows, err := db.Table(data.AgentCapability{}).GetAll(ctx)
	if err != nil {
		return nil, err
	}
	methodProviders := make(map[string]map[string]string)
	for _, row := range rows {
		cap := row.(*data.AgentCapability)
		capMethod := strings.TrimSpace(cap.Method)
		capAgentID := strings.TrimSpace(cap.AgentID)
		if capMethod == "" {
			continue
		}
		if _, ok := peerMemberSet[capAgentID]; !ok {
			continue
		}
		displayMethod := normalizeLegacyCompanyMethod(capMethod)
		providers, ok := methodProviders[displayMethod]
		if !ok {
			providers = make(map[string]string)
			methodProviders[displayMethod] = providers
		}
		current := strings.TrimSpace(providers[capAgentID])
		if preferProviderMethod(current, capMethod, displayMethod) {
			providers[capAgentID] = capMethod
		}
	}
	return methodProviders, nil
}

func preferProviderMethod(current, candidate, displayMethod string) bool {
	current = strings.TrimSpace(current)
	candidate = strings.TrimSpace(candidate)
	if current == "" {
		return true
	}
	// Prefer canonical method names over legacy aliases when both are present.
	return current != displayMethod && candidate == displayMethod
}

func listA2AMethodDefinitionsByName(ctx context.Context, db gowild_data.Database) (map[string]data.A2AMethod, error) {
	systemSvc := data.NewAgentService(db, "system")
	methodDefs, err := systemSvc.ListA2AMethods(ctx)
	if err != nil {
		return nil, err
	}
	methodByName := make(map[string]data.A2AMethod, len(methodDefs))
	for _, m := range methodDefs {
		methodByName[strings.TrimSpace(m.Method)] = m
	}
	return methodByName, nil
}

func buildCompanyMethodToolSpecs(methodProviders map[string]map[string]string, methodByName map[string]data.A2AMethod) []companyMethodToolSpec {
	out := make([]companyMethodToolSpec, 0, len(methodProviders))
	for displayMethod, providers := range methodProviders {
		out = append(out, buildCompanyMethodToolSpec(displayMethod, providers, methodByName))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Method < out[j].Method
	})
	return out
}

func buildCompanyMethodToolSpec(displayMethod string, providers map[string]string, methodByName map[string]data.A2AMethod) companyMethodToolSpec {
	providerAgentIDs, providerMethods := buildProviderMethods(displayMethod, providers, methodByName)
	spec := companyMethodToolSpec{
		ToolName:         companyMethodToolName(displayMethod),
		Method:           displayMethod,
		ProviderAgentIDs: providerAgentIDs,
		providerMethods:  providerMethods,
	}
	if def, ok := resolveCompanyMethodDefinition(displayMethod, providerAgentIDs, providerMethods, methodByName); ok {
		applyCompanyMethodDefinition(&spec, def)
	}
	return spec
}

func buildProviderMethods(displayMethod string, providers map[string]string, methodByName map[string]data.A2AMethod) ([]string, map[string]string) {
	providerAgentIDs := make([]string, 0, len(providers))
	providerMethods := make(map[string]string, len(providers))
	for providerID, rawMethod := range providers {
		id := strings.TrimSpace(providerID)
		if id == "" {
			continue
		}
		providerAgentIDs = append(providerAgentIDs, id)
		providerMethods[id] = resolveProviderTargetMethod(displayMethod, rawMethod, methodByName)
	}
	sort.Strings(providerAgentIDs)
	return providerAgentIDs, providerMethods
}

func resolveProviderTargetMethod(displayMethod, rawMethod string, methodByName map[string]data.A2AMethod) string {
	displayMethod = strings.TrimSpace(displayMethod)
	if _, ok := methodByName[displayMethod]; ok {
		return displayMethod
	}
	targetMethod := strings.TrimSpace(rawMethod)
	if targetMethod == "" {
		targetMethod = displayMethod
	}
	return targetMethod
}

func resolveCompanyMethodDefinition(displayMethod string, providerAgentIDs []string, providerMethods map[string]string, methodByName map[string]data.A2AMethod) (data.A2AMethod, bool) {
	if def, ok := methodByName[strings.TrimSpace(displayMethod)]; ok {
		return def, true
	}
	for _, providerID := range providerAgentIDs {
		if methodDef, ok := methodByName[strings.TrimSpace(providerMethods[providerID])]; ok {
			return methodDef, true
		}
	}
	return data.A2AMethod{}, false
}

func applyCompanyMethodDefinition(spec *companyMethodToolSpec, def data.A2AMethod) {
	if spec == nil {
		return
	}
	spec.Description = strings.TrimSpace(def.Description)
	if schema, err := parseCapabilitySchema(def.InputSchemaJSON); err == nil && schema != nil {
		spec.InputSchema = schema
	}
	if schema, err := parseCapabilitySchema(def.OutputSchemaJSON); err == nil && schema != nil {
		spec.OutputSchema = schema
	}
}

func findCompanyMethodToolSpec(specs []companyMethodToolSpec, toolName string) (companyMethodToolSpec, bool) {
	toolName = strings.TrimSpace(toolName)
	for _, spec := range specs {
		if strings.TrimSpace(spec.ToolName) == toolName {
			return spec, true
		}
		if strings.TrimSpace(legacyCompanyMethodToolName(spec.Method)) == toolName {
			return spec, true
		}
	}
	return companyMethodToolSpec{}, false
}

func companyMethodToolSpecForAgent(ctx context.Context, db gowild_data.Database, agentID, toolName string) (companyMethodToolSpec, bool, error) {
	specs, err := listCompanyMethodTools(ctx, db, agentID)
	if err != nil {
		return companyMethodToolSpec{}, false, err
	}
	spec, ok := findCompanyMethodToolSpec(specs, toolName)
	return spec, ok, nil
}
