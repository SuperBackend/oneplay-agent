package rtc

import (
	"encoding/json"
	"fmt"
	"image"
	"log"
	"strconv"
	"strings"
	"time"

	"oneplay-videostream-browser/internal/encoders"
	"oneplay-videostream-browser/internal/rdisplay"

	"github.com/gorilla/websocket"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v3"
)

// RemoteScreenPeerConn is a webrtc.PeerConnection wrapper that implements the
// PeerConnection interface
type RemoteScreenPeerConn struct {
	connection *webrtc.PeerConnection
	stunServer string
	track      *webrtc.TrackLocalStaticSample
	streamer   videoStreamer
	grabber    rdisplay.ScreenGrabber
	encService encoders.Service
}

func codecsFromMediaDescription(m *sdp.MediaDescription) (out []webrtc.RTPCodecParameters, err error) {
	s := &sdp.SessionDescription{
		MediaDescriptions: []*sdp.MediaDescription{m},
	}

	for _, payloadStr := range m.MediaName.Formats {
		payloadType, err := strconv.ParseUint(payloadStr, 10, 8)
		if err != nil {
			return nil, err
		}

		codec, err := s.GetCodecForPayloadType(uint8(payloadType))
		if err != nil {
			if payloadType == 0 {
				continue
			}
			return nil, err
		}

		channels := uint16(0)
		val, err := strconv.ParseUint(codec.EncodingParameters, 10, 16)
		if err == nil {
			channels = uint16(val)
		}

		feedback := []webrtc.RTCPFeedback{}
		for _, raw := range codec.RTCPFeedback {
			split := strings.Split(raw, " ")
			entry := webrtc.RTCPFeedback{Type: split[0]}
			if len(split) == 2 {
				entry.Parameter = split[1]
			}

			feedback = append(feedback, entry)
		}

		out = append(out, webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{m.MediaName.Media + "/" + codec.Name, codec.ClockRate, channels, codec.Fmtp, feedback},
			PayloadType:        webrtc.PayloadType(payloadType),
		})
	}

	return out, nil
}

// func findBestCodec(sdp *sdp.SessionDescription, encService encoders.Service, h264Profile string) (*webrtc.RTPCodec, encoders.VideoCodec, error) {
// 	var h264Codec *webrtc.RTPCodec
// 	var vp8Codec *webrtc.RTPCodec
// 	for _, md := range sdp.MediaDescriptions {
// 		for _, format := range md.MediaName.Formats {
// 			intPt, err := strconv.Atoi(format)
// 			payloadType := uint8(intPt)
// 			sdpCodec, err := sdp.GetCodecForPayloadType(payloadType)
// 			if err != nil {
// 				return nil, encoders.NoCodec, fmt.Errorf("Can't find codec for %d", payloadType)
// 			}

// 			if sdpCodec.Name == "H264" && h264Codec == nil {
// 				packetSupport := strings.Contains(sdpCodec.Fmtp, "packetization-mode=1")
// 				supportsProfile := strings.Contains(sdpCodec.Fmtp, fmt.Sprintf("profile-level-id=%s", h264Profile))
// 				if packetSupport && supportsProfile {
// 					h264Codec = webrtc.NewRTPH264Codec(payloadType, sdpCodec.ClockRate)
// 					h264Codec.SDPFmtpLine = sdpCodec.Fmtp
// 				}
// 			} else if sdpCodec.Name == webrtc.VP8 && vp8Codec == nil {
// 				vp8Codec = webrtc.NewRTPVP8Codec(payloadType, sdpCodec.ClockRate)
// 				vp8Codec.SDPFmtpLine = sdpCodec.Fmtp
// 			}
// 		}
// 	}
// 	if vp8Codec != nil && encService.Supports(encoders.VP8Codec) {
// 		return vp8Codec, encoders.VP8Codec, nil
// 	}
// 	if h264Codec != nil && encService.Supports(encoders.H264Codec) {
// 		return h264Codec, encoders.H264Codec, nil
// 	}
// 	return nil, encoders.NoCodec, fmt.Errorf("Couldn't find a matching codec")

// 	for _, md := range sdp.MediaDescriptions {
// 		codecParameters, err := codecsFromMediaDescription(md)

// 	}
// }

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

// var flag bool

type newSessionResponse struct {
	WSType string
	Answer string
}

// var isWaiting bool

