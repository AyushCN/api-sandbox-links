package main

import "testing"

func TestAPIListenerIsLoopbackOnly(t *testing.T) {
	if got := apiAddr(8080); got != "127.0.0.1:8080" {
		t.Fatalf("apiAddr = %q, want loopback binding", got)
	}
}

func TestGatewayListenerStaysReachableFromTraefik(t *testing.T) {
	if got := gatewayAddr(8090); got != ":8090" {
		t.Fatalf("gatewayAddr = %q, want all-interface binding for Traefik", got)
	}
}
