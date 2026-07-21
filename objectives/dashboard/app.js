// GoWild Objectives Dashboard

(function () {
    "use strict";

    const API_BASE = window.location.origin;
    let ws = null;
    let reconnectTimer = null;
    let selectedObjectiveID = null;
    let selectedMissionID = null;
    let allMissions = []; // cached root objectives

    // --- DOM refs ---
    const treeContainer = document.getElementById("tree-container");
    const escalationContainer = document.getElementById("escalation-container");
    const statusDot = document.getElementById("status-dot");
    const statusText = document.getElementById("status-text");
    const objectiveCount = document.getElementById("objective-count");
    const escalationCount = document.getElementById("escalation-count");
    const footerStatus = document.getElementById("footer-status");
    const footerTime = document.getElementById("footer-time");
    const detailTitle = document.getElementById("detail-title");
    const tabActivity = document.getElementById("tab-activity");
    const tabGraphs = document.getElementById("tab-graphs");

    // --- API calls ---
    async function fetchJSON(path) {
        const resp = await fetch(API_BASE + path);
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        return resp.json();
    }

    async function postJSON(path, body) {
        const resp = await fetch(API_BASE + path, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
        });
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        return resp.json();
    }

    // --- Tab switching ---
    document.querySelectorAll(".tab-bar .tab").forEach(function (tab) {
        tab.addEventListener("click", function () {
            document.querySelectorAll(".tab-bar .tab").forEach(function (t) { t.classList.remove("active"); });
            document.querySelectorAll(".tab-content").forEach(function (c) { c.classList.remove("active"); });
            tab.classList.add("active");
            var target = document.getElementById("tab-" + tab.dataset.tab);
            if (target) target.classList.add("active");
        });
    });

    // --- Mission selector ---
    const missionSelect = document.getElementById("mission-select");
    const deleteMissionBtn = document.getElementById("delete-mission-btn");

    missionSelect.addEventListener("change", function () {
        selectedMissionID = missionSelect.value || null;
        selectedObjectiveID = null;
        detailTitle.textContent = "Activity Feed";
        detailActions.style.display = "none";
        refreshTree();
        if (selectedMissionID) {
            loadObjectiveDetail(selectedMissionID);
        } else {
            tabActivity.innerHTML = '<div class="empty-state">Select a mission</div>';
            tabGraphs.innerHTML = '<div class="empty-state">Select a mission to see its graphs</div>';
        }
    });

    deleteMissionBtn.addEventListener("click", async function () {
        if (!selectedMissionID) return;
        deleteMissionBtn.disabled = true;
        try {
            await fetch(API_BASE + "/api/objectives/" + selectedMissionID, { method: "DELETE" });
            selectedMissionID = null;
            selectedObjectiveID = null;
            missionSelect.value = "";
            detailTitle.textContent = "Activity Feed";
            detailActions.style.display = "none";
            tabActivity.innerHTML = '<div class="empty-state">Select a mission</div>';
            tabGraphs.innerHTML = '<div class="empty-state">Select a mission to see its graphs</div>';
            await refreshAll();
        } catch (err) {
            console.error("Delete mission failed:", err);
        } finally {
            deleteMissionBtn.disabled = false;
        }
    });

    function populateMissionDropdown(missions) {
        var currentVal = missionSelect.value;
        missionSelect.innerHTML = '<option value="">Select a mission...</option>';
        for (var i = 0; i < missions.length; i++) {
            var m = missions[i];
            var opt = document.createElement("option");
            opt.value = m.id;
            var statusLabel = m.status ? " [" + m.status + "]" : "";
            opt.textContent = m.title + statusLabel;
            missionSelect.appendChild(opt);
        }
        // Restore selection
        if (currentVal && missions.some(function (m) { return m.id === currentVal; })) {
            missionSelect.value = currentVal;
        } else if (selectedMissionID) {
            // Mission was deleted
            selectedMissionID = null;
            missionSelect.value = "";
        }
    }

    // --- Tree rendering ---
    function buildTree(objectives) {
        const byParent = {};
        let count = 0;

        // Separate missions from children
        allMissions = [];
        for (const raw of objectives) {
            const obj = raw.objective || raw;
            if (!obj.parent_id) {
                allMissions.push(obj);
            }
        }
        populateMissionDropdown(allMissions);

        // If no mission selected, show prompt
        if (!selectedMissionID) {
            objectiveCount.textContent = allMissions.length;
            return '<div class="empty-state">Select a mission above</div>';
        }

        // Fetch full tree via the tree status endpoint would be ideal,
        // but for now filter from the flat list — we need the full tree.
        // We'll load it async instead.
        objectiveCount.textContent = "...";
        loadMissionTree(selectedMissionID);
        return "";
    }

    async function loadMissionTree(missionID) {
        try {
            var data = await fetchJSON("/api/objectives/" + missionID + "/tree");
            var items = data || [];
            var byParent = {};
            var count = 0;
            for (var i = 0; i < items.length; i++) {
                var obj = items[i].objective || items[i];
                count++;
                var pid = obj.parent_id || "__root__";
                if (!byParent[pid]) byParent[pid] = [];
                byParent[pid].push(obj);
            }
            objectiveCount.textContent = count;
            treeContainer.innerHTML = renderTreeLevel(byParent, "__root__");
        } catch (err) {
            console.error("Load mission tree failed:", err);
            treeContainer.innerHTML = '<div class="empty-state">Failed to load tree</div>';
        }
    }

    function renderTreeLevel(byParent, parentID) {
        const children = byParent[parentID] || [];
        if (children.length === 0) return "";

        let html = "";
        for (const obj of children) {
            const hasChildren = byParent[obj.id] && byParent[obj.id].length > 0;
            const toggleClass = hasChildren ? "expanded" : "leaf";
            const badgeClass = "badge-" + (obj.status || "pending");
            const selectedClass = obj.id === selectedObjectiveID ? " selected" : "";

            html += `<div class="tree-node">
                <div class="tree-node-row${selectedClass}" data-id="${obj.id}">
                    <span class="tree-toggle ${toggleClass}">\u25B6</span>
                    <span class="badge ${badgeClass}">${obj.status || "pending"}</span>
                    <span class="tree-title" title="${escapeHTML(obj.description || "")}">${escapeHTML(obj.title)}</span>
                </div>`;

            if (hasChildren) {
                html += `<div class="tree-children expanded">`;
                html += renderTreeLevel(byParent, obj.id);
                html += `</div>`;
            }
            html += `</div>`;
        }
        return html;
    }

    function escapeHTML(str) {
        const div = document.createElement("div");
        div.textContent = str;
        return div.innerHTML;
    }

    // --- Activity rendering ---
    function renderActivity(events) {
        if (!events || events.length === 0) {
            tabActivity.innerHTML = '<div class="empty-state">No activity yet</div>';
            return;
        }

        let html = "";
        for (const ev of events) {
            const time = formatTime(ev.created_at);
            const severityClass = ev.severity || "info";
            html += `<div class="activity-item">
                <div class="activity-header">
                    <span class="activity-type ${severityClass}">${escapeHTML(ev.event_type || "event")}</span>
                    <span class="activity-time">${time}</span>
                </div>
                <div class="activity-summary">${escapeHTML(ev.summary || "")}</div>
                ${ev.objective_id ? `<div class="activity-objective">Objective: ${ev.objective_id.substring(0, 8)}...</div>` : ""}
            </div>`;
        }
        tabActivity.innerHTML = html;
        tabActivity.scrollTop = 0;
    }

    // --- Graph rendering ---
    function renderGraphs(events) {
        var graphs = (events || []).filter(function (ev) { return ev.event_type === "graph_planned"; });

        if (graphs.length === 0) {
            tabGraphs.innerHTML = '<div class="empty-state">No graphs yet</div>';
            return;
        }

        tabGraphs.innerHTML = "";
        for (var i = 0; i < graphs.length; i++) {
            var ev = graphs[i];
            var details = ev.details || {};
            var nodes = details.nodes || [];
            var time = formatTime(ev.created_at);

            var card = document.createElement("div");
            card.className = "graph-card";

            var header = '<div class="graph-header">';
            header += '<span class="graph-title">' + escapeHTML(ev.summary || "Graph") + '</span>';
            header += '<span class="activity-time">' + time + '</span>';
            header += '</div>';
            if (details.reasoning) {
                header += '<div class="graph-reasoning">' + escapeHTML(details.reasoning) + '</div>';
            }
            card.innerHTML = header;

            // SVG DAG visualization
            if (nodes.length > 0) {
                card.appendChild(buildDAGSvg(nodes));
            }

            // Node detail list
            var nodeList = document.createElement("div");
            nodeList.className = "graph-nodes";
            for (var j = 0; j < nodes.length; j++) {
                var node = nodes[j];
                var html = '<div class="graph-node-header">';
                html += '<span class="graph-node-id">' + escapeHTML(node.id || "?") + '</span>';
                if (node.type) html += '<span class="graph-node-type">' + escapeHTML(node.type) + '</span>';
                html += '</div>';
                if (node.prompt) html += '<div class="graph-node-prompt">' + escapeHTML(node.prompt) + '</div>';
                html += '<div class="graph-node-tools">';
                if (node.tools && node.tools.length > 0) {
                    for (var t = 0; t < node.tools.length; t++) {
                        html += '<span class="graph-node-tool">' + escapeHTML(node.tools[t]) + '</span>';
                    }
                } else if (node.type === "agentic") {
                    html += '<span class="graph-node-tool-missing">no tools assigned</span>';
                }
                html += '</div>';
                var nodeEl = document.createElement("div");
                nodeEl.className = "graph-node";
                nodeEl.innerHTML = html;
                nodeList.appendChild(nodeEl);
            }
            card.appendChild(nodeList);
            tabGraphs.appendChild(card);
        }
        tabGraphs.scrollTop = 0;
    }

    // --- SVG DAG builder ---
    function buildDAGSvg(nodes) {
        var NODE_W = 160, NODE_H = 36, PAD_X = 40, PAD_Y = 60, MARGIN = 20;

        // Build adjacency and compute layers via longest-path
        var idIndex = {};
        for (var i = 0; i < nodes.length; i++) idIndex[nodes[i].id] = i;

        var depth = new Array(nodes.length).fill(0);
        var changed = true;
        while (changed) {
            changed = false;
            for (var i = 0; i < nodes.length; i++) {
                var deps = nodes[i].depends_on || [];
                for (var d = 0; d < deps.length; d++) {
                    var pi = idIndex[deps[d]];
                    if (pi !== undefined && depth[i] < depth[pi] + 1) {
                        depth[i] = depth[pi] + 1;
                        changed = true;
                    }
                }
            }
        }

        // Group by layer
        var maxDepth = 0;
        var layers = {};
        for (var i = 0; i < nodes.length; i++) {
            if (depth[i] > maxDepth) maxDepth = depth[i];
            if (!layers[depth[i]]) layers[depth[i]] = [];
            layers[depth[i]].push(i);
        }

        // Compute positions
        var maxLayerWidth = 0;
        for (var d = 0; d <= maxDepth; d++) {
            if (layers[d] && layers[d].length > maxLayerWidth) maxLayerWidth = layers[d].length;
        }

        var svgW = Math.max(maxLayerWidth * (NODE_W + PAD_X) - PAD_X + MARGIN * 2, 300);
        var svgH = (maxDepth + 1) * (NODE_H + PAD_Y) - PAD_Y + MARGIN * 2;

        var positions = {};
        for (var d = 0; d <= maxDepth; d++) {
            var layer = layers[d] || [];
            var totalW = layer.length * (NODE_W + PAD_X) - PAD_X;
            var startX = (svgW - totalW) / 2;
            var y = MARGIN + d * (NODE_H + PAD_Y);
            for (var li = 0; li < layer.length; li++) {
                var ni = layer[li];
                positions[ni] = {
                    x: startX + li * (NODE_W + PAD_X),
                    y: y,
                    cx: startX + li * (NODE_W + PAD_X) + NODE_W / 2,
                    cy: y + NODE_H / 2
                };
            }
        }

        // Build SVG
        var ns = "http://www.w3.org/2000/svg";
        var svg = document.createElementNS(ns, "svg");
        svg.setAttribute("width", svgW);
        svg.setAttribute("height", svgH);
        svg.setAttribute("class", "dag-svg");

        // Arrow marker
        var defs = document.createElementNS(ns, "defs");
        var marker = document.createElementNS(ns, "marker");
        marker.setAttribute("id", "arrow-" + Math.random().toString(36).substr(2, 5));
        marker.setAttribute("viewBox", "0 0 10 10");
        marker.setAttribute("refX", "10");
        marker.setAttribute("refY", "5");
        marker.setAttribute("markerWidth", "8");
        marker.setAttribute("markerHeight", "8");
        marker.setAttribute("orient", "auto-start-reverse");
        var arrowPath = document.createElementNS(ns, "path");
        arrowPath.setAttribute("d", "M 0 0 L 10 5 L 0 10 z");
        arrowPath.setAttribute("fill", "#58a6ff");
        marker.appendChild(arrowPath);
        defs.appendChild(marker);
        svg.appendChild(defs);
        var markerId = "url(#" + marker.getAttribute("id") + ")";

        // Draw edges
        for (var i = 0; i < nodes.length; i++) {
            var deps = nodes[i].depends_on || [];
            for (var d = 0; d < deps.length; d++) {
                var fromIdx = idIndex[deps[d]];
                if (fromIdx === undefined) continue;
                var from = positions[fromIdx];
                var to = positions[i];
                if (!from || !to) continue;

                var line = document.createElementNS(ns, "line");
                line.setAttribute("x1", from.cx);
                line.setAttribute("y1", from.y + NODE_H);
                line.setAttribute("x2", to.cx);
                line.setAttribute("y2", to.y);
                line.setAttribute("stroke", "#58a6ff");
                line.setAttribute("stroke-width", "2");
                line.setAttribute("marker-end", markerId);
                svg.appendChild(line);
            }
        }

        // Draw nodes
        for (var i = 0; i < nodes.length; i++) {
            var pos = positions[i];
            if (!pos) continue;
            var node = nodes[i];

            var rect = document.createElementNS(ns, "rect");
            rect.setAttribute("x", pos.x);
            rect.setAttribute("y", pos.y);
            rect.setAttribute("width", NODE_W);
            rect.setAttribute("height", NODE_H);
            rect.setAttribute("rx", "6");
            rect.setAttribute("fill", "#21262d");
            rect.setAttribute("stroke", "#bc8cff");
            rect.setAttribute("stroke-width", "1.5");
            svg.appendChild(rect);

            var label = document.createElementNS(ns, "text");
            label.setAttribute("x", pos.cx);
            label.setAttribute("y", pos.cy + 1);
            label.setAttribute("text-anchor", "middle");
            label.setAttribute("dominant-baseline", "middle");
            label.setAttribute("fill", "#e6edf3");
            label.setAttribute("font-size", "11");
            label.setAttribute("font-family", "monospace");
            // Truncate label to fit
            var labelText = node.id || "?";
            if (labelText.length > 20) labelText = labelText.substring(0, 18) + "..";
            label.textContent = labelText;
            svg.appendChild(label);

            // Type + tools indicator below node
            var infoY = pos.y + NODE_H + 12;
            if (node.type) {
                var typeLabel = document.createElementNS(ns, "text");
                typeLabel.setAttribute("x", pos.cx);
                typeLabel.setAttribute("y", infoY);
                typeLabel.setAttribute("text-anchor", "middle");
                typeLabel.setAttribute("fill", "#484f58");
                typeLabel.setAttribute("font-size", "9");
                typeLabel.textContent = node.type;
                svg.appendChild(typeLabel);
                infoY += 11;
            }
            if (node.tools && node.tools.length > 0) {
                var toolLabel = document.createElementNS(ns, "text");
                toolLabel.setAttribute("x", pos.cx);
                toolLabel.setAttribute("y", infoY);
                toolLabel.setAttribute("text-anchor", "middle");
                toolLabel.setAttribute("fill", "#3fb950");
                toolLabel.setAttribute("font-size", "8");
                toolLabel.textContent = node.tools.join(", ");
                svg.appendChild(toolLabel);
            }
        }

        var wrapper = document.createElement("div");
        wrapper.className = "dag-wrapper";
        wrapper.appendChild(svg);
        return wrapper;
    }

    // --- Escalation rendering ---
    function renderEscalations(escalations) {
        escalationCount.textContent = escalations ? escalations.length : 0;

        if (!escalations || escalations.length === 0) {
            escalationContainer.innerHTML = '<div class="empty-state">No pending escalations</div>';
            return;
        }

        let html = "";
        for (const esc of escalations) {
            const severityClass = "severity-" + (esc.severity || "info");
            const time = formatTime(esc.created_at);
            html += `<div class="escalation-card ${severityClass}">
                <div class="escalation-header">
                    <span class="escalation-severity" style="color: var(--${severityColor(esc.severity)})">${escapeHTML(esc.severity || "info")}</span>
                    <span class="activity-time">${time}</span>
                </div>
                <div class="escalation-question">${escapeHTML(esc.question)}</div>
                ${esc.context ? `<div class="escalation-context">${escapeHTML(esc.context)}</div>` : ""}
                <div class="resolve-form">
                    <input class="resolve-input" type="text" placeholder="Resolution..." data-id="${esc.id}">
                    <button class="resolve-btn" onclick="resolveEscalation('${esc.id}', this)">Resolve</button>
                </div>
            </div>`;
        }
        escalationContainer.innerHTML = html;
    }

    function severityColor(s) {
        switch (s) {
            case "critical": return "red";
            case "error": return "red";
            case "warning": return "orange";
            default: return "blue";
        }
    }

    // --- Resolve action ---
    window.resolveEscalation = async function (id, btn) {
        const input = btn.parentElement.querySelector(".resolve-input");
        const resolution = input.value.trim();
        if (!resolution) {
            input.focus();
            return;
        }

        btn.disabled = true;
        btn.textContent = "...";

        try {
            await postJSON(`/api/escalations/${id}/resolve`, { resolution: resolution });
            await refreshAll();
        } catch (err) {
            console.error("Resolve failed:", err);
            btn.disabled = false;
            btn.textContent = "Resolve";
        }
    };

    // --- New Mission form ---
    const newMissionBtn = document.getElementById("new-mission-btn");
    const missionForm = document.getElementById("new-mission-form");
    const missionTitle = document.getElementById("mission-title");
    const missionDesc = document.getElementById("mission-desc");
    const missionSubmit = document.getElementById("mission-submit");
    const missionCancel = document.getElementById("mission-cancel");

    newMissionBtn.addEventListener("click", function () {
        missionForm.style.display = missionForm.style.display === "none" ? "block" : "none";
        if (missionForm.style.display === "block") {
            missionTitle.focus();
        }
    });

    missionCancel.addEventListener("click", function () {
        missionForm.style.display = "none";
        missionTitle.value = "";
        missionDesc.value = "";
    });

    missionSubmit.addEventListener("click", async function () {
        const title = missionTitle.value.trim();
        if (!title) {
            missionTitle.focus();
            return;
        }

        missionSubmit.disabled = true;
        missionSubmit.textContent = "...";

        try {
            await postJSON("/api/objectives", {
                title: title,
                description: missionDesc.value.trim(),
            });
            missionForm.style.display = "none";
            missionTitle.value = "";
            missionDesc.value = "";
            await refreshAll();
        } catch (err) {
            console.error("Create mission failed:", err);
        } finally {
            missionSubmit.disabled = false;
            missionSubmit.textContent = "Create";
        }
    });

    missionTitle.addEventListener("keydown", function (e) {
        if (e.key === "Enter") {
            e.preventDefault();
            missionSubmit.click();
        }
        if (e.key === "Escape") {
            missionCancel.click();
        }
    });

    missionDesc.addEventListener("keydown", function (e) {
        if (e.key === "Escape") {
            missionCancel.click();
        }
    });

    // --- Objective action buttons ---
    const detailActions = document.getElementById("detail-actions");
    const btnResume = document.getElementById("btn-resume");
    const btnPause = document.getElementById("btn-pause");

    btnResume.addEventListener("click", async function () {
        var id = selectedObjectiveID || selectedMissionID;
        if (!id) return;
        btnResume.disabled = true;
        try {
            await postJSON("/api/objectives/" + id + "/resume", {});
            await refreshAll();
        } catch (err) {
            console.error("Resume failed:", err);
        } finally {
            btnResume.disabled = false;
        }
    });

    btnPause.addEventListener("click", async function () {
        var id = selectedObjectiveID || selectedMissionID;
        if (!id) return;
        btnPause.disabled = true;
        try {
            await postJSON("/api/objectives/" + id + "/pause", {});
            await refreshAll();
        } catch (err) {
            console.error("Pause failed:", err);
        } finally {
            btnPause.disabled = false;
        }
    });

    // --- Tree click: select objective + toggle ---
    document.addEventListener("click", function (e) {
        const row = e.target.closest(".tree-node-row");
        if (!row) return;

        const id = row.dataset.id;
        const toggle = row.querySelector(".tree-toggle");
        const node = row.parentElement;
        const children = node.querySelector(".tree-children");

        // Toggle expand/collapse
        if (children && toggle && !toggle.classList.contains("leaf")) {
            children.classList.toggle("expanded");
            toggle.classList.toggle("expanded");
        }

        // Select objective and load its data
        if (id && id !== selectedObjectiveID) {
            selectedObjectiveID = id;
            document.querySelectorAll(".tree-node-row.selected").forEach(function (el) {
                el.classList.remove("selected");
            });
            row.classList.add("selected");
            loadObjectiveDetail(id);
        }
    });

    async function loadObjectiveDetail(id) {
        try {
            var data = await fetchJSON("/api/objectives/" + id);
            var obj = data.objective;
            var events = data.recent_activity || [];

            detailTitle.textContent = obj.title || "Objective";
            detailActions.style.display = "flex";
            renderActivity(events);
            renderGraphs(events);
        } catch (err) {
            console.error("Load objective detail failed:", err);
        }
    }

    // --- WebSocket ---
    function connectWS() {
        if (ws) {
            ws.close();
            ws = null;
        }

        const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
        const url = proto + "//" + window.location.host + "/api/stream";

        ws = new WebSocket(url);

        ws.onopen = function () {
            setConnected(true);
            if (reconnectTimer) {
                clearTimeout(reconnectTimer);
                reconnectTimer = null;
            }
        };

        ws.onmessage = function (event) {
            try {
                const msg = JSON.parse(event.data);
                handleWSMessage(msg);
            } catch (e) {
                console.error("WS parse error:", e);
            }
        };

        ws.onclose = function () {
            setConnected(false);
            scheduleReconnect();
        };

        ws.onerror = function () {
            setConnected(false);
        };
    }

    function scheduleReconnect() {
        if (reconnectTimer) return;
        reconnectTimer = setTimeout(function () {
            reconnectTimer = null;
            connectWS();
        }, 3000);
    }

    function handleWSMessage(msg) {
        refreshAll();
    }

    function setConnected(connected) {
        if (connected) {
            statusDot.className = "status-dot connected";
            statusText.textContent = "Connected";
            footerStatus.textContent = "WebSocket connected";
        } else {
            statusDot.className = "status-dot disconnected";
            statusText.textContent = "Disconnected";
            footerStatus.textContent = "WebSocket disconnected — reconnecting...";
        }
    }

    // --- Utilities ---
    function formatTime(isoStr) {
        if (!isoStr) return "";
        const d = new Date(isoStr);
        if (isNaN(d.getTime())) return "";
        const now = new Date();
        const diffMs = now - d;
        const diffMin = Math.floor(diffMs / 60000);
        if (diffMin < 1) return "just now";
        if (diffMin < 60) return diffMin + "m ago";
        const diffHr = Math.floor(diffMin / 60);
        if (diffHr < 24) return diffHr + "h ago";
        return d.toLocaleDateString();
    }

    function updateClock() {
        footerTime.textContent = new Date().toLocaleTimeString();
    }

    // --- Data refresh ---
    function refreshTree() {
        if (selectedMissionID) {
            loadMissionTree(selectedMissionID);
        } else {
            treeContainer.innerHTML = '<div class="empty-state">Select a mission above</div>';
            objectiveCount.textContent = allMissions.length;
        }
    }

    async function refreshAll() {
        try {
            const [objectives, escalations] = await Promise.all([
                fetchJSON("/api/objectives"),
                fetchJSON("/api/escalations"),
            ]);

            // buildTree populates the mission dropdown and triggers tree load
            buildTree(objectives || []);
            renderEscalations(escalations);

            // Refresh selected objective detail, or show mission-level activity
            if (selectedObjectiveID) {
                loadObjectiveDetail(selectedObjectiveID);
            } else if (selectedMissionID) {
                loadObjectiveDetail(selectedMissionID);
            } else {
                tabActivity.innerHTML = '<div class="empty-state">Select a mission</div>';
                tabGraphs.innerHTML = '<div class="empty-state">Select a mission to see its graphs</div>';
            }
        } catch (err) {
            console.error("Refresh failed:", err);
        }
    }

    // --- Init ---
    refreshAll();
    connectWS();
    setInterval(updateClock, 1000);
    updateClock();

    // Periodic refresh as backup to WebSocket
    setInterval(refreshAll, 30000);
})();
