package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"oneplay-videostream-browser/internal/api"
	"oneplay-videostream-browser/internal/encoders"
	"oneplay-videostream-browser/internal/rdisplay"
	"oneplay-videostream-browser/rtc"

	"github.com/gorilla/websocket"
)

const (
	httpDefaultPort   = "9000"
	defaultStunServer = "stun:stun.l.google.com:19302"
)

func main() {

	socketUrl := "ws://localhost:8080" + "/host"
	ws, _, err := websocket.DefaultDialer.Dial(socketUrl, nil)
	if err != nil {
		log.Fatal(err)
	}

	httpPort := flag.String("http.port", httpDefaultPort, "HTTP listen port")
	stunServer := flag.String("stun.server", defaultStunServer, "STUN server URL (stun:)")
	flag.Parse()

	var video rdisplay.Service
	video, err = rdisplay.NewVideoProvider()
	if err != nil {
		log.Fatalf("Can't init video: %v", err)
	}
	_, err = video.Screens()
	if err != nil {
		log.Fatalf("Can't get screens: %v", err)
	}

	var enc encoders.Service = &encoders.EncoderService{}
	if err != nil {
		log.Fatalf("Can't create encoder service: %v", err)
	}

	var webrtc rtc.Service
	webrtc = rtc.NewRemoteScreenService(*stunServer, video, enc)

	mux := http.NewServeMux()

	// Endpoint to create a new speech to text session
	mux.Handle("/api/", http.StripPrefix("/api", api.MakeHandler(webrtc, video)))
	fmt.Println("Finding Panic Error : 4")
	reader(ws, webrtc, video)
	fmt.Println("Finding Panic Error : 5")

	// Serve static assets
	mux.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir("./web"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, "./web/index.html")
	})

	errors := make(chan error, 2)
	go func() {
		log.Printf("Starting signaling server on port %s", *httpPort)
		errors <- http.ListenAndServe(fmt.Sprintf(":%s", *httpPort), mux)
	}()

	go func() {
		interrupt := make(chan os.Signal)
		signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
		errors <- fmt.Errorf("Received %v signal", <-interrupt)
	}()

	err = <-errors
	log.Printf("%s, exiting.", err)
}

type Message struct {
	WSType string
	Screen int
	SDP    string
	// wsSDP  *webrtc.SessionDescription `json:"wsSDP"`
}

type screenPayload struct {
	Index int `json:"index"`
}

type screensResponse struct {
	WSType string
	Screen []screenPayload
	SDP    string
}

type newSessionResponse struct {
	WSType string
	Answer string
}

func reader(conn *websocket.Conn, webrtc rtc.Service, display rdisplay.Service) {
	fmt.Println("Finding Panic Error : 1")
	for {
		// read in a message

		messageType, p, err := conn.ReadMessage()

		if err != nil {
			log.Println(err)
			return
		}

		fmt.Println("Message : ", string(p))

		var msg *Message
		err = json.Unmarshal(p, &msg)
		if err != nil {
			fmt.Println("Error occurred in Unmarshal")
			log.Fatal(err)
			return
		}
		fmt.Println("Finding Panic Error : 3")

		fmt.Println(msg.WSType)
		fmt.Println(msg.SDP)
		fmt.Println(msg.Screen)

		if msg.WSType == "Screen" {
			fmt.Println("In Screen Progress")
			screens, err := display.Screens()

			if err != nil {
				log.Fatal(err)
				return
			}

			screensPayload := make([]screenPayload, len(screens))

			for i, s := range screens {
				screensPayload[i].Index = s.Index
			}
			payload, err := json.Marshal(screensResponse{
				WSType: "Screen",
				Screen: screensPayload,
				SDP:    "",
			})
			if err != nil {
				log.Fatal(err)
				return
			}

			// conn.WriteMessage(messageType, payload)
			fmt.Println("Before sending to Client")
			conn.WriteMessage(messageType, payload)
		} else if msg.WSType == "SDP" {
			fmt.Println("In SDP Progress")
			peer, err := webrtc.CreateRemoteScreenConnection(msg.Screen, 60)
			if err != nil {
				log.Fatal(err)
				return
			}

			answer, err := peer.ProcessOffer(msg.SDP)

			if err != nil {
				log.Fatal(err)
				return
			}

			payload, err := json.Marshal(newSessionResponse{
				WSType: "SDP",
				Answer: answer,
			})
			if err != nil {
				log.Fatal(err)
				return
			}

			fmt.Println("Before sending to client")
			conn.WriteMessage(messageType, payload)
		}

		// var msg *Message
		// json.Unmarshal(p, &msg)
		// if msg.wsType == "To_Client" {
		// 	// ! from host to client
		// 	if err := conn.WriteMessage(messageType, p); err != nil {
		// 		log.Println(err)
		// 		return
		// 	}
		// } else if msg.wsType == "To_Server" {
		// 	// ! from client to host
		// 	if len(hostConnection) != 0 {
		// 		if err := hostConnection[0].WriteMessage(messageType, p); err != nil {
		// 			log.Println(err)
		// 			return
		// 		}
		// 	}
		// }
	}
}
