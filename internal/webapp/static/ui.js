const state = { envs: [], env: null, apps: [], assets: [], assetOpenDirs: new Set(), inventory: null, buildPreview: null, buildPreviewPath: null, buildPreviewContent: null, buildPreviewOpenDirs: new Set(), buildChecks: {}, clusterStatusEnabled: false, clusterStatus: null, clusterStatusLoading: false, git: null, appFile: null, appContentHash: null, appReferences: null, sidecarEnvOverride: null, sidecarResourcesOverride: null, sidecarStartupOverride: null, defaults: null, defaultsContentHash: null, defaultsContainerProfile: null, defaultsSidecarDefinition: null, defaultsSidecarStartup: null, defaultsSidecarResources: null, defaultsSidecarEnvs: null, defaultsSidecarReferences: null, appView: 'effective', inspectedApp: null, inspection: null, inspectionEnv: null, inspectionError: '', assetPath: null, active: 'dashboard', specialValueRow: null, changedGroup: 'all', changedStatus: 'all', changedSelected: new Set(), changedExpanded: new Set(), changedDiffs: new Map() };
const qs = (s) => document.querySelector(s);
const qsa = (s) => Array.from(document.querySelectorAll(s));
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const compactTextValue = (s) => String(s ?? '').replace(/\r?\n/g, '\\n');
async function apiErrorMessage(response) {
  const body = await response.text();
  try {
    const payload = JSON.parse(body);
    if (payload?.error) return `${response.status} ${response.statusText}: ${payload.error}`;
  } catch (_) {}
  return `${response.status} ${response.statusText}: ${body}`;
}
const appBasePath = document.body?.dataset.basePath === '/' ? '' : (document.body?.dataset.basePath || '').replace(/\/$/, '');
const appURL = (url) => url.startsWith('/') ? `${appBasePath}${url}` : url;
const api = async (url) => { const r = await fetch(appURL(url), { credentials: 'same-origin' }); if (!r.ok) throw new Error(await apiErrorMessage(r)); return await r.json(); };
const apiPost = async (url) => { const r = await fetch(appURL(url), { method: 'POST', credentials: 'same-origin' }); if (!r.ok) throw new Error(await apiErrorMessage(r)); return await r.json(); };
const apiPostJSON = async (url, payload) => { const r = await fetch(appURL(url), { method: 'POST', credentials: 'same-origin', headers: {'Content-Type':'application/json'}, body: JSON.stringify(payload || {}) }); if (!r.ok) throw new Error(await apiErrorMessage(r)); return await r.json(); };
const apiPatch = async (url, payload) => { const r = await fetch(appURL(url), { method: 'PATCH', credentials: 'same-origin', headers: {'Content-Type':'application/json'}, body: JSON.stringify(payload) }); if (!r.ok) throw new Error(await apiErrorMessage(r)); return await r.json(); };
function showError(e) { const el = qs('#ui-error'); el.textContent = String(e); el.classList.remove('hidden'); }
function clearError() { const el = qs('#ui-error'); el.textContent = ''; el.classList.add('hidden'); }
function setText(id, value) { const el = qs(id); if (el) el.textContent = value ?? '-'; }
function setHTML(id, value) { const el = qs(id); if (el) el.innerHTML = value ?? ''; }
async function copyTextToClipboard(text) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(String(text ?? ''));
    return;
  }
  const textarea = document.createElement('textarea');
  textarea.value = String(text ?? '');
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand('copy');
  textarea.remove();
}
function openModalElement(modalEl, focusEl, onClosed = null) {
  if (!modalEl) return () => {};
  const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const backdrop = document.createElement('div');
  backdrop.className = 'modal-backdrop fade show';
  const openDepth = qsa('.modal.show').length;
  const backdropZIndex = 1050 + openDepth * 20;
  const modalZIndex = backdropZIndex + 5;
  backdrop.style.zIndex = String(backdropZIndex);
  modalEl.style.zIndex = String(modalZIndex);
  const closeButtons = Array.from(modalEl.querySelectorAll('[data-modal-close], [data-bs-dismiss="modal"], .btn-close'));
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    modalEl.classList.remove('show');
    modalEl.style.display = 'none';
    modalEl.style.removeProperty('z-index');
    modalEl.setAttribute('aria-hidden', 'true');
    if (!qs('.modal.show')) {
      document.body.classList.remove('modal-open');
      document.body.style.removeProperty('overflow');
    }
    backdrop.removeEventListener('click', onBackdropClick);
    document.removeEventListener('keydown', onKeydown);
    closeButtons.forEach((button) => button.removeEventListener('click', close));
    backdrop.remove();
    onClosed?.();
    if (previouslyFocused?.isConnected) previouslyFocused.focus();
  };
  const onKeydown = (e) => {
    if (e.key === 'Escape' && isTopModal(modalEl)) close();
  };
  const onBackdropClick = () => {
    if (isTopModal(modalEl)) close();
  };
  backdrop.addEventListener('click', onBackdropClick);
  document.addEventListener('keydown', onKeydown);
  closeButtons.forEach((button) => button.addEventListener('click', close));
  document.body.appendChild(backdrop);
  document.body.classList.add('modal-open');
  document.body.style.overflow = 'hidden';
  modalEl.style.display = 'block';
  modalEl.removeAttribute('aria-hidden');
  modalEl.classList.add('show');
  (focusEl || modalEl.querySelector('button, input, textarea, select'))?.focus();
  return close;
}
function isTopModal(modalEl) {
  const shown = qsa('.modal.show');
  if (!shown.length) return false;
  return shown.reduce((top, item) => Number(item.style.zIndex || 0) > Number(top.style.zIndex || 0) ? item : top, shown[0]) === modalEl;
}
function confirmAction({title, body, subject = '', confirmLabel = 'Confirm', confirmClass = 'btn-warning', icon = 'ti-alert-triangle', statusClass = 'bg-warning'} = {}) {
  const modalEl = qs('#confirm-modal');
  if (!modalEl) return Promise.resolve(false);
  setText('#confirm-modal-title', title || 'Confirm action');
  setText('#confirm-modal-body', body || '');
  setText('#confirm-modal-subject', subject);
  qs('#confirm-modal-subject')?.classList.toggle('hidden', !subject);
  const iconEl = qs('#confirm-modal-icon');
  if (iconEl) iconEl.className = `ti ${icon} icon mb-2 ${modalToneClass(confirmClass)} icon-lg`;
  const statusEl = modalEl.querySelector('.modal-status');
  if (statusEl) statusEl.className = `modal-status ${statusClass}`;
  const confirm = qs('#confirm-modal-confirm');
  if (confirm) {
    confirm.textContent = confirmLabel;
    confirm.className = `btn ${confirmClass} w-100`;
  }
  return new Promise((resolve) => {
    let confirmed = false;
    let settled = false;
    let closeModal = () => {};
    const finish = () => {
      if (settled) return;
      settled = true;
      confirm?.removeEventListener('click', onConfirm);
      resolve(confirmed);
    };
    const onConfirm = () => {
      confirmed = true;
      closeModal();
    };
    confirm?.addEventListener('click', onConfirm);
    closeModal = openModalElement(modalEl, confirm, finish);
  });
}
function modalToneClass(buttonClass) {
  if (String(buttonClass || '').includes('danger')) return 'text-danger';
  if (String(buttonClass || '').includes('primary')) return 'text-primary';
  if (String(buttonClass || '').includes('success')) return 'text-success';
  return 'text-warning';
}
function openContentModal({
  modal,
  titleSelector,
  subtitleSelector,
  bodySelector,
  title,
  subtitle = '',
  body,
  focusSelector = 'button, input, textarea, select',
  wide = false,
  onClosed = null,
} = {}) {
  if (!modal || body === undefined || body === null) return () => {};
  setText(titleSelector, title || '');
  setText(subtitleSelector, subtitle || '');
  const subtitleEl = qs(subtitleSelector);
  subtitleEl?.classList.toggle('hidden', !subtitle);
  setHTML(bodySelector, body);
  modal.classList.toggle('modal-wide', !!wide);
  return openModalElement(modal, modal.querySelector(focusSelector), () => {
    modal.classList.remove('modal-wide');
    onClosed?.();
  });
}
function emptyState(icon, title, text = '') {
  return `<div class="empty-state-lite"><i class="ti ${icon}"></i><div><div class="fw-semibold">${esc(title)}</div>${text ? `<div class="text-muted small">${esc(text)}</div>` : ''}</div></div>`;
}
function setActive(page) {
  state.active = page;
  for (const link of qsa('[data-nav]')) {
    const active = link.dataset.nav === page;
    link.classList.toggle('active', active);
    link.closest('.nav-item')?.classList.toggle('active', active);
  }
  for (const pageEl of qsa('[data-page]')) pageEl.classList.toggle('hidden', pageEl.dataset.page !== page);
  setText('#page-pretitle', state.env ? `Environment ${state.env}` : 'Environment repository');
  setText('#page-title', page === 'dashboard' ? 'Select environment' : page === 'changes' ? 'Changed files' : page === 'apps' ? 'Inspect applications' : page === 'defaults' ? 'Inspect defaults' : page === 'assets' ? 'Inspect assets' : 'Build preview');
  if (page === 'defaults') loadInspection();
  if (page === 'build') loadBuildData();
}
function routeHash({env = state.env, page = state.active, app = state.appFile, asset = state.assetPath} = {}) {
  const params = new URLSearchParams();
  if (env) params.set('env', env);
  if (page) params.set('page', page);
  if (page === 'apps' && app) params.set('app', app);
  if (page === 'assets' && asset) params.set('asset', asset);
  return `#${params.toString()}`;
}
function pushRoute(route = {}) {
  const hash = routeHash(route);
  if (window.location.hash !== hash) window.history.pushState(null, '', hash);
}
function replaceRoute(route = {}) {
  const hash = routeHash(route);
  if (window.location.hash !== hash) window.history.replaceState(null, '', hash);
}
function parseRouteHash() {
  const raw = window.location.hash.startsWith('#') ? window.location.hash.slice(1) : '';
  const params = new URLSearchParams(raw);
  return {
    env: params.get('env') || '',
    page: params.get('page') || '',
    app: params.get('app') || '',
    asset: params.get('asset') || '',
  };
}
async function restoreRouteFromHash() {
  if (!state.envs.length) return;
  const route = parseRouteHash();
  const env = state.envs.some((item) => item.name === route.env) ? route.env : (state.env || localStorage.getItem('activeEnv') || state.envs[0].name);
  if (env !== state.env) await selectEnv(env, {updateRoute: false});
  const page = ['dashboard', 'changes', 'apps', 'defaults', 'assets', 'build'].includes(route.page) ? route.page : state.active;
  setActive(page || 'dashboard');
  if (page === 'apps' && route.app) await selectApp(route.app, {updateRoute: false});
  if (page === 'assets' && route.asset) await selectAsset(route.asset, {updateRoute: false});
  replaceRoute({env: state.env, page: state.active, app: state.appFile, asset: state.assetPath});
}
function applyTheme(theme) {
  if (theme === 'auto') theme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  document.documentElement.setAttribute('data-bs-theme', theme);
}
function setupTheme() {
  const saved = localStorage.getItem('theme') || 'auto';
  applyTheme(saved);
  qs('#theme-toggle')?.addEventListener('click', (e) => { e.preventDefault(); qs('#theme-menu')?.classList.toggle('show'); });
  for (const item of qsa('[data-theme]')) item.addEventListener('click', (e) => { e.preventDefault(); const t = item.dataset.theme || 'auto'; localStorage.setItem('theme', t); applyTheme(t); qs('#theme-menu')?.classList.remove('show'); });
}
async function init() {
  setupTheme();
  window.addEventListener('hashchange', () => restoreRouteFromHash());
  qsa('[data-nav]').forEach((x) => x.addEventListener('click', (e) => {
    e.preventDefault();
    if (x.dataset.nav !== 'dashboard' && !state.env) return showError('Select environment first.');
    setActive(x.dataset.nav);
    pushRoute({page: x.dataset.nav});
  }));
  qs('#refresh-btn')?.addEventListener('click', () => refreshCurrentView());
  qs('#apps-filter')?.addEventListener('input', () => renderApps());
  qs('#defaults-filter')?.addEventListener('input', () => renderDefaultsOverview());
  qs('#defaults-open-source')?.addEventListener('click', () => openDefaultsSource());
  qs('#app-view-effective')?.addEventListener('click', () => setAppView('effective'));
  qs('#app-view-local')?.addEventListener('click', () => setAppView('local'));
  qs('#assets-filter')?.addEventListener('input', () => renderAssets());
  qs('#build-validate-btn')?.addEventListener('click', () => runBuildValidate());
  qs('#build-refresh-btn')?.addEventListener('click', () => loadBuildData());
  qs('#cluster-refresh-btn')?.addEventListener('click', () => loadClusterStatus({force: true}));
  qs('#build-preview-btn')?.addEventListener('click', () => loadBuildPreview());
  qs('#build-preview-tree')?.addEventListener('click', (e) => handleBuildPreviewTreeClick(e));
  qs('#build-preview-filter')?.addEventListener('input', () => renderBuildPreview(state.buildPreview));
  qs('#build-preview-expand-btn')?.addEventListener('click', () => expandBuildPreviewTree());
  qs('#build-preview-collapse-btn')?.addEventListener('click', () => collapseBuildPreviewTree());
  qs('#build-preview-copy-path')?.addEventListener('click', () => copyBuildPreviewPath());
  qs('#build-preview-copy-content')?.addEventListener('click', () => copyBuildPreviewContent());
  qs('#accept-changes-confirm')?.addEventListener('click', () => acceptSelectedChanges());
  qs('#inventory-filter')?.addEventListener('input', () => renderInventory());
  qs('#inventory-path')?.addEventListener('input', () => renderInventory());
  qs('#inventory-clear-btn')?.addEventListener('click', () => {
    const filter = qs('#inventory-filter');
    const path = qs('#inventory-path');
    if (filter) filter.value = '';
    if (path) path.value = '';
    renderInventory();
  });
  qs('#app-vars-save-btn')?.addEventListener('click', () => saveAppVars());
  qs('#app-replicas-save-btn')?.addEventListener('click', () => saveAppReplicas());
  qs('#app-overview')?.addEventListener('change', (e) => {
    const enabled = e.target.closest('[data-autoscaling-enabled]');
    if (enabled && !state.readOnly) syncAutoscalingEditor();
  });
  qs('#app-vars-add-btn')?.addEventListener('click', () => addAppVarRow());
  qs('#app-vars')?.addEventListener('click', (e) => {
    const button = e.target.closest('[data-remove-app-var]');
    if (!button || state.readOnly) return;
    button.closest('tr')?.remove();
    syncAppVarsEmptyState();
  });
  qs('#app-overview')?.addEventListener('click', (e) => {
    const advanced = e.target.closest('[data-toggle-effective-advanced]');
    if (advanced) return toggleEffectiveAdvanced(advanced);
    const dependency = e.target.closest('[data-dependency-kind]');
    if (dependency) return openDependencyDefinition(dependency.dataset.dependencyKind, dependency.dataset.dependencyName);
    const sidecarEnv = e.target.closest('[data-edit-sidecar-env]');
    if (sidecarEnv && !state.readOnly) return openSidecarEnvOverride(sidecarEnv.dataset.sidecarName, sidecarEnv.dataset.envName, sidecarEnv.dataset.effectiveValue);
    const sidecarResources = e.target.closest('[data-edit-sidecar-resources]');
    if (sidecarResources && !state.readOnly) return openSidecarResourcesOverride(sidecarResources.dataset.sidecarName);
    const sidecarStartup = e.target.closest('[data-edit-sidecar-startup]');
    if (sidecarStartup && !state.readOnly) return openSidecarStartupOverride(sidecarStartup.dataset.sidecarName);
    const saveResources = e.target.closest('[data-save-resources]');
    if (saveResources && !state.readOnly) return saveContainerResources(Number(saveResources.dataset.saveResources));
    const saveVars = e.target.closest('[data-save-container-envs]');
    if (saveVars && !state.readOnly) return saveContainerEnvs(Number(saveVars.dataset.saveContainerEnvs));
    const saveRuntime = e.target.closest('[data-save-runtime]');
    if (saveRuntime && !state.readOnly) return saveContainerRuntime(Number(saveRuntime.dataset.saveRuntime));
    const saveProbes = e.target.closest('[data-save-probes]');
    if (saveProbes && !state.readOnly) return saveContainerProbes(Number(saveProbes.dataset.saveProbes));
    const saveAutoscaling = e.target.closest('[data-save-autoscaling]');
    if (saveAutoscaling && !state.readOnly) return saveAppAutoscaling();
    const editPanel = e.target.closest('[data-edit-panel]');
    if (editPanel && !state.readOnly) return openEditPanel(editPanel.dataset.editPanel, Number(editPanel.dataset.containerIndex));
    const fixLegacyProbes = e.target.closest('[data-fix-legacy-probes]');
    if (fixLegacyProbes && !state.readOnly) return fixContainerLegacyProbes(Number(fixLegacyProbes.dataset.fixLegacyProbes));
    const addVar = e.target.closest('[data-add-container-env]');
    if (addVar && !state.readOnly) return addContainerEnvRow(Number(addVar.dataset.addContainerEnv));
    const editPorts = e.target.closest('[data-edit-ports]');
    if (editPorts && !state.readOnly) return openPortsEditorModal(Number(editPorts.dataset.editPorts));
    const removeVar = e.target.closest('[data-remove-container-env]');
    if (removeVar && !state.readOnly) {
      const index = Number(removeVar.dataset.removeContainerEnv);
      removeVar.closest('tr')?.remove();
      syncContainerEnvsEmptyState(index);
    }
  });
  qs('#asset-structured')?.addEventListener('click', (e) => handleAssetStructuredClick(e));
  qs('#defaults-overview')?.addEventListener('click', async (e) => {
    if (e.target.closest('[data-edit-defaults-vars]') && !state.readOnly) return openDefaultsVarsEditor();
    if (e.target.closest('[data-edit-defaults-container-envs]') && !state.readOnly) return openDefaultsContainerEnvsEditor();
    const editProfile = e.target.closest('[data-edit-container-profile]');
    if (editProfile && !state.readOnly) return openDefaultsContainerProfileEditor(editProfile.dataset.editContainerProfile);
    const editReferences = e.target.closest('[data-edit-sidecar-definition-references]');
    if (editReferences && !state.readOnly) return openDefaultsSidecarReferencesEditor(editReferences.dataset.editSidecarDefinitionReferences);
    const editEnvs = e.target.closest('[data-edit-sidecar-definition-envs]');
    if (editEnvs && !state.readOnly) return openDefaultsSidecarEnvsEditor(editEnvs.dataset.editSidecarDefinitionEnvs);
    const editResources = e.target.closest('[data-edit-sidecar-definition-resources]');
    if (editResources && !state.readOnly) return openDefaultsSidecarResourcesEditor(editResources.dataset.editSidecarDefinitionResources);
    const editStartup = e.target.closest('[data-edit-sidecar-definition-startup]');
    if (editStartup && !state.readOnly) return openDefaultsSidecarStartupEditor(editStartup.dataset.editSidecarDefinitionStartup);
    const editSidecar = e.target.closest('[data-edit-sidecar-definition]');
    if (editSidecar && !state.readOnly) return openDefaultsSidecarDefinitionEditor(editSidecar.dataset.editSidecarDefinition);
    const app = e.target.closest('[data-defaults-app]');
    if (app) {
      setActive('apps');
      return selectApp(app.dataset.defaultsApp);
    }
    const asset = e.target.closest('[data-defaults-asset]');
    if (asset) {
      setActive('assets');
      return selectAsset(asset.dataset.defaultsAsset);
    }
  });
  qs('#asset-structured')?.addEventListener('input', (e) => handleAssetStructuredInput(e));
  qs('#ports-edit-modal')?.addEventListener('click', (e) => handlePortsModalClick(e));
  qs('#edit-modal')?.addEventListener('click', (e) => handleEditModalClick(e));
  qs('#edit-modal')?.addEventListener('input', (e) => handleEditModalInput(e));
  qs('#edit-modal')?.addEventListener('change', (e) => handleEditModalInput(e));
  qs('#value-edit-apply')?.addEventListener('click', () => applySpecialValueDialog());
  window.setInterval(() => {
    if (state.clusterStatusEnabled && state.env && state.active === 'build') loadClusterStatus();
  }, 15000);
  await loadAll();
}
async function loadAll() {
  clearError();
  try {
    const info = await api('/api/v1/info');
    state.readOnly = !!info.read_only;
    state.clusterStatusEnabled = !!info.cluster_status?.enabled;
    document.body.classList.toggle('read-only-mode', state.readOnly);
    qs('#cluster-status-card')?.classList.toggle('hidden', !state.clusterStatusEnabled);
    setText('#app-version', formatAppVersion(info));
    qs('#read-only-badge').classList.toggle('hidden', !info.read_only);
    qs('#write-mode-badge')?.classList.toggle('hidden', !!info.read_only);
    qs('#read-only-hint')?.classList.toggle('hidden', !info.read_only);
    await refreshRepositorySnapshot();
    if (state.envs.length) {
      const route = parseRouteHash();
      if (route.env || route.page || route.app || route.asset) await restoreRouteFromHash();
      else await selectEnv(state.env || localStorage.getItem('activeEnv') || state.envs[0].name);
    }
  } catch (e) { showError(e); }
}
async function refreshRepositorySnapshot() {
  const [git, data] = await Promise.all([
    api('/api/v1/git/status'),
    api('/api/v1/envs'),
  ]);
  state.git = git;
  renderGitStatus();
  state.envs = data.items || [];
  state.inspection = null;
  state.inspectionEnv = null;
  state.inspectionError = '';
  setText('#repo-root', data.root || '-');
  setText('#env-count', state.envs.length);
  renderEnvs();
  renderEnvChanges();
}
async function refreshCurrentView() {
  clearError();
  try {
    await refreshRepositorySnapshot();
    if (state.env) {
      await Promise.all([loadApps(), loadAssets(), loadInspection(true)]);
      if (state.active === 'apps' && state.appFile) await selectApp(state.appFile);
      if (state.active === 'assets' && state.assetPath) await selectAsset(state.assetPath);
      if (state.active === 'build') resetBuildView();
    }
  } catch (e) { showError(e); }
}
function renderGitStatus() {
  const badge = qs('#git-status-badge');
  if (!badge) return;
  const git = state.git || {};
  if (!git.available) {
    badge.className = 'badge bg-secondary-lt';
    badge.innerHTML = '<i class="ti ti-git-branch me-1"></i>no git';
    return;
  }
  badge.className = `badge ${git.dirty ? 'bg-yellow-lt' : 'bg-green-lt'}`;
  const branch = git.branch || git.commit || 'detached';
  badge.innerHTML = `<i class="ti ti-git-branch me-1"></i>${esc(branch)}${git.dirty ? ` · ${git.dirty_count}` : ''}`;
  renderBuildWorkflow();
}
function formatAppVersion(info) {
  const version = info?.version || 'dev';
  const commit = info?.commit || '';
  if (!commit || commit === 'unknown') return version === 'dev' ? 'dev build' : version;
  return `${version} / ${commit}`;
}
function gitFilesForEnv(env) {
  const prefix = `${env}/`;
  return (state.git?.files || []).filter((file) => file.path === env || file.path.startsWith(prefix));
}
function gitFile(path) {
  return (state.git?.files || []).find((file) => file.path === path);
}
function gitCodeLabel(code) {
  if (code === '??') return 'untracked';
  if (code === 'M') return 'modified';
  if (code === 'A') return 'added';
  if (code === 'D') return 'deleted';
  return code || 'dirty';
}
function dirtyBadge(file) {
  if (!file) return '';
  return `<span class="badge bg-yellow-lt ms-2"><i class="ti ti-alert-triangle me-1"></i>${esc(gitCodeLabel(file.code))}</span>`;
}
async function selectEnv(env, options = {}) {
  if (!state.envs.some((x) => x.name === env)) env = state.envs[0]?.name;
  if (!env) return;
  const changedEnv = state.env !== env;
  if (changedEnv && !(await profileEditor.leave())) return;
  state.env = env; localStorage.setItem('activeEnv', env);
  if (changedEnv) resetSelectedDetails();
  renderEnvs();
  renderEnvChanges();
  await Promise.all([loadApps(), loadAssets(), loadInspection()]);
  const hasChanges = gitFilesForEnv(env).length > 0;
  setActive(state.active === 'dashboard' ? (hasChanges ? 'changes' : 'apps') : state.active);
  if (options.updateRoute !== false) pushRoute({env, page: state.active});
}
function renderEnvs() {
  const host = qs('#env-list');
  if (!state.envs.length) { host.innerHTML = `<div class="col-12">${emptyState('ti-stack-2', 'No environments found', 'Check --root or ENVIRONMENTS_ROOT.')}</div>`; return; }
  host.innerHTML = state.envs.map((e) => `
    <div class="col-sm-6 col-xl-4">
      <a href="#" class="card env-card text-reset text-decoration-none h-100 ${state.env === e.name ? 'border-primary' : ''}" data-env="${esc(e.name)}">
        <div class="card-body">
          <div class="d-flex align-items-start justify-content-between gap-3">
            <div><div class="text-uppercase text-muted small fw-semibold">Environment</div><h3 class="mb-1">${esc(e.name)}</h3><div class="text-muted small font-monospace text-break">${esc(e.path)}</div></div>
            <span class="avatar bg-blue-lt text-blue"><i class="ti ti-stack-2"></i></span>
          </div>
          <div class="mt-3 d-flex flex-wrap gap-2">
            <span class="badge bg-blue-lt">apps ${e.app_files_count ?? 0}</span>
            <span class="badge bg-cyan-lt">assets ${e.asset_files_count ?? 0}</span>
            <span class="badge ${e.has_env_secured_json ? 'bg-yellow-lt' : 'bg-secondary-lt'}">secured ${e.has_env_secured_json ? 'yes' : 'no'}</span>
            ${e.is_dirty ? `<span class="badge bg-yellow-lt"><i class="ti ti-alert-triangle me-1"></i>dirty ${gitFilesForEnv(e.name).length || ''}</span>` : ''}
          </div>
        </div>
      </a>
    </div>`).join('');
  qsa('[data-env]').forEach((x) => x.addEventListener('click', (e) => { e.preventDefault(); selectEnv(x.dataset.env); }));
  renderEnvChanges();
}
function renderEnvChanges() {
  const list = qs('#env-changes-list');
  const navItem = qs('#changes-nav-item');
  const count = qs('#changes-count');
  if (!list || !state.env) return;
  const files = gitFilesForEnv(state.env);
  const filePaths = new Set(files.map((file) => file.path));
  state.changedSelected = new Set(Array.from(state.changedSelected).filter((path) => filePaths.has(path)));
  state.changedExpanded = new Set(Array.from(state.changedExpanded).filter((path) => filePaths.has(path)));
  if (navItem) navItem.classList.toggle('hidden', files.length === 0);
  if (count) count.textContent = String(files.length);
  list.innerHTML = files.length ? renderChangedFilesReview(files) : `<div class="list-group-item">${emptyState('ti-git-compare', 'No changed files', 'The selected environment matches the current Git working tree.')}</div>`;
  qsa('[data-dirty-path]').forEach((item) => item.addEventListener('click', (event) => {
    event.preventDefault();
    openDirtyPath(item.dataset.dirtyPath);
  }));
  qsa('[data-changes-group]').forEach((item) => item.addEventListener('click', () => {
    state.changedGroup = item.dataset.changesGroup || 'all';
    renderEnvChanges();
  }));
  qsa('[data-changes-status]').forEach((item) => item.addEventListener('change', () => {
    state.changedStatus = item.value || 'all';
    renderEnvChanges();
  }));
  qsa('[data-changed-select]').forEach((item) => item.addEventListener('change', () => {
    if (item.checked) state.changedSelected.add(item.dataset.changedSelect);
    else state.changedSelected.delete(item.dataset.changedSelect);
    syncChangedSelectionSummary();
  }));
  qsa('[data-changed-select-visible]').forEach((item) => item.addEventListener('click', () => selectVisibleChangedFiles(true)));
  qsa('[data-changed-clear-selection]').forEach((item) => item.addEventListener('click', () => selectVisibleChangedFiles(false)));
  qsa('[data-changed-expand-all]').forEach((item) => item.addEventListener('click', () => expandVisibleChangedDiffs()));
  qsa('[data-changed-collapse-all]').forEach((item) => item.addEventListener('click', () => collapseVisibleChangedDiffs()));
  qsa('[data-changed-toggle-diff]').forEach((item) => item.addEventListener('click', () => toggleChangedDiff(item.dataset.changedToggleDiff)));
  qsa('[data-accept-selected]').forEach((item) => item.addEventListener('click', () => openAcceptChangesModal()));
  qsa('[data-restore-path]').forEach((item) => item.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    restoreGitPath(item.dataset.restorePath, item.dataset.restoreCode);
  }));
  syncChangedSelectionSummary();
  state.changedExpanded.forEach((path) => loadChangedDiff(path));
}
function renderChangedFilesReview(files) {
  const groups = changedFileGroups(files);
  const visible = filterChangedFiles(files);
  const groupTabs = [{ label: 'All', key: 'all', icon: 'ti-list', count: files.length }, ...groups.map((group) => ({...group, count: group.items.length}))];
  const statusCounts = changedStatusCounts(files);
  return `
    <div class="changed-review">
      <div class="changed-toolbar">
        <div class="changed-tabs" role="tablist">
          ${groupTabs.map((tab) => `<button class="btn btn-sm ${state.changedGroup === tab.key ? 'btn-primary' : 'btn-outline-secondary'}" type="button" data-changes-group="${esc(tab.key)}"><i class="ti ${tab.icon} me-1"></i>${esc(tab.label)} <span class="badge ${state.changedGroup === tab.key ? 'bg-white text-primary' : 'bg-secondary-lt'} ms-1">${tab.count}</span></button>`).join('')}
        </div>
        <div class="changed-actions">
          <select class="form-select form-select-sm changed-status-filter" data-changes-status>
            ${[
              ['all', `All statuses (${files.length})`],
              ['changed', `Changed (${statusCounts.changed})`],
              ['untracked', `Untracked (${statusCounts.untracked})`],
              ['deleted', `Deleted (${statusCounts.deleted})`],
            ].map(([value, label]) => `<option value="${value}" ${state.changedStatus === value ? 'selected' : ''}>${esc(label)}</option>`).join('')}
          </select>
          <button class="btn btn-sm btn-outline-secondary" type="button" data-changed-select-visible><i class="ti ti-checks me-1"></i>Select visible</button>
          <button class="btn btn-sm btn-outline-secondary" type="button" data-changed-clear-selection><i class="ti ti-x me-1"></i>Clear</button>
          <button class="btn btn-sm btn-outline-primary" type="button" data-changed-expand-all><i class="ti ti-arrows-maximize me-1"></i>Expand all diffs</button>
          <button class="btn btn-sm btn-outline-secondary" type="button" data-changed-collapse-all><i class="ti ti-arrows-minimize me-1"></i>Collapse all</button>
          ${state.readOnly ? '' : '<button class="btn btn-sm btn-primary" type="button" data-accept-selected><i class="ti ti-git-commit me-1"></i>Accept selected</button>'}
        </div>
      </div>
      <div class="changed-summary">
        <span data-changed-selection-summary>0 selected</span>
        <span>${visible.length}/${files.length} visible</span>
        <span class="text-muted">Accept selected commits only checked files. Other dirty files remain untouched.</span>
      </div>
      <div class="changed-file-list">
        ${visible.length ? renderChangedFileGroups(visible) : emptyState('ti-filter-off', 'No matching changed files', 'Adjust group or status filters.')}
      </div>
    </div>`;
}
function changedFileGroups(files) {
  const groups = [
    { label: 'Apps', key: 'apps', icon: 'ti-apps', items: [] },
    { label: 'Assets', key: 'assets', icon: 'ti-folders', items: [] },
    { label: 'Env files', key: 'env', icon: 'ti-file-settings', items: [] },
    { label: 'Other', key: 'other', icon: 'ti-dots', items: [] },
  ];
  for (const file of files) groups[changedFileGroupIndex(file.path)].items.push(file);
  return groups;
}
function renderChangedFileGroups(files) {
  const groups = changedFileGroups(files);
  return groups.filter((group) => group.items.length).map((group) => `
    <div class="changed-group-header"><i class="ti ${group.icon} me-2"></i>${esc(group.label)} · ${group.items.length}</div>
    ${group.items.map(renderChangedFileRow).join('')}`).join('');
}
function filterChangedFiles(files) {
  return files.filter((file) => {
    if (state.changedGroup !== 'all' && changedFileGroupKey(file.path) !== state.changedGroup) return false;
    if (state.changedStatus !== 'all' && changedStatusKey(file.code) !== state.changedStatus) return false;
    return true;
  });
}
function changedFileGroupIndex(path) {
  if (!state.env || !path.startsWith(`${state.env}/`)) return 3;
  const rest = path.slice(state.env.length + 1);
  if (rest.startsWith('apps/')) return 0;
  if (rest.startsWith('assets/') || rest.startsWith('assets.')) return 1;
  if (rest.startsWith('env.')) return 2;
  return 3;
}
function changedFileGroupKey(path) {
  return ['apps', 'assets', 'env', 'other'][changedFileGroupIndex(path)] || 'other';
}
function changedStatusKey(code) {
  if (code === '??') return 'untracked';
  if (String(code || '').includes('D')) return 'deleted';
  return 'changed';
}
function changedStatusCounts(files) {
  return files.reduce((counts, file) => {
    counts[changedStatusKey(file.code)] += 1;
    return counts;
  }, {changed: 0, untracked: 0, deleted: 0});
}
function renderChangedFileRow(file) {
  const target = dirtyNavigationTarget(file.path);
  const restore = state.readOnly ? '' : `<button class="btn btn-sm btn-outline-danger" type="button" data-restore-path="${esc(file.path)}" data-restore-code="${esc(file.code)}" title="Discard this change"><i class="ti ti-restore me-1"></i>Discard</button>`;
  const expanded = state.changedExpanded.has(file.path);
  const checked = state.changedSelected.has(file.path) ? 'checked' : '';
  return `
    <div class="changed-file-card" data-changed-file="${esc(file.path)}">
      <div class="changed-file-head">
        <label class="form-check m-0 changed-file-check">
          <input class="form-check-input" type="checkbox" data-changed-select="${esc(file.path)}" ${checked}>
        </label>
        <button class="btn btn-sm btn-ghost-secondary btn-icon changed-diff-toggle" type="button" data-changed-toggle-diff="${esc(file.path)}" title="${expanded ? 'Collapse diff' : 'Expand diff'}"><i class="ti ${expanded ? 'ti-chevron-down' : 'ti-chevron-right'}"></i></button>
        <a href="#" class="text-reset text-decoration-none min-w-0 ${target ? '' : 'disabled'}" data-dirty-path="${esc(file.path)}">
          <div class="font-monospace text-break fw-semibold">${esc(file.path)}</div>
          <div class="text-muted small">${esc(changedFileGroupKey(file.path))} · ${esc(gitCodeLabel(file.code))}</div>
        </a>
        <div class="changed-file-actions">
          <span class="badge ${changedStatusBadgeClass(file.code)}">${esc(gitCodeLabel(file.code))}</span>
          ${restore}
        </div>
      </div>
      <div class="changed-diff ${expanded ? '' : 'hidden'}" data-changed-diff="${esc(file.path)}">${renderChangedDiffContent(file.path)}</div>
    </div>`;
}
function changedStatusBadgeClass(code) {
  if (code === '??') return 'bg-cyan-lt';
  if (String(code || '').includes('D')) return 'bg-red-lt';
  return 'bg-yellow-lt';
}
function renderChangedDiffContent(path) {
  const cached = state.changedDiffs.get(path);
  if (cached?.loading) return `<div class="changed-diff-loading"><span class="spinner-border spinner-border-sm me-2"></span>Loading diff...</div>`;
  if (cached?.error) return `<div class="alert alert-danger mb-0">${esc(cached.error)}</div>`;
  if (cached?.content !== undefined) return `<pre class="code-block diff-block changed-diff-block">${renderDiff(cached.content || 'No textual diff available.')}</pre>`;
  return `<div class="changed-diff-loading text-muted">Diff not loaded.</div>`;
}
function visibleChangedFiles() {
  return filterChangedFiles(gitFilesForEnv(state.env));
}
function selectVisibleChangedFiles(selected) {
  visibleChangedFiles().forEach((file) => {
    if (selected) state.changedSelected.add(file.path);
    else state.changedSelected.delete(file.path);
  });
  renderEnvChanges();
}
function syncChangedSelectionSummary() {
  setText('[data-changed-selection-summary]', `${state.changedSelected.size} selected`);
}
function openAcceptChangesModal() {
  if (state.readOnly) return;
  const paths = selectedChangedPaths();
  if (!paths.length) return showError('Select at least one changed file first.');
  clearError();
  const filesHost = qs('#accept-changes-files');
  if (filesHost) filesHost.innerHTML = paths.map((path) => `<div>${esc(path)}</div>`).join('');
  const message = qs('#accept-changes-message');
  if (message) message.value = suggestedCommitMessage(paths);
  const validate = qs('#accept-changes-validate');
  if (validate) validate.checked = true;
  const error = qs('#accept-changes-error');
  if (error) {
    error.textContent = '';
    error.classList.add('hidden');
  }
  resetAcceptChangesResult();
  state.acceptChangesModalClose = openModalElement(qs('#accept-changes-modal'), message, () => {
    state.acceptChangesModalClose = null;
  });
}
function selectedChangedPaths() {
  const dirty = new Set(gitFilesForEnv(state.env).map((file) => file.path));
  return Array.from(state.changedSelected).filter((path) => dirty.has(path)).sort();
}
function suggestedCommitMessage(paths) {
  if (paths.length === 1) return `Accept changes in ${paths[0].split('/').pop()}`;
  return `Accept ${paths.length} selected changes`;
}
async function acceptSelectedChanges() {
  const paths = selectedChangedPaths();
  const message = qs('#accept-changes-message')?.value?.trim() || '';
  const error = qs('#accept-changes-error');
  const confirm = qs('#accept-changes-confirm');
  const validate = qs('#accept-changes-validate')?.checked;
  if (error) {
    error.textContent = '';
    error.classList.add('hidden');
  }
  if (!paths.length) return setAcceptChangesError('Select at least one changed file.');
  if (!message) return setAcceptChangesError('Commit message is required.');
  clearError();
  if (confirm) confirm.disabled = true;
  try {
    if (validate) {
      await apiPost(`/api/v1/envs/${encodeURIComponent(state.env)}/validate`);
    }
    const result = await apiPostJSON('/api/v1/git/commit', {paths, message});
    state.git = result.status;
    state.changedSelected.clear();
    state.changedExpanded.clear();
    state.changedDiffs.clear();
    renderAcceptChangesResult(result, message);
    await refreshCurrentView();
  } catch (e) {
    setAcceptChangesError(String(e));
  } finally {
    if (confirm && qs('#accept-changes-result')?.classList.contains('hidden')) confirm.disabled = false;
  }
}
function resetAcceptChangesResult() {
  const result = qs('#accept-changes-result');
  if (result) {
    result.innerHTML = '';
    result.classList.add('hidden');
  }
  const confirm = qs('#accept-changes-confirm');
  if (confirm) {
    confirm.disabled = false;
    confirm.classList.remove('hidden');
  }
  const cancel = qs('#accept-changes-cancel');
  if (cancel) cancel.textContent = 'Cancel';
  qs('#accept-changes-message')?.removeAttribute('disabled');
  qs('#accept-changes-validate')?.removeAttribute('disabled');
}
function renderAcceptChangesResult(result, message) {
  const resultEl = qs('#accept-changes-result');
  if (!resultEl) return;
  const paths = result.paths || [];
  resultEl.innerHTML = `
    <div class="d-flex align-items-start gap-2">
      <i class="ti ti-circle-check text-success mt-1"></i>
      <div>
        <div class="fw-semibold">Committed ${paths.length || 'selected'} file(s)</div>
        <div class="small mt-1">Commit <span class="font-monospace">${esc(result.commit || 'unknown')}</span>${message ? ` · ${esc(message)}` : ''}</div>
        <div class="accept-result-files font-monospace small mt-2">${paths.map((path) => `<div>${esc(path)}</div>`).join('')}</div>
      </div>
    </div>`;
  resultEl.classList.remove('hidden');
  const confirm = qs('#accept-changes-confirm');
  if (confirm) confirm.classList.add('hidden');
  const cancel = qs('#accept-changes-cancel');
  if (cancel) cancel.textContent = 'Close';
  qs('#accept-changes-message')?.setAttribute('disabled', 'disabled');
  qs('#accept-changes-validate')?.setAttribute('disabled', 'disabled');
}
function setAcceptChangesError(message) {
  const error = qs('#accept-changes-error');
  if (!error) return showError(message);
  error.textContent = message;
  error.classList.remove('hidden');
}
async function toggleChangedDiff(path) {
  if (!path) return;
  if (state.changedExpanded.has(path)) state.changedExpanded.delete(path);
  else state.changedExpanded.add(path);
  renderEnvChanges();
}
async function expandVisibleChangedDiffs() {
  visibleChangedFiles().forEach((file) => state.changedExpanded.add(file.path));
  renderEnvChanges();
}
function collapseVisibleChangedDiffs() {
  visibleChangedFiles().forEach((file) => state.changedExpanded.delete(file.path));
  renderEnvChanges();
}
async function loadChangedDiff(path) {
  if (!path || state.changedDiffs.has(path)) return;
  state.changedDiffs.set(path, {loading: true});
  const host = qs(`[data-changed-diff="${cssString(path)}"]`);
  if (host) host.innerHTML = renderChangedDiffContent(path);
  try {
    const encoded = path.split('/').map(encodeURIComponent).join('/');
    const diff = await api(`/api/v1/git/diff/${encoded}`);
    state.changedDiffs.set(path, {content: diff.content || diff.error || 'No textual diff available.'});
  } catch (e) {
    state.changedDiffs.set(path, {error: String(e)});
  }
  const target = qs(`[data-changed-diff="${cssString(path)}"]`);
  if (target) target.innerHTML = renderChangedDiffContent(path);
}
function cssString(value) {
  return String(value).replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}
