// 画面の動き全般。フレームワークは使わず素のJSで書いている
'use strict';

const TOKEN = new URLSearchParams(location.search).get('t') || '';
const $ = (s, el) => (el || document).querySelector(s);
const $$ = (s, el) => Array.from((el || document).querySelectorAll(s));
const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

const S = {
  settings: null, profiles: [], profile: null, cmdSets: [], detected: [],
  job: null, pre: {}, showPass: false, search: '', curSet: '', setDirty: false,
  ttpmacroOk: false, appName: '', dataDir: '', debugDir: '',
};

// 貼り付けやEnter移動で使う、編集できる列の並び
const EDIT_COLS = ['group', 'name', 'proto', 'ip', 'cmdSet', 'user', 'pass', 'enable', 'devType'];
const DT_LABEL = { cisco: 'Cisco IOS', fortigate: 'FortiGate', yamaha: 'YAMAHA RTX', other: '汎用' };
const DT_ORDER = ['cisco', 'fortigate', 'yamaha', 'other'];

async function api(path, body) {
  const r = await fetch('/api/' + path, {
    method: 'POST',
    headers: { 'X-Token': TOKEN, 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {}),
  });
  if (!r.ok) throw new Error('アプリ本体と通信できません（' + r.status + '）');
  const j = await r.json();
  if (!j.ok) throw new Error(j.error || '不明なエラー');
  return j.data;
}

// ---------- 小物 ----------
function toast(msg, kind, ms) {
  const t = document.createElement('div');
  t.className = 'toast' + (kind ? ' ' + kind : '');
  t.textContent = msg;
  $('#toasts').appendChild(t);
  setTimeout(() => t.remove(), ms || (kind === 'ng' ? 7000 : 3500));
}

function modal({ title, body, size, buttons, onOpen, sticky }) {
  const ov = document.createElement('div');
  ov.className = 'overlay';
  ov.innerHTML = `<div class="modal ${size || ''}" role="dialog" aria-modal="true">
    <div class="modal-head"><span>${esc(title)}</span></div>
    <div class="modal-body"></div><div class="modal-foot"></div></div>`;
  const bodyEl = $('.modal-body', ov), foot = $('.modal-foot', ov);
  if (typeof body === 'string') bodyEl.innerHTML = body; else bodyEl.appendChild(body);
  const close = () => { ov.remove(); document.removeEventListener('keydown', onKey); };
  // sticky のときは Esc で閉じない（同意画面など、必ずボタンで答えてもらうもの）
  const onKey = e => { if (e.key === 'Escape' && !sticky && $('#modals').lastElementChild === ov) { e.stopImmediatePropagation(); close(); } };
  (buttons || [{ label: '閉じる' }]).forEach(b => {
    const el = document.createElement('button');
    el.className = 'btn ' + (b.cls || '');
    el.textContent = b.label;
    el.onclick = async () => {
      if (b.onClick) {
        try { if (await b.onClick(ov) === false) return; }
        catch (e) { toast(e.message, 'ng'); return; }
      }
      close();
    };
    foot.appendChild(el);
  });
  document.addEventListener('keydown', onKey);
  $('#modals').appendChild(ov);
  if (onOpen) onOpen(ov);
  const first = $('input,select,textarea', bodyEl) || $('button', foot);
  if (first) first.focus();
  return { el: ov, close };
}

function askText(title, label, initial, okLabel) {
  return new Promise(res => {
    let done = false;
    const m = modal({
      title,
      body: `<label class="fld v"><span>${esc(label)}</span><input type="text" id="askIn" value="${esc(initial || '')}"></label>`,
      buttons: [
        { label: 'キャンセル', onClick: () => { done = true; res(null); } },
        { label: okLabel || 'OK', cls: 'primary', onClick: ov => { const v = $('#askIn', ov).value.trim(); if (!v) return false; done = true; res(v); } },
      ],
      onOpen: ov => {
        const i = $('#askIn', ov); i.select();
        i.addEventListener('keydown', e => { if (e.key === 'Enter') $$('.modal-foot .btn', ov)[1].click(); });
      },
    });
    // Escで閉じた場合もキャンセル扱いにする
    const ob = new MutationObserver(() => { if (!document.body.contains(m.el)) { ob.disconnect(); if (!done) res(null); } });
    ob.observe($('#modals'), { childList: true });
  });
}

function askConfirm(title, msg, okLabel, danger) {
  return new Promise(res => {
    let done = false;
    const m = modal({
      title, body: `<div>${msg}</div>`,
      buttons: [
        { label: 'キャンセル', onClick: () => { done = true; res(false); } },
        { label: okLabel || 'OK', cls: danger ? 'danger' : 'primary', onClick: () => { done = true; res(true); } },
      ],
    });
    const ob = new MutationObserver(() => { if (!document.body.contains(m.el)) { ob.disconnect(); if (!done) res(false); } });
    ob.observe($('#modals'), { childList: true });
  });
}

const newId = () => 'h' + Date.now().toString(36) + Math.random().toString(36).slice(2, 7);
const newHost = o => Object.assign({
  id: newId(), on: true, group: '', name: '', proto: 'telnet', ip: '', port: 0, cmdSet: '', user: '', pass: '', enable: '', loginTimeout: 0, cmdTimeout: 0,
  devType: 'cisco', promptUsername: '', promptPassword: '', promptUser: '', promptEnable: '', enableCmd: null, preCmds: null, clockCmd: null, separator: null,
}, o || {});
// 個別設定（機器種別の既定値の上書き）が入っているか
const hasOverride = h => !!(h.port || h.loginTimeout || h.cmdTimeout || h.promptUsername || h.promptPassword || h.promptUser || h.promptEnable ||
  h.enableCmd != null || h.preCmds != null || h.clockCmd != null || h.separator != null);
const running = () => !!(S.job && S.job.running);

// ---------- 保存 ----------
let saveTimer = null, saving = Promise.resolve();
function touch() {
  $('#saveState').textContent = '保存中…';
  clearTimeout(saveTimer);
  saveTimer = setTimeout(flushSave, 500);
}
function flushSave() {
  clearTimeout(saveTimer); saveTimer = null;
  const snap = JSON.parse(JSON.stringify(S.profile));
  saving = saving.then(() => api('profile/save', snap))
    .then(() => { if (!saveTimer) $('#saveState').textContent = '保存済み'; })
    .catch(e => { $('#saveState').textContent = '保存に失敗'; toast('保存できませんでした: ' + e.message, 'ng'); });
  return saving;
}

// ---------- 起動 ----------
async function init() {
  const d = await api('state');
  Object.assign(S, { settings: d.settings, profiles: d.profiles, profile: d.profile, cmdSets: d.cmdSets, detected: d.detected || [], ttpmacroOk: d.ttpmacroOk, appName: d.appName, dataDir: d.dataDir, debugDir: d.debugDir, version: d.version, info: d });
  document.title = S.appName;
  // パスワード欄は type=password だとブラウザが「保存しますか」と聞いてくるので、テキスト欄を伏せ字表示にしている
  $$('.secret').forEach(i => { i.type = 'text'; i.autocomplete = 'off'; });
  S.curSet = S.profile.common.cmdSet || (S.cmdSets[0] && S.cmdSets[0].name) || '';
  if (!S.cmdSets.find(c => c.name === S.curSet) && S.cmdSets[0]) S.curSet = S.cmdSets[0].name;
  bindStatic();
  renderAll();
  connectEvents();
  $('#saveState').textContent = '';
  if (!S.ttpmacroOk) {
    toast('Tera Term（ttpmacro.exe）の場所が未設定です。詳細設定で指定してください。', 'ng', 9000);
  }
  noticeLogReset(d.logDirReset);
  // 初回（または規約の改定後）は利用規約への同意を求め、そのあと更新を確認する
  if (S.settings.termsAccepted !== d.termsVersion) await askTerms();
  if (S.settings.updateCheck) checkUpdate(false);
}

// 保存先フォルダが見つからず、アプリと同じ場所のLogに付け替えたときのお知らせ
function noticeLogReset(old) {
  if (old) toast(`ログ保存先「${old}」が見つからないため、アプリと同じ場所の Log フォルダに変更しました。`, '', 9000);
}

function renderAll() {
  renderProfiles(); renderTTChip(); renderSide(); renderSets(); renderGroups(); renderRows(); renderBar(); applyMask();
}

