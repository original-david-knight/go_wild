// ============================================================
// Missions
// ============================================================

let missionsCompanyID = '';
let missionsSelectedID = null;
let missionsSelectedMissionID = null;
let missionsPollTimer = null;

function showMissions(companyID) {
    document.getElementById('missions-modal').style.display = 'flex';
    // Populate company selector — use companySummaries (lightweight) or companies (full)
    const sel = document.getElementById('missions-company-select');
    const list = companySummaries.length ? companySummaries : companies;
    populateMissionsCompanySelect(sel, list);
    // Fetch from API if we have nothing cached
    if (!list.length) {
        api('GET', '/api/companies').then(data => {
            const fetched = data.companies || [];
            if (!companySummaries.length) {
                companySummaries = fetched.map(c => ({ id: c.id, name: c.name, ceo_agent_id: c.ceo_agent_id }));
            }
            populateMissionsCompanySelect(sel, fetched);
            // If a companyID was requested (from URL), select it after populating
            if (companyID) {
                sel.value = companyID;
                onMissionsCompanyChange();
            }
        }).catch(() => {});
    } else if (companyID) {
        sel.value = companyID;
        onMissionsCompanyChange();
    }
    // Update URL
    if (!companyID) history.pushState(null, '', '/missions');
    // Start polling
    if (missionsPollTimer) clearInterval(missionsPollTimer);
    missionsPollTimer = setInterval(() => {
        if (missionsCompanyID) refreshMissionsData();
    }, 30000);
}

function populateMissionsCompanySelect(sel, list) {
    sel.innerHTML = '<option value="">Select a company...</option>';
    for (const c of list) {
        sel.innerHTML += `<option value="${escAttr(c.id)}">${escHtml(c.name || c.id)}</option>`;
    }
}

function closeMissions() {
    document.getElementById('missions-modal').style.display = 'none';
    missionsCompanyID = '';
    missionsSelectedID = null;
    missionsSelectedMissionID = null;
    if (missionsPollTimer) { clearInterval(missionsPollTimer); missionsPollTimer = null; }
    history.pushState(null, '', '/');
}

async function onMissionsCompanyChange() {
    const sel = document.getElementById('missions-company-select');
    missionsCompanyID = sel.value;
    missionsSelectedID = null;
    missionsSelectedMissionID = null;
    document.getElementById('missions-new-btn').style.display = missionsCompanyID ? '' : 'none';
    document.getElementById('missions-new-form').style.display = 'none';
    document.getElementById('missions-detail-actions').style.display = 'none';
    document.getElementById('missions-detail-title').textContent = 'Detail';
    document.getElementById('missions-tab-activity').innerHTML = '<div class="missions-empty">Select a mission</div>';
    document.getElementById('missions-tab-graphs').innerHTML = '<div class="missions-empty">Select a mission to see graphs</div>';
    // Update URL with company ID
    if (missionsCompanyID) {
        history.pushState(null, '', '/missions/' + encodeURIComponent(missionsCompanyID));
    } else {
        history.pushState(null, '', '/missions');
    }
    if (missionsCompanyID) {
        await refreshMissionsData();
    } else {
        document.getElementById('missions-tree-container').innerHTML = '<div class="missions-empty">Select a company</div>';
        document.getElementById('missions-obj-count').textContent = '0';
        document.getElementById('missions-esc-count').textContent = '0';
        document.getElementById('missions-escalation-container').innerHTML = '<div class="missions-empty">No escalations</div>';
    }
}

async function refreshMissionsData() {
    if (!missionsCompanyID) return;
    const base = `/api/companies/${missionsCompanyID}/missions`;
    try {
        const [missions, escalations] = await Promise.all([
            api('GET', base),
            api('GET', base + '/escalations'),
        ]);
        renderMissionTree(missions || []);
        // Skip escalation re-render if user is typing in a resolve input
        const activeEl = document.activeElement;
        const isTypingResolve = activeEl && activeEl.classList.contains('missions-resolve-input');
        if (!isTypingResolve) {
            renderMissionsEscalations(escalations || []);
        } else {
            // Just update the count without destroying input
            document.getElementById('missions-esc-count').textContent = escalations ? escalations.length : 0;
        }
        if (missionsSelectedID) {
            loadMissionDetail(missionsSelectedID);
        } else if (missionsSelectedMissionID) {
            loadMissionDetail(missionsSelectedMissionID);
        }
    } catch (e) {
        console.error('Missions refresh failed:', e);
    }
}

