// HFS Desktop Admin Panel
// Uses fetch() for API calls, Wails bridge for native features.
(function() {
'use strict';

// ---- Detect Wails (injected by Wails runtime) ----
var W = null;
function wailsReady() {
  try {
    if (window.go && window.go.main && window.go.main.App) {
      W = window.go.main.App;
      return true;
    }
  } catch(e) {}
  return false;
}

// ---- API helper (fetch to local HTTP server) ----
var API_BASE = 'http://localhost:8080';
async function api(path, opts) {
  opts = opts || {};
  var url = API_BASE + '/api' + path;
  var res = await fetch(url, {
    headers: Object.assign({ 'Content-Type': 'application/json' }, opts.headers || {}),
    method: opts.method || 'GET',
    body: opts.body,
  });
  if (!res.ok) throw new Error('HTTP ' + res.status);
  return res.json();
}

// ---- Log ----
var logLines = [];
function log(msg) {
  var time = new Date().toLocaleTimeString();
  var full = time + '  ' + msg;
  logLines.push(full);
  if (logLines.length > 500) logLines.shift();
  var el = document.getElementById('log-box');
  if (!el) return;
  var div = document.createElement('div');
  div.className = 'log-line';
  div.textContent = full;
  el.appendChild(div);
  if (el.children.length > 500) el.firstChild.remove();
  el.scrollTop = el.scrollHeight;
}

// ---- State ----
var selectedNode = null;
var bwHistory = [];
var serverPort = 8080;
var lastBytesIn = 0, lastBytesOut = 0;

// ---- Formatting ----
function fmtBytes(n) {
  if (!n || n === 0) return '0 B';
  var k = 1024, sizes = ['B','KB','MB','GB','TB'];
  var i = Math.floor(Math.log(n) / Math.log(k));
  return (n / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i];
}
function esc(s) { return s ? String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;') : ''; }

// ---- VFS Tree ----
function buildTree(node, depth, parentEl) {
  var div = document.createElement('div');
  div.className = 'tree-node' + (depth === 0 ? ' root' : '');
  div.style.paddingLeft = (depth * 14 + 4) + 'px';
  div.dataset.vpath = node.vpath || '/';

  var isFolder = node.type === 1 || node.is_folder;
  var hasKids = node.children && node.children.length > 0;

  var toggle = document.createElement('span');
  toggle.className = 'toggle';
  toggle.textContent = hasKids ? '▶' : ' ';
  div.appendChild(toggle);

  var icon = document.createElement('span');
  icon.className = 'icon';
  icon.textContent = isFolder ? '📁' : '📄';
  div.appendChild(icon);

  var name = document.createElement('span');
  name.className = 'name';
  name.textContent = (node.name || '/') + (isFolder ? '/' : '');
  div.appendChild(name);

  div.addEventListener('click', function(e) {
    e.stopPropagation();
    document.querySelectorAll('.tree-node').forEach(function(n) { n.classList.remove('selected'); });
    div.classList.add('selected');
    selectedNode = node;
  });

  div.addEventListener('dblclick', function(e) {
    e.stopPropagation();
    if (selectedNode && selectedNode.vpath === node.vpath) showProps(node);
  });

  if (hasKids) {
    toggle.addEventListener('click', function(e) {
      e.stopPropagation();
      var cd = div.nextElementSibling;
      if (cd && cd.classList.contains('tree-children')) {
        cd.classList.toggle('open');
        toggle.textContent = cd.classList.contains('open') ? '▼' : '▶';
      }
    });
  }

  parentEl.appendChild(div);

  if (hasKids) {
    var kids = document.createElement('div');
    kids.className = 'tree-children' + (depth === 0 ? ' open' : '');
    if (depth === 0) toggle.textContent = '▼';
    // sort folders first
    var sorted = node.children.slice().sort(function(a, b) {
      var aF = a.type === 1 || a.is_folder, bF = b.type === 1 || b.is_folder;
      if (aF && !bF) return -1;
      if (!aF && bF) return 1;
      return (a.name || '').localeCompare(b.name || '');
    });
    sorted.forEach(function(c) { buildTree(c, depth + 1, kids); });
    parentEl.appendChild(kids);
  }
}

async function refreshTree() {
  try {
    var data = await api('/vfs/tree');
    var el = document.getElementById('vfs-tree');
    el.innerHTML = '';
    if (data.root) {
      buildTree(data.root, 0, el);
      updateParentSelect(data.root);
    }
  } catch(e) { log('Tree error: ' + e.message); }
}

function updateParentSelect(root) {
  var sel = document.getElementById('modal-parent');
  if (!sel) return;
  sel.innerHTML = '';
  (function walk(n, d) {
    if ((n.type === 1 || n.is_folder) && n.vpath) {
      var o = document.createElement('option');
      o.value = n.vpath;
      o.textContent = Array(d*2+1).join(' ') + (n.vpath === '/' ? '/ (root)' : n.vpath);
      sel.appendChild(o);
    }
    if (n.children) n.children.forEach(function(c) { walk(c, d+1); });
  })(root, 0);
}

// ---- Properties Modal ----
function showProps(node) {
  var ov = document.getElementById('props-overlay');
  ov.classList.remove('hidden');

  var isF = node.type === 1 || node.is_folder;
  document.getElementById('props-info').innerHTML =
    '<div class="r"><span class="l">Name:</span> ' + esc(node.name) + '</div>' +
    '<div class="r"><span class="l">Type:</span> ' + (isF ? 'Folder' : 'File') + '</div>' +
    '<div class="r"><span class="l">Path:</span> ' + esc(node.vpath || '/') + '</div>' +
    (node.rpath ? '<div class="r"><span class="l">Real Path:</span> ' + esc(node.rpath) + '</div>' : '') +
    '<div class="r"><span class="l">Downloads:</span> ' + (node.dl_count || 0) + '</div>';

  document.getElementById('props-comment').value = node.comment || '';
  document.getElementById('props-filter').value = node.upload_filter || '';

  var f = node.flags || 0;
  document.getElementById('prop-browsable').checked = !!(f & 1);
  document.getElementById('prop-archivable').checked = !!(f & 2);
  document.getElementById('prop-deletable').checked = !!(f & 4);
  document.getElementById('prop-uploadable').checked = !!(f & 8);
  document.getElementById('prop-dontlog').checked = !!(f & 16);
  document.getElementById('prop-hidden').checked = !!(f & 32);
}

document.getElementById('props-save').addEventListener('click', async function() {
  if (!selectedNode) return;
  var vp = selectedNode.vpath || '/';
  if (vp === '/') { log('Cannot edit root'); return; }
  var flags = 0;
  try {
    if (document.getElementById('prop-browsable').checked)   flags |= 1;
    if (document.getElementById('prop-archivable').checked)  flags |= 2;
    if (document.getElementById('prop-deletable').checked)   flags |= 4;
    if (document.getElementById('prop-uploadable').checked)  flags |= 8;
    if (document.getElementById('prop-dontlog').checked)     flags |= 16;
    if (document.getElementById('prop-hidden').checked)      flags |= 32;
  } catch(e) {}

  try {
    await api('/vfs/nodes' + vp, {
      method: 'PATCH',
      body: JSON.stringify({
        comment: document.getElementById('props-comment').value,
        upload_filter: document.getElementById('props-filter').value,
        flags: flags
      })
    });
    document.getElementById('props-overlay').classList.add('hidden');
    refreshTree();
    log('Updated: ' + vp);
  } catch(e) { log('Error: ' + e.message); }
});

document.getElementById('props-remove').addEventListener('click', async function() {
  if (!selectedNode) return;
  var vp = selectedNode.vpath || '/';
  if (vp === '/') { log('Cannot remove root'); return; }
  if (!confirm('Remove "' + selectedNode.name + '" from VFS?\n(Files on disk are NOT deleted)')) return;
  try {
    await api('/vfs/nodes' + vp, { method: 'DELETE' });
    document.getElementById('props-overlay').classList.add('hidden');
    selectedNode = null;
    refreshTree();
    log('Removed from VFS: ' + vp);
  } catch(e) { log('Error: ' + e.message); }
});

document.getElementById('props-cancel').addEventListener('click', function() {
  document.getElementById('props-overlay').classList.add('hidden');
});

// ---- Add Folder Modal ----
document.getElementById('modal-cancel').addEventListener('click', function() {
  document.getElementById('modal-overlay').classList.add('hidden');
});

document.getElementById('modal-submit').addEventListener('click', async function() {
  var path = document.getElementById('modal-path').value.trim();
  var name = document.getElementById('modal-name').value.trim();
  var parent = document.getElementById('modal-parent').value || '/';

  if (!path && !name) return;
  try {
    var body = { name: name || path.split('/').pop(), parent: parent };
    if (path) body.real_path = path;
    await api('/vfs/folders', { method: 'POST', body: JSON.stringify(body) });
    document.getElementById('modal-overlay').classList.add('hidden');
    document.getElementById('modal-path').value = '';
    document.getElementById('modal-name').value = '';
    refreshTree();
    log('Added folder');
  } catch(e) { log('Error: ' + e.message); }
});

document.getElementById('modal-browse').addEventListener('click', async function() {
  if (W && W.PickFolder) {
    try {
      var path = await W.PickFolder();
      if (path) document.getElementById('modal-path').value = path;
    } catch(e) { log('Dialog error: ' + e); }
  } else {
    alert('Native folder picker not available.\nType the path manually or use headless mode in a browser.');
  }
});

// ---- Toolbar ----
document.getElementById('btn-browse').addEventListener('click', function() {
  window.open('http://localhost:' + serverPort, '_blank');
  log('Opening in browser: http://localhost:' + serverPort);
});

document.getElementById('btn-port').addEventListener('click', function() {
  var p = prompt('Enter port:', String(serverPort));
  if (p && parseInt(p) > 0) {
    serverPort = parseInt(p);
    document.getElementById('port-label').textContent = serverPort;
    document.getElementById('url-box').value = 'http://localhost:' + serverPort;
    API_BASE = 'http://localhost:' + serverPort;
    updateConfigPort(serverPort);
    log('Port changed to ' + serverPort + '. Restart required.');
  }
});

document.getElementById('btn-add-folder').addEventListener('click', function() {
  document.getElementById('modal-overlay').classList.remove('hidden');
  document.getElementById('modal-path').focus();
});

async function updateConfigPort(port) {
  try {
    await api('/config', { method: 'PUT', body: JSON.stringify({ port: port }) });
  } catch(e) {}
}

// ---- Start/Stop ----
var running = true;
document.getElementById('btn-start').addEventListener('click', function() {
  running = !running;
  var btn = document.getElementById('btn-start');
  if (running) {
    btn.innerHTML = '■ Stop';
    btn.classList.add('running');
    btn.classList.remove('stopped');
    log('Server started');
  } else {
    btn.innerHTML = '▶ Start';
    btn.classList.add('stopped');
    btn.classList.remove('running');
    log('Server stopped');
  }
});

// ---- Connections Table ----
async function refreshConnections() {
  try {
    var data = await api('/server/connections');
    var body = document.getElementById('conn-body');
    if (!data || data.length === 0) {
      body.innerHTML = '<tr><td colspan="6" class="empty">No active connections</td></tr>';
      return;
    }
    body.innerHTML = data.map(function(c) {
      var elapsed = 0;
      var speed = 0;
      if (c.connected_at) {
        elapsed = (Date.now() - new Date(c.connected_at).getTime()) / 1000;
        if (elapsed > 0) speed = Math.round((c.bytes_sent || 0) / elapsed);
      }
      return '<tr>' +
        '<td>' + esc(c.address) +
        '<td>' + esc(c.request_url || '') +
        '<td>' + (speed > 1024 ? 'transferring' : 'idle') +
        '<td>' + fmtBytes(speed) + '/s' +
        '<td>' + (c.time_left || '') +
        '<td><div class="conn-progress"><div class="fill" style="width:0%"></div></div>' +
      '</tr>';
    }).join('');
  } catch(e) { /* silent */ }
}

// ---- Status Bar ----
async function refreshStatus() {
  try {
    var stats = await api('/server/stats');
    document.getElementById('sb-total-in').textContent = '⬆ ' + fmtBytes(stats.bytes_recv) + ' total in';
    document.getElementById('sb-total-out').textContent = '⬇ ' + fmtBytes(stats.bytes_sent) + ' total out';
    document.getElementById('sb-connections').textContent = stats.connections + ' connections';
    document.getElementById('sb-uptime').textContent = 'Up: ' + (stats.uptime || '0s');

    var outNow = stats.bytes_sent || 0;
    var inNow = stats.bytes_recv || 0;
    if (lastBytesOut > 0 || lastBytesIn > 0) {
      bwHistory.push({ out: outNow - lastBytesOut, in: inNow - lastBytesIn });
      if (bwHistory.length > 120) bwHistory.shift();
      drawGraph();
      var totalSpeed = bwHistory.length > 0 ? bwHistory[bwHistory.length-1].out + bwHistory[bwHistory.length-1].in : 0;
      document.getElementById('sb-total-speed').textContent = fmtBytes(totalSpeed) + '/s';
    }
    lastBytesOut = outNow;
    lastBytesIn = inNow;
  } catch(e) { /* silent */ }
}

// ---- Bandwidth Graph ----
function drawGraph() {
  var c = document.getElementById('bw-graph');
  if (!c) return;
  c.width = c.offsetWidth;
  var ctx = c.getContext('2d');
  var W = c.width, H = 30;
  ctx.fillStyle = '#000';
  ctx.fillRect(0, 0, W, H);
  if (bwHistory.length < 2) return;

  var maxV = 1024;
  bwHistory.forEach(function(p) {
    if (p.out > maxV) maxV = p.out;
    if (p.in > maxV) maxV = p.in;
  });

  var step = W / (bwHistory.length - 1);
  ctx.lineWidth = 1;
  ['out','in'].forEach(function(key) {
    ctx.beginPath();
    ctx.strokeStyle = key === 'out' ? '#e090a0' : '#e0d090';
    for (var i = 0; i < bwHistory.length; i++) {
      var x = i * step, y = H - 2 - (bwHistory[i][key] / maxV) * (H - 4);
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.stroke();
  });
}

// ---- Resizable Splitters ----
(function() {
  var vs = document.getElementById('splitter-v');
  var hs = document.getElementById('splitter-h');
  var dragging = null, sx, sy, sw, sh;

  vs.addEventListener('mousedown', function(e) {
    dragging = 'v'; sx = e.clientX; sw = document.getElementById('vfs-panel').offsetWidth;
    document.body.style.cursor = 'col-resize'; e.preventDefault();
  });
  hs.addEventListener('mousedown', function(e) {
    dragging = 'h'; sy = e.clientY; sh = document.getElementById('conn-panel').offsetHeight;
    document.body.style.cursor = 'row-resize'; e.preventDefault();
  });
  document.addEventListener('mousemove', function(e) {
    if (!dragging) return;
    if (dragging === 'v') {
      document.getElementById('vfs-panel').style.width = Math.max(80, Math.min(600, sw + e.clientX - sx)) + 'px';
    } else {
      document.getElementById('conn-panel').style.height = Math.max(40, Math.min(300, sh + sy - e.clientY)) + 'px';
    }
    drawGraph();
  });
  document.addEventListener('mouseup', function() { dragging = null; document.body.style.cursor = ''; });
})();

// ---- Log Filter ----
document.getElementById('log-filter').addEventListener('input', function() {
  var f = this.value.toLowerCase();
  document.querySelectorAll('#log-box .log-line').forEach(function(l) {
    l.style.display = f ? (l.textContent.toLowerCase().indexOf(f) >= 0 ? '' : 'none') : '';
  });
});

// ---- Drag & Drop (native, via Wails) ----
document.addEventListener('dragover', function(e) { e.preventDefault(); });
document.addEventListener('drop', async function(e) {
  e.preventDefault();
  if (!W || !W.HandleDrop) return;
  var paths = [];
  if (e.dataTransfer.items) {
    for (var i = 0; i < e.dataTransfer.items.length; i++) {
      var item = e.dataTransfer.items[i];
      if (item.kind === 'file') {
        var f = item.getAsFile();
        if (f && f.path) paths.push(f.path);
      }
    }
  }
  if (paths.length === 0) return;
  try {
    var parent = selectedNode ? (selectedNode.vpath || '/') : '/';
    var added = await W.HandleDrop(paths, parent);
    if (added && added.length) {
      refreshTree();
      log('Added ' + added.length + ' item(s) via drag & drop');
    }
  } catch(err) { log('Drop error: ' + err); }
});

// ---- Init ----
async function init() {
  // Detect Wails
  if (wailsReady()) {
    log('Desktop mode active (Wails bridge available)');
  } else {
    log('Headless mode (no Wails bridge - native features disabled)');
  }

  // Determine API base
  try {
    var cfg = await api('/config');
    serverPort = (cfg && cfg.server && cfg.server.port) || 8080;
  } catch(e) { serverPort = 8080; }
  API_BASE = 'http://localhost:' + serverPort;
  document.getElementById('port-label').textContent = serverPort;
  document.getElementById('url-box').value = 'http://localhost:' + serverPort;

  log('HFS starting...');
  log('API base: ' + API_BASE);

  await refreshTree();
  log('VFS tree loaded');

  // Initial status
  await refreshStatus();
  await refreshConnections();

  // Periodic updates
  setInterval(function() {
    refreshStatus();
    refreshConnections();
  }, 2000);

  document.getElementById('btn-start').classList.add('running');
  document.getElementById('btn-start').innerHTML = '■ Stop';
  log('Ready.');
}

// Wait for DOM and Wails
document.addEventListener('DOMContentLoaded', function() {
  // Wails runtime is injected automatically - give it a moment
  var attempts = 0;
  function tryInit() {
    attempts++;
    if (wailsReady() || attempts > 20) {
      init();
    } else {
      setTimeout(tryInit, 100);
    }
  }
  // Start immediately - Wails detection can happen async
  init();
});

})();
