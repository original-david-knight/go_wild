// ============================================================
// Pipelines UI
// ============================================================

function switchPipelinesModalTab(tabName) {
    const tab = String(tabName || '').trim() || 'runs';
    const valid = new Set(['initial', 'editor', 'runs', 'methods', 'deep-research']);
    pipelinesModalActiveTab = valid.has(tab) ? tab : 'runs';

    document.querySelectorAll('[data-pipeline-modal-tab]').forEach(btn => {
        const btnTab = btn.getAttribute('data-pipeline-modal-tab');
        btn.classList.toggle('active', btnTab === pipelinesModalActiveTab);
    });
    document.querySelectorAll('[data-pipeline-modal-panel]').forEach(panel => {
        const panelTab = panel.getAttribute('data-pipeline-modal-panel');
        panel.classList.toggle('active', panelTab === pipelinesModalActiveTab);
    });

    if (pipelinesModalActiveTab === 'methods') {
        renderA2AMethodsList();
    } else if (pipelinesModalActiveTab === 'deep-research') {
        renderDeepResearchMethodsList();
    }

    if (pipelinesModalActiveTab === 'runs') {
        startPipelinesPoll();
    } else {
        stopPipelinesPoll();
    }
}

async function showPipelines(initialTab) {
    const modal = document.getElementById('pipelines-modal');
    if (!modal) return;
    modal.style.display = 'flex';
    switchPipelinesModalTab(initialTab || 'runs');
    const initialParamsEl = document.getElementById('pipeline-initial-params');
    if (initialParamsEl && !initialParamsEl.value.trim()) {
        initialParamsEl.value = '{}';
    }
    await Promise.all([
        loadPipelinesModalData(),
        refreshAgents(),
    ]);
    if (pipelinesModalActiveTab === 'runs') startPipelinesPoll();
    if (!pipelineEditorDraft) {
        pipelineEditorNew(true);
    } else {
        pipelineEditorRender();
        pipelineEditorUpdateValidation();
        refreshPipelineCapabilitiesForCurrentScope(true);
    }
}

function closePipelines() {
    document.getElementById('pipelines-modal').style.display = 'none';
    closePipelineStepIOModal();
    stopPipelinesPoll();
}

function capturePipelinesRunsPanelScroll() {
    const panel = document.querySelector('.pipelines-modal-panel.active[data-pipeline-modal-panel="runs"]');
    if (!panel) return null;
    return {
        panel,
        top: panel.scrollTop,
    };
}

function restorePipelinesRunsPanelScroll(state) {
    if (!state || !state.panel || !state.panel.isConnected) return;
    state.panel.scrollTop = state.top || 0;
}

async function withPipelinesRunsPanelScrollPreserved(work) {
    const scrollState = capturePipelinesRunsPanelScroll();
    const result = await work();
    restorePipelinesRunsPanelScroll(scrollState);
    return result;
}

function startPipelinesPoll() {
    stopPipelinesPoll();
    pipelinesPollTimer = setInterval(async () => {
        await withPipelinesRunsPanelScrollPreserved(async () => {
            await Promise.all([loadPipelineRuns(), loadActiveJobs()]);
            await refreshExpandedRunDetails();
        });
    }, 3000);
}

function stopPipelinesPoll() {
    if (pipelinesPollTimer) { clearInterval(pipelinesPollTimer); pipelinesPollTimer = null; }
}

async function refreshExpandedRunDetails() {
    const details = document.querySelectorAll('.pipeline-run-detail');
    for (const detail of details) {
        if (detail.style.display === 'none') continue;
        const runId = detail.id.replace('pipeline-run-detail-', '');
        if (!runId) continue;
        try {
            const data = await api('GET', `/api/pipelines/runs/${encodeURIComponent(runId)}`);
            const steps = data.steps || [];
            if (!steps.length) {
                detail.innerHTML = '<div style="color:var(--text-muted);font-size:11px;padding:6px 0">No step runs.</div>';
            } else {
                detail.innerHTML = renderPipelineStepRuns(runId, steps);
            }
        } catch {}
    }
}

async function loadPipelinesModalData() {
    await withPipelinesRunsPanelScrollPreserved(async () => {
        await Promise.all([
            loadA2AMethods(),
            loadDeepResearchMethods(),
            loadPipelineCapabilities(),
            loadCompanySummaries(),
            loadPipelineDefinitions(),
            loadPipelineRuns(),
            loadActiveJobs(),
        ]);
    });
    renderPipelineEditorDatalists();
    renderA2AMethodsList();
    renderDeepResearchMethodsList();
}

async function loadPipelineCapabilities(companyID) {
    const filterCompanyID = String(companyID || '').trim();
    const query = filterCompanyID ? `?company_id=${encodeURIComponent(filterCompanyID)}` : '';
    try {
        const data = await api('GET', `/api/pipelines/capabilities${query}`);
        const caps = Array.isArray(data.capabilities) ? data.capabilities : [];
        pipelineCapabilities = caps.slice().sort((a, b) => {
            const ka = `${a.role || ''}/${a.method || ''}/${a.agent_name || a.agent_id || ''}`.toLowerCase();
            const kb = `${b.role || ''}/${b.method || ''}/${b.agent_name || b.agent_id || ''}`.toLowerCase();
            return ka.localeCompare(kb);
        });
        pipelineCapabilitiesFilterCompanyID = filterCompanyID;
    } catch (e) {
        pipelineCapabilities = [];
        pipelineCapabilitiesFilterCompanyID = filterCompanyID;
    }
    renderPipelineCapabilityOptions();
    if (pipelineEditorDraft) {
        renderPipelineEditorActions();
        pipelineEditorUpdateValidation();
    }
}

function getPipelineScopeFilterCompanyID() {
    if (!pipelineEditorDraft) return '';
    const scopeMode = String(pipelineEditorDraft.scope_mode || '').trim() || 'global';
    if (scopeMode !== 'company') return '';
    return String(pipelineEditorDraft.scope_company_id || '').trim();
}

function refreshPipelineCapabilitiesForCurrentScope(force) {
    const filterCompanyID = getPipelineScopeFilterCompanyID();
    if (!force && filterCompanyID === pipelineCapabilitiesFilterCompanyID) {
        return;
    }
    loadPipelineCapabilities(filterCompanyID);
}

async function loadCompanySummaries() {
    try {
        const data = await api('GET', '/api/companies');
        companySummaries = Array.isArray(data.companies) ? data.companies : [];
    } catch (e) {
        companySummaries = [];
    }
    refreshPipelineEditorCompanyOptions();
}

function renderPipelineCapabilityOptions() {
    const select = document.getElementById('pipeline-target-capability');
    if (!select) return;

    if (!pipelineCapabilities.length) {
        select.innerHTML = '<option value="">No capabilities registered</option>';
        document.getElementById('pipeline-target-role').value = '';
        document.getElementById('pipeline-target-method').value = '';
        renderPipelineEditorDatalists();
        return;
    }

    const prev = Number(select.value);
    select.innerHTML = pipelineCapabilities.map((cap, idx) => {
        const agentLabel = cap.agent_name || cap.agent_id;
        const role = cap.role || 'unknown';
        const method = cap.method || 'unknown';
        return `<option value="${idx}">${escHtml(agentLabel)} · ${escHtml(role)}/${escHtml(method)}</option>`;
    }).join('');
    if (Number.isInteger(prev) && prev >= 0 && prev < pipelineCapabilities.length) {
        select.value = String(prev);
    } else {
        select.value = '0';
    }
    renderPipelineEditorDatalists();
    handlePipelineCapabilityChange();
}

function handlePipelineCapabilityChange() {
    const cap = getSelectedPipelineCapability('pipeline-target-capability');
    const roleInput = document.getElementById('pipeline-target-role');
    const methodInput = document.getElementById('pipeline-target-method');
    if (!roleInput || !methodInput) return;

    if (!cap) {
        roleInput.value = '';
        methodInput.value = '';
        return;
    }

    roleInput.value = cap.role || '';
    methodInput.value = cap.method || '';
    pipelineEditorMaybeAdoptInitialCapability(cap);
}

function getSelectedPipelineCapability(selectID) {
    const select = document.getElementById(selectID);
    if (!select) return null;
    const idx = Number(select.value);
    if (!Number.isInteger(idx) || idx < 0 || idx >= pipelineCapabilities.length) {
        return null;
    }
    return pipelineCapabilities[idx];
}

async function loadPipelineDefinitions() {
    try {
        const data = await api('GET', '/api/pipelines');
        pipelineDefinitions = Array.isArray(data) ? data : [];
    } catch (e) {
        pipelineDefinitions = [];
    }
    renderPipelineDefinitions();
}