function renderMissionTree(missions) {
    const container = document.getElementById('missions-tree-container');
    const countEl = document.getElementById('missions-obj-count');
    if (!missions.length) {
        container.innerHTML = '<div class="missions-empty">No missions yet</div>';
        countEl.textContent = '0';
        return;
    }
    countEl.textContent = missions.length;
    let html = '';
    for (const m of missions) {
        const obj = m.objective || m;
        const status = obj.status || 'pending';
        const sel = obj.id === missionsSelectedMissionID ? ' missions-selected' : '';
        html += `<div class="missions-tree-node${sel}" data-missions-id="${escAttr(obj.id)}" onclick="selectMission('${escAttr(obj.id)}')">
            <span class="missions-badge missions-badge-${status}">${escHtml(status)}</span>
            <span class="missions-tree-title">${escHtml(obj.title || 'Untitled')}</span>
            <span class="missions-tree-meta">${missionsFormatChildren(m)}</span>
        </div>`;
    }
    container.innerHTML = html;
}

function missionsFormatChildren(rollup) {
    if (rollup.total_count) return rollup.total_count + ' objectives';
    return '';
}

async function selectMission(id) {
    missionsSelectedMissionID = id;
    missionsSelectedID = null;
    // Highlight in tree
    document.querySelectorAll('.missions-tree-node').forEach(n => n.classList.remove('missions-selected'));
    const node = document.querySelector(`[data-missions-id="${id}"]`);
    if (node) node.classList.add('missions-selected');
    // Load tree view for this mission
    await loadMissionTreeExpanded(id);
    await loadMissionDetail(id);
}

async function loadMissionTreeExpanded(missionID) {
    const container = document.getElementById('missions-tree-container');
    const countEl = document.getElementById('missions-obj-count');
    try {
        const base = `/api/companies/${missionsCompanyID}/missions`;
        const treeData = await api('GET', base + '/' + missionID + '/tree');
        const items = treeData || [];
        if (!items.length) {
            await refreshMissionsData();
            return;
        }
        const byParent = {};
        let count = 0;
        for (const raw of items) {
            const obj = raw.objective || raw;
            count++;
            const pid = obj.parent_id || '__root__';
            if (!byParent[pid]) byParent[pid] = [];
            byParent[pid].push(obj);
        }
        countEl.textContent = count;
        container.innerHTML = missionsRenderTreeLevel(byParent, '__root__');
    } catch (e) {
        console.error('Load mission tree failed:', e);
        container.innerHTML = '<div class="missions-empty">Failed to load tree</div>';
    }
}

function missionsRenderTreeLevel(byParent, parentID) {
    const children = byParent[parentID] || [];
    if (!children.length) return '';
    let html = '';
    for (const obj of children) {
        const hasKids = byParent[obj.id] && byParent[obj.id].length > 0;
        const isLeaf = !hasKids;
        const toggleClass = hasKids ? 'expanded' : 'leaf';
        const status = obj.status || 'pending';
        const leafClass = isLeaf ? ' missions-leaf' : '';
        const badgeClass = isLeaf ? `missions-task missions-task-${status}` : `missions-badge missions-badge-${status}`;
        const badgeText = isLeaf ? missionsTaskLabel(status) : status;
        const sel = obj.id === missionsSelectedID ? ' missions-selected' : '';
        html += `<div class="missions-tree-item">
            <div class="missions-tree-row${sel}${leafClass}" data-missions-obj-id="${escAttr(obj.id)}" onclick="selectMissionObjective('${escAttr(obj.id)}', event)">
                <span class="missions-tree-toggle ${toggleClass}">&#9654;</span>
                <span class="${badgeClass}">${escHtml(badgeText)}</span>
                <span class="missions-tree-title" title="${escAttr(obj.description || '')}">${escHtml(obj.title)}</span>
            </div>`;
        if (hasKids) {
            html += `<div class="missions-tree-children expanded">${missionsRenderTreeLevel(byParent, obj.id)}</div>`;
        }
        html += '</div>';
    }
    return html;
}

