// ============================================================
// A2A Methods (global)
// ============================================================

async function loadA2AMethods() {
    try {
        const data = await api('GET', '/api/a2a-methods');
        const methods = Array.isArray(data.methods) ? data.methods : [];
        a2aMethods = methods.slice().sort((a, b) => String(a.method || '').localeCompare(String(b.method || '')));
    } catch (e) {
        a2aMethods = [];
    }
}

const METHOD_SCHEMA_TEMPLATES = {
    object_loose: {
        type: 'object',
        required: ['field_name'],
        properties: {
            field_name: { type: 'string' },
        },
    },
    object_strict: {
        type: 'object',
        required: ['field_name'],
        properties: {
            field_name: { type: 'string' },
        },
        additionalProperties: false,
    },
    status_response: {
        type: 'object',
        required: ['status'],
        properties: {
            status: { type: 'string', enum: ['ok', 'error'] },
            message: { type: 'string' },
        },
        additionalProperties: false,
    },
};

function methodSchemaTargetIds(target) {
    const t = String(target || '').toLowerCase();
    if (t === 'output') {
        return {
            schemaId: 'a2a-method-form-output-schema',
            exampleId: 'a2a-method-form-output-example',
            label: 'Output',
        };
    }
    return {
        schemaId: 'a2a-method-form-input-schema',
        exampleId: 'a2a-method-form-input-example',
        label: 'Input',
    };
}

function applyMethodSchemaTemplate(target, templateKey) {
    const ids = methodSchemaTargetIds(target);
    const schemaEl = document.getElementById(ids.schemaId);
    if (!schemaEl) return;

    const template = METHOD_SCHEMA_TEMPLATES[String(templateKey || '').trim()];
    if (!template) return;

    schemaEl.value = JSON.stringify(template, null, 2);
}

function generateSchemaFromMethodExample(target) {
    const ids = methodSchemaTargetIds(target);
    const schemaEl = document.getElementById(ids.schemaId);
    const exampleEl = document.getElementById(ids.exampleId);
    if (!schemaEl || !exampleEl) return;

    const raw = String(exampleEl.value || '').trim();
    if (!raw) {
        alert(`${ids.label} example JSON is empty`);
        return;
    }

    let sample;
    try {
        sample = JSON.parse(raw);
    } catch (e) {
        alert(`${ids.label} example must be valid JSON`);
        return;
    }

    const inferred = inferJSONSchemaFromSample(sample);
    schemaEl.value = JSON.stringify(inferred, null, 2);
}

function inferJSONSchemaFromSample(value) {
    if (value === null) return { type: 'null' };

    if (Array.isArray(value)) {
        if (value.length === 0) {
            return { type: 'array', items: {} };
        }
        const itemSchemas = value.map(item => inferJSONSchemaFromSample(item));
        const deduped = dedupeSchemas(itemSchemas);
        if (deduped.length === 1) {
            return { type: 'array', items: deduped[0] };
        }
        return { type: 'array', items: { oneOf: deduped } };
    }

    const t = typeof value;
    if (t === 'string') return { type: 'string' };
    if (t === 'boolean') return { type: 'boolean' };
    if (t === 'number') return { type: Number.isInteger(value) ? 'integer' : 'number' };

    if (t === 'object') {
        const props = {};
        const required = [];
        for (const key of Object.keys(value)) {
            props[key] = inferJSONSchemaFromSample(value[key]);
            required.push(key);
        }
        const schema = { type: 'object', properties: props };
        if (required.length) schema.required = required;
        return schema;
    }

    return {};
}

function dedupeSchemas(schemas) {
    const seen = new Set();
    const out = [];
    for (const schema of schemas) {
        const key = JSON.stringify(schema);
        if (seen.has(key)) continue;
        seen.add(key);
        out.push(schema);
    }
    return out;
}

