package main

import (
	"context"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Printf("agent starting, target gateway: %s", gatewayURL)

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

		runConnection(ctx, conn)

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

func runConnection(ctx context.Context, conn *websocket.Conn) {
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

	//message reader if messaged by server - blocks until error or ctx.done
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("read error (likely disconnect): %v", err)
			return
		}
		log.Printf("received from gateway: %s", string(msg))
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
