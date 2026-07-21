// ============================================================
// API helpers
// ============================================================

async function api(method, path, body) {
    const opts = { method, headers: { 'Content-Type': 'application/json' } };
    if (body) opts.body = JSON.stringify(body);
    const resp = await fetch(path, opts);
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || 'Request failed');
    return data;
}

// ============================================================
// Agent Picker
// ============================================================

function toggleAgentPicker() {
    const dropdown = document.getElementById('agent-picker-dropdown');
    dropdown.classList.toggle('active');
    if (dropdown.classList.contains('active')) {
        renderAgentPicker();
        document.getElementById('agent-search').focus();
        // Close on outside click
        setTimeout(() => {
            document.addEventListener('click', closeAgentPickerOnOutsideClick);
        }, 0);
    } else {
        document.removeEventListener('click', closeAgentPickerOnOutsideClick);
    }
}

function closeAgentPickerOnOutsideClick(e) {
    const picker = document.querySelector('.agent-picker');
    if (!picker.contains(e.target)) {
        document.getElementById('agent-picker-dropdown').classList.remove('active');
        document.removeEventListener('click', closeAgentPickerOnOutsideClick);
    }
}

function filterAgentPicker() {
    renderAgentPicker();
}

function renderAgentPicker() {
    const list = document.getElementById('agent-picker-list');
    const searchTerm = document.getElementById('agent-search').value.toLowerCase();

    const items = [];
    const builtinSearchText = 'builtin builtins built in methods method terminal pipeline stream io';
    if (!searchTerm || builtinSearchText.includes(searchTerm)) {
        const isDisplayed = displayedAgents.has(builtinMethodsColumnId);
        const displayedClass = isDisplayed ? ' displayed' : '';
        const statusText = isDisplayed ? 'live stream (shown)' : 'live stream';
        items.push(`<div class="agent-picker-item${displayedClass}" onclick="${isDisplayed ? '' : 'addBuiltinMethodsColumn()'}">
            <span class="status-dot builtin"></span>
            <span class="agent-name">${escHtml(builtinMethodsColumnTitle)}</span>
            <span class="agent-badges"></span>
            <span class="agent-status">${statusText}</span>
        </div>`);
    }

    const filtered = agents.filter(a => {
        const name = (a.name || a.id).toLowerCase();
        return name.includes(searchTerm);
    });

    if (!filtered.length && !items.length) {
        list.innerHTML = '<div style="padding:12px;color:var(--text-muted);font-size:13px;text-align:center">No agents or builtins found</div>';
        return;
    }

    items.push(...filtered.map(a => {
        const dotClass = a.image_stale ? 'stale' : (a.container_status || 'unknown');
        const isDisplayed = displayedAgents.has(a.id);
        const displayedClass = isDisplayed ? ' displayed' : '';
        const badges = [];
        if (a.has_telegram) badges.push('<span class="integration-dot" title="Telegram">T</span>');
        if (a.has_email) badges.push('<span class="integration-dot" title="Email">E</span>');
        const statusText = (a.container_status || 'no container') + (a.image_stale ? ' · stale' : '') + (isDisplayed ? ' (shown)' : '');
        return `<div class="agent-picker-item${displayedClass}" onclick="${isDisplayed ? '' : `addAgentColumn('${a.id}')`}">
            <span class="status-dot ${dotClass}"></span>
            <span class="agent-name">${escHtml(a.name || a.id)}</span>
            <span class="agent-badges">${badges.join('')}</span>
            <span class="agent-status">${statusText}</span>
        </div>`;
    }));

    list.innerHTML = items.join('');
}

// ============================================================
// Agent list management
// ============================================================

async function refreshAgents() {
    try {
        agents = await api('GET', '/api/agents');
    } catch (e) {
        console.error('Failed to refresh agents:', e);
    }
    if (document.getElementById('companies-modal')?.style.display === 'flex') {
        refreshCompanyCreateCEOOptions();
    }
    // Update displayed columns with latest status
    updateDisplayedColumns();
    if (typeof refreshPipelineActionAgentSelects === 'function') {
        refreshPipelineActionAgentSelects();
    }
}

function updateDisplayedColumns() {
    for (const [agentId, state] of displayedAgents) {
        const agent = agents.find(a => a.id === agentId);
        if (!agent) continue;

        const col = state.columnEl;
        const badge = col.querySelector('.column-badge');
        const status = agent.container_status || 'no container';
        badge.textContent = status;
        badge.className = 'badge column-badge ' + (agent.container_status || 'unknown');

        const staleBadge = col.querySelector('.column-stale-badge');
        if (staleBadge) {
            if (agent.image_stale) {
                staleBadge.style.display = '';
                staleBadge.className = 'badge column-stale-badge stale';
                staleBadge.textContent = 'stale';
                const imageId = agent.image_build_id || 'unknown';
                const desiredId = agent.desired_build_id || 'unknown';
                staleBadge.title = `image ${imageId} != expected ${desiredId}`;
            } else {
                staleBadge.style.display = 'none';
                staleBadge.title = '';
            }
        }

        const refreshBtn = col.querySelector('.btn-refresh-image');
        if (refreshBtn) {
            refreshBtn.style.display = agent.image_stale ? '' : 'none';
        }

        // Show/hide start/stop buttons
        const isRunning = agent.container_status === 'running';
        col.querySelector('.btn-start').style.display = isRunning ? 'none' : '';
        col.querySelector('.btn-stop').style.display = isRunning ? '' : 'none';
    }

}

// ============================================================
// Column Management
// ============================================================

function addAgentColumn(agentId) {
    if (displayedAgents.has(agentId)) return;

    const agent = agents.find(a => a.id === agentId);
    if (!agent) return;

    // Hide empty state
    document.getElementById('empty-state').style.display = 'none';

    // Clone template
    const template = document.getElementById('agent-column-template');
    const columnEl = template.content.firstElementChild.cloneNode(true);
    columnEl.setAttribute('data-agent-id', agentId);

    // Populate header
    columnEl.querySelector('.column-agent-name').textContent = agent.name || agent.id;
    const badge = columnEl.querySelector('.column-badge');
    const status = agent.container_status || 'no container';
    badge.textContent = status;
    badge.className = 'badge column-badge ' + (agent.container_status || 'unknown');

    const staleBadge = columnEl.querySelector('.column-stale-badge');
    if (staleBadge) {
        if (agent.image_stale) {
            staleBadge.style.display = '';
            staleBadge.className = 'badge column-stale-badge stale';
            staleBadge.textContent = 'stale';
            const imageId = agent.image_build_id || 'unknown';
            const desiredId = agent.desired_build_id || 'unknown';
            staleBadge.title = `image ${imageId} != expected ${desiredId}`;
        } else {
            staleBadge.style.display = 'none';
            staleBadge.title = '';
        }
    }

    const refreshBtn = columnEl.querySelector('.btn-refresh-image');
    if (refreshBtn) {
        refreshBtn.style.display = agent.image_stale ? '' : 'none';
    }

    const isRunning = agent.container_status === 'running';
    columnEl.querySelector('.btn-start').style.display = isRunning ? 'none' : '';
    columnEl.querySelector('.btn-stop').style.display = isRunning ? '' : 'none';
    // Don't set smart toggle from agent.smart_default here — the actual
    // runtime state arrives via the agent's smart_mode WebSocket message,
    // which the manager replays to newly-connected clients.
    markSmartToggleUnknown(columnEl);
    setAutoplayState(columnEl, false);

    // Populate config tab
    populateConfigTab(columnEl, agent);

    // Add to container
    document.getElementById('columns-container').appendChild(columnEl);

    // Create state
    const state = new ColumnState(agentId, columnEl);
    state.emailEnabled = !agent.enabled_tools || agent.enabled_tools.includes('email');
    displayedAgents.set(agentId, state);

    // Setup input handlers
    setupColumnInputHandlers(state);

    // Check for pending emails
    checkPendingEmails(state);

    // Connect terminal if running
    if (isRunning) {
        connectColumnTerminal(state);
    }

    // Close picker
    document.getElementById('agent-picker-dropdown').classList.remove('active');
}

function addBuiltinMethodsColumn() {
    if (displayedAgents.has(builtinMethodsColumnId)) return;

    document.getElementById('empty-state').style.display = 'none';

    const template = document.getElementById('agent-column-template');
    const columnEl = template.content.firstElementChild.cloneNode(true);
    columnEl.setAttribute('data-agent-id', builtinMethodsColumnId);
    columnEl.dataset.columnKind = 'builtin-methods';

    columnEl.querySelector('.column-agent-name').textContent = builtinMethodsColumnTitle;
    const badge = columnEl.querySelector('.column-badge');
    badge.textContent = 'live';
    badge.className = 'badge column-badge builtin';

    const staleBadge = columnEl.querySelector('.column-stale-badge');
    if (staleBadge) staleBadge.style.display = 'none';

    [
        '.autoplay-toggle',
        '.smart-toggle',
        '.btn-refresh-image',
        '.btn-start',
        '.btn-stop',
        '.btn-work-tasks',
        '.btn-clear-context',
        '.btn-clone',
        '.btn-restart',
    ].forEach(selector => {
        const el = columnEl.querySelector(selector);
        if (el) el.style.display = 'none';
    });

    columnEl.querySelectorAll('.tab').forEach(tab => {
        const active = tab.dataset.tab === 'terminal';
        tab.classList.toggle('active', active);
        if (!active) tab.style.display = 'none';
    });
    columnEl.querySelectorAll('.tab-panel').forEach(panel => {
        const active = panel.dataset.panel === 'terminal';
        panel.classList.toggle('active', active);
        if (!active) panel.style.display = 'none';
    });

    const inputBar = columnEl.querySelector('.chat-input-bar');
    if (inputBar) inputBar.style.display = 'none';
    const fileIndicator = columnEl.querySelector('.file-indicator');
    if (fileIndicator) fileIndicator.style.display = 'none';
    const emailNotification = columnEl.querySelector('.email-notification');
    if (emailNotification) emailNotification.style.display = 'none';

    document.getElementById('columns-container').appendChild(columnEl);

    const state = new ColumnState(builtinMethodsColumnId, columnEl, { columnKind: 'builtin-methods' });
    displayedAgents.set(builtinMethodsColumnId, state);
    connectColumnTerminal(state);

    document.getElementById('agent-picker-dropdown').classList.remove('active');
}

function removeColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');

    const state = displayedAgents.get(agentId);
    if (state) {
        state.destroy();
        displayedAgents.delete(agentId);
    }

    col.remove();

    // Show empty state if no columns
    if (displayedAgents.size === 0) {
        document.getElementById('empty-state').style.display = '';
    }
}

function renderToolGroupCheckboxes(columnEl, enabledTools) {
    const grid = columnEl.querySelector('.tool-groups-grid');
    if (!grid) return;
    grid.innerHTML = '';
    // null/undefined means not configured yet — all enabled
    const enabled = enabledTools ? new Set(enabledTools) : null;
    for (const g of toolGroups) {
        const row = document.createElement('div');
        row.style.cssText = 'display:flex;align-items:center;gap:6px;justify-content:space-between';

        const label = document.createElement('label');
        label.style.cssText = 'display:flex;align-items:center;gap:6px;font-size:12px;cursor:pointer;flex:1;min-width:0';
        const cb = document.createElement('input');
        cb.type = 'checkbox';
        let checked = enabled === null || enabled.has(g.id);
        if (!checked && enabled !== null) {
            // Legacy config: polymarket implied read + buy + sell.
            if (enabled.has('polymarket') && (g.id === 'polymarket_read' || g.id === 'polymarket_buy' || g.id === 'polymarket_sell')) checked = true;
            // Legacy config: polymarket_trade implied buy + sell.
            if (enabled.has('polymarket_trade') && (g.id === 'polymarket_buy' || g.id === 'polymarket_sell')) checked = true;
            // Legacy config: shopify or company_commerce implied read + write.
            if ((enabled.has('shopify') || enabled.has('company_commerce')) && (g.id === 'shopify_read' || g.id === 'shopify_write')) checked = true;
        }
        cb.checked = checked;
        cb.dataset.toolGroup = g.id;
        label.appendChild(cb);
        const name = document.createElement('span');
        name.textContent = g.display_name;
        label.appendChild(name);

        const help = document.createElement('span');
        help.textContent = '?';
        help.setAttribute('role', 'img');
        help.setAttribute('aria-label', `${g.display_name} tools`);
        help.style.cssText = 'display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;border-radius:50%;border:1px solid var(--border);font-size:11px;line-height:1;color:var(--text-muted);cursor:help;user-select:none;position:relative';
        const tipText = toolGroupTooltipText(g);
        help.addEventListener('mouseenter', function() {
            let tip = this.querySelector('.tool-tip');
            if (!tip) {
                tip = document.createElement('div');
                tip.className = 'tool-tip';
                tip.style.cssText = 'position:absolute;bottom:calc(100% + 6px);right:0;background:#131a24;border:1px solid var(--border);border-radius:6px;padding:8px 10px;font-size:11px;line-height:1.5;color:var(--text);white-space:pre;z-index:1000;pointer-events:none;box-shadow:0 4px 12px rgba(0,0,0,0.3);max-height:300px;overflow-y:auto';
                tip.textContent = tipText;
                this.appendChild(tip);
            }
            tip.style.display = 'block';
        });
        help.addEventListener('mouseleave', function() {
            const tip = this.querySelector('.tool-tip');
            if (tip) tip.style.display = 'none';
        });

        row.appendChild(label);
        row.appendChild(help);
        grid.appendChild(row);
    }
}

