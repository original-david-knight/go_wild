// ============================================================
// MCP Servers UI
// ============================================================

function showMCPServers() {
    document.getElementById('mcp-servers-modal').style.display = 'flex';
    refreshMCPAgentOptions();
    loadMCPServers();
}

function closeMCPServers() {
    document.getElementById('mcp-servers-modal').style.display = 'none';
}

function refreshMCPAgentOptions() {
    const agentSelect = document.getElementById('mcp-agent-select');
    if (!agentSelect) return;
    agentSelect.innerHTML = '';
    for (const a of agents) {
        const opt = document.createElement('option');
        opt.value = a.id;
        opt.textContent = a.name || a.id;
        agentSelect.appendChild(opt);
    }
}

async function loadMCPServers() {
    try {
        const data = await api('GET', '/api/mcp-servers');
        mcpServers = data.servers || [];
    } catch (e) {
        mcpServers = [];
    }
    renderMCPServersList();
    refreshMCPServerOptions();
    const agentId = document.getElementById('mcp-agent-select')?.value;
    if (agentId) {
        await loadAgentMCPConfigs(agentId);
    }
}

function renderMCPServersList() {
    const container = document.getElementById('mcp-servers-list');
    if (!container) return;
    if (!mcpServers.length) {
        container.innerHTML = '<div style="color:var(--text-muted);text-align:center;padding:16px">No MCP servers yet.</div>';
        return;
    }
    container.innerHTML = mcpServers.map(s => {
        return `<div class="peer-group-card" style="margin-bottom:8px">
            <div style="display:flex;justify-content:space-between;gap:8px;align-items:center">
                <div>
                    <div style="font-weight:600">${escHtml(s.name || s.id)}</div>
                    <div style="font-size:12px;color:var(--text-muted)">${escHtml(s.id)}</div>
                    <div style="font-size:12px;color:var(--text-muted)">${escHtml(s.command || '')}</div>
                </div>
                <div style="display:flex;gap:6px">
                    <button class="btn btn-sm" onclick="editMCPServer('${escAttr(s.id)}')">Edit</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteMCPServer('${escAttr(s.id)}')">Delete</button>
                </div>
            </div>
        </div>`;
    }).join('');
}

function refreshMCPServerOptions() {
    const select = document.getElementById('mcp-server-select');
    if (!select) return;
    select.innerHTML = '';
    for (const s of mcpServers) {
        const opt = document.createElement('option');
        opt.value = s.id;
        opt.textContent = s.name || s.id;
        select.appendChild(opt);
    }
    handleMCPServerSelect();
}

function clearMCPServerForm() {
    document.getElementById('mcp-server-id').value = '';
    document.getElementById('mcp-server-name').value = '';
    document.getElementById('mcp-server-desc').value = '';
    document.getElementById('mcp-server-command').value = '';
    document.getElementById('mcp-server-args').value = '';
    document.getElementById('mcp-server-working-dir').value = '';
    document.getElementById('mcp-server-env').value = '';
}

function editMCPServer(id) {
    const server = mcpServers.find(s => s.id === id);
    if (!server) return;
    document.getElementById('mcp-server-id').value = server.id || '';
    document.getElementById('mcp-server-name').value = server.name || '';
    document.getElementById('mcp-server-desc').value = server.description || '';
    document.getElementById('mcp-server-command').value = server.command || '';
    document.getElementById('mcp-server-args').value = server.args ? JSON.stringify(server.args, null, 2) : '';
    document.getElementById('mcp-server-working-dir').value = server.working_dir || '';
    document.getElementById('mcp-server-env').value = server.default_env ? JSON.stringify(server.default_env, null, 2) : '';
}

async function saveMCPServer() {
    const id = document.getElementById('mcp-server-id').value.trim();
    const name = document.getElementById('mcp-server-name').value.trim();
    const description = document.getElementById('mcp-server-desc').value.trim();
    const command = document.getElementById('mcp-server-command').value.trim();
    const argsText = document.getElementById('mcp-server-args').value.trim();
    const workingDir = document.getElementById('mcp-server-working-dir').value.trim();
    const envText = document.getElementById('mcp-server-env').value.trim();

    if (!id) return alert('id is required');
    if (!name) return alert('name is required');
    if (!command) return alert('command is required');

    let args = undefined;
    if (argsText) {
        try {
            args = JSON.parse(argsText);
        } catch (e) {
            return alert('Invalid args JSON');
        }
    }
    let env = undefined;
    if (envText) {
        try {
            env = JSON.parse(envText);
        } catch (e) {
            return alert('Invalid env JSON');
        }
    }

    const body = { id, name, description, command, args, working_dir: workingDir, default_env: env };
    const existing = mcpServers.find(s => s.id === id);
    try {
        if (existing) {
            await api('PUT', `/api/mcp-servers/${id}`, body);
        } else {
            await api('POST', '/api/mcp-servers', body);
        }
        clearMCPServerForm();
        await loadMCPServers();
    } catch (e) {
        alert('Failed to save MCP server: ' + e.message);
    }
}

