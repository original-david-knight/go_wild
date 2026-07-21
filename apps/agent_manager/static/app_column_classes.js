// ============================================================
// Column State - each displayed agent gets its own state
// ============================================================

class ColumnState {
    constructor(agentId, columnEl, options) {
        options = options || {};
        this.agentId = agentId;
        this.columnEl = columnEl;
        this.columnKind = options.columnKind || 'agent';
        this.ws = null;
        this.streamParser = null;
        this.chatUI = new ColumnChatUI(columnEl);
        this.logsAnsi = new AnsiToHtml();
        this.kgNodes = [];
        this.kgEdges = [];
        this.selectedNodeID = null;
        this.pendingInteractiveCmd = null;
        this.pendingFile = null;
        this.promptHistory = [];
        this.historyIndex = -1;
        this.autoPlayAudio = false;
        this.logsPollInterval = null;
        this.emailEnabled = false;
        this.seenIncomingCallJobIds = new Set();
    }

    destroy() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
        }
        if (this.streamParser) {
            this.streamParser.reset();
            this.streamParser = null;
        }
        if (this.logsPollInterval) {
            clearInterval(this.logsPollInterval);
            this.logsPollInterval = null;
        }
    }
}

// ============================================================
// ANSI → HTML converter
// ============================================================

class AnsiToHtml {
    constructor() {
        this.reset();
    }

    reset() {
        this.bold = false;
        this.dim = false;
        this.italic = false;
        this.underline = false;
        this.fgColor = null;
        this.fgStyle = null;
        this.pendingEsc = '';
    }

    convert(text) {
        if (this.pendingEsc) {
            text = this.pendingEsc + text;
            this.pendingEsc = '';
        }

        let result = '';
        let i = 0;

        while (i < text.length) {
            if (text[i] === '\x1b' && text[i + 1] === '[') {
                let j = i + 2;
                while (j < text.length && text[j] !== 'm' && !/[A-Za-z]/.test(text[j])) {
                    j++;
                }
                if (j >= text.length) {
                    this.pendingEsc = text.slice(i);
                    break;
                }
                if (text[j] === 'm') {
                    const params = text.slice(i + 2, j).split(';').map(Number);
                    this._applySgr(params);
                    i = j + 1;
                } else {
                    i = j + 1;
                }
            } else if (text[i] === '\x1b') {
                this.pendingEsc = text.slice(i);
                break;
            } else if (text[i] === '\r') {
                if (text[i + 1] === '\n') {
                    result += '\n';
                    i += 2;
                } else {
                    const lastNl = result.lastIndexOf('\n');
                    if (lastNl >= 0) {
                        result = result.slice(0, lastNl + 1);
                    } else {
                        result = '';
                    }
                    i++;
                }
            } else {
                const open = this._openTag();
                const close = open ? '</span>' : '';
                result += open + this._escHtml(text[i]) + close;
                i++;
            }
        }
        return result;
    }

    _applySgr(params) {
        let i = 0;
        while (i < params.length) {
            const p = params[i];
            if (p === 0) {
                this.reset();
            } else if (p === 1) {
                this.bold = true;
            } else if (p === 2) {
                this.dim = true;
            } else if (p === 3) {
                this.italic = true;
            } else if (p === 4) {
                this.underline = true;
            } else if (p === 22) {
                this.bold = false;
                this.dim = false;
            } else if (p === 23) {
                this.italic = false;
            } else if (p === 24) {
                this.underline = false;
            } else if (p >= 30 && p <= 37) {
                this.fgColor = this._colorName(p - 30);
                this.fgStyle = null;
            } else if (p === 39) {
                this.fgColor = null;
                this.fgStyle = null;
            } else if (p >= 90 && p <= 97) {
                this.fgColor = this._colorName(p - 90) + '-bright';
                this.fgStyle = null;
            } else if (p === 38 && params[i + 1] === 5 && params[i + 2] !== undefined) {
                this.fgStyle = this._color256(params[i + 2]);
                this.fgColor = null;
                i += 2;
            }
            i++;
        }
    }

    _colorName(idx) {
        const names = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white'];
        return names[idx] || 'white';
    }

    _color256(n) {
        if (n < 8) return null;
        if (n < 16) return null;
        if (n < 232) {
            n -= 16;
            const r = Math.floor(n / 36) * 51;
            const g = Math.floor((n % 36) / 6) * 51;
            const b = (n % 6) * 51;
            return `rgb(${r},${g},${b})`;
        }
        const gray = 8 + (n - 232) * 10;
        return `rgb(${gray},${gray},${gray})`;
    }