function dirtyNavigationTarget(path) {
	if (!state.env || !path.startsWith(`${state.env}/`)) return null;
	const rest = path.slice(state.env.length + 1);
	if (rest === 'apps/_defaults.yml' || rest === 'apps/_defaults.yaml') return { page: 'assets', value: rest.slice('apps/'.length) };
	if (rest.startsWith('apps/')) return { page: 'apps', value: rest.slice('apps/'.length) };
	if (rest.startsWith('assets/')) return { page: 'assets', value: rest.slice('assets/'.length) };
  if (isSpecial(rest)) return { page: 'assets', value: rest };
  return null;
}
function openDirtyPath(path) {
  const target = dirtyNavigationTarget(path);
  if (!target) return;
  setActive(target.page);
  if (target.page === 'apps') selectApp(target.value);
  if (target.page === 'assets') selectAsset(target.value);
}
async function restoreGitPath(path, code) {
  if (state.readOnly || !path) return;
  const untracked = code === '??';
  const confirmed = await confirmAction({
    title: untracked ? 'Delete untracked file?' : 'Discard local Git changes?',
    body: untracked ? 'This removes the file from disk.' : 'This restores the file from Git.',
    subject: path,
    confirmLabel: untracked ? 'Delete file' : 'Discard changes',
    confirmClass: 'btn-danger',
    statusClass: 'bg-danger',
  });
  if (!confirmed) return;
  clearError();
  try {
    const encoded = path.split('/').map(encodeURIComponent).join('/');
    const result = await apiPostJSON(`/api/v1/git/restore/${encoded}`, {delete_untracked: untracked});
    state.git = result.status;
    await refreshCurrentView();
  } catch (e) { showError(e); }
}
function resetSelectedDetails() {
  state.appFile = null;
  state.model = null;
  state.appVars = [];
  state.appReferences = null;
  state.sidecarEnvOverride = null;
  state.sidecarResourcesOverride = null;
  state.sidecarStartupOverride = null;
  state.inspectedApp = null;
  state.inspection = null;
  state.inspectionEnv = null;
  state.inspectionError = '';
  state.defaults = null;
  state.defaultsContentHash = null;
  state.assetPath = null;
  state.assetOpenDirs = new Set();
  state.inventory = null;
  setText('#app-detail-title', 'Select app');
  setText('#app-detail-path', '');
  setText('#app-detail-meta', '');
  state.appContentHash = null;
  setHTML('#app-detail-badges', '');
  setHTML('#app-overview', '<div class="text-muted">Select an application to show structured model overview.</div>');
  setHTML('#app-effective-status', '');
  setText('#app-overview-title', 'Effective overview');
  setHTML('#defaults-status', '');
  setHTML('#defaults-overview', '');
  setText('#app-raw', '');
  setText('#app-rendered', '');
  renderAppReplicasEditor(null);
  const appVarsBody = qs('#app-vars tbody');
  if (appVarsBody) appVarsBody.innerHTML = '';
  qs('#app-diff-section')?.classList.add('hidden');
  setText('#app-diff', '');
  setText('#asset-detail-title', 'Select asset');
  setText('#asset-detail-path', '');
  setHTML('#asset-detail-badges', '');
  setText('#asset-raw', '');
  qs('#asset-diff-section')?.classList.add('hidden');
  setText('#asset-diff', '');
  resetBuildView();
}

function resetBuildView() {
	state.inventory = null;
	state.buildPreview = null;
	state.buildPreviewPath = null;
	state.buildPreviewContent = null;
	state.buildPreviewOpenDirs = new Set();
	state.buildChecks = {};
	state.clusterStatus = null;
	state.clusterStatusLoading = false;
	setBuildStatus('info', 'Select an environment and run a build check.');
	setBuildDataEnv(null);
	renderBuildWorkflow();
	renderClusterStatus(null);
	setHTML('#build-totals', '');
  const summaryBody = qs('#build-summary-table tbody');
  if (summaryBody) summaryBody.innerHTML = '<tr><td colspan="9" class="text-muted">No summary loaded.</td></tr>';
  const summaryFoot = qs('#build-summary-table tfoot');
	if (summaryFoot) summaryFoot.innerHTML = '';
	const inventoryBody = qs('#build-inventory-table tbody');
	if (inventoryBody) inventoryBody.innerHTML = '<tr><td colspan="5" class="text-muted">No inventory loaded.</td></tr>';
	setHTML('#build-preview-totals', '');
	setHTML('#build-preview-events', '');
	setHTML('#build-preview-tree', '<div class="text-muted p-3">No preview loaded.</div>');
	const previewFilter = qs('#build-preview-filter');
	if (previewFilter) previewFilter.value = '';
	renderBuildPreviewContent(null);
}

async function loadInspection(force = false) {
  if (!state.env) return null;
  const env = state.env;
  if (!force && state.inspection && state.inspectionEnv === env) {
    if (state.active === 'defaults') renderDefaultsOverview();
    return state.inspection;
  }
  state.inspectionError = '';
  if (state.active === 'defaults') {
    setHTML('#defaults-status', '<div class="alert alert-info py-2 mb-0"><i class="ti ti-loader me-2"></i>Loading source and effective model...</div>');
  }
  try {
    const inspection = await api(`/api/v1/envs/${encodeURIComponent(env)}/inspect`);
    if (state.env !== env) return null;
    state.inspection = inspection;
    state.inspectionEnv = env;
    state.inspectionError = '';
    renderApps();
    if (state.appFile) state.inspectedApp = inspectedAppForFile(state.appFile);
    if (state.active === 'defaults') renderDefaultsOverview();
    return inspection;
  } catch (e) {
    if (state.env !== env) return null;
    state.inspection = null;
    state.inspectionEnv = env;
    state.inspectionError = String(e);
    renderApps();
    if (state.active === 'defaults') renderDefaultsOverview();
    return null;
  }
}

function inspectedAppForFile(file) {
  return (state.inspection?.apps || []).find((item) => item.file_name === file) || null;
}

function setAppView(view) {
  state.appView = view === 'local' ? 'local' : 'effective';
  renderSelectedAppView();
}

function renderSelectedAppView() {
  const effectiveButton = qs('#app-view-effective');
  const localButton = qs('#app-view-local');
  effectiveButton?.classList.toggle('btn-primary', state.appView === 'effective');
  effectiveButton?.classList.toggle('btn-outline-secondary', state.appView !== 'effective');
  localButton?.classList.toggle('btn-primary', state.appView === 'local');
  localButton?.classList.toggle('btn-outline-secondary', state.appView !== 'local');
  if (!state.appFile || !state.model) return;
  if (state.appView === 'local') {
    setText('#app-overview-title', 'Local source overview');
    setHTML('#app-effective-status', '<div class="alert alert-info py-2"><i class="ti ti-file-code me-2"></i>This view contains only declarations stored in the application YAML. Shared defaults are not materialized here.</div>');
    renderAppOverview(state.model);
    return;
  }
  setText('#app-overview-title', 'Effective overview');
  if (state.inspectionError) {
    setHTML('#app-effective-status', `<div class="alert alert-warning py-2"><i class="ti ti-alert-triangle me-2"></i>Effective model is unavailable: ${esc(state.inspectionError)}. Local source remains available.</div>`);
    setHTML('#app-overview', emptyState('ti-alert-triangle', 'Effective model unavailable', 'Switch to Local source to inspect and repair this app.'));
    return;
  }
  const inspected = state.inspectedApp;
  if (!inspected) {
    setHTML('#app-effective-status', '<div class="alert alert-info py-2"><i class="ti ti-loader me-2"></i>Loading effective model...</div>');
    setHTML('#app-overview', emptyState('ti-hourglass', 'Loading effective model'));
    return;
  }
  if (!inspected.effective) {
    const message = inspected.effective_error || state.inspection?.effective_error || 'Effective composition failed.';
    setHTML('#app-effective-status', `<div class="alert alert-warning py-2"><i class="ti ti-alert-triangle me-2"></i>${esc(message)} Local source remains available.</div>`);
    setHTML('#app-overview', emptyState('ti-alert-triangle', 'Effective model unavailable', 'Switch to Local source to inspect and repair this app.'));
    return;
  }
  setHTML('#app-effective-status', renderEffectiveContextStatus(state.inspection));
  renderEffectiveAppOverview(inspected);
}

function renderEffectiveContextStatus(inspection) {
  const context = inspection?.context || {};
  const sources = (context.variable_sources || []).join(' + ') || 'none';
  return `<div class="alert alert-success py-2"><i class="ti ti-layers-linked me-2"></i>Resolved for namespace <span class="font-monospace">${esc(inspection.namespace || '?')}</span> from ${esc(sources)} variables${context.release_manifests?.length > 1 ? ` and ${esc(context.release_manifests.length)} release manifests` : context.release_manifest ? ' and release manifest' : ''}.</div>`;
}

function renderDefaultsOverview() {
  const host = qs('#defaults-overview');
  if (!host) return;
  if (state.inspectionError) {
    setHTML('#defaults-status', `<div class="alert alert-danger py-2 mb-0"><i class="ti ti-alert-circle me-2"></i>${esc(state.inspectionError)}</div>`);
    if (profileEditor.active()) { profileEditor.render(); return; }
    host.innerHTML = emptyState('ti-alert-circle', 'Defaults snapshot unavailable', 'The Apps local source view remains usable.');
    return;
  }
  const inspection = state.inspection;
  if (!inspection) {
    if (profileEditor.active()) { profileEditor.render(); return; }
    host.innerHTML = emptyState('ti-hourglass', 'Loading defaults');
    return;
  }
  const effectiveError = inspection.effective_error;
  setHTML('#defaults-status', effectiveError
    ? `<div class="alert alert-warning py-2 mb-0"><i class="ti ti-alert-triangle me-2"></i>Source catalogs are available, but effective evaluation failed: ${esc(effectiveError)}</div>`
    : `<div class="alert alert-success py-2 mb-0"><i class="ti ti-check me-2"></i>Source catalogs loaded for namespace <span class="font-monospace">${esc(inspection.namespace || '?')}</span>.</div>`);
  if (profileEditor.active()) { profileEditor.render(); return; }
  const model = inspection.defaults?.model || {};
  const shared = inspection.shared_assets?.model || {};
  const sections = defaultsCatalogSections(model, shared);
  const query = (qs('#defaults-filter')?.value || '').trim().toLowerCase();
  let visible = 0;
  const rendered = sections.map((section) => {
    const items = section.items.filter((item) => !query || JSON.stringify(item).toLowerCase().includes(query) || section.label.toLowerCase().includes(query));
    visible += items.length;
    const emptyEditableSection = !query && !section.items.length && !state.readOnly && ['variable', 'container_env_defaults'].includes(section.kind);
    if (!items.length && !emptyEditableSection) return '';
    const action = defaultsSectionEditAction(section.kind);
    const body = items.length
      ? section.kind === 'variable' ? renderDefaultsVariablesTable(items) : `<div class="row g-2">${items.map((item) => renderDefaultsCatalogItem(section.kind, item)).join('')}</div>`
      : emptyState('ti-variable', `No ${section.label.toLowerCase()}`, 'Use the editor to create the first entry.');
    return `<div class="mb-4"><div class="d-flex align-items-center justify-content-between gap-2 mb-2"><h3 class="h4 mb-0"><i class="ti ${section.icon} me-2 text-muted"></i>${esc(section.label)}</h3><div class="d-flex align-items-center gap-2">${action}<span class="badge bg-secondary-lt">${items.length}</span></div></div>${body}</div>`;
  }).join('');
  setText('#defaults-filter-count', `${visible} visible`);
  host.innerHTML = rendered || emptyState('ti-search', 'No matching definitions', 'Try a different filter.');
}

function renderDefaultsVariablesTable(items) {
  const rows = items.map((item) => {
    const name = String(item.name || '?');
    const usages = (state.inspection?.usage || []).filter((usage) => usage.kind === 'variable' && usage.name === name);
    const value = item.value === undefined || item.value === null ? '' : String(item.value);
    return `<tr><td class="font-monospace fw-semibold text-break">${esc(name)}</td><td class="font-monospace text-break">${value ? esc(value) : '<span class="text-muted">empty</span>'}</td><td class="text-end text-nowrap"><span class="badge ${usages.length ? 'bg-blue-lt' : 'bg-secondary-lt'}">${usages.length}</span></td></tr>`;
  }).join('');
  return `<div class="card card-sm"><div class="table-responsive"><table class="table table-sm table-vcenter mb-0"><thead><tr><th style="width: 34%">Name</th><th>Value</th><th class="text-end" style="width: 5rem">Uses</th></tr></thead><tbody>${rows}</tbody></table></div></div>`;
}

function defaultsSectionEditAction(kind) {
  if (state.readOnly) return '';
  if (kind === 'variable') return `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-defaults-vars><i class="ti ti-pencil me-1"></i>Edit variables</button>`;
  if (kind === 'container_env_defaults') return `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-defaults-container-envs><i class="ti ti-pencil me-1"></i>Edit container envs</button>`;
  return '';
}

function defaultsCatalogSections(model, shared) {
  const named = (value) => Array.isArray(value) ? value : [];
  const singleton = (name, value) => value && typeof value === 'object' && Object.keys(value).length ? [{name, ...value}] : [];
  const workload = model.workload_identity || {};
  return [
    {label: 'Variables', kind: 'variable', icon: 'ti-variable', items: named(model.vars)},
    {label: 'Container env defaults', kind: 'container_env_defaults', icon: 'ti-braces', items: named(model.container_envs).map((item) => ({name: item.container_ref_name || '?', ...item}))},
    {label: 'Container profiles', kind: 'container_profile', icon: 'ti-template', items: named(model.container_profiles)},
    {label: 'Sidecar definitions', kind: 'sidecar_definition', icon: 'ti-box-multiple', items: named(model.sidecar_definitions)},
    {label: 'Workload identity tokens', kind: 'workload_identity_token', icon: 'ti-key', items: named(workload.tokens)},
    {label: 'Runtime asset defaults', kind: 'runtime_asset_defaults', icon: 'ti-settings', items: singleton('runtime asset defaults', model.runtime_asset_defaults)},
    {label: 'Runtime asset definitions', kind: 'runtime_asset_definition', icon: 'ti-file-settings', items: named(model.runtime_asset_definitions)},
    {label: 'Shared assets', kind: 'shared_asset', icon: 'ti-files', items: named(shared.assets)},
    {label: 'Pod, registry and metadata', kind: 'app_defaults', icon: 'ti-adjustments', items: [
      ...singleton('pod', model.pod), ...singleton('registry', model.registry),
      ...singleton('labels', model.labels), ...singleton('annotations', model.annotations),
      ...singleton('workload identity', workload.service_account ? {service_account: workload.service_account} : null),
    ]},
  ];
}

function renderDefaultsCatalogItem(kind, item) {
  const name = String(item.name || '?');
  const usages = (state.inspection?.usage || []).filter((usage) => usage.kind === kind && usage.name === name);
  const keys = Object.keys(item).filter((key) => key !== 'name');
  const summary = keys.slice(0, 7).map((key) => `<span class="badge bg-secondary-lt">${esc(key)}</span>`).join('');
  const usedBy = usages.slice(0, 6).map((usage) => `<button class="btn btn-sm btn-ghost-secondary" type="button" data-defaults-app="${esc(usage.app_file)}" title="${esc(usage.yaml_path)}"><i class="ti ti-apps me-1"></i>${esc(usage.app)}${usage.container ? ` / ${esc(usage.container)}` : ''}</button>`).join('');
  const assetLink = kind === 'shared_asset' && item.file ? `<button class="btn btn-sm btn-outline-secondary" type="button" data-defaults-asset="${esc(sharedAssetBrowserPath(item.file))}"><i class="ti ti-file me-1"></i>Open file</button>` : '';
  const editAction = !state.readOnly && kind === 'container_profile'
    ? `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-container-profile="${esc(name)}"><i class="ti ti-pencil me-1"></i>Edit profile</button>`
    : kind === 'sidecar_definition' && !state.readOnly ? `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-sidecar-definition="${esc(name)}"><i class="ti ti-photo me-1"></i>Edit image</button><button class="btn btn-sm btn-outline-primary" type="button" data-edit-sidecar-definition-startup="${esc(name)}"><i class="ti ti-terminal-2 me-1"></i>Edit startup</button><button class="btn btn-sm btn-outline-primary" type="button" data-edit-sidecar-definition-resources="${esc(name)}"><i class="ti ti-cpu me-1"></i>Edit resources</button><button class="btn btn-sm btn-outline-primary" type="button" data-edit-sidecar-definition-envs="${esc(name)}"><i class="ti ti-braces me-1"></i>Edit envs</button><button class="btn btn-sm btn-outline-primary" type="button" data-edit-sidecar-definition-references="${esc(name)}"><i class="ti ti-link me-1"></i>Edit references</button>` : '';
  return `<div class="col-12 col-lg-6"><div class="card card-sm h-100"><div class="card-body"><div class="d-flex align-items-start justify-content-between gap-2"><div class="fw-semibold font-monospace text-break">${esc(name)}</div><span class="badge ${usages.length ? 'bg-blue-lt' : 'bg-secondary-lt'}">${usages.length} use${usages.length === 1 ? '' : 's'}</span></div><div class="d-flex flex-wrap gap-1 mt-2">${summary || '<span class="text-muted small">No additional fields.</span>'}</div>${usedBy || assetLink || editAction ? `<div class="d-flex flex-wrap gap-1 mt-3">${usedBy}${assetLink}${editAction}</div>` : ''}</div></div></div>`;
}

async function openDefaultsSource() {
  if (!state.env) return;
  setActive('assets');
  const sourcePath = state.inspection?.defaults?.path || '_defaults.yml';
  await selectAsset(sourcePath.split('/').at(-1));
}

