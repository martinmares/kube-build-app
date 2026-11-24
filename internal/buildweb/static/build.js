'use strict';
const byId = id => document.getElementById(id);
let catalog = { releases: [] }, activeRun = null, pollTimer = null, displayedRun = '', selectedFile = '', navigation = 0, fileNavigation = 0;
let deploymentPackageReview = null;
let publicationReview = null, remoteRelease = null, urlLoading = false, sourceStatusTimer = null;
const text = (tag, value, className) => { const el = document.createElement(tag); el.textContent = value; if (className) el.className = className; return el; };
async function request(path, options) {
  const response = await fetch(path, options);
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || `Request failed (${response.status})`);
  return data;
}
function showError(message) { byId('error').textContent = message || ''; byId('error').hidden = !message; }
function releases() {
  const previous = byId('release').value, search = byId('release-filter').value.toLowerCase();
  byId('release').replaceChildren();
  for (const release of catalog.releases || []) {
    if (!`${release.id} ${release.path}`.toLowerCase().includes(search)) continue;
    const option = text('option', `${release.id} · ${release.path}`); option.value = release.path;
    byId('release').append(option);
  }
  if ([...byId('release').options].some(option => option.value === previous)) byId('release').value = previous;
  else byId('release').selectedIndex = 0;
  releaseDetail();
}
function selectedRelease() {
  return byId('release-source').value !== 'local' ? remoteRelease?.release : (catalog.releases || []).find(item => item.path === byId('release').value);
}
function sourceLabel(origin) {
  return origin.commit ? `${origin.url || origin.repository} · ${origin.tag} @ ${origin.commit} · ${origin.path}` : origin.url;
}
function releaseDetail() {
  const release = selectedRelease(), origin = remoteRelease?.git_origin || remoteRelease?.origin;
  if (byId('release-source').value !== 'local') {
    byId('release-detail').textContent = remoteRelease ? `${release.id} · ${release.images} images · ${origin.expected_sha256 ? 'Expected SHA256 matched' : 'SHA256 computed (no expected value)'} · ${origin.sha256}` : 'Read a release manifest and review it before building.';
  } else byId('release-detail').textContent = release ? `${release.images} images · ${release.path}` : 'No matching image release. Add a manifest under releases/ or distributions/<registry>/.';
  byId('build-button').disabled = !release || !byId('environment').value || activeRun?.status === 'running' || urlLoading;
}
function invalidateURL() {
  remoteRelease = null; byId('url-preview').hidden = true; byId('url-content').textContent = ''; byId('empty').hidden = Boolean(activeRun); releaseDetail();
}
function sourceFields() {
  const source = byId('release-source').value;
  for (const [id, mode] of [['local-release-fields', 'local'], ['url-release-fields', 'url'], ['configured-release-fields', 'configured']]) {
    byId(id).hidden = source !== mode; byId(id).disabled = source !== mode;
  }
  invalidateURL();
}
byId('release-source').addEventListener('change', sourceFields);
for (const id of ['manifest-url', 'url-sha256', 'url-token', 'url-username', 'url-password', 'configured-release-id', 'configured-sha256']) byId(id).addEventListener('input', invalidateURL);
byId('url-auth').addEventListener('change', () => {
  const auth = byId('url-auth').value;
  byId('url-token-field').hidden = auth !== 'bearer'; byId('url-basic-fields').hidden = auth !== 'basic';
  byId('url-token').value = ''; byId('url-password').value = ''; invalidateURL();
});
async function readRemoteRelease(mode) {
  invalidateURL(); showError(''); urlLoading = true;
  const button = byId(mode === 'url' ? 'read-url' : 'read-configured');
  button.disabled = true; button.textContent = 'Reading manifest…'; releaseDetail();
  let input;
  if (mode === 'configured') {
    input = { release_id: byId('configured-release-id').value.trim(), environment: byId('environment').value, sha256: byId('configured-sha256').value.trim() };
    for (const name of catalog.artifact_source?.variables || []) input[name] = byId(`configured-${name}`).value.trim();
  } else {
    const auth = byId('url-auth').value;
    input = { url: byId('manifest-url').value.trim(), sha256: byId('url-sha256').value.trim(), auth };
    if (auth === 'bearer') input.token = byId('url-token').value;
    if (auth === 'basic') { input.username = byId('url-username').value; input.password = byId('url-password').value; }
  }
  const body = JSON.stringify(input); delete input.token; delete input.password;
  byId('url-token').value = ''; byId('url-password').value = '';
  byId('url-release-fields').disabled = true; byId('configured-release-fields').disabled = true; byId('release-source').disabled = true; byId('environment').disabled = true;
  try {
    remoteRelease = await request(`/api/releases/${mode}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body });
    byId('url-content').textContent = remoteRelease.content; byId('url-preview-title').textContent = `${remoteRelease.release.id} · ${remoteRelease.release.images} images`; const origin = remoteRelease.git_origin || remoteRelease.origin; byId('url-preview-source').textContent = `${sourceLabel(origin)} · SHA256 ${origin.sha256} · ${origin.expected_sha256 ? 'Expected checksum matched' : 'Computed checksum; no expected value supplied'}`; byId('url-preview').hidden = false; byId('empty').hidden = true;
  } catch (error) { showError(error.message); }
  finally {
    urlLoading = false; button.disabled = false; button.textContent = 'Read and review manifest';
    byId('url-release-fields').disabled = byId('release-source').value !== 'url'; byId('configured-release-fields').disabled = byId('release-source').value !== 'configured';
    byId('release-source').disabled = false; byId('environment').disabled = false; releaseDetail();
  }
}
byId('read-url').addEventListener('click', () => readRemoteRelease('url'));
byId('read-configured').addEventListener('click', () => readRemoteRelease('configured'));
window.addEventListener('pageshow', () => { byId('url-token').value = ''; byId('url-password').value = ''; });
function refreshStatus(status) {
  const element = byId('source-refresh-status');
  element.hidden = !status;
  if (!status) return;
  const successful = status.succeeded_at && !status.succeeded_at.startsWith('0001-');
  element.textContent = `${status.error || 'Git refresh status'} · ${successful ? `Last successful refresh: ${new Date(status.succeeded_at).toLocaleString()}` : 'No completed refresh yet'}`;
  element.className = `small text-break ${status.error ? 'text-danger' : 'text-secondary'}`;
}
async function loadCatalog() {
  catalog = await request('/api/catalog');
  const sourceSelect = byId('release-source');
  if (catalog.artifact_source && !sourceSelect.querySelector('option[value="configured"]')) {
    const option = text('option', catalog.artifact_source.storage === 'git' ? 'Configured Git' : 'Configured Artifactory'); option.value = 'configured'; byId('release-source').append(option);
    byId('configured-checksum-field').hidden = catalog.artifact_source.fixed_sha256 || !catalog.artifact_source.checksum_required;
    for (const name of catalog.artifact_source.variables || []) {
      const label = text('label', name === 'revision' ? 'Revision' : 'Distribution', 'form-label mb-0');
      const input = document.createElement('input'); input.id = `configured-${name}`; input.className = 'form-control mt-1'; input.autocomplete = 'off'; input.addEventListener('input', invalidateURL);
      label.append(input); byId('configured-variables').append(label);
    }
  }
  const localOption = sourceSelect.querySelector('option[value="local"]');
  if (!catalog.local_releases_enabled && localOption) {
    const wasLocal = sourceSelect.value === 'local';
    localOption.remove();
    if (wasLocal) {
      sourceSelect.value = catalog.artifact_source ? 'configured' : 'url';
      sourceFields();
    }
  }
  refreshStatus(catalog.artifact_source?.refresh);
  if (catalog.artifact_source?.managed && !sourceStatusTimer) {
    const poll = async () => {
      try { refreshStatus(await request('/api/releases/source-status')); } catch { byId('source-refresh-status').textContent = 'Git refresh status is unavailable.'; }
      sourceStatusTimer = setTimeout(poll,5000);
    };
    sourceStatusTimer = setTimeout(poll,5000);
  }
  const previous = byId('environment').value;
  byId('environment').replaceChildren(...(catalog.environments || []).map(env => { const option = text('option', env); option.value = env; return option; }));
  if ((catalog.environments || []).includes(previous)) byId('environment').value = previous;
  byId('server-context').textContent = JSON.stringify(catalog.context, null, 2); byId('root').textContent = catalog.root; byId('policy').textContent = catalog.resource_policy_root || 'Environment defaults'; byId('version').textContent = catalog.version || '';
  byId('warnings').textContent = (catalog.warnings || []).join('\n'); byId('warnings').hidden = !(catalog.warnings || []).length;
  releases();
  byId('history').replaceChildren();
  for (const run of catalog.history || []) {
    const button = text('button', `${run.release_id} · ${run.selection.environment}`, 'history-item'); button.type = 'button';
    button.append(text('small', `${({ running: 'Building…', succeeded: 'Complete', failed: 'Failed', interrupted: 'Interrupted' })[run.status] || run.status} · ${new Date(run.created_at).toLocaleString()}`));
    button.addEventListener('click', () => loadRun(run.id).catch(error => showError(error.message)));
    byId('history').append(button);
  }
  if (!(catalog.history || []).length) byId('history').append(text('p', 'No builds yet.', 'text-secondary mb-0'));
}
function metric(label, value) { const box = text('div', '', 'total'); box.append(text('span', label), text('strong', value)); return box; }
function provenance(data) {
  byId('evidence').hidden = !data;
  if (!data) return;
  const files = data.files || [], repos = data.repositories || [], changes = files.filter(file => ['modified', 'deleted', 'untracked'].includes(file.local_state));
  const capturedGit = new Set(files.filter(file => file.git_origin).map(file => `${file.git_origin.url || file.git_origin.repository}@${file.git_origin.commit}`)).size;
  byId('evidence-summary').textContent = `${files.filter(file => !file.missing).length} captured file inputs · ${repos.length} working Git repositories${capturedGit ? ` · ${capturedGit} captured Git sources` : ''} · ${changes.length} local changes`;
  byId('evidence-changed').hidden = !repos.some(repo => repo.changed_during_build);
  byId('evidence-repositories').replaceChildren(...repos.map(repo => {
    const item = document.createElement('li'), path = text('small', repo.root, 'd-block text-secondary text-truncate');
    path.title = repo.root;
    item.append(text('strong', repo.root.split('/').pop()), text('span', ` · ${repo.commit || 'No commit (working files only)'}`, 'text-break'), path);
    return item;
  }));
  byId('evidence-files').replaceChildren(...files.map(file => {
    const row = document.createElement('tr'), name = text('td', (file.git_origin ? sourceLabel(file.git_origin) : file.url_origin?.url) || file.git_path || file.path, 'text-break');
    name.title = file.path;
    if (file.git_root) name.append(text('small', file.git_root.split('/').pop(), 'd-block text-secondary'));
    const state = file.local_state || (file.missing ? 'absent' : 'outside Git');
    const stateCell = text('td', ({ 'matches-commit': 'Matches commit', modified: 'Modified', deleted: 'Deleted', untracked: 'Untracked', absent: 'Absent' })[state] || state, changes.includes(file) ? 'text-warning' : 'text-secondary');
    const hash = text('td', file.sha256 ? file.sha256.replace('sha256:', '').slice(0, 12) : '—'); hash.title = file.sha256 || 'Optional input was absent';
    row.append(name, stateCell, hash); return row;
  }));
  byId('evidence-builder').textContent = data.executable_sha256;
}
function summary(data) {
  byId('summary').hidden = !data; if (!data) return;
  const totals = data.totals, n = value => Number(value).toLocaleString(undefined, { maximumFractionDigits: 2 });
  byId('totals').replaceChildren(metric('Applications', totals.apps), metric('Replicas', totals.replicas), metric('CPU req / limit', `${n(totals.cpu_request_cores)} / ${n(totals.cpu_limit_cores)}`), metric('Memory req / limit', `${n(totals.memory_request_mib)} / ${n(totals.memory_limit_mib)} MiB`));
  byId('resource-rows').replaceChildren();
  for (const item of data.items || []) {
    const row = document.createElement('tr');
    const name = text('td', item.app); name.append(text('small', item.container, 'd-block text-secondary'));
    row.append(name, text('td', item.replicas), text('td', `${item.cpu_request} / ${item.cpu_limit}`), text('td', `${item.memory_request} / ${item.memory_limit}`)); byId('resource-rows').append(row);
  }
}
function fileTree() {
  const root = Object.create(null), search = byId('file-filter').value.toLowerCase();
  for (const path of activeRun?.files || []) {
    if (!path.toLowerCase().includes(search)) continue;
    let node = root; const parts = path.split('/');
    parts.forEach((part, index) => { if (index === parts.length - 1) node[part] = path; else node = node[part] ||= Object.create(null); });
  }
  function render(node) {
    const list = document.createElement('ul');
    for (const [name, value] of Object.entries(node)) {
      const item = document.createElement('li');
      if (typeof value === 'string') {
        const button = text('button', name, value === selectedFile ? 'active' : ''); button.type = 'button';
        button.addEventListener('click', () => showFile(value).catch(error => showError(error.message))); item.append(button);
      } else { const folder = document.createElement('details'); folder.open = true; folder.append(text('summary', name), render(value)); item.append(folder); }
      list.append(item);
    }
    return list;
  }
  byId('files').replaceChildren(render(root));
}
async function showFile(path) {
  const runId = activeRun.id, generation = ++fileNavigation;
  selectedFile = path; byId('file-name').textContent = path; byId('file-content').textContent = 'Loading…'; byId('copy-file').disabled = true; fileTree();
  const result = await request(`/api/builds/${runId}/file/${path.split('/').map(encodeURIComponent).join('/')}`);
  if (activeRun.id !== runId || generation !== fileNavigation) return;
  selectedFile = path; byId('file-name').textContent = result.path; byId('file-content').textContent = result.content; byId('copy-file').disabled = false; fileTree();
}
async function loadRun(id, generation = ++navigation) {
  clearTimeout(pollTimer);
  byId('url-preview').hidden = true;
  const run = await request(`/api/builds/${id}`); if (generation !== navigation) return; activeRun = run;
  byId('empty').hidden = true; byId('result').hidden = false;
  byId('run-title').textContent = `${run.release_id} · ${run.selection.environment}`;
  const statusLabel = { running: 'Building…', succeeded: 'Build complete', failed: 'Build failed', interrupted: 'Interrupted' };
  byId('status').textContent = statusLabel[run.status] || run.status; byId('status').className = `badge ${run.status === 'succeeded' ? 'bg-green-lt text-green' : run.status === 'running' ? 'bg-blue-lt text-blue' : 'bg-red-lt text-red'}`;
  byId('spinner').hidden = run.status !== 'running'; byId('run-time').textContent = new Date(run.created_at).toLocaleString();
  byId('run-inputs').textContent = JSON.stringify({ builder: run.builder, environment_root: run.environment_root, releases_root: run.releases_root, selection: run.selection, context: run.context }, null, 2);
  byId('manifest-path').textContent = (run.release_git_source ? sourceLabel(run.release_git_source) : run.release_source?.url) || run.selection.manifest;
  byId('run-error').textContent = run.error || ''; byId('run-error').hidden = !run.error;
  byId('log').textContent = (run.events || []).map(event => [event.type, event.app, event.container, event.name, event.path].filter(Boolean).join(' · ')).join('\n') || (run.status === 'running' ? 'Loading and validating build inputs…' : 'No render events.');
  byId('progress-details').open = run.status !== 'succeeded';
  summary(run.summary);
  provenance(run.evidence);
  byId('artifacts').hidden = run.status !== 'succeeded'; byId('download').hidden = run.status !== 'succeeded'; byId('download').href = `/api/builds/${id}/archive`;
  byId('package').hidden = run.status !== 'succeeded';
  byId('publication').hidden = run.status !== 'succeeded';
  byId('publication-target').textContent = catalog.deployments_root ? `Repository: ${catalog.deployments_root} · deploy/${run.selection.environment}` : 'Git publication is not configured. Start the server with --deployments-root pointing to a dedicated deployment checkout.';
  byId('review-publication').disabled = !catalog.deployments_root;
  if (displayedRun !== id) {
    publicationReview = null; deploymentPackageReview = null;
    for (const element of ['publication-review', 'publication-result', 'publication-error', 'package-review', 'package-result', 'package-error']) byId(element).hidden = true;
  }
  if (displayedRun !== id) { displayedRun = id; selectedFile = ''; byId('file-filter').value = ''; byId('file-content').textContent = ''; byId('file-name').textContent = 'Select a file'; byId('copy-file').disabled = true; }
  fileTree(); releaseDetail();
  if (run.status === 'running') pollTimer = setTimeout(() => loadRun(id, generation).catch(error => showError(error.message)), 700);
  else {
    await loadCatalog();
    if (generation !== navigation) return;
    if (!selectedFile && run.files?.length) await showFile(run.files.find(path => path.startsWith('deployments/')) || run.files[0]);
  }
}
byId('build-form').addEventListener('submit', async event => {
  event.preventDefault(); showError(''); byId('build-button').disabled = true;
  try {
    const release = selectedRelease();
    if (!release) throw new Error('Choose and review a release first.');
    const chosen = { environment: byId('environment').value, manifest_sha256: release.sha256 };
    if (byId('release-source').value !== 'local') chosen.remote_release = remoteRelease.token; else chosen.manifest = release.path;
    const run = await request('/api/builds', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(chosen) });
    await loadRun(run.id);
  } catch (error) { showError(error.message); releaseDetail(); }
});
byId('release-filter').addEventListener('input', releases); byId('release').addEventListener('change', releaseDetail); byId('environment').addEventListener('change', () => { if (byId('release-source').value === 'configured') invalidateURL(); else releaseDetail(); }); byId('file-filter').addEventListener('input', fileTree);
byId('copy-file').addEventListener('click', async () => { try { await navigator.clipboard.writeText(byId('file-content').textContent); byId('copy-file').textContent = 'Copied'; setTimeout(() => { byId('copy-file').textContent = 'Copy'; }, 1500); } catch (error) { showError(error.message); } });
function publicationError(error) { byId('publication-error').textContent = error.message; byId('publication-error').hidden = false; }
byId('review-publication').addEventListener('click', async () => {
  const runId = activeRun.id;
  byId('review-publication').disabled = true; byId('publication-error').hidden = true; byId('publication-result').hidden = true; byId('publication-review').hidden = true; publicationReview = null;
  try {
    const review = await request(`/api/builds/${runId}/publication-review`, { method: 'POST' });
    if (activeRun.id !== runId) return;
    publicationReview = review;
    byId('publication-description').textContent = `Branch ${review.branch} · tag ${review.tag} · base ${review.base.slice(0, 12)}`;
    byId('publication-summary').textContent = review.summary;
    byId('publication-diff').textContent = review.diff; byId('publication-review').hidden = false; byId('confirm-publication').disabled = false;
  } catch (error) { if (activeRun.id === runId) publicationError(error); }
  finally { if (activeRun.id === runId) byId('review-publication').disabled = false; }
});
byId('confirm-publication').addEventListener('click', async () => {
  const review = publicationReview;
  if (!review) return;
  byId('confirm-publication').disabled = true; byId('review-publication').disabled = true; byId('publication-error').hidden = true;
  try {
    const result = await request(`/api/builds/${review.run_id}/publish/${review.id}`, { method: 'POST' });
    if (activeRun.id !== review.run_id) return;
    byId('publication-result').textContent = `Published locally: ${result.tag} · ${result.commit}. Ready for your Git push and deployment process.`;
    byId('publication-result').hidden = false; byId('publication-review').hidden = true;
  } catch (error) { if (activeRun.id === review.run_id) { publicationError(error); byId('confirm-publication').disabled = false; } }
  finally { if (activeRun.id === review.run_id) byId('review-publication').disabled = false; }
});
function packageError(error) { byId('package-error').textContent = error.message; byId('package-error').hidden = false; }
byId('prepare-package').addEventListener('click', async () => {
  const runId = activeRun.id;
  deploymentPackageReview = null;
  byId('prepare-package').disabled = true; byId('prepare-package').textContent = 'Preparing package…';
  for (const id of ['package-review', 'package-error', 'package-result']) byId(id).hidden = true;
  try {
    const review = await request(`/api/builds/${runId}/package-review`, { method: 'POST' });
    if (activeRun.id !== runId) return;
    deploymentPackageReview = review;
    byId('package-summary').textContent = `${review.release_id} · ${review.environment} · ${review.files.length} manifests · ${Number(review.bytes).toLocaleString()} ZIP bytes`;
    byId('package-checksum').textContent = `ZIP SHA256: ${review.sha256}`;
    byId('package-files').replaceChildren(...review.files.map(file => {
      const row = document.createElement('tr'), hash = text('td', file.sha256.replace('sha256:', '').slice(0, 12)); hash.title = file.sha256;
      row.append(text('td', file.path, 'text-break'), text('td', Number(file.bytes).toLocaleString()), hash); return row;
    }));
    byId('package-destination').textContent = review.url ? `Publish to: ${review.url}` : review.directory ? `Save to: ${review.directory}/${review.path}. No automatic Git commit or push.` : 'Package publication is not configured. The package is available for download.';
    byId('download-package').href = `/api/builds/${runId}/packages/${review.id}/archive`;
    byId('save-package').textContent = review.url ? 'Publish package to Artifactory' : 'Save package to directory';
    byId('save-package').disabled = !(review.directory || review.url); byId('package-review').hidden = false;
  } catch (error) { if (activeRun.id === runId) packageError(error); }
  finally { byId('prepare-package').disabled = false; byId('prepare-package').textContent = 'Prepare package'; }
});
byId('save-package').addEventListener('click', async () => {
  const review = deploymentPackageReview;
  if (!review?.directory && !review?.url) return;
  byId('save-package').disabled = true; byId('prepare-package').disabled = true; byId('package-error').hidden = true;
  try {
    const result = await request(`/api/builds/${review.run_id}/packages/${review.id}/publish`, { method: 'POST' });
    if (activeRun.id !== review.run_id) return;
    byId('package-result').textContent = `${result.url ? 'Published: ' + result.url : 'Saved: ' + result.directory + '/' + result.path} · SHA256 ${result.sha256}.`;
    byId('package-result').hidden = false;
  } catch (error) { if (activeRun.id === review.run_id) packageError(error); }
  finally { byId('save-package').disabled = !(deploymentPackageReview?.directory || deploymentPackageReview?.url); byId('prepare-package').disabled = false; }
});
byId('theme').value = localStorage.getItem('theme') || 'auto'; byId('theme').addEventListener('change', () => { const theme = byId('theme').value; localStorage.setItem('theme', theme); document.documentElement.dataset.bsTheme = theme === 'auto' ? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : theme; });
loadCatalog().then(() => { const running = (catalog.history || []).find(run => run.status === 'running'); if (running) return loadRun(running.id); }).catch(error => showError(error.message));
