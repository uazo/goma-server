// Copyright 2017 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package execlog

import (
	"context"
	"fmt"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
)

// DefaultMaxReqMsgSize is max request message size for execlog service.
// execlog server may receives > 8MB.
// grpc's default is 4MB.
const DefaultMaxReqMsgSize = 10 * 1024 * 1024

var (
	requests = stats.Int64(
		"go.chromium.org/goma/execlog/requests",
		"number of execlog entries",
		stats.UnitDimensionless)

	osFamilyKey           = tag.MustNewKey("os_family")
	gomaErrorKey          = tag.MustNewKey("goma_error")
	compilerProxyErrorKey = tag.MustNewKey("compiler_proxy_error")
	cacheHitKey           = tag.MustNewKey("cache_hit")
	depscacheUsedKey      = tag.MustNewKey("depscache_used")
	localRunKey           = tag.MustNewKey("local_run")
	execExitStatusKey     = tag.MustNewKey("exec_exit_status")
	execRequestRetryKey   = tag.MustNewKey("exec_request_retry")

	handlerTime = stats.Float64(
		"go.chromium.org/goma/execlog/handler_time",
		"Time in compiler_proxy handler",
		stats.UnitMilliseconds)

	pendingTime = stats.Float64(
		"go.chromium.org/goma/execlog/pending_time",
		"Time in pending queue in compiler_proxy",
		stats.UnitMilliseconds)
	// need compiler_info_process_time?

	includeProcessorWaitTime = stats.Float64(
		"go.chromium.org/goma/execlog/include_processor_wait_time",
		"Time to wait include processor",
		stats.UnitMilliseconds)
	includeProcessorRunTime = stats.Float64(
		"go.chromium.org/goma/execlog/include_processor_run_time",
		"Time to run include processor",
		stats.UnitMilliseconds)
	includePreprocessTotalFiles = stats.Int64(
		"go.chromium.org/goma/execlog/include_preprocess_total_files",
		"Number of files processed in include preprocess",
		stats.UnitDimensionless)

	includeFileloadPendingTime = stats.Float64(
		"go.chromium.org/goma/execlog/include_fileload_pending_time",
		"Time to wait upload input files",
		stats.UnitMilliseconds)
	includeFileloadRunTime = stats.Float64(
		"go.chromium.org/goma/execlog/include_fileload_run_time",
		"Time to upload input files",
		stats.UnitMilliseconds)

	rpcThrottleTime = stats.Float64(
		"go.chromium.org/goma/execlog/rpc_throttle_time",
		"Time to wait to call Exec by throttling (backoff by error)",
		stats.UnitMilliseconds)
	rpcPendingTime = stats.Float64(
		"go.chromium.org/goma/execlog/rpc_pending_time",
		"Time to wait to call Exec (too many requests)",
		stats.UnitMilliseconds)
	rpcWaitTime = stats.Float64(
		"go.chromium.org/goma/execlog/rpc_wait_time",
		"Time to wait Exec call response (i.e. server latency)",
		stats.UnitMilliseconds)

	fileResponseTime = stats.Float64(
		"go.chromium.org/goma/execlog/file_response_time",
		"Time to process output files",
		stats.UnitMilliseconds)

	localDelayTime = stats.Float64(
		"go.chromium.org/goma/execlog/local_delay_time",
		"Time delayed to start run locally",
		stats.UnitMilliseconds)
	localPendingTime = stats.Float64(
		"go.chromium.org/goma/execlog/local_penging_time",
		"Time to wait to run locally",
		stats.UnitMilliseconds)
	localRunTime = stats.Float64(
		"go.chromium.org/goma/execlog/local_run_time",
		"Time to run locally",
		stats.UnitMilliseconds)

	defaultLatencyDistribution = view.Distribution(1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000, 200000, 500000)

	tagKeys = []tag.Key{
		osFamilyKey,
		cacheHitKey,
		depscacheUsedKey,
		localRunKey,
		execExitStatusKey,
	}

	DefaultViews = []*view.View{
		{
			TagKeys: []tag.Key{
				osFamilyKey,
				gomaErrorKey,
				compilerProxyErrorKey,
				cacheHitKey,
				depscacheUsedKey,
				localRunKey,
				execExitStatusKey,
				execRequestRetryKey,
			},
			Measure:     requests,
			Aggregation: view.Sum(),
		},
		{
			TagKeys:     tagKeys,
			Measure:     handlerTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     pendingTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     includeProcessorWaitTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     includeProcessorRunTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     includePreprocessTotalFiles,
			Aggregation: view.Sum(),
		},
		{
			TagKeys:     tagKeys,
			Measure:     includeFileloadPendingTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     includeFileloadRunTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     rpcThrottleTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     rpcPendingTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     rpcWaitTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     fileResponseTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     localDelayTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     localPendingTime,
			Aggregation: defaultLatencyDistribution,
		},
		{
			TagKeys:     tagKeys,
			Measure:     localRunTime,
			Aggregation: defaultLatencyDistribution,
		},
	}
)