async function renderCompanyMethodToolCheckboxes(columnEl, agentId, enabledTools) {
    const grid = columnEl.querySelector('.company-method-tools-grid');
    if (!grid) return;
    grid.innerHTML = '<div style="font-size:12px;color:var(--text-muted)">Loading teammate A2A tools...</div>';

    let tools = [];
    try {
        const data = await api('GET', `/api/agents/${agentId}/company-method-tools`);
        tools = Array.isArray(data.tools) ? data.tools : [];
    } catch (e) {
        grid.innerHTML = '<div style="font-size:12px;color:var(--text-muted)">Unable to load teammate A2A tools.</div>';
        return;
    }

    if (!tools.length) {
        grid.innerHTML = '<div style="font-size:12px;color:var(--text-muted)">No teammate methods available.</div>';
        return;
    }

    const enabled = enabledTools ? new Set(enabledTools) : new Set();
    grid.innerHTML = '';
    for (const t of tools) {
        const toolName = t && t.tool_name ? String(t.tool_name) : '';
        const method = t && t.method ? String(t.method) : toolName;
        const legacyNames = Array.isArray(t && t.legacy_tool_names) ? t.legacy_tool_names.map(v => String(v || '').trim()).filter(Boolean) : [];
        if (!toolName) continue;

        const providerCount = Number.isFinite(Number(t.provider_agent_count)) ? Number(t.provider_agent_count) : 0;
        const providerLabel = providerCount > 0 ? ` (${providerCount} provider${providerCount === 1 ? '' : 's'})` : '';
        const titleParts = [method];
        if (t.description) titleParts.push(String(t.description));

        const label = document.createElement('label');
        label.style.cssText = 'display:flex;align-items:center;gap:6px;font-size:12px;cursor:pointer';
        label.title = titleParts.join('\n\n');

        const cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.checked = enabled.has(toolName) || legacyNames.some(name => enabled.has(name));
        cb.dataset.companyMethodTool = toolName;

        label.appendChild(cb);
        label.appendChild(document.createTextNode(method + providerLabel));
        grid.appendChild(label);
    }
}

async function renderDeepResearchToolCheckboxes(columnEl, enabledTools) {
    const grid = columnEl.querySelector('.deep-research-tools-grid');
    if (!grid) return;
    grid.innerHTML = '<div style="font-size:12px;color:var(--text-muted)">Loading deep research methods...</div>';

    if (!deepResearchMethods.length) {
        await loadDeepResearchMethods();
    }

    const methods = deepResearchMethods.filter(m => m.enabled);
    if (!methods.length) {
        grid.innerHTML = '<div style="font-size:12px;color:var(--text-muted)">No enabled deep research methods.</div>';
        return;
    }

    const enabled = enabledTools ? new Set(enabledTools) : new Set();
    grid.innerHTML = '';
    for (const m of methods) {
        const methodName = String(m.method || '');
        if (!methodName) continue;

        const label = document.createElement('label');
        label.style.cssText = 'display:flex;align-items:center;gap:6px;font-size:12px;cursor:pointer';
        label.title = m.description ? String(m.description) : methodName;

        const cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.checked = enabled.has(methodName);
        cb.dataset.deepResearchTool = methodName;

        label.appendChild(cb);
        label.appendChild(document.createTextNode(methodName));
        grid.appendChild(label);
    }
}

function collectEnabledTools(col) {
    const enabled = [];
    const seen = new Set();
    const pushEnabled = (id) => {
        if (!id || seen.has(id)) return;
        seen.add(id);
        enabled.push(id);
    };

    const groupGrid = col.querySelector('.tool-groups-grid');
    if (!groupGrid) return null;
    const groupCheckboxes = groupGrid.querySelectorAll('input[data-tool-group]');
    if (groupCheckboxes.length === 0) {
        // Tool groups have not rendered yet (for example tool-group list failed to load).
        // Return null so callers can avoid clobbering the server-side setting.
        return null;
    }
    groupCheckboxes.forEach(cb => {
        if (cb.checked) pushEnabled(cb.dataset.toolGroup);
    });

    const companyMethodGrid = col.querySelector('.company-method-tools-grid');
    if (companyMethodGrid) {
        companyMethodGrid.querySelectorAll('input[data-company-method-tool]').forEach(cb => {
            if (cb.checked) pushEnabled(cb.dataset.companyMethodTool);
        });
    }

    const deepResearchGrid = col.querySelector('.deep-research-tools-grid');
    if (deepResearchGrid) {
        deepResearchGrid.querySelectorAll('input[data-deep-research-tool]').forEach(cb => {
            if (cb.checked) pushEnabled(cb.dataset.deepResearchTool);
        });
    }

    return enabled;
}

function syncWorkerModeInputs(columnEl) {
    // Worker mode has been removed.
}

function toggleWorkerModeInColumn(input) {
    // Worker mode has been removed.
}

function updateProviderSpecificInputs(columnEl) {
    if (!columnEl) return;
    const provider = (columnEl.querySelector('.cfg-model-provider')?.value || 'gemini').trim().toLowerCase();
    const authGroup = columnEl.querySelector('.cfg-openai-auth-group');
    if (authGroup) {
        authGroup.style.display = provider === 'openai' ? '' : 'none';
    }

    const modelInput = columnEl.querySelector('.cfg-model');
    const smartModelInput = columnEl.querySelector('.cfg-smart-model');
    if (!modelInput || !smartModelInput) return;

    if (provider === 'openai') {
        modelInput.placeholder = 'gpt-5.4';
        smartModelInput.placeholder = 'gpt-5.4';
    } else if (provider === 'anthropic') {
        modelInput.placeholder = 'claude-opus-4-7';
        smartModelInput.placeholder = 'claude-opus-4-7';
    } else {
        modelInput.placeholder = 'gemini-3-flash-preview';
        smartModelInput.placeholder = 'gemini-3.1-pro-preview';
    }
}

function populateConfigTab(columnEl, agent) {
    columnEl.querySelector('.info-id').textContent = agent.id;
    columnEl.querySelector('.info-name').textContent = agent.name || '—';
    columnEl.querySelector('.info-description').textContent = agent.description || '—';

    const integrationsEl = columnEl.querySelector('.info-integrations');
    const integrations = [];
    if (agent.has_telegram) integrations.push('<span class="badge" style="background:rgba(88,166,255,0.15);color:var(--accent)">Telegram</span>');
    if (agent.has_email) integrations.push('<span class="badge" style="background:rgba(88,166,255,0.15);color:var(--accent)">Email</span>');
    if (integrations.length) {
        integrationsEl.innerHTML = '<strong>Integrations:</strong> ' + integrations.join(' ');
        integrationsEl.style.display = '';
    } else {
        integrationsEl.style.display = 'none';
    }

    columnEl.querySelector('.cfg-model-provider').value = agent.model_provider || 'gemini';
    columnEl.querySelector('.cfg-openai-auth-mode').value = agent.openai_auth_mode || 'api_key';
    columnEl.querySelector('.cfg-model').value = agent.model || '';
    columnEl.querySelector('.cfg-smart-model').value = agent.smart_model || '';
    columnEl.querySelector('.cfg-smart-default').checked = agent.smart_default || false;
    columnEl.querySelector('.cfg-max-turns').value = agent.max_turns || 100;
    columnEl.querySelector('.cfg-heartbeat').value = agent.heartbeat || '';
    columnEl.querySelector('.cfg-worktasks-timeout').value = agent.work_tasks_timeout || '';
    const extraFlagsInput = columnEl.querySelector('.cfg-extra-flags');
    if (extraFlagsInput) {
        extraFlagsInput.value = agent.extra_flags || '';
    }
    columnEl.querySelector('.cfg-env-vars').value = agent.env_vars ? JSON.stringify(agent.env_vars, null, 2) : '';
    columnEl.querySelector('.cfg-memory-limit').value = agent.memory_limit || '';
    columnEl.querySelector('.cfg-cpu-limit').value = agent.cpu_limit || '';
    columnEl.querySelector('.cfg-auto-start').checked = agent.auto_start || false;
    columnEl.querySelector('.cfg-system-prompt').value = agent.system_prompt || '';
    renderToolGroupCheckboxes(columnEl, agent.enabled_tools);
    renderCompanyMethodToolCheckboxes(columnEl, agent.id, agent.enabled_tools);
    renderDeepResearchToolCheckboxes(columnEl, agent.enabled_tools);
    updateProviderSpecificInputs(columnEl);
    syncWorkerModeInputs(columnEl);

    // Telegram token: show empty field; placeholder indicates if one is configured
    const telegramInput = columnEl.querySelector('.cfg-telegram-token');
    if (telegramInput) {
        telegramInput.value = '';
        telegramInput.placeholder = agent.has_telegram ? 'Token configured — leave blank to keep' : 'From @BotFather';
    }
    const clearTelegramBtn = columnEl.querySelector('.btn-clear-telegram');
    if (clearTelegramBtn) {
        clearTelegramBtn.style.display = agent.has_telegram ? '' : 'none';
    }

    // Agent Net status
    const agentNetSection = columnEl.querySelector('.agent-net-section');
    if (agentNetSection) {
        const pubkeyEl = agentNetSection.querySelector('.agent-net-pubkey');
        const badge = agentNetSection.querySelector('.agent-net-premium-badge');
        const grantBtn = agentNetSection.querySelector('.btn-grant-premium');
        if (agent.agent_net_public_key) {
            pubkeyEl.textContent = agent.agent_net_public_key;
            if (agent.agent_net_premium) {
                badge.style.display = '';
                grantBtn.style.display = 'none';
            } else {
                badge.style.display = 'none';
                grantBtn.style.display = '';
            }
        } else {
            pubkeyEl.textContent = 'No wallet seed phrase';
            badge.style.display = 'none';
            grantBtn.style.display = 'none';
        }
    }
}

function setupColumnInputHandlers(state) {
    const input = state.columnEl.querySelector('.chat-input');

    input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            sendMessageInColumnState(state);
        } else if (e.key === 'ArrowUp' && (input.value === '' || input.selectionStart === 0)) {
            if (state.promptHistory.length > 0 && state.historyIndex > 0) {
                e.preventDefault();
                state.historyIndex--;
                input.value = state.promptHistory[state.historyIndex];
            }
        } else if (e.key === 'ArrowDown' && input.selectionStart === input.value.length) {
            if (state.historyIndex < state.promptHistory.length) {
                e.preventDefault();
                state.historyIndex++;
                if (state.historyIndex >= state.promptHistory.length) {
                    input.value = '';
                } else {
                    input.value = state.promptHistory[state.historyIndex];
                }
            }
        }
    });

    input.addEventListener('input', () => {
        input.style.height = 'auto';
        input.style.height = Math.min(input.scrollHeight, 120) + 'px';
    });
}

// ============================================================
// Terminal Connection (per column)
// ============================================================