function renderA2AMethodsList() {
    const listEl = document.getElementById('a2a-methods-list');
    if (!listEl) return;

    if (!a2aMethods.length) {
        listEl.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:6px 0">No methods defined.</div>';
        return;
    }

    listEl.innerHTML = a2aMethods.map(m => {
        const methodName = String(m.method || '');
        const desc = String(m.description || '');
        const instructions = String(m.instructions || '').trim();
        const modelTier = String(m.model_tier || '').trim();
        const autoMarketNote = !!m.auto_market_note;
        const freshContext = !!m.fresh_context;
        const redactMarketPrices = !!m.redact_market_prices;
        const disableMarketNotes = !!m.disable_market_notes;
        const disablePolymarketNoteAugmentation = !!m.disable_polymarket_note_augmentation;
        const inputSchemaJSON = m.input_schema ? escHtml(JSON.stringify(m.input_schema, null, 2)) : '';
        const outputSchemaJSON = m.output_schema ? escHtml(JSON.stringify(m.output_schema, null, 2)) : '';
        return `<div class="pipeline-definition-item" style="background:var(--bg);margin-bottom:8px">
            <div style="display:flex;justify-content:space-between;gap:10px;align-items:flex-start">
                <div style="min-width:0">
                    <div class="pipeline-definition-title">${escHtml(methodName || '(unnamed)')}</div>
                    ${desc ? `<div class="pipeline-definition-meta">${escHtml(desc)}</div>` : ''}
                    ${modelTier === 'fast' ? `<div class="pipeline-definition-meta" style="margin-top:4px;color:var(--accent)">Model tier: fast</div>` : ''}
                    ${autoMarketNote ? `<div class="pipeline-definition-meta" style="margin-top:4px;color:var(--accent)">Auto market note enabled</div>` : ''}
                    ${freshContext ? `<div class="pipeline-definition-meta" style="margin-top:4px;color:var(--accent)">Fresh empty context on every call</div>` : ''}
                    ${redactMarketPrices ? `<div class="pipeline-definition-meta" style="margin-top:4px;color:var(--accent)">Redact live market prices from pipeline input</div>` : ''}
                    ${disableMarketNotes ? `<div class="pipeline-definition-meta" style="margin-top:4px;color:var(--accent)">Direct market-note access disabled</div>` : ''}
                    ${disablePolymarketNoteAugmentation ? `<div class="pipeline-definition-meta" style="margin-top:4px;color:var(--accent)">Polymarket note augmentation disabled</div>` : ''}
                    ${instructions ? `<details class="capability-schema"><summary>Method instructions</summary><pre>${escHtml(instructions)}</pre></details>` : ''}
                    ${inputSchemaJSON ? `<details class="capability-schema"><summary>Input schema</summary><pre>${inputSchemaJSON}</pre></details>` : ''}
                    ${outputSchemaJSON ? `<details class="capability-schema"><summary>Output schema</summary><pre>${outputSchemaJSON}</pre></details>` : ''}
                </div>
                <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;justify-content:flex-end">
                    <button class="btn btn-sm" onclick="editA2AMethod('${escAttr(methodName)}')">Edit</button>
                    <button class="btn btn-sm" onclick="cloneA2AMethod('${escAttr(methodName)}')">Clone</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteA2AMethod('${escAttr(methodName)}')">Delete</button>
                </div>
            </div>
        </div>`;
    }).join('');
}

function showA2AMethods() {
    showPipelines('methods');
    clearA2AMethodForm();
}

function closeA2AMethods() {
    closePipelines();
}