function renderPipelineDefinitions() {
    const container = document.getElementById('pipelines-list');
    if (!container) return;
    if (!pipelineDefinitions.length) {
        container.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No pipelines configured.</div>';
        return;
    }

    container.innerHTML = pipelineDefinitions.map(p => {
        const steps = Array.isArray(p.steps) ? p.steps.length : 0;
        const enabledText = p.enabled ? 'enabled' : 'disabled';
        const scopeMode = String(p.scope_mode || 'global').trim() || 'global';
        const scopeText = scopeMode === 'company'
            ? `company:${String(p.scope_company_id || '').trim() || '(missing)'}`
            : 'global';
        const scheduleText = String(p.schedule || '').trim();
        const scheduleLabel = scheduleText ? `every ${scheduleText}` : 'manual';
        const active = pipelineEditorDraft && pipelineEditorDraft.loaded_id === p.id;
        const activeStyle = active ? 'border-color:var(--accent);' : '';
        const triggering = pipelineTriggerInFlight.has(p.id);
        const triggerDisabled = !p.enabled || triggering;
        const triggerLabel = triggering ? 'Triggering...' : 'Trigger';
        return `<div class="pipeline-definition-item" style="${activeStyle}" data-pipeline-id="${escAttr(p.id || '')}">
            <div style="display:flex;justify-content:space-between;gap:10px;align-items:flex-start">
                <div style="min-width:0">
                    <div class="pipeline-definition-title">${escHtml(p.name || p.id || 'Unnamed Pipeline')}</div>
                    <div class="pipeline-definition-meta">
                        id: ${escHtml(p.id || '')} · ${enabledText} · scope ${escHtml(scopeText)} · ${escHtml(scheduleLabel)} · ${steps} action${steps === 1 ? '' : 's'}
                    </div>
                </div>
                <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;justify-content:flex-end">
                    <button class="btn btn-sm btn-trigger pipeline-def-trigger" data-pipeline-id="${escAttr(p.id || '')}"${triggerDisabled ? ' disabled' : ''}>${triggerLabel}</button>
                    <button class="btn btn-sm pipeline-def-edit" data-pipeline-id="${escAttr(p.id || '')}">Edit</button>
                    <button class="btn btn-sm pipeline-def-duplicate" data-pipeline-id="${escAttr(p.id || '')}">Duplicate</button>
                    <button class="btn btn-sm pipeline-def-toggle" data-pipeline-id="${escAttr(p.id || '')}">${p.enabled ? 'Disable' : 'Enable'}</button>
                    <button class="btn btn-sm btn-danger pipeline-def-delete" data-pipeline-id="${escAttr(p.id || '')}">Delete</button>
                </div>
            </div>
        </div>`;
    }).join('');

    attachPipelineDefinitionsListHandlers();
}

async function loadPipelineRuns() {
    try {
        const data = await api('GET', '/api/pipelines/runs');
        pipelineRuns = Array.isArray(data) ? data : [];
    } catch (e) {
        pipelineRuns = [];
    }
    renderPipelineRuns();
}

let activeJobs = [];

async function loadActiveJobs() {
    try {
        const data = await api('GET', '/api/pipelines/jobs');
        activeJobs = Array.isArray(data) ? data : [];
    } catch (e) {
        activeJobs = [];
    }
    renderActiveJobs();
}

function updateDeleteAllJobsButtonState() {
    const btn = document.getElementById('pipeline-delete-all-jobs-btn');
    if (!btn) return;
    const busy = btn.dataset.busy === 'true';
    const count = Array.isArray(activeJobs) ? activeJobs.filter(j => !!j.cancelable).length : 0;
    btn.disabled = busy || count === 0;
    btn.textContent = busy ? 'Deleting...' : `Delete All Jobs${count > 0 ? ` (${count})` : ''}`;
}

function renderActiveJobs() {
    const container = document.getElementById('pipeline-active-jobs');
    if (!container) return;
    if (!activeJobs.length) {
        container.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No active jobs.</div>';
        updateDeleteAllJobsButtonState();
        return;
    }

    container.innerHTML = `<table class="pipeline-steps-table">
        <thead><tr>
            <th>Company</th><th>Method</th><th>Params</th><th>Agent</th><th>Status</th><th>Since</th><th></th>
        </tr></thead>
        <tbody>${activeJobs.map(j => {
            const status = (j.status || 'unknown').toLowerCase();
            const since = j.claimed_at ? formatDate(j.claimed_at) : formatDate(j.created_at);
            const toName = j.to_agent_name || j.to_agent || '—';
            const paramsSummary = summarizeJobParams(j.params);
            const cancelCell = j.cancelable
                ? `<button class="btn-cancel-job" onclick="cancelPipelineJob('${escAttr(j.id)}')" title="Cancel job">&times;</button>`
                : '—';
            return `<tr>
                <td>${escHtml(j.company || '—')}</td>
                <td>${escHtml(j.method || '—')}</td>
                <td class="job-params-cell" title="${escAttr(paramsSummary.full)}">${escHtml(paramsSummary.short)}</td>
                <td>${escHtml(toName)}</td>
                <td><span class="pipeline-run-status ${escAttr(status)}">${escHtml(status)}</span></td>
                <td>${escHtml(since)}</td>
                <td>${cancelCell}</td>
            </tr>`;
        }).join('')}</tbody>
    </table>`;
    updateDeleteAllJobsButtonState();
}

async function cancelPipelineJob(jobId) {
    if (!confirm('Cancel this job?')) return;
    try {
        await api('DELETE', '/api/pipelines/jobs/' + encodeURIComponent(jobId));
        await loadActiveJobs();
    } catch (e) {
        alert('Failed to cancel job: ' + (e.message || e));
    }
}

async function deleteAllPipelineJobs() {
    const count = Array.isArray(activeJobs) ? activeJobs.length : 0;
    if (count === 0) return;
    const noun = count === 1 ? 'job' : 'jobs';
    if (!confirm(`Delete all ${count} active ${noun}?`)) return;

    const btn = document.getElementById('pipeline-delete-all-jobs-btn');
    if (btn) {
        btn.dataset.busy = 'true';
    }
    updateDeleteAllJobsButtonState();

    try {
        await api('DELETE', '/api/pipelines/jobs');
        await loadActiveJobs();
    } catch (e) {
        alert('Failed to delete all jobs: ' + (e.message || e));
    } finally {
        if (btn) {
            btn.dataset.busy = 'false';
        }
        updateDeleteAllJobsButtonState();
    }
}

async function triggerPipelineByID(pipelineID) {
    try {
        const result = await api('POST', `/api/pipelines/${encodeURIComponent(pipelineID)}/trigger`, { params: {} });
        const runId = result.run_id || '';
        alert(`Pipeline triggered! Run ID: ${runId}`);
        await withPipelinesRunsPanelScrollPreserved(async () => {
            await Promise.all([loadActiveJobs(), loadPipelineRuns()]);
        });
    } catch (e) {
        alert('Failed to trigger pipeline: ' + (e.message || e));
    }
}

function renderPipelineRuns() {
    const container = document.getElementById('pipeline-runs-list');
    if (!container) return;
    if (!pipelineRuns.length) {
        container.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No pipeline runs yet.</div>';
        return;
    }

    // Preserve expanded run IDs so we don't collapse them on re-render
    const expandedRunIds = new Set();
    container.querySelectorAll('.pipeline-run-detail').forEach(el => {
        if (el.style.display !== 'none') {
            expandedRunIds.add(el.id.replace('pipeline-run-detail-', ''));
        }
    });

    container.innerHTML = pipelineRuns.slice(0, 20).map(run => {
        const status = (run.status || 'unknown').toLowerCase();
        const created = run.created_at ? formatDate(run.created_at) : 'unknown';
        const runId = escAttr(run.id || '');
        const expanded = expandedRunIds.has(run.id);
        return `<div class="pipeline-run-item" style="cursor:pointer" onclick="togglePipelineRunDetail('${runId}')">
            <div style="display:flex;align-items:center;gap:6px">
                <span class="pipeline-run-expand" id="pipeline-run-arrow-${runId}">${expanded ? '▼' : '▶'}</span>
                <div class="pipeline-definition-title">${escHtml(run.pipeline_id || 'unknown')}</div>
            </div>
            <div class="pipeline-run-meta">
                <span class="pipeline-run-status ${escAttr(status)}">${escHtml(status)}</span>
                · step ${run.current_step || 0} · ${escHtml(created)}
            </div>
            <div class="pipeline-run-detail" id="pipeline-run-detail-${runId}" style="display:${expanded ? '' : 'none'}" onclick="event.stopPropagation()"></div>
        </div>`;
    }).join('');
}

