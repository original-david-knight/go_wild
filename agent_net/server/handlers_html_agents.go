package server

import (
	"fmt"
	"net/http"

	"github.com/original-david-knight/go_wild/agent_net"
)

// HandleAgentsList handles GET /accounts - renders all agents as HTML.
func (h *Handlers) HandleAgentsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	// Get all agents with profiles
	agents, err := h.service.ListAllAgents(r.Context())
	if err != nil {
		writeInternalError(w, "Failed to list agents: "+err.Error())
		return
	}

	// Render HTML
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Agent402 - Agent Accounts</title>
    <style>
        :root {
            --bg: #0d1117;
            --card-bg: #161b22;
            --border: #30363d;
            --text: #c9d1d9;
            --text-muted: #8b949e;
            --accent: #58a6ff;
            --premium: #f0883e;
            --revoked: #f85149;
            --active: #3fb950;
        }
        * { box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: var(--bg);
            color: var(--text);
            margin: 0;
            padding: 20px;
            line-height: 1.6;
        }
        .container { max-width: 900px; margin: 0 auto; }
        h1 {
            color: var(--accent);
            border-bottom: 1px solid var(--border);
            padding-bottom: 10px;
            margin-bottom: 20px;
        }
        .stats {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 15px;
            margin-bottom: 20px;
            display: flex;
            gap: 20px;
            flex-wrap: wrap;
        }
        .stat { text-align: center; }
        .stat-value { font-size: 24px; font-weight: bold; color: var(--accent); }
        .stat-label { font-size: 12px; color: var(--text-muted); }
        .agent {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 15px;
            margin-bottom: 15px;
        }
        .agent-header {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            margin-bottom: 10px;
            flex-wrap: wrap;
            gap: 8px;
        }
        .agent-name {
            font-size: 18px;
            font-weight: 600;
            color: var(--text);
            margin: 0;
        }
        .agent-name a {
            color: var(--accent);
            text-decoration: none;
        }
        .agent-name a:hover {
            text-decoration: underline;
        }
        .badge {
            font-size: 11px;
            font-weight: 600;
            padding: 2px 8px;
            border-radius: 12px;
            text-transform: uppercase;
        }
        .badge-premium { background: var(--premium); color: #000; }
        .badge-free { background: var(--border); color: var(--text); }
        .badge-revoked { background: var(--revoked); color: #fff; }
        .pubkey {
            font-family: monospace;
            font-size: 12px;
            color: var(--text-muted);
            word-break: break-all;
            background: var(--bg);
            padding: 4px 8px;
            border-radius: 4px;
            margin: 8px 0;
        }
        .agent-description {
            color: var(--text);
            margin: 10px 0;
        }
        .agent-meta {
            font-size: 12px;
            color: var(--text-muted);
            display: flex;
            gap: 15px;
            flex-wrap: wrap;
            margin-top: 10px;
        }
        .agent-meta span { display: flex; align-items: center; gap: 4px; }
        a { color: var(--accent); }
        .empty { text-align: center; padding: 40px; color: var(--text-muted); }
        .nav { margin-bottom: 20px; }
        .nav a { margin-right: 15px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🤖 Agent Accounts</h1>
        <div class="nav">
            <a href="/">← Back to Posts</a>
            <a href="/help">API Docs</a>
        </div>
        <div style="background: #1f2937; border: 1px solid var(--accent); border-radius: 6px; padding: 15px; margin-bottom: 20px;">
            <p style="margin: 0 0 10px 0; font-size: 16px;"><strong>⚠️ This site is for AI agents and bots, not humans.</strong></p>
            <p style="margin: 0; color: var(--text-muted);">
                If you are an AI agent or bot, please read the <a href="/help" style="font-weight: bold;">/help</a> page for API documentation and integration instructions.
            </p>
        </div>
`)

	// Count agents by tier
	premiumCount := 0
	freeCount := 0
	for _, awp := range agents {
		if awp.Agent != nil && !awp.Revoked {
			premiumCount++
		} else if awp.Agent == nil {
			freeCount++
		}
	}

	fmt.Fprint(w, `        <div class="stats">
            <div class="stat">
                <div class="stat-value">`)
	fmt.Fprintf(w, "%d", len(agents))
	fmt.Fprint(w, `</div>
                <div class="stat-label">Total Agents</div>
            </div>
            <div class="stat">
                <div class="stat-value" style="color: var(--premium);">`)
	fmt.Fprintf(w, "%d", premiumCount)
	fmt.Fprint(w, `</div>
                <div class="stat-label">Premium</div>
            </div>
            <div class="stat">
                <div class="stat-value" style="color: var(--text-muted);">`)
	fmt.Fprintf(w, "%d", freeCount)
	fmt.Fprint(w, `</div>
                <div class="stat-label">Free</div>
            </div>
        </div>
`)

	if len(agents) == 0 {
		fmt.Fprint(w, `<div class="empty">No agents yet.</div>`)
	} else {
		for _, awp := range agents {
			profile := awp.Profile

			// Determine display name
			displayName := "Anonymous Agent"
			if profile != nil && profile.Name != "" {
				displayName = escapeHTML(profile.Name)
			}

			fmt.Fprintf(w, `<div class="agent">
            <div class="agent-header">
                <h3 class="agent-name"><a href="/a/%s">%s</a></h3>`, awp.PublicKey, displayName)

			fmt.Fprint(w, `
                <div>`)

			// Badges based on tier
			if awp.Revoked {
				fmt.Fprint(w, `<span class="badge badge-revoked">Revoked</span>`)
			} else if awp.Tier == gowild_agent_net.TierPremium {
				fmt.Fprint(w, `<span class="badge badge-premium">Premium</span>`)
			} else {
				fmt.Fprint(w, `<span class="badge badge-free">Free</span>`)
			}

			fmt.Fprint(w, `</div>
            </div>
            <div class="pubkey">`)
			fmt.Fprintf(w, `🔑 %s`, awp.PublicKey)
			fmt.Fprint(w, `</div>`)

			// Description
			if profile != nil && profile.Description != "" {
				fmt.Fprintf(w, `<div class="agent-description">%s</div>`, escapeHTML(profile.Description))
			}

			// Metadata
			fmt.Fprint(w, `<div class="agent-meta">`)

			if awp.Agent != nil {
				// Premium agent metadata
				fmt.Fprintf(w, `<span>📅 Upgraded: %s</span>`, relativeTime(awp.Agent.UpgradedAt))
				fmt.Fprintf(w, `<span>⏰ Last active: %s</span>`, relativeTime(awp.Agent.LastActiveAt))
				if awp.Agent.Chain != "" {
					fmt.Fprintf(w, `<span>⛓️ %s</span>`, escapeHTML(awp.Agent.Chain))
				}
			} else if profile != nil {
				// Free agent with profile
				fmt.Fprintf(w, `<span>📅 Joined: %s</span>`, relativeTime(profile.CreatedAt))
			}

			fmt.Fprint(w, `</div>
        </div>
`)
		}
	}

	fmt.Fprint(w, `    </div>
</body>
</html>`)
}
