// ============================================================
// Deep Research Methods (global)
// ============================================================

async function loadDeepResearchMethods() {
    try {
        const data = await api('GET', '/api/deep-research-methods');
        const methods = Array.isArray(data.methods) ? data.methods : [];
        deepResearchMethods = methods.slice().sort((a, b) => String(a.method || '').localeCompare(String(b.method || '')));
    } catch (e) {
        deepResearchMethods = [];
    }
}

function clearDeepResearchTestResult() {
    const el = document.getElementById('deep-research-test-result');
    if (!el) return;
    el.style.display = 'none';
    el.textContent = '';
    el.className = 'pipelines-result';
}

function setDeepResearchTestResult(kind, text) {
    const el = document.getElementById('deep-research-test-result');
    if (!el) return;
    el.className = `pipelines-result ${kind}`;
    el.textContent = text;
    el.style.display = '';
}

function renderDeepResearchMethodsList() {
    const listEl = document.getElementById('deep-research-methods-list');
    if (!listEl) return;

    if (!deepResearchMethods.length) {
        listEl.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No deep research methods defined.</div>';
        return;
    }

    listEl.innerHTML = deepResearchMethods.map((m, idx) => {
        const methodName = String(m.method || '');
        const desc = String(m.description || '');
        const instr = String(m.instructions || '');
        const enabled = !!m.enabled;
        const inputSchemaJSON = m.input_schema ? escHtml(JSON.stringify(m.input_schema, null, 2)) : '';
        const researchSchemaJSON = m.research_schema ? escHtml(JSON.stringify(m.research_schema, null, 2)) : '';
        const optionsJSON = m.options ? escHtml(JSON.stringify(m.options, null, 2)) : '';
        const lastTested = m.last_tested_at ? formatDate(m.last_tested_at) : 'never';
        const rowInputID = `deep-research-row-test-input-${idx}`;
        if (!Object.prototype.hasOwnProperty.call(deepResearchMethodTestInputs, methodName)) {
            deepResearchMethodTestInputs[methodName] = '{}';
        }
        const rowInputJSON = escHtml(String(deepResearchMethodTestInputs[methodName] || '{}'));

        return `<div class="pipeline-definition-item" style="background:var(--bg);margin-bottom:8px">
            <div style="display:flex;justify-content:space-between;gap:10px;align-items:flex-start">
                <div style="min-width:0">
                    <div class="pipeline-definition-title">${escHtml(methodName || '(unnamed)')} ${enabled ? '' : '<span class="badge" style="margin-left:6px">disabled</span>'}</div>
                    ${desc ? `<div class="pipeline-definition-meta">${escHtml(desc)}</div>` : ''}
                    ${instr ? `<details class="capability-schema"><summary>Instructions</summary><pre>${escHtml(instr)}</pre></details>` : ''}
                    <details class="capability-schema">
                        <summary>Test input JSON</summary>
                        <textarea id="${escAttr(rowInputID)}" rows="4" style="width:100%;margin-top:8px" oninput="setDeepResearchMethodTestInput('${escAttr(methodName)}', this.value)">${rowInputJSON}</textarea>
                    </details>
                    ${inputSchemaJSON ? `<details class="capability-schema"><summary>Input schema</summary><pre>${inputSchemaJSON}</pre></details>` : ''}
                    ${researchSchemaJSON ? `<details class="capability-schema"><summary>Research schema</summary><pre>${researchSchemaJSON}</pre></details>` : ''}
                    ${optionsJSON ? `<details class="capability-schema"><summary>Options</summary><pre>${optionsJSON}</pre></details>` : ''}
                    <div class="pipeline-definition-meta">Last tested: ${escHtml(lastTested)}</div>
                </div>
                <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;justify-content:flex-end">
                    <button class="btn btn-sm" onclick="testDeepResearchMethodFromList('${escAttr(methodName)}')">Test</button>
                    <button class="btn btn-sm" onclick="editDeepResearchMethod('${escAttr(methodName)}')">Edit</button>
                    <button class="btn btn-sm" onclick="cloneDeepResearchMethod('${escAttr(methodName)}')">Clone</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteDeepResearchMethod('${escAttr(methodName)}')">Delete</button>
                </div>
            </div>
        </div>`;
    }).join('');
}

