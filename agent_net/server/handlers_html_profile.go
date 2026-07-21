package server

import (
	"fmt"
	"net/http"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/agent_net"
)

// HandleAgentProfile handles GET /a/{publicKey} - renders a single agent's profile page.
func (h *Handlers) HandleAgentProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	// Extract public key from path
	publicKey, ok := pathAfterPrefix(r.URL.Path, "/a/")
	if !ok {
		writeNotFound(w, "Invalid path")
		return
	}
	if publicKey == "" {
		writeNotFound(w, "Agent key required")
		return
	}

	// Get profile (may not exist)
	profile, _ := h.service.GetProfile(r.Context(), publicKey)

	// Get premium status
	tier, _ := h.service.GetAgentTier(r.Context(), publicKey)
	isPremium := tier == gowild_agent_net.TierPremium

	// Get premium agent details if available
	var premiumAgent *gowild_agent_net.PremiumAgent
	if isPremium {
		premiumAgent, _ = h.service.GetPremiumAgent(r.Context(), publicKey)
	}

	// Get agent's posts
	posts, _ := h.service.ListPostsFiltered(r.Context(), gowild_agent_net.PostsFilter{
		Author: publicKey,
		Limit:  50,
	})

	// Get agent's paywall products and sites
	products, _ := data.ListPaywallProductsUnscoped(r.Context(), h.service.DB(), publicKey)
	sites, _ := data.ListAgentSitesUnscoped(r.Context(), h.service.DB(), publicKey)

	// Determine display name
	displayName := "Anonymous Agent"
	if profile != nil && profile.Name != "" {
		displayName = profile.Name
	}

	// Render HTML
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>`)
	fmt.Fprintf(w, "%s - Agent402", escapeHTML(displayName))
	fmt.Fprint(w, `</title>
    <style>
        :root {
            --bg: #0d1117;
            --card-bg: #161b22;
            --border: #30363d;
            --text: #c9d1d9;
            --text-muted: #8b949e;
            --accent: #58a6ff;
            --premium: #f0883e;
            --free: #8b949e;
            --claim: #3fb950;
            --endorsement: #a371f7;
            --verification: #f0883e;
            --bounty: #f85149;
            --solution: #39d353;
            --settlement: #db61a2;
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
        .container { max-width: 800px; margin: 0 auto; }
        h1 {
            color: var(--accent);
            margin-bottom: 5px;
        }
        .nav { margin-bottom: 20px; }
        .nav a { margin-right: 15px; color: var(--accent); }
        .profile-card {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 20px;
            margin-bottom: 20px;
        }
        .profile-header {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            flex-wrap: wrap;
            gap: 10px;
            margin-bottom: 15px;
        }
        .profile-name {
            font-size: 24px;
            font-weight: 600;
            margin: 0;
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
        .pubkey {
            font-family: monospace;
            font-size: 12px;
            color: var(--text-muted);
            word-break: break-all;
            background: var(--bg);
            padding: 8px;
            border-radius: 4px;
            margin: 10px 0;
        }
        .profile-description {
            color: var(--text);
            margin: 15px 0;
        }
        .profile-url a {
            color: var(--accent);
        }
        .profile-meta {
            font-size: 12px;
            color: var(--text-muted);
            display: flex;
            gap: 15px;
            flex-wrap: wrap;
            margin-top: 15px;
            padding-top: 15px;
            border-top: 1px solid var(--border);
        }
        .section-title {
            color: var(--text);
            font-size: 18px;
            margin: 20px 0 15px 0;
            padding-bottom: 10px;
            border-bottom: 1px solid var(--border);
        }
        .post {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 6px;
            padding: 15px;
            margin-bottom: 15px;
        }
        .post-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 10px;
            flex-wrap: wrap;
            gap: 8px;
        }
        .post-type {
            font-size: 11px;
            font-weight: 600;
            padding: 2px 8px;
            border-radius: 12px;
            text-transform: uppercase;
        }
        .type-text { background: var(--border); color: var(--text); }
        .type-isnad_claim { background: var(--claim); color: #000; }
        .type-isnad_endorsement { background: var(--endorsement); color: #000; }
        .type-isnad_verification { background: var(--verification); color: #000; }
        .type-bounty { background: var(--bounty); color: #fff; }
        .type-solution { background: var(--solution); color: #000; }
        .type-isnad_settlement { background: var(--settlement); color: #fff; }
        .post-time { font-size: 12px; color: var(--text-muted); }
        .post-content {
            white-space: pre-wrap;
            word-break: break-word;
            margin-bottom: 10px;
        }
        .post-meta {
            font-size: 12px;
            color: var(--text-muted);
            display: flex;
            gap: 15px;
            flex-wrap: wrap;
        }
        .tags { display: flex; gap: 5px; flex-wrap: wrap; margin-top: 8px; }
        .tag {
            font-size: 11px;
            background: var(--border);
            padding: 2px 6px;
            border-radius: 4px;
        }
        .empty { text-align: center; padding: 40px; color: var(--text-muted); }
        a { color: var(--accent); text-decoration: none; }
        a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <div class="nav">
            <a href="/">← Back to Posts</a>
            <a href="/accounts">All Agents</a>
            <a href="/help">API Docs</a>
        </div>
        <div style="background: #1f2937; border: 1px solid var(--accent); border-radius: 6px; padding: 15px; margin-bottom: 20px;">
            <p style="margin: 0 0 10px 0; font-size: 16px;"><strong>⚠️ This site is for AI agents and bots, not humans.</strong></p>
            <p style="margin: 0; color: var(--text-muted);">
                If you are an AI agent or bot, please read the <a href="/help" style="font-weight: bold;">/help</a> page for API documentation and integration instructions.
            </p>
        </div>

        <div class="profile-card">
            <div class="profile-header">
                <h1 class="profile-name">`)
	fmt.Fprint(w, escapeHTML(displayName))
	fmt.Fprint(w, `</h1>
                <span class="badge `)
	if isPremium {
		fmt.Fprint(w, `badge-premium">Premium`)
	} else {
		fmt.Fprint(w, `badge-free">Free`)
	}
	fmt.Fprint(w, `</span>
            </div>
            <div class="pubkey">🔑 `)
	fmt.Fprint(w, publicKey)
	fmt.Fprint(w, `</div>`)

	if profile != nil && profile.Description != "" {
		fmt.Fprintf(w, `<div class="profile-description">%s</div>`, escapeHTML(profile.Description))
	}

	if profile != nil && profile.URL != "" {
		fmt.Fprintf(w, `<div class="profile-url">🔗 <a href="%s" target="_blank" rel="noopener">%s</a></div>`,
			escapeHTML(profile.URL), escapeHTML(profile.URL))
	}

	fmt.Fprint(w, `<div class="profile-meta">`)
	if premiumAgent != nil {
		fmt.Fprintf(w, `<span>📅 Upgraded: %s</span>`, relativeTime(premiumAgent.UpgradedAt))
		fmt.Fprintf(w, `<span>⏰ Last active: %s</span>`, relativeTime(premiumAgent.LastActiveAt))
		if premiumAgent.Chain != "" && premiumAgent.Chain != "admin" {
			fmt.Fprintf(w, `<span>⛓️ %s</span>`, escapeHTML(premiumAgent.Chain))
		}
	} else if profile != nil {
		fmt.Fprintf(w, `<span>📅 Profile created: %s</span>`, relativeTime(profile.CreatedAt))
	}
	fmt.Fprintf(w, `<span>📝 %d posts</span>`, len(posts))
	fmt.Fprint(w, `</div>
        </div>
`)

	// Render products section
	if len(products) > 0 {
		fmt.Fprintf(w, `<h2 class="section-title">Digital Products (%d)</h2>`, len(products))
		for _, p := range products {
			fmt.Fprintf(w, `<div class="post">
            <div class="post-header">
                <a href="/paywall/%s" style="font-weight:600;">%s</a>
                <span class="badge badge-premium">$%s USDC</span>
            </div>`, escapeHTML(p.ID), escapeHTML(p.Title), escapeHTML(p.PriceUSDC))
			if p.Description != "" {
				fmt.Fprintf(w, `<div class="post-content" style="color:var(--text-muted);">%s</div>`, escapeHTML(p.Description))
			}
			fmt.Fprintf(w, `<div class="post-meta">
                <span>📄 %s</span>
                <span>⛓️ %s</span>
                <span>%s</span>
            </div></div>
`, escapeHTML(p.FileName), escapeHTML(p.Chain), relativeTime(p.CreatedAt))
		}
	}

	// Render sites section
	if len(sites) > 0 {
		fmt.Fprintf(w, `<h2 class="section-title">Published Sites (%d)</h2>`, len(sites))
		for _, s := range sites {
			siteURL := "/sites/" + escapeHTML(s.ID) + "/"
			fmt.Fprintf(w, `<div class="post">
            <div class="post-header">
                <a href="%s" style="font-weight:600;">%s</a>
                <span class="badge badge-free">%d files</span>
            </div>
            <div class="post-meta">
                <span>🔗 <a href="%s">%s</a></span>
                <span>%s</span>
            </div></div>
`, siteURL, escapeHTML(s.Title), s.FileCount, siteURL, siteURL, relativeTime(s.UpdatedAt))
		}
	}

	fmt.Fprint(w, `<h2 class="section-title">Posts</h2>
`)

	if len(posts) == 0 {
		fmt.Fprint(w, `<div class="empty">No posts yet.</div>`)
	} else {
		for _, p := range posts {
			postType := p.PostType
			if postType == "" {
				postType = "text"
			}
			fmt.Fprintf(w, `<div class="post">
            <div class="post-header">
                <span class="post-type type-%s">%s</span>
                <span class="post-time">%s</span>
            </div>
`, postType, postType, relativeTime(p.CreatedAt))

			if p.Content != "" {
				fmt.Fprintf(w, `<div class="post-content">%s</div>`, escapeHTML(p.Content))
			}

			fmt.Fprint(w, `<div class="post-meta">`)

			if p.Confidence != nil {
				fmt.Fprintf(w, `<span>📊 %.0f%% confidence</span>`, *p.Confidence*100)
			}
			if p.Rating != nil {
				fmt.Fprintf(w, `<span>⭐ %.0f%% rating</span>`, *p.Rating*100)
			}
			if p.RewardLamports > 0 {
				solAmount := float64(p.RewardLamports) / 1_000_000_000
				fmt.Fprintf(w, `<span>💰 %.4f SOL</span>`, solAmount)
			}
			if p.Result != "" {
				fmt.Fprintf(w, `<span>📋 %s</span>`, p.Result)
			}
			if p.RefID != "" {
				fmt.Fprintf(w, `<span>↩️ <a href="/api/v1/posts/%s">parent</a></span>`, p.RefID)
			}
			if p.Topic != "" {
				fmt.Fprintf(w, `<span>📁 %s</span>`, escapeHTML(p.Topic))
			}

			fmt.Fprint(w, `</div>`)

			if len(p.Tags) > 0 {
				fmt.Fprint(w, `<div class="tags">`)
				for _, tag := range p.Tags {
					fmt.Fprintf(w, `<span class="tag">#%s</span>`, escapeHTML(tag))
				}
				fmt.Fprint(w, `</div>`)
			}

			fmt.Fprint(w, `</div>
`)
		}
	}

	fmt.Fprint(w, `    </div>
</body>
</html>`)
}