function selectMissionObjective(id, event) {
    if (event) event.stopPropagation();
    const row = document.querySelector(`[data-missions-obj-id="${id}"]`);
    if (!row) return;
    // Toggle expand/collapse
    const toggle = row.querySelector('.missions-tree-toggle');
    const node = row.parentElement;
    const children = node.querySelector('.missions-tree-children');
    if (children && toggle && !toggle.classList.contains('leaf')) {
        children.classList.toggle('expanded');
        toggle.classList.toggle('expanded');
    }
    // Select
    if (id !== missionsSelectedID) {
        missionsSelectedID = id;
        document.querySelectorAll('.missions-tree-row.missions-selected').forEach(n => n.classList.remove('missions-selected'));
        row.classList.add('missions-selected');
        loadMissionDetail(id);
    }
}

async function loadMissionDetail(id) {
    const base = `/api/companies/${missionsCompanyID}/missions`;
    try {
        const data = await api('GET', base + '/' + id);
        const obj = data.objective;
        const events = data.recent_activity || [];
        document.getElementById('missions-detail-title').textContent = obj.title || 'Objective';
        document.getElementById('missions-detail-actions').style.display = 'flex';
        document.getElementById('missions-chat-bar').style.display = 'flex';
        missionsRenderActivity(events);
        missionsRenderGraphs(events);
    } catch (e) {
        console.error('Load mission detail failed:', e);
    }
}

// Stored activity events for detail view lookup
let _missionsActivityEvents = [];

function missionsRenderActivity(events) {
    const container = document.getElementById('missions-tab-activity');
    _missionsActivityEvents = events || [];
    if (!events || !events.length) {
        container.innerHTML = '<div class="missions-empty">No activity yet</div>';
        return;
    }
    let html = '';
    for (let i = 0; i < events.length; i++) {
        const ev = events[i];
        const time = missionsFormatTime(ev.created_at);
        const timeTitle = formatDatePrecise(ev.created_at);
        const sev = ev.severity || 'info';
        const isDirective = ev.event_type === 'user_directive';
        const extraClass = isDirective ? ' missions-directive' : '';
        const typeLabel = isDirective ? 'You' : escHtml(ev.event_type || 'event');
        // Use full text from details if available, fall back to summary
        const fullText = (ev.details && ev.details.text) ? ev.details.text : '';
        const summary = fullText || ev.summary || '';
        const isLong = summary.length > 200;
        const hasNodeDetails = ev.details && (ev.details.prompt || ev.details.tools);
        html += `<div class="missions-activity-item${extraClass}${isLong ? ' missions-expandable' : ''}" ${isLong ? 'onclick="if(!event.target.classList.contains(\'missions-detail-link\'))this.classList.toggle(\'expanded\')"' : ''}>
            <div class="missions-activity-header">
                <span class="missions-activity-type missions-sev-${sev}">${typeLabel}</span>
                <span class="missions-activity-time"${timeTitle ? ` title="${escAttr(timeTitle)}"` : ''}>${hasNodeDetails ? `<a class="missions-detail-link" href="#" onclick="event.preventDefault();event.stopPropagation();missionsShowNodeDetail(${i})">details</a> · ` : ''}${time}</span>
            </div>
            <div class="missions-activity-summary">${escHtml(summary)}</div>
            ${ev.objective_id && !isDirective ? `<div class="missions-activity-obj">Objective: ${ev.objective_id.substring(0, 8)}...</div>` : ''}
        </div>`;
    }
    container.innerHTML = html;
    container.scrollTop = 0;
}

