// ============================================================
// Companies
// ============================================================

async function showCompanies() {
    document.getElementById('companies-modal').style.display = 'flex';
    selectedCompanyID = '';
    refreshCompanyCreateCEOOptions();
    await loadCompanies();
}

function closeCompanies() {
    document.getElementById('companies-modal').style.display = 'none';
    selectedCompanyID = '';
}

function refreshCompanyCreateCEOOptions() {
    const select = document.getElementById('company-create-ceo');
    if (!select) return;
    const previous = select.value || '';
    const options = ['<option value="">(none)</option>'];
    for (const a of agents) {
        options.push(`<option value="${escAttr(a.id)}">${escHtml(a.name || a.id)}</option>`);
    }
    select.innerHTML = options.join('');
    select.value = previous;
}

async function loadCompanies() {
    const container = document.getElementById('companies-list');
    if (!container) return;
    container.innerHTML = '<div style="color:var(--text-muted);padding:12px">Loading companies...</div>';

    try {
        const data = await api('GET', '/api/companies');
        const companyList = data.companies || [];
        companyKnowledgeCache = {};
        companySummaries = companyList.map((company) => ({
            id: company.id,
            name: company.name,
            ceo_agent_id: company.ceo_agent_id,
        }));
        refreshPipelineEditorCompanyOptions();
        refreshPolymarketPortfolioCompanyOptions();
        companies = await Promise.all(companyList.map(async (company) => {
            try {
                const [poly, shopify, topdawg, cjd, amz] = await Promise.all([
                    api('GET', `/api/companies/${company.id}/polymarket`),
                    api('GET', `/api/companies/${company.id}/shopify`),
                    api('GET', `/api/companies/${company.id}/topdawg`),
                    api('GET', `/api/companies/${company.id}/cjdropshipping`),
                    api('GET', `/api/companies/${company.id}/amazon`),
                ]);
                return {
                    ...company,
                    polymarket: poly.polymarket || null,
                    shopify: shopify.shopify || null,
                    topdawg: topdawg.topdawg || null,
                    cjdropshipping: cjd.cjdropshipping || null,
                    amazon: amz.amazon || null,
                    polymarket_error: '',
                    shopify_error: '',
                    topdawg_error: '',
                    cjdropshipping_error: '',
                    amazon_error: '',
                };
            } catch (e) {
                void e;
                let polymarket = null;
                let shopify = null;
                let topdawg = null;
                let cjdropshipping = null;
                let amazonConn = null;
                let polymarketError = '';
                let shopifyError = '';
                let topdawgError = '';
                let cjdropshippingError = '';
                let amazonError = '';
                try {
                    const poly = await api('GET', `/api/companies/${company.id}/polymarket`);
                    polymarket = poly.polymarket || null;
                } catch (polyErr) {
                    polymarketError = companySectionErrorMessage(polyErr, 'Failed to load Polymarket config');
                }
                try {
                    const shop = await api('GET', `/api/companies/${company.id}/shopify`);
                    shopify = shop.shopify || null;
                } catch (shopErr) {
                    shopifyError = companySectionErrorMessage(shopErr, 'Failed to load Shopify config');
                }
                try {
                    const td = await api('GET', `/api/companies/${company.id}/topdawg`);
                    topdawg = td.topdawg || null;
                } catch (tdErr) {
                    topdawgError = companySectionErrorMessage(tdErr, 'Failed to load TopDawg config');
                }
                try {
                    const cj = await api('GET', `/api/companies/${company.id}/cjdropshipping`);
                    cjdropshipping = cj.cjdropshipping || null;
                } catch (cjErr) {
                    cjdropshippingError = companySectionErrorMessage(cjErr, 'Failed to load CJ Dropshipping config');
                }
                try {
                    const amzResp = await api('GET', `/api/companies/${company.id}/amazon`);
                    amazonConn = amzResp.amazon || null;
                } catch (amzErr) {
                    amazonError = companySectionErrorMessage(amzErr, 'Failed to load Amazon config');
                }
                return {
                    ...company,
                    polymarket,
                    shopify,
                    topdawg,
                    cjdropshipping,
                    amazon: amazonConn,
                    polymarket_error: polymarketError,
                    shopify_error: shopifyError,
                    topdawg_error: topdawgError,
                    cjdropshipping_error: cjdropshippingError,
                    amazon_error: amazonError,
                };
            }
        }));
        renderCompanies(companies);
    } catch (e) {
        companies = [];
        companySummaries = [];
        refreshPipelineEditorCompanyOptions();
        refreshPolymarketPortfolioCompanyOptions();
        container.innerHTML = `<div style="color:var(--red);padding:12px">Failed to load companies: ${escHtml(e.message || 'request failed')}</div>`;
    }
}

