// Package server 提供 HTTP API 和 WebSocket 实时推送。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/claw-works/fable/internal/llm"
	"github.com/claw-works/fable/internal/schema"
	"github.com/claw-works/fable/internal/storage"
	"github.com/claw-works/fable/internal/world"
	"github.com/gorilla/websocket"
)

// Server 是 Fable 的 HTTP 服务。
type Server struct {
	world     *world.World
	cfg       schema.Config
	llm       *llm.Client
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]bool
	mu        sync.Mutex
	cancel    context.CancelFunc
	running   bool
	wsCh      chan []byte // 串行化 WebSocket 写入
	interrupt chan struct{} // 玩家操作中断信号
}

// New 创建一个新的 Server。
func New(w *world.World, cfg schema.Config, llmClient *llm.Client) *Server {
	s := &Server{
		world: w,
		cfg:   cfg,
		llm:   llmClient,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients:   make(map[*websocket.Conn]bool),
		wsCh:      make(chan []byte, 64),
		interrupt: make(chan struct{}, 1),
	}
	go s.writeLoop()
	w.OnTick = s.broadcast
	w.OnEvent = s.broadcastEvent
	return s
}

// Run 启动 HTTP 服务。
func (s *Server) Run() error {
	mux := http.NewServeMux()

	// 静态文件
	mux.Handle("/frontend/", http.StripPrefix("/frontend/", http.FileServer(http.Dir("frontend"))))
	mux.Handle("/frontend-pixel/", http.StripPrefix("/frontend-pixel/", http.FileServer(http.Dir("frontend-pixel"))))
	mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir("admin"))))
	mux.Handle("/xui/", http.StripPrefix("/xui/", http.FileServer(http.Dir("xui/dist"))))

	// API
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/tick", s.handleTick)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/run", s.handleRun)
	mux.HandleFunc("/api/config/world", s.handleWorldConfig)
	mux.HandleFunc("/api/config/agents", s.handleAgentsConfig)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/ws", s.handleWS)

	// Player API
	mux.HandleFunc("/api/player/join", s.handlePlayerJoin)
	mux.HandleFunc("/api/player/leave", s.handlePlayerLeave)
	mux.HandleFunc("/api/player/state", s.handlePlayerState)
	mux.HandleFunc("/api/player/action", s.handlePlayerAction)
	mux.HandleFunc("/api/player/interrupt", s.handlePlayerInterrupt)
	mux.HandleFunc("/api/player/autopilot", s.handlePlayerAutopilot)

	// Conversation API
	mux.HandleFunc("/api/conversation/start", s.handleConvStart)
	mux.HandleFunc("/api/conversation/say", s.handleConvSay)
	mux.HandleFunc("/api/conversation/action", s.handleConvAction)
	mux.HandleFunc("/api/conversation/end", s.handleConvEnd)
	mux.HandleFunc("/api/conversation/history", s.handleConvHistory)

	// 查询 API
	mux.HandleFunc("/api/query/agent", s.handleQueryAgent)
	mux.HandleFunc("/api/query/tick", s.handleQueryTick)
	mux.HandleFunc("/api/query/location", s.handleQueryLocation)

	// 游戏管理 API
	mux.HandleFunc("/api/worlds", s.handleListWorlds)
	mux.HandleFunc("/api/saves", s.handleListSaves)
	mux.HandleFunc("/api/new-game", s.handleNewGame)

	// 根路径重定向到观察端
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/frontend/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	addr := fmt.Sprintf(":%d", s.cfg.Server.Port)
	log.Printf("Fable server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

// broadcast 向所有 WebSocket 客户端推送世界状态。
func (s *Server) broadcast(state schema.WorldState) {
	data, err := json.Marshal(state)
	if err != nil {
		log.Printf("broadcast marshal error: %v", err)
		return
	}
	s.sendToClients(data)
}

// broadcastEvent 向所有客户端推送增量事件（NPC 推理完成时立即调用）。
func (s *Server) broadcastEvent(event world.StreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	s.sendToClients(data)
}

// sendToClients 非阻塞地向所有 WebSocket 客户端发送消息。
func (s *Server) sendToClients(data []byte) {
	select {
	case s.wsCh <- data:
	default:
		if s.cfg.DevMode {
			log.Println("[dev] broadcast channel 已满，丢弃消息")
		}
	}
}

// writeLoop 单 goroutine 串行写入所有 WebSocket 客户端，避免并发写 panic。
func (s *Server) writeLoop() {
	for data := range s.wsCh {
		s.mu.Lock()
		clients := make([]*websocket.Conn, 0, len(s.clients))
		for conn := range s.clients {
			clients = append(clients, conn)
		}
		s.mu.Unlock()

		for _, conn := range clients {
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				if s.cfg.DevMode {
					log.Printf("[dev] ws 写入失败，断开连接: %v", err)
				}
				conn.Close()
				s.mu.Lock()
				delete(s.clients, conn)
				s.mu.Unlock()
			}
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	state := s.world.GetState()
	data, _ := json.Marshal(state)
	conn.WriteMessage(websocket.TextMessage, data)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			s.mu.Lock()
			delete(s.clients, conn)
			s.mu.Unlock()
			conn.Close()
			return
		}
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.world.GetState())
}

