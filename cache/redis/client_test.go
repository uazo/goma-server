// Copyright 2020 Google LLC. All Rights Reserved.

package redis

import (
	"context"
	"flag"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"go.chromium.org/goma/server/log"
	pb "go.chromium.org/goma/server/proto/cache"
)

var (
	numFilesPerExecReq = flag.Int("num_files_per_exec_req", 500, "number of files per ExecReq for BenchmarkGet")
)

func BenchmarkGet(b *testing.B) {
	log.SetZapLogger(zap.NewNop())
	s := NewFakeServer(b)

	ctx := context.Background()
	c := NewClient(ctx, s.Addr().String(), Opts{
		MaxIdleConns:   DefaultMaxIdleConns,
		MaxActiveConns: DefaultMaxActiveConns,
	})
	defer c.Close()

	b.Logf("b.N=%d", b.N)
	var wg sync.WaitGroup
	var (
		mu    sync.Mutex
		nerrs int
	)
	wg.Add(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			var rg sync.WaitGroup
			rg.Add(*numFilesPerExecReq)
			for i := 0; i < *numFilesPerExecReq; i++ {
				go func() {
					defer rg.Done()
					_, err := c.Get(ctx, &pb.GetReq{
						Key: "key",
					})
					if err != nil {
						mu.Lock()
						nerrs++
						mu.Unlock()
					}
				}()
			}
			rg.Wait()
		}()
	}
	wg.Wait()
	mu.Lock()
	b.Logf("nerrs=%d", nerrs)
	mu.Unlock()
}