async function deleteMCPServer(id) {
    if (!confirm(`Delete MCP server ${id}?`)) return;
    try {
        await api('DELETE', `/api/mcp-servers/${id}`);
        await loadMCPServers();
    } catch (e) {
        alert('Failed to delete MCP server: ' + e.message);
    }
}

async function handleMCPAgentChange() {
    const agentId = document.getElementById('mcp-agent-select')?.value;
    if (!agentId) return;
    await loadAgentMCPConfigs(agentId);
}

function handleMCPServerSelect() {
    const serverId = document.getElementById('mcp-server-select')?.value;
    const cfg = serverId ? mcpAgentConfigs[serverId] : null;
    document.getElementById('mcp-agent-enabled').checked = cfg ? !!cfg.enabled : false;
    document.getElementById('mcp-agent-args').value = cfg?.args ? JSON.stringify(cfg.args, null, 2) : '';
    document.getElementById('mcp-agent-working-dir').value = cfg?.working_dir || '';
    document.getElementById('mcp-agent-env').value = cfg?.env ? JSON.stringify(cfg.env, null, 2) : '';
    renderAgentMCPConfigsList();
}

async function loadAgentMCPConfigs(agentId) {
    mcpAgentConfigs = {};
    try {
        const data = await api('GET', `/api/agents/${agentId}/mcp-servers`);
        (data.configs || []).forEach(c => { mcpAgentConfigs[c.server_id] = c; });
    } catch (e) {
        mcpAgentConfigs = {};
    }
    handleMCPServerSelect();
}

function renderAgentMCPConfigsList() {
    const container = document.getElementById('mcp-agent-configs-list');
    if (!container) return;
    const entries = Object.values(mcpAgentConfigs);
    if (!entries.length) {
        container.innerHTML = '<div style="color:var(--text-muted);text-align:center;padding:12px">No per-agent MCP configs yet.</div>';
        return;
    }
    container.innerHTML = entries.map(c => {
        const label = c.server_name || c.server_id;
        return `<div class="peer-group-card" style="margin-bottom:8px">
            <div style="display:flex;justify-content:space-between;gap:8px;align-items:center">
                <div>
                    <div style="font-weight:600">${escHtml(label)}</div>
                    <div style="font-size:12px;color:var(--text-muted)">${escHtml(c.server_id)}</div>
                </div>
                <span class="badge ${c.enabled ? 'running' : 'stopped'}" style="text-transform:uppercase">${c.enabled ? 'enabled' : 'disabled'}</span>
            </div>
        </div>`;
    }).join('');
}

async function saveAgentMCPConfig() {
    const agentId = document.getElementById('mcp-agent-select')?.value;
    const serverId = document.getElementById('mcp-server-select')?.value;
    if (!agentId || !serverId) return alert('Select agent and server');

    const enabled = document.getElementById('mcp-agent-enabled').checked;
    const argsText = document.getElementById('mcp-agent-args').value.trim();
    const workingDir = document.getElementById('mcp-agent-working-dir').value.trim();
    const envText = document.getElementById('mcp-agent-env').value.trim();

    let args = undefined;
    if (argsText) {
        try {
            args = JSON.parse(argsText);
        } catch (e) {
            return alert('Invalid args JSON');
        }
    }
    let env = undefined;
    if (envText) {
        try {
            env = JSON.parse(envText);
        } catch (e) {
            return alert('Invalid env JSON');
        }
    }

    try {
        await api('PUT', `/api/agents/${agentId}/mcp-servers/${serverId}`, {
            enabled,
            args,
            working_dir: workingDir,
            env,
        });
        await loadAgentMCPConfigs(agentId);
    } catch (e) {
        alert('Failed to save agent config: ' + e.message);
    }
}

async function deleteAgentMCPConfig() {
    const agentId = document.getElementById('mcp-agent-select')?.value;
    const serverId = document.getElementById('mcp-server-select')?.value;
    if (!agentId || !serverId) return alert('Select agent and server');
    if (!confirm('Remove config for this agent/server?')) return;
    try {
        await api('DELETE', `/api/agents/${agentId}/mcp-servers/${serverId}`);
        await loadAgentMCPConfigs(agentId);
    } catch (e) {
        alert('Failed to remove agent config: ' + e.message);
    }
}

async function testAgentMCPConfig() {
    const agentId = document.getElementById('mcp-agent-select')?.value;
    const serverId = document.getElementById('mcp-server-select')?.value;
    if (!agentId || !serverId) return alert('Select agent and server');
    try {
        const data = await api('POST', `/api/agents/${agentId}/mcp-servers/${serverId}/test`);
        const count = data.tool_count ?? 0;
        const names = (data.tools || []).slice(0, 10).join(', ');
        alert(`MCP OK: ${count} tool(s)` + (names ? `\\n${names}` : ''));
    } catch (e) {
        alert('MCP test failed: ' + e.message);
    }
}

