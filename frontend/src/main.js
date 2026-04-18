// Fable 观察端 - WebSocket 实时接收 WorldState 并渲染
(function () {
  const $ = (sel) => document.querySelector(sel);
  let events = [];

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
        render(state);
      } catch (err) {
        console.error("parse error:", err);
      }
    };
  }

  function render(state) {
    $("#game-time").textContent = state.game_time || "—";
    $("#tick-count").textContent = `Tick: ${state.tick}`;

    // 渲染地点
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

    // 渲染事件
    if (state.events && state.events.length) {
      state.events.forEach((ev) => {
        events.unshift({ time: state.game_time, text: ev });
      });
      if (events.length > 200) events = events.slice(0, 200);
    }

    // 渲染 Agent 详情到事件流
    (state.agents || []).forEach((a) => {
      if (a.inner_thought) {
        events.unshift({
          time: state.game_time,
          text: `💭 ${a.agent_id}（内心）：${a.inner_thought}`,
          cls: "thought",
        });
      }
      if (a.dialogue) {
        events.unshift({
          time: state.game_time,
          text: `💬 ${a.agent_id}：「${a.dialogue}」`,
          cls: "dialogue",
        });
      }
    });

    const list = $("#event-list");
    list.innerHTML = events
      .slice(0, 100)
      .map(
        (ev) =>
          `<div class="event-item${ev.cls ? " " + ev.cls : ""}">` +
          `<span class="time">[${ev.time}]</span>${ev.text}</div>`
      )
      .join("");
  }

  connect();
})();
