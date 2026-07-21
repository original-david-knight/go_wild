// Self-contained node:test harness for the pipelines frontend.
//
// No package.json or npm install needed — uses only built-in node:test + vm.
// Run with: node --test gowild_agent_manager/static/app_pipelines.test.mjs
//
// Scope: the trigger-button debounce behavior. The concern this guards
// against: re-renders of the pipelines list (fired on toggle/delete/refresh)
// previously wiped `btn.dataset.busy`, re-enabling the button mid-request.
// Debounce state now lives in a module-level Set; these tests pin that.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const appJS = readFileSync(resolve(here, 'app.js'), 'utf8');
const appPipelinesJS = readFileSync(resolve(here, 'app_pipelines.js'), 'utf8');

// Appended to the VM script so the test can reach into the script-scoped
// `let` bindings (pipelineDefinitions, pipelineTriggerInFlight, etc.) that
// would otherwise be invisible from outside vm.runInContext.
const testShim = `
globalThis.__test = {
    setDefinitions: (d) => { pipelineDefinitions = d; },
    setInFlight: (ids) => { pipelineTriggerInFlight = new Set(ids); },
    getInFlight: () => Array.from(pipelineTriggerInFlight),
    setEditorDraft: (d) => { pipelineEditorDraft = d; },
    render: () => renderPipelineDefinitions(),
    validate: () => pipelineEditorValidateDraft(),
    runners: {
        claudeCode: pipelineStepRunnerClaudeCode,
        codex: pipelineStepRunnerCodex,
    },
    // Expose the live builtin-method catalog so tests don't have to hard-code
    // a specific method name (which would couple them to a fixture that may
    // be renamed or removed independently of the validator copy).
    someBuiltinMethod: () => {
        const entry = (pipelineBuiltinMethodCatalog || []).find((m) => m && m.canonical);
        return entry ? entry.canonical : null;
    },
    // Tests for the hidden-method behavior want to drive a specific entry
    // but not couple to its canonical name.
    someHiddenBuiltin: () => {
        const entry = (pipelineBuiltinMethodCatalog || []).find((m) => m && m.canonical && m.hidden);
        return entry ? { canonical: entry.canonical, display: entry.display || entry.canonical } : null;
    },
    someVisibleBuiltin: () => {
        const entry = (pipelineBuiltinMethodCatalog || []).find((m) => m && m.canonical && !m.hidden);
        return entry ? { canonical: entry.canonical, display: entry.display || entry.canonical } : null;
    },
    renderBuiltinOptions: (selected) => renderPipelineBuiltinMethodOptionList(selected),
    renderDatalists: () => renderPipelineEditorDatalists(),
    isKnownBuiltin: (m) => isKnownPipelineBuiltinMethod(m),
    // Drive the runner-switch path (field='runner' -> builtin) so tests can
    // confirm the default method auto-populated on the draft is not a hidden
    // debug entry even if the catalog is reordered.
    setupDraftForRunnerSwitch: () => {
        pipelineEditorDraft = {
            id: 'p-test',
            name: 'Test Pipeline',
            scope_mode: 'global',
            scope_company_id: '',
            trigger_method: 'some_method',
            trigger_status: 'succeeded',
            trigger_from_role: '*',
            actions: [{
                runner: 'agent',
                to_role: 'r',
                to_agent_id: '',
                next_method: 'unknown_method_xyz',
                param_map_text: '',
                fan_out: false,
                fan_out_key: '',
            }],
        };
    },
    switchActionToBuiltin: () => pipelineEditorUpdateActionField(0, 'runner', 'builtin'),
    firstActionNextMethod: () => pipelineEditorDraft.actions[0].next_method,
};
`;

