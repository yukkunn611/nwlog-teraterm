// 使い方・利用規約・免責事項の本文
// 画面の「💡使い方」と初回起動時の同意画面で使う。GitHub の TERMS.md と内容をそろえること
'use strict';

const DOCS = {};

// ---------------- 利用規約 ----------------
DOCS.terms = `
<h3>利用規約</h3>
<p>この利用規約（以下「本規約」）は、「NWログ取得マクロ（TeraTerm）」（以下「本ソフトウェア」）の利用条件を定めるものです。本ソフトウェアを利用した時点で、本規約に同意したものとみなします。</p>

<h4>第1条（利用許諾）</h4>
<p>本ソフトウェアは MIT License のもとで無償公開しています。個人・法人を問わず、どなたでも無償で利用・複製・改変・再配布できます。再配布する場合は、著作権表示と MIT License の全文を含めてください。</p>

<h4>第2条（利用者の責任）</h4>
<ul>
<li>本ソフトウェアで接続する機器について、利用者が接続・操作する正当な権限を持っていることを確認したうえで利用してください。</li>
<li>本ソフトウェアは、利用者が設定したコマンドをそのまま機器に送ります。送るコマンドの内容と、その結果（設定の変更、負荷、通信への影響など）は利用者の責任となります。</li>
<li>パスワードなどの認証情報は、本ソフトウェアのフォルダ内（data フォルダ）に暗号化されずに保存されます。PCやフォルダの管理、取得したログの取り扱いは、利用者の所属組織の規程に従って適切に行ってください。</li>
</ul>

<h4>第3条（禁止事項）</h4>
<ul>
<li>権限のない機器・ネットワークへの接続、その他の不正アクセス行為</li>
<li>法令または公序良俗に反する目的での利用</li>
<li>第三者の権利を侵害する行為</li>
</ul>

<h4>第4条（サポート）</h4>
<p>本ソフトウェアは無償で提供しており、開発者は問い合わせへの回答、不具合の修正、機能追加などを行う義務を負いません。不具合の報告や要望は GitHub の Issues で受け付けますが、対応を約束するものではありません。</p>

<h4>第5条（提供の変更・終了）</h4>
<p>開発者は、予告なく本ソフトウェアの内容を変更し、または公開・提供を終了することがあります。</p>

<h4>第6条（規約の変更）</h4>
<p>開発者は本規約を変更することがあります。変更後の規約は、本ソフトウェア内または GitHub に掲載した時点から効力を持ちます。内容を大きく変えた場合は、本ソフトウェアの起動時にあらためて同意を求めます。</p>

<h4>第7条（準拠法）</h4>
<p>本規約は日本法に準拠し、日本法に従って解釈されます。</p>
`;

// ---------------- 免責事項 ----------------
DOCS.disclaimer = `
<h3>免責事項</h3>
<ul>
<li>本ソフトウェアは「現状のまま」提供され、明示・黙示を問わず、商品性、特定目的への適合性、正確性、継続性、不具合がないことなどについて、一切の保証をしません。</li>
<li>本ソフトウェアの利用または利用できなかったことによって生じたいかなる損害（機器の停止や再起動、設定の変更・消失、通信障害、業務の中断、データの消失、情報漏えい、取得したログの欠落や誤りなどを含みますが、これらに限りません）についても、開発者は責任を負いません。</li>
<li>本ソフトウェアは、機器の応答を文字列で判定して動作します。機器の種類・バージョン・設定によっては、意図どおりに動作しない場合があります。重要な作業の前には、少数の機器で動作を確認してください。</li>
<li>「SSH初回接続の確認ダイアログを出さない」（既定でON）を有効にすると、接続先のホスト鍵の確認が省略されます。信頼できるネットワークでのみ使用してください。</li>
<li>機器種別「YAMAHA RTX」の既定の前処理（console lines infinity）は、機器の動作中の設定を変更します（保存はしません）。必要に応じて詳細設定で変更してください。</li>
<li>本ソフトウェアは Tera Term を同梱していません。Tera Term は利用者が各自で入手し、そのライセンスに従って利用してください。</li>
</ul>

<h3>通信とプライバシー</h3>
<ul>
<li>本ソフトウェアの画面は、このPCの中（127.0.0.1）だけで動作し、外部からは接続できません。</li>
<li>外部への通信は、更新の確認と更新ファイルのダウンロードのために GitHub（api.github.com / github.com）へアクセスする場合のみです。ホストリスト・認証情報・ログなどを外部へ送信することはありません。</li>
<li>更新の確認は、詳細設定の「起動時に新しいバージョンを確認する」をOFFにすると止められます。</li>
</ul>

<h3>商標について</h3>
<p>Cisco、Cisco IOS は Cisco Systems, Inc.、FortiGate は Fortinet, Inc.、YAMAHA はヤマハ株式会社、Windows、Microsoft Edge は Microsoft Corporation の、それぞれ米国その他の国における商標または登録商標です。Tera Term は TeraTerm Project の著作物です。本ソフトウェアは、これらの企業・団体とは関係のない個人が開発したものです。</p>
`;

