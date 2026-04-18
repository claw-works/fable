// 清水镇像素前端 - 游戏主逻辑
(function () {
  const $ = (s) => document.querySelector(s);
  const canvas = $('#game-canvas');
  const ctx = canvas.getContext('2d', { alpha: false });

  // 满屏 Canvas
  function resizeCanvas() {
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
    ctx.imageSmoothingEnabled = false;
    ctx.mozImageSmoothingEnabled = false;
    ctx.webkitImageSmoothingEnabled = false;
    ctx.msImageSmoothingEnabled = false;
  }
  resizeCanvas();
  window.addEventListener('resize', resizeCanvas);

  // 视口偏移（摄像机）
  let camX = 0, camY = 0;
  const MAP_PX_W = MAP_COLS * TILE;
  const MAP_PX_H = MAP_ROWS * TILE;
  let scale = 2; // 像素放大倍数，可缩放
  const SCALE_MIN = 1, SCALE_MAX = 5;

  // 拖拽平移状态
  let dragging = false;
  let dragStartX = 0, dragStartY = 0;
  let camDragging = false; // 是否正在手动拖拽摄像机

  // ── 状态 ──
  const map = generateMap();
  let worldConfig = null;
  let gameTime = 'Day1 08:00';
  let tick = 0;
  let events = [];
  let playerJoined = false;

  // NPC 列表 {id, name, color, tx, ty, targetTx, targetTy, dialogue, thought, emotion}
  const NPC_COLORS = {
    lao_chen: '#CD853F', lao_zhang: '#696969', sun_xiansheng: '#556B2F',
    xiao_liu: '#DAA520', wang_laohan: '#2E8B57', su_niang: '#CD853F',
    qin_xiuniang: '#DEB887', sun_miaoyin: '#9370DB',
  };
  let npcs = {};

  // 玩家
  let player = {
    name: '', tx: 14, ty: 17, color: '#FF4444', location: '茶馆',
    joined: false, dialogue: '',
  };

  // 键盘状态
  const keys = {};
  let moveTimer = 0;
  const MOVE_INTERVAL = 150; // ms 移动间隔

  // ── 初始化 ──
  async function init() {
    await loadWorldConfig();
    await loadCurrentState();
    connectWS();
    checkPlayerState();
    document.addEventListener('keydown', e => { keys[e.key] = true; });
    document.addEventListener('keyup', e => { keys[e.key] = false; });

    // 滚轮/触控板：ctrlKey=捏合缩放，否则=平移
    canvas.addEventListener('wheel', e => {
      e.preventDefault();
      if (e.ctrlKey) {
        // 捏合缩放（Safari 触控板 + 鼠标 Ctrl+滚轮）
        const oldScale = scale;
        scale = Math.max(SCALE_MIN, Math.min(SCALE_MAX, scale - e.deltaY * 0.01));
        const mx = e.offsetX, my = e.offsetY;
        camX = (camX + mx) * (scale / oldScale) - mx;
        camY = (camY + my) * (scale / oldScale) - my;
      } else {
        // 双指拖动 / 普通滚轮 → 平移
        camX += e.deltaX;
        camY += e.deltaY;
        camDragging = true;
      }
      clampCam();
    }, { passive: false });

    // 鼠标拖拽平移
    canvas.addEventListener('mousedown', e => {
      if (e.button === 0) { dragging = true; dragStartX = e.clientX; dragStartY = e.clientY; camDragging = false; }
    });
    window.addEventListener('mousemove', e => {
      if (!dragging) return;
      const dx = e.clientX - dragStartX, dy = e.clientY - dragStartY;
      if (!camDragging && Math.abs(dx) + Math.abs(dy) > 3) camDragging = true;
      if (camDragging) {
        camX -= dx; camY -= dy;
        clampCam();
        dragStartX = e.clientX; dragStartY = e.clientY;
      }
    });
    window.addEventListener('mouseup', () => { dragging = false; });

    // 触摸拖拽 + 捏合缩放
    let lastTouchDist = 0;
    let lastTouchX = 0, lastTouchY = 0;
    canvas.addEventListener('touchstart', e => {
      if (e.touches.length === 1) {
        dragging = true; camDragging = false;
        lastTouchX = e.touches[0].clientX; lastTouchY = e.touches[0].clientY;
      } else if (e.touches.length === 2) {
        dragging = false;
        lastTouchDist = Math.hypot(e.touches[1].clientX - e.touches[0].clientX, e.touches[1].clientY - e.touches[0].clientY);
      }
    }, { passive: true });
    canvas.addEventListener('touchmove', e => {
      e.preventDefault();
      if (e.touches.length === 1 && dragging) {
        const dx = e.touches[0].clientX - lastTouchX, dy = e.touches[0].clientY - lastTouchY;
        if (!camDragging && Math.abs(dx) + Math.abs(dy) > 3) camDragging = true;
        if (camDragging) {
          camX -= dx; camY -= dy; clampCam();
          lastTouchX = e.touches[0].clientX; lastTouchY = e.touches[0].clientY;
        }
      } else if (e.touches.length === 2) {
        const dist = Math.hypot(e.touches[1].clientX - e.touches[0].clientX, e.touches[1].clientY - e.touches[0].clientY);
        if (lastTouchDist > 0) {
          const oldScale = scale;
          scale = Math.max(SCALE_MIN, Math.min(SCALE_MAX, scale * (dist / lastTouchDist)));
          const cx = (e.touches[0].clientX + e.touches[1].clientX) / 2;
          const cy = (e.touches[0].clientY + e.touches[1].clientY) / 2;
          camX = (camX + cx) * (scale / oldScale) - cx;
          camY = (camY + cy) * (scale / oldScale) - cy;
          clampCam();
        }
        lastTouchDist = dist;
      }
    }, { passive: false });
    canvas.addEventListener('touchend', () => { dragging = false; lastTouchDist = 0; });

    requestAnimationFrame(gameLoop);
  }

  async function loadWorldConfig() {
    try {
      const res = await fetch('/api/config/world');
      worldConfig = await res.json();
      const sel = $('#p-location');
      (worldConfig.locations || []).forEach(l => sel.add(new Option(l.name, l.name)));
    } catch (e) { console.error(e); }
  }

  async function loadCurrentState() {
    try {
      const res = await fetch('/api/state');
      const state = await res.json();
      handleState(state);
    } catch (e) { console.error(e); }
  }

  async function checkPlayerState() {
    try {
      const res = await fetch('/api/player/state');
      const d = await res.json();
      if (d.player_id) {
        player.joined = true;
        player.name = d.player_id;
        player.location = d.location;
        const c = getLocationCenter(d.location);
        if (c) { player.tx = c.x; player.ty = c.y; }
        showJoinedUI();
      }
    } catch (e) { /* not joined */ }
  }

  // ── WebSocket ──
  function connectWS() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/ws`);
    ws.onopen = () => { $('#ws-dot').className = 'dot connected'; };
    ws.onclose = () => { $('#ws-dot').className = 'dot'; setTimeout(connectWS, 3000); };
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === 'agent_update') {
          handleAgentUpdate(msg);
        } else if (msg.type === 'event') {
          addEvent(msg.game_time, msg.text, '');
          renderEvents();
        } else if (msg.locations) {
          // 完整 WorldState（tick 结束）
          handleState(msg);
        }
      } catch (err) { console.error(err); }
    };
  }

  function handleState(state) {
    gameTime = state.game_time || gameTime;
    tick = state.tick || tick;
    $('#hud-time').textContent = gameTime;
    $('#hud-tick').textContent = `Tick ${tick}`;

    // 完整状态更新：刷新所有 NPC 位置
    const locs = state.locations || {};
    const agentMap = {};
    (state.agents || []).forEach(a => { agentMap[a.agent_id] = a; });

    for (const [locName, ids] of Object.entries(locs)) {
      (ids || []).forEach((id, i) => {
        if (id === 'player') return;
        const a = agentMap[id] || {};
        const pos = getLocationSpread(locName, i);
        if (!pos) return;
        if (!npcs[id]) {
          npcs[id] = { id, name: a.name || a.agent_id || id, color: NPC_COLORS[id] || '#888',
            tx: pos.x, ty: pos.y,
            dialogue: '', thought: '', emotion: '', bubbleTimer: 0 };
        }
        npcs[id].targetTx = pos.x;
        npcs[id].targetTy = pos.y;
        npcs[id].dialogue = a.dialogue || '';
        npcs[id].thought = a.inner_thought || '';
        npcs[id].emotion = a.emotion || '';
        npcs[id].location = locName;
        if (a.dialogue || a.inner_thought) npcs[id].bubbleTimer = 6000;
      });
    }

    // 加载事件流
    if (state.events && state.events.length > 0) {
      state.events.forEach(text => addEvent(gameTime, text, ''));
    }
    // 加载 agents 的对话和行动
    (state.agents || []).forEach(a => {
      const name = a.name || a.agent_id;
      if (a.dialogue) addEvent(gameTime, `💬 ${name}：「${a.dialogue}」`, 'dialogue');
      if (a.action) addEvent(gameTime, `${name} 在${a.location}${a.action}`, '');
    });
    renderEvents();
  }

  // 处理单个 NPC 推理完成的增量事件（实时推送）
  function handleAgentUpdate(msg) {
    const a = msg.agent_state;
    if (!a) return;
    const id = a.agent_id;
    gameTime = msg.game_time || gameTime;
    tick = msg.tick || tick;
    $('#hud-time').textContent = gameTime;
    $('#hud-tick').textContent = `Tick ${tick}`;

    // 更新 NPC 状态
    const loc = a.location;
    // 计算该角色在同地点中的索引
    const sameLocIds = Object.keys(npcs).filter(k => npcs[k].location === loc);
    let idx = sameLocIds.indexOf(id);
    if (idx < 0) idx = sameLocIds.length;
    const pos = getLocationSpread(loc, idx);
    if (pos) {
      if (!npcs[id]) {
        npcs[id] = { id, name: a.name || id, color: NPC_COLORS[id] || '#888',
          tx: pos.x, ty: pos.y, dialogue: '', thought: '', emotion: '', bubbleTimer: 0 };
      }
      npcs[id].targetTx = pos.x;
      npcs[id].targetTy = pos.y;
      npcs[id].dialogue = a.dialogue || '';
      npcs[id].thought = a.inner_thought || '';
      npcs[id].emotion = a.emotion || '';
      npcs[id].location = loc;
      if (a.dialogue || a.inner_thought) npcs[id].bubbleTimer = 6000;
    }

    // 立即显示事件
    const name = npcs[id] ? npcs[id].name : id;
    if (a.dialogue) addEvent(gameTime, `💬 ${name}：「${a.dialogue}」`, 'dialogue');
    if (a.inner_thought) addEvent(gameTime, `💭 ${name}：${a.inner_thought}`, 'thought');
    addEvent(gameTime, `${name} 在${a.location}${a.action}`, '');
    renderEvents();
  }

  function addEvent(time, text, cls) {
    events.unshift({ time, text, cls });
    if (events.length > 100) events.length = 100;
  }

  // 待显示的事件队列（逐条动画）
  let pendingEvents = [];
  let eventFlushTimer = 0;
  const EVENT_INTERVAL = 400; // 每条事件间隔 ms

  function queueEvent(time, text, cls) {
    pendingEvents.push({ time, text, cls });
  }

  function flushEvents(dt) {
    if (pendingEvents.length === 0) return;
    eventFlushTimer += dt;
    if (eventFlushTimer < EVENT_INTERVAL) return;
    eventFlushTimer = 0;
    const ev = pendingEvents.shift();
    addEvent(ev.time, ev.text, ev.cls);
    renderEvents();
  }

  function renderEvents() {
    const el = $('#event-list');
    el.innerHTML = events.slice(0, 50).map((ev, i) =>
      `<div class="ev ${ev.cls || ''}${i === 0 ? ' new' : ''}"><span class="t">[${ev.time}]</span> ${ev.text}</div>`
    ).join('');
  }

  // ── 玩家移动（键盘 WASD / 方向键）──
  function handlePlayerMove(dt) {
    if (!player.joined) return;
    // 焦点在输入框时不响应移动
    const ae = document.activeElement;
    if (ae && (ae.tagName === 'INPUT' || ae.tagName === 'TEXTAREA' || ae.tagName === 'SELECT')) return;

    moveTimer += dt;
    if (moveTimer < MOVE_INTERVAL) return;
    moveTimer = 0;

    let dx = 0, dy = 0;
    if (keys['ArrowUp'] || keys['w'] || keys['W']) dy = -1;
    if (keys['ArrowDown'] || keys['s'] || keys['S']) dy = 1;
    if (keys['ArrowLeft'] || keys['a'] || keys['A']) dx = -1;
    if (keys['ArrowRight'] || keys['d'] || keys['D']) dx = 1;
    if (dx === 0 && dy === 0) return;

    const nx = player.tx + dx, ny = player.ty + dy;
    if (isWalkable(map, nx, ny)) {
      player.tx = nx;
      player.ty = ny;
      camDragging = false; // 移动时摄像机重新跟随
      // 检测进入新地点
      const loc = getLocationAt(nx, ny);
      if (loc && loc !== player.location) {
        player.location = loc;
        addEvent(gameTime, `🚶 你来到了「${loc}」`, 'move');
        renderEvents();
        // 通知后端移动
        if (player.joined) {
          fetch('/api/player/action', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ type: 'move', location: loc }),
          }).catch(() => {});
        }
      }
    }
  }

  // ── NPC 缓动移动 ──
  function updateNPCs(dt) {
    for (const npc of Object.values(npcs)) {
      if (npc.targetTx !== undefined && npc.targetTy !== undefined) {
        if (npc.tx < npc.targetTx) npc.tx++;
        else if (npc.tx > npc.targetTx) npc.tx--;
        if (npc.ty < npc.targetTy) npc.ty++;
        else if (npc.ty > npc.targetTy) npc.ty--;
      }
      // 气泡倒计时
      if (npc.bubbleTimer > 0) {
        npc.bubbleTimer -= dt;
        if (npc.bubbleTimer <= 0) {
          npc.dialogue = '';
          npc.thought = '';
        }
      }
    }
  }

  function clampCam() {
    const PAD = 80; // 地图外额外空间，防止边缘文字被裁
    const maxX = MAP_PX_W * scale - canvas.width + PAD;
    const maxY = MAP_PX_H * scale - canvas.height + PAD;
    camX = Math.max(-PAD, Math.min(camX, maxX));
    camY = Math.max(-PAD, Math.min(camY, maxY));
  }

  // ── 渲染 ──
  function render() {
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    // 摄像机：玩家加入且未手动拖拽时跟随玩家
    if (player.joined && !camDragging) {
      const targetCamX = player.tx * TILE * scale - canvas.width / 2 + TILE * scale / 2;
      const targetCamY = player.ty * TILE * scale - canvas.height / 2 + TILE * scale / 2;
      camX += (targetCamX - camX) * 0.1;
      camY += (targetCamY - camY) * 0.1;
      clampCam();
    }

    ctx.save();
    ctx.translate(-Math.round(camX), -Math.round(camY));
    ctx.scale(scale, scale);

    renderMap(ctx, map);
    renderLocationLabels(ctx);

    // NPC
    for (const npc of Object.values(npcs)) {
      drawCharacter(ctx, npc.tx, npc.ty, npc.color, npc.name);
      if (npc.bubbleTimer > 0 && npc.dialogue) drawBubble(ctx, npc.tx, npc.ty, npc.name + '：「' + npc.dialogue + '」', '#FFD700');
      else if (npc.bubbleTimer > 0 && npc.thought) drawBubble(ctx, npc.tx, npc.ty, '💭 ' + npc.thought, '#888', true);
    }

    // 玩家
    if (player.joined) {
      drawCharacter(ctx, player.tx, player.ty, player.color, player.name || '你');
      const ppx = (player.tx * TILE) | 0;
      const ppy = (player.ty * TILE) | 0;
      ctx.strokeStyle = '#FF4444';
      ctx.lineWidth = 1;
      ctx.strokeRect(ppx + 0.5, ppy + 0.5, TILE - 1, TILE - 1);
    }

    ctx.restore();
  }

  function drawCharacter(ctx, tx, ty, color, name) {
    const px = (tx * TILE) | 0;
    const py = (ty * TILE) | 0;
    // 身体
    ctx.fillStyle = color;
    ctx.fillRect(px + 2, py + 2, TILE - 4, TILE - 4);
    // 白色边框
    ctx.strokeStyle = '#fff';
    ctx.strokeRect(px + 2.5, py + 2.5, TILE - 5, TILE - 5);
    // 名字
    ctx.font = '9px monospace';
    ctx.fillStyle = '#fff';
    ctx.textAlign = 'center';
    ctx.shadowColor = '#000';
    ctx.shadowBlur = 3;
    ctx.shadowOffsetX = 1;
    ctx.shadowOffsetY = 1;
    ctx.fillText(name, px + TILE / 2, py - 2);
    ctx.shadowBlur = 0;
    ctx.shadowOffsetX = 0;
    ctx.shadowOffsetY = 0;
    ctx.textAlign = 'left';
  }

  function drawBubble(ctx, tx, ty, text, borderColor, dashed) {
    const px = (tx * TILE + TILE / 2) | 0;
    const py = (ty * TILE - 20) | 0;
    ctx.font = '10px monospace';
    const maxW = 180;
    let display = text.length > 30 ? text.slice(0, 30) + '…' : text;
    const tw = Math.min(ctx.measureText(display).width + 12, maxW) | 0;
    ctx.fillStyle = 'rgba(0,0,0,0.85)';
    ctx.fillRect(px - (tw / 2) | 0, py - 14, tw, 18);
    ctx.strokeStyle = borderColor;
    ctx.lineWidth = 1;
    if (dashed) ctx.setLineDash([3, 2]);
    ctx.strokeRect((px - tw / 2) | 0, py - 14, tw, 18);
    ctx.setLineDash([]);
    ctx.fillStyle = dashed ? '#aaa' : '#fff';
    ctx.textAlign = 'center';
    ctx.fillText(display, px, py - 1);
    ctx.textAlign = 'left';
  }

  // ── 游戏循环 ──
  let lastTime = 0;
  function gameLoop(ts) {
    const dt = ts - lastTime;
    lastTime = ts;
    handlePlayerMove(dt);
    updateNPCs(dt);
    flushEvents(dt);
    render();
    requestAnimationFrame(gameLoop);
  }

  // ── 玩家加入/离开 UI ──
  window.showJoinForm = () => {
    $('#join-panel').style.display = 'block';
    $('#join-btn').style.display = 'none';
  };

  window.doJoin = async () => {
    const body = {
      id: 'player',
      name: $('#p-name').value || '李逍遥',
      age: parseInt($('#p-age').value) || 22,
      occupation: $('#p-occupation').value || '游侠',
      personality: $('#p-personality').value || '豪爽仗义',
      backstory: $('#p-backstory').value || '江湖漂泊的年轻侠客',
      init_location: $('#p-location').value || '茶馆',
    };
    try {
      await fetch('/api/player/join', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      player.joined = true;
      player.name = body.name;
      player.location = body.init_location;
      const c = getLocationCenter(body.init_location);
      if (c) { player.tx = c.x; player.ty = c.y; }
      showJoinedUI();
      addEvent(gameTime, `🎮 ${body.name} 加入了清水镇`, '');
      renderEvents();
    } catch (e) { console.error(e); }
  };

  function showJoinedUI() {
    $('#join-panel').style.display = 'none';
    $('#join-btn').style.display = 'none';
    $('#player-controls').style.display = 'block';
    $('#player-name-display').textContent = player.name;
    canvas.focus();
  }

  window.doLeave = async () => {
    try {
      await fetch('/api/player/leave', { method: 'DELETE' });
    } catch (e) {}
    player.joined = false;
    $('#player-controls').style.display = 'none';
    $('#join-btn').style.display = 'block';
    addEvent(gameTime, `👋 ${player.name} 离开了清水镇`, '');
    renderEvents();
  };

  window.doTalk = async () => {
    const content = $('#talk-input').value.trim();
    if (!content) return;
    try {
      await fetch('/api/player/action', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'talk', content, target: '' }),
      });
      addEvent(gameTime, `💬 ${player.name}：「${content}」`, 'dialogue');
      renderEvents();
      $('#talk-input').value = '';
    } catch (e) { console.error(e); }
  };

  window.doAct = async () => {
    const content = $('#act-input').value.trim();
    if (!content) return;
    try {
      await fetch('/api/player/action', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'act', content }),
      });
      addEvent(gameTime, `⚡ ${player.name}：${content}`, '');
      renderEvents();
      $('#act-input').value = '';
    } catch (e) { console.error(e); }
  };

  window.doTick = async () => {
    try { await fetch('/api/tick', { method: 'POST' }); } catch (e) { console.error(e); }
  };

  window.doStart = async () => {
    try { await fetch('/api/start', { method: 'POST' }); } catch (e) { console.error(e); }
  };

  window.doStop = async () => {
    try { await fetch('/api/stop', { method: 'POST' }); } catch (e) { console.error(e); }
  };

  init();
})();