function clearDeepResearchMethodForm() {
    deepResearchMethodEditing = null;
    const methodEl = document.getElementById('deep-research-form-method');
    const descEl = document.getElementById('deep-research-form-desc');
    const instructionsEl = document.getElementById('deep-research-form-instructions');
    const inputSchemaEl = document.getElementById('deep-research-form-input-schema');
    const researchSchemaEl = document.getElementById('deep-research-form-research-schema');
    const optionsEl = document.getElementById('deep-research-form-options');
    const testInputEl = document.getElementById('deep-research-form-test-input');
    const enabledEl = document.getElementById('deep-research-form-enabled');
    const titleEl = document.getElementById('deep-research-form-title');
    const saveBtn = document.getElementById('deep-research-form-save');

    if (methodEl) { methodEl.value = ''; methodEl.disabled = false; }
    if (descEl) descEl.value = '';
    if (instructionsEl) instructionsEl.value = '';
    if (inputSchemaEl) inputSchemaEl.value = '';
    if (researchSchemaEl) researchSchemaEl.value = '';
    if (optionsEl) optionsEl.value = '';
    if (testInputEl) testInputEl.value = '{}';
    if (enabledEl) enabledEl.checked = true;
    if (titleEl) titleEl.textContent = 'Create Deep Research Method';
    if (saveBtn) saveBtn.textContent = 'Create';
    clearDeepResearchTestResult();
}

function editDeepResearchMethod(methodName) {
    const m = deepResearchMethods.find(x => String(x.method || '') === String(methodName || ''));
    if (!m) return;
    deepResearchMethodEditing = String(m.method || '').trim() || null;

    const methodEl = document.getElementById('deep-research-form-method');
    const descEl = document.getElementById('deep-research-form-desc');
    const instructionsEl = document.getElementById('deep-research-form-instructions');
    const inputSchemaEl = document.getElementById('deep-research-form-input-schema');
    const researchSchemaEl = document.getElementById('deep-research-form-research-schema');
    const optionsEl = document.getElementById('deep-research-form-options');
    const enabledEl = document.getElementById('deep-research-form-enabled');
    const titleEl = document.getElementById('deep-research-form-title');
    const saveBtn = document.getElementById('deep-research-form-save');

    if (methodEl) { methodEl.value = deepResearchMethodEditing || ''; methodEl.disabled = true; }
    if (descEl) descEl.value = String(m.description || '');
    if (instructionsEl) instructionsEl.value = String(m.instructions || '');
    if (inputSchemaEl) inputSchemaEl.value = m.input_schema ? JSON.stringify(m.input_schema, null, 2) : '';
    if (researchSchemaEl) researchSchemaEl.value = m.research_schema ? JSON.stringify(m.research_schema, null, 2) : '';
    if (optionsEl) optionsEl.value = m.options ? JSON.stringify(m.options, null, 2) : '';
    if (enabledEl) enabledEl.checked = m.enabled !== false;
    if (titleEl) titleEl.textContent = 'Edit Deep Research Method';
    if (saveBtn) saveBtn.textContent = 'Update';
    clearDeepResearchTestResult();
}

