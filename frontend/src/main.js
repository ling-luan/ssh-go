import { createIcons, icons } from 'lucide';
import {
  DeleteProfile,
  DeleteTunnel,
  ReloadSnapshot,
  SaveProfile,
  SaveTunnel,
  Snapshot,
  StartAll,
  StartTunnel,
  StopAll,
  StopTunnel,
} from '../wailsjs/go/main/App.js';
import './styles.css';

const state = {
  data: null,
  selectedTunnelId: '',
  selectedProfileId: '',
  modal: null,
  busyAction: '',
  toast: null,
  refreshInFlight: false,
  refreshQueued: false,
  realtimeTimer: null,
};

const app = document.querySelector('#app');
const REALTIME_REFRESH_MS = 1000;

const stateText = {
  running: '运行中',
  starting: '启动中',
  stopping: '停止中',
  error: '错误',
  stopped: '已停止',
};

function formatBytes(value = 0) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = Number(value) || 0;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`;
}

function formatTime(value) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleTimeString('zh-CN', { hour12: false });
}

function escapeHtml(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;');
}

async function refresh(silent = false, reloadConfig = false) {
  if (state.refreshInFlight) {
    state.refreshQueued = true;
    return false;
  }
  state.refreshInFlight = true;
  try {
    const hadData = Boolean(state.data);
    const snapshot = reloadConfig ? ReloadSnapshot() : Snapshot();
    const next = await withTimeout(snapshot, 8000, '读取后端状态超时，请重试或重启程序');
    state.data = next;
    reconcileSelection();
    if (hadData) {
      renderLiveDataOnly();
      return true;
    }
    render();
    return true;
  } catch (error) {
    if (!silent) {
      showToast(error.message || String(error), 'error');
      renderError(error.message || String(error));
    }
    return false;
  } finally {
    state.refreshInFlight = false;
    if (state.refreshQueued) {
      state.refreshQueued = false;
      window.setTimeout(() => refresh(true), 0);
    }
  }
}

function reconcileSelection() {
  const tunnels = state.data?.tunnels ?? [];
  const profiles = state.data?.profiles ?? [];
  if (!tunnels.some((item) => item.id === state.selectedTunnelId)) {
    state.selectedTunnelId = tunnels[0]?.id || '';
  }
  if (!profiles.some((item) => item.id === state.selectedProfileId)) {
    state.selectedProfileId = profiles[0]?.id || '';
  }
}

function withTimeout(promise, ms, message) {
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      window.setTimeout(() => reject(new Error(message)), ms);
    }),
  ]);
}

function delay(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

async function waitForRefreshIdle() {
  while (state.refreshInFlight) {
    await delay(50);
  }
}

async function runAction(action, successMessage) {
  if (!state.busyAction) {
    state.busyAction = 'pending';
    updateBusyButtons();
  }
  try {
    await action();
    if (successMessage) showToast(successMessage, 'success');
    await refresh(true);
    return true;
  } catch (error) {
    showToast(error.message || String(error), 'error');
    return false;
  } finally {
    state.busyAction = '';
    updateBusyButtons();
  }
}

function showToast(message, type = 'info') {
  state.toast = { message, type };
  if (state.data) {
    renderToastOnly();
  } else {
    render();
  }
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => {
    state.toast = null;
    if (state.data) {
      renderToastOnly();
    } else {
      render();
    }
  }, 3200);
}

function renderToastOnly() {
  app.querySelectorAll('.toast').forEach((element) => element.remove());
  if (!state.toast) return;
  app.insertAdjacentHTML(
    'beforeend',
    `<div class="toast ${state.toast.type}">${escapeHtml(state.toast.message)}</div>`,
  );
}

function setFieldError(name, message) {
  const input = app.querySelector(`.modal [name="${name}"]`);
  if (!input) return;
  input.classList.add('field-invalid');
  const label = input.closest('label');
  if (!label) return;
  label.querySelector('.field-error')?.remove();
  label.insertAdjacentHTML('beforeend', `<strong class="field-error">${escapeHtml(message)}</strong>`);
  input.focus();
}

function clearFieldErrors() {
  app.querySelectorAll('.field-invalid').forEach((element) => element.classList.remove('field-invalid'));
  app.querySelectorAll('.field-error').forEach((element) => element.remove());
}

function profileConfig(id) {
  return state.data?.config.profiles.find((item) => item.id === id);
}

function tunnelConfig(id) {
  return state.data?.config.tunnels.find((item) => item.id === id);
}

function selectedTunnel() {
  return state.data?.tunnels.find((item) => item.id === state.selectedTunnelId);
}

function totals() {
  const tunnels = state.data?.tunnels ?? [];
  return tunnels.reduce(
    (acc, item) => {
      acc.running += item.running ? 1 : 0;
      acc.connections += item.activeConnections || 0;
      acc.bytesIn += item.bytesIn || 0;
      acc.bytesOut += item.bytesOut || 0;
      return acc;
    },
    { running: 0, connections: 0, bytesIn: 0, bytesOut: 0 },
  );
}

function render() {
  if (!state.data) {
    app.innerHTML = '<main class="shell loading">加载中...</main>';
    return;
  }

  const scrollPositions = captureScrollPositions();
  const stats = totals();
  const tun = selectedTunnel();

  app.innerHTML = `
    <main class="shell">
      <header class="topbar">
        <div>
          <h1>SSH Forwarder</h1>
          <p>${escapeHtml(state.data.configPath)}</p>
        </div>
        <div class="toolbar">
          ${buttonHtml('refresh-cw', '刷新', 'refresh')}
          ${buttonHtml('play', '全部启动', 'start-all', 'primary')}
          ${buttonHtml('square', '全部停止', 'stop-all')}
        </div>
      </header>

      <section class="workspace">
        <aside class="rail">
          <section class="metrics">
            ${metricHtml('activity', '运行隧道', `${stats.running} / ${state.data.tunnels.length}`)}
            ${metricHtml('plug-zap', '活动连接', stats.connections)}
            ${metricHtml('download', '已接收', formatBytes(stats.bytesIn))}
            ${metricHtml('upload', '已发送', formatBytes(stats.bytesOut))}
          </section>

          <section class="panel profiles">
            <div class="panel-head">
              <h2>SSH Profile</h2>
              <div class="icon-actions">
                ${iconButtonHtml('plus', '新建 Profile', 'profile-new')}
                ${iconButtonHtml('pencil', '编辑 Profile', 'profile-edit')}
                ${iconButtonHtml('trash-2', '删除 Profile', 'profile-delete')}
              </div>
            </div>
            <div class="profile-list">${profileRowsHtml()}</div>
          </section>

          <section class="panel logs">
            <div class="panel-head">
              <h2>事件</h2>
              <span>${state.data.events.length}</span>
            </div>
            <div class="event-list">${eventsHtml()}</div>
          </section>
        </aside>

        <section class="panel tunnels">
          <div class="panel-head">
            <h2>隧道</h2>
            <div class="toolbar compact">
              ${buttonHtml('plus', '新建隧道', 'tunnel-new', 'primary')}
              ${buttonHtml('pencil', '编辑', 'tunnel-edit')}
              ${buttonHtml('trash-2', '删除', 'tunnel-delete')}
              <span class="split"></span>
              ${buttonHtml('play', '启动', 'tunnel-start', 'primary')}
              ${buttonHtml('square', '停止', 'tunnel-stop')}
            </div>
          </div>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>名称</th>
                  <th>状态</th>
                  <th>Profile</th>
                  <th>本地地址</th>
                  <th>目标地址</th>
                  <th>自动</th>
                  <th>连接</th>
                  <th>接收</th>
                  <th>发送</th>
                  <th>错误</th>
                </tr>
              </thead>
              <tbody>${tunnelRowsHtml()}</tbody>
            </table>
          </div>
          <div class="details">${detailsHtml(tun)}</div>
        </section>
      </section>
      ${state.toast ? `<div class="toast ${state.toast.type}">${escapeHtml(state.toast.message)}</div>` : ''}
      ${modalHtml()}
    </main>
  `;

  createIcons({ icons });
  bindEvents();
  restoreScrollPositions(scrollPositions);
}

function renderLiveDataOnly() {
  if (!state.data) return;

  const stats = totals();
  const metrics = app.querySelector('.metrics');
  if (metrics) {
    metrics.innerHTML = metricsHtml(stats);
  }

  updateProfileList();
  const eventCount = app.querySelector('.logs .panel-head span');
  if (eventCount) {
    eventCount.textContent = String(state.data.events.length);
  }
  updateEventList();
  updateTunnelList();
  updateDetails();

  createIcons({ icons });
  updateBusyButtons();
}

function metricsHtml(stats) {
  return `
    ${metricHtml('activity', '运行隧道', `${stats.running} / ${state.data.tunnels.length}`)}
    ${metricHtml('plug-zap', '活动连接', stats.connections)}
    ${metricHtml('download', '已接收', formatBytes(stats.bytesIn))}
    ${metricHtml('upload', '已发送', formatBytes(stats.bytesOut))}
  `;
}

function replaceHtml(selector, html) {
  const element = app.querySelector(selector);
  if (!element) return;
  element.innerHTML = html;
  bindEvents(element);
}

function updateProfileList() {
  const list = app.querySelector('.profile-list');
  if (!list) return;

  const rows = state.data.profiles;
  const existing = Array.from(list.querySelectorAll('.profile-row'));
  const canPatch =
    rows.length > 0 &&
    existing.length === rows.length &&
    existing.every((element, index) => element.dataset.id === rows[index].id);

  if (!canPatch) {
    list.innerHTML = profileRowsHtml();
    bindEvents(list);
    return;
  }

  for (let index = 0; index < rows.length; index += 1) {
    const item = rows[index];
    const row = existing[index];
    row.classList.toggle('active', item.id === state.selectedProfileId);
    row.querySelector('.status-dot')?.classList.toggle('connected', item.connected);
    row.querySelector('strong').textContent = item.name;
    row.querySelector('em').textContent = item.address;
    row.querySelector('b').textContent = String(item.activeTunnels);
  }
}

function updateEventList() {
  const list = app.querySelector('.event-list');
  if (!list) return;
  const signature = state.data.events
    .slice(0, 120)
    .map((item) => `${item.time}|${item.level}|${item.tunnelId}|${item.tunnelName}|${item.message}`)
    .join('\n');
  if (list.dataset.signature === signature) return;
  list.dataset.signature = signature;
  list.innerHTML = eventsHtml();
}

function updateTunnelList() {
  const tbody = app.querySelector('.tunnels tbody');
  if (!tbody) return;

  const rows = state.data.tunnels;
  const existing = Array.from(tbody.querySelectorAll('tr[data-id]'));
  const canPatch =
    rows.length > 0 &&
    existing.length === rows.length &&
    existing.every((element, index) => element.dataset.id === rows[index].id);

  if (!canPatch) {
    tbody.innerHTML = tunnelRowsHtml();
    bindEvents(tbody);
    return;
  }

  for (let index = 0; index < rows.length; index += 1) {
    updateTunnelRow(existing[index], rows[index]);
  }
}

function updateTunnelRow(row, item) {
  row.classList.toggle('active', item.id === state.selectedTunnelId);
  const cells = row.children;
  cells[0].querySelector('strong').textContent = item.name;
  const badge = cells[1].querySelector('.badge');
  badge.className = `badge ${item.state}`;
  badge.textContent = stateText[item.state] || item.state;
  cells[2].textContent = item.profileName;
  cells[3].textContent = item.localAddress;
  cells[4].textContent = item.targetAddress;
  cells[5].textContent = item.autoStart ? '是' : '否';
  cells[6].textContent = String(item.activeConnections);
  cells[7].textContent = formatBytes(item.bytesIn);
  cells[8].textContent = formatBytes(item.bytesOut);
  cells[9].textContent = item.lastError || '-';
}

function updateDetails() {
  replaceHtml('.details', detailsHtml(selectedTunnel()));
}

function captureScrollPositions() {
  return ['.table-wrap', '.profile-list', '.event-list'].map((selector) => {
    const element = app.querySelector(selector);
    return {
      selector,
      top: element?.scrollTop ?? 0,
      left: element?.scrollLeft ?? 0,
    };
  });
}

function restoreScrollPositions(positions) {
  for (const item of positions) {
    const element = app.querySelector(item.selector);
    if (!element) continue;
    element.scrollTop = item.top;
    element.scrollLeft = item.left;
  }
}

function renderError(message) {
  if (state.data) return;
  app.innerHTML = `
    <main class="shell loading">
      <div class="startup-error">
        <h1>SSH Forwarder</h1>
        <p>${escapeHtml(message)}</p>
        <button class="btn primary" data-action="refresh"><i data-lucide="refresh-cw"></i><span>重试</span></button>
      </div>
    </main>
  `;
  createIcons({ icons });
  bindEvents();
}

function metricHtml(icon, label, value) {
  return `
    <div class="metric">
      <div class="metric-icon"><i data-lucide="${icon}"></i></div>
      <div>
        <span>${label}</span>
        <strong>${escapeHtml(value)}</strong>
      </div>
    </div>
  `;
}

function profileRowsHtml() {
  const rows = state.data.profiles;
  if (rows.length === 0) {
    return '<div class="empty">暂无 Profile</div>';
  }
  return rows
    .map((item) => {
      const active = item.id === state.selectedProfileId ? 'active' : '';
      const connected = item.connected ? 'connected' : '';
      return `
        <button class="profile-row ${active}" data-action="profile-select" data-context-action="profile-edit" data-id="${escapeHtml(item.id)}">
          <span class="status-dot ${connected}"></span>
          <span>
            <strong>${escapeHtml(item.name)}</strong>
            <em>${escapeHtml(item.address)}</em>
          </span>
          <b>${item.activeTunnels}</b>
        </button>
      `;
    })
    .join('');
}

function tunnelRowsHtml() {
  const rows = state.data.tunnels;
  if (rows.length === 0) {
    return '<tr><td colspan="10" class="empty-cell">暂无隧道</td></tr>';
  }
  return rows
    .map((item) => {
      const active = item.id === state.selectedTunnelId ? 'active' : '';
      return `
        <tr class="${active}" data-action="tunnel-select" data-dbl-action="tunnel-toggle" data-context-action="tunnel-edit" data-id="${escapeHtml(item.id)}" title="双击启动或停止，右键编辑">
          <td><strong>${escapeHtml(item.name)}</strong></td>
          <td><span class="badge ${item.state}">${stateText[item.state] || item.state}</span></td>
          <td>${escapeHtml(item.profileName)}</td>
          <td>${escapeHtml(item.localAddress)}</td>
          <td>${escapeHtml(item.targetAddress)}</td>
          <td>${item.autoStart ? '是' : '否'}</td>
          <td>${item.activeConnections}</td>
          <td>${formatBytes(item.bytesIn)}</td>
          <td>${formatBytes(item.bytesOut)}</td>
          <td class="error-text">${escapeHtml(item.lastError || '-')}</td>
        </tr>
      `;
    })
    .join('');
}

function detailsHtml(item) {
  if (!item) {
    return '<div class="empty">未选择隧道</div>';
  }
  return `
    <div><span>选中</span><strong>${escapeHtml(item.name)}</strong></div>
    <div><span>状态</span><strong class="state-${item.state}">${stateText[item.state] || item.state}</strong></div>
    <div><span>路由</span><strong>${escapeHtml(item.localAddress)} -> ${escapeHtml(item.targetAddress)}</strong></div>
    <div><span>Profile</span><strong>${escapeHtml(item.profileName)}</strong></div>
    <div><span>流量</span><strong>连接 ${item.activeConnections}，接收 ${formatBytes(item.bytesIn)}，发送 ${formatBytes(item.bytesOut)}</strong></div>
    <div><span>最后错误</span><strong class="error-text">${escapeHtml(item.lastError || '-')}</strong></div>
  `;
}

function eventsHtml() {
  const events = state.data.events;
  if (events.length === 0) {
    return '<div class="empty dark">暂无事件</div>';
  }
  return events
    .slice(0, 120)
    .map((item) => {
      const tunnelLabel = item.tunnelName || item.tunnelId;
      return `
        <div class="event ${item.level}">
          <time>${formatTime(item.time)}</time>
          <span>${tunnelLabel ? `[${escapeHtml(tunnelLabel)}] ` : ''}${escapeHtml(item.message)}</span>
        </div>
      `;
    })
    .join('');
}

function modalHtml() {
  if (!state.modal) return '';
  const { type, mode, data } = state.modal;
  const title = `${mode === 'edit' ? '编辑' : '新建'}${type === 'profile' ? ' SSH Profile' : '隧道'}`;
  return `
    <div class="modal-backdrop">
      <form class="modal" data-form="${type}">
        <div class="modal-head">
          <h2>${title}</h2>
          ${iconButtonHtml('x', '关闭', 'modal-close')}
        </div>
        <div class="form-grid">
          ${type === 'profile' ? profileFormHtml(data, mode) : tunnelFormHtml(data, mode)}
        </div>
        <footer class="modal-actions">
          ${buttonHtml('x', '取消', 'modal-close')}
          ${buttonHtml('save', '保存', 'modal-save', 'primary')}
        </footer>
      </form>
    </div>
  `;
}

function fieldHtml(label, name, value = '', attrs = '') {
  const required = attrs.includes('required') ? '<em>必填</em>' : '';
  return `
    <label>
      <span>${label}${required}</span>
      <input name="${name}" value="${escapeHtml(value)}" ${attrs} />
    </label>
  `;
}

function numberFieldHtml(label, name, value = '', min = 1, max = 65535) {
  return fieldHtml(label, name, value, `type="number" min="${min}" max="${max}"`);
}

function selectHtml(label, name, value, options) {
  return `
    <label>
      <span>${label}</span>
      <select name="${name}">
        ${options.map((item) => `<option value="${escapeHtml(item.value)}" ${item.value === value ? 'selected' : ''}>${escapeHtml(item.label)}</option>`).join('')}
      </select>
    </label>
  `;
}

function profileFormHtml(profile, mode) {
  const readonly = mode === 'edit' ? 'readonly' : '';
  return `
    ${fieldHtml('ID', 'id', profile.id, `${readonly} placeholder="留空自动生成"`)}
    ${fieldHtml('名称', 'name', profile.name, 'required')}
    ${fieldHtml('SSH 主机', 'host', profile.host, 'required')}
    ${numberFieldHtml('端口', 'port', profile.port || 22)}
    ${fieldHtml('用户名', 'username', profile.username, 'required')}
    ${selectHtml('认证方式', 'authType', profile.auth?.type || 'key', [
      { value: 'key', label: '私钥' },
      { value: 'password', label: '密码' },
    ])}
    ${fieldHtml('密码', 'password', profile.auth?.password || '', 'type="password"')}
    ${fieldHtml('私钥路径', 'keyPath', profile.auth?.keyPath || '', 'placeholder="key 认证方式必填"')}
    ${fieldHtml('私钥口令', 'passphrase', profile.auth?.passphrase || '', 'type="password"')}
    ${selectHtml('主机密钥策略', 'hostKeyPolicy', profile.hostKeyPolicy || 'accept-new', [
      { value: 'accept-new', label: 'accept-new' },
      { value: 'known-hosts', label: 'known-hosts' },
      { value: 'insecure', label: 'insecure' },
    ])}
    ${fieldHtml('known_hosts', 'knownHostsPath', profile.knownHostsPath || '')}
    ${numberFieldHtml('连接超时', 'connectTimeoutSeconds', profile.connectTimeoutSeconds || 8, 1, 120)}
    ${numberFieldHtml('保活间隔', 'keepAliveSeconds', profile.keepAliveSeconds || 30, 5, 600)}
  `;
}

function tunnelFormHtml(tunnel, mode) {
  const readonly = mode === 'edit' ? 'readonly' : '';
  const profileOptions = state.data.config.profiles.map((item) => ({
    value: item.id,
    label: `${item.id} - ${item.name}`,
  }));
  return `
    ${fieldHtml('ID', 'id', tunnel.id, `${readonly} placeholder="留空自动生成"`)}
    ${fieldHtml('名称', 'name', tunnel.name, 'required')}
    ${selectHtml('SSH Profile', 'profileId', tunnel.profileId || profileOptions[0]?.value || '', profileOptions)}
    <label class="checkbox">
      <input name="autoStart" type="checkbox" ${tunnel.autoStart ? 'checked' : ''} />
      <span>自动启动</span>
    </label>
    ${fieldHtml('本地地址', 'localHost', tunnel.localHost || '127.0.0.1')}
    ${numberFieldHtml('本地端口', 'localPort', tunnel.localPort || 8080)}
    ${fieldHtml('目标地址', 'targetHost', tunnel.targetHost || '127.0.0.1')}
    ${numberFieldHtml('目标端口', 'targetPort', tunnel.targetPort || 80)}
  `;
}

function buttonHtml(icon, text, action, variant = '') {
  return `<button class="btn ${variant}" data-action="${action}" ${isActionDisabled(action) ? 'disabled' : ''}><i data-lucide="${icon}"></i><span>${text}</span></button>`;
}

function iconButtonHtml(icon, title, action) {
  return `<button class="icon-btn" type="button" title="${title}" data-action="${action}" ${isActionDisabled(action) ? 'disabled' : ''}><i data-lucide="${icon}"></i></button>`;
}

function bindEvents(root = app) {
  root.querySelectorAll('[data-action]').forEach((element) => {
    element.addEventListener('click', handleAction);
  });
  root.querySelectorAll('[data-dbl-action]').forEach((element) => {
    element.addEventListener('dblclick', handleDoubleAction);
  });
  root.querySelectorAll('[data-context-action]').forEach((element) => {
    element.addEventListener('contextmenu', handleContextAction);
  });
  root.querySelectorAll('.modal input, .modal select').forEach((element) => {
    element.addEventListener('input', clearFieldErrorOnInput);
    element.addEventListener('change', clearFieldErrorOnInput);
  });
}

function updateBusyButtons() {
  app.querySelectorAll('[data-action]').forEach((element) => {
    element.disabled = isActionDisabled(element.dataset.action);
  });
}

function isActionDisabled(action) {
  return Boolean(state.busyAction && state.busyAction === action);
}

function clearFieldErrorOnInput(event) {
  const input = event.currentTarget;
  input.classList.remove('field-invalid');
  input.closest('label')?.querySelector('.field-error')?.remove();
}

function markBusy(action) {
  state.busyAction = action;
  updateBusyButtons();
}

function handleAction(event) {
  const action = event.currentTarget.dataset.action;
  const id = event.currentTarget.dataset.id;

  if (action === 'profile-select') {
    if (state.selectedProfileId === id) return;
    state.selectedProfileId = id;
    renderLiveDataOnly();
    return;
  }
  if (action === 'tunnel-select') {
    if (state.selectedTunnelId === id) return;
    state.selectedTunnelId = id;
    renderLiveDataOnly();
    return;
  }

  const actions = {
    refresh: () => manualRefresh(),
    'start-all': () => {
      markBusy('start-all');
      return runAction(async () => reportStartAll(await StartAll()), '已执行全部启动');
    },
    'stop-all': () => {
      markBusy('stop-all');
      return runAction(() => StopAll(), '已执行全部停止');
    },
    'tunnel-start': () => selectedTunnelAction(StartTunnel, '隧道已启动'),
    'tunnel-stop': () => selectedTunnelAction(StopTunnel, '隧道已停止'),
    'profile-new': () => openProfileModal(),
    'profile-edit': () => openProfileModal(state.selectedProfileId),
    'profile-delete': () => deleteSelectedProfile(),
    'tunnel-new': () => openTunnelModal(),
    'tunnel-edit': () => openTunnelModal(state.selectedTunnelId),
    'tunnel-delete': () => deleteSelectedTunnel(),
    'modal-close': () => closeModal(),
    'modal-save': () => saveModal(event),
  };
  actions[action]?.();
}

function handleContextAction(event) {
  event.preventDefault();
  const action = event.currentTarget.dataset.contextAction;
  const id = event.currentTarget.dataset.id;
  if (!id) return;

  if (action === 'profile-edit') {
    state.selectedProfileId = id;
    renderLiveDataOnly();
    openProfileModal(id);
    return;
  }

  if (action === 'tunnel-edit') {
    state.selectedTunnelId = id;
    renderLiveDataOnly();
    openTunnelModal(id);
  }
}

async function manualRefresh() {
  if (state.busyAction === 'refresh') return;
  const previousBusyAction = state.busyAction;
  if (!previousBusyAction) {
    markBusy('refresh');
  }
  try {
    await waitForRefreshIdle();
    const ok = await refresh(false, true);
    if (ok) {
      showToast('刷新成功，已重新读取配置文件', 'success');
    }
  } finally {
    if (!previousBusyAction) {
      state.busyAction = '';
      updateBusyButtons();
    }
  }
}

function handleDoubleAction(event) {
  const action = event.currentTarget.dataset.dblAction;
  const id = event.currentTarget.dataset.id;
  if (action !== 'tunnel-toggle' || !id) return;

  state.selectedTunnelId = id;
  const item = state.data.tunnels.find((row) => row.id === id);
  if (!item) return;
  if (item.running || item.state === 'starting') {
    markBusy('row-toggle');
    runAction(() => StopTunnel(id), '隧道已停止');
  } else {
    markBusy('row-toggle');
    runAction(() => StartTunnel(id), '隧道已启动');
  }
}

function selectedTunnelAction(fn, message) {
  if (!state.selectedTunnelId) {
    showToast('请选择隧道', 'error');
    return;
  }
  markBusy(fn === StartTunnel ? 'tunnel-start' : 'tunnel-stop');
  return runAction(() => fn(state.selectedTunnelId), message);
}

function reportStartAll(result) {
  const failed = Object.entries(result).filter(([, value]) => value !== 'ok');
  if (failed.length > 0) {
    throw new Error(failed.map(([id, value]) => `${id}: ${value}`).join('\n'));
  }
}

function openProfileModal(id = '') {
  const profile = id ? profileConfig(id) : null;
  if (id && !profile) {
    showToast('请选择 SSH Profile', 'error');
    return;
  }
  state.modal = {
    type: 'profile',
    mode: id ? 'edit' : 'new',
    originalId: id,
    data: profile
      ? structuredClone(profile)
      : {
          id: '',
          name: '',
          host: '',
          port: 22,
          username: '',
          auth: { type: 'key', password: '', keyPath: '', passphrase: '' },
          hostKeyPolicy: 'accept-new',
          knownHostsPath: '',
          connectTimeoutSeconds: 8,
          keepAliveSeconds: 30,
        },
  };
  render();
}

function openTunnelModal(id = '') {
  if (state.data.config.profiles.length === 0) {
    showToast('请先创建 SSH Profile', 'error');
    return;
  }
  const tunnel = id ? tunnelConfig(id) : null;
  const status = id ? state.data.tunnels.find((item) => item.id === id) : null;
  if (id && !tunnel) {
    showToast('请选择隧道', 'error');
    return;
  }
  if (status && ['starting', 'running', 'stopping'].includes(status.state)) {
    showToast('运行中的隧道不可修改，请先停止隧道', 'error');
    return;
  }
  state.modal = {
    type: 'tunnel',
    mode: id ? 'edit' : 'new',
    originalId: id,
    data: tunnel
      ? structuredClone(tunnel)
      : {
          id: '',
          name: '',
          profileId: state.data.config.profiles[0].id,
          localHost: '127.0.0.1',
          localPort: 8080,
          targetHost: '127.0.0.1',
          targetPort: 80,
          autoStart: false,
        },
  };
  render();
}

function closeModal() {
  state.modal = null;
  render();
}

function saveModal(event) {
  event.preventDefault();
  const form = app.querySelector('.modal');
  clearFieldErrors();
  const data = new FormData(form);
  const type = form.dataset.form;
  if (type === 'profile') {
    const profile = {
      id: data.get('id'),
      name: data.get('name'),
      host: data.get('host'),
      port: Number(data.get('port')),
      username: data.get('username'),
      auth: {
        type: data.get('authType'),
        password: data.get('password'),
        keyPath: data.get('keyPath'),
        passphrase: data.get('passphrase'),
      },
      hostKeyPolicy: data.get('hostKeyPolicy'),
      knownHostsPath: data.get('knownHostsPath'),
      connectTimeoutSeconds: Number(data.get('connectTimeoutSeconds')),
      keepAliveSeconds: Number(data.get('keepAliveSeconds')),
    };
    const error = validateProfile(profile);
    if (error) {
      setFieldError(error.field, error.message);
      return false;
    }
    markBusy('modal-save');
    return runAction(
      () => SaveProfile({ originalId: state.modal.originalId, profile }),
      'Profile 已保存',
    ).then((ok) => {
      if (ok) closeModal();
    });
  }

  const tunnel = {
    id: data.get('id'),
    name: data.get('name'),
    profileId: data.get('profileId'),
    localHost: data.get('localHost'),
    localPort: Number(data.get('localPort')),
    targetHost: data.get('targetHost'),
    targetPort: Number(data.get('targetPort')),
    autoStart: data.get('autoStart') === 'on',
  };
  const error = validateTunnel(tunnel);
  if (error) {
    setFieldError(error.field, error.message);
    return false;
  }
  markBusy('modal-save');
  return runAction(
    () => SaveTunnel({ originalId: state.modal.originalId, tunnel }),
    '隧道已保存',
  ).then((ok) => {
    if (ok) closeModal();
  });
}

function validateProfile(profile) {
  if (!profile.name.trim()) return { field: 'name', message: '请填写 SSH Profile 名称' };
  if (!profile.host.trim()) return { field: 'host', message: '请填写 SSH 主机' };
  if (!profile.username.trim()) return { field: 'username', message: '请填写用户名' };
  if (!profile.port || profile.port < 1 || profile.port > 65535) return { field: 'port', message: 'SSH 端口必须在 1-65535 之间' };
  if (profile.auth.type === 'password' && !profile.auth.password) return { field: 'password', message: 'password 认证方式需要填写密码' };
  if (profile.auth.type === 'key' && !profile.auth.keyPath.trim()) return { field: 'keyPath', message: 'key 认证方式需要填写私钥路径' };
  if (!profile.connectTimeoutSeconds || profile.connectTimeoutSeconds < 1 || profile.connectTimeoutSeconds > 120) {
    return { field: 'connectTimeoutSeconds', message: '连接超时必须在 1-120 秒之间' };
  }
  if (!profile.keepAliveSeconds || profile.keepAliveSeconds < 5 || profile.keepAliveSeconds > 600) {
    return { field: 'keepAliveSeconds', message: '保活间隔必须在 5-600 秒之间' };
  }
  return null;
}

function validateTunnel(tunnel) {
  if (!tunnel.name.trim()) return { field: 'name', message: '请填写隧道名称' };
  if (!tunnel.profileId) return { field: 'profileId', message: '请选择 SSH Profile' };
  if (!tunnel.localHost.trim()) return { field: 'localHost', message: '请填写本地地址' };
  if (!tunnel.localPort || tunnel.localPort < 1 || tunnel.localPort > 65535) return { field: 'localPort', message: '本地端口必须在 1-65535 之间' };
  if (!tunnel.targetHost.trim()) return { field: 'targetHost', message: '请填写目标地址' };
  if (!tunnel.targetPort || tunnel.targetPort < 1 || tunnel.targetPort > 65535) return { field: 'targetPort', message: '目标端口必须在 1-65535 之间' };
  return null;
}

function deleteSelectedProfile() {
  if (!state.selectedProfileId) {
    showToast('请选择 SSH Profile', 'error');
    return;
  }
  markBusy('profile-delete');
  return runAction(() => DeleteProfile(state.selectedProfileId), 'Profile 已删除');
}

function deleteSelectedTunnel() {
  if (!state.selectedTunnelId) {
    showToast('请选择隧道', 'error');
    return;
  }
  markBusy('tunnel-delete');
  return runAction(() => DeleteTunnel(state.selectedTunnelId), '隧道已删除');
}

function startRealtimeRefresh() {
  if (state.realtimeTimer) {
    window.clearInterval(state.realtimeTimer);
  }
  state.realtimeTimer = window.setInterval(() => {
    if (document.hidden) return;
    refresh(true);
  }, REALTIME_REFRESH_MS);
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) refresh(true);
  });
  window.addEventListener('focus', () => refresh(true));
}

render();
window.setTimeout(() => refresh(), 100);
startRealtimeRefresh();
