// ============================================================
// Peer Groups
// ============================================================

async function showPeerGroups() {
    document.getElementById('peer-groups-modal').style.display = 'flex';
    await loadPeerGroups();
}

function closePeerGroups() {
    document.getElementById('peer-groups-modal').style.display = 'none';
}

async function loadPeerGroups() {
    try {
        const data = await api('GET', '/api/peer-groups');
        renderPeerGroups(data.groups || []);
    } catch (e) {
        document.getElementById('peer-groups-list').innerHTML = '<div style="color:var(--text-muted)">Failed to load peer groups</div>';
    }
}

function renderPeerGroups(groups) {
    const container = document.getElementById('peer-groups-list');
    if (groups.length === 0) {
        container.innerHTML = '<div style="color:var(--text-muted);text-align:center;padding:24px">No peer groups yet. Create one to enable inter-agent messaging.</div>';
        return;
    }

    container.innerHTML = groups.map(g => {
        const members = (g.members || []).map(m =>
            `<span class="member-tag">${escHtml(m.name || m.agent_id)}<span class="remove-member" onclick="removePeerGroupMember('${g.id}', '${m.agent_id}')">&times;</span></span>`
        ).join('');

        const agentOptions = agents.map(a =>
            `<option value="${a.id}">${escHtml(a.name || a.id)}</option>`
        ).join('');

        return `<div class="peer-group-card">
            <div class="group-header">
                <span class="group-name">${escHtml(g.name)}</span>
                <button class="btn btn-sm btn-danger" onclick="deletePeerGroup('${g.id}')">Delete</button>
            </div>
            <div class="group-members">${members || '<span style="color:var(--text-muted);font-size:12px">No members</span>'}</div>
            <div class="add-member-row">
                <select id="add-member-${g.id}" style="flex:1">${agentOptions}</select>
                <button class="btn btn-sm btn-primary" onclick="addPeerGroupMember('${g.id}')">Add</button>
            </div>
        </div>`;
    }).join('');
}

async function createPeerGroup() {
    const input = document.getElementById('new-group-name');
    const name = input.value.trim();
    if (!name) return;

    try {
        await api('POST', '/api/peer-groups', { name });
        input.value = '';
        await loadPeerGroups();
    } catch (e) {
        alert('Failed to create group: ' + e.message);
    }
}

async function deletePeerGroup(groupId) {
    if (!confirm('Delete this peer group?')) return;
    try {
        await api('DELETE', `/api/peer-groups/${groupId}`);
        await loadPeerGroups();
    } catch (e) {
        alert('Failed to delete group: ' + e.message);
    }
}

async function addPeerGroupMember(groupId) {
    const select = document.getElementById(`add-member-${groupId}`);
    const agentId = select.value;
    if (!agentId) return;

    try {
        await api('POST', `/api/peer-groups/${groupId}/members`, { agent_id: agentId });
        await loadPeerGroups();
    } catch (e) {
        alert('Failed to add member: ' + e.message);
    }
}

async function removePeerGroupMember(groupId, agentId) {
    try {
        await api('DELETE', `/api/peer-groups/${groupId}/members/${agentId}`);
        await loadPeerGroups();
    } catch (e) {
        alert('Failed to remove member: ' + e.message);
    }
}