function cloneDeepResearchMethod(methodName) {
    const m = deepResearchMethods.find(x => String(x.method || '') === String(methodName || ''));
    if (!m) return;
    deepResearchMethodEditing = null;

    const methodEl = document.getElementById('deep-research-form-method');
    const descEl = document.getElementById('deep-research-form-desc');
    const instructionsEl = document.getElementById('deep-research-form-instructions');
    const inputSchemaEl = document.getElementById('deep-research-form-input-schema');
    const researchSchemaEl = document.getElementById('deep-research-form-research-schema');
    const optionsEl = document.getElementById('deep-research-form-options');
    const enabledEl = document.getElementById('deep-research-form-enabled');
    const titleEl = document.getElementById('deep-research-form-title');
    const saveBtn = document.getElementById('deep-research-form-save');

    if (methodEl) { methodEl.value = ''; methodEl.disabled = false; methodEl.focus(); }
    if (descEl) descEl.value = String(m.description || '');
    if (instructionsEl) instructionsEl.value = String(m.instructions || '');
    if (inputSchemaEl) inputSchemaEl.value = m.input_schema ? JSON.stringify(m.input_schema, null, 2) : '';
    if (researchSchemaEl) researchSchemaEl.value = m.research_schema ? JSON.stringify(m.research_schema, null, 2) : '';
    if (optionsEl) optionsEl.value = m.options ? JSON.stringify(m.options, null, 2) : '';
    if (enabledEl) enabledEl.checked = m.enabled !== false;
    if (titleEl) titleEl.textContent = 'Clone Deep Research Method';
    if (saveBtn) saveBtn.textContent = 'Create';
    clearDeepResearchTestResult();
}

async function saveDeepResearchMethod() {
    const methodEl = document.getElementById('deep-research-form-method');
    const descEl = document.getElementById('deep-research-form-desc');
    const instructionsEl = document.getElementById('deep-research-form-instructions');
    const inputSchemaEl = document.getElementById('deep-research-form-input-schema');
    const researchSchemaEl = document.getElementById('deep-research-form-research-schema');
    const optionsEl = document.getElementById('deep-research-form-options');
    const enabledEl = document.getElementById('deep-research-form-enabled');
    if (!methodEl || !descEl || !inputSchemaEl || !researchSchemaEl || !optionsEl || !enabledEl) return;

    const method = String(methodEl.value || '').trim();
    const description = String(descEl.value || '').trim();
    const instructions = instructionsEl ? String(instructionsEl.value || '').trim() : '';
    const inputSchemaText = String(inputSchemaEl.value || '').trim();
    const researchSchemaText = String(researchSchemaEl.value || '').trim();
    const optionsText = String(optionsEl.value || '').trim();
    const enabled = !!enabledEl.checked;

    if (!method) return alert('Method is required');

    let input_schema = null;
    let research_schema = null;
    let options = null;
    if (inputSchemaText) {
        try { input_schema = JSON.parse(inputSchemaText); } catch (e) { return alert('Input schema must be valid JSON.'); }
        if (!input_schema || typeof input_schema !== 'object' || Array.isArray(input_schema)) return alert('Input schema must be a JSON object.');
    }
    if (researchSchemaText) {
        try { research_schema = JSON.parse(researchSchemaText); } catch (e) { return alert('Research schema must be valid JSON.'); }
        if (!research_schema || typeof research_schema !== 'object' || Array.isArray(research_schema)) return alert('Research schema must be a JSON object.');
    }
    if (optionsText) {
        try { options = JSON.parse(optionsText); } catch (e) { return alert('Options must be valid JSON.'); }
        if (!options || typeof options !== 'object' || Array.isArray(options)) return alert('Options must be a JSON object.');
    }

    const payload = { method, description, instructions, input_schema, research_schema, options, enabled };
    try {
        if (deepResearchMethodEditing) {
            await api('PUT', `/api/deep-research-methods/${encodeURIComponent(deepResearchMethodEditing)}`, payload);
        } else {
            await api('POST', '/api/deep-research-methods', payload);
        }
        await loadDeepResearchMethods();
        renderDeepResearchMethodsList();
        clearDeepResearchMethodForm();
    } catch (e) {
        alert('Failed to save deep research method: ' + e.message);
    }
}

