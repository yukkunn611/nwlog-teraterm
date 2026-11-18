// 使い方画面・利用規約の同意・更新の確認
// app.js より先に読み込む（関数は app.js の初期化処理から呼ばれる）
'use strict';

// ---------- 更新履歴 ----------
let CHANGELOG = null;
async function loadChangelog() {
  if (CHANGELOG) return CHANGELOG;
  try { CHANGELOG = await (await fetch('changelog.json', { cache: 'no-store' })).json(); }
  catch (e) { CHANGELOG = []; }
  return CHANGELOG;
}

function changelogHTML(list) {
  return list.map(r => `<div class="rel">
    <div class="rel-head"><b>v${esc(r.version)}</b><span>${esc(r.date || '')}</span>${r.version === S.version ? '<em>使用中</em>' : ''}</div>
    <ul>${r.changes.map(c => `<li>${esc(c)}</li>`).join('')}</ul></div>`).join('');
}

// ---------- 使い方 ----------
async function openHelp(target) {
  const cl = await loadChangelog();
  const sections = DOCS.help.concat([
    ['history', '更新履歴', changelogHTML(cl)],
    ['terms', '利用規約', DOCS.terms],
    ['disclaimer', '免責事項・通信・商標', DOCS.disclaimer],
    ['license', 'ライセンス', DOCS.license],
  ]);
  const nav = sections.map(([id, t]) => `<a data-go="${id}">${esc(t)}</a>`).join('');
  const content = sections.map(([id, t, html]) => `<section id="help-${id}"><h2>${esc(t)}</h2>${html}</section>`).join('');
  const body = document.createElement('div');
  body.className = 'help';
  body.innerHTML = `<nav class="help-nav">${nav}</nav>
    <div class="help-main">${content}
      <footer class="help-foot">
        <div class="about">
          <span class="logo" aria-hidden="true"></span>
          <div><b>${esc(S.appName)}</b><span>バージョン ${esc(S.version)}</span></div>
        </div>
        <div class="foot-btns">
          <button class="btn sm" id="hUpd">更新を確認</button>
          <button class="btn sm" id="hGit">GitHub を開く</button>
        </div>
        <p class="dev">開発：${esc(S.info.author)}</p>
      </footer>
    </div>`;
  modal({
    title: '💡 使い方', size: 'helpwin', body,
    buttons: [{ label: '閉じる', cls: 'primary' }],
    onOpen: ov => {
      const main = $('.help-main', ov);
      const links = $$('.help-nav a', ov);
      const go = id => {
        const el = $('#help-' + id, ov);
        if (el) main.scrollTo({ top: el.offsetTop - main.offsetTop - 8, behavior: 'smooth' });
      };
      links.forEach(a => a.onclick = () => go(a.dataset.go));
      // 読んでいる場所を目次で強調する
      const mark = () => {
        let cur = links[0];
        for (const a of links) {
          const el = $('#help-' + a.dataset.go, ov);
          if (el.offsetTop - main.offsetTop - 40 <= main.scrollTop) cur = a;
        }
        links.forEach(a => a.classList.toggle('on', a === cur));
      };
      main.addEventListener('scroll', mark); mark();
      $('#hUpd', ov).onclick = () => checkUpdate(true);
      $('#hGit', ov).onclick = () => api('open-url', { url: 'https://github.com/' + S.info.repo }).catch(e => toast(e.message, 'ng'));
      if (target) setTimeout(() => go(target), 50);
    },
  });
}

