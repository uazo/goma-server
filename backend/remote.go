// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package backend

import (
	"context"
	"crypto/tls"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/balancer"
	bpb "github.com/bazelbuild/remote-apis-sdks/go/pkg/balancer/proto"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/client"
	"go.opencensus.io/plugin/ocgrpc"
	bspb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"go.chromium.org/goma/server/log"
	pb "go.chromium.org/goma/server/proto/backend"
	execpb "go.chromium.org/goma/server/proto/exec"
	execlogpb "go.chromium.org/goma/server/proto/execlog"
	filepb "go.chromium.org/goma/server/proto/file"
)

// FromRemoteBackend creates new GRPC from cfg.
// returned func would release resources associated with GRPC.
func FromRemoteBackend(ctx context.Context, cfg *pb.RemoteBackend, opt Option) (GRPC, func(), error) {
	logger := log.FromContext(ctx)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time: 10 * time.Second,
		}),
	}
	// TODO: configurable?
	// use the same default as re-client (remote-apis-sdks).
	// but don't set MaxConcurrentStreamsLowWatermark not to
	// open more connections to avoid "Too many open files" error
	// in nginx.  crbug.com/1151576
	ac := &bpb.ApiConfig{
		ChannelPool: &bpb.ChannelPoolConfig{
			MaxSize: client.DefaultMaxConcurrentRequests,
		},
		Method: []*bpb.MethodConfig{
			{
				Name: []string{".*"},
				Affinity: &bpb.AffinityConfig{
					Command:     bpb.AffinityConfig_BIND,
					AffinityKey: "bind-affinity",
				},
			},
		},
	}
	logger.Infof("api_config=%s", ac)
	grpcInt := balancer.NewGCPInterceptor(ac)
	opts = append(opts,
		grpc.WithBalancerName(balancer.Name),
		grpc.WithUnaryInterceptor(grpcInt.GCPUnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpcInt.GCPStreamClientInterceptor))

	conn, err := grpc.DialContext(ctx, cfg.Address, opts...)
	if err != nil {
		return GRPC{}, func() {}, err
	}
	var apiKey []byte
	if cfg.ApiKeyName != "" {
		apiKey, err = ioutil.ReadFile(filepath.Join(opt.APIKeyDir, cfg.ApiKeyName))
		if err != nil {
			return GRPC{}, func() { conn.Close() }, err
		}
	}
	be := GRPC{
		ExecServer: ExecServer{
			Client: execpb.NewExecServiceClient(conn),
		},
		FileServer: FileServer{
			Client: filepb.NewFileServiceClient(conn),
		},
		ExeclogServer: ExeclogServer{
			Client: execlogpb.NewLogServiceClient(conn),
		},
		// TODO: propagate metadata.
		ByteStreamClient: bspb.NewByteStreamClient(conn),
		Auth:             opt.Auth,
		APIKey:           strings.TrimSpace(string(apiKey)),
	}
	return be, func() { conn.Close() }, nil
}