function clearA2AMethodForm() {
    a2aMethodEditing = null;
    const methodEl = document.getElementById('a2a-method-form-method');
    const descEl = document.getElementById('a2a-method-form-desc');
    const instructionsEl = document.getElementById('a2a-method-form-instructions');
    const modelTierEl = document.getElementById('a2a-method-form-model-tier');
    const autoMarketNoteEl = document.getElementById('a2a-method-form-auto-market-note');
    const freshContextEl = document.getElementById('a2a-method-form-fresh-context');
    const redactMarketPricesEl = document.getElementById('a2a-method-form-redact-market-prices');
    const disableMarketNotesEl = document.getElementById('a2a-method-form-disable-market-notes');
    const disablePolymarketNoteAugmentationEl = document.getElementById('a2a-method-form-disable-polymarket-note-augmentation');
    const completionTimestampKeyEl = document.getElementById('a2a-method-form-completion-timestamp-key');
    const completionSuccessKeyEl = document.getElementById('a2a-method-form-completion-success-key');
    const inputEl = document.getElementById('a2a-method-form-input-schema');
    const outputEl = document.getElementById('a2a-method-form-output-schema');
    const inputExampleEl = document.getElementById('a2a-method-form-input-example');
    const outputExampleEl = document.getElementById('a2a-method-form-output-example');
    const saveBtn = document.getElementById('a2a-method-form-save');
    const titleEl = document.getElementById('a2a-method-form-title');

    if (titleEl) titleEl.textContent = 'Create Method';
    if (methodEl) { methodEl.value = ''; methodEl.disabled = false; }
    if (descEl) descEl.value = '';
    if (instructionsEl) instructionsEl.value = '';
    if (modelTierEl) modelTierEl.value = '';
    if (autoMarketNoteEl) autoMarketNoteEl.checked = false;
    if (freshContextEl) freshContextEl.checked = false;
    if (redactMarketPricesEl) redactMarketPricesEl.checked = false;
    if (disableMarketNotesEl) disableMarketNotesEl.checked = false;
    if (disablePolymarketNoteAugmentationEl) disablePolymarketNoteAugmentationEl.checked = false;
    if (completionTimestampKeyEl) completionTimestampKeyEl.value = '';
    if (completionSuccessKeyEl) completionSuccessKeyEl.value = '';
    if (inputEl) inputEl.value = '';
    if (outputEl) outputEl.value = '';
    if (inputExampleEl) inputExampleEl.value = '';
    if (outputExampleEl) outputExampleEl.value = '';
    if (saveBtn) saveBtn.textContent = 'Create';
}

function editA2AMethod(methodName) {
    const m = a2aMethods.find(x => String(x.method || '') === String(methodName || ''));
    if (!m) return;
    a2aMethodEditing = String(m.method || '').trim() || null;

    const methodEl = document.getElementById('a2a-method-form-method');
    const descEl = document.getElementById('a2a-method-form-desc');
    const instructionsEl = document.getElementById('a2a-method-form-instructions');
    const modelTierEl = document.getElementById('a2a-method-form-model-tier');
    const autoMarketNoteEl = document.getElementById('a2a-method-form-auto-market-note');
    const freshContextEl = document.getElementById('a2a-method-form-fresh-context');
    const redactMarketPricesEl = document.getElementById('a2a-method-form-redact-market-prices');
    const disableMarketNotesEl = document.getElementById('a2a-method-form-disable-market-notes');
    const disablePolymarketNoteAugmentationEl = document.getElementById('a2a-method-form-disable-polymarket-note-augmentation');
    const completionTimestampKeyEl = document.getElementById('a2a-method-form-completion-timestamp-key');
    const completionSuccessKeyEl = document.getElementById('a2a-method-form-completion-success-key');
    const inputEl = document.getElementById('a2a-method-form-input-schema');
    const outputEl = document.getElementById('a2a-method-form-output-schema');
    const inputExampleEl = document.getElementById('a2a-method-form-input-example');
    const outputExampleEl = document.getElementById('a2a-method-form-output-example');
    const saveBtn = document.getElementById('a2a-method-form-save');
    const titleEl = document.getElementById('a2a-method-form-title');

    if (titleEl) titleEl.textContent = 'Edit Method';
    if (methodEl) { methodEl.value = a2aMethodEditing || ''; methodEl.disabled = true; }
    if (descEl) descEl.value = String(m.description || '');
    if (instructionsEl) instructionsEl.value = String(m.instructions || '');
    if (modelTierEl) modelTierEl.value = String(m.model_tier || '');
    if (autoMarketNoteEl) autoMarketNoteEl.checked = !!m.auto_market_note;
    if (freshContextEl) freshContextEl.checked = !!m.fresh_context;
    if (redactMarketPricesEl) redactMarketPricesEl.checked = !!m.redact_market_prices;
    if (disableMarketNotesEl) disableMarketNotesEl.checked = !!m.disable_market_notes;
    if (disablePolymarketNoteAugmentationEl) disablePolymarketNoteAugmentationEl.checked = !!m.disable_polymarket_note_augmentation;
    if (completionTimestampKeyEl) completionTimestampKeyEl.value = String(m.completion_timestamp_key || '');
    if (completionSuccessKeyEl) completionSuccessKeyEl.value = String(m.completion_success_key || '');
    if (inputEl) inputEl.value = m.input_schema ? JSON.stringify(m.input_schema, null, 2) : '';
    if (outputEl) outputEl.value = m.output_schema ? JSON.stringify(m.output_schema, null, 2) : '';
    if (inputExampleEl) inputExampleEl.value = '';
    if (outputExampleEl) outputExampleEl.value = '';
    if (saveBtn) saveBtn.textContent = 'Update';
}

