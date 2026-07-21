// ============================================================
// Email Approval (per column)
// ============================================================

let emailApprovalAgentId = null;

async function checkPendingEmails(state) {
    if (!state.emailEnabled) return;
    try {
        const data = await api('GET', `/api/agents/${state.agentId}/pending-emails`);
        const count = data.count || 0;
        const notif = state.columnEl.querySelector('.email-notification');
        if (!notif) return;
        if (count > 0) {
            notif.querySelector('.email-notification-text').textContent =
                `${count} email${count > 1 ? 's' : ''} pending approval`;
            notif.style.display = 'flex';
        } else {
            notif.style.display = 'none';
        }
    } catch (e) {
        // Silently ignore — agent may not have email configured
    }
}

function showEmailApproval(btn) {
    const col = btn.closest('.agent-column');
    const agentId = col.getAttribute('data-agent-id');
    emailApprovalAgentId = agentId;
    document.getElementById('email-approval-modal').style.display = 'flex';
    loadPendingEmails(agentId);
}

function closeEmailApproval() {
    document.getElementById('email-approval-modal').style.display = 'none';
    emailApprovalAgentId = null;
}

async function loadPendingEmails(agentId, autoClose) {
    const list = document.getElementById('email-approval-list');
    const actions = document.getElementById('email-approval-actions');

    try {
        const data = await api('GET', `/api/agents/${agentId}/pending-emails`);
        const emails = data.emails || [];

        if (emails.length === 0) {
            if (autoClose) {
                closeEmailApproval();
                return;
            }
            list.innerHTML = '<div style="color:var(--text-muted);text-align:center;padding:24px">No pending emails</div>';
            actions.style.display = 'none';
            return;
        }

        actions.style.display = 'flex';
        list.innerHTML = emails.map(e => {
            const time = formatDate(e.created_at);
            return `<div class="email-card" data-email-id="${e.id}">
                <div class="email-card-header">
                    <span class="email-type-badge">${escHtml(e.type)}</span>
                    <span class="email-card-recipients">${escHtml(e.recipients)}</span>
                    <span class="email-card-time">${time}</span>
                </div>
                <div class="email-card-subject">${escHtml(e.subject)}</div>
                <div class="email-card-preview">${escHtml(e.preview)}</div>
                <div class="email-card-actions">
                    <button class="btn btn-sm btn-primary" onclick="approveEmail('${agentId}', '${e.id}')">Approve</button>
                    <button class="btn btn-sm btn-danger" onclick="rejectEmail('${agentId}', '${e.id}')">Reject</button>
                    <label class="whitelist-label">
                        <input type="checkbox" class="whitelist-checkbox" data-recipients="${escHtml(e.recipients)}">
                        Whitelist recipient${e.recipients.includes(',') ? 's' : ''}
                    </label>
                </div>
            </div>`;
        }).join('');
    } catch (e) {
        list.innerHTML = `<div style="color:var(--status-error);padding:12px">Error loading emails: ${escHtml(e.message)}</div>`;
        actions.style.display = 'none';
    }
}

async function approveEmail(agentId, emailId) {
    const card = document.querySelector(`.email-card[data-email-id="${emailId}"]`);
    const whitelistCb = card ? card.querySelector('.whitelist-checkbox') : null;

    try {
        await api('POST', `/api/agents/${agentId}/pending-emails/approve`, { id: emailId });
        if (whitelistCb && whitelistCb.checked) {
            const recipients = whitelistCb.dataset.recipients.split(',').map(r => r.trim());
            for (const r of recipients) {
                await api('POST', `/api/agents/${agentId}/email-whitelist`, { email: r }).catch(() => {});
            }
        }
        await loadPendingEmails(agentId, true);
        refreshEmailNotification(agentId);
    } catch (e) {
        alert('Failed to approve email: ' + e.message);
    }
}

async function rejectEmail(agentId, emailId) {
    try {
        await api('POST', `/api/agents/${agentId}/pending-emails/reject`, { id: emailId });
        await loadPendingEmails(agentId, true);
        refreshEmailNotification(agentId);
    } catch (e) {
        alert('Failed to reject email: ' + e.message);
    }
}

async function approveAllEmails() {
    if (!emailApprovalAgentId) return;
    try {
        await api('POST', `/api/agents/${emailApprovalAgentId}/pending-emails/approve`, { all: true });
        await loadPendingEmails(emailApprovalAgentId, true);
        refreshEmailNotification(emailApprovalAgentId);
    } catch (e) {
        alert('Failed to approve all: ' + e.message);
    }
}

async function rejectAllEmails() {
    if (!emailApprovalAgentId) return;
    try {
        await api('POST', `/api/agents/${emailApprovalAgentId}/pending-emails/reject`, { all: true });
        await loadPendingEmails(emailApprovalAgentId, true);
        refreshEmailNotification(emailApprovalAgentId);
    } catch (e) {
        alert('Failed to reject all: ' + e.message);
    }
}

function refreshEmailNotification(agentId) {
    const state = displayedAgents.get(agentId);
    if (state) checkPendingEmails(state);
}

