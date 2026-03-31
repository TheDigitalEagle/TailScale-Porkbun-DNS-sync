const entriesEl = document.querySelector('#entries');
const inventoryEl = document.querySelector('#inventory');
const changesEl = document.querySelector('#changes');
const statusEl = document.querySelector('#sync-status');

const nameEl = document.querySelector('#name');
const publicEl = document.querySelector('#public');
const localEl = document.querySelector('#local');
const httpEl = document.querySelector('#http');

document.querySelector('#preview').addEventListener('click', async () => {
  const entry = readForm();
  const data = await request('/entries/preview', { method: 'POST', body: JSON.stringify(entry) });
  renderChanges(data.changes || []);
});

document.querySelector('#save').addEventListener('click', async () => {
  const entry = readForm();
  await request(`/entries/${encodeURIComponent(entry.name)}`, { method: 'PUT', body: JSON.stringify(entry) });
  await refresh();
});

document.querySelector('#apply').addEventListener('click', async () => {
  const entry = readForm();
  const data = await request('/entries/apply', { method: 'POST', body: JSON.stringify(entry) });
  renderChanges(data.changes || []);
  await refresh();
});

document.querySelector('#delete').addEventListener('click', async () => {
  const name = nameEl.value.trim();
  if (!name) return;
  const data = await request(`/entries/${encodeURIComponent(name)}`, { method: 'DELETE' });
  renderChanges(data.changes || []);
  clearForm();
  await refresh();
});

document.querySelector('#apply-all').addEventListener('click', async () => {
  const data = await request('/apply', { method: 'POST' });
  renderChanges(data.changes || []);
  await refresh();
});

async function refresh() {
  const [entries, inventory, syncStatus] = await Promise.all([
    request('/entries'),
    request('/records'),
    request('/sync/status'),
  ]);

  renderEntries(entries.entries || []);
  renderInventory(inventory.records || []);
  statusEl.textContent = JSON.stringify(syncStatus, null, 2);
}

function renderEntries(entries) {
  entriesEl.innerHTML = '';
  for (const entry of entries) {
    const card = document.createElement('button');
    card.className = 'card';
    card.innerHTML = `<strong>${entry.name}</strong><div class="meta">${summary(entry)}</div>`;
    card.addEventListener('click', () => fillForm(entry));
    entriesEl.append(card);
  }
}

function renderInventory(records) {
  inventoryEl.innerHTML = '';
  for (const record of records) {
    const card = document.createElement('div');
    card.className = 'card';
    card.innerHTML = `
      <strong>${record.fqdn}</strong>
      <div class="meta">${record.scope} · ${record.type} · ${record.status}</div>
      <div class="meta">desired: ${(record.desired_values || []).join(', ') || 'none'}</div>
      <div class="meta">observed: ${(record.observed_values || []).join(', ') || 'none'}</div>
    `;
    inventoryEl.append(card);
  }
}

function renderChanges(changes) {
  changesEl.innerHTML = '';
  for (const change of changes) {
    const card = document.createElement('div');
    card.className = 'card';
    card.innerHTML = `
      <strong>${change.action.toUpperCase()} ${change.name}</strong>
      <div class="meta">${change.target} · ${change.scope} · ${change.type || 'n/a'}</div>
      <div class="meta">before: ${change.before || 'none'}</div>
      <div class="meta">after: ${change.after || 'none'}</div>
    `;
    changesEl.append(card);
  }
}

function summary(entry) {
  const parts = [];
  if ((entry.public || []).length) parts.push(`public ${entry.public.length}`);
  if ((entry.local || []).length) parts.push(`local ${entry.local.length}`);
  if (entry.http?.enabled) parts.push(`http ${entry.http.upstream}`);
  return parts.join(' · ');
}

function fillForm(entry) {
  nameEl.value = entry.name || '';
  publicEl.value = JSON.stringify(entry.public || [], null, 2);
  localEl.value = JSON.stringify(entry.local || [], null, 2);
  httpEl.value = entry.http ? JSON.stringify(entry.http, null, 2) : '';
}

function clearForm() {
  nameEl.value = '';
  publicEl.value = '';
  localEl.value = '';
  httpEl.value = '';
}

function readForm() {
  return {
    name: nameEl.value.trim(),
    public: parseJSON(publicEl.value, []),
    local: parseJSON(localEl.value, []),
    http: parseJSON(httpEl.value, null),
  };
}

function parseJSON(text, fallback) {
  if (!text.trim()) return fallback;
  return JSON.parse(text);
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

refresh().catch((error) => {
  statusEl.textContent = error.message;
});