function renderCompanies(companyList) {
    const container = document.getElementById('companies-list');
    if (!container) return;

    if (!companyList.length) {
        selectedCompanyID = '';
        container.innerHTML = '<div style="color:var(--text-muted);text-align:center;padding:20px">No companies yet.</div>';
        return;
    }

    const selectedCompany = selectedCompanyID
        ? companyList.find((company) => String(company.id || '').trim() === selectedCompanyID)
        : null;
    if (selectedCompanyID && !selectedCompany) {
        selectedCompanyID = '';
    }

    if (!selectedCompanyID) {
        container.innerHTML = `
            <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
                <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase">Select a company to edit</div>
                <button class="btn btn-sm" onclick='loadCompanies()'>Refresh</button>
            </div>
            ${companyList.map((company) => {
                const members = company.members || [];
                const companyID = String(company.id || '').trim();
                const memberIDs = members.map(m => String(m.agent_id || '').trim()).filter(Boolean);
                const openAllAttr = escAttr(JSON.stringify(memberIDs));
                return `<div class="peer-group-card" style="cursor:pointer;margin-bottom:8px" onclick='selectCompany(${JSON.stringify(companyID)})'>
                    <div class="group-header">
                        <div>
                            <div class="group-name">${escHtml(company.name || company.id)}</div>
                            <div style="font-size:12px;color:var(--text-muted);margin-top:2px">ID: ${escHtml(company.id)}</div>
                            <div style="font-size:12px;color:var(--text-muted);margin-top:2px">CEO: ${escHtml(company.ceo_agent_id || '(not set)')} · Members: ${members.length}</div>
                        </div>
                        <div style="display:flex;align-items:center;gap:8px">
                            ${members.length ? `<button class="btn btn-sm btn-primary" onclick='event.stopPropagation(); openCompanyAgents(${openAllAttr})' title="Open all company agents as columns">Open All</button>` : ''}
                            <div style="font-size:12px;color:var(--accent)">Edit</div>
                        </div>
                    </div>
                </div>`;
            }).join('')}
        `;
        return;
    }

    const editableCompanies = selectedCompany ? [selectedCompany] : [];
    const selectedName = selectedCompany ? (selectedCompany.name || selectedCompany.id) : selectedCompanyID;

    const detailMemberIDs = (selectedCompany && selectedCompany.members || []).map(m => String(m.agent_id || '').trim()).filter(Boolean);
    const detailOpenAllAttr = escAttr(JSON.stringify(detailMemberIDs));
    container.innerHTML = `
        <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin-bottom:12px;flex-wrap:wrap">
            <button class="btn btn-sm" onclick='clearSelectedCompany()'>Back to List</button>
            <div style="display:flex;align-items:center;gap:8px">
                ${detailMemberIDs.length ? `<button class="btn btn-sm btn-primary" onclick='openCompanyAgents(${detailOpenAllAttr})'>Open All Agents</button>` : ''}
                <div style="font-size:12px;color:var(--text-muted)">Editing: ${escHtml(selectedName || selectedCompanyID)}</div>
            </div>
        </div>
    ` + editableCompanies.map((company) => {
        const members = company.members || [];
        const memberIDSet = new Set(members.map((m) => String(m.agent_id || '').trim()));
        const membersMarkup = members.length
            ? members.map((m) => {
                const memberKey = companyMemberEntryKey(m.agent_id);
                return `<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:6px">
                    <span class="member-tag" style="margin:0">${escHtml(m.agent_id)}</span>
                    <input type="text" id="company-member-role-${escAttr(company.id)}-${escAttr(memberKey)}" value="${escAttr(m.role || '')}" placeholder="role" style="width:150px">
                    <button class="btn btn-sm" onclick='updateCompanyMemberRole(${JSON.stringify(company.id)}, ${JSON.stringify(m.agent_id)})'>Save Role</button>
                    <button class="btn btn-sm btn-danger" onclick='removeCompanyMember(${JSON.stringify(company.id)}, ${JSON.stringify(m.agent_id)})'>Remove</button>
                </div>`;
            }).join('')
            : '<span style="color:var(--text-muted);font-size:12px">No members</span>';
        const availableMemberOptions = agents
            .filter((agent) => !memberIDSet.has(String(agent.id || '').trim()))
            .map((agent) => `<option value="${escAttr(agent.id)}">${escHtml(agent.name || agent.id)}</option>`)
            .join('');
        const ceoOptions = members
            .map((m) => `<option value="${escAttr(m.agent_id)}" ${String(company.ceo_agent_id || '') === String(m.agent_id || '') ? 'selected' : ''}>${escHtml(m.agent_id)}${m.role ? ` (${escHtml(m.role)})` : ''}</option>`)
            .join('');

        const poly = company.polymarket || {};
        const sigType = Number.isFinite(Number(poly.signature_type)) ? Number(poly.signature_type) : 0;
        const chainIDValue = poly.chain_id ? String(poly.chain_id) : '';
        const polyEnabled = company.polymarket ? !!poly.enabled : true;
        const polyStatusText = company.polymarket_error ? escHtml(company.polymarket_error) : '';
        const polyStatusColor = company.polymarket_error ? 'var(--red)' : 'var(--text-muted)';

        const shopify = company.shopify || {};
        const shopifyEnabled = company.shopify ? !!shopify.enabled : true;
        const shopifyStatusText = company.shopify_error ? escHtml(company.shopify_error) : '';
        const shopifyStatusColor = company.shopify_error ? 'var(--red)' : 'var(--text-muted)';
        const shopifyClientSecretPlaceholder = company.shopify && shopify.has_client_secret ? 'Stored secret (leave blank to keep existing)' : 'Client secret';
        const shopifyPanelOpen = !!companyShopifyPanelOpen[String(company.id || '').trim()];
        const shopifyToggleLabel = shopifyPanelOpen ? 'Hide Shopify Form' : 'Configure Shopify';

        const topdawg = company.topdawg || {};
        const topdawgEnabled = company.topdawg ? !!topdawg.enabled : false;
        const topdawgStatusText = company.topdawg_error ? escHtml(company.topdawg_error) : '';
        const topdawgStatusColor = company.topdawg_error ? 'var(--red)' : 'var(--text-muted)';
        const topdawgAPIKeyPlaceholder = company.topdawg && topdawg.has_api_key ? 'Stored API key (leave blank to keep existing)' : 'TopDawg API key';

        const cjdropshipping = company.cjdropshipping || {};
        const cjdropshippingEnabled = company.cjdropshipping ? !!cjdropshipping.enabled : true;
        const cjdropshippingStatusText = company.cjdropshipping_error ? escHtml(company.cjdropshipping_error) : '';
        const cjdropshippingStatusColor = company.cjdropshipping_error ? 'var(--red)' : 'var(--text-muted)';
        const cjdropshippingAPIKeyPlaceholder = company.cjdropshipping && cjdropshipping.has_api_key ? 'Stored API key (leave blank to keep existing)' : 'CJ API key';

        const amazonData = company.amazon || {};
        const amazonEnabled = company.amazon ? !!amazonData.enabled : true;
        const amazonStatusText = company.amazon_error ? escHtml(company.amazon_error) : '';
        const amazonStatusColor = company.amazon_error ? 'var(--red)' : 'var(--text-muted)';
        const amazonAccessKeyPlaceholder = company.amazon && amazonData.has_access_key ? 'Stored (leave blank to keep)' : 'Access key';
        const amazonSecretKeyPlaceholder = company.amazon && amazonData.has_secret_key ? 'Stored (leave blank to keep)' : 'Secret key';
        const walletPublicKeys = (company && typeof company.wallet_public_keys === 'object' && company.wallet_public_keys) ? company.wallet_public_keys : {};
        const ethPublicKey = String(walletPublicKeys.ethereum || '').trim();
        const solPublicKey = String(walletPublicKeys.solana || '').trim();
        const walletSeedError = String(company.wallet_seed_phrase_error || '').trim();
        const walletPublicKeysError = String(company.wallet_public_keys_error || '').trim();
        const walletStatusText = walletSeedError || walletPublicKeysError;
        const walletStatusColor = walletStatusText ? 'var(--red)' : 'var(--text-muted)';

        const defaultKnowledgeKind = 'policy';

        return `<div class="peer-group-card">
            <div class="group-header">
                <div>
                    <div class="group-name">${escHtml(company.name || company.id)}</div>
                    <div style="font-size:12px;color:var(--text-muted);margin-top:2px">ID: ${escHtml(company.id)}</div>
                    <div style="font-size:12px;color:var(--text-muted);margin-top:2px">CEO: ${escHtml(company.ceo_agent_id || '(not set)')} · Members: ${members.length}</div>
                </div>
                <div style="display:flex;gap:8px">
                    <button class="btn btn-sm" onclick='loadCompanies()'>Refresh</button>
                    <button class="btn btn-sm btn-danger" onclick='deleteCompany(${JSON.stringify(company.id)})'>Delete</button>
                </div>
            </div>
            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:8px 0 10px">Metadata</div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-name-${escAttr(company.id)}">Name</label>
                    <input type="text" id="company-name-${escAttr(company.id)}" value="${escAttr(company.name || '')}">
                </div>
                <div class="form-group">
                    <label for="company-ceo-${escAttr(company.id)}">CEO</label>
                    <select id="company-ceo-${escAttr(company.id)}">
                        <option value="">(none)</option>
                        ${ceoOptions}
                    </select>
                </div>
            </div>
            <div class="form-group">
                <label for="company-description-${escAttr(company.id)}">Description</label>
                <input type="text" id="company-description-${escAttr(company.id)}" value="${escAttr(company.description || '')}">
            </div>
            <div style="display:flex;gap:8px;justify-content:flex-end">
                <button class="btn btn-sm btn-primary" onclick='saveCompanyMetadata(${JSON.stringify(company.id)})'>Save Metadata</button>
                <button class="btn btn-sm" onclick='setCompanyCEO(${JSON.stringify(company.id)})'>Set CEO</button>
            </div>
            <div id="company-meta-status-${escAttr(company.id)}" style="font-size:11px;color:var(--text-muted);margin-top:8px;min-height:16px"></div>

            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:12px 0 10px">Wallet</div>
            <div class="form-group">
                <label>Wallet Seed Phrase</label>
                <div style="font-size:12px;color:var(--text-muted)">Hidden for security. Use Copy Seed Phrase to place it on the clipboard.</div>
            </div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-wallet-eth-${escAttr(company.id)}">Ethereum Public Address</label>
                    <input type="text" id="company-wallet-eth-${escAttr(company.id)}" value="${escAttr(ethPublicKey)}" readonly style="font-family:monospace">
                </div>
                <div class="form-group">
                    <label for="company-wallet-sol-${escAttr(company.id)}">Solana Public Key</label>
                    <input type="text" id="company-wallet-sol-${escAttr(company.id)}" value="${escAttr(solPublicKey)}" readonly style="font-family:monospace">
                </div>
            </div>
            <div style="display:flex;gap:8px;justify-content:flex-end;flex-wrap:wrap">
                <button class="btn btn-sm" onclick='copyCompanyWalletKey(${JSON.stringify(company.id)}, "ethereum")'>Copy Ethereum Key</button>
                <button class="btn btn-sm" onclick='copyCompanyWalletKey(${JSON.stringify(company.id)}, "solana")'>Copy Solana Key</button>
                <button class="btn btn-sm" onclick='copyCompanySeedPhrase(${JSON.stringify(company.id)})'>Copy Seed Phrase</button>
            </div>
            <div id="company-wallet-status-${escAttr(company.id)}" style="font-size:11px;color:${walletStatusColor};margin-top:8px;min-height:16px">${escHtml(walletStatusText)}</div>

            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:12px 0 10px">Members</div>
            <div class="group-members">${membersMarkup}</div>
            <div class="add-member-row">
                <select id="company-add-member-agent-${escAttr(company.id)}" style="flex:1">
                    ${availableMemberOptions || '<option value="">No available agents</option>'}
                </select>
                <input type="text" id="company-add-member-role-${escAttr(company.id)}" placeholder="role (optional)" style="width:180px">
                <button class="btn btn-sm btn-primary" onclick='addCompanyMember(${JSON.stringify(company.id)})'>Add</button>
            </div>
            <div id="company-members-status-${escAttr(company.id)}" style="font-size:11px;color:var(--text-muted);margin-top:8px;min-height:16px"></div>

            <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;margin:12px 0 10px">
                <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase">Shopify (Company Scope)</div>
                <button class="btn btn-sm" id="company-shopify-toggle-${escAttr(company.id)}" onclick='toggleCompanyShopifyForm(${JSON.stringify(company.id)})'>${shopifyToggleLabel}</button>
            </div>
            <div id="company-shopify-panel-${escAttr(company.id)}" style="display:${shopifyPanelOpen ? 'block' : 'none'}">
                <div class="form-row">
                    <div class="form-group">
                        <label for="company-shopify-url-${escAttr(company.id)}">Shop URL</label>
                        <input type="text" id="company-shopify-url-${escAttr(company.id)}" value="${escAttr(shopify.shop_url || '')}" placeholder="your-store.myshopify.com">
                    </div>
                    <div class="form-group">
                        <label for="company-shopify-version-${escAttr(company.id)}">API Version</label>
                        <input type="text" id="company-shopify-version-${escAttr(company.id)}" value="${escAttr(shopify.api_version || '')}" placeholder="2025-01">
                    </div>
                </div>
                <div class="form-row">
                    <div class="form-group">
                        <label for="company-shopify-client-id-${escAttr(company.id)}">Client ID</label>
                        <input type="text" id="company-shopify-client-id-${escAttr(company.id)}" value="${escAttr(shopify.client_id || '')}" placeholder="Shopify app client ID">
                    </div>
                    <div class="form-group">
                        <label for="company-shopify-client-secret-${escAttr(company.id)}">Client Secret</label>
                        <input type="password" id="company-shopify-client-secret-${escAttr(company.id)}" value="" placeholder="${escAttr(shopifyClientSecretPlaceholder)}">
                    </div>
                </div>
                <div class="form-row">
                    <div class="form-group" style="display:flex;align-items:flex-end">
                        <div style="font-size:12px;color:var(--text-muted)">Manager exchanges client credentials for short-lived Shopify access tokens automatically, and Shopify webhook signatures use your app client secret.</div>
                    </div>
                </div>
                <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap">
                    <label style="font-size:12px;color:var(--text-muted)">
                        <input type="checkbox" id="company-shopify-enabled-${escAttr(company.id)}" ${shopifyEnabled ? 'checked' : ''}>Enabled
                    </label>
                    <div style="display:flex;gap:8px">
                        <button class="btn btn-sm btn-primary" onclick='saveCompanyShopify(${JSON.stringify(company.id)})'>Save Shopify</button>
                        <button class="btn btn-sm" onclick='testCompanyShopify(${JSON.stringify(company.id)})'>Test Shopify</button>
                        <button class="btn btn-sm btn-danger" onclick='deleteCompanyShopify(${JSON.stringify(company.id)})'>Delete Shopify</button>
                    </div>
                </div>
                <div id="company-shopify-status-${escAttr(company.id)}" style="font-size:11px;color:${shopifyStatusColor};margin-top:8px;min-height:16px">${shopifyStatusText}</div>
            </div>

            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:8px 0 10px">TopDawg (Company Scope)</div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-topdawg-supplier-id-${escAttr(company.id)}">Supplier ID</label>
                    <input type="text" id="company-topdawg-supplier-id-${escAttr(company.id)}" value="${escAttr(topdawg.supplier_id || '')}" placeholder="TopDawg supplier ID">
                </div>
                <div class="form-group">
                    <label for="company-topdawg-api-key-${escAttr(company.id)}">API Key</label>
                    <input type="password" id="company-topdawg-api-key-${escAttr(company.id)}" value="" placeholder="${escAttr(topdawgAPIKeyPlaceholder)}">
                </div>
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap">
                <label style="font-size:12px;color:var(--text-muted)">
                    <input type="checkbox" id="company-topdawg-enabled-${escAttr(company.id)}" ${topdawgEnabled ? 'checked' : ''}>Enabled
                </label>
                <div style="display:flex;gap:8px">
                    <button class="btn btn-sm btn-primary" onclick='saveCompanyTopDawg(${JSON.stringify(company.id)})'>Save TopDawg</button>
                    <button class="btn btn-sm" onclick='testCompanyTopDawg(${JSON.stringify(company.id)})'>Test TopDawg</button>
                    <button class="btn btn-sm btn-danger" onclick='deleteCompanyTopDawg(${JSON.stringify(company.id)})'>Delete TopDawg</button>
                </div>
            </div>
            <div id="company-topdawg-status-${escAttr(company.id)}" style="font-size:11px;color:${topdawgStatusColor};margin-top:8px;min-height:16px">${topdawgStatusText}</div>

            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:8px 0 10px">CJ Dropshipping (Company Scope)</div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-cjd-api-key-${escAttr(company.id)}">API Key</label>
                    <input type="password" id="company-cjd-api-key-${escAttr(company.id)}" value="" placeholder="${escAttr(cjdropshippingAPIKeyPlaceholder)}">
                </div>
                <div class="form-group">
                    <label for="company-cjd-default-from-country-${escAttr(company.id)}">Default From Country</label>
                    <input type="text" id="company-cjd-default-from-country-${escAttr(company.id)}" value="${escAttr(cjdropshipping.default_from_country_code || '')}" placeholder="US, CN, GB, etc.">
                </div>
            </div>
            <div class="form-row">
                <div class="form-group" style="display:flex;align-items:flex-end">
                    <div style="font-size:12px;color:var(--text-muted)">Manager exchanges API key for access tokens and rotates them automatically.</div>
                </div>
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap">
                <label style="font-size:12px;color:var(--text-muted)">
                    <input type="checkbox" id="company-cjd-enabled-${escAttr(company.id)}" ${cjdropshippingEnabled ? 'checked' : ''}>Enabled
                </label>
                <div style="display:flex;gap:8px">
                    <button class="btn btn-sm btn-primary" onclick='saveCompanyCJDropshipping(${JSON.stringify(company.id)})'>Save CJ</button>
                    <button class="btn btn-sm" onclick='testCompanyCJDropshipping(${JSON.stringify(company.id)})'>Test CJ</button>
                    <button class="btn btn-sm btn-danger" onclick='deleteCompanyCJDropshipping(${JSON.stringify(company.id)})'>Delete CJ</button>
                </div>
            </div>
            <div id="company-cjd-status-${escAttr(company.id)}" style="font-size:11px;color:${cjdropshippingStatusColor};margin-top:8px;min-height:16px">${cjdropshippingStatusText}</div>

            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:8px 0 10px">Amazon PAAPI (Company Scope)</div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-amazon-access-key-${escAttr(company.id)}">Access Key</label>
                    <input type="password" id="company-amazon-access-key-${escAttr(company.id)}" value="" placeholder="${escAttr(amazonAccessKeyPlaceholder)}">
                </div>
                <div class="form-group">
                    <label for="company-amazon-secret-key-${escAttr(company.id)}">Secret Key</label>
                    <input type="password" id="company-amazon-secret-key-${escAttr(company.id)}" value="" placeholder="${escAttr(amazonSecretKeyPlaceholder)}">
                </div>
            </div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-amazon-partner-tag-${escAttr(company.id)}">Partner Tag</label>
                    <input type="text" id="company-amazon-partner-tag-${escAttr(company.id)}" value="${escAttr(amazonData.partner_tag || '')}" placeholder="example-20">
                </div>
                <div class="form-group">
                    <label for="company-amazon-marketplace-${escAttr(company.id)}">Marketplace</label>
                    <input type="text" id="company-amazon-marketplace-${escAttr(company.id)}" value="${escAttr(amazonData.marketplace || 'US')}" placeholder="US, UK, DE, FR, JP, CA, AU">
                </div>
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap">
                <label style="font-size:12px;color:var(--text-muted)">
                    <input type="checkbox" id="company-amazon-enabled-${escAttr(company.id)}" ${amazonEnabled ? 'checked' : ''}>Enabled
                </label>
                <div style="display:flex;gap:8px">
                    <button class="btn btn-sm btn-primary" onclick='saveCompanyAmazon(${JSON.stringify(company.id)})'>Save Amazon</button>
                    <button class="btn btn-sm" onclick='testCompanyAmazon(${JSON.stringify(company.id)})'>Test Amazon</button>
                    <button class="btn btn-sm btn-danger" onclick='deleteCompanyAmazon(${JSON.stringify(company.id)})'>Delete Amazon</button>
                </div>
            </div>
            <div id="company-amazon-status-${escAttr(company.id)}" style="font-size:11px;color:${amazonStatusColor};margin-top:8px;min-height:16px">${amazonStatusText}</div>

            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:8px 0 10px">Polymarket (Company Scope)</div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-poly-proxy-${escAttr(company.id)}">Proxy URL</label>
                    <input type="text" id="company-poly-proxy-${escAttr(company.id)}" value="${escAttr(poly.proxy_url || '')}" placeholder="socks5://127.0.0.1:9050">
                </div>
                <div class="form-group">
                    <label for="company-poly-rpc-${escAttr(company.id)}">Onchain RPC URL</label>
                    <input type="text" id="company-poly-rpc-${escAttr(company.id)}" value="${escAttr(poly.onchain_rpc_url || '')}" placeholder="https://polygon-rpc.com">
                </div>
            </div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-poly-funder-${escAttr(company.id)}">Funder Address (optional)</label>
                    <input type="text" id="company-poly-funder-${escAttr(company.id)}" value="${escAttr(poly.funder_address || '')}" placeholder="0x...">
                </div>
                <div class="form-group">
                    <label for="company-poly-sig-${escAttr(company.id)}">Signature Type</label>
                    <select id="company-poly-sig-${escAttr(company.id)}">
                        <option value="0" ${sigType === 0 ? 'selected' : ''}>0 - EOA</option>
                        <option value="1" ${sigType === 1 ? 'selected' : ''}>1 - Proxy Wallet</option>
                        <option value="2" ${sigType === 2 ? 'selected' : ''}>2 - Gnosis Safe</option>
                    </select>
                </div>
                <div class="form-group">
                    <label for="company-poly-chain-${escAttr(company.id)}">Chain ID (optional)</label>
                    <input type="number" id="company-poly-chain-${escAttr(company.id)}" min="0" value="${escAttr(chainIDValue)}" placeholder="137">
                </div>
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap">
                <label style="font-size:12px;color:var(--text-muted)">
                    <input type="checkbox" id="company-poly-enabled-${escAttr(company.id)}" ${polyEnabled ? 'checked' : ''}>Enabled
                </label>
                <div style="display:flex;gap:8px">
                    <button class="btn btn-sm" onclick='showPolymarketPortfolio(${JSON.stringify(company.id)})'>Open Portfolio</button>
                    <button class="btn btn-sm btn-primary" onclick='saveCompanyPolymarket(${JSON.stringify(company.id)})'>Save Polymarket</button>
                    <button class="btn btn-sm btn-danger" onclick='deleteCompanyPolymarket(${JSON.stringify(company.id)})'>Delete Polymarket</button>
                </div>
            </div>
            <div id="company-poly-status-${escAttr(company.id)}" style="font-size:11px;color:${polyStatusColor};margin-top:8px;min-height:16px">${polyStatusText}</div>

            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin:12px 0 10px">Company Knowledge</div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-knowledge-query-${escAttr(company.id)}">Search Query</label>
                    <input type="text" id="company-knowledge-query-${escAttr(company.id)}" placeholder="title/content/tags">
                </div>
                <div class="form-group">
                    <label for="company-knowledge-kind-${escAttr(company.id)}">Kind Filter (optional)</label>
                    <input type="text" id="company-knowledge-kind-${escAttr(company.id)}" placeholder="policy">
                </div>
                <div class="form-group" style="max-width:120px">
                    <label for="company-knowledge-limit-${escAttr(company.id)}">Limit</label>
                    <input type="number" id="company-knowledge-limit-${escAttr(company.id)}" value="20" min="1" max="100">
                </div>
            </div>
            <div style="display:flex;gap:8px;justify-content:flex-end">
                <button class="btn btn-sm" onclick='loadCompanyKnowledge(${JSON.stringify(company.id)})'>Load Knowledge</button>
            </div>
            <div class="form-row" style="margin-top:8px">
                <div class="form-group">
                    <label for="company-knowledge-add-kind-${escAttr(company.id)}">New Kind</label>
                    <input type="text" id="company-knowledge-add-kind-${escAttr(company.id)}" value="${escAttr(defaultKnowledgeKind)}">
                </div>
                <div class="form-group">
                    <label for="company-knowledge-add-title-${escAttr(company.id)}">New Title</label>
                    <input type="text" id="company-knowledge-add-title-${escAttr(company.id)}" placeholder="Optional title">
                </div>
            </div>
            <div class="form-group">
                <label for="company-knowledge-add-content-${escAttr(company.id)}">New Content</label>
                <textarea id="company-knowledge-add-content-${escAttr(company.id)}" rows="3" placeholder="Required content"></textarea>
            </div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-knowledge-add-tags-${escAttr(company.id)}">Tags (comma-separated)</label>
                    <input type="text" id="company-knowledge-add-tags-${escAttr(company.id)}" placeholder="ops,policy">
                </div>
                <div class="form-group" style="max-width:180px;display:flex;align-items:flex-end;justify-content:flex-end">
                    <button class="btn btn-sm btn-primary" onclick='addCompanyKnowledge(${JSON.stringify(company.id)})'>Add Knowledge</button>
                </div>
            </div>
            <div id="company-knowledge-status-${escAttr(company.id)}" style="font-size:11px;color:var(--text-muted);margin-top:8px;min-height:16px"></div>
            <div id="company-knowledge-list-${escAttr(company.id)}" style="margin-top:8px"></div>
        </div>`;
    }).join('');

    for (const company of editableCompanies) {
        renderCompanyKnowledgeList(company.id);
    }
}