async function connectColumnTerminal(state) {
    if (isBuiltinMethodsColumnState(state)) {
        connectBuiltinMethodsTerminal(state);
        return;
    }

    state.chatUI.clear();
    state.chatUI.hideInput();

    // Load chat history before connecting
    try {
        const resp = await fetch(`/api/agents/${state.agentId}/chat-history?limit=50`);
        if (resp.ok) {
            const data = await resp.json();
            if (data.messages && data.messages.length > 0) {
                for (const msg of data.messages) {
                    if (msg.role === 'user') {
                        state.chatUI.addUserMessage(msg.content);
                        state.promptHistory.push(msg.content);
                    } else if (msg.role === 'assistant') {
                        state.chatUI.addAssistantHistory(msg.content);
                    }
                }
                state.chatUI.addHistorySeparator();
                state.historyIndex = state.promptHistory.length;
            }
        }
    } catch (e) {
        // History load is best-effort
    }

    state.streamParser = new StreamParser((type, html, final, msgId) => {
        switch (type) {
            case 'system':
                state.chatUI.addSystemMessage(html);
                break;
            case 'prompt':
                state.chatUI.enableInput();
                break;
            case 'thinking':
                state.chatUI.showThinking();
                break;
            case 'thinking-done':
                state.chatUI.hideThinking();
                break;
            case 'tool-status':
                state.chatUI.showToolStatus(html);
                break;
            case 'tool-summary':
                state.chatUI.addToolSummary(JSON.parse(html));
                break;
            case 'assistant-start':
                state.chatUI.startAssistantMessage(msgId);
                state.chatUI.disableInput();
                break;
            case 'assistant-chunk':
                state.chatUI.appendToAssistant(msgId, html);
                break;
            case 'assistant-end':
                state.chatUI.finalizeAssistant(msgId);
                state.chatUI.enableInput();
                break;
        }
    });

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    state.ws = new WebSocket(`${proto}//${location.host}/api/agents/${state.agentId}/terminal`);

    state.ws.onopen = async () => {
        state.chatUI.addStatusMessage('Connected', 'status-connected');
        try {
            const resp = await fetch(`/api/agents/${state.agentId}/runtime-status`);
            if (resp.ok) {
                const status = await resp.json();
                applyRuntimeStatus(state, status);
            }
        } catch (e) { /* best-effort */ }
    };

    state.ws.onmessage = (event) => {
        try {
            const msg = JSON.parse(event.data);
            if (msg.type === 'agent') {
                handleAgentMessage(state, msg);
            } else if (msg.type === 'command_result') {
                handleCommandResult(state, msg);
            } else if (msg.type === 'output' && msg.data) {
                const bytes = base64ToUtf8(msg.data);
                const recovered = recoverStructuredAgentMessagesFromRaw(state, bytes);
                if (recovered) state.streamParser.feed(recovered);
            } else if (msg.type === 'status') {
                if (msg.status === 'exited') {
                    state.chatUI.addStatusMessage('Container exited', 'status-error');
                    state.chatUI.hideInput();
                    markSmartToggleUnknown(state.columnEl);
                    refreshAgents();
                }
            }
        } catch (e) {
            state.streamParser.feed(event.data);
        }
    };

    state.ws.onclose = () => {
        state.chatUI.addStatusMessage('Disconnected', 'status-warn');
        state.chatUI.hideInput();
        markSmartToggleUnknown(state.columnEl);
    };

    state.ws.onerror = () => {
        state.chatUI.addStatusMessage('WebSocket error', 'status-error');
    };
}

function connectBuiltinMethodsTerminal(state) {
    if (state.ws) {
        state.ws.close();
        state.ws = null;
    }

    state.chatUI.clear();
    state.chatUI.hideInput();
    state.logsAnsi.reset();
    state.builtinTerminalBuffer = '';
    state.chatUI.addStatusMessage('Watching builtin method request/result I/O for all pipeline runs', 'status-info');

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    state.ws = new WebSocket(`${proto}//${location.host}${builtinMethodsTerminalPath}`);

    state.ws.onopen = () => {
        state.chatUI.addStatusMessage('Connected', 'status-connected');
    };

    state.ws.onmessage = (event) => {
        try {
            const msg = JSON.parse(event.data);
            if (msg.type === 'output' && msg.data) {
                processBuiltinTerminalChunk(state, base64ToUtf8(msg.data));
            } else if (msg.type === 'status' && msg.message && msg.status !== 'running') {
                const cssClass = msg.status === 'error' ? 'status-error' : 'status-warn';
                state.chatUI.addStatusMessage(msg.message, cssClass);
            }
        } catch (e) {
            processBuiltinTerminalChunk(state, event.data);
        }
    };

    state.ws.onclose = () => {
        state.chatUI.addStatusMessage('Disconnected', 'status-warn');
    };

    state.ws.onerror = () => {
        state.chatUI.addStatusMessage('WebSocket error', 'status-error');
    };
}

function processBuiltinTerminalChunk(state, text) {
    if (!state || typeof text !== 'string' || text === '') return;
    const extracted = extractBuiltinTerminalEntries((state.builtinTerminalBuffer || '') + text);
    state.builtinTerminalBuffer = extracted.remainder || '';

    extracted.entries.forEach(entry => renderBuiltinTerminalEntry(state, entry));

    if (!extracted.entries.length) {
        const leftover = String(extracted.remainder || '').trim();
        if (leftover && !leftover.startsWith('{')) {
            state.chatUI.addStatusMessage(leftover, 'status-system');
            state.builtinTerminalBuffer = '';
        }
    }
}

function extractBuiltinTerminalEntries(buffer) {
    const text = String(buffer || '');
    const entries = [];
    let cursor = 0;

    while (cursor < text.length) {
        const start = text.indexOf('{', cursor);
        if (start < 0) {
            return {
                entries,
                remainder: text.slice(cursor),
            };
        }

        let depth = 0;
        let inString = false;
        let escaped = false;
        let end = -1;
        for (let i = start; i < text.length; i += 1) {
            const ch = text[i];
            if (inString) {
                if (escaped) {
                    escaped = false;
                } else if (ch === '\\') {
                    escaped = true;
                } else if (ch === '"') {
                    inString = false;
                }
                continue;
            }
            if (ch === '"') {
                inString = true;
                continue;
            }
            if (ch === '{') {
                depth += 1;
            } else if (ch === '}') {
                depth -= 1;
                if (depth === 0) {
                    end = i;
                    break;
                }
            }
        }

        if (end < 0) {
            return {
                entries,
                remainder: text.slice(start),
            };
        }

        const candidate = text.slice(start, end + 1);
        try {
            entries.push(JSON.parse(candidate));
            cursor = end + 1;
        } catch (e) {
            return {
                entries,
                remainder: text.slice(start),
            };
        }
    }

    return { entries, remainder: '' };
}

function formatBuiltinTerminalJSON(value) {
    if (value == null) return '';
    if (typeof value === 'string') {
        const parsed = parseLogJSONLine(value);
        if (parsed !== null) {
            return JSON.stringify(parsed, null, 2);
        }
        return value;
    }
    try {
        return JSON.stringify(value, null, 2);
    } catch (e) {
        return String(value);
    }
}

