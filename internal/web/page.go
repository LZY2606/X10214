package web

import "net/http"

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(page))
}

const page = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>捕获轨迹室</title>
<style>
:root { --bg:#0f1420; --panel:#1a2233; --fg:#dbe4f0; --acc:#5ec1ff; --warn:#ffb454; --bad:#ff6b6b; --ok:#6be08a; }
* { box-sizing: border-box; }
body { margin:0; font:14px/1.5 ui-monospace,Menlo,monospace; background:var(--bg); color:var(--fg); }
header { padding:14px 20px; border-bottom:1px solid #2a3550; display:flex; align-items:baseline; gap:16px; }
header h1 { margin:0; font-size:20px; color:var(--acc); }
header .ver { color:#8fa3c0; font-size:12px; }
main { display:grid; grid-template-columns: 340px 1fr; gap:12px; padding:12px; }
section { background:var(--panel); border:1px solid #2a3550; border-radius:8px; padding:12px; margin-bottom:12px; }
h2 { margin:0 0 8px; font-size:14px; color:var(--acc); }
label { display:block; margin:6px 0 2px; color:#8fa3c0; font-size:12px; }
input[type=text], textarea { width:100%; background:#0c111c; color:var(--fg); border:1px solid #2a3550; border-radius:4px; padding:6px; font:inherit; }
button { background:#24406b; color:var(--fg); border:1px solid #35548c; border-radius:4px; padding:6px 12px; margin:4px 4px 0 0; cursor:pointer; font:inherit; }
button:hover { background:#2d5086; }
table { border-collapse:collapse; width:100%; font-size:12px; }
th, td { border-bottom:1px solid #2a3550; padding:3px 6px; text-align:left; vertical-align:top; }
.status-match { color:var(--ok); } .status-no-match { color:var(--warn); } .status-indeterminate { color:var(--bad); }
.ev-capture-set { color:var(--ok); } .ev-capture-rollback { color:var(--bad); } .ev-fail { color:#a06; }
.ev-branch { color:var(--acc); } .ev-pruned { color:var(--warn); } .ev-match { color:var(--ok); font-weight:bold; }
.ast ul { margin:0 0 0 16px; padding:0; } .ast li { list-style:none; }
.ast .k { color:var(--acc); } .ast .l { color:#9fb4d0; }
.prog div { padding:1px 6px; border-radius:3px; cursor:pointer; white-space:pre; }
.prog div.bp { background:#5a2a2a; } .prog div:hover { background:#22304d; }
#trace { max-height:420px; overflow:auto; }
.grid2 { display:grid; grid-template-columns:1fr 1fr; gap:12px; }
.pill { display:inline-block; padding:1px 8px; border-radius:10px; background:#24406b; margin:2px; font-size:12px; }
</style>
</head>
<body>
<header>
  <h1>捕获轨迹室</h1>
  <span class="ver" id="versions">语法版本 / Unicode 数据版本：运行后钉住</span>
</header>
<main>
<div>
  <section>
    <h2>执行设置</h2>
    <label>Pattern</label>
    <input type="text" id="pattern" value="(?P<word>\w+)(?=\s*\d)">
    <label>输入</label>
    <input type="text" id="input" value="abc 123">
    <div class="grid2">
      <div><label>步数预算</label><input type="text" id="maxSteps" value="0"></div>
      <div><label>状态预算</label><input type="text" id="maxStates" value="0"></div>
      <div><label>捕获快照预算</label><input type="text" id="maxSaves" value="0"></div>
      <div><label>断点 pc（逗号分隔）</label><input type="text" id="breaks" value=""></div>
    </div>
    <label><input type="checkbox" id="optimize"> 应用保序优化（jump threading）</label>
    <div>
      <button onclick="doRun()">运行</button>
      <button onclick="doStep(1)">单步 +1</button>
      <button onclick="doStep(10)">单步 +10</button>
      <button onclick="doStep(1000000)">从 frontier 继续到底</button>
    </div>
  </section>
  <section>
    <h2>结果</h2>
    <div id="result">尚未运行</div>
  </section>
  <section>
    <h2>语义差分</h2>
    <label>Pattern A</label><input type="text" id="diffA" value="a+">
    <label>Pattern B</label><input type="text" id="diffB" value="a*">
    <label>最大长度</label><input type="text" id="diffLen" value="4">
    <button onclick="doDiff()">求有限反例</button>
    <div id="diffOut"></div>
  </section>
  <section>
    <h2>历史运行</h2>
    <div id="runs"></div>
  </section>
</div>
<div>
  <section class="ast"><h2>AST</h2><div id="ast">—</div></section>
  <section><h2>指令（点击设置/取消断点）</h2><div class="prog" id="prog">—</div></section>
  <section><h2>活跃状态 / frontier</h2><div id="frontier">—</div></section>
  <section><h2>轨迹（捕获变化、分支、回退、剪枝）</h2><div id="trace">—</div></section>
</div>
</main>
<script>
let runID = null;
let breakpoints = new Set();
let allEvents = [];

function bpList() { return Array.from(breakpoints).sort((a,b)=>a-b); }

async function api(path, body) {
  const r = await fetch(path, {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body)});
  const j = await r.json();
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}

function budget() {
  return {
    maxSteps: parseInt(document.getElementById('maxSteps').value)||0,
    maxStates: parseInt(document.getElementById('maxStates').value)||0,
    maxSaves: parseInt(document.getElementById('maxSaves').value)||0,
  };
}

async function doRun() {
  breakpoints = new Set((document.getElementById('breaks').value||'').split(',').map(s=>parseInt(s.trim())).filter(n=>!isNaN(n)));
  allEvents = [];
  try {
    const j = await api('/api/run', {
      pattern: document.getElementById('pattern').value,
      input: document.getElementById('input').value,
      optimize: document.getElementById('optimize').checked,
      budget: budget(), breaks: bpList(),
    });
    runID = j.runID;
    allEvents = j.events || [];
    render(j);
  } catch(e) { document.getElementById('result').textContent = '错误: ' + e.message; }
}

async function doStep(n) {
  if (runID === null) { await doRun(); return; }
  try {
    const j = await api('/api/step', {runID, steps:n, breaks: bpList()});
    allEvents = allEvents.concat(j.events || []);
    render(j);
  } catch(e) { document.getElementById('result').textContent = '错误: ' + e.message; }
}

async function doDiff() {
  const out = document.getElementById('diffOut');
  try {
    const j = await api('/api/diff', {
      patternA: document.getElementById('diffA').value,
      patternB: document.getElementById('diffB').value,
      maxLen: parseInt(document.getElementById('diffLen').value)||4,
    });
    let h = '<p>' + esc(j.note) + '</p>';
    if (j.counterexamples && j.counterexamples.length) {
      h += '<p>反例（按长度再按字典序）：</p>' + j.counterexamples.map(c=>'<span class="pill">'+esc(JSON.stringify(c))+'</span>').join('');
    } else {
      h += '<p>预算范围内无反例。</p>';
    }
    out.innerHTML = h;
  } catch(e) { out.textContent = '错误: ' + e.message; }
}

function esc(s) { return String(s).replace(/[&<>"]/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])); }

function renderAST(n) {
  let h = '<li><span class="k">'+esc(n.kind)+'</span> <span class="l">'+esc(n.label||'')+'</span>';
  if (n.children && n.children.length) {
    h += '<ul>' + n.children.map(renderAST).join('') + '</ul>';
  }
  return h + '</li>';
}

function render(j) {
  document.getElementById('versions').textContent =
    '语法版本 ' + j.syntaxVersion + ' · Unicode 数据版本 ' + j.unicodeVersion;
  let r = '<div class="status-'+j.status+'">状态: ' + j.status + (j.reason? ' ('+esc(j.reason)+')':'') + '</div>';
  r += '<div>计数: steps='+j.counters.steps+' states='+j.counters.states+' saves='+j.counters.saves+'</div>';
  if (j.groups && j.groups.length) {
    r += '<table><tr><th>组</th><th>名</th><th>cp</th><th>byte</th><th>文本</th></tr>' +
      j.groups.map(g=>'<tr><td>'+g.index+'</td><td>'+esc(g.name)+'</td><td>['+g.startCP+','+g.endCP+')</td><td>['+g.startByte+','+g.endByte+')</td><td>'+esc(g.text)+'</td></tr>').join('') + '</table>';
  }
  document.getElementById('result').innerHTML = r;
  document.getElementById('ast').innerHTML = '<ul>'+renderAST(j.ast)+'</ul>';
  document.getElementById('prog').innerHTML = j.program.map((line,i)=>{
    const pc = parseInt(line);
    return '<div data-pc="'+pc+'" class="'+(breakpoints.has(pc)?'bp':'')+'" onclick="toggleBP('+pc+')">'+esc(line)+'</div>';
  }).join('');
  let f = '—';
  if (j.frontier) {
    const states = (j.frontier.stack||[]).map(s=>'pc='+s.pc+' pos='+s.pos).join(' · ');
    f = '<p>待探索状态数: ' + ((j.frontier.stack||[]).length + (j.frontier.cur?1:0)) + '</p><p>'+esc(states)+'</p>';
  } else {
    const pruned = allEvents.filter(e=>e.kind==='pruned').length;
    f = '<p>执行已结束。被剪枝分支: ' + pruned + '</p>';
  }
  document.getElementById('frontier').innerHTML = f;
  document.getElementById('trace').innerHTML = '<table><tr><th>#</th><th>step</th><th>事件</th><th>pc</th><th>cp</th><th>byte</th><th>详情</th></tr>' +
    allEvents.map((e,i)=>'<tr class="ev-'+e.kind+'"><td>'+i+'</td><td>'+e.step+'</td><td>'+esc(e.kind)+'</td><td>'+e.pc+'</td><td>'+e.cp+'</td><td>'+e.byte+'</td><td>'+esc(e.detail)+'</td></tr>').join('') + '</table>';
}

function toggleBP(pc) {
  if (breakpoints.has(pc)) breakpoints.delete(pc); else breakpoints.add(pc);
  document.getElementById('breaks').value = bpList().join(',');
  document.querySelectorAll('.prog div').forEach(d=>{
    d.classList.toggle('bp', breakpoints.has(parseInt(d.dataset.pc)));
  });
}

async function loadRuns() {
  try {
    const r = await fetch('/api/runs');
    const runs = await r.json();
    document.getElementById('runs').innerHTML = (runs||[]).map(x=>
      '<div>#'+x.ID+' <span class="status-'+x.Status+'">'+x.Status+'</span> '+esc(x.Pattern)+' / '+esc(x.Input)+'</div>').join('') || '—';
  } catch(e) {}
}
loadRuns();
setInterval(loadRuns, 5000);
</script>
</body>
</html>
`
