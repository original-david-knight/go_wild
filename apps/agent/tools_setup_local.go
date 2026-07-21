package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"google.golang.org/genai"
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
	"github.com/original-david-knight/go_wild/tools"
)

func addLocalWebSearchTools(agent *loop.AgenticLoop) {
	webTools := tools.NewWebTools("")
	if webTools == nil || !webTools.Available() {
		fmt.Println(color.HiBlackString("Tool: web_search (disabled - GEMINI_API_KEY required)"))
		return
	}
	agent.AddTools(loop.WrapToolsWithDescriptions(webTools)...)
	fmt.Println(color.HiBlackString("Tool: web_search"))
}

func addLocalTaskTools(agent *loop.AgenticLoop, service *data.AgentService) {
	taskTools := tools.NewTaskTools(service)
	if taskTools == nil {
		fmt.Println(color.HiBlackString("Tools: tasks (disabled - database unavailable)"))
		return
	}
	agent.AddTools(loop.WrapToolsWithDescriptions(taskTools)...)
	fmt.Println(color.HiBlackString("Tools: add_task, mark_task_done, mark_task_deprecated, list_tasks, move_task, block_task, unblock_task, plan_task, evaluate_task"))
}

func addLocalReportTools(agent *loop.AgenticLoop, service *data.AgentService) {
	if service == nil {
		fmt.Println(color.HiBlackString("Tools: report (disabled - database unavailable)"))
		return
	}
	reportTools := tools.NewReportTools(service)
	agent.AddTools(loop.WrapToolsWithDescriptions(reportTools)...)
	fmt.Println(color.HiBlackString("Tools: set_report_html, get_report_html"))
}

func addLocalSoulTools(agent *loop.AgenticLoop, service *data.AgentService) {
	if service == nil {
		fmt.Println(color.HiBlackString("Tools: soul (disabled - database unavailable)"))
		return
	}
	soulTools := tools.NewSoulTools(service)
	agent.AddTools(loop.WrapToolsWithDescriptions(soulTools)...)
	fmt.Println(color.HiBlackString("Tools: read_soul, update_soul"))
}

func addLocalSkillsTools(agent *loop.AgenticLoop, service *data.AgentService) {
	if service == nil {
		fmt.Println(color.HiBlackString("Tools: skills (disabled - database unavailable)"))
		return
	}
	pythonTools, err := tools.NewPythonTools()
	if err != nil {
		fmt.Println(color.HiBlackString("Tools: skills (disabled - no Python available)"))
		return
	}
	skillsTools := tools.NewSkillsTools(pythonTools, service)
	agent.AddTools(loop.WrapToolsWithDescriptions(pythonTools)...)
	agent.AddTools(loop.WrapToolsWithDescriptions(skillsTools)...)
	fmt.Println(color.HiBlackString("Tool: run_python"))
	fmt.Println(color.HiBlackString("Tools: save_skill, execute_skill, test_skill, list_skills, get_skill, delete_skill"))
}

func addLocalMessagingTools(ctx context.Context, agent *loop.AgenticLoop, service *data.AgentService) {
	if service == nil {
		fmt.Println(color.HiBlackString("Tools: messaging (disabled - database unavailable)"))
		return
	}
	peers, err := service.GetPeerAgents(ctx)
	if err != nil || len(peers) == 0 {
		fmt.Println(color.HiBlackString("Tools: messaging (disabled - no peers)"))
		return
	}
	messagingTools := tools.NewMessagingTools(service)
	agent.AddTools(loop.WrapToolsWithDescriptions(messagingTools)...)
	fmt.Println(color.HiBlackString("Tools: list_peers, send_message, read_messages, mark_messages_read"))
}

func addLocalTelegramTools(ctx context.Context, agent *loop.AgenticLoop, service *data.AgentService) {
	if globalTelegramTools != nil {
		globalTelegramTools.Stop()
		globalTelegramTools = nil
	}
	if service == nil {
		fmt.Println(color.HiBlackString("Tools: telegram (disabled - database unavailable)"))
		return
	}
	token, err := service.GetTelegramBotToken(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		fmt.Println(color.HiBlackString("Tools: telegram (disabled - bot token not configured)"))
		return
	}

	telegramTools := tools.NewTelegramTools(token)
	if err := telegramTools.Start(ctx); err != nil {
		fmt.Println(color.HiBlackString("Tools: telegram (disabled - %v)", err))
		return
	}
	globalTelegramTools = telegramTools
	agent.AddTools(loop.WrapToolsWithDescriptions(telegramTools)...)
	fmt.Println(color.HiBlackString("Tools: telegram_send, telegram_get_updates, telegram_get_chats, telegram_get_bot_info"))
}

