/////////////////////////////////
// handler for agent/websocket //
/////////////////////////////////

package agent

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Manager struct {
	secret string

	mu       sync.RWMutex
	conn     *websocket.Conn
	online   bool
	uploadMu sync.Mutex
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

var ErrAgentOffline = errors.New("agent offline")

type UploadMeta struct {
	ID       string
	Filename string
	Size     int64
	SHA256   string
}

type controlMessage struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
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

func (m *Manager) StreamUpload(ctx context.Context, meta UploadMeta, src io.Reader) error {
	m.uploadMu.Lock()
	defer m.uploadMu.Unlock()

	m.mu.RLock()
	conn := m.conn
	online := m.online
	m.mu.RUnlock()

	if !online || conn == nil {
		return ErrAgentOffline
	}

	begin := controlMessage{
		Type:     "begin_upload",
		ID:       meta.ID,
		Filename: meta.Filename,
		Size:     meta.Size,
		SHA256:   meta.SHA256,
	}

	if err := conn.WriteJSON(begin); err != nil {
		return err
	}

	buf := make([]byte, 64*1024) //64 KiB chunks
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	end := controlMessage{
		Type: "end_upload",
		ID:   meta.ID,
	}
	if err := conn.WriteJSON(end); err != nil {
		return err
	}

	log.Printf("streamed upload id=%s filename=%s", meta.ID, meta.Filename)
	return nil
}