async function togglePipelineRunDetail(runId) {
    const detail = document.getElementById('pipeline-run-detail-' + runId);
    const arrow = document.getElementById('pipeline-run-arrow-' + runId);
    if (!detail) return;

    if (detail.style.display !== 'none') {
        detail.style.display = 'none';
        if (arrow) arrow.textContent = '▶';
        return;
    }

    detail.innerHTML = '<div style="color:var(--text-muted);font-size:11px;padding:6px 0">Loading...</div>';
    detail.style.display = '';
    if (arrow) arrow.textContent = '▼';

    try {
        const data = await api('GET', `/api/pipelines/runs/${encodeURIComponent(runId)}`);
        const steps = data.steps || [];
        if (!steps.length) {
            detail.innerHTML = '<div style="color:var(--text-muted);font-size:11px;padding:6px 0">No step runs.</div>';
            return;
        }
        detail.innerHTML = renderPipelineStepRuns(runId, steps);
    } catch (e) {
        detail.innerHTML = `<div style="color:var(--red);font-size:11px;padding:6px 0">${escHtml(e.message || 'Failed to load')}</div>`;
    }
}

function setPipelineStepIOContent(key, title, text) {
    if (!key) return;
    pipelineStepIOContents.set(key, {
        title: String(title || ''),
        text: String(text || ''),
    });
    if (pipelineStepIOActiveKey === key) {
        const titleEl = document.getElementById('pipeline-step-io-modal-title');
        const bodyEl = document.getElementById('pipeline-step-io-modal-body');
        if (titleEl) titleEl.textContent = String(title || '');
        if (bodyEl) bodyEl.textContent = String(text || '');
    }
}

function openPipelineStepIOModal(key) {
    const entry = pipelineStepIOContents.get(key);
    if (!entry) return;
    pipelineStepIOActiveKey = key;
    const modal = document.getElementById('pipeline-step-io-modal');
    const titleEl = document.getElementById('pipeline-step-io-modal-title');
    const bodyEl = document.getElementById('pipeline-step-io-modal-body');
    if (!modal || !titleEl || !bodyEl) return;
    titleEl.textContent = entry.title || 'Pipeline Step IO';
    bodyEl.textContent = entry.text || '';
    modal.style.display = 'flex';
}

function closePipelineStepIOModal() {
    pipelineStepIOActiveKey = '';
    const modal = document.getElementById('pipeline-step-io-modal');
    if (!modal) return;
    modal.style.display = 'none';
}

function renderPipelineStepRuns(runId, steps) {
    function pipelineStepIOPreview(value) {
        if (value == null) return '';
        let text = '';
        if (typeof value === 'string') {
            text = value;
        } else {
            try {
                text = JSON.stringify(value);
            } catch {
                text = String(value);
            }
        }
        text = String(text || '').replace(/\s+/g, ' ').trim();
        return text.length > 80 ? text.slice(0, 80) + '…' : text;
    }

    function pipelineStepIOText(value) {
        if (value == null) return '';
        if (typeof value === 'string') return value;
        try {
            return JSON.stringify(value, null, 2);
        } catch {
            return String(value);
        }
    }

    function renderPipelineStepIOCell(stepKey, baseTitle, label, value, extras) {
        const primaryText = pipelineStepIOText(value);
        const primaryPreview = pipelineStepIOPreview(value);
        const extraItems = Array.isArray(extras) ? extras.filter(extra => extra && extra.value != null && pipelineStepIOText(extra.value)) : [];
        if (!primaryText && extraItems.length === 0) return '—';

        function renderPipelineStepIOButton(keySuffix, buttonLabel, preview, text) {
            const key = `${stepKey}:${keySuffix}`;
            const title = `${baseTitle} · ${buttonLabel}`;
            setPipelineStepIOContent(key, title, text);
            return `<button type="button" class="pipeline-step-io-button" onclick="openPipelineStepIOModal('${escAttr(key)}'); event.stopPropagation();">${escHtml((buttonLabel ? buttonLabel + ': ' : '') + (preview || 'View'))}</button>`;
        }

        let html = '';
        if (primaryText) {
            html += renderPipelineStepIOButton('primary', label || 'View', primaryPreview, primaryText);
        }
        extraItems.forEach((extra, idx) => {
            const extraText = pipelineStepIOText(extra.value);
            const extraPreview = pipelineStepIOPreview(extra.value);
            html += renderPipelineStepIOButton(`extra-${idx}`, extra.label || 'View', extraPreview, extraText);
        });
        return html;
    }

    function buildPipelineStepOutputExtras(step) {
        const extras = [];
        if (step.claude_log) {
            extras.push({ label: 'Claude Event Log', value: step.claude_log });
        }
        if (step.claude_stderr) {
            extras.push({ label: 'Claude STDERR', value: step.claude_stderr });
        }
        if (step.raw_output) {
            extras.push({ label: 'Raw Claude Stream', value: step.raw_output });
        }
        return extras;
    }

    return `<table class="pipeline-steps-table">
        <thead><tr>
            <th>Step</th><th>Runner</th><th>Method</th><th>Agent</th><th>Status</th><th>Input</th><th>Output</th>
        </tr></thead>
        <tbody>${steps.map((s, index) => {
            const status = (s.status || 'unknown').toLowerCase();
            const runner = escHtml(s.runner || '—');
            const method = escHtml(s.method || '—');
            const agent = escHtml(s.agent_id || '—');
            const stepIdentity = String(s.id || s.a2a_job_id || (s.step_index != null ? `step-${s.step_index}-${index}` : index));
            const stepKey = `${runId}:${stepIdentity}`;
            const stepTitle = `Step ${s.step_index != null ? s.step_index : index} · ${s.method || 'unknown'}`;
            const inputValue = s.request && typeof s.request === 'object' && s.request.params !== undefined
                ? s.request.params
                : s.request;
            const outputValue = s.error || s.result;
            return `<tr>
                <td>${s.step_index}</td>
                <td>${runner}</td>
                <td>${method}</td>
                <td>${agent}</td>
                <td><span class="pipeline-run-status ${escAttr(status)}">${escHtml(status)}</span></td>
                <td class="pipeline-step-result">${renderPipelineStepIOCell(`${stepKey}:input`, stepTitle, 'Input', inputValue)}</td>
                <td class="pipeline-step-result">${renderPipelineStepIOCell(`${stepKey}:output`, stepTitle, 'Output', outputValue, buildPipelineStepOutputExtras(s))}</td>
            </tr>`;
        }).join('')}</tbody>
    </table>`;
}

function setPipelineInitialResult(kind, text) {
    const el = document.getElementById('pipeline-initial-result');
    if (!el) return;
    el.className = `pipelines-result ${kind}`;
    el.textContent = text;
    el.style.display = '';
}

function setPipelineEditorResult(kind, text) {
    const el = document.getElementById('pipeline-editor-result');
    if (!el) return;
    el.className = `pipelines-result ${kind}`;
    el.textContent = text;
    el.style.display = '';
}

function pipelineEditorClearResult() {
    const el = document.getElementById('pipeline-editor-result');
    if (!el) return;
    el.textContent = '';
    el.style.display = 'none';
}

function setPipelineEditorValidation(errors) {
    const el = document.getElementById('pipeline-editor-validation');
    if (!el) return;
    if (!errors || errors.length === 0) {
        el.style.display = 'none';
        el.textContent = '';
        return;
    }
    el.className = 'pipelines-result error';
    el.textContent = 'Validation:\n- ' + errors.join('\n- ');
    el.style.display = '';
}

function parseJSONInputObject(raw, label) {
    const text = (raw || '').trim();
    if (!text) return {};
    let parsed;
    try {
        parsed = JSON.parse(text);
    } catch (e) {
        throw new Error(`${label} must be valid JSON: ${e.message}`);
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error(`${label} must be a JSON object`);
    }
    return parsed;
}

function parseStringMapInput(raw, label) {
    const parsed = parseJSONInputObject(raw, label);
    const out = {};
    for (const [k, v] of Object.entries(parsed)) {
        if (typeof v !== 'string') {
            throw new Error(`${label} value for "${k}" must be a string`);
        }
        out[k] = v;
    }
    return out;
}

async function submitPipelineInitialRequestFromForm() {
    const paramsEl = document.getElementById('pipeline-initial-params');
    if (!paramsEl) {
        throw new Error('Initial params field not found');
    }
    const cap = getSelectedPipelineCapability('pipeline-target-capability');
    if (!cap) {
        throw new Error('No target capability selected');
    }

    const params = parseJSONInputObject(paramsEl.value, 'Params');
    const resp = await api('POST', '/api/pipelines/initial-request', {
        to_role: cap.role,
        method: cap.method,
        params,
    });
    return { resp, cap };
}

