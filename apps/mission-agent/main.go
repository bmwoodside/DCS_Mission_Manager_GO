package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	gatewayURL := getEnv("GATEWAY_URL", "ws://127.0.0.1:8000/agent/ws")
	secret := os.Getenv("AGENT_SECRET")

	destDir := getEnv("MISSION_DIR", "./incoming")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		log.Fatalf("failed to create mission dir %s: %v", destDir, err)
	}

	header := http.Header{}
	if secret != "" {
		header.Set("Authorization", "Bearer "+secret)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Printf("agent starting, target gateway: %s", gatewayURL)
	log.Printf("missions will be stored under: %s", destDir)

	for {
		if ctx.Err() != nil {
			log.Println("agent shutting down (ctx canceled before connect)")
			return
		}

		log.Printf("connecting to gateway: %s", gatewayURL)
		conn, _, err := websocket.DefaultDialer.Dial(gatewayURL, header)
		if err != nil {
			log.Printf("failed to connect to gateway: %v", err)

			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				log.Println("agent shutting down (ctx canceled during backoff)")
				return
			}
		}
		log.Println("connected to gateway.")

		runConnection(ctx, conn, destDir)

		log.Println("disconnected from gateway, will retry...")

		select {
		case <-time.After(5 * time.Second):
			// loop continues and attempts another dial
		case <-ctx.Done():
			log.Println("agent shutting down (ctx canceled after disconnect)")
			return
		}
	}
}

type controlMessage struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

type uploadState struct {
	id          string
	filename    string
	expectedSha string
	size        int64

	tmpPath string
	f       *os.File
	hasher  hash.Hash
	bytes   int64
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == string(os.PathSeparator) {
		return "upload.bin"
	}
	return name
}

func runConnection(ctx context.Context, conn *websocket.Conn, destDir string) {
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
					log.Printf("write ping error: %v", err)
					return
				}
			}
		}
	}()

	var current *uploadState

	//message reader if messaged by server - blocks until error or ctx.done
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("read error (likely disconnect): %v", err)
			// cleanup  upload-in-progress
			if current != nil {
				_ = current.f.Close()
				_ = os.Remove(current.tmpPath)
				current = nil
			}
			return
		}

		switch msgType {
		case websocket.TextMessage:
			var ctrl controlMessage
			if err := json.Unmarshal(data, &ctrl); err != nil {
				log.Printf("failed to parse control message: %v", err)
				continue
			}
			switch ctrl.Type {
			case "begin_upload":
				if current != nil {
					log.Printf("received begin_upload while another upload is active (id=%s)", current.id)
					continue
				}
				filename := sanitizeFilename(ctrl.Filename)
				tmpFile, err := os.CreateTemp("", "dcs-upload-*")
				if err != nil {
					log.Printf("failed to create temp file: %v", err)
					continue
				}
				current = &uploadState{
					id:          ctrl.ID,
					filename:    filename,
					expectedSha: strings.ToLower(ctrl.SHA256),
					size:        ctrl.Size,
					tmpPath:     tmpFile.Name(),
					f:           tmpFile,
					hasher:      sha256.New(),
				}
				log.Printf("begin upload id=%s filename=%s size=%d", current.id, current.filename, current.size)

			case "end_upload":
				if current == nil {
					log.Printf("received end_upload with no active upload (id=%s)", ctrl.ID)
					continue
				}
				if ctrl.ID != "" && ctrl.ID != current.id {
					log.Printf("end_upload id mismatch: got=%s expected=%s", ctrl.ID, current.id)
					continue
				}
				if err := current.f.Close(); err != nil {
					log.Printf("failed to close tempp file: %v", err)
					_ = os.Remove(current.tmpPath)
					current = nil
					continue
				}
				sum := current.hasher.Sum(nil)
				gotSha := strings.ToLower(hex.EncodeToString(sum))

				if current.expectedSha != "" && current.expectedSha != gotSha {
					log.Printf("sha256 mismatch for upload id=%s filename=%s expected=%s got=%s", current.id, current.filename, current.expectedSha, gotSha)
					_ = os.Remove(current.tmpPath)
					current = nil
					continue
				}

				dstPath := filepath.Join(destDir, current.filename)
				if err := os.Rename(current.tmpPath, dstPath); err != nil {
					log.Printf("failed to move upload to final location: %v", err)
					_ = os.Remove(current.tmpPath)
					current = nil
					continue
				}

				log.Printf("upload complete id=%s filename=%s bytes=%d sha256=%s -> %s", current.id, current.filename, current.bytes, gotSha, dstPath)

				current = nil

			default:
				log.Printf("unknown control message type: %s", ctrl.Type)
			}

		case websocket.BinaryMessage:
			if current == nil {
				log.Printf("received binary chunk with no active upload")
				continue
			}
			if len(data) == 0 {
				continue
			}
			n, err := current.f.Write(data)
			if err != nil {
				log.Printf("failed to write to temp file: %v", err)
				_ = current.f.Close()
				_ = os.Remove(current.tmpPath)
				current = nil
				continue
			}
			if _, err := current.hasher.Write(data[:n]); err != nil {
				log.Printf("failed to update hash: %v", err)
				_ = current.f.Close()
				_ = os.Remove(current.tmpPath)
				current = nil
				continue
			}
			current.bytes += int64(n)

		default:
			// ignore other message types (ping/pong handled internally by gorilla)
		}
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
