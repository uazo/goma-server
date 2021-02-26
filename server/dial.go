// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package server

import (
	"context"
	"time"

	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // also register compressor for server side
	"google.golang.org/grpc/keepalive"
)

// DefaultDialOption is default dial option to record opencensus stats and traces.
func DefaultDialOption() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: false,
		}),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
	}
}

// DialContext dials to addr with default dial options.
func DialContext(ctx context.Context, addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append(opts, DefaultDialOption()...)
	return grpc.DialContext(ctx, addr, opts...)
}