// ---------- 利用規約への同意 ----------
function askTerms() {
  return new Promise(resolve => {
    const body = `<p>ご利用の前に、利用規約と免責事項をお読みください。</p>
      <div class="terms-box">${DOCS.terms}${DOCS.disclaimer}</div>
      <label class="chk agree"><input type="checkbox" id="tAgree"> 利用規約と免責事項を読み、内容に同意します</label>`;
    modal({
      title: '利用規約・免責事項', size: 'mid', body, sticky: true,
      buttons: [
        { label: '同意しない（終了する）', onClick: () => {
          document.body.innerHTML = `<div class="bye"><h2>${esc(S.appName)}</h2><p>利用規約に同意いただけないため、終了します。このウィンドウを閉じてください。</p></div>`;
          setTimeout(() => window.close(), 400);
        } },
        { label: '同意して使い始める', cls: 'primary', onClick: async ov => {
          if (!$('#tAgree', ov).checked) { toast('同意する場合は、チェックを入れてください'); return false; }
          await api('terms/accept');
          S.settings.termsAccepted = S.info.termsVersion;
          resolve();
        } },
      ],
      onOpen: ov => {
        const btn = $$('.modal-foot .btn', ov)[1];
        const sync = () => { btn.disabled = !$('#tAgree', ov).checked; };
        $('#tAgree', ov).onchange = sync; sync();
      },
    });
  });
}

// ---------- 更新の確認 ----------
function verParse(s) {
  const m = /^v?(\d+)\.(\d+)\.(\d+)$/.exec(String(s || '').trim());
  return m ? [+m[1], +m[2], +m[3]] : null;
}
function verCmp(a, b) {
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1;
  return 0;
}

// GitHub のリリース一覧。まず画面(Edge)から取りに行き、だめならアプリ側から取る
async function fetchReleases() {
  try {
    const r = await fetch(S.info.releasesAPI, { headers: { Accept: 'application/vnd.github+json' }, cache: 'no-store' });
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return await r.json();
  } catch (e) {
    return await api('update/check');
  }
}

// リリースノート（Markdownの箇条書き程度）を表示用にする
function notesHTML(md) {
  const out = [];
  let list = false;
  String(md || '').split(/\r?\n/).forEach(line => {
    const t = line.trim();
    const li = /^[-*・]\s*(.+)$/.exec(t);
    if (li) { if (!list) { out.push('<ul>'); list = true; } out.push(`<li>${esc(li[1])}</li>`); return; }
    if (list) { out.push('</ul>'); list = false; }
    if (!t) return;
    const h = /^#{1,6}\s*(.+)$/.exec(t);
    out.push(h ? `<h4>${esc(h[1])}</h4>` : `<p>${esc(t)}</p>`);
  });
  if (list) out.push('</ul>');
  return out.join('');
}

let updateChecking = false;
async function checkUpdate(manual) {
  if (updateChecking) return;
  updateChecking = true;
  try {
    const cur = verParse(S.version);
    const rels = (await fetchReleases() || [])
      // 公開直後でEXEの添付が終わっていないReleaseは、まだ出ていない扱いにする
      .filter(r => !r.draft && !r.prerelease && verParse(r.tag_name) && (r.assets || []).some(a => a.name === S.info.assetExe))
      .map(r => Object.assign(r, { v: verParse(r.tag_name) }))
      .sort((a, b) => verCmp(b.v, a.v));
    const latest = rels[0];
    if (!latest || !cur || verCmp(latest.v, cur) <= 0) {
      if (manual) toast(`最新版を使用中です（v${S.version}）`, 'ok');
      return;
    }
    const ver = latest.v.join('.');
    if (!manual && S.settings.skipVersion === ver) return;
    // いまの版より新しいリリースの変更点をまとめて出す
    const newer = rels.filter(r => verCmp(r.v, cur) > 0);
    showUpdate(latest, ver, newer);
  } catch (e) {
    if (manual) toast('更新を確認できませんでした: ' + e.message, 'ng', 8000);
  } finally {
    updateChecking = false;
  }
}

