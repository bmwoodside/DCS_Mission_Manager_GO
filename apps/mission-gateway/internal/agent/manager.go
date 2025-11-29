/////////////////////////////////
// handler for agent/websocket //
/////////////////////////////////

package agent

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Manager struct {
	secret string

	mu     sync.RWMutex
	conn   *websocket.Conn
	online bool
}

func NewManager(secret string) *Manager {
	return &Manager{
		secret: secret,
	}
}

func (m *Manager) Online() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.online && m.conn != nil
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.secret != "" {
		auth := r.Header.Get("Authorization")
		expected := "Bearer" + m.secret
		if auth != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("agent ws upgrade error: %v", err)
		return
	}

	log.Printf("agent connected from %s", r.RemoteAddr)

	m.mu.Lock()
	if m.conn != nil {
		_ = m.conn.Close()
	}
	m.conn = conn
	m.online = true
	m.mu.Unlock()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			log.Printf("agent ws read error (disconnect): %v", err)
			break
		}
	}

	m.mu.Lock()
	if m.conn == conn {
		m.conn = nil
		m.online = false
	}
	m.mu.Unlock()

	log.Printf("agent disconnected at %s", time.Now().Format(time.RFC3339))
}
