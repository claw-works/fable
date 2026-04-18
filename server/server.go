// Package server 提供 HTTP API 和 WebSocket 实时推送。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/claw-works/fable/internal/schema"
	"github.com/claw-works/fable/internal/world"
	"github.com/gorilla/websocket"
)

// Server 是 Fable 的 HTTP 服务。
type Server struct {
	world    *world.World
	cfg      schema.Config
	upgrader websocket.Upgrader
	clients  map[*websocket.Conn]bool
	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
}

// New 创建一个新的 Server。
func New(w *world.World, cfg schema.Config) *Server {
	s := &Server{
		world: w,
		cfg:   cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients: make(map[*websocket.Conn]bool),
	}
	w.OnTick = s.broadcast
	return s
}

// Run 启动 HTTP 服务。
func (s *Server) Run() error {
	mux := http.NewServeMux()

	// 静态文件
	mux.Handle("/frontend/", http.StripPrefix("/frontend/", http.FileServer(http.Dir("frontend"))))
	mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir("admin"))))

	// API
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/tick", s.handleTick)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/config/world", s.handleWorldConfig)
	mux.HandleFunc("/api/config/agents", s.handleAgentsConfig)
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/ws", s.handleWS)

	// Player API
	mux.HandleFunc("/api/player/join", s.handlePlayerJoin)
	mux.HandleFunc("/api/player/leave", s.handlePlayerLeave)
	mux.HandleFunc("/api/player/state", s.handlePlayerState)
	mux.HandleFunc("/api/player/action", s.handlePlayerAction)

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
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
			delete(s.clients, conn)
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
	resp := s.world.Tick(r.Context())
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
		writeJSON(w, map[string]string{"status": "already running"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Duration(s.cfg.Simulation.TickInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.world.Tick(ctx)
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
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.running = false
	}
	s.mu.Unlock()
	writeJSON(w, map[string]string{"status": "stopped"})
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
	writeJSON(w, s.world.History)
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
	writeJSON(w, map[string]string{"status": "joined", "player_id": cfg.ID})
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
	writeJSON(w, map[string]string{"status": "submitted"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