    _openTag() {
        const classes = [];
        let style = '';

        if (this.bold) classes.push('ansi-bold');
        if (this.dim) classes.push('ansi-dim');
        if (this.italic) classes.push('ansi-italic');
        if (this.underline) classes.push('ansi-underline');
        if (this.fgColor) classes.push('ansi-fg-' + this.fgColor);
        if (this.fgStyle) style = `color:${this.fgStyle}`;

        if (!classes.length && !style) return '';
        let tag = '<span';
        if (classes.length) tag += ` class="${classes.join(' ')}"`;
        if (style) tag += ` style="${style}"`;
        tag += '>';
        return tag;
    }

    _escHtml(ch) {
        switch (ch) {
            case '&': return '&amp;';
            case '<': return '&lt;';
            case '>': return '&gt;';
            case '"': return '&quot;';
            default: return ch;
        }
    }
}

// ============================================================
// Stream Parser — state machine for terminal output
// ============================================================

const STATES = { STARTUP: 0, WAITING: 1, ECHO: 2, RESPONDING: 3 };
const PROMPT_SEQ = '\x1b[36myou>';

class StreamParser {
    constructor(onMessage) {
        this.state = STATES.STARTUP;
        this.buffer = '';
        this.trailingWindow = '';
        this.onMessage = onMessage;
        this.ansi = new AnsiToHtml();
        this.echoText = '';
        this.echoTimer = null;
        this.currentMsgId = null;
        this.startupTimer = null;
        this._startStartupTimeout();
    }

    _startStartupTimeout() {
        this.startupTimer = setTimeout(() => {
            if (this.state === STATES.STARTUP) {
                if (this.buffer.trim()) {
                    const html = this.ansi.convert(this.buffer);
                    this.onMessage('system', html, true);
                    this.ansi.reset();
                    this.buffer = '';
                }
                this.state = STATES.WAITING;
                this.onMessage('prompt', '', true);
            }
        }, 2000);
    }

    feed(raw) {
        this.trailingWindow = (this.trailingWindow + raw).slice(-60);
        const combined = this.buffer + raw;
        this.buffer = '';

        switch (this.state) {
            case STATES.STARTUP:
                this._handleStartup(combined);
                break;
            case STATES.WAITING:
                this.buffer += combined;
                break;
            case STATES.ECHO:
                this._handleEcho(combined);
                break;
            case STATES.RESPONDING:
                this._handleResponding(combined);
                break;
        }
    }

    _handleStartup(data) {
        if (this.startupTimer) {
            clearTimeout(this.startupTimer);
            this.startupTimer = null;
        }
        const idx = data.indexOf(PROMPT_SEQ);
        if (idx >= 0) {
            const startup = data.slice(0, idx);
            if (startup.trim()) {
                const html = this.ansi.convert(startup);
                this.onMessage('system', html, true);
                this.ansi.reset();
            }
            const afterPrompt = this._consumePrompt(data, idx);
            this.state = STATES.WAITING;
            this.onMessage('prompt', '', true);
            if (afterPrompt) {
                this.buffer = afterPrompt;
            }
        } else {
            this.buffer = data;
        }
    }

    userSent(text) {
        this.echoText = text;
        this.state = STATES.ECHO;
        this.echoTimer = setTimeout(() => {
            this._startResponding();
        }, 2000);
    }

    _handleEcho(data) {
        // Server normalizes line endings to \n, so check for \n (not \r\n)
        const lastNl = data.lastIndexOf('\n');
        if (lastNl >= 0) {
            this.echoText = '';
            this._startResponding();
            return;
        }
        this.buffer = data;
    }

    _startResponding() {
        if (this.echoTimer) {
            clearTimeout(this.echoTimer);
            this.echoTimer = null;
        }
        this.state = STATES.RESPONDING;
        this.buffer = '';
        this.ansi.reset();
        this.currentMsgId = 'msg-' + Date.now();
        this.showingThinking = false;
        this.bubbleStarted = false;
        this.toolCalls = [];
    }

