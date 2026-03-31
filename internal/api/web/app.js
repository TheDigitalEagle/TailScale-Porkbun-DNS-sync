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
const aquariumEl = document.querySelector('#aquarium');

const aquarium = {
  canvas: aquariumEl,
  ctx: aquariumEl.getContext('2d'),
  fish: [],
  width: 0,
  height: 0,
  animationFrame: null,
  phase: 0,
};

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
    await refreshEntries();
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
    await refreshAll();
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
    await refreshAll();
  });
});

document.querySelector('#apply-all').addEventListener('click', async () => {
  await withAction(async () => {
    const data = await request('/apply', { method: 'POST' });
    state.changes = data.changes || [];
    renderChanges();
    showToast(`Applied ${state.changes.length} change${state.changes.length === 1 ? '' : 's'} from saved entries.`);
    await refreshAll();
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

async function refreshEntries() {
  const entries = await request('/entries');
  state.entries = entries.entries || [];

  if (state.selectedName && !state.entries.some((entry) => entry.name === state.selectedName)) {
    state.selectedName = '';
  }

  renderEntries();
  renderStats();
}

async function refreshInventory() {
  const inventory = await request('/records');
  state.records = inventory.records || [];
  renderInventory();
  renderStats();
  syncAquarium();
}

async function refreshSyncStatus() {
  state.syncStatus = await request('/sync/status');
  renderSyncStatus();
}

async function refreshAll() {
  await Promise.all([refreshEntries(), refreshInventory(), refreshSyncStatus()]);
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

function syncAquarium() {
  const records = state.records.slice(0, 36);
  const nextFish = records.map((record, index) => {
    const existing = aquarium.fish.find((fish) => fish.id === fishId(record, index));
    return existing || createFish(record, index);
  });

  aquarium.fish = nextFish;
  resizeAquarium();
  if (!aquarium.animationFrame) {
    aquarium.phase = performance.now();
    aquarium.animationFrame = requestAnimationFrame(drawAquarium);
  }
}

function fishId(record, index) {
  return `${record.fqdn || record.name || 'record'}:${record.scope || 'unknown'}:${record.type || 'n/a'}:${index}`;
}

function createFish(record, index) {
  const hash = hashString(fishId(record, index));
  const palette = [
    ['#63e6be', '#1fd1a5', '#dffff4'],
    ['#6aa7ff', '#447dff', '#dce9ff'],
    ['#ffbc5d', '#ff8f3f', '#fff1cb'],
    ['#ff8d8d', '#ff6666', '#ffdede'],
    ['#d69eff', '#a96cff', '#f4e6ff'],
    ['#a7ff83', '#5ee05b', '#efffe8'],
  ][hash % 6];

  return {
    id: fishId(record, index),
    scope: record.scope || 'public',
    status: record.status || 'in_sync',
    label: record.fqdn || record.name || 'record',
    x: (hash % 1000) / 1000,
    y: ((hash >> 4) % 1000) / 1000,
    vx: (hash % 2 === 0 ? 1 : -1) * (0.00016 + ((hash >> 8) % 6) * 0.000022),
    vy: (((hash >> 12) % 5) - 2) * 0.000028,
    bob: ((hash >> 16) % 1000) / 1000,
    size: 2 + (hash % 3),
    colors: palette,
  };
}

function resizeAquarium() {
  const rect = aquarium.canvas.getBoundingClientRect();
  const ratio = Math.max(1, Math.min(window.devicePixelRatio || 1, 2));
  const width = Math.max(240, Math.floor(rect.width * ratio));
  const height = Math.max(180, Math.floor(rect.height * ratio));
  if (aquarium.width === width && aquarium.height === height) {
    return;
  }
  aquarium.width = width;
  aquarium.height = height;
  aquarium.canvas.width = width;
  aquarium.canvas.height = height;
}

function drawAquarium(timestamp) {
  aquarium.animationFrame = requestAnimationFrame(drawAquarium);
  resizeAquarium();

  const { ctx, width, height, fish } = aquarium;
  ctx.clearRect(0, 0, width, height);
  drawWater(ctx, width, height, timestamp);

  if (!fish.length) {
    drawEmptyAquarium(ctx, width, height);
    return;
  }

  const delta = Math.min(48, Math.max(16, timestamp - aquarium.phase));
  aquarium.phase = timestamp;

  for (const fishItem of fish) {
    updateFish(fishItem, delta, width, height, timestamp);
    drawFish(ctx, fishItem, width, height, timestamp);
  }

  drawForeground(ctx, width, height, timestamp);
}

function drawWater(ctx, width, height, timestamp) {
  const top = ctx.createLinearGradient(0, 0, 0, height);
  top.addColorStop(0, 'rgba(72, 147, 255, 0.18)');
  top.addColorStop(0.6, 'rgba(15, 35, 61, 0.18)');
  top.addColorStop(1, 'rgba(2, 8, 15, 0.82)');
  ctx.fillStyle = top;
  ctx.fillRect(0, 0, width, height);

  const waveCount = 4;
  for (let i = 0; i < waveCount; i += 1) {
    ctx.fillStyle = `rgba(164, 216, 255, ${0.02 + i * 0.015})`;
    const y = height * (0.1 + i * 0.12);
    for (let x = 0; x < width; x += 20) {
      const wobble = Math.sin((x + timestamp * 0.05 + i * 40) / 26) * 3;
      ctx.fillRect(x, y + wobble, 14, 2);
    }
  }
}

function drawForeground(ctx, width, height, timestamp) {
  ctx.fillStyle = 'rgba(18, 52, 39, 0.8)';
  for (let i = 0; i < 7; i += 1) {
    const baseX = ((i + 0.4) / 7) * width;
    const sway = Math.sin(timestamp * 0.0016 + i) * 6;
    ctx.fillRect(baseX + sway, height - 34, 4, 28);
    ctx.fillRect(baseX + sway - 4, height - 24, 4, 18);
    ctx.fillRect(baseX + sway + 4, height - 20, 4, 18);
  }
  ctx.fillStyle = 'rgba(201, 181, 120, 0.75)';
  ctx.fillRect(0, height - 14, width, 14);
}

function drawEmptyAquarium(ctx, width, height) {
  ctx.fillStyle = 'rgba(223, 241, 255, 0.8)';
  ctx.font = `${Math.max(12, Math.floor(width / 24))}px monospace`;
  ctx.textAlign = 'center';
  ctx.fillText('Add records to wake the bowl.', width / 2, height / 2);
}

function updateFish(fish, delta, width, height, timestamp) {
  fish.x += fish.vx * delta;
  fish.y += fish.vy * delta + Math.sin(timestamp * 0.0013 + fish.bob * Math.PI * 2) * 0.00016 * delta;

  const marginX = 0.06;
  const marginY = 0.1;
  if (fish.x < marginX || fish.x > 1 - marginX) {
    fish.vx *= -1;
    fish.x = clamp(fish.x, marginX, 1 - marginX);
  }
  if (fish.y < marginY || fish.y > 0.82) {
    fish.vy *= -1;
    fish.y = clamp(fish.y, marginY, 0.82);
  }

  if (fish.status === 'drifted') {
    fish.vx *= 1.00008;
  }
}

function drawFish(ctx, fish, width, height, timestamp) {
  const pixel = fish.size;
  const x = Math.floor(fish.x * width);
  const y = Math.floor(fish.y * height);
  const direction = fish.vx >= 0 ? 1 : -1;
  const tailToggle = Math.floor(timestamp / 180 + fish.bob * 10) % 2;
  const bodyPattern = [
    { x: 0, y: 0, c: 0 }, { x: 1, y: 0, c: 0 }, { x: 2, y: 0, c: 1 }, { x: 3, y: 0, c: 1 },
    { x: -1, y: 1, c: 0 }, { x: 0, y: 1, c: 0 }, { x: 1, y: 1, c: 1 }, { x: 2, y: 1, c: 1 }, { x: 3, y: 1, c: 2 },
    { x: 0, y: 2, c: 0 }, { x: 1, y: 2, c: 1 }, { x: 2, y: 2, c: 1 }, { x: 3, y: 2, c: 1 },
  ];
  const tailPattern = tailToggle === 0
    ? [{ x: -2, y: 0, c: 1 }, { x: -2, y: 1, c: 1 }, { x: -3, y: 1, c: 1 }, { x: -2, y: 2, c: 1 }]
    : [{ x: -2, y: 0, c: 1 }, { x: -3, y: 0, c: 1 }, { x: -2, y: 1, c: 1 }, { x: -2, y: 2, c: 1 }];
  const finPattern = [{ x: 1, y: -1, c: 2 }, { x: 1, y: 3, c: 2 }];
  const bubble = Math.floor(timestamp / 450 + fish.bob * 100) % 8 === 0;

  ctx.save();
  if (direction < 0) {
    ctx.translate(x, 0);
    ctx.scale(-1, 1);
    drawPattern(ctx, 0, y, bodyPattern, pixel, fish.colors);
    drawPattern(ctx, 0, y, tailPattern, pixel, fish.colors);
    drawPattern(ctx, 0, y, finPattern, pixel, fish.colors);
    ctx.fillStyle = '#04131f';
    ctx.fillRect(pixel * 2, y + pixel, pixel, pixel);
  } else {
    drawPattern(ctx, x, y, bodyPattern, pixel, fish.colors);
    drawPattern(ctx, x, y, tailPattern, pixel, fish.colors);
    drawPattern(ctx, x, y, finPattern, pixel, fish.colors);
    ctx.fillStyle = '#04131f';
    ctx.fillRect(x + pixel * 2, y + pixel, pixel, pixel);
  }
  ctx.restore();

  if (fish.status === 'drifted') {
    ctx.fillStyle = 'rgba(255, 141, 141, 0.45)';
    ctx.fillRect(x - pixel * 2, y - pixel * 2, pixel * 8, pixel);
  }

  if (bubble) {
    ctx.fillStyle = 'rgba(220, 245, 255, 0.75)';
    ctx.fillRect(x + pixel * 2, y - pixel * 2, pixel, pixel);
    ctx.fillRect(x + pixel * 3, y - pixel * 4, pixel, pixel);
  }
}

function drawPattern(ctx, baseX, baseY, pattern, pixel, colors) {
  for (const point of pattern) {
    ctx.fillStyle = colors[point.c];
    ctx.fillRect(baseX + point.x * pixel, baseY + point.y * pixel, pixel, pixel);
  }
}

function hashString(value) {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return Math.abs(hash >>> 0);
}

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
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
boot();

window.addEventListener('focus', () => {
  refreshInventory().catch(() => {});
  refreshSyncStatus().catch(() => {});
});

window.addEventListener('pageshow', () => {
  refreshInventory().catch(() => {});
});

window.addEventListener('resize', () => {
  resizeAquarium();
});

window.refreshFishbowl = refreshAll;

async function boot() {
  const results = await Promise.allSettled([refreshEntries(), refreshInventory(), refreshSyncStatus()]);
  const rejected = results.find((result) => result.status === 'rejected');
  if (rejected) {
    const error = rejected.reason instanceof Error ? rejected.reason : new Error(String(rejected.reason));
    showFormError(error.message);
    statusEl.textContent = error.message;
  }
}
