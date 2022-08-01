package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"oneplay-videostream-browser/internal/encoders"
	"oneplay-videostream-browser/internal/rdisplay"
	"oneplay-videostream-browser/rtc"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

const (
	httpDefaultPort   = "9000"
	defaultStunServer = "turn:13.250.13.83:3478?transport=udp"
)

func main() {

	socketUrl := "ws://oneplay-heroku.herokuapp.com" + "/host"
	// socketUrl := "ws://localhost:8080" + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(socketUrl, nil)
	if err != nil {
		log.Fatal(err)
	}

	// sigStr := "host"
	// sigData := make([]byte, len(sigStr))
	// copied := copy(sigData, sigStr)
	// fmt.Println(copied)

	// err = ws.WriteMessage(1, sigData)
	// if err != nil {
	// 	panic(err)
	// }

	// _, pMsg, err := ws.ReadMessage()
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Println(pMsg)

	//httpPort := flag.String("http.port", httpDefaultPort, "HTTP listen port")
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
	fmt.Println(*stunServer, video, enc)
	webrtc = rtc.NewRemoteScreenService(*stunServer, video, enc)

	//mux := http.NewServeMux()

	// // Endpoint to create a new speech to text session
	// mux.Handle("/api/", http.StripPrefix("/api", api.MakeHandler(webrtc, video)))
	// fmt.Println("Finding Panic Error : 4")
	reader(ws, webrtc, video)
	// fmt.Println("Finding Panic Error : 5")

	// // Serve static assets
	// mux.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir("./web"))))
	// mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
	// 	if r.URL.Path != "/" {
	// 		w.WriteHeader(http.StatusNotFound)
	// 		return
	// 	}
	// 	http.ServeFile(w, r, "./web/index.html")
	// })

	errors := make(chan error, 2)
	// go func() {
	// 	log.Printf("Starting signaling server on port %s", *httpPort)
	// 	errors <- http.ListenAndServe(fmt.Sprintf(":%s", *httpPort), mux)
	// }()

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
	ICE    webrtc.ICECandidateInit
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

var ICEinfo webrtc.ICECandidateInit
var peer rtc.RemoteScreenConnection

var flag_sdp bool
var flag_ice bool

func reader(conn *websocket.Conn, rtcService rtc.Service, display rdisplay.Service) {
	flag_sdp = false
	flag_ice = false
	fmt.Println("Finding Panic Error : 1")
	for {
		// read in a message

		messageType, p, err := conn.ReadMessage()

		if err != nil {
			log.Println(err)
			return
		}

		var msg *Message

		err = json.Unmarshal(p, &msg)
		if err != nil {
			fmt.Println("Error occurred in Unmarshal")
			log.Fatal(err)
			return
		}

		if msg.WSType == "Screen" {

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

			conn.WriteMessage(messageType, payload)
		} else if msg.WSType == "SDP" {

			var err error
			peer, err = rtcService.CreateRemoteScreenConnection(msg.Screen, 60)
			if err != nil {
				log.Fatal(err)
				return
			}

			flag_sdp = true

			peer.ProcessOffer(msg.SDP, conn, messageType)

			if flag_ice {
				peer.ProcessICE(ICEinfo)
			}
		} else if msg.WSType == "ICE" {

			fmt.Println("ICE Confirmation")

			fmt.Println(msg.ICE)
			ICEinfo = msg.ICE

			flag_ice = true

			if flag_sdp {
				peer.ProcessICE(ICEinfo)
			}
		}
	}
}
