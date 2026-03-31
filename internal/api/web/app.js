const state = {
  entries: [],
  records: [],
  changes: [],
  syncStatus: null,
  selectedName: '',
  scopeFilter: 'all',
  search: '',
  toastTimer: null,
};

const entriesEl = document.querySelector('#entries');
const entriesEmptyEl = document.querySelector('#entries-empty');
const inventoryEl = document.querySelector('#inventory');
const inventoryEmptyEl = document.querySelector('#inventory-empty');
const inventoryCountEl = document.querySelector('#inventory-count');
const changesEl = document.querySelector('#changes');
const changesEmptyEl = document.querySelector('#changes-empty');
const statusEl = document.querySelector('#sync-status');
const formErrorEl = document.querySelector('#form-error');
const toastEl = document.querySelector('#toast');

const statEntriesEl = document.querySelector('#stat-entries');
const statRecordsEl = document.querySelector('#stat-records');
const statDriftEl = document.querySelector('#stat-drift');

const nameEl = document.querySelector('#name');
const publicEl = document.querySelector('#public');
const localEl = document.querySelector('#local');
const httpEl = document.querySelector('#http');
const searchEl = document.querySelector('#inventory-search');

document.querySelector('#preview').addEventListener('click', async () => {
  await withAction(async () => {
    const entry = readForm();
    const data = await request('/entries/preview', { method: 'POST', body: JSON.stringify(entry) });
    state.changes = data.changes || [];
    renderChanges();
    showToast(`Previewed ${state.changes.length} change${state.changes.length === 1 ? '' : 's'}.`);
  });
});

document.querySelector('#save').addEventListener('click', async () => {
  await withAction(async () => {
    const entry = readForm();
    await request(`/entries/${encodeURIComponent(entry.name)}`, { method: 'PUT', body: JSON.stringify(entry) });
    state.selectedName = entry.name;
    showToast(`Saved ${entry.name}.`);
    await refresh();
  });
});

document.querySelector('#apply').addEventListener('click', async () => {
  await withAction(async () => {
    const entry = readForm();
    const data = await request('/entries/apply', { method: 'POST', body: JSON.stringify(entry) });
    state.changes = data.changes || [];
    renderChanges();
    state.selectedName = entry.name;
    showToast(`Applied ${entry.name}.`);
    await refresh();
  });
});

document.querySelector('#delete').addEventListener('click', async () => {
  await withAction(async () => {
    const name = nameEl.value.trim();
    if (!name) {
      throw new Error('Enter a hostname before deleting.');
    }
    const data = await request(`/entries/${encodeURIComponent(name)}`, { method: 'DELETE' });
    state.changes = data.changes || [];
    renderChanges();
    clearForm();
    state.selectedName = '';
    showToast(`Deleted ${name}.`);
    await refresh();
  });
});

document.querySelector('#apply-all').addEventListener('click', async () => {
  await withAction(async () => {
    const data = await request('/apply', { method: 'POST' });
    state.changes = data.changes || [];
    renderChanges();
    showToast(`Applied ${state.changes.length} change${state.changes.length === 1 ? '' : 's'} from saved entries.`);
    await refresh();
  });
});

document.querySelector('#new-entry').addEventListener('click', () => {
  state.selectedName = '';
  clearForm();
  renderEntries();
  showToast('Editor cleared for a new entry.');
});

document.querySelector('#scope-filter').addEventListener('click', (event) => {
  const button = event.target.closest('button[data-scope]');
  if (!button) {
    return;
  }
  state.scopeFilter = button.dataset.scope;
  renderScopeFilter();
  renderInventory();
});

searchEl.addEventListener('input', () => {
  state.search = searchEl.value.trim().toLowerCase();
  renderInventory();
});

async function refresh() {
  const [entries, inventory, syncStatus] = await Promise.all([
    request('/entries'),
    request('/records'),
    request('/sync/status'),
  ]);

  state.entries = entries.entries || [];
  state.records = inventory.records || [];
  state.syncStatus = syncStatus;

  if (state.selectedName && !state.entries.some((entry) => entry.name === state.selectedName)) {
    state.selectedName = '';
  }

  renderStats();
  renderEntries();
  renderInventory();
  renderChanges();
  renderSyncStatus();
}

function renderStats() {
  statEntriesEl.textContent = String(state.entries.length);
  statRecordsEl.textContent = String(state.records.length);
  statDriftEl.textContent = String(state.records.filter((record) => record.status === 'drifted').length);
}

function renderEntries() {
  entriesEl.innerHTML = '';
  entriesEmptyEl.classList.toggle('hidden', state.entries.length > 0);

  for (const entry of state.entries) {
    const card = document.createElement('button');
    const selected = entry.name === state.selectedName;
    card.className = `entry-card${selected ? ' is-selected' : ''}`;
    card.innerHTML = `
      <div class="card-topline">
        <p class="card-title">${escapeHTML(entry.name)}</p>
        <span class="badge ${entry.http?.enabled ? 'ingress' : 'local'}">${entry.http?.enabled ? 'route' : 'entry'}</span>
      </div>
      <div class="meta-row">${escapeHTML(summary(entry) || 'No providers configured yet')}</div>
    `;
    card.addEventListener('click', () => {
      state.selectedName = entry.name;
      fillForm(entry);
      renderEntries();
    });
    entriesEl.append(card);
  }
}