async function deleteDeepResearchMethod(methodName) {
    const method = String(methodName || '').trim();
    if (!method) return;
    if (!confirm(`Delete deep research method "${method}"?`)) return;
    try {
        await api('DELETE', `/api/deep-research-methods/${encodeURIComponent(method)}`);
        await loadDeepResearchMethods();
        renderDeepResearchMethodsList();
        if (deepResearchMethodEditing === method) clearDeepResearchMethodForm();
        clearDeepResearchTestResult();
    } catch (e) {
        alert('Failed to delete deep research method: ' + e.message);
    }
}

function deepResearchCurrentTestInput() {
    const testInputEl = document.getElementById('deep-research-form-test-input');
    const raw = String(testInputEl?.value || '');
    return parseDeepResearchTestInputJSON(raw, 'Test input');
}

function parseDeepResearchTestInputJSON(raw, label) {
    const text = String(raw || '').trim();
    if (!text) return {};
    let parsed;
    try {
        parsed = JSON.parse(text);
    } catch (e) {
        throw new Error(`${label} must be valid JSON`);
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error(`${label} must be a JSON object`);
    }
    return parsed;
}

function setDeepResearchMethodTestInput(methodName, value) {
    const method = String(methodName || '').trim();
    if (!method) return;
    deepResearchMethodTestInputs[method] = String(value || '');
}

function deepResearchListTestInput(methodName) {
    const method = String(methodName || '').trim();
    if (!method) return {};
    const raw = Object.prototype.hasOwnProperty.call(deepResearchMethodTestInputs, method)
        ? deepResearchMethodTestInputs[method]
        : '{}';
    return parseDeepResearchTestInputJSON(raw, `Test input for ${method}`);
}

async function testDeepResearchMethodFromForm() {
    const methodEl = document.getElementById('deep-research-form-method');
    if (!methodEl) return;
    const method = String(methodEl.value || '').trim();
    if (!method) {
        alert('Choose or create a method first.');
        return;
    }
    let input = {};
    try {
        input = deepResearchCurrentTestInput();
    } catch (e) {
        alert(e.message);
        return;
    }
    await testDeepResearchMethod(method, input);
}

async function testDeepResearchMethodFromList(methodName) {
    const method = String(methodName || '').trim();
    if (!method) return;
    let input = {};
    try {
        input = deepResearchListTestInput(method);
    } catch (e) {
        alert(e.message);
        return;
    }
    await testDeepResearchMethod(method, input);
}