function formatBuiltinTerminalDuration(durationMs) {
    if (!Number.isFinite(durationMs) || durationMs < 0) return '';
    if (durationMs < 1000) return `${durationMs}ms`;
    const seconds = durationMs / 1000;
    if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 2 : 1)}s`;
    const minutes = Math.floor(seconds / 60);
    const remaining = Math.round(seconds % 60);
    return `${minutes}m ${remaining}s`;
}

function parseBuiltinTerminalNumber(value) {
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string') {
        const parsed = Number.parseFloat(value.trim());
        if (Number.isFinite(parsed)) return parsed;
    }
    return null;
}

function formatBuiltinTerminalNumber(value) {
    const num = parseBuiltinTerminalNumber(value);
    if (!Number.isFinite(num)) return '';
    const abs = Math.abs(num);
    let decimals = 4;
    if (abs >= 100) decimals = 2;
    else if (abs >= 10) decimals = 3;
    const fixed = num.toFixed(decimals);
    return fixed.replace(/\.?0+$/, '');
}

function formatBuiltinTerminalSizingSource(value) {
    const raw = String(value || '').trim().toLowerCase();
    switch (raw) {
        case 'live_cache':
            return 'Cache';
        case 'live_snapshot':
            return 'Live';
        case 'current_position_fallback':
            return 'Fallback';
        case 'payload':
            return 'Payload';
        default:
            return '';
    }
}

function formatBuiltinTerminalThesisSource(value) {
    const raw = String(value || '').trim().toLowerCase();
    switch (raw) {
        case 'latest_note':
            return 'Note';
        case 'missing':
            return 'Missing';
        case 'payload':
            return 'Payload';
        default:
            return '';
    }
}

function extractBuiltinTerminalResultSummary(method, result) {
    if (!result || typeof result !== 'object' || Array.isArray(result)) return [];
    const targetPosition = parseBuiltinTerminalNumber(result.target_position);
    const targetGap = parseBuiltinTerminalNumber(result.target_gap);
    const targetSidePosition = parseBuiltinTerminalNumber(result.target_side_position);
    const maxAllowed = parseBuiltinTerminalNumber(result.max_allowed);
    const aum = parseBuiltinTerminalNumber(result.aum);
    const confidence = parseBuiltinTerminalNumber(result.confidence);
    const evDesiredShares = parseBuiltinTerminalNumber(result.ev_desired_shares);
    const marketPrice = parseBuiltinTerminalNumber(result.market_price);
    const sizingSource = formatBuiltinTerminalSizingSource(result.sizing_context_source);
    const thesisSource = formatBuiltinTerminalThesisSource(result.thesis_input_source);
    const hasSizing = Number.isFinite(targetPosition) || Number.isFinite(targetGap) || Number.isFinite(targetSidePosition);
    if (!hasSizing) return [];

    const fields = [];
    const side = String(result.side || '').trim().toUpperCase();
    if (side) {
        fields.push({ label: 'Side', value: side });
    }
    if (Number.isFinite(targetSidePosition)) {
        fields.push({ label: 'Held', value: formatBuiltinTerminalNumber(targetSidePosition) });
    }
    if (Number.isFinite(targetPosition)) {
        fields.push({ label: 'Target', value: formatBuiltinTerminalNumber(targetPosition) });
    }
    if (Number.isFinite(targetGap)) {
        fields.push({ label: 'Gap', value: formatBuiltinTerminalNumber(targetGap) });
    }
    if (Number.isFinite(evDesiredShares)) {
        fields.push({ label: 'EV Size', value: formatBuiltinTerminalNumber(evDesiredShares) });
    }
    if (Number.isFinite(maxAllowed) && maxAllowed > 0) {
        fields.push({ label: 'Max', value: formatBuiltinTerminalNumber(maxAllowed) });
    }
    if (Number.isFinite(aum) && aum > 0) {
        fields.push({ label: 'AUM', value: formatBuiltinTerminalNumber(aum) });
    }
    if (Number.isFinite(confidence) && confidence >= 0) {
        fields.push({ label: 'Conf', value: formatBuiltinTerminalNumber(confidence) });
    }
    if (sizingSource) {
        fields.push({ label: 'Cap Src', value: sizingSource });
    }
    if (thesisSource) {
        fields.push({ label: 'Thesis', value: thesisSource });
    }
    if (Number.isFinite(marketPrice) && marketPrice > 0) {
        fields.push({ label: 'Price', value: formatBuiltinTerminalNumber(marketPrice) });
    }
    return fields;
}

function renderBuiltinTerminalEntry(state, entry) {
    if (!state || !entry || typeof entry !== 'object' || Array.isArray(entry)) return;
    const meta = entry.meta && typeof entry.meta === 'object' && !Array.isArray(entry.meta) ? entry.meta : {};
    const payload = entry.payload && typeof entry.payload === 'object' && !Array.isArray(entry.payload) ? entry.payload : {};
    const event = String(meta.event || '').trim().toLowerCase();

    if (event === 'request') {
        state.chatUI.addBuiltinMethodCallMessage({
            method: String(meta.method || ''),
            pipelineId: String(meta.pipeline_id || ''),
            runId: String(meta.run_id || ''),
            stepIndex: Number.isFinite(meta.step_index) ? meta.step_index : Number.parseInt(meta.step_index, 10),
            fromAgent: 'pipeline',
            time: String(meta.time || ''),
            params: formatBuiltinTerminalJSON(payload.params),
        });
        return;
    }

    const bodyValue = event === 'error'
        ? { error: payload.error || 'builtin method failed' }
        : payload.result;
    const status = event === 'error'
        ? String(payload.status || 'failed')
        : String(payload.status || 'succeeded');

    state.chatUI.addBuiltinMethodResultMessage({
        kind: event === 'error' ? 'error' : 'result',
        title: event === 'error' ? 'Builtin Method Error' : 'Builtin Method Result',
        method: String(meta.method || ''),
        pipelineId: String(meta.pipeline_id || ''),
        runId: String(meta.run_id || ''),
        stepIndex: Number.isFinite(meta.step_index) ? meta.step_index : Number.parseInt(meta.step_index, 10),
        status,
        durationText: formatBuiltinTerminalDuration(Number(payload.duration_ms)),
        time: String(meta.time || ''),
        summaryFields: extractBuiltinTerminalResultSummary(String(meta.method || ''), bodyValue),
        bodyText: formatBuiltinTerminalJSON(bodyValue),
    });
}

function handleCommandResult(state, msg) {
    if (msg.success) {
        if (msg.message) {
            state.chatUI.addStatusMessage(msg.message, 'status-info');
        }
    } else {
        state.chatUI.addErrorMessage(msg.message || 'Command failed: ' + msg.command);
    }
    state.chatUI.enableInput();
    state.streamParser.toWaiting();
}

function isHeartbeatSystemMessage(content) {
    if (typeof content !== 'string') return false;
    const trimmed = content.trim();
    const withoutIcon = trimmed.startsWith('\ud83d\udccb ') ? trimmed.slice(3).trimStart() : trimmed;
    const normalized = withoutIcon.toLowerCase();
    return normalized.startsWith('heartbeat:') || normalized.startsWith('this is a heartbeat');
}

const recoverableAgentOutputTypes = new Set([
    'prompt',
    'system',
    'thinking',
    'response',
    'response_end',
    'tool_call',
    'tool_result',
    'smart_mode',
    'runtime_status',
    'content',
    'context_dump',
    'error',
]);

function recoverStructuredAgentMessagesFromRaw(state, raw) {
    if (typeof raw !== 'string' || raw === '') return raw;

    const lines = raw.replace(/\r\n/g, '\n').split('\n');
    const passthrough = [];

    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) {
            passthrough.push(line);
            continue;
        }

        let parsed = null;
        if (trimmed.startsWith('{') && trimmed.endsWith('}')) {
            try {
                parsed = JSON.parse(trimmed);
            } catch (e) {
                parsed = null;
            }
        }

        const agentType = parsed && typeof parsed.type === 'string' ? parsed.type : '';
        if (!recoverableAgentOutputTypes.has(agentType)) {
            passthrough.push(line);
            continue;
        }

        handleAgentMessage(state, {
            type: 'agent',
            agent_type: agentType,
            content: parsed.content || '',
            content_type: parsed.content_type || '',
            name: parsed.name || '',
            detail: parsed.detail || '',
            status: parsed.status || '',
            tokens: parsed.tokens || 0,
            duration: parsed.duration || '',
        });
    }

    const remainder = passthrough.join('\n');
    return remainder.replace(/^\n+|\n+$/g, '') ? remainder : '';
}

function isCompanyMethodCallHeartbeat(content) {
    if (typeof content !== 'string') return false;
    const trimmed = content.trim();
    const withoutIcon = trimmed.startsWith('\ud83d\udccb ') ? trimmed.slice(3).trimStart() : trimmed;
    return withoutIcon.toLowerCase().startsWith('this is a heartbeat for a company method call');
}

function parseCompanyMethodCallHeartbeat(content) {
    if (typeof content !== 'string') return null;
    const trimmed = content.trim();
    const withoutIcon = trimmed.startsWith('\ud83d\udccb ') ? trimmed.slice(3).trimStart() : trimmed;
    const extractField = (label) => {
        const escaped = String(label || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const pattern = new RegExp(`^${escaped}:[ \\t]*([^\\r\\n]*)$`, 'mi');
        const match = withoutIcon.match(pattern);
        return match ? match[1].trim() : '';
    };

    const method = extractField('Method');
    const jobID = extractField('Job ID');
    const fromAgent = extractField('From Agent');

    const marker = 'Input Parameters (JSON):';
    const markerIdx = withoutIcon.indexOf(marker);
    let params = '';
    if (markerIdx >= 0) {
        let block = withoutIcon.slice(markerIdx + marker.length).replace(/^\s*\n/, '');
        const stopMatch = block.match(/\n\n(?:Input Schema \(JSON\):|Output Schema \(JSON\):|Completion Rules:)/);
        if (stopMatch && typeof stopMatch.index === 'number') {
            block = block.slice(0, stopMatch.index);
        }
        params = block
            .split('\n')
            .map(line => line.replace(/^\s{2}/, ''))
            .join('\n')
            .trim();
    }

    let prettyParams = params;
    if (params) {
        try {
            prettyParams = JSON.stringify(JSON.parse(params), null, 2);
        } catch (e) {
            // Keep original text if it is not strict JSON.
        }
    }

    return {
        method: method || '',
        jobId: jobID || '',
        fromAgent: fromAgent || '',
        params: prettyParams || '',
    };
}

function systemMessageStatusClass(content) {
    const text = String(content || '').toLowerCase();
    if (!text) return 'status-system';
    if (text.includes('failed') || text.includes('error') || text.includes('panic')) {
        return 'status-error';
    }
    if (text.includes('warning') || text.includes('\u26a0') || text.includes('approaching limit')) {
        return 'status-warn';
    }
    return 'status-system';
}

function handleAgentMessage(state, msg) {
    if (!state.currentMsgId) {
        state.currentMsgId = 'msg-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);
    }

    switch (msg.agent_type) {
        case 'prompt':
            state.chatUI.finalizeAssistant(state.currentMsgId);
            state.chatUI.enableInput();
            state.streamParser.toWaiting();
            state.currentMsgId = null;
            checkPendingEmails(state);
            break;

        case 'system':
            if (msg.content && msg.content.startsWith('[DEBUG]')) break;
            if (isCompanyMethodCallHeartbeat(msg.content)) {
                const parsed = parseCompanyMethodCallHeartbeat(msg.content);
                if (parsed) {
                    const dedupeKey = parsed.jobId || '';
                    if (dedupeKey && state.seenIncomingCallJobIds.has(dedupeKey)) {
                        break;
                    }
                    if (dedupeKey) state.seenIncomingCallJobIds.add(dedupeKey);
                    state.chatUI.addIncomingCallMessage(parsed);
                    break;
                }
            }
            if (isHeartbeatSystemMessage(msg.content)) {
                state.chatUI.addHeartbeatMessage(msg.content);
                break;
            }
            state.chatUI.addStatusMessage(msg.content, systemMessageStatusClass(msg.content));
            break;

        case 'thinking':
            state.chatUI.showThinking();
            break;

        case 'response':
            state.chatUI.hideThinking();
            state.chatUI.ensureAssistantBubble(state.currentMsgId);
            state.chatUI.appendToAssistant(state.currentMsgId, msg.content, true);
            break;

        case 'response_end':
            if (state.pendingToolCalls && state.pendingToolCalls.length > 0) {
                state.chatUI.addToolSummary(state.pendingToolCalls);
            }
            state.pendingToolCalls = [];
            state.chatUI.finalizeAssistant(state.currentMsgId);
            state.currentMsgId = null;
            break;

        case 'tool_call':
            state.chatUI.hideThinking();
            state.chatUI.showToolStatus(msg.name);
            if (!state.pendingToolCalls) state.pendingToolCalls = [];
            state.pendingToolCalls.push({ name: msg.name, detail: msg.detail || '', status: 'running' });
            break;

        case 'tool_result': {
            state.chatUI.showToolStatus(null);
            if (state.pendingToolCalls) {
                const tc = state.pendingToolCalls.find(t => t.name === msg.name && t.status === 'running');
                if (tc) {
                    tc.status = msg.status === 'failed' ? 'failed' : 'done';
                    if (msg.duration) tc.duration = msg.duration;
                }
            }
            if (msg.name === 'send_email') checkPendingEmails(state);
            break;
        }

        case 'smart_mode': {
            updateSmartToggle(state.columnEl, msg.content === 'on');
            const label = msg.content === 'on' ? 'Smart mode ON' : 'Smart mode OFF';
            const detail = msg.detail ? ` (${msg.detail})` : '';
            state.chatUI.addStatusMessage(label + detail, 'status-system');
            break;
        }

        case 'runtime_status': {
            // The runtime_status has been cached server-side; re-fetch to apply.
            fetch(`/api/agents/${state.agentId}/runtime-status`)
                .then(r => r.ok ? r.json() : null)
                .then(s => { if (s) applyRuntimeStatus(state, s); })
                .catch(() => {});
            break;
        }

        case 'content':
            state.chatUI.addContentMessage(msg.content_type, msg.content, msg.detail);
            break;

        case 'context_dump':
            handleContextDumpMessage(state, msg);
            break;

        case 'error':
            state.chatUI.addErrorMessage(msg.content);
            break;
    }
}

// ============================================================
// Message Sending (per column)
// ============================================================

function sendMessageInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) sendMessageInColumnState(state);
}

function sendMessageInColumnState(state) {
    const input = state.columnEl.querySelector('.chat-input');
    const text = input.value.trim();
    if (!text) return;
    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) return;

    if (state.pendingInteractiveCmd) {
        handleInteractiveInputInColumn(state, text);
        input.value = '';
        input.style.height = 'auto';
        return;
    }

    const cmd = parseCommand(text);

    if (cmd) {
        const prompt = getInteractivePrompt(cmd);
        if (prompt) {
            state.chatUI.addUserMessage(text);
            input.value = '';
            input.style.height = 'auto';

            if (prompt.async) {
                handleAsyncPromptInColumn(state, cmd, prompt);
                return;
            }

            state.pendingInteractiveCmd = { cmd, prompt };
            state.chatUI.addStatusMessage(prompt.prompt, 'status-prompt');
            return;
        }
    }

    // If a file is attached, send /file command first, then the user's text
    if (state.pendingFile) {
        const filePath = state.pendingFile.path;
        const fileName = state.pendingFile.name;
        state.pendingFile = null;
        state.columnEl.querySelector('.file-indicator').style.display = 'none';

        state.chatUI.addUserMessage(`[${fileName}] ${text}`);
        state.streamParser.userSent(text);

        // Send /file command
        state.ws.send(JSON.stringify({ type: 'command', command: 'file', args: { path: filePath }, raw: '/file ' + filePath }));
        // Send the actual prompt after a short delay to ensure ordering
        setTimeout(() => {
            state.ws.send(JSON.stringify({ type: 'prompt', text }));
        }, 200);

        input.value = '';
        input.style.height = 'auto';
        state.chatUI.disableInput();
        return;
    }

    state.chatUI.addUserMessage(text);
    state.streamParser.userSent(text);

    // Track prompt history for up-arrow cycling
    if (!cmd) {
        state.promptHistory.push(text);
        state.historyIndex = state.promptHistory.length;
    }

    if (cmd) {
        const finalText = buildCommandText(cmd);
        cmd.raw = finalText;
        state.ws.send(JSON.stringify(cmd));
    } else {
        state.ws.send(JSON.stringify({ type: 'prompt', text }));
    }

    input.value = '';
    input.style.height = 'auto';
    state.chatUI.disableInput();
}

function handleInteractiveInputInColumn(state, text) {
    const { cmd, prompt, tasks } = state.pendingInteractiveCmd;

    if (prompt.field === 'both') {
        const parts = text.trim().split(/\s+/);
        if (parts.length < 2 || !looksLikeInterval(parts[0])) {
            state.chatUI.addStatusMessage('Invalid format. Use: 30m check email', 'status-error');
            return;
        }
        cmd.args.interval = parts[0];
        cmd.args.description = parts.slice(1).join(' ');
    } else if (prompt.field === 'interval') {
        if (!looksLikeInterval(text.trim())) {
            state.chatUI.addStatusMessage('Invalid interval. Use: 30m, 2h, 1d', 'status-error');
            return;
        }
        cmd.args.interval = text.trim();
    } else if (prompt.field === 'description') {
        cmd.args.description = text.trim();
    } else if (prompt.field === 'deleterecurring_select') {
        const num = parseInt(text.trim(), 10);
        if (isNaN(num) || num < 0 || num > tasks.length) {
            state.chatUI.addStatusMessage(`Invalid selection. Enter 1-${tasks.length} or 0 to cancel.`, 'status-error');
            return;
        }
        if (num === 0) {
            state.pendingInteractiveCmd = null;
            state.chatUI.addStatusMessage('Cancelled.', 'status-info');
            return;
        }
        const selectedTask = tasks[num - 1];
        cmd.args.id = selectedTask.id;
    }

    state.pendingInteractiveCmd = null;

    if (cmd.command !== 'deleterecurring') {
        const nextPrompt = getInteractivePrompt(cmd);
        if (nextPrompt && !nextPrompt.async) {
            state.pendingInteractiveCmd = { cmd, prompt: nextPrompt };
            state.chatUI.addUserMessage(text);
            state.chatUI.addStatusMessage(nextPrompt.prompt, 'status-prompt');
            return;
        }
    }

    const finalText = buildCommandText(cmd);
    state.chatUI.addUserMessage(finalText);
    state.streamParser.userSent(finalText);
    cmd.raw = finalText;
    state.ws.send(JSON.stringify(cmd));
    state.chatUI.disableInput();
}

async function handleAsyncPromptInColumn(state, cmd, prompt) {
    if (prompt.field === 'deleterecurring') {
        try {
            const data = await api('GET', `/api/agents/${state.agentId}/recurring-tasks`);
            const tasks = data.tasks || [];

            if (tasks.length === 0) {
                state.chatUI.addStatusMessage('No recurring tasks found.', 'status-info');
                return;
            }

            let listText = 'Recurring tasks:\n';
            tasks.forEach((t, i) => {
                const intervalStr = formatInterval(t.interval_minutes);
                listText += `  ${i + 1}. [${intervalStr}] ${t.description}\n`;
            });
            state.chatUI.addStatusMessage(listText, 'status-info');

            state.pendingInteractiveCmd = {
                cmd,
                prompt: { field: 'deleterecurring_select', prompt: 'Enter task number to delete (0 to cancel):' },
                tasks
            };
            state.chatUI.addStatusMessage('Enter task number to delete (0 to cancel):', 'status-prompt');

        } catch (err) {
            state.chatUI.addStatusMessage(`Error fetching recurring tasks: ${err.message}`, 'status-error');
        }
    }
}

// ============================================================
// Command parsing helpers
// ============================================================

function parseCommand(text) {
    if (!text.startsWith('/')) return null;

    const parts = text.slice(1).split(/\s+/);
    const command = parts[0].toLowerCase();
    const argsText = parts.slice(1).join(' ');

    const args = {};
    switch (command) {
        case 'addtask':
            args.description = argsText;
            break;
        case 'addrecurring':
            if (parts.length > 1 && looksLikeInterval(parts[1])) {
                args.interval = parts[1];
                args.description = parts.slice(2).join(' ');
            } else {
                args.description = argsText;
            }
            break;
        case 'image':
        case 'file':
        case 'system':
            args.path = argsText;
            break;
        case 'approve':
        case 'reject':
        case 'deleterecurring':
            args.id = parts[1] || 'all';
            break;
        case 'telegram':
        case 'email':
        case 'whitelist':
            args.value = argsText;
            break;
        case 'smart':
            if (argsText === 'on' || argsText === 'off') {
                args.enabled = argsText === 'on';
            } else if (argsText === 'status' || argsText === 'state') {
                args.status = true;
            }
            break;
    }

    return { type: 'command', command, args, raw: text };
}

function looksLikeInterval(s) {
    if (!s || s.length < 2) return false;
    return /^(\d+[dhm])+$/i.test(s);
}

function getInteractivePrompt(cmd) {
    if (cmd.command === 'addrecurring') {
        if (!cmd.args.interval && !cmd.args.description) {
            return { field: 'both', prompt: 'Enter interval and description (e.g., "30m check email"):' };
        }
        if (!cmd.args.interval) {
            return { field: 'interval', prompt: 'Recurrence interval (e.g., 30m, 2h, 1d):' };
        }
        if (!cmd.args.description) {
            return { field: 'description', prompt: 'Task description:' };
        }
    }
    if (cmd.command === 'deleterecurring') {
        if (!cmd.args.id || cmd.args.id === 'all') {
            return { async: true, field: 'deleterecurring' };
        }
    }
    return null;
}

function buildCommandText(cmd) {
    if (cmd.command === 'addrecurring') {
        return `/addrecurring ${cmd.args.interval} ${cmd.args.description}`;
    }
    if (cmd.command === 'deleterecurring' && cmd.args.id) {
        return `/deleterecurring ${cmd.args.id}`;
    }
    return cmd.raw;
}

// ============================================================
// File Attach/Upload (per column)
// ============================================================

function attachFileInColumn(btn) {
    const col = btn.closest('.agent-column');
    const fileInput = col.querySelector('.file-input-hidden');
    fileInput.value = '';
    fileInput.click();
}

async function handleFileSelectedInColumn(input) {
    const col = input.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state) return;

    const file = input.files[0];
    if (!file) return;

    const formData = new FormData();
    formData.append('file', file);

    state.chatUI.addStatusMessage(`Uploading ${file.name}...`, 'status-info');

    try {
        const resp = await fetch(`/api/agents/${agentId}/upload`, {
            method: 'POST',
            body: formData,
        });
        const data = await resp.json();
        if (!resp.ok) throw new Error(data.error || 'Upload failed');

        state.pendingFile = { path: data.path, name: data.name, size: data.size };

        // Show file indicator
        const indicator = col.querySelector('.file-indicator');
        const nameSpan = col.querySelector('.file-indicator-name');
        const sizeStr = data.size < 1024 ? data.size + 'B'
            : data.size < 1024 * 1024 ? Math.round(data.size / 1024) + 'KB'
            : (data.size / (1024 * 1024)).toFixed(1) + 'MB';
        nameSpan.textContent = `File attached: ${data.name} (${sizeStr})`;
        indicator.style.display = '';
    } catch (e) {
        state.chatUI.addErrorMessage('Upload failed: ' + e.message);
    }
}

function removeAttachedFile(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) state.pendingFile = null;

    col.querySelector('.file-indicator').style.display = 'none';
}

// ============================================================
// Tab Switching (per column)
// ============================================================

function switchColumnTab(tabEl, tabName) {
    const col = tabEl.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);

    col.querySelectorAll('.tab').forEach(t => {
        t.classList.toggle('active', t.dataset.tab === tabName);
    });
    col.querySelectorAll('.tab-panel').forEach(p => {
        p.classList.toggle('active', p.dataset.panel === tabName);
    });

    // Stop logs polling when leaving the tab
    if (state && state.logsPollInterval) {
        clearInterval(state.logsPollInterval);
        state.logsPollInterval = null;
    }

    if (tabName === 'logs' && state) {
        refreshLogsInColumnState(state);
        state.logsPollInterval = setInterval(() => refreshLogsInColumnState(state), 3000);
    }
    if (tabName === 'config' && state) {
        loadRecurringTasksInColumn(state);
        loadCapabilitiesInColumn(state);
    }
    if (tabName === 'state' && state) {
        loadAgentStateInColumn(state);
    }
    if (tabName === 'context' && state) {
        loadAgentContextInColumn(state);
    }
    if (tabName === 'report' && state) {
        loadReportInColumn(state);
    }
    if (tabName === 'knowledge' && state) {
        loadKGNodesInColumnState(state);
    }
}

// ============================================================
// Logs (per column)
// ============================================================

function refreshLogsInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) refreshLogsInColumnState(state);
}

async function refreshLogsInColumnState(state) {
    const tail = state.columnEl.querySelector('.log-tail').value || 200;
    try {
        const data = await api('GET', `/api/agents/${state.agentId}/logs?tail=${tail}`);
        const raw = data.logs || 'No logs available.';
        state.logsAnsi.reset();
        const html = colorizeLogLines(state.logsAnsi, raw);
        const panel = state.columnEl.querySelector('.logs-content');
        panel.innerHTML = html;
        panel.scrollTop = panel.scrollHeight;
    } catch (e) {
        state.columnEl.querySelector('.logs-content').textContent = 'Error: ' + e.message;
    }
}

function classifyLogLine(text) {
    const plain = text.replace(/\x1b\[[0-9;]*m/g, '');
    if (/error|ERROR|panic|PANIC|fatal|FATAL|failed|FAILED/i.test(plain)) return 'log-line-error';
    if (/you>/.test(plain)) return 'log-line-user';
    if (/\[(calling |.*completed|.*failed)\]/.test(plain)) return 'log-line-tool';
    if (/```/.test(plain) || /^\s*(func |def |class |import |from |const |let |var |if |for |return )/.test(plain)) return 'log-line-code';
    return '';
}