function renderInventory() {
  const filtered = filteredRecords();
  inventoryEl.innerHTML = '';
  inventoryCountEl.textContent = `${filtered.length} record${filtered.length === 1 ? '' : 's'}`;
  inventoryEmptyEl.classList.toggle('hidden', filtered.length > 0);

  for (const record of filtered) {
    const card = document.createElement('article');
    card.className = 'inventory-card';
    card.innerHTML = `
      <div class="card-topline">
        <p class="card-title">${escapeHTML(record.fqdn || record.name || 'unknown')}</p>
        <span class="badge ${badgeClass(record)}">${escapeHTML(record.status || record.scope || 'unknown')}</span>
      </div>
      <div class="meta-row"><strong>${escapeHTML(record.scope || 'unknown')}</strong> · ${escapeHTML(record.type || 'n/a')} · owner ${escapeHTML(record.owner || 'n/a')}</div>
      <div class="value-row"><strong>Desired:</strong> ${escapeHTML((record.desired_values || []).join(', ') || 'none')}</div>
      <div class="value-row"><strong>Observed:</strong> ${escapeHTML((record.observed_values || []).join(', ') || 'none')}</div>
    `;
    inventoryEl.append(card);
  }
}

function renderChanges() {
  changesEl.innerHTML = '';
  changesEmptyEl.classList.toggle('hidden', state.changes.length > 0);

  for (const change of state.changes) {
    const card = document.createElement('article');
    card.className = 'change-card';
    card.innerHTML = `
      <div class="card-topline">
        <p class="card-title">${escapeHTML(`${change.action?.toUpperCase() || 'CHANGE'} ${change.name || 'record'}`)}</p>
        <span class="badge ${changeBadgeClass(change)}">${escapeHTML(change.target || 'provider')}</span>
      </div>
      <div class="change-grid">
        <div class="meta-row">${escapeHTML(change.scope || 'n/a')} · ${escapeHTML(change.type || 'n/a')}</div>
        <div class="value-row"><strong>Before:</strong> ${escapeHTML(change.before || 'none')}</div>
        <div class="value-row"><strong>After:</strong> ${escapeHTML(change.after || 'none')}</div>
      </div>
    `;
    changesEl.append(card);
  }
}

function renderSyncStatus() {
  statusEl.textContent = JSON.stringify(state.syncStatus, null, 2);
}

function renderScopeFilter() {
  for (const button of document.querySelectorAll('#scope-filter button[data-scope]')) {
    button.classList.toggle('is-active', button.dataset.scope === state.scopeFilter);
  }
}

function filteredRecords() {
  return state.records.filter((record) => {
    if (state.scopeFilter === 'drifted' && record.status !== 'drifted') {
      return false;
    }
    if (state.scopeFilter !== 'all' && state.scopeFilter !== 'drifted' && record.scope !== state.scopeFilter) {
      return false;
    }

    if (!state.search) {
      return true;
    }

    const haystack = [
      record.fqdn,
      record.name,
      record.scope,
      record.type,
      record.status,
      ...(record.desired_values || []),
      ...(record.observed_values || []),
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();

    return haystack.includes(state.search);
  });
}

function summary(entry) {
  const parts = [];
  if ((entry.public || []).length) {
    parts.push(`public ${entry.public.length}`);
  }
  if ((entry.local || []).length) {
    parts.push(`local ${entry.local.length}`);
  }
  if (entry.http?.enabled) {
    parts.push(`route ${entry.http.upstream || 'enabled'}`);
  }
  return parts.join(' · ');
}

function fillForm(entry) {
  nameEl.value = entry.name || '';
  publicEl.value = entry.public ? JSON.stringify(entry.public, null, 2) : '';
  localEl.value = entry.local ? JSON.stringify(entry.local, null, 2) : '';
  httpEl.value = entry.http ? JSON.stringify(entry.http, null, 2) : '';
  clearFormError();
}

function clearForm() {
  nameEl.value = '';
  publicEl.value = '';
  localEl.value = '';
  httpEl.value = '';
  clearFormError();
}

function readForm() {
  const name = nameEl.value.trim();
  if (!name) {
    throw new Error('Hostname is required.');
  }

  return {
    name,
    public: parseJSON(publicEl.value, []),
    local: parseJSON(localEl.value, []),
    http: parseJSON(httpEl.value, null),
  };
}

function parseJSON(text, fallback) {
  if (!text.trim()) {
    return fallback;
  }
  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(`Invalid JSON: ${error.message}`);
  }
}

async function request(path, options = {}) {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || response.statusText);
  }
  return data;
}

async function withAction(action) {
  clearFormError();
  try {
    await action();
  } catch (error) {
    showFormError(error.message);
  }
}

function showFormError(message) {
  formErrorEl.textContent = message;
  formErrorEl.classList.remove('hidden');
}

function clearFormError() {
  formErrorEl.textContent = '';
  formErrorEl.classList.add('hidden');
}

function showToast(message) {
  toastEl.textContent = message;
  toastEl.classList.remove('hidden');
  clearTimeout(state.toastTimer);
  state.toastTimer = window.setTimeout(() => {
    toastEl.classList.add('hidden');
  }, 2800);
}

function badgeClass(record) {
  if (record.status === 'drifted') {
    return 'drifted';
  }
  if (record.scope === 'ingress') {
    return 'ingress';
  }
  if (record.scope === 'local') {
    return 'local';
  }
  return 'public';
}

function changeBadgeClass(change) {
  if (change.target === 'caddy') {
    return 'ingress';
  }
  if (change.target === 'pihole') {
    return 'local';
  }
  if (change.action === 'delete') {
    return 'drifted';
  }
  return 'public';
}

function escapeHTML(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

renderScopeFilter();
refresh().catch((error) => {
  showFormError(error.message);
  statusEl.textContent = error.message;
});