function renderProfiles() {
  $('#profSel').innerHTML = S.profiles.map(n => `<option ${n === S.profile.name ? 'selected' : ''}>${esc(n)}</option>`).join('');
}

function renderTTChip() {
  const c = $('#ttChip'), st = S.settings;
  const wm = { normal: '通常表示（遅い）', min: '最小化', hidden: '非表示' }[st.windowMode];
  if (!S.ttpmacroOk) { c.className = 'tt-chip bad'; c.textContent = 'Tera Term 未設定'; }
  else { c.className = 'tt-chip'; c.textContent = `Tera Term ${st.ttVersion}系 ・ ウィンドウ: ${wm}`; }
  $('#dbgChip').hidden = !st.debug;
}

function applyMask() {
  $$('.secret').forEach(i => i.classList.toggle('masked', !S.showPass));
}

// ---------- 右側 ----------
function renderSide() {
  const p = S.profile;
  $('#ident').value = p.ident || '';
  $('#logDir').value = p.logDir || '';
  $('#conc').value = p.concurrency;
  $('#jumpOn').checked = !!p.jump.enabled;
  $('#jProto').value = p.jump.proto; $('#jIP').value = p.jump.ip || ''; $('#jPort').value = p.jump.port || '';
  $('#jUser').value = p.jump.user || ''; $('#jPass').value = p.jump.pass || '';
  $('#cUser').value = p.common.user || ''; $('#cPass').value = p.common.pass || ''; $('#cEnable').value = p.common.enable || '';
  renderMode();
}

function renderMode() {
  const p = S.profile, jump = !!p.jump.enabled;
  const mode = jump ? 'serial' : p.mode;
  $$('#modeSeg button').forEach(b => { b.classList.toggle('on', b.dataset.v === mode); b.disabled = jump && b.dataset.v === 'parallel'; });
  $('#concFld').hidden = mode !== 'parallel';
  $('#jumpBox').hidden = !jump;
  $('#jumpLbl').textContent = jump ? '使う（踏み台経由）' : '使わない（直接接続）';
  const id = (p.ident || '').trim();
  $('#identNote').textContent = (id ? id + '_' : '') + 'ホスト名_YYYYMMDD-HHMMSS.txt';
}

// ---------- コマンドセット ----------
function renderSets() {
  const def = S.profile.common.cmdSet;
  $('#setSel').innerHTML = S.cmdSets.map(c => `<option value="${esc(c.name)}" ${c.name === S.curSet ? 'selected' : ''}>${esc(c.name)}${c.name === def ? '　★共通の既定' : ''}</option>`).join('');
  const cur = S.cmdSets.find(c => c.name === S.curSet);
  $('#setText').value = cur ? cur.content.replace(/\r\n/g, '\n') : '';
  S.setDirty = false;
  renderSetInfo();
}
function renderSetInfo() {
  const n = $('#setText').value.split('\n').filter(l => l.trim()).length;
  $('#setInfo').textContent = `${n} コマンド` + (S.setDirty ? '（未保存）' : '');
  $('#setSave').disabled = !S.setDirty;
}
async function saveSet(name) {
  const d = await api('cmdset/save', { name: name || S.curSet, content: $('#setText').value });
  S.cmdSets = d.cmdSets; S.curSet = d.name;
  renderSets(); renderRows();
}

// ---------- グループ ----------
function renderGroups() {
  const m = new Map();
  S.profile.hosts.forEach(h => { const g = (h.group || '').trim(); if (g) m.set(g, (m.get(g) || 0) + 1); });
  const el = $('#groups');
  if (!m.size) { el.innerHTML = ''; return; }
  el.innerHTML = '<span class="glabel">グループ一括:</span>' + Array.from(m).map(([g, n]) =>
    `<span class="gchip"><b>${esc(g)}</b><span class="n">${n}</span><button data-g="${esc(g)}" data-on="1">ON</button><button data-g="${esc(g)}" data-on="0">OFF</button><button data-g="${esc(g)}" data-on="only" title="このグループだけONにする">のみ</button></span>`).join('');
}

// ---------- ホスト表 ----------
function visibleHosts() {
  const q = S.search.trim().toLowerCase();
  if (!q) return S.profile.hosts;
  return S.profile.hosts.filter(h => [h.name, h.ip, h.group].some(v => (v || '').toLowerCase().includes(q)));
}

function setOptions(h) {
  const names = S.cmdSets.map(c => c.name);
  const def = S.profile.common.cmdSet;
  let o = `<option value="">共通${def ? '（' + esc(def) + '）' : ''}</option>`;
  o += names.map(n => `<option value="${esc(n)}" ${n === h.cmdSet ? 'selected' : ''}>${esc(n)}</option>`).join('');
  if (h.cmdSet && !names.includes(h.cmdSet)) o += `<option value="${esc(h.cmdSet)}" selected>⚠ ${esc(h.cmdSet)}（未登録）</option>`;
  return o;
}

function rowHTML(h, no) {
  const c = S.profile.common;
  const miss = h.cmdSet && !S.cmdSets.find(x => x.name === h.cmdSet);
  const custom = hasOverride(h);
  const dtOpts = DT_ORDER.map(k => `<option value="${k}" ${k === (h.devType || 'cisco') ? 'selected' : ''}>${DT_LABEL[k]}</option>`).join('');
  return `<tr data-id="${h.id}" class="${h.on ? '' : 'off'}">
    <td class="c-chk"><input type="checkbox" data-f="on" ${h.on ? 'checked' : ''} title="取得対象にする"></td>
    <td class="c-no">${no}</td>
    <td><input data-f="group" value="${esc(h.group)}" autocomplete="off"></td>
    <td><input data-f="name" value="${esc(h.name)}" autocomplete="off"></td>
    <td><select data-f="proto"><option ${h.proto === 'telnet' ? 'selected' : ''}>telnet</option><option ${h.proto === 'ssh' ? 'selected' : ''}>ssh</option></select></td>
    <td class="c-ip"><input data-f="ip" value="${esc(h.ip)}" autocomplete="off" spellcheck="false"></td>
    <td><select data-f="cmdSet" class="${miss ? 'missing' : ''}">${setOptions(h)}</select></td>
    <td><input data-f="user" value="${esc(h.user)}" placeholder="${c.user ? '共通' : ''}" autocomplete="off"></td>
    <td><input data-f="pass" class="secret" value="${esc(h.pass)}" placeholder="${c.pass ? '共通' : ''}" autocomplete="off"></td>
    <td><input data-f="enable" class="secret" value="${esc(h.enable)}" placeholder="${c.enable ? '共通' : ''}" autocomplete="off"></td>
    <td><select data-f="devType" class="dt dt-${esc(h.devType || 'cisco')}">${dtOpts}</select></td>
    <td class="c-act"><button class="icon-btn ${custom ? 'has' : ''}" data-act="detail" title="${custom ? '個別設定あり — ' : ''}個別設定（機器種別の上書き・ポート・タイムアウト）とマクロの確認">⚙</button><button class="icon-btn del" data-act="del" title="この行を削除">✕</button></td>
    <td class="c-status"><div class="st"></div></td>
  </tr>`;
}

function renderRows() {
  const all = S.profile.hosts, vis = visibleHosts();
  $('#rows').innerHTML = vis.map(h => rowHTML(h, all.indexOf(h) + 1)).join('');
  $('#empty').hidden = all.length > 0;
  applyMask(); renderStatus(); renderCounts();
}

// 台数表示と全選択チェックの状態だけ更新する
function renderCounts() {
  const all = S.profile.hosts, vis = visibleHosts();
  const on = all.filter(h => h.on).length;
  $('#hostCount').textContent = all.length ? `${all.length}台（取得対象 ${on}台）` : '';
  const vOn = vis.filter(h => h.on).length;
  const ca = $('#chkAll'); ca.checked = vis.length > 0 && vOn === vis.length; ca.indeterminate = vOn > 0 && vOn < vis.length;
  renderBar();
}

function hostById(id) { return S.profile.hosts.find(h => h.id === id); }

// 状態列だけを書き換える（入力中のフォーカスを奪わないため表全体は描き直さない）
function renderStatus() {
  const js = {};
  if (S.job) S.job.hosts.forEach(h => js[h.id] = h);
  $$('#rows tr').forEach(tr => {
    const id = tr.dataset.id, el = $('.st', tr), j = js[id], pre = S.pre[id];
    let html = '';
    if (j) html = statusHTML(j);
    else if (pre) html = pre.ok ? `<span class="pill ok">到達OK</span><span class="txt">${esc(pre.addr)}</span>` : `<span class="pill ng">到達不可</span><span class="txt err">${esc(pre.addr)} に届きません</span>`;
    if (el.dataset.h !== html) { el.innerHTML = html; el.dataset.h = html; }
  });
}

