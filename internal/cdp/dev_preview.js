const $ = (selector) => document.querySelector(selector);
const targets = $('#targets');
const status = $('#status');
const view = $('#view');
const stage = $('#stage');
const screen = $('#screen');
const screenSpace = $('#screen-space');
const mirror = createPreviewMirror({ passive: true });
const activityList = $('#activity-list');
const activityScroll = $('#activity-scroll');
const follow = $('#follow');
const theme = $('#theme');
const systemTheme = matchMedia('(prefers-color-scheme: dark)');
let socket;
let reconnectTimer = 0;
let reconnectDelay = 250;
let discoverySequence = 0;
let selectedTarget = '';
let pendingPreview = null;
let previewPaint = 0;
let paused = false;
let viewport = { width: 1280, height: 720 };
let snapshots = 0;
let entries = [];
let totalEvents = 0;
let activityPaint = 0;
let filter = 'all';
let lastInput = null;
let ringTimer = 0;

function applyTheme() {
  document.documentElement.dataset.theme =
    theme.value === 'system' ? (systemTheme.matches ? 'dark' : 'light') : theme.value;
}
try {
  const saved = localStorage.getItem('mimic-preview-theme');
  theme.value = ['light', 'dark', 'system'].includes(saved) ? saved : 'system';
} catch {
  theme.value = 'system';
}
applyTheme();
theme.addEventListener('change', () => {
  applyTheme();
  try {
    localStorage.setItem('mimic-preview-theme', theme.value);
  } catch {
    // The theme still works when browser storage is unavailable.
  }
});
systemTheme.addEventListener('change', applyTheme);

function setStatus(text, state = 'connecting') {
  status.textContent = text;
  $('#connection').dataset.state = state;
}

function showEmpty(title, description) {
  $('#empty-title').textContent = title;
  $('#empty-description').textContent = description;
  $('#empty-state').hidden = false;
  screenSpace.hidden = true;
}

function scalePreview() {
  const styles = getComputedStyle(stage);
  const availableWidth =
    stage.clientWidth - parseFloat(styles.paddingLeft) - parseFloat(styles.paddingRight);
  const availableHeight =
    stage.clientHeight - parseFloat(styles.paddingTop) - parseFloat(styles.paddingBottom);
  const zoom = $('#zoom').value;
  const scale =
    zoom === 'fit'
      ? Math.max(
          0.05,
          Math.min(1, availableWidth / viewport.width, availableHeight / viewport.height),
        )
      : Number(zoom);
  screen.style.width = view.style.width = viewport.width + 'px';
  screen.style.height = view.style.height = viewport.height + 'px';
  screen.style.transform = `scale(${scale})`;
  screenSpace.style.width = Math.ceil(viewport.width * scale) + 'px';
  screenSpace.style.height = Math.ceil(viewport.height * scale) + 'px';
  screenSpace.style.marginTop = Math.max(0, (availableHeight - viewport.height * scale) / 2) + 'px';
  $('#dimensions').textContent =
    `${viewport.width} × ${viewport.height} · ${Math.round(scale * 100)}%`;
}
new ResizeObserver(scalePreview).observe(stage);
$('#zoom').addEventListener('change', scalePreview);

function schedulePreview(packet) {
  pendingPreview = packet;
  if (paused || previewPaint) return;
  previewPaint = requestAnimationFrame(() => {
    previewPaint = 0;
    if (paused || !pendingPreview) return;
    const packet = pendingPreview;
    pendingPreview = null;
    if (packet.width > 0 && packet.height > 0) {
      viewport = { width: packet.width, height: packet.height };
    }
    scalePreview();
    mirror.update(view, packet.html || '');
    $('#empty-state').hidden = true;
    screenSpace.hidden = false;
    snapshots++;
    $('#snapshot-status').textContent =
      `Snapshot ${snapshots} · ${new Date().toLocaleTimeString()}`;
    if (lastInput) drawInput(lastInput, false);
  });
}