async function submitPipelineInitialRequest() {
    try {
        const { resp, cap } = await submitPipelineInitialRequestFromForm();
        setPipelineInitialResult(
            'ok',
            `Submitted job ${resp.job_id}\nTarget agent: ${resp.target_agent_id}\nCapability: ${cap.role}/${cap.method}`,
        );
        await loadPipelineRuns();
    } catch (e) {
        setPipelineInitialResult('error', `Failed to submit initial request: ${e.message}`);
    }
}

function normalizePipelineActionRunner(rawRunner) {
    const runner = String(rawRunner || '').trim().toLowerCase();
    if (runner === pipelineStepRunnerBuiltin) return pipelineStepRunnerBuiltin;
    if (runner === pipelineStepRunnerClaudeCode || runner === 'claude_code' || runner === 'claudecode') return pipelineStepRunnerClaudeCode;
    if (runner === pipelineStepRunnerCodex || runner === 'codex-code' || runner === 'openai') return pipelineStepRunnerCodex;
    return pipelineStepRunnerAgent;
}

// Returns the dropdown-level runner (agent or builtin).
function pipelineActionRunnerCategory(runner) {
    const r = normalizePipelineActionRunner(runner);
    if (r === pipelineStepRunnerBuiltin) return pipelineStepRunnerBuiltin;
    return pipelineStepRunnerAgent;
}

function normalizePipelineBuiltinMethod(rawMethod) {
    const method = String(rawMethod || '').trim();
    if (!method) return '';
    const lower = method.toLowerCase();
    for (const builtin of pipelineBuiltinMethodCatalog) {
        if (!builtin) continue;
        if (String(builtin.canonical || '').toLowerCase() === lower) {
            return String(builtin.canonical || '').trim();
        }
        if (String(builtin.display || '').toLowerCase() === lower) {
            return String(builtin.canonical || '').trim();
        }
        const aliases = Array.isArray(builtin.aliases) ? builtin.aliases : [];
        for (const alias of aliases) {
            if (String(alias || '').trim().toLowerCase() === lower) {
                return String(builtin.canonical || '').trim();
            }
        }
    }
    return method;
}

function pipelineBuiltinMethodDisplayName(rawMethod) {
    const canonical = normalizePipelineBuiltinMethod(rawMethod);
    if (!canonical) return '';
    for (const builtin of pipelineBuiltinMethodCatalog) {
        if (!builtin) continue;
        if (String(builtin.canonical || '').trim() === canonical) {
            return String(builtin.display || builtin.canonical || '').trim();
        }
    }
    return String(rawMethod || '').trim();
}

function isKnownPipelineBuiltinMethod(rawMethod) {
    const canonical = normalizePipelineBuiltinMethod(rawMethod);
    if (!canonical) return false;
    return pipelineBuiltinMethodCatalog.some(builtin => String(builtin?.canonical || '').trim() === canonical);
}

function renderPipelineBuiltinMethodOptionList(selectedMethod) {
    const selectedDisplay = pipelineBuiltinMethodDisplayName(selectedMethod);
    const knownDisplay = new Set();
    let options = '<option value="">Select builtin method...</option>';
    for (const builtin of pipelineBuiltinMethodCatalog) {
        if (!builtin) continue;
        const display = String(builtin.display || builtin.canonical || '').trim();
        const description = String(builtin.description || '').trim();
        if (!display) continue;
        // Hidden (debug) methods: only surface if the current draft already
        // references one, so operators can still see/edit existing pipelines
        // without accidentally discovering the method in the dropdown.
        if (builtin.hidden && selectedDisplay !== display) continue;
        knownDisplay.add(display);
        const selected = selectedDisplay === display ? ' selected' : '';
        const label = description ? `${display} - ${description}` : display;
        options += `<option value="${escAttr(display)}"${selected}>${escHtml(label)}</option>`;
    }
    if (selectedDisplay && !knownDisplay.has(selectedDisplay)) {
        options += `<option value="${escAttr(selectedDisplay)}" selected>${escHtml(selectedDisplay)} (custom)</option>`;
    }
    return options;
}

function renderPipelineA2AMethodOptionList(selectedMethod) {
    const selected = String(selectedMethod || '').trim();
    const known = new Set();
    let options = '<option value="">Select method...</option>';
    for (const methodDef of a2aMethods) {
        if (!methodDef || !methodDef.method) continue;
        const method = String(methodDef.method || '').trim();
        const description = String(methodDef.description || '').trim();
        if (!method) continue;
        known.add(method);
        const isSelected = selected === method ? ' selected' : '';
        const label = description ? `${method} - ${description}` : method;
        options += `<option value="${escAttr(method)}"${isSelected}>${escHtml(label)}</option>`;
    }
    if (selected && !known.has(selected)) {
        options += `<option value="${escAttr(selected)}" selected>${escHtml(selected)} (custom)</option>`;
    }
    return options;
}

function renderPipelineEditorDatalists() {
    const methodsEl = document.getElementById('pipeline-methods-datalist');
    const rolesEl = document.getElementById('pipeline-roles-datalist');
    if (!methodsEl && !rolesEl) return;

    const methods = new Set();
    const roles = new Set();
    for (const m of a2aMethods) {
        if (m && m.method) methods.add(String(m.method));
    }
    for (const builtin of pipelineBuiltinMethodCatalog) {
        if (!builtin) continue;
        if (builtin.hidden) continue;
        if (builtin.display) methods.add(String(builtin.display));
        if (builtin.canonical) methods.add(String(builtin.canonical));
        const aliases = Array.isArray(builtin.aliases) ? builtin.aliases : [];
        for (const alias of aliases) {
            if (alias) methods.add(String(alias));
        }
    }
    for (const cap of pipelineCapabilities) {
        if (cap && cap.role) roles.add(String(cap.role));
    }
    const methodList = Array.from(methods).sort((a, b) => a.localeCompare(b));
    const roleList = Array.from(roles).sort((a, b) => a.localeCompare(b));

    if (methodsEl) {
        methodsEl.innerHTML = methodList.map(m => `<option value="${escAttr(m)}"></option>`).join('');
    }
    if (rolesEl) {
        const withWildcard = ['*', ...roleList.filter(r => r !== '*')];
        rolesEl.innerHTML = withWildcard.map(r => `<option value="${escAttr(r)}"></option>`).join('');
    }
}

function pipelineEditorMaybeAdoptInitialCapability(cap) {
    if (!cap || !pipelineEditorDraft) return;
    if (pipelineEditorDraft.loaded_id) return;
    if (pipelineEditorDirty) return;

    const triggerEl = document.getElementById('pipeline-editor-trigger-method');
    if (triggerEl) {
        triggerEl.value = cap.method || '';
        pipelineEditorDraft.trigger_method = cap.method || '';
    }

    const idEl = document.getElementById('pipeline-editor-id');
    if (idEl && !idEl.value.trim()) {
        const suggestion = suggestPipelineIDFromMethod(cap.method || '');
        if (suggestion) {
            idEl.value = suggestion;
            pipelineEditorDraft.id = suggestion;
        }
    }

    const nameEl = document.getElementById('pipeline-editor-name');
    if (nameEl && !nameEl.value.trim()) {
        const name = `Pipeline for ${cap.role || ''}/${cap.method || ''}`.replace(/\/$/, '');
        nameEl.value = name.trim() || 'Pipeline';
        pipelineEditorDraft.name = nameEl.value;
    }

    pipelineEditorUpdateValidation();
    pipelineEditorRefreshActionSubtitles();
}

function suggestPipelineIDFromMethod(method) {
    const token = (method || '')
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '_')
        .replace(/^_+|_+$/g, '');
    if (!token) return '';
    const suffix = Date.now().toString().slice(-6);
    return `pipeline_${token}_${suffix}`;
}

function defaultPipelineEditorDraft() {
    const cap = getSelectedPipelineCapability('pipeline-target-capability');
    const triggerMethod = cap ? (cap.method || '') : '';
    const draft = {
        loaded_id: null,
        id: triggerMethod ? suggestPipelineIDFromMethod(triggerMethod) : '',
        name: cap ? `Pipeline for ${cap.role || ''}/${cap.method || ''}`.trim() : '',
        enabled: true,
        schedule: '',
        scope_mode: 'global',
        scope_company_id: '',
        trigger_method: triggerMethod,
        trigger_status: 'succeeded',
        trigger_from_role: '*',
        actions: [defaultPipelineEditorAction()],
    };
    if (!draft.name) draft.name = 'Pipeline';
    return draft;
}

