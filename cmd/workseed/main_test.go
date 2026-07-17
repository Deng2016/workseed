package main

import (
	"net"
	"testing"
)

func TestListenOnAvailablePortSkipsOccupiedPort(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	startPort := occupied.Addr().(*net.TCPAddr).Port
	if startPort == 65535 {
		t.Skip("random occupied port is the last valid port")
	}

	listener, port, err := listenOnAvailablePort("127.0.0.1", startPort)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if port <= startPort {
		t.Fatalf("expected a port after occupied port %d, got %d", startPort, port)
	}
}