function parseLogJSONLine(text) {
    const plain = String(text || '').replace(/\x1b\[[0-9;]*m/g, '').trim();
    if (!plain) return null;
    if (!(plain.startsWith('{') && plain.endsWith('}')) && !(plain.startsWith('[') && plain.endsWith(']'))) {
        return null;
    }
    try {
        return JSON.parse(plain);
    } catch (e) {
        return null;
    }
}

function classifyStructuredLogValue(value) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return 'log-line-json';
    const type = String(value.type || '').toLowerCase();
    switch (type) {
        case 'prompt':
            return 'log-line-user';
        case 'tool_call':
        case 'tool_result':
            return 'log-line-tool';
        case 'response':
        case 'response_end':
            return 'log-line-reply';
        case 'error':
            return 'log-line-error';
        case 'system':
            if (value.content) {
                const statusClass = systemMessageStatusClass(String(value.content));
                if (statusClass === 'status-error') return 'log-line-error';
                if (statusClass === 'status-warn') return 'log-line-tool';
            }
            return 'log-line-json';
        default:
            return 'log-line-json';
    }
}

function renderPrettyLogJSON(value, cssClass) {
    const pretty = JSON.stringify(value, null, 2);
    const cls = cssClass || 'log-line-json';
    return `<div class="log-json-block ${cls}">${escHtml(pretty)}</div>`;
}

function colorizeLogLines(ansi, raw) {
    const lines = raw.replace(/\r\n/g, '\n').replace(/\r/g, '').split('\n');
    let inReply = false;
    const result = [];

    for (const line of lines) {
        const plain = line.replace(/\x1b\[[0-9;]*m/g, '');
        const parsedJSON = parseLogJSONLine(line);
        if (parsedJSON !== null) {
            const cls = classifyStructuredLogValue(parsedJSON);
            result.push(renderPrettyLogJSON(parsedJSON, cls));
            if (cls === 'log-line-user') {
                inReply = true;
            } else if (cls === 'log-line-tool') {
                inReply = false;
            }
            continue;
        }
        const cls = classifyLogLine(line);

        // Track agent reply regions: after a prompt until next you> or tool call
        if (/you>/.test(plain)) {
            inReply = false;
        } else if (cls === 'log-line-tool') {
            inReply = false;
        } else if (cls === '' && !inReply && plain.trim() && !/^\[DEBUG\]/.test(plain) && !/^\s*$/.test(plain)) {
            // Heuristic: non-empty, non-debug, non-classified lines after user prompt are agent replies
            // We start reply mode when we see content after a blank classified section
        }

        const effectiveCls = cls || (inReply ? 'log-line-reply' : '');
        const html = ansi.convert(line);

        if (effectiveCls) {
            result.push(`<span class="${effectiveCls}">${html}</span>`);
        } else {
            result.push(html);
        }

        // After user prompt line, next non-tool non-error content is agent reply
        if (cls === 'log-line-user') {
            inReply = true;
        }
    }

    return result.join('\n');
}

// ============================================================
// Agent Report (per column)
// ============================================================

function refreshReportInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) loadReportInColumn(state);
}

function buildReportSrcDoc(html) {
    if (!html) return '';
    const trimmed = html.trim();
    if (/<!doctype|<html\b/i.test(trimmed)) return trimmed;
    return `<!doctype html><html><head><meta charset=\"utf-8\"><style>\n` +
        `body{font-family:-apple-system,BlinkMacSystemFont,\"Segoe UI\",Helvetica,Arial,sans-serif;` +
        `margin:0;padding:16px;color:#111;background:#fff;}\n` +
        `img,svg,video,canvas{max-width:100%;height:auto;}\n` +
        `table{border-collapse:collapse;max-width:100%;}\n` +
        `td,th{border:1px solid #ddd;padding:6px 8px;text-align:left;}\n` +
        `code,pre{font-family:\"SFMono-Regular\",Consolas,\"Liberation Mono\",Menlo,monospace;}\n` +
        `</style></head><body>` + html + `</body></html>`;
}