$('#pause').addEventListener('click', () => {
  paused = !paused;
  $('#pause').setAttribute('aria-pressed', String(paused));
  $('#pause').textContent = paused ? 'Resume view' : 'Pause view';
  if (paused) $('#snapshot-status').textContent = 'View paused · activity is live';
  else if (pendingPreview) schedulePreview(pendingPreview);
  else $('#snapshot-status').textContent = `Snapshot ${snapshots} · view resumed`;
});

function drawInput(event, animate = true) {
  const data = event.data || {};
  if (
    data.method !== 'Input.dispatchMouseEvent' ||
    !Number.isFinite(data.x) ||
    !Number.isFinite(data.y)
  )
    return;
  const cursor = $('#automation-cursor');
  cursor.hidden = false;
  cursor.style.left = data.x + 'px';
  cursor.style.top = data.y + 'px';
  if (animate && data.type === 'mousePressed') {
    const ring = $('#click-ring');
    ring.style.left = data.x + 'px';
    ring.style.top = data.y + 'px';
    ring.hidden = false;
    ring.classList.remove('pulse');
    void ring.offsetWidth;
    ring.classList.add('pulse');
    clearTimeout(ringTimer);
    ringTimer = setTimeout(() => {
      ring.hidden = true;
    }, 550);
  }
}

function describeEvent(event) {
  const data = event.data || {};
  let category = 'actions';
  let badge = 'PAGE';
  let title = event.name;
  let detail = data.url || data.source || '';
  if (event.kind === 'cdp') {
    badge = 'CMD';
    title = data.method || event.name;
    if (event.name === 'commandError') {
      category = 'errors';
      badge = 'ERROR';
      title = `Command #${data.commandId} failed`;
      detail = data.error || '';
    }
    if (data.method === 'Input.dispatchMouseEvent') {
      badge = 'INPUT';
      const names = {
        mouseMoved: 'Pointer moved',
        mousePressed: 'Mouse pressed',
        mouseReleased: 'Mouse released',
        mouseWheel: 'Scrolled',
      };
      title = names[data.type] || data.type || title;
      detail = `${data.button || 'pointer'} · ${data.x}, ${data.y}`;
      if (data.type === 'mouseWheel') detail += ` · Δ ${data.deltaX || 0}, ${data.deltaY || 0}`;
    } else if (data.method === 'Input.dispatchKeyEvent') {
      badge = 'INPUT';
      title = `${data.type || 'Key'} · ${data.key || data.text || ''}`;
    } else if (data.method === 'Input.insertText') {
      badge = 'INPUT';
      title = 'Text inserted';
      detail = data.text || '';
    } else if (data.method === 'Page.navigate') {
      title = 'Navigate';
    } else {
      detail = data.selector || data.expression || data.functionDeclaration || detail;
    }
  } else if (event.kind === 'network') {
    category = 'network';
    badge = data.status ? String(data.status) : 'NET';
    title = `${data.method || ''} ${event.name}`.trim();
    if (event.name === 'failed' || data.status >= 400) {
      category = 'errors';
      detail = data.error ? `${data.error} · ${detail}` : detail;
    }
  } else if (event.kind === 'console') {
    category = 'console';
    badge = event.name;
    title = consoleText(data.args);
    if (event.name === 'error' || event.name === 'assert') category = 'errors';
  } else if (event.kind === 'exception' || event.kind === 'error') {
    category = 'errors';
    badge = 'ERROR';
    title = data.error || event.name;
  } else if (event.kind === 'viewer') {
    badge = 'VIEW';
    title = event.name;
    detail = data.detail || '';
    if (data.error) {
      category = 'errors';
      badge = 'ERROR';
      detail = data.error;
    }
  }
  return { category, badge, title: String(title || ''), detail: String(detail) };
}

function consoleText(args) {
  try {
    const values = JSON.parse(args);
    return Array.isArray(values)
      ? values.map((value) => (typeof value === 'string' ? value : JSON.stringify(value))).join(' ')
      : String(args || '');
  } catch {
    return String(args || '');
  }
}

function textNode(tag, className, text) {
  const node = document.createElement(tag);
  node.className = className;
  node.textContent = text;
  return node;
}