function statusHTML(j) {
  const host = j.hostname ? `<span class="txt" title="${esc(j.hostname)}">${esc(j.hostname)}</span>` : '';
  switch (j.status) {
    case 'queued': return `<span class="pill">待機中</span>`;
    case 'check': return `<span class="pill run">疎通確認</span>`;
    case 'running': {
      const pct = j.cmdTotal ? Math.round(j.cmdDone / j.cmdTotal * 100) : 0;
      const prog = j.stage === 'ログ取得中' ? `<span class="mini"><i style="width:${pct}%"></i></span><span class="txt">${j.cmdDone}/${j.cmdTotal}　${esc(j.hostname)}</span>` : '';
      return `<span class="pill run">${esc(j.stage)}</span>${prog}`;
    }
    case 'ok': return `<span class="pill ok">完了</span>${logLink(j)}${host}`;
    case 'warn': return `<span class="pill warn">要確認</span>${logLink(j)}<span class="txt" data-msg="${esc(j.message)}" title="${esc(j.message)}">${esc(j.message)}</span>`;
    case 'aborted': return `<span class="pill">中断</span><span class="txt">${esc(j.message)}</span>`;
    default: return `<span class="pill ng">${esc(j.stage || '失敗')}</span><span class="txt err" data-msg="${esc(j.message)}" title="${esc(j.message)}">${esc(j.message)}</span>`;
  }
}
function logLink(j) { return j.logFile ? `<a data-open="${esc(j.logFile)}" title="${esc(j.logFile)}">ログを開く</a>` : ''; }

// ---------- 下のバー ----------
function targetIds() { return S.profile.hosts.filter(h => h.on && (h.ip || '').trim()).map(h => h.id); }

function fmtDur(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  return s >= 60 ? `${Math.floor(s / 60)}分${String(s % 60).padStart(2, '0')}秒` : `${s}秒`;
}

function renderBar() {
  const run = running(), j = S.job, n = targetIds().length;
  $('#btnStart').hidden = run; $('#btnAbort').hidden = !run;
  $('#btnAbort').disabled = !!(j && j.aborting);
  $('#btnAbort').textContent = j && j.aborting ? '中断しています…' : '■ 中断';
  $('#startLbl').textContent = n ? `取得開始（${n}台）` : '取得開始';
  $('#btnStart').disabled = n === 0;
  $('#btnCheck').disabled = run || n === 0;
  $('#profSel').disabled = run;
  ['profNew', 'profDup', 'profRen', 'profDel'].forEach(id => $('#' + id).disabled = run);

  $('#btnDebug').hidden = !(j && j.debugDir);
  if (!j) { $('#progress').hidden = true; $('#btnRetry').hidden = true; $('#btnResult').hidden = true; return; }
  const c = { ok: 0, warn: 0, ng: 0, rest: 0 };
  j.hosts.forEach(h => {
    if (h.status === 'ok') c.ok++; else if (h.status === 'warn') c.warn++;
    else if (h.status === 'error' || h.status === 'aborted') c.ng++; else c.rest++;
  });
  const total = j.hosts.length, fin = total - c.rest;
  $('#progress').hidden = false;
  $('#pFill').style.width = (total ? fin / total * 100 : 0) + '%';
  $('.pbar').classList.toggle('done-ng', !run && c.ng > 0);
  const how = j.relay ? '踏み台経由・連続' : (j.parallel > 1 ? `並列 ${j.parallel}` : '連続');
  $('#pText').textContent = run ? `取得中 ${fin}/${total}（${how}）` : `終了 ${fin}/${total}`;
  $('#cOk').textContent = `成功 ${c.ok}`;
  $('#cWarn').hidden = !c.warn; $('#cWarn').textContent = `要確認 ${c.warn}`;
  $('#cNg').textContent = `失敗 ${c.ng}`;
  $('#cRest').hidden = !run; $('#cRest').textContent = `残り ${c.rest}`;
  $('#pTime').textContent = fmtDur((run ? Date.now() : j.ended) - j.started);
  $('#btnRetry').hidden = run || c.ng === 0;
  $('#btnResult').hidden = run;
  document.title = run ? `(${fin}/${total}) ${S.appName}` : S.appName;
}

// ---------- 実行 ----------
async function startRun(ids) {
  if (!S.ttpmacroOk) { openSettings('まず Tera Term の ttpmacro.exe を指定してください。'); return; }
  if (S.setDirty) await saveSet();
  clearTimeout(saveTimer); saveTimer = null;
  S.pre = {};
  // 通常表示のまま複数台を流すと描画で大幅に遅くなるので、開始時に一言知らせる
  if (S.settings.windowMode === 'normal' && ids.length > 1 && S.profile.mode === 'parallel' && !S.profile.jump.enabled) {
    toast('ウィンドウ表示が「通常表示」のため、取得に時間がかかります。速度を優先する場合は詳細設定で「最小化」か「非表示」にしてください。', '', 9000);
  }
  try {
    await api('run', { profile: S.profile, ids });
    $('#saveState').textContent = '保存済み';
  } catch (e) { toast(e.message, 'ng'); }
}

function connectEvents() {
  const es = new EventSource('/api/events?t=' + encodeURIComponent(TOKEN));
  es.onmessage = ev => {
    const prev = S.job, wasRun = running();
    S.job = JSON.parse(ev.data);
    renderStatus(); renderBar();
    if (wasRun && S.job && !S.job.running && prev && prev.seq === S.job.seq) onFinished();
  };
}

function onFinished() {
  const j = S.job;
  const ng = j.hosts.filter(h => h.status === 'error' || h.status === 'aborted').length;
  const warn = j.hosts.filter(h => h.status === 'warn').length;
  const ok = j.hosts.length - ng - warn;
  if (ng) toast(`取得が終わりました。成功 ${ok + warn}台 / 失敗 ${ng}台`, 'ng', 8000);
  else if (warn) toast(`取得が終わりました（${warn}台は内容の確認が必要です）`, '', 8000);
  else toast(`すべて完了しました（${ok}台）`, 'ok', 6000);
  if (j.debugDir) toast('デバッグログを保存しました。下の「デバッグログを開く」から確認できます。', '', 7000);
  if (S.settings.notifySound) beep(ng === 0);
  if (S.settings.openLogDirOnFinish && ok + warn > 0) api('open', { path: j.logDir }).catch(() => { });
}

function beep(good) {
  try {
    const ac = new (window.AudioContext || window.webkitAudioContext)();
    (good ? [880, 1175] : [440, 330]).forEach((f, i) => {
      const o = ac.createOscillator(), g = ac.createGain();
      o.frequency.value = f; o.type = 'sine';
      g.gain.setValueAtTime(0.0001, ac.currentTime + i * 0.16);
      g.gain.exponentialRampToValueAtTime(0.12, ac.currentTime + i * 0.16 + 0.02);
      g.gain.exponentialRampToValueAtTime(0.0001, ac.currentTime + i * 0.16 + 0.15);
      o.connect(g).connect(ac.destination);
      o.start(ac.currentTime + i * 0.16); o.stop(ac.currentTime + i * 0.16 + 0.16);
    });
    setTimeout(() => ac.close(), 800);
  } catch (e) { /* 音が出せない環境では黙って諦める */ }
}

