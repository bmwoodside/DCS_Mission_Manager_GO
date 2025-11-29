package v1

import (
	"encoding/json"
	"net/http"
)

type HealthResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(HealthResponse{OK: true})
}

type AgentStatusResponse struct {
	Online bool `json:"online"`
}

type AgentStatusProvider interface {
	Online() bool
}

func NewAgentStatusHandler(p AgentStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := AgentStatusResponse{
			Online: p.Online(),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "upload stream-through not implemented yet...", http.StatusNotImplemented) //placeholder _temp
}