function makeContext() {
    const pipelinesListEl = {
        dataset: {},
        innerHTML: '',
        addEventListener: () => {},
        querySelectorAll: () => [],
    };
    const methodsDatalistEl = { innerHTML: '' };
    const rolesDatalistEl = { innerHTML: '' };
    const ctx = {
        console,
        setInterval: () => 0,
        clearInterval: () => {},
        setTimeout: (fn) => { return 0; },
        clearTimeout: () => {},
        window: {
            addEventListener: () => {},
            removeEventListener: () => {},
            location: { pathname: '/', href: '' },
            history: { pushState: () => {}, replaceState: () => {} },
            WebSocket: function () {},
        },
        document: {
            getElementById: (id) => {
                if (id === 'pipelines-list') return pipelinesListEl;
                if (id === 'pipeline-methods-datalist') return methodsDatalistEl;
                if (id === 'pipeline-roles-datalist') return rolesDatalistEl;
                return null;
            },
            querySelectorAll: () => [],
            querySelector: () => null,
            addEventListener: () => {},
        },
        // Escape helpers invoked inside the render template.
        escAttr: (s) => String(s ?? '').replace(/"/g, '&quot;').replace(/&/g, '&amp;'),
        escHtml: (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;'),
        // Other helpers the file may touch; render paths don't exercise them
        // but they're referenced at load time in nested closures.
        formatDate: (s) => String(s ?? ''),
        api: async () => ({}),
        alert: () => {},
        confirm: () => false,
        marked: { parse: (s) => s, setOptions: () => {} },
    };
    ctx.globalThis = ctx;
    vm.createContext(ctx);
    return { ctx, pipelinesListEl, methodsDatalistEl, rolesDatalistEl };
}

function loadScripts(ctx) {
    vm.runInContext(appJS, ctx);
    vm.runInContext(appPipelinesJS, ctx);
    vm.runInContext(testShim, ctx);
}

test('trigger button shows "Trigger" when pipeline is idle', () => {
    const { ctx, pipelinesListEl } = makeContext();
    loadScripts(ctx);
    ctx.__test.setEditorDraft(null);
    ctx.__test.setDefinitions([{ id: 'p1', name: 'Pipe1', enabled: true, steps: [] }]);
    ctx.__test.setInFlight([]);
    ctx.__test.render();
    const html = pipelinesListEl.innerHTML;
    assert.match(html, /class="[^"]*pipeline-def-trigger[^"]*"[^>]*data-pipeline-id="p1"(?![^>]*\bdisabled\b)[^>]*>Trigger</);
    assert.doesNotMatch(html, /Triggering\.\.\./);
});

test('trigger button shows "Triggering..." and is disabled while in-flight', () => {
    const { ctx, pipelinesListEl } = makeContext();
    loadScripts(ctx);
    ctx.__test.setEditorDraft(null);
    ctx.__test.setDefinitions([{ id: 'p1', name: 'Pipe1', enabled: true, steps: [] }]);
    ctx.__test.setInFlight(['p1']);
    ctx.__test.render();
    const html = pipelinesListEl.innerHTML;
    assert.match(html, /class="[^"]*pipeline-def-trigger[^"]*"[^>]*data-pipeline-id="p1"[^>]* disabled[^>]*>Triggering\.\.\.</);
});

test('in-flight state survives a re-render (the TODO regression)', () => {
    // Simulates the bug path: a trigger is in flight, then a sibling
    // action (toggle/delete/refresh) re-renders the list. The freshly
    // rendered button must still reflect the busy state, not reset to
    // a clickable "Trigger" that would allow a duplicate request.
    const { ctx, pipelinesListEl } = makeContext();
    loadScripts(ctx);
    ctx.__test.setEditorDraft(null);
    ctx.__test.setDefinitions([{ id: 'p1', name: 'Pipe1', enabled: true, steps: [] }]);
    ctx.__test.setInFlight(['p1']);
    ctx.__test.render();
    const firstHTML = pipelinesListEl.innerHTML;
    ctx.__test.render();
    const secondHTML = pipelinesListEl.innerHTML;
    assert.equal(secondHTML, firstHTML);
    assert.match(secondHTML, /Triggering\.\.\./);
});

test('only the in-flight pipeline is busy; siblings remain clickable', () => {
    const { ctx, pipelinesListEl } = makeContext();
    loadScripts(ctx);
    ctx.__test.setEditorDraft(null);
    ctx.__test.setDefinitions([
        { id: 'p1', name: 'Pipe1', enabled: true, steps: [] },
        { id: 'p2', name: 'Pipe2', enabled: true, steps: [] },
    ]);
    ctx.__test.setInFlight(['p1']);
    ctx.__test.render();
    const html = pipelinesListEl.innerHTML;
    assert.match(html, /data-pipeline-id="p1"[^>]* disabled[^>]*>Triggering\.\.\./);
    assert.match(html, /data-pipeline-id="p2"(?![^>]*\bdisabled\b)[^>]*>Trigger</);
});

test('disabled pipelines are still disabled when not in-flight', () => {
    const { ctx, pipelinesListEl } = makeContext();
    loadScripts(ctx);
    ctx.__test.setEditorDraft(null);
    ctx.__test.setDefinitions([{ id: 'p1', name: 'Pipe1', enabled: false, steps: [] }]);
    ctx.__test.setInFlight([]);
    ctx.__test.render();
    const html = pipelinesListEl.innerHTML;
    assert.match(html, /data-pipeline-id="p1"[^>]* disabled[^>]*>Trigger</);
});

// --- Codex vs Claude Code validation: error messages must be distinguishable.
//
// The concern this guards: Codex validation mirrors Claude Code validation
// (both require a target agent, both reject builtin methods). If the error
// messages differ only by the substring "Claude Code" vs "Codex", a user
// with mixed runners in one pipeline sees four near-identical bullet points
// that are easy to mis-read. These tests pin that the two runners produce
// genuinely distinct error text.

function makeDraftWithAction(overrides) {
    return {
        id: 'p-test',
        name: 'Test Pipeline',
        scope_mode: 'global',
        scope_company_id: '',
        trigger_method: 'some_method',
        trigger_status: 'succeeded',
        trigger_from_role: '*',
        actions: [{
            runner: 'agent',
            to_role: '',
            to_agent_id: '',
            next_method: '',
            param_map_text: '',
            fan_out: false,
            fan_out_key: '',
            ...overrides,
        }],
    };
}

function actionErrors(errors, actionIdx = 0) {
    const prefix = `Action ${actionIdx + 1}:`;
    return errors.filter((e) => e.startsWith(prefix));
}

test('Claude Code missing-target-agent error names the claude CLI', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.claudeCode,
        next_method: 'ask_model',
    }));
    const { errors } = ctx.__test.validate();
    const a1 = actionErrors(errors);
    const match = a1.find((e) => /target agent/i.test(e));
    assert.ok(match, `expected a target-agent error, got: ${JSON.stringify(a1)}`);
    assert.match(match, /Claude Code step/);
    assert.match(match, /claude CLI/);
    assert.doesNotMatch(match, /codex CLI/);
});

