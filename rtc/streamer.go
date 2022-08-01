package rtc

import (
	"fmt"
	"image"
	"time"

	"oneplay-videostream-browser/internal/encoders"
	"oneplay-videostream-browser/internal/rdisplay"

	"github.com/nfnt/resize"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

func resizeImage(src *image.RGBA, target image.Point) *image.RGBA {
	return resize.Resize(uint(target.X), uint(target.Y), src, resize.Lanczos3).(*image.RGBA)
}

type rtcStreamer struct {
	track   *webrtc.TrackLocalStaticSample
	stop    chan struct{}
	screen  *rdisplay.ScreenGrabber
	encoder *encoders.Encoder
	size    image.Point
}

func newRTCStreamer(track *webrtc.TrackLocalStaticSample, screen *rdisplay.ScreenGrabber, encoder *encoders.Encoder, size image.Point) videoStreamer {
	// p, err := webrtc.NewTrackLocalStaticSample(track.Codec(), track.ID(), track.StreamID())
	// if err != nil {
	// 	panic(err)
	// }
	return &rtcStreamer{
		track:   track,
		stop:    make(chan struct{}),
		screen:  screen,
		encoder: encoder,
		size:    size,
	}
}

func (s *rtcStreamer) start() {
	go s.startStream()
}

func (s *rtcStreamer) startStream() {
	screen := *s.screen
	screen.Start()
	frames := screen.Frames()
	for {
		select {
		case <-s.stop:
			screen.Stop()
			return
		case frame := <-frames:
			err := s.stream(frame)
			if err != nil {
				fmt.Printf("Streamer: %v\n", err)
				return
			}
		}
	}
}

func (s *rtcStreamer) stream(frame *image.RGBA) error {
	resized := resizeImage(frame, s.size)
	payload, err := (*s.encoder).Encode(resized)
	if err != nil {
		return err
	}
	if payload == nil {
		return nil
	}
	return s.track.WriteSample(media.Sample{
		Data:     payload,
		Duration: time.Second,
	})
}

func (s *rtcStreamer) close() {
	close(s.stop)
}
