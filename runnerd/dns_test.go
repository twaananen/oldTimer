package main

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
)

func TestRunnerResolverBypassesHostStubAndFallsBack(t *testing.T) {
	wantErr := errors.New("second resolver reached")
	var addresses []string
	resolver := newRunnerResolver(func(_ context.Context, _, address string) (net.Conn, error) {
		addresses = append(addresses, address)
		if len(addresses) == 1 {
			return nil, errors.New("first resolver unavailable")
		}
		return nil, wantErr
	})

	_, err := resolver.Dial(context.Background(), "udp", "127.0.0.53:53")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Dial error = %v, want %v", err, wantErr)
	}
	wantAddresses := []string{"1.1.1.1:53", "9.9.9.9:53"}
	if !reflect.DeepEqual(addresses, wantAddresses) {
		t.Fatalf("resolver addresses = %v, want %v", addresses, wantAddresses)
	}
}

func TestRunnerResolverAlternatesServersAcrossQueryAttempts(t *testing.T) {
	var addresses []string
	var peers []net.Conn
	resolver := newRunnerResolver(func(_ context.Context, _, address string) (net.Conn, error) {
		addresses = append(addresses, address)
		connection, peer := net.Pipe()
		peers = append(peers, peer)
		return connection, nil
	})
	defer func() {
		for _, peer := range peers {
			peer.Close()
		}
	}()

	for range 2 {
		connection, err := resolver.Dial(context.Background(), "udp", "127.0.0.53:53")
		if err != nil {
			t.Fatalf("Dial returned an error: %v", err)
		}
		connection.Close()
	}
	wantAddresses := []string{"1.1.1.1:53", "9.9.9.9:53"}
	if !reflect.DeepEqual(addresses, wantAddresses) {
		t.Fatalf("resolver addresses = %v, want %v", addresses, wantAddresses)
	}
}