function missionsShowNodeDetail(index) {
    const ev = _missionsActivityEvents[index];
    if (!ev || !ev.details) return;
    const d = ev.details;
    // Build detail overlay
    let overlay = document.getElementById('missions-node-detail-overlay');
    if (!overlay) {
        overlay = document.createElement('div');
        overlay.id = 'missions-node-detail-overlay';
        overlay.className = 'missions-node-detail-overlay';
        document.body.appendChild(overlay);
    }
    const nodeId = d.node_id || '?';
    const nodeType = d.node_type || '?';
    const tools = d.tools || [];
    const prompt = d.prompt || '';
    const text = d.text || '';
    const output = d.output || '';
    const turns = d.turns || 0;
    const error = d.error || '';

    let html = `<div class="missions-node-detail-panel">
        <div class="missions-node-detail-header">
            <span class="missions-node-detail-title">Node: ${escHtml(nodeId)}</span>
            <a href="#" onclick="event.preventDefault();document.getElementById('missions-node-detail-overlay').style.display='none'" class="missions-node-detail-close">&times;</a>
        </div>
        <div class="missions-node-detail-body">
            <div class="missions-node-detail-meta">
                <span class="missions-node-detail-badge">${escHtml(nodeType)}</span>
                ${turns ? `<span class="missions-node-detail-badge">${turns} turns</span>` : ''}
                ${error ? `<span class="missions-node-detail-badge missions-sev-error">FAILED</span>` : ''}
            </div>`;
    if (tools.length > 0) {
        html += `<div class="missions-node-detail-section">
            <div class="missions-node-detail-label">Tools</div>
            <div class="missions-node-detail-tools">${tools.map(t => `<span class="missions-node-detail-tool">${escHtml(t)}</span>`).join(' ')}</div>
        </div>`;
    }
    if (prompt) {
        html += `<div class="missions-node-detail-section">
            <div class="missions-node-detail-label">Prompt</div>
            <pre class="missions-node-detail-pre">${escHtml(prompt)}</pre>
        </div>`;
    }
    if (error) {
        html += `<div class="missions-node-detail-section">
            <div class="missions-node-detail-label">Error</div>
            <pre class="missions-node-detail-pre missions-sev-error">${escHtml(error)}</pre>
        </div>`;
    }
    if (text) {
        html += `<div class="missions-node-detail-section">
            <div class="missions-node-detail-label">Output</div>
            <pre class="missions-node-detail-pre">${escHtml(text)}</pre>
        </div>`;
    }
    if (output && output !== text) {
        html += `<div class="missions-node-detail-section">
            <div class="missions-node-detail-label">Structured Output</div>
            <pre class="missions-node-detail-pre">${escHtml(output)}</pre>
        </div>`;
    }
    html += `</div></div>`;
    overlay.innerHTML = html;
    overlay.style.display = 'flex';
}

function missionsRenderGraphs(events) {
    const container = document.getElementById('missions-tab-graphs');
    const graphs = (events || []).filter(ev => ev.event_type === 'graph_planned');
    if (!graphs.length) {
        container.innerHTML = '<div class="missions-empty">No graphs yet</div>';
        return;
    }
    container.innerHTML = '';
    for (const ev of graphs) {
        const details = ev.details || {};
        const nodes = details.nodes || [];
        const time = missionsFormatTime(ev.created_at);
        const timeTitle = formatDatePrecise(ev.created_at);
        const card = document.createElement('div');
        card.className = 'missions-graph-card';
        let header = `<div class="missions-graph-header">
            <span class="missions-graph-title">${escHtml(ev.summary || 'Graph')}</span>
            <span class="missions-activity-time"${timeTitle ? ` title="${escAttr(timeTitle)}"` : ''}>${time}</span>
        </div>`;
        if (details.reasoning) {
            header += `<div class="missions-graph-reasoning">${escHtml(details.reasoning)}</div>`;
        }
        card.innerHTML = header;
        if (nodes.length > 0) {
            card.appendChild(missionsBuildDAGSvg(nodes));
        }
        // Node detail list
        const nodeList = document.createElement('div');
        nodeList.className = 'missions-graph-nodes';
        for (const node of nodes) {
            let nh = `<div class="missions-graph-node-header">
                <span class="missions-graph-node-id">${escHtml(node.id || '?')}</span>
                ${node.type ? `<span class="missions-graph-node-type">${escHtml(node.type)}</span>` : ''}
            </div>`;
            if (node.prompt) nh += `<div class="missions-graph-node-prompt">${escHtml(node.prompt)}</div>`;
            nh += '<div class="missions-graph-node-tools">';
            if (node.tools && node.tools.length > 0) {
                for (const t of node.tools) nh += `<span class="missions-graph-node-tool">${escHtml(t)}</span>`;
            }
            nh += '</div>';
            const nodeEl = document.createElement('div');
            nodeEl.className = 'missions-graph-node';
            nodeEl.innerHTML = nh;
            nodeList.appendChild(nodeEl);
        }
        card.appendChild(nodeList);
        container.appendChild(card);
    }
    container.scrollTop = 0;
}