async function loadReportInColumn(state) {
    const col = state.columnEl;
    const frame = col.querySelector('.report-frame');
    const empty = col.querySelector('.report-empty');
    const updated = col.querySelector('.report-updated');

    try {
        const data = await api('GET', `/api/agents/${state.agentId}/report`);
        const html = (data.html || '').trim();

        if (!html) {
            frame.srcdoc = '';
            frame.style.display = 'none';
            empty.textContent = 'No report available.';
            empty.style.display = '';
            updated.textContent = '';
            return;
        }

        frame.srcdoc = buildReportSrcDoc(html);
        frame.style.display = '';
        empty.style.display = 'none';
        updated.textContent = data.updated_at ? formatDate(data.updated_at) : '';
    } catch (e) {
        frame.srcdoc = '';
        frame.style.display = 'none';
        empty.textContent = 'Error loading report: ' + e.message;
        empty.style.display = '';
        updated.textContent = '';
    }
}

// ============================================================
// Agent State (per column)
// ============================================================

function refreshStateInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) loadAgentStateInColumn(state);
}

async function loadAgentStateInColumn(state) {
    const col = state.columnEl;

    const [soulData, tasksData] = await Promise.all([
        api('GET', `/api/agents/${state.agentId}/soul`).catch(() => ({ content: '' })),
        api('GET', `/api/agents/${state.agentId}/tasks`).catch(() => ({ tasks: [] })),
    ]);

    const soulText = col.querySelector('.soul-text');
    const soulUpdated = col.querySelector('.soul-updated');
    if (soulData.content) {
        soulText.textContent = soulData.content;
        soulUpdated.textContent = soulData.updated_at ? formatDate(soulData.updated_at) : '';
    } else {
        soulText.textContent = 'No soul defined';
        soulUpdated.textContent = '';
    }

    // Tasks
    const taskList = col.querySelector('.task-list');
    const tasksCount = col.querySelector('.tasks-count');
    const tasks = tasksData.tasks || [];
    tasksCount.textContent = `(${tasks.length})`;

    if (tasks.length === 0) {
        taskList.innerHTML = '<div class="state-empty">No pending tasks</div>';
    } else {
        // Group tasks: top-level and subtasks by parent
        const topLevel = [];
        const subtasksByParent = {};
        const parentIDs = new Set();
        for (const t of tasks) {
            if (t.parent_task_id) {
                if (!subtasksByParent[t.parent_task_id]) subtasksByParent[t.parent_task_id] = [];
                subtasksByParent[t.parent_task_id].push(t);
                parentIDs.add(t.parent_task_id);
            } else {
                topLevel.push(t);
            }
        }

        let num = 0;
        let html = '';
        for (const t of topLevel) {
            num++;
            const isParent = parentIDs.has(t.id);
            html += renderTaskEntry(t, num, false, isParent);
            // Render subtasks indented
            if (subtasksByParent[t.id]) {
                for (const st of subtasksByParent[t.id]) {
                    html += renderTaskEntry(st, null, true, false);
                }
            }
        }
        // Orphan subtasks whose parent isn't in pending list
        for (const parentId of Object.keys(subtasksByParent)) {
            if (!topLevel.find(t => t.id === parentId)) {
                for (const st of subtasksByParent[parentId]) {
                    html += renderTaskEntry(st, null, true, false);
                }
            }
        }
        taskList.innerHTML = html;
    }
}

// ============================================================
// Full Context (per column)
// ============================================================

function refreshContextInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) loadAgentContextInColumn(state);
}

async function loadAgentContextInColumn(state) {
    requestContextDump(state, true);
}

function requestContextDump(state, showLoading) {
    const col = state.columnEl;
    const textEl = col.querySelector('.context-dump-text');
    const metaEl = col.querySelector('.context-dump-meta');

    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
        if (textEl) textEl.textContent = 'Agent not connected';
        if (metaEl) metaEl.textContent = '';
        return;
    }

    if (showLoading && textEl) {
        textEl.textContent = 'Loading agent context...';
    }
    if (metaEl) metaEl.textContent = '';

    const msg = {
        type: 'command',
        command: 'contextdump',
        args: {},
    };
    state.ws.send(JSON.stringify(msg));
}

function handleContextDumpMessage(state, msg) {
    const col = state.columnEl;
    const textEl = col.querySelector('.context-dump-text');
    const metaEl = col.querySelector('.context-dump-meta');
    if (!textEl || !metaEl) return;

    let dump = null;
    try {
        dump = JSON.parse(msg.content || '{}');
    } catch (e) {
        textEl.textContent = 'Error parsing context dump';
        metaEl.textContent = '';
        return;
    }

    if (!dump || !dump.messages) {
        textEl.textContent = 'No context loaded';
        metaEl.textContent = '';
        return;
    }

    const msgCount = dump.message_count || (dump.messages ? dump.messages.length : 0);
    const metaParts = [];
    if (dump.generated_at) metaParts.push(`updated ${formatDate(dump.generated_at)}`);
    if (msgCount) metaParts.push(`${msgCount} messages`);
    if (dump.tool_outputs_full !== undefined || dump.tool_outputs_masked !== undefined) {
        const full = dump.tool_outputs_full || 0;
        const masked = dump.tool_outputs_masked || 0;
        metaParts.push(`tools ${full} full, ${masked} masked`);
    }
    if (dump.approx_tokens) metaParts.push(`~${dump.approx_tokens} tokens`);
    metaEl.textContent = metaParts.join(' · ');

    textEl.textContent = formatContextDump(dump);
}

function formatContextDump(dump) {
    const lines = [];
    const messages = dump.messages || [];

    for (const msg of messages) {
        const idx = msg.index !== undefined ? msg.index + 1 : '';
        const role = (msg.role || 'unknown').toUpperCase();
        lines.push(`--- ${role}${idx ? ' #' + idx : ''} ---`);

        const parts = msg.parts || [];
        for (const part of parts) {
            if (part.type === 'text') {
                lines.push(part.text || '');
                continue;
            }
            if (part.type === 'tool_call') {
                const args = part.args ? JSON.stringify(part.args, null, 2) : '';
                lines.push(`[tool_call ${part.tool_name || 'unknown'}]${args ? '\n' + args : ''}`);
                continue;
            }
            if (part.type === 'tool_result') {
                if (part.masked) {
                    const summary = part.summary ? String(part.summary) : '[masked]';
                    lines.push(`[tool_result ${part.tool_name || 'unknown'}] [MASKED] ${summary}`);
                } else if (part.response) {
                    const resp = JSON.stringify(part.response, null, 2);
                    lines.push(`[tool_result ${part.tool_name || 'unknown'}]\n${resp}`);
                } else {
                    lines.push(`[tool_result ${part.tool_name || 'unknown'}] (no response)`);
                }
                continue;
            }
            if (part.type === 'inline_data') {
                const bytes = part.bytes ? ` (${part.bytes} bytes)` : '';
                lines.push(`[inline_data ${part.mime_type || 'unknown'}]${bytes}`);
                continue;
            }
            lines.push(`[part ${part.type || 'unknown'}]`);
        }

        lines.push('');
    }

    return lines.join('\n').trim() || 'No context loaded';
}

function renderTaskEntry(t, num, isSubtask, isParent) {
    let badges = '';
    if (t.blocked) badges += '<span class="task-badge task-blocked">blocked</span>';
    if (t.sleep_until) {
        const sleepDate = new Date(t.sleep_until);
        if (sleepDate > new Date()) {
            badges += `<span class="task-badge task-sleeping">sleeping until ${formatDate(t.sleep_until)}</span>`;
        }
    }
    const cls = isSubtask ? 'task-entry task-subtask' : 'task-entry';
    const pos = isSubtask ? '<span class="task-position">◦</span>' : `<span class="task-position">${isParent ? '▸' : ''} ${num}.</span>`;
    return `<div class="${cls}">
        ${pos}
        <span class="task-description">${escHtml(t.description)}</span>
        ${badges}
    </div>`;
}

function toggleSectionInColumn(headerEl, name) {
    const section = headerEl.closest('.state-section');
    const content = section.querySelector('.state-section-content');
    const toggle = headerEl.querySelector('.section-toggle');

    if (content.style.display === 'none') {
        content.style.display = '';
        toggle.textContent = '▼';
    } else {
        content.style.display = 'none';
        toggle.textContent = '▶';
    }
}

// ============================================================
// Knowledge Graph (per column)
// ============================================================

function loadKGNodesInColumn(el) {
    const col = el.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) loadKGNodesInColumnState(state);
}

async function loadKGNodesInColumnState(state) {
    const col = state.columnEl;
    const typeFilter = col.querySelector('.kg-type-filter').value;
    const url = typeFilter
        ? `/api/agents/${state.agentId}/kg/nodes?type=${encodeURIComponent(typeFilter)}`
        : `/api/agents/${state.agentId}/kg/nodes`;

    try {
        const data = await api('GET', url);
        state.kgNodes = data.nodes || [];

        const edgeData = await api('GET', `/api/agents/${state.agentId}/kg/edges`);
        state.kgEdges = edgeData.edges || [];

        updateTypeFilterInColumn(state);
        renderKGNodesInColumn(state);
    } catch (e) {
        col.querySelector('.kg-nodes-list').innerHTML =
            `<div class="kg-empty">Error loading nodes: ${escHtml(e.message)}</div>`;
    }
}

function updateTypeFilterInColumn(state) {
    const types = [...new Set(state.kgNodes.map(n => n.type))].sort();
    const filter = state.columnEl.querySelector('.kg-type-filter');
    const currentValue = filter.value;

    filter.innerHTML = '<option value="">All Types</option>' +
        types.map(t => `<option value="${escHtml(t)}">${escHtml(t)}</option>`).join('');

    if (types.includes(currentValue)) {
        filter.value = currentValue;
    }
}

function renderKGNodesInColumn(state) {
    const list = state.columnEl.querySelector('.kg-nodes-list');

    if (state.kgNodes.length === 0) {
        list.innerHTML = '<div class="kg-empty">No nodes in knowledge graph</div>';
        return;
    }

    const byType = {};
    for (const n of state.kgNodes) {
        if (!byType[n.type]) byType[n.type] = [];
        byType[n.type].push(n);
    }

    let html = '';
    for (const type of Object.keys(byType).sort()) {
        html += `<div class="kg-type-group">
            <div class="kg-type-header">${escHtml(type)} (${byType[type].length})</div>`;
        for (const n of byType[type]) {
            const selected = n.id === state.selectedNodeID ? ' selected' : '';
            html += `<div class="kg-node-item${selected}" onclick="selectKGNodeInColumn(this, '${n.id}')">
                <span class="kg-node-name">${escHtml(n.name)}</span>
            </div>`;
        }
        html += '</div>';
    }

    list.innerHTML = html;
}

async function selectKGNodeInColumn(el, nodeID) {
    const col = el.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state) return;

    state.selectedNodeID = nodeID;
    renderKGNodesInColumn(state);

    const detailContent = col.querySelector('.kg-detail-content');
    detailContent.innerHTML = '<div class="kg-loading">Loading...</div>';

    try {
        const data = await api('GET', `/api/agents/${state.agentId}/kg/node/${nodeID}`);
        renderNodeDetailInColumn(state, data.node, data.neighbors || []);
    } catch (e) {
        detailContent.innerHTML = `<div class="kg-error">Error: ${escHtml(e.message)}</div>`;
    }
}

function renderNodeDetailInColumn(state, node, neighbors) {
    const detailContent = state.columnEl.querySelector('.kg-detail-content');

    let html = `
        <div class="kg-node-detail">
            <div class="kg-detail-name">${escHtml(node.name)}</div>
            <div class="kg-detail-type">${escHtml(node.type)}</div>
            <div class="kg-detail-id">${node.id}</div>
        </div>`;

    if (node.notes) {
        html += `<div class="kg-detail-section">
            <div class="kg-section-title">Notes</div>
            <div class="kg-notes">${escHtml(node.notes)}</div>
        </div>`;
    }

    if (node.properties && Object.keys(node.properties).length > 0) {
        html += '<div class="kg-detail-section"><div class="kg-section-title">Properties</div>';
        html += '<div class="kg-properties">';
        for (const [key, value] of Object.entries(node.properties)) {
            html += `<div class="kg-property">
                <span class="kg-prop-key">${escHtml(key)}:</span>
                <span class="kg-prop-value">${escHtml(JSON.stringify(value))}</span>
            </div>`;
        }
        html += '</div></div>';
    }

    if (neighbors.length > 0) {
        const outgoing = neighbors.filter(n => n.edge.direction === 'outgoing');
        const incoming = neighbors.filter(n => n.edge.direction === 'incoming');

        if (outgoing.length > 0) {
            html += '<div class="kg-detail-section"><div class="kg-section-title">Outgoing Relations</div>';
            html += outgoing.map(n => `
                <div class="kg-neighbor" onclick="selectKGNodeInColumn(this, '${n.node.id}')">
                    <span class="kg-relation">—[${escHtml(n.edge.relation_type)}]→</span>
                    <span class="kg-neighbor-name">${escHtml(n.node.name)}</span>
                    <span class="kg-neighbor-type">(${escHtml(n.node.type)})</span>
                </div>
            `).join('');
            html += '</div>';
        }

        if (incoming.length > 0) {
            html += '<div class="kg-detail-section"><div class="kg-section-title">Incoming Relations</div>';
            html += incoming.map(n => `
                <div class="kg-neighbor" onclick="selectKGNodeInColumn(this, '${n.node.id}')">
                    <span class="kg-relation">←[${escHtml(n.edge.relation_type)}]—</span>
                    <span class="kg-neighbor-name">${escHtml(n.node.name)}</span>
                    <span class="kg-neighbor-type">(${escHtml(n.node.type)})</span>
                </div>
            `).join('');
            html += '</div>';
        }
    } else {
        html += '<div class="kg-detail-section"><div class="kg-empty">No connections</div></div>';
    }

    html += `<div class="kg-detail-section kg-timestamps">
        <div>Created: ${formatDate(node.created_at)}</div>
        <div>Updated: ${formatDate(node.updated_at)}</div>
    </div>`;

    detailContent.innerHTML = html;
}