function defaultPipelineEditorAction() {
    return {
        runner: pipelineStepRunnerAgent,
        _runner_category: pipelineStepRunnerAgent,
        to_role: '',
        to_agent_id: '',
        next_method: '',
        param_map_text: '{"$":"input"}',
        fan_out: false,
        fan_out_key: '',
    };
}

function pipelineEditorNew(force) {
    if (pipelineEditorDirty && !force) {
        if (!confirm('Discard unsaved pipeline changes?')) return;
    }
    pipelineEditorDraft = defaultPipelineEditorDraft();
    pipelineEditorDirty = false;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
    refreshPipelineCapabilitiesForCurrentScope(true);
}

function pipelineEditorLoad(pipelineID) {
    if (pipelineEditorDirty) {
        if (!confirm('Discard unsaved pipeline changes?')) return;
    }
    const def = pipelineDefinitions.find(p => p && p.id === pipelineID);
    if (!def) {
        alert('Pipeline not found: ' + pipelineID);
        return;
    }
    const steps = Array.isArray(def.steps) ? def.steps : [];
    const first = steps[0] || {};
    const scopeMode = String(def.scope_mode || 'global').trim() || 'global';
    const scopeCompanyID = scopeMode === 'company' ? String(def.scope_company_id || '').trim() : '';
    pipelineEditorDraft = {
        loaded_id: def.id,
        id: def.id || '',
        name: def.name || def.id || '',
        enabled: !!def.enabled,
        schedule: String(def.schedule || '').trim(),
        scope_mode: scopeMode,
        scope_company_id: scopeCompanyID,
        trigger_method: first.on_method || '',
        trigger_status: first.on_status || 'succeeded',
        trigger_from_role: first.from_role || '*',
        actions: steps.length ? steps.map(step => {
            const runner = normalizePipelineActionRunner(step.runner);
            const nextMethod = runner === pipelineStepRunnerBuiltin
                ? pipelineBuiltinMethodDisplayName(step.next_method || '')
                : (step.next_method || '');
            return {
                runner,
                _runner_category: pipelineActionRunnerCategory(runner),
                to_role: step.to_role || '',
                to_agent_id: step.to_agent_id || '',
                next_method: nextMethod,
                param_map_text: JSON.stringify(step.param_map || {}, null, 2),
                fan_out: !!step.fan_out,
                fan_out_key: step.fan_out_key || '',
            };
        }) : [defaultPipelineEditorAction()],
    };
    pipelineEditorDirty = false;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
    refreshPipelineCapabilitiesForCurrentScope(true);
}

function pipelineEditorRender() {
    if (!pipelineEditorDraft) {
        pipelineEditorDraft = defaultPipelineEditorDraft();
    }

    const idEl = document.getElementById('pipeline-editor-id');
    const nameEl = document.getElementById('pipeline-editor-name');
    const enabledEl = document.getElementById('pipeline-editor-enabled');
    const scheduleEl = document.getElementById('pipeline-editor-schedule');
    const scopeModeEl = document.getElementById('pipeline-editor-scope-mode');
    const scopeCompanyEl = document.getElementById('pipeline-editor-scope-company');
    const triggerMethodEl = document.getElementById('pipeline-editor-trigger-method');
    const triggerStatusEl = document.getElementById('pipeline-editor-trigger-status');
    const triggerFromRoleEl = document.getElementById('pipeline-editor-trigger-from-role');
    const delBtn = document.getElementById('pipeline-editor-delete-btn');

    if (idEl) {
        idEl.value = pipelineEditorDraft.id || '';
        idEl.readOnly = !!pipelineEditorDraft.loaded_id;
    }
    if (nameEl) {
        nameEl.value = pipelineEditorDraft.name || '';
    }
    if (enabledEl) {
        enabledEl.checked = !!pipelineEditorDraft.enabled;
    }
    if (scheduleEl) {
        scheduleEl.value = pipelineEditorDraft.schedule || '';
    }
    if (scopeModeEl) {
        scopeModeEl.value = pipelineEditorDraft.scope_mode === 'company' ? 'company' : 'global';
    }
    if (scopeCompanyEl) {
        refreshPipelineEditorCompanyOptions(String(pipelineEditorDraft.scope_company_id || '').trim());
        scopeCompanyEl.value = String(pipelineEditorDraft.scope_company_id || '').trim();
        scopeCompanyEl.disabled = scopeModeEl ? scopeModeEl.value !== 'company' : true;
    }
    if (triggerMethodEl) {
        triggerMethodEl.value = pipelineEditorDraft.trigger_method || '';
    }
    if (triggerStatusEl) {
        triggerStatusEl.value = pipelineEditorDraft.trigger_status || 'succeeded';
    }
    if (triggerFromRoleEl) {
        triggerFromRoleEl.value = pipelineEditorDraft.trigger_from_role || '*';
    }
    if (delBtn) {
        delBtn.disabled = !pipelineEditorDraft.loaded_id;
    }

    renderPipelineEditorActions();
    renderPipelineDefinitions();
}

function refreshPipelineEditorCompanyOptions(selectedCompanyID) {
    const select = document.getElementById('pipeline-editor-scope-company');
    if (!select) return;

    const selectedID = String(selectedCompanyID || select.value || '').trim();
    const options = ['<option value="">Select company...</option>'];
    const ids = new Set();
    for (const company of companySummaries) {
        const id = String(company.id || '').trim();
        if (!id) continue;
        ids.add(id);
        const label = String(company.name || id).trim() || id;
        options.push(`<option value="${escAttr(id)}">${escHtml(label)} (${escHtml(id)})</option>`);
    }
    if (selectedID && !ids.has(selectedID)) {
        options.push(`<option value="${escAttr(selectedID)}">${escHtml(selectedID)} (missing)</option>`);
    }
    select.innerHTML = options.join('');
    select.value = selectedID;
}

function pipelineEditorAgentOptions(selectedAgentID) {
    const selectedID = String(selectedAgentID || '').trim();
    const seen = new Set();
    let html = '<option value="">— Select agent —</option>';
    for (const agent of agents) {
        const agentID = String(agent?.id || '').trim();
        if (!agentID || seen.has(agentID)) continue;
        seen.add(agentID);
        const label = String(agent?.name || agentID).trim() || agentID;
        html += `<option value="${escAttr(agentID)}"${agentID === selectedID ? ' selected' : ''}>${escHtml(label)}</option>`;
    }
    if (selectedID && !seen.has(selectedID)) {
        html += `<option value="${escAttr(selectedID)}" selected>${escHtml(selectedID)} (missing)</option>`;
    }
    return html;
}

function refreshPipelineActionAgentSelects() {
    if (!pipelineEditorDraft || !Array.isArray(pipelineEditorDraft.actions)) return;
    pipelineEditorDraft.actions.forEach((action, idx) => {
        const select = document.getElementById(`pipeline-action-to-agent-id-${idx}`);
        if (!select) return;
        const selectedID = String(action?.to_agent_id || '').trim();
        select.innerHTML = pipelineEditorAgentOptions(selectedID);
        select.value = selectedID;
    });
}