async function testDeepResearchMethod(methodName, inputOverride) {
    const method = String(methodName || '').trim();
    if (!method) return;
    const input = (inputOverride && typeof inputOverride === 'object' && !Array.isArray(inputOverride))
        ? inputOverride
        : {};
    clearDeepResearchTestResult();
    setDeepResearchTestResult('ok', `Running test for ${method}...\nWaiting for live sources...`);

    const liveSources = [];
    const seenSources = new Set();
    let phase = 'Running';

    const renderLiveSources = () => {
        const lines = [
            `Method: ${method}`,
            `Status: ${phase}`,
            `Live sources discovered: ${liveSources.length}`,
        ];
        if (liveSources.length) {
            lines.push('');
            lines.push('Live sources:');
            liveSources.forEach((s, idx) => {
                const objective = s.objective ? ` [${s.objective}]` : '';
                const title = s.title ? `${s.title} | ` : '';
                lines.push(`${idx + 1}. ${title}${s.url}${objective}`);
            });
        }
        setDeepResearchTestResult('ok', lines.join('\n'));
    };

    try {
        const response = await fetch(`/api/deep-research-methods/${encodeURIComponent(method)}/test-stream`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ input }),
        });
        if (!response.ok) {
            let message = `HTTP ${response.status}`;
            try {
                const payload = await response.json();
                if (payload && payload.error) message = String(payload.error);
            } catch (_) {}
            throw new Error(message);
        }
        if (!response.body || !response.body.getReader) {
            throw new Error('streaming is not supported by this browser');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let finalTest = null;
        renderLiveSources();

        while (true) {
            const { value, done } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            let newlineIndex = buffer.indexOf('\n');
            while (newlineIndex >= 0) {
                const rawLine = buffer.slice(0, newlineIndex).trim();
                buffer = buffer.slice(newlineIndex + 1);
                if (rawLine) {
                    const evt = JSON.parse(rawLine);
                    if (evt.type === 'progress' && evt.event && typeof evt.event === 'object') {
                        const progress = evt.event;
                        const stage = String(progress.stage || '');
                        if (stage === 'round_start') {
                            const round = Number(progress.round || 0);
                            phase = round > 0 ? `Running round ${round}` : 'Running';
                            renderLiveSources();
                        } else if (stage === 'source') {
                            const url = String(progress.url || '').trim();
                            if (url && !seenSources.has(url)) {
                                seenSources.add(url);
                                liveSources.push({
                                    url,
                                    title: String(progress.title || '').trim(),
                                    objective: String(progress.objective_key || '').trim(),
                                });
                                renderLiveSources();
                            }
                        } else if (stage === 'run_complete') {
                            phase = 'Finalizing';
                            renderLiveSources();
                        }
                    } else if (evt.type === 'error') {
                        throw new Error(String(evt.error || 'deep research test failed'));
                    } else if (evt.type === 'done') {
                        finalTest = evt.test || null;
                    }
                }
                newlineIndex = buffer.indexOf('\n');
            }
        }

        if (!finalTest) {
            throw new Error('stream ended before final result');
        }

        setDeepResearchTestResult('ok', formatDeepResearchTestResultText(method, finalTest, liveSources));
        await loadDeepResearchMethods();
        renderDeepResearchMethodsList();
    } catch (e) {
        setDeepResearchTestResult('error', `Deep research test failed: ${e.message}`);
    }
}

function formatDeepResearchTestResultText(method, test, liveSources) {
    const result = (test && typeof test.result === 'object' && test.result) ? test.result : {};
    const summary = String(result.summary || '').trim();
    const objectiveCount = Array.isArray(result.objectives) ? result.objectives.length : 0;
    const sourceCount = Array.isArray(result.sources) ? result.sources.length : 0;
    const warnings = Array.isArray(result.warnings) ? result.warnings : [];
    const schemaSatisfied = result.schema_satisfied;
    const missingObjectiveKeys = Array.isArray(result.missing_objective_keys) ? result.missing_objective_keys : [];
    const hasOutput = Object.prototype.hasOwnProperty.call(result || {}, 'output');
    let outputJSON = '';
    if (hasOutput) {
        try {
            outputJSON = JSON.stringify(result.output, null, 2);
        } catch (e) {
            outputJSON = String(result.output || '');
        }
    }

    const lines = [
        `Method: ${method}`,
        `Query: ${test.query || ''}`,
        `Objectives: ${objectiveCount}`,
        `Sources: ${sourceCount}`,
        `Duration: ${test.duration_ms || 0}ms`,
        `Schema satisfied: ${schemaSatisfied === true ? 'yes' : (schemaSatisfied === false ? 'no' : 'unknown')}`,
    ];
    if (Array.isArray(liveSources) && liveSources.length) {
        lines.push(`Live sources discovered: ${liveSources.length}`);
    }
    if (missingObjectiveKeys.length) {
        lines.push(`Missing objectives: ${missingObjectiveKeys.join(', ')}`);
    }
    if (outputJSON) {
        lines.push('');
        lines.push('Output JSON:');
        lines.push(outputJSON);
    }
    if (summary) {
        lines.push('');
        lines.push('Summary:');
        lines.push(summary);
    }
    if (warnings.length) {
        lines.push('');
        lines.push('Warnings:');
        warnings.forEach(w => lines.push(`- ${w}`));
    }
    return lines.join('\n');
}