function missionsBuildDAGSvg(nodes) {
    const NODE_W = 160, NODE_H = 36, PAD_X = 40, PAD_Y = 60, MARGIN = 20;
    const idIndex = {};
    for (let i = 0; i < nodes.length; i++) idIndex[nodes[i].id] = i;
    const depth = new Array(nodes.length).fill(0);
    let changed = true;
    while (changed) {
        changed = false;
        for (let i = 0; i < nodes.length; i++) {
            const deps = nodes[i].depends_on || [];
            for (const d of deps) {
                const pi = idIndex[d];
                if (pi !== undefined && depth[i] < depth[pi] + 1) {
                    depth[i] = depth[pi] + 1;
                    changed = true;
                }
            }
        }
    }
    let maxDepth = 0;
    const layers = {};
    for (let i = 0; i < nodes.length; i++) {
        if (depth[i] > maxDepth) maxDepth = depth[i];
        if (!layers[depth[i]]) layers[depth[i]] = [];
        layers[depth[i]].push(i);
    }
    let maxLayerWidth = 0;
    for (let d = 0; d <= maxDepth; d++) {
        if (layers[d] && layers[d].length > maxLayerWidth) maxLayerWidth = layers[d].length;
    }
    const svgW = Math.max(maxLayerWidth * (NODE_W + PAD_X) - PAD_X + MARGIN * 2, 300);
    const svgH = (maxDepth + 1) * (NODE_H + PAD_Y) - PAD_Y + MARGIN * 2;
    const positions = {};
    for (let d = 0; d <= maxDepth; d++) {
        const layer = layers[d] || [];
        const totalW = layer.length * (NODE_W + PAD_X) - PAD_X;
        const startX = (svgW - totalW) / 2;
        const y = MARGIN + d * (NODE_H + PAD_Y);
        for (let li = 0; li < layer.length; li++) {
            const ni = layer[li];
            positions[ni] = { x: startX + li * (NODE_W + PAD_X), y, cx: startX + li * (NODE_W + PAD_X) + NODE_W / 2, cy: y + NODE_H / 2 };
        }
    }
    const ns = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('width', svgW);
    svg.setAttribute('height', svgH);
    svg.setAttribute('class', 'missions-dag-svg');
    // Arrow marker
    const defs = document.createElementNS(ns, 'defs');
    const marker = document.createElementNS(ns, 'marker');
    const mid = 'marr-' + Math.random().toString(36).substr(2, 5);
    marker.setAttribute('id', mid);
    marker.setAttribute('viewBox', '0 0 10 10');
    marker.setAttribute('refX', '10'); marker.setAttribute('refY', '5');
    marker.setAttribute('markerWidth', '8'); marker.setAttribute('markerHeight', '8');
    marker.setAttribute('orient', 'auto-start-reverse');
    const arrowPath = document.createElementNS(ns, 'path');
    arrowPath.setAttribute('d', 'M 0 0 L 10 5 L 0 10 z');
    arrowPath.setAttribute('fill', '#58a6ff');
    marker.appendChild(arrowPath);
    defs.appendChild(marker);
    svg.appendChild(defs);
    const markerId = `url(#${mid})`;
    // Edges
    for (let i = 0; i < nodes.length; i++) {
        for (const d of (nodes[i].depends_on || [])) {
            const fi = idIndex[d];
            if (fi === undefined) continue;
            const from = positions[fi], to = positions[i];
            if (!from || !to) continue;
            const line = document.createElementNS(ns, 'line');
            line.setAttribute('x1', from.cx); line.setAttribute('y1', from.y + NODE_H);
            line.setAttribute('x2', to.cx); line.setAttribute('y2', to.y);
            line.setAttribute('stroke', '#58a6ff'); line.setAttribute('stroke-width', '2');
            line.setAttribute('marker-end', markerId);
            svg.appendChild(line);
        }
    }
    // Nodes
    for (let i = 0; i < nodes.length; i++) {
        const pos = positions[i];
        if (!pos) continue;
        const node = nodes[i];
        const rect = document.createElementNS(ns, 'rect');
        rect.setAttribute('x', pos.x); rect.setAttribute('y', pos.y);
        rect.setAttribute('width', NODE_W); rect.setAttribute('height', NODE_H);
        rect.setAttribute('rx', '6'); rect.setAttribute('fill', '#21262d');
        rect.setAttribute('stroke', '#bc8cff'); rect.setAttribute('stroke-width', '1.5');
        svg.appendChild(rect);
        const label = document.createElementNS(ns, 'text');
        label.setAttribute('x', pos.cx); label.setAttribute('y', pos.cy + 1);
        label.setAttribute('text-anchor', 'middle'); label.setAttribute('dominant-baseline', 'middle');
        label.setAttribute('fill', '#e6edf3'); label.setAttribute('font-size', '11'); label.setAttribute('font-family', 'monospace');
        let lt = node.id || '?';
        if (lt.length > 20) lt = lt.substring(0, 18) + '..';
        label.textContent = lt;
        svg.appendChild(label);
        if (node.type) {
            const tl = document.createElementNS(ns, 'text');
            tl.setAttribute('x', pos.cx); tl.setAttribute('y', pos.y + NODE_H + 12);
            tl.setAttribute('text-anchor', 'middle'); tl.setAttribute('fill', '#484f58'); tl.setAttribute('font-size', '9');
            tl.textContent = node.type;
            svg.appendChild(tl);
        }
    }
    const wrapper = document.createElement('div');
    wrapper.className = 'missions-dag-wrapper';
    wrapper.appendChild(svg);
    return wrapper;
}