function renderPipelineEditorActions() {
    const container = document.getElementById('pipeline-editor-actions');
    if (!container) return;

    const actions = Array.isArray(pipelineEditorDraft?.actions) ? pipelineEditorDraft.actions : [];
    if (!actions.length) {
        container.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No actions yet.</div>';
        return;
    }

    container.innerHTML = actions.map((a, idx) => {
        const runner = normalizePipelineActionRunner(a.runner);
        a.runner = runner;
        const isClaudeCode = runner === pipelineStepRunnerClaudeCode;
        const isBuiltinCategory = runner === pipelineStepRunnerBuiltin;
        a._runner_category = pipelineActionRunnerCategory(runner);

        const afterMethod = idx === 0
            ? (pipelineEditorDraft.trigger_method || '(trigger)')
            : (actions[idx - 1].next_method || '(previous action)');
        const afterStatus = idx === 0 ? (pipelineEditorDraft.trigger_status || 'succeeded') : 'succeeded';
        const subtitle = `Runs after ${pipelineBuiltinMethodDisplayName(afterMethod) || afterMethod} ${afterStatus}`;

        const builtinMethodOptions = renderPipelineBuiltinMethodOptionList(a.next_method);
        const a2aMethodOptions = renderPipelineA2AMethodOptionList(a.next_method);

        const fanOutChecked = a.fan_out ? 'checked' : '';
        const fanOutKeyDisabled = a.fan_out ? '' : 'disabled';
        const runnerAgentSelected = runner === pipelineStepRunnerAgent ? ' selected' : '';
        const runnerClaudeSelected = runner === pipelineStepRunnerClaudeCode ? ' selected' : '';
        const runnerCodexSelected = runner === pipelineStepRunnerCodex ? ' selected' : '';
        const runnerBuiltinSelected = isBuiltinCategory ? ' selected' : '';

        return `<div class="pipeline-editor-action-card">
            <div class="pipeline-editor-action-header">
                <div style="min-width:0">
                    <div class="pipeline-editor-action-title">Action ${idx + 1}</div>
                    <div class="pipeline-editor-action-subtitle" id="pipeline-action-subtitle-${idx}">${escHtml(subtitle)}</div>
                </div>
                <div class="pipeline-editor-action-controls">
                    <button class="btn btn-sm" onclick="pipelineEditorMoveAction(${idx}, -1)" ${idx === 0 ? 'disabled' : ''}>↑</button>
                    <button class="btn btn-sm" onclick="pipelineEditorMoveAction(${idx}, 1)" ${idx === actions.length - 1 ? 'disabled' : ''}>↓</button>
                    <button class="btn btn-sm" onclick="pipelineEditorDuplicateAction(${idx})">Duplicate</button>
                    <button class="btn btn-sm btn-danger" onclick="pipelineEditorRemoveAction(${idx})" ${actions.length === 1 ? 'disabled' : ''}>Remove</button>
                </div>
            </div>

            <div class="form-row">
                <div class="form-group">
                    <label>Runner</label>
                    <select id="pipeline-action-runner-${idx}" onchange="pipelineEditorUpdateActionField(${idx}, 'runner', this.value)">
                        <option value="${pipelineStepRunnerAgent}"${runnerAgentSelected}>Agent (A2A)</option>
                        <option value="${pipelineStepRunnerClaudeCode}"${runnerClaudeSelected}>Claude Code</option>
                        <option value="${pipelineStepRunnerCodex}"${runnerCodexSelected}>Codex</option>
                        <option value="${pipelineStepRunnerBuiltin}"${runnerBuiltinSelected}>Builtin Method</option>
                    </select>
                </div>
                <div class="form-group">
                    <label>Next Method</label>
                    ${isBuiltinCategory
                        ? `<select id="pipeline-action-next-method-${idx}" onchange="pipelineEditorUpdateActionField(${idx}, 'next_method', this.value)">
                            ${builtinMethodOptions}
                        </select>`
                        : `<select id="pipeline-action-next-method-${idx}" onchange="pipelineEditorUpdateActionField(${idx}, 'next_method', this.value)">
                            ${a2aMethodOptions}
                        </select>`
                    }
                </div>
            </div>

            ${!isBuiltinCategory ? `
            <div class="form-row">
                <div class="form-group">
                    <label>To Role</label>
                    <input type="text" id="pipeline-action-to-role-${idx}" list="pipeline-roles-datalist" value="${escAttr(a.to_role || '')}" oninput="pipelineEditorUpdateActionField(${idx}, 'to_role', this.value)">
                </div>
                <div class="form-group">
                    <label>Target Agent${isClaudeCode ? '' : ' (optional)'}</label>
                    <select id="pipeline-action-to-agent-id-${idx}" onchange="pipelineEditorUpdateActionField(${idx}, 'to_agent_id', this.value); pipelineEditorRender();">
                        ${pipelineEditorAgentOptions(a.to_agent_id)}
                    </select>
                </div>
            </div>
            ` : ''}

            <div class="form-group">
                <label>Param Map (JSON)</label>
                <textarea id="pipeline-action-param-map-${idx}" rows="4" placeholder='{\"$\":\"input\"}' oninput="pipelineEditorUpdateActionField(${idx}, 'param_map_text', this.value)">${escHtml(a.param_map_text || '')}</textarea>
                <div class="help">Use <code>{\"$\":\"input\"}</code> to send prior output as <code>input</code>.</div>
                <div class="pipeline-editor-action-error" id="pipeline-action-error-${idx}" style="display:none"></div>
            </div>

            <div class="form-row">
                <div class="form-group">
                    <label style="font-size:12px;color:var(--text-muted)">
                        <input type="checkbox" id="pipeline-action-fanout-${idx}" ${fanOutChecked} onchange="pipelineEditorUpdateActionField(${idx}, 'fan_out', this.checked)">Fan-out
                    </label>
                </div>
                <div class="form-group">
                    <label>Fan-out Key</label>
                    <input type="text" id="pipeline-action-fanout-key-${idx}" value="${escAttr(a.fan_out_key || '')}" ${fanOutKeyDisabled} placeholder="items" oninput="pipelineEditorUpdateActionField(${idx}, 'fan_out_key', this.value)">
                </div>
            </div>
        </div>`;
    }).join('');

    pipelineEditorRefreshActionSubtitles();
}

function pipelineEditorUpdateMeta() {
    if (!pipelineEditorDraft) pipelineEditorDraft = defaultPipelineEditorDraft();
    const idEl = document.getElementById('pipeline-editor-id');
    const nameEl = document.getElementById('pipeline-editor-name');
    const enabledEl = document.getElementById('pipeline-editor-enabled');
    const scheduleEl = document.getElementById('pipeline-editor-schedule');
    if (idEl && !pipelineEditorDraft.loaded_id) pipelineEditorDraft.id = idEl.value;
    if (nameEl) pipelineEditorDraft.name = nameEl.value;
    if (enabledEl) pipelineEditorDraft.enabled = !!enabledEl.checked;
    if (scheduleEl) pipelineEditorDraft.schedule = scheduleEl.value.trim();
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorUpdateValidation();
    renderPipelineDefinitions();
}

function pipelineEditorUpdateScope() {
    if (!pipelineEditorDraft) pipelineEditorDraft = defaultPipelineEditorDraft();
    const modeEl = document.getElementById('pipeline-editor-scope-mode');
    const companyEl = document.getElementById('pipeline-editor-scope-company');

    const scopeMode = modeEl && modeEl.value === 'company' ? 'company' : 'global';
    const scopeCompanyID = companyEl ? companyEl.value.trim() : '';

    pipelineEditorDraft.scope_mode = scopeMode;
    pipelineEditorDraft.scope_company_id = scopeCompanyID;
    if (companyEl) {
        companyEl.disabled = scopeMode !== 'company';
    }

    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorUpdateValidation();
    renderPipelineDefinitions();
    refreshPipelineCapabilitiesForCurrentScope(true);
}

function pipelineEditorUpdateTrigger() {
    if (!pipelineEditorDraft) pipelineEditorDraft = defaultPipelineEditorDraft();
    const methodEl = document.getElementById('pipeline-editor-trigger-method');
    const statusEl = document.getElementById('pipeline-editor-trigger-status');
    const fromRoleEl = document.getElementById('pipeline-editor-trigger-from-role');
    if (methodEl) pipelineEditorDraft.trigger_method = methodEl.value;
    if (statusEl) pipelineEditorDraft.trigger_status = statusEl.value;
    if (fromRoleEl) pipelineEditorDraft.trigger_from_role = fromRoleEl.value;
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorUpdateValidation();
    pipelineEditorRefreshActionSubtitles();
}

function pipelineEditorAddAction() {
    if (!pipelineEditorDraft) pipelineEditorDraft = defaultPipelineEditorDraft();
    if (!Array.isArray(pipelineEditorDraft.actions)) pipelineEditorDraft.actions = [];
    pipelineEditorDraft.actions.push(defaultPipelineEditorAction());
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
}

function pipelineEditorRemoveAction(idx) {
    if (!pipelineEditorDraft || !Array.isArray(pipelineEditorDraft.actions)) return;
    if (pipelineEditorDraft.actions.length <= 1) return;
    pipelineEditorDraft.actions.splice(idx, 1);
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
}

function pipelineEditorDuplicateAction(idx) {
    if (!pipelineEditorDraft || !Array.isArray(pipelineEditorDraft.actions)) return;
    const src = pipelineEditorDraft.actions[idx];
    if (!src) return;
    const copy = {
        runner: normalizePipelineActionRunner(src.runner),
        _runner_category: pipelineActionRunnerCategory(src._runner_category || src.runner),
        to_role: src.to_role || '',
        to_agent_id: src.to_agent_id || '',
        next_method: src.next_method || '',
        param_map_text: src.param_map_text || '',
        fan_out: !!src.fan_out,
        fan_out_key: src.fan_out_key || '',
    };
    pipelineEditorDraft.actions.splice(idx + 1, 0, copy);
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
}

function pipelineEditorMoveAction(idx, delta) {
    if (!pipelineEditorDraft || !Array.isArray(pipelineEditorDraft.actions)) return;
    const j = idx + delta;
    if (j < 0 || j >= pipelineEditorDraft.actions.length) return;
    const tmp = pipelineEditorDraft.actions[idx];
    pipelineEditorDraft.actions[idx] = pipelineEditorDraft.actions[j];
    pipelineEditorDraft.actions[j] = tmp;
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
}

