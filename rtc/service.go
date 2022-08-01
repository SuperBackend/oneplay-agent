package rtc

import (
	"io"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

type videoStreamer interface {
	start()
	close()
}

// RemoteScreenConnection Represents a WebRTC connection to a single peer
type RemoteScreenConnection interface {
	io.Closer
	ProcessOffer(offer string, conn *websocket.Conn, messageType int)
	ProcessICE(ICE webrtc.ICECandidateInit)
}

// Service WebRTC service
type Service interface {
	CreateRemoteScreenConnection(screenIx int, fps int) (RemoteScreenConnection, error)
}
