// Capture Trace Room front-end.
(function () {
  "use strict";

  const $ = (id) => document.getElementById(id);
  let current = null;
  let frontier = null;
  let astView = null;
  let positions = null;
  let breakpoints = [];
  let shownEvents = [];

  async function api(path, body) {
    const res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || res.statusText);
    return data;
  }

  async function get(path) {
    const res = await fetch(path);
    return res.json();
  }

  function budget() {
    return {
      max_steps: +$("bSteps").value,
      max_states: +$("bStates").value,
      max_snapshots: +$("bSnaps").value,
      max_events: +$("bEvents").value,
    };
  }

  const kindTag = {
    step: ["step", "步"],
    branch: ["branch", "分支"],
    prune: ["prune", "剪枝"],
    fail: ["fail", "失败"],
    capture: ["capture", "捕获"],
    try: ["try", "尝试"],
    zero_width_stop: ["zero", "零宽终止"],
    match: ["match", "命中"],
    no_match: ["fail", "无匹配"],
    pause: ["branch", "暂停"],
    indeterminate: ["prune", "不定"],
  };

  function esc(s) {
    return String(s == null ? "" : s)
      .replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
  }

  function renderAST(node) {
    if (!node) return "";
    const label = (astView.labels || {})[node.id] || node.kind;
    const leaf = ["char", "dot", "anchor", "empty", "backref"].includes(node.kind);
    let html = '<span class="node" data-node="' + node.id + '" title="id=' + node.id + '">'
      + '<span class="k">#' + node.id + " " + esc(node.kind) + "</span> "
      + esc(label) + "</span>";
    if (node.children && node.children.length) {
      html += "(" + node.children.map(renderAST).join(" &nbsp;|&nbsp; ") + ")";
    }
    if (node.body) {
      html += " { " + renderAST(node.body) + " }";
    }
    if (leaf) return html;
    return html;
  }

  function renderPositions() {
    if (!positions) return;
    const out = [];
    for (let i = 0; i < positions.runes.length; i++) {
      const b = positions.rune_to_byte[i];
      const shown = positions.runes[i] === "\n" ? "\\n" : positions.runes[i];
      out.push('<span class="rp">r' + i + "@b" + b + "</span> " + esc(shown));
    }
    out.push('<span class="rp">r' + positions.runes.length + "@b" + positions.bytes + "</span> EOF");
    $("positions").innerHTML = out.join("  ");
    $("cursor").textContent = positions.has_invalid
      ? "输入包含无效 UTF-8 字节（以 RuneError/单字节位置呈现）"
      : "共 " + positions.runes.length + " 个 code point，" + positions.bytes + " 字节";
  }

  function renderStatus(p) {
    const r = p.result;
    const box = $("status");
    box.className = "status " + r.status.replace("_", "");
    box.textContent = "状态：" + r.status + (r.reason ? " — " + r.reason : "");

    if (r.match) {
      $("match").innerHTML = "匹配：byte [" + r.match.byte_lo + "," + r.match.byte_hi
        + ") rune [" + r.match.rune_lo + "," + r.match.rune_hi + ")";
    } else {
      $("match").textContent = "";
    }

    const cap = [];
    (r.captures || []).forEach((c, i) => {
      if (!c.matched) return;
      const text = $("input").value.slice(c.byte_lo, c.byte_hi);
      cap.push("组" + (i + 1) + " [" + c.byte_lo + "," + c.byte_hi + ")=<code>" + esc(text) + "</code>");
    });
    Object.entries(r.named || {}).forEach(([name, c]) => {
      const text = $("input").value.slice(c.byte_lo, c.byte_hi);
      cap.push("命名 " + esc(name) + " [" + c.byte_lo + "," + c.byte_hi
        + ")=<code>" + esc(text) + "</code>");
    });
    $("captures").innerHTML = cap.join("<br>");

    $("budgets").innerHTML = "钉版：syntax=" + r.syntax_version + " unicode=" + r.unicode_version
      + " ｜ 步数 " + r.budget_used.steps + "/" + r.budget.max_steps
      + " ｜ 状态 " + r.budget_used.states + "/" + r.budget.max_states
      + " ｜ 快照 " + r.budget_used.snapshots + "/" + r.budget.max_snapshots
      + (r.events_truncated ? " ｜ 事件已截断" : "");

    frontier = r.frontier || null;
    const resumable = frontier !== null;
    $("btnResume").disabled = !resumable;
    $("btnRunTo").disabled = !resumable;
    $("frontierbox").innerHTML = frontier
      ? "frontier：栈深度 " + frontier.stack.length + "，下一步试 rune=" + frontier.next_rune_pos
        + "；从该 frontier 继续不会被报告成 no-match。"
      : "";

    shownEvents = r.events || [];
    renderTrace();
  }

  function renderTrace() {
    const filter = $("filter").value;
    const rows = shownEvents.filter((e) => {
      if (filter === "all") return true;
      if (filter === "zero") return e.kind === "zero_width_stop";
      if (filter === "prune") return e.kind === "prune" || e.kind === "indeterminate";
      if (filter === "branch") return e.kind === "branch" || e.kind === "pause";
      return e.kind === filter;
    });
    const tb = $("trace").querySelector("tbody");
    tb.innerHTML = rows.map((e) => {
      const t = kindTag[e.kind] || ["step", e.kind];
      let detail = esc(e.detail || "");
      if (e.kind === "capture" && e.captures) {
        const cs = Object.entries(e.captures)
          .map(([g, s]) => g + "=[" + s.byte_lo + "," + s.byte_hi + ")").join(" ");
        detail = "slot " + e.slot + " -> " + cs;
      }
      return "<tr><td>" + e.seq + '</td><td><span class="tag ' + t[0] + '">' + t[1] + "</span></td>"
        + "<td>" + e.rune_pos + "</td><td>" + e.byte_pos + "</td><td>" + (e.node_id || "")
        + "</td><td>" + e.active + "</td><td>" + detail + "</td></tr>";
    }).join("");
  }

  async function doParse() {
    try {
      astView = await api("/api/parse", { pattern: $("pattern").value, optimize: $("optimize").checked });
      $("ast").innerHTML = renderAST(astView.root);
    } catch (e) {
      $("ast").textContent = "解析失败：" + e.message;
    }
  }

  async function updatePositions() {
    positions = await api("/api/positions", { input: $("input").value });
    renderPositions();
  }

  async function doRun(stepLimit) {
    try {
      current = await api("/api/run", {
        pattern: $("pattern").value,
        input: $("input").value,
        optimize: $("optimize").checked,
        step_limit: stepLimit || 0,
        breakpoints: breakpoints,
        budget: budget(),
        persist: $("persist").checked,
      });
      astView = current.program;
      $("ast").innerHTML = renderAST(astView.root);
      await updatePositions();
      renderStatus(current);
      loadHistory();
    } catch (e) {
      $("status").className = "status nomatch";
      $("status").textContent = "错误：" + e.message;
    }
  }

  async function doResume(stepLimit) {
    if (!frontier) return;
    try {
      current = await api("/api/resume", {
        pattern: $("pattern").value,
        input: $("input").value,
        optimize: $("optimize").checked,
        frontier: frontier,
        run_id: current && current.run_id ? current.run_id : 0,
        step_limit: stepLimit || 0,
        breakpoints: breakpoints,
        budget: budget(),
      });
      renderStatus(current);
    } catch (e) {
      $("status").textContent = "错误：" + e.message;
    }
  }

  function renderBps() {
    $("bps").innerHTML = breakpoints.map((b, i) =>
      "<span data-i='" + i + "'>节点 " + (b.node_id || "*") + " @rune " + b.rune_pos
      + " <a href='#' data-del='" + i + "' style='color:inherit'>x</a></span>").join("");
    $("bps").querySelectorAll("[data-del]").forEach((a) => {
      a.onclick = (ev) => {
        ev.preventDefault();
        breakpoints.splice(+a.dataset.del, 1);
        renderBps();
      };
    });
  }

  async function doDiff() {
    const body = {
      pattern_a: $("dA").value,
      pattern_b: $("dB").value,
      persist: true,
      budget: {
        max_length: +$("dLen").value,
        alphabet: $("dAlpha").value,
        max_cases: +$("dMax").value,
        per_match: { max_steps: 20000, max_states: 5000, max_snapshots: 5000, max_events: 200 },
      },
    };
    try {
      const r = await api("/api/diff", body);
      $("diffMeta").innerHTML = "枚举 " + r.enumerated + " 个输入；反例 " + r.cases.length
        + " 个；" + (r.exhaustive ? "预算内已穷尽" : "预算内未穷尽（可能还有反例）")
        + "。反例按 code point 长度、再按字母表字典序稳定排列。"
        + " 钉版 syntax=" + r.syntax_version + " unicode=" + r.unicode_version
        + (r.indeterminate_inputs && r.indeterminate_inputs.length
          ? "；不定输入：" + r.indeterminate_inputs.map(esc).join(", ") : "");
      $("diffTable").querySelector("tbody").innerHTML = r.cases.map((c, i) =>
        "<tr><td>" + (i + 1) + "</td><td>" + c.rune_length + "</td><td><code>"
        + esc(c.input.replaceAll("\n", "\\n").replaceAll("\t", "\\t")) + "</code></td><td>"
        + c.verdict_a + "</td><td>" + c.verdict_b + "</td></tr>").join("");
    } catch (e) {
      $("diffMeta").textContent = "差分失败：" + e.message;
    }
  }

  async function loadHistory() {
    const r = await get("/api/runs?limit=40");
    $("history").innerHTML = (r.runs || []).map((x) =>
      '<div data-id="' + x.id + '">#' + x.id + " [" + x.status + "] <code>" + esc(x.pattern)
      + "</code> on <code>" + esc(x.input) + "</code> " + x.syntax_version + "/"
      + x.unicode_version + "</div>").join("");
    $("history").querySelectorAll("[data-id]").forEach((d) => {
      d.onclick = async () => {
        const detail = await get("/api/runs/" + d.dataset.id);
        shownEvents = detail.events;
        $("trace").querySelector("tbody").innerHTML = shownEvents.map((e) => {
          const t = kindTag[e.kind] || ["step", e.kind];
          return "<tr><td>" + e.seq + '</td><td><span class="tag ' + t[0] + '">' + t[1]
            + "</span></td><td>" + e.rune_pos + "</td><td>" + e.byte_pos + "</td><td>"
            + (e.node_id || "") + "</td><td>" + e.active + "</td><td>" + esc(e.detail || "")
            + "</td></tr>";
        }).join("");
      };
    });
  }

  $("btnParse").onclick = doParse;
  $("btnRun").onclick = () => doRun(0);
  $("btnStep").onclick = () => doRun(1);
  $("btnResume").onclick = () => doResume(0);
  $("btnRunTo").onclick = () => doResume(1);
  $("btnAddBp").onclick = () => {
    breakpoints.push({
      node_id: +$("bpNode").value || 0,
      rune_pos: $("bpRune").value === "" ? -1 : +$("bpRune").value,
    });
    renderBps();
  };
  $("filter").onchange = renderTrace;
  $("btnClearTrace").onclick = () => {
    $("trace").querySelector("tbody").innerHTML = "";
  };
  $("btnDiff").onclick = doDiff;
  $("btnHistory").onclick = loadHistory;
  $("input").oninput = updatePositions;

  (async function init() {
    const v = await get("/api/versions");
    $("versions").textContent = "语法版本 " + v.syntax_version
      + " ｜ Unicode 数据版本 " + v.unicode_version
      + " ｜ 每个结果均钉住以上版本";
    await doParse();
    await updatePositions();
    await loadHistory();
  })();
})();
