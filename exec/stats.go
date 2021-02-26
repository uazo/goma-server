// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package exec

import (
	"context"
	"fmt"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"go.chromium.org/goma/server/log"
	gomapb "go.chromium.org/goma/server/proto/api"
)

var (
	apiErrors = stats.Int64(
		"go.chromium.org/goma/server/exec.api-error",
		"exec request api-error",
		stats.UnitDimensionless)
	clientRetries = stats.Int64(
		"go.chromium.org/goma/server/exec.client-retry",
		"exec request per client retry",
		stats.UnitDimensionless)

	apiErrorKey    = tag.MustNewKey("api-error")
	clientRetryKey = tag.MustNewKey("client-retry")

	// DefaultViews are the default views provided by this package.
	// You need to register the view for data to actually be collected.
	DefaultViews = []*view.View{
		{
			Description: "exec request api-error",
			TagKeys: []tag.Key{
				apiErrorKey,
			},
			Measure:     apiErrors,
			Aggregation: view.Count(),
		},
		{
			Description: "exec request client retry",
			TagKeys: []tag.Key{
				clientRetryKey,
			},
			Measure:     clientRetries,
			Aggregation: view.Count(),
		},
		{
			Description: `counts toolchain selection. result is "used", "found", "requested" or "missed"`,
			TagKeys: []tag.Key{
				selectorKey,
				resultKey,
			},
			Measure:     toolchainSelects,
			Aggregation: view.Count(),
		},
	}
)

func apiErrorValue(ctx context.Context, resp *gomapb.ExecResp) string {
	logger := log.FromContext(ctx)
	if errVal := resp.GetError(); errVal != gomapb.ExecResp_OK {
		// marked as BAD_REQUEST for
		// - compiler/subprogram not found
		// - bad path_type in command config
		// - input root detection failed
		logger.Errorf("api-error=%s error_message=%s", errVal, resp.ErrorMessage)
		return errVal.String()
	}
	if len(resp.ErrorMessage) > 0 {
		logger.Errorf("api-error=internal: error_messge=%s", resp.ErrorMessage)
		return "internal"
	}
	if len(resp.MissingInput) > 0 {
		logger.Errorf("api-error=missing-inputs: missing=%d", len(resp.MissingInput))
		return "missing-inputs"
	}
	return "OK"
}

// RecordAPIError records api-error in resp.
func RecordAPIError(ctx context.Context, resp *gomapb.ExecResp) error {
	ctx, err := tag.New(ctx, tag.Upsert(apiErrorKey, apiErrorValue(ctx, resp)))
	if err != nil {
		return err
	}
	stats.Record(ctx, apiErrors.M(1))
	return nil
}

// RecordRequesterInfo records requester info.
// e.g. client retry count.
func RecordRequesterInfo(ctx context.Context, reqInfo *gomapb.RequesterInfo) error {
	return stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(clientRetryKey, fmt.Sprintf("%d", reqInfo.GetRetry()))}, clientRetries.M(1))
	// TODO: record api version / goma revision etc?
}