func addLocalEmailTools(ctx context.Context, agent *loop.AgenticLoop, service *data.AgentService) {
	globalEmailTools = nil
	globalEmailOutbox = nil
	if service == nil {
		fmt.Println(color.HiBlackString("Tools: email (disabled - database unavailable)"))
		return
	}

	apiKey, err := service.GetAgentMailAPIKey(ctx)
	if err != nil || strings.TrimSpace(apiKey) == "" {
		fmt.Println(color.HiBlackString("Tools: email (disabled - API key not configured)"))
		return
	}
	inboxID, err := service.GetAgentMailInboxID(ctx)
	if err != nil || strings.TrimSpace(inboxID) == "" {
		fmt.Println(color.HiBlackString("Tools: email (disabled - inbox not configured)"))
		return
	}

	emailTools := tools.NewEmailTools(apiKey, inboxID)
	globalEmailTools = emailTools
	agent.AddTools(loop.WrapToolsWithDescriptions(emailTools)...)

	outbox := tools.NewEmailOutbox(emailTools, service)
	globalEmailOutbox = outbox
	agent.AddTools(loop.WrapToolsWithDescriptions(outbox)...)
	fmt.Println(color.HiBlackString("Tools: list_emails, read_email, send_email"))
}

func addLocalWalletTools(ctx context.Context, agent *loop.AgenticLoop, service *data.AgentService) {
	if service == nil {
		fmt.Println(color.HiBlackString("Tools: wallet (disabled - database unavailable)"))
		return
	}

	seedPhrase, err := service.GetWalletSeedPhrase(ctx)
	if err != nil || strings.TrimSpace(seedPhrase) == "" {
		fmt.Println(color.HiBlackString("Tools: wallet (disabled - seed phrase not configured)"))
		return
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		fmt.Println(color.HiBlackString("Tools: wallet (disabled - %v)", err))
		return
	}
	walletTools, err := tools.NewWalletToolsWithAgentService(derived.EthPrivateKey, derived.SolPrivateKey, service)
	if err != nil {
		fmt.Println(color.HiBlackString("Tools: wallet (disabled - %v)", err))
		return
	}

	defs := []loop.ToolDefinition{
		{
			Name:        "get_wallet_address",
			Description: walletTools.DescribeTool("get_wallet_address"),
			Method:      "GetWalletAddressTool",
		},
		{
			Name:        "sign_message",
			Description: walletTools.DescribeTool("sign_message"),
			Method:      "SignMessageTool",
		},
		{
			Name:        "send_token",
			Description: walletTools.DescribeTool("send_token"),
			Method:      "SendTokenTool",
		},
		{
			Name:        "swap_token",
			Description: walletTools.DescribeTool("swap_token"),
			Method:      "SwapTokenTool",
		},
		{
			Name:        "contract_call",
			Description: walletTools.DescribeTool("contract_call"),
			Method:      "ContractCallTool",
		},
		{
			Name:        "get_transaction_history",
			Description: walletTools.DescribeTool("get_transaction_history"),
			Method:      "GetTransactionHistoryTool",
		},
		{
			Name:        "encrypt_message",
			Description: walletTools.DescribeTool("encrypt_message"),
			Method:      "EncryptMessageTool",
		},
		{
			Name:        "decrypt_message",
			Description: walletTools.DescribeTool("decrypt_message"),
			Method:      "DecryptMessageTool",
		},
		{
			Name:        "get_ed25519_public_key",
			Description: walletTools.DescribeTool("get_ed25519_public_key"),
			Method:      "GetEd25519PublicKeyTool",
		},
	}
	agent.AddTools(loop.WrapToolsWithDefinitions(walletTools, defs)...)
	agent.AddTools(loop.NewFuncTool(
		"get_balances",
		"Get a fixed balance snapshot with no options: solana, eth, polygon, polygon_usdte, and polygon_usdce.",
		&genai.Schema{Type: genai.TypeObject},
		func(ctx context.Context, _ map[string]any) (*loop.ToolResult, error) {
			checks := []struct {
				name  string
				input tools.GetBalanceInput
			}{
				{name: "solana", input: tools.GetBalanceInput{Chain: "solana"}},
				{name: "eth", input: tools.GetBalanceInput{Chain: "ethereum"}},
				{name: "polygon", input: tools.GetBalanceInput{Chain: "polygon"}},
				{name: "polygon_usdte", input: tools.GetBalanceInput{Chain: "polygon", TokenAddress: "0xC2132D05D31c914a87C6611C10748AEb04B58e8F"}},
				{name: "polygon_usdce", input: tools.GetBalanceInput{Chain: "polygon", TokenAddress: "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"}},
			}

			snapshot := make(map[string]any, len(checks))
			for _, check := range checks {
				result, err := walletTools.GetBalanceTool(ctx, check.input)
				if err != nil {
					return nil, err
				}
				if !result.Success {
					snapshot[check.name] = map[string]any{"error": result.Error}
					continue
				}
				snapshot[check.name] = result.Content
			}
			return loop.NewSuccessResult(snapshot), nil
		},
	))
	fmt.Println(color.HiBlackString("Tools: wallet"))
}
