// Fable 观察端 + 玩家模式
(function () {
  const $ = (sel) => document.querySelector(sel);
  let events = [];
  let worldConfig = null;
  let playerJoined = false;
  let lastState = null;

  // 加载世界配置（地点列表）
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
    } catch (e) {
      console.error("load config:", e);
    }
  }

  // 检查玩家状态
  async function checkPlayerState() {
    try {
      const res = await fetch("/api/player/state");
      const data = await res.json();
      if (data.player_id) {
        playerJoined = true;
        showPlayerUI(data);
      }
    } catch (e) { /* not joined */ }
  }

  function connect() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${location.host}/ws`);

    ws.onopen = () => {
      $("#ws-status").textContent = "● 已连接";
      $("#ws-status").className = "connected";
    };
    ws.onclose = () => {
      $("#ws-status").textContent = "● 已断开";
      $("#ws-status").className = "disconnected";
      setTimeout(connect, 3000);
    };
    ws.onmessage = (e) => {
      try {
        const state = JSON.parse(e.data);
        lastState = state;
        render(state);
      } catch (err) {
        console.error("parse error:", err);
      }
    };
  }

  function render(state) {
    $("#game-time").textContent = state.game_time || "—";
    $("#tick-count").textContent = `Tick: ${state.tick}`;

    const grid = $("#location-grid");
    grid.innerHTML = "";
    const locs = state.locations || {};
    const agentMap = {};
    (state.agents || []).forEach((a) => (agentMap[a.agent_id] = a));

    for (const [loc, ids] of Object.entries(locs)) {
      const card = document.createElement("div");
      card.className = "location-card";
      const agentTags = (ids || [])
        .map((id) => {
          const a = agentMap[id];
          const name = a ? a.agent_id : id;
          const emotion = a ? a.emotion : "";
          return `<span class="agent-tag">${name}${emotion ? " · " + emotion : ""}</span>`;
        })
        .join("");
      card.innerHTML = `<h3>${loc}</h3><div class="agents">${agentTags || "<em>无人</em>"}</div>`;
      grid.appendChild(card);
    }

    if (state.events && state.events.length) {
      state.events.forEach((ev) => {
        events.unshift({ time: state.game_time, text: ev });
      });
      if (events.length > 200) events = events.slice(0, 200);
    }

    (state.agents || []).forEach((a) => {
      if (a.inner_thought) {
        events.unshift({ time: state.game_time, text: `💭 ${a.agent_id}（内心）：${a.inner_thought}`, cls: "thought" });
      }
      if (a.dialogue) {
        events.unshift({ time: state.game_time, text: `💬 ${a.agent_id}：「${a.dialogue}」`, cls: "dialogue" });
      }
    });

    const list = $("#event-list");
    list.innerHTML = events
      .slice(0, 100)
      .map((ev) =>
        `<div class="event-item${ev.cls ? " " + ev.cls : ""}">` +
        `<span class="time">[${ev.time}]</span>${ev.text}</div>`
      )
      .join("");

    // 更新玩家面板中的 NPC 下拉
    if (playerJoined) updateTalkTargets(state);
  }

  // 更新对话目标下拉（当前地点的 NPC）
  function updateTalkTargets(state) {
    const sel = $("#talk-target");
    sel.innerHTML = "";
    if (!state.locations) return;
    // 找到玩家所在地点
    for (const [loc, ids] of Object.entries(state.locations)) {
      if (ids.includes("player")) {
        ids.filter((id) => id !== "player").forEach((id) => {
          sel.add(new Option(id, id));
        });
        // 更新移动下拉为相邻地点
        if (worldConfig) {
          const locCfg = worldConfig.locations.find((l) => l.name === loc);
          if (locCfg) {
            const moveSel = $("#move-target");
            moveSel.innerHTML = "";
            (locCfg.connected || []).forEach((c) => moveSel.add(new Option(c, c)));
          }
        }
        break;
      }
    }
  }

  function showPlayerUI(ps) {
    $("#player-not-joined").style.display = "none";
    $("#join-form").style.display = "none";
    $("#player-joined").style.display = "block";
    $("#player-info").innerHTML =
      `<strong>${ps.player_id}</strong> · 📍 ${ps.location} · ${ps.action}`;
  }

  // 全局函数供 HTML onclick 调用
  window.showJoinForm = () => {
    $("#player-not-joined").style.display = "none";
    $("#join-form").style.display = "block";
  };
  window.hideJoinForm = () => {
    $("#join-form").style.display = "none";
    $("#player-not-joined").style.display = "block";
  };

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
    await fetch("/api/player/join", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    playerJoined = true;
    showPlayerUI({ player_id: body.name, location: body.init_location, action: "刚刚到达" });
    $("#waiting-banner").style.display = "block";
    $("#action-panel").style.display = "block";
  };

  window.doLeave = async () => {
    await fetch("/api/player/leave", { method: "DELETE" });
    playerJoined = false;
    $("#player-joined").style.display = "none";
    $("#waiting-banner").style.display = "none";
    $("#action-panel").style.display = "none";
    $("#player-not-joined").style.display = "block";
  };

  window.switchTab = (tab) => {
    document.querySelectorAll(".tab").forEach((t) => t.classList.remove("active"));
    document.querySelector(`.tab[data-tab="${tab}"]`).classList.add("active");
    document.querySelectorAll(".tab-content").forEach((c) => (c.style.display = "none"));
    $(`#tab-${tab}`).style.display = "block";
  };

  window.submitAction = async (type) => {
    const action = { type };
    if (type === "move") action.location = $("#move-target").value;
    if (type === "talk") {
      action.target = $("#talk-target").value;
      action.content = $("#talk-content").value;
    }
    if (type === "act") action.content = $("#act-content").value;

    await fetch("/api/player/action", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(action),
    });
    $("#waiting-banner").style.display = "none";
    // 清空输入
    $("#talk-content").value = "";
    $("#act-content").value = "";
  };

  // 轮询玩家状态以检测 waiting_for_player
  setInterval(async () => {
    if (!playerJoined) return;
    try {
      const res = await fetch("/api/player/state");
      const ps = await res.json();
      if (ps.player_id) {
        showPlayerUI(ps);
        $("#waiting-banner").style.display = "block";
        $("#action-panel").style.display = "block";
      }
    } catch (e) { /* ignore */ }
  }, 2000);

  loadWorldConfig();
  checkPlayerState();
  connect();
})();
