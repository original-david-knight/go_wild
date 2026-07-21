// GoWild Agent Manager - Multi-Column Frontend

// Configure marked.js for clean markdown rendering, with inline fallback
if (typeof marked !== 'undefined') {
    marked.setOptions({ breaks: true, gfm: true });
} else {
    // Fallback: basic markdown-to-HTML when CDN fails to load
    console.warn('marked.js not loaded from CDN, using basic fallback');
    window.marked = {
        parse: function(text) {
            var esc = function(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); };
            var inline = function(s) {
                s = esc(s);
                s = s.replace(/`([^`]+)`/g, function(_, c) { return '<code>' + c + '</code>'; });
                s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
                s = s.replace(/\*(.+?)\*/g, '<em>$1</em>');
                return s;
            };
            // Process fenced code blocks first
            var codeBlocks = [];
            text = text.replace(/```(\w*)\n([\s\S]*?)```/g, function(_, lang, code) {
                codeBlocks.push('<pre><code>' + esc(code.replace(/\n$/, '')) + '</code></pre>');
                return '\x00CB' + (codeBlocks.length - 1) + '\x00';
            });
            var lines = text.split('\n');
            var html = [];
            var inList = false;
            for (var i = 0; i < lines.length; i++) {
                var line = lines[i];
                // Restore code blocks
                var cbMatch = line.match(/^\x00CB(\d+)\x00$/);
                if (cbMatch) {
                    if (inList) { html.push('</ul>'); inList = false; }
                    html.push(codeBlocks[parseInt(cbMatch[1])]);
                    continue;
                }
                // Headers
                var hMatch = line.match(/^(#{1,6})\s+(.+)$/);
                if (hMatch) {
                    if (inList) { html.push('</ul>'); inList = false; }
                    html.push('<h' + hMatch[1].length + '>' + inline(hMatch[2]) + '</h' + hMatch[1].length + '>');
                    continue;
                }
                // List items
                var liMatch = line.match(/^[\s]*[*\-+]\s+(.+)$/);
                if (liMatch) {
                    if (!inList) { html.push('<ul>'); inList = true; }
                    html.push('<li>' + inline(liMatch[1]) + '</li>');
                    continue;
                }
                if (inList) { html.push('</ul>'); inList = false; }
                if (!line.trim()) continue;
                // Blockquote
                var bqMatch = line.match(/^>\s*(.*)$/);
                if (bqMatch) {
                    html.push('<blockquote>' + inline(bqMatch[1]) + '</blockquote>');
                    continue;
                }
                html.push('<p>' + inline(line) + '</p>');
            }
            if (inList) html.push('</ul>');
            return html.join('\n');
        }
    };
}

let agents = [];
let displayedAgents = new Map(); // agentId -> ColumnState
let pollInterval = null;
let mcpServers = [];
let mcpAgentConfigs = {};
let pipelineCapabilities = [];
let pipelineCapabilitiesFilterCompanyID = '';
let pipelineDefinitions = [];
let pipelineTriggerInFlight = new Set();
let pipelineRuns = [];
let pipelineStepIOContents = new Map();
let pipelineStepIOActiveKey = '';
let pipelineEditorDraft = null;
let pipelineEditorDirty = false;
let a2aMethods = [];
let a2aMethodEditing = null; // method name when editing, else null
let deepResearchMethods = [];
let deepResearchMethodEditing = null;
let deepResearchMethodTestInputs = {};
let pipelinesModalActiveTab = 'runs';
let pipelinesPollTimer = null;
let companies = [];
let companySummaries = [];
let companyKnowledgeCache = {};
let companyShopifyPanelOpen = {};
let selectedCompanyID = '';
let polymarketPortfolioCompanyID = '';
let polymarketPortfolioSnapshot = null;
let polymarketNotesState = { companyID: '', conditionID: '', title: '' };

let toolGroups = [];

const pipelineStepRunnerAgent = 'agent';
const pipelineStepRunnerBuiltin = 'builtin';
const pipelineStepRunnerClaudeCode = 'claude-code';
const pipelineStepRunnerCodex = 'codex';
const builtinMethodsColumnId = '__builtin_methods_terminal__';
const builtinMethodsTerminalPath = '/api/builtin-methods/terminal';
const builtinMethodsColumnTitle = 'Builtin Methods';

const pipelineBuiltinMethodCatalog = [
    {
        canonical: 'builtin_polymarket_snapshot',
        display: '/polymarket_review_positions',
        aliases: ['polymarket_review_positions'],
        description: 'Fetch current positions and orders for the scoped company',
    },
    {
        canonical: 'builtin_polymarket_manage_position',
        display: 'polymarket_manage_position',
        description: 'Risk-manage a Polymarket market from research output: buy, sell, cancel, replace, or hold',
    },
    {
        canonical: 'builtin_polymarket_find_markets',
        display: 'polymarket_find_markets',
        description: 'Find candidate Polymarket markets for the scoped company, skipping markets with current exposure, open orders, recent notes, or far-dated resolutions',
    },
    {
        canonical: 'builtin_test_seed',
        display: 'test_seed',
        description: 'Returns 4 hardcoded test topics for fan-out testing',
        // Debug/test-only method: kept in the catalog so existing pipelines
        // validate and round-trip, but hidden from dropdown and datalist UI
        // so operators don't pick it in production.
        hidden: true,
    },
];

function isBuiltinMethodsColumnState(state) {
    return !!(state && state.columnKind === 'builtin-methods');
}

function toolGroupTooltipText(group) {
    const tools = group.tools;
    if (!Array.isArray(tools) || !tools.length) {
        return `${group.display_name}: no tools defined.`;
    }
    return `${group.display_name} tools (${tools.length}):\n${tools.join('\n')}`;
}

// ============================================================
// Init
// ============================================================

function updateHeaderClock() {
    var el = document.getElementById('header-clock');
    if (el) el.textContent = formatHeaderClock(new Date());
}

document.addEventListener('DOMContentLoaded', async () => {
    updateHeaderClock();
    setInterval(updateHeaderClock, 1000);
    try { toolGroups = await api('GET', '/api/tool-groups'); } catch (e) { console.error('Failed to fetch tool groups:', e); }
    refreshAgents();
    pollInterval = setInterval(refreshAgents, 5000);

    // Client-side URL routing: open missions if URL matches /missions or /missions/{companyID}
    const path = window.location.pathname;
    if (path === '/missions' || path.startsWith('/missions/')) {
        const companyID = path.startsWith('/missions/') ? decodeURIComponent(path.slice('/missions/'.length)) : '';
        showMissions(companyID || undefined);
    }
    if (path === '/polymarket' || path.startsWith('/polymarket/')) {
        const companyID = path.startsWith('/polymarket/') ? decodeURIComponent(path.slice('/polymarket/'.length)) : '';
        showPolymarketPortfolio(companyID || undefined);
    }
});

// Handle browser back/forward
window.addEventListener('popstate', () => {
    const path = window.location.pathname;
    if (path === '/missions' || path.startsWith('/missions/')) {
        const companyID = path.startsWith('/missions/') ? decodeURIComponent(path.slice('/missions/'.length)) : '';
        if (document.getElementById('missions-modal').style.display !== 'flex') {
            showMissions(companyID || undefined);
        } else if (companyID) {
            const sel = document.getElementById('missions-company-select');
            sel.value = companyID;
            onMissionsCompanyChange();
        }
    } else if (path === '/polymarket' || path.startsWith('/polymarket/')) {
        const companyID = path.startsWith('/polymarket/') ? decodeURIComponent(path.slice('/polymarket/'.length)) : '';
        if (document.getElementById('polymarket-portfolio-modal').style.display !== 'flex') {
            showPolymarketPortfolio(companyID || undefined);
        } else {
            polymarketPortfolioCompanyID = companyID;
            refreshPolymarketPortfolioCompanyOptions();
            if (companyID) {
                loadPolymarketPortfolio();
            } else {
                polymarketPortfolioSnapshot = null;
                renderPolymarketPortfolioEmpty('Select a company to load positions and open orders.');
                setPolymarketPortfolioStatus('Select a company to begin.', false);
            }
        }
    } else {
        if (document.getElementById('missions-modal').style.display === 'flex') {
            document.getElementById('missions-modal').style.display = 'none';
            missionsCompanyID = '';
            if (missionsPollTimer) { clearInterval(missionsPollTimer); missionsPollTimer = null; }
        }
        if (document.getElementById('polymarket-portfolio-modal').style.display === 'flex') {
            document.getElementById('polymarket-portfolio-modal').style.display = 'none';
            polymarketPortfolioCompanyID = '';
            polymarketPortfolioSnapshot = null;
        }
    }
});