function pad(n) { return String(n).padStart(2, '0'); }
function fmtTime(ms) { if (!ms) return ''; const d = new Date(ms); return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`; }

async function saveResultCsv() {
  const j = S.job; if (!j) return;
  const label = { ok: '成功', warn: '要確認', error: '失敗', aborted: '中断', queued: '未実行', running: '実行中', check: '実行中' };
  const q = v => '"' + String(v == null ? '' : v).replace(/"/g, '""') + '"';
  const lines = [['No', '名称', 'IPアドレス', 'ホスト名', '結果', '詳細', 'ログファイル', '開始', '終了', '所要秒'].map(q).join(',')];
  j.hosts.forEach((h, i) => lines.push([i + 1, h.name, h.ip, h.hostname, label[h.status] || h.status, h.message, h.logFile, fmtTime(h.started), fmtTime(h.ended), h.ended && h.started ? Math.round((h.ended - h.started) / 1000) : ''].map(q).join(',')));
  const d = new Date(j.started);
  const id = (S.profile.ident || '').trim();
  const name = `取得結果_${id ? id + '_' : ''}${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}.csv`;
  try {
    const r = await api('save-text', { dir: j.logDir, name, content: lines.join('\r\n') + '\r\n' });
    toast('結果を保存しました: ' + r.path, 'ok', 6000);
    api('open', { path: r.path }).catch(() => { });
  } catch (e) { toast(e.message, 'ng'); }
}

// ---------- CSV / 貼り付け ----------
function decodeBytes(buf) {
  try { return new TextDecoder('utf-8', { fatal: true }).decode(buf); }
  catch (e) { return new TextDecoder('shift_jis').decode(buf); }
}

// 旧マクロの host_list.csv 形式（A:読み飛ばし B:方式 C:IP D:コマンド E:user F:pass G:enable H:仕切り I:備考）
function parseOldRow(cols) {
  const c = cols.map(x => (x || '').trim());
  if (!/^(telnet|ssh)$/i.test(c[1] || '') || (c[0] && !c[0].startsWith(';'))) return null;
  const strip = v => (v || '').replace(/^;/, '').trim();
  return newHost({ on: !c[0].startsWith(';'), proto: c[1].toLowerCase(), ip: c[2] || '', cmdSet: c[3] || '', user: c[4] || '', pass: c[5] || '', enable: c[6] || '', name: strip(c[8]), group: strip(c[9]), devType: normDevType(strip(c[10])) });
}

function parseCsv(text) {
  const out = [];
  text.split(/\r?\n/).forEach(line => {
    if (!line.trim()) return;
    const h = parseOldRow(line.split(','));
    if (h && h.ip) out.push(h);
  });
  return out;
}

// 共通の既定と同じコマンドセットは「共通」に寄せておく（あとで一括変更しやすい）
function tidyImported(list) {
  const def = S.profile.common.cmdSet;
  list.forEach(h => { if (def && h.cmdSet === def) h.cmdSet = ''; });
  return list;
}

function importHosts(list, label) {
  if (!list.length) { toast('取り込めるホストが見つかりませんでした', 'ng'); return; }
  tidyImported(list);
  const add = replace => {
    S.profile.hosts = replace ? list : S.profile.hosts.concat(list);
    S.pre = {}; touch(); renderGroups(); renderRows();
    toast(`${list.length}台を${replace ? '読み込みました' : '追加しました'}`, 'ok');
  };
  if (!S.profile.hosts.length) { add(true); return; }
  modal({
    title: label,
    body: `<div>${list.length}台のホストが見つかりました。今のリスト（${S.profile.hosts.length}台）をどうしますか？</div>`,
    buttons: [
      { label: 'キャンセル' },
      { label: '今のリストに追加', onClick: () => add(false) },
      { label: '置き換える', cls: 'primary', onClick: () => add(true) },
    ],
  });
}

function normProto(v) { return /ssh/i.test(v) ? 'ssh' : 'telnet'; }
// 「FortiGate」「RTX1210」などの表記から機器種別を決める。分からなければ Cisco IOS
function normDevType(v) {
  v = (v || '').trim();
  if (DT_LABEL[v]) return v;
  if (/forti|fgt/i.test(v)) return 'fortigate';
  if (/yamaha|rtx|ヤマハ/i.test(v)) return 'yamaha';
  if (/汎用|other|その他/i.test(v)) return 'other';
  return 'cisco';
}
function setField(h, f, v) {
  v = (v == null ? '' : String(v)).trim();
  if (f === 'proto') h.proto = normProto(v);
  else if (f === 'devType') h.devType = normDevType(v);
  else if (f === 'cmdSet') h.cmdSet = (v === '共通' || v === S.profile.common.cmdSet) ? '' : v;
  else h[f] = v;
}

const IP_RE = /^\d{1,3}(\.\d{1,3}){3}$/;

// Excelなどからの貼り付け。セルにフォーカスがあればそこを起点に、無ければIP列を手がかりに行を追加する
function handlePaste(e) {
  const text = (e.clipboardData || window.clipboardData).getData('text');
  if (!text) return;
  const t = e.target;
  const inGrid = t.closest && t.closest('#rows') && t.dataset && t.dataset.f && t.dataset.f !== 'on';
  const inOtherInput = !inGrid && t.matches && t.matches('input,textarea,select');
  if (inOtherInput) return;
  const multi = /\t/.test(text) || /\r?\n[^\r\n]/.test(text);
  if (inGrid && !multi) return; // 単一セルは普通の貼り付けに任せる
  e.preventDefault();
  const rows = text.replace(/\r?\n$/, '').split(/\r?\n/).map(r => r.split('\t'));

  // 旧CSVと同じ並び（B列がtelnet/ssh）ならその形式として取り込む
  const old = rows.map(parseOldRow).filter(h => h && h.ip);
  if (old.length && old.length >= rows.filter(r => r.join('').trim()).length - 5) {
    if (inGrid) { S.profile.hosts = S.profile.hosts.concat(tidyImported(old)); touch(); renderGroups(); renderRows(); toast(`${old.length}台を追加しました`, 'ok'); }
    else importHosts(old, '貼り付け');
    return;
  }

  if (inGrid) {
    const vis = visibleHosts();
    let r0 = vis.indexOf(hostById(t.closest('tr').dataset.id));
    const c0 = EDIT_COLS.indexOf(t.dataset.f);
    rows.forEach((cells, ri) => {
      let h = vis[r0 + ri];
      if (!h) { h = newHost(); S.profile.hosts.push(h); }
      cells.forEach((v, ci) => { const f = EDIT_COLS[c0 + ci]; if (f) setField(h, f, v); });
    });
    touch(); renderGroups(); renderRows();
    return;
  }
  // 表の外に貼られた場合。IPらしい列を見つけて位置合わせする
  let ipCol = -1;
  for (const cells of rows) { ipCol = cells.findIndex(v => IP_RE.test((v || '').trim())); if (ipCol >= 0) break; }
  const off = ipCol >= 0 ? EDIT_COLS.indexOf('ip') - ipCol : EDIT_COLS.indexOf('name');
  const list = [];
  rows.forEach(cells => {
    if (!cells.join('').trim()) return;
    const h = newHost();
    cells.forEach((v, ci) => { const f = EDIT_COLS[ci + off]; if (f) setField(h, f, v); });
    if (h.ip || h.name) list.push(h);
  });
  importHosts(list, '貼り付け');
}

// ---------- 個別設定 ----------
// 機器種別の既定値（詳細設定で変えた値）
function dtDefault(k) { return (S.settings.devTypes || {})[k] || S.info.devTypeDefaults[k]; }
const showVal = v => v ? v.replace(/\n/g, ' / ') : '（なし）';

function openDetail(h) {
  const st = S.settings;
  const defPort = h.proto === 'ssh' ? st.sshPort : st.telnetPort;
  const dtSel = DT_ORDER.map(k => `<option value="${k}" ${k === (h.devType || 'cisco') ? 'selected' : ''}>${DT_LABEL[k]}</option>`).join('');
  // 空欄なら種別の既定値を使う項目
  const txt = (id, val) => `<input type="text" id="${id}" class="mono" value="${esc(val || '')}" spellcheck="false" autocomplete="off">`;
  // 「使わない」も選べる項目。null=既定値 / ''=使わない / 文字列=指定
  const opt = (id, val, area) => `<span class="optf">
      ${area ? `<textarea id="${id}" class="mono" rows="3" spellcheck="false">${esc(val || '')}</textarea>`
             : `<input type="text" id="${id}" class="mono" value="${esc(val || '')}" spellcheck="false" autocomplete="off" ${id === 'dEnCmd' ? 'list="enCmdList"' : ''}>`}
      <label class="chk"><input type="checkbox" id="${id}_off" ${val === '' ? 'checked' : ''}> 使わない</label></span>`;
  const body = `
    <p class="note">空欄の項目は、機器種別の既定値（右に表示）を使います。既定値は詳細設定で変えられます。</p>
    <div class="dsec">
      <h3>機器種別</h3>
      <label class="fld"><span>機器種別</span><select id="dType">${dtSel}</select></label>
    </div>
    <div class="dsec">
      <h3>ログイン・プロンプト</h3>
      <div class="drow"><span>ユーザ名の入力待ち</span>${txt('dPU', h.promptUsername)}<em data-d="promptUsername"></em></div>
      <div class="drow"><span>パスワードの入力待ち</span>${txt('dPP', h.promptPassword)}<em data-d="promptPassword"></em></div>
      <div class="drow"><span>ユーザモードの記号</span>${txt('dPUser', h.promptUser)}<em data-d="promptUser"></em></div>
      <div class="drow"><span>特権モードの記号</span>${txt('dPEn', h.promptEnable)}<em data-d="promptEnable"></em></div>
      <div class="drow"><span>特権モードに移行するコマンド</span>${opt('dEnCmd', h.enableCmd)}<em data-d="enableCmd"></em></div>
      <datalist id="enCmdList"><option value="enable"><option value="administrator"></datalist>
    </div>
    <div class="dsec">
      <h3>ログの取り方</h3>
      <div class="drow al-top"><span>前処理コマンド<small>ログを取る前に送る（1行1コマンド）</small></span>${opt('dPre', h.preCmds, true)}<em data-d="preCmds"></em></div>
      <div class="drow"><span>時刻確認コマンド</span>${opt('dClock', h.clockCmd)}<em data-d="clockCmd"></em></div>
      <div class="drow"><span>コマンド間の区切り</span>${opt('dSep', h.separator)}<em data-d="separator"></em></div>
    </div>
    <div class="dsec">
      <h3>接続・タイムアウト</h3>
      <div class="drow"><span>ポート番号</span><input type="number" id="dPort" class="num" min="0" max="65535" value="${h.port || ''}"><em>既定: ${defPort}</em></div>
      <div class="drow"><span>ログイン待ち（秒）</span><input type="number" id="dLogin" class="num" min="0" value="${h.loginTimeout || ''}"><em>既定: ${st.loginTimeout}</em></div>
      <div class="drow"><span>無応答で打ち切り（秒）</span><input type="number" id="dCmd" class="num" min="0" value="${h.cmdTimeout || ''}"><em>既定: ${st.cmdTimeout}（出力が止まってからの時間）</em></div>
    </div>`;
  // 機器種別を切り替えたら、右側の既定値の表示も切り替える
  const showDefaults = ov => {
    const d = dtDefault($('#dType', ov).value);
    $$('em[data-d]', ov).forEach(e => { e.textContent = '既定: ' + showVal(d[e.dataset.d]); });
  };
  const optVal = (ov, id) => {
    if ($('#' + id + '_off', ov).checked) return '';
    const v = $('#' + id, ov).value.trim();
    return v ? v : null;
  };
  modal({
    title: `個別設定 — ${h.name || h.ip || '(名称なし)'}`, size: 'mid', body,
    buttons: [
      { label: 'このホストのマクロを確認', cls: 'left', onClick: ov => { previewTTL(Object.assign({}, h, collect(ov))); return false; } },
      { label: '個別設定をすべて消す', onClick: ov => {
        $$('input[type=text], input[type=number], textarea', ov).forEach(i => i.value = '');
        $$('input[type=checkbox]', ov).forEach(i => i.checked = false);
        return false;
      } },
      { label: 'キャンセル' },
      { label: '反映', cls: 'primary', onClick: ov => { Object.assign(h, collect(ov)); touch(); renderRows(); } },
    ],
    onOpen: ov => {
      showDefaults(ov);
      $('#dType', ov).onchange = () => showDefaults(ov);
      // 「使わない」にしたら入力欄は使えなくする
      $$('.optf', ov).forEach(f => {
        const c = $('input[type=checkbox]', f), i = $('input[type=text], textarea', f);
        const sync = () => { i.disabled = c.checked; };
        c.onchange = sync; sync();
      });
    },
  });
  function collect(ov) {
    return {
      devType: $('#dType', ov).value,
      promptUsername: $('#dPU', ov).value.trim(), promptPassword: $('#dPP', ov).value.trim(),
      promptUser: $('#dPUser', ov).value.trim(), promptEnable: $('#dPEn', ov).value.trim(),
      enableCmd: optVal(ov, 'dEnCmd'), preCmds: optVal(ov, 'dPre'), clockCmd: optVal(ov, 'dClock'), separator: optVal(ov, 'dSep'),
      port: +$('#dPort', ov).value || 0, loginTimeout: +$('#dLogin', ov).value || 0, cmdTimeout: +$('#dCmd', ov).value || 0,
    };
  }
}

async function previewTTL(h) {
  try {
    if (S.setDirty) await saveSet();
    const prof = Object.assign({}, S.profile, { hosts: S.profile.hosts.map(x => x.id === h.id ? h : x) });
    const d = await api('ttl-preview', { profile: prof, id: h.id });
    const pre = document.createElement('pre'); pre.textContent = d.ttl;
    modal({ title: '実行されるマクロ（パスワードは伏せ字）', size: 'wide', body: pre });
  } catch (e) { toast(e.message, 'ng'); }
}

// ---------- 詳細設定 ----------
function dtEditor(k, d, show) {
  const f = (dk, label, hint) => `<label class="fld"><span>${label}</span><input type="text" class="mono" data-dt="${k}" data-dk="${dk}" value="${esc(d[dk] || '')}" spellcheck="false">${hint ? `<span class="unit">${hint}</span>` : ''}</label>`;
  return `<div class="dt-pane" data-pane="${k}" ${show ? '' : 'hidden'}>
    <div class="two-col">
      <div>
        ${f('promptUsername', 'ユーザ名の入力待ち')}
        ${f('promptPassword', 'パスワードの入力待ち')}
        ${f('promptUser', 'ユーザモードの記号')}
        ${f('promptEnable', '特権モードの記号')}
        ${f('enableCmd', '特権モードに移行するコマンド', '空欄で移行しない')}
      </div>
      <div>
        <label class="fld al-top"><span>前処理コマンド</span><textarea class="mono" rows="3" data-dt="${k}" data-dk="preCmds" spellcheck="false">${esc(d.preCmds || '')}</textarea></label>
        ${f('clockCmd', '時刻確認コマンド', '空欄で送らない')}
        ${f('separator', 'コマンド間の区切り', '空欄で送らない')}
        ${f('moreStr', 'ページ送りの表示', '出たらスペースで進める')}
      </div>
    </div>
    <div class="row between"><span class="note">前処理はログを取り始める前に送り、ログには残しません（1行1コマンド）。</span><button class="btn sm" data-reset="${k}">${DT_LABEL[k]} を初期値に戻す</button></div>
  </div>`;
}

function openSettings(msg) {
  const st = JSON.parse(JSON.stringify(S.settings));
  const num = (k, unit, min, max) => `<span class="row"><input type="number" class="num" data-k="${k}" data-t="n" min="${min}" max="${max}" value="${st[k]}"> <span class="unit">${unit}</span></span>`;
  const chk = (k, label) => `<label class="chk"><input type="checkbox" data-k="${k}" data-t="b" ${st[k] ? 'checked' : ''}> ${label}</label>`;
  const txt = (k, cls) => `<input type="text" data-k="${k}" data-t="s" class="${cls || ''}" value="${esc(st[k])}" spellcheck="false">`;
  const radio = (k, v, label) => `<label class="radio"><input type="radio" name="r_${k}" data-k="${k}" data-t="r" value="${v}" ${st[k] === v ? 'checked' : ''}> ${label}</label>`;
  const body = `
  ${msg ? `<p class="warn-note">${esc(msg)}</p>` : ''}
  <div class="sgrid">
    <div class="sgroup full">
      <h3>Tera Term</h3>
      <label class="fld"><span>ttpmacro.exe の場所</span>
        <span class="row grow"><input type="text" class="grow mono" data-k="ttpmacroPath" data-t="s" id="sPath" value="${esc(st.ttpmacroPath)}" spellcheck="false" placeholder="C:\\Program Files (x86)\\teraterm5\\ttpmacro.exe">
        <button class="btn sm" id="sPick">参照</button></span></label>
      <div class="fld"><span></span><span class="path-state" id="sPathState"></span></div>
      ${S.detected.length ? `<div class="fld"><span>見つかったTera Term</span><div class="detected grow">${S.detected.map((d, i) => `<button data-i="${i}">${esc(d.detail)}　<span class="mono">${esc(d.path)}</span></button>`).join('')}</div></div>` : ''}
      <div class="fld"><span>バージョン</span><span>${radio('ttVersion', '5', '5系')}${radio('ttVersion', '4', '4系')}<span class="unit">場所を指定すると自動で判定します。合わない場合は手で切り替えてください</span></span></div>
      <div class="fld"><span>ウィンドウ表示</span><span>${radio('windowMode', 'min', '最小化（おすすめ）')}${radio('windowMode', 'normal', '通常表示（動作確認用・遅い）')}${radio('windowMode', 'hidden', '非表示')}</span></div>
      <div class="fld"><span></span><span class="unit">通常表示は画面の描画に時間を取られ、並列取得では最小化・非表示の4〜5倍の時間がかかります（実測）。止まる場所を目で確認したいとき専用です。</span></div>
      <label class="fld"><span>起動の間隔</span>${num('launchIntervalMs', 'ミリ秒（並列時、1台ずつこの間隔をあけて起動）', 0, 10000)}</label>
    </div>
    <div class="sgroup">
      <h3>タイムアウト</h3>
      <label class="fld"><span>接続待ち</span>${num('connectTimeout', '秒', 3, 600)}</label>
      <label class="fld"><span>ログイン待ち</span>${num('loginTimeout', '秒', 1, 600)}</label>
      <label class="fld"><span>コマンド無応答で打ち切り</span>${num('cmdTimeout', '秒', 1, 7200)}</label>
      <p class="note">出力が流れている間は何分でも待ちます。この秒数のあいだ何も出てこないときだけ「要確認」にします（show tech などの長いコマンド向け）。</p>
      <label class="fld"><span>1台あたりの上限</span>${num('hostTimeout', '秒（0で無制限）', 0, 86400)}</label>
      <label class="fld"><span>コマンド間のウェイト</span>${num('cmdDelayMs', 'ミリ秒', 0, 60000)}</label>
    </div>
    <div class="sgroup">
      <h3>接続</h3>
      <label class="fld"><span>SSHの既定ポート</span>${num('sshPort', '', 1, 65535)}</label>
      <label class="fld"><span>Telnetの既定ポート</span>${num('telnetPort', '', 1, 65535)}</label>
      ${chk('noSecurityWarning', 'SSH初回接続の確認ダイアログを出さない')}
      <p class="note">known_hosts 未登録の機器で確認ダイアログが出ると並列取得が止まるため、既定でONです。ホスト鍵の確認が省かれるので、信頼できる社内ネットワークでのみ使ってください。</p>
      ${chk('serialStartup', '並列取得のとき、1台ずつ接続完了を待ってから次を起動する（安定優先）')}
      <p class="note">Tera Termを同時に立ち上げると接続に失敗することがあるための対策です。ログの取得自体は並列で進みます。OFFにすると起動間隔だけあけて一斉に起動します（速いが不安定になることがあります）。</p>
      ${chk('precheck', '開始前に疎通チェックを行い、届かない機器は飛ばす')}
      <label class="fld"><span>疎通チェックの待ち</span>${num('precheckTimeoutMs', 'ミリ秒', 200, 30000)}</label>
    </div>
    <div class="sgroup full">
      <h3>機器種別ごとの既定値</h3>
      <p class="note">ホストの機器種別に応じて使われる値です。ホストごとに変えたいときは、表の行の ⚙（個別設定）で上書きできます。</p>
      <div class="dt-tabs">${DT_ORDER.map((k, i) => `<button class="${i ? '' : 'on'}" data-tab="${k}">${DT_LABEL[k]}</button>`).join('')}</div>
      ${DT_ORDER.map((k, i) => dtEditor(k, st.devTypes[k], i === 0)).join('')}
    </div>
    <div class="sgroup">
      <h3>完了したとき</h3>
      ${chk('openLogDirOnFinish', 'ログフォルダを自動で開く')}
      ${chk('notifySound', '音で知らせる')}
    </div>
    <div class="sgroup">
      <h3>更新</h3>
      ${chk('updateCheck', '起動時に新しいバージョンを確認する')}
      <p class="note">GitHub（api.github.com）に最新版の情報を問い合わせます。ホストリストなどの情報は送りません。</p>
      <div><button class="btn sm" id="sUpd">今すぐ確認する</button></div>
    </div>
    <div class="sgroup full">
      <h3>🐞 調査用（デバッグモード）</h3>
      ${chk('debug', 'デバッグモードで実行する（調査用ログを残す）')}
      <p class="note">ONにして取得すると、実行のたびに <span class="mono">${esc(S.debugDir)}\\run_日時</span> に次のファイルを残します。うまく動かないときは、このフォルダごと送ってもらえれば原因を追えます。<br>
      ・<b>app.log</b> … アプリ側の記録（起動・接続完了・タイムアウトの時刻、止まった時に出ていたウィンドウやダイアログの文面）<br>
      ・<b>hN_IP_trace.txt</b> … マクロがどこまで進んだか（connectの戻り値、ログイン、enable、各コマンド）<br>
      ・<b>hN_IP_macro.ttl</b> … 実際に流したマクロの写し（パスワードは伏せ字）<br>
      調査のときはウィンドウ表示を「通常表示」にすると、Tera Termの画面でも止まった場所を確認できます。</p>
      <div><button class="btn sm" id="sDbgOpen">デバッグフォルダを開く</button></div>
    </div>
    <div class="sgroup full">
      <h3>踏み台から対象へ入るコマンド</h3>
      <label class="fld"><span>SSHのとき</span>${txt('jumpSshCmd', 'mono grow')}</label>
      <label class="fld"><span>Telnetのとき</span>${txt('jumpTelnetCmd', 'mono grow')}</label>
      <label class="fld"><span>踏み台のプロンプト記号</span>${txt('jumpPrompts', 'mono')}<span class="unit">空白区切り。{user} {ip} {port} が対象の値に置き換わります</span></label>
    </div>
  </div>
  <p class="note">設定とデータの保存先: <span class="mono">${esc(S.dataDir)}</span>　／　${esc(S.appName)} v${esc(S.version)}　開発: ${esc(S.info.author)}</p>`;

  const collect = ov => {
    $$('[data-dt]', ov).forEach(i => { st.devTypes[i.dataset.dt][i.dataset.dk] = i.value; });
    $$('[data-k]', ov).forEach(i => {
      const k = i.dataset.k, t = i.dataset.t;
      if (t === 'n') st[k] = parseInt(i.value, 10) || 0;
      else if (t === 'b') st[k] = i.checked;
      else if (t === 'r') { if (i.checked) st[k] = i.value; }
      else st[k] = i.value;
    });
    return st;
  };
  const checkPath = async ov => {
    const el = $('#sPathState', ov), p = $('#sPath', ov).value.trim();
    if (!p) { el.className = 'path-state ng'; el.textContent = '未設定です。Tera Termのインストールフォルダにある ttpmacro.exe を指定してください'; return; }
    try {
      const d = await api('ttinfo', { path: p });
      if (!d.exists) { el.className = 'path-state ng'; el.textContent = '✕ この場所にファイルがありません'; return; }
      if (d.path && d.path !== p) $('#sPath', ov).value = d.path; // ttermpro.exe等が選ばれたらttpmacro.exeに直す
      el.className = 'path-state ok'; el.textContent = '✓ ' + d.detail;
      $$('[data-k=ttVersion]', ov).forEach(r => r.checked = r.value === d.version);
    } catch (e) { el.textContent = e.message; }
  };
  modal({
    title: '詳細設定', size: 'wide', body,
    buttons: [
      { label: 'キャンセル' },
      { label: '保存', cls: 'primary', onClick: async ov => {
        const d = await api('settings/save', collect(ov));
        S.settings = d.settings; S.ttpmacroOk = d.ttpmacroOk;
        renderTTChip(); renderBar();
        if (!S.ttpmacroOk) toast('ttpmacro.exe が見つかりません。場所を確認してください。', 'ng');
        else toast('設定を保存しました', 'ok');
      } },
    ],
    onOpen: ov => {
      checkPathInit(ov);
      $('#sPath', ov).addEventListener('change', () => checkPath(ov));
      $('#sPick', ov).onclick = async () => {
        try {
          const d = await api('pick', { kind: 'ttpmacro', current: $('#sPath', ov).value });
          if (d.path) { $('#sPath', ov).value = d.path; checkPath(ov); }
        } catch (e) { toast(e.message, 'ng'); }
      };
      $$('.detected button', ov).forEach(b => b.onclick = () => { $('#sPath', ov).value = S.detected[+b.dataset.i].path; checkPath(ov); });
      $('#sDbgOpen', ov).onclick = () => api('open', { path: S.debugDir, create: true }).catch(e => toast(e.message, 'ng'));
      $('#sUpd', ov).onclick = () => checkUpdate(true);
      // 機器種別のタブ切り替えと「初期値に戻す」
      $$('.dt-tabs button', ov).forEach(b => b.onclick = () => {
        $$('.dt-tabs button', ov).forEach(x => x.classList.toggle('on', x === b));
        $$('.dt-pane', ov).forEach(p => p.hidden = p.dataset.pane !== b.dataset.tab);
      });
      $$('[data-reset]', ov).forEach(b => b.onclick = () => {
        const k = b.dataset.reset, d = S.info.devTypeDefaults[k];
        $$(`[data-dt="${k}"]`, ov).forEach(i => { i.value = d[i.dataset.dk] || ''; });
        toast(`${DT_LABEL[k]} を初期値に戻しました（保存で確定します）`);
      });
    },
  });
  // 開いた直後は保存済みのバージョン選択を上書きしないよう、存在確認だけ行う
  async function checkPathInit(ov) {
    const el = $('#sPathState', ov), p = st.ttpmacroPath;
    if (!p) { el.className = 'path-state ng'; el.textContent = '未設定です。Tera Termのインストールフォルダにある ttpmacro.exe を指定してください'; return; }
    try {
      const d = await api('ttinfo', { path: p });
      el.className = 'path-state ' + (d.exists ? 'ok' : 'ng');
      el.textContent = d.exists ? '✓ ' + d.detail : '✕ この場所にファイルがありません';
    } catch (e) { /* 表示だけなので無視 */ }
  }
}

// ---------- イベントの結び付け ----------
function bindStatic() {
  // プロファイル
  $('#profSel').onchange = async e => {
    try { await flushSave(); const d = await api('profile/load', { name: e.target.value }); S.profile = d.profile; noticeLogReset(d.logDirReset); S.pre = {}; S.search = ''; $('#search').value = ''; renderAll(); $('#saveState').textContent = ''; }
    catch (err) { toast(err.message, 'ng'); renderProfiles(); }
  };
  const createProfile = async (copy) => {
    const name = await askText(copy ? 'プロファイルを複製' : '新しいプロファイル', '名前（案件名など）', copy ? S.profile.name + ' のコピー' : '', '作成');
    if (!name) return;
    try {
      await flushSave();
      const d = await api('profile/create', { name, copyFrom: copy ? S.profile.name : '' });
      S.profile = d.profile; S.profiles = d.profiles; S.pre = {}; renderAll();
    } catch (e) { toast(e.message, 'ng'); }
  };
  $('#profNew').onclick = () => createProfile(false);
  $('#profDup').onclick = () => createProfile(true);
  $('#profRen').onclick = async () => {
    const to = await askText('名前の変更', '新しい名前', S.profile.name, '変更');
    if (!to || to === S.profile.name) return;
    try { await flushSave(); const d = await api('profile/rename', { from: S.profile.name, to }); S.profile.name = to; S.profiles = d.profiles; renderProfiles(); }
    catch (e) { toast(e.message, 'ng'); }
  };
  $('#profDel').onclick = async () => {
    if (!await askConfirm('プロファイルの削除', `「${esc(S.profile.name)}」を削除します。ホストリストも消えます。よろしいですか？`, '削除する', true)) return;
    try { clearTimeout(saveTimer); saveTimer = null; await saving; const d = await api('profile/delete', { name: S.profile.name }); S.profile = d.profile; S.profiles = d.profiles; S.pre = {}; renderAll(); }
    catch (e) { toast(e.message, 'ng'); }
  };

  $('#btnSettings').onclick = () => openSettings();
  $('#btnHelp').onclick = () => openHelp();
  $('#ttChip').onclick = () => openSettings();

  // 表の上
  $('#search').oninput = e => { S.search = e.target.value; renderRows(); };
  $('#showPass').onchange = e => { S.showPass = e.target.checked; applyMask(); };
  $('#addRow').onclick = () => {
    const last = S.profile.hosts[S.profile.hosts.length - 1];
    const h = newHost(last ? { group: last.group, proto: last.proto, cmdSet: last.cmdSet } : {});
    S.profile.hosts.push(h); S.search = ''; $('#search').value = '';
    touch(); renderGroups(); renderRows();
    const tr = $(`#rows tr[data-id="${h.id}"]`); if (tr) { tr.scrollIntoView({ block: 'nearest' }); $('[data-f=name]', tr).focus(); }
  };
  $('#delRows').onclick = async () => {
    const vis = visibleHosts().filter(h => h.on);
    if (!vis.length) { toast('チェックの入った行がありません'); return; }
    if (!await askConfirm('行の削除', `チェックの入った ${vis.length} 行を削除します。よろしいですか？`, '削除する', true)) return;
    const ids = new Set(vis.map(h => h.id));
    S.profile.hosts = S.profile.hosts.filter(h => !ids.has(h.id));
    touch(); renderGroups(); renderRows();
  };
  $('#chkAll').onchange = e => { visibleHosts().forEach(h => h.on = e.target.checked); touch(); renderRows(); };
  $('#importCsv').onclick = () => $('#csvFile').click();
  $('#csvFile').onchange = async e => {
    const f = e.target.files[0]; e.target.value = '';
    if (!f) return;
    importHosts(parseCsv(decodeBytes(await f.arrayBuffer())), 'CSV読み込み');
  };
  $('#exportCsv').onclick = async () => {
    try {
      const r = await fetch('/api/export-csv', { method: 'POST', headers: { 'X-Token': TOKEN }, body: JSON.stringify(S.profile) });
      if (!r.ok) throw new Error('書き出しに失敗しました');
      const a = document.createElement('a');
      a.href = URL.createObjectURL(await r.blob()); a.download = 'host_list.csv'; a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 5000);
    } catch (e) { toast(e.message, 'ng'); }
  };
  $('#groups').onclick = e => {
    const b = e.target.closest('button'); if (!b) return;
    const g = b.dataset.g, on = b.dataset.on;
    S.profile.hosts.forEach(h => {
      const mine = (h.group || '').trim() === g;
      if (on === 'only') h.on = mine; else if (mine) h.on = on === '1';
    });
    touch(); renderRows();
  };

  // 表の中（イベントはまとめて tbody で受ける）
  const rows = $('#rows');
  const onEdit = e => {
    const t = e.target, f = t.dataset.f; if (!f) return;
    const h = hostById(t.closest('tr').dataset.id); if (!h) return;
    if (f === 'on') { h.on = t.checked; t.closest('tr').classList.toggle('off', !h.on); renderCounts(); }
    else if (f === 'proto') { h.proto = t.value; }
    else if (f === 'cmdSet') { h.cmdSet = t.value; t.classList.remove('missing'); }
    else if (f === 'devType') { h.devType = t.value; t.className = 'dt dt-' + t.value; }
    else { h[f] = t.value; }
    if (f === 'ip') renderBar();
    touch();
  };
  rows.addEventListener('input', e => { if (e.target.tagName === 'INPUT' && e.target.type !== 'checkbox') onEdit(e); });
  rows.addEventListener('change', e => {
    if (e.target.tagName === 'SELECT' || e.target.type === 'checkbox') onEdit(e);
    else if (e.target.dataset.f === 'group') renderGroups();
    else if (e.target.dataset.f === 'ip') { e.target.value = e.target.value.trim(); onEdit(e); }
  });
  rows.addEventListener('click', async e => {
    const a = e.target.closest('[data-open]');
    if (a) { api('open', { path: a.dataset.open }).catch(err => toast(err.message, 'ng')); return; }
    // 長いエラー文は欄に収まらないので、クリックで全文を出す
    const m = e.target.closest('[data-msg]');
    if (m) { const h = hostById(m.closest('tr').dataset.id); modal({ title: `詳細 — ${h.name || h.ip}`, body: `<div>${esc(m.dataset.msg)}</div>` }); return; }
    const b = e.target.closest('[data-act]'); if (!b) return;
    const h = hostById(b.closest('tr').dataset.id);
    if (b.dataset.act === 'detail') openDetail(h);
    else if (b.dataset.act === 'del') { S.profile.hosts = S.profile.hosts.filter(x => x !== h); touch(); renderGroups(); renderRows(); }
  });
  // Enterで下のセルへ。最終行ならそのまま1行足す
  rows.addEventListener('keydown', e => {
    if (e.key !== 'Enter' || e.target.tagName !== 'INPUT' || e.target.type === 'checkbox' || e.isComposing) return;
    e.preventDefault();
    const tr = e.target.closest('tr'), f = e.target.dataset.f;
    let next = e.shiftKey ? tr.previousElementSibling : tr.nextElementSibling;
    if (!next && !e.shiftKey && !S.search) { $('#addRow').click(); next = rows.lastElementChild; }
    if (next) { const el = $(`[data-f="${f}"]`, next); el.focus(); if (el.select) el.select(); }
  });
  document.addEventListener('paste', handlePaste);

  // コマンドセット
  $('#setSel').onchange = async e => {
    if (S.setDirty && !await askConfirm('未保存の変更', 'コマンドの変更が保存されていません。破棄して切り替えますか？', '破棄して切り替え', true)) { e.target.value = S.curSet; return; }
    S.curSet = e.target.value; renderSets();
  };
  $('#setText').oninput = () => { S.setDirty = true; renderSetInfo(); };
  $('#setSave').onclick = () => saveSet().then(() => toast('コマンドセットを保存しました', 'ok')).catch(e => toast(e.message, 'ng'));
  $('#setNew').onclick = async () => {
    const name = await askText('新しいコマンドセット', '名前（例: command_L3.txt）', '', '作成');
    if (!name) return;
    const real = /\.[A-Za-z0-9]+$/.test(name) ? name : name + '.txt';
    if (S.cmdSets.find(c => c.name.toLowerCase() === real.toLowerCase())) { toast('同じ名前のコマンドセットが既にあります', 'ng'); return; }
    const keep = S.setDirty ? $('#setText').value : 'terminal length 0\n';
    try { $('#setText').value = keep; await saveSet(real); $('#setText').focus(); }
    catch (e) { toast(e.message, 'ng'); }
  };
  $('#setDel').onclick = async () => {
    const used = S.profile.hosts.filter(h => h.cmdSet === S.curSet).length;
    if (!await askConfirm('コマンドセットの削除', `「${esc(S.curSet)}」を削除します。${used ? `<br>このプロファイルで ${used} 台が使用中です（未登録の表示になります）。` : ''}`, '削除する', true)) return;
    try {
      const d = await api('cmdset/delete', { name: S.curSet });
      S.cmdSets = d.cmdSets;
      if (S.profile.common.cmdSet === S.curSet) { S.profile.common.cmdSet = S.cmdSets[0].name; touch(); }
      S.curSet = S.cmdSets[0].name; renderSets(); renderRows();
    } catch (e) { toast(e.message, 'ng'); }
  };
  $('#setImport').onclick = () => $('#setFile').click();
  $('#setFile').onchange = async e => {
    const f = e.target.files[0]; e.target.value = '';
    if (!f) return;
    const text = decodeBytes(await f.arrayBuffer()).replace(/\r\n/g, '\n');
    const exist = S.cmdSets.find(c => c.name.toLowerCase() === f.name.toLowerCase());
    if (exist && !await askConfirm('ファイル取込', `「${esc(f.name)}」は既にあります。上書きしますか？`, '上書きする')) return;
    try { $('#setText').value = text; await saveSet(exist ? exist.name : f.name); toast(`「${f.name}」を取り込みました`, 'ok'); }
    catch (err) { toast(err.message, 'ng'); }
  };
  $('#setApplyChecked').onclick = () => {
    const list = S.profile.hosts.filter(h => h.on);
    if (!list.length) { toast('チェックの入ったホストがありません'); return; }
    const v = S.curSet === S.profile.common.cmdSet ? '' : S.curSet;
    list.forEach(h => h.cmdSet = v);
    touch(); renderRows(); toast(`${list.length}台のコマンドセットを「${S.curSet}」にしました`, 'ok');
  };
  $('#setDefault').onclick = () => {
    S.profile.common.cmdSet = S.curSet;
    touch(); const keep = $('#setText').value, dirty = S.setDirty;
    renderSets(); $('#setText').value = keep; S.setDirty = dirty; renderSetInfo(); renderRows();
    toast(`「${S.curSet}」を共通の既定にしました`, 'ok');
  };

  // 取得設定
  const bindText = (id, set, after) => $('#' + id).addEventListener('input', e => { set(e.target.value); touch(); if (after) after(); });
  bindText('ident', v => S.profile.ident = v, renderMode);
  bindText('logDir', v => S.profile.logDir = v);
  bindText('jIP', v => S.profile.jump.ip = v.trim());
  bindText('jUser', v => S.profile.jump.user = v);
  bindText('jPass', v => S.profile.jump.pass = v);
  bindText('cUser', v => S.profile.common.user = v, refreshPlaceholders);
  bindText('cPass', v => S.profile.common.pass = v, refreshPlaceholders);
  bindText('cEnable', v => S.profile.common.enable = v, refreshPlaceholders);
  $('#jPort').oninput = e => { S.profile.jump.port = +e.target.value || 0; touch(); };
  $('#jProto').onchange = e => { S.profile.jump.proto = e.target.value; touch(); };
  $('#conc').onchange = e => { let v = parseInt(e.target.value, 10) || 10; v = Math.min(200, Math.max(1, v)); e.target.value = v; S.profile.concurrency = v; touch(); };
  $('#modeSeg').onclick = e => { const b = e.target.closest('button'); if (!b || b.disabled) return; S.profile.mode = b.dataset.v; touch(); renderMode(); };
  $('#jumpOn').onchange = e => { S.profile.jump.enabled = e.target.checked; touch(); renderMode(); };
  $('#logPick').onclick = async () => {
    try { const d = await api('pick', { kind: 'folder' }); if (d.path) { S.profile.logDir = d.path; $('#logDir').value = d.path; touch(); } }
    catch (e) { toast(e.message, 'ng'); }
  };
  const openLog = () => api('open', { path: S.profile.logDir, create: true }).catch(e => toast(e.message, 'ng'));
  $('#logOpen').onclick = openLog; $('#btnLogDir').onclick = openLog;

  // 実行バー
  $('#btnStart').onclick = () => startRun(targetIds());
  $('#btnAbort').onclick = async () => {
    if (!await askConfirm('取得の中断', '実行中のTera Termをすべて閉じて中断します。よろしいですか？', '中断する', true)) return;
    api('abort').catch(e => toast(e.message, 'ng'));
  };
  $('#btnRetry').onclick = () => {
    const ids = S.job.hosts.filter(h => h.status === 'error' || h.status === 'aborted').map(h => h.id).filter(id => hostById(id));
    if (ids.length) startRun(ids);
  };
  $('#btnCheck').onclick = async () => {
    const b = $('#btnCheck'); b.disabled = true; b.textContent = '確認中…';
    try {
      const res = await api('precheck', { profile: S.profile, ids: targetIds() });
      S.job = null; S.pre = {}; res.forEach(r => S.pre[r.id] = r);
      const ng = res.filter(r => !r.ok).length;
      renderStatus(); renderBar();
      toast(ng ? `${res.length}台中 ${ng}台に届きませんでした` : `${res.length}台すべて到達できました`, ng ? 'ng' : 'ok');
      if (S.profile.jump.enabled) toast('踏み台経由の設定です。このチェックは手元PCから対象への直接到達性を見ています。');
    } catch (e) { toast(e.message, 'ng'); }
    b.textContent = '疎通チェック'; renderBar();
  };
  $('#btnResult').onclick = saveResultCsv;
  $('#btnDebug').onclick = () => api('open', { path: S.job.debugDir }).catch(e => toast(e.message, 'ng'));
  $('#dbgChip').onclick = () => openSettings();

  // 実行中はうっかり閉じないように
  window.addEventListener('beforeunload', e => { if (running()) { e.preventDefault(); e.returnValue = ''; } });
  setInterval(() => { if (running()) renderBar(); }, 1000);
}

function refreshPlaceholders() {
  const c = S.profile.common;
  $$('#rows tr').forEach(tr => {
    $('[data-f=user]', tr).placeholder = c.user ? '共通' : '';
    $('[data-f=pass]', tr).placeholder = c.pass ? '共通' : '';
    $('[data-f=enable]', tr).placeholder = c.enable ? '共通' : '';
  });
}

init().catch(e => {
  document.body.innerHTML = `<div style="padding:40px;line-height:1.8"><h2>起動できませんでした</h2><p>${esc(e.message)}</p><p>アプリを一度閉じて、もう一度起動してください。</p></div>`;
});