function pipelineEditorToggleClaudeCode(actionIdx, checked) {
    if (!pipelineEditorDraft || !Array.isArray(pipelineEditorDraft.actions)) return;
    const a = pipelineEditorDraft.actions[actionIdx];
    if (!a) return;
    if (checked) {
        a._runner_category = pipelineStepRunnerAgent;
        a.runner = pipelineStepRunnerClaudeCode;
    } else {
        a.runner = pipelineStepRunnerAgent;
        a.to_agent_id = '';
    }
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
}

function pipelineEditorUpdateActionField(actionIdx, field, value) {
    if (!pipelineEditorDraft || !Array.isArray(pipelineEditorDraft.actions)) return;
    const a = pipelineEditorDraft.actions[actionIdx];
    if (!a) return;
    const runner = normalizePipelineActionRunner(a.runner);
    a.runner = runner;

    if (field === 'runner') {
        const nextRunner = normalizePipelineActionRunner(value);
        a._runner_category = pipelineActionRunnerCategory(nextRunner);
        a.runner = nextRunner;
        if (nextRunner === pipelineStepRunnerBuiltin) {
            a.runner = pipelineStepRunnerBuiltin;
            a.to_role = '';
            a.to_agent_id = '';
            if (isKnownPipelineBuiltinMethod(a.next_method)) {
                a.next_method = pipelineBuiltinMethodDisplayName(a.next_method);
            } else {
                const firstVisible = pipelineBuiltinMethodCatalog.find(b => b && !b.hidden);
                a.next_method = pipelineBuiltinMethodDisplayName(firstVisible?.canonical || '');
            }
        } else {
            if (runner === pipelineStepRunnerBuiltin && isKnownPipelineBuiltinMethod(a.next_method)) {
                a.next_method = '';
            }
        }
        pipelineEditorDirty = true;
        pipelineEditorClearResult();
        pipelineEditorRender();
        pipelineEditorUpdateValidation();
        pipelineEditorRefreshActionSubtitles();
        return;
    }

    const category = pipelineActionRunnerCategory(a._runner_category || runner);
    if (field === 'next_method' && category === pipelineStepRunnerBuiltin) {
        a.next_method = pipelineBuiltinMethodDisplayName(value);
    } else {
        a[field] = value;
    }

    if (field === 'fan_out') {
        const keyEl = document.getElementById(`pipeline-action-fanout-key-${actionIdx}`);
        if (keyEl) {
            keyEl.disabled = !a.fan_out;
        }
        if (!value) {
            // Keep the key in state, but stop showing parse/required errors.
        }
    }

    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorUpdateValidation();

    if (field === 'next_method') {
        // Subsequent action subtitles depend on the previous next_method.
        pipelineEditorRefreshActionSubtitles();
    }
}

function pipelineEditorRefreshActionSubtitles() {
    if (!pipelineEditorDraft || !Array.isArray(pipelineEditorDraft.actions)) return;
    const actions = pipelineEditorDraft.actions;
    for (let i = 0; i < actions.length; i++) {
        const el = document.getElementById(`pipeline-action-subtitle-${i}`);
        if (!el) continue;
        const after = i === 0
            ? (pipelineEditorDraft.trigger_method || '(trigger)')
            : (actions[i - 1].next_method || '(previous action)');
        const afterStatus = i === 0 ? (pipelineEditorDraft.trigger_status || 'succeeded') : 'succeeded';
        el.textContent = `Runs after ${pipelineBuiltinMethodDisplayName(after) || after} ${afterStatus}`;
    }
}

function pipelineEditorValidateDraft() {
    const errors = [];
    const actionErrors = {};
    if (!pipelineEditorDraft) return { errors: ['No pipeline loaded'], actionErrors };

    const id = (pipelineEditorDraft.id || '').trim();
    const name = (pipelineEditorDraft.name || '').trim();
    const scopeMode = (pipelineEditorDraft.scope_mode || '').trim() || 'global';
    const scopeCompanyID = (pipelineEditorDraft.scope_company_id || '').trim();
    const triggerMethod = (pipelineEditorDraft.trigger_method || '').trim();
    const triggerStatus = (pipelineEditorDraft.trigger_status || '').trim() || 'succeeded';
    const triggerFromRole = (pipelineEditorDraft.trigger_from_role || '').trim();

    if (!id) {
        errors.push('Pipeline ID is required');
    } else if (id.length > 256) {
        errors.push('Pipeline ID is too long (max 256 chars)');
    } else if (/[\u0000-\u001f\u007f]/.test(id)) {
        errors.push('Pipeline ID contains control characters');
    }
    if (!name) {
        errors.push('Pipeline name is required');
    }
    if (!['global', 'company'].includes(scopeMode)) {
        errors.push('Scope mode must be global or company');
    }
    if (scopeMode === 'company' && !scopeCompanyID) {
        errors.push('Scope company is required when scope mode is company');
    }
    if (!triggerMethod) {
        errors.push('Trigger method is required');
    }
    if (!['succeeded', 'failed', '*'].includes(triggerStatus)) {
        errors.push('Trigger status must be succeeded, failed, or *');
    }
    if (triggerFromRole && triggerFromRole !== '*' && !/^[a-zA-Z0-9._-]{1,128}$/.test(triggerFromRole)) {
        errors.push('From role contains invalid characters (use * or a simple token)');
    }

    const actions = Array.isArray(pipelineEditorDraft.actions) ? pipelineEditorDraft.actions : [];
    if (!actions.length) {
        errors.push('At least one action is required');
        return { errors, actionErrors };
    }

    actions.forEach((a, idx) => {
        const prefix = `Action ${idx + 1}:`;
        const runner = normalizePipelineActionRunner(a.runner);
        const toRole = String(a.to_role || '').trim();
        const toAgentID = String(a.to_agent_id || '').trim();
        if (runner === pipelineStepRunnerAgent && !toRole && !toAgentID) {
            errors.push(`${prefix} to_role or target agent is required for Agent (A2A) steps`);
        }
        if (runner === pipelineStepRunnerClaudeCode && !toAgentID) {
            errors.push(`${prefix} Claude Code step needs a target agent (runs the claude CLI inside the agent container)`);
        }
        if (runner === pipelineStepRunnerCodex && !toAgentID) {
            errors.push(`${prefix} Codex step needs a target agent (runs the codex CLI inside the agent container)`);
        }
        if (!a.next_method || !String(a.next_method).trim()) {
            errors.push(`${prefix} next_method is required`);
        } else if (runner === pipelineStepRunnerBuiltin && !isKnownPipelineBuiltinMethod(a.next_method)) {
            errors.push(`${prefix} unknown builtin method "${String(a.next_method || '').trim()}"`);
        } else if (runner === pipelineStepRunnerClaudeCode && isKnownPipelineBuiltinMethod(a.next_method)) {
            errors.push(`${prefix} Claude Code step cannot use builtin method "${String(a.next_method || '').trim()}" — switch runner to Builtin Method or use an A2A method name`);
        } else if (runner === pipelineStepRunnerCodex && isKnownPipelineBuiltinMethod(a.next_method)) {
            errors.push(`${prefix} Codex step cannot use builtin method "${String(a.next_method || '').trim()}" — switch runner to Builtin Method or use an A2A method name`);
        }
        try {
            parseStringMapInput(a.param_map_text || '', `${prefix} param map`);
        } catch (e) {
            actionErrors[idx] = e.message || String(e);
            errors.push(`${prefix} param map is invalid`);
        }
        if (a.fan_out) {
            if (!a.fan_out_key || !String(a.fan_out_key).trim()) {
                errors.push(`${prefix} fan_out_key is required when fan-out is enabled`);
            }
        }
    });

    return { errors, actionErrors };
}

function isValidPipelineID(id) {
    const text = String(id || '').trim();
    if (!text) return false;
    if (text.length > 256) return false;
    if (/[\u0000-\u001f\u007f]/.test(text)) return false;
    return true;
}

function pipelineEditorUpdateValidation() {
    const saveBtn = document.getElementById('pipeline-editor-save-btn');
    const { errors, actionErrors } = pipelineEditorValidateDraft();
    setPipelineEditorValidation(errors);

    // Per-action param map error display.
    if (pipelineEditorDraft && Array.isArray(pipelineEditorDraft.actions)) {
        for (let i = 0; i < pipelineEditorDraft.actions.length; i++) {
            const el = document.getElementById(`pipeline-action-error-${i}`);
            if (!el) continue;
            const msg = actionErrors[i];
            if (msg) {
                el.textContent = msg;
                el.style.display = '';
            } else {
                el.textContent = '';
                el.style.display = 'none';
            }
        }
    }

    if (saveBtn) {
        saveBtn.disabled = errors.length > 0;
    }
}