    _cleanResponseData(data) {
        data = data.replace(/\x1b\[36myou>\x1b\[0m/g, '');
        data = data.replace(/\x1b\[36myou>/g, '');
        data = data.replace(/\x1b\[J|\x1b\[2K|\x08/g, '');
        data = data.replace(/\r(?!\n)/g, '');
        data = data.replace(/((?:\x1b\[[0-9;]*m)+)#{1,6}\s/g, '$1');
        data = data.replace(/.*\[DEBUG\][^\n]*/g, '');
        return data;
    }

    _extractToolCalls(data) {
        const toolPattern = /\x1b\[3[13]m\[([^\]]+)\]\s*\n?\x1b\[0m\n?/g;
        let match;

        while ((match = toolPattern.exec(data)) !== null) {
            const content = match[1];

            if (content.startsWith('calling ')) {
                const name = content.replace('calling ', '').replace('...', '');
                this.toolCalls.push({ name, detail: '', status: 'running' });
                this.onMessage('tool-status', name, false, this.currentMsgId);
            } else if (content.endsWith(' completed')) {
                const name = content.replace(' completed', '');
                const tc = this.toolCalls.find(t => t.name === name && t.status === 'running');
                if (tc) tc.status = 'done';
                this.onMessage('tool-status', '', false, this.currentMsgId);
            } else if (content.includes(' failed')) {
                const name = content.split(' failed')[0];
                const tc = this.toolCalls.find(t => t.name === name && t.status === 'running');
                if (tc) tc.status = 'failed';
                this.onMessage('tool-status', '', false, this.currentMsgId);
            } else if (content.includes(': ')) {
                const colonIdx = content.indexOf(': ');
                const name = content.slice(0, colonIdx);
                const detail = content.slice(colonIdx + 2);
                this.toolCalls.push({ name, detail, status: 'running' });
                this.onMessage('tool-status', name, false, this.currentMsgId);
            }
        }

        return data.replace(toolPattern, '');
    }

    _handleResponding(data) {
        const needle = '\n' + PROMPT_SEQ;
        let idx = data.indexOf(needle);
        if (idx < 0 && data.startsWith(PROMPT_SEQ)) {
            idx = 0;
        }
        if (idx < 0) {
            const anyPromptIdx = data.indexOf(PROMPT_SEQ);
            if (anyPromptIdx >= 0) {
                idx = anyPromptIdx;
            }
        }
        const debugEndIdx = data.indexOf('[DEBUG] end of loop iteration');
        if (idx < 0 && debugEndIdx >= 0) {
            idx = debugEndIdx;
        }
        if (idx < 0) {
            const compactionEndIdx = data.indexOf('┘\n');
            if (compactionEndIdx >= 0 && data.includes('Context Compacted')) {
                idx = compactionEndIdx + 2;
            }
        }
        if (idx >= 0) {
            let chunk = this._cleanResponseData(data.slice(0, idx));
            chunk = this._extractToolCalls(chunk);
            this._emitResponseContent(chunk);
            if (this.showingThinking) {
                this.onMessage('thinking-done', '', false, this.currentMsgId);
            }
            if (this.toolCalls.length > 0) {
                this.onMessage('tool-summary', JSON.stringify(this.toolCalls), true, this.currentMsgId);
            }
            this.onMessage('assistant-end', '', true, this.currentMsgId);
            this.ansi.reset();
            const afterPrompt = this._consumePrompt(data, idx + 1);
            this.state = STATES.WAITING;
            this.onMessage('prompt', '', true);
            if (afterPrompt) this.buffer = afterPrompt;
            return;
        }

        data = this._cleanResponseData(data);
        if (!data.replace(/[\r\n\s]/g, '')) return;

        if (/\x1b\[90mthinking\.\.\.\x1b\[0m/.test(data)) {
            if (!this.showingThinking) {
                this.showingThinking = true;
                this.onMessage('thinking', '', false, this.currentMsgId);
            }
            data = data.replace(/\x1b\[90mthinking\.\.\.\x1b\[0m/g, '');
            if (!data.replace(/[\r\n\s]/g, '')) return;
        }

        data = this._extractToolCalls(data);
        this._emitResponseContent(data);
    }

    _emitResponseContent(data) {
        if (!data || !data.replace(/[\r\n\s]/g, '')) return;

        const html = this.ansi.convert(data);
        const visibleText = html.replace(/<[^>]*>/g, '').replace(/[\r\n\s]/g, '');
        if (!visibleText) return;

        if (this.showingThinking) {
            this.showingThinking = false;
            this.onMessage('thinking-done', '', false, this.currentMsgId);
        }

        if (!this.bubbleStarted) {
            this.bubbleStarted = true;
            this.onMessage('assistant-start', '', false, this.currentMsgId);
        }

        this.onMessage('assistant-chunk', html, false, this.currentMsgId);
    }

    _consumePrompt(data, promptIdx) {
        let i = promptIdx + PROMPT_SEQ.length;
        if (data.slice(i, i + 4) === '\x1b[0m') {
            i += 4;
        }
        if (data[i] === ' ') i++;
        return data.slice(i) || '';
    }

    // Transition to WAITING state, cancelling any pending echo/response.
    // Call this when the structured JSON path signals the response cycle is
    // complete (agent_type "prompt") so that stale RESPONDING state cannot
    // accidentally re-disable the input on later raw output.
    toWaiting() {
        if (this.echoTimer) {
            clearTimeout(this.echoTimer);
            this.echoTimer = null;
        }
        this.state = STATES.WAITING;
        this.buffer = '';
    }

    reset() {
        this.state = STATES.STARTUP;
        this.buffer = '';
        this.trailingWindow = '';
        this.ansi.reset();
        this.echoText = '';
        if (this.echoTimer) {
            clearTimeout(this.echoTimer);
            this.echoTimer = null;
        }
        if (this.startupTimer) {
            clearTimeout(this.startupTimer);
            this.startupTimer = null;
        }
        this.currentMsgId = null;
        this.showingThinking = false;
        this.bubbleStarted = false;
        this.toolCalls = [];
    }
}

// ============================================================
// Column Chat UI - scoped to a single column
// ============================================================

class ColumnChatUI {
    constructor(columnEl) {
        this.columnEl = columnEl;
        this._toolCalls = {};
    }

    getContainer() {
        return this.columnEl.querySelector('.chat-messages');
    }

    clear() {
        this.getContainer().innerHTML = '';
    }

    addSystemMessage(html) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-system';

        const toggle = document.createElement('div');
        toggle.className = 'system-toggle';
        toggle.textContent = 'System startup';
        toggle.onclick = () => msg.classList.toggle('expanded');

        const content = document.createElement('div');
        content.className = 'system-content';
        content.innerHTML = html;

        msg.appendChild(toggle);
        msg.appendChild(content);
        container.appendChild(msg);
        this._scrollBottom();
    }

    addUserMessage(text) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-user';

        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble';
        bubble.textContent = text;

        msg.appendChild(bubble);
        container.appendChild(msg);
        this._scrollBottom();
    }

    addHeartbeatMessage(text) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-user chat-msg-heartbeat';

        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble';
        bubble.textContent = text;

        msg.appendChild(bubble);
        container.appendChild(msg);
        this._scrollBottom();
    }

    addIncomingCallMessage(call) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-incoming';

        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble incoming-call-bubble';

        const title = document.createElement('div');
        title.className = 'incoming-call-title';
        title.textContent = 'Incoming Method Call';
        bubble.appendChild(title);

        const appendRow = (label, value) => {
            if (!value) return;
            const row = document.createElement('div');
            row.className = 'incoming-call-row';
            const labelEl = document.createElement('span');
            labelEl.className = 'incoming-call-label';
            labelEl.textContent = label + ': ';
            const valueEl = document.createElement('span');
            valueEl.className = 'incoming-call-value';
            valueEl.textContent = value;
            row.appendChild(labelEl);
            row.appendChild(valueEl);
            bubble.appendChild(row);
        };

        appendRow('Method', call && call.method);
        appendRow('Job ID', call && call.jobId);
        appendRow('From', call && call.fromAgent);

        if (call && call.params) {
            this._appendExpandableTextSection(bubble, 'Params', call.params, {
                titleClass: 'incoming-call-params-title',
                preClass: 'incoming-call-params',
                collapsedClass: 'incoming-call-params-collapsed',
                toggleClass: 'incoming-call-toggle',
                expandLabel: 'Show full params',
                collapseLabel: 'Show less',
                maxChars: 1200,
                maxLines: 20,
            });
        }

        msg.appendChild(bubble);
        container.appendChild(msg);
        this._scrollBottom();
    }

    addBuiltinMethodCallMessage(call) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-incoming';

        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble incoming-call-bubble builtin-terminal-bubble builtin-terminal-request';

        const title = document.createElement('div');
        title.className = 'incoming-call-title builtin-terminal-title';
        title.textContent = 'Builtin Method Call';
        bubble.appendChild(title);

        this._appendIncomingCallRow(bubble, 'Method', call && call.method);
        this._appendIncomingCallRow(bubble, 'Pipeline', call && call.pipelineId);
        this._appendIncomingCallRow(bubble, 'Run ID', call && call.runId);
        this._appendIncomingCallRow(bubble, 'Step', Number.isFinite(call && call.stepIndex) ? String(call.stepIndex) : '');
        this._appendIncomingCallRow(bubble, 'From', call && call.fromAgent);
        this._appendIncomingCallRow(bubble, 'Time', call && call.time);

        if (call && call.params) {
            this._appendExpandableTextSection(bubble, 'Params', call.params, {
                titleClass: 'builtin-terminal-json-title',
                preClass: 'builtin-terminal-json',
                collapsedClass: 'builtin-terminal-json-collapsed',
                toggleClass: 'builtin-terminal-toggle',
                expandLabel: 'Show full params',
                collapseLabel: 'Show less',
                maxChars: 1800,
                maxLines: 24,
            });
        }

        msg.appendChild(bubble);
        container.appendChild(msg);
        this._scrollBottom();
    }

    addBuiltinMethodResultMessage(entry) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-assistant';

        const bubble = document.createElement('div');
        bubble.className = `chat-bubble builtin-terminal-bubble builtin-terminal-${entry && entry.kind ? entry.kind : 'result'}`;

        const title = document.createElement('div');
        title.className = 'builtin-terminal-title';
        title.textContent = entry && entry.title ? entry.title : 'Builtin Method Result';
        bubble.appendChild(title);

        this._appendBuiltinMetaRow(bubble, 'Method', entry && entry.method);
        this._appendBuiltinMetaRow(bubble, 'Pipeline', entry && entry.pipelineId);
        this._appendBuiltinMetaRow(bubble, 'Run ID', entry && entry.runId);
        this._appendBuiltinMetaRow(bubble, 'Step', Number.isFinite(entry && entry.stepIndex) ? String(entry.stepIndex) : '');
        this._appendBuiltinMetaRow(bubble, 'Status', entry && entry.status);
        this._appendBuiltinMetaRow(bubble, 'Duration', entry && entry.durationText);
        this._appendBuiltinMetaRow(bubble, 'Time', entry && entry.time);
        this._appendBuiltinSummary(bubble, entry && entry.summaryFields);

        if (entry && entry.bodyText) {
            this._appendExpandableTextSection(
                bubble,
                entry.kind === 'error' ? 'Error' : 'Result',
                entry.bodyText,
                {
                    titleClass: 'builtin-terminal-json-title',
                    preClass: 'builtin-terminal-json',
                    collapsedClass: 'builtin-terminal-json-collapsed',
                    toggleClass: 'builtin-terminal-toggle',
                    expandLabel: 'Show full JSON',
                    collapseLabel: 'Show less',
                    maxChars: 2200,
                    maxLines: 28,
                },
            );
        }

        msg.appendChild(bubble);
        container.appendChild(msg);
        this._scrollBottom();
    }

    _appendIncomingCallRow(bubble, label, value) {
        if (!bubble || !value) return;
        const row = document.createElement('div');
        row.className = 'incoming-call-row';
        const labelEl = document.createElement('span');
        labelEl.className = 'incoming-call-label';
        labelEl.textContent = label + ': ';
        const valueEl = document.createElement('span');
        valueEl.className = 'incoming-call-value';
        const formatted = this._formatMetadataValue(label, value);
        valueEl.textContent = formatted.display;
        if (formatted.title) valueEl.title = formatted.title;
        row.appendChild(labelEl);
        row.appendChild(valueEl);
        bubble.appendChild(row);
    }

    _appendBuiltinMetaRow(bubble, label, value) {
        if (!bubble || !value) return;
        const row = document.createElement('div');
        row.className = 'builtin-terminal-row';
        const labelEl = document.createElement('span');
        labelEl.className = 'builtin-terminal-label';
        labelEl.textContent = label + ': ';
        const valueEl = document.createElement('span');
        valueEl.className = 'builtin-terminal-value';
        const formatted = this._formatMetadataValue(label, value);
        valueEl.textContent = formatted.display;
        if (formatted.title) valueEl.title = formatted.title;
        row.appendChild(labelEl);
        row.appendChild(valueEl);
        bubble.appendChild(row);
    }

    _formatMetadataValue(label, value) {
        const raw = String(value || '').trim();
        if (!raw) return { display: '' };
        if (!this._isDateLikeMetadataLabel(label)) {
            return { display: raw };
        }

        const display = formatDate(raw);
        const precise = formatDatePrecise(raw);
        if (!display) {
            return { display: raw };
        }
        if (display === raw && precise === raw) {
            return { display: raw };
        }
        if (precise && precise !== display) {
            return { display, title: precise };
        }
        return { display };
    }

    _isDateLikeMetadataLabel(label) {
        const normalized = String(label || '').trim().toLowerCase();
        return normalized === 'time' ||
            normalized === 'date' ||
            normalized === 'created' ||
            normalized === 'updated' ||
            normalized === 'started' ||
            normalized === 'finished';
    }

    _appendBuiltinSummary(bubble, fields) {
        if (!bubble || !Array.isArray(fields) || !fields.length) return;
        const title = document.createElement('div');
        title.className = 'builtin-terminal-summary-title';
        title.textContent = 'Position Sizing';
        bubble.appendChild(title);

        const grid = document.createElement('div');
        grid.className = 'builtin-terminal-summary-grid';
        for (const field of fields) {
            if (!field || !field.label || !field.value) continue;
            const card = document.createElement('div');
            card.className = 'builtin-terminal-summary-card';

            const labelEl = document.createElement('div');
            labelEl.className = 'builtin-terminal-summary-label';
            labelEl.textContent = String(field.label);
            card.appendChild(labelEl);

            const valueEl = document.createElement('div');
            valueEl.className = 'builtin-terminal-summary-value';
            valueEl.textContent = String(field.value);
            card.appendChild(valueEl);

            grid.appendChild(card);
        }
        if (grid.childNodes.length > 0) {
            bubble.appendChild(grid);
        }
    }

    _appendExpandableTextSection(bubble, title, text, options = {}) {
        if (!bubble || !text) return;

        const titleEl = document.createElement('div');
        titleEl.className = options.titleClass || 'incoming-call-params-title';
        titleEl.textContent = title;
        bubble.appendChild(titleEl);

        const pre = document.createElement('pre');
        pre.className = options.preClass || 'incoming-call-params';
        const fullText = String(text);
        const maxChars = Number.isFinite(options.maxChars) ? options.maxChars : 1200;
        const maxLines = Number.isFinite(options.maxLines) ? options.maxLines : 20;
        const collapsedText = this._truncateForPreview(fullText, maxChars, maxLines);
        const isTruncated = collapsedText.length < fullText.length;
        pre.textContent = isTruncated ? collapsedText : fullText;
        pre.dataset.fullText = fullText;
        pre.dataset.shortText = collapsedText;
        pre.dataset.expanded = isTruncated ? 'false' : 'true';
        if (isTruncated && options.collapsedClass) {
            pre.classList.add(options.collapsedClass);
        }
        bubble.appendChild(pre);

        if (!isTruncated) {
            return;
        }

        const toggle = document.createElement('button');
        toggle.type = 'button';
        toggle.className = options.toggleClass || 'incoming-call-toggle';
        toggle.textContent = options.expandLabel || 'Show more';
        toggle.onclick = () => {
            const expanded = pre.dataset.expanded === 'true';
            if (expanded) {
                pre.textContent = pre.dataset.shortText || '';
                if (options.collapsedClass) pre.classList.add(options.collapsedClass);
                pre.dataset.expanded = 'false';
                toggle.textContent = options.expandLabel || 'Show more';
            } else {
                pre.textContent = pre.dataset.fullText || '';
                if (options.collapsedClass) pre.classList.remove(options.collapsedClass);
                pre.dataset.expanded = 'true';
                toggle.textContent = options.collapseLabel || 'Show less';
                this._scrollBottom();
            }
        };
        bubble.appendChild(toggle);
    }

    _truncateForPreview(text, maxChars, maxLines) {
        if (!text) return '';
        const lines = text.split('\n');
        const out = [];
        let used = 0;
        for (let i = 0; i < lines.length; i += 1) {
            if (out.length >= maxLines || used >= maxChars) break;
            const line = lines[i];
            const nextLen = line.length + (out.length > 0 ? 1 : 0);
            if (used + nextLen <= maxChars) {
                out.push(line);
                used += nextLen;
                continue;
            }
            const remaining = Math.max(0, maxChars - used);
            if (remaining > 0) {
                out.push(line.slice(0, remaining));
            }
            used = maxChars;
            break;
        }
        let preview = out.join('\n').trimEnd();
        if (preview.length < text.length) preview += '\n...';
        return preview;
    }

    startAssistantMessage(msgId) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-assistant';
        msg.id = msgId;

        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble';

        msg.appendChild(bubble);
        container.appendChild(msg);
        this._scrollBottom();
    }

    appendToAssistant(msgId, html, raw) {
        const msg = this.columnEl.querySelector('#' + msgId);
        if (!msg) return;
        const bubble = msg.querySelector('.chat-bubble');
        if (!bubble) return;
        if (raw) {
            // Raw text: accumulate for markdown rendering on finalize
            bubble.dataset.rawText = (bubble.dataset.rawText || '') + html;
            bubble.textContent = bubble.dataset.rawText;
        } else {
            // Pre-formatted HTML (e.g. ANSI-converted)
            bubble.innerHTML += html;
        }
        this._scrollBottom();
    }

    finalizeAssistant(msgId) {
        if (msgId) {
            const msg = this.columnEl.querySelector('#' + msgId);
            if (msg) {
                const bubble = msg.querySelector('.chat-bubble');
                if (bubble && bubble.dataset.rawText) {
                    this._renderAssistantBubbleContent(bubble, bubble.dataset.rawText);
                    delete bubble.dataset.rawText;
                }
            }
        }
        this._scrollBottom();
    }

    addAssistantHistory(text) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-assistant';

        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble';
        this._renderAssistantBubbleContent(bubble, text);

        msg.appendChild(bubble);
        container.appendChild(msg);
    }

    _renderAssistantBubbleContent(bubble, text) {
        const rawText = String(text || '');
        const parsedJSON = parseLogJSONLine(rawText);
        bubble.classList.remove('markdown-body', 'terminal-json-bubble');
        if (parsedJSON !== null) {
            bubble.classList.add('terminal-json-bubble');
            bubble.textContent = JSON.stringify(parsedJSON, null, 2);
            return;
        }
        try {
            bubble.classList.add('markdown-body');
            bubble.innerHTML = marked.parse(rawText);
            this._constrainInlineImages(bubble);
        } catch (e) {
            console.warn('Markdown rendering failed:', e);
            bubble.textContent = rawText;
        }
    }

    addHistorySeparator() {
        const container = this.getContainer();
        const sep = document.createElement('div');
        sep.className = 'chat-history-separator';
        sep.innerHTML = '<span>previous messages</span>';
        container.appendChild(sep);
    }

    showThinking() {
        this.hideThinking();
        const container = this.getContainer();
        const el = document.createElement('div');
        el.className = 'chat-thinking';
        el.setAttribute('data-thinking', 'true');
        el.innerHTML = '<span class="thinking-dots">thinking</span>';
        container.appendChild(el);
        this._scrollBottom();
    }

    hideThinking() {
        const el = this.columnEl.querySelector('[data-thinking="true"]');
        if (el) el.remove();
    }

    showToolStatus(toolName) {
        let el = this.columnEl.querySelector('[data-tool-status="true"]');
        if (!toolName) {
            if (el) el.remove();
            return;
        }
        if (!el) {
            el = document.createElement('div');
            el.className = 'chat-tool-status';
            el.setAttribute('data-tool-status', 'true');
            this.getContainer().appendChild(el);
        }
        el.textContent = 'calling ' + toolName + '...';
        this._scrollBottom();
    }

    addToolSummary(toolCalls) {
        const statusEl = this.columnEl.querySelector('[data-tool-status="true"]');
        if (statusEl) statusEl.remove();

        if (!toolCalls.length) return;
        const container = this.getContainer();
        const wrapper = document.createElement('div');
        wrapper.className = 'chat-msg chat-msg-tool-summary';

        const pill = document.createElement('button');
        pill.className = 'tool-pill';
        pill.textContent = 'called ' + toolCalls.length + ' tool' + (toolCalls.length > 1 ? 's' : '');
        pill.onclick = () => {
            let popup = wrapper.querySelector('.tool-popup');
            if (popup) {
                popup.remove();
                return;
            }
            popup = document.createElement('div');
            popup.className = 'tool-popup';
            popup.innerHTML = toolCalls.map(tc => {
                const icon = tc.status === 'failed' ? '\u2717' : '\u2713';
                const cls = tc.status === 'failed' ? 'tool-failed' : 'tool-done';
                const detail = tc.detail ? ' \u2014 ' + escHtml(tc.detail) : '';
                const dur = tc.duration ? ' <span class="tool-duration">(' + escHtml(tc.duration) + ')</span>' : '';
                return '<div class="tool-popup-item ' + cls + '">' +
                    '<span class="tool-icon">' + icon + '</span>' +
                    '<span class="tool-name">' + escHtml(tc.name) + '</span>' +
                    detail + dur + '</div>';
            }).join('');
            wrapper.appendChild(popup);
            this._scrollBottom();
        };

        wrapper.appendChild(pill);
        container.appendChild(wrapper);
        this._scrollBottom();
    }

    addStatusMessage(text, cssClass) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-status ' + (cssClass || '');
        msg.textContent = text;
        container.appendChild(msg);
        this._scrollBottom();
    }

    enableInput() {
        const bar = this.columnEl.querySelector('.chat-input-bar');
        const input = this.columnEl.querySelector('.chat-input');
        bar.style.display = '';
        input.disabled = false;
        input.focus();
    }

    disableInput() {
        const input = this.columnEl.querySelector('.chat-input');
        if (input) input.disabled = true;
    }

    hideInput() {
        const bar = this.columnEl.querySelector('.chat-input-bar');
        if (bar) bar.style.display = 'none';
    }

    ensureAssistantBubble(msgId) {
        if (!this.columnEl.querySelector('#' + msgId)) {
            this.startAssistantMessage(msgId);
        }
    }

    addErrorMessage(text) {
        const container = this.getContainer();
        const msg = document.createElement('div');
        msg.className = 'chat-msg chat-msg-status status-error';
        msg.textContent = text;
        container.appendChild(msg);
        this._scrollBottom();
    }

    addContentMessage(contentType, data, alt) {
        const container = this.getContainer();
        const wrapper = document.createElement('div');
        wrapper.className = 'chat-msg chat-msg-content';

        if (contentType === 'image/svg+xml') {
            const svgContainer = document.createElement('div');
            svgContainer.className = 'content-svg';
            svgContainer.innerHTML = data;
            svgContainer.onclick = () => this._showOverlay(data, alt, 'svg');
            wrapper.appendChild(svgContainer);
        } else if (contentType && contentType.startsWith('image/')) {
            const img = document.createElement('img');
            img.src = 'data:' + contentType + ';base64,' + data;
            img.alt = alt || '';
            img.onclick = () => this._showOverlay(img.src, alt);
            wrapper.appendChild(img);
        } else if (contentType && contentType.startsWith('audio/')) {
            const audio = document.createElement('audio');
            audio.className = 'content-audio';
            audio.controls = true;
            audio.preload = 'metadata';
            audio.src = 'data:' + contentType + ';base64,' + data;
            wrapper.appendChild(audio);
            if (this._shouldAutoPlayAudio()) {
                audio.autoplay = true;
                audio.play().catch(() => {});
            }
        } else {
            const label = document.createElement('div');
            label.className = 'content-fallback';
            label.textContent = '[' + (contentType || 'unknown') + '] ' + (alt || '');
            wrapper.appendChild(label);
        }

        if (alt) {
            const caption = document.createElement('div');
            caption.className = 'content-caption';
            caption.textContent = alt;
            wrapper.appendChild(caption);
        }

        container.appendChild(wrapper);
        this._scrollBottom();
    }

    _shouldAutoPlayAudio() {
        if (this.columnEl && this.columnEl.dataset && this.columnEl.dataset.autoplayAudio === 'true') {
            return true;
        }
        const input = this.columnEl.querySelector('.autoplay-toggle-input');
        return !!(input && input.checked);
    }

    _showOverlay(src, alt, type) {
        const overlay = document.createElement('div');
        overlay.className = 'content-overlay';
        overlay.onclick = () => overlay.remove();
        if (type === 'svg') {
            const container = document.createElement('div');
            container.className = 'overlay-svg';
            container.innerHTML = src;
            overlay.appendChild(container);
        } else {
            const img = document.createElement('img');
            img.src = src;
            img.alt = alt || '';
            overlay.appendChild(img);
        }
        document.body.appendChild(overlay);
    }

    _constrainInlineImages(container) {
        container.querySelectorAll('img').forEach(img => {
            img.style.maxHeight = '300px';
            img.style.objectFit = 'contain';
            img.style.cursor = 'pointer';
            img.style.borderRadius = '6px';
            img.onclick = () => this._showOverlay(img.src, img.alt);
        });
    }

    _scrollBottom() {
        const c = this.getContainer();
        requestAnimationFrame(() => {
            c.scrollTop = c.scrollHeight;
        });
    }
}
