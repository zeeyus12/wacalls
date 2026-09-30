package main

import (
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// Browser-facing WebRTC needs a fixed UDP port and a public IP when running
// behind NAT (e.g. Fly.io). Enabled only when WACALLS_PUBLIC_IP and
// WACALLS_UDP_PORT are set; otherwise default pion behaviour is unchanged.
//
//	WACALLS_PUBLIC_IP   dedicated public IPv4 advertised to the browser
//	WACALLS_UDP_PORT    single UDP port all browser media uses
//	WACALLS_UDP_BIND    bind address (Fly: fly-global-services), default 0.0.0.0

var (
	bridgeAPIOnce sync.Once
	bridgeAPIInst *webrtc.API
)

func bridgeAPI() *webrtc.API {
	bridgeAPIOnce.Do(func() {
		ip := os.Getenv("WACALLS_PUBLIC_IP")
		port := os.Getenv("WACALLS_UDP_PORT")
		if ip == "" || port == "" {
			return
		}
		bind := os.Getenv("WACALLS_UDP_BIND")
		if bind == "" {
			bind = "0.0.0.0"
		}
		conn, err := net.ListenPacket("udp4", net.JoinHostPort(bind, port))
		if err != nil {
			slog.Error("udp listen failed, using default webrtc settings", "err", err)
			return
		}
		se := webrtc.SettingEngine{}
		se.SetICEUDPMux(ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: conn}))
		se.SetNAT1To1IPs([]string{ip}, webrtc.ICECandidateTypeHost)
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
		bridgeAPIInst = webrtc.NewAPI(webrtc.WithSettingEngine(se))
		slog.Info("webrtc udp mux ready", "bind", bind, "port", port, "public_ip", ip)
	})
	return bridgeAPIInst
}

func newBridgePC() (*webrtc.PeerConnection, error) {
	if api := bridgeAPI(); api != nil {
		return api.NewPeerConnection(webrtc.Configuration{})
	}
	return webrtc.NewPeerConnection(webrtc.Configuration{})
}