function cloneA2AMethod(methodName) {
    const m = a2aMethods.find(x => String(x.method || '') === String(methodName || ''));
    if (!m) return;
    a2aMethodEditing = null;

    const methodEl = document.getElementById('a2a-method-form-method');
    const descEl = document.getElementById('a2a-method-form-desc');
    const instructionsEl = document.getElementById('a2a-method-form-instructions');
    const modelTierEl = document.getElementById('a2a-method-form-model-tier');
    const autoMarketNoteEl = document.getElementById('a2a-method-form-auto-market-note');
    const freshContextEl = document.getElementById('a2a-method-form-fresh-context');
    const redactMarketPricesEl = document.getElementById('a2a-method-form-redact-market-prices');
    const disableMarketNotesEl = document.getElementById('a2a-method-form-disable-market-notes');
    const disablePolymarketNoteAugmentationEl = document.getElementById('a2a-method-form-disable-polymarket-note-augmentation');
    const completionTimestampKeyEl = document.getElementById('a2a-method-form-completion-timestamp-key');
    const completionSuccessKeyEl = document.getElementById('a2a-method-form-completion-success-key');
    const inputEl = document.getElementById('a2a-method-form-input-schema');
    const outputEl = document.getElementById('a2a-method-form-output-schema');
    const inputExampleEl = document.getElementById('a2a-method-form-input-example');
    const outputExampleEl = document.getElementById('a2a-method-form-output-example');
    const saveBtn = document.getElementById('a2a-method-form-save');
    const titleEl = document.getElementById('a2a-method-form-title');

    if (titleEl) titleEl.textContent = 'Clone Method';
    if (methodEl) { methodEl.value = ''; methodEl.disabled = false; methodEl.focus(); }
    if (descEl) descEl.value = String(m.description || '');
    if (instructionsEl) instructionsEl.value = String(m.instructions || '');
    if (modelTierEl) modelTierEl.value = String(m.model_tier || '');
    if (autoMarketNoteEl) autoMarketNoteEl.checked = !!m.auto_market_note;
    if (freshContextEl) freshContextEl.checked = !!m.fresh_context;
    if (redactMarketPricesEl) redactMarketPricesEl.checked = !!m.redact_market_prices;
    if (disableMarketNotesEl) disableMarketNotesEl.checked = !!m.disable_market_notes;
    if (disablePolymarketNoteAugmentationEl) disablePolymarketNoteAugmentationEl.checked = !!m.disable_polymarket_note_augmentation;
    if (completionTimestampKeyEl) completionTimestampKeyEl.value = String(m.completion_timestamp_key || '');
    if (completionSuccessKeyEl) completionSuccessKeyEl.value = String(m.completion_success_key || '');
    if (inputEl) inputEl.value = m.input_schema ? JSON.stringify(m.input_schema, null, 2) : '';
    if (outputEl) outputEl.value = m.output_schema ? JSON.stringify(m.output_schema, null, 2) : '';
    if (inputExampleEl) inputExampleEl.value = '';
    if (outputExampleEl) outputExampleEl.value = '';
    if (saveBtn) saveBtn.textContent = 'Create';
}

