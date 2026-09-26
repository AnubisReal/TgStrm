package httpapi

import (
	"context"
	"log"
	"net/http"
	"time"
)

func (a *API) groupVideoScan(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	groupID := r.PathValue("groupID")
	if r.Method == http.MethodGet {
		status, err := service.GroupVideoScanStatus(r.Context(), groupID)
		if err != nil {
			fail(w, http.StatusInternalServerError, "scan_status_failed", "No se pudo consultar el escaneo.")
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

	a.groupScanMu.Lock()
	if a.groupScans == nil {
		a.groupScans = make(map[string]bool)
	}
	if a.groupScans[groupID] {
		a.groupScanMu.Unlock()
		status, _ := service.GroupVideoScanStatus(r.Context(), groupID)
		writeJSON(w, http.StatusAccepted, status)
		return
	}
	a.groupScans[groupID] = true
	a.groupScanMu.Unlock()

	go func() {
		defer func() {
			a.groupScanMu.Lock()
			delete(a.groupScans, groupID)
			a.groupScanMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		if err := service.ScanGroupVideos(ctx, groupID); err != nil {
			log.Printf("scan group videos group=%s: %v", groupID, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "running"})
}
