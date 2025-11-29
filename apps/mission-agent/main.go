package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	gatewayURL := getEnv("GATEWAY_URL", "ws://127.0.0.1:8000/agent/ws")
	secret := os.Getenv("AGENT_SECRET")

	header := http.Header{}
	if secret != "" {
		header.Set("Authorization", "Bearer "+secret)
	}

	log.Printf("connecting to gateway: %s", gatewayURL)

	conn, _, err := websocket.DefaultDialer.Dial(gatewayURL, header)
	if err != nil {
		log.Fatalf("failed to connect to Gateway: %v", err)
	}
	log.Println("connected to gateway.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	// periodic heartbeat keep-alive
	go func() {
		for {
			time.Sleep(30 * time.Second)
			if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
				log.Printf("write ping error: %v", err)
				return
			}
		}
	}()

	// server-sent message reader
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read error (probably disconnected): %v", err)
				return
			}
			log.Printf("received from gateway: %s", string(msg))
		}
	}()

	<-stop
	log.Println("shutting down agent...")
	_ = conn.Close()
	log.Println("agent stopped")
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
