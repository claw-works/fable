// Fable 管理端
const $ = (sel) => document.querySelector(sel);

// API 调用
async function api(path, method = "GET") {
  try {
    const resp = await fetch(path, { method });
    const data = await resp.json();
    if (path === "/api/tick" || path === "/api/start" || path === "/api/stop") {
      refreshState();
    }
    return data;
  } catch (err) {
    console.error("api error:", err);
  }
}

// WebSocket 连接
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
      renderState(state);
    } catch (err) {
      console.error("parse error:", err);
    }
  };
}

function renderState(state) {
  $("#sim-info").textContent = `Tick: ${state.tick} | 时间: ${state.game_time || "—"}`;
}

async function refreshState() {
  const state = await api("/api/state");
  if (state) renderState(state);
}

// 加载世界配置
async function loadWorldConfig() {
  const data = await api("/api/config/world");
  if (data) {
    $("#world-config").textContent = JSON.stringify(data, null, 2);
  }
}

// 加载 Agent 列表
async function loadAgents() {
  const agents = await api("/api/config/agents");
  if (!agents) return;
  const list = $("#agent-list");
  list.innerHTML = agents
    .map(
      (a) =>
        `<div class="agent-card">
          <h3>${a.name} (${a.id})</h3>
          <p>年龄: ${a.age} | 职业: ${a.occupation}</p>
          <p>性格: ${a.personality}</p>
          <p>初始位置: ${a.init_location}</p>
        </div>`
    )
    .join("");
}

// 加载历史记录
async function loadHistory() {
  const history = await api("/api/history");
  if (!history) return;
  const list = $("#history-list");
  list.innerHTML = history
    .slice(-20)
    .reverse()
    .map(
      (s) =>
        `<div class="history-item">
          <strong>Tick ${s.tick}</strong> [${s.game_time}]
          — ${(s.events || []).slice(0, 3).join("; ")}
        </div>`
    )
    .join("");
}

// 初始化
connect();
loadWorldConfig();
loadAgents();
