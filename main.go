// Copyright 2023 Ross Light
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//		 https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/signal"
	"sync"

	"github.com/spf13/cobra"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
	"gocloud.dev/blob/gcsblob"
	"gocloud.dev/blob/s3blob"
	"gocloud.dev/gcp"
	"zombiezen.com/go/bass/sigterm"
	"zombiezen.com/go/log"
)

type globalConfig struct {
	// global options go here
}

func main() {
	rootCommand := &cobra.Command{
		Use:           "nixcached",
		Short:         "Nix cache daemon",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	g := new(globalConfig)
	showDebug := rootCommand.PersistentFlags().Bool("debug", false, "show debugging output")
	rootCommand.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		initLogging(*showDebug)
		return nil
	}

	rootCommand.AddCommand(
		newUploadCommand(g),
		// ...
	)

	ctx, cancel := signal.NotifyContext(context.Background(), sigterm.Signals()...)
	err := rootCommand.ExecuteContext(ctx)
	cancel()
	if err != nil {
		initLogging(*showDebug)
		log.Errorf(context.Background(), "%v", err)
		os.Exit(1)
	}
}

func newBucketURLOpener(ctx context.Context) (*blob.URLMux, error) {
	mux := new(blob.URLMux)
	mux.RegisterBucket(fileblob.Scheme, &fileblob.URLOpener{})

	gcpCreds, err := gcp.DefaultCredentials(ctx)
	var gcsClient *gcp.HTTPClient
	if err != nil {
		log.Debugf(ctx, "Google credentials not set (%v), using anonymous", err)
		gcsClient = gcp.NewAnonymousHTTPClient(gcp.DefaultTransport())
	} else {
		gcsClient, err = gcp.NewHTTPClient(gcp.DefaultTransport(), gcp.CredentialsTokenSource(gcpCreds))
		if err != nil {
			return nil, err
		}
	}
	mux.RegisterBucket(gcsblob.Scheme, &gcsblob.URLOpener{
		Client: gcsClient,
	})

	mux.RegisterBucket(s3blob.Scheme, &s3blob.URLOpener{
		UseV2: true,
	})

	return mux, nil
}

var initLogOnce sync.Once

func initLogging(showDebug bool) {
	initLogOnce.Do(func() {
		minLogLevel := log.Info
		if showDebug {
			minLogLevel = log.Debug
		}
		log.SetDefault(&log.LevelFilter{
			Min:    minLogLevel,
			Output: log.New(os.Stderr, "nixcached: ", log.StdFlags, nil),
		})
	})
}