function openCompanyAgents(memberIDs) {
    const ids = typeof memberIDs === 'string' ? JSON.parse(memberIDs) : memberIDs;
    if (!Array.isArray(ids) || !ids.length) return;
    for (const id of ids) {
        addAgentColumn(id);
    }
    // Close the companies modal so the columns are visible
    closeCompanies();
}

function selectCompany(companyID) {
    const id = String(companyID || '').trim();
    if (!id) return;
    selectedCompanyID = id;
    renderCompanies(companies);
}

function clearSelectedCompany() {
    selectedCompanyID = '';
    renderCompanies(companies);
}

async function createCompany() {
    const nameInput = document.getElementById('company-create-name');
    const descriptionInput = document.getElementById('company-create-description');
    const ceoSelect = document.getElementById('company-create-ceo');
    if (!nameInput || !descriptionInput || !ceoSelect) return;

    const name = nameInput.value.trim();
    const description = descriptionInput.value.trim();
    const ceoAgentID = ceoSelect.value.trim();
    if (!name) {
        alert('Company name is required');
        return;
    }

    const body = { name, description };
    if (ceoAgentID) {
        body.ceo_agent_id = ceoAgentID;
    }

    try {
        await api('POST', '/api/companies', body);
        nameInput.value = '';
        descriptionInput.value = '';
        ceoSelect.value = '';
        await loadCompanies();
    } catch (e) {
        alert('Failed to create company: ' + e.message);
    }
}

function setCompanyMetaStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-meta-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

function setCompanyWalletStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-wallet-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

async function copyTextToClipboard(text) {
    if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        return;
    }

    const temp = document.createElement('textarea');
    temp.value = text;
    temp.setAttribute('readonly', '');
    temp.style.position = 'fixed';
    temp.style.left = '-9999px';
    temp.style.top = '-9999px';
    document.body.appendChild(temp);
    temp.focus();
    temp.select();
    const copied = document.execCommand('copy');
    document.body.removeChild(temp);
    if (!copied) {
        throw new Error('clipboard copy command failed');
    }
    window.getSelection()?.removeAllRanges();
}

async function copyCompanySeedPhrase(companyID) {
    const key = String(companyID || '').trim();
    if (!key) return;
    const company = companies.find((item) => String(item?.id || '').trim() === key);
    if (!company) {
        setCompanyWalletStatus(companyID, 'Company data not loaded yet. Refresh and try again.', true);
        return;
    }

    const seedPhrase = String(company.wallet_seed_phrase || '').trim();
    if (!seedPhrase) {
        setCompanyWalletStatus(companyID, 'No wallet seed phrase available to copy.', true);
        return;
    }

    try {
        await copyTextToClipboard(seedPhrase);
        setCompanyWalletStatus(companyID, 'Wallet seed phrase copied to clipboard.', false);
    } catch (e) {
        const msg = e && e.message ? String(e.message) : 'copy failed';
        setCompanyWalletStatus(companyID, `Failed to copy wallet seed phrase: ${msg}`, true);
    }
}

async function copyCompanyWalletKey(companyID, chain) {
    const key = String(companyID || '').trim();
    if (!key) return;
    const company = companies.find((item) => String(item?.id || '').trim() === key);
    if (!company) {
        setCompanyWalletStatus(companyID, 'Company data not loaded yet. Refresh and try again.', true);
        return;
    }

    const walletPublicKeys = (company && typeof company.wallet_public_keys === 'object' && company.wallet_public_keys) ? company.wallet_public_keys : {};
    const normalizedChain = String(chain || '').trim().toLowerCase();
    let label = '';
    let value = '';
    if (normalizedChain === 'ethereum') {
        label = 'Ethereum public address';
        value = String(walletPublicKeys.ethereum || '').trim();
    } else if (normalizedChain === 'solana') {
        label = 'Solana public key';
        value = String(walletPublicKeys.solana || '').trim();
    } else {
        setCompanyWalletStatus(companyID, 'Unknown wallet key type requested.', true);
        return;
    }

    if (!value) {
        setCompanyWalletStatus(companyID, `${label} is not available to copy.`, true);
        return;
    }

    try {
        await copyTextToClipboard(value);
        setCompanyWalletStatus(companyID, `${label} copied to clipboard.`, false);
    } catch (e) {
        const msg = e && e.message ? String(e.message) : 'copy failed';
        setCompanyWalletStatus(companyID, `Failed to copy ${label.toLowerCase()}: ${msg}`, true);
    }
}

async function saveCompanyMetadata(companyID) {
    const nameInput = document.getElementById(`company-name-${companyID}`);
    const descriptionInput = document.getElementById(`company-description-${companyID}`);
    if (!nameInput || !descriptionInput) return;

    const name = nameInput.value.trim();
    const description = descriptionInput.value.trim();
    if (!name) {
        setCompanyMetaStatus(companyID, 'Company name is required', true);
        return;
    }

    setCompanyMetaStatus(companyID, 'Saving company metadata...', false);
    try {
        await api('PATCH', `/api/companies/${companyID}`, { name, description });
        setCompanyMetaStatus(companyID, 'Company metadata saved.', false);
        await loadCompanies();
    } catch (e) {
        setCompanyMetaStatus(companyID, companySectionErrorMessage(e, 'Failed to save company metadata'), true);
    }
}

async function deleteCompany(companyID) {
    if (!confirm('Delete this company and all related company resources?')) return;
    setCompanyMetaStatus(companyID, 'Deleting company...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}`);
        await loadCompanies();
    } catch (e) {
        setCompanyMetaStatus(companyID, companySectionErrorMessage(e, 'Failed to delete company'), true);
    }
}

function setCompanyMembersStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-members-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

function companySectionErrorMessage(err, fallback) {
    const raw = String((err && err.message) || fallback || 'Request failed').trim();
    return raw.replace(/^failed to [^:]+:\s*/i, '');
}

function companyMemberEntryKey(agentID) {
    return encodeURIComponent(String(agentID || '').trim());
}

async function addCompanyMember(companyID) {
    const agentSelect = document.getElementById(`company-add-member-agent-${companyID}`);
    const roleInput = document.getElementById(`company-add-member-role-${companyID}`);
    if (!agentSelect || !roleInput) return;

    const agentID = (agentSelect.value || '').trim();
    const role = (roleInput.value || '').trim();
    if (!agentID) {
        setCompanyMembersStatus(companyID, 'Select an agent to add.', true);
        return;
    }

    setCompanyMembersStatus(companyID, 'Adding member...', false);
    try {
        await api('POST', `/api/companies/${companyID}/members`, {
            agent_id: agentID,
            role,
        });
        setCompanyMembersStatus(companyID, 'Member added.', false);
        roleInput.value = '';
        await loadCompanies();
    } catch (e) {
        setCompanyMembersStatus(companyID, companySectionErrorMessage(e, 'Failed to add member'), true);
    }
}

async function updateCompanyMemberRole(companyID, agentID) {
    const memberKey = companyMemberEntryKey(agentID);
    const roleInput = document.getElementById(`company-member-role-${companyID}-${memberKey}`);
    if (!roleInput) return;
    const role = (roleInput.value || '').trim();
    if (!role) {
        setCompanyMembersStatus(companyID, 'Role is required to update a member.', true);
        return;
    }

    setCompanyMembersStatus(companyID, `Updating role for ${agentID}...`, false);
    try {
        await api('PATCH', `/api/companies/${companyID}/members/${agentID}`, { role });
        setCompanyMembersStatus(companyID, `Role updated for ${agentID}.`, false);
        await loadCompanies();
    } catch (e) {
        setCompanyMembersStatus(companyID, companySectionErrorMessage(e, 'Failed to update member role'), true);
    }
}

async function removeCompanyMember(companyID, agentID) {
    if (!confirm(`Remove ${agentID} from this company?`)) return;
    setCompanyMembersStatus(companyID, 'Removing member...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}/members/${agentID}`);
        setCompanyMembersStatus(companyID, 'Member removed.', false);
        await loadCompanies();
    } catch (e) {
        setCompanyMembersStatus(companyID, companySectionErrorMessage(e, 'Failed to remove member'), true);
    }
}

async function setCompanyCEO(companyID) {
    const ceoSelect = document.getElementById(`company-ceo-${companyID}`);
    if (!ceoSelect) return;
    const agentID = (ceoSelect.value || '').trim();
    if (!agentID) {
        setCompanyMetaStatus(companyID, 'Select a member to assign as CEO.', true);
        return;
    }

    setCompanyMetaStatus(companyID, 'Assigning CEO...', false);
    try {
        await api('PUT', `/api/companies/${companyID}/ceo`, { agent_id: agentID });
        setCompanyMetaStatus(companyID, 'CEO updated.', false);
        await loadCompanies();
    } catch (e) {
        setCompanyMetaStatus(companyID, companySectionErrorMessage(e, 'Failed to set CEO'), true);
    }
}

function setCompanyShopifyFormOpen(companyID, open) {
    const key = String(companyID || '').trim();
    if (!key) return;
    companyShopifyPanelOpen[key] = !!open;

    const panelEl = document.getElementById(`company-shopify-panel-${key}`);
    if (panelEl) {
        panelEl.style.display = open ? 'block' : 'none';
    }

    const buttonEl = document.getElementById(`company-shopify-toggle-${key}`);
    if (buttonEl) {
        buttonEl.textContent = open ? 'Hide Shopify Form' : 'Configure Shopify';
    }
}

function toggleCompanyShopifyForm(companyID) {
    const key = String(companyID || '').trim();
    if (!key) return;
    const currentlyOpen = !!companyShopifyPanelOpen[key];
    setCompanyShopifyFormOpen(key, !currentlyOpen);
}

function getCompanyShopifyPayload(companyID) {
    const shopURLInput = document.getElementById(`company-shopify-url-${companyID}`);
    const apiVersionInput = document.getElementById(`company-shopify-version-${companyID}`);
    const clientIDInput = document.getElementById(`company-shopify-client-id-${companyID}`);
    const clientSecretInput = document.getElementById(`company-shopify-client-secret-${companyID}`);
    const enabledInput = document.getElementById(`company-shopify-enabled-${companyID}`);
    if (!shopURLInput || !apiVersionInput || !clientIDInput || !clientSecretInput || !enabledInput) {
        throw new Error('Company Shopify form is not available');
    }

    return {
        shop_url: shopURLInput.value.trim(),
        api_version: apiVersionInput.value.trim(),
        client_id: clientIDInput.value.trim(),
        client_secret: clientSecretInput.value.trim(),
        enabled: !!enabledInput.checked,
    };
}

function setCompanyShopifyStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-shopify-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

async function saveCompanyShopify(companyID) {
    let payload;
    try {
        payload = getCompanyShopifyPayload(companyID);
    } catch (e) {
        alert(e.message);
        return;
    }

    setCompanyShopifyStatus(companyID, 'Saving Shopify config...', false);
    try {
        const resp = await api('PUT', `/api/companies/${companyID}/shopify`, payload);
        const webhookURL = resp?.webhook?.public_url || '';
        const webhookWarning = resp?.webhook_warning || '';
        let status = 'Shopify config saved.';
        if (webhookURL) {
            status += ` Default webhook: ${webhookURL}`;
        }
        if (webhookWarning) {
            status += ` ${webhookWarning}`;
        }
        await loadCompanies();
        setCompanyShopifyFormOpen(companyID, true);
        setCompanyShopifyStatus(companyID, status, !!webhookWarning);
    } catch (e) {
        setCompanyShopifyStatus(companyID, companySectionErrorMessage(e, 'Failed to save Shopify config'), true);
    }
}

async function deleteCompanyShopify(companyID) {
    if (!confirm('Delete company Shopify configuration?')) return;
    setCompanyShopifyStatus(companyID, 'Deleting Shopify config...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}/shopify`);
        setCompanyShopifyStatus(companyID, 'Shopify config deleted.', false);
        await loadCompanies();
    } catch (e) {
        setCompanyShopifyStatus(companyID, companySectionErrorMessage(e, 'Failed to delete Shopify config'), true);
    }
}

async function testCompanyShopify(companyID) {
    setCompanyShopifyStatus(companyID, 'Testing Shopify connection...', false);
    try {
        await api('POST', `/api/companies/${companyID}/shopify/test`, {});
        setCompanyShopifyStatus(companyID, 'Shopify connection test passed.', false);
    } catch (e) {
        setCompanyShopifyStatus(companyID, companySectionErrorMessage(e, 'Shopify test failed'), true);
    }
}

function setCompanyKnowledgeStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-knowledge-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

function parseCompanyKnowledgeTags(raw) {
    if (!raw) return [];
    return String(raw)
        .split(',')
        .map((tag) => tag.trim())
        .filter(Boolean);
}

function companyKnowledgeEntryKey(entryID) {
    return encodeURIComponent(String(entryID || '').trim());
}

async function loadCompanyKnowledge(companyID) {
    const queryInput = document.getElementById(`company-knowledge-query-${companyID}`);
    const kindInput = document.getElementById(`company-knowledge-kind-${companyID}`);
    const limitInput = document.getElementById(`company-knowledge-limit-${companyID}`);
    if (!queryInput || !kindInput || !limitInput) return;

    const query = queryInput.value.trim();
    const kind = kindInput.value.trim();
    const limitRaw = limitInput.value.trim();
    const params = new URLSearchParams();
    if (query) params.set('query', query);
    if (kind) params.set('kind', kind);
    if (limitRaw) params.set('limit', limitRaw);

    setCompanyKnowledgeStatus(companyID, 'Loading company knowledge...', false);
    try {
        const suffix = params.toString() ? `?${params.toString()}` : '';
        const data = await api('GET', `/api/companies/${companyID}/knowledge${suffix}`);
        companyKnowledgeCache[companyID] = data.entries || [];
        renderCompanyKnowledgeList(companyID);
        setCompanyKnowledgeStatus(companyID, `Loaded ${(data.entries || []).length} knowledge entries.`, false);
    } catch (e) {
        setCompanyKnowledgeStatus(companyID, companySectionErrorMessage(e, 'Failed to load company knowledge'), true);
    }
}

function renderCompanyKnowledgeList(companyID) {
    const container = document.getElementById(`company-knowledge-list-${companyID}`);
    if (!container) return;
    const entries = companyKnowledgeCache[companyID] || [];
    if (!entries.length) {
        container.innerHTML = '<div style="color:var(--text-muted);font-size:12px;padding:8px 0">No entries loaded.</div>';
        return;
    }

    container.innerHTML = entries.map((entry) => {
        const key = companyKnowledgeEntryKey(entry.id);
        const tagsRaw = Array.isArray(entry.tags) ? entry.tags.join(', ') : '';
        return `<div class="peer-group-card" style="margin-top:8px">
            <div style="display:flex;justify-content:space-between;gap:8px;align-items:center">
                <div style="font-size:12px;color:var(--text-muted)">
                    ID: ${escHtml(entry.id || '')}
                </div>
                <button class="btn btn-sm btn-danger" onclick='deleteCompanyKnowledgeEntry(${JSON.stringify(companyID)}, ${JSON.stringify(entry.id)})'>Delete</button>
            </div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-knowledge-entry-kind-${escAttr(companyID)}-${escAttr(key)}">Kind</label>
                    <input type="text" id="company-knowledge-entry-kind-${escAttr(companyID)}-${escAttr(key)}" value="${escAttr(entry.kind || '')}">
                </div>
                <div class="form-group">
                    <label for="company-knowledge-entry-title-${escAttr(companyID)}-${escAttr(key)}">Title</label>
                    <input type="text" id="company-knowledge-entry-title-${escAttr(companyID)}-${escAttr(key)}" value="${escAttr(entry.title || '')}">
                </div>
            </div>
            <div class="form-group">
                <label for="company-knowledge-entry-content-${escAttr(companyID)}-${escAttr(key)}">Content</label>
                <textarea id="company-knowledge-entry-content-${escAttr(companyID)}-${escAttr(key)}" rows="3">${escHtml(entry.content || '')}</textarea>
            </div>
            <div class="form-row">
                <div class="form-group">
                    <label for="company-knowledge-entry-tags-${escAttr(companyID)}-${escAttr(key)}">Tags (comma-separated)</label>
                    <input type="text" id="company-knowledge-entry-tags-${escAttr(companyID)}-${escAttr(key)}" value="${escAttr(tagsRaw)}">
                </div>
                <div class="form-group" style="max-width:180px;display:flex;align-items:flex-end;justify-content:flex-end">
                    <button class="btn btn-sm btn-primary" onclick='saveCompanyKnowledgeEntry(${JSON.stringify(companyID)}, ${JSON.stringify(entry.id)})'>Save Entry</button>
                </div>
            </div>
        </div>`;
    }).join('');
}

async function addCompanyKnowledge(companyID) {
    const kindInput = document.getElementById(`company-knowledge-add-kind-${companyID}`);
    const titleInput = document.getElementById(`company-knowledge-add-title-${companyID}`);
    const contentInput = document.getElementById(`company-knowledge-add-content-${companyID}`);
    const tagsInput = document.getElementById(`company-knowledge-add-tags-${companyID}`);
    if (!kindInput || !titleInput || !contentInput || !tagsInput) return;

    const kind = kindInput.value.trim();
    const title = titleInput.value.trim();
    const content = contentInput.value.trim();
    const tags = parseCompanyKnowledgeTags(tagsInput.value.trim());
    if (!content) {
        setCompanyKnowledgeStatus(companyID, 'Content is required to create company knowledge.', true);
        return;
    }

    setCompanyKnowledgeStatus(companyID, 'Adding company knowledge...', false);
    try {
        await api('POST', `/api/companies/${companyID}/knowledge`, {
            kind,
            title,
            content,
            tags,
        });
        contentInput.value = '';
        tagsInput.value = '';
        await loadCompanyKnowledge(companyID);
        setCompanyKnowledgeStatus(companyID, 'Knowledge entry added.', false);
    } catch (e) {
        setCompanyKnowledgeStatus(companyID, companySectionErrorMessage(e, 'Failed to add company knowledge'), true);
    }
}

async function saveCompanyKnowledgeEntry(companyID, entryID) {
    const key = companyKnowledgeEntryKey(entryID);
    const kindInput = document.getElementById(`company-knowledge-entry-kind-${companyID}-${key}`);
    const titleInput = document.getElementById(`company-knowledge-entry-title-${companyID}-${key}`);
    const contentInput = document.getElementById(`company-knowledge-entry-content-${companyID}-${key}`);
    const tagsInput = document.getElementById(`company-knowledge-entry-tags-${companyID}-${key}`);
    if (!kindInput || !titleInput || !contentInput || !tagsInput) return;

    const payload = {
        kind: kindInput.value.trim(),
        title: titleInput.value.trim(),
        content: contentInput.value.trim(),
        tags: parseCompanyKnowledgeTags(tagsInput.value.trim()),
    };

    setCompanyKnowledgeStatus(companyID, 'Saving company knowledge entry...', false);
    try {
        await api('PATCH', `/api/companies/${companyID}/knowledge/${entryID}`, payload);
        await loadCompanyKnowledge(companyID);
        setCompanyKnowledgeStatus(companyID, 'Knowledge entry saved.', false);
    } catch (e) {
        setCompanyKnowledgeStatus(companyID, companySectionErrorMessage(e, 'Failed to save company knowledge entry'), true);
    }
}

async function deleteCompanyKnowledgeEntry(companyID, entryID) {
    if (!confirm('Delete this company knowledge entry?')) return;
    setCompanyKnowledgeStatus(companyID, 'Deleting company knowledge entry...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}/knowledge/${entryID}`);
        await loadCompanyKnowledge(companyID);
        setCompanyKnowledgeStatus(companyID, 'Knowledge entry deleted.', false);
    } catch (e) {
        setCompanyKnowledgeStatus(companyID, companySectionErrorMessage(e, 'Failed to delete company knowledge entry'), true);
    }
}

function getCompanyTopDawgPayload(companyID) {
    const supplierIDInput = document.getElementById(`company-topdawg-supplier-id-${companyID}`);
    const apiKeyInput = document.getElementById(`company-topdawg-api-key-${companyID}`);
    const enabledInput = document.getElementById(`company-topdawg-enabled-${companyID}`);
    if (!supplierIDInput || !apiKeyInput || !enabledInput) {
        throw new Error('Company TopDawg form is not available');
    }
    return {
        supplier_id: supplierIDInput.value.trim(),
        api_key: apiKeyInput.value.trim(),
        enabled: !!enabledInput.checked,
    };
}

function setCompanyTopDawgStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-topdawg-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

async function saveCompanyTopDawg(companyID) {
    let payload;
    try {
        payload = getCompanyTopDawgPayload(companyID);
    } catch (e) {
        alert(e.message);
        return;
    }
    setCompanyTopDawgStatus(companyID, 'Saving TopDawg config...', false);
    try {
        await api('PUT', `/api/companies/${companyID}/topdawg`, payload);
        await loadCompanies();
        setCompanyTopDawgStatus(companyID, 'TopDawg config saved.', false);
    } catch (e) {
        setCompanyTopDawgStatus(companyID, companySectionErrorMessage(e, 'Failed to save TopDawg config'), true);
    }
}

async function deleteCompanyTopDawg(companyID) {
    if (!confirm('Delete company TopDawg configuration?')) return;
    setCompanyTopDawgStatus(companyID, 'Deleting TopDawg config...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}/topdawg`);
        await loadCompanies();
        setCompanyTopDawgStatus(companyID, 'TopDawg config deleted.', false);
    } catch (e) {
        setCompanyTopDawgStatus(companyID, companySectionErrorMessage(e, 'Failed to delete TopDawg config'), true);
    }
}

async function testCompanyTopDawg(companyID) {
    setCompanyTopDawgStatus(companyID, 'Testing TopDawg connection...', false);
    try {
        await api('POST', `/api/companies/${companyID}/topdawg/test`, {});
        setCompanyTopDawgStatus(companyID, 'TopDawg connection test passed.', false);
    } catch (e) {
        setCompanyTopDawgStatus(companyID, companySectionErrorMessage(e, 'TopDawg test failed'), true);
    }
}

function getCompanyCJDropshippingPayload(companyID) {
    const apiKeyInput = document.getElementById(`company-cjd-api-key-${companyID}`);
    const defaultFromCountryInput = document.getElementById(`company-cjd-default-from-country-${companyID}`);
    const enabledInput = document.getElementById(`company-cjd-enabled-${companyID}`);
    if (!apiKeyInput || !defaultFromCountryInput || !enabledInput) {
        throw new Error('Company CJ Dropshipping form is not available');
    }

    const defaultFromCountry = defaultFromCountryInput.value.trim().toUpperCase();
    if (defaultFromCountry && defaultFromCountry.length !== 2) {
        throw new Error('Default From Country must be a 2-letter ISO country code (e.g. US, CN, GB)');
    }

    return {
        api_key: apiKeyInput.value.trim(),
        default_from_country_code: defaultFromCountry,
        enabled: !!enabledInput.checked,
    };
}

function setCompanyCJDropshippingStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-cjd-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

async function saveCompanyCJDropshipping(companyID) {
    let payload;
    try {
        payload = getCompanyCJDropshippingPayload(companyID);
    } catch (e) {
        alert(e.message);
        return;
    }
    setCompanyCJDropshippingStatus(companyID, 'Saving CJ Dropshipping config...', false);
    try {
        await api('PUT', `/api/companies/${companyID}/cjdropshipping`, payload);
        await loadCompanies();
        setCompanyCJDropshippingStatus(companyID, 'CJ Dropshipping config saved.', false);
    } catch (e) {
        setCompanyCJDropshippingStatus(companyID, companySectionErrorMessage(e, 'Failed to save CJ Dropshipping config'), true);
    }
}

async function deleteCompanyCJDropshipping(companyID) {
    if (!confirm('Delete company CJ Dropshipping configuration?')) return;
    setCompanyCJDropshippingStatus(companyID, 'Deleting CJ Dropshipping config...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}/cjdropshipping`);
        await loadCompanies();
        setCompanyCJDropshippingStatus(companyID, 'CJ Dropshipping config deleted.', false);
    } catch (e) {
        setCompanyCJDropshippingStatus(companyID, companySectionErrorMessage(e, 'Failed to delete CJ Dropshipping config'), true);
    }
}

async function testCompanyCJDropshipping(companyID) {
    setCompanyCJDropshippingStatus(companyID, 'Testing CJ Dropshipping connection...', false);
    try {
        await api('POST', `/api/companies/${companyID}/cjdropshipping/test`, {});
        setCompanyCJDropshippingStatus(companyID, 'CJ Dropshipping connection test passed.', false);
    } catch (e) {
        setCompanyCJDropshippingStatus(companyID, companySectionErrorMessage(e, 'CJ Dropshipping test failed'), true);
    }
}

function getCompanyAmazonPayload(companyID) {
    const accessKeyInput = document.getElementById(`company-amazon-access-key-${companyID}`);
    const secretKeyInput = document.getElementById(`company-amazon-secret-key-${companyID}`);
    const partnerTagInput = document.getElementById(`company-amazon-partner-tag-${companyID}`);
    const marketplaceInput = document.getElementById(`company-amazon-marketplace-${companyID}`);
    const enabledInput = document.getElementById(`company-amazon-enabled-${companyID}`);
    if (!accessKeyInput || !secretKeyInput || !partnerTagInput || !marketplaceInput || !enabledInput) {
        throw new Error('Company Amazon form is not available');
    }
    return {
        access_key: accessKeyInput.value.trim(),
        secret_key: secretKeyInput.value.trim(),
        partner_tag: partnerTagInput.value.trim(),
        marketplace: marketplaceInput.value.trim(),
        enabled: !!enabledInput.checked,
    };
}

function setCompanyAmazonStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-amazon-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

async function saveCompanyAmazon(companyID) {
    let payload;
    try {
        payload = getCompanyAmazonPayload(companyID);
    } catch (e) {
        alert(e.message);
        return;
    }
    setCompanyAmazonStatus(companyID, 'Saving Amazon config...', false);
    try {
        await api('PUT', `/api/companies/${companyID}/amazon`, payload);
        await loadCompanies();
        setCompanyAmazonStatus(companyID, 'Amazon config saved.', false);
    } catch (e) {
        setCompanyAmazonStatus(companyID, companySectionErrorMessage(e, 'Failed to save Amazon config'), true);
    }
}

async function deleteCompanyAmazon(companyID) {
    if (!confirm('Delete company Amazon configuration?')) return;
    setCompanyAmazonStatus(companyID, 'Deleting Amazon config...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}/amazon`);
        await loadCompanies();
        setCompanyAmazonStatus(companyID, 'Amazon config deleted.', false);
    } catch (e) {
        setCompanyAmazonStatus(companyID, companySectionErrorMessage(e, 'Failed to delete Amazon config'), true);
    }
}

async function testCompanyAmazon(companyID) {
    setCompanyAmazonStatus(companyID, 'Testing Amazon connection...', false);
    try {
        await api('POST', `/api/companies/${companyID}/amazon/test`, {});
        setCompanyAmazonStatus(companyID, 'Amazon connection test passed.', false);
    } catch (e) {
        setCompanyAmazonStatus(companyID, companySectionErrorMessage(e, 'Amazon test failed'), true);
    }
}

function getCompanyPolymarketPayload(companyID) {
    const proxyInput = document.getElementById(`company-poly-proxy-${companyID}`);
    const rpcInput = document.getElementById(`company-poly-rpc-${companyID}`);
    const funderInput = document.getElementById(`company-poly-funder-${companyID}`);
    const signatureInput = document.getElementById(`company-poly-sig-${companyID}`);
    const chainIDInput = document.getElementById(`company-poly-chain-${companyID}`);
    const enabledInput = document.getElementById(`company-poly-enabled-${companyID}`);
    if (!proxyInput || !rpcInput || !funderInput || !signatureInput || !chainIDInput || !enabledInput) {
        throw new Error('Company Polymarket form is not available');
    }

    const payload = {
        proxy_url: proxyInput.value.trim(),
        onchain_rpc_url: rpcInput.value.trim(),
        funder_address: funderInput.value.trim(),
        enabled: !!enabledInput.checked,
    };

    const sigRaw = signatureInput.value.trim();
    if (sigRaw !== '') {
        const signatureType = Number(sigRaw);
        if (!Number.isInteger(signatureType) || signatureType < 0 || signatureType > 2) {
            throw new Error('Signature type must be 0, 1, or 2');
        }
        payload.signature_type = signatureType;
    }

    const chainRaw = chainIDInput.value.trim();
    if (chainRaw !== '') {
        const chainID = Number(chainRaw);
        if (!Number.isInteger(chainID) || chainID < 0) {
            throw new Error('Chain ID must be a non-negative integer');
        }
        payload.chain_id = chainID;
    }

    return payload;
}