async function saveA2AMethod() {
    const methodEl = document.getElementById('a2a-method-form-method');
    const descEl = document.getElementById('a2a-method-form-desc');
    const instructionsEl = document.getElementById('a2a-method-form-instructions');
    const modelTierEl = document.getElementById('a2a-method-form-model-tier');
    const autoMarketNoteEl = document.getElementById('a2a-method-form-auto-market-note');
    const freshContextEl = document.getElementById('a2a-method-form-fresh-context');
    const redactMarketPricesEl = document.getElementById('a2a-method-form-redact-market-prices');
    const disableMarketNotesEl = document.getElementById('a2a-method-form-disable-market-notes');
    const disablePolymarketNoteAugmentationEl = document.getElementById('a2a-method-form-disable-polymarket-note-augmentation');
    const completionTimestampKeyEl = document.getElementById('a2a-method-form-completion-timestamp-key');
    const completionSuccessKeyEl = document.getElementById('a2a-method-form-completion-success-key');
    const inputEl = document.getElementById('a2a-method-form-input-schema');
    const outputEl = document.getElementById('a2a-method-form-output-schema');
    if (!methodEl || !descEl || !instructionsEl || !autoMarketNoteEl || !freshContextEl || !redactMarketPricesEl || !disableMarketNotesEl || !disablePolymarketNoteAugmentationEl || !inputEl || !outputEl) return;

    const method = String(methodEl.value || '').trim();
    const description = String(descEl.value || '').trim();
    const instructions = String(instructionsEl.value || '').trim();
    const model_tier = modelTierEl ? String(modelTierEl.value || '').trim() : '';
    const auto_market_note = !!autoMarketNoteEl.checked;
    const fresh_context = !!freshContextEl.checked;
    const redact_market_prices = !!redactMarketPricesEl.checked;
    const disable_market_notes = !!disableMarketNotesEl.checked;
    const disable_polymarket_note_augmentation = !!disablePolymarketNoteAugmentationEl.checked;
    const completion_timestamp_key = completionTimestampKeyEl ? String(completionTimestampKeyEl.value || '').trim() : '';
    const completion_success_key = completionSuccessKeyEl ? String(completionSuccessKeyEl.value || '').trim() : '';
    const inputSchemaText = String(inputEl.value || '').trim();
    const outputSchemaText = String(outputEl.value || '').trim();

    if (!method) return alert('Method is required');

    let input_schema = null;
    let output_schema = null;
    if (inputSchemaText) {
        try { input_schema = JSON.parse(inputSchemaText); } catch (e) { return alert('Input schema must be valid JSON. Use "Generate from example" if needed.'); }
        if (!input_schema || typeof input_schema !== 'object' || Array.isArray(input_schema)) return alert('Input schema must be a JSON object. Use "Generate from example" if needed.');
    }
    if (outputSchemaText) {
        try { output_schema = JSON.parse(outputSchemaText); } catch (e) { return alert('Output schema must be valid JSON. Use "Generate from example" if needed.'); }
        if (!output_schema || typeof output_schema !== 'object' || Array.isArray(output_schema)) return alert('Output schema must be a JSON object. Use "Generate from example" if needed.');
    }

    const payload = {
        method,
        description,
        instructions,
        model_tier: model_tier || null,
        auto_market_note,
        fresh_context,
        redact_market_prices,
        disable_market_notes,
        disable_polymarket_note_augmentation,
        completion_timestamp_key,
        completion_success_key,
        input_schema,
        output_schema,
    };

    try {
        if (a2aMethodEditing) {
            await api('PUT', `/api/a2a-methods/${encodeURIComponent(a2aMethodEditing)}`, payload);
        } else {
            await api('POST', '/api/a2a-methods', payload);
        }
        await loadA2AMethods();
        renderA2AMethodsList();
        renderPipelineEditorDatalists();
        // Capability add forms and capability lists reference method metadata.
        displayedAgents.forEach(state => {
            if (!isBuiltinMethodsColumnState(state)) loadCapabilitiesInColumn(state);
        });
        clearA2AMethodForm();
    } catch (e) {
        alert('Failed to save method: ' + e.message);
    }
}

async function deleteA2AMethod(methodName) {
    const method = String(methodName || '').trim();
    if (!method) return;
    if (!confirm(`Delete method "${method}"?`)) return;
    try {
        await api('DELETE', `/api/a2a-methods/${encodeURIComponent(method)}`);
        await loadA2AMethods();
        renderA2AMethodsList();
        renderPipelineEditorDatalists();
        displayedAgents.forEach(state => {
            if (!isBuiltinMethodsColumnState(state)) loadCapabilitiesInColumn(state);
        });
        if (a2aMethodEditing === method) clearA2AMethodForm();
    } catch (e) {
        alert('Failed to delete method: ' + e.message);
    }
}
