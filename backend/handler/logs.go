package handler

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"pgaio/model"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type LogHandler struct {
	logPath string
}

func NewLogHandler(logPath string) *LogHandler {
	return &LogHandler{logPath: logPath}
}

// readLastN reads the last N lines from the log file.
func (h *LogHandler) readLastN(n int) []string {
	f, err := os.Open(h.logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// GetRecentLogs returns the last N lines as JSON.
func (h *LogHandler) GetRecentLogs(w http.ResponseWriter, r *http.Request) {
	nStr := r.URL.Query().Get("n")
	n := 200
	if nStr != "" {
		if v, err := strconv.Atoi(nStr); err == nil && v > 0 && v <= 2000 {
			n = v
		}
	}
	lines := h.readLastN(n)
	if lines == nil {
		lines = []string{}
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: lines})
}

// StreamLogs streams log lines via WebSocket using polling.
func (h *LogHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[logs-ws] accept error: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	log.Println("[logs-ws] client connected")

	ctx := r.Context()
	var lastSize int64

	// Get initial file size
	if info, err := os.Stat(h.logPath); err == nil {
		lastSize = info.Size()
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[logs-ws] client disconnected")
			return
		case <-ticker.C:
			info, err := os.Stat(h.logPath)
			if err != nil || info.Size() <= lastSize {
				continue
			}

			// Read new content
			f, err := os.Open(h.logPath)
			if err != nil {
				continue
			}

			f.Seek(lastSize, 0)
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 64*1024), 64*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					continue
				}
				if err := wsjson.Write(ctx, conn, map[string]string{"line": line}); err != nil {
					f.Close()
					return
				}
			}
			lastSize = info.Size()
			f.Close()
		}
	}
}