// ---------------- ライセンス ----------------
DOCS.license = `
<h3>ライセンス（MIT License）</h3>
<pre class="license">MIT License

Copyright (c) 2026 Yuki Kayamura

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.</pre>
<p class="note">本ソフトウェアは Go の標準ライブラリ（BSD-3-Clause License, Copyright The Go Authors）を使用しています。</p>
`;

// ---------------- 使い方 ----------------
// [id, 目次の見出し, 本文]
DOCS.help = [
['start', 'はじめに', `
<p class="lead">ホストリストの編集・コマンドの選択・取得開始を1画面で行え、複数台のログを同時に取得できます。</p>
<div class="cards">
  <div class="hcard"><b>⚡ 並列取得</b><span>最大200台を同時に取得。show tech のような長いコマンドも最後まで取れます。</span></div>
  <div class="hcard"><b>🧩 機器種別</b><span>Cisco IOS・FortiGate・YAMAHA RTX のプロンプトや特権モードの入り方に対応。</span></div>
  <div class="hcard"><b>📁 プロファイル</b><span>拠点・案件ごとのホストリストをプルダウンで切り替え。</span></div>
</div>
`],
['launch', '起動と初期設定', `
<h4>起動のしかた</h4>
<ul>
<li>「NWログ取得マクロ（TeraTerm）.exe」をダブルクリックするだけです（インストール不要）。</li>
<li>初回は「WindowsによってPCが保護されました」と出ることがあります。<b>「詳細情報」→「実行」</b>で起動できます（署名のないEXEのため）。</li>
<li>画面は Microsoft Edge（無ければ Chrome）の枠だけのウィンドウで表示されます。通信はこのPCの中（127.0.0.1）だけです。</li>
<li>ウィンドウを閉じるとアプリも数秒で終了します。</li>
</ul>
<h4>最初に1回だけ行う設定</h4>
<ol>
<li>右上の「⚙ 詳細設定」を開きます。</li>
<li>Tera Term の <code>ttpmacro.exe</code> の場所を指定します。標準の場所にインストールされていれば自動で見つかります。</li>
<li>場所を指定すると 4系 / 5系 を自動で判定します。合わなければ手動で切り替えてください。</li>
</ol>
`],
['hosts', '① ホストリスト', `
<ul>
<li>「＋ 行を追加」で1台ずつ入力します。<kbd>Enter</kbd> で下の行へ進みます。</li>
<li>Excelの範囲をコピーし、表のセルを選んで <kbd>Ctrl</kbd>+<kbd>V</kbd> で貼り付けできます。</li>
<li>これまでの <code>host_list.csv</code> は「CSV読み込み」でそのまま取り込めます。</li>
<li>左端のチェックが取得対象です（旧マクロの「;」読み飛ばしに相当）。</li>
<li>グループ欄を入れておくと、グループ単位で ON / OFF / のみ を切り替えられます。</li>
<li>ユーザ / パスワード / enable が空欄のホストは、右下「共通の認証情報」の値を使います。</li>
<li><b>機器種別</b>の列で Cisco IOS / FortiGate / YAMAHA RTX / 汎用 を選びます。プロンプトや特権モードの入り方が種別ごとに切り替わります。</li>
</ul>
<h4>ホストごとの個別設定（行の ⚙）</h4>
<p>空欄の項目は、機器種別の既定値（詳細設定で変更可）を使います。</p>
<table class="doc">
<tr><th>ポート番号・タイムアウト</th><td>そのホストだけ別の値にしたいとき</td></tr>
<tr><th>ユーザ名 / パスワードの入力待ち</th><td>ログイン時に待つ文字列（例: <code>sername:</code> <code>ogin:</code> <code>assword:</code>）</td></tr>
<tr><th>ユーザモード / 特権モードの記号</th><td>プロンプトの末尾の記号（例: <code>&gt;</code> <code>#</code> <code>$</code>）</td></tr>
<tr><th>特権モードに移行するコマンド</th><td>Cisco は <code>enable</code>、YAMAHA RTX は <code>administrator</code>。手入力もできます</td></tr>
<tr><th>前処理コマンド</th><td>ログを取り始める前に送るコマンド（ログには残りません）。例: <code>terminal length 0</code></td></tr>
<tr><th>時刻確認コマンド</th><td>ログの先頭で時刻を記録するコマンド。例: <code>show clock</code> <code>execute time</code></td></tr>
<tr><th>コマンド間の区切り</th><td>各コマンドの前に送る区切り（例: <code>!</code>）</td></tr>
</table>
<p>同じ画面の「このホストのマクロを確認」で、実際に流れるマクロの中身も確認できます（パスワードは伏せ字）。</p>
`],
['commands', '② 取得コマンド', `
<ul>
<li>コマンドセット（command.txt など）を作成・編集・保存できます。1行に1コマンドです。</li>
<li>ホストごとに表の「コマンドセット」列で選べます。「チェックしたホストに適用」でまとめて切り替えられます。</li>
<li>FortiGate や YAMAHA RTX 用には、それぞれ専用のコマンドセットを作っておくと便利です。</li>
</ul>
`],
['settings', '③ 取得設定', `
<table class="doc">
<tr><th>ログ識別子</th><td>ログファイル名の先頭に付きます（旧マクロの入力ボックスに相当）。</td></tr>
<tr><th>ログ保存先</th><td>起動時とプロファイル切り替え時に、フォルダがあるか確認します。見つからない場合はアプリと同じ場所の Log フォルダに自動で切り替えます。</td></tr>
<tr><th>取得方式</th><td>並列取得 / 連続取得。並列のときは同時接続数（初期値10、最大200）を指定します。</td></tr>
<tr><th>踏み台</th><td>ONにすると、踏み台にログインしてから対象へ telnet / ssh します。踏み台を使うときは連続取得のみです。</td></tr>
</table>
`],
['run', '取得の実行', `
<p>下の「▶ 取得開始」で実行します。進み具合と結果は表の「状態」列に出ます。</p>
<table class="doc">
<tr><th>失敗したホストだけ再実行</th><td>失敗・中断した機器だけ取り直します。</td></tr>
<tr><th>疎通チェック</th><td>22 / 23番ポートに届くかだけ先に確認します。</td></tr>
<tr><th>結果CSVを保存</th><td>成功 / 失敗の一覧をログ保存先にCSVで保存します。</td></tr>
<tr><th>■ 中断</th><td>実行中のTera Termをすべて閉じて止めます。</td></tr>
</table>
<h4>ログファイル</h4>
<p>ファイル名は <code>識別子_ホスト名_YYYYMMDD-HHMMSS.txt</code>（旧マクロと同じ）です。中身は、時刻確認コマンドのあと、区切りを挟んでコマンドを順に実行した結果です。</p>
`],
['devtypes', '機器種別', `
<p>既定値は「⚙ 詳細設定」の「機器種別ごとの既定値」で変更できます。</p>
<table class="doc">
<tr><th>Cisco IOS</th><td><code>enable</code> で特権モードへ。前処理 <code>terminal length 0</code>、時刻確認 <code>show clock</code>、区切り <code>!</code></td></tr>
<tr><th>FortiGate</th><td>特権モードへの移行なし（ログイン直後から <code>#</code>）。「ホスト名 # 」形式やVDOM表記のプロンプトに対応。<code>--More--</code> は自動でスペースを送って進めます（機器の設定は変えません）。時刻確認 <code>execute time</code></td></tr>
<tr><th>YAMAHA RTX</th><td><code>administrator</code> で管理ユーザーへ。前処理 <code>console lines infinity</code>（保存はしません）、時刻確認 <code>show environment</code></td></tr>
<tr><th>汎用</th><td>特権モードへの移行・前処理・時刻確認なし。その他の機器向けの出発点です。</td></tr>
</table>
`],
['changes', '旧マクロから変わった点', `
<ul>
<li>いままでは連続取得（1台ずつ取得）のみでしたが、1台ごとにマクロを生成することで、複数台のログを同時に取得できるようになりました（マクロは終わったら自動で削除します）。</li>
<li>プロファイル機能により、プルダウンで素早く別拠点のホストリストに切り替えられるようになりました。</li>
<li>Cisco 以外に FortiGate・YAMAHA RTX にも対応しました。</li>
</ul>
`],
['caution', '注意', `
<ul>
<li>パスワードは data フォルダ内に<b>平文で保存</b>されます（旧マクロのCSVと同じ扱い）。他の人にアプリを渡す場合は、data フォルダを含めないでください（情報漏えいの原因になります）。</li>
<li>実行中は一時フォルダ（<code>%TEMP%\\nwlog_tt</code>）にパスワード入りのマクロが置かれます。終了時・次回起動時に削除します。</li>
<li>「SSH初回接続の確認ダイアログを出さない」は既定でONです。ホスト鍵の確認が省かれるため、信頼できるネットワークでのみ使ってください。</li>
<li>コマンドの待ち時間は「出力が止まってからの秒数」で判定します。show tech のように何分もかかるコマンドでも、出力が流れ続けている間は打ち切りません。「コマンド無応答で打ち切り」の秒数のあいだ何も出てこないときだけ「要確認」になります。</li>
<li>1台あたりの上限時間（既定60分）を超えると、出力中でも打ち切ります。</li>
</ul>
`],
['trouble', '困ったとき', `
<h4>並列取得で接続失敗になる</h4>
<ul>
<li>詳細設定「接続」の「並列時の起動方法」を「接続完了まで待つ」にしてください（既定は速い「リンクだけ1台ずつ」）。</li>
<li>それでも失敗する場合は「接続待ち」の秒数を伸ばすか、同時接続数を減らしてください。</li>
</ul>
<h4>取得が遅い</h4>
<ul>
<li>ウィンドウ表示が「通常表示」だと、Tera Termの画面描画に時間を取られて大幅に遅くなります（15台・show tech 込みの実測で、最小化 / 非表示は約2分40秒、通常表示は約11分）。普段は「最小化」か「非表示」を使ってください。</li>
</ul>
<h4>その他</h4>
<table class="doc">
<tr><th>全台「接続失敗」になる</th><td>ウィンドウ表示を「通常表示」にして1台だけ実行し、Tera Termの画面でどこで止まっているか確認してください。</td></tr>
<tr><th>文字化け・構文エラーが出る</th><td>詳細設定のバージョン（4系 / 5系）が実際のTera Termと合っているか確認してください。</td></tr>
<tr><th>ログイン後に進まない</th><td>機器種別が合っているか、行の ⚙ で「入力待ち」「記号」を確認してください。</td></tr>
<tr><th>画面が開かない</th><td>タスクマネージャーでEXEを一度終了してから起動し直してください。</td></tr>
</table>
<h4>デバッグモード（原因調査用）</h4>
<p>詳細設定の「🐞 調査用（デバッグモード）」をONにして実行すると、<code>data\\debug\\run_日時</code> に調査用のログを残します。実行後は画面下の「🐞 デバッグログを開く」でフォルダが開きます。</p>
<table class="doc">
<tr><th>app.log</th><td>起動・接続完了・タイムアウトの時刻、取得速度、止まったときに出ていたダイアログの文面</td></tr>
<tr><th>hN_IP_trace.txt</th><td>マクロがどこまで進んだか（connectの戻り値、ログイン、特権モード、各コマンド）</td></tr>
<tr><th>hN_IP_macro.ttl</th><td>実際に流したマクロの写し（パスワードは伏せ字）</td></tr>
</table>
`],
['files', 'ファイル構成', `
<table class="doc">
<tr><th>NWログ取得マクロ（TeraTerm）.exe</th><td>本体</td></tr>
<tr><th>data\\settings.json</th><td>詳細設定</td></tr>
<tr><th>data\\profiles\\*.json</th><td>プロファイル（ホストリストと取得設定）</td></tr>
<tr><th>data\\commands\\*.txt</th><td>コマンドセット</td></tr>
<tr><th>data\\debug\\run_日時\\</th><td>デバッグモードの調査用ログ</td></tr>
<tr><th>Log\\</th><td>既定のログ保存先</td></tr>
</table>
`],
];
