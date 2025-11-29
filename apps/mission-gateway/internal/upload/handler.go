// //////////////////////////////////////////
// Current client expected request format: //
// Method: POST /api/v1/upload             //
// HEADERS:                                //
// X-File-Name: mission.miz                //
// X-File-Sha256: <hex-hash>               //
// Content-Length: <size>                  //
// //////////////////////////////////////////
package upload

import (
	"dcs-mission-manager/mission-gateway/internal/agent"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	Agent   *agent.Manager
	MaxSize int64 // 0 = unlimited
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.Agent.Online() {
		http.Error(w, "agent offline", http.StatusServiceUnavailable)
		return
	}

	filename := strings.TrimSpace(r.Header.Get("X-File-Name"))
	if filename == "" {
		http.Error(w, "missing X-File-Name header", http.StatusBadRequest)
		return
	}
	sha := strings.TrimSpace(r.Header.Get("X-File-Sha256"))

	if h.MaxSize > 0 && r.ContentLength > 0 && r.ContentLength > h.MaxSize {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	// time-based uploadID - TODO: Replace this later
	id := strconv.FormatInt(time.Now().UnixNano(), 10)

	meta := agent.UploadMeta{
		ID:       id,
		Filename: filename,
		Size:     r.ContentLength,
		SHA256:   sha,
	}

	var src io.Reader = r.Body
	if h.MaxSize > 0 {
		src = io.LimitReader(r.Body, h.MaxSize)
	}

	if err := h.Agent.StreamUpload(r.Context(), meta, src); err != nil {
		if errors.Is(err, agent.ErrAgentOffline) {
			http.Error(w, "agent offline", http.StatusServiceUnavailable)
			return
		}
		log.Printf("upload %s failed: %v", id, err)
		http.Error(w, "upload failed", http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