func (s *Server) handleTick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.DevMode {
		log.Println("[dev] POST /api/tick 收到请求")
	}
	resp := s.world.Tick(r.Context())
	if s.cfg.DevMode {
		log.Printf("[dev] POST /api/tick 完成, tick=%d, agents=%d, waiting=%v",
			resp.State.Tick, len(resp.State.Agents), resp.WaitingForPlayer)
	}
	writeJSON(w, resp)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		if s.cfg.DevMode {
			log.Println("[dev] POST /api/start → already running")
		}
		writeJSON(w, map[string]string{"status": "already running"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	if s.cfg.DevMode {
		log.Printf("[dev] POST /api/start → 启动自动运行, interval=%ds", s.cfg.Simulation.TickInterval)
	}
	s.world.SetMode(schema.ModeRunning)

	go func() {
		pauseTimeout := 30 * time.Second
		pauseStart := time.Time{}
		tickWait := 20 * time.Second // 每 tick 后等待 20s，给玩家操作时间
		for {
			select {
			case <-ctx.Done():
				if s.cfg.DevMode {
					log.Println("[dev] 自动运行已停止")
				}
				s.world.SetMode(schema.ModeIdle)
				return
			default:
			}

			mode := s.world.GetMode()
			if mode == schema.ModePaused {
				if pauseStart.IsZero() {
					pauseStart = time.Now()
				}
				if time.Since(pauseStart) > pauseTimeout {
					if s.cfg.DevMode {
						log.Println("[dev] 暂停超时 30s，自动恢复运行")
					}
					s.world.SetMode(schema.ModeRunning)
					pauseStart = time.Time{}
				}
				time.Sleep(500 * time.Millisecond)
				continue
			}
			pauseStart = time.Time{}

			if s.cfg.DevMode {
				log.Println("[dev] 自动运行: 触发 tick")
			}
			s.world.Tick(ctx)

			// 等待 20s，期间可被中断
			select {
			case <-ctx.Done():
				return
			case <-s.interrupt:
				log.Println("[server] 玩家中断，暂停自动运行")
				s.world.SetMode(schema.ModePaused)
				// 等待恢复
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					if s.world.GetMode() != schema.ModePaused {
						break
					}
					time.Sleep(300 * time.Millisecond)
				}
			case <-time.After(tickWait):
			}
		}
	}()
	writeJSON(w, map[string]string{"status": "started"})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.DevMode {
		log.Println("[dev] POST /api/stop")
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.running = false
	}
	s.mu.Unlock()
	writeJSON(w, map[string]string{"status": "stopped"})
}