test('Codex missing-target-agent error names the codex CLI', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.codex,
        next_method: 'ask_model',
    }));
    const { errors } = ctx.__test.validate();
    const a1 = actionErrors(errors);
    const match = a1.find((e) => /target agent/i.test(e));
    assert.ok(match, `expected a target-agent error, got: ${JSON.stringify(a1)}`);
    assert.match(match, /Codex step/);
    assert.match(match, /codex CLI/);
    assert.doesNotMatch(match, /claude CLI/);
});

test('Claude Code vs Codex missing-target-agent errors are not substring-equal', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);

    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.claudeCode,
        next_method: 'ask_model',
    }));
    const claudeErr = actionErrors(ctx.__test.validate().errors)
        .find((e) => /target agent/i.test(e));

    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.codex,
        next_method: 'ask_model',
    }));
    const codexErr = actionErrors(ctx.__test.validate().errors)
        .find((e) => /target agent/i.test(e));

    assert.ok(claudeErr && codexErr);
    // The behavioral requirement is "distinguishable", not "structurally
    // identical except for swappable tokens". Pin distinctness only; leave
    // future copywriting free to improve wording without breaking the test.
    assert.notEqual(claudeErr, codexErr);
});

test('Claude Code builtin-method error interpolates the method name and names the runner', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    const builtin = ctx.__test.someBuiltinMethod();
    assert.ok(builtin, 'pipelineBuiltinMethodCatalog must expose at least one canonical method');
    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.claudeCode,
        to_agent_id: 'agent-1',
        next_method: builtin,
    }));
    const { errors } = ctx.__test.validate();
    const match = actionErrors(errors).find((e) => /builtin method/i.test(e));
    assert.ok(match, `expected a builtin-method error, got: ${JSON.stringify(actionErrors(errors))}`);
    assert.match(match, /Claude Code step/);
    assert.ok(match.includes(`"${builtin}"`), `expected method name "${builtin}" in error: ${match}`);
});

test('Codex builtin-method error interpolates the method name and names the runner', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    const builtin = ctx.__test.someBuiltinMethod();
    assert.ok(builtin);
    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.codex,
        to_agent_id: 'agent-1',
        next_method: builtin,
    }));
    const { errors } = ctx.__test.validate();
    const match = actionErrors(errors).find((e) => /builtin method/i.test(e));
    assert.ok(match, `expected a builtin-method error, got: ${JSON.stringify(actionErrors(errors))}`);
    assert.match(match, /Codex step/);
    assert.ok(match.includes(`"${builtin}"`), `expected method name "${builtin}" in error: ${match}`);
});

test('Claude Code vs Codex builtin-method errors differ on the runner name, not the remedy alone', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    const builtin = ctx.__test.someBuiltinMethod();
    assert.ok(builtin);

    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.claudeCode,
        to_agent_id: 'agent-1',
        next_method: builtin,
    }));
    const claudeErr = actionErrors(ctx.__test.validate().errors)
        .find((e) => /builtin method/i.test(e));

    ctx.__test.setEditorDraft(makeDraftWithAction({
        runner: ctx.__test.runners.codex,
        to_agent_id: 'agent-1',
        next_method: builtin,
    }));
    const codexErr = actionErrors(ctx.__test.validate().errors)
        .find((e) => /builtin method/i.test(e));

    assert.ok(claudeErr && codexErr);
    assert.notEqual(claudeErr, codexErr);
    assert.match(claudeErr, /Claude Code step/);
    assert.match(codexErr, /Codex step/);
});