function setCompanyPolymarketStatus(companyID, message, isError) {
    const statusEl = document.getElementById(`company-poly-status-${companyID}`);
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

async function saveCompanyPolymarket(companyID) {
    let payload;
    try {
        payload = getCompanyPolymarketPayload(companyID);
    } catch (e) {
        alert(e.message);
        return;
    }

    setCompanyPolymarketStatus(companyID, 'Saving Polymarket config...', false);
    try {
        await api('PUT', `/api/companies/${companyID}/polymarket`, payload);
        setCompanyPolymarketStatus(companyID, 'Polymarket config saved.', false);
        await loadCompanies();
    } catch (e) {
        setCompanyPolymarketStatus(companyID, companySectionErrorMessage(e, 'Failed to save Polymarket config'), true);
    }
}

async function deleteCompanyPolymarket(companyID) {
    if (!confirm('Delete company Polymarket configuration?')) return;
    setCompanyPolymarketStatus(companyID, 'Deleting Polymarket config...', false);
    try {
        await api('DELETE', `/api/companies/${companyID}/polymarket`);
        setCompanyPolymarketStatus(companyID, 'Polymarket config deleted.', false);
        await loadCompanies();
    } catch (e) {
        setCompanyPolymarketStatus(companyID, companySectionErrorMessage(e, 'Failed to delete Polymarket config'), true);
    }
}

async function loadPolymarketPortfolioCompanies() {
    const data = await api('GET', '/api/companies');
    companySummaries = (data.companies || []).map((company) => ({
        id: company.id,
        name: company.name,
        ceo_agent_id: company.ceo_agent_id,
    }));
    refreshPipelineEditorCompanyOptions();
    refreshPolymarketPortfolioCompanyOptions();
    return companySummaries;
}

function refreshPolymarketPortfolioCompanyOptions() {
    const select = document.getElementById('polymarket-portfolio-company-select');
    if (!select) return;

    const current = String(polymarketPortfolioCompanyID || select.value || '').trim();
    const options = ['<option value="">Select a company...</option>'];
    for (const company of companySummaries) {
        const id = String(company?.id || '').trim();
        if (!id) continue;
        const label = company?.name ? `${company.name} (${id})` : id;
        options.push(`<option value="${escAttr(id)}">${escHtml(label)}</option>`);
    }
    select.innerHTML = options.join('');
    if (current) {
        select.value = current;
    }
}

function polymarketPortfolioPath(companyID) {
    const normalized = String(companyID || '').trim();
    if (!normalized) return '/polymarket';
    return '/polymarket/' + encodeURIComponent(normalized);
}

function setPolymarketPortfolioStatus(message, isError) {
    const statusEl = document.getElementById('polymarket-portfolio-status');
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

function renderPolymarketPortfolioEmpty(message) {
    const container = document.getElementById('polymarket-portfolio-content');
    if (!container) return;
    container.innerHTML = `<div style="color:var(--text-muted);padding:16px 0">${escHtml(message || 'Select a company to load positions and open orders.')}</div>`;
}

async function showPolymarketPortfolio(initialCompanyID) {
    document.getElementById('polymarket-portfolio-modal').style.display = 'flex';
    setPolymarketPortfolioStatus('Loading companies...', false);
    try {
        const summaries = await loadPolymarketPortfolioCompanies();
        const requestedID = String(initialCompanyID || polymarketPortfolioCompanyID || '').trim();
        const hasRequested = summaries.some((company) => String(company?.id || '').trim() === requestedID);
        if (hasRequested) {
            polymarketPortfolioCompanyID = requestedID;
        } else if (summaries.length === 1) {
            polymarketPortfolioCompanyID = String(summaries[0]?.id || '').trim();
        } else if (!summaries.some((company) => String(company?.id || '').trim() === String(polymarketPortfolioCompanyID || '').trim())) {
            polymarketPortfolioCompanyID = '';
        }
        refreshPolymarketPortfolioCompanyOptions();
        if (!polymarketPortfolioCompanyID) {
            polymarketPortfolioSnapshot = null;
            renderPolymarketPortfolioEmpty('Select a company to load positions and open orders.');
            setPolymarketPortfolioStatus('Select a company to begin.', false);
            history.pushState(null, '', '/polymarket');
            return;
        }
        history.pushState(null, '', polymarketPortfolioPath(polymarketPortfolioCompanyID));
        await loadPolymarketPortfolio();
    } catch (e) {
        polymarketPortfolioSnapshot = null;
        renderPolymarketPortfolioEmpty('Failed to load company list.');
        setPolymarketPortfolioStatus(companySectionErrorMessage(e, 'Failed to load companies'), true);
    }
}

function closePolymarketPortfolio() {
    closePolymarketNotes();
    document.getElementById('polymarket-portfolio-modal').style.display = 'none';
    history.pushState(null, '', '/');
}

function setPolymarketNotesStatus(message, isError) {
    const statusEl = document.getElementById('polymarket-notes-status');
    if (!statusEl) return;
    statusEl.textContent = message || '';
    statusEl.style.color = isError ? 'var(--red)' : 'var(--text-muted)';
}

function closePolymarketNotes() {
    const modal = document.getElementById('polymarket-notes-modal');
    if (!modal) return;
    modal.style.display = 'none';
}

async function openPolymarketNotes(conditionID, marketTitle) {
    const companyID = String(polymarketPortfolioCompanyID || '').trim();
    const normalizedConditionID = String(conditionID || '').trim();
    if (!companyID || !normalizedConditionID) return;

    polymarketNotesState = {
        companyID,
        conditionID: normalizedConditionID,
        title: String(marketTitle || normalizedConditionID).trim(),
    };

    const modal = document.getElementById('polymarket-notes-modal');
    const titleEl = document.getElementById('polymarket-notes-title');
    const subtitleEl = document.getElementById('polymarket-notes-subtitle');
    const listEl = document.getElementById('polymarket-notes-list');
    if (!modal || !titleEl || !subtitleEl || !listEl) return;

    titleEl.textContent = polymarketNotesState.title || 'Market Notes';
    subtitleEl.textContent = normalizedConditionID;
    listEl.innerHTML = '<div style="color:var(--text-muted);padding:12px 0">Loading notes...</div>';
    setPolymarketNotesStatus('Loading notes...', false);
    modal.style.display = 'flex';

    try {
        const resp = await api('GET', `/api/companies/${companyID}/polymarket/notes?condition_id=${encodeURIComponent(normalizedConditionID)}&limit=100`);
        const notes = Array.isArray(resp.notes) ? resp.notes : [];
        if (!notes.length) {
            listEl.innerHTML = '<div style="color:var(--text-muted);padding:12px 0">No notes for this market.</div>';
            setPolymarketNotesStatus('0 notes.', false);
            return;
        }

        listEl.innerHTML = notes.map((note) => {
            const created = note.created_at ? formatDate(note.created_at) : '';
            const creator = String(note.created_by_agent_id || '').trim();
            return `<div class="peer-group-card" style="margin:0">
                <div style="display:flex;justify-content:space-between;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:8px">
                    <div style="font-size:12px;color:var(--text-muted)">${escHtml(created || 'Unknown time')}</div>
                    <div style="font-size:12px;color:var(--text-muted)">${escHtml(creator || 'unknown author')}</div>
                </div>
                <div style="white-space:pre-wrap;line-height:1.45">${escHtml(String(note.content || '').trim())}</div>
            </div>`;
        }).join('');
        const count = Number(resp.count || notes.length || 0);
        if (count > notes.length) {
            setPolymarketNotesStatus(`Showing ${notes.length} of ${count} notes. Scroll to read.`, false);
        } else {
            setPolymarketNotesStatus(`${count} note${count === 1 ? '' : 's'}. Scroll to read.`, false);
        }
    } catch (e) {
        listEl.innerHTML = '<div style="color:var(--red);padding:12px 0">Failed to load notes.</div>';
        setPolymarketNotesStatus(companySectionErrorMessage(e, 'Failed to load notes'), true);
    }
}

async function onPolymarketPortfolioCompanyChange() {
    const select = document.getElementById('polymarket-portfolio-company-select');
    polymarketPortfolioCompanyID = String(select?.value || '').trim();
    polymarketPortfolioSnapshot = null;
    closePolymarketNotes();
    if (!polymarketPortfolioCompanyID) {
        renderPolymarketPortfolioEmpty('Select a company to load positions and open orders.');
        setPolymarketPortfolioStatus('Select a company to begin.', false);
        history.pushState(null, '', '/polymarket');
        return;
    }
    history.pushState(null, '', polymarketPortfolioPath(polymarketPortfolioCompanyID));
    await loadPolymarketPortfolio();
}

function getPolymarketPortfolioMarket(conditionID) {
    const markets = (polymarketPortfolioSnapshot && typeof polymarketPortfolioSnapshot.markets === 'object' && polymarketPortfolioSnapshot.markets) ? polymarketPortfolioSnapshot.markets : {};
    return markets[String(conditionID || '').trim()] || null;
}

function renderPolymarketPortfolioNote(noteCount) {
    const count = Number(noteCount || 0);
    return `${count} note${count === 1 ? '' : 's'}`;
}

function renderPolymarketPortfolioNoteButton(conditionID, marketTitle, noteCount) {
    const normalizedConditionID = String(conditionID || '').trim();
    const label = escHtml(renderPolymarketPortfolioNote(noteCount));
    if (!normalizedConditionID) {
        return `<span>${label}</span>`;
    }
    return `<button type="button" data-condition-id="${escAttr(normalizedConditionID)}" data-market-title="${escAttr(String(marketTitle || '').trim())}" onclick="openPolymarketNotes(this.dataset.conditionId, this.dataset.marketTitle)" style="padding:0;border:none;background:none;color:var(--accent);cursor:pointer;font:inherit;text-decoration:underline;text-underline-offset:2px">${label}</button>`;
}

function getPolymarketPortfolioMarketImage(market) {
    if (!market || typeof market !== 'object') return '';
    const image = String(market.image || '').trim();
    if (image) return image;
    return String(market.icon || '').trim();
}

function renderPolymarketPortfolio() {
    const container = document.getElementById('polymarket-portfolio-content');
    if (!container) return;
    if (!polymarketPortfolioSnapshot) {
        renderPolymarketPortfolioEmpty('Select a company to load positions and open orders.');
        return;
    }

    const snapshot = polymarketPortfolioSnapshot;
    const summary = (snapshot && typeof snapshot.summary === 'object' && snapshot.summary) ? snapshot.summary : {};
    const positions = Array.isArray(snapshot.positions) ? snapshot.positions : [];
    const orders = Array.isArray(snapshot.orders) ? snapshot.orders : [];
    const errors = (snapshot && typeof snapshot.errors === 'object' && snapshot.errors) ? snapshot.errors : {};
    const company = companySummaries.find((item) => String(item?.id || '').trim() === String(polymarketPortfolioCompanyID || '').trim());
    const companyLabel = company?.name || polymarketPortfolioCompanyID || 'Company';
    const errorEntries = Object.entries(errors).filter(([, message]) => String(message || '').trim());

    const summaryCards = [
        {
            label: 'Total Assets',
            value: formatUSD(summary.total_assets),
            meta: 'USD assets + Polymarket value',
        },
        {
            label: 'USD Assets',
            value: formatUSD(summary.usd_assets),
            meta: `USDC.e ${formatUSD(summary.usdce_assets)} · USDT.e ${formatUSD(summary.usdte_assets)}`,
        },
        {
            label: 'Polymarket Assets',
            value: formatUSD(summary.polymarket_assets),
            meta: `${formatDisplayNumber(summary.positions_count, 0)} position${Number(summary.positions_count || 0) === 1 ? '' : 's'} · ${formatDisplayNumber(summary.open_orders_count, 0)} open order${Number(summary.open_orders_count || 0) === 1 ? '' : 's'}`,
        },
    ];

    const errorsMarkup = errorEntries.length
        ? `<div class="peer-group-card" style="margin-bottom:12px;border-color:rgba(239,68,68,0.35)">
            <div style="font-size:12px;color:var(--red);text-transform:uppercase;margin-bottom:8px">Warnings</div>
            ${errorEntries.map(([section, message]) => `<div style="font-size:12px;color:var(--text-muted);margin-bottom:6px"><strong>${escHtml(section)}</strong>: ${escHtml(String(message || '').trim())}</div>`).join('')}
        </div>`
        : '';

    const positionsRows = positions.length
        ? positions.map((position) => {
            const conditionID = String(position.condition_id || '').trim();
            const asset = String(position.asset || '').trim();
            const outcome = String(position.outcome || '').trim();
            const market = getPolymarketPortfolioMarket(conditionID) || {};
            const marketImage = getPolymarketPortfolioMarketImage(market);
            const title = String(position.market_question || position.title || market.question || conditionID || '(unknown market)').trim();
            const slug = String(position.slug || market.slug || '').trim();
            const relatedOrders = orders.filter((order) => String(order?.market || '').trim() === conditionID);
            const openOrdersText = `${relatedOrders.length} open order${relatedOrders.length === 1 ? '' : 's'}`;
            return `<tr>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">
                    <div style="display:flex;gap:12px;align-items:flex-start">
                        ${marketImage ? `<img src="${escAttr(marketImage)}" alt="" style="width:54px;height:54px;border-radius:12px;object-fit:cover;flex:0 0 auto;background:rgba(255,255,255,0.06)" onerror="this.style.display='none'">` : ''}
                        <div style="min-width:0">
                            <div style="font-weight:600;font-size:16px;line-height:1.2">${escHtml(title)}</div>
                            <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-top:8px">
                                <span style="display:inline-flex;align-items:center;padding:3px 8px;border-radius:999px;background:rgba(239,68,68,0.12);color:var(--red);font-size:12px">${escHtml(outcome || asset || conditionID)}</span>
                                <span style="font-size:12px;color:var(--text-muted)">${escHtml(formatShareAmount(position.size))} shares</span>
                                <span style="font-size:12px;color:var(--text-muted)">${escHtml(openOrdersText)}</span>
                            </div>
                            ${slug ? `<div style="font-size:11px;color:var(--text-muted);margin-top:6px">${escHtml(slug)}</div>` : ''}
                        </div>
                    </div>
                </td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(formatShareAmount(position.size))}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(formatShareAmount(position.avg_price))}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(formatShareAmount(position.cur_price))}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(formatUSD(position.current_value))}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border);color:${Number(position.cash_pnl || 0) >= 0 ? 'var(--green)' : 'var(--red)'}">
                    ${escHtml(formatUSD(position.cash_pnl))}
                </td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${renderPolymarketPortfolioNoteButton(conditionID, title, position.note_count)}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">
                    <button class="btn btn-sm btn-danger" onclick='exitPolymarketPortfolioPosition(${JSON.stringify(conditionID)}, ${JSON.stringify(asset)}, ${JSON.stringify(outcome)})'>Sell All + Cancel Orders</button>
                    <div style="font-size:11px;color:var(--text-muted);margin-top:6px">Cancels every open order for this market, then sells the full held position at the best bid.</div>
                </td>
            </tr>`;
        }).join('')
        : '<tr><td colspan="8" style="padding:16px;color:var(--text-muted);text-align:center">No Polymarket positions.</td></tr>';

    const ordersRows = orders.length
        ? orders.map((order) => {
            const market = getPolymarketPortfolioMarket(order.market) || {};
            const conditionID = String(order.market || '').trim();
            const title = String(order.market_question || market.question || conditionID || '(unknown market)').trim();
            return `<tr>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">
                    <div style="font-weight:600">${escHtml(title)}</div>
                    <div style="font-size:11px;color:var(--text-muted);margin-top:4px">${escHtml(String(order.outcome || order.asset_id || '').trim())}</div>
                    <div style="font-size:11px;color:var(--text-muted);margin-top:4px">${escHtml(String(order.id || '').trim())}</div>
                </td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(String(order.side || '').trim())}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(formatShareAmount(order.price))}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(formatShareAmount(order.remaining_size))}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${escHtml(formatPolymarketTimestamp(order.created_at))}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">${renderPolymarketPortfolioNoteButton(conditionID, title, order.note_count)}</td>
                <td style="padding:10px;vertical-align:top;border-top:1px solid var(--border)">
                    <button class="btn btn-sm btn-danger" onclick='cancelPolymarketPortfolioOrder(${JSON.stringify(String(order.id || '').trim())})'>Cancel</button>
                </td>
            </tr>`;
        }).join('')
        : '<tr><td colspan="7" style="padding:16px;color:var(--text-muted);text-align:center">No open Polymarket orders.</td></tr>';

    container.innerHTML = `
        <div style="margin-bottom:12px">
            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin-bottom:6px">${escHtml(companyLabel)}</div>
            <div style="font-size:20px;font-weight:600">${escHtml(companyLabel)} Portfolio</div>
        </div>
        ${errorsMarkup}
        <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px;margin-bottom:16px">
            ${summaryCards.map((card) => `<div class="peer-group-card" style="margin:0">
                <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase">${escHtml(card.label)}</div>
                <div style="font-size:24px;font-weight:700;margin-top:6px">${escHtml(card.value)}</div>
                <div style="font-size:12px;color:var(--text-muted);margin-top:6px">${escHtml(card.meta)}</div>
            </div>`).join('')}
        </div>
        <div class="peer-group-card" style="margin-bottom:12px">
            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin-bottom:8px">Positions</div>
            <div style="overflow-x:auto">
                <table style="width:100%;border-collapse:collapse;font-size:12px">
                    <thead>
                        <tr>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Market</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Shares</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Avg</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Current</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Value</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">PnL</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Notes</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Sell</th>
                        </tr>
                    </thead>
                    <tbody>${positionsRows}</tbody>
                </table>
            </div>
        </div>
        <div class="peer-group-card" style="margin-bottom:0">
            <div style="font-size:12px;color:var(--text-muted);text-transform:uppercase;margin-bottom:8px">Open Orders</div>
            <div style="overflow-x:auto">
                <table style="width:100%;border-collapse:collapse;font-size:12px">
                    <thead>
                        <tr>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Market</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Side</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Price</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Remaining</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Created</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Notes</th>
                            <th style="text-align:left;padding:8px 10px;color:var(--text-muted);font-weight:600">Action</th>
                        </tr>
                    </thead>
                    <tbody>${ordersRows}</tbody>
                </table>
            </div>
        </div>
    `;
}

async function loadPolymarketPortfolio() {
    const companyID = String(polymarketPortfolioCompanyID || '').trim();
    if (!companyID) {
        renderPolymarketPortfolioEmpty('Select a company to load positions and open orders.');
        setPolymarketPortfolioStatus('Select a company to begin.', false);
        return;
    }

    const container = document.getElementById('polymarket-portfolio-content');
    if (container) {
        container.innerHTML = '<div style="color:var(--text-muted);padding:16px 0">Loading Polymarket portfolio...</div>';
    }
    setPolymarketPortfolioStatus('Loading Polymarket portfolio...', false);
    try {
        polymarketPortfolioSnapshot = await api('GET', `/api/companies/${companyID}/polymarket/portfolio`);
        renderPolymarketPortfolio();
        const positionsCount = Number(polymarketPortfolioSnapshot?.summary?.positions_count || 0);
        const ordersCount = Number(polymarketPortfolioSnapshot?.summary?.open_orders_count || 0);
        setPolymarketPortfolioStatus(`Loaded ${positionsCount} position${positionsCount === 1 ? '' : 's'} and ${ordersCount} open order${ordersCount === 1 ? '' : 's'}.`, false);
    } catch (e) {
        polymarketPortfolioSnapshot = null;
        renderPolymarketPortfolioEmpty('Failed to load Polymarket portfolio.');
        setPolymarketPortfolioStatus(companySectionErrorMessage(e, 'Failed to load Polymarket portfolio'), true);
    }
}

async function exitPolymarketPortfolioPosition(conditionID, asset, outcome) {
    const companyID = String(polymarketPortfolioCompanyID || '').trim();
    if (!companyID) return;

    const label = String(outcome || asset || conditionID || 'position').trim();
    if (!confirm(`Sell all ${label} shares and cancel all open orders for this market?`)) return;

    setPolymarketPortfolioStatus(`Exiting ${label} position...`, false);
    try {
        const resp = await api('POST', `/api/companies/${companyID}/polymarket/exit`, {
            condition_id: String(conditionID || '').trim(),
            asset: String(asset || '').trim(),
            outcome: String(outcome || '').trim(),
        });
        await loadPolymarketPortfolio();
        let status = `Exited ${label}: sold ${formatShareAmount(resp?.sold_size)} shares and cancelled ${Number(resp?.cancelled_orders || 0)} order${Number(resp?.cancelled_orders || 0) === 1 ? '' : 's'}.`;
        if (resp?.market_note_error) {
            status += ` Market note warning: ${resp.market_note_error}`;
        } else if (resp?.market_note) {
            status += ' Market note added.';
        }
        setPolymarketPortfolioStatus(status, !!resp?.market_note_error);
    } catch (e) {
        setPolymarketPortfolioStatus(companySectionErrorMessage(e, 'Failed to exit position'), true);
    }
}

async function cancelPolymarketPortfolioOrder(orderID) {
    const companyID = String(polymarketPortfolioCompanyID || '').trim();
    const normalizedOrderID = String(orderID || '').trim();
    if (!companyID || !normalizedOrderID) return;
    if (!confirm(`Cancel order ${normalizedOrderID}?`)) return;

    setPolymarketPortfolioStatus(`Cancelling order ${normalizedOrderID}...`, false);
    try {
        const resp = await api('POST', `/api/companies/${companyID}/polymarket/cancel`, {
            order_id: normalizedOrderID,
        });
        await loadPolymarketPortfolio();
        let status = `Order ${normalizedOrderID} cancelled.`;
        if (resp?.market_note_error) {
            status += ` Market note warning: ${resp.market_note_error}`;
        } else if (resp?.market_note) {
            status += ' Market note added.';
        }
        setPolymarketPortfolioStatus(status, !!resp?.market_note_error);
    } catch (e) {
        setPolymarketPortfolioStatus(companySectionErrorMessage(e, 'Failed to cancel order'), true);
    }
}

