package main

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

var runnerDNSServers = []string{"1.1.1.1:53", "9.9.9.9:53"}

type contextDialer func(context.Context, string, string) (net.Conn, error)

func newRunnerResolver(dial contextDialer) *net.Resolver {
	var nextServer atomic.Uint64
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialErr error
			start := int(nextServer.Add(1)-1) % len(runnerDNSServers)
			for offset := range len(runnerDNSServers) {
				address := runnerDNSServers[(start+offset)%len(runnerDNSServers)]
				connection, err := dial(ctx, network, address)
				if err == nil {
					return connection, nil
				}
				dialErr = errors.Join(dialErr, err)
			}
			return nil, dialErr
		},
	}
}

func installRunnerResolver() {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	net.DefaultResolver = newRunnerResolver(dialer.DialContext)
}