// Service represents goma execlog service.
type Service struct {
}

func osFamily(e *gomapb.ExecLog) string {
	oi := e.GetOsInfo().GetOsInfoOneof()
	switch oi.(type) {
	case *gomapb.OSInfo_LinuxInfo_:
		return "Linux"
	case *gomapb.OSInfo_WinInfo_:
		return "Windows"
	case *gomapb.OSInfo_MacInfo_:
		return "Mac"
	default:
		return "Unknown"
	}
}

// SaveLog emits some metrics.
//  * go.chromium.org/goma/execlog/requests
//      {os_family, ,goma_error, compiler_proxy_error,
//       cache_hit, depscache_used, local_run,
//       exec_exit_status, exec_request_retry}
//  * go.chromium.org/goma/execlog/handler_time
// TODO: implement saving logic to GCS?
func (Service) SaveLog(ctx context.Context, req *gomapb.SaveLogReq) (*gomapb.SaveLogResp, error) {
	logger := log.FromContext(ctx)
	for _, e := range req.GetExecLog() {
		os := osFamily(e)
		localRun := e.GetLocalRunTime() > 0
		tags := []tag.Mutator{
			tag.Upsert(osFamilyKey, os),
			tag.Upsert(cacheHitKey, fmt.Sprint(e.GetCacheHit())),
			tag.Upsert(depscacheUsedKey, fmt.Sprint(e.GetDepscacheUsed())),
			tag.Upsert(localRunKey, fmt.Sprint(localRun)),
			tag.Upsert(execExitStatusKey, fmt.Sprint(e.GetExecExitStatus())),
		}
		ctx, err := tag.New(ctx, tags...)
		if err != nil {
			logger.Errorf("Failed to set tags for savelog: %v", err)
			continue
		}
		stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(gomaErrorKey, fmt.Sprint(e.GetGomaError())),
			tag.Upsert(compilerProxyErrorKey, fmt.Sprint(e.GetCompilerProxyError())),
			tag.Upsert(execRequestRetryKey, fmt.Sprint(e.GetExecRequestRetry())),
		}, requests.M(1))
		stats.Record(ctx, handlerTime.M(float64(e.GetHandlerTime())))
		stats.Record(ctx, pendingTime.M(float64(e.GetPendingTime())))
		stats.Record(ctx, includeProcessorWaitTime.M(float64(e.GetIncludeProcessorWaitTime())))
		stats.Record(ctx, includeProcessorRunTime.M(float64(e.GetIncludeProcessorRunTime())))
		stats.Record(ctx, includePreprocessTotalFiles.M(int64(e.GetIncludePreprocessTotalFiles())))
		for _, t := range e.GetIncludeFileloadPendingTime() {
			stats.Record(ctx, includeFileloadPendingTime.M(float64(t)))
		}
		for _, t := range e.GetIncludeFileloadRunTime() {
			stats.Record(ctx, includeFileloadRunTime.M(float64(t)))
		}
		for _, t := range e.GetRpcThrottleTime() {
			stats.Record(ctx, rpcThrottleTime.M(float64(t)))
		}
		for _, t := range e.GetRpcPendingTime() {
			stats.Record(ctx, rpcPendingTime.M(float64(t)))
		}
		for _, t := range e.GetRpcWaitTime() {
			stats.Record(ctx, rpcWaitTime.M(float64(t)))
		}
		stats.Record(ctx, fileResponseTime.M(float64(e.GetFileResponseTime())))

		stats.Record(ctx, localDelayTime.M(float64(e.GetLocalDelayTime())))
		stats.Record(ctx, localPendingTime.M(float64(e.GetLocalPendingTime())))
		stats.Record(ctx, localRunTime.M(float64(e.GetLocalRunTime())))
	}

	return &gomapb.SaveLogResp{}, nil
}