function renderMissionsEscalations(escalations) {
    const container = document.getElementById('missions-escalation-container');
    const countEl = document.getElementById('missions-esc-count');
    countEl.textContent = escalations ? escalations.length : 0;
    if (!escalations || !escalations.length) {
        container.innerHTML = '<div class="missions-empty">No pending escalations</div>';
        return;
    }
    let html = '';
    for (const esc of escalations) {
        const sev = esc.severity || 'info';
        const time = missionsFormatTime(esc.created_at);
        const timeTitle = formatDatePrecise(esc.created_at);
        html += `<div class="missions-escalation-card missions-sev-border-${sev}">
            <div class="missions-escalation-header">
                <span class="missions-sev-${sev}">${escHtml(sev)}</span>
                <span class="missions-activity-time"${timeTitle ? ` title="${escAttr(timeTitle)}"` : ''}>${time}</span>
            </div>
            <div class="missions-escalation-question">${escHtml(esc.question || '')}</div>
            ${esc.context ? `<div class="missions-escalation-context">${escHtml(esc.context)}</div>` : ''}
            <div class="missions-resolve-form">
                <textarea class="missions-resolve-input" rows="2" placeholder="Resolution..." data-esc-id="${escAttr(esc.id)}"></textarea>
                <button class="btn btn-sm btn-primary" onclick="resolveMissionsEscalation('${escAttr(esc.id)}', this)">Resolve</button>
            </div>
        </div>`;
    }
    container.innerHTML = html;
}

