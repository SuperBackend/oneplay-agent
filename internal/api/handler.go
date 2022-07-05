package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"internal/rdisplay"
	"internal/rtc"
)

func handleError(w http.ResponseWriter, err error) {
	fmt.Printf("Error: %v", err)
	w.WriteHeader(http.StatusInternalServerError)
}

// MakeHandler returns an HTTP handler for the session service
func MakeHandler(webrtc rtc.Service, display rdisplay.Service) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/screens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		screens, err := display.Screens()

		if err != nil {
			handleError(w, err)
			return
		}

		screensPayload := make([]screenPayload, len(screens))

		for i, s := range screens {
			screensPayload[i].Index = s.Index
		}
		payload, err := json.Marshal(screensResponse{
			Screens: screensPayload,
		})
		if err != nil {
			handleError(w, err)
			return
		}

		w.Write(payload)
	})
	return mux
}
