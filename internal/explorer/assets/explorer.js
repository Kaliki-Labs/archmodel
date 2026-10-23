// Architecture Explorer. Vanilla JS; the graph is Cytoscape.js + dagre.
// Reads the Architecture model from #archmodel-model, page settings from
// #archmodel-config and an optional diff object from #archmodel-diff (the seam
// for diff mode: {added:{modules,edges,operations,tables}, removed:{...}} with
// edges written "from>to"). All teaching text (rule and metric Rationales,
// metric readings) comes from the model.
(function () {
  'use strict';

  function $(id) { return document.getElementById(id); }
  var model = JSON.parse($('archmodel-model').textContent);
  var cfg = JSON.parse($('archmodel-config').textContent);
  var diff = $('archmodel-diff') ? JSON.parse($('archmodel-diff').textContent) : null;

  // ---- indexes ------------------------------------------------------------
  var mods = {}, ops = {}, schemas = {}, tables = {}, funcs = {};
  model.modules.forEach(function (m) { mods[m.id] = m; });
  model.operations.forEach(function (o) { ops[o.id] = o; });
  model.schemas.forEach(function (s) { schemas[s.id] = s; });
  model.tables.forEach(function (t) { tables[t.name] = t; });
  model.functions.forEach(function (f) { funcs[f.id] = f; });
  var depsFrom = {}, depsTo = {}, edgeByKey = {};
  model.dependencies.forEach(function (d) {
    (depsFrom[d.from] = depsFrom[d.from] || []).push(d);
    (depsTo[d.to] = depsTo[d.to] || []).push(d);
    edgeByKey[d.from + '>' + d.to] = d;
  });
  var violByEdge = {}, violByModule = {};
  model.violations.forEach(function (v) {
    var map = v.to ? violByEdge : violByModule;
    var key = v.to ? v.module + '>' + v.to : v.module;
    (map[key] = map[key] || []).push(v);
  });
  var rationale = model.metric_rationales || {};

  var KIND_LABEL = {
    composition_root: 'Composition root', domain: 'Domain',
    infrastructure: 'Infrastructure module', foundation: 'Foundation',
    test_only: 'Test-only module'
  };
  function kindLabel(k) { return KIND_LABEL[k] || k; }

  // ---- small DOM helpers (never innerHTML: model text is data) -------------
  function h(tag, attrs, kids) {
    var e = document.createElement(tag);
    if (attrs) Object.keys(attrs).forEach(function (k) {
      var v = attrs[k];
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
    if (Array.isArray(kids)) { kids.forEach(function (k) { add(e, k); }); return; }
    e.appendChild(typeof kids === 'object' ? kids : document.createTextNode(String(kids)));
  }
  function enc(s) { return encodeURIComponent(s); }
  var R = {
    system: function () { return '/'; },
    module: function (id) { return '/module/' + enc(id); },
    operation: function (id) { return '/operation/' + enc(id); },
    schema: function (id) { return '/schema/' + enc(id); },
    table: function (n) { return '/table/' + enc(n); },
    fn: function (id) { return '/function/' + enc(id); },
    edge: function (a, b) { return '/edge/' + enc(a) + '/' + enc(b); }
  };
  function link(route, text, cls) { return h('a', { href: '#' + route, class: cls }, text); }
  function chip(text, cls, style) { return h('span', { class: 'chip ' + (cls || ''), style: style }, text); }
  function kindChip(kind) { return chip(kindLabel(kind), 'kind', 'background:var(--c-' + kind + ')'); }
  function empty(text) { return h('p', { class: 'empty' }, text); }
  function section(title, body) { return [h('h3', null, title), body]; }
  function list(items) {
    return h('ul', { class: 'list' }, items.map(function (i) { return h('li', null, i); }));
  }
  function fmt(n, d) { return typeof n === 'number' ? n.toFixed(d == null ? 2 : d) : '-'; }
  function pct(n) { return typeof n === 'number' ? Math.round(n * 1000) / 10 + '%' : '-'; }
  function diffClass(kind, id) {
    if (!diff) return '';
    var a = diff.added && diff.added[kind], r = diff.removed && diff.removed[kind];
    if (a && a.indexOf(id) >= 0) return 'diff-added';
    if (r && r.indexOf(id) >= 0) return 'diff-removed';
    return '';
  }

  // Source link: a configured URL template, else copyable path:line text.
  function src(path, line, label) {
    var text = label || (path + (line ? ':' + line : ''));
    var t = cfg.source.url_template, ref = cfg.source.ref;
    if (t && !(t.indexOf('{ref}') >= 0 && !ref)) {
      var url = t.replace(/\{path\}/g, encodeURI(path)).replace(/\{line\}/g, String(line || 1))
        .replace(/\{ref\}/g, encodeURIComponent(ref || ''));
      return h('a', { href: url, target: '_blank', rel: 'noopener', class: 'path' }, text);
    }
    return h('code', { class: 'path' }, text);
  }
  function siteLinks(sites) {
    return list((sites || []).map(function (s) {
      return h('div', { class: 'item' }, [src(s.file, s.line), s.test ? chip('test file') : null]);
    }));
  }

  // ---- model queries --------------------------------------------------------
  function levelText(m) {
    if (m.level == null) return 'outside the production graph';
    return 'Level ' + (m.level + 1) + ' of ' + model.levels.length + ' (1 = outermost)';
  }
  function funcForSymbol(s) {
    var recv = (s.receiver || '').replace(/^\*/, '');
    var id = s.module + '.' + (recv ? recv + '.' : '') + s.name;
    return funcs[id] ? id : null;
  }
  function moduleFunctions(id) { return model.functions.filter(function (f) { return f.module === id; }); }
  function funcLabel(f) { return (f.receiver ? f.receiver.replace(/^\*/, '') + '.' : '') + f.name; }

  // ---- cards ----------------------------------------------------------------
  function violationBox(v) {
    return h('div', { class: 'violbox' }, [
      h('h4', null, (v.severity === 'error' ? 'Violation' : 'Warning') + ': ' + v.rule),
      h('p', null, h('b', null, v.message)),
      h('p', { class: 'why' }, [h('b', null, 'Why: '), v.rationale]),
      h('p', { class: 'rule-fix' }, [h('b', null, 'Fix: '), v.fix]),
      v.sites && v.sites.length ? siteLinks(v.sites) : null
    ]);
  }

  function edgeDetail(from, to) {
    var d = edgeByKey[from + '>' + to];
    var vs = violByEdge[from + '>' + to] || [];
    var kids = [h('p', null, [link(R.module(from), from), ' imports ', link(R.module(to), to), ' ',
      chip(d ? (d.test_only ? 'test-only edge' : 'production edge') : 'no such edge')])];
    vs.forEach(function (v) { kids.push(violationBox(v)); });
    if (!vs.length && d) kids.push(h('p', { class: 'sub' }, d.test_only
      ? 'Only _test.go files create this edge, so no rule applies to it.'
      : 'This edge follows every rule.'));
    if (d && d.sites.length) kids.push(section('Imported at', siteLinks(d.sites)));
    return kids;
  }

  function moduleLinkItem(id, edge) {
    var m = mods[id];
    var bad = edge && violByEdge[edge.from + '>' + edge.to];
    return h('div', { class: 'item' }, [
      link(R.module(id), id, 'grow'),
      m ? chip(kindLabel(m.kind), 'kind', 'background:var(--c-' + m.kind + ')') : null,
      edge && edge.test_only ? chip('test-only') : null,
      bad ? link(R.edge(edge.from, edge.to), 'violates ' + bad.map(function (v) { return v.rule; }).join(', '), 'chip bad') : null
    ]);
  }
  function depList(deps, side) {
    if (!deps.length) return empty('None.');
    return list(deps.map(function (d) { return moduleLinkItem(d[side], d); }));
  }

  function metricRow(label, value, key, note) {
    return h('details', null, [
      h('summary', null, h('span', { class: 'metric' }, [h('span', { class: 'v' }, value), h('span', null, label)])),
      h('div', null, [note ? h('p', { class: 'sub' }, note) : null,
        h('p', { class: 'why' }, rationale[key] || 'No Rationale recorded in the model.')])
    ]);
  }

  function metricsBlock(m) {
    var x = m.metrics;
    if (!x) return empty('Test-only modules are outside the production graph, so they have no package metrics.');
    var rows = [
      metricRow('Afferent coupling (Ca): modules that import this one', x.ca, 'ca'),
      metricRow('Efferent coupling (Ce): modules this one imports', x.ce, 'ce'),
      metricRow('Instability (I)', x.instability_defined ? fmt(x.instability) : 'n/a', 'i',
        x.instability_defined ? null : 'Ca + Ce = 0, so I is undefined (recorded as 0).'),
      metricRow('Abstractness (A): ' + x.abstract_types + ' interfaces of ' + x.exported_types + ' exported types', fmt(x.abstractness), 'a'),
      metricRow('Distance (D) from the main sequence', fmt(x.distance), 'd'),
      metricRow('Worst cyclomatic complexity (CC)' + (x.worst_cc_function ? ': ' + x.worst_cc_function : ''), String(x.worst_cc), 'cc')
    ];
    if (typeof x.worst_crap === 'number') {
      rows.push(metricRow('Worst CRAP' + (x.worst_crap_function ? ': ' + x.worst_crap_function : ''), fmt(x.worst_crap, 1), 'crap'));
    }
    if (typeof x.coverage === 'number') rows.push(metricRow('Coverage', pct(x.coverage), 'crap'));
    return [x.reading ? h('div', { class: 'card' }, x.reading) : null, rows];
  }

  function funcItem(f) {
    return h('div', { class: 'item' }, [
      link(R.fn(f.id), funcLabel(f), 'grow'),
      chip('CC ' + f.cc),
      typeof f.crap === 'number' ? chip('CRAP ' + fmt(f.crap, 1)) : null,
      typeof f.coverage === 'number' ? chip(pct(f.coverage) + ' covered') : null
    ]);
  }

  function worstFunctions(id) {
    var fs = moduleFunctions(id);
    if (!fs.length) return empty('No functions.');
    var hasCrap = fs.some(function (f) { return typeof f.crap === 'number'; });
    fs = fs.slice().sort(function (a, b) {
      return hasCrap ? (b.crap || 0) - (a.crap || 0) || b.cc - a.cc : b.cc - a.cc;
    }).slice(0, 5);
    return [h('p', { class: 'sub' }, hasCrap ? 'Highest CRAP first.' : 'Highest cyclomatic complexity first. Build with --coverprofile to add coverage and CRAP.'),
      list(fs.map(funcItem))];
  }

  function surface(id) {
    var syms = model.symbols.filter(function (s) { return s.module === id; });
    if (!syms.length) return empty('No exported types or functions.');
    var order = ['interface', 'struct', 'type', 'func', 'method'];
    syms.sort(function (a, b) { return order.indexOf(a.kind) - order.indexOf(b.kind); });
    var body = list(syms.map(function (s) {
      var fid = (s.kind === 'func' || s.kind === 'method') ? funcForSymbol(s) : null;
      var name = (s.receiver ? s.receiver.replace(/^\*/, '') + '.' : '') + s.name;
      return h('div', { class: 'item' }, [
        h('span', { class: 'm' }, s.kind),
        fid ? link(R.fn(fid), name, 'grow') : h('code', { class: 'grow' }, name),
        src(s.file, s.line)
      ]);
    }));
    return h('details', syms.length > 25 ? null : { open: true }, [
      h('summary', null, syms.length + ' exported'), body]);
  }

  function ownsBlock(id) {
    var o = model.operations.filter(function (x) { return x.owner === id; });
    var t = model.tables.filter(function (x) { return x.owners.indexOf(id) >= 0; });
    var q = model.queries.filter(function (x) { return x.module === id; });
    var out = [];
    out.push(h('h3', null, 'Owns: operations (' + o.length + ')'));
    out.push(o.length ? list(o.map(opItem)) : empty(mods[id].kind === 'infrastructure' || mods[id].kind === 'foundation'
      ? 'None. Infrastructure modules and Foundation own no API operations.' : 'None.'));
    out.push(h('h3', null, 'Owns: tables (' + t.length + ')'));
    out.push(t.length ? list(t.map(function (x) {
      return h('div', { class: 'item' }, [link(R.table(x.name), x.name, 'grow'), chip(x.columns.length + ' columns')]);
    })) : empty('None.'));
    out.push(h('h3', null, 'Owns: queries (' + q.length + ')'));
    out.push(q.length ? list(q.map(queryItem)) : empty('None.'));
    return out;
  }
  function opItem(o) {
    return h('div', { class: 'item' }, [
      h('span', { class: 'm' }, o.method),
      link(R.operation(o.id), o.path, 'grow'),
      h('span', { class: 'sub' }, o.id)
    ]);
  }
  function queryItem(q) {
    return h('div', { class: 'item' }, [
      h('code', { class: 'grow' }, q.id), chip(q.kind),
      q.tables.map(function (t) { return link(R.table(t), t, 'chip'); }),
      src(q.file, q.line)
    ]);
  }

  function subpackages(m) {
    if (!m.subpackages.length) return null;
    return [h('h3', null, 'Subpackages (' + m.subpackages.length + ')'), list(m.subpackages.map(function (s) {
      return h('div', { class: 'item' }, [
        h('span', { class: 'grow' }, s.present ? src(s.dir, 0) : h('code', { class: 'path' }, s.dir)),
        s.generated ? chip(s.present ? 'generated' : 'gen, not built', s.present ? '' : 'warn') : null,
        s.purpose ? h('span', { class: 'sub' }, s.purpose) : null,
        s.present ? null : h('span', { class: 'sub' }, 'not on disk (run codegen to see it)')
      ]);
    }))];
  }

  function moduleCard(m) {
    var vs = violByModule[m.id] || [];
    var outEdges = (depsFrom[m.id] || []);
    var inEdges = (depsTo[m.id] || []);
    var bad = [];
    outEdges.forEach(function (d) { (violByEdge[d.from + '>' + d.to] || []).forEach(function (v) { bad.push(v); }); });
    inEdges.forEach(function (d) { (violByEdge[d.from + '>' + d.to] || []).forEach(function (v) { if (bad.indexOf(v) < 0) bad.push(v); }); });
    return [
      h('h2', { class: diffClass('modules', m.id) }, m.id),
      h('div', { class: 'meta' }, [kindChip(m.kind), chip(levelText(m)),
        m.generated ? chip(m.present ? 'generated' : 'gen, not built', m.present ? '' : 'warn') : null]),
      m.purpose ? h('p', { class: 'purpose' }, m.purpose)
        : m.generated ? h('p', { class: 'sub' }, 'Generated code: it has no package doc comment, and none is expected.')
        : h('p', { class: 'bad-text' }, 'No package doc comment, so there is no Purpose to show.'),
      vs.concat(bad).map(violationBox),
      h('h3', null, 'Metrics'), metricsBlock(m),
      h('h3', null, 'Depends on (' + outEdges.length + ')'), depList(outEdges, 'to'),
      h('h3', null, 'Depended on by (' + inEdges.length + ')'), depList(inEdges, 'from'),
      ownsBlock(m.id),
      h('h3', null, 'Public surface'), surface(m.id),
      subpackages(m),
      h('h3', null, 'Worst functions'), worstFunctions(m.id),
      h('h3', null, 'Source'), h('p', null, src(m.dir, 0))
    ];
  }

  function systemCard() {
    var prodEdges = model.dependencies.filter(function (d) { return !d.test_only; }).length;
    var out = [
      h('h2', null, 'System'),
      h('p', { class: 'sub' }, model.modules.length + ' modules, ' + prodEdges + ' production dependencies, ' +
        model.operations.length + ' operations, ' + model.tables.length + ' tables.')
    ];
    out.push(h('h3', null, 'Violations (' + model.violations.length + ')'));
    out.push(model.violations.length ? model.violations.map(function (v) {
      return h('div', null, [violationBox(v),
        h('p', null, v.to ? link(R.edge(v.module, v.to), 'Show edge ' + v.subject) : link(R.module(v.module), 'Open ' + v.module))]);
    }) : h('p', null, chip('All ' + model.rules.length + ' rules pass', 'kind', 'background:var(--added)')));
    var levelsToShow = model.levels.slice();
    levelsToShow.forEach(function (k) {
      var ms = model.modules.filter(function (m) { return m.kind === k; });
      out.push(h('h3', null, kindLabel(k) + ' (' + ms.length + ')'));
      out.push(ms.length ? list(ms.map(modItem)) : empty('None.'));
    });
    var to = model.modules.filter(function (m) { return m.kind === 'test_only'; });
    if (to.length) {
      out.push(h('h3', null, 'Test-only modules (' + to.length + ')'));
      out.push(h('p', { class: 'sub' }, 'Only _test.go files import these, so they sit outside the production graph.'));
      out.push(list(to.map(modItem)));
    }
    out.push(h('h3', null, 'Rules'));
    out.push(model.rules.map(function (r) {
      return h('details', null, [
        h('summary', null, [h('b', null, r.id), chip(r.severity, r.severity === 'error' ? 'bad' : 'warn')]),
        h('div', null, [h('p', { class: 'why' }, [h('b', null, 'Why: '), r.rationale]),
          h('p', { class: 'rule-fix' }, [h('b', null, 'Fix: '), r.fix])])
      ]);
    }));
    if (model.warnings.length) {
      out.push(h('h3', null, 'Build warnings (' + model.warnings.length + ')'));
      out.push(list(model.warnings.map(function (w) {
        return h('div', { class: 'item' }, [chip(w.code, 'warn'), h('span', { class: 'grow' }, w.message), w.path ? h('code', { class: 'path' }, w.path) : null]);
      })));
    }
    return out;
  }
  function modItem(m) {
    return h('div', { class: 'item modrow ' + diffClass('modules', m.id) }, [
      link(R.module(m.id), m.id, 'name'),
      h('span', { class: 'sub summary' }, m.summary || (m.generated ? 'generated' : 'no package doc')),
      m.generated && !m.present ? chip('gen, not built', 'warn') : null
    ]);
  }

  function operationCard(o) {
    var kv = [];
    function row(k, v) { kv.push(h('dt', null, k), h('dd', null, v)); }
    row('Owner', link(R.module(o.owner), o.owner));
    row('Security', o.security.length ? o.security.join(', ') : 'public (no security scheme)');
    if (o.rate_limit) row('Rate limit', h('code', null, JSON.stringify(o.rate_limit)));
    if (o.tags.length) row('Tags', o.tags.join(', '));
    row('Spec', src(o.spec, o.line));
    if (o.implementation) row('Implemented by', [link(R.fn(o.implementation.function), o.implementation.function), ' ', src(o.implementation.file, o.implementation.line)]);
    else row('Implemented by', 'no matching Go method found');
    return [
      h('h2', { class: diffClass('operations', o.id) }, o.id),
      h('div', { class: 'meta' }, [chip(o.method, 'kind', 'background:var(--c-operation)'), h('code', null, o.path)]),
      o.summary ? h('p', null, o.summary) : null,
      h('dl', { class: 'kv' }, kv),
      h('h3', null, 'Request schemas'),
      o.request_schemas.length ? list(o.request_schemas.map(schemaItem)) : empty('No request body.'),
      o.parameters.length ? [h('h3', null, 'Parameters'), h('div', { class: 'tbl-wrap' }, h('table', { class: 'tbl' }, o.parameters.map(function (p) {
        return h('tr', null, [h('td', null, h('code', null, p.name)), h('td', null, p.in), h('td', null, p.type || ''), h('td', null, p.required ? 'required' : 'optional')]);
      })))] : null,
      h('h3', null, 'Documented responses'),
      list(o.responses.map(function (r) {
        return h('div', { class: 'item' }, [chip(r.status, /^[45]/.test(r.status) ? 'warn' : ''), h('span', { class: 'grow' }, r.description),
          r.schemas.map(function (s) { return schemaLink(s); })]);
      }))
    ];
  }
  function schemaLink(id) { return schemas[id] ? link(R.schema(id), id, 'chip') : chip(id); }
  function schemaItem(id) { return h('div', { class: 'item' }, schemaLink(id)); }

  function schemaCard(s) {
    var usedBy = model.schemas.filter(function (x) { return x.refs.indexOf(s.id) >= 0; });
    var usedOps = model.operations.filter(function (o) {
      return o.request_schemas.indexOf(s.id) >= 0 || o.responses.some(function (r) { return r.schemas.indexOf(s.id) >= 0; });
    });
    return [
      h('h2', null, s.id),
      h('div', { class: 'meta' }, [chip(s.type || 'schema'), link(R.module(s.owner), s.owner, 'chip')]),
      s.description ? h('p', null, s.description) : null,
      h('h3', null, 'Properties'),
      s.properties.length ? h('div', { class: 'tbl-wrap' }, h('table', { class: 'tbl' }, s.properties.map(function (p) {
        return h('tr', null, [h('td', null, h('code', null, p.name)), h('td', null, p.type + (p.format ? ' (' + p.format + ')' : '')),
          h('td', null, p.required ? 'required' : ''), h('td', null, (p.refs || []).map(schemaLink))]);
      }))) : empty('No properties.'),
      h('h3', null, 'References ($ref)'), s.refs.length ? list(s.refs.map(schemaItem)) : empty('None.'),
      h('h3', null, 'Referenced by'),
      usedBy.length || usedOps.length ? list(usedBy.map(function (x) { return schemaItem(x.id); })
        .concat(usedOps.map(function (o) { return opItem(o); }))) : empty('Nothing.'),
      h('h3', null, 'Source'), h('p', null, src(s.spec, 0))
    ];
  }

  function tableCard(t) {
    var qs = model.queries.filter(function (q) { return q.tables.indexOf(t.name) >= 0; });
    var refd = model.tables.filter(function (x) { return x.columns.some(function (c) { return c.references === t.name; }); });
    return [
      h('h2', { class: diffClass('tables', t.name) }, t.name),
      h('h3', null, 'Owners'),
      t.owners.length ? list(t.owners.map(function (o) { return moduleLinkItem(o); })) : empty('No module queries this table.'),
      h('h3', null, 'Columns (' + t.columns.length + ')'),
      h('div', { class: 'tbl-wrap' }, h('table', { class: 'tbl' }, t.columns.map(function (c) {
        return h('tr', null, [h('td', null, h('code', null, c.name)), h('td', null, c.type + (c.nullable ? '' : ' not null')),
          h('td', null, [c.primary_key ? chip('PK') : null, c.references ? link(R.table(c.references), '-> ' + c.references, 'chip') : null])]);
      }))),
      refd.length ? [h('h3', null, 'Referenced by tables'), list(refd.map(function (x) { return h('div', { class: 'item' }, link(R.table(x.name), x.name)); }))] : null,
      h('h3', null, 'Queries touching it (' + qs.length + ')'),
      qs.length ? list(qs.map(function (q) {
        return h('div', { class: 'item' }, [h('code', { class: 'grow' }, q.id), chip(q.kind), link(R.module(q.module), q.module, 'chip'), src(q.file, q.line)]);
      })) : empty('None.'),
      h('h3', null, 'Migrations'), list(t.migrations.map(function (f) { return h('div', { class: 'item' }, src(f, 0)); }))
    ];
  }

  function functionCard(f) {
    var impl = model.operations.filter(function (o) { return o.implementation && o.implementation.function === f.id; });
    var kv = [];
    function row(k, v) { kv.push(h('dt', null, k), h('dd', null, v)); }
    row('Module', link(R.module(f.module), f.module));
    row('Source', src(f.file, f.line));
    row('Lines', f.line + '-' + f.end_line);
    return [
      h('h2', null, funcLabel(f)),
      h('dl', { class: 'kv' }, kv),
      impl.length ? [h('h3', null, 'Implements'), list(impl.map(opItem))] : null,
      h('h3', null, 'Metrics'),
      metricRow('Cyclomatic complexity (CC)', String(f.cc), 'cc'),
      typeof f.coverage === 'number'
        ? [metricRow('Coverage', pct(f.coverage) + ' (' + f.covered + ' of ' + f.statements + ' statements)', 'crap'),
          metricRow('CRAP', fmt(f.crap, 1), 'crap')]
        : h('p', { class: 'sub' }, 'No coverage data for this function, so CRAP is not computed. Build with --coverprofile to add it.')
    ];
  }

  function edgeCard(a, b) {
    return [h('h2', null, a + ' -> ' + b), edgeDetail(a, b)];
  }

  // ---- routes ---------------------------------------------------------------
  function parseRoute() {
    var hs = location.hash.replace(/^#/, '');
    if (!hs || hs === '/') return { t: 'system' };
    var p = hs.split('/').slice(1).map(function (s) { try { return decodeURIComponent(s); } catch (e) { return s; } });
    return { t: p[0], a: p[1], b: p[2] };
  }

  function crumb(label, route) { return { label: label, route: route }; }
  var SYS = crumb('System', R.system());

  // view(route) returns {crumbs, card, graph} or null when the entity is unknown.
  function view(r) {
    var m, o, s, t, f;
    switch (r.t) {
      case 'system':
        return { crumbs: [SYS], card: systemCard(), graph: systemGraph() };
      case 'module':
        if (!(m = mods[r.a])) return null;
        return { crumbs: [SYS, crumb(m.id)], card: moduleCard(m), graph: moduleGraph(m) };
      case 'operation':
        if (!(o = ops[r.a])) return null;
        return { crumbs: [SYS, crumb(o.owner, R.module(o.owner)), crumb(o.id)], card: operationCard(o), graph: operationGraph(o) };
      case 'schema':
        if (!(s = schemas[r.a])) return null;
        return { crumbs: [SYS, crumb(s.owner, R.module(s.owner)), crumb(s.name)], card: schemaCard(s), graph: schemaGraph(s) };
      case 'table':
        if (!(t = tables[r.a])) return null;
        return { crumbs: [SYS].concat(t.owners[0] ? [crumb(t.owners[0], R.module(t.owners[0]))] : [], [crumb(t.name)]), card: tableCard(t), graph: tableGraph(t) };
      case 'function':
        if (!(f = funcs[r.a])) return null;
        return { crumbs: [SYS, crumb(f.module, R.module(f.module)), crumb(funcLabel(f))], card: functionCard(f), graph: functionGraph(f) };
      case 'edge':
        if (!mods[r.a] || !mods[r.b]) return null;
        return { crumbs: [SYS, crumb(r.a, R.module(r.a)), crumb('imports ' + r.b)], card: edgeCard(r.a, r.b), graph: systemGraph(r.a + '>' + r.b) };
    }
    return null;
  }

  // ---- graph specs ------------------------------------------------------------
  // node: {id,label,color,cls,route,dashed}; edge: {id,source,target,cls,label,dep}
  var opts = { testNodes: true, testEdges: true };

  function modNode(m, cls) {
    return { id: 'm:' + m.id, label: m.id, color: 'var(--c-' + m.kind + ')', route: R.module(m.id),
      cls: [m.kind === 'test_only' ? 'testonly' : '', m.generated && !m.present ? 'dashed' : '', diffClass('modules', m.id), cls || ''].join(' ') };
  }
  function depEdge(d) {
    var bad = violByEdge[d.from + '>' + d.to];
    return { id: 'd:' + d.from + '>' + d.to, source: 'm:' + d.from, target: 'm:' + d.to, dep: d,
      cls: [d.test_only || (mods[d.from] && mods[d.from].kind === 'test_only') ? 'test' : '', bad ? 'violation' : '', diffClass('edges', d.from + '>' + d.to)].join(' ') };
  }

  function systemGraph(selEdge) {
    var nodes = [], edges = [];
    model.modules.forEach(function (m) {
      if (m.kind === 'test_only' && !opts.testNodes) return;
      nodes.push(modNode(m));
    });
    var have = {};
    nodes.forEach(function (n) { have[n.id] = 1; });
    model.dependencies.forEach(function (d) {
      var e = depEdge(d);
      if (!have[e.source] || !have[e.target]) return;
      if (/\btest\b/.test(e.cls) && !opts.testEdges) return;
      if (selEdge && d.from + '>' + d.to === selEdge) e.cls += ' selected';
      edges.push(e);
    });
    var legend = model.levels.concat(['test_only']).map(function (k) { return [kindLabel(k), 'var(--c-' + k + ')']; });
    return { nodes: nodes, edges: edges, layout: 'system', legend: legend, bar: true };
  }

  function moduleGraph(m) {
    var nodes = [modNode(m, 'center')], edges = [], seen = {};
    seen['m:' + m.id] = 1;
    function addNode(n) { if (!seen[n.id]) { seen[n.id] = 1; nodes.push(n); } }
    (depsTo[m.id] || []).forEach(function (d) { addNode(modNode(mods[d.from])); edges.push(depEdge(d)); });
    (depsFrom[m.id] || []).forEach(function (d) { addNode(modNode(mods[d.to])); edges.push(depEdge(d)); });
    m.subpackages.forEach(function (s) {
      var id = 'p:' + s.dir;
      addNode({ id: id, label: s.dir.split('/').slice(-2).join('/'), color: 'var(--c-subpackage)', cls: s.present ? '' : 'dashed', sub: s.present ? null : 'gen, not built' });
      edges.push({ id: 'c:' + id, source: 'm:' + m.id, target: id, cls: 'own' });
    });
    model.operations.filter(function (o) { return o.owner === m.id; }).forEach(function (o) {
      addNode({ id: 'o:' + o.id, label: o.method + ' ' + o.path, color: 'var(--c-operation)', route: R.operation(o.id), cls: diffClass('operations', o.id) });
      edges.push({ id: 'c:o:' + o.id, source: 'm:' + m.id, target: 'o:' + o.id, cls: 'own' });
    });
    model.queries.filter(function (q) { return q.module === m.id; }).forEach(function (q) {
      addNode({ id: 'q:' + q.id, label: q.id, color: 'var(--c-query)', cls: '' });
      edges.push({ id: 'c:q:' + q.id, source: 'm:' + m.id, target: 'q:' + q.id, cls: 'own' });
      q.tables.forEach(function (t) {
        addNode({ id: 't:' + t, label: t, color: 'var(--c-table)', route: R.table(t), cls: diffClass('tables', t) });
        edges.push({ id: 'qt:' + q.id + ':' + t, source: 'q:' + q.id, target: 't:' + t, cls: 'own' });
      });
    });
    model.tables.filter(function (t) { return t.owners.indexOf(m.id) >= 0; }).forEach(function (t) {
      addNode({ id: 't:' + t.name, label: t.name, color: 'var(--c-table)', route: R.table(t.name), cls: diffClass('tables', t.name) });
    });
    return { nodes: nodes, edges: edges, layout: 'dagre', rankDir: 'LR', tight: true, legend: [
      ['Module', 'var(--c-' + m.kind + ')'], ['Subpackage', 'var(--c-subpackage)'], ['Operation', 'var(--c-operation)'],
      ['Query', 'var(--c-query)'], ['Table', 'var(--c-table)']] };
  }

  function schemaGraphFrom(rootNodes, edgesIn, schemaIds) {
    var nodes = rootNodes, edges = edgesIn, seen = {};
    nodes.forEach(function (n) { seen[n.id] = 1; });
    var queue = schemaIds.slice(), count = 0;
    while (queue.length && count < 60) {
      var id = queue.shift(), s = schemas[id];
      if (!s) continue;
      if (!seen['s:' + id]) { seen['s:' + id] = 1; nodes.push(schemaNode(s)); count++; }
      s.refs.forEach(function (r) {
        if (!schemas[r]) return;
        var eid = 'r:' + id + '>' + r;
        if (seen[eid]) return;
        seen[eid] = 1;
        edges.push({ id: eid, source: 's:' + id, target: 's:' + r, cls: 'own', label: '$ref' });
        queue.push(r);
      });
    }
    return { nodes: nodes, edges: edges, layout: 'dagre', rankDir: 'LR', legend: [
      ['Operation', 'var(--c-operation)'], ['Schema', 'var(--c-schema)'], ['Module', 'var(--c-domain)']] };
  }
  function schemaNode(s, cls) { return { id: 's:' + s.id, label: s.name, color: 'var(--c-schema)', route: R.schema(s.id), cls: cls || '' }; }

  function operationGraph(o) {
    var nodes = [{ id: 'o:' + o.id, label: o.method + ' ' + o.path, color: 'var(--c-operation)', cls: 'center' }];
    var owner = mods[o.owner], edges = [], ids = [];
    if (owner) { nodes.push(modNode(owner)); edges.push({ id: 'own', source: 'm:' + o.owner, target: 'o:' + o.id, cls: 'own' }); }
    o.request_schemas.forEach(function (s) { ids.push(s); edges.push({ id: 'rq:' + s, source: 'o:' + o.id, target: 's:' + s, cls: 'own', label: 'request' }); });
    o.responses.forEach(function (r) {
      r.schemas.forEach(function (s) { ids.push(s); edges.push({ id: 'rs:' + r.status + s, source: 'o:' + o.id, target: 's:' + s, cls: 'own', label: r.status }); });
    });
    edges = edges.filter(function (e) { return e.id === 'own' || schemas[e.target.slice(2)]; });
    return schemaGraphFrom(nodes, edges, ids);
  }

  function schemaGraph(s) {
    var nodes = [schemaNode(s, 'center')], edges = [];
    model.operations.forEach(function (o) {
      var uses = o.request_schemas.indexOf(s.id) >= 0 || o.responses.some(function (r) { return r.schemas.indexOf(s.id) >= 0; });
      if (!uses) return;
      nodes.push({ id: 'o:' + o.id, label: o.method + ' ' + o.path, color: 'var(--c-operation)', route: R.operation(o.id) });
      edges.push({ id: 'u:' + o.id, source: 'o:' + o.id, target: 's:' + s.id, cls: 'own' });
    });
    model.schemas.forEach(function (x) {
      if (x.refs.indexOf(s.id) < 0) return;
      nodes.push(schemaNode(x));
      edges.push({ id: 'ub:' + x.id, source: 's:' + x.id, target: 's:' + s.id, cls: 'own', label: '$ref' });
    });
    return schemaGraphFrom(nodes, edges, [s.id]);
  }

  function tableGraph(t) {
    var nodes = [{ id: 't:' + t.name, label: t.name, color: 'var(--c-table)', cls: 'center' }], edges = [], seen = { ['t:' + t.name]: 1 };
    function addNode(n) { if (!seen[n.id]) { seen[n.id] = 1; nodes.push(n); } }
    t.owners.forEach(function (o) {
      if (!mods[o]) return;
      addNode(modNode(mods[o])); edges.push({ id: 'ow:' + o, source: 'm:' + o, target: 't:' + t.name, cls: 'own' });
    });
    t.columns.forEach(function (c) {
      if (c.references && tables[c.references]) {
        addNode({ id: 't:' + c.references, label: c.references, color: 'var(--c-table)', route: R.table(c.references) });
        edges.push({ id: 'fk:' + c.name, source: 't:' + t.name, target: 't:' + c.references, cls: 'own', label: c.name });
      }
    });
    model.tables.forEach(function (x) {
      x.columns.forEach(function (c) {
        if (c.references === t.name) {
          addNode({ id: 't:' + x.name, label: x.name, color: 'var(--c-table)', route: R.table(x.name) });
          edges.push({ id: 'fkb:' + x.name + c.name, source: 't:' + x.name, target: 't:' + t.name, cls: 'own', label: c.name });
        }
      });
    });
    model.queries.forEach(function (q) {
      if (q.tables.indexOf(t.name) < 0) return;
      addNode({ id: 'q:' + q.id, label: q.id, color: 'var(--c-query)' });
      edges.push({ id: 'qq:' + q.id, source: 'q:' + q.id, target: 't:' + t.name, cls: 'own' });
    });
    return { nodes: nodes, edges: edges, layout: 'dagre', rankDir: 'LR', legend: [
      ['Table', 'var(--c-table)'], ['Query', 'var(--c-query)'], ['Module', 'var(--c-domain)']] };
  }

  function functionGraph(f) {
    var nodes = [{ id: 'f:' + f.id, label: funcLabel(f), color: 'var(--c-function)', cls: 'center' }], edges = [];
    if (mods[f.module]) { nodes.push(modNode(mods[f.module])); edges.push({ id: 'in', source: 'm:' + f.module, target: 'f:' + f.id, cls: 'own' }); }
    model.operations.forEach(function (o) {
      if (o.implementation && o.implementation.function === f.id) {
        nodes.push({ id: 'o:' + o.id, label: o.method + ' ' + o.path, color: 'var(--c-operation)', route: R.operation(o.id) });
        edges.push({ id: 'im:' + o.id, source: 'o:' + o.id, target: 'f:' + f.id, cls: 'own', label: 'implemented by' });
      }
    });
    return { nodes: nodes, edges: edges, layout: 'dagre', rankDir: 'TB', legend: [['Function', 'var(--c-function)'], ['Module', 'var(--c-domain)']] };
  }

  // ---- graph rendering ----------------------------------------------------------
  var cy = null, graphDirty = true, currentGraph = null;
  var narrow = window.matchMedia('(max-width: 699px)');
  var viewTab = 'cards';
  try { viewTab = sessionStorage.getItem('archmodel-tab') || 'cards'; } catch (e) { /* storage unavailable */ }

  function cssVar(name) { return getComputedStyle(document.documentElement).getPropertyValue(name).trim(); }
  function resolveColor(c) {
    var m = /^var\((--[\w-]+)\)$/.exec(c || '');
    return m ? cssVar(m[1]) : c;
  }
  function graphVisible() { return !narrow.matches || viewTab === 'graph'; }

  // Node size from the label (Cytoscape's 'label' sizing keyword is deprecated).
  function nodeWidth(n) {
    var longest = String(n.data('label')).split('\n').reduce(function (a, l) { return Math.max(a, l.length); }, 0);
    return Math.max(40, longest * 8.4 + 28);
  }
  function nodeHeight(n) { return 20 + 18 * String(n.data('label')).split('\n').length; }

  function initCy() {
    if (cy) return;
    if (typeof cytoscape === 'undefined') { $('cy').textContent = 'The graph library failed to load.'; return; }
    try { if (!cytoscape('layout', 'dagre') && window.cytoscapeDagre) window.cytoscapeDagre(cytoscape); } catch (e) { /* already registered */ }
    var violation = cssVar('--violation'), edgeColor = cssVar('--edge'), added = cssVar('--added'), removed = cssVar('--removed');
    cy = cytoscape({
      container: $('cy'), minZoom: 0.15, maxZoom: 2.5,
      style: [
        { selector: 'node', style: { 'label': 'data(label)', 'background-color': 'data(color)', 'color': '#fff', 'text-valign': 'center',
          'text-halign': 'center', 'shape': 'round-rectangle', 'width': nodeWidth, 'height': nodeHeight, 'padding': '0px', 'font-size': 14,
          'font-weight': 600, 'border-width': 0 } },
        { selector: 'node.center', style: { 'border-width': 4, 'border-color': '#1c2330' } },
        { selector: 'node.dashed', style: { 'border-width': 2, 'border-style': 'dashed', 'border-color': '#1c2330', 'opacity': 0.8 } },
        { selector: 'node.testonly', style: { 'opacity': 0.45 } },
        { selector: 'node.diff-added', style: { 'border-width': 4, 'border-color': added } },
        { selector: 'node.diff-removed', style: { 'border-width': 4, 'border-color': removed, 'border-style': 'dashed', 'opacity': 0.7 } },
        { selector: 'edge', style: { 'curve-style': 'bezier', 'width': 2, 'line-color': edgeColor, 'target-arrow-color': edgeColor,
          'target-arrow-shape': 'triangle', 'arrow-scale': 1.3, 'label': 'data(label)', 'font-size': 11, 'color': '#4b5563',
          'text-background-color': '#fff', 'text-background-opacity': 0.85, 'text-background-padding': 2 } },
        { selector: 'edge.test', style: { 'line-style': 'dashed', 'opacity': 0.6 } },
        { selector: 'edge.violation', style: { 'line-color': violation, 'target-arrow-color': violation, 'width': 5, 'opacity': 1, 'z-index': 10 } },
        { selector: 'edge.diff-added', style: { 'line-color': added, 'target-arrow-color': added, 'width': 4 } },
        { selector: 'edge.diff-removed', style: { 'line-color': removed, 'target-arrow-color': removed, 'line-style': 'dashed' } },
        { selector: 'edge.selected', style: { 'width': 6, 'overlay-color': '#1858b8', 'overlay-opacity': 0.25, 'overlay-padding': 6 } }
      ]
    });
    cy.on('tap', 'node', function (e) {
      var route = e.target.data('route');
      hideSheet();
      if (route) go(route);
    });
    cy.on('tap', 'edge', function (e) { if (e.target.data('dep')) showSheet(e.target.data('dep')); });
    cy.on('tap', function (e) { if (e.target === cy) hideSheet(); });
    cy.on('mouseover', 'edge', function (e) { if (e.target.data('dep')) showTip(e); });
    cy.on('mouseout', 'edge', hideTip);
    cy.on('pan zoom', hideTip);
  }

  function fitGraph() {
    cy.fit(undefined, 24);
    if (cy.zoom() > 1.2) { cy.zoom(1.2); cy.center(); }
  }

  function drawGraph() {
    if (!currentGraph) return;
    if (!graphVisible()) { graphDirty = true; return; }
    initCy();
    if (!cy) return;
    graphDirty = false;
    var g = currentGraph;
    cy.resize();
    cy.batch(function () {
      cy.elements().remove();
      cy.add(g.nodes.map(function (n) {
        return { group: 'nodes', data: { id: n.id, label: n.label + (n.sub ? '\n(' + n.sub + ')' : ''), color: resolveColor(n.color), route: n.route }, classes: (n.cls || '').trim() };
      }));
      cy.add(g.edges.map(function (e) {
        return { group: 'edges', data: { id: e.id, source: e.source, target: e.target, label: e.label || '', dep: e.dep }, classes: (e.cls || '').trim() };
      }));
    });
    var dagre = { name: 'dagre', rankDir: g.rankDir || 'TB', nodeSep: g.tight ? 10 : 28, rankSep: g.tight ? 120 : 70, edgeSep: 12, fit: false, animate: false };
    if (g.layout === 'system') {
      var main = cy.elements().not('.testonly').not('edge.test');
      main.layout(dagre).run();
      var tos = cy.nodes('.testonly');
      if (tos.length) {
        var bb = cy.nodes().not('.testonly').boundingBox();
        var x = bb.x1;
        tos.forEach(function (n) { n.position({ x: x + n.width() / 2, y: bb.y2 + 130 }); x += n.width() + 30; });
      }
    } else {
      cy.layout(dagre).run();
    }
    fitGraph();
    renderBar(g);
    var lg = $('legend');
    lg.textContent = '';
    (g.legend || []).forEach(function (l) { lg.appendChild(chip(l[0], 'kind', 'background:' + l[1])); });
  }

  function renderBar(g) {
    var bar = $('graphbar');
    bar.textContent = '';
    bar.hidden = !g.bar;
    if (!g.bar) return;
    function toggle(label, key) {
      var cb = h('input', { type: 'checkbox', id: 'opt-' + key });
      cb.checked = opts[key];
      cb.addEventListener('change', function () { opts[key] = cb.checked; render(true); });
      bar.appendChild(h('label', null, [cb, label]));
    }
    toggle('Test-only modules', 'testNodes');
    toggle('Test-only edges (dashed)', 'testEdges');
  }

  // Edge info: hover tooltip (mouse) and tap sheet (touch and mouse).
  function edgeSummary(d) {
    var vs = violByEdge[d.from + '>' + d.to] || [];
    var kids = [h('div', null, h('b', null, d.from + ' -> ' + d.to))];
    if (vs.length) vs.forEach(function (v) {
      kids.push(h('div', null, [h('b', null, v.rule + ': '), v.rationale]), h('div', null, [h('b', null, 'Fix: '), v.fix]));
    }); else kids.push(h('div', null, d.test_only ? 'Test-only edge: only _test.go files import it.' : 'Production edge. No rule violated.'));
    return kids;
  }
  function showTip(e) {
    var tip = $('tip'), p = e.renderedPosition || e.target.renderedMidpoint();
    tip.textContent = '';
    add(tip, edgeSummary(e.target.data('dep')));
    tip.hidden = false;
    var w = $('graphpane').clientWidth;
    tip.style.left = Math.max(4, Math.min(p.x + 12, w - 350)) + 'px';
    tip.style.top = ($('cy').offsetTop + p.y + 14) + 'px';
  }
  function hideTip() { $('tip').hidden = true; }
  function showSheet(d) {
    hideTip();
    var s = $('sheet');
    s.textContent = '';
    s.className = violByEdge[d.from + '>' + d.to] ? 'bad' : '';
    s.appendChild(h('button', { class: 'close', 'aria-label': 'Close', onclick: hideSheet }, '×'));
    add(s, edgeDetail(d.from, d.to));
    s.appendChild(h('p', null, link(R.edge(d.from, d.to), 'Open edge page')));
    s.hidden = false;
  }
  function hideSheet() { $('sheet').hidden = true; }

  // ---- shell: routing, crumbs, tabs, search, banner ----------------------------
  function go(route) { location.hash = '#' + route; }

  var lastKey = null;
  function render(force) {
    var r = parseRoute();
    var v = view(r);
    var panel = $('panel');
    var key = location.hash;
    hideSheet(); hideTip();
    if (!v) {
      v = { crumbs: [SYS, crumb('Not found')], card: [h('h2', null, 'Not found'),
        h('p', null, 'Nothing in the model matches ' + JSON.stringify(location.hash) + '. It may have been renamed or removed.'),
        link(R.system(), 'Back to the system')], graph: systemGraph() };
    }
    var keep = key === lastKey ? panel.scrollTop : 0;
    panel.textContent = '';
    add(panel, v.card);
    panel.scrollTop = force ? keep : 0;
    lastKey = key;
    var cr = $('crumbs');
    cr.textContent = '';
    v.crumbs.forEach(function (c, i) {
      if (i) cr.appendChild(h('span', { class: 'sep' }, '›'));
      cr.appendChild(c.route && i < v.crumbs.length - 1 ? link(c.route, c.label) : h('span', { class: 'cur' }, c.label));
    });
    document.title = v.crumbs[v.crumbs.length - 1].label + ' - Architecture Explorer';
    currentGraph = v.graph;
    drawGraph();
  }

  function setTab(t) {
    viewTab = t;
    try { sessionStorage.setItem('archmodel-tab', t); } catch (e) { /* storage unavailable */ }
    document.body.classList.toggle('view-graph', t === 'graph');
    $('tab-cards').setAttribute('aria-selected', String(t === 'cards'));
    $('tab-graph').setAttribute('aria-selected', String(t === 'graph'));
    if (t === 'graph' && (graphDirty || cy)) drawGraph();
  }
  $('tab-cards').addEventListener('click', function () { setTab('cards'); });
  $('tab-graph').addEventListener('click', function () { setTab('graph'); });
  narrow.addEventListener('change', function () { setTab(viewTab); drawGraph(); });
  window.addEventListener('resize', function () { if (cy && graphVisible()) { cy.resize(); fitGraph(); } });
  window.addEventListener('hashchange', function () { render(false); });

  // Search
  var searchIdx = [];
  model.modules.forEach(function (m) { searchIdx.push({ k: 'Module', l: m.id, s: m.summary, r: R.module(m.id), t: m.id }); });
  model.operations.forEach(function (o) { searchIdx.push({ k: 'Operation', l: o.id, s: o.method + ' ' + o.path, r: R.operation(o.id), t: o.id + ' ' + o.path }); });
  model.tables.forEach(function (t) { searchIdx.push({ k: 'Table', l: t.name, s: '', r: R.table(t.name), t: t.name }); });
  model.schemas.forEach(function (s) { searchIdx.push({ k: 'Schema', l: s.id, s: '', r: R.schema(s.id), t: s.id }); });
  model.functions.forEach(function (f) { searchIdx.push({ k: 'Function', l: f.id, s: f.module, r: R.fn(f.id), t: f.id }); });
  var sel = 0;
  function runSearch() {
    var q = $('search').value.trim().toLowerCase(), box = $('results');
    box.textContent = '';
    if (!q) { box.hidden = true; return; }
    var hits = searchIdx.filter(function (e) { return e.t.toLowerCase().indexOf(q) >= 0; });
    hits.sort(function (a, b) {
      var sa = a.l.toLowerCase().indexOf(q), sb = b.l.toLowerCase().indexOf(q);
      return (sa < 0 ? 99 : sa) - (sb < 0 ? 99 : sb) || a.l.length - b.l.length;
    });
    hits.slice(0, 12).forEach(function (e, i) {
      box.appendChild(h('a', { href: '#' + e.r, class: i === 0 ? 'sel' : '', onclick: clearSearch }, [chip(e.k), h('b', null, e.l), h('span', { class: 'sub' }, e.s)]));
    });
    if (!hits.length) box.appendChild(h('div', { class: 'none sub' }, 'No match.'));
    sel = 0;
    box.hidden = false;
  }
  function clearSearch() { $('search').value = ''; $('results').hidden = true; $('search').blur(); }
  $('search').addEventListener('input', runSearch);
  $('search').addEventListener('keydown', function (e) {
    var items = $('results').querySelectorAll('a');
    if (e.key === 'Escape') clearSearch();
    else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      if (!items.length) return;
      e.preventDefault();
      items[sel].classList.remove('sel');
      sel = (sel + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length;
      items[sel].classList.add('sel');
    } else if (e.key === 'Enter' && items.length) { var a = items[sel]; location.hash = a.getAttribute('href'); clearSearch(); }
  });
  document.addEventListener('click', function (e) { if (!$('searchwrap').contains(e.target)) $('results').hidden = true; });
  document.addEventListener('keydown', function (e) {
    if (e.key === '/' && document.activeElement !== $('search') && !/input|textarea/i.test(document.activeElement.tagName)) { e.preventDefault(); $('search').focus(); }
  });

  // Live updates from `archmodel serve`.
  function showBanner(text) {
    var b = $('banner');
    b.hidden = !text;
    b.textContent = text || '';
  }
  if (cfg.live && window.EventSource && /^https?:$/.test(location.protocol)) {
    var es = new EventSource('/events');
    es.addEventListener('reload', function () { location.reload(); });
    es.addEventListener('builderror', function (e) {
      var msg = e.data;
      try { msg = JSON.parse(e.data).message; } catch (x) { /* plain text */ }
      showBanner('Rebuild failed; showing the last good model.\n' + msg);
    });
  }

  setTab(viewTab);
  render(false);
})();
