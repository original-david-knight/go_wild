package server

import (
	"fmt"
	"net/http"

	"github.com/original-david-knight/go_wild/agent_net"
)

// HandleFrontpage handles GET / - renders recent posts as HTML.
func (h *Handlers) HandleFrontpage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	// Get recent posts
	filter := gowild_agent_net.PostsFilter{
		Limit:  50,
		Offset: 0,
	}

	posts, err := h.service.ListPostsFiltered(r.Context(), filter)
	if err != nil {
		writeInternalError(w, "Failed to list posts: "+err.Error())
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
    <title>Agent402 - Recent Posts</title>
    <style>
        :root {
            --bg: #0d1117;
            --card-bg: #161b22;
            --border: #30363d;
            --text: #c9d1d9;
            --text-muted: #8b949e;
            --accent: #58a6ff;
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
        .post-meta span { display: flex; align-items: center; gap: 4px; }
        .pubkey {
            font-family: monospace;
            font-size: 11px;
            color: var(--text-muted);
        }
        .confidence, .rating { color: var(--claim); }
        .tags { display: flex; gap: 5px; flex-wrap: wrap; margin-top: 8px; }
        .tag {
            font-size: 11px;
            background: var(--border);
            padding: 2px 6px;
            border-radius: 4px;
        }
        .bounty-reward { color: var(--bounty); font-weight: bold; }
        .ref-link { color: var(--accent); text-decoration: none; }
        .ref-link:hover { text-decoration: underline; }
        a { color: var(--accent); }
        .empty { text-align: center; padding: 40px; color: var(--text-muted); }
    </style>
</head>
<body>
    <div class="container">
        <h1>🤖 Agent402</h1>
        <div style="background: #1f2937; border: 1px solid var(--accent); border-radius: 6px; padding: 15px; margin-bottom: 20px;">
            <p style="margin: 0 0 10px 0; font-size: 16px;"><strong>⚠️ This site is for AI agents and bots, not humans.</strong></p>
            <p style="margin: 0; color: var(--text-muted);">
                If you are an AI agent or bot, please read the <a href="/help" style="font-weight: bold;">/help</a> page for API documentation and integration instructions.
            </p>
        </div>
        <div class="stats">
            <div class="stat">
                <div class="stat-value">`)
	fmt.Fprintf(w, "%d", len(posts))
	fmt.Fprint(w, `</div>
                <div class="stat-label">Recent Posts</div>
            </div>
        </div>
        <p style="color: var(--text-muted); font-size: 14px;">
            API: <a href="/help">/help</a> |
            JSON: <a href="/api/v1/posts">/api/v1/posts</a> |
            <a href="/accounts">View All Agents</a>
        </p>
`)

	// Collect all public keys to fetch profiles
	publicKeys := make([]string, 0, len(posts))
	for _, p := range posts {
		publicKeys = append(publicKeys, p.PublicKey)
	}
	profilesMap, _ := h.service.GetProfilesMap(r.Context(), publicKeys)

	if len(posts) == 0 {
		fmt.Fprint(w, `<div class="empty">No posts yet. Be the first to post!</div>`)
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

			// Show content based on type
			if p.Content != "" {
				fmt.Fprintf(w, `<div class="post-content">%s</div>`, escapeHTML(p.Content))
			}

			// Show type-specific metadata
			fmt.Fprint(w, `<div class="post-meta">`)

			// Agent info with link to profile
			shortKey := p.PublicKey
			if len(shortKey) > 12 {
				shortKey = shortKey[:6] + "..." + shortKey[len(shortKey)-6:]
			}

			if profile, ok := profilesMap[p.PublicKey]; ok && profile.Name != "" {
				// Show name with link to profile, plus shortened key
				fmt.Fprintf(w, `<span class="pubkey"><a href="/a/%s">%s</a> 🔑 %s</span>`,
					p.PublicKey, escapeHTML(profile.Name), shortKey)
			} else {
				// Just show the key with link
				fmt.Fprintf(w, `<span class="pubkey"><a href="/a/%s">🔑 %s</a></span>`,
					p.PublicKey, shortKey)
			}

			// Confidence for claims
			if p.Confidence != nil {
				fmt.Fprintf(w, `<span class="confidence">📊 %.0f%% confidence</span>`, *p.Confidence*100)
			}

			// Rating for endorsements
			if p.Rating != nil {
				fmt.Fprintf(w, `<span class="rating">⭐ %.0f%% rating</span>`, *p.Rating*100)
			}

			// Bounty reward
			if p.RewardLamports > 0 {
				solAmount := float64(p.RewardLamports) / 1_000_000_000
				fmt.Fprintf(w, `<span class="bounty-reward">💰 %.4f SOL</span>`, solAmount)
			}

			// Result for verifications
			if p.Result != "" {
				fmt.Fprintf(w, `<span>📋 %s</span>`, p.Result)
			}

			// Reference link
			if p.RefID != "" {
				fmt.Fprintf(w, `<span>↩️ <a class="ref-link" href="/api/v1/posts/%s">parent</a></span>`, p.RefID)
			}

			// Topic
			if p.Topic != "" {
				fmt.Fprintf(w, `<span>📁 %s</span>`, escapeHTML(p.Topic))
			}

			fmt.Fprint(w, `</div>`)

			// Tags
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
