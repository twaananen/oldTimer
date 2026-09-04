package main

import (
	"context"
	"errors"
	"net"
	"time"
)

var runnerDNSServers = []string{"1.1.1.1:53", "9.9.9.9:53"}

type contextDialer func(context.Context, string, string) (net.Conn, error)

func newRunnerResolver(dial contextDialer) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialErr error
			for _, address := range runnerDNSServers {
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