async function resolveMissionsEscalation(escID, btn) {
    const input = btn.parentElement.querySelector('.missions-resolve-input');
    const resolution = input.value.trim();
    if (!resolution) { input.focus(); return; }
    btn.disabled = true; btn.textContent = '...';
    try {
        await api('POST', `/api/companies/${missionsCompanyID}/missions/escalations/${escID}/resolve`, { resolution });
        await refreshMissionsData();
    } catch (e) {
        console.error('Resolve escalation failed:', e);
        btn.disabled = false; btn.textContent = 'Resolve';
    }
}

function toggleMissionForm() {
    const form = document.getElementById('missions-new-form');
    form.style.display = form.style.display === 'none' ? '' : 'none';
    if (form.style.display !== 'none') document.getElementById('missions-new-title').focus();
}

async function submitNewMission() {
    const title = document.getElementById('missions-new-title').value.trim();
    if (!title) { document.getElementById('missions-new-title').focus(); return; }
    const desc = document.getElementById('missions-new-desc').value.trim();
    try {
        await api('POST', `/api/companies/${missionsCompanyID}/missions`, { title, description: desc });
        document.getElementById('missions-new-title').value = '';
        document.getElementById('missions-new-desc').value = '';
        document.getElementById('missions-new-form').style.display = 'none';
        await refreshMissionsData();
    } catch (e) {
        console.error('Create mission failed:', e);
    }
}

async function deleteSelectedMission() {
    const id = missionsSelectedID || missionsSelectedMissionID;
    if (!id) return;
    try {
        await fetch(`/api/companies/${missionsCompanyID}/missions/${id}`, { method: 'DELETE' });
        missionsSelectedID = null;
        missionsSelectedMissionID = null;
        document.getElementById('missions-detail-actions').style.display = 'none';
        document.getElementById('missions-detail-title').textContent = 'Detail';
        document.getElementById('missions-tab-activity').innerHTML = '<div class="missions-empty">Select a mission</div>';
        document.getElementById('missions-tab-graphs').innerHTML = '<div class="missions-empty">Select a mission to see graphs</div>';
        await refreshMissionsData();
    } catch (e) {
        console.error('Delete mission failed:', e);
    }
}

async function pauseSelectedMission() {
    const id = missionsSelectedID || missionsSelectedMissionID;
    if (!id) return;
    try {
        await api('POST', `/api/companies/${missionsCompanyID}/missions/${id}/pause`, {});
        await refreshMissionsData();
    } catch (e) {
        console.error('Pause failed:', e);
    }
}

async function resumeSelectedMission() {
    const id = missionsSelectedID || missionsSelectedMissionID;
    if (!id) return;
    try {
        await api('POST', `/api/companies/${missionsCompanyID}/missions/${id}/resume`, {});
        await refreshMissionsData();
    } catch (e) {
        console.error('Resume failed:', e);
    }
}

function missionsTaskLabel(status) {
    switch (status) {
        case 'pending': return 'queued';
        case 'active': return 'running';
        case 'completed': return 'done';
        case 'failed': return 'error';
        case 'blocked': return 'blocked';
        case 'paused': return 'paused';
        default: return status;
    }
}

function switchMissionsTab(tab) {
    document.querySelectorAll('.missions-tab').forEach(t => t.classList.toggle('active', t.dataset.missionsTab === tab));
    document.querySelectorAll('.missions-tab-content').forEach(c => c.classList.remove('active'));
    const target = document.getElementById('missions-tab-' + tab);
    if (target) target.classList.add('active');
}

async function sendMissionMessage() {
    const input = document.getElementById('missions-chat-input');
    const content = input.value.trim();
    if (!content) return;
    const id = missionsSelectedMissionID;
    if (!id || !missionsCompanyID) return;
    input.disabled = true;
    try {
        await api('POST', `/api/companies/${missionsCompanyID}/missions/${id}/message`, { content });
        input.value = '';
        loadMissionDetail(id);
    } catch (e) {
        console.error('Send message failed:', e);
    } finally {
        input.disabled = false;
        input.focus();
    }
}

function missionsFormatTime(isoStr) {
    return formatRecentDate(isoStr);
}