async function loadApps() {
  if (!state.env) return;
  const data = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/apps`);
  state.apps = data.items || [];
  setText('#apps-count', state.apps.length);
  renderApps();
}
function renderApps() {
  const body = qs('#apps-table tbody');
  const dirtyApps = state.apps.filter((a) => gitFile(`${state.env}/apps/${a.file_name}`));
  const dirtyBadgeEl = qs('#apps-dirty-count');
  if (dirtyBadgeEl) {
    dirtyBadgeEl.classList.toggle('hidden', dirtyApps.length === 0);
    dirtyBadgeEl.textContent = dirtyApps.length ? `${dirtyApps.length} changed` : '';
  }
  const query = (qs('#apps-filter')?.value || '').trim().toLowerCase();
  const apps = state.apps.filter((a) => {
    if (!query) return true;
    return [a.app_name, a.file_name, a.replicas, a.containers_count].some((value) => String(value ?? '').toLowerCase().includes(query));
  });
  setText('#apps-filter-count', `${apps.length}/${state.apps.length}`);
  body.innerHTML = apps.map((a) => {
    const inspected = inspectedAppForFile(a.file_name);
    const effective = inspected?.effective;
    const runtime = effective ? `${effective.containers?.length || 0} main + ${effective.sidecars?.length || 0} sidecar` : inspected?.effective_error ? 'effective error' : `${a.containers_count ?? 0} local`;
    return `
    <tr class="row-link ${state.appFile === a.file_name ? 'selected-row' : ''}" data-app="${esc(a.file_name)}">
      <td>
        <div class="fw-semibold app-list-name">${esc(a.app_name || a.file_name)}${dirtyBadge(gitFile(`${state.env}/apps/${a.file_name}`))}</div>
        <div class="text-muted small font-monospace text-break">${esc(a.file_name)}</div>
      </td>
      <td class="text-end">
        <div class="badge bg-blue-lt" title="replicas">${a.replicas ?? '-'}</div>
        <div class="text-muted small mt-1">${esc(runtime)}</div>
      </td>
    </tr>`;
  }).join('') || `<tr><td colspan="2">${emptyState('ti-apps', query ? 'No matching apps' : 'No apps', query ? 'Try a different filter.' : 'No app YAML files were found.')}</td></tr>`;
  qsa('[data-app]').forEach((x) => x.addEventListener('click', () => selectApp(x.dataset.app)));
}
async function selectApp(file, options = {}) {
  state.appFile = file; renderApps(); clearError();
  if (options.updateRoute !== false) {
    setActive('apps');
    pushRoute({page: 'apps', app: file});
  }
  try {
    const [detail, rendered, vars, model, references] = await Promise.all([
      api(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(file)}`),
      api(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(file)}/rendered`),
      api(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(file)}/vars`),
      api(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(file)}/model`),
      api(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(file)}/references`),
    ]);
    state.model = model;
    state.appVars = vars.items || [];
    state.appReferences = references;
    await loadInspection();
    state.inspectedApp = inspectedAppForFile(file);
    setText('#app-detail-title', model.app_name || detail.summary?.app_name || detail.file_name);
    state.appContentHash = detail.content_hash || vars.content_hash || null;
    setText('#app-detail-path', detail.path);
    setText('#app-detail-meta', `${model.containers?.length || 0} container(s), ${vars.items?.length || 0} local variable(s)${detail.is_dirty ? ', dirty file' : ''}`);
    setHTML('#app-detail-badges', renderAppDetailBadges(model, detail));
    renderSelectedAppView();
    setText('#app-raw', detail.content);
    setText('#app-rendered', rendered.content);
    await loadGitDiff(`${state.env}/apps/${detail.file_name}`, detail.is_dirty, '#app-diff-section', '#app-diff');
  } catch (e) { showError(e); }
}
function renderAppVarsEditor(items) {
  const body = qs('#app-vars tbody');
  if (!body) return;
  body.innerHTML = items.map((v) => renderAppVarRow(v.name, v.value)).join('');
  syncAppVarsEmptyState();
  const save = qs('#app-vars-save-btn');
  if (save) save.disabled = state.readOnly || !state.appFile;
  const add = qs('#app-vars-add-btn');
  if (add) add.disabled = state.readOnly || !state.appFile;
}
function renderAppReplicasEditor(model) {
  const input = qs('#app-replicas-input');
  const save = qs('#app-replicas-save-btn');
  const hint = qs('#app-replicas-hint');
  const selected = !!model && !!state.appFile;
  if (input) {
    input.value = selected && model.replicas !== null && model.replicas !== undefined ? model.replicas : '';
    input.disabled = state.readOnly || !selected;
  }
  if (save) save.disabled = state.readOnly || !selected;
  if (hint) {
    if (!selected) hint.textContent = 'Select an application to edit replicas.';
    else if (model.autoscaling?.enabled) hint.textContent = 'Autoscaling is enabled. Deployment replicas are initial desired state; runtime count is controlled by HPA.';
    else hint.textContent = 'Updates the top-level replicas field in the app YAML.';
  }
}
function renderAppVarRow(name = '', value = '') {
  const disabled = state.readOnly ? 'disabled' : '';
  return `
    <tr>
      <td><input class="form-control form-control-sm font-monospace app-var-name" placeholder="NAME" value="${esc(name)}" ${disabled}></td>
      <td><input class="form-control form-control-sm font-monospace app-var-value" placeholder="value" value="${esc(value)}" ${disabled}></td>
      <td class="table-action-col"><button class="btn btn-sm btn-outline-danger btn-icon" type="button" data-remove-app-var title="Remove variable" ${disabled}><i class="ti ti-trash"></i></button></td>
    </tr>`;
}
function syncAppVarsEmptyState() {
  const body = qs('[data-app-vars-modal] tbody') || qs('#app-vars tbody');
  if (!body) return;
  const hasRows = Array.from(body.querySelectorAll('tr')).some((row) => row.querySelector('.app-var-name'));
  if (!hasRows) body.innerHTML = `<tr><td colspan="3">${emptyState('ti-variable', 'No local variables', state.readOnly ? 'Start with --allow-write to add variables.' : 'Use Add variable to create the first one.')}</td></tr>`;
}
function addAppVarRow() {
  if (state.readOnly || !state.appFile) return;
  const body = qs('[data-app-vars-modal] tbody') || qs('#app-vars tbody');
  if (!body) return;
  if (!Array.from(body.querySelectorAll('tr')).some((row) => row.querySelector('.app-var-name'))) body.innerHTML = '';
  body.insertAdjacentHTML('beforeend', renderAppVarRow());
  Array.from(body.querySelectorAll('tr')).at(-1)?.querySelector('.app-var-name')?.focus();
}
async function saveAppVars() {
  if (!state.env || !state.appFile || state.readOnly) return;
  clearError();
  const body = qs('[data-app-vars-modal] tbody') || qs('#app-vars tbody');
  const validation = validateNamedRows(body, '.app-var-name', 'Variable name');
  if (!validation.ok) return showError(validation.message);
  const items = Array.from(body?.querySelectorAll('tr') || []).map((row) => ({
    name: row.querySelector('.app-var-name')?.value?.trim() || '',
    value: row.querySelector('.app-var-value')?.value || '',
  })).filter((item) => item.name !== '');
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/vars`, {items, expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
async function saveAppReplicas() {
  if (!state.env || !state.appFile || state.readOnly) return;
  const input = qs('#app-replicas-input');
  const raw = input?.value?.trim() ?? '';
  const replicas = Number(raw);
  clearInvalidInputs('#edit-modal');
  if (!raw || !/^[0-9]+$/.test(raw) || !Number.isInteger(replicas) || replicas < 0) {
    return showError(invalidInput(input, 'Replicas must be an integer greater than or equal to 0.').message);
  }
  clearError();
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/replicas`, {replicas, expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function renderAppDetailBadges(model, detail) {
  const containers = model.containers || [];
  const hasJava = containers.some((container) => container.runtime?.java?.enabled);
  const hasLegacyProbes = containers.some((container) => container.probes?.legacy);
  const hasProbes = containers.some((container) => container.probes?.enabled);
  const effective = state.inspectedApp?.effective;
  const badges = [
    detail.is_dirty ? badge('dirty', 'bg-yellow-lt', 'ti-alert-triangle') : '',
    effective ? badge(`${effective.containers?.length || 0} main`, 'bg-blue-lt', 'ti-box') : '',
    effective?.sidecars?.length ? badge(`${effective.sidecars.length} sidecar`, 'bg-cyan-lt', 'ti-box-multiple') : '',
    state.inspectedApp?.effective_error ? badge('effective error', 'bg-yellow-lt', 'ti-alert-triangle') : '',
    model.autoscaling?.enabled ? badge('autoscaling', 'bg-blue-lt', 'ti-arrows-maximize') : '',
    model.init_containers_count ? badge(`init ${model.init_containers_count}`, 'bg-purple-lt', 'ti-player-skip-forward') : '',
    hasJava ? badge('java runtime', 'bg-orange-lt', 'ti-coffee') : '',
    hasLegacyProbes ? badge('legacy probes', 'bg-yellow-lt', 'ti-alert-triangle') : hasProbes ? badge('probes', 'bg-green-lt', 'ti-heartbeat') : '',
  ].filter(Boolean);
  return badges.join('') || '<span class="badge bg-secondary-lt">basic app</span>';
}
function badge(label, color, icon) {
  return `<span class="badge ${color}"><i class="ti ${icon} me-1"></i>${esc(label)}</span>`;
}
function renderAppOverview(model) {
  const host = qs('#app-overview');
  if (!host) return;
  const autoscaling = model.autoscaling || {};
  const appFacts = [
    fact('Kind', model.kind || 'Deployment', 'ti-cube'),
    fact('Replicas', model.replicas ?? '-', 'ti-copy'),
    fact('Containers', (model.containers || []).length, 'ti-box'),
    fact('Init containers', model.init_containers_count || 0, 'ti-player-skip-forward'),
    fact('Autoscaling', autoscaling.enabled ? `HPA ${autoscaling.min_replicas ?? '?'}-${autoscaling.max_replicas ?? '?'}` : 'off', autoscaling.enabled ? 'ti-arrows-maximize' : 'ti-arrows-minimize'),
  ];
  const autoscalingMetrics = autoscaling.enabled ? `
    <div class="overview-section">
      <div class="overview-title"><i class="ti ti-arrows-maximize me-1"></i>Autoscaling</div>
      <div class="chip-row">
        ${chip('min', autoscaling.min_replicas ?? '-')}
        ${chip('max', autoscaling.max_replicas ?? '-')}
        ${chip('cpu avg', autoscaling.cpu_average_utilization ? `${autoscaling.cpu_average_utilization}%` : '-')}
        ${chip('memory avg', autoscaling.memory_average_utilization ? `${autoscaling.memory_average_utilization}%` : '-')}
      </div>
    </div>` : '';
  const containers = (model.containers || []).map((container) => renderContainerOverview(container)).join('');
  host.innerHTML = `
    <div class="overview-grid">${appFacts.join('')}</div>
    ${renderAppVarsPreview(state.appVars || [])}
    ${renderReferencesPreview(state.appReferences)}
    ${autoscalingMetrics}
    ${renderAppQuickEditors(model, autoscaling)}
    <div class="overview-section">
      <div class="overview-title"><i class="ti ti-box me-1"></i>Containers</div>
      <div class="container-stack">${containers || emptyState('ti-box', 'No containers', 'This app model does not declare containers.')}</div>
    </div>`;
}

function renderEffectiveAppOverview(inspected) {
  const host = qs('#app-overview');
  if (!host) return;
  const app = inspected.effective;
  const appFacts = [
    fact('Kind', app.kind || 'Deployment', 'ti-cube'),
    fact('Replicas', app.replicas ?? '-', 'ti-copy'),
    fact('Main containers', (app.containers || []).length, 'ti-box'),
    fact('Sidecars', (app.sidecars || []).length, 'ti-box-multiple'),
    fact('Init containers', (app.init_containers || []).length, 'ti-player-skip-forward'),
  ];
  const appMetadata = [
    app.workload_identity && Object.keys(app.workload_identity).length ? effectiveSummaryCard('Workload identity', app.workload_identity, 'ti-id-badge-2') : '',
    app.pod && Object.keys(app.pod).length ? effectiveSummaryCard('Pod defaults', app.pod, 'ti-box-model-2') : '',
    (app.runtime_assets || []).length ? effectiveSummaryCard('Runtime assets', app.runtime_assets.map((asset) => asset.name || '?'), 'ti-file-settings') : '',
    app.autoscaling?.enabled ? effectiveSummaryCard('Autoscaling', app.autoscaling, 'ti-arrows-maximize') : '',
  ].filter(Boolean).join('');
  host.innerHTML = `
    <div class="overview-grid">${appFacts.join('')}</div>
    ${appMetadata ? `<div class="row g-2 mb-3">${appMetadata}</div>` : ''}
    ${renderAppDependencyMap(inspected)}
    ${renderEffectiveAdvancedApp(inspected)}
    ${renderEffectiveContainerSection('Main containers', app.containers || [], 'main')}
    ${renderEffectiveContainerSection('Sidecars', app.sidecars || [], 'sidecar')}
    ${renderEffectiveInitContainers(app.init_containers || [])}`;
}

function renderAppDependencyMap(inspected) {
  const app = inspected.effective || {};
  const usage = (state.inspection?.usage || []).filter((item) => item.app_file === inspected.file_name);
  const appUsage = uniqueDependencyUsage(usage.filter((item) => item.container === '*'));
  const workloads = [
    ...(app.containers || []).map((container) => ({container, role: 'main'})),
    ...(app.sidecars || []).map((container) => ({container, role: 'sidecar'})),
  ];
  const appName = usage[0]?.app || state.model?.app_name || inspected.file_name.replace(/\.ya?ml$/i, '');
  const edit = state.readOnly ? '' : '<button class="btn btn-sm btn-outline-primary" type="button" data-edit-panel="references"><i class="ti ti-pencil me-1"></i>Edit references</button>';
  return `<div class="overview-section">
    <div class="d-flex align-items-center justify-content-between gap-2 mb-3">
      <div><div class="overview-title mb-0"><i class="ti ti-hierarchy-2 me-1"></i>Dependency map</div><div class="text-muted small">Resolved links from this application to shared metamodel definitions.</div></div>
      ${edit}
    </div>
    <div class="row g-2 align-items-stretch">
      <div class="col-12 col-xl-3">
        <div class="card card-sm h-100 border-primary">
          <div class="card-body">
            <div class="text-muted text-uppercase small mb-1">Application</div>
            <div class="fw-semibold font-monospace text-break">${esc(appName)}</div>
            <div class="mt-3"><div class="text-muted small mb-1">App-level dependencies</div>${renderDependencyNodes(appUsage, true)}</div>
          </div>
        </div>
      </div>
      <div class="col-12 col-xl-9">
        <div class="d-flex flex-column gap-2">${workloads.map((item) => renderWorkloadDependencyRow(item.container, item.role, usage)).join('') || emptyState('ti-box', 'No workloads')}</div>
      </div>
    </div>
  </div>`;
}

function renderWorkloadDependencyRow(container, role, usage) {
  const name = container.name || '?';
  const matches = usage.filter((item) => item.container === name || (role === 'sidecar' && item.kind === 'sidecar_definition' && item.name === name));
  const dependencies = uniqueDependencyUsage(matches);
  return `<div class="card card-sm"><div class="card-body"><div class="row g-2 align-items-center">
    <div class="col-12 col-lg-4">
      <div class="d-flex align-items-center gap-2"><i class="ti ${role === 'sidecar' ? 'ti-box-multiple text-cyan' : 'ti-box text-blue'}"></i><div><div class="fw-semibold font-monospace text-break">${esc(name)}</div><div class="text-muted small">${esc(role === 'sidecar' ? 'Sidecar' : 'Main container')}</div></div></div>
    </div>
    <div class="col-12 col-lg-8"><div class="d-flex flex-wrap gap-1">${renderDependencyNodes(dependencies)}</div></div>
  </div></div></div>`;
}

function uniqueDependencyUsage(items) {
  const seen = new Set();
  return (items || []).filter((item) => {
    const key = `${item.kind}:${item.name}`;
    if (!item.name || seen.has(key)) return false;
    seen.add(key);
    return true;
  }).sort((left, right) => `${left.kind}:${left.name}`.localeCompare(`${right.kind}:${right.name}`));
}

function renderDependencyNodes(items, stacked = false) {
  if (!items.length) return '<span class="text-muted small">No shared dependencies.</span>';
  const kinds = {
    sidecar_definition: ['Shared sidecar', 'ti-box-multiple', 'bg-cyan-lt'],
    container_profile: ['Profile', 'ti-template', 'bg-blue-lt'],
    runtime_asset_definition: ['Runtime asset', 'ti-file-settings', 'bg-azure-lt'],
    workload_identity_token: ['Token', 'ti-key', 'bg-orange-lt'],
    shared_asset: ['Shared asset', 'ti-files', 'bg-green-lt'],
  };
  return items.map((item) => {
    const [label, icon, color] = kinds[item.kind] || [item.kind, 'ti-link', 'bg-secondary-lt'];
    const badgeClass = stacked ? 'd-flex align-items-start text-start text-wrap w-100' : '';
    return `<button class="badge border-0 dependency-link ${color} mw-100 ${badgeClass} ${stacked ? 'mb-1' : ''}" type="button" data-dependency-kind="${esc(item.kind)}" data-dependency-name="${esc(item.name)}" title="Open ${esc(label)} in Defaults"><i class="ti ${icon} me-1 flex-shrink-0"></i><span>${esc(label)}: <span class="font-monospace text-break">${esc(item.name)}</span></span></button>`;
  }).join('');
}

function openDependencyDefinition(kind, name) {
  if (!name) return;
  setActive('defaults');
  pushRoute({page: 'defaults'});
  const filter = qs('#defaults-filter');
  if (filter) filter.value = name;
  renderDefaultsOverview();
  window.requestAnimationFrame(() => qs('#defaults-overview')?.scrollIntoView({behavior: 'smooth', block: 'start'}));
}

function renderEffectiveAdvancedApp(inspected) {
  const app = inspected.effective || {};
  const configured = (path) => inspected.source?.fields?.[path]?.present || state.inspection?.defaults?.fields?.[path]?.present;
  const object = (fields) => Object.fromEntries(fields.filter(([path, value]) => configured(path) && value !== undefined).map(([path, value]) => [path, value]));
  const entries = [
    effectiveAdvancedEntry('Deployment behavior', 'ti-adjustments', object([
      ['strategy', app.strategy], ['subdomain_name', app.subdomain_name], ['min_available', app.min_available], ['max_unavailable', app.max_unavailable],
      ['termination_grace_period', app.termination_grace_period], ['disable_shared_assets', app.disable_shared_assets], ['disable_create_service', app.disable_create_service],
    ])),
    effectiveAdvancedEntry('Metadata', 'ti-tags', object([
      ['labels', app.labels], ['selector_labels', app.selector_labels], ['annotations', app.annotations], ['pod_annotations', app.pod_annotations],
    ])),
    effectiveAdvancedEntry('Pod identity and security', 'ti-shield-lock', object([
      ['security_context', app.security_context], ['service_account', app.service_account], ['pod_info', app.pod_info], ['downward_api', app.downward_api],
    ])),
    effectiveAdvancedEntry('Scheduling', 'ti-affiliate', object([
      ['arch', app.arch], ['node_selector', app.node_selector], ['tolerations', app.tolerations], ['scheduling', app.scheduling],
    ])),
    effectiveAdvancedEntry('Rollout triggers', 'ti-refresh', configured('rollout_on') ? app.rollout_on : null),
    effectiveAdvancedEntry('Tools, registry and DNS', 'ti-tool', object([
      ['tools', app.tools], ['registry', app.registry], ['dns', app.dns],
    ])),
    effectiveAdvancedEntry('Autoscaling raw', 'ti-arrows-maximize', configured('autoscaling.raw') ? app.autoscaling?.raw : null),
    effectiveAdvancedEntry('Raw Kubernetes overlays', 'ti-code', object([
      ['deployment_raw', app.deployment_raw], ['pod_raw', app.pod_raw],
    ])),
  ].filter(Boolean);
  return renderEffectiveAdvancedDisclosure('Advanced application configuration', 'Scheduling, security, rollout and raw builder fields.', entries);
}

function effectiveAdvancedEntry(label, icon, value) {
  if (!hasEffectiveValue(value)) return null;
  return {label, icon, value};
}

function hasEffectiveValue(value) {
  if (value === null || value === undefined || value === '') return false;
  if (Array.isArray(value)) return value.length > 0;
  if (typeof value === 'object') return Object.keys(value).length > 0;
  return true;
}

function renderEffectiveAdvancedDisclosure(title, subtitle, entries) {
  if (!entries.length) return '';
  return `<div class="card card-sm mb-3" data-effective-advanced>
    <div class="card-header p-2">
      <button class="btn btn-sm btn-ghost-secondary w-100 d-flex align-items-center justify-content-between text-start" type="button" data-toggle-effective-advanced aria-expanded="false">
        <span><i class="ti ti-chevron-right me-1" data-effective-advanced-icon></i><span class="fw-semibold">${esc(title)}</span><span class="text-muted ms-2 d-none d-md-inline">${esc(subtitle)}</span></span>
        <span class="badge bg-secondary-lt">${entries.length}</span>
      </button>
    </div>
    <div class="card-body hidden" data-effective-advanced-body><div class="row g-2">${entries.map((entry) => `<div class="col-12 col-xl-6"><div class="border rounded p-2 h-100"><div class="overview-subtitle"><i class="ti ${entry.icon} me-1"></i>${esc(entry.label)}</div><div class="font-monospace small text-break">${esc(compactEffectiveValue(entry.value))}</div></div></div>`).join('')}</div></div>
  </div>`;
}

function toggleEffectiveAdvanced(button) {
  const disclosure = button.closest('[data-effective-advanced]');
  const body = disclosure?.querySelector('[data-effective-advanced-body]');
  const icon = button.querySelector('[data-effective-advanced-icon]');
  if (!body) return;
  const expanded = body.classList.contains('hidden');
  body.classList.toggle('hidden', !expanded);
  button.setAttribute('aria-expanded', String(expanded));
  if (icon) icon.className = `ti ${expanded ? 'ti-chevron-down' : 'ti-chevron-right'} me-1`;
}

function effectiveSummaryCard(title, value, icon) {
  return `<div class="col-12 col-lg-6"><div class="metric-card"><div class="overview-subtitle"><i class="ti ${icon} me-1"></i>${esc(title)}</div><div class="font-monospace small text-break">${esc(compactEffectiveValue(value))}</div></div></div>`;
}

function renderEffectiveContainerSection(title, containers, role) {
  if (!containers.length) return '';
  return `<div class="overview-section"><div class="overview-title"><i class="ti ${role === 'sidecar' ? 'ti-box-multiple' : 'ti-box'} me-1"></i>${esc(title)}</div><div class="container-stack">${containers.map((container) => renderEffectiveContainer(container, role)).join('')}</div></div>`;
}

function renderEffectiveContainer(container, role) {
  const resources = container.resources || {};
  const startup = container.startup || {};
  const probes = container.probes || {};
  const origins = effectiveContainerOrigins(container);
  const imageOrigins = container.fields?.image?.origins || [];
  const imageOrigin = imageOrigins.length ? imageOrigins[imageOrigins.length - 1] : null;
  const envs = container.env_entries || [];
  return `<div class="container-card">
    <div class="d-flex align-items-start justify-content-between gap-2 mb-3">
      <div class="min-w-0"><div class="fw-semibold font-monospace text-break">${esc(container.name || '?')}</div><div class="d-flex flex-wrap align-items-center gap-1"><span class="text-muted small font-monospace text-break">${esc(container.image || 'image not configured')}</span>${imageOrigin ? renderOriginBadge(imageOrigin) : ''}</div></div>
      <span class="badge ${role === 'sidecar' ? 'bg-cyan-lt' : 'bg-blue-lt'}">${esc(role)}</span>
    </div>
    <div class="d-flex flex-wrap gap-1 mb-3">${origins.length ? origins.map(renderOriginBadge).join('') : '<span class="badge bg-secondary-lt">origin unavailable</span>'}</div>
    <div class="row g-2">
      <div class="col-12 col-xl-4"><div class="overview-subtitle">Resources</div><div class="chip-row">${effectiveResourceChips(resources)}</div>${renderEffectiveResourcesAction(container, role)}</div>
      <div class="col-12 col-xl-4"><div class="overview-subtitle">Startup</div><div class="font-monospace small text-break">${esc(compactEffectiveValue(startup) || '-')}</div>${renderEffectiveStartupAction(container, role)}</div>
      <div class="col-12 col-xl-4"><div class="overview-subtitle">Probes</div><div class="font-monospace small text-break">${esc(compactEffectiveValue(pruneDisplayValue(probes)) || '-')}</div></div>
    </div>
    <div class="row g-2 mt-1">
      <div class="col-12 col-xl-6"><div class="overview-subtitle">Ports</div>${(container.ports || []).length ? `<div class="chip-row">${container.ports.map((port) => chip(port.name || 'port', port.port ?? '?')).join('')}</div>` : '<div class="text-muted small">No ports.</div>'}</div>
      <div class="col-12 col-xl-6"><div class="overview-subtitle">Runtime assets</div>${(container.runtime_asset_ref_names || []).length ? `<div class="chip-row">${container.runtime_asset_ref_names.map((name) => chip('asset', name)).join('')}</div>` : '<div class="text-muted small">No direct asset references.</div>'}</div>
    </div>
    <div class="overview-section"><div class="overview-subtitle">Environment</div>${envs.length ? `<div class="d-flex flex-column gap-1">${envs.map((entry) => renderEffectiveEnvEntry(container, entry, role)).join('')}</div>` : '<div class="text-muted small">No environment entries.</div>'}</div>
    ${renderEffectiveContainerAdvanced(container)}
  </div>`;
}

function renderEffectiveContainerAdvanced(container) {
  const entries = [
    effectiveAdvancedEntry('Assets', 'ti-files', container.assets),
    effectiveAdvancedEntry('Mounts', 'ti-folders', container.mounts),
    effectiveAdvancedEntry('Environment sources', 'ti-database-import', container.env_from),
    effectiveAdvancedEntry('Security context', 'ti-shield-lock', container.security_context),
    effectiveAdvancedEntry('Raw Kubernetes overlay', 'ti-code', container.raw),
  ].filter(Boolean);
  return renderEffectiveAdvancedDisclosure('Advanced container configuration', 'Assets, mounts, env sources, security and raw fields.', entries);
}

function renderEffectiveResourcesAction(container, role) {
  const field = container.fields?.resources;
  const editable = role === 'sidecar' && !state.readOnly && field?.capabilities?.can_override && field?.write_target?.name_assertion;
  if (!editable) return '';
  const label = field.capabilities.can_reset ? 'Edit override' : 'Override';
  return `<button class="btn btn-sm btn-ghost-secondary mt-2" type="button" data-edit-sidecar-resources data-sidecar-name="${esc(container.name)}"><i class="ti ti-pencil me-1"></i>${label}</button>`;
}

function renderEffectiveStartupAction(container, role) {
  const field = container.fields?.startup;
  const editable = role === 'sidecar' && !state.readOnly && field?.capabilities?.can_override && field?.write_target?.name_assertion;
  if (!editable) return '';
  const label = field.capabilities.can_reset ? 'Edit override' : 'Override';
  return `<button class="btn btn-sm btn-ghost-secondary mt-2" type="button" data-edit-sidecar-startup data-sidecar-name="${esc(container.name)}"><i class="ti ti-pencil me-1"></i>${label}</button>`;
}

function renderEffectiveEnvEntry(container, entry, role) {
  const value = effectiveEnvValue(entry.effective);
  const simpleValue = entry.effective && Object.prototype.hasOwnProperty.call(entry.effective, 'value');
  const editable = role === 'sidecar' && !state.readOnly && simpleValue && entry.capabilities?.can_override && entry.write_target?.name_assertion;
  const local = (entry.origins || []).some((origin) => origin.kind === 'local');
  const action = editable ? `<button class="btn btn-sm btn-ghost-secondary" type="button" data-edit-sidecar-env data-sidecar-name="${esc(container.name)}" data-env-name="${esc(entry.name)}" data-effective-value="${esc(value)}"><i class="ti ti-pencil me-1"></i>${local ? 'Edit override' : 'Override'}</button>` : '';
  return `<div class="d-flex align-items-center justify-content-between gap-2"><div class="chip-row">${chip(entry.name || '?', value)}</div>${action}</div>`;
}

function effectiveContainerOrigins(container) {
  const seen = new Set();
  const result = [];
  for (const origin of container.origins || []) {
    const key = `${origin.kind}:${origin.definition_name || ''}:${origin.document || ''}`;
    if (!seen.has(key)) { seen.add(key); result.push(origin); }
  }
  for (const [fieldName, field] of Object.entries(container.fields || {})) {
    if (fieldName === 'image') continue;
    for (const origin of field.origins || []) {
      const key = `${origin.kind}:${origin.definition_name || ''}:${origin.document || ''}`;
      if (!seen.has(key)) { seen.add(key); result.push(origin); }
    }
  }
  return result;
}

function renderOriginBadge(origin) {
  const styles = {
    local: ['bg-green-lt', 'Local'], container_profile: ['bg-blue-lt', 'Profile'],
    container_env_defaults: ['bg-secondary-lt', 'Defaults env'], sidecar_definition: ['bg-cyan-lt', 'Shared sidecar'],
    external_resource_policy: ['bg-orange-lt', 'External policy'], release_manifest: ['bg-purple-lt', 'Release'],
    cli_image_override: ['bg-yellow-lt', 'CLI image'],
  };
  const [style, label] = styles[origin.kind] || ['bg-secondary-lt', origin.kind || 'origin'];
  const detail = origin.definition_name ? `: ${origin.definition_name}` : '';
  return `<span class="badge ${style}" title="${esc(`${origin.document || ''} ${origin.yaml_path || ''}`)}">${esc(label + detail)}</span>`;
}

function effectiveResourceChips(resources) {
  const value = (name, side) => resources?.[name]?.[side] ?? resources?.[name]?.[side === 'requests' ? 'from' : 'to'] ?? '-';
  const items = [chip('CPU req', value('cpu', 'requests')), chip('CPU lim', value('cpu', 'limits')), chip('Mem req', value('memory', 'requests')), chip('Mem lim', value('memory', 'limits'))];
  if (resources?.['ephemeral-storage']) {
    items.push(chip('Ephemeral req', value('ephemeral-storage', 'requests')), chip('Ephemeral lim', value('ephemeral-storage', 'limits')));
  }
  return items.join('');
}

function effectiveEnvValue(env) {
  if (!env) return '';
  if (Object.prototype.hasOwnProperty.call(env, 'value')) return env.value ?? '';
  if (env.workload_identity_token_ref_name) return `token: ${env.workload_identity_token_ref_name}`;
  if (env.shared_asset_ref_name) return `asset: ${env.shared_asset_ref_name}`;
  if (env.remove) return 'remove inherited';
  return compactEffectiveValue(env.valueFrom || env);
}

function compactEffectiveValue(value) {
  if (value === null || value === undefined) return '';
  if (Array.isArray(value)) return value.map((item) => typeof item === 'object' ? (item.name || JSON.stringify(item)) : item).join(', ');
  if (typeof value === 'object') return Object.entries(value).map(([key, item]) => `${key}=${typeof item === 'object' ? JSON.stringify(item) : item}`).join(', ');
  return String(value);
}

function pruneDisplayValue(value) {
  if (Array.isArray(value)) return value.map(pruneDisplayValue).filter((item) => item !== null);
  if (!value || typeof value !== 'object') return value === 0 || value === false || value === '' ? null : value;
  const result = {};
  for (const [key, item] of Object.entries(value)) {
    const pruned = pruneDisplayValue(item);
    if (pruned !== null && (!Array.isArray(pruned) || pruned.length) && (typeof pruned !== 'object' || Array.isArray(pruned) || Object.keys(pruned).length)) result[key] = pruned;
  }
  return result;
}

function renderEffectiveInitContainers(containers) {
  if (!containers.length) return '';
  return `<div class="overview-section"><div class="overview-title"><i class="ti ti-player-skip-forward me-1"></i>Init containers</div><div class="container-stack">${containers.map(renderEffectiveInitContainer).join('')}</div></div>`;
}

function renderEffectiveInitContainer(container) {
  const detail = Object.fromEntries(Object.entries(container || {}).filter(([key, value]) => !['name', 'image'].includes(key) && hasEffectiveValue(value)));
  const entries = [effectiveAdvancedEntry('Resolved init configuration', 'ti-player-skip-forward', detail)].filter(Boolean);
  return `<div class="card card-sm"><div class="card-body"><div class="d-flex align-items-start justify-content-between gap-2"><div><div class="fw-semibold font-monospace text-break">${esc(container.name || '?')}</div><div class="text-muted small font-monospace text-break">${esc(container.image || 'image not configured')}</div></div><span class="badge bg-secondary-lt">init</span></div>${renderEffectiveAdvancedDisclosure('Init container details', 'Startup, resources, mounts and security.', entries)}</div></div>`;
}
function renderContainerOverview(container) {
  const resources = container.resources || {};
  const java = container.runtime?.java || {};
  const probes = container.probes || {};
  const ports = container.ports || [];
  const vars = container.envs || [];
  const javaBlock = java.enabled ? `
    <div class="chip-row">
      ${chip('JVM Xms', java.xms || '-')}
      ${chip('JVM Xmx', java.xmx || '-')}
      ${chip('export', java.export_env_name || 'JAVA_OPTS')}
      ${java.opts?.length ? chip('opts', java.opts.length) : ''}
    </div>` : '<div class="text-muted small">Java runtime not configured.</div>';
  const editButton = (panel, label) => state.readOnly ? '' : `<button class="btn btn-sm btn-outline-primary mt-2" type="button" data-edit-panel="${panel}" data-container-index="${container.index}"><i class="ti ti-pencil me-1"></i>${esc(label)}</button>`;
  return `
    <div class="container-card">
      <div class="d-flex align-items-start justify-content-between gap-2 mb-2">
        <div>
          <div class="fw-semibold font-monospace">${esc(container.name || `container-${container.index}`)}</div>
          <div class="text-muted small">${vars.length} variable(s), ${container.env_from_count || 0} env_from, ${container.mounts_count || 0} mount(s), ${ports.length} port(s)</div>
        </div>
        <span class="badge bg-blue-lt">#${container.index + 1}</span>
      </div>
      <div class="row g-2">
        <div class="col-12 col-xl-4"><div class="overview-subtitle">Resources</div><div class="chip-row">
          ${chip('CPU req', resources.cpu_request || '-')}
          ${chip('CPU lim', resources.cpu_limit || '-')}
          ${chip('Mem req', resources.memory_request || '-')}
          ${chip('Mem lim', resources.memory_limit || '-')}
          ${resources.ephemeral_storage_request || resources.ephemeral_storage_limit ? `${chip('Ephemeral req', resources.ephemeral_storage_request || '-')}${chip('Ephemeral lim', resources.ephemeral_storage_limit || '-')}` : ''}
        </div>${editButton('resources', 'Edit resources')}</div>
        <div class="col-12 col-xl-4"><div class="overview-subtitle">Java runtime</div>${javaBlock}${editButton('runtime', 'Edit runtime')}</div>
        <div class="col-12 col-xl-4"><div class="overview-subtitle">Probes</div>${renderProbePreview(container.index, probes)}${editButton('probes', 'Edit probes')}</div>
      </div>
      ${renderPortsPreview(container.index, ports)}
      ${renderContainerEnvsPreview(container.index, vars)}
    </div>`;
}
function renderAppQuickEditors(model, autoscaling) {
  if (state.readOnly) return '';
  return `
    <div class="overview-section compact-actions">
      <button class="btn btn-sm btn-outline-primary" type="button" data-edit-panel="autoscaling"><i class="ti ti-arrows-maximize me-1"></i>Edit autoscaling</button>
      <button class="btn btn-sm btn-outline-primary" type="button" data-edit-panel="replicas"><i class="ti ti-copy me-1"></i>Edit replicas</button>
    </div>`;
}
function renderAppVarsPreview(items) {
  const edit = state.readOnly ? '' : `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-panel="app-vars"><i class="ti ti-pencil me-1"></i>Edit local variables</button>`;
  const chips = (items || []).slice(0, 10).map((item) => chip(item.name || '?', item.value || '')).join('');
  const more = (items || []).length > 10 ? `<span class="badge bg-secondary-lt">+${items.length - 10} more</span>` : '';
  return `
    <div class="overview-section">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Local variables</div>
        ${edit}
      </div>
      ${(items || []).length ? `<div class="chip-row">${chips}${more}</div>` : emptyState('ti-variable', 'No local variables', state.readOnly ? '' : 'Use Edit local variables to add top-level vars.')}
    </div>`;
}
function renderReferencesPreview(references) {
  if (!references) return '';
  const chipsFor = (label, values) => (values || []).map((value) => chip(label, value)).join('');
  const scopes = [
    ...(references.containers || []).map((item) => ({label: `container ${item.name}`, values: [...(item.profile_ref_names || []).map((value) => `profile: ${value}`), ...(item.runtime_asset_ref_names || []).map((value) => `asset: ${value}`)]})),
    ...(references.sidecars || []).map((item) => ({label: `sidecar ${item.name}`, values: [...(item.profile_ref_names || []).map((value) => `profile: ${value}`), ...(item.runtime_asset_ref_names || []).map((value) => `asset: ${value}`)]})),
  ].filter((item) => item.values.length);
  return `<div class="overview-section">
    <div class="d-flex align-items-center justify-content-between gap-2 mb-2"><div class="overview-subtitle mb-0">Explicit references</div>${state.readOnly ? '' : '<button class="btn btn-sm btn-outline-primary" type="button" data-edit-panel="references"><i class="ti ti-pencil me-1"></i>Edit references</button>'}</div>
    <div class="chip-row">${chipsFor('sidecar', references.sidecar_ref_names)}${chipsFor('asset', references.runtime_asset_ref_names)}${!references.sidecar_ref_names?.length && !references.runtime_asset_ref_names?.length ? '<span class="text-muted small">No app-level references.</span>' : ''}</div>
    ${scopes.map((scope) => `<div class="mt-2"><span class="text-muted small me-2">${esc(scope.label)}</span>${scope.values.map((value) => `<span class="badge bg-secondary-lt me-1">${esc(value)}</span>`).join('')}</div>`).join('')}
  </div>`;
}

function renderReferencesEditor(references) {
  if (!references) return emptyState('ti-alert-triangle', 'References unavailable', 'Refresh the application and try again.');
  const catalog = references.catalog || {};
  const section = (title, hint, content) => `<div class="overview-section mb-3"><div class="overview-subtitle mb-1">${esc(title)}</div><div class="text-muted small mb-2">${esc(hint)}</div>${content}</div>`;
  const scoped = (scope, item) => `<div class="card card-sm mb-2"><div class="card-body"><div class="fw-semibold font-monospace mb-2">${esc(item.name || `${scope}-${item.index + 1}`)}</div>${renderReferenceList('Profiles', item.profile_ref_names, catalog.container_profiles, scope, item.index, 'profile_ref_names')}${renderReferenceList('Runtime assets', item.runtime_asset_ref_names, catalog.runtime_asset_definitions, scope, item.index, 'runtime_asset_ref_names')}</div></div>`;
  return `<div data-references-editor>
    ${section('Application references', 'Sidecars and runtime assets selected for the whole application.', `${renderReferenceList('Sidecars', references.sidecar_ref_names, catalog.sidecar_definitions, 'app', -1, 'sidecar_ref_names')}${renderReferenceList('Runtime assets', references.runtime_asset_ref_names, catalog.runtime_asset_definitions, 'app', -1, 'runtime_asset_ref_names')}`)}
    ${section('Main containers', 'Profile order is significant; later profiles override earlier profiles.', (references.containers || []).map((item) => scoped('containers', item)).join('') || '<div class="text-muted">No main containers.</div>')}
    ${section('Local sidecars', 'References on app-local sidecars. Shared sidecar definitions are selected above.', (references.sidecars || []).map((item) => scoped('sidecars', item)).join('') || '<div class="text-muted">No local sidecars.</div>')}
    <div class="alert alert-info py-2"><i class="ti ti-info-circle me-2"></i>Saving changes only reference lists. It does not copy inherited values into this app YAML.</div>
    <div class="text-end"><button class="btn btn-primary" type="button" data-save-references><i class="ti ti-device-floppy me-1"></i>Save references</button></div>
  </div>`;
}

function renderReferenceList(label, values, catalog, scope, index, field, owner = 'app') {
  const options = catalog || [];
  const rows = (values || []).map((value) => renderReferenceRow(value, options)).join('');
  return `<div class="mb-3" data-reference-list data-reference-owner="${owner}" data-reference-scope="${scope}" data-reference-index="${index}" data-reference-field="${field}">
    <div class="d-flex align-items-center justify-content-between gap-2 mb-1"><label class="form-label mb-0">${esc(label)}</label><button class="btn btn-sm btn-outline-secondary" type="button" data-add-reference ${(values || []).length < options.length ? '' : 'disabled'}><i class="ti ti-plus me-1"></i>Add</button></div>
    <div data-reference-rows>${rows || '<div class="text-muted small" data-reference-empty>No references selected.</div>'}</div>
  </div>`;
}

function renderReferenceRow(value, catalog) {
  return `<div class="d-flex align-items-center gap-1 mb-1" data-reference-row>
    <select class="form-select form-select-sm font-monospace" data-reference-value>${(catalog || []).map((item) => `<option value="${esc(item)}" ${item === value ? 'selected' : ''}>${esc(item)}</option>`).join('')}</select>
    <button class="btn btn-sm btn-ghost-secondary btn-icon" type="button" data-move-reference="up" title="Move up"><i class="ti ti-arrow-up"></i></button>
    <button class="btn btn-sm btn-ghost-secondary btn-icon" type="button" data-move-reference="down" title="Move down"><i class="ti ti-arrow-down"></i></button>
    <button class="btn btn-sm btn-ghost-danger btn-icon" type="button" data-remove-reference title="Remove"><i class="ti ti-trash"></i></button>
  </div>`;
}
function renderAutoscalingEditor(autoscaling) {
  if (state.readOnly) return '';
  const disabled = state.readOnly ? 'disabled' : '';
  const enabled = autoscaling?.enabled ? 'checked' : '';
  return `
    <div class="overview-section">
      <div class="resource-editor" data-autoscaling-editor>
        <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
          <div class="overview-title mb-0"><i class="ti ti-arrows-maximize me-1"></i>Autoscaling editor</div>
          <label class="form-check form-switch mb-0">
            <input class="form-check-input" type="checkbox" data-autoscaling-enabled ${enabled} ${disabled}>
            <span class="form-check-label">enabled</span>
          </label>
        </div>
        <div class="row g-2" data-autoscaling-fields>
          ${autoscalingInput('min-replicas', 'Min replicas', autoscaling?.min_replicas ?? '')}
          ${autoscalingInput('max-replicas', 'Max replicas', autoscaling?.max_replicas ?? '')}
          ${autoscalingInput('cpu-average-utilization', 'CPU avg %', autoscaling?.cpu_average_utilization ?? '')}
          ${autoscalingInput('memory-average-utilization', 'Memory avg %', autoscaling?.memory_average_utilization ?? '')}
        </div>
        <div class="d-flex align-items-center justify-content-between gap-2 mt-2">
          <div class="text-muted small">Saves top-level autoscaling.enabled/min/max and CPU/memory average utilization.</div>
          <button class="btn btn-sm btn-primary" type="button" data-save-autoscaling ${disabled}><i class="ti ti-device-floppy me-1"></i>Save autoscaling</button>
        </div>
      </div>
    </div>`;
}
function renderReplicasEditorModal(model) {
  const value = model?.replicas ?? '';
  return `
    <div class="detail-panel">
      <div class="row g-2 align-items-end">
        <div class="col-12 col-sm-5"><label class="form-label">Desired replicas</label><input class="form-control" id="app-replicas-input" type="number" min="0" step="1" value="${esc(value)}"></div>
        <div class="col-12 col-sm-7"><div class="text-muted small">${model?.autoscaling?.enabled ? 'Autoscaling is enabled. Deployment replicas are initial desired state; runtime count is controlled by HPA.' : 'Updates the top-level replicas field in the app YAML.'}</div></div>
      </div>
      <div class="text-end mt-3"><button class="btn btn-primary" type="button" data-save-replicas-modal><i class="ti ti-device-floppy me-1"></i>Save replicas</button></div>
    </div>`;
}
function renderAppVarsEditorModal(items) {
  const rows = (items || []).map((v) => renderAppVarRow(v.name, v.value)).join('');
  return `
    <div class="detail-panel">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Local variables</div>
        <button class="btn btn-sm btn-outline-primary" type="button" data-add-app-var-modal><i class="ti ti-plus me-1"></i>Add variable</button>
      </div>
      <div class="table-responsive"><table class="table table-sm" data-app-vars-modal><tbody>${rows || `<tr><td colspan="3">${emptyState('ti-variable', 'No local variables', 'Use Add variable to create the first one.')}</td></tr>`}</tbody></table></div>
      <div class="text-end mt-3"><button class="btn btn-primary" type="button" data-save-app-vars-modal><i class="ti ti-device-floppy me-1"></i>Save variables</button></div>
    </div>`;
}
function autoscalingInput(field, label, value) {
  const disabled = state.readOnly ? 'disabled' : '';
  return `<div class="col-6 col-lg-3"><label class="form-label small mb-1">${esc(label)}</label><input class="form-control form-control-sm font-monospace" type="number" min="0" step="1" data-autoscaling-field="${field}" value="${esc(value)}" ${disabled}></div>`;
}
function syncAutoscalingEditor() {
  const enabled = !!qs('[data-autoscaling-enabled]')?.checked;
  qsa('[data-autoscaling-field]').forEach((input) => { input.disabled = state.readOnly || !enabled; });
}
async function saveAppAutoscaling() {
  if (!state.env || !state.appFile || state.readOnly) return;
  const value = (field) => qs(`[data-autoscaling-field="${field}"]`)?.value?.trim() || '';
  const autoscaling = {
    enabled: !!qs('[data-autoscaling-enabled]')?.checked,
    min_replicas: value('min-replicas'),
    max_replicas: value('max-replicas'),
    cpu_average_utilization: value('cpu-average-utilization'),
    memory_average_utilization: value('memory-average-utilization'),
  };
  clearError();
  const validation = validateAutoscalingForm(autoscaling);
  if (!validation.ok) return showError(validation.message);
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/autoscaling`, {autoscaling, expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function validateAutoscalingForm(autoscaling) {
  clearInvalidInputs('[data-autoscaling-editor]');
  const integer = (field, label, options = {}) => {
    const input = qs(`[data-autoscaling-field="${field}"]`);
    const raw = input?.value?.trim() || '';
    if (!raw) {
      if (options.required) return invalidInput(input, `${label} is required.`);
      return {ok: true, value: 0, empty: true};
    }
    const value = Number(raw);
    if (!/^[0-9]+$/.test(raw) || !Number.isInteger(value)) return invalidInput(input, `${label} must be an integer.`);
    if (options.min !== undefined && value < options.min) return invalidInput(input, `${label} must be greater than or equal to ${options.min}.`);
    if (options.max !== undefined && value > options.max) return invalidInput(input, `${label} must be less than or equal to ${options.max}.`);
    return {ok: true, value, empty: false};
  };
  if (!autoscaling.enabled) {
    for (const [field, label] of [
      ['min-replicas', 'Min replicas'],
      ['max-replicas', 'Max replicas'],
      ['cpu-average-utilization', 'CPU average utilization'],
      ['memory-average-utilization', 'Memory average utilization'],
    ]) {
      const validation = integer(field, label, {min: 0});
      if (!validation.ok) return validation;
    }
    return {ok: true};
  }
  const min = integer('min-replicas', 'Min replicas', {required: true, min: 1});
  if (!min.ok) return min;
  const max = integer('max-replicas', 'Max replicas', {required: true, min: 1});
  if (!max.ok) return max;
  if (max.value < min.value) return invalidInput(qs('[data-autoscaling-field="max-replicas"]'), 'Max replicas must be greater than or equal to min replicas.');
  const cpu = integer('cpu-average-utilization', 'CPU average utilization', {min: 1, max: 100});
  if (!cpu.ok) return cpu;
  const memory = integer('memory-average-utilization', 'Memory average utilization', {min: 1, max: 100});
  if (!memory.ok) return memory;
  if (cpu.empty && memory.empty) return invalidInput(qs('[data-autoscaling-field="cpu-average-utilization"]'), 'Autoscaling requires CPU or memory average utilization.');
  return {ok: true};
}
function renderProbePreview(index, probes) {
  if (!probes?.enabled) return '<div class="text-muted small">Not configured.</div>';
  const modern = [
    probes.preset && chip('preset', probes.preset),
    probes.port && chip('port', probes.port),
    probes.path && chip('path', probes.path),
    Object.keys(probes.path_by_type || {}).length && chip('path variants', Object.keys(probes.path_by_type).join('/')),
    (probes.http?.port || probes.http?.path) && chip('shared HTTP', [probes.http?.port, probes.http?.path].filter(Boolean).join(' ')),
    probeHealthConfigured(probes.live) && chip('live', probeHealthSummary(probes.live)),
    probeHealthConfigured(probes.ready) && chip('ready', probeHealthSummary(probes.ready)),
    probeHealthConfigured(probes.start) && chip('start', probeHealthSummary(probes.start)),
  ].filter(Boolean).join('');
  const legacy = probes.legacy ? `<div class="alert alert-warning py-2 px-2 mb-2 small"><i class="ti ti-alert-triangle me-1"></i>Legacy ${esc(legacyProbeKinds(probes))} detected.</div>` : '';
  const custom = probes.legacy ? 'Legacy-only configuration.' : 'Custom probes configured.';
  return `${legacy}${modern ? `<div class="chip-row">${modern}</div>` : `<div class="text-muted small">${custom}</div>`}`;
}
function legacyProbeKinds(probes) {
  return (probes?.legacy_kinds || []).join(' + ') || 'health/probe';
}
function renderProbesEditor(index, probes) {
  if (state.readOnly) return '';
  const disabled = state.readOnly ? 'disabled' : '';
  const fixButton = probes?.legacy ? `<button class="btn btn-sm btn-outline-warning" type="button" data-fix-legacy-probes="${index}" ${disabled}><i class="ti ti-wand me-1"></i>Fix legacy probes</button>` : '';
  return `
    <div class="probe-editor mt-2" data-probe-editor="${index}">
      <div class="row g-2">
        ${probeInput(index, 'preset', 'Preset', probes?.preset || '')}
        ${probeInput(index, 'port', 'Port', probes?.port || '')}
        ${probeInput(index, 'path', 'Path', probes?.path || '')}
      </div>
      <div class="row g-2 mt-1">
        ${probeInput(index, 'http-port', 'Shared HTTP port', probes?.http?.port || '')}
        ${probeInput(index, 'http-path', 'Shared HTTP path', probes?.http?.path || '', 'col-12 col-md-8')}
      </div>
      <div class="row g-2 mt-1">
        ${probeInput(index, 'path-live', 'Live path variant', probes?.path_by_type?.live || '')}
        ${probeInput(index, 'path-ready', 'Ready path variant', probes?.path_by_type?.ready || '')}
        ${probeInput(index, 'path-start', 'Start path variant', probes?.path_by_type?.start || '')}
      </div>
      <div class="text-muted small mt-2">Path variants replace the scalar Path. Per-probe HTTP or exec settings override shared values.</div>
      <div class="row g-2 mt-1">
        ${renderProbeHealthEditor(index, 'live', 'Liveness', probes?.live || {})}
        ${renderProbeHealthEditor(index, 'ready', 'Readiness', probes?.ready || {})}
        ${renderProbeHealthEditor(index, 'start', 'Startup', probes?.start || {})}
      </div>
      <div class="d-flex align-items-center justify-content-between gap-2 mt-2">
        <div class="text-muted small">Known fields are updated; unknown YAML probe fields are preserved.</div>
        <div class="d-flex gap-2 flex-wrap justify-content-end">
          ${fixButton}
          <button class="btn btn-sm btn-primary" type="button" data-save-probes="${index}" ${disabled}><i class="ti ti-device-floppy me-1"></i>Save probes</button>
        </div>
      </div>
    </div>`;
}
function probeInput(index, field, label, value, columns = 'col-12 col-md-4') {
  const disabled = state.readOnly ? 'disabled' : '';
  return `<div class="${columns}"><label class="form-label small mb-1">${esc(label)}</label><input class="form-control form-control-sm font-monospace" data-probe-field="${index}:${field}" value="${esc(value)}" ${disabled}></div>`;
}
function probeHealthConfigured(health) {
  return !!(health?.http?.port || health?.http?.path || health?.command?.length || health?.delay || health?.period || health?.timeout || health?.success || health?.failure);
}
function probeHealthMode(health) {
  if (health?.command?.length) return 'exec';
  if (health?.http?.port || health?.http?.path) return 'http';
  return 'inherit';
}
function probeHealthSummary(health) {
  const mode = probeHealthMode(health);
  return mode === 'exec' ? 'exec' : mode === 'http' ? `HTTP ${health?.http?.port || ''}${health?.http?.path || ''}`.trim() : 'timing override';
}
function renderProbeHealthEditor(index, kind, label, health) {
  const disabled = state.readOnly ? 'disabled' : '';
  const mode = probeHealthMode(health);
  return `<div class="col-12"><div class="card card-sm"><div class="card-body py-2">
    <div class="row g-2 align-items-end">
      <div class="col-12 col-md-2"><label class="form-label small mb-1">${esc(label)} handler</label><select class="form-select form-select-sm" data-probe-mode="${index}:${kind}" ${disabled}>
        <option value="inherit" ${mode === 'inherit' ? 'selected' : ''}>Shared / preset</option><option value="http" ${mode === 'http' ? 'selected' : ''}>HTTP</option><option value="exec" ${mode === 'exec' ? 'selected' : ''}>Exec</option>
      </select></div>
      <div class="col-12 col-md-5 ${mode === 'http' ? '' : 'd-none'}" data-probe-http-fields="${index}:${kind}"><div class="row g-2">
        ${probeInput(index, `${kind}-http-port`, 'HTTP port', health?.http?.port || '', 'col-4')}
        ${probeInput(index, `${kind}-http-path`, 'HTTP path', health?.http?.path || '', 'col-8')}
      </div></div>
      <div class="col-12 col-md-5 ${mode === 'exec' ? '' : 'd-none'}" data-probe-exec-fields="${index}:${kind}"><label class="form-label small mb-1">Exec command, one argument per line</label><textarea class="form-control form-control-sm font-monospace" rows="2" data-probe-field="${index}:${kind}-command" ${disabled}>${esc((health?.command || []).join('\n'))}</textarea></div>
      ${['delay', 'period', 'timeout', 'success', 'failure'].map((field) => probeInput(index, `${kind}-${field}`, field[0].toUpperCase() + field.slice(1), health?.[field] || '', 'col-6 col-md-2')).join('')}
    </div>
  </div></div></div>`;
}
function renderJavaRuntimeEditor(index, java) {
  if (state.readOnly) return '';
  const disabled = state.readOnly ? 'disabled' : '';
  return `
    <div class="resource-editor mt-2" data-runtime-editor="${index}">
      <div class="row g-2">
        ${runtimeInput(index, 'xms', 'Xms', java?.xms || '')}
        ${runtimeInput(index, 'xmx', 'Xmx', java?.xmx || '')}
        ${runtimeInput(index, 'export-env-name', 'Export env', java?.export_env_name || 'JAVA_OPTS')}
        <div class="col-12">
          <label class="form-label small mb-1">Opts</label>
          <textarea class="form-control form-control-sm font-monospace" rows="3" data-runtime-field="${index}:opts" ${disabled}>${esc((java?.opts || []).join('\n'))}</textarea>
        </div>
      </div>
      <div class="d-flex align-items-center justify-content-between gap-2 mt-2">
        <div class="text-muted small">Saves runtime.java xms/xmx/opts/export.env_name. Empty fields remove the runtime block.</div>
        <button class="btn btn-sm btn-primary" type="button" data-save-runtime="${index}" ${disabled}><i class="ti ti-device-floppy me-1"></i>Save runtime</button>
      </div>
    </div>`;
}
function runtimeInput(index, field, label, value) {
  const disabled = state.readOnly ? 'disabled' : '';
  return `<div class="col-12 col-md-4"><label class="form-label small mb-1">${esc(label)}</label><input class="form-control form-control-sm font-monospace" data-runtime-field="${index}:${field}" value="${esc(value)}" ${disabled}></div>`;
}
function renderResourcesEditor(index, resources) {
  if (state.readOnly) return '';
  const disabled = state.readOnly ? 'disabled' : '';
  return `
    <div class="resource-editor mt-2" data-resource-editor="${index}">
      <div class="row g-2">
        ${resourceInput(index, 'cpu-request', 'CPU req', resources.cpu_request || '')}
        ${resourceInput(index, 'cpu-limit', 'CPU lim', resources.cpu_limit || '')}
        ${resourceInput(index, 'memory-request', 'Mem req', resources.memory_request || '')}
        ${resourceInput(index, 'memory-limit', 'Mem lim', resources.memory_limit || '')}
        ${resourceInput(index, 'ephemeral-storage-request', 'Ephemeral req', resources.ephemeral_storage_request || '')}
        ${resourceInput(index, 'ephemeral-storage-limit', 'Ephemeral lim', resources.ephemeral_storage_limit || '')}
      </div>
      <div class="d-flex align-items-center justify-content-between gap-2 mt-2">
        <div class="text-muted small">Saves CPU, memory and ephemeral-storage requests/limits.</div>
        <button class="btn btn-sm btn-primary" type="button" data-save-resources="${index}" ${disabled}><i class="ti ti-device-floppy me-1"></i>Save resources</button>
      </div>
    </div>`;
}
function resourceInput(index, field, label, value) {
  const disabled = state.readOnly ? 'disabled' : '';
  return `<div class="col-6"><label class="form-label small mb-1">${esc(label)}</label><input class="form-control form-control-sm font-monospace" data-resource-field="${index}:${field}" value="${esc(value)}" ${disabled}></div>`;
}
async function saveContainerResources(index) {
  if (!state.env || !state.appFile || state.readOnly || !Number.isInteger(index)) return;
  const value = (field) => qs(`[data-resource-field="${index}:${field}"]`)?.value?.trim() || '';
  const resources = {
    cpu_request: value('cpu-request'),
    cpu_limit: value('cpu-limit'),
    memory_request: value('memory-request'),
    memory_limit: value('memory-limit'),
    ephemeral_storage_request: value('ephemeral-storage-request'),
    ephemeral_storage_limit: value('ephemeral-storage-limit'),
  };
  clearError();
  const validation = validateResourcesForm(index);
  if (!validation.ok) return showError(validation.message);
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/containers/${index}/resources`, {resources, expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
const cpuQuantityRe = /^([0-9]+(\.[0-9]+)?|\.[0-9]+)m?$/;
const memoryQuantityRe = /^([0-9]+(\.[0-9]+)?|\.[0-9]+)(Ki|Mi|Gi|Ti|Pi|Ei|K|M|G|T|P|E)?$/;
const javaMemoryQuantityRe = /^[0-9]+[kKmMgGtT]?[bB]?$/;
const variableNameRe = /^[A-Za-z_][A-Za-z0-9_]*$/;
function validateResourcesForm(index) {
  clearInvalidInputs(`[data-resource-editor="${index}"]`);
  const checks = [
    ['cpu-request', 'CPU request', cpuQuantityRe, 'Use values like 100m, 0.5 or 1.'],
    ['cpu-limit', 'CPU limit', cpuQuantityRe, 'Use values like 100m, 0.5 or 1.'],
    ['memory-request', 'Memory request', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['memory-limit', 'Memory limit', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['ephemeral-storage-request', 'Ephemeral storage request', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['ephemeral-storage-limit', 'Ephemeral storage limit', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
  ];
  for (const [field, label, pattern, hint] of checks) {
    const input = qs(`[data-resource-field="${index}:${field}"]`);
    const value = input?.value?.trim() || '';
    if (value && !pattern.test(value) && !completeSourceTemplateRe.test(value)) return invalidInput(input, `${label} is invalid. ${hint} A complete {{env:NAME}} or {{var:NAME}} template is also allowed.`);
  }
  return {ok: true};
}
function clearInvalidInputs(scopeSelector) {
  clearInvalidInputsIn(qs(scopeSelector));
}
function clearInvalidInputsIn(scope) {
  scope?.querySelectorAll?.('.is-invalid')?.forEach((input) => {
    input.classList.remove('is-invalid');
    input.removeAttribute('aria-invalid');
    input.removeAttribute('aria-describedby');
  });
  scope?.querySelectorAll?.('[data-validation-feedback]')?.forEach((feedback) => feedback.remove());
}
function invalidInput(input, message) {
  input?.classList.add('is-invalid');
  input?.setAttribute('aria-invalid', 'true');
  renderValidationFeedback(input, message);
  input?.focus();
  return {ok: false, message};
}
function renderValidationFeedback(input, message) {
  if (!input) return;
  const host = validationFeedbackHost(input);
  if (host.nextElementSibling?.hasAttribute('data-validation-feedback')) host.nextElementSibling.remove();
  const feedback = document.createElement('div');
  const feedbackId = `validation-feedback-${Math.random().toString(36).slice(2)}`;
  feedback.id = feedbackId;
  feedback.className = 'invalid-feedback validation-feedback d-block';
  feedback.dataset.validationFeedback = 'true';
  feedback.textContent = message;
  host.insertAdjacentElement('afterend', feedback);
  input.setAttribute('aria-describedby', feedbackId);
}
function validationFeedbackHost(input) {
  return input.closest('.input-group') || input;
}
function validateNamedRows(scope, inputSelector, label) {
  if (!scope) return {ok: true};
  clearInvalidInputsIn(scope);
  const seen = new Set();
  for (const input of Array.from(scope.querySelectorAll(inputSelector))) {
    const value = input.value.trim();
    if (!value) continue;
    if (!variableNameRe.test(value)) return invalidInput(input, `${label} must match [A-Za-z_][A-Za-z0-9_]*.`);
    if (seen.has(value)) return invalidInput(input, `Duplicate ${label.toLowerCase()} "${value}".`);
    seen.add(value);
  }
  return {ok: true};
}
async function saveContainerRuntime(index) {
  if (!state.env || !state.appFile || state.readOnly || !Number.isInteger(index)) return;
  const value = (field) => qs(`[data-runtime-field="${index}:${field}"]`)?.value?.trim() || '';
  const opts = (qs(`[data-runtime-field="${index}:opts"]`)?.value || '').split('\n').map((item) => item.trim()).filter(Boolean);
  const runtime = {
    xms: value('xms'),
    xmx: value('xmx'),
    opts,
    export_env_name: value('export-env-name'),
  };
  clearError();
  const validation = validateRuntimeForm(index);
  if (!validation.ok) return showError(validation.message);
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/containers/${index}/runtime/java`, {runtime, expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function validateRuntimeForm(index) {
  clearInvalidInputs(`[data-runtime-editor="${index}"]`);
  for (const [field, label] of [['xms', 'Xms'], ['xmx', 'Xmx']]) {
    const input = qs(`[data-runtime-field="${index}:${field}"]`);
    const value = input?.value?.trim() || '';
    if (value && !javaMemoryQuantityRe.test(value)) return invalidInput(input, `${label} must be a JVM memory quantity like 256m, 1g or 512M.`);
  }
  const exportInput = qs(`[data-runtime-field="${index}:export-env-name"]`);
  const exportEnv = exportInput?.value?.trim() || '';
  if (exportEnv && !variableNameRe.test(exportEnv)) return invalidInput(exportInput, 'Export env must match [A-Za-z_][A-Za-z0-9_]*.');
  return {ok: true};
}
async function saveContainerProbes(index) {
  if (!state.env || !state.appFile || state.readOnly || !Number.isInteger(index)) return;
  const value = (field) => qs(`[data-probe-field="${index}:${field}"]`)?.value?.trim() || '';
  const health = (kind) => {
    const mode = qs(`[data-probe-mode="${index}:${kind}"]`)?.value || 'inherit';
    return {
      http: {port: mode === 'http' ? value(`${kind}-http-port`) : '', path: mode === 'http' ? value(`${kind}-http-path`) : ''},
      command: mode === 'exec' ? value(`${kind}-command`).split(/\r?\n/).map((item) => item.trim()).filter(Boolean) : [],
      delay: value(`${kind}-delay`), period: value(`${kind}-period`), timeout: value(`${kind}-timeout`),
      success: value(`${kind}-success`), failure: value(`${kind}-failure`),
    };
  };
  const probes = {
    preset: value('preset'),
    port: value('port'),
    path: value('path'),
    path_by_type: {live: value('path-live'), ready: value('path-ready'), start: value('path-start')},
    http: {port: value('http-port'), path: value('http-path')},
    live: health('live'), ready: health('ready'), start: health('start'),
  };
  clearError();
  const validation = validateProbeForm(index);
  if (!validation.ok) return showError(validation.message);
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/containers/${index}/probes`, {probes, expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function validateProbeForm(index) {
  clearInvalidInputs(`[data-probe-editor="${index}"]`);
  for (const [field, label] of [
    ['port', 'Probe port'], ['http-port', 'Shared HTTP port'],
    ['live-http-port', 'Liveness HTTP port'], ['ready-http-port', 'Readiness HTTP port'], ['start-http-port', 'Startup HTTP port'],
  ]) {
    const input = qs(`[data-probe-field="${index}:${field}"]`);
    const value = input?.value?.trim() || '';
    if (value && !completeSourceTemplateRe.test(value)) {
      const validation = validatePortValue(input, label);
      if (!validation.ok) return validation;
    }
  }
  for (const kind of ['live', 'ready', 'start']) {
    for (const field of ['delay', 'period', 'timeout', 'success', 'failure']) {
      const input = qs(`[data-probe-field="${index}:${kind}-${field}"]`);
      const value = input?.value?.trim() || '';
      if (!value || completeSourceTemplateRe.test(value)) continue;
      const number = Number(value);
      const minimum = field === 'delay' ? 0 : 1;
      if (!/^[0-9]+$/.test(value) || !Number.isInteger(number) || number < minimum) return invalidInput(input, `${kind} ${field} must be an integer greater than or equal to ${minimum}.`);
    }
    const mode = qs(`[data-probe-mode="${index}:${kind}"]`)?.value || 'inherit';
    if (mode === 'http' && !qs(`[data-probe-field="${index}:${kind}-http-port"]`)?.value?.trim() && !qs(`[data-probe-field="${index}:${kind}-http-path"]`)?.value?.trim()) {
      return invalidInput(qs(`[data-probe-mode="${index}:${kind}"]`), `${kind} HTTP requires a port or path.`);
    }
    if (mode === 'exec' && !qs(`[data-probe-field="${index}:${kind}-command"]`)?.value?.trim()) {
      return invalidInput(qs(`[data-probe-field="${index}:${kind}-command"]`), `${kind} exec requires at least one command argument.`);
    }
  }
  return {ok: true};
}
async function fixContainerLegacyProbes(index) {
  if (!state.env || !state.appFile || state.readOnly || !Number.isInteger(index)) return;
  const confirmed = await confirmAction({
    title: 'Fix legacy probes?',
    body: 'Convert legacy health/probe configuration to the modern probes block for this container.',
    subject: state.appFile,
    confirmLabel: 'Fix probes',
  });
  if (!confirmed) return;
  clearError();
  try {
    await apiPostJSON(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/containers/${index}/probes/fix-legacy`, {expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function renderPortsEditor(index, ports) {
  if (state.readOnly) return '';
  const disabled = state.readOnly ? 'disabled' : '';
  const rows = (ports || []).map((port) => renderPortRow(index, port)).join('');
  return `
    <div class="container-env-editor mt-3" data-ports-editor="${index}">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Ports / services / ingress</div>
        <button class="btn btn-sm btn-outline-primary" type="button" data-add-port="${index}" ${disabled}><i class="ti ti-plus me-1"></i>Add port</button>
      </div>
      <div data-port-body="${index}">${rows || renderPortsEmpty(index)}</div>
      <div class="d-flex align-items-center justify-content-between gap-2 mt-2">
        <div class="text-muted small">Uses service_name as Service DNS name. Legacy expose_as.hostname is read as an alias.</div>
        <button class="btn btn-sm btn-primary" type="button" data-save-ports="${index}" ${disabled}><i class="ti ti-device-floppy me-1"></i>Save ports</button>
      </div>
    </div>`;
}
function renderPortsPreview(index, ports) {
  const rows = (ports || []).map((port) => renderPortPreviewLine(port)).join('');
  const edit = state.readOnly ? '' : `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-ports="${index}"><i class="ti ti-pencil me-1"></i>Edit ports</button>`;
  return `
    <div class="overview-section">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Ports / services / ingress</div>
        ${edit}
      </div>
      <div class="ports-preview">${rows || emptyState('ti-route', 'No ports', state.readOnly ? 'No container ports configured.' : 'Use Edit ports to add container ports.')}</div>
    </div>`;
}
function renderContainerEnvsPreview(index, vars) {
  const edit = state.readOnly ? '' : `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-panel="container-envs" data-container-index="${index}"><i class="ti ti-pencil me-1"></i>Edit envs</button>`;
  const chips = (vars || []).slice(0, 8).map((item) => chip(item.name || '?', envVarPreviewValue(item))).join('');
  const more = (vars || []).length > 8 ? `<span class="badge bg-secondary-lt">+${vars.length - 8} more</span>` : '';
  return `
    <div class="overview-section">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Container envs</div>
        ${edit}
      </div>
      ${(vars || []).length ? `<div class="chip-row">${chips}${more}</div>` : emptyState('ti-variable', 'No container envs', state.readOnly ? '' : 'Use Edit envs to add container envs.')}
    </div>`;
}
function envVarPreviewValue(item) {
  if (item.kind === 'value') return item.value ?? '';
  if (item.kind === 'workload_identity_token') return `token: ${item.workload_identity_token_ref_name || '?'}`;
  if (item.kind === 'shared_asset') return `asset: ${item.shared_asset_ref_name || '?'}`;
  if (item.kind === 'remove') return 'remove inherited';
  return envVarReadOnlyValue(item) || item.kind || 'unknown';
}
function renderPortPreviewLine(port) {
  const exposes = (port.expose_as || []).map((expose) => {
    const externals = (expose.externals || []).map((external) => {
      const kind = external.as_route ? 'route' : 'ingress';
      const hosts = [...(external.http || []), ...(external.https || [])].map((host) => host.hostname).filter(Boolean);
      const tls = external.tls_termination ? ` / TLS ${external.tls_termination}` : '';
      return `<div class="ports-preview-external"><i class="ti ti-world me-1"></i>${esc(kind)} ${esc(external.name || '?')}${hosts.length ? ` -> ${esc(hosts.join(', '))}` : ''}${esc(tls)}</div>`;
    }).join('');
    return `<div class="ports-preview-service"><i class="ti ti-plug-connected me-1"></i>svc <span class="font-monospace">${esc(expose.service_name || expose.hostname || '?')}:${esc(expose.port || '?')}</span>${externals}</div>`;
  }).join('');
  return `
    <div class="ports-preview-item">
      <div><i class="ti ti-route me-1"></i><span class="font-monospace">${esc(port.name || '?')}:${esc(port.port || '?')}</span>${port.metrics ? ' <span class="badge bg-green-lt ms-1">metrics</span>' : ''}</div>
      ${exposes || '<div class="text-muted small ms-4">No service exposure.</div>'}
    </div>`;
}
function openPortsEditorModal(index) {
  if (state.readOnly || !state.model || !Number.isInteger(index)) return;
  const container = (state.model.containers || []).find((item) => item.index === index);
  if (!container) return;
  const modal = qs('#ports-edit-modal');
  state.portsModalClose = openContentModal({
    modal,
    titleSelector: '#ports-edit-modal-title',
    subtitleSelector: '#ports-edit-modal-subtitle',
    bodySelector: '#ports-edit-modal-body',
    title: `Edit ports: ${container.name || `container-${index + 1}`}`,
    subtitle: 'Container ports, services and external exposure.',
    body: renderPortsEditor(index, container.ports || []),
    focusSelector: '[data-add-port], [data-save-ports], button, input, textarea, select',
    onClosed: () => { state.portsModalClose = null; },
  });
}
function openEditPanel(panel, index) {
  if (state.readOnly || !state.model) return;
  const container = Number.isInteger(index) ? (state.model.containers || []).find((item) => item.index === index) : null;
  const titleName = container?.name || (Number.isInteger(index) ? `container-${index + 1}` : state.appFile);
  const modal = qs('#edit-modal');
  let title = 'Edit';
  let subtitle = '';
  let body = '';
  if (panel === 'resources' && container) {
    title = `Edit resources: ${titleName}`;
    subtitle = 'Container CPU and memory requests/limits.';
    body = renderResourcesEditor(index, container.resources || {});
  } else if (panel === 'runtime' && container) {
    title = `Edit Java runtime: ${titleName}`;
    subtitle = 'runtime.java xms/xmx/opts/export.env_name.';
    body = renderJavaRuntimeEditor(index, container.runtime?.java || {});
  } else if (panel === 'probes' && container) {
    title = `Edit probes: ${titleName}`;
    subtitle = 'Shared and per-probe HTTP, exec and timing configuration.';
    body = renderProbesEditor(index, container.probes || {});
  } else if (panel === 'container-envs' && container) {
    title = `Edit envs: ${titleName}`;
    subtitle = 'Container-local environment variables.';
    body = renderContainerEnvsEditor(index, container.envs || []);
  } else if (panel === 'autoscaling') {
    title = 'Edit autoscaling';
    subtitle = 'Top-level HPA settings.';
    body = renderAutoscalingEditor(state.model.autoscaling || {});
  } else if (panel === 'replicas') {
    title = 'Edit replicas';
    subtitle = 'Top-level deployment replica count.';
    body = renderReplicasEditorModal(state.model);
  } else if (panel === 'app-vars') {
    title = 'Edit local variables';
    subtitle = 'Top-level vars used by {{var:NAME}} placeholders.';
    body = renderAppVarsEditorModal(state.appVars || []);
  } else if (panel === 'references') {
    title = 'Edit references';
    subtitle = 'Ordered references to definitions stored in apps/_defaults.yml.';
    body = renderReferencesEditor(state.appReferences);
  }
  if (!body) return;
  state.editModalClose = openContentModal({
    modal,
    titleSelector: '#edit-modal-title',
    subtitleSelector: '#edit-modal-subtitle',
    bodySelector: '#edit-modal-body',
    title,
    subtitle,
    body,
    wide: panel === 'container-envs',
    onClosed: () => { state.editModalClose = null; },
  });
  syncAutoscalingEditor();
}
function handleEditModalClick(e) {
  if (e.target.closest('[data-save-sidecar-env-override]') && !state.readOnly) return saveSidecarEnvOverride('set');
  if (e.target.closest('[data-reset-sidecar-env-override]') && !state.readOnly) return confirmResetSidecarEnvOverride();
  if (e.target.closest('[data-save-sidecar-resources-override]') && !state.readOnly) return saveSidecarResourcesOverride('set');
  if (e.target.closest('[data-reset-sidecar-resources-override]') && !state.readOnly) return confirmResetSidecarResourcesOverride();
  if (e.target.closest('[data-save-sidecar-startup-override]') && !state.readOnly) return saveSidecarStartupOverride('set');
  if (e.target.closest('[data-reset-sidecar-startup-override]') && !state.readOnly) return confirmResetSidecarStartupOverride();
  const saveResources = e.target.closest('[data-save-resources]');
  if (saveResources && !state.readOnly) return saveContainerResources(Number(saveResources.dataset.saveResources));
  const saveRuntime = e.target.closest('[data-save-runtime]');
  if (saveRuntime && !state.readOnly) return saveContainerRuntime(Number(saveRuntime.dataset.saveRuntime));
  const saveProbes = e.target.closest('[data-save-probes]');
  if (saveProbes && !state.readOnly) return saveContainerProbes(Number(saveProbes.dataset.saveProbes));
  const fixLegacyProbes = e.target.closest('[data-fix-legacy-probes]');
  if (fixLegacyProbes && !state.readOnly) return fixContainerLegacyProbes(Number(fixLegacyProbes.dataset.fixLegacyProbes));
  const saveVars = e.target.closest('[data-save-container-envs]');
  if (saveVars && !state.readOnly) return saveContainerEnvs(Number(saveVars.dataset.saveContainerEnvs));
  const addVar = e.target.closest('[data-add-container-env]');
  if (addVar && !state.readOnly) return addContainerEnvRow(Number(addVar.dataset.addContainerEnv));
  const removeVar = e.target.closest('[data-remove-container-env]');
  if (removeVar && !state.readOnly) {
    const index = Number(removeVar.dataset.removeContainerEnv);
    removeVar.closest('tr')?.remove();
    syncContainerEnvsEmptyState(index);
  }
  const saveAutoscaling = e.target.closest('[data-save-autoscaling]');
  if (saveAutoscaling && !state.readOnly) return saveAppAutoscaling();
  const saveReplicas = e.target.closest('[data-save-replicas-modal]');
  if (saveReplicas && !state.readOnly) return saveAppReplicas();
  const addAppVar = e.target.closest('[data-add-app-var-modal]');
  if (addAppVar && !state.readOnly) return addAppVarRow();
  const removeAppVar = e.target.closest('[data-remove-app-var]');
  if (removeAppVar && !state.readOnly) {
    removeAppVar.closest('tr')?.remove();
    syncAppVarsEmptyState();
  }
  const saveAppVar = e.target.closest('[data-save-app-vars-modal]');
  if (saveAppVar && !state.readOnly) return saveAppVars();
  if (e.target.closest('[data-save-references]') && !state.readOnly) return saveAppReferences();
  if (e.target.closest('[data-save-defaults-sidecar-definition]') && !state.readOnly) return saveDefaultsSidecarDefinition();
  if (e.target.closest('[data-save-defaults-sidecar-startup]') && !state.readOnly) return saveDefaultsSidecarStartup('set');
  if (e.target.closest('[data-remove-defaults-sidecar-startup]') && !state.readOnly) return confirmRemoveDefaultsSidecarStartup();
  if (e.target.closest('[data-save-defaults-sidecar-resources]') && !state.readOnly) return saveDefaultsSidecarResources('set');
  if (e.target.closest('[data-remove-defaults-sidecar-resources]') && !state.readOnly) return confirmRemoveDefaultsSidecarResources();
  if (e.target.closest('[data-save-defaults-sidecar-envs]') && !state.readOnly) return saveDefaultsSidecarEnvs('set');
  if (e.target.closest('[data-remove-defaults-sidecar-envs]') && !state.readOnly) return confirmRemoveDefaultsSidecarEnvs();
  if (e.target.closest('[data-add-defaults-sidecar-env]') && !state.readOnly) return addDefaultsSidecarEnvRow();
  const removeSidecarEnv = e.target.closest('[data-remove-defaults-sidecar-env]');
  if (removeSidecarEnv && !state.readOnly) return removeSidecarEnv.closest('[data-defaults-sidecar-env-row]')?.remove();
  const moveSidecarEnv = e.target.closest('[data-move-defaults-sidecar-env]');
  if (moveSidecarEnv && !state.readOnly) return moveDefaultsSidecarEnvRow(moveSidecarEnv.closest('[data-defaults-sidecar-env-row]'), moveSidecarEnv.dataset.moveDefaultsSidecarEnv);
  if (e.target.closest('[data-save-defaults-sidecar-references]') && !state.readOnly) return saveDefaultsSidecarReferences();
  const addReference = e.target.closest('[data-add-reference]');
  if (addReference && !state.readOnly) return addReferenceRow(addReference.closest('[data-reference-list]'));
  const removeReference = e.target.closest('[data-remove-reference]');
  if (removeReference && !state.readOnly) return removeReferenceRow(removeReference.closest('[data-reference-row]'));
  const moveReference = e.target.closest('[data-move-reference]');
  if (moveReference && !state.readOnly) return moveReferenceRow(moveReference.closest('[data-reference-row]'), moveReference.dataset.moveReference);
  if (e.target.closest('[data-save-defaults-vars]') && !state.readOnly) return saveDefaultsVars();
  if (e.target.closest('[data-add-defaults-var]') && !state.readOnly) return addDefaultsVarRow();
  const removeDefaultsVar = e.target.closest('[data-remove-defaults-var]');
  if (removeDefaultsVar && !state.readOnly) {
    removeDefaultsVar.closest('tr')?.remove();
    syncDefaultsVarsEmptyState();
  }
  if (e.target.closest('[data-save-defaults-container-envs]') && !state.readOnly) return saveDefaultsContainerEnvs();
  if (e.target.closest('[data-add-defaults-env-group]') && !state.readOnly) return addDefaultsEnvGroup();
  const removeGroup = e.target.closest('[data-remove-defaults-env-group]');
  if (removeGroup && !state.readOnly) return removeGroup.closest('[data-defaults-env-group]')?.remove();
  const addEnv = e.target.closest('[data-add-defaults-env]');
  if (addEnv && !state.readOnly) return addDefaultsEnvRow(addEnv.closest('[data-defaults-env-group]'));
  const removeEnv = e.target.closest('[data-remove-defaults-env]');
  if (removeEnv && !state.readOnly) return removeEnv.closest('tr')?.remove();
  if (e.target.closest('[data-save-special-entries]') && !state.readOnly) return saveSpecialEntries();
  if (e.target.closest('[data-add-special-entry]') && !state.readOnly) return addSpecialEntryRow();
  const specialValue = e.target.closest('[data-edit-special-value]');
  if (specialValue && !state.readOnly) return openSpecialValueDialog(specialValue.closest('[data-special-entry-row]'));
  const specialAction = e.target.closest('[data-special-entry-action]');
  if (specialAction && !state.readOnly) return handleSpecialEntryAction(specialAction);
}

function sidecarEnvOverrideURL(sidecarName, envName) {
  return `/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/sidecars/${encodeURIComponent(sidecarName)}/envs/${encodeURIComponent(envName)}/override`;
}

async function openSidecarEnvOverride(sidecarName, envName, effectiveValue) {
  if (state.readOnly || !state.appFile) return;
  clearError();
  try {
    const override = await api(sidecarEnvOverrideURL(sidecarName, envName));
    state.sidecarEnvOverride = {...override, effective_value: effectiveValue};
    const localValue = override.local?.is_value_editable ? (override.local.value ?? '') : effectiveValue;
    const reset = override.local ? `<button class="btn btn-outline-warning" type="button" data-reset-sidecar-env-override><i class="ti ti-arrow-back-up me-1"></i>${override.shared_definition ? 'Reset to inherited' : 'Remove local value'}</button>` : '';
    const context = override.shared_definition
      ? (override.local ? 'This value currently has an app-local patch over the shared sidecar definition.' : 'This creates a minimal app-local patch. Other sidecar fields remain inherited.')
      : 'This sidecar is defined only in the application YAML.';
    const body = `<div data-sidecar-env-override-editor>
      <div class="alert alert-info py-2"><i class="ti ti-info-circle me-2"></i>${esc(context)}</div>
      <div class="mb-3"><label class="form-label">Sidecar</label><input class="form-control font-monospace" value="${esc(sidecarName)}" disabled></div>
      <div class="mb-3"><label class="form-label">Environment variable</label><input class="form-control font-monospace" value="${esc(envName)}" disabled></div>
      <div class="mb-3"><label class="form-label" for="sidecar-env-override-value">Local value</label><input class="form-control font-monospace" id="sidecar-env-override-value" value="${esc(localValue)}"></div>
      <div class="d-flex align-items-center justify-content-between gap-2">${reset}<button class="btn btn-primary ms-auto" type="button" data-save-sidecar-env-override><i class="ti ti-device-floppy me-1"></i>Save override</button></div>
    </div>`;
    state.editModalClose = openContentModal({
      modal: qs('#edit-modal'), titleSelector: '#edit-modal-title', subtitleSelector: '#edit-modal-subtitle', bodySelector: '#edit-modal-body',
      title: `Override ${envName}`, subtitle: `Sidecar ${sidecarName}`, body, focusSelector: '#sidecar-env-override-value',
      onClosed: () => { state.editModalClose = null; state.sidecarEnvOverride = null; },
    });
  } catch (e) { showError(e); }
}

async function confirmResetSidecarEnvOverride() {
  const current = state.sidecarEnvOverride;
  if (!current) return;
  const confirmed = await confirmAction({
    title: current.shared_definition ? 'Reset to inherited value?' : 'Remove local value?',
    body: current.shared_definition ? 'Only the local environment override will be removed. The shared sidecar reference and inherited definition remain unchanged.' : 'The environment item will be removed from this app-only sidecar.',
    subject: `${current.sidecar_name} / ${current.env_name}`,
    confirmLabel: current.shared_definition ? 'Reset override' : 'Remove value',
    confirmClass: 'btn-warning', statusClass: 'bg-warning',
  });
  if (confirmed) await saveSidecarEnvOverride('reset');
}

async function saveSidecarEnvOverride(action) {
  const current = state.sidecarEnvOverride;
  if (state.readOnly || !current) return;
  clearError();
  try {
    await apiPatch(sidecarEnvOverrideURL(current.sidecar_name, current.env_name), {
      expected_hash: current.content_hash,
      expected_defaults_hash: current.defaults_hash,
      override: {action, value: action === 'set' ? (qs('#sidecar-env-override-value')?.value ?? '') : ''},
    });
    state.editModalClose?.();
    state.editModalClose = null;
    state.sidecarEnvOverride = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}

function sidecarResourcesOverrideURL(sidecarName) {
  return `/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/sidecars/${encodeURIComponent(sidecarName)}/resources/override`;
}

function effectiveResourceForm(resources) {
  const value = (name, side) => resources?.[name]?.[side] ?? resources?.[name]?.[side === 'requests' ? 'from' : 'to'] ?? '';
  return {
    cpu_request: value('cpu', 'requests'), cpu_limit: value('cpu', 'limits'),
    memory_request: value('memory', 'requests'), memory_limit: value('memory', 'limits'),
    ephemeral_storage_request: value('ephemeral-storage', 'requests'), ephemeral_storage_limit: value('ephemeral-storage', 'limits'),
  };
}

function renderSidecarResourcesOverrideEditor(current, local, effective) {
  const input = (field, label) => `<div class="col-6"><label class="form-label small mb-1" for="sidecar-resource-${field}">${esc(label)}</label><input class="form-control form-control-sm font-monospace" id="sidecar-resource-${field}" data-sidecar-resource-field="${field}" value="${esc(local?.[field] || '')}" placeholder="Effective: ${esc(effective[field] || '-')}"></div>`;
  const reset = current.local ? `<button class="btn btn-outline-warning" type="button" data-reset-sidecar-resources-override><i class="ti ti-arrow-back-up me-1"></i>${current.shared_definition ? 'Reset to inherited' : 'Remove local resources'}</button>` : '';
  return `<div data-sidecar-resources-override-editor>
    <div class="alert alert-info py-2"><i class="ti ti-info-circle me-2"></i>${esc(current.shared_definition ? 'This writes only a minimal app-local resources patch. Other sidecar fields remain inherited.' : 'This sidecar is defined only in the application YAML.')}</div>
    <div class="row g-2">
      ${input('cpu_request', 'CPU request')}${input('cpu_limit', 'CPU limit')}
      ${input('memory_request', 'Memory request')}${input('memory_limit', 'Memory limit')}
      ${input('ephemeral_storage_request', 'Ephemeral storage request')}${input('ephemeral_storage_limit', 'Ephemeral storage limit')}
    </div>
    <div class="d-flex align-items-center justify-content-between gap-2 mt-3">${reset}<button class="btn btn-primary ms-auto" type="button" data-save-sidecar-resources-override><i class="ti ti-device-floppy me-1"></i>Save override</button></div>
  </div>`;
}

async function openSidecarResourcesOverride(sidecarName) {
  if (state.readOnly || !state.appFile) return;
  clearError();
  try {
    const current = await api(sidecarResourcesOverrideURL(sidecarName));
    const effectiveContainer = (state.inspectedApp?.effective?.sidecars || []).find((item) => item.name === sidecarName);
    const effective = effectiveResourceForm(effectiveContainer?.resources || {});
    state.sidecarResourcesOverride = current;
    state.editModalClose = openContentModal({
      modal: qs('#edit-modal'), titleSelector: '#edit-modal-title', subtitleSelector: '#edit-modal-subtitle', bodySelector: '#edit-modal-body',
      title: `Edit resources: ${sidecarName}`, subtitle: current.shared_definition ? 'App-local override of a shared sidecar.' : 'App-only sidecar resources.',
      body: renderSidecarResourcesOverrideEditor(current, current.local, effective), focusSelector: '[data-sidecar-resource-field]',
      onClosed: () => { state.editModalClose = null; state.sidecarResourcesOverride = null; },
    });
  } catch (e) { showError(e); }
}

function sidecarResourceValues() {
  const value = (field) => qs(`[data-sidecar-resource-field="${field}"]`)?.value?.trim() || '';
  return {
    cpu_request: value('cpu_request'), cpu_limit: value('cpu_limit'),
    memory_request: value('memory_request'), memory_limit: value('memory_limit'),
    ephemeral_storage_request: value('ephemeral_storage_request'), ephemeral_storage_limit: value('ephemeral_storage_limit'),
  };
}

function validateSidecarResourcesForm() {
  clearInvalidInputs('[data-sidecar-resources-override-editor]');
  const checks = [
    ['cpu_request', 'CPU request', cpuQuantityRe, 'Use values like 100m, 0.5 or 1.'],
    ['cpu_limit', 'CPU limit', cpuQuantityRe, 'Use values like 100m, 0.5 or 1.'],
    ['memory_request', 'Memory request', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['memory_limit', 'Memory limit', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['ephemeral_storage_request', 'Ephemeral storage request', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['ephemeral_storage_limit', 'Ephemeral storage limit', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
  ];
  let populated = false;
  for (const [field, label, pattern, hint] of checks) {
    const input = qs(`[data-sidecar-resource-field="${field}"]`);
    const value = input?.value?.trim() || '';
    populated ||= value !== '';
    if (value && !pattern.test(value) && !completeSourceTemplateRe.test(value)) return invalidInput(input, `${label} is invalid. ${hint} A complete {{env:NAME}} or {{var:NAME}} template is also allowed.`);
  }
  if (!populated) return {ok: false, message: 'Enter at least one resource value, or use Reset to inherited.'};
  return {ok: true};
}

async function confirmResetSidecarResourcesOverride() {
  const current = state.sidecarResourcesOverride;
  if (!current) return;
  const confirmed = await confirmAction({
    title: current.shared_definition ? 'Reset resources to inherited?' : 'Remove local resources?',
    body: current.shared_definition ? 'Only the local resources block will be removed. The shared sidecar reference and other local patch fields remain unchanged.' : 'The resources block will be removed from this app-only sidecar.',
    subject: current.sidecar_name, confirmLabel: current.shared_definition ? 'Reset resources' : 'Remove resources', confirmClass: 'btn-warning', statusClass: 'bg-warning',
  });
  if (confirmed) await saveSidecarResourcesOverride('reset');
}

async function saveSidecarResourcesOverride(action) {
  const current = state.sidecarResourcesOverride;
  if (state.readOnly || !current) return;
  clearError();
  const resources = sidecarResourceValues();
  if (action === 'set') {
    const validation = validateSidecarResourcesForm();
    if (!validation.ok) return showError(validation.message);
  }
  try {
    await apiPatch(sidecarResourcesOverrideURL(current.sidecar_name), {
      expected_hash: current.content_hash, expected_defaults_hash: current.defaults_hash,
      override: {action, resources},
    });
    state.editModalClose?.();
    state.editModalClose = null;
    state.sidecarResourcesOverride = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}

function sidecarStartupOverrideURL(sidecarName) {
  return `/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/sidecars/${encodeURIComponent(sidecarName)}/startup/override`;
}

function startupLines(value) {
  return Array.isArray(value) ? value.map((item) => String(item)).join('\n') : '';
}

function renderSidecarStartupOverrideEditor(current, effective) {
  const local = current.local || {};
  const row = (field, label, present, values, effectiveValues) => `<div class="mb-3" data-startup-list-row="${field}">
    <label class="form-check form-switch mb-2"><input class="form-check-input" type="checkbox" data-startup-list-enabled="${field}" ${present ? 'checked' : ''}><span class="form-check-label">Override ${esc(label.toLowerCase())}</span></label>
    <textarea class="form-control font-monospace" rows="3" data-startup-list-value="${field}" placeholder="Effective, one item per line:\n${esc(startupLines(effectiveValues))}" ${present ? '' : 'disabled'}>${esc(startupLines(values))}</textarea>
  </div>`;
  const reset = current.local ? `<button class="btn btn-outline-warning" type="button" data-reset-sidecar-startup-override><i class="ti ti-arrow-back-up me-1"></i>${current.shared_definition ? 'Reset to inherited' : 'Remove local startup'}</button>` : '';
  return `<div data-sidecar-startup-override-editor>
    <div class="alert alert-info py-2"><i class="ti ti-info-circle me-2"></i>Command and arguments are whole-list overrides. Enable only the lists that should be local.</div>
    ${row('command', 'Command', local.command_present, local.command, effective.command)}
    ${row('arguments', 'Arguments', local.arguments_present, local.arguments, effective.arguments)}
    <div class="d-flex align-items-center justify-content-between gap-2">${reset}<button class="btn btn-primary ms-auto" type="button" data-save-sidecar-startup-override><i class="ti ti-device-floppy me-1"></i>Save override</button></div>
  </div>`;
}

async function openSidecarStartupOverride(sidecarName) {
  if (state.readOnly || !state.appFile) return;
  clearError();
  try {
    const current = await api(sidecarStartupOverrideURL(sidecarName));
    const effectiveContainer = (state.inspectedApp?.effective?.sidecars || []).find((item) => item.name === sidecarName);
    state.sidecarStartupOverride = current;
    state.editModalClose = openContentModal({
      modal: qs('#edit-modal'), titleSelector: '#edit-modal-title', subtitleSelector: '#edit-modal-subtitle', bodySelector: '#edit-modal-body',
      title: `Edit startup: ${sidecarName}`, subtitle: current.shared_definition ? 'App-local override of a shared sidecar.' : 'App-only sidecar startup.',
      body: renderSidecarStartupOverrideEditor(current, effectiveContainer?.startup || {}), focusSelector: '[data-startup-list-enabled]',
      onClosed: () => { state.editModalClose = null; state.sidecarStartupOverride = null; },
    });
    qsa('[data-startup-list-enabled]').forEach((toggle) => toggle.addEventListener('change', () => {
      const field = toggle.dataset.startupListEnabled;
      const textarea = qs(`[data-startup-list-value="${field}"]`);
      if (textarea) textarea.disabled = !toggle.checked;
    }));
  } catch (e) { showError(e); }
}

function sidecarStartupValues() {
  const list = (field) => {
    const present = Boolean(qs(`[data-startup-list-enabled="${field}"]`)?.checked);
    const raw = qs(`[data-startup-list-value="${field}"]`)?.value || '';
    return {present, values: raw.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)};
  };
  const command = list('command');
  const args = list('arguments');
  return {command_present: command.present, command: command.values, arguments_present: args.present, arguments: args.values};
}

async function confirmResetSidecarStartupOverride() {
  const current = state.sidecarStartupOverride;
  if (!current) return;
  const confirmed = await confirmAction({
    title: current.shared_definition ? 'Reset startup to inherited?' : 'Remove local startup?',
    body: current.shared_definition ? 'Only the local startup block will be removed. The shared sidecar reference and other local patch fields remain unchanged.' : 'The startup block will be removed from this app-only sidecar.',
    subject: current.sidecar_name, confirmLabel: current.shared_definition ? 'Reset startup' : 'Remove startup', confirmClass: 'btn-warning', statusClass: 'bg-warning',
  });
  if (confirmed) await saveSidecarStartupOverride('reset');
}

async function saveSidecarStartupOverride(action) {
  const current = state.sidecarStartupOverride;
  if (state.readOnly || !current) return;
  clearError();
  const startup = sidecarStartupValues();
  if (action === 'set' && !startup.command_present && !startup.arguments_present) return showError('Enable command or arguments, or use Reset to inherited.');
  try {
    await apiPatch(sidecarStartupOverrideURL(current.sidecar_name), {
      expected_hash: current.content_hash, expected_defaults_hash: current.defaults_hash,
      override: {action, startup},
    });
    state.editModalClose?.();
    state.editModalClose = null;
    state.sidecarStartupOverride = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}

function referenceCatalogForList(list) {
  if (list?.dataset.referenceOwner === 'defaults-sidecar') {
    if (list.dataset.referenceField === 'profile_ref_names') return state.defaultsSidecarReferences?.container_profiles || [];
    return state.defaultsSidecarReferences?.runtime_asset_definitions || [];
  }
  const catalog = state.appReferences?.catalog || {};
  if (list?.dataset.referenceField === 'sidecar_ref_names') return catalog.sidecar_definitions || [];
  if (list?.dataset.referenceField === 'profile_ref_names') return catalog.container_profiles || [];
  return catalog.runtime_asset_definitions || [];
}

function syncReferenceList(list) {
  if (!list) return;
  const rows = list.querySelector('[data-reference-rows]');
  if (!rows) return;
  rows.querySelector('[data-reference-empty]')?.remove();
  if (!rows.querySelector('[data-reference-row]')) rows.innerHTML = '<div class="text-muted small" data-reference-empty>No references selected.</div>';
  const add = list.querySelector('[data-add-reference]');
  if (add) add.disabled = referenceValues(list).length >= referenceCatalogForList(list).length;
}

function addReferenceRow(list) {
  if (!list) return;
  const catalog = referenceCatalogForList(list);
  const selected = new Set(Array.from(list.querySelectorAll('[data-reference-value]')).map((input) => input.value));
  const value = catalog.find((item) => !selected.has(item));
  if (!value) return showError('All available definitions are already selected.');
  const rows = list.querySelector('[data-reference-rows]');
  rows?.querySelector('[data-reference-empty]')?.remove();
  rows?.insertAdjacentHTML('beforeend', renderReferenceRow(value, catalog));
  syncReferenceList(list);
}

function removeReferenceRow(row) {
  const list = row?.closest('[data-reference-list]');
  row?.remove();
  syncReferenceList(list);
}

function moveReferenceRow(row, direction) {
  if (!row) return;
  if (direction === 'up' && row.previousElementSibling?.matches('[data-reference-row]')) row.parentElement.insertBefore(row, row.previousElementSibling);
  if (direction === 'down' && row.nextElementSibling?.matches('[data-reference-row]')) row.parentElement.insertBefore(row.nextElementSibling, row);
}

function referenceValues(list) {
  return Array.from(list?.querySelectorAll('[data-reference-value]') || []).map((input) => input.value);
}

async function saveAppReferences() {
  if (state.readOnly || !state.appReferences || !state.appFile) return;
  const lists = qsa('[data-references-editor] [data-reference-list]');
  const find = (scope, index, field) => lists.find((list) => list.dataset.referenceScope === scope && Number(list.dataset.referenceIndex) === index && list.dataset.referenceField === field);
  const scoped = (scope, items) => (items || []).map((item) => ({
    index: item.index,
    name: item.name,
    profile_ref_names: referenceValues(find(scope, item.index, 'profile_ref_names')),
    runtime_asset_ref_names: referenceValues(find(scope, item.index, 'runtime_asset_ref_names')),
  }));
  const references = {
    sidecar_ref_names: referenceValues(find('app', -1, 'sidecar_ref_names')),
    runtime_asset_ref_names: referenceValues(find('app', -1, 'runtime_asset_ref_names')),
    containers: scoped('containers', state.appReferences.containers),
    sidecars: scoped('sidecars', state.appReferences.sidecars),
  };
  for (const list of lists) {
    const values = referenceValues(list);
    if (new Set(values).size !== values.length) return showError('A reference list contains duplicate definitions.');
  }
  const selectedSidecars = new Set(references.sidecar_ref_names);
  const localSidecars = new Set((state.appReferences.sidecars || []).map((item) => item.name));
  const removedPatches = (state.appReferences.sidecar_ref_names || []).filter((name) => !selectedSidecars.has(name) && localSidecars.has(name));
  if (removedPatches.length) {
    const confirmed = await confirmAction({
      title: 'Remove sidecar reference and local patch?',
      body: 'These sidecars have app-local patch data. Saving will remove both the shared reference and the complete local patch.',
      subject: removedPatches.join(', '), confirmLabel: 'Remove both', confirmClass: 'btn-danger', statusClass: 'bg-danger',
    });
    if (!confirmed) return;
  }
  references.remove_sidecar_patches = removedPatches;
  clearError();
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/references`, {
      expected_hash: state.appReferences.content_hash,
      expected_defaults_hash: state.appReferences.defaults_hash,
      references,
    });
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function handleEditModalInput(e) {
  const probeMode = e.target.closest('[data-probe-mode]');
  if (probeMode) {
    const key = probeMode.dataset.probeMode;
    qs(`[data-probe-http-fields="${key}"]`)?.classList.toggle('d-none', probeMode.value !== 'http');
    qs(`[data-probe-exec-fields="${key}"]`)?.classList.toggle('d-none', probeMode.value !== 'exec');
  }
  const sidecarEnvKind = e.target.closest('[data-defaults-sidecar-env-kind]');
  if (sidecarEnvKind) syncDefaultsSidecarEnvRow(sidecarEnvKind.closest('[data-defaults-sidecar-env-row]'));
  const containerEnvKind = e.target.closest('[data-container-env-kind]');
  if (containerEnvKind) syncContainerEnvRow(containerEnvKind.closest('[data-container-env-row]'));
  const filter = e.target.closest('[data-special-entry-filter]');
  if (filter) filterSpecialEntryRows(filter.value || '');
  const compactValue = e.target.closest('.special-entry-value-input');
  if (compactValue) {
    const row = compactValue.closest('[data-special-entry-row]');
    const value = row?.querySelector('.special-entry-value');
    if (value) value.value = compactValue.value;
  }
}
function handleAssetStructuredInput(e) {
  const filter = e.target.closest('[data-special-preview-filter]');
  if (filter) filterSpecialPreviewRows(filter.value || '');
}
function handlePortsModalClick(e) {
  const savePorts = e.target.closest('[data-save-ports]');
  if (savePorts && !state.readOnly) return saveContainerPorts(Number(savePorts.dataset.savePorts));
  const addPort = e.target.closest('[data-add-port]');
  if (addPort && !state.readOnly) return addPortRow(Number(addPort.dataset.addPort));
  const addExpose = e.target.closest('[data-add-expose]');
  if (addExpose && !state.readOnly) return addExposeRow(Number(addExpose.dataset.addExpose), addExpose);
  const addExternal = e.target.closest('[data-add-external]');
  if (addExternal && !state.readOnly) return addExternalRow(Number(addExternal.dataset.addExternal), addExternal);
  const removePortItem = e.target.closest('[data-remove-port-item]');
  if (removePortItem && !state.readOnly) {
    const index = Number(removePortItem.dataset.removePortItem);
    removePortItem.closest('[data-port-row]')?.remove();
    syncPortsEmptyState(index);
  }
  const removeExposeItem = e.target.closest('[data-remove-expose-item]');
  if (removeExposeItem && !state.readOnly) {
    const index = Number(removeExposeItem.dataset.removeExposeItem);
    removeExposeItem.closest('[data-expose-row]')?.remove();
    syncExposeEmptyState(index);
  }
  const removeExternalItem = e.target.closest('[data-remove-external-item]');
  if (removeExternalItem && !state.readOnly) {
    const index = Number(removeExternalItem.dataset.removeExternalItem);
    removeExternalItem.closest('[data-external-row]')?.remove();
    syncExternalEmptyState(index);
  }
}
function openEditorModal(title, subtitle, body, options = {}) {
  const modal = qs('#edit-modal');
  if (!modal || !body) return;
  state.editModalClose = openContentModal({
    modal,
    titleSelector: '#edit-modal-title',
    subtitleSelector: '#edit-modal-subtitle',
    bodySelector: '#edit-modal-body',
    title,
    subtitle,
    body,
    wide: !!options.wide,
    onClosed: () => { state.editModalClose = null; },
  });
}
async function loadDefaultsEditorState() {
  if (!state.env) throw new Error('Select an environment first.');
  const defaults = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/defaults`);
  state.defaults = defaults;
  state.defaultsContentHash = defaults.content_hash || null;
  return defaults;
}

async function openDefaultsVarsEditor() {
  if (state.readOnly) return;
  clearError();
  try {
    const defaults = await loadDefaultsEditorState();
    openEditorModal('Edit defaults vars', 'Top-level vars in apps/_defaults.yml used by {{var:NAME}} placeholders.', renderDefaultsVarsEditor(defaults.vars || []));
  } catch (e) { showError(e); }
}

async function openDefaultsContainerProfileEditor(name) {
  return profileEditor.open(name);
}

async function openDefaultsSidecarDefinitionEditor(name) {
  if (!state.env || state.readOnly || !name) return;
  clearError();
  try {
    const definition = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/defaults/sidecar-definitions/${encodeURIComponent(name)}`);
    state.defaultsSidecarDefinition = definition;
    const usages = (state.inspection?.usage || []).filter((usage) => usage.kind === 'sidecar_definition' && usage.name === name);
    const usedBy = usages.length
      ? usages.map((usage) => `<span class="badge bg-blue-lt">${esc(usage.app)}${usage.container ? ` / ${esc(usage.container)}` : ''}</span>`).join('')
      : '<span class="text-muted small">This definition is currently unused.</span>';
    openEditorModal(
      `Edit sidecar image: ${name}`,
      'Updates only the image scalar in apps/_defaults.yml. All other definition fields stay unchanged.',
      `<div class="overview-section">
        <div class="overview-subtitle mb-2">Used by</div>
        <div class="d-flex flex-wrap gap-1 mb-3">${usedBy}</div>
        <label class="form-label" for="defaults-sidecar-image">Image</label>
        <input class="form-control font-monospace" id="defaults-sidecar-image" value="${esc(definition.image || '')}" autocomplete="off" required>
        <div class="form-hint">Templates such as {{env:REGISTRY_URL}} are preserved as text.</div>
        <div class="d-flex justify-content-end mt-3"><button class="btn btn-primary" type="button" data-save-defaults-sidecar-definition><i class="ti ti-device-floppy me-1"></i>Save image</button></div>
      </div>`,
    );
    qs('#defaults-sidecar-image')?.focus();
  } catch (e) { showError(e); }
}

async function saveDefaultsSidecarDefinition() {
  const current = state.defaultsSidecarDefinition;
  if (!state.env || state.readOnly || !current) return;
  const input = qs('#defaults-sidecar-image');
  const image = input?.value?.trim() || '';
  if (!image) {
    input?.classList.add('is-invalid');
    return showError('Sidecar definition image is required.');
  }
  input?.classList.remove('is-invalid');
  clearError();
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/defaults/sidecar-definitions/${encodeURIComponent(current.name)}`, {
      expected_hash: current.content_hash,
      definition: {image},
    });
    state.editModalClose?.();
    state.editModalClose = null;
    state.defaultsSidecarDefinition = null;
    await refreshRepositorySnapshot();
    await loadInspection(true);
  } catch (e) { showError(e); }
}

function defaultsSidecarStartupURL(name) {
  return `/api/v1/envs/${encodeURIComponent(state.env)}/defaults/sidecar-definitions/${encodeURIComponent(name)}/startup`;
}

function renderDefaultsSidecarStartupEditor(current) {
  const startup = current.startup || {};
  const row = (field, label, present, values) => `<div class="mb-3">
    <label class="form-check form-switch mb-2"><input class="form-check-input" type="checkbox" data-defaults-startup-enabled="${field}" ${present ? 'checked' : ''}><span class="form-check-label">Define ${esc(label.toLowerCase())}</span></label>
    <textarea class="form-control font-monospace" rows="3" data-defaults-startup-value="${field}" placeholder="One item per line" ${present ? '' : 'disabled'}>${esc(startupLines(values))}</textarea>
  </div>`;
  const remove = current.startup ? '<button class="btn btn-outline-danger" type="button" data-remove-defaults-sidecar-startup><i class="ti ti-trash me-1"></i>Remove startup</button>' : '';
  return `<div data-defaults-sidecar-startup-editor>
    <div class="alert alert-info py-2"><i class="ti ti-info-circle me-2"></i>Command and arguments are whole lists. An enabled empty list is saved explicitly as [].</div>
    ${row('command', 'Command', startup.command_present, startup.command)}
    ${row('arguments', 'Arguments', startup.arguments_present, startup.arguments)}
    <div class="d-flex align-items-center justify-content-between gap-2">${remove}<button class="btn btn-primary ms-auto" type="button" data-save-defaults-sidecar-startup><i class="ti ti-device-floppy me-1"></i>Save startup</button></div>
  </div>`;
}

async function openDefaultsSidecarStartupEditor(name) {
  if (!state.env || state.readOnly || !name) return;
  clearError();
  try {
    const current = await api(defaultsSidecarStartupURL(name));
    state.defaultsSidecarStartup = current;
    openEditorModal(
      `Edit sidecar startup: ${name}`,
      'Updates only startup.command and startup.arguments in apps/_defaults.yml.',
      renderDefaultsSidecarStartupEditor(current),
    );
    qsa('[data-defaults-startup-enabled]').forEach((toggle) => toggle.addEventListener('change', () => {
      const textarea = qs(`[data-defaults-startup-value="${toggle.dataset.defaultsStartupEnabled}"]`);
      if (textarea) textarea.disabled = !toggle.checked;
    }));
  } catch (e) { showError(e); }
}

function defaultsSidecarStartupValues() {
  const list = (field) => {
    const present = Boolean(qs(`[data-defaults-startup-enabled="${field}"]`)?.checked);
    const raw = qs(`[data-defaults-startup-value="${field}"]`)?.value || '';
    return {present, values: raw.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)};
  };
  const command = list('command');
  const args = list('arguments');
  return {command_present: command.present, command: command.values, arguments_present: args.present, arguments: args.values};
}

async function confirmRemoveDefaultsSidecarStartup() {
  const current = state.defaultsSidecarStartup;
  if (!current?.startup) return;
  const confirmed = await confirmAction({
    title: 'Remove shared sidecar startup?',
    body: 'The complete startup block will be removed from this shared definition. Applications using it may change behavior.',
    subject: current.name, confirmLabel: 'Remove startup', confirmClass: 'btn-danger', statusClass: 'bg-danger',
  });
  if (confirmed) await saveDefaultsSidecarStartup('remove');
}

async function saveDefaultsSidecarStartup(action) {
  const current = state.defaultsSidecarStartup;
  if (!state.env || state.readOnly || !current) return;
  const startup = defaultsSidecarStartupValues();
  if (action === 'set' && !startup.command_present && !startup.arguments_present) return showError('Define command or arguments, or remove the startup block.');
  clearError();
  try {
    await apiPatch(defaultsSidecarStartupURL(current.name), {
      expected_hash: current.content_hash,
      startup: {action, startup},
    });
    state.editModalClose?.();
    state.editModalClose = null;
    state.defaultsSidecarStartup = null;
    await refreshRepositorySnapshot();
    await loadInspection(true);
  } catch (e) { showError(e); }
}

function defaultsSidecarResourcesURL(name) {
  return `/api/v1/envs/${encodeURIComponent(state.env)}/defaults/sidecar-definitions/${encodeURIComponent(name)}/resources`;
}

function renderDefaultsSidecarResourcesEditor(current) {
  const resources = current.resources || {};
  const input = (field, label) => `<div class="col-12 col-md-6"><label class="form-label small mb-1" for="defaults-sidecar-resource-${field}">${esc(label)}</label><input class="form-control font-monospace" id="defaults-sidecar-resource-${field}" data-defaults-sidecar-resource-field="${field}" value="${esc(resources[field] || '')}" autocomplete="off"></div>`;
  const remove = current.resources ? '<button class="btn btn-outline-danger" type="button" data-remove-defaults-sidecar-resources><i class="ti ti-trash me-1"></i>Remove resources</button>' : '';
  return `<div data-defaults-sidecar-resources-editor>
    <div class="alert alert-warning py-2"><i class="ti ti-alert-triangle me-2"></i>This shared definition may affect every application that references it.</div>
    <div class="row g-2">
      ${input('cpu_request', 'CPU request')}${input('cpu_limit', 'CPU limit')}
      ${input('memory_request', 'Memory request')}${input('memory_limit', 'Memory limit')}
      ${input('ephemeral_storage_request', 'Ephemeral storage request')}${input('ephemeral_storage_limit', 'Ephemeral storage limit')}
    </div>
    <div class="form-hint mt-2">Use a Kubernetes quantity or a complete template such as {{var:CPU_REQUEST}}. Existing from/to or requests/limits spelling is preserved.</div>
    <div class="d-flex align-items-center justify-content-between gap-2 mt-3">${remove}<button class="btn btn-primary ms-auto" type="button" data-save-defaults-sidecar-resources><i class="ti ti-device-floppy me-1"></i>Save resources</button></div>
  </div>`;
}

async function openDefaultsSidecarResourcesEditor(name) {
  if (!state.env || state.readOnly || !name) return;
  clearError();
  try {
    const current = await api(defaultsSidecarResourcesURL(name));
    state.defaultsSidecarResources = current;
    openEditorModal(
      `Edit sidecar resources: ${name}`,
      'Updates CPU, memory and ephemeral-storage resources in apps/_defaults.yml.',
      renderDefaultsSidecarResourcesEditor(current),
    );
    qs('[data-defaults-sidecar-resource-field]')?.focus();
  } catch (e) { showError(e); }
}

function defaultsSidecarResourceValues() {
  const value = (field) => qs(`[data-defaults-sidecar-resource-field="${field}"]`)?.value?.trim() || '';
  return {
    cpu_request: value('cpu_request'), cpu_limit: value('cpu_limit'),
    memory_request: value('memory_request'), memory_limit: value('memory_limit'),
    ephemeral_storage_request: value('ephemeral_storage_request'), ephemeral_storage_limit: value('ephemeral_storage_limit'),
  };
}

const completeSourceTemplateRe = /^\{\{[^{}\r\n]+\}\}$/;
function validateDefaultsSidecarResourcesForm() {
  clearInvalidInputs('[data-defaults-sidecar-resources-editor]');
  const checks = [
    ['cpu_request', 'CPU request', cpuQuantityRe, 'Use values like 100m, 0.5 or 1.'],
    ['cpu_limit', 'CPU limit', cpuQuantityRe, 'Use values like 100m, 0.5 or 1.'],
    ['memory_request', 'Memory request', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['memory_limit', 'Memory limit', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['ephemeral_storage_request', 'Ephemeral storage request', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
    ['ephemeral_storage_limit', 'Ephemeral storage limit', memoryQuantityRe, 'Use values like 256Mi, 1Gi or 512M.'],
  ];
  let populated = false;
  for (const [field, label, pattern, hint] of checks) {
    const input = qs(`[data-defaults-sidecar-resource-field="${field}"]`);
    const value = input?.value?.trim() || '';
    populated ||= value !== '';
    if (value && !pattern.test(value) && !completeSourceTemplateRe.test(value)) return invalidInput(input, `${label} is invalid. ${hint} A complete {{env:NAME}} or {{var:NAME}} template is also allowed.`);
  }
  if (!populated) return {ok: false, message: 'Enter at least one resource value, or remove the resources block.'};
  return {ok: true};
}

async function confirmRemoveDefaultsSidecarResources() {
  const current = state.defaultsSidecarResources;
  if (!current?.resources) return;
  const confirmed = await confirmAction({
    title: 'Remove shared sidecar resources?',
    body: 'The complete resources block will be removed from this shared definition. Applications using it may change behavior.',
    subject: current.name, confirmLabel: 'Remove resources', confirmClass: 'btn-danger', statusClass: 'bg-danger',
  });
  if (confirmed) await saveDefaultsSidecarResources('remove');
}

async function saveDefaultsSidecarResources(action) {
  const current = state.defaultsSidecarResources;
  if (!state.env || state.readOnly || !current) return;
  const resources = defaultsSidecarResourceValues();
  if (action === 'set') {
    const validation = validateDefaultsSidecarResourcesForm();
    if (!validation.ok) return showError(validation.message);
  }
  clearError();
  try {
    await apiPatch(defaultsSidecarResourcesURL(current.name), {
      expected_hash: current.content_hash,
      resources: {action, resources},
    });
    state.editModalClose?.();
    state.editModalClose = null;
    state.defaultsSidecarResources = null;
    await refreshRepositorySnapshot();
    await loadInspection(true);
  } catch (e) { showError(e); }
}

function defaultsSidecarEnvsURL(name) {
  return `/api/v1/envs/${encodeURIComponent(state.env)}/defaults/sidecar-definitions/${encodeURIComponent(name)}/envs`;
}

const defaultsSidecarEnvKinds = [
  ['value', 'Value'], ['secret', 'Secret key'], ['resource', 'Container resource'], ['field', 'Pod field'],
  ['workload_identity_token', 'Workload identity token'], ['shared_asset', 'Shared asset'], ['remove', 'Remove inherited'],
];

function renderDefaultsSidecarEnvConfig(item = {}) {
  const field = (kind, name, placeholder, value) => {
    const list = kind === 'workload_identity_token' ? ' list="defaults-sidecar-token-names"' : kind === 'shared_asset' ? ' list="defaults-sidecar-asset-names"' : '';
    return `<div data-defaults-sidecar-env-config="${kind}" class="${item.kind === kind ? '' : 'd-none'}"><input class="form-control form-control-sm font-monospace" data-defaults-sidecar-env-field="${name}" value="${esc(value || '')}" placeholder="${esc(placeholder)}" autocomplete="off"${list}></div>`;
  };
  const pair = (kind, fields) => `<div data-defaults-sidecar-env-config="${kind}" class="${item.kind === kind ? '' : 'd-none'}"><div class="row g-1">${fields.map(([name, placeholder, value]) => `<div class="col"><input class="form-control form-control-sm font-monospace" data-defaults-sidecar-env-field="${name}" value="${esc(value || '')}" placeholder="${esc(placeholder)}" autocomplete="off"></div>`).join('')}</div></div>`;
  return `${field('value', 'value', 'Value', item.value)}
    ${pair('secret', [['secret_name', 'Secret name', item.secret_name], ['key', 'Key', item.key]])}
    ${pair('resource', [['resource_name', 'requests.cpu', item.resource_name], ['divisor', 'Divisor (optional)', item.divisor]])}
    ${field('field', 'field_path', 'metadata.name', item.field_path)}
    ${field('workload_identity_token', 'workload_identity_token_ref_name', 'Token definition name', item.workload_identity_token_ref_name)}
    ${field('shared_asset', 'shared_asset_ref_name', 'Shared asset name', item.shared_asset_ref_name)}
    <div data-defaults-sidecar-env-config="remove" class="${item.kind === 'remove' ? '' : 'd-none'}"><span class="text-muted small">Removes an inherited variable with this name.</span></div>`;
}

function renderDefaultsSidecarEnvRow(item = {kind: 'value'}) {
  const kind = item.kind || 'value';
  const options = defaultsSidecarEnvKinds.map(([value, label]) => `<option value="${value}" ${value === kind ? 'selected' : ''}>${esc(label)}</option>`).join('');
  return `<tr data-defaults-sidecar-env-row>
    <td><input class="form-control form-control-sm font-monospace" data-defaults-sidecar-env-name value="${esc(item.name || '')}" placeholder="VARIABLE_NAME" autocomplete="off"></td>
    <td><select class="form-select form-select-sm" data-defaults-sidecar-env-kind>${options}</select></td>
    <td>${renderDefaultsSidecarEnvConfig({...item, kind})}</td>
    <td class="text-nowrap text-end">
      <button class="btn btn-sm btn-ghost-secondary btn-icon" type="button" data-move-defaults-sidecar-env="up" title="Move up"><i class="ti ti-arrow-up"></i></button>
      <button class="btn btn-sm btn-ghost-secondary btn-icon" type="button" data-move-defaults-sidecar-env="down" title="Move down"><i class="ti ti-arrow-down"></i></button>
      <button class="btn btn-sm btn-ghost-danger btn-icon" type="button" data-remove-defaults-sidecar-env title="Remove variable"><i class="ti ti-trash"></i></button>
    </td>
  </tr>`;
}

function renderDefaultsSidecarEnvsEditor(current) {
  const rows = (current.envs || []).map((item) => renderDefaultsSidecarEnvRow(item)).join('');
  const remove = current.envs_present ? '<button class="btn btn-outline-danger" type="button" data-remove-defaults-sidecar-envs><i class="ti ti-trash me-1"></i>Remove envs block</button>' : '';
  return `<div data-defaults-sidecar-envs-editor>
    <div class="alert alert-warning py-2"><i class="ti ti-alert-triangle me-2"></i>This shared definition may affect every application that references it. Environment variable order is preserved.</div>
    <div class="d-flex justify-content-end mb-2"><button class="btn btn-sm btn-outline-primary" type="button" data-add-defaults-sidecar-env><i class="ti ti-plus me-1"></i>Add variable</button></div>
    <div class="table-responsive"><table class="table table-sm table-vcenter"><thead><tr><th style="width: 24%">Name</th><th style="width: 22%">Type</th><th>Configuration</th><th class="text-end">Actions</th></tr></thead><tbody data-defaults-sidecar-env-rows>${rows}</tbody></table></div>
    <datalist id="defaults-sidecar-token-names">${(current.workload_identity_token_ref_names || []).map((name) => `<option value="${esc(name)}"></option>`).join('')}</datalist>
    <datalist id="defaults-sidecar-asset-names">${(current.shared_asset_ref_names || []).map((name) => `<option value="${esc(name)}"></option>`).join('')}</datalist>
    <div class="form-hint">References are stored by name and validated against the local defaults/shared-assets catalogs.</div>
    <div class="d-flex align-items-center justify-content-between gap-2 mt-3">${remove}<button class="btn btn-primary ms-auto" type="button" data-save-defaults-sidecar-envs><i class="ti ti-device-floppy me-1"></i>Save envs</button></div>
  </div>`;
}

function syncDefaultsSidecarEnvRow(row) {
  if (!row) return;
  const kind = row.querySelector('[data-defaults-sidecar-env-kind]')?.value || 'value';
  row.querySelectorAll('[data-defaults-sidecar-env-config]').forEach((config) => config.classList.toggle('d-none', config.dataset.defaultsSidecarEnvConfig !== kind));
}

function addDefaultsSidecarEnvRow() {
  const rows = qs('[data-defaults-sidecar-env-rows]');
  if (!rows) return;
  rows.insertAdjacentHTML('beforeend', renderDefaultsSidecarEnvRow());
  rows.lastElementChild?.querySelector('[data-defaults-sidecar-env-name]')?.focus();
}

function moveDefaultsSidecarEnvRow(row, direction) {
  if (!row) return;
  if (direction === 'up' && row.previousElementSibling) row.parentElement.insertBefore(row, row.previousElementSibling);
  if (direction === 'down' && row.nextElementSibling) row.parentElement.insertBefore(row.nextElementSibling, row);
}

async function openDefaultsSidecarEnvsEditor(name) {
  if (!state.env || state.readOnly || !name) return;
  clearError();
  try {
    const current = await api(defaultsSidecarEnvsURL(name));
    state.defaultsSidecarEnvs = current;
    openEditorModal(
      `Edit sidecar envs: ${name}`,
      'Updates the ordered envs list in apps/_defaults.yml.',
      renderDefaultsSidecarEnvsEditor(current),
      {wide: true},
    );
    qsa('[data-defaults-sidecar-env-row]').forEach(syncDefaultsSidecarEnvRow);
  } catch (e) { showError(e); }
}

function defaultsSidecarEnvValues() {
  const field = (row, name) => row.querySelector(`[data-defaults-sidecar-env-field="${name}"]`)?.value || '';
  return qsa('[data-defaults-sidecar-env-row]').map((row) => {
    const item = {name: row.querySelector('[data-defaults-sidecar-env-name]')?.value?.trim() || '', kind: row.querySelector('[data-defaults-sidecar-env-kind]')?.value || 'value'};
    if (item.kind === 'value') item.value = field(row, 'value');
    if (item.kind === 'secret') Object.assign(item, {secret_name: field(row, 'secret_name').trim(), key: field(row, 'key').trim()});
    if (item.kind === 'resource') Object.assign(item, {resource_name: field(row, 'resource_name').trim(), divisor: field(row, 'divisor').trim()});
    if (item.kind === 'field') item.field_path = field(row, 'field_path').trim();
    if (item.kind === 'workload_identity_token') item.workload_identity_token_ref_name = field(row, 'workload_identity_token_ref_name').trim();
    if (item.kind === 'shared_asset') item.shared_asset_ref_name = field(row, 'shared_asset_ref_name').trim();
    return item;
  });
}

function validateDefaultsSidecarEnvsForm(items) {
  clearInvalidInputs('[data-defaults-sidecar-envs-editor]');
  const rows = qsa('[data-defaults-sidecar-env-row]');
  const seen = new Set();
  for (let index = 0; index < items.length; index++) {
    const item = items[index];
    const row = rows[index];
    const nameInput = row?.querySelector('[data-defaults-sidecar-env-name]');
    if (!variableNameRe.test(item.name)) return invalidInput(nameInput, `Variable ${index + 1} needs a valid name.`);
    if (seen.has(item.name)) return invalidInput(nameInput, `Variable name ${item.name} is duplicated.`);
    seen.add(item.name);
    const required = (field, message) => {
      const input = row?.querySelector(`[data-defaults-sidecar-env-field="${field}"]`);
      return input?.value?.trim() ? null : invalidInput(input, message);
    };
    let invalid = null;
    if (item.kind === 'secret') invalid = required('secret_name', 'Secret name is required.') || required('key', 'Secret key is required.');
    if (item.kind === 'resource') invalid = required('resource_name', 'Resource name is required.');
    if (item.kind === 'field') invalid = required('field_path', 'Field path is required.');
    if (item.kind === 'workload_identity_token') invalid = required('workload_identity_token_ref_name', 'Token definition name is required.');
    if (item.kind === 'shared_asset') invalid = required('shared_asset_ref_name', 'Shared asset name is required.');
    if (invalid) return invalid;
    if (item.kind === 'workload_identity_token' && !(state.defaultsSidecarEnvs?.workload_identity_token_ref_names || []).includes(item.workload_identity_token_ref_name)) {
      return invalidInput(row?.querySelector('[data-defaults-sidecar-env-field="workload_identity_token_ref_name"]'), `Unknown token definition ${item.workload_identity_token_ref_name}.`);
    }
    if (item.kind === 'shared_asset' && !(state.defaultsSidecarEnvs?.shared_asset_ref_names || []).includes(item.shared_asset_ref_name)) {
      return invalidInput(row?.querySelector('[data-defaults-sidecar-env-field="shared_asset_ref_name"]'), `Unknown shared asset ${item.shared_asset_ref_name}.`);
    }
  }
  return {ok: true};
}

async function confirmRemoveDefaultsSidecarEnvs() {
  const current = state.defaultsSidecarEnvs;
  if (!current?.envs_present) return;
  const confirmed = await confirmAction({
    title: 'Remove shared sidecar envs?',
    body: 'The complete envs block will be removed from this shared definition. Applications using it may change behavior.',
    subject: current.name, confirmLabel: 'Remove envs', confirmClass: 'btn-danger', statusClass: 'bg-danger',
  });
  if (confirmed) await saveDefaultsSidecarEnvs('remove');
}

async function saveDefaultsSidecarEnvs(action) {
  const current = state.defaultsSidecarEnvs;
  if (!state.env || state.readOnly || !current) return;
  const envs = defaultsSidecarEnvValues();
  if (action === 'set') {
    const validation = validateDefaultsSidecarEnvsForm(envs);
    if (!validation.ok) return showError(validation.message);
  }
  clearError();
  try {
    await apiPatch(defaultsSidecarEnvsURL(current.name), {expected_hash: current.content_hash, envs: {action, envs}});
    state.editModalClose?.();
    state.editModalClose = null;
    state.defaultsSidecarEnvs = null;
    await refreshRepositorySnapshot();
    await loadInspection(true);
  } catch (e) { showError(e); }
}

function defaultsSidecarReferencesURL(name) {
  return `/api/v1/envs/${encodeURIComponent(state.env)}/defaults/sidecar-definitions/${encodeURIComponent(name)}/references`;
}

function renderDefaultsSidecarReferencesEditor(current) {
  return `<div data-defaults-sidecar-references-editor>
    <div class="alert alert-warning py-2"><i class="ti ti-alert-triangle me-2"></i>Profile order is significant. Later profiles override earlier profiles for this shared sidecar.</div>
    ${renderReferenceList('Container profiles', current.profile_ref_names, current.container_profiles, 'defaults-sidecar', -1, 'profile_ref_names', 'defaults-sidecar')}
    ${renderReferenceList('Runtime assets', current.runtime_asset_ref_names, current.runtime_asset_definitions, 'defaults-sidecar', -1, 'runtime_asset_ref_names', 'defaults-sidecar')}
    <div class="text-end"><button class="btn btn-primary" type="button" data-save-defaults-sidecar-references><i class="ti ti-device-floppy me-1"></i>Save references</button></div>
  </div>`;
}

async function openDefaultsSidecarReferencesEditor(name) {
  if (!state.env || state.readOnly || !name) return;
  clearError();
  try {
    const current = await api(defaultsSidecarReferencesURL(name));
    state.defaultsSidecarReferences = current;
    openEditorModal(
      `Edit sidecar references: ${name}`,
      'Updates profile_ref_names and runtime_asset_ref_names in apps/_defaults.yml.',
      renderDefaultsSidecarReferencesEditor(current),
    );
  } catch (e) { showError(e); }
}

async function saveDefaultsSidecarReferences() {
  const current = state.defaultsSidecarReferences;
  if (!state.env || state.readOnly || !current) return;
  const find = (field) => qs(`[data-defaults-sidecar-references-editor] [data-reference-field="${field}"]`);
  const references = {
    profile_ref_names: referenceValues(find('profile_ref_names')),
    runtime_asset_ref_names: referenceValues(find('runtime_asset_ref_names')),
  };
  clearError();
  try {
    await apiPatch(defaultsSidecarReferencesURL(current.name), {expected_hash: current.content_hash, references});
    state.editModalClose?.();
    state.editModalClose = null;
    state.defaultsSidecarReferences = null;
    await refreshRepositorySnapshot();
    await loadInspection(true);
  } catch (e) { showError(e); }
}

function renderDefaultsVarsEditor(items) {
  const rows = (items || []).map((item) => renderDefaultsVarRow(item)).join('');
  return `
    <div class="detail-panel">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Defaults vars</div>
        <button class="btn btn-sm btn-outline-primary" type="button" data-add-defaults-var><i class="ti ti-plus me-1"></i>Add variable</button>
      </div>
      <div class="table-responsive"><table class="table table-sm" data-defaults-vars><tbody>${rows || renderDefaultsVarsEmpty()}</tbody></table></div>
      <div class="text-end mt-3"><button class="btn btn-primary" type="button" data-save-defaults-vars><i class="ti ti-device-floppy me-1"></i>Save vars</button></div>
    </div>`;
}
function renderDefaultsVarRow(item = {}) {
  return `
    <tr>
      <td><input class="form-control form-control-sm font-monospace defaults-var-name" placeholder="NAME" value="${esc(item.name || '')}"></td>
      <td><input class="form-control form-control-sm font-monospace defaults-var-value" placeholder="value" value="${esc(item.value || '')}"></td>
      <td class="table-action-col"><button class="btn btn-sm btn-outline-danger btn-icon" type="button" data-remove-defaults-var title="Remove variable"><i class="ti ti-trash"></i></button></td>
    </tr>`;
}
function renderDefaultsVarsEmpty() {
  return `<tr><td colspan="3">${emptyState('ti-variable', 'No defaults vars', 'Use Add variable to create the first one.')}</td></tr>`;
}
function syncDefaultsVarsEmptyState() {
  const body = qs('[data-defaults-vars] tbody');
  if (!body) return;
  if (!body.querySelector('.defaults-var-name')) body.innerHTML = renderDefaultsVarsEmpty();
}
function addDefaultsVarRow() {
  const body = qs('[data-defaults-vars] tbody');
  if (!body) return;
  if (!body.querySelector('.defaults-var-name')) body.innerHTML = '';
  body.insertAdjacentHTML('beforeend', renderDefaultsVarRow());
  body.querySelector('tr:last-child .defaults-var-name')?.focus();
}
async function saveDefaultsVars() {
  if (!state.env || state.readOnly) return;
  clearError();
  const body = qs('[data-defaults-vars] tbody');
  const validation = validateNamedRows(body, '.defaults-var-name', 'Variable name');
  if (!validation.ok) return showError(validation.message);
  const items = Array.from(body?.querySelectorAll('tr') || []).map((row) => ({
    name: row.querySelector('.defaults-var-name')?.value?.trim() || '',
    value: row.querySelector('.defaults-var-value')?.value || '',
  })).filter((item) => item.name !== '');
  try {
    const updated = await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/defaults/vars`, {items, expected_hash: state.defaultsContentHash});
    state.defaults = updated;
    state.defaultsContentHash = updated.content_hash || null;
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadAssets();
    await loadInspection(true);
    if (state.active === 'assets' && isDefaultsAsset(state.assetPath)) await selectAsset(state.assetPath);
  } catch (e) { showError(e); }
}
async function openDefaultsContainerEnvsEditor() {
  if (state.readOnly) return;
  clearError();
  try {
    const defaults = await loadDefaultsEditorState();
    openEditorModal('Edit defaults container envs', 'Default container envs in apps/_defaults.yml grouped by container reference name or "*".', renderDefaultsContainerEnvsEditor(defaults.container_envs || []));
  } catch (e) { showError(e); }
}
function renderDefaultsContainerEnvsEditor(groups) {
  const rows = (groups || []).map((group) => renderDefaultsEnvGroup(group)).join('');
  return `
    <div class="detail-panel">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Defaults container envs</div>
        <button class="btn btn-sm btn-outline-primary" type="button" data-add-defaults-env-group><i class="ti ti-plus me-1"></i>Add group</button>
      </div>
      <div data-defaults-env-groups>${rows || emptyState('ti-variable', 'No container env groups', 'Use Add group to create the first one.')}</div>
      <div class="text-end mt-3"><button class="btn btn-primary" type="button" data-save-defaults-container-envs><i class="ti ti-device-floppy me-1"></i>Save container envs</button></div>
    </div>`;
}
function renderDefaultsEnvGroup(group = {}) {
  const envs = group.envs || [];
  const rows = envs.map((item) => renderDefaultsEnvRow(item)).join('');
  return `
    <div class="resource-editor mb-3" data-defaults-env-group>
      <div class="row g-2 align-items-end mb-2">
        <div class="col-12 col-lg-8"><label class="form-label small mb-1">Container reference</label><input class="form-control form-control-sm font-monospace defaults-env-group-name" placeholder="* or container name" value="${esc(group.container_ref_name || '')}"></div>
        <div class="col-6 col-lg-2"><button class="btn btn-sm btn-outline-primary w-100" type="button" data-add-defaults-env><i class="ti ti-plus me-1"></i>Add env</button></div>
        <div class="col-6 col-lg-2"><button class="btn btn-sm btn-outline-danger w-100" type="button" data-remove-defaults-env-group><i class="ti ti-trash me-1"></i>Remove</button></div>
      </div>
      <div class="table-responsive"><table class="table table-sm mb-0"><tbody>${rows || renderDefaultsEnvEmpty()}</tbody></table></div>
    </div>`;
}
function renderDefaultsEnvRow(item = {}) {
  const editable = item.is_value_editable !== false;
  if (!editable) return `
    <tr data-defaults-env-editable="false">
      <td><span class="font-monospace">${esc(item.name || '?')}</span></td>
      <td><span class="text-muted font-monospace">${esc(envVarReadOnlyValue(item))}</span></td>
      <td class="table-action-col"><span class="badge bg-secondary-lt">${esc(item.kind || 'read-only')}</span></td>
    </tr>`;
  return `
    <tr data-defaults-env-editable="true">
      <td><input class="form-control form-control-sm font-monospace defaults-env-name" placeholder="NAME" value="${esc(item.name || '')}"></td>
      <td><input class="form-control form-control-sm font-monospace defaults-env-value" placeholder="value" value="${esc(item.value || '')}"></td>
      <td class="table-action-col"><button class="btn btn-sm btn-outline-danger btn-icon" type="button" data-remove-defaults-env title="Remove env"><i class="ti ti-trash"></i></button></td>
    </tr>`;
}
function renderDefaultsEnvEmpty() {
  return `<tr><td colspan="3">${emptyState('ti-variable', 'No envs in this group', 'Use Add env to create the first one.')}</td></tr>`;
}
function addDefaultsEnvGroup() {
  const host = qs('[data-defaults-env-groups]');
  if (!host) return;
  if (!host.querySelector('[data-defaults-env-group]')) host.innerHTML = '';
  host.insertAdjacentHTML('beforeend', renderDefaultsEnvGroup({container_ref_name: '*'}));
  host.querySelector('[data-defaults-env-group]:last-child .defaults-env-group-name')?.focus();
}
function addDefaultsEnvRow(groupEl) {
  const body = groupEl?.querySelector('tbody');
  if (!body) return;
  if (!body.querySelector('.defaults-env-name')) body.innerHTML = '';
  body.insertAdjacentHTML('beforeend', renderDefaultsEnvRow());
  body.querySelector('tr:last-child .defaults-env-name')?.focus();
}
async function saveDefaultsContainerEnvs() {
  if (!state.env || state.readOnly) return;
  clearError();
  for (const groupEl of qsa('[data-defaults-env-group]')) {
    const validation = validateNamedRows(groupEl, '.defaults-env-name', 'Environment variable name');
    if (!validation.ok) return showError(validation.message);
  }
  const groups = qsa('[data-defaults-env-group]').map((groupEl) => ({
    container_ref_name: groupEl.querySelector('.defaults-env-group-name')?.value?.trim() || '',
    envs: Array.from(groupEl.querySelectorAll('tbody tr[data-defaults-env-editable="true"]')).map((row) => ({
      name: row.querySelector('.defaults-env-name')?.value?.trim() || '',
      value: row.querySelector('.defaults-env-value')?.value || '',
    })).filter((item) => item.name !== ''),
  })).filter((group) => group.container_ref_name !== '');
  try {
    const updated = await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/defaults/container-envs`, {groups, expected_hash: state.defaultsContentHash});
    state.defaults = updated;
    state.defaultsContentHash = updated.content_hash || null;
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadAssets();
    await loadInspection(true);
    if (state.active === 'assets' && isDefaultsAsset(state.assetPath)) await selectAsset(state.assetPath);
  } catch (e) { showError(e); }
}
function openSpecialEntriesEditor() {
  const secured = state.assetPath === 'env.secured.json';
  const title = `Edit ${state.assetPath || 'special entries'}`;
  const description = secured ? 'Decrypted environment values. Saving re-encrypts env.secured.json through EncJson.' : 'Environment values with explicit JSON value type.';
  openEditorModal(title, description, renderSpecialEntriesEditor(state.specialEntries?.entries || []), {wide: true});
}
function renderSpecialEntriesEditor(entries) {
  const rows = (entries || []).map((entry) => renderSpecialEntryRow(entry)).join('');
  return `
    <div class="detail-panel">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Entries</div>
        <button class="btn btn-sm btn-outline-primary" type="button" data-add-special-entry><i class="ti ti-plus me-1"></i>Add entry</button>
      </div>
      <div class="input-icon mb-3">
        <span class="input-icon-addon"><i class="ti ti-search"></i></span>
        <input class="form-control" data-special-entry-filter placeholder="Filter key, value or type...">
      </div>
      <div class="text-muted small mb-2" data-special-entry-count>${entries?.length || 0} entries</div>
      <div class="special-entry-editor-list">
        <table class="table table-sm special-entry-editor-table" data-special-entries>
          <thead>
            <tr>
              <th class="special-entry-key-col">Variable name</th>
              <th>Value</th>
              <th class="special-entry-type-col">Type</th>
              <th class="special-entry-actions-col">Actions</th>
            </tr>
          </thead>
          <tbody>${rows || renderSpecialEntriesEmpty()}</tbody>
        </table>
      </div>
      <div class="text-end mt-3"><button class="btn btn-primary" type="button" data-save-special-entries><i class="ti ti-device-floppy me-1"></i>Save entries</button></div>
    </div>`;
}
function renderSpecialEntryRow(entry = {}) {
  return `
    <tr data-special-entry-row>
      <td><input class="form-control form-control-sm font-monospace special-entry-key" placeholder="KEY" value="${esc(entry.key || '')}" spellcheck="false"></td>
      <td>
        <div class="input-group input-group-sm special-entry-value-group">
          <input class="form-control font-monospace special-entry-value-input" placeholder="value" value="${esc(compactTextValue(entry.value_text || ''))}" spellcheck="false">
          <button class="btn btn-outline-secondary btn-icon" type="button" data-edit-special-value title="Edit value in large textarea"><i class="ti ti-arrows-maximize"></i></button>
        </div>
        <textarea class="special-entry-value hidden">${esc(entry.value_text || '')}</textarea>
      </td>
      <td><select class="form-select form-select-sm special-entry-type">${['string','number','bool','null','json'].map((type) => `<option value="${type}" ${entry.value_type === type ? 'selected' : ''}>${type}</option>`).join('')}</select></td>
      <td class="special-entry-actions-col">
        <div class="btn-list justify-content-end flex-nowrap">
          <button class="btn btn-sm btn-ghost-secondary special-entry-action-btn" type="button" data-special-entry-action="add-above" title="Add empty entry above">Add ↑</button>
          <button class="btn btn-sm btn-ghost-secondary special-entry-action-btn" type="button" data-special-entry-action="add-below" title="Add empty entry below">Add ↓</button>
          <button class="btn btn-sm btn-ghost-secondary special-entry-action-btn" type="button" data-special-entry-action="duplicate-above" title="Duplicate this entry above">Copy ↑</button>
          <button class="btn btn-sm btn-ghost-secondary special-entry-action-btn" type="button" data-special-entry-action="duplicate-below" title="Duplicate this entry below">Copy ↓</button>
          <button class="btn btn-sm btn-ghost-danger btn-icon special-entry-action-btn" type="button" data-special-entry-action="delete" title="Delete entry"><i class="ti ti-trash"></i></button>
        </div>
      </td>
    </tr>`;
}
function renderSpecialEntriesEmpty() {
  return `<tr data-special-entries-empty><td colspan="4">${emptyState('ti-json', 'No entries', 'Use Add entry to create the first one.')}</td></tr>`;
}
function addSpecialEntryRow() {
  const body = qs('[data-special-entries] tbody');
  if (!body) return;
  body.querySelector('[data-special-entries-empty]')?.remove();
  body.insertAdjacentHTML('beforeend', renderSpecialEntryRow({value_type: 'string'}));
  body.querySelector('[data-special-entry-row]:last-child .special-entry-key')?.focus();
  filterSpecialEntryRows(qs('[data-special-entry-filter]')?.value || '');
}
function handleSpecialEntryAction(button) {
  const row = button.closest('[data-special-entry-row]');
  const body = qs('[data-special-entries] tbody');
  if (!row || !body) return;
  const action = button.dataset.specialEntryAction;
  if (action === 'delete') {
    row.remove();
    if (!body.querySelector('[data-special-entry-row]')) body.innerHTML = renderSpecialEntriesEmpty();
    return syncSpecialEntriesFilterCount();
  }
  if (action === 'add-above') return insertSpecialEntryRow(row, {value_type: 'string'}, 'beforebegin');
  if (action === 'add-below') return insertSpecialEntryRow(row, {value_type: 'string'}, 'afterend');
  if (action === 'duplicate-above') return insertSpecialEntryRow(row, specialEntryFromRow(row, true), 'beforebegin');
  if (action === 'duplicate-below') return insertSpecialEntryRow(row, specialEntryFromRow(row, true), 'afterend');
}
function insertSpecialEntryRow(targetRow, entry, position) {
  targetRow.insertAdjacentHTML(position, renderSpecialEntryRow(entry));
  const newRow = position === 'beforebegin' ? targetRow.previousElementSibling : targetRow.nextElementSibling;
  newRow?.querySelector('.special-entry-key')?.focus();
  filterSpecialEntryRows(qs('[data-special-entry-filter]')?.value || '');
}
function specialEntryFromRow(row, duplicate = false) {
  const key = row.querySelector('.special-entry-key')?.value?.trim() || '';
  return {
    key: duplicate ? nextSpecialEntryName(key) : key,
    value_type: row.querySelector('.special-entry-type')?.value || 'string',
    value_text: row.querySelector('.special-entry-value')?.value || '',
  };
}
function nextSpecialEntryName(key) {
  const base = key || 'NEW_ENTRY';
  const names = new Set(qsa('[data-special-entry-row] .special-entry-key').map((input) => input.value.trim()).filter(Boolean));
  let counter = 2;
  let candidate = `${base}_${counter}`;
  while (names.has(candidate)) {
    counter += 1;
    candidate = `${base}_${counter}`;
  }
  return candidate;
}
function openSpecialValueDialog(row) {
  if (!row) return;
  state.specialValueRow = row;
  setText('#value-edit-modal-title', `Edit value: ${row.querySelector('.special-entry-key')?.value || 'entry'}`);
  const textarea = qs('#value-edit-textarea');
  if (textarea) textarea.value = row.querySelector('.special-entry-value')?.value || '';
  state.specialValueModalClose = openModalElement(qs('#value-edit-modal'), textarea, () => {
    state.specialValueRow = null;
    state.specialValueModalClose = null;
  });
}
function applySpecialValueDialog() {
  const row = state.specialValueRow;
  const value = row?.querySelector('.special-entry-value');
  const input = row?.querySelector('.special-entry-value-input');
  const textarea = qs('#value-edit-textarea');
  if (value && textarea) value.value = textarea.value;
  if (input && textarea) input.value = compactTextValue(textarea.value);
  state.specialValueModalClose?.();
}
async function saveSpecialEntries() {
  if (!state.env || !state.assetPath || state.readOnly) return;
  clearError();
  const validation = validateSpecialEntriesForm();
  if (!validation.ok) return showError(validation.message);
  const entries = qsa('[data-special-entry-row]').map((row) => ({
    key: row.querySelector('.special-entry-key')?.value?.trim() || '',
    value_type: row.querySelector('.special-entry-type')?.value || 'string',
    value_text: row.querySelector('.special-entry-value')?.value || '',
  })).filter((entry) => entry.key !== '');
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/special/${encodeURIComponent(state.assetPath)}/entries`, {entries, expected_hash: state.assetContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadAssets();
    await selectAsset(state.assetPath);
  } catch (e) { showError(e); }
}
function validateSpecialEntriesForm() {
  const table = qs('[data-special-entries]');
  clearInvalidInputsIn(table);
  const keyValidation = validateNamedRows(table, '.special-entry-key', 'Entry key');
  if (!keyValidation.ok) return keyValidation;
  for (const row of qsa('[data-special-entry-row]')) {
    const key = row.querySelector('.special-entry-key')?.value?.trim() || 'entry';
    const type = row.querySelector('.special-entry-type')?.value || 'string';
    const valueInput = row.querySelector('.special-entry-value-input');
    const value = row.querySelector('.special-entry-value')?.value || '';
    const trimmed = value.trim();
    if (type === 'number' && (!trimmed || Number.isNaN(Number(trimmed)))) return invalidInput(valueInput, `Entry "${key}" number value is invalid.`);
    if (type === 'bool' && !['true', 'false'].includes(trimmed.toLowerCase())) return invalidInput(valueInput, `Entry "${key}" bool value must be true or false.`);
    if (type === 'json') {
      try {
        JSON.parse(value);
      } catch (_) {
        return invalidInput(valueInput, `Entry "${key}" JSON value is invalid.`);
      }
    }
  }
  return {ok: true};
}
function filterSpecialEntryRows(query) {
  const needle = String(query || '').trim().toLowerCase();
  qsa('[data-special-entry-row]').forEach((row) => {
    const haystack = [
      row.querySelector('.special-entry-key')?.value,
      row.querySelector('.special-entry-type')?.value,
      row.querySelector('.special-entry-value')?.value,
    ].join(' ').toLowerCase();
    row.classList.toggle('hidden', !!needle && !haystack.includes(needle));
  });
  syncSpecialEntriesFilterCount();
}
function syncSpecialEntriesFilterCount() {
  const rows = qsa('[data-special-entry-row]');
  const visible = rows.filter((row) => !row.classList.contains('hidden')).length;
  setText('[data-special-entry-count]', rows.length ? `${visible}/${rows.length} visible` : '0 entries');
}
function renderPortRow(index, port = {}) {
  const rowId = crypto.randomUUID?.() || String(Date.now() + Math.random());
  const exposes = (port.expose_as || []).map((expose) => renderExposeRow(index, expose)).join('');
  return `
    <div class="detail-panel mb-2" data-port-row="${index}" data-source-index="${Number.isInteger(port.index) ? port.index : -1}">
      <div class="row g-2 align-items-end">
        ${portInput(index, 'name', 'Port name', port.name || '')}
        ${portInput(index, 'port', 'Container port', port.port || '', 'number')}
        ${portInput(index, 'metrics-path-for', 'Metrics path for', port.metrics_path_for || '')}
        <div class="col-6 col-lg-2">
          <label class="form-check mb-2">
            <input class="form-check-input" type="checkbox" data-port-field="${index}:metrics" ${port.metrics ? 'checked' : ''} ${state.readOnly ? 'disabled' : ''}>
            <span class="form-check-label">metrics</span>
          </label>
        </div>
        <div class="col-6 col-lg-1 text-end">
          <button class="btn btn-sm btn-outline-danger btn-icon" type="button" data-remove-port-item="${index}" title="Remove port"><i class="ti ti-trash"></i></button>
        </div>
      </div>
      <div class="mt-2 ps-2 border-start" data-expose-host="${rowId}">
        <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
          <div class="text-muted small fw-semibold">Service exposure</div>
          <button class="btn btn-sm btn-outline-secondary" type="button" data-add-expose="${index}" data-expose-target="${rowId}"><i class="ti ti-plus me-1"></i>Add service</button>
        </div>
        <div data-expose-body="${index}">${exposes || renderExposeEmpty(index)}</div>
      </div>
    </div>`;
}
function renderExposeRow(index, expose = {}) {
  const rowId = crypto.randomUUID?.() || String(Date.now() + Math.random());
  const externals = (expose.externals || []).map((external) => renderExternalRow(index, external)).join('');
  return `
    <div class="resource-editor mb-2" data-expose-row="${index}" data-source-index="${Number.isInteger(expose.index) ? expose.index : -1}">
      <div class="row g-2 align-items-end">
        ${exposeInput(index, 'service-name', 'Service DNS name', expose.service_name || expose.hostname || '')}
        ${exposeInput(index, 'port', 'Service port', expose.port || '', 'number')}
        ${exposeInput(index, 'service-type', 'Service type', expose.service_type || '')}
        <div class="col-12 col-lg-1 text-end">
          <button class="btn btn-sm btn-outline-danger btn-icon" type="button" data-remove-expose-item="${index}" title="Remove service"><i class="ti ti-trash"></i></button>
        </div>
      </div>
      <div class="mt-2 ps-2 border-start" data-external-host="${rowId}">
        <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
          <div class="text-muted small fw-semibold">External exposure</div>
          <button class="btn btn-sm btn-outline-secondary" type="button" data-add-external="${index}" data-external-target="${rowId}"><i class="ti ti-plus me-1"></i>Add external</button>
        </div>
        <div data-external-body="${index}">${externals || renderExternalEmpty(index)}</div>
      </div>
    </div>`;
}
function renderExternalRow(index, external = {}) {
  return `
    <div class="metric-card mb-2" data-external-row="${index}" data-source-index="${Number.isInteger(external.index) ? external.index : -1}">
      <div class="row g-2 align-items-end">
        ${externalInput(index, 'name', 'Name', external.name || '')}
        ${externalInput(index, 'class-name', 'Ingress class', external.class_name || '')}
        ${externalInput(index, 'tls-termination', 'Route TLS termination', external.tls_termination || '')}
        ${externalInput(index, 'tls-policy', 'Insecure edge policy', external.tls_insecure_termination_policy || '')}
        <div class="col-6 col-lg-2">
          <label class="form-check mb-2">
            <input class="form-check-input" type="checkbox" data-external-field="${index}:as-route" ${external.as_route ? 'checked' : ''} ${state.readOnly ? 'disabled' : ''}>
            <span class="form-check-label">Route</span>
          </label>
        </div>
        <div class="col-6 col-lg-1 text-end">
          <button class="btn btn-sm btn-outline-danger btn-icon" type="button" data-remove-external-item="${index}" title="Remove external"><i class="ti ti-trash"></i></button>
        </div>
      </div>
      <div class="row g-2 mt-1">
        ${externalTextarea(index, 'http-hosts', 'HTTP hosts', formatExternalHosts(external.http || [], false), 'hostname | path')}
        ${externalTextarea(index, 'https-hosts', 'HTTPS hosts / TLS', formatExternalHosts(external.https || [], true), 'hostname | path | secret_name')}
        ${externalTextarea(index, 'annotations', 'Annotations', formatMetadataItems(external.annotations || []), 'key=value')}
        ${externalTextarea(index, 'labels', 'Labels', formatMetadataItems(external.labels || []), 'key=value')}
      </div>
    </div>`;
}
function portInput(index, field, label, value, type = 'text') {
  return `<div class="col-12 col-lg-3"><label class="form-label small mb-1">${esc(label)}</label><input class="form-control form-control-sm font-monospace" type="${type}" data-port-field="${index}:${field}" value="${esc(value)}" ${state.readOnly ? 'disabled' : ''}></div>`;
}
function exposeInput(index, field, label, value, type = 'text') {
  return `<div class="col-12 col-lg-3"><label class="form-label small mb-1">${esc(label)}</label><input class="form-control form-control-sm font-monospace" type="${type}" data-expose-field="${index}:${field}" value="${esc(value)}" ${state.readOnly ? 'disabled' : ''}></div>`;
}
function externalInput(index, field, label, value) {
  return `<div class="col-12 col-lg-3"><label class="form-label small mb-1">${esc(label)}</label><input class="form-control form-control-sm font-monospace" data-external-field="${index}:${field}" value="${esc(value)}" ${state.readOnly ? 'disabled' : ''}></div>`;
}
function externalTextarea(index, field, label, value, placeholder) {
  return `<div class="col-12 col-lg-6"><label class="form-label small mb-1">${esc(label)}</label><textarea class="form-control form-control-sm font-monospace" rows="3" data-external-field="${index}:${field}" placeholder="${esc(placeholder)}" ${state.readOnly ? 'disabled' : ''}>${esc(value)}</textarea></div>`;
}
function formatExternalHosts(hosts, includeSecret) {
  return hosts.map((host) => [host.hostname || '', host.path || '', ...(includeSecret ? [host.secret_name || ''] : [])].join(' | ')).join('\n');
}
function formatMetadataItems(items) {
  return items.map((item) => `${item.name || ''}=${item.value ?? ''}`).join('\n');
}
function parseExternalHosts(value, includeSecret) {
  return String(value || '').split(/\r?\n/).map((line, index) => {
    const parts = line.split('|').map((item) => item.trim());
    return {source_index: index, hostname: parts[0] || '', path: parts[1] || '', secret_name: includeSecret ? parts[2] || '' : ''};
  }).filter((item) => item.hostname || item.path || item.secret_name);
}
function parseMetadataItems(value) {
  return String(value || '').split(/\r?\n/).map((line) => {
    const separator = line.indexOf('=');
    return separator < 0 ? {name: line.trim(), value: ''} : {name: line.slice(0, separator).trim(), value: line.slice(separator + 1)};
  }).filter((item) => item.name);
}
function renderPortsEmpty(index) {
  return `<div data-port-empty="${index}">${emptyState('ti-route', 'No ports', 'Use Add port to declare a container port.')}</div>`;
}
function renderExposeEmpty(index) {
  return `<div data-expose-empty="${index}">${emptyState('ti-plug-connected', 'No service exposure', 'Use Add service to expose this port inside the cluster.')}</div>`;
}
function renderExternalEmpty(index) {
  return `<div data-external-empty="${index}">${emptyState('ti-world', 'No external exposure', 'Use Add external to create an Ingress or Route.')}</div>`;
}
function syncPortsEmptyState(index) {
  const body = qs(`[data-port-body="${index}"]`);
  if (body && !body.querySelector(`[data-port-row="${index}"]`)) body.innerHTML = renderPortsEmpty(index);
}
function syncExposeEmptyState(index) {
  qsa(`[data-expose-body="${index}"]`).forEach((body) => {
    if (!body.querySelector(`[data-expose-row="${index}"]`)) body.innerHTML = renderExposeEmpty(index);
  });
}
function syncExternalEmptyState(index) {
  qsa(`[data-external-body="${index}"]`).forEach((body) => {
    if (!body.querySelector(`[data-external-row="${index}"]`)) body.innerHTML = renderExternalEmpty(index);
  });
}
function addPortRow(index) {
  const body = qs(`[data-port-body="${index}"]`);
  if (!body) return;
  body.querySelector(`[data-port-empty="${index}"]`)?.remove();
  body.insertAdjacentHTML('beforeend', renderPortRow(index));
}
function addExposeRow(index, button) {
  const host = button?.dataset.exposeTarget ? qs(`[data-expose-host="${button.dataset.exposeTarget}"]`) : null;
  const body = host?.querySelector(`[data-expose-body="${index}"]`);
  if (!body) return;
  body.querySelector(`[data-expose-empty="${index}"]`)?.remove();
  body.insertAdjacentHTML('beforeend', renderExposeRow(index));
}
function addExternalRow(index, button) {
  const host = button?.dataset.externalTarget ? qs(`[data-external-host="${button.dataset.externalTarget}"]`) : null;
  const body = host?.querySelector(`[data-external-body="${index}"]`);
  if (!body) return;
  body.querySelector(`[data-external-empty="${index}"]`)?.remove();
  body.insertAdjacentHTML('beforeend', renderExternalRow(index));
}
async function saveContainerPorts(index) {
  if (!state.env || !state.appFile || state.readOnly || !Number.isInteger(index)) return;
  const ports = qsa(`[data-port-row="${index}"]`).map((portRow) => ({
    source_index: Number(portRow.dataset.sourceIndex ?? -1),
    name: portRow.querySelector(`[data-port-field="${index}:name"]`)?.value?.trim() || '',
    port: portRow.querySelector(`[data-port-field="${index}:port"]`)?.value?.trim() || '',
    metrics: !!portRow.querySelector(`[data-port-field="${index}:metrics"]`)?.checked,
    metrics_path_for: portRow.querySelector(`[data-port-field="${index}:metrics-path-for"]`)?.value?.trim() || '',
    expose_as: Array.from(portRow.querySelectorAll(`[data-expose-row="${index}"]`)).map((exposeRow) => ({
      source_index: Number(exposeRow.dataset.sourceIndex ?? -1),
      service_name: exposeRow.querySelector(`[data-expose-field="${index}:service-name"]`)?.value?.trim() || '',
      port: exposeRow.querySelector(`[data-expose-field="${index}:port"]`)?.value?.trim() || '',
      service_type: exposeRow.querySelector(`[data-expose-field="${index}:service-type"]`)?.value?.trim() || '',
      externals: Array.from(exposeRow.querySelectorAll(`[data-external-row="${index}"]`)).map((externalRow) => ({
        source_index: Number(externalRow.dataset.sourceIndex ?? -1),
        name: externalRow.querySelector(`[data-external-field="${index}:name"]`)?.value?.trim() || '',
        as_route: !!externalRow.querySelector(`[data-external-field="${index}:as-route"]`)?.checked,
        class_name: externalRow.querySelector(`[data-external-field="${index}:class-name"]`)?.value?.trim() || '',
        http: parseExternalHosts(externalRow.querySelector(`[data-external-field="${index}:http-hosts"]`)?.value, false),
        https: parseExternalHosts(externalRow.querySelector(`[data-external-field="${index}:https-hosts"]`)?.value, true),
        annotations: parseMetadataItems(externalRow.querySelector(`[data-external-field="${index}:annotations"]`)?.value),
        labels: parseMetadataItems(externalRow.querySelector(`[data-external-field="${index}:labels"]`)?.value),
        tls_termination: externalRow.querySelector(`[data-external-field="${index}:tls-termination"]`)?.value?.trim() || '',
        tls_insecure_termination_policy: externalRow.querySelector(`[data-external-field="${index}:tls-policy"]`)?.value?.trim() || '',
      })),
    })),
  })).filter((port) => port.name || port.port);
  clearError();
  const validation = validatePortsForm(index);
  if (!validation.ok) return showError(validation.message);
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/containers/${index}/ports`, {ports, expected_hash: state.appContentHash});
    state.portsModalClose?.();
    state.portsModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function validatePortsForm(index) {
  clearInvalidInputs('#ports-edit-modal');
  const seenPorts = new Set();
  for (const portRow of qsa(`[data-port-row="${index}"]`)) {
    const nameInput = portRow.querySelector(`[data-port-field="${index}:name"]`);
    const portInputEl = portRow.querySelector(`[data-port-field="${index}:port"]`);
    const name = nameInput?.value?.trim() || '';
    const port = portInputEl?.value?.trim() || '';
    if (!name) return invalidInput(nameInput, 'Port name is required.');
    if (/[\\r\\n:]/.test(name)) return invalidInput(nameInput, 'Port name cannot contain colon or newline.');
    if (seenPorts.has(name)) return invalidInput(nameInput, `Duplicate port name "${name}".`);
    seenPorts.add(name);
    const portValidation = validatePortValue(portInputEl, 'Container port');
    if (!portValidation.ok) return portValidation;
    const seenServices = new Set();
    for (const exposeRow of portRow.querySelectorAll(`[data-expose-row="${index}"]`)) {
      const serviceInput = exposeRow.querySelector(`[data-expose-field="${index}:service-name"]`);
      const servicePortInput = exposeRow.querySelector(`[data-expose-field="${index}:port"]`);
      const serviceName = serviceInput?.value?.trim() || '';
      if (!serviceName) return invalidInput(serviceInput, 'Service DNS name is required.');
      if (/[\\r\\n:]/.test(serviceName)) return invalidInput(serviceInput, 'Service DNS name cannot contain colon or newline.');
      if (seenServices.has(serviceName)) return invalidInput(serviceInput, `Duplicate service DNS name "${serviceName}".`);
      seenServices.add(serviceName);
      const servicePortValidation = validatePortValue(servicePortInput, 'Service port');
      if (!servicePortValidation.ok) return servicePortValidation;
      const seenExternals = new Set();
      for (const externalRow of exposeRow.querySelectorAll(`[data-external-row="${index}"]`)) {
        const externalNameInput = externalRow.querySelector(`[data-external-field="${index}:name"]`);
        const externalName = externalNameInput?.value?.trim() || '';
        if (!externalName) return invalidInput(externalNameInput, 'External name is required.');
        if (/[\\r\\n:]/.test(externalName)) return invalidInput(externalNameInput, 'External name cannot contain colon or newline.');
        if (seenExternals.has(externalName)) return invalidInput(externalNameInput, `Duplicate external name "${externalName}".`);
        seenExternals.add(externalName);
        const httpInput = externalRow.querySelector(`[data-external-field="${index}:http-hosts"]`);
        const httpsInput = externalRow.querySelector(`[data-external-field="${index}:https-hosts"]`);
        const httpHosts = parseExternalHosts(httpInput?.value, false);
        const httpsHosts = parseExternalHosts(httpsInput?.value, true);
        if (!httpHosts.length && !httpsHosts.length) return invalidInput(httpInput, `External "${externalName}" requires an HTTP or HTTPS hostname.`);
        for (const [input, hosts, label] of [[httpInput, httpHosts, 'HTTP'], [httpsInput, httpsHosts, 'HTTPS']]) {
          if (hosts.some((host) => !host.hostname)) return invalidInput(input, `${label} host lines require a hostname.`);
        }
        for (const field of ['annotations', 'labels']) {
          const input = externalRow.querySelector(`[data-external-field="${index}:${field}"]`);
          const lines = String(input?.value || '').split(/\r?\n/).filter((line) => line.trim());
          if (lines.some((line) => !line.includes('=') || !line.slice(0, line.indexOf('=')).trim())) return invalidInput(input, `${field} must use one key=value entry per line.`);
        }
      }
    }
  }
  return {ok: true};
}
function validatePortValue(input, label) {
  const value = input?.value?.trim() || '';
  const port = Number(value);
  if (!/^[0-9]+$/.test(value) || !Number.isInteger(port) || port < 1 || port > 65535) {
    return invalidInput(input, `${label} must be an integer between 1 and 65535.`);
  }
  return {ok: true};
}
function renderContainerEnvsEditor(index, vars) {
  if (state.readOnly) return '';
  const rows = (vars || []).map((item) => renderContainerEnvRow(index, item)).join('');
  return `
    <div class="container-env-editor mt-3" data-container-env-editor="${index}">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="overview-subtitle mb-0">Container envs</div>
        <button class="btn btn-sm btn-outline-primary" type="button" data-add-container-env="${index}"><i class="ti ti-plus me-1"></i>Add env</button>
      </div>
      <div class="table-responsive">
        <table class="table table-sm table-vcenter mb-0 container-env-table">
          <thead><tr><th style="width: 24%">Name</th><th style="width: 22%">Type</th><th>Configuration</th><th class="text-end">Action</th></tr></thead>
          <tbody data-container-env-body="${index}">
            ${rows || renderContainerEnvsEmpty(index)}
          </tbody>
        </table>
      </div>
      <div class="d-flex align-items-center justify-content-between gap-2 mt-2">
        <div class="text-muted small">Builder env sources are editable. Raw Kubernetes valueFrom and unknown entries remain read-only.</div>
        <button class="btn btn-sm btn-primary" type="button" data-save-container-envs="${index}"><i class="ti ti-device-floppy me-1"></i>Save envs</button>
      </div>
      <datalist id="container-env-token-names">${(state.model?.workload_identity_token_ref_names || []).map((name) => `<option value="${esc(name)}"></option>`).join('')}</datalist>
      <datalist id="container-env-asset-names">${(state.model?.shared_asset_ref_names || []).map((name) => `<option value="${esc(name)}"></option>`).join('')}</datalist>
    </div>`;
}
function renderContainerEnvConfig(item, editable) {
  if (!editable) return `<div class="form-control form-control-sm font-monospace text-muted" aria-readonly="true">${esc(envVarReadOnlyValue(item))}</div>`;
  const field = (kind, name, placeholder, value) => {
    const list = kind === 'workload_identity_token' ? ' list="container-env-token-names"' : kind === 'shared_asset' ? ' list="container-env-asset-names"' : '';
    return `<div data-container-env-config="${kind}" class="${item.kind === kind ? '' : 'd-none'}"><input class="form-control form-control-sm font-monospace" data-container-env-config-field="${name}" value="${esc(value || '')}" placeholder="${esc(placeholder)}" autocomplete="off"${list}></div>`;
  };
  const pair = (kind, fields) => `<div data-container-env-config="${kind}" class="${item.kind === kind ? '' : 'd-none'}"><div class="row g-1">${fields.map(([name, placeholder, value]) => `<div class="col"><input class="form-control form-control-sm font-monospace" data-container-env-config-field="${name}" value="${esc(value || '')}" placeholder="${esc(placeholder)}" autocomplete="off"></div>`).join('')}</div></div>`;
  return `${field('value', 'value', 'Value', item.value)}
    ${pair('secret', [['secret_name', 'Secret name', item.secret_name], ['key', 'Key', item.key]])}
    ${pair('resource', [['resource_name', 'requests.cpu', item.resource_name], ['divisor', 'Divisor (optional)', item.divisor]])}
    ${field('field', 'field_path', 'metadata.name', item.field_path)}
    ${field('workload_identity_token', 'workload_identity_token_ref_name', 'Token definition name', item.workload_identity_token_ref_name)}
    ${field('shared_asset', 'shared_asset_ref_name', 'Shared asset name', item.shared_asset_ref_name)}
    <div data-container-env-config="remove" class="${item.kind === 'remove' ? '' : 'd-none'}"><span class="text-muted small">Removes an inherited variable with this name.</span></div>`;
}
function renderContainerEnvRow(index, item = {kind: 'value', index: -1, is_value_editable: true}) {
  const editable = item.is_value_editable !== false;
  const kind = item.kind || 'value';
  const sourceIndex = Number.isInteger(item.index) ? item.index : -1;
  const kindControl = editable
    ? `<select class="form-select form-select-sm" data-container-env-kind>${defaultsSidecarEnvKinds.map(([value, label]) => `<option value="${value}" ${value === kind ? 'selected' : ''}>${esc(label)}</option>`).join('')}</select>`
    : `<span class="badge bg-secondary-lt">${esc(kind === 'kubernetes_value_from' ? 'Raw valueFrom' : 'Unsupported')}</span>`;
  const remove = editable
    ? `<button class="btn btn-sm btn-ghost-danger btn-icon" type="button" data-remove-container-env="${index}" title="Remove env"><i class="ti ti-trash"></i></button>`
    : `<span class="badge bg-secondary-lt">read-only</span>`;
  return `
    <tr data-container-env-row="${index}" data-env-editable="${editable ? 'true' : 'false'}" data-source-index="${sourceIndex}">
      <td><input class="form-control form-control-sm font-monospace" data-container-env-name placeholder="VARIABLE_NAME" value="${esc(item.name || '')}" ${editable ? '' : 'disabled'}></td>
      <td>${kindControl}</td>
      <td>${renderContainerEnvConfig({...item, kind}, editable)}</td>
      <td class="table-action-col">${remove}</td>
    </tr>`;
}
function envVarReadOnlyValue(item) {
  if (item.kind === 'workload_identity_token') return item.workload_identity_token_ref_name || 'workload identity token';
  if (item.kind === 'shared_asset') return item.shared_asset_ref_name || 'shared asset';
  if (item.kind === 'remove') return 'remove inherited value';
  if (item.kind === 'secret') return `${item.secret_name || '?'}:${item.key || '?'}`;
  if (item.kind === 'resource') return [item.resource_name, item.divisor].filter(Boolean).join(' / ') || 'resourceFieldRef';
  if (item.kind === 'field') return item.field_path || 'fieldRef';
  return item.value || '';
}
function renderContainerEnvsEmpty(index) {
  return `<tr data-container-env-empty="${index}"><td colspan="4">${emptyState('ti-variable', 'No container envs', state.readOnly ? 'Start with --allow-write to add envs.' : 'Use Add env to create the first one.')}</td></tr>`;
}
function syncContainerEnvsEmptyState(index) {
  const body = qs(`[data-container-env-body="${index}"]`);
  if (!body) return;
  const hasRows = qsa(`[data-container-env-row="${index}"]`).length > 0;
  if (!hasRows) body.innerHTML = renderContainerEnvsEmpty(index);
}
function addContainerEnvRow(index) {
  if (state.readOnly || !state.appFile || !Number.isInteger(index)) return;
  const body = qs(`[data-container-env-body="${index}"]`);
  if (!body) return;
  body.querySelector(`[data-container-env-empty="${index}"]`)?.remove();
  body.insertAdjacentHTML('beforeend', renderContainerEnvRow(index));
  qsa(`[data-container-env-row="${index}"]`).at(-1)?.querySelector('[data-container-env-name]')?.focus();
}
function syncContainerEnvRow(row) {
  if (!row) return;
  const kind = row.querySelector('[data-container-env-kind]')?.value || 'value';
  row.querySelectorAll('[data-container-env-config]').forEach((config) => config.classList.toggle('d-none', config.dataset.containerEnvConfig !== kind));
}
function containerEnvValues(index) {
  const field = (row, name) => row.querySelector(`[data-container-env-config-field="${name}"]`)?.value || '';
  return qsa(`[data-container-env-row="${index}"]`).map((row) => {
    const sourceIndex = Number(row.dataset.sourceIndex);
    if (row.dataset.envEditable !== 'true') return {source_index: sourceIndex, name: row.querySelector('[data-container-env-name]')?.value || '', kind: 'preserve'};
    const item = {source_index: sourceIndex, name: row.querySelector('[data-container-env-name]')?.value?.trim() || '', kind: row.querySelector('[data-container-env-kind]')?.value || 'value'};
    if (item.kind === 'value') item.value = field(row, 'value');
    if (item.kind === 'secret') Object.assign(item, {secret_name: field(row, 'secret_name').trim(), key: field(row, 'key').trim()});
    if (item.kind === 'resource') Object.assign(item, {resource_name: field(row, 'resource_name').trim(), divisor: field(row, 'divisor').trim()});
    if (item.kind === 'field') item.field_path = field(row, 'field_path').trim();
    if (item.kind === 'workload_identity_token') item.workload_identity_token_ref_name = field(row, 'workload_identity_token_ref_name').trim();
    if (item.kind === 'shared_asset') item.shared_asset_ref_name = field(row, 'shared_asset_ref_name').trim();
    return item;
  });
}
function validateContainerEnvsForm(index, items) {
  clearInvalidInputs(`[data-container-env-editor="${index}"]`);
  const rows = qsa(`[data-container-env-row="${index}"]`);
  const seen = new Set();
  for (let position = 0; position < items.length; position++) {
    const item = items[position];
    const row = rows[position];
    const nameInput = row?.querySelector('[data-container-env-name]');
    if (!variableNameRe.test(item.name)) return invalidInput(nameInput, `Variable ${position + 1} needs a valid name.`);
    if (seen.has(item.name)) return invalidInput(nameInput, `Variable name ${item.name} is duplicated.`);
    seen.add(item.name);
    if (item.kind === 'preserve') continue;
    const required = (name, message) => {
      const input = row?.querySelector(`[data-container-env-config-field="${name}"]`);
      return input?.value?.trim() ? null : invalidInput(input, message);
    };
    let invalid = null;
    if (item.kind === 'secret') invalid = required('secret_name', 'Secret name is required.') || required('key', 'Secret key is required.');
    if (item.kind === 'resource') invalid = required('resource_name', 'Resource name is required.');
    if (item.kind === 'field') invalid = required('field_path', 'Field path is required.');
    if (item.kind === 'workload_identity_token') invalid = required('workload_identity_token_ref_name', 'Token definition name is required.');
    if (item.kind === 'shared_asset') invalid = required('shared_asset_ref_name', 'Shared asset name is required.');
    if (invalid) return invalid;
    if (item.kind === 'workload_identity_token' && !(state.model?.workload_identity_token_ref_names || []).includes(item.workload_identity_token_ref_name)) return invalidInput(row?.querySelector('[data-container-env-config-field="workload_identity_token_ref_name"]'), `Unknown token definition ${item.workload_identity_token_ref_name}.`);
    if (item.kind === 'shared_asset' && !(state.model?.shared_asset_ref_names || []).includes(item.shared_asset_ref_name)) return invalidInput(row?.querySelector('[data-container-env-config-field="shared_asset_ref_name"]'), `Unknown shared asset ${item.shared_asset_ref_name}.`);
  }
  return {ok: true};
}
async function saveContainerEnvs(index) {
  if (!state.env || !state.appFile || state.readOnly || !Number.isInteger(index)) return;
  clearError();
  const items = containerEnvValues(index);
  const validation = validateContainerEnvsForm(index, items);
  if (!validation.ok) return showError(validation.message);
  try {
    await apiPatch(`/api/v1/envs/${encodeURIComponent(state.env)}/apps/${encodeURIComponent(state.appFile)}/containers/${index}/envs`, {items, expected_hash: state.appContentHash});
    state.editModalClose?.();
    state.editModalClose = null;
    await refreshRepositorySnapshot();
    await loadApps();
    await selectApp(state.appFile);
  } catch (e) { showError(e); }
}
function fact(label, value, icon) {
  return `<div class="overview-fact"><div class="text-muted small"><i class="ti ${icon} me-1"></i>${esc(label)}</div><div class="fw-semibold">${esc(value)}</div></div>`;
}
function chip(label, value) {
  return `<span class="model-chip"><span>${esc(label)}</span><strong>${esc(value)}</strong></span>`;
}
async function loadAssets() {
  if (!state.env) return;
  const data = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets`);
  state.assets = data.items || [];
  setText('#assets-count', state.assets.length);
  renderAssets();
}
function renderAssets() {
  const host = qs('#assets-list');
  const dirtyAssets = state.assets.filter((a) => gitFile(assetGitPath(a.relative_path)));
  const dirtyBadgeEl = qs('#assets-dirty-count');
  if (dirtyBadgeEl) {
    dirtyBadgeEl.classList.toggle('hidden', dirtyAssets.length === 0);
    dirtyBadgeEl.textContent = dirtyAssets.length ? `${dirtyAssets.length} changed` : '';
  }
  if (!host) return;
  const query = (qs('#assets-filter')?.value || '').trim().toLowerCase();
  const assets = state.assets.filter((asset) => {
    if (!query) return true;
    return [asset.relative_path, asset.file_name, asset.driver, asset.size_bytes].some((value) => String(value ?? '').toLowerCase().includes(query));
  });
  setText('#assets-filter-count', `${assets.length}/${state.assets.length}`);
  if (query) state.assetOpenDirs = assetDefaultOpenDirs(assets);
  host.innerHTML = renderAssetTree(assets, query);
  qsa('[data-asset]').forEach((x) => x.addEventListener('click', () => selectAsset(x.dataset.asset)));
  qsa('[data-asset-dir]').forEach((x) => x.addEventListener('click', () => {
    const path = x.dataset.assetDir || '';
    if (state.assetOpenDirs.has(path)) state.assetOpenDirs.delete(path);
    else state.assetOpenDirs.add(path);
    renderAssets();
  }));
}
function renderAssetTree(assets, query = '') {
  if (!assets.length) return `<div class="asset-empty">${emptyState('ti-folders', query ? 'No matching assets' : 'No assets', query ? 'Try a different filter.' : 'No assets were found for this environment.')}</div>`;
  const root = { name: '', path: '', dirs: new Map(), files: [] };
  for (const asset of assets) addAssetTreeNode(root, asset);
  if (!state.assetOpenDirs.size) state.assetOpenDirs = assetDefaultOpenDirs(assets);
  return `<div class="py-1">${renderAssetTreeNode(root)}</div>`;
}
function addAssetTreeNode(root, asset) {
  const parts = asset.relative_path.split('/').filter(Boolean);
  if (parts.length <= 1) {
    root.files.push(asset);
    return;
  }
  let node = root;
  for (const part of parts.slice(0, -1)) {
    if (!node.dirs.has(part)) {
      const path = node.path ? `${node.path}/${part}` : part;
      node.dirs.set(part, { name: part, path, dirs: new Map(), files: [] });
    }
    node = node.dirs.get(part);
  }
  node.files.push(asset);
}
function assetDefaultOpenDirs(assets) {
  const dirs = new Set(['']);
  for (const asset of assets || []) {
    const parts = String(asset.relative_path || '').split('/').filter(Boolean);
    for (let i = 1; i < parts.length; i += 1) dirs.add(parts.slice(0, i).join('/'));
  }
  return dirs;
}
function renderAssetTreeNode(node) {
  const dirs = Array.from(node.dirs.values()).sort((a, b) => a.name.localeCompare(b.name));
  const files = node.files.slice().sort((a, b) => a.relative_path.localeCompare(b.relative_path));
  return [
    ...dirs.map((dir) => renderAssetDirectory(dir)),
    ...files.map((asset) => renderAssetFile(asset)),
  ].join('');
}
function renderAssetDirectory(dir) {
  const open = state.assetOpenDirs.has(dir.path);
  return `
    <div class="build-preview-tree-dir">
      <div class="build-preview-tree-node" data-asset-dir="${esc(dir.path)}">
        <div class="build-preview-tree-main">
          <span class="build-preview-tree-icon text-muted"><i class="ti ${open ? 'ti-chevron-down' : 'ti-chevron-right'}"></i></span>
          <i class="ti ${open ? 'ti-folder-open' : 'ti-folder'} text-warning"></i>
          <span class="font-monospace small text-truncate" title="${esc(dir.path)}">${esc(dir.name)}</span>
        </div>
        <span class="build-preview-tree-size">${dir.files.length + dir.dirs.size}</span>
      </div>
      ${open ? `<div class="build-preview-tree-children">${renderAssetTreeNode(dir)}</div>` : ''}
    </div>`;
}
function renderAssetFile(asset) {
  const dirty = gitFile(assetGitPath(asset.relative_path));
  return `
    <div class="build-preview-tree-node ${state.assetPath === asset.relative_path ? 'active' : ''}" data-asset="${esc(asset.relative_path)}">
      <div class="build-preview-tree-main">
        <span class="build-preview-tree-icon text-muted"><i class="ti ti-minus"></i></span>
        <i class="ti ${assetIcon(asset)}"></i>
        <span class="font-monospace small text-truncate" title="${esc(asset.relative_path)}">${esc(assetFileName(asset.relative_path))}</span>
        ${dirtyBadge(dirty)}
      </div>
      <div class="build-preview-tree-size d-flex align-items-center gap-2">
        <span>${esc(formatBytes(asset.size_bytes ?? 0))}</span>
        <span class="badge bg-secondary-lt">${esc(asset.driver)}</span>
      </div>
    </div>`;
}
function assetFileName(path) {
  const parts = path.split('/').filter(Boolean);
  return parts[parts.length - 1] || path;
}
function assetIcon(asset) {
  if (asset.driver === 'defaults') return 'ti-settings';
  if (asset.driver === 'special') return 'ti-lock-square-rounded';
  const name = asset.relative_path.toLowerCase();
  if (name.endsWith('.yml') || name.endsWith('.yaml')) return 'ti-file-code';
  if (name.endsWith('.json')) return 'ti-json';
  if (name.endsWith('.conf') || name.endsWith('.tpl')) return 'ti-file-settings';
  return 'ti-file';
}
function formatBytes(bytes) {
  const value = Number(bytes || 0);
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}
function assetGitPath(relativePath) {
  if (isDefaultsAsset(relativePath)) return `${state.env}/apps/${relativePath}`;
  return isRootMetadataAsset(relativePath) ? `${state.env}/${relativePath}` : `${state.env}/assets/${relativePath}`;
}
async function selectAsset(path, options = {}) {
  state.assetPath = path; renderAssets(); clearError();
  if (options.updateRoute !== false) {
    setActive('assets');
    pushRoute({page: 'assets', asset: path});
  }
  try {
    const selectedAsset = state.assets.find((asset) => asset.relative_path === path);
    setText('#asset-detail-title', path);
    setHTML('#asset-structured', '');
    if (isSpecial(path)) {
      const [detail, rawEntries, preflight] = await Promise.all([
        api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/content/${path.split('/').map(encodeURIComponent).join('/')}`),
        api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/special/${encodeURIComponent(path)}/entries`),
        api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/special/${encodeURIComponent(path)}/preflight`),
      ]);
      let entries = rawEntries;
      if (isSecuredSpecial(path) && preflight?.ok && preflight?.decrypt_ok) {
        try {
          entries = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/special/${encodeURIComponent(path)}/decrypted-entries`);
          entries.decrypted = true;
        } catch (e) {
          entries = {...rawEntries, warning: `Decrypt preview failed: ${String(e)}`};
        }
      } else if (isSecuredSpecial(path) && preflight?.issues?.length) {
        entries = {...rawEntries, warning: preflight.issues.join('\n')};
      }
      entries.preflight = preflight;
      state.assetContentHash = entries.content_hash || detail.content_hash || null;
      state.specialEntries = entries;
      setText('#asset-detail-path', `${entries.entries?.length || 0} entrie(s), editable=${entries.editable}`);
      setHTML('#asset-detail-badges', renderAssetDetailBadges(selectedAsset, entries));
      setHTML('#asset-structured', renderSpecialEntriesPreview(entries));
      setText('#asset-raw', detail.content);
      await loadGitDiff(`${state.env}/${path}`, entries.is_dirty, '#asset-diff-section', '#asset-diff');
    } else if (isDefaultsAsset(path)) {
      const [detail, defaults] = await Promise.all([
        api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/content/${path.split('/').map(encodeURIComponent).join('/')}`),
        api(`/api/v1/envs/${encodeURIComponent(state.env)}/defaults`),
      ]);
      state.assetContentHash = defaults.content_hash || detail.content_hash || null;
      state.defaults = defaults;
      state.defaultsContentHash = defaults.content_hash || null;
      setText('#asset-detail-path', detail.path);
      setHTML('#asset-detail-badges', renderAssetDetailBadges(selectedAsset, defaults));
      setHTML('#asset-structured', renderDefaultsPreview(defaults));
      setText('#asset-raw', detail.content);
      await loadGitDiff(assetGitPath(detail.relative_path), detail.is_dirty, '#asset-diff-section', '#asset-diff');
    } else if (isSharedAssetsAsset(path)) {
      const detail = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/content/${path.split('/').map(encodeURIComponent).join('/')}`);
      const inspection = await loadInspection();
      state.assetContentHash = detail.content_hash || null;
      setText('#asset-detail-path', detail.path);
      setHTML('#asset-detail-badges', renderAssetDetailBadges(selectedAsset, detail));
      setHTML('#asset-structured', renderSharedAssetsMetadata(inspection?.shared_assets));
      setText('#asset-raw', detail.content);
      await loadGitDiff(assetGitPath(detail.relative_path), detail.is_dirty, '#asset-diff-section', '#asset-diff');
    } else {
      const detail = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/assets/content/${path.split('/').map(encodeURIComponent).join('/')}`);
      state.assetContentHash = detail.content_hash || null;
      setText('#asset-detail-path', detail.path);
      setHTML('#asset-detail-badges', renderAssetDetailBadges(selectedAsset, detail));
      setText('#asset-raw', detail.content);
      await loadGitDiff(assetGitPath(detail.relative_path), detail.is_dirty, '#asset-diff-section', '#asset-diff');
    }
  } catch (e) { showError(e); }
}
function renderAssetDetailBadges(asset, detail) {
  return [
    asset ? badge(asset.driver, 'bg-cyan-lt', 'ti-tag') : '',
    asset ? badge(formatBytes(asset.size_bytes ?? 0), 'bg-secondary-lt', 'ti-database') : '',
    detail?.is_dirty ? badge('dirty', 'bg-yellow-lt', 'ti-alert-triangle') : '',
    detail && 'editable' in detail ? badge(detail.editable ? 'editable' : 'read-only', detail.editable ? 'bg-green-lt' : 'bg-secondary-lt', detail.editable ? 'ti-pencil' : 'ti-lock') : '',
  ].filter(Boolean).join('');
}
function renderDefaultsPreview(defaults) {
  const vars = defaults.vars || [];
  const groups = defaults.container_envs || [];
  return `
    <div class="alert alert-info py-2 mb-3"><div class="d-flex flex-wrap align-items-center justify-content-between gap-2"><span><i class="ti ti-info-circle me-2"></i>This is a read-only source summary. Edit definitions in the Defaults workspace.</span><button class="btn btn-sm btn-outline-primary" type="button" data-open-defaults-catalog><i class="ti ti-adjustments-horizontal me-1"></i>Open Defaults</button></div></div>
    <div class="overview-section mb-3">
      <div class="overview-subtitle mb-2">Defaults local variables</div>
      ${vars.length ? `<div class="chip-row">${vars.map((item) => chip(item.name || '?', item.value || '')).join('')}</div>` : emptyState('ti-variable', 'No default vars')}
    </div>
    <div class="overview-section mb-3">
      <div class="overview-subtitle mb-2">Defaults container envs</div>
      ${groups.length ? groups.map(renderDefaultsGroupPreview).join('') : emptyState('ti-variable', 'No container env defaults')}
    </div>`;
}
function renderSharedAssetsMetadata(document) {
  const assets = document?.model?.assets || [];
  return `<div class="overview-section mb-3">
    <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
      <div><div class="overview-subtitle mb-1">Shared asset definitions</div><div class="text-muted small">Metadata references source files stored under the environment assets directory.</div></div>
      <span class="badge bg-secondary-lt">${assets.length} asset${assets.length === 1 ? '' : 's'}</span>
    </div>
    ${assets.length ? `<div class="row g-2">${assets.map((item) => renderDefaultsCatalogItem('shared_asset', item)).join('')}</div>` : emptyState('ti-files', 'No shared assets', 'This metadata document contains no asset definitions.')}
  </div>`;
}
function renderDefaultsGroupPreview(group) {
  const envs = group.envs || [];
  return `
    <div class="metric-card mb-2">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="fw-semibold font-monospace">${esc(group.container_ref_name || '?')}</div>
        <span class="badge bg-secondary-lt">${envs.length} envs</span>
      </div>
      ${envs.length ? `<div class="chip-row">${envs.slice(0, 8).map((item) => chip(item.name || '?', item.kind === 'value' ? (item.value || '') : item.kind)).join('')}${envs.length > 8 ? `<span class="badge bg-secondary-lt">+${envs.length - 8} more</span>` : ''}</div>` : '<div class="text-muted small">No envs in this group.</div>'}
    </div>`;
}
function renderSpecialEntriesPreview(entries) {
  const items = entries.entries || [];
  const canEditEntries = !state.readOnly && entries.editable && (state.assetPath === 'env.unsecured.json' || (state.assetPath === 'env.secured.json' && entries.decrypted));
  const edit = canEditEntries ? `<button class="btn btn-sm btn-outline-primary" type="button" data-edit-special-entries><i class="ti ti-pencil me-1"></i>Edit entries</button>` : '';
  const decryptBadge = entries.decrypted ? `<span class="badge bg-green-lt"><i class="ti ti-lock-open me-1"></i>${entries.editable ? 'decrypted editable' : 'decrypted preview'}</span>` : (isSecuredSpecial(state.assetPath) ? `<span class="badge bg-yellow-lt"><i class="ti ti-lock me-1"></i>encrypted/raw</span>` : '');
  return `
    <div class="overview-section mb-3">
      <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
        <div class="d-flex align-items-center gap-2">
          <div class="overview-subtitle mb-0">Special entries</div>
          ${decryptBadge}
        </div>
        ${edit}
      </div>
      ${entries.warning ? `<div class="alert ${entries.decrypted ? 'alert-info' : 'alert-warning'} py-2">${esc(entries.warning)}</div>` : ''}
      ${items.length ? `
        <div class="input-icon mb-2">
          <span class="input-icon-addon"><i class="ti ti-search"></i></span>
          <input class="form-control form-control-sm" data-special-preview-filter placeholder="Filter key, value or type...">
        </div>
        <div class="text-muted small mb-2" data-special-preview-count>${items.length} entries</div>
        <div class="special-entry-preview-list">${items.map(renderSpecialEntryPreview).join('')}</div>` : emptyState('ti-json', 'No entries', 'No entries in this special file.')}
    </div>`;
}
function renderSpecialEntryPreview(item) {
  return `
    <div class="special-entry-card" data-special-preview-card>
      <div class="special-entry-card-head">
        <div class="special-entry-name font-monospace text-break">${esc(item.key)}</div>
        <div class="special-entry-card-actions">
          <button class="btn btn-sm btn-ghost-secondary btn-icon special-entry-action-btn" type="button" data-copy-special-entry="name" data-copy-value="${esc(item.key)}" title="Copy name"><i class="ti ti-copy"></i></button>
          <button class="btn btn-sm btn-ghost-secondary btn-icon special-entry-action-btn" type="button" data-copy-special-entry="value" data-copy-value="${esc(item.value_text)}" title="Copy value"><i class="ti ti-copy-check"></i></button>
          <span class="badge bg-blue-lt">${esc(item.value_type)}</span>
        </div>
      </div>
      <div class="special-entry-preview-value font-monospace">${esc(item.value_text)}</div>
    </div>`;
}
function filterSpecialPreviewRows(query) {
  const needle = String(query || '').trim().toLowerCase();
  const cards = qsa('[data-special-preview-card]');
  cards.forEach((card) => {
    card.classList.toggle('hidden', !!needle && !card.textContent.toLowerCase().includes(needle));
  });
  const visible = cards.filter((card) => !card.classList.contains('hidden')).length;
  setText('[data-special-preview-count]', cards.length ? `${visible}/${cards.length} visible` : '0 entries');
}
function handleAssetStructuredClick(e) {
  const copyButton = e.target.closest('[data-copy-special-entry]');
  if (copyButton) return copySpecialEntryValue(copyButton);
  if (e.target.closest('[data-open-defaults-catalog]')) {
    setActive('defaults');
    pushRoute({page: 'defaults'});
    return;
  }
  const sharedAsset = e.target.closest('[data-defaults-asset]');
  if (sharedAsset) return selectAsset(sharedAsset.dataset.defaultsAsset);
  if (state.readOnly) return;
  if (e.target.closest('[data-edit-special-entries]')) return openSpecialEntriesEditor();
}
async function copySpecialEntryValue(button) {
  clearError();
  try {
    await copyTextToClipboard(button.dataset.copyValue || '');
    const icon = button.querySelector('i');
    const originalTitle = button.getAttribute('title') || '';
    const originalIcon = icon?.className || '';
    button.setAttribute('title', 'Copied');
    if (icon) icon.className = 'ti ti-check';
    window.setTimeout(() => {
      button.setAttribute('title', originalTitle);
      if (icon) icon.className = originalIcon;
    }, 900);
  } catch (e) {
    showError(e);
  }
}
function isSpecial(path) { return ['env.secured.json','env.unsecured.json','assets.secured.json','assets.unsecured.json'].includes(path); }
function isSecuredSpecial(path) { return ['env.secured.json','assets.secured.json'].includes(path); }
function isDefaultsAsset(path) { return path === '_defaults.yml' || path === '_defaults.yaml'; }
function isSharedAssetsAsset(path) { return path === 'shared.assets.yml'; }
function isRootMetadataAsset(path) { return isSpecial(path) || isSharedAssetsAsset(path) || path === 'replica-profiles.yml'; }
function sharedAssetBrowserPath(path) { return String(path || '').replace(/^assets\//, ''); }
async function loadGitDiff(relativePath, isDirty, sectionSelector, targetSelector) {
  const section = qs(sectionSelector);
  const target = qs(targetSelector);
  if (!section || !target) return;
  if (!isDirty) {
    section.classList.add('hidden');
    target.textContent = '';
    return;
  }
  const encoded = relativePath.split('/').map(encodeURIComponent).join('/');
  const diff = await api(`/api/v1/git/diff/${encoded}`);
  section.classList.remove('hidden');
  target.innerHTML = renderDiff(diff.content || diff.error || 'No textual diff available.');
}
function renderDiff(content) {
  return String(content || '').split('\n').map((line) => {
    let cls = 'diff-line';
    if (line.startsWith('+++') || line.startsWith('---')) cls += ' diff-file';
    else if (line.startsWith('@@')) cls += ' diff-hunk';
    else if (line.startsWith('+')) cls += ' diff-add';
    else if (line.startsWith('-')) cls += ' diff-del';
    else if (line.startsWith('diff --git') || line.startsWith('index ')) cls += ' diff-meta';
    return `<span class="${cls}">${esc(line) || ' '}</span>`;
  }).join('');
}
async function runBuildValidate() {
  if (!state.env) return showError('Select environment first.');
  clearError();
  const env = state.env;
  try {
    state.buildChecks.validation = {state: 'running', at: new Date()};
    renderBuildWorkflow();
    setBuildStatus('info', 'Validation is running...');
    const result = await apiPost(`/api/v1/envs/${encodeURIComponent(env)}/validate`);
    if (state.env !== env) return;
    state.buildChecks.validation = {state: result.ok ? 'ok' : 'fail', at: new Date(), message: result.message || ''};
    renderBuildWorkflow();
    setBuildStatus(result.ok ? 'success' : 'danger', formatBuildError(result.message || (result.ok ? 'Validation OK' : 'Validation failed')), true);
  } catch (e) {
    if (state.env !== env) return;
    state.buildChecks.validation = {state: 'fail', at: new Date(), message: String(e)};
    renderBuildWorkflow();
    setBuildStatus('danger', formatBuildError(String(e)), true);
    showError(e);
  }
}
async function loadBuildSummary() {
  if (!state.env) return;
  clearError();
  const env = state.env;
  try {
    setBuildDataEnv(env, 'loading');
    const summary = await apiPost(`/api/v1/envs/${encodeURIComponent(env)}/summary`);
    if (state.env !== env) return;
    renderBuildSummary(summary);
    state.buildChecks.summary = {state: 'ok', at: new Date(), message: `${summary.items?.length || 0} row(s)`};
    renderBuildWorkflow();
    setBuildDataEnv(env, 'loaded');
  } catch (e) {
    if (state.env !== env) return;
    state.buildChecks.summary = {state: 'fail', at: new Date(), message: String(e)};
    renderBuildWorkflow();
    showError(e);
  }
}
async function loadBuildInventory() {
  if (!state.env) return showError('Select environment first.');
  clearError();
  const env = state.env;
  try {
    const inventory = await apiPost(`/api/v1/envs/${encodeURIComponent(env)}/inventory`);
    if (state.env !== env) return;
    state.inventory = inventory;
    renderInventory();
    state.buildChecks.inventory = {state: 'ok', at: new Date(), message: `${inventory.items?.length || 0} item(s)`};
    renderBuildWorkflow();
  } catch (e) {
    if (state.env !== env) return;
    state.inventory = null;
    renderInventory();
    state.buildChecks.inventory = {state: 'fail', at: new Date(), message: String(e)};
    renderBuildWorkflow();
    showError(e);
	}
}
async function loadBuildPreview() {
	if (!state.env) return showError('Select environment first.');
	clearError();
	const env = state.env;
	try {
		state.buildChecks.preview = {state: 'running', at: new Date()};
		renderBuildWorkflow();
		setBuildStatus('info', 'Build preview is rendering into a temporary directory...');
		const preview = await apiPost(`/api/v1/envs/${encodeURIComponent(env)}/preview`);
		if (state.env !== env) return;
		state.buildPreview = preview;
		state.buildPreviewPath = null;
		state.buildPreviewContent = null;
		state.buildPreviewOpenDirs = buildPreviewDefaultOpenDirs(preview.files || []);
		renderBuildPreview(preview);
		state.buildChecks.preview = {state: 'ok', at: new Date(), message: `${preview.totals?.files ?? 0} file(s)`};
		renderBuildWorkflow();
		setBuildStatus('success', `Build preview rendered ${preview.totals?.files ?? 0} file(s).`);
	} catch (e) {
		if (state.env !== env) return;
		state.buildPreview = null;
		state.buildPreviewPath = null;
		state.buildPreviewContent = null;
		renderBuildPreview(null);
		state.buildChecks.preview = {state: 'fail', at: new Date(), message: String(e)};
		renderBuildWorkflow();
		setBuildStatus('danger', formatBuildError(String(e)), true);
		showError(e);
	}
}
async function loadBuildData() {
  if (!state.env) return;
  clearError();
  try {
    await Promise.all([loadBuildSummary(), loadBuildInventory(), loadClusterStatus()]);
  } catch (e) { showError(e); }
}
async function loadClusterStatus(options = {}) {
  if (!state.clusterStatusEnabled || !state.env || state.clusterStatusLoading && !options.force) return;
  const env = state.env;
  state.clusterStatusLoading = true;
  renderClusterStatus(state.clusterStatus, true);
  try {
    const status = await api(`/api/v1/envs/${encodeURIComponent(env)}/cluster`);
    if (state.env !== env) return;
    state.clusterStatus = status;
    renderClusterStatus(status, false);
  } catch (e) {
    if (state.env !== env) return;
    state.clusterStatus = {enabled: true, error: String(e)};
    renderClusterStatus(state.clusterStatus, false);
  } finally {
    if (state.env === env) state.clusterStatusLoading = false;
  }
}
function setBuildStatus(kind, text, html = false) {
  const el = qs('#build-status');
  if (!el) return;
  el.className = `alert alert-${kind} mb-0`;
  if (html) el.innerHTML = text;
  else el.textContent = text;
}
function setBuildDataEnv(env, stateName) {
  const el = qs('#build-data-env');
  if (!el) return;
  if (!env) {
    el.className = 'badge bg-secondary-lt';
    el.innerHTML = '<i class="ti ti-stack-2 me-1"></i>not loaded';
    return;
  }
  el.className = `badge ${stateName === 'loading' ? 'bg-blue-lt' : 'bg-green-lt'}`;
  el.innerHTML = `<i class="ti ti-stack-2 me-1"></i>${esc(stateName || 'loaded')} for ${esc(env)}`;
}
function renderBuildWorkflow() {
  const host = qs('#build-workflow');
  if (!host) return;
  const env = state.env;
  if (!env) {
    host.innerHTML = buildStep('pending', 'Select environment', 'Build checks need an active environment.');
    return;
  }
  const dirtyCount = gitFilesForEnv(env).length;
  const checks = state.buildChecks || {};
  host.innerHTML = [
    buildStep(dirtyCount ? 'warn' : 'ok', dirtyCount ? 'Working tree has changes' : 'Working tree clean', dirtyCount ? `${dirtyCount} changed file(s) in ${env}. Validate before commit.` : 'No changed files detected for this environment.'),
    buildStep(checkState(checks.summary, checks.inventory), 'Summary and inventory', buildCheckText([checks.summary, checks.inventory], 'Use Refresh data to reload resource summary and inventory.')),
    buildStep(checkState(checks.validation), 'Validation', buildCheckText([checks.validation], 'Run Validate after edits.')),
    buildStep(checkState(checks.preview), 'Build preview', buildCheckText([checks.preview], 'Run Preview files to render generated manifests into a temporary directory.')),
  ].join('');
}
function buildStep(kind, title, text) {
  const icon = kind === 'ok' ? 'ti-circle-check' : kind === 'warn' ? 'ti-alert-triangle' : kind === 'fail' ? 'ti-circle-x' : 'ti-circle-dashed';
  return `<div class="build-step ${kind}"><i class="ti ${icon}"></i><div><div class="build-step-title">${esc(title)}</div><div class="build-step-text">${esc(text)}</div></div></div>`;
}
function checkState(...items) {
  const checks = items.filter(Boolean);
  if (!checks.length) return 'pending';
  if (checks.some((item) => item.state === 'fail')) return 'fail';
  if (checks.some((item) => item.state === 'running')) return 'warn';
  if (checks.every((item) => item.state === 'ok')) return 'ok';
  return 'pending';
}
function buildCheckText(items, fallback) {
  const checks = items.filter(Boolean);
  if (!checks.length) return fallback;
  return checks.map((item) => {
    const label = item.state === 'running' ? 'running' : item.state === 'ok' ? 'OK' : item.state === 'fail' ? 'failed' : item.state || 'pending';
    const suffix = item.message ? `: ${item.message}` : '';
    return `${label}${item.at ? ` at ${formatTime(item.at)}` : ''}${suffix}`;
  }).join(' / ');
}
function formatTime(value) {
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit', second: '2-digit'});
}
function renderClusterStatus(payload, loading = false) {
  const card = qs('#cluster-status-card');
  if (!card) return;
  card.classList.toggle('hidden', !state.clusterStatusEnabled);
  if (!state.clusterStatusEnabled) return;
  const body = qs('#cluster-status-body');
  const updated = qs('#cluster-status-updated');
  if (updated) {
    updated.className = `badge ${loading ? 'bg-blue-lt' : payload?.cluster?.available ? 'bg-green-lt' : payload?.error || payload?.cluster?.error ? 'bg-red-lt' : 'bg-secondary-lt'}`;
    updated.textContent = loading ? 'loading' : payload?.updated_at ? `updated ${formatTime(payload.updated_at)}` : 'not loaded';
  }
  if (!body) return;
  if (loading && !payload) {
    body.innerHTML = '<div class="text-muted">Loading runtime snapshot...</div>';
    return;
  }
  if (!payload) {
    body.innerHTML = '<div class="text-muted">Cluster status is not loaded.</div>';
    return;
  }
  if (payload.enabled === false) {
    body.innerHTML = '<div class="text-muted">Cluster status is disabled.</div>';
    return;
  }
  if (payload.error) {
    body.innerHTML = `<div class="alert alert-danger mb-0">${esc(payload.error)}</div>`;
    return;
  }
  const cluster = payload.cluster || {};
  if (!cluster.available) {
    body.innerHTML = `<div class="alert alert-warning mb-0">${esc(cluster.error || 'Cluster status is unavailable.')}</div>`;
    return;
  }
  body.innerHTML = `
    <div class="row g-2 mb-3">
      ${metricCard('Namespace', cluster.namespace || '-')}
      ${metricCard('Deployments', `${cluster.ready_deployments ?? 0}/${cluster.deployment_count ?? 0} ready`)}
      ${metricCard('Pods', `${cluster.ready_pods ?? 0}/${cluster.pod_count ?? 0} ready`)}
      ${metricCard('Services', cluster.service_count ?? 0)}
    </div>
    <div class="row g-3">
      ${runtimeList('Deployments', cluster.deployments || [], (item) => `
        <div class="runtime-row-main">${esc(item.name)}</div>
        <div class="runtime-row-meta"><span class="badge bg-green-lt">${esc(item.ready ?? 0)}/${esc(item.desired ?? 0)}</span><span>${esc(item.updated ?? 0)} updated</span></div>`)}
      ${runtimeList('Pods', cluster.pods || [], (item) => `
        <div class="runtime-row-main">${esc(item.name)}</div>
        <div class="runtime-row-meta"><span class="badge ${item.phase === 'Running' ? 'bg-green-lt' : 'bg-yellow-lt'}">${esc(item.phase || '?')}</span><span>${esc(item.ready || '-')}</span><span>${esc(item.restarts ?? 0)} restarts</span></div>`)}
      ${runtimeList('Services', cluster.services || [], (item) => `
        <div class="runtime-row-main">${esc(item.name)}</div>
        <div class="runtime-row-meta"><span class="badge bg-secondary-lt">${esc(item.type || '?')}</span><span>${esc(item.cluster_ip || '-')}</span><span>${esc(item.ports || '-')}</span></div>`)}
    </div>`;
}
function metricCard(label, value) {
  return `<div class="col-6 col-xl-3"><div class="metric-card"><div class="overview-subtitle">${esc(label)}</div><div class="fs-3 fw-semibold">${esc(value)}</div></div></div>`;
}
function runtimeList(title, items, renderItem) {
  const rows = items.length ? items.map((item) => `<div class="runtime-row">${renderItem(item)}</div>`).join('') : '<div class="text-muted small">No data.</div>';
  return `<div class="col-12 col-xl-4"><h4 class="mb-2">${esc(title)}</h4><div class="runtime-list">${rows}</div></div>`;
}
function formatBuildError(message) {
  const text = String(message || '');
  if (!text.includes('\n') && text.length < 180) return esc(text);
  const lines = text.split('\n').filter(Boolean);
  const headline = lines[0] || text.slice(0, 180);
  const details = lines.slice(1).join('\n') || text;
  return `<div class="fw-semibold">${esc(headline)}</div><div class="build-error-details">${esc(details)}</div>`;
}
function renderBuildSummary(summary) {
  const totals = summary.totals || {};
  setHTML('#build-totals', [
    metricCard('Apps', totals.apps ?? 0, 'ti-apps'),
    metricCard('Containers', totals.containers ?? 0, 'ti-box'),
    metricCard('Replicas', totals.replicas ?? 0, 'ti-copy'),
    metricCard('CPU req', `${fmt(totals.cpu_request_cores)} cores`, 'ti-cpu'),
    metricCard('Mem req', `${fmt(totals.memory_request_mib)} MiB`, 'ti-database'),
    metricCard('Mem lim', `${fmt(totals.memory_limit_mib)} MiB`, 'ti-gauge'),
  ].join(''));
  const items = summary.items || [];
  qs('#build-summary-table tbody').innerHTML = items.map((item) => `
    <tr>
      <td class="font-monospace">${esc(item.app)}</td>
      <td class="font-monospace">${esc(item.container)}</td>
      <td class="text-end">${item.replicas ?? 0}</td>
      <td class="text-end">${esc(item.cpu_request)}</td>
      <td class="text-end">${esc(item.cpu_limit)}</td>
      <td class="text-end">${esc(item.memory_request)}</td>
      <td class="text-end">${esc(item.memory_limit)}</td>
      <td class="text-end">${esc(item.java_xms)}</td>
      <td class="text-end">${esc(item.java_xmx)}</td>
    </tr>`).join('') || '<tr><td colspan="9" class="text-muted">No summary items.</td></tr>';
  qs('#build-summary-table tfoot').innerHTML = items.length ? `
    <tr class="fw-bold">
      <td colspan="2">Total</td>
      <td class="text-end">${totals.replicas ?? 0}</td>
      <td class="text-end">${fmt(totals.cpu_request_cores)} cores</td>
      <td class="text-end">${fmt(totals.cpu_limit_cores)} cores</td>
      <td class="text-end">${fmt(totals.memory_request_mib)} MiB</td>
      <td class="text-end">${fmt(totals.memory_limit_mib)} MiB</td>
      <td class="text-end">-</td>
      <td class="text-end">-</td>
	</tr>` : '';
}
function renderBuildPreview(preview) {
	const totals = preview?.totals || {};
	setHTML('#build-preview-totals', preview ? [
		metricCard('Files', totals.files ?? 0, 'ti-files'),
		metricCard('Deployments', totals.deployments ?? 0, 'ti-rocket'),
		metricCard('Services', totals.services ?? 0, 'ti-route'),
		metricCard('Assets', totals.assets ?? 0, 'ti-folders'),
	].join('') : '');
	setHTML('#build-preview-events', preview ? renderBuildEvents(preview.events || []) : '');
  const query = (qs('#build-preview-filter')?.value || '').trim().toLowerCase();
	const files = (preview?.files || []).filter((file) => !query || String(file.path || '').toLowerCase().includes(query));
  if (query) state.buildPreviewOpenDirs = buildPreviewDefaultOpenDirs(files);
	setHTML('#build-preview-tree', files.length ? renderBuildPreviewTree(files) : '<div class="text-muted p-3">No preview loaded.</div>');
	if (!preview || !state.buildPreviewPath) renderBuildPreviewContent(null);
}
function buildPreviewDefaultOpenDirs(files) {
  const dirs = new Set(['']);
  for (const file of files || []) {
    const parts = String(file.path || '').split('/').filter(Boolean);
    for (let i = 1; i < parts.length; i += 1) dirs.add(parts.slice(0, i).join('/'));
  }
  return dirs;
}
function renderBuildPreviewTree(files) {
  const root = {name: '', path: '', dirs: new Map(), files: []};
  for (const file of files) addBuildPreviewTreeFile(root, file);
  return `<div class="py-1">${renderBuildPreviewTreeChildren(root)}</div>`;
}
function addBuildPreviewTreeFile(root, file) {
  const parts = String(file.path || '').split('/').filter(Boolean);
  if (!parts.length) return;
  let node = root;
  for (const part of parts.slice(0, -1)) {
    if (!node.dirs.has(part)) {
      const path = node.path ? `${node.path}/${part}` : part;
      node.dirs.set(part, {name: part, path, dirs: new Map(), files: []});
    }
    node = node.dirs.get(part);
  }
  node.files.push({...file, name: parts[parts.length - 1]});
}
function renderBuildPreviewTreeChildren(node) {
  const dirs = Array.from(node.dirs.values()).sort((a, b) => a.name.localeCompare(b.name));
  const files = node.files.sort((a, b) => a.name.localeCompare(b.name));
  return [
    ...dirs.map((dir) => renderBuildPreviewDir(dir)),
    ...files.map((file) => renderBuildPreviewFile(file)),
  ].join('');
}
function renderBuildPreviewDir(dir) {
  const open = state.buildPreviewOpenDirs?.has(dir.path);
  return `
    <div class="build-preview-tree-dir">
      <div class="build-preview-tree-node" data-build-preview-dir="${esc(dir.path)}">
        <div class="build-preview-tree-main">
          <span class="build-preview-tree-icon text-muted"><i class="ti ${open ? 'ti-chevron-down' : 'ti-chevron-right'}"></i></span>
          <i class="ti ${open ? 'ti-folder-open' : 'ti-folder'} text-warning"></i>
          <span class="font-monospace small text-truncate" title="${esc(dir.path)}">${esc(dir.name)}</span>
        </div>
        <span class="build-preview-tree-size">${dir.files.length + dir.dirs.size}</span>
      </div>
      ${open ? `<div class="build-preview-tree-children">${renderBuildPreviewTreeChildren(dir)}</div>` : ''}
    </div>`;
}
function renderBuildPreviewFile(file) {
  const active = state.buildPreviewPath === file.path;
  return `
    <div class="build-preview-tree-node ${active ? 'active' : ''}" data-build-preview-path="${esc(file.path)}">
      <div class="build-preview-tree-main">
        <span class="build-preview-tree-icon text-muted"><i class="ti ti-minus"></i></span>
        <i class="ti ${fileIcon(file.name || file.path)}"></i>
        <span class="font-monospace small text-truncate" title="${esc(file.path)}">${esc(file.name || file.path)}</span>
      </div>
      <span class="build-preview-tree-size">${formatBytes(file.size_bytes ?? 0)}</span>
    </div>`;
}
function fileIcon(name) {
  const lower = String(name || '').toLowerCase();
  const ext = lower.split('.').pop() || '';
  if (lower === 'dockerfile') return 'ti-brand-docker text-blue';
  if (lower === 'makefile' || lower === 'gnumakefile') return 'ti-file-code text-muted';
  if (lower.startsWith('.env')) return 'ti-key text-green';
  const icons = {
    yaml: 'ti-settings text-blue',
    yml: 'ti-settings text-blue',
    json: 'ti-braces text-yellow',
    conf: 'ti-settings-2 text-muted',
    cfg: 'ti-settings-2 text-muted',
    ini: 'ti-settings-2 text-muted',
    env: 'ti-key text-green',
    sh: 'ti-terminal text-green',
    txt: 'ti-file-text text-muted',
    md: 'ti-markdown text-muted',
  };
  return icons[ext] || 'ti-file text-muted';
}
function renderBuildEvents(events) {
  if (!events.length) return '<div class="text-muted small">No render events reported.</div>';
  const counts = events.reduce((acc, event) => {
    const key = event.type || 'event';
    acc[key] = (acc[key] || 0) + 1;
    return acc;
  }, {});
  return `<div class="overview-subtitle">Render events</div><div class="build-event-list">${Object.entries(counts).sort().map(([type, count]) => `<span class="badge bg-secondary-lt"><i class="ti ti-activity me-1"></i>${esc(type)} ${count}</span>`).join('')}</div>`;
}
function expandBuildPreviewTree() {
  state.buildPreviewOpenDirs = buildPreviewDefaultOpenDirs(state.buildPreview?.files || []);
  renderBuildPreview(state.buildPreview);
}
function collapseBuildPreviewTree() {
  state.buildPreviewOpenDirs = new Set(['']);
  renderBuildPreview(state.buildPreview);
}
function handleBuildPreviewTreeClick(e) {
  const dir = e.target.closest('[data-build-preview-dir]');
  if (dir) {
    const path = dir.dataset.buildPreviewDir || '';
    if (state.buildPreviewOpenDirs.has(path)) state.buildPreviewOpenDirs.delete(path);
    else state.buildPreviewOpenDirs.add(path);
    renderBuildPreview(state.buildPreview);
    return;
  }
  const file = e.target.closest('[data-build-preview-path]');
  if (file) loadBuildPreviewContent(file.dataset.buildPreviewPath);
}
async function loadBuildPreviewContent(path) {
  if (!state.env || !state.buildPreview?.id || !path) return;
  clearError();
  state.buildPreviewPath = path;
  renderBuildPreview(state.buildPreview);
  renderBuildPreviewContent({path, content: 'Loading generated file...', size_bytes: 0});
  try {
    const encodedPath = path.split('/').map(encodeURIComponent).join('/');
    const content = await api(`/api/v1/envs/${encodeURIComponent(state.env)}/preview/${encodeURIComponent(state.buildPreview.id)}/content/${encodedPath}`);
    if (state.buildPreviewPath !== path) return;
    state.buildPreviewContent = content;
    renderBuildPreviewContent(content);
  } catch (e) {
    if (state.buildPreviewPath !== path) return;
    state.buildPreviewContent = null;
    renderBuildPreviewContent({path, content: String(e), size_bytes: 0});
    showError(e);
  }
}
function renderBuildPreviewContent(content) {
  if (!content) {
    setText('#build-preview-content-path', 'Select a generated file.');
    setText('#build-preview-content-size', '-');
    setHTML('#build-preview-content', '<div class="build-preview-message">No generated file selected.</div>');
    return;
  }
  setText('#build-preview-content-path', content.path || '-');
  setText('#build-preview-content-size', content.size_bytes ? formatBytes(content.size_bytes) : '-');
  if (content.binary) {
    setHTML('#build-preview-content', `<div class="build-preview-message">Binary file preview is not available.<br>Content-Type: ${esc(content.content_type || 'unknown')}</div>`);
    return;
  }
  const suffix = content.truncated ? '\n\n--- truncated after 1 MiB ---' : '';
  setHTML('#build-preview-content', renderCodeLines(`${content.content || ''}${suffix}`));
}
function renderCodeLines(content) {
  const lines = String(content || '').split('\n');
  return `<table class="build-preview-code-table"><tbody>${lines.map((line, index) => `<tr><td class="build-preview-lineno">${index + 1}</td><td class="build-preview-line">${esc(line) || ' '}</td></tr>`).join('')}</tbody></table>`;
}
async function copyBuildPreviewPath() {
  if (!state.buildPreviewPath) return;
  await copyTextToClipboard(state.buildPreviewPath);
}
async function copyBuildPreviewContent() {
  if (!state.buildPreviewContent || state.buildPreviewContent.binary) return;
  await copyTextToClipboard(state.buildPreviewContent.content || '');
}
function renderInventory() {
  const table = qs('#build-inventory-table tbody');
  if (!table) return;
  const items = state.inventory?.items || [];
  const query = (qs('#inventory-filter')?.value || '').trim().toLowerCase();
  const dotPath = (qs('#inventory-path')?.value || '').trim();
  const filtered = items.filter((item) => {
    if (!query) return true;
    return [item.app, item.container, item.image, item.app_kind].some((value) => String(value || '').toLowerCase().includes(query));
  });
  table.innerHTML = filtered.map((item) => `
    <tr>
      <td class="font-monospace">${esc(item.app)}</td>
      <td class="font-monospace">${esc(item.container)}</td>
      <td class="font-monospace text-break">${esc(item.image)}</td>
      <td class="text-end">${item.replicas ?? '-'}</td>
      <td class="font-monospace text-break">${esc(dotPath ? valueAtPath(item, dotPath) : 'resources, mtls, rollout_checksums')}</td>
    </tr>`).join('') || '<tr><td colspan="5" class="text-muted">No inventory rows.</td></tr>';
}
function valueAtPath(value, path) {
  let current = value;
  for (const part of path.split('.').filter(Boolean)) {
    if (current == null || typeof current !== 'object' || !(part in current)) return '';
    current = current[part];
  }
  if (current == null) return '';
  if (typeof current === 'object') return JSON.stringify(current);
  return String(current);
}
function metricCard(label, value, icon) {
  return `<div class="col-6 col-lg-4"><div class="metric-card"><div class="text-muted small"><i class="ti ${icon} me-1"></i>${esc(label)}</div><div class="h3 mb-0">${esc(value)}</div></div></div>`;
}
function fmt(value) {
  const n = Number(value || 0);
  return Number.isInteger(n) ? String(n) : n.toFixed(2);
}
document.addEventListener('DOMContentLoaded', init);
