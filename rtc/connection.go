package rtc

import (
	"encoding/json"
	"fmt"
	"image"
	"log"
	"math/rand"
	"strconv"
	"strings"

	"oneplay-videostream-browser/internal/encoders"
	"oneplay-videostream-browser/internal/rdisplay"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pion/sdp"
	"github.com/pion/webrtc/v2"
)

// RemoteScreenPeerConn is a webrtc.PeerConnection wrapper that implements the
// PeerConnection interface
type RemoteScreenPeerConn struct {
	connection *webrtc.PeerConnection
	stunServer string
	track      *webrtc.Track
	streamer   videoStreamer
	grabber    rdisplay.ScreenGrabber
	encService encoders.Service
}

func findBestCodec(sdp *sdp.SessionDescription, encService encoders.Service, h264Profile string) (*webrtc.RTPCodec, encoders.VideoCodec, error) {
	var h264Codec *webrtc.RTPCodec
	var vp8Codec *webrtc.RTPCodec
	for _, md := range sdp.MediaDescriptions {
		for _, format := range md.MediaName.Formats {
			intPt, err := strconv.Atoi(format)
			payloadType := uint8(intPt)
			sdpCodec, err := sdp.GetCodecForPayloadType(payloadType)
			if err != nil {
				return nil, encoders.NoCodec, fmt.Errorf("Can't find codec for %d", payloadType)
			}

			if sdpCodec.Name == webrtc.H264 && h264Codec == nil {
				packetSupport := strings.Contains(sdpCodec.Fmtp, "packetization-mode=1")
				supportsProfile := strings.Contains(sdpCodec.Fmtp, fmt.Sprintf("profile-level-id=%s", h264Profile))
				if packetSupport && supportsProfile {
					h264Codec = webrtc.NewRTPH264Codec(payloadType, sdpCodec.ClockRate)
					h264Codec.SDPFmtpLine = sdpCodec.Fmtp
				}
			} else if sdpCodec.Name == webrtc.VP8 && vp8Codec == nil {
				vp8Codec = webrtc.NewRTPVP8Codec(payloadType, sdpCodec.ClockRate)
				vp8Codec.SDPFmtpLine = sdpCodec.Fmtp
			}
		}
	}
	if vp8Codec != nil && encService.Supports(encoders.VP8Codec) {
		return vp8Codec, encoders.VP8Codec, nil
	}
	if h264Codec != nil && encService.Supports(encoders.H264Codec) {
		return h264Codec, encoders.H264Codec, nil
	}
	return nil, encoders.NoCodec, fmt.Errorf("Couldn't find a matching codec")
}

func newRemoteScreenPeerConn(stunServer string, grabber rdisplay.ScreenGrabber, encService encoders.Service) *RemoteScreenPeerConn {
	return &RemoteScreenPeerConn{
		stunServer: stunServer,
		grabber:    grabber,
		encService: encService,
	}
}

func getTrackDirection(sdp *sdp.SessionDescription) webrtc.RTPTransceiverDirection {
	for _, mediaDesc := range sdp.MediaDescriptions {
		if mediaDesc.MediaName.Media == "video" {
			if _, recvOnly := mediaDesc.Attribute("recvonly"); recvOnly {
				return webrtc.RTPTransceiverDirectionRecvonly
			} else if _, sendRecv := mediaDesc.Attribute("sendrecv"); sendRecv {
				return webrtc.RTPTransceiverDirectionSendrecv
			}
		}
	}
	return webrtc.RTPTransceiverDirectionInactive
}

type iceResponse struct {
	WSType string
	ICE    webrtc.ICECandidateInit
}

var flag bool

type newSessionResponse struct {
	WSType string
	Answer string
}

