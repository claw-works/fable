// Fable 观察端 + 玩家模式
(function () {
  const $ = (sel) => document.querySelector(sel);
  let worldConfig = null;
  let playerJoined = false;
  let lastState = null;

  // tickCards: [{ tick, time, agentId, name, emotion, location, items: [{icon,label,text,cls}] }]
  let tickCards = [];
  let focusAgent = null; // 聚焦的 agent_id

  async function loadWorldConfig() {
    try {
      const res = await fetch("/api/config/world");
      worldConfig = await res.json();
      const locSelect = $("#p-location");
      const moveSelect = $("#move-target");
      (worldConfig.locations || []).forEach((loc) => {
        locSelect.add(new Option(loc.name, loc.name));
        moveSelect.add(new Option(loc.name, loc.name));
      });
    } catch (e) { console.error("load config:", e); }
  }

  async function checkPlayerState() {
    try {
      const res = await fetch("/api/player/state");
      const data = await res.json();
      if (data.player_id) { playerJoined = true; showPlayerUI(data); }
    } catch (e) { /* not joined */ }
  }

  function connect() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${location.host}/ws`);
    ws.onopen = () => { $("#ws-status").textContent = "● 已连接"; $("#ws-status").className = "connected"; };
    ws.onclose = () => { $("#ws-status").textContent = "● 已断开"; $("#ws-status").className = "disconnected"; setTimeout(connect, 3000); };
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === 'agent_update' && msg.agent_state) {
          const a = msg.agent_state;
          if (lastState) {
            const idx = lastState.agents.findIndex(x => x.agent_id === a.agent_id);
            if (idx >= 0) lastState.agents[idx] = a; else lastState.agents.push(a);
            if (a.location) {
              for (const ids of Object.values(lastState.locations)) {
                const i = ids.indexOf(a.agent_id);
                if (i >= 0) ids.splice(i, 1);
              }
              if (!lastState.locations[a.location]) lastState.locations[a.location] = [];
              lastState.locations[a.location].push(a.agent_id);
            }
            lastState.game_time = msg.game_time || lastState.game_time;
            lastState.tick = msg.tick !== undefined ? msg.tick : lastState.tick;
          }
          $("#game-time").textContent = msg.game_time || "—";
          $("#tick-count").textContent = `Tick: ${msg.tick}`;
          addAgentTickCard(a, msg.tick, msg.game_time);
          // NPC 对玩家说话 → 加到玩家日志
          if (a.dialogue && a.target === (playerJoined ? 'player' : null)) {
            addPlayerLog(`💬 ${a.name || a.agent_id} 对你说：「${a.dialogue}」`, msg.tick, msg.game_time);
          }
          renderLocations();
          renderTickStream();
        } else if (msg.type === 'event') {
          // 玩家/系统事件 → 归入玩家 tick 卡片
          addPlayerEventCard(msg.text, msg.tick, msg.game_time);
          addPlayerLog(msg.text, msg.tick, msg.game_time);
          renderTickStream();
        } else if (msg.tick !== undefined) {
          lastState = msg;
          ingestFullState(msg);
          render(msg);
        }
      } catch (err) { console.error("parse error:", err); }
    };
  }

  // 将一个 agent 在某 tick 的状态整合为一张卡片（如果同 tick 同 agent 已存在则合并）
  function addAgentTickCard(a, tick, time) {
    const items = [];
    if (a.action) items.push({ icon: '⚡', label: '行动', text: a.action, cls: 'action' });
    if (a.dialogue) items.push({ icon: '💬', label: '对话', text: `「${a.dialogue}」`, cls: 'dialogue' });
    if (a.inner_thought) items.push({ icon: '💭', label: '内心', text: a.inner_thought, cls: 'thought' });
    if (a.memory_update && a.memory_update.length) items.push({ icon: '📝', label: '记忆', text: a.memory_update.join('；'), cls: 'memory' });
    if (!items.length) return;

    const key = `${tick}::${a.agent_id}`;
    const existing = tickCards.find(c => c.key === key);
    const card = {
      key, tick, time,
      agentId: a.agent_id,
      name: a.name || a.agent_id,
      emotion: a.emotion || '',
      location: a.location || '',
      items,
    };
    if (existing) Object.assign(existing, card);
    else tickCards.push(card);

    if (tickCards.length > 300) tickCards = tickCards.slice(-300);
  }

  // 玩家事件：累积到当前 tick 的玩家卡片
  function addPlayerEventCard(text, tick, time) {
    if (!text) return;
    const key = `${tick}::player`;
    const existing = tickCards.find(c => c.key === key);
    const item = { icon: '🎭', label: '玩家', text: text.replace(/^【玩家】/, ''), cls: 'player-event' };
    if (existing) {
      existing.items.push(item);
    } else {
      const ps = lastState && lastState.agents ? (lastState.agents.find(x => x.agent_id === 'player')) : null;
      tickCards.push({
        key, tick, time,
        agentId: 'player',
        name: (ps && ps.name) || '玩家',
        emotion: ps ? ps.emotion : '',
        location: ps ? ps.location : '',
        items: [item],
        isPlayer: true,
      });
    }
    if (tickCards.length > 300) tickCards = tickCards.slice(-300);
  }

  function ingestFullState(state) {
    (state.agents || []).forEach(a => addAgentTickCard(a, state.tick, state.game_time));
    // 玩家状态也作为一张卡片
    // 注：后端 PlayerState 不在 agents 里，由 loadCurrentState 单独处理
  }

  async function loadCurrentState() {
    try {
      const res = await fetch("/api/state");
      lastState = await res.json();
      ingestFullState(lastState);
      render(lastState);
    } catch (e) { console.error(e); }
  }

  function render(state) {
    $("#game-time").textContent = state.game_time || "—";
    $("#tick-count").textContent = `Tick: ${state.tick}`;
    renderLocations();
    renderTickStream();
    if (playerJoined) updateConvTargets();
  }

  function renderLocations() {
    const grid = $("#location-grid");
    if (!lastState) { grid.innerHTML = ''; return; }
    const agentMap = {};
    (lastState.agents || []).forEach((a) => (agentMap[a.agent_id] = a));
    const locs = lastState.locations || {};

    grid.innerHTML = Object.entries(locs).map(([loc, ids]) => {
      const tags = (ids || []).map(id => {
        const a = agentMap[id];
        const name = a ? (a.name || a.agent_id) : id;
        const emotion = a ? a.emotion : '';
        const isFocus = focusAgent === id ? ' focused' : '';
        return `<span class="agent-tag${isFocus}" onclick="focusOnAgent('${id}')" oncontextmenu="event.preventDefault(); showAgentCard('${id}')">${name}${emotion ? ' · ' + emotion : ''}</span>`;
      }).join('');
      return `<div class="location-card"><h3>${loc}</h3><div class="agents">${tags || '<em>无人</em>'}</div></div>`;
    }).join('');
  }

  function renderTickStream() {
    const el = $("#tick-stream");
    const cards = focusAgent ? tickCards.filter(c => c.agentId === focusAgent) : tickCards;
    const list = cards.slice(-60); // 取最近 60 张

    // 按 tick 分组
    const groups = {};
    list.forEach(c => { (groups[c.tick] ||= []).push(c); });
    const ticks = Object.keys(groups).map(Number).sort((a, b) => a - b);

    el.innerHTML = ticks.map(t => {
      const g = groups[t];
      const time = g[0].time;
      const cardsHtml = g.map(c => `
        <div class="agent-card-item${c.isPlayer ? ' player' : ''}" onclick="focusOnAgent('${c.agentId}')">
          <div class="aci-head">
            <span class="aci-name">${c.isPlayer ? '🎭 ' : ''}${c.name}</span>
            ${c.emotion ? `<span class="aci-emotion">${c.emotion}</span>` : ''}
            ${c.location ? `<span class="aci-loc">📍${c.location}</span>` : ''}
          </div>
          <div class="aci-body">
            ${c.items.map(i => `<div class="aci-line ${i.cls}"><span class="aci-icon">${i.icon}</span><span class="aci-text">${escapeHtml(i.text)}</span></div>`).join('')}
          </div>
        </div>
      `).join('');
      return `<div class="tick-block">
        <div class="tick-header"><span class="tick-no">Tick ${t}</span><span class="tick-time">${time || ''}</span></div>
        <div class="tick-cards">${cardsHtml}</div>
      </div>`;
    }).join('');

    el.scrollTop = el.scrollHeight;
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  window.focusOnAgent = (id) => {
    focusAgent = id;
    const a = lastState ? (lastState.agents || []).find(x => x.agent_id === id) : null;
    const name = a ? (a.name || id) : id;
    $("#focus-name").textContent = name;
    $("#focus-bar").style.display = 'flex';
    $("#timeline-title").textContent = `🎯 聚焦事件流`;
    renderLocations();
    renderTickStream();
  };

  window.clearFocus = () => {
    focusAgent = null;
    $("#focus-bar").style.display = 'none';
    $("#timeline-title").textContent = '📜 事件流';
    renderLocations();
    renderTickStream();
  };

  function showPlayerUI(ps) {
    $("#player-not-joined").style.display = "none";
    $("#join-form").style.display = "none";
    $("#player-joined").style.display = "block";
    $("#player-log-section").style.display = "block";
    const name = ps.name || ps.player_id || "玩家";
    $("#player-info").innerHTML = `<strong>${name}</strong> · 📍 ${ps.location} · ${ps.action || ""}`;
  }

  window.showJoinForm = () => { $("#player-not-joined").style.display = "none"; $("#join-form").style.display = "block"; };
  window.hideJoinForm = () => { $("#join-form").style.display = "none"; $("#player-not-joined").style.display = "block"; };

  function hideSubPanels() {
    ['move-panel', 'act-panel', 'conv-picker'].forEach(id => $(('#' + id)).style.display = 'none');
  }
  window.showMovePanel = () => { hideSubPanels(); $('#move-panel').style.display = 'flex'; };
  window.showActPanel = () => { hideSubPanels(); $('#act-panel').style.display = 'flex'; };
  window.showConvPicker = () => { hideSubPanels(); updateConvTargets(); $('#conv-picker').style.display = 'flex'; };

  function updateConvTargets() {
    const sel = $('#conv-target');
    sel.innerHTML = '';
    if (!lastState || !lastState.locations) return;
    for (const [, ids] of Object.entries(lastState.locations)) {
      if (ids.includes('player')) {
        ids.filter(id => id !== 'player').forEach(id => {
          const a = (lastState.agents || []).find(x => x.agent_id === id);
          const name = a ? (a.name || id) : id;
          sel.add(new Option(name, id));
        });
        break;
      }
    }
  }

  window.doJoin = async () => {
    const body = {
      id: "player",
      name: $("#p-name").value,
      age: parseInt($("#p-age").value) || 22,
      occupation: $("#p-occupation").value,
      personality: $("#p-personality").value,
      backstory: $("#p-backstory").value,
      init_location: $("#p-location").value,
    };
    await fetch("/api/player/join", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    playerJoined = true;
    showPlayerUI({ name: body.name, location: body.init_location, action: "刚刚到达" });
    addPlayerLog(`🎮 ${body.name} 加入了清水镇（${body.init_location}）`);
  };

  window.doLeave = async () => {
    await fetch("/api/player/leave", { method: "DELETE" });
    playerJoined = false;
    $("#player-joined").style.display = "none";
    $("#player-not-joined").style.display = "block";
    $("#player-log-section").style.display = "none";
    addPlayerLog(`👋 离开了清水镇`);
  };

  window.submitAction = async (type) => {
    const action = { type };
    if (type === "move") { action.location = $("#move-target").value; addPlayerLog(`🚶 前往「${action.location}」`); }
    if (type === "act") { action.content = $("#act-content").value; addPlayerLog(`⚡ ${action.content}`); $("#act-content").value = ""; }
    await fetch("/api/player/action", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(action) });
    hideSubPanels();
  };

  // ── 对话 Drawer ──
  window.startConversation = async () => {
    const npcID = $('#conv-target').value;
    if (!npcID) return;
    const a = (lastState.agents || []).find(x => x.agent_id === npcID);
    const npcName = a ? (a.name || npcID) : npcID;
    try {
      const res = await fetch('/api/conversation/start', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ npc_id: npcID }) });
      if (!res.ok) { alert(await res.text()); return; }
    } catch (e) { alert(e.message); return; }
    $('#conv-npc-name').textContent = `💬 与 ${npcName} 对话`;
    $('#conv-messages').innerHTML = `<div class="conv-system">你走向${npcName}，开始了一段对话…</div>`;
    $('#conv-drawer').classList.add('open');
    $('#conv-input').focus();
    hideSubPanels();
    addPlayerLog(`💬 开始与 ${npcName} 对话`);
  };

  window.sendConvMessage = async () => {
    const input = $('#conv-input');
    const content = input.value.trim();
    if (!content) return;
    input.value = '';
    appendConvMsg('player', content);
    const loading = document.createElement('div');
    loading.className = 'conv-msg npc loading';
    loading.textContent = '思考中…';
    $('#conv-messages').appendChild(loading);
    $('#conv-messages').scrollTop = $('#conv-messages').scrollHeight;
    try {
      const res = await fetch('/api/conversation/say', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content }) });
      const data = await res.json();
      loading.remove();
      if (data.reply) { appendConvMsg('npc', data.reply); addPlayerLog(`💬 NPC回复：${data.reply.slice(0, 40)}…`); }
    } catch (e) { loading.textContent = '回复失败'; }
  };

  function appendConvMsg(role, text) {
    const div = document.createElement('div');
    div.className = `conv-msg ${role}`;
    div.textContent = role === 'player' ? `你：${text}` : text;
    $('#conv-messages').appendChild(div);
    $('#conv-messages').scrollTop = $('#conv-messages').scrollHeight;
  }

  window.endConversation = async () => {
    try { await fetch('/api/conversation/end', { method: 'DELETE' }); } catch (e) {}
    $('#conv-drawer').classList.remove('open');
    addPlayerLog(`💬 对话结束`);
  };

  // ── 玩家事件日志 ──
  let playerLogs = [];
  function addPlayerLog(text, tick, time) {
    const t = time || (lastState ? lastState.game_time : '');
    const k = tick !== undefined ? tick : (lastState ? lastState.tick : 0);
    playerLogs.unshift({ time: t, tick: k, text });
    if (playerLogs.length > 100) playerLogs.length = 100;
    renderPlayerLog();
  }
  function renderPlayerLog() {
    const el = $('#player-log');
    if (!el) return;
    el.innerHTML = playerLogs.map(l => `<div class="plog-item"><span class="tick">T${l.tick}</span><span class="time">${l.time}</span> ${escapeHtml(l.text)}</div>`).join('');
  }

  // 加载存档
  window.showLoadSaveDialog = async () => {
    let overlay = $('#load-save-overlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'load-save-overlay';
      overlay.addEventListener('click', (e) => { if (e.target === overlay) overlay.style.display = 'none'; });
      document.body.appendChild(overlay);
    }
    let worlds = [];
    try { worlds = await (await fetch('/api/worlds')).json(); } catch (e) {}
    const worldOptions = (worlds || []).map(w => `<option value="${w}">${w}</option>`).join('');
    overlay.innerHTML = `<div class="agent-card">
      <h3>📂 加载存档</h3>
      <div class="card-field"><label>世界</label><select id="ls-world" onchange="loadSavesForLoad()">${worldOptions}</select></div>
      <div class="card-field"><label>存档</label><select id="ls-save"></select></div>
      <div style="margin-top:1rem;display:flex;gap:0.5rem">
        <button onclick="doLoadSave()">加载</button>
        <button onclick="this.closest('#load-save-overlay').style.display='none'">取消</button>
      </div>
    </div>`;
    overlay.style.display = 'flex';
    loadSavesForLoad();
  };

  window.loadSavesForLoad = async () => {
    const worldID = $('#ls-world') ? $('#ls-world').value : '';
    if (!worldID) return;
    try {
      const saves = await (await fetch(`/api/saves?world=${encodeURIComponent(worldID)}`)).json();
      const sel = $('#ls-save');
      sel.innerHTML = '';
      (saves || []).forEach(s => sel.add(new Option(s, s)));
    } catch (e) {}
  };

  window.doLoadSave = async () => {
    const worldID = $('#ls-world').value;
    const saveName = $('#ls-save').value;
    if (!worldID || !saveName) { alert('请选择世界和存档'); return; }
    try {
      const res = await fetch('/api/new-game', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ world_id: worldID, save_name: saveName }) });
      if (!res.ok) { alert(await res.text()); return; }
      window.location.reload();
    } catch (e) { alert('加载失败: ' + e.message); }
  };

  window.showCurrentInfo = () => {
    if (!lastState) { alert('暂无数据'); return; }
    alert(`世界: ${lastState.game_time || '—'}\nTick: ${lastState.tick || 0}\nNPC: ${(lastState.agents || []).length} 个\n地点: ${Object.keys(lastState.locations || {}).length} 个`);
  };

  window.showNewGameDialog = async () => {
    let overlay = $('#new-game-overlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'new-game-overlay';
      overlay.addEventListener('click', (e) => { if (e.target === overlay) overlay.style.display = 'none'; });
      document.body.appendChild(overlay);
    }
    let worlds = [];
    try { worlds = await (await fetch('/api/worlds')).json(); } catch (e) {}
    const worldOptions = (worlds || []).map(w => `<option value="${w}">${w}</option>`).join('');
    overlay.innerHTML = `<div class="agent-card">
      <h3>🎮 新建游戏</h3>
      <div class="card-field"><label>选择世界</label><select id="ng-world" onchange="loadSavesForWorld()">${worldOptions}</select></div>
      <div class="card-field"><label>已有存档</label><span id="ng-existing">加载中…</span></div>
      <div class="card-field"><label>新存档名</label><input id="ng-save" value="" placeholder="输入存档名，如 save1" /></div>
      <div style="margin-top:1rem;display:flex;gap:0.5rem">
        <button onclick="doNewGame()">创建</button>
        <button onclick="this.closest('#new-game-overlay').style.display='none'">取消</button>
      </div>
    </div>`;
    overlay.style.display = 'flex';
    loadSavesForWorld();
  };

  window.loadSavesForWorld = async () => {
    const worldID = $('#ng-world') ? $('#ng-world').value : '';
    if (!worldID) return;
    try {
      const res = await fetch(`/api/saves?world=${encodeURIComponent(worldID)}`);
      const saves = await res.json();
      $('#ng-existing').textContent = (saves && saves.length) ? saves.join(', ') : '无';
    } catch (e) { $('#ng-existing').textContent = '无'; }
  };

  window.doNewGame = async () => {
    const worldID = $('#ng-world').value;
    const saveName = $('#ng-save').value.trim();
    if (!worldID || !saveName) { alert('请填写世界和存档名'); return; }
    try {
      const res = await fetch('/api/new-game', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ world_id: worldID, save_name: saveName }) });
      if (!res.ok) { alert(await res.text()); return; }
      window.location.reload();
    } catch (e) { alert('创建失败: ' + e.message); }
  };

  window.showAgentCard = (id) => {
    if (!lastState) return;
    const a = (lastState.agents || []).find(x => x.agent_id === id);
    if (!a) return;
    const name = a.name || a.agent_id;
    let existing = $('#agent-card-overlay');
    if (!existing) {
      existing = document.createElement('div');
      existing.id = 'agent-card-overlay';
      existing.addEventListener('click', (e) => { if (e.target === existing) existing.style.display = 'none'; });
      document.body.appendChild(existing);
    }
    existing.innerHTML = `<div class="agent-card">
      <h3>${name}</h3>
      <div class="card-field"><label>📍 位置</label><span>${a.location}</span></div>
      <div class="card-field"><label>😊 情绪</label><span>${a.emotion || '—'}</span></div>
      <div class="card-field"><label>⚡ 行动</label><span>${a.action || '—'}</span></div>
      ${a.dialogue ? `<div class="card-field"><label>💬 对话</label><span>「${a.dialogue}」</span></div>` : ''}
      ${a.inner_thought ? `<div class="card-field"><label>💭 内心</label><span>${a.inner_thought}</span></div>` : ''}
      ${a.memory_update && a.memory_update.length ? `<div class="card-field"><label>📝 记忆</label><span>${a.memory_update.join('；')}</span></div>` : ''}
      <div style="display:flex;gap:0.5rem;margin-top:1rem">
        <button onclick="focusOnAgent('${id}'); this.closest('#agent-card-overlay').style.display='none'">🎯 聚焦此角色</button>
        <button onclick="this.closest('#agent-card-overlay').style.display='none'">关闭</button>
      </div>
    </div>`;
    existing.style.display = 'flex';
  };

  loadWorldConfig();
  loadCurrentState();
  checkPlayerState();
  connect();

  document.addEventListener('click', (e) => {
    const menu = document.querySelector('.menu-dropdown');
    if (menu && !menu.contains(e.target)) menu.classList.remove('open');
  });
})();