function showUpdate(latest, ver, newer) {
  const notes = newer.map(r => `<div class="rel">
      <div class="rel-head"><b>v${esc(r.v.join('.'))}</b><span>${esc((r.published_at || '').slice(0, 10))}</span></div>
      ${notesHTML(r.body) || '<p class="muted">（変更点の記載なし）</p>'}</div>`).join('');
  const body = `<div class="upd-ver"><span>v${esc(S.version)}</span><i>→</i><b>v${esc(ver)}</b></div>
    <p>新しいバージョンがあります。更新しますか？</p>
    <div class="upd-notes">${notes}</div>
    <p class="note">「はい」を押すと新しいバージョンをダウンロードして入れ替え、アプリを再起動します。ホストリストや設定（data フォルダ）はそのまま引き継がれます。</p>`;
  modal({
    title: '新しいバージョンのお知らせ', size: 'mid', body,
    buttons: [
      { label: 'このバージョンは再度表示しない', cls: 'left', onClick: async () => {
        await api('update/skip', { version: ver });
        S.settings.skipVersion = ver;
        toast(`v${ver} のお知らせは表示しません。使い方画面の「更新を確認」からいつでも更新できます。`, '', 7000);
      } },
      { label: 'いいえ' },
      { label: 'はい（更新する）', cls: 'primary', onClick: () => { applyUpdate(latest, ver); } },
    ],
  });
}

async function applyUpdate(rel, ver) {
  if (running()) { toast('ログ取得の実行中は更新できません。終わってからもう一度お試しください。', 'ng'); return; }
  const exe = (rel.assets || []).find(a => a.name === S.info.assetExe);
  const sha = (rel.assets || []).find(a => a.name === S.info.assetSha256);
  if (!exe || !sha) { updateFailed(rel, 'リリースに更新用のファイルが見つかりませんでした'); return; }
  const m = modal({
    title: '更新しています', sticky: true,
    body: `<div class="upd-progress"><span class="spin"></span><div><b>v${esc(ver)} をダウンロードしています…</b><p class="note">完了すると自動で再起動します。このまましばらくお待ちください。</p></div></div>`,
    buttons: [],
  });
  let firstErr = '';
  try {
    // 1) アプリ側でダウンロード（Windowsのプロキシ設定を使う）
    await api('update/apply', { exeUrl: exe.browser_download_url, shaUrl: sha.browser_download_url });
  } catch (e) {
    firstErr = e.message;
    try {
      // 2) 画面(Edge)側でダウンロードしてアプリに渡す（自動構成スクリプトのプロキシ環境向け）
      const get = async a => {
        const r = await fetch(a.url, { headers: { Accept: 'application/octet-stream' } });
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r;
      };
      const shaText = (await (await get(sha)).text()).trim();
      const blob = await (await get(exe)).arrayBuffer();
      const r = await fetch('/api/update/apply-bytes', { method: 'POST', headers: { 'X-Token': TOKEN, 'X-Sha256': shaText }, body: blob });
      const j = await r.json();
      if (!j.ok) throw new Error(j.error);
    } catch (e2) {
      m.close();
      updateFailed(rel, firstErr + (e2.message && e2.message !== firstErr ? ' / ' + e2.message : ''));
      return;
    }
  }
  $('.upd-progress', m.el).innerHTML = `<span class="done">✓</span><div><b>更新が完了しました</b><p class="note">新しいバージョンを起動しています。このウィンドウは自動で閉じます（閉じない場合は手で閉じてください）。</p></div>`;
  setTimeout(() => window.close(), 2500);
}

function updateFailed(rel, msg) {
  modal({
    title: '自動更新できませんでした',
    body: `<p>${esc(msg)}</p><p>お手数ですが、ダウンロードページから新しいEXEを取得し、いまのEXEと置き換えてください。data フォルダはそのまま使えます。</p>`,
    buttons: [
      { label: '閉じる' },
      { label: 'ダウンロードページを開く', cls: 'primary', onClick: () => { api('open-url', { url: rel.html_url || S.info.releasePage }).catch(e => toast(e.message, 'ng')); } },
    ],
  });
}