function handleKGSearchInColumn(el, event) {
    if (event.key !== 'Enter') return;

    const col = el.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state) return;

    const query = el.value.trim();
    if (!query) {
        loadKGNodesInColumnState(state);
        return;
    }

    api('GET', `/api/agents/${state.agentId}/kg/search?q=${encodeURIComponent(query)}`)
        .then(data => {
            state.kgNodes = data.nodes || [];
            renderKGNodesInColumn(state);
        })
        .catch(e => {
            col.querySelector('.kg-nodes-list').innerHTML =
                `<div class="kg-empty">Error: ${escHtml(e.message)}</div>`;
        });
}

// ============================================================
// Recurring Tasks (per column)
// ============================================================

async function loadRecurringTasksInColumn(state) {
    const listEl = state.columnEl.querySelector('.recurring-tasks-list');
    if (!listEl) return;

    try {
        const data = await api('GET', `/api/agents/${state.agentId}/recurring-tasks`);
        const tasks = data.tasks || [];

        if (tasks.length === 0) {
            listEl.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No recurring tasks</div>';
            return;
        }

        listEl.innerHTML = tasks.map(t => {
            const intervalStr = formatInterval(t.interval_minutes);
            const nextDue = new Date(t.next_due);
            const now = new Date();
            const isOverdue = nextDue < now;
            const nextDueStr = formatRelativeTime(nextDue);
            const nextDueClass = isOverdue ? 'overdue' : '';
            const safeDescription = String(t.description || '')
                .replace(/\\/g, '\\\\')
                .replace(/'/g, "\\'")
                .replace(/\r?\n/g, '\\n');

            return `<div class="recurring-task-item" data-id="${t.id}">
                <div class="recurring-task-info">
                    <div class="recurring-task-desc">${escHtml(t.description)}</div>
                    <div class="recurring-task-meta">
                        <span class="recurring-task-interval">Every ${intervalStr}</span>
                        <span class="recurring-task-next ${nextDueClass}">Next: ${nextDueStr}</span>
                    </div>
                </div>
                <div class="recurring-task-actions">
                    <button class="btn btn-sm" onclick="editRecurringTaskInColumn(this, '${t.id}', '${safeDescription}', ${t.interval_minutes})">Edit</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteRecurringTaskInColumn(this, '${t.id}')">Delete</button>
                </div>
            </div>`;
        }).join('');
    } catch (e) {
        listEl.innerHTML = `<div style="color:var(--red);font-size:12px">Error: ${escHtml(e.message)}</div>`;
    }
}

function openRecurringTaskModal({ agentId, id = '', description = '', intervalMinutes = 60 } = {}) {
    const modal = document.getElementById('recurring-task-modal');
    if (!modal) return;

    const titleEl = modal.querySelector('.recurring-task-modal-title');
    const agentEl = modal.querySelector('.recurring-task-modal-agent');
    const idEl = modal.querySelector('.recurring-task-modal-id');
    const descEl = modal.querySelector('.recurring-task-modal-desc');
    const intervalEl = modal.querySelector('.recurring-task-modal-interval');
    const unitEl = modal.querySelector('.recurring-task-modal-unit');

    agentEl.value = agentId || '';
    idEl.value = id || '';
    descEl.value = description || '';

    let interval;
    let unit;
    if (intervalMinutes >= 1440 && intervalMinutes % 1440 === 0) {
        interval = intervalMinutes / 1440;
        unit = '1440';
    } else if (intervalMinutes >= 60 && intervalMinutes % 60 === 0) {
        interval = intervalMinutes / 60;
        unit = '60';
    } else {
        interval = intervalMinutes || 1;
        unit = '1';
    }

    intervalEl.value = interval;
    unitEl.value = unit;
    titleEl.textContent = id ? 'Edit Recurring Task' : 'Add Recurring Task';
    modal.style.display = 'flex';
    setTimeout(() => descEl.focus(), 0);
}

function closeRecurringTaskModal() {
    const modal = document.getElementById('recurring-task-modal');
    if (!modal) return;
    modal.style.display = 'none';
}

function showAddRecurringTaskInColumn(btn) {
    const col = btn.closest('.agent-column');
    if (!col) return;
    const agentId = col.getAttribute('data-agent-id');
    openRecurringTaskModal({ agentId });
}

function editRecurringTaskInColumn(btn, id, description, intervalMinutes) {
    const col = btn.closest('.agent-column');
    if (!col) return;
    const agentId = col.getAttribute('data-agent-id');
    openRecurringTaskModal({ agentId, id, description, intervalMinutes });
}

function hideRecurringTaskFormInColumn() {
    closeRecurringTaskModal();
}

async function saveRecurringTaskFromModal() {
    const modal = document.getElementById('recurring-task-modal');
    if (!modal) return;

    const agentId = modal.querySelector('.recurring-task-modal-agent').value;
    const state = displayedAgents.get(agentId);
    if (!state) return;

    const id = modal.querySelector('.recurring-task-modal-id').value;
    const description = modal.querySelector('.recurring-task-modal-desc').value.trim();
    const interval = parseInt(modal.querySelector('.recurring-task-modal-interval').value) || 0;
    const unit = parseInt(modal.querySelector('.recurring-task-modal-unit').value) || 1;
    const intervalMinutes = interval * unit;

    if (!description) {
        alert('Description is required');
        return;
    }
    if (intervalMinutes <= 0) {
        alert('Interval must be positive');
        return;
    }

    try {
        if (id) {
            await api('PUT', `/api/agents/${state.agentId}/recurring-tasks/${id}`, {
                description,
                interval_minutes: intervalMinutes
            });
        } else {
            await api('POST', `/api/agents/${state.agentId}/recurring-tasks`, {
                description,
                interval_minutes: intervalMinutes
            });
        }

        closeRecurringTaskModal();
        loadRecurringTasksInColumn(state);
    } catch (e) {
        alert('Failed to save: ' + e.message);
    }
}

async function deleteRecurringTaskInColumn(btn, id) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state) return;
    if (!confirm('Delete this recurring task?')) return;

    try {
        await api('DELETE', `/api/agents/${state.agentId}/recurring-tasks/${id}`);
        loadRecurringTasksInColumn(state);
    } catch (e) {
        alert('Failed to delete: ' + e.message);
    }
}

// ============================================================
// A2A Capabilities (per column)
// ============================================================

async function loadCapabilitiesInColumn(state) {
    const listEl = state.columnEl.querySelector('.capabilities-list');
    if (!listEl) return;

    try {
        const data = await api('GET', `/api/agents/${state.agentId}/capabilities`);
        const caps = data.capabilities || [];

        if (caps.length === 0) {
            listEl.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No capabilities configured</div>';
            return;
        }

        listEl.innerHTML = caps.map(c => {
            const inputSchemaJSON = c.input_schema ? escHtml(JSON.stringify(c.input_schema, null, 2)) : '';
            const outputSchemaJSON = c.output_schema ? escHtml(JSON.stringify(c.output_schema, null, 2)) : '';
            const instructions = String(c.instructions || '').trim();
            return `<div class="capability-item" data-id="${c.id}">
                <div class="capability-header">
                    <span class="capability-method">${escHtml(c.method)}</span>
                    <button class="btn btn-sm btn-danger" onclick="deleteCapabilityInColumn(this, '${c.id}')">Delete</button>
                </div>
                <div class="capability-role">Role: ${escHtml(c.role)}</div>
                <div class="capability-desc">${escHtml(c.description || '')}</div>
                ${instructions ? `<details class="capability-schema"><summary>Method instructions</summary><pre>${escHtml(instructions)}</pre></details>` : ''}
                ${inputSchemaJSON ? `<details class="capability-schema"><summary>Input schema</summary><pre>${inputSchemaJSON}</pre></details>` : ''}
                ${outputSchemaJSON ? `<details class="capability-schema"><summary>Output schema</summary><pre>${outputSchemaJSON}</pre></details>` : ''}
            </div>`;
        }).join('');
    } catch (e) {
        listEl.innerHTML = `<div style="color:var(--red);font-size:12px">Error: ${escHtml(e.message)}</div>`;
    }
}

async function showAddCapabilityInColumn(btn) {
    const col = btn.closest('.agent-column');
    if (!col) return;
    const listEl = col.querySelector('.capabilities-list');
    if (!listEl) return;

    // Check if form already exists
    if (listEl.querySelector('.capability-add-form')) return;

    await loadA2AMethods();

    if (!a2aMethods.length) {
        const emptyHtml = `<div class="capability-add-form" style="padding:8px 10px;background:var(--bg);border:1px solid var(--accent);border-radius:6px;margin-bottom:6px">
            <div style="color:var(--text-muted);font-size:12px;margin-bottom:10px">No methods defined yet.</div>
            <div style="display:flex;gap:6px;justify-content:flex-end">
                <button class="btn btn-sm btn-primary" onclick="showA2AMethods()">Create Method</button>
                <button class="btn btn-sm" onclick="this.closest('.capability-add-form').remove()">Close</button>
            </div>
        </div>`;
        listEl.insertAdjacentHTML('afterbegin', emptyHtml);
        return;
    }

    let options = '<option value=\"\">Select method...</option>';
    options += a2aMethods.map(m => `<option value="${escAttr(m.method || '')}">${escHtml(m.method || '')}</option>`).join('');

    const formHtml = `<div class="capability-add-form" style="padding:8px 10px;background:var(--bg);border:1px solid var(--accent);border-radius:6px;margin-bottom:6px">
        <div class="form-group" style="margin-bottom:8px">
            <label style="font-size:11px;color:var(--text-muted)">Method</label>
            <select class="cap-method" onchange="capabilityAddFormMethodChanged(this)" style="width:100%;padding:4px 6px;background:var(--bg-light);border:1px solid var(--border);border-radius:4px;color:var(--text);font-size:12px">
                ${options}
            </select>
            <div class="cap-method-info" style="margin-top:6px"></div>
        </div>
        <div class="form-group" style="margin-bottom:8px">
            <label style="font-size:11px;color:var(--text-muted)">Role</label>
            <input type="text" class="cap-role" placeholder="e.g. fulfiller" style="width:100%;padding:4px 6px;background:var(--bg-light);border:1px solid var(--border);border-radius:4px;color:var(--text);font-size:12px">
        </div>
        <div style="display:flex;gap:6px;justify-content:flex-end">
            <button class="btn btn-sm btn-primary" onclick="saveCapabilityInColumn(this)">Save</button>
            <button class="btn btn-sm" onclick="this.closest('.capability-add-form').remove()">Cancel</button>
        </div>
    </div>`;

    listEl.insertAdjacentHTML('afterbegin', formHtml);
    const sel = listEl.querySelector('.cap-method');
    if (sel) sel.focus();
}

function capabilityAddFormMethodChanged(sel) {
    const form = sel.closest('.capability-add-form');
    if (!form) return;
    updateCapabilityAddFormMethodInfo(form);
}

