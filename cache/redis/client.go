// Copyright 2019 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/gomodule/redigo/redis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.chromium.org/goma/server/log"
	pb "go.chromium.org/goma/server/proto/cache"
	"go.chromium.org/goma/server/rpc"
)

// Client is cache service client for redis.
type Client struct {
	prefix string
	pool   *redis.Pool

	// to workaround pool.wait. maintain active conns.
	sema chan struct{}
}

// AddrFromEnv returns redis server address from environment variables.
func AddrFromEnv() (string, error) {
	host := os.Getenv("REDISHOST")
	port := os.Getenv("REDISPORT")
	if host == "" {
		return "", errors.New("no REDISHOST environment")
	}
	if port == "" {
		port = "6379" // redis default port
	}
	return fmt.Sprintf("%s:%s", host, port), nil
}

// Opts is redis client option.
type Opts struct {
	// Prefix is key prefix used by the client.
	Prefix string

	// MaxIdleConns is max number of idle connections.
	MaxIdleConns int

	// MaxActiveConns is max number of active connections.
	MaxActiveConns int
}

// default max number of connections.
// note: in GCP, redis quota is 65,000
const (
	DefaultMaxIdleConns   = 50
	DefaultMaxActiveConns = 200
)

// NewClient creates new cache client for redis.
func NewClient(ctx context.Context, addr string, opts Opts) Client {
	return Client{
		prefix: opts.Prefix,
		pool: &redis.Pool{
			DialContext: func(ctx context.Context) (redis.Conn, error) {
				return redis.DialContext(ctx, "tcp", addr)
			},
			MaxIdle:   opts.MaxIdleConns,
			MaxActive: opts.MaxActiveConns,
			// https://github.com/gomodule/redigo/issues/520
			Wait: false,
		},
		sema: make(chan struct{}, opts.MaxActiveConns),
	}
}

// Close releases the resources used by the client.
func (c Client) Close() error {
	return c.pool.Close()
}

type temporary interface {
	Temporary() bool
}

func isConnError(err error) bool {
	errno, ok := err.(syscall.Errno)
	if !ok {
		serr, ok := err.(*os.SyscallError)
		if !ok {
			return false
		}
		errno, ok = serr.Err.(syscall.Errno)
		if !ok {
			return false
		}
	}
	return errno == syscall.ECONNRESET || errno == syscall.ECONNABORTED
}

// retryErr converts err to rpc.RetriableError if it is retriable error.
func retryErr(err error) error {
	if errors.Is(err, redis.ErrNil) {
		return status.Error(codes.NotFound, err.Error())
	}
	// retriable if temporary error.
	if terr, ok := err.(temporary); ok && terr.Temporary() {
		return rpc.RetriableError{
			Err: err,
		}
	}
	// redis might return net.OpError as is.
	operr, ok := err.(*net.OpError)
	if !ok {
		return err
	}
	// retry if it is ECONNRESET or ECONNABORTED.
	if isConnError(operr.Err) {
		return rpc.RetriableError{
			Err: err,
		}
	}
	return err
}

type activeConn struct {
	redis.Conn
	c Client
}

func (c activeConn) Close() error {
	<-c.c.sema
	return c.Conn.Close()
}

func (c Client) poolGetContext(ctx context.Context) (redis.Conn, error) {
	t := time.Now()
	select {
	case c.sema <- struct{}{}:
		d := time.Since(t)
		if d > 100*time.Millisecond {
			logger := log.FromContext(ctx)
			logger.Warnf("redis pool wait %s actives=%d", d, len(c.sema))
		}
		conn, err := c.pool.GetContext(ctx)
		if err != nil {
			<-c.sema
			return nil, err
		}
		return activeConn{
			Conn: conn,
			c:    c,
		}, nil
	case <-ctx.Done():
		d := time.Since(t)
		if d > 100*time.Millisecond {
			logger := log.FromContext(ctx)
			logger.Warnf("redis pool timed-out wait %s actives=%d", d, len(c.sema))
		}
		return nil, ctx.Err()
	}
}

// Get fetches value for the key from redis.
func (c Client) Get(ctx context.Context, in *pb.GetReq, opts ...grpc.CallOption) (*pb.GetResp, error) {
	conn, err := c.poolGetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var v []byte
	err = rpc.Retry{
		MaxRetry: -1,
	}.Do(ctx, func() error {
		v, err = redis.Bytes(conn.Do("GET", c.prefix+in.Key))
		return retryErr(err)
	})
	if err != nil {
		return nil, err
	}
	return &pb.GetResp{
		Kv: &pb.KV{
			Key:   in.Key,
			Value: v,
		},
		InMemory: true,
	}, nil
}

// Put stores key:value pair on redis.
func (c Client) Put(ctx context.Context, in *pb.PutReq, opts ...grpc.CallOption) (*pb.PutResp, error) {
	conn, err := c.poolGetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	err = rpc.Retry{
		MaxRetry: -1,
	}.Do(ctx, func() error {
		_, err := conn.Do("SET", c.prefix+in.Kv.Key, in.Kv.Value)
		return retryErr(err)
	})
	if err != nil {
		return nil, err
	}
	return &pb.PutResp{}, nil
}