// POST /api/run {"ticks": 10} — 运行指定步数后自动停止
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Ticks int `json:"ticks"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Ticks <= 0 || body.Ticks > 200 {
		body.Ticks = 10
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeJSON(w, map[string]string{"status": "already running"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	s.world.SetMode(schema.ModeRunning)
	tickWait := 20 * time.Second
	target := body.Ticks

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			s.world.SetMode(schema.ModeIdle)
		}()
		for i := 0; i < target; i++ {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if s.world.GetMode() == schema.ModePaused {
				// 等待恢复或取消
				for s.world.GetMode() == schema.ModePaused {
					select {
					case <-ctx.Done():
						return
					default:
						time.Sleep(300 * time.Millisecond)
					}
				}
			}
			s.world.Tick(ctx)
			if i < target-1 { // 最后一步不等待
				select {
				case <-ctx.Done():
					return
				case <-s.interrupt:
					s.world.SetMode(schema.ModePaused)
					for s.world.GetMode() == schema.ModePaused {
						select {
						case <-ctx.Done():
							return
						default:
							time.Sleep(300 * time.Millisecond)
						}
					}
				case <-time.After(tickWait):
				}
			}
		}
		log.Printf("[server] 运行 %d 步完成", target)
	}()

	writeJSON(w, map[string]any{"status": "running", "ticks": target})
}

// POST /api/player/interrupt — 玩家中断自动运行
func (s *Server) handlePlayerInterrupt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	select {
	case s.interrupt <- struct{}{}:
		writeJSON(w, map[string]string{"status": "interrupted"})
	default:
		writeJSON(w, map[string]string{"status": "already_paused"})
	}
}

// POST /api/player/autopilot {"enabled": true/false}
func (s *Server) handlePlayerAutopilot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	s.world.Autopilot = body.Enabled
	writeJSON(w, map[string]any{"autopilot": body.Enabled})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"world_id": s.world.WorldID, "save_name": s.world.SaveName})
}

func (s *Server) handleWorldConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.world.Config)
}

func (s *Server) handleAgentsConfig(w http.ResponseWriter, r *http.Request) {
	configs := make([]schema.AgentConfig, 0, len(s.world.Agents))
	for _, a := range s.world.Agents {
		configs = append(configs, a.Config)
	}
	writeJSON(w, configs)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	states, err := storage.QueryRecentTicks(limit)
	if err != nil {
		// fallback 到内存
		writeJSON(w, s.world.History)
		return
	}
	writeJSON(w, states)
}

// handlePlayerJoin 创建玩家角色加入模拟。
func (s *Server) handlePlayerJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cfg schema.PlayerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.ID == "" {
		cfg.ID = "player"
	}
	if cfg.InitLocation == "" {
		cfg.InitLocation = "茶馆"
	}
	s.world.JoinPlayer(cfg)
	// 玩家加入后自动停止自动运行，切换到手动模式
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.running = false
	}
	s.world.SetMode(schema.ModeIdle)
	writeJSON(w, map[string]string{"status": "joined", "player_id": cfg.ID, "name": cfg.Name})
}

// handlePlayerLeave 玩家离开模拟。
func (s *Server) handlePlayerLeave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.world.LeavePlayer()
	writeJSON(w, map[string]string{"status": "left"})
}

// handlePlayerState 获取玩家当前状态。
func (s *Server) handlePlayerState(w http.ResponseWriter, r *http.Request) {
	ps := s.world.PlayerState
	if ps == nil {
		writeJSON(w, map[string]string{"status": "not_joined"})
		return
	}
	writeJSON(w, ps)
}

// handlePlayerAction 提交玩家行动。
func (s *Server) handlePlayerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action schema.PlayerAction
	if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.world.SubmitAction(action)
	// 玩家操作自动中断自动运行等待
	select {
	case s.interrupt <- struct{}{}:
	default:
	}
	writeJSON(w, map[string]string{"status": "submitted"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleConvStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		NPCID string `json:"npc_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	playerID := ""
	if s.world.Player != nil {
		playerID = s.world.Player.ID
	}
	if err := s.world.StartConversation(playerID, body.NPCID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "started", "npc_id": body.NPCID})
}