function updateCapabilityAddFormMethodInfo(form) {
    const method = form.querySelector('.cap-method')?.value || '';
    const infoEl = form.querySelector('.cap-method-info');
    if (!infoEl) return;

    const m = a2aMethods.find(x => String(x.method || '') === String(method || ''));
    if (!m) {
        infoEl.innerHTML = '';
        return;
    }

    const desc = String(m.description || '').trim();
    const instructions = String(m.instructions || '').trim();
    const inputSchemaJSON = m.input_schema ? escHtml(JSON.stringify(m.input_schema, null, 2)) : '';
    const outputSchemaJSON = m.output_schema ? escHtml(JSON.stringify(m.output_schema, null, 2)) : '';

    infoEl.innerHTML = `${desc ? `<div style="font-size:11px;color:var(--text-muted);white-space:pre-wrap">${escHtml(desc)}</div>` : ''}
        ${instructions ? `<details class="capability-schema"><summary>Method instructions</summary><pre>${escHtml(instructions)}</pre></details>` : ''}
        ${inputSchemaJSON ? `<details class="capability-schema"><summary>Input schema</summary><pre>${inputSchemaJSON}</pre></details>` : ''}
        ${outputSchemaJSON ? `<details class="capability-schema"><summary>Output schema</summary><pre>${outputSchemaJSON}</pre></details>` : ''}`;
}

async function saveCapabilityInColumn(btn) {
    const form = btn.closest('.capability-add-form');
    const col = btn.closest('.agent-column');
    if (!form || !col) return;

    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state) return;

    const method = form.querySelector('.cap-method').value.trim();
    const role = form.querySelector('.cap-role').value.trim();

    if (!method) { alert('Method is required'); return; }
    if (!role) { alert('Role is required'); return; }

    const payload = { role, method };

    try {
        await api('POST', `/api/agents/${agentId}/capabilities`, payload);
        loadCapabilitiesInColumn(state);
    } catch (e) {
        alert('Failed to save: ' + e.message);
    }
}

async function deleteCapabilityInColumn(btn, capId) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state) return;
    if (!confirm('Delete this capability?')) return;

    try {
        await api('DELETE', `/api/agents/${agentId}/capabilities/${capId}`);
        loadCapabilitiesInColumn(state);
    } catch (e) {
        alert('Failed to delete: ' + e.message);
    }
}

// ============================================================
// Config save (per column)
// ============================================================

async function saveConfigInColumn(form, event) {
    event.preventDefault();
    const col = form.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const configBody = buildConfigBody(col);
    if (configBody == null) return;

    const telegramToken = col.querySelector('.cfg-telegram-token')?.value || '';
    const body = {
        ...configBody,
        telegram_bot_token: telegramToken,
    };

    try {
        await api('PUT', `/api/agents/${agentId}`, body);
        await refreshAgents();
        const agent = agents.find(a => a.id === agentId);
        if (agent) {
            populateConfigTab(col, agent);
            const state = displayedAgents.get(agentId);
            if (state) state.emailEnabled = !agent.enabled_tools || agent.enabled_tools.includes('email');
        }
    } catch (e) {
        alert('Failed to save: ' + e.message);
    }
}

// ============================================================
// System Prompt save (per column)
// ============================================================

async function saveSystemPromptInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const configBody = buildConfigBody(col);
    if (configBody == null) return;

    const telegramToken = col.querySelector('.cfg-telegram-token')?.value || '';
    const body = {
        ...configBody,
        telegram_bot_token: telegramToken,
    };

    try {
        await api('PUT', `/api/agents/${agentId}`, body);
        await refreshAgents();
        const agent = agents.find(a => a.id === agentId);
        if (agent) {
            populateConfigTab(col, agent);
        }
        btn.textContent = 'Saved!';
        setTimeout(() => { btn.textContent = 'Save'; }, 1500);
    } catch (e) {
        alert('Failed to save: ' + e.message);
    }
}

// Clear telegram token for an agent via the config form.
async function clearTelegramTokenInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    if (!confirm('Remove the Telegram bot token for this agent?')) return;
    try {
        // Send the current config with the CLEAR sentinel to remove the token
        const agent = agents.find(a => a.id === agentId);
        if (!agent) return;
        const configBody = buildConfigBody(col);
        if (configBody == null) return;
        await api('PUT', `/api/agents/${agentId}`, { ...configBody, telegram_bot_token: 'CLEAR' });
        await refreshAgents();
        const updated = agents.find(a => a.id === agentId);
        if (updated) populateConfigTab(col, updated);
    } catch (e) {
        alert('Failed to clear token: ' + e.message);
    }
}

// Build the standard config body from form fields (shared by save functions).
function buildConfigBody(col) {
    let envVars = null;
    const envStr = col.querySelector('.cfg-env-vars').value.trim();
    if (envStr) {
        try {
            envVars = JSON.parse(envStr);
        } catch (e) {
            alert('Invalid JSON in environment variables');
            return null;
        }
    }
    const extraFlagsInput = col.querySelector('.cfg-extra-flags');
    const enabledTools = collectEnabledTools(col);
    const body = {
        model_provider: col.querySelector('.cfg-model-provider').value,
        openai_auth_mode: col.querySelector('.cfg-openai-auth-mode').value,
        model: col.querySelector('.cfg-model').value,
        smart_model: col.querySelector('.cfg-smart-model').value,
        smart_default: col.querySelector('.cfg-smart-default').checked,
        mode: 'interactive',
        worker_context_mode: 'stateless',
        max_turns: parseInt(col.querySelector('.cfg-max-turns').value) || 100,
        heartbeat: col.querySelector('.cfg-heartbeat').value,
        work_tasks_timeout: col.querySelector('.cfg-worktasks-timeout').value,
        env_vars: envVars,
        memory_limit: col.querySelector('.cfg-memory-limit').value,
        cpu_limit: col.querySelector('.cfg-cpu-limit').value,
        auto_start: col.querySelector('.cfg-auto-start').checked,
        system_prompt: col.querySelector('.cfg-system-prompt').value,
        ...(extraFlagsInput ? { extra_flags: extraFlagsInput.value } : {}),
    };
    if (enabledTools !== null) body.enabled_tools = enabledTools;
    return body;
}

// ============================================================
// Agent lifecycle (per column)
// ============================================================

function setAutoplayState(columnEl, enabled) {
    const toggle = columnEl.querySelector('.autoplay-toggle');
    const input = toggle ? toggle.querySelector('.autoplay-toggle-input') : null;
    if (input) input.checked = !!enabled;
    columnEl.dataset.autoplayAudio = enabled ? 'true' : 'false';
    if (toggle) {
        toggle.title = enabled ? 'Autoplay audio ON' : 'Autoplay audio OFF';
    }
}

function toggleAutoplayInColumn(input) {
    const col = input.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (state) {
        state.autoPlayAudio = input.checked;
    }
    setAutoplayState(col, input.checked);
}

// Update the smart toggle switch visual state.
function updateSmartToggle(columnEl, isActive) {
    const toggle = columnEl.querySelector('.smart-toggle');
    if (!toggle) return;
    toggle.classList.remove('unknown');
    const input = toggle.querySelector('.smart-toggle-input');
    if (input) input.checked = !!isActive;
    toggle.title = isActive ? 'Smart mode ON – click to disable' : 'Smart mode OFF – click to enable';
}

// Apply a RuntimeStatus snapshot (from REST) to restore UI state on reconnect.
function applyRuntimeStatus(state, status) {
    if (!status) return;
    // Restore input field if agent is idle (waiting for input)
    if (status.state === 'idle') {
        state.chatUI.enableInput();
    }
    // Restore smart mode toggle
    if (status.smart_mode !== undefined) {
        updateSmartToggle(state.columnEl, status.smart_mode);
    }
}

function markSmartToggleUnknown(columnEl) {
    const toggle = columnEl.querySelector('.smart-toggle');
    if (!toggle) return;
    const input = toggle.querySelector('.smart-toggle-input');
    if (input) input.checked = false;
    toggle.classList.add('unknown');
    toggle.title = 'Smart mode unknown (connect to agent)';
}

// Toggle smart mode on a running agent by sending the /smart command.
function toggleSmartModeInColumn(input) {
    const col = input.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state || !state.ws || state.ws.readyState !== WebSocket.OPEN) {
        // Revert the checkbox — agent not running
        input.checked = !input.checked;
        return;
    }
    // Send the /smart command to the agent — toggle state will update
    // when the agent's smart_mode message arrives via WebSocket.
    state.ws.send(JSON.stringify({ type: 'command', command: 'smart', args: {} }));
}

async function startAgentInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);

    try {
        await api('POST', `/api/agents/${agentId}/start`);
        await refreshAgents();
        if (state) {
            connectColumnTerminal(state);
            checkPendingEmails(state);
        }
    } catch (e) {
        alert('Failed to start: ' + e.message);
    }
}

async function stopAgentInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);

    try {
        if (state) {
            if (state.ws) {
                state.ws.close();
                state.ws = null;
            }
            state.chatUI.hideInput();
        }
        await api('POST', `/api/agents/${agentId}/stop`);
        await refreshAgents();
    } catch (e) {
        alert('Failed to stop: ' + e.message);
    }
}

async function grantPremiumInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    btn.disabled = true;
    btn.textContent = 'Granting...';
    try {
        const result = await api('POST', `/api/agents/${agentId}/premium`);
        const section = col.querySelector('.agent-net-section');
        if (section) {
            const pubkeyEl = section.querySelector('.agent-net-pubkey');
            const badge = section.querySelector('.agent-net-premium-badge');
            if (result.public_key) pubkeyEl.textContent = result.public_key;
            badge.style.display = '';
            btn.style.display = 'none';
        }
    } catch (e) {
        alert('Failed to grant premium: ' + e.message);
        btn.disabled = false;
        btn.textContent = 'Grant Premium';
    }
}

async function restartAgentInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);

    try {
        if (state && state.ws) {
            state.ws.close();
            state.ws = null;
        }
        await api('POST', `/api/agents/${agentId}/restart`);
        await refreshAgents();
        if (state) {
            connectColumnTerminal(state);
        }
    } catch (e) {
        alert('Failed to restart: ' + e.message);
    }
}

async function cloneAgentInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const name = prompt('Enter name for the cloned agent:');
    if (!name || !name.trim()) return;

    try {
        const clone = await api('POST', `/api/agents/${agentId}/clone`, { name: name.trim() });
        await refreshAgents();
        addAgentColumn(clone.id);
    } catch (e) {
        alert('Failed to clone agent: ' + e.message);
    }
}

function sendAgentCommand(btn, command, args = {}) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);
    if (!state || !state.ws || state.ws.readyState !== WebSocket.OPEN) {
        alert('Agent is not running. Start the agent first.');
        return false;
    }
    state.ws.send(JSON.stringify({ type: 'command', command, args }));
    return true;
}

function clearContextInColumn(btn) {
    if (!confirm('Clear conversation history? This cannot be undone.')) return;
    sendAgentCommand(btn, 'clear');
}

function workTasksInColumn(btn) {
    sendAgentCommand(btn, 'worktasks');
}

async function refreshImageInColumn(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    const state = displayedAgents.get(agentId);

    const originalText = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Updating...';

    try {
        if (state && state.ws) {
            state.ws.close();
            state.ws = null;
        }
        await api('POST', `/api/agents/${agentId}/refresh-image`);
        await refreshAgents();
        if (state && (agents.find(a => a.id === agentId)?.container_status === 'running')) {
            connectColumnTerminal(state);
        }
    } catch (e) {
        alert('Failed to update image: ' + e.message);
    } finally {
        btn.disabled = false;
        btn.textContent = originalText;
    }
}

// ============================================================
// Docker management
// ============================================================

async function checkDocker() {
    try {
        const data = await api('GET', '/api/docker/status');
        const msg = `Docker: ${data.docker_available ? 'Available' : 'NOT AVAILABLE'}\nImage: ${data.image_exists ? 'Built' : 'Not built'}`;
        alert(msg);
    } catch (e) {
        alert('Error: ' + e.message);
    }
}

async function buildImage() {
    if (!confirm('Build the gowild-agent Docker image?')) return;
    try {
        await api('POST', '/api/docker/build');
        alert('Image built successfully');
    } catch (e) {
        alert('Build failed: ' + e.message);
    }
}

async function createNewAgent() {
    const name = prompt('Enter agent name:');
    if (!name || !name.trim()) return;

    try {
        const agent = await api('POST', '/api/agents', { name: name.trim() });
        await refreshAgents();
        addAgentColumn(agent.id);
    } catch (e) {
        alert('Failed to create agent: ' + e.message);
    }
}