function buildPipelineUpsertRequestFromEditor() {
    const draft = pipelineEditorDraft;
    if (!draft) throw new Error('No pipeline loaded');

    const triggerMethod = String(draft.trigger_method || '').trim();
    const triggerStatus = String(draft.trigger_status || '').trim() || 'succeeded';
    const triggerFromRole = String(draft.trigger_from_role || '').trim() || '*';
    const scopeMode = String(draft.scope_mode || '').trim() || 'global';
    const scopeCompanyID = String(draft.scope_company_id || '').trim();

    const actions = Array.isArray(draft.actions) ? draft.actions : [];
    const steps = actions.map((a, idx) => {
        const runner = normalizePipelineActionRunner(a.runner);
        const prevAction = idx > 0 ? actions[idx - 1] : null;
        let onMethod = idx === 0 ? triggerMethod : String(prevAction?.next_method || '').trim();
        if (prevAction) {
            if (normalizePipelineActionRunner(prevAction.runner) === pipelineStepRunnerBuiltin) {
                onMethod = normalizePipelineBuiltinMethod(onMethod);
            }
        }
        const onStatus = idx === 0 ? triggerStatus : 'succeeded';
        const fromRole = idx === 0 ? triggerFromRole : '*';
        const paramMap = parseStringMapInput(a.param_map_text || '', `Action ${idx + 1} param map`);
        const nextMethodRaw = String(a.next_method || '').trim();
        const nextMethod = runner === pipelineStepRunnerBuiltin
            ? normalizePipelineBuiltinMethod(nextMethodRaw)
            : nextMethodRaw;
        const step = {
            runner,
            on_method: onMethod,
            on_status: onStatus,
            from_role: fromRole,
            to_role: runner === pipelineStepRunnerBuiltin ? '' : String(a.to_role || '').trim(),
            to_agent_id: runner === pipelineStepRunnerBuiltin ? '' : String(a.to_agent_id || '').trim(),
            next_method: nextMethod,
            param_map: paramMap,
        };
        if (a.fan_out) {
            step.fan_out = true;
            step.fan_out_key = String(a.fan_out_key || '').trim();
        }
        return step;
    });

    const req = {
        id: String(draft.id || '').trim(),
        name: String(draft.name || '').trim(),
        enabled: !!draft.enabled,
        schedule: String(draft.schedule || '').trim(),
        scope_mode: scopeMode,
        steps,
    };
    if (scopeMode === 'company') {
        req.scope_company_id = scopeCompanyID;
    }
    return req;
}

async function pipelineEditorSave() {
    try {
        pipelineEditorUpdateMeta();
        pipelineEditorUpdateScope();
        pipelineEditorUpdateTrigger();
        const { errors } = pipelineEditorValidateDraft();
        if (errors.length) {
            setPipelineEditorResult('error', 'Fix validation errors before saving.');
            return;
        }
        const req = buildPipelineUpsertRequestFromEditor();
        const resp = await api('POST', '/api/pipelines', req);
        pipelineEditorDraft.loaded_id = req.id;
        pipelineEditorDirty = false;
        pipelineEditorRender();
        setPipelineEditorResult('ok', `Saved pipeline ${req.id} (${resp.status || 'ok'})`);
        await loadPipelineDefinitions();
    } catch (e) {
        setPipelineEditorResult('error', `Failed to save pipeline: ${e.message}`);
    }
}

async function pipelineEditorDelete() {
    if (!pipelineEditorDraft || !pipelineEditorDraft.loaded_id) return;
    const pipelineID = pipelineEditorDraft.loaded_id;
    if (!confirm(`Delete pipeline ${pipelineID}?`)) return;
    try {
        await api('DELETE', `/api/pipelines/${encodeURIComponent(pipelineID)}`);
        await loadPipelineDefinitions();
        pipelineEditorDirty = false;
        pipelineEditorDraft = null;
        pipelineEditorNew(true);
        setPipelineEditorResult('ok', `Deleted pipeline ${pipelineID}`);
    } catch (e) {
        setPipelineEditorResult('error', `Failed to delete pipeline: ${e.message}`);
    }
}

function pipelineEditorDuplicate() {
    if (!pipelineEditorDraft) return;
    const baseID = String(pipelineEditorDraft.id || 'pipeline').trim() || 'pipeline';
    const suffix = Date.now().toString().slice(-6);
    const suggested = `${baseID}_copy_${suffix}`.replace(/[^a-zA-Z0-9._-]+/g, '_').slice(0, 128);
    const newID = prompt('New pipeline ID:', suggested);
    if (!newID || !newID.trim()) return;
    if (!isValidPipelineID(newID)) {
        alert('Invalid pipeline ID.');
        return;
    }
    pipelineEditorDraft.loaded_id = null;
    pipelineEditorDraft.id = newID.trim();
    pipelineEditorDraft.name = (pipelineEditorDraft.name || baseID) + ' (copy)';
    pipelineEditorDirty = true;
    pipelineEditorClearResult();
    pipelineEditorRender();
    pipelineEditorUpdateValidation();
}

function attachPipelineDefinitionsListHandlers() {
    const container = document.getElementById('pipelines-list');
    if (!container) return;
    if (container.dataset.handlersAttached === '1') return;
    container.dataset.handlersAttached = '1';

    container.addEventListener('click', async (e) => {
        const btn = e.target.closest('button');
        const item = e.target.closest('.pipeline-definition-item');
        const pipelineID = btn?.dataset?.pipelineId || item?.dataset?.pipelineId;
        if (!pipelineID) return;

        if (!btn) {
            pipelineEditorLoad(pipelineID);
            return;
        }
        if (btn.classList.contains('pipeline-def-trigger')) {
            if (pipelineTriggerInFlight.has(pipelineID)) return;
            pipelineTriggerInFlight.add(pipelineID);
            renderPipelineDefinitions();
            try {
                await triggerPipelineByID(pipelineID);
            } finally {
                pipelineTriggerInFlight.delete(pipelineID);
                renderPipelineDefinitions();
            }
            return;
        }
        if (btn.classList.contains('pipeline-def-edit')) {
            pipelineEditorLoad(pipelineID);
            return;
        }
        if (btn.classList.contains('pipeline-def-duplicate')) {
            const def = pipelineDefinitions.find(p => p && p.id === pipelineID);
            if (!def) return;
            pipelineEditorLoad(pipelineID);
            pipelineEditorDuplicate();
            return;
        }
        if (btn.classList.contains('pipeline-def-toggle')) {
            await togglePipelineEnabled(pipelineID);
            return;
        }
        if (btn.classList.contains('pipeline-def-delete')) {
            await deletePipelineDefinitionByID(pipelineID);
            return;
        }
    });
}

async function togglePipelineEnabled(pipelineID) {
    const def = pipelineDefinitions.find(p => p && p.id === pipelineID);
    if (!def) return alert('Pipeline not found: ' + pipelineID);
    const desired = !def.enabled;
    const scopeMode = String(def.scope_mode || 'global').trim() || 'global';
    try {
        const payload = {
            id: def.id,
            name: def.name,
            enabled: desired,
            scope_mode: scopeMode,
            steps: Array.isArray(def.steps) ? def.steps : [],
        };
        if (scopeMode === 'company') {
            payload.scope_company_id = String(def.scope_company_id || '').trim();
        }
        await api('POST', '/api/pipelines', payload);
        await loadPipelineDefinitions();
        if (pipelineEditorDraft && pipelineEditorDraft.loaded_id === pipelineID) {
            const enabledEl = document.getElementById('pipeline-editor-enabled');
            pipelineEditorDraft.enabled = desired;
            if (enabledEl) enabledEl.checked = desired;
        }
        setPipelineEditorResult('ok', `${desired ? 'Enabled' : 'Disabled'} pipeline ${pipelineID}`);
    } catch (e) {
        setPipelineEditorResult('error', `Failed to toggle pipeline: ${e.message}`);
    }
}

async function deletePipelineDefinitionByID(pipelineID) {
    if (!confirm(`Delete pipeline ${pipelineID}?`)) return;
    try {
        await api('DELETE', `/api/pipelines/${encodeURIComponent(pipelineID)}`);
        await loadPipelineDefinitions();
        if (pipelineEditorDraft && pipelineEditorDraft.loaded_id === pipelineID) {
            pipelineEditorDraft = null;
            pipelineEditorDirty = false;
            pipelineEditorNew(true);
        }
        setPipelineEditorResult('ok', `Deleted pipeline ${pipelineID}`);
    } catch (e) {
        setPipelineEditorResult('error', `Failed to delete pipeline: ${e.message}`);
    }
}