function createEntry(event) {
  const description = describeEvent(event);
  const row = document.createElement('li');
  row.className = 'event';
  row.dataset.category = description.category;
  row.dataset.kind = event.kind;
  const details = document.createElement('details');
  const summary = document.createElement('summary');
  const top = textNode('div', 'event-top', '');
  const date = new Date(event.time || Date.now());
  const time = textNode('time', '', date.toLocaleTimeString([], { hour12: false }));
  time.dateTime = date.toISOString();
  top.append(textNode('span', 'event-badge', description.badge), time);
  summary.append(top, textNode('div', 'event-title', description.title));
  if (description.detail) summary.append(textNode('div', 'event-detail', description.detail));
  details.append(summary, textNode('pre', '', JSON.stringify(event.data || {}, null, 2)));
  row.append(details);
  return {
    node: row,
    category: description.category,
    kind: event.kind,
    search:
      `${description.title} ${description.detail} ${JSON.stringify(event.data || {})}`.toLowerCase(),
  };
}

function renderActivity() {
  activityPaint = 0;
  const search = $('#search').value.trim().toLowerCase();
  let visible = 0;
  for (const entry of entries) {
    const matches =
      (filter === 'all' ||
        filter === entry.category ||
        (filter === 'network' && entry.kind === 'network') ||
        (filter === 'console' && entry.kind === 'console')) &&
      entry.search.includes(search);
    entry.node.hidden = !matches;
    if (matches) visible++;
    if (!entry.node.isConnected) activityList.append(entry.node);
  }
  $('#event-count').textContent = totalEvents.toLocaleString();
  $('#activity-empty').hidden = visible > 0;
  $('#activity-empty strong').textContent = entries.length
    ? 'No matching activity'
    : 'Follow every step';
  $('#activity-empty p').textContent = entries.length
    ? 'Try another filter or search term.'
    : 'Commands, requests, console output and errors appear here while this target is connected.';
  $('#retention').textContent = totalEvents > 500 ? '500 retained' : 'Last 500 events';
  if (follow.checked) activityScroll.scrollTop = activityScroll.scrollHeight;
}

function queueActivityRender() {
  if (!activityPaint) activityPaint = requestAnimationFrame(renderActivity);
}

function addEvent(event) {
  if (event.kind === 'cdp' && event.data?.method === 'Input.dispatchMouseEvent') {
    lastInput = event;
    if (!paused) drawInput(event);
  }
  entries.push(createEntry(event));
  totalEvents++;
  while (entries.length > 500) entries.shift().node.remove();
  queueActivityRender();
}

function viewerEvent(name, data = {}) {
  addEvent({ kind: 'viewer', name, data, time: new Date().toISOString() });
}

function clearActivity() {
  entries = [];
  totalEvents = 0;
  activityList.replaceChildren();
  queueActivityRender();
}
$('#clear').addEventListener('click', clearActivity);
$('#search').addEventListener('input', queueActivityRender);
for (const button of document.querySelectorAll('[data-filter]')) {
  button.addEventListener('click', () => {
    filter = button.dataset.filter;
    for (const other of document.querySelectorAll('[data-filter]')) {
      other.classList.toggle('active', other === button);
      other.setAttribute('aria-pressed', String(other === button));
    }
    queueActivityRender();
  });
}
follow.addEventListener('change', queueActivityRender);
activityScroll.addEventListener(
  'wheel',
  (event) => {
    if (event.deltaY < 0) follow.checked = false;
  },
  { passive: true },
);

function queueReconnect() {
  if (reconnectTimer) return;
  setStatus('Disconnected — reconnecting…', 'error');
  const delay = reconnectDelay;
  reconnectDelay = Math.min(reconnectDelay * 2, 5000);
  reconnectTimer = setTimeout(() => {
    reconnectTimer = 0;
    list(true);
  }, delay);
}