// ProcessOffer handles the SDP offer coming from the client,
// return the SDP answer that must be passed back to stablish the WebRTC
// connection.
func (p *RemoteScreenPeerConn) ProcessOffer(strOffer string, conn *websocket.Conn, messageType int) {
	flag = false
	sdp := sdp.SessionDescription{}
	err := sdp.Unmarshal(strOffer)
	if err != nil {
		panic(err)
	}

	webrtcCodec, encCodec, err := findBestCodec(&sdp, p.encService, "42e01f")
	if err != nil {
		panic(err)
	}
	mediaEngine := webrtc.MediaEngine{}
	mediaEngine.RegisterCodec(webrtcCodec)

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	pcconf := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{p.stunServer},
			},
		},
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlan,
	}

	peerConn, err := api.NewPeerConnection(pcconf)
	if err != nil {
		panic(err)
	}
	p.connection = peerConn

	track, err := peerConn.NewTrack(
		webrtcCodec.PayloadType,
		uint32(rand.Int31()),
		uuid.New().String(),
		fmt.Sprintf("remote-screen"),
	)

	log.Printf("Using codec %s (%d) %s", webrtcCodec.Name, webrtcCodec.PayloadType, webrtcCodec.SDPFmtpLine)

	direction := getTrackDirection(&sdp)

	if direction == webrtc.RTPTransceiverDirectionSendrecv {
		_, err = peerConn.AddTrack(track)
	} else if direction == webrtc.RTPTransceiverDirectionRecvonly {
		_, err = peerConn.AddTransceiverFromTrack(track, webrtc.RtpTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendonly,
		})
	} else {
		fmt.Printf("Unsupported transceiver direction")
		return
	}

	offerSdp := webrtc.SessionDescription{
		SDP:  strOffer,
		Type: webrtc.SDPTypeOffer,
	}
	err = peerConn.SetRemoteDescription(offerSdp)
	if err != nil {
		panic(err)
	}

	p.track = track

	answer, err := peerConn.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	screen := p.grabber.Screen()
	sourceSize := image.Point{
		screen.Bounds.Dx(),
		screen.Bounds.Dy(),
	}

	fmt.Printf("encCodec: %+v\nsourceSize: %+v\nfps: %+v\n", encCodec, sourceSize, p.grabber.Fps())
	encoder, err := p.encService.NewEncoder(encCodec, sourceSize, p.grabber.Fps())
	fmt.Println("encoder start: ============")
	fmt.Println(encoder)
	fmt.Println("encoder end: ============")
	if err != nil {
		return
	}

	size, err := encoder.VideoSize()
	if err != nil {
		return
	}

	p.streamer = newRTCStreamer(p.track, &p.grabber, &encoder, size)

	peerConn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if flag {
			fmt.Println("ICE Candidate ------------------------")
			if c == nil {
				return
			}

			// outbound, marshalErr := json.Marshal(c.ToJSON())
			// if marshalErr != nil {
			// 	log.Fatal(marshalErr)
			// 	return
			// }

			msg := iceResponse{
				WSType: "ICE",
				ICE:    c.ToJSON(),
			}

			payload, err := json.Marshal(msg)
			if err != nil {
				log.Fatal(err)
				return
			}

			if err := conn.WriteMessage(messageType, payload); err != nil {
				log.Fatal(err)
				return
			}
		}
		flag = true
	})

	err = peerConn.SetLocalDescription(answer)
	if err != nil {
		return
	}

	resAnswer := answer.SDP
	if err != nil {
		log.Fatal(err)
		return
	}

	payload, err := json.Marshal(newSessionResponse{
		WSType: "SDP",
		Answer: resAnswer,
	})
	if err != nil {
		log.Fatal(err)
		return
	}

	fmt.Println("Before sending to client")
	if err := conn.WriteMessage(messageType, payload); err != nil {
		panic(err)
	}

	peerConn.OnICEConnectionStateChange(func(connState webrtc.ICEConnectionState) {
		if connState == webrtc.ICEConnectionStateConnected {
			p.start()
		}
		if connState == webrtc.ICEConnectionStateDisconnected {
			p.Close()
		}
		log.Printf("Connection state: %s \n", connState.String())
	})
}

func (p *RemoteScreenPeerConn) ProcessICE(ICE webrtc.ICECandidateInit) {
	// var candidate webrtc.ICECandidateInit
	// if err := json.Unmarshal(ICE, &candidate); err != nil {
	// 	log.Fatal(err)
	// 	return
	// }
	fmt.Println("ICE - START1")
	if ICE.Candidate != "" {
		fmt.Println("ICE - START2")
		if err := p.connection.AddICECandidate(ICE); err != nil {
			log.Fatal(err)
			return
		}
	}
}

func (p *RemoteScreenPeerConn) start() {
	p.streamer.start()
}

// Close Stops the video streamer and closes the WebRTC peer connection
func (p *RemoteScreenPeerConn) Close() error {

	if p.streamer != nil {
		p.streamer.close()
	}

	if p.connection != nil {
		return p.connection.Close()
	}
	return nil
}
