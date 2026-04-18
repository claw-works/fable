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
	cancel   context.CancelFunc // 用于停止自动运行
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
	// 注册 Tick 回调，推送给所有 WebSocket 客户端
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

	// 发送当前状态
	state := s.world.GetState()
	data, _ := json.Marshal(state)
	conn.WriteMessage(websocket.TextMessage, data)

	// 保持连接，读取客户端消息（忽略）
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
	state := s.world.Tick(r.Context())
	writeJSON(w, state)
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
