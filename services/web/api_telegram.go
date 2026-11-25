package web

import (
	"encoding/json"
	"net/http"
)

// Telegram pairing is deliberately web-only: it grants an external chat
// control of the instance and must require an authenticated admin session.
func (s *Server) handleTelegram(w http.ResponseWriter, r *http.Request) {
	if s.telegram == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Telegram bot is unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, http.StatusOK, s.telegram.Status())
	case http.MethodPost:
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		switch req.Action {
		case "pair":
			code, err := s.telegram.PairCode()
			if err != nil {
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]string{"code": code, "expires_in": "10m"})
		case "disconnect":
			if err := s.telegram.Disconnect(); err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, s.telegram.Status())
		case "test":
			if err := s.telegram.Test(r.Context()); err != nil {
				s.writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]string{"message": "Test message sent"})
		default:
			s.writeError(w, http.StatusBadRequest, "Unknown Telegram action")
		}
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