// ProcessOffer handles the SDP offer coming from the client,
// return the SDP answer that must be passed back to stablish the WebRTC
// connection.
func (p *RemoteScreenPeerConn) ProcessOffer(strOffer string, conn *websocket.Conn, messageType int) {
	// flag = false
	fmt.Println("session to offer : ", strOffer)
	sdp := sdp.SessionDescription{}
	strOfferByte := make([]byte, len(strOffer))
	copy(strOfferByte, strOffer)
	err := sdp.Unmarshal(strOfferByte)
	if err != nil {
		panic(err)
	}

	// webrtcCodec, encCodec, err := findBestCodec(&sdp, p.encService, "42e01f")
	// if err != nil {
	// 	panic(err)
	// }
	encCodec := encoders.H264Codec
	mediaEngine := webrtc.MediaEngine{}

	var cParameter webrtc.RTPCodecParameters
	for _, md := range sdp.MediaDescriptions {
		codecParameters, err := codecsFromMediaDescription(md)
		if err != nil {
			continue
		}
		for i := 0; i < len(codecParameters); i++ {
			if codecParameters[i].MimeType == webrtc.MimeTypeH264 {
				fmtp := strings.Contains(codecParameters[i].SDPFmtpLine, fmt.Sprintf("profile-level-id=%s", "42e01f"))
				packet := strings.Contains(codecParameters[i].SDPFmtpLine, "packetization-mode=1")
				fmt.Println(codecParameters[i])
				if fmtp && packet {
					cParameter = codecParameters[i]
					err := mediaEngine.RegisterCodec(codecParameters[i], webrtc.RTPCodecTypeVideo)
					if err != nil {
						panic(err)
					}
				}
			}
		}
	}

	err = mediaEngine.RegisterDefaultCodecs()
	if err != nil {
		log.Fatal(err)
	}

	// mediaEngine.RegisterCodec(webrtcCodec)
	// err = mediaEngine.RegisterDefaultCodecs()
	// if err != nil {
	// 	log.Fatal(err)
	// }

	api := webrtc.NewAPI(webrtc.WithMediaEngine(&mediaEngine))

	pcconf := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs:       []string{p.stunServer},
				Username:   "YzYNCouZM1mhqhmseWk6",
				Credential: "YzYNCouZM1mhqhmseWk6",
			},
		},
		SDPSemantics: webrtc.SDPSemanticsUnifiedPlan,
	}

	peerConn, err := api.NewPeerConnection(pcconf)
	if err != nil {
		panic(err)
	}

	peerConn.OnDataChannel(func(d *webrtc.DataChannel) {
		fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

		// Register channel opening handling
		d.OnOpen(func() {
			fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", d.Label(), d.ID())

			// for range time.NewTicker(5 * time.Second).C {

			// 	fmt.Printf("Sending '%s'\n", message)

			// 	// Send the message as text
			// 	sendErr := d.SendText(message)
			// 	if sendErr != nil {
			// 		panic(sendErr)
			// 	}
			// }
		})

		// Register text message handling
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
		})
	})

	p.connection = peerConn

	var payloads [][]byte

	peerConn.OnICECandidate(func(c *webrtc.ICECandidate) {

		if c == nil {
			return
		}

		msg := iceResponse{
			WSType: "ICE",
			ICE:    c.ToJSON(),
		}

		payload, err := json.Marshal(msg)
		if err != nil {
			log.Fatal(err)
			return
		}

		payloads = append(payloads, payload)
		if err = peerConn.AddICECandidate(c.ToJSON()); err != nil {
			panic(err)
		}
	})

	peerConn.OnICEConnectionStateChange(func(connState webrtc.ICEConnectionState) {
		if connState == webrtc.ICEConnectionStateConnected {
			p.start()
		}
		if connState == webrtc.ICEConnectionStateDisconnected {
			p.Close()
		}
		fmt.Println(connState)
		log.Printf("Connection state: %s \n", connState.String())
		//p.start()
	})
	// outputTracks := map[string]*webrtc.TrackLocalStaticRTP{}

	// Create Track that we send video back to browser on
	// outputTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video_q", "pion_q")
	// if err != nil {
	// 	panic(err)
	// }0

	outputTrack, err := webrtc.NewTrackLocalStaticSample(cParameter.RTPCodecCapability, "video_q", "pion_q")
	// outputTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video_q", "pion_q")
	if err != nil {
		panic(err)
	}

	direction := getTrackDirection(&sdp)

	if direction == webrtc.RTPTransceiverDirectionSendrecv {
		fmt.Println("In Send and Recv")
		if _, err = peerConn.AddTrack(outputTrack); err != nil {
			panic(err)
		}
	} else if direction == webrtc.RTPTransceiverDirectionRecvonly {
		fmt.Println("In Recv")
		_, err = peerConn.AddTransceiverFromTrack(outputTrack, webrtc.RtpTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionSendonly,
		})
		if err != nil {
			fmt.Println("Add Track Error")
		}
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

	p.track = outputTrack

	answer, err := peerConn.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	screen := p.grabber.Screen()
	sourceSize := image.Point{
		screen.Bounds.Dx(),
		screen.Bounds.Dy(),
	}

	fmt.Println("------------------------ : ", encCodec)
	encoder, err := p.encService.NewEncoder(encCodec, sourceSize, p.grabber.Fps())
	if err != nil {
		return
	}

	size, err := encoder.VideoSize()
	if err != nil {
		return
	}

	fmt.Println(p.grabber, encoder, size)

	p.streamer = newRTCStreamer(p.track, &p.grabber, &encoder, size)

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

	time.Sleep(2 * time.Second)
	for i := 0; i < len(payloads); i++ {
		fmt.Println("ICE :", string(payloads[i]))
		if err = conn.WriteMessage(messageType, payloads[i]); err != nil {
			panic(err)
		}
	}
}

func (p *RemoteScreenPeerConn) ProcessICE(ICE webrtc.ICECandidateInit) {
	// var candidate webrtc.ICECandidateInit
	// if err := json.Unmarshal(ICE, &candidate); err != nil {
	// 	log.Fatal(err)
	// 	return
	// }
	fmt.Println("------------START----ICECANDIDATE------START------------------")
	fmt.Println("ICE : ", ICE)
	if ICE.Candidate != "" {
		if err := p.connection.AddICECandidate(ICE); err != nil {
			fmt.Println("Error occurred when add client ICE candidate")
			panic(err)
		} else {
			fmt.Println("ICE added successfully")
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