async function list(retrying = false) {
  const sequence = ++discoverySequence;
  try {
    const response = await fetch('/json/list', { cache: 'no-store' });
    if (!response.ok) throw Error('Target discovery failed: ' + response.status);
    const data = await response.json();
    if (sequence !== discoverySequence) return;
    const old = targets.value || new URLSearchParams(location.search).get('target');
    targets.replaceChildren();
    for (const target of data.filter((target) => target.type === 'page')) {
      const option = document.createElement('option');
      option.value = target.id;
      option.textContent = `${target.title || target.url || 'Untitled page'} · ${target.id.slice(0, 8)}`;
      option.title = target.url;
      targets.append(option);
    }
    if ([...targets.options].some((option) => option.value === old)) targets.value = old;
    if (!socket || socket.readyState > WebSocket.OPEN || selectedTarget !== targets.value)
      connect();
  } catch (error) {
    if (sequence !== discoverySequence) return;
    setStatus('Discovery failed', 'error');
    viewerEvent('Target discovery failed', { error: String(error) });
    if (retrying || !socket) queueReconnect();
  }
}

function connect() {
  clearTimeout(reconnectTimer);
  reconnectTimer = 0;
  if (socket) {
    socket.onclose = null;
    socket.close();
    socket = null;
  }
  if (previewPaint) cancelAnimationFrame(previewPaint);
  previewPaint = 0;
  pendingPreview = null;
  snapshots = 0;
  paused = false;
  $('#pause').setAttribute('aria-pressed', 'false');
  $('#pause').textContent = 'Pause view';
  lastInput = null;
  clearTimeout(ringTimer);
  $('#automation-cursor').hidden = $('#click-ring').hidden = true;
  mirror.update(view, '');
  $('#snapshot-status').textContent = 'No snapshots yet';
  if (selectedTarget !== targets.value) clearActivity();
  selectedTarget = targets.value;
  if (!selectedTarget) {
    showEmpty(
      'No open targets',
      'Create a page through your automation, then it will appear here automatically.',
    );
    setStatus('No targets');
    const delay = Math.max(reconnectDelay, 1000);
    reconnectDelay = Math.min(reconnectDelay * 2, 5000);
    reconnectTimer = setTimeout(() => {
      reconnectTimer = 0;
      list(true);
    }, delay);
    return;
  }
  const url = new URL(location.href);
  url.searchParams.set('target', selectedTarget);
  history.replaceState(null, '', url);
  showEmpty('Connecting to target', 'Waiting for the first live snapshot…');
  const current = new WebSocket(
    (location.protocol === 'https:' ? 'wss:' : 'ws:') +
      '//' +
      location.host +
      '/debug/preview/ws?target=' +
      encodeURIComponent(selectedTarget),
  );
  socket = current;
  setStatus('Connecting…');
  current.onopen = () => {
    if (socket !== current) return;
    reconnectDelay = 250;
    setStatus('Live', 'live');
    viewerEvent('Connected to target', { detail: selectedTarget });
  };
  current.onclose = () => {
    if (socket !== current) return;
    socket = null;
    viewerEvent('Connection lost', { detail: 'Reconnecting automatically' });
    queueReconnect();
  };
  current.onmessage = (event) => {
    if (socket !== current) return;
    try {
      const packet = JSON.parse(event.data);
      if (packet.type === 'activity') {
        if (packet.dropped)
          viewerEvent('Activity overflow', {
            error: `${packet.dropped} events omitted because the viewer could not keep up.`,
          });
        if (packet.event) addEvent(packet.event);
        return;
      }
      if (packet.error) {
        setStatus('Observation error', 'error');
        viewerEvent('Observation error', { error: packet.error });
        return;
      }
      if (packet.closed) {
        setStatus('Target closed', 'error');
        viewerEvent('Target closed');
        showEmpty(
          'Target closed',
          'Choose another target or create a new page through your automation.',
        );
        list(true);
        return;
      }
      setStatus('Live', 'live');
      const option = targets.selectedOptions[0];
      if (option && option.title !== packet.url) {
        option.title = packet.url;
        option.textContent = `${packet.url} · ${selectedTarget.slice(0, 8)}`;
      }
      schedulePreview(packet);
    } catch (error) {
      setStatus('Preview error', 'error');
      viewerEvent('Invalid preview message', { error: String(error) });
    }
  };
}
targets.addEventListener('change', () => {
  discoverySequence++;
  connect();
});
$('#refresh').addEventListener('click', () => {
  reconnectDelay = 250;
  list();
});
window.addEventListener('pagehide', () => {
  clearTimeout(reconnectTimer);
  if (socket) {
    socket.onclose = null;
    socket.close();
  }
});
list();
