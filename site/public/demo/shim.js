// shim.js: answers the Studio's API calls from static files, so the real
// Studio page runs as a read only demo with no server.
//
// The files come from `studio-snapshot` (generator/cmd/studio-snapshot), one
// folder per project under data/. Fixed pages are plain files. The call graph
// and search are computed here from graph.json with the same rules as the Go
// handlers in internal/presentation/v23_endpoints.go.
(function () {
  'use strict';

  var PROJECTS = { hold: 1, shop: 1, payroll: 1 };
  var params = new URLSearchParams(location.search);
  var project = PROJECTS[params.get('p')] ? params.get('p') : 'hold';
  var BASE = 'data/' + project + '/';
  var realFetch = window.fetch.bind(window);
  var cache = {};

  function file(name) {
    if (!cache[name]) {
      cache[name] = realFetch(BASE + name).then(function (r) {
        if (!r.ok) throw new Error('missing ' + name);
        return r.json();
      });
    }
    return cache[name];
  }

  var graphPromise = null;
  function graph() {
    if (!graphPromise) {
      graphPromise = file('graph.json').then(function (g) {
        var byId = {}, byName = {};
        g.symbols.forEach(function (s) {
          byId[s.id] = s;
          (byName[s.n] = byName[s.n] || []).push(s);
        });
        return { name: g.name, symbols: g.symbols, byId: byId, byName: byName };
      });
    }
    return graphPromise;
  }

  function lookup(g, idOrName) {
    return g.byId[idOrName] || (g.byName[idOrName] || [])[0];
  }

  function reply(status, body) {
    return new Response(JSON.stringify(body), {
      status: status,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  function text(status, msg) {
    return new Response(msg, { status: status, headers: { 'Content-Type': 'text/plain' } });
  }

  // Same rules as handleCallgraphV23: BFS to depth (1 to 4, default 2), at
  // most 200 nodes, stdlib neighbours hidden unless include_stdlib, a caller
  // always gives the edge caller -> current.
  function callgraph(g, q) {
    var root = lookup(g, q.get('root') || '');
    if (!root) return text(404, 'symbol not found');
    var depth = parseInt(q.get('depth'), 10);
    if (!(depth > 0 && depth <= 4)) depth = 2;
    var direction = q.get('direction') || 'callees';
    var includeStdlib = q.get('include_stdlib') === '1' || q.get('include_stdlib') === 'true';
    var resp = { root: q.get('root'), direction: direction, depth: depth, nodes: [], edges: [] };
    var visited = {};
    function addNode(s, d) {
      if (visited[s.id] !== undefined) return false;
      visited[s.id] = d;
      resp.nodes.push({
        symbol_id: s.id, name: s.init ? 'new ' + s.recv : s.q, kind: s.kind,
        language: s.lang, file: s.file, line: s.line, depth: d,
        provenance: s.prov || undefined, is_init: s.init || undefined, effects: s.efx,
      });
      return true;
    }
    addNode(root, 0);
    var frontier = [root];
    for (var d = 1; d <= depth; d++) {
      var next = [];
      for (var i = 0; i < frontier.length; i++) {
        var cur = frontier[i];
        if (resp.nodes.length >= 200) { resp.truncated = true; break; }
        var neighbours = [];
        if (direction === 'callees' || direction === 'both') {
          (cur.out || []).forEach(function (id) { neighbours.push({ s: g.byId[id], caller: false }); });
          (cur.unres || []).forEach(function (name) {
            resp.edges.push({ from: cur.id, to_name: name, unresolved: true });
          });
        }
        if (direction === 'callers' || direction === 'both') {
          (cur.in || []).forEach(function (id) { neighbours.push({ s: g.byId[id], caller: true }); });
        }
        neighbours.forEach(function (n) {
          var nb = n.s;
          if (!nb) return;
          if (!includeStdlib && nb.prov) return;
          if (n.caller) resp.edges.push({ from: nb.id, to: cur.id, to_name: cur.n, unresolved: false });
          else resp.edges.push({ from: cur.id, to: nb.id, to_name: nb.n, unresolved: false });
          if (addNode(nb, d)) next.push(nb);
        });
      }
      if (resp.truncated) break;
      frontier = next;
      if (!frontier.length) break;
    }
    return reply(200, resp);
  }

  function subsequence(needle, hay) {
    var i = 0;
    for (var j = 0; j < hay.length && i < needle.length; j++) if (needle[i] === hay[j]) i++;
    return i === needle.length;
  }

  // Same ranking as handleSearchV23: exact 0, prefix 1, substring 2,
  // subsequence 3, empty query 4; then by name.
  function search(g, q) {
    var query = (q.get('q') || '').trim();
    var effect = (q.get('effect') || '').trim().toLowerCase();
    var includeStdlib = q.get('include_stdlib') === '1' || q.get('include_stdlib') === 'true';
    var limit = parseInt(q.get('limit'), 10);
    if (!(limit > 0 && limit <= 500)) limit = 100;
    var ql = query.toLowerCase();
    var hits = [];
    g.symbols.forEach(function (s) {
      if (s.kind === 'module') return;
      if (!includeStdlib && s.prov) return;
      var n = s.n.toLowerCase(), qu = s.q.toLowerCase(), rank = -1;
      if (query === '') rank = 4;
      else if (n === ql || qu === ql) rank = 0;
      else if (n.indexOf(ql) === 0 || qu.indexOf(ql) === 0) rank = 1;
      else if (n.indexOf(ql) >= 0 || qu.indexOf(ql) >= 0) rank = 2;
      else if (subsequence(ql, n) || subsequence(ql, qu)) rank = 3;
      if (rank < 0) return;
      if (effect && (s.efx || []).indexOf(effect) < 0) return;
      hits.push({
        symbol_id: s.id, name: s.q, kind: s.kind, language: s.lang, file: s.file, line: s.line,
        exported: !!s.exp, provenance: s.prov || undefined, effects: s.efx, match_rank: rank,
      });
    });
    hits.sort(function (a, b) {
      if (a.match_rank !== b.match_rank) return a.match_rank - b.match_rank;
      return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
    });
    return reply(200, { query: query, total: hits.length, hits: hits.slice(0, limit) });
  }

  function symbolFile(g, q, param, part) {
    var s = lookup(g, q.get(param) || '');
    if (!s || !s.k) return Promise.resolve(text(404, 'not found'));
    return file('sym/' + s.k + '.json').then(function (body) {
      return body[part] ? reply(200, body[part]) : text(404, 'not found');
    });
  }

  var READ_ONLY = 'This is a read only demo. Install gen-system to verify or correct beliefs.';

  window.fetch = function (input, init) {
    var url = new URL(typeof input === 'string' ? input : input.url, location.href);
    if (url.pathname.indexOf('/api/') !== 0) return realFetch(input, init);
    var method = ((init && init.method) || 'GET').toUpperCase();
    var q = url.searchParams;
    return graph().then(function (g) {
      if (method !== 'GET') return reply(403, { error: READ_ONLY });
      switch (url.pathname) {
        case '/api/present/v23/architecture': return file('architecture.json').then(function (b) { return reply(200, b); });
        case '/api/present/beliefs': return file('beliefs.json').then(function (b) { return reply(200, b); });
        case '/api/present/v23/effects':
          return file(q.get('include_stdlib') === '1' ? 'effects-stdlib.json' : 'effects.json')
            .then(function (b) { return reply(200, b); });
        case '/api/present/flow': return symbolFile(g, q, 'func', 'flow');
        case '/api/present/source': return symbolFile(g, q, 'sym', 'source');
        case '/api/present/v23/callgraph': return callgraph(g, q);
        case '/api/present/v23/search': return search(g, q);
        case '/api/project/current': return reply(200, { root: g.name, live: false, loaded: true });
        case '/api/project/base': return reply(200, { base: g.name });
        case '/api/project/list': return reply(200, { enabled: false, dirs: [] });
      }
      return text(404, 'not in the demo');
    }).catch(function (e) { return text(500, String(e)); });
  };

  // The Studio listens for file change events; the demo has none.
  window.EventSource = function () {
    this.readyState = 2;
    this.close = function () {};
    this.addEventListener = function () {};
  };
})();