func (s *Server) handleConvSay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	s.world.AddConversationTurn("player", body.Content)
	// NPC 用 LLM 回复
	reply, err := s.world.ConversationReply(r.Context())
	if err != nil {
		writeJSON(w, map[string]any{"player": body.Content, "reply": "", "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"player": body.Content, "reply": reply})
}

func (s *Server) handleConvAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	// 对话中行动消耗 Tick
	s.world.AddConversationTurn("player", "[行动] "+body.Content)
	resp := s.world.Tick(r.Context())
	writeJSON(w, resp)
}

func (s *Server) handleConvEnd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	summary := s.world.EndConversation()
	writeJSON(w, map[string]string{"status": "ended", "summary": summary})
}

func (s *Server) handleConvHistory(w http.ResponseWriter, r *http.Request) {
	conv := s.world.Conversation
	if conv == nil {
		writeJSON(w, map[string]any{"active": false, "history": []any{}})
		return
	}
	writeJSON(w, conv)
}

// GET /api/query/agent?id=lao_chen&limit=50
func (s *Server) handleQueryAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("id")
	if agentID == "" {
		http.Error(w, "missing id", 400)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	results, err := storage.QueryAgentHistory(agentID, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, results)
}

// GET /api/query/tick?tick=5
func (s *Server) handleQueryTick(w http.ResponseWriter, r *http.Request) {
	var tick int
	fmt.Sscanf(r.URL.Query().Get("tick"), "%d", &tick)
	state, err := storage.QueryTickState(tick)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, state)
}

// GET /api/query/location?name=茶馆&tick=5
func (s *Server) handleQueryLocation(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	var tick int
	fmt.Sscanf(r.URL.Query().Get("tick"), "%d", &tick)
	results, err := storage.QueryAgentsByLocation(name, tick)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, results)
}

// GET /api/worlds — 列出可用世界
func (s *Server) handleListWorlds(w http.ResponseWriter, r *http.Request) {
	dirs, err := storage.ListWorldDirs()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, dirs)
}

// GET /api/saves?world=qingshui-town — 列出某世界的存档
func (s *Server) handleListSaves(w http.ResponseWriter, r *http.Request) {
	worldID := r.URL.Query().Get("world")
	if worldID == "" {
		worldID = s.world.WorldID
	}
	saves, err := storage.ListSaves(worldID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, saves)
}

// POST /api/new-game {world_id, save_name}
func (s *Server) handleNewGame(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorldID  string `json:"world_id"`
		SaveName string `json:"save_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if req.WorldID == "" || req.SaveName == "" {
		http.Error(w, "world_id and save_name required", 400)
		return
	}

	// 停止当前自动运行
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.running = false
	}

	// 找到世界目录
	worldDir := filepath.Join(storage.WorldsDir(), req.WorldID)
	if _, err := os.Stat(worldDir); os.IsNotExist(err) {
		// 尝试项目内置目录
		worldDir = filepath.Join("worlds", req.WorldID)
	}

	// 切换 DB
	if err := storage.InitDB(req.WorldID, req.SaveName); err != nil {
		http.Error(w, "init db: "+err.Error(), 500)
		return
	}

	// 重新加载世界
	newWorld, err := world.Load(worldDir, req.SaveName, s.llm)
	if err != nil {
		http.Error(w, "load world: "+err.Error(), 500)
		return
	}
	newWorld.OnTick = s.broadcast
	newWorld.OnEvent = s.broadcastEvent
	s.world = newWorld

	// 记住会话
	storage.SaveLastSession(schema.LastSession{WorldID: req.WorldID, SaveName: req.SaveName})

	writeJSON(w, map[string]any{
		"world_id":  req.WorldID,
		"save_name": req.SaveName,
		"tick":      newWorld.GetState().Tick,
	})
}