// --- Hidden (debug-only) builtin methods: kept in the catalog so the
// backend handler still resolves and existing pipelines round-trip, but
// stripped from the editor's discovery surfaces so operators don't stumble
// onto them alongside real production methods. These tests pin that split.

test('catalog exposes at least one hidden builtin (the UI-contract fixture)', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    const hidden = ctx.__test.someHiddenBuiltin();
    assert.ok(hidden, 'expected at least one catalog entry with hidden: true — the subsequent tests assume this');
});

test('dropdown omits hidden builtins when nothing selected', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    const hidden = ctx.__test.someHiddenBuiltin();
    if (!hidden) return;
    const html = ctx.__test.renderBuiltinOptions('');
    assert.ok(!html.includes(`value="${hidden.display}"`),
        `hidden method "${hidden.display}" should not appear in dropdown options: ${html}`);
    assert.ok(!html.includes(hidden.canonical),
        `hidden method canonical "${hidden.canonical}" should not appear either: ${html}`);
});

test('dropdown includes a hidden builtin when it is the currently-selected value', () => {
    // Backward-compat: a pipeline saved before the hide was applied must
    // still show its selected method in the dropdown; otherwise the option
    // renders as "(custom)" and the editor drifts on re-save.
    const { ctx } = makeContext();
    loadScripts(ctx);
    const hidden = ctx.__test.someHiddenBuiltin();
    if (!hidden) return;
    const html = ctx.__test.renderBuiltinOptions(hidden.display);
    assert.ok(html.includes(`value="${hidden.display}"`),
        `selected hidden method "${hidden.display}" should render as an option: ${html}`);
    assert.ok(!html.includes('(custom)'),
        `selected hidden method should be recognized (not "(custom)"): ${html}`);
    assert.match(html, new RegExp(`value="${hidden.display}"[^>]*selected`));
});

test('dropdown still offers visible builtins when a hidden one is selected', () => {
    const { ctx } = makeContext();
    loadScripts(ctx);
    const hidden = ctx.__test.someHiddenBuiltin();
    const visible = ctx.__test.someVisibleBuiltin();
    if (!hidden || !visible) return;
    const html = ctx.__test.renderBuiltinOptions(hidden.display);
    assert.ok(html.includes(`value="${visible.display}"`),
        `visible method "${visible.display}" should still be offered alongside a selected hidden one: ${html}`);
});

test('datalist suggestions exclude hidden builtins', () => {
    // The free-text method datalist is a second discovery surface —
    // hiding from the dropdown but leaking via autocomplete would defeat
    // the point.
    const { ctx, methodsDatalistEl } = makeContext();
    loadScripts(ctx);
    const hidden = ctx.__test.someHiddenBuiltin();
    if (!hidden) return;
    ctx.__test.renderDatalists();
    const dataHTML = methodsDatalistEl.innerHTML;
    assert.ok(!dataHTML.includes(`value="${hidden.display}"`),
        `hidden display "${hidden.display}" should not appear in datalist: ${dataHTML}`);
    assert.ok(!dataHTML.includes(`value="${hidden.canonical}"`),
        `hidden canonical "${hidden.canonical}" should not appear in datalist: ${dataHTML}`);
});

test('isKnownPipelineBuiltinMethod still accepts hidden builtins (validation compat)', () => {
    // The backend handler for the hidden method still exists, so the
    // frontend must not report it as "unknown builtin method" for
    // existing pipelines — else the editor flags pre-existing drafts
    // as invalid even though the runtime happily executes them.
    const { ctx } = makeContext();
    loadScripts(ctx);
    const hidden = ctx.__test.someHiddenBuiltin();
    if (!hidden) return;
    assert.equal(ctx.__test.isKnownBuiltin(hidden.canonical), true);
    assert.equal(ctx.__test.isKnownBuiltin(hidden.display), true);
});

test('switching runner to builtin defaults to a visible method, never a hidden one', () => {
    // The runner-switch path auto-populates next_method with the first
    // catalog entry when the draft's method isn't a known builtin. If the
    // catalog is ever reordered to put the hidden entry first, that must
    // not leak into the UI as the new default.
    const { ctx } = makeContext();
    loadScripts(ctx);
    const hidden = ctx.__test.someHiddenBuiltin();
    if (!hidden) return;
    ctx.__test.setupDraftForRunnerSwitch();
    ctx.__test.switchActionToBuiltin();
    const chosen = ctx.__test.firstActionNextMethod();
    assert.ok(chosen, 'runner switch should populate a default method');
    assert.notEqual(chosen, hidden.display, `default must not be the hidden "${hidden.display}"`);
    assert.notEqual(chosen, hidden.canonical, `default must not be the hidden canonical "${hidden.canonical}"`);
    assert.equal(ctx.__test.isKnownBuiltin(chosen), true,
        `default "${chosen}" should still be a known (visible) builtin`);
});
