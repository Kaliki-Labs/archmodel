// Architecture Explorer. Vanilla JS; graphs are Cytoscape.js + dagre.
// Reads the Architecture model from #archmodel-model, page settings from
// #archmodel-config and an optional diff object from #archmodel-diff (the seam
// for diff mode: {added:{modules,edges,operations,tables}, removed:{...},
// changed:{...}, diff:{...}} with edges written "from>to"). All teaching text
// (rule and metric Rationales, metric readings) comes from the model; this file
// only holds UI labels and the one-line Level captions.
(function () {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const model = JSON.parse($('archmodel-model').textContent);
  const cfg = JSON.parse($('archmodel-config').textContent);
  const diff = $('archmodel-diff') ? JSON.parse($('archmodel-diff').textContent) : null;

  // ---- indexes ------------------------------------------------------------
  const mods = {}, ops = {}, schemas = {}, tables = {}, funcs = {};
  model.modules.forEach((m) => { mods[m.id] = m; });
  model.operations.forEach((o) => { ops[o.id] = o; });
  model.schemas.forEach((s) => { schemas[s.id] = s; });
  model.tables.forEach((t) => { tables[t.name] = t; });
  model.functions.forEach((f) => { funcs[f.id] = f; });
  const depsFrom = {}, depsTo = {}, edgeByKey = {};
  model.dependencies.forEach((d) => {
    (depsFrom[d.from] = depsFrom[d.from] || []).push(d);
    (depsTo[d.to] = depsTo[d.to] || []).push(d);
    edgeByKey[d.from + '>' + d.to] = d;
  });
  const violByEdge = {}, violByModule = {}, modViol = {};
  model.violations.forEach((v) => {
    const map = v.to ? violByEdge : violByModule;
    const key = v.to ? v.module + '>' + v.to : v.module;
    (map[key] = map[key] || []).push(v);
    modViol[v.module] = true;
    if (v.to) modViol[v.to] = true;
  });
  const opCount = {}, tableCount = {};
  model.operations.forEach((o) => { opCount[o.owner] = (opCount[o.owner] || 0) + 1; });
  model.tables.forEach((t) => { (t.owners || []).forEach((o) => { tableCount[o] = (tableCount[o] || 0) + 1; }); });
  const rationale = model.metric_rationales || {};
  const prodIn = (id) => (depsTo[id] || []).filter((d) => !d.test_only);
  const prodOut = (id) => (depsFrom[id] || []).filter((d) => !d.test_only);

  const KIND_LABEL = {
    composition_root: 'Composition root', domain: 'Domain',
    infrastructure: 'Infrastructure module', foundation: 'Foundation',
    test_only: 'Test-only module'
  };
  // One-line plain explanations of each Level (UI captions, not model text).
  const KIND_CAPTION = {
    composition_root: 'Wires everything together. Nothing may import it.',
    domain: 'A business capability. May use Infrastructure and Foundation, not other Domains.',
    infrastructure: 'Supporting code such as mail or caching. Owns no API operations.',
    foundation: 'The innermost code. Everything may use it; it uses nothing else.',
    test_only: 'Only test files import these, so they sit outside the production graph.'
  };
  // Shorter captions for the map's lane labels.
  const LANE_CAPTION = {
    composition_root: 'Wires everything together. Nothing imports it.',
    domain: 'Business capabilities. They may not import each other.',
    infrastructure: 'Supporting code such as mail or caching.',
    foundation: 'Innermost code. Everything may use it.',
    test_only: 'Imported only by test files.'
  };
  const kindLabel = (k) => KIND_LABEL[k] || k;

  // ---- small DOM helpers (never innerHTML for model text: it is data) -------
  function h(tag, attrs, kids) {
    const e = document.createElement(tag);
    if (attrs) Object.keys(attrs).forEach((k) => {
      const v = attrs[k];
      if (v == null || v === false) return;
      if (k === 'class') e.className = v;
      else if (k === 'style') e.style.cssText = v;
      else if (k.indexOf('on') === 0) e.addEventListener(k.slice(2), v);
      else e.setAttribute(k, v === true ? '' : v);
    });
    add(e, kids);
    return e;
  }
  function add(e, kids) {
    if (kids == null || kids === false) return;
    if (Array.isArray(kids)) { kids.forEach((k) => add(e, k)); return; }
    e.appendChild(typeof kids === 'object' ? kids : document.createTextNode(String(kids)));
  }
  const SVGNS = 'http://www.w3.org/2000/svg';
  function s(tag, attrs, kids) {
    const e = document.createElementNS(SVGNS, tag);
    if (attrs) Object.keys(attrs).forEach((k) => { if (attrs[k] != null) e.setAttribute(k, attrs[k]); });
    add(e, kids);
    return e;
  }
  // Static icon markup only (never model text).
  const ICONS = {
    lock: '<svg class="lock" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><rect x="3" y="7" width="10" height="7" rx="1.5"/><path d="M5.5 7V5a2.5 2.5 0 015 0v2"/></svg>',
    bolt: '<svg class="ic" viewBox="0 0 10 12" fill="currentColor" aria-hidden="true"><path d="M6 0L0 7h4l-1 5 6-7H5z"/></svg>',
    grid: '<svg class="ic" viewBox="0 0 10 10" fill="currentColor" aria-hidden="true"><path d="M0 0h4v4H0zM6 0h4v4H6zM0 6h4v4H0zM6 6h4v4H6z"/></svg>',
    sun: '<svg width="18" height="18" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><circle cx="10" cy="10" r="3.6"/><path d="M10 2v2M10 16v2M2 10h2M16 10h2M4.3 4.3l1.4 1.4M14.3 14.3l1.4 1.4M4.3 15.7l1.4-1.4M14.3 5.7l1.4-1.4"/></svg>',
    moon: '<svg width="18" height="18" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" aria-hidden="true"><path d="M16.5 12.2A7 7 0 017.8 3.5a7 7 0 108.7 8.7z"/></svg>'
  };
  function icon(name) {
    const w = document.createElement('span');
    w.style.display = 'inline-flex';
    w.innerHTML = ICONS[name]; // eslint-disable-line no-unsanitized/property
    return w;
  }
  const enc = encodeURIComponent;
  const R = {
    system: () => '/',
    module: (id) => '/module/' + enc(id),
    operation: (id) => '/operation/' + enc(id),
    schema: (id) => '/schema/' + enc(id),
    table: (n) => '/table/' + enc(n),
    fn: (id) => '/function/' + enc(id),
    edge: (a, b) => '/edge/' + enc(a) + '/' + enc(b)
  };
  const link = (route, text, cls) => h('a', { href: '#' + route, class: cls }, text);
  const badge = (text, cls, style) => h('span', { class: 'badge ' + (cls || ''), style }, text);
  const kindBadge = (kind) => badge(kindLabel(kind), 'kind', 'background:var(--c-' + kind + ')');
  const empty = (text) => h('p', { class: 'empty' }, text);
  const fmt = (n, d) => (typeof n === 'number' ? n.toFixed(d == null ? 2 : d) : '-');
  const pct = (n) => (typeof n === 'number' ? Math.round(n * 1000) / 10 + '%' : '-');
  const plural = (n, one, many) => n + ' ' + (n === 1 ? one : (many || one + 's'));
  function section(title, body, extra) {
    return h('div', { class: 'sect' }, [h('h3', null, [title, extra]), body]);
  }
  function list(items) {
    return h('ul', { class: 'list' }, items.map((i) => h('li', null, i)));
  }
  // Doc comments are hard-wrapped: reflow single newlines, keep blank-line breaks.
  function paragraphs(text) {
    return String(text || '').split(/\n[ \t]*\n/).map((p) => p.replace(/\s*\n\s*/g, ' ').trim()).filter(Boolean);
  }
  function firstSentence(t) {
    const re = /[.!?](?=\s|$)/g;
    let m, end = t.length;
    while ((m = re.exec(t))) { if (m.index + 1 >= 40) { end = m.index + 1; break; } }
    return t.slice(0, end);
  }

  // ---- diff helpers ------------------------------------------------------------
  function diffClass(kind, id) {
    if (!diff) return '';
    const a = diff.added && diff.added[kind], r = diff.removed && diff.removed[kind], c = diff.changed && diff.changed[kind];
    if (a && a.indexOf(id) >= 0) return 'diff-added';
    if (r && r.indexOf(id) >= 0) return 'diff-removed';
    if (c && c.indexOf(id) >= 0) return 'diff-changed';
    return '';
  }
  const DIFF_TEXT = { 'diff-added': '+ added', 'diff-removed': '− removed', 'diff-changed': '~ changed' };
  function diffBadge(kind, id) {
    const c = diffClass(kind, id);
    return c ? badge(DIFF_TEXT[c], 'diff ' + c) : null;
  }
  function diffTotals() {
    const d = diff && diff.diff;
    if (!d) return null;
    const t = { added: 0, removed: 0, changed: 0 };
    t.added = d.modules.added.length + d.dependencies.added.length + d.operations.added.length + d.tables.added.length;
    t.removed = d.modules.removed.length + d.dependencies.removed.length + d.operations.removed.length + d.tables.removed.length;
    t.changed = d.modules.kind_changed.length + d.dependencies.changed.length + d.operations.changed.length + d.tables.changed.length + d.metrics.length;
    return t;
  }

  // ---- theme -------------------------------------------------------------------
  const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');
  function isDark() {
    const t = document.documentElement.getAttribute('data-theme');
    return t ? t === 'dark' : darkQuery.matches;
  }
  function paintThemeButton() {
    const b = $('theme');
    b.textContent = '';
    b.appendChild(icon(isDark() ? 'sun' : 'moon'));
    b.title = isDark() ? 'Switch to light mode' : 'Switch to dark mode';
  }
  try {
    const saved = localStorage.getItem('archmodel-theme');
    if (saved === 'dark' || saved === 'light') document.documentElement.setAttribute('data-theme', saved);
  } catch (e) { /* storage unavailable */ }
  $('theme').addEventListener('click', () => {
    const next = isDark() ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    try { localStorage.setItem('archmodel-theme', next); } catch (e) { /* storage unavailable */ }
    paintThemeButton();
    render(true);
  });
  darkQuery.addEventListener('change', () => { paintThemeButton(); render(true); });

  const cssVar = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  function resolveColor(c) {
    const m = /^var\((--[\w-]+)\)$/.exec(c || '');
    return m ? cssVar(m[1]) : c;
  }

  // ---- source links -------------------------------------------------------------
  function src(path, line, label) {
    const text = label || (path + (line ? ':' + line : ''));
    const t = cfg.source.url_template, ref = cfg.source.ref;
    if (t && !(t.indexOf('{ref}') >= 0 && !ref)) {
      const url = t.replace(/\{path\}/g, encodeURI(path)).replace(/\{line\}/g, String(line || 1))
        .replace(/\{ref\}/g, encodeURIComponent(ref || ''));
      return h('a', { href: url, target: '_blank', rel: 'noopener', class: 'path' }, text);
    }
    return h('code', { class: 'path' }, text);
  }
  const siteLinks = (sites) => list((sites || []).map((x) =>
    h('div', { class: 'item' }, [src(x.file, x.line), x.test ? badge('test file') : null])));

  // ---- model queries --------------------------------------------------------------
  function funcForSymbol(sym) {
    const recv = (sym.receiver || '').replace(/^\*/, '');
    const id = sym.module + '.' + (recv ? recv + '.' : '') + sym.name;
    return funcs[id] ? id : null;
  }
  const moduleFunctions = (id) => model.functions.filter((f) => f.module === id);
  const funcLabel = (f) => (f.receiver ? f.receiver.replace(/^\*/, '') + '.' : '') + f.name;
  const hasCoverage = model.functions.some((f) => typeof f.crap === 'number');
  const crapBucket = (v) => (v > 30 ? 'bad' : v > 15 ? 'warn' : '');
  const ccBucket = (v) => (v > 20 ? 'bad' : v > 10 ? 'warn' : '');

  // ---- shared components -------------------------------------------------------------
  function stat(o) {
    const tag = o.href ? 'a' : o.onclick ? 'button' : 'div';
    return h(tag, {
      class: 'stat ' + (o.cls || ''), href: o.href ? '#' + o.href : null, type: tag === 'button' ? 'button' : null,
      title: o.title, onclick: o.onclick, 'aria-pressed': o.pressed == null ? null : String(o.pressed)
    }, [
      h('span', { class: 'l' }, o.l),
      h('span', { class: 'v ' + (o.vcls || '') }, o.v),
      o.s ? h('span', { class: 's' }, o.s) : null
    ]);
  }

  function violationBox(v, withLink) {
    return h('div', { class: 'violbox' }, [
      h('h4', null, [badge(v.severity === 'error' ? 'Violation' : 'Warning', 'bad'), v.rule]),
      h('p', null, h('b', null, v.message)),
      h('p', { class: 'why' }, [h('b', null, 'Why: '), v.rationale]),
      h('p', { class: 'fix' }, [h('b', null, 'Fix: '), v.fix]),
      v.sites && v.sites.length ? siteLinks(v.sites) : null,
      withLink ? h('div', { class: 'acts' }, v.to
        ? [link(R.edge(v.module, v.to), 'Show the edge ' + v.subject, 'btn')]
        : [link(R.module(v.module), 'Open ' + v.module, 'btn')]) : null
    ]);
  }

  function levelTrack(m) {
    if (m.level == null) return badge('Outside the production graph', 'mute');
    const dots = [];
    for (let i = 0; i < model.levels.length; i++) {
      dots.push(h('i', { class: i === m.level ? 'on' : '', style: i === m.level ? 'background:var(--c-' + m.kind + ')' : '' }));
    }
    return h('span', { class: 'track', title: 'Level ' + (m.level + 1) + ' of ' + model.levels.length + ' (1 is the outermost); dependencies point inward, to higher numbers.' },
      [dots, 'Level ' + (m.level + 1) + ' of ' + model.levels.length]);
  }

  function metricTitle(key) { return rationale[key] || ''; }

  // ---- the Abstractness x Instability chart (inline SVG) ----------------------------
  // The zone cut-offs mirror internal/metrics (stable/concrete <= 0.3, abstract/
  // unstable >= 0.7, "on the main sequence" D <= 0.2). They only shape the
  // shading; the zone each Module is *in* comes from the model.
  const ZONE_LOW = 0.3, ZONE_HIGH = 0.7, ZONE_D = 0.2;
  function chartableModules() {
    return model.modules.filter((m) => m.metrics);
  }
  function aiChart(o) {
    const mini = !!o.mini;
    const W = mini ? 200 : 760, H = mini ? 168 : 580;
    const px = mini ? 24 : 70, py = mini ? 8 : 20, pw = mini ? W - px - 8 : W - px - 24, ph = mini ? H - py - 24 : H - py - 66;
    const X = (i) => px + i * pw, Y = (a) => py + (1 - a) * ph;
    const svg = s('svg', { viewBox: '0 0 ' + W + ' ' + H, role: 'img', 'aria-label': 'Abstractness against instability for every Module' });
    const txt = (x, y, t, st, anchor, extra) => s('text', Object.assign({ x, y, 'text-anchor': anchor || 'start', style: st }, extra || {}), t);
    const fs = mini ? 9 : 12;
    svg.appendChild(s('rect', { x: px, y: py, width: pw, height: ph, style: 'fill:var(--surface);stroke:var(--line-2)', rx: 3 }));
    // main-sequence band, zones, diagonal
    const band = [[0, 1 - ZONE_D], [0, 1], [ZONE_D, 1], [1, ZONE_D], [1, 0], [1 - ZONE_D, 0]].map((p) => X(p[0]) + ',' + Y(p[1])).join(' ');
    svg.appendChild(s('polygon', { points: band, style: 'fill:var(--added);fill-opacity:.09' }));
    svg.appendChild(s('rect', { x: X(0), y: Y(ZONE_LOW), width: pw * ZONE_LOW, height: ph * ZONE_LOW, style: 'fill:var(--violation);fill-opacity:.12' }));
    svg.appendChild(s('rect', { x: X(ZONE_HIGH), y: Y(1), width: pw * (1 - ZONE_HIGH), height: ph * (1 - ZONE_HIGH), style: 'fill:var(--c-schema);fill-opacity:.14' }));
    svg.appendChild(s('line', { x1: X(0), y1: Y(1), x2: X(1), y2: Y(0), style: 'stroke:var(--added);stroke-width:1.5;stroke-dasharray:5 4' }));
    // ticks
    [0, 0.5, 1].forEach((t) => {
      svg.appendChild(txt(X(t), py + ph + (mini ? 10 : 18), String(t), 'fill:var(--ink-3);font-size:' + fs + 'px', 'middle'));
      svg.appendChild(txt(px - 6, Y(t) + 4, String(t), 'fill:var(--ink-3);font-size:' + fs + 'px', 'end'));
    });
    if (mini) {
      svg.appendChild(txt(X(0.5), H - 3, 'Instability (I)', 'fill:var(--ink-3);font-size:9px', 'middle'));
      svg.appendChild(txt(8, Y(0.5), 'A', 'fill:var(--ink-3);font-size:9px', 'middle'));
    } else {
      svg.appendChild(txt(X(0.5), H - 10, 'Instability (I): from “everything depends on it” to “it depends on everything”', 'fill:var(--ink-2);font-size:13px;font-weight:600', 'middle'));
      svg.appendChild(txt(0, 0, 'Abstractness (A): interfaces ÷ exported types', 'fill:var(--ink-2);font-size:13px;font-weight:600', 'middle', { transform: 'translate(16,' + (py + ph / 2) + ') rotate(-90)' }));
      svg.appendChild(txt(X(0) + 10, Y(0) - 12, 'Zone of pain', 'fill:var(--violation);font-size:13px;font-weight:700'));
      svg.appendChild(txt(X(0) + 10, Y(0) - 28, 'stable and concrete', 'fill:var(--violation);font-size:11px;opacity:.85'));
      svg.appendChild(txt(X(1) - 10, Y(1) + 22, 'Zone of uselessness', 'fill:var(--c-schema);font-size:13px;font-weight:700', 'end'));
      svg.appendChild(txt(X(1) - 10, Y(1) + 38, 'abstract and unstable', 'fill:var(--c-schema);font-size:11px;opacity:.85', 'end'));
      const mx = X(0.5), my = Y(0.5), ang = Math.atan2(Y(0) - Y(1), X(1) - X(0)) * 180 / Math.PI;
      svg.appendChild(txt(0, 0, 'main sequence', 'fill:var(--added);font-size:12px;font-weight:700', 'middle', { transform: 'translate(' + mx + ',' + (my - 8) + ') rotate(' + ang.toFixed(1) + ')' }));
    }
    // dots: spread Modules that share a position
    const pts = chartableModules().map((m) => ({ m, i: m.metrics.instability, a: m.metrics.abstractness }));
    const groups = {};
    pts.forEach((p) => { const k = p.i.toFixed(3) + ',' + p.a.toFixed(3); (groups[k] = groups[k] || []).push(p); });
    const r = mini ? 3 : 7;
    Object.keys(groups).forEach((k) => {
      const g = groups[k];
      g.forEach((p, idx) => {
        let ox = 0, oy = 0;
        if (g.length > 1 && !mini) {
          const ring = idx < 6 ? 1 : 2, n = ring === 1 ? Math.min(g.length, 6) : g.length - 6, j = ring === 1 ? idx : idx - 6;
          const ang = -Math.PI / 2 + (2 * Math.PI * j) / n;
          ox = Math.cos(ang) * 11 * ring; oy = Math.sin(ang) * 11 * ring;
        }
        p.x = X(p.i) + ox; p.y = Y(p.a) + oy;
      });
    });
    const placed = pts.map((p) => ({ x1: p.x - r, y1: p.y - r, x2: p.x + r, y2: p.y + r }));
    const labelBoxes = [];
    const overlap = (a, b) => a.x1 < b.x2 && a.x2 > b.x1 && a.y1 < b.y2 && a.y2 > b.y1;
    const order = pts.slice().sort((p, q) => p.i - q.i || p.a - q.a);
    order.forEach((p) => {
      const sel = o.highlight === p.m.id;
      if (mini && !sel) {
        svg.appendChild(s('circle', { cx: p.x, cy: p.y, r: 2.6, style: 'fill:var(--ink-3);opacity:.45' }));
        return;
      }
      const m = p.m, met = m.metrics;
      const tip = m.id + ': I=' + fmt(met.instability) + ', A=' + fmt(met.abstractness) + ', D=' + fmt(met.distance) + ' (' + (met.zone || '') + ')' + (met.reading ? '\n' + met.reading : '');
      const a = s('a', { href: '#' + R.module(m.id), class: 'dot' }, [s('title', null, tip)]);
      a.appendChild(s('circle', { cx: p.x, cy: p.y, r: sel ? 6 : r, style: 'fill:var(--c-' + m.kind + ');stroke:' + (modViol[m.id] ? 'var(--violation)' : 'var(--surface)') + ';stroke-width:' + (modViol[m.id] ? 3 : 2) }));
      if (!mini) {
        const w = m.id.length * 6.6 + 4, hh = 14;
        const cands = [
          [p.x + r + 4, p.y + 4, 'start', 0], [p.x - r - 4, p.y + 4, 'end', 1], [p.x, p.y - r - 5, 'middle', 2], [p.x, p.y + r + 14, 'middle', 3],
          [p.x + r, p.y - r - 3, 'start', 4], [p.x + r, p.y + r + 12, 'start', 5], [p.x - r, p.y - r - 3, 'end', 6], [p.x - r, p.y + r + 12, 'end', 7]
        ].map((c) => {
          const x1 = c[2] === 'start' ? c[0] : c[2] === 'end' ? c[0] - w : c[0] - w / 2;
          const box = { x1, y1: c[1] - hh + 2, x2: x1 + w, y2: c[1] + 3 };
          let bad = 0;
          if (box.x1 < px - 4 || box.x2 > px + pw + 4 || box.y1 < py - 2 || box.y2 > py + ph + 12) bad += 5;
          labelBoxes.forEach((b) => { if (overlap(box, b)) bad += 3; });
          placed.forEach((b, i) => { if (pts[i] !== p && overlap(box, b)) bad += 2; });
          return { c, box, bad };
        });
        cands.sort((u, v) => u.bad - v.bad || u.c[3] - v.c[3]);
        const best = cands[0];
        labelBoxes.push(best.box);
        a.appendChild(s('text', { x: best.c[0], y: best.c[1], 'text-anchor': best.c[2], style: 'font-size:12px;font-weight:600;fill:var(--ink);paint-order:stroke;stroke:var(--surface);stroke-width:3px;stroke-linejoin:round' }, m.id));
      } else {
      }
      svg.appendChild(a);
    });
    return svg;
  }

  function chartTab() {
    return {
      id: 'chart', label: 'Chart', scroll: true,
      build() {
        const ms = chartableModules();
        const caption = (k, title) => rationale[k] ? h('div', null, [h('b', null, title), rationale[k]]) : null;
        return h('div', { class: 'chartbox' }, h('div', { class: 'chart card' }, [
          h('h3', null, 'Abstractness × Instability: where each Module sits'),
          ms.length ? aiChart({}) : empty('No Module has metrics.'),
          h('div', { class: 'leg', style: 'margin-top:8px' }, model.levels.map((k) => badge(kindLabel(k), 'kind', 'background:var(--c-' + k + ')'))),
          h('div', { class: 'howto' }, [
            caption('i', 'Left to right: Instability (I)'),
            caption('a', 'Bottom to top: Abstractness (A)'),
            caption('d', 'Distance from the main sequence (D)')
          ]),
          h('p', { class: 'sub' }, 'Dots are Modules, colored by Level; click one to open it. A red ring marks a Module in a rule violation. Hover for its reading.')
        ]));
      }
    };
  }

  // ---- violations, metrics, tiles for a Module ------------------------------------------
  function metricTiles(m, explain) {
    const x = m.metrics;
    const tiles = [];
    const mk = (key, l, v, sub, extra) => {
      let node;
      node = stat(Object.assign({ l, v, s: sub, title: metricTitle(key) + ' (click for details)', pressed: false, onclick: () => {
        const on = explain.key !== key;
        explain.key = on ? key : null;
        explain.paint();
        tiles.forEach((t) => t.setAttribute('aria-pressed', 'false'));
        if (on) node.setAttribute('aria-pressed', 'true');
      } }, extra || {}));
      tiles.push(node);
      return node;
    };
    const word = (v) => (v <= ZONE_LOW ? 'stable' : v >= ZONE_HIGH ? 'unstable' : 'in between');
    mk('ca', 'Ca', x.ca, 'Modules that import it');
    mk('ce', 'Ce', x.ce, 'Modules it imports');
    mk('i', 'I', x.instability_defined ? fmt(x.instability) : 'n/a', x.instability_defined ? word(x.instability) : 'Ca + Ce = 0');
    mk('a', 'A', fmt(x.abstractness), x.abstract_types + ' of ' + x.exported_types + ' types are interfaces');
    mk('d', 'D', fmt(x.distance), x.zone || 'distance from the main sequence');
    mk('cc', 'Worst CC', x.worst_cc, x.worst_cc_function ? funcLink(x.worst_cc_function) : 'no functions', { vcls: ccBucket(x.worst_cc) === 'bad' ? '' : '' });
    if (typeof x.worst_crap === 'number') {
      mk('crap', 'Worst CRAP', fmt(x.worst_crap, 1), x.worst_crap_function ? funcLink(x.worst_crap_function) : '');
    } else {
      mk('crap', 'Worst CRAP', '–', 'no coverage data');
    }
    return tiles;
  }
  function funcLink(id) {
    const f = funcs[id];
    return f ? link(R.fn(id), funcLabel(f)) : String(id);
  }

  // ---- hero for a Module ------------------------------------------------------------------
  function moduleHero(m) {
    const paras = paragraphs(m.purpose);
    const flat = paras.join(' ');
    const lead = m.summary ? firstSentence(paragraphs(m.summary).join(' ')) : '';
    const more = flat && flat.length > lead.length + 4 ? paras : null;
    return h('div', { class: 'hero' }, [
      h('div', { class: 'main ' + diffClass('modules', m.id) }, [
        h('h1', null, [h('span', { class: 't rm' }, m.id), diffBadge('modules', m.id)]),
        h('div', { class: 'meta' }, [kindBadge(m.kind), levelTrack(m),
          m.generated ? badge(m.present ? 'generated' : 'generated, not built', m.present ? '' : 'warn') : null,
          h('span', { class: 'sub' }, src(m.dir, 0))]),
        lead ? h('p', { class: 'lead' }, lead)
          : m.generated ? h('p', { class: 'sub' }, 'Generated code: it has no package doc comment, and none is expected.')
          : h('p', { class: 'bad-text' }, 'No package doc comment, so there is no Purpose to show.'),
        more ? h('details', { class: 'more' }, [h('summary', null, 'Read the full purpose'), more.map((p) => h('p', null, p))]) : null
      ])
    ]);
  }
  function miniPlot(m) {
    const x = m.metrics;
    if (!x) return null;
    return h('div', { class: 'miniplot', title: 'Where this Module sits between Instability and Abstractness' }, [
      aiChart({ mini: true, highlight: m.id }),
      h('div', { class: 'cap' }, 'I ' + fmt(x.instability) + ' \u00B7 A ' + fmt(x.abstractness) + ' \u00B7 ' + (x.zone || ''))
    ]);
  }

  function moduleStrip(m, explain) {
    if (!m.metrics) {
      return h('div', { class: 'card' }, empty('Test-only modules are outside the production graph, so they have no package metrics.'));
    }
    return h('div', { class: 'strip' }, metricTiles(m, explain));
  }

  function readingBlock(m, explain) {
    const x = m.metrics;
    if (!x || !x.reading) return null;
    const box = h('div', { class: 'reading' });
    explain.paint = () => {
      box.textContent = '';
      const body = h('div', { class: 'grow' }, [h('p', null, x.reading)]);
      if (explain.key) body.appendChild(h('p', { class: 'why' }, [h('b', null, explain.key.toUpperCase() + ': '), rationale[explain.key] || 'No Rationale recorded in the model.']));
      else body.appendChild(h('p', { class: 'why' }, 'Click a number above to see what it measures and why it matters.'));
      box.appendChild(body);
    };
    explain.paint();
    return box;
  }

  // ---- Module tabs ------------------------------------------------------------------------
  function depRow(id, edge, side) {
    const m = mods[id];
    const bad = violByEdge[edge.from + '>' + edge.to];
    return h('div', { class: 'item ' + diffClass('modules', id) }, [
      link(R.module(id), id, 'grow name'),
      m ? kindBadge(m.kind) : null,
      edge.test_only ? badge('test-only', 'mute') : null,
      bad ? link(R.edge(edge.from, edge.to), 'violates ' + bad.map((v) => v.rule).join(', '), 'badge bad') : null
    ]);
  }
  function depList(deps, side) {
    if (!deps.length) return empty('None.');
    return list(deps.map((d) => depRow(d[side], d, side)));
  }

  function overviewTab(m) {
    return {
      id: 'overview', label: 'Overview',
      build() {
        const byProd = (a, b) => (a.test_only ? 1 : 0) - (b.test_only ? 1 : 0) || (a.from + a.to).localeCompare(b.from + b.to);
        const outs = (depsFrom[m.id] || []).slice().sort(byProd), ins = (depsTo[m.id] || []).slice().sort(byProd);
        const bad = [];
        (violByModule[m.id] || []).forEach((v) => bad.push(v));
        outs.concat(ins).forEach((d) => (violByEdge[d.from + '>' + d.to] || []).forEach((v) => { if (bad.indexOf(v) < 0) bad.push(v); }));
        const cols = h('div', { class: 'mgrid' }, [
          h('div', { class: 'mcol' }, [h('div', { class: 'card' }, [
            h('h3', null, 'How it connects'),
            h('div', { class: 'neigh', id: 'neigh', style: 'height:360px' }),
            h('p', { class: 'sub', style: 'margin:8px 0 0' }, 'Used by Modules above, depends on Modules below. Dashed lines are test-only imports; red is a rule violation.')
          ])]),
          h('div', { class: 'mcol' }, [
            h('div', { class: 'card dlist' }, [h('h3', null, 'Depends on (' + outs.length + ')'), depList(outs, 'to')]),
            h('div', { class: 'card dlist' }, [h('h3', null, 'Used by (' + ins.length + ')'), depList(ins, 'from')])
          ])
        ]);
        const wrap = h('div', null, [
          bad.length ? h('div', null, bad.map((v) => violationBox(v, false))) : null,
          cols]);
        wrap.afterShow = true;
        return wrap;
      },
      show() { neighborGraph(m); }
    };
  }

  function groupBy(items, keyFn) {
    const groups = [], idx = {};
    items.forEach((i) => {
      const k = keyFn(i);
      if (!(k in idx)) { idx[k] = groups.length; groups.push({ key: k, items: [] }); }
      groups[idx[k]].items.push(i);
    });
    return groups;
  }
  function pathGroup(p) {
    const seg = p.split('/').filter(Boolean)[0] || '';
    return '/' + seg;
  }
  function rateLimitText(rl) {
    if (!rl) return null;
    if (rl.requests != null && rl.window) return rl.requests + ' per ' + rl.window + (rl.key ? ' by ' + rl.key : '');
    return JSON.stringify(rl);
  }
  function apiRow(o) {
    const codes = (o.responses || []).map((r) => h('span', { class: 'code c' + String(r.status)[0], title: r.description }, r.status));
    const impl = o.implementation ? o.implementation.function.replace(/^.*?\./, '') : null;
    return h('a', { class: 'api ' + diffClass('operations', o.id), href: '#' + R.operation(o.id) }, [
      h('span', { class: 'badge verb verb-' + o.method.toLowerCase() }, o.method),
      h('span', { class: 'p' }, [h('span', { class: 'rm' }, o.path), h('small', null, o.summary ? o.summary.slice(0, 110) + (o.summary.length > 110 ? '…' : '') : o.id)]),
      h('span', { class: 'impl' }, impl ? ['→ ', h('code', null, impl)] : h('span', { class: 'faint' }, 'no Go method')),
      h('span', { class: 'tags' }, [
        o.security.length ? h('span', { title: 'Requires ' + o.security.join(', ') }, icon('lock')) : h('span', { class: 'faint sub', title: 'No security scheme' }, 'public'),
        o.rate_limit ? badge(rateLimitText(o.rate_limit), 'warn') : null
      ]),
      h('span', { class: 'codes' }, codes)
    ]);
  }
  function apiTab(m) {
    const o = model.operations.filter((x) => x.owner === m.id);
    return {
      id: 'api', label: 'API', n: o.length,
      build() {
        const groups = groupBy(o.slice().sort((a, b) => a.path.localeCompare(b.path) || a.method.localeCompare(b.method)), (x) => pathGroup(x.path));
        return h('div', { class: 'stack' }, groups.map((g) => h('div', { class: 'card', style: 'padding:var(--sp-3) var(--sp-2)' }, [
          h('h3', { style: 'padding:0 var(--sp-2)' }, [h('code', null, g.key), ' ', badge(g.items.length)]),
          g.items.map(apiRow)
        ])));
      }
    };
  }

  function dataTab(m) {
    const qs = model.queries.filter((q) => q.module === m.id);
    const owned = model.tables.filter((t) => t.owners.indexOf(m.id) >= 0).map((t) => t.name);
    const names = owned.slice();
    qs.forEach((q) => q.tables.forEach((t) => { if (names.indexOf(t) < 0 && tables[t]) names.push(t); }));
    names.sort();
    return {
      id: 'data', label: 'Data', n: names.length,
      build() {
        const cards = names.map((name) => {
          const t = tables[name], mine = qs.filter((q) => q.tables.indexOf(name) >= 0);
          const isOwn = owned.indexOf(name) >= 0;
          const kinds = { one: 0, many: 0, exec: 0 };
          mine.forEach((q) => { if (kinds[q.kind] != null) kinds[q.kind]++; });
          return h('div', { class: 'tcard ' + (isOwn ? '' : 'ghost ') + diffClass('tables', name) }, [
            h('header', null, [link(R.table(name), name), badge(t.columns.length + ' columns'),
              isOwn ? null : badge('owned by ' + t.owners.join(', '), 'mute'), diffBadge('tables', name)]),
            h('ul', { class: 'cols' }, t.columns.map((c) => h('li', { class: c.nullable ? 'nullable' : '' }, [
              c.primary_key ? h('span', { class: 'pk' }, 'PK') : null,
              h('span', { class: 'cn' }, c.name),
              c.references ? link(R.table(c.references), '→ ' + c.references, 'badge') : null,
              h('span', { class: 'ct' }, c.type + (c.nullable ? '?' : ''))
            ]))),
            h('div', { class: 'qs' }, [
              h('h5', null, plural(mine.length, 'query', 'queries') + ' from this Module'),
              mine.length ? mine.map((q) => h('div', { class: 'q' }, [
                h('span', { class: 'qk ' + q.kind, title: ':' + q.kind }, ':' + q.kind), h('code', null, q.id)])) : h('span', { class: 'faint' }, 'None.')
            ])
          ]);
        });
        const loose = qs.filter((q) => !q.tables.length || q.tables.every((t) => !tables[t]));
        return h('div', null, [
          cards.length ? h('div', { class: 'tcards' }, cards) : empty('This Module owns no tables and runs no queries.'),
          loose.length ? section('Queries without a table in the model', list(loose.map((q) => h('div', { class: 'item' }, [h('span', { class: 'qk ' + q.kind }, ':' + q.kind), h('code', { class: 'grow' }, q.id), src(q.file, q.line)])))) : null
        ]);
      }
    };
  }

  function sortableFunctions(fs) {
    const state = { key: hasCoverage ? 'crap' : 'cc', dir: -1 };
    const maxCC = Math.max(10, ...fs.map((f) => f.cc));
    const maxCrap = Math.max(30, ...fs.map((f) => f.crap || 0));
    const wrap = h('div');
    let showAll = false;
    const cols = [['name', 'Function', false], ['cc', 'CC', true]].concat(hasCoverage ? [['crap', 'CRAP', true], ['coverage', 'Coverage', true]] : []);
    function bar(v, max, cls) {
      return h('div', { class: 'barcell' }, [h('span', { class: 't' }, h('span', { class: 'bar ' + (cls || ''), style: 'width:' + Math.max(2, Math.min(100, v / max * 100)) + '%' })),
        h('span', { class: 'x' }, '')]);
    }
    function paint() {
      const sorted = fs.slice().sort((a, b) => {
        if (state.key === 'name') return state.dir * funcLabel(a).localeCompare(funcLabel(b));
        return state.dir * ((a[state.key] || 0) - (b[state.key] || 0)) || a.name.localeCompare(b.name);
      });
      const rows = (showAll ? sorted : sorted.slice(0, 15)).map((f) => {
        const cells = [h('td', null, link(R.fn(f.id), funcLabel(f))),
          h('td', null, (() => { const b = bar(f.cc, maxCC, ccBucket(f.cc)); b.lastChild.textContent = f.cc; return b; })())];
        if (hasCoverage) {
          cells.push(h('td', null, typeof f.crap === 'number' ? (() => { const b = bar(f.crap, maxCrap, crapBucket(f.crap)); b.lastChild.textContent = fmt(f.crap, 1); return b; })() : h('span', { class: 'dim' }, '–')));
          cells.push(h('td', null, typeof f.coverage === 'number' ? (() => { const b = bar(f.coverage * 100, 100, 'cov'); b.lastChild.textContent = pct(f.coverage); return b; })() : h('span', { class: 'dim' }, '–')));
        }
        return h('tr', null, cells);
      });
      wrap.textContent = '';
      wrap.appendChild(h('div', { class: 'tbl-wrap' }, h('table', { class: 'tbl' }, [
        h('thead', null, h('tr', null, cols.map((c) => h('th', { 'aria-sort': state.key === c[0] ? (state.dir < 0 ? 'descending' : 'ascending') : 'none' }, h('button', {
          type: 'button', onclick: () => { if (state.key === c[0]) state.dir = -state.dir; else { state.key = c[0]; state.dir = c[0] === 'name' ? 1 : -1; } paint(); }
        }, [c[1], state.key === c[0] ? (state.dir < 0 ? ' ↓' : ' ↑') : '']))))),
        h('tbody', null, rows)])));
      if (fs.length > 15) wrap.appendChild(h('button', { class: 'btn', type: 'button', style: 'margin-top:8px', onclick: () => { showAll = !showAll; paint(); } }, showAll ? 'Show the top 15' : 'Show all ' + fs.length));
    }
    paint();
    return wrap;
  }

  function codeTab(m) {
    return {
      id: 'code', label: 'Code',
      build() {
        const parts = [];
        if (m.subpackages.length) {
          parts.push(section('Subpackages', m.subpackages.map((sp) => h('div', { class: 'sub-row' + (sp.present ? '' : ' ghost') }, [
            sp.present ? src(sp.dir, 0) : h('code', { class: 'path' }, sp.dir),
            sp.generated ? badge(sp.present ? 'generated' : 'generated, not built', sp.present ? '' : 'warn') : null,
            sp.present ? null : h('span', { class: 'sub' }, 'not on disk; run codegen to see it'),
            sp.purpose ? h('span', { class: 'sub' }, sp.purpose) : null
          ]))));
        }
        const syms = model.symbols.filter((x) => x.module === m.id);
        const groupsDef = [['Interfaces', ['interface']], ['Types', ['struct', 'type']], ['Functions', ['func', 'method']]];
        parts.push(section('Public surface (' + syms.length + ' exported)', syms.length ? h('div', { class: 'surface card' }, groupsDef.map((gd) => {
          const items = syms.filter((x) => gd[1].indexOf(x.kind) >= 0);
          return h('div', null, [h('h4', null, gd[0] + ' (' + items.length + ')'), items.length ? items.map((x) => {
            const fid = (x.kind === 'func' || x.kind === 'method') ? funcForSymbol(x) : null;
            const name = (x.receiver ? x.receiver.replace(/^\*/, '') + '.' : '') + x.name;
            return h('div', { class: 'sym' }, [fid ? link(R.fn(fid), name, 'mono') : h('code', null, name)]);
          }) : h('span', { class: 'faint sub' }, 'None')]);
        })) : empty('No exported types or functions.')));
        const fs = moduleFunctions(m.id);
        parts.push(section('Functions (' + fs.length + ')', fs.length ? h('div', { class: 'card' }, [
          sortableFunctions(fs),
          hasCoverage ? null : h('p', { class: 'sub' }, 'Build with --coverprofile to add coverage and CRAP.')
        ]) : empty('No functions.')));
        parts.push(section('Source', h('p', null, src(m.dir, 0))));
        return h('div', null, parts);
      }
    };
  }

  function moduleView(m) {
    const explain = { key: null, paint() {} };
    const holder = {};
    const tabs = [];
    const ovT = overviewTab(m);
    tabs.push(ovT);
    if (opCount[m.id]) tabs.push(apiTab(m));
    const dt = dataTab(m);
    if (dt.n) tabs.push(dt);
    tabs.push(codeTab(m));
    holder.tv = tabView('module:' + m.id, [{ name: 'main', cls: 'main', tabs }], {});
    return {
      crumbs: [SYS, crumb(m.id)],
      head: [h('div', { class: 'mhead' }, [h('div', { class: 'mleft' }, [moduleHero(m), moduleStrip(m, explain)]), miniPlot(m)]), readingBlock(m, explain)],
      body: holder.tv.node, tv: holder.tv, plain: true
    };
  }

  // ---- other entity views --------------------------------------------------------------------
  function simpleHero(title, metaKids, lead, cls) {
    return h('div', { class: 'hero ' + (cls || '') }, h('div', { class: 'main' }, [
      h('h1', null, title), h('div', { class: 'meta' }, metaKids), lead ? h('p', { class: 'lead' }, lead) : null]));
  }

  // A collapsible schema tree: properties, with $ref drill-down (cycle-safe).
  function schemaTree(id, seen, depth) {
    const sc = schemas[id];
    if (!sc) return h('span', { class: 'sub' }, id + ' (not in the model)');
    if (seen.indexOf(id) >= 0) return h('span', { class: 'sub' }, '↻ recursive: ' + sc.name);
    const next = seen.concat([id]);
    const body = h('div', { class: 'node' }, sc.properties.length ? sc.properties.map((p) => {
      const refs = (p.refs || []).filter((r) => schemas[r]);
      const row = h('div', { class: 'prop' }, [h('span', { class: 'pn' }, p.name), h('span', { class: 'pt' }, p.type + (p.format ? ' (' + p.format + ')' : '')),
        p.required ? h('span', { class: 'req', title: 'required' }, '*') : null]);
      if (!refs.length || depth > 6) return row;
      return h('details', null, [h('summary', null, [row.childNodes.length ? row : '']), refs.map((r) => schemaTree(r, next, depth + 1))]);
    }) : h('span', { class: 'sub' }, sc.description || 'No properties.'));
    return h('details', { open: depth < 2 }, [
      h('summary', null, [link(R.schema(id), sc.name), badge(sc.type || 'schema', 'mute'), sc.properties.length ? h('span', { class: 'sub' }, plural(sc.properties.length, 'property', 'properties')) : null]),
      body]);
  }

  function operationView(o) {
    const impl = o.implementation;
    const strip = h('div', { class: 'strip' }, [
      stat({ l: 'Owner', v: o.owner, vcls: 'txt', href: R.module(o.owner) }),
      stat({ l: 'Implemented by', v: impl ? funcLabelFromId(impl.function) : 'none found', vcls: 'txt', href: impl ? R.fn(impl.function) : null,
        s: impl ? src(impl.file, impl.line) : 'no matching Go method' }),
      stat({ l: 'Security', v: o.security.length ? o.security.join(', ') : 'public', vcls: 'txt', s: o.security.length ? icon('lock') : 'no security scheme' }),
      stat({ l: 'Rate limit', v: o.rate_limit ? rateLimitText(o.rate_limit) : 'none', vcls: 'txt', s: o.rate_limit && o.rate_limit.scope ? 'scope: ' + o.rate_limit.scope : null }),
      stat({ l: 'Responses', v: o.responses.length, s: o.responses.map((r) => r.status).join(' ') })
    ]);
    const body = h('div', { class: 'col' }, [
      o.request_schemas.length ? section('Request body', h('div', { class: 'card tree' }, o.request_schemas.map((sid) => schemaTree(sid, [], 0)))) : null,
      o.parameters.length ? section('Parameters', h('div', { class: 'card' }, h('div', { class: 'tbl-wrap' }, h('table', { class: 'tbl' }, [
        h('thead', null, h('tr', null, ['Name', 'In', 'Type', ''].map((x) => h('th', null, x)))),
        h('tbody', null, o.parameters.map((p) => h('tr', null, [h('td', null, h('code', null, p.name)), h('td', null, p.in), h('td', null, h('code', null, p.type || '')), h('td', null, p.required ? badge('required', 'warn') : h('span', { class: 'dim' }, 'optional'))])))])))) : null,
      section('Responses', h('div', { class: 'card' }, h('div', { class: 'tbl-wrap' }, h('table', { class: 'tbl' }, [
        h('thead', null, h('tr', null, ['Status', 'Meaning', 'Body'].map((x) => h('th', null, x)))),
        h('tbody', null, o.responses.map((r) => h('tr', null, [
          h('td', null, h('span', { class: 'code c' + String(r.status)[0] }, r.status)), h('td', null, r.description),
          h('td', null, r.schemas.length ? r.schemas.map((sid) => h('div', { class: 'tree' }, schemaTree(sid, [], 2))) : h('span', { class: 'dim' }, 'no body'))])))])))),
      section('Source', h('p', null, ['Spec: ', src(o.spec, o.line)]))
    ]);
    return {
      crumbs: [SYS, crumb(o.owner, R.module(o.owner)), crumb(o.id)],
      head: [h('div', { class: 'hero method ' + diffClass('operations', o.id) }, h('div', { class: 'main' }, [
        h('h1', null, [h('span', { class: 'badge verb verb-' + o.method.toLowerCase(), style: 'font-size:15px;padding:7px 12px' }, o.method), h('code', { class: 'rm' }, o.path), diffBadge('operations', o.id)]),
        h('div', { class: 'meta' }, [badge(o.id, 'mono mute'), o.tags.map((t) => badge(t))]),
        o.summary ? h('p', { class: 'lead' }, o.summary) : null
      ])), strip],
      body, plain: true
    };
  }
  function funcLabelFromId(id) { const f = funcs[id]; return f ? funcLabel(f) : id; }

  function schemaView(sc) {
    const usedBy = model.schemas.filter((x) => x.refs.indexOf(sc.id) >= 0);
    const usedOps = model.operations.filter((o) => o.request_schemas.indexOf(sc.id) >= 0 || o.responses.some((r) => r.schemas.indexOf(sc.id) >= 0));
    const body = h('div', { class: 'col' }, [
      section('Properties', h('div', { class: 'card tree' }, schemaTree(sc.id, [], 0))),
      section('Referenced by', usedBy.length || usedOps.length ? h('div', { class: 'card' }, list(
        usedBy.map((x) => h('div', { class: 'item' }, link(R.schema(x.id), x.id))).concat(usedOps.map((o) => opRow(o))))) : empty('Nothing.')),
      section('Source', h('p', null, src(sc.spec, 0)))
    ]);
    return {
      crumbs: [SYS, crumb(sc.owner, R.module(sc.owner)), crumb(sc.name)],
      head: [simpleHero(sc.id, [badge(sc.type || 'schema'), link(R.module(sc.owner), sc.owner, 'badge')], sc.description),
        h('div', { class: 'strip' }, [
          stat({ l: 'Properties', v: sc.properties.length }), stat({ l: 'References ($ref)', v: sc.refs.length }),
          stat({ l: 'Referenced by', v: usedBy.length + usedOps.length })])],
      body, plain: true
    };
  }
  function opRow(o) {
    return h('div', { class: 'item' }, [h('span', { class: 'badge verb verb-' + o.method.toLowerCase() }, o.method), link(R.operation(o.id), o.path, 'grow'), h('span', { class: 'sub' }, o.id)]);
  }

  function tableView(t) {
    const qs = model.queries.filter((q) => q.tables.indexOf(t.name) >= 0);
    const refd = model.tables.filter((x) => x.columns.some((c) => c.references === t.name));
    const pk = t.columns.filter((c) => c.primary_key).map((c) => c.name);
    const byMod = groupBy(qs, (q) => q.module);
    const body = h('div', { class: 'col' }, [
      section('Columns', h('div', { class: 'card' }, h('div', { class: 'tbl-wrap' }, h('table', { class: 'tbl' }, [
        h('thead', null, h('tr', null, ['Column', 'Type', 'Null?', 'Key'].map((x) => h('th', null, x)))),
        h('tbody', null, t.columns.map((c) => h('tr', null, [h('td', null, h('code', null, c.name)), h('td', null, h('code', null, c.type)),
          h('td', null, c.nullable ? 'nullable' : h('span', { class: 'dim' }, 'not null')),
          h('td', null, [c.primary_key ? h('span', { class: 'pk' }, 'PK') : null, ' ', c.references ? link(R.table(c.references), '→ ' + c.references, 'badge') : null])])))])))),
      refd.length ? section('Referenced by tables', h('div', { class: 'chips' }, refd.map((x) => link(R.table(x.name), x.name, 'badge')))) : null,
      section('Queries (' + qs.length + ')', qs.length ? byMod.map((g) => h('div', { class: 'card group' }, [
        h('h4', null, [link(R.module(g.key), g.key), badge(g.items.length)]),
        list(g.items.map((q) => h('div', { class: 'item' }, [h('span', { class: 'qk ' + q.kind }, ':' + q.kind), h('code', { class: 'grow' }, q.id), src(q.file, q.line)])))])) : empty('No query touches this table.')),
      section('Migrations', t.migrations.length ? h('div', { class: 'card' }, list(t.migrations.map((f) => h('div', { class: 'item' }, src(f, 0))))) : empty('None.'))
    ]);
    return {
      crumbs: [SYS].concat(t.owners[0] ? [crumb(t.owners[0], R.module(t.owners[0]))] : [], [crumb(t.name)]),
      head: [h('div', { class: 'hero ' + diffClass('tables', t.name) }, h('div', { class: 'main' }, [
        h('h1', null, [h('span', { class: 'rm' }, t.name), diffBadge('tables', t.name)]),
        h('div', { class: 'meta' }, [badge('table', 'kind', 'background:var(--c-table)'), t.owners.length ? [h('span', { class: 'sub' }, 'Owned by'), t.owners.map((o) => link(R.module(o), o, 'badge'))] : badge('no Module queries it', 'mute')])])),
      h('div', { class: 'strip' }, [stat({ l: 'Columns', v: t.columns.length }), stat({ l: 'Primary key', v: pk.join(', ') || 'none', vcls: 'txt' }),
        stat({ l: 'Queries', v: qs.length }), stat({ l: 'Referenced by', v: refd.length, s: 'tables' })])],
      body, plain: true
    };
  }

  function functionView(f) {
    const impl = model.operations.filter((o) => o.implementation && o.implementation.function === f.id);
    const hasCov = typeof f.coverage === 'number';
    const body = h('div', { class: 'col' }, [
      h('div', { class: 'card' }, h('dl', { class: 'kv' }, [
        h('dt', null, 'Module'), h('dd', null, link(R.module(f.module), f.module)),
        h('dt', null, 'Source'), h('dd', null, src(f.file, f.line)),
        h('dt', null, 'Lines'), h('dd', null, f.line + '–' + f.end_line)])),
      impl.length ? section('Implements', h('div', { class: 'card' }, list(impl.map(opRow)))) : null,
      section('What the numbers mean', h('div', { class: 'card' }, [
        h('div', { class: 'why' }, [h('b', null, 'CC: '), rationale.cc || '']),
        hasCov || typeof f.crap === 'number' ? h('div', { class: 'why', style: 'margin-top:8px' }, [h('b', null, 'CRAP: '), rationale.crap || '']) : h('p', { class: 'sub' }, 'No coverage data for this function, so CRAP is not computed. Build with --coverprofile to add it.')]))
    ]);
    return {
      crumbs: [SYS, crumb(f.module, R.module(f.module)), crumb(funcLabel(f))],
      head: [simpleHero(funcLabel(f), [badge('function', 'kind', 'background:var(--c-function)'), link(R.module(f.module), f.module, 'badge'), src(f.file, f.line)]),
        h('div', { class: 'strip' }, [
          stat({ l: 'Cyclomatic complexity', v: f.cc, title: rationale.cc }),
          stat({ l: 'Coverage', v: hasCov ? pct(f.coverage) : '–', s: hasCov ? f.covered + ' of ' + f.statements + ' statements' : 'no coverage data' }),
          stat({ l: 'CRAP', v: typeof f.crap === 'number' ? fmt(f.crap, 1) : '–', title: rationale.crap, s: crapBucket(f.crap) ? 'high: complex and weakly tested' : null })])],
      body, plain: true
    };
  }

  function edgeView(a, b) {
    const d = edgeByKey[a + '>' + b];
    const vs = violByEdge[a + '>' + b] || [];
    const holder = {};
    const body = h('div', { class: 'col' }, [
      vs.map((v) => violationBox(v, false)),
      !vs.length && d ? h('div', { class: 'card' }, h('p', { style: 'margin:0' }, d.test_only ? 'Only _test.go files create this edge, so no rule applies to it.' : 'This edge follows every rule.')) : null,
      h('div', { class: 'card', style: 'margin-top:12px' }, [h('div', { class: 'neigh', id: 'edgegraph', style: 'height:260px' })]),
      d && d.sites.length ? section('Imported at', h('div', { class: 'card' }, siteLinks(d.sites))) : null
    ]);
    holder.show = () => edgeGraph(a, b);
    return {
      crumbs: [SYS, crumb(a, R.module(a)), crumb('imports ' + b)],
      head: [simpleHero([link(R.module(a), a), ' → ', link(R.module(b), b)],
        [d ? badge(d.test_only ? 'test-only edge' : 'production edge', d.test_only ? 'mute' : '') : badge('no such edge', 'warn'),
          vs.length ? badge(plural(vs.length, 'violation'), 'bad') : null])],
      body, plain: true, after: holder.show
    };
  }

  // ---- system view --------------------------------------------------------------------------------
  function hotspotData() {
    const fs = model.functions.slice().sort((a, b) => hasCoverage ? ((b.crap || 0) - (a.crap || 0) || b.cc - a.cc) : b.cc - a.cc);
    const pain = model.modules.filter((m) => m.metrics && m.metrics.zone === 'zone of pain' && m.kind !== 'foundation' && m.kind !== 'test_only');
    return { funcs: fs.slice(0, 3), pain };
  }
  function focusLevel(kind) {
    if (!current || !current.tv) return;
    current.tv.select('modules');
    const el = document.querySelector('.level[data-kind="' + kind + '"]');
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    el.classList.add('flash');
    setTimeout(() => el.classList.remove('flash'), 1400);
  }
  function systemStrip() {
    const tiles = [];
    const totals = diffTotals();
    if (totals) {
      const nv = (diff.diff.violations.new || []).length;
      tiles.push(stat({ cls: 'wide', l: 'Architecture diff', v: [h('span', { class: 'plus' }, '+' + totals.added), ' ', h('span', { style: 'color:var(--removed)' }, '−' + totals.removed), ' ', h('span', { style: 'color:var(--changed)' }, '~' + totals.changed)],
        s: diff.headline || 'No changes.', onclick: () => current && current.tv && current.tv.select('changes'), title: 'Added, removed and changed Modules, dependencies, operations and tables' }));
      tiles.push(stat({ cls: nv ? 'bad' : 'ok', l: 'New violations', v: nv, s: nv ? 'introduced by this change' : 'none introduced', onclick: nv ? () => current.tv.select('violations') : null }));
    }
    const nv = model.violations.length;
    tiles.push(stat({ cls: nv ? 'bad' : 'ok', l: 'Rules', v: nv ? plural(nv, 'violation') : 'All ' + model.rules.length + ' pass', vcls: 'sm',
      s: nv ? 'click to see why and how to fix' : 'Dependencies follow the rules',
      onclick: () => (current && current.tv ? current.tv.select(nv ? 'violations' : 'rules') : null) }));
    const kinds = model.levels.concat(model.modules.some((m) => m.kind === 'test_only') ? ['test_only'] : []);
    tiles.push(stat({ cls: 'wide', l: 'Modules', v: model.modules.length, s: h('span', { class: 'chips' }, kinds.map((k) => {
      const n = model.modules.filter((m) => m.kind === k).length;
      return h('span', { class: 'chipbtn', role: 'button', tabindex: 0, style: 'background:var(--c-' + k + ')', title: 'Jump to ' + kindLabel(k),
        onclick: (e) => { e.stopPropagation(); focusLevel(k); }, onkeydown: (e) => { if (e.key === 'Enter') focusLevel(k); } }, [kindLabel(k), ' ' + n]);
    })) }));
    tiles.push(stat({ l: 'Operations', v: model.operations.length, s: 'API endpoints' }));
    tiles.push(stat({ l: 'Tables', v: model.tables.length, s: plural(model.queries.length, 'query', 'queries') }));
    const hs = hotspotData();
    tiles.push(stat({ cls: 'wide', l: 'Hotspots', v: '', s: null }));
    const hot = tiles[tiles.length - 1];
    hot.querySelector('.v').remove();
    hot.appendChild(h('div', { class: 'hot' }, hs.funcs.map((f) => link(R.fn(f.id), [h('span', null, [funcLabel(f), h('span', { class: 'faint' }, '  ' + f.module)]), h('span', { class: 'n' }, typeof f.crap === 'number' ? 'CRAP ' + fmt(f.crap, 1) : 'CC ' + f.cc)]))));
    hot.appendChild(h('div', { class: 's', style: 'margin-top:4px' }, hs.pain.length ? ['In the zone of pain:'].concat(hs.pain.slice(0, 4).map((m) => link(R.module(m.id), m.id, 'badge soft-bad')), hs.pain.length > 4 ? [h('span', { class: 'faint' }, '+' + (hs.pain.length - 4) + ' more')] : []) : 'No Module in the zone of pain.'));
    if (!hs.funcs.length) hot.querySelector('.hot').replaceWith(h('div', { class: 's' }, 'No functions in the model.'));
    return h('div', { class: 'strip' }, tiles);
  }

  function modRow(m) {
    const x = m.metrics, o = opCount[m.id] || 0, t = tableCount[m.id] || 0;
    return h('a', { class: 'mod ' + (m.kind === 'test_only' ? 'test ' : '') + diffClass('modules', m.id), href: '#' + R.module(m.id), style: '--lc:var(--c-' + m.kind + ')', 'data-mod': m.id,
      onmouseenter: () => highlightModule(m.id), onmouseleave: () => highlightModule(null) }, [
      h('span', { class: 'top' }, [
        h('span', { class: 'name rm' }, m.id), modViol[m.id] ? h('span', { class: 'viol', title: 'In a rule violation' }) : null, diffBadge('modules', m.id),
        m.generated && !m.present ? badge('generated, not built', 'warn') : null,
        h('span', { class: 'grow' }),
        o ? h('span', { class: 'badge', title: plural(o, 'operation') }, [icon('bolt'), o]) : null,
        t ? h('span', { class: 'badge', title: plural(t, 'table') }, [icon('grid'), t]) : null,
        x ? h('span', { class: 'badge', title: 'Worst cyclomatic complexity: ' + (x.worst_cc_function || '') }, 'CC ' + x.worst_cc) : null
      ]),
      h('span', { class: 'sum clamp' }, m.summary || (m.generated ? 'generated' : 'no package doc comment'))
    ]);
  }
  function modulesPane() {
    const kinds = model.levels.concat(['test_only']);
    const out = [];
    kinds.forEach((k) => {
      const ms = model.modules.filter((m) => m.kind === k).sort((a, b) => a.id.localeCompare(b.id));
      if (!ms.length) return;
      out.push(h('section', { class: 'level', 'data-kind': k, style: '--lc:var(--c-' + k + ')' }, [
        h('header', null, [h('b', null, [kindLabel(k), ' ', h('span', { class: 'n' }, ms.length)]), h('span', { class: 'cap' }, KIND_CAPTION[k] || '')]),
        h('div', { class: 'mods' }, ms.map(modRow))]));
    });
    return h('div', null, out);
  }
  function violationsPane() {
    return h('div', null, model.violations.map((v) => violationBox(v, true)));
  }
  function rulesPane() {
    const out = model.rules.map((r) => {
      const n = model.violations.filter((v) => v.rule === r.id).length;
      return h('details', { class: 'rule' }, [
        h('summary', null, [h('b', null, r.id), badge(r.severity, r.severity === 'error' ? 'soft-bad' : 'warn'), n ? badge(plural(n, 'violation'), 'bad') : badge('passes', 'ok')]),
        h('div', null, [h('p', { class: 'why' }, [h('b', null, 'Why: '), r.rationale]), h('p', { class: 'fix' }, [h('b', null, 'Fix: '), r.fix])])]);
    });
    if (model.warnings.length) {
      out.push(section('Build warnings (' + model.warnings.length + ')', h('div', { class: 'card' }, list(model.warnings.map((w) =>
        h('div', { class: 'item' }, [badge(w.code, 'warn'), h('span', { class: 'grow' }, w.message), w.path ? h('code', { class: 'path' }, w.path) : null])))) ));
    }
    return h('div', null, out);
  }

  function changesPane() {
    const d = diff.diff;
    const out = [h('div', { class: 'card diffbar' }, [h('h3', null, 'Architecture diff'), h('p', { style: 'margin:0' }, diff.headline || 'No changes.'),
      h('p', { class: 'sub' }, 'Green = added, red with strikethrough = removed (ghosted from the base model), amber = changed.')])];
    const nv = d.violations.new || [];
    if (nv.length) out.push(section('New violations (' + nv.length + ')', nv.map((v) => violationBox(v, true))));
    if ((d.violations.resolved || []).length) out.push(h('p', null, badge(d.violations.resolved.length + ' violation(s) resolved', 'diff diff-added')));
    const row = (cls, label, kids) => h('div', { class: 'item ' + cls }, [badge(label, 'diff ' + cls)].concat(kids));
    const groups = [];
    function group(title, items) { if (items.length) groups.push(h('div', { class: 'changelist' }, [h('h4', null, title), list(items)])); }
    let it = [];
    d.modules.added.forEach((m) => it.push(row('diff-added', 'added', [link(R.module(m.id), m.id)])));
    d.modules.removed.forEach((m) => it.push(row('diff-removed', 'removed', [link(R.module(m.id), m.id, 'rm')])));
    d.modules.kind_changed.forEach((k) => it.push(row('diff-changed', 'changed', [link(R.module(k.id), k.id), h('span', { class: 'sub' }, kindLabel(k.from) + ' to ' + kindLabel(k.to))])));
    group('Modules', it); it = [];
    const dep = (cls, label, e) => it.push(row(cls, label, [link(R.edge(e.from, e.to), e.from + ' → ' + e.to), e.test_only ? badge('test-only', 'mute') : null]));
    d.dependencies.added.forEach((e) => dep('diff-added', 'added', e));
    d.dependencies.removed.forEach((e) => dep('diff-removed', 'removed', e));
    d.dependencies.changed.forEach((e) => it.push(row('diff-changed', 'changed', [link(R.edge(e.from, e.to), e.from + ' → ' + e.to), h('span', { class: 'sub' }, e.now_test_only ? 'now test-only' : 'now production')])));
    group('Dependencies', it); it = [];
    d.operations.added.forEach((o) => it.push(row('diff-added', 'added', [link(R.operation(o.id), o.method + ' ' + o.path), h('span', { class: 'sub' }, o.id)])));
    d.operations.removed.forEach((o) => it.push(row('diff-removed', 'removed', [link(R.operation(o.id), o.method + ' ' + o.path, 'rm'), h('span', { class: 'sub' }, o.id)])));
    d.operations.changed.forEach((o) => it.push(row('diff-changed', 'changed', [link(R.operation(o.id), o.method + ' ' + o.path), h('span', { class: 'sub' }, o.changes.join('; '))])));
    group('Operations', it); it = [];
    d.tables.added.forEach((t) => it.push(row('diff-added', 'added', [link(R.table(t.name), t.name)])));
    d.tables.removed.forEach((t) => it.push(row('diff-removed', 'removed', [link(R.table(t.name), t.name, 'rm')])));
    d.tables.changed.forEach((t) => {
      const parts = t.columns_added.map((c) => '+' + c).concat(t.columns_removed.map((c) => '−' + c), t.columns_changed.map((c) => '~' + c));
      it.push(row('diff-changed', 'changed', [link(R.table(t.name), t.name), h('span', { class: 'sub' }, parts.join(' '))]));
    });
    group('Tables', it); it = [];
    d.metrics.forEach((m) => it.push(row('diff-changed', 'metrics', [link(R.module(m.module), m.module), h('span', { class: 'sub' }, m.reasons.join('; '))])));
    group('Metric changes', it);
    return h('div', null, out.concat(groups));
  }

  function systemView() {
    const nv = model.violations.length;
    const side = [{ id: 'modules', label: 'Modules', n: model.modules.length, scroll: true, build: modulesPane }];
    if (diff && diff.diff) side.unshift({ id: 'changes', label: 'Changes', scroll: true, build: changesPane });
    if (nv) side.push({ id: 'violations', label: 'Violations', n: nv, bad: true, scroll: true, build: violationsPane });
    side.push({ id: 'rules', label: 'Rules', n: model.rules.length, scroll: true, build: rulesPane });
    const holder = {};
    holder.tv = tabView('system', [
      { name: 'stage', cls: 'stage', tabs: [mapTab(), chartTab()] },
      { name: 'side', cls: 'side', tabs: side, def: diff && diff.diff ? 'changes' : 'modules' }
    ], { split: true, phoneDefault: diff && diff.diff ? 'changes' : 'modules',
      phoneOrder: ['changes', 'modules', 'violations', 'map', 'chart', 'rules'] });
    const nTest = model.modules.filter((m) => m.kind === 'test_only').length;
    const hero = h('div', { class: 'hero', style: 'padding:0 0 var(--sp-3)' }, h('div', { class: 'main' }, [
      h('p', { class: 'sub', style: 'margin:0;font-size:var(--fs-md)' }, [h('b', { style: 'color:var(--ink)' }, (model.project || 'Architecture') + ': '),  plural(model.modules.length - nTest, 'Module') + ' in ' + model.levels.length + ' Levels' + (nTest ? ', plus ' + nTest + ' test-only' : '') + '. Dependencies point inward: Composition root, then Domain, then Infrastructure, then Foundation.'])]));
    return { crumbs: [SYS], head: [hero, systemStrip()], body: holder.tv.node, tv: holder.tv, split: true };
  }

  // ---- tabs ---------------------------------------------------------------------------------------------
  const narrow = window.matchMedia('(max-width: 899px)');
  const tabMemo = {};
  // cols: [{name, cls, tabs:[{id,label,n,bad,scroll,build,show}], def}]. Panes are built the
  // first time they are shown. In split mode (system view) there are two columns on a wide
  // screen and one combined segmented control on a phone.
  function tabView(key, cols, opt) {
    const memo = (tabMemo[key] = tabMemo[key] || {});
    const all = [];
    cols.forEach((c) => c.tabs.forEach((t) => { t.col = c; all.push(t); }));
    const byId = (id) => all.find((t) => t.id === id);
    cols.forEach((c) => { c.sel = byId(memo[c.name]) && byId(memo[c.name]).col === c ? memo[c.name] : (c.def || c.tabs[0].id); });
    const phoneList = opt.phoneOrder ? opt.phoneOrder.map(byId).filter(Boolean) : all;
    let phoneSel = byId(memo.phone) ? memo.phone : (opt.phoneDefault || phoneList[0].id);
    const root = h('div', { class: opt.split ? 'split-cols' : 'col' });
    const buttons = [];
    function mkButton(t) {
      const b = h('button', { type: 'button', role: 'tab', 'data-tab': t.id, onclick: () => self.select(t.id) },
        [t.label, t.n != null ? h('span', { class: 'n' + (t.bad ? ' bad' : '') }, t.n) : null]);
      buttons.push([t, b]);
      return b;
    }
    const colEls = [];
    let phoneBar = null;
    if (opt.split) {
      phoneBar = h('div', { class: 'seg phoneseg', role: 'tablist' }, phoneList.map(mkButton));
      root.appendChild(phoneBar);
    }
    cols.forEach((c) => {
      const holder = opt.split ? h('div', { class: 'pane-col ' + c.cls }) : root;
      const bar = c.tabs.length > 1 ? h('div', { class: 'seg' + (opt.split ? '' : ' tabbar'), role: 'tablist' }, c.tabs.map(mkButton)) : null;
      if (bar) holder.appendChild(bar);
      const panes = h('div', { class: 'panes' });
      c.tabs.forEach((t) => {
        t.pane = h('div', { class: 'pane' + (t.scroll ? ' scroll' : ''), role: 'tabpanel', hidden: true });
        panes.appendChild(t.pane);
      });
      holder.appendChild(panes);
      if (opt.split) { root.appendChild(holder); colEls.push([c, holder]); }
    });
    const self = {
      node: root,
      select(id) {
        const t = byId(id);
        if (!t) return;
        t.col.sel = id; phoneSel = id;
        memo[t.col.name] = id; memo.phone = id;
        self.apply();
      },
      apply() {
        const phone = narrow.matches && opt.split;
        all.forEach((t) => {
          const on = phone ? t.id === phoneSel : t.id === t.col.sel;
          if (on && !t.built) { t.pane.appendChild(t.build()); t.built = true; }
          t.pane.hidden = !on;
        });
        colEls.forEach((p) => { p[1].hidden = phone && !p[0].tabs.some((t) => t.id === phoneSel); });
        buttons.forEach((p) => {
          const t = p[0], on = (opt.split && narrow.matches && p[1].parentNode === phoneBar) ? t.id === phoneSel : t.id === t.col.sel;
          p[1].setAttribute('aria-selected', String(on));
        });
        all.forEach((t) => { if (!t.pane.hidden && t.show) t.show(); });
      }
    };
    return self;
  }

  // ---- node cards (SVG backgrounds) and graph plumbing ---------------------------------------------
  const esc = (t) => String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  const FONT = 'system-ui,-apple-system,Segoe UI,Roboto,sans-serif';
  const NODE_H = 46;
  function nodeWidth(id) { return Math.max(118, Math.min(236, Math.min(id.length, 26) * 8.3 + 46)); }
  function nodeCard(m, w) {
    const ink = cssVar('--ink'), ink3 = cssVar('--ink-3'), surf = cssVar('--surface'), col = cssVar('--c-' + m.kind), viol = cssVar('--violation');
    const o = opCount[m.id] || 0, t = tableCount[m.id] || 0;
    const name = m.id.length > 26 ? m.id.slice(0, 25) + '…' : m.id;
    let x = 18, sub = '';
    const icoY = NODE_H - 21;
    if (o) {
      sub += '<path transform="translate(' + x + ',' + icoY + ') scale(.85)" fill="' + ink3 + '" d="M6 0L0 7h4l-1 5 6-7H5z"/><text x="' + (x + 13) + '" y="' + (NODE_H - 11) + '" font-size="12" font-weight="600" fill="' + ink3 + '">' + o + '</text>';
      x += 13 + String(o).length * 7 + 12;
    }
    if (t) {
      sub += '<path transform="translate(' + x + ',' + (icoY + 1) + ') scale(.9)" fill="' + ink3 + '" d="M0 0h4v4H0zM6 0h4v4H6zM0 6h4v4H0zM6 6h4v4H6z"/><text x="' + (x + 14) + '" y="' + (NODE_H - 11) + '" font-size="12" font-weight="600" fill="' + ink3 + '">' + t + '</text>';
    }
    if (!o && !t) sub = '<text x="18" y="' + (NODE_H - 11) + '" font-size="11.5" fill="' + ink3 + '">' + (m.kind === 'test_only' ? 'test-only' : 'used by ' + prodIn(m.id).length) + '</text>';
    const dot = modViol[m.id] ? '<circle cx="' + (w - 14) + '" cy="14" r="5.5" fill="' + viol + '"/>' : '';
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="' + w + '" height="' + NODE_H + '" viewBox="0 0 ' + w + ' ' + NODE_H + '"><rect width="' + w + '" height="' + NODE_H + '" fill="' + surf + '"/><rect width="7" height="' + NODE_H + '" fill="' + col + '"/><text x="18" y="21" font-family="' + FONT + '" font-size="14.5" font-weight="700" fill="' + ink + '">' + esc(name) + '</text><g font-family="' + FONT + '">' + sub + '</g>' + dot + '</svg>';
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(svg);
  }
  function modNodeData(m, extraCls) {
    const w = nodeWidth(m.id);
    const worst = m.metrics && typeof m.metrics.worst_crap === 'number' ? crapBucket(m.metrics.worst_crap) : null;
    return {
      group: 'nodes',
      data: { id: 'm:' + m.id, mod: m.id, w, h: NODE_H, img: nodeCard(m, w), border: worst === 'bad' ? cssVar('--crap-bad') : worst === 'warn' ? cssVar('--crap-warn') : cssVar('--line-2') },
      classes: [m.kind === 'test_only' ? 'testonly' : '', m.generated && !m.present ? 'dashed' : '', diffClass('modules', m.id), extraCls || ''].join(' ').trim()
    };
  }
  function depEdgeData(d) {
    const bad = violByEdge[d.from + '>' + d.to];
    const test = d.test_only || (mods[d.from] && mods[d.from].kind === 'test_only');
    return { group: 'edges', data: { id: 'd:' + d.from + '>' + d.to, source: 'm:' + d.from, target: 'm:' + d.to, dep: d },
      classes: [test ? 'test' : '', bad ? 'violation' : '', diffClass('edges', d.from + '>' + d.to)].join(' ').trim() };
  }
  function cyStyle() {
    const v = (n) => cssVar(n);
    const edge = v('--edge');
    return [
      { selector: 'node', style: { 'width': 'data(w)', 'height': 'data(h)', 'shape': 'round-rectangle', 'corner-radius': 9, 'background-color': v('--surface'),
        'background-image': 'data(img)', 'background-fit': 'cover', 'background-clip': 'node', 'background-image-opacity': 1, 'border-width': 1.5, 'border-color': 'data(border)', 'label': '' } },
      { selector: 'node.dashed', style: { 'border-style': 'dashed', 'opacity': 0.8 } },
      { selector: 'node.testonly', style: { 'opacity': 0.6, 'border-style': 'dashed' } },
      { selector: 'node.diff-added', style: { 'border-width': 4, 'border-color': v('--added') } },
      { selector: 'node.diff-changed', style: { 'border-width': 4, 'border-color': v('--changed') } },
      { selector: 'node.diff-removed', style: { 'border-width': 3, 'border-color': v('--removed'), 'border-style': 'dashed', 'opacity': 0.6 } },
      { selector: 'node.center', style: { 'border-width': 3.5, 'border-color': v('--accent') } },
      { selector: 'node.nout', style: { 'border-width': 2.5, 'border-color': v('--out') } },
      { selector: 'node.nin', style: { 'border-width': 2.5, 'border-color': v('--in') } },
      { selector: 'node.hot', style: { 'border-width': 3.5, 'border-color': v('--accent') } },
      { selector: 'node.faded', style: { 'opacity': 0.22 } },
      { selector: 'edge', style: { 'curve-style': 'bezier', 'width': 1.4, 'line-color': edge, 'target-arrow-color': edge, 'target-arrow-shape': 'triangle', 'arrow-scale': 0.9, 'opacity': 0.4, 'label': '' } },
      { selector: 'edge.test', style: { 'line-style': 'dashed', 'opacity': 0.3 } },
      { selector: 'edge.hide', style: { 'display': 'none' } },
      { selector: 'edge.out', style: { 'line-color': v('--out'), 'target-arrow-color': v('--out'), 'width': 2.8, 'opacity': 1, 'z-index': 50 } },
      { selector: 'edge.in', style: { 'line-color': v('--in'), 'target-arrow-color': v('--in'), 'width': 2.8, 'opacity': 1, 'z-index': 50 } },
      { selector: 'edge.faded', style: { 'opacity': 0.05 } },
      { selector: 'edge.diff-added', style: { 'line-color': v('--added'), 'target-arrow-color': v('--added'), 'width': 3, 'opacity': 1 } },
      { selector: 'edge.diff-changed', style: { 'line-color': v('--changed'), 'target-arrow-color': v('--changed'), 'width': 3, 'opacity': 1 } },
      { selector: 'edge.diff-removed', style: { 'line-color': v('--removed'), 'target-arrow-color': v('--removed'), 'line-style': 'dashed', 'width': 2.4, 'opacity': 0.9 } },
      { selector: 'edge.violation', style: { 'line-color': v('--violation'), 'target-arrow-color': v('--violation'), 'width': 4, 'opacity': 1, 'z-index': 99, 'arrow-scale': 1.2 } },
      { selector: 'edge.violation.faded', style: { 'opacity': 0.45 } },
      { selector: 'edge.selected', style: { 'overlay-color': v('--accent'), 'overlay-opacity': 0.25, 'overlay-padding': 6 } }
    ];
  }
  const cys = [];
  function newCy(container, extra) {
    if (typeof cytoscape === 'undefined') { container.textContent = 'The graph library failed to load.'; return null; }
    try { if (!cytoscape('layout', 'dagre') && window.cytoscapeDagre) window.cytoscapeDagre(cytoscape); } catch (e) { /* already registered */ }
    const cy = cytoscape(Object.assign({ container, style: cyStyle(), minZoom: 0.2, maxZoom: 2.5, boxSelectionEnabled: false }, extra || {}));
    cys.push(cy);
    return cy;
  }
  function destroyGraphs() {
    cys.splice(0).forEach((c) => { try { c.destroy(); } catch (e) { /* already gone */ } });
    systemMap = null;
  }

  // ---- the system map: Level lanes ------------------------------------------------------------------------
  let systemMap = null;
  const mapOpts = { testEdges: false };
  const LANE_LABEL_W = 196;

  function planLanes(availW, availH, phone) {
    const kinds = model.levels.concat(['test_only']);
    const lanes = [];
    kinds.forEach((k) => {
      const ms = model.modules.filter((m) => m.kind === k);
      if (ms.length) lanes.push({ kind: k, mods: ms.map((m) => ({ m, w: nodeWidth(m.id), x: 0 })).sort((a, b) => a.m.id.localeCompare(b.m.id)) });
    });
    const W = phone ? Math.max(300, availW) : Math.max(620, availW * 1.02);
    const gap = 18;
    const rowsOf = (lane) => {
      const rows = [[]];
      let used = 0;
      lane.mods.forEach((n) => {
        const cur = rows[rows.length - 1];
        if (cur.length && used + n.w + gap > W) { rows.push([]); used = 0; }
        rows[rows.length - 1].push(n); used += n.w + gap;
      });
      // balance rows so a lane of 11 becomes 6 + 5, not 10 + 1
      if (rows.length > 1) {
        const total = lane.mods.length, per = Math.ceil(total / rows.length), bal = [];
        for (let i = 0; i < total; i += per) bal.push(lane.mods.slice(i, i + per));
        if (bal.every((r) => r.reduce((a, n) => a + n.w + gap, 0) <= W + 1)) return bal;
      }
      return rows;
    };
    const place = () => lanes.forEach((lane) => {
      lane.rows = rowsOf(lane);
      lane.rows.forEach((row) => {
        const sum = row.reduce((a, n) => a + n.w, 0);
        const g = Math.min(120, (W - sum) / row.length);
        let x = (W - (sum + g * (row.length - 1))) / 2;
        row.forEach((n) => { n.x = x + n.w / 2; x += n.w + g; });
      });
    });
    place();
    const pos = {};
    const refresh = () => lanes.forEach((l) => l.mods.forEach((n) => { pos[n.m.id] = n; }));
    refresh();
    const nbrs = (id) => prodIn(id).map((d) => d.from).concat(prodOut(id).map((d) => d.to)).filter((x) => pos[x]);
    for (let it = 0; it < 6; it++) {
      lanes.forEach((lane) => {
        if (lane.kind === 'test_only') return;
        lane.mods.forEach((n) => {
          const nb = nbrs(n.m.id);
          n.bary = nb.length ? nb.reduce((a, id) => a + pos[id].x, 0) / nb.length : n.x;
        });
        lane.mods.sort((a, b) => a.bary - b.bary || a.m.id.localeCompare(b.m.id));
      });
      place();
    }
    // vertical geometry in model units
    const padY = 20, rowGap = 18, minH = phone ? 0 : 98;
    let y = 0;
    const heights = lanes.map((lane) => Math.max(minH, padY * 2 + lane.rows.length * NODE_H + (lane.rows.length - 1) * rowGap));
    let total = heights.reduce((a, b) => a + b, 0);
    // spare vertical room goes to the lanes, so the map fills its box
    const zw = Math.min(1.15, availW / W), spare = availH && !phone ? availH / (zw * 0.98) - total : 0;
    if (spare > 0) { const add = Math.min(spare, 60 * lanes.length) / lanes.length; heights.forEach((x, i) => { heights[i] = x + add; }); }
    lanes.forEach((lane, li) => {
      const nRows = lane.rows.length;
      lane.h = heights[li];
      lane.top = y;
      const inner = nRows * NODE_H + (nRows - 1) * rowGap, start = y + (lane.h - inner) / 2;
      lane.rows.forEach((row, r) => row.forEach((n) => { n.y = start + r * (NODE_H + rowGap) + NODE_H / 2; }));
      y += lane.h;
    });
    return { lanes, W, H: y, pos };
  }

  function mapTab() {
    const tab = {
      id: 'map', label: 'Map',
      build() {
        const lanesEl = h('div', { class: 'lanes' });
        const cyEl = h('div', { class: 'cy' });
        const cywrap = h('div', { class: 'cywrap' }, cyEl);
        const tip = h('div', { id: 'tip', hidden: true });
        const sheet = h('div', { class: 'sheet', hidden: true });
        const wrap = h('div', { class: 'mapwrap' }, [lanesEl, cywrap, tip, sheet]);
        const test = h('input', { type: 'checkbox', id: 'opt-testEdges', onchange: (e) => { mapOpts.testEdges = e.target.checked; if (systemMap) systemMap.cy.edges('.test').toggleClass('hide', !mapOpts.testEdges); } });
        test.checked = mapOpts.testEdges;
        const tools = h('div', { class: 'maptools' }, [
          h('div', { class: 'key' }, [
            h('span', null, [h('i', { style: 'border-color:var(--edge)' }), 'dependency']),
            h('span', null, [h('i', { style: 'border-color:var(--violation)' }), 'rule violation']),
            h('span', { class: 'faint' }, 'Hover a Module:'),
            h('span', null, [h('i', { style: 'border-color:var(--out)' }), 'it depends on']),
            h('span', null, [h('i', { style: 'border-color:var(--in)' }), 'uses it'])]),
          h('span', { class: 'grow' }),
          h('label', null, [test, 'Show test-only imports']),
          h('button', { class: 'btn', type: 'button', onclick: () => systemMap && fitMap() }, 'Reset view')
        ]);
        tab.parts = { lanesEl, cyEl, cywrap, tip, sheet, wrap };
        return h('div', { class: 'mapbox' }, [tools, wrap]);
      },
      show() { initMap(tab); }
    };
    return tab;
  }

  let selectedMod = null, hoverMod = null;
  function initMap(tab) {
    const p = tab.parts;
    const phone = narrow.matches;
    const labelW = phone ? 0 : LANE_LABEL_W;
    p.cywrap.style.left = labelW + 'px';
    const availW = p.wrap.clientWidth - labelW, availH = p.wrap.clientHeight;
    if (availW < 50 || availH < 50) return;
    if (systemMap && systemMap.tab === tab) { systemMap.cy.resize(); fitMap(); return; }
    const plan = planLanes(availW, availH, phone);
    const cy = newCy(p.cyEl, { autoungrabify: true });
    if (!cy) return;
    const nodes = [], edges = [];
    plan.lanes.forEach((lane) => lane.mods.forEach((n) => { const d = modNodeData(n.m); d.position = { x: n.x, y: n.y }; nodes.push(d); }));
    model.dependencies.forEach((d) => {
      if (!plan.pos[d.from] || !plan.pos[d.to]) return;
      const e = depEdgeData(d);
      if (/\btest\b/.test(e.classes) && !mapOpts.testEdges) e.classes += ' hide';
      edges.push(e);
    });
    cy.add(nodes); cy.add(edges);
    cy.autounselectify(true);
    systemMap = { tab, cy, plan, labelW };
    // lane bands (HTML behind the transparent canvas)
    p.lanesEl.textContent = '';
    p.lanesEl.appendChild(h('div', { class: 'laneflow' }, h('span', null, 'dependencies point down ↓')));
    plan.lanes.forEach((lane) => {
      lane.el = h('div', { class: 'lane' + (lane.kind === 'test_only' ? ' test' : ''), style: '--lc:var(--c-' + lane.kind + ')' }, [
        h('div', { class: 'lbl' }, [h('b', null, kindLabel(lane.kind)), h('span', null, LANE_CAPTION[lane.kind] || '')])]);
      p.lanesEl.appendChild(lane.el);
    });
    const sync = () => {
      const z = cy.zoom(), pan = cy.pan();
      plan.lanes.forEach((lane) => { lane.el.style.top = (lane.top * z + pan.y) + 'px'; lane.el.style.height = (lane.h * z) + 'px'; });
    };
    cy.on('viewport resize', sync);
    // interaction
    const show = () => applyFocus();
    cy.on('mouseover', 'node', (e) => { hoverMod = e.target.data('mod'); show(); showNodeTip(e, p); });
    cy.on('mouseout', 'node', () => { hoverMod = null; show(); p.tip.hidden = true; });
    cy.on('tap', 'node', (e) => {
      const id = e.target.data('mod');
      if (selectedMod === id && !narrow.matches) { go(R.module(id)); return; }
      selectedMod = id; show(); showModSheet(mods[id], p);
    });
    cy.on('dbltap', 'node', (e) => go(R.module(e.target.data('mod'))));
    cy.on('tap', 'edge', (e) => { if (e.target.data('dep')) { selectedMod = null; show(); showEdgeSheet(e.target.data('dep'), p); } });
    cy.on('mouseover', 'edge', (e) => { if (e.target.data('dep') && !e.target.hasClass('faded')) showEdgeTip(e, p); });
    cy.on('mouseout', 'edge', () => { p.tip.hidden = true; });
    cy.on('tap', (e) => { if (e.target === cy) { selectedMod = null; show(); p.sheet.hidden = true; } });
    cy.on('pan zoom', () => { p.tip.hidden = true; });
    fitMap();
    if (selectedMod && mods[selectedMod]) { applyFocus(); }
  }
  function fitMap() {
    const m = systemMap;
    if (!m) return;
    const p = m.tab.parts;
    m.cy.resize();
    const availW = p.wrap.clientWidth - m.labelW, availH = p.wrap.clientHeight;
    const phone = narrow.matches;
    let z = phone ? availW / m.plan.W : Math.min(availW / m.plan.W, availH / m.plan.H);
    z = Math.max(0.3, Math.min(1.15, z * 0.98));
    const px = (availW - m.plan.W * z) / 2;
    const py = phone ? 0 : Math.max(0, (availH - m.plan.H * z) / 2);
    m.cy.viewport({ zoom: z, pan: { x: px, y: py } });
    m.cy.emit('viewport');
  }
  function highlightModule(id) {
    if (!systemMap) return;
    hoverMod = id;
    applyFocus();
  }
  function applyFocus() {
    if (!systemMap) return;
    const cy = systemMap.cy, target = hoverMod || selectedMod;
    cy.batch(() => {
      cy.elements().removeClass('faded out in nout nin hot');
      if (!target) return;
      const n = cy.getElementById('m:' + target);
      if (!n.length) return;
      const nb = n.closedNeighborhood();
      cy.elements().not(nb).addClass('faded');
      n.outgoers('edge').addClass('out'); n.incomers('edge').addClass('in');
      n.outgoers('node').addClass('nout'); n.incomers('node').addClass('nin');
      n.addClass('hot');
    });
  }
  function tipAt(p, e, kids) {
    const tip = p.tip, pos = e.renderedPosition || e.target.renderedMidpoint();
    tip.textContent = ''; add(tip, kids); tip.hidden = false;
    const left = pos.x + (p.cywrap.offsetLeft || 0);
    const W = p.wrap.clientWidth;
    tip.style.left = Math.max(4, Math.min(left + 14, W - tip.offsetWidth - 6)) + 'px';
    tip.style.top = Math.max(4, Math.min(pos.y + 18, p.wrap.clientHeight - tip.offsetHeight - 6)) + 'px';
  }
  function showNodeTip(e, p) {
    const m = mods[e.target.data('mod')];
    const out = prodOut(m.id).length, inn = prodIn(m.id).length;
    tipAt(p, e, [h('div', null, h('b', null, m.id)), h('div', null, [kindLabel(m.kind), ' · ']),
      h('div', null, [h('span', { class: 'o' }, 'depends on ' + out), ' · ', h('span', { class: 'i' }, 'used by ' + inn)]),
      modViol[m.id] ? h('div', { class: 'v' }, 'In a rule violation') : null, h('div', { style: 'opacity:.7;margin-top:2px' }, 'Click to select, click again to open')]);
    // the kind line is a plain-text sibling; keep the tooltip compact
    p.tip.children[1].textContent = kindLabel(m.kind);
  }
  function showEdgeTip(e, p) {
    const d = e.target.data('dep'), vs = violByEdge[d.from + '>' + d.to] || [];
    tipAt(p, e, [h('div', null, h('b', null, d.from + ' → ' + d.to)),
      vs.length ? vs.map((v) => h('div', { class: 'v' }, v.rule + ': ' + v.message)) : h('div', null, d.test_only ? 'Test-only import' : 'Follows every rule'),
      h('div', { style: 'opacity:.7' }, 'Click for details')]);
  }
  function sheetClose(sheet) {
    return h('button', { class: 'close', 'aria-label': 'Close', type: 'button', onclick: () => { sheet.hidden = true; selectedMod = null; applyFocus(); } }, '×');
  }
  function showModSheet(m, p) {
    const sheet = p.sheet;
    sheet.textContent = ''; sheet.className = 'sheet' + (modViol[m.id] ? ' bad' : '');
    sheet.appendChild(sheetClose(sheet));
    sheet.appendChild(h('h4', null, [m.id, kindBadge(m.kind)]));
    if (m.summary) sheet.appendChild(h('p', { class: 'sub clamp' }, m.summary));
    sheet.appendChild(h('p', null, [h('b', { style: 'color:var(--out)' }, 'Depends on ' + prodOut(m.id).length), ' · ', h('b', { style: 'color:var(--in)' }, 'Used by ' + prodIn(m.id).length)]));
    sheet.appendChild(h('div', { class: 'acts' }, [link(R.module(m.id), 'Open ' + m.id, 'btn primary')]));
    sheet.hidden = false;
  }
  function showEdgeSheet(d, p) {
    const sheet = p.sheet, vs = violByEdge[d.from + '>' + d.to] || [];
    sheet.textContent = ''; sheet.className = 'sheet' + (vs.length ? ' bad' : '');
    sheet.appendChild(sheetClose(sheet));
    sheet.appendChild(h('h4', null, [d.from + ' → ' + d.to, badge(d.test_only ? 'test-only' : 'production', 'mute')]));
    vs.forEach((v) => sheet.appendChild(violationBox(v, false)));
    if (!vs.length) sheet.appendChild(h('p', { class: 'sub' }, d.test_only ? 'Only _test.go files create this edge, so no rule applies to it.' : 'This edge follows every rule.'));
    sheet.appendChild(h('div', { class: 'acts' }, [link(R.edge(d.from, d.to), 'Open edge page', 'btn')]));
    sheet.hidden = false;
  }

  // ---- small focused graphs (Module neighbors, an edge) -------------------------------------------------------
  function neighborGraph(m) {
    const el = $('neigh');
    if (!el || narrow.matches || el.offsetWidth < 50) return;
    if (el.dataset.drawn === m.id) { const c = cys[cys.length - 1]; if (c) { c.resize(); c.fit(undefined, 20); } return; }
    el.dataset.drawn = m.id;
    const cy = newCy(el);
    if (!cy) return;
    const seen = {}, els = [];
    const addMod = (id, cls) => { if (seen[id] || !mods[id]) return; seen[id] = 1; els.push(modNodeData(mods[id], cls)); };
    addMod(m.id, 'center');
    (depsTo[m.id] || []).forEach((d) => { addMod(d.from); els.push(depEdgeData(d)); });
    (depsFrom[m.id] || []).forEach((d) => { addMod(d.to); els.push(depEdgeData(d)); });
    cy.add(els);
    cy.autounselectify(true);
    cy.layout({ name: 'dagre', rankDir: 'TB', nodeSep: 22, rankSep: 70, edgeSep: 10, fit: true, padding: 20, animate: false }).run();
    if (cy.zoom() > 1.1) { cy.zoom(1.1); cy.center(); }
    cy.on('tap', 'node', (e) => { const id = e.target.data('mod'); if (id !== m.id) go(R.module(id)); });
    cy.on('tap', 'edge', (e) => { const d = e.target.data('dep'); if (d) go(R.edge(d.from, d.to)); });
    cy.on('mouseover', 'node', (e) => { cy.elements().removeClass('faded'); const n = e.target; cy.elements().not(n.closedNeighborhood()).addClass('faded'); });
    cy.on('mouseout', 'node', () => cy.elements().removeClass('faded'));
  }
  function edgeGraph(a, b) {
    const el = $('edgegraph');
    if (!el || el.dataset.drawn) return;
    el.dataset.drawn = '1';
    const cy = newCy(el);
    if (!cy) return;
    const els = [];
    if (mods[a]) els.push(modNodeData(mods[a]));
    if (mods[b]) els.push(modNodeData(mods[b]));
    const d = edgeByKey[a + '>' + b];
    if (d) els.push(depEdgeData(d));
    cy.add(els);
    cy.autounselectify(true);
    cy.layout({ name: 'dagre', rankDir: 'TB', nodeSep: 40, rankSep: 90, fit: true, padding: 24, animate: false }).run();
    if (cy.zoom() > 1.2) { cy.zoom(1.2); cy.center(); }
    cy.on('tap', 'node', (e) => go(R.module(e.target.data('mod'))));
  }

  // ---- routes and shell ------------------------------------------------------------------------------------------
  function parseRoute() {
    const hs = location.hash.replace(/^#/, '');
    if (!hs || hs === '/') return { t: 'system' };
    const p = hs.split('/').slice(1).map((x) => { try { return decodeURIComponent(x); } catch (e) { return x; } });
    return { t: p[0], a: p[1], b: p[2] };
  }
  function crumb(label, route) { return { label, route }; }
  const SYS = crumb('System', R.system());

  // view(route) returns {crumbs, head, body, split?, tv?} or null for an unknown entity.
  function view(r) {
    let m, o, sc, t, f;
    switch (r.t) {
      case 'system': return systemView();
      case 'module': return (m = mods[r.a]) ? moduleView(m) : null;
      case 'operation': return (o = ops[r.a]) ? operationView(o) : null;
      case 'schema': return (sc = schemas[r.a]) ? schemaView(sc) : null;
      case 'table': return (t = tables[r.a]) ? tableView(t) : null;
      case 'function': return (f = funcs[r.a]) ? functionView(f) : null;
      case 'edge': return mods[r.a] && mods[r.b] ? edgeView(r.a, r.b) : null;
    }
    return null;
  }

  let current = null, lastKey = null;
  function paintHealth() {
    const pill = $('health'), n = model.violations.length;
    pill.className = 'pill ' + (n ? 'bad' : 'ok');
    pill.textContent = n ? plural(n, 'violation') : 'All ' + model.rules.length + ' rules pass';
    pill.title = n ? 'Jump to the violations' : 'Show the rules and why they exist';
    pill.setAttribute('href', '#/');
  }
  $('health').addEventListener('click', (e) => {
    e.preventDefault();
    const n = model.violations.length, tab = n ? 'violations' : 'rules';
    const memo = (tabMemo.system = tabMemo.system || {});
    memo.side = tab; memo.phone = tab;
    if (parseRoute().t === 'system' && current && current.tv) current.tv.select(tab);
    else go(R.system());
  });

  function render(force) {
    const r = parseRoute();
    let v = view.call(null, r);
    destroyGraphs();
    const key = location.hash;
    if (!v) {
      v = { crumbs: [SYS, crumb('Not found')], head: [h('div', { class: 'hero' }, h('div', { class: 'main' }, h('h1', null, 'Not found')))],
        body: h('div', { class: 'col' }, [h('p', null, 'Nothing in the model matches ' + JSON.stringify(location.hash) + '. It may have been renamed or removed.'), link(R.system(), 'Back to the system', 'btn')]), plain: true };
    }
    current = v;
    document.body.classList.toggle('split', !!v.split);
    const page = $('page'), keep = key === lastKey && force ? page.scrollTop : 0;
    const cr = $('crumbs');
    cr.textContent = '';
    v.crumbs.forEach((c, i) => {
      if (i) cr.appendChild(h('span', { class: 'sep' }, '›'));
      cr.appendChild(c.route && i < v.crumbs.length - 1 ? link(c.route, c.label) : h('span', { class: 'cur' }, c.label));
    });
    const head = $('head');
    head.textContent = '';
    head.appendChild(h('div', { class: v.split ? '' : 'col' }, v.head));
    const body = $('body');
    body.textContent = '';
    body.appendChild(v.split ? v.body : (v.plain ? v.body : h('div', { class: 'col' }, v.body)));
    document.title = v.crumbs[v.crumbs.length - 1].label + ' - ' + (model.project || 'Architecture') + ' Explorer';
    if (v.tv) v.tv.apply();
    if (v.after) v.after();
    page.scrollTop = keep;
    lastKey = key;
  }
  function go(route) { location.hash = '#' + route; }

  narrow.addEventListener('change', () => render(true));
  let resizeTimer = null;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => { if (systemMap) { systemMap.cy.resize(); fitMap(); } }, 80);
  });
  window.addEventListener('hashchange', () => { selectedMod = null; hoverMod = null; render(false); });

  // ---- search ------------------------------------------------------------------------------------------------------
  const searchIdx = [];
  model.modules.forEach((m) => searchIdx.push({ k: 'Module', l: m.id, s: m.summary, r: R.module(m.id), t: m.id }));
  model.operations.forEach((o) => searchIdx.push({ k: 'Operation', l: o.id, s: o.method + ' ' + o.path, r: R.operation(o.id), t: o.id + ' ' + o.path }));
  model.tables.forEach((t) => searchIdx.push({ k: 'Table', l: t.name, s: '', r: R.table(t.name), t: t.name }));
  model.schemas.forEach((sc) => searchIdx.push({ k: 'Schema', l: sc.id, s: '', r: R.schema(sc.id), t: sc.id }));
  model.functions.forEach((f) => searchIdx.push({ k: 'Function', l: f.id, s: f.module, r: R.fn(f.id), t: f.id }));
  let sel = 0;
  function runSearch() {
    const q = $('search').value.trim().toLowerCase(), box = $('results');
    box.textContent = '';
    if (!q) { box.hidden = true; return; }
    const hits = searchIdx.filter((e) => e.t.toLowerCase().indexOf(q) >= 0);
    hits.sort((a, b) => {
      const sa = a.l.toLowerCase().indexOf(q), sb = b.l.toLowerCase().indexOf(q);
      return (sa < 0 ? 99 : sa) - (sb < 0 ? 99 : sb) || a.l.length - b.l.length;
    });
    hits.slice(0, 12).forEach((e, i) => {
      box.appendChild(h('a', { href: '#' + e.r, class: i === 0 ? 'sel' : '', onclick: clearSearch }, [badge(e.k, e.k === 'Module' ? 'kind' : '', e.k === 'Module' ? 'background:var(--accent)' : ''), h('b', null, e.l), h('span', { class: 'sub clamp' }, e.s)]));
    });
    if (!hits.length) box.appendChild(h('div', { class: 'none sub' }, 'No match.'));
    sel = 0;
    box.hidden = false;
  }
  function clearSearch() { $('search').value = ''; $('results').hidden = true; $('search').blur(); }
  $('search').addEventListener('input', runSearch);
  $('search').addEventListener('keydown', (e) => {
    const items = $('results').querySelectorAll('a');
    if (e.key === 'Escape') clearSearch();
    else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      if (!items.length) return;
      e.preventDefault();
      items[sel].classList.remove('sel');
      sel = (sel + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length;
      items[sel].classList.add('sel');
    } else if (e.key === 'Enter' && items.length) { const a = items[sel]; location.hash = a.getAttribute('href'); clearSearch(); }
  });
  document.addEventListener('click', (e) => { if (!$('searchwrap').contains(e.target)) $('results').hidden = true; });
  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && document.activeElement !== $('search') && !/input|textarea/i.test(document.activeElement.tagName)) { e.preventDefault(); $('search').focus(); }
  });

  // Live updates from `archmodel serve`.
  function showBanner(text) {
    const b = $('banner');
    b.hidden = !text;
    b.textContent = text || '';
  }
  if (cfg.live && window.EventSource && /^https?:$/.test(location.protocol)) {
    const es = new EventSource('/events');
    es.addEventListener('reload', () => location.reload());
    es.addEventListener('builderror', (e) => {
      let msg = e.data;
      try { msg = JSON.parse(e.data).message; } catch (x) { /* plain text */ }
      showBanner('Rebuild failed; showing the last good model.\n' + msg);
    });
  }

  $('projname').textContent = model.project || 'Architecture';
  paintThemeButton();
  paintHealth();
  render(false);
})();
