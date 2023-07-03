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
	"io"
	"os"
	"os/signal"
	"sync"

	"github.com/spf13/cobra"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
	"gocloud.dev/blob/gcsblob"
	"gocloud.dev/blob/s3blob"
	"gocloud.dev/gcp"
	"golang.org/x/sys/unix"
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
		newSendCommand(g),
		newServeCommand(g),
		newUploadCommand(g),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), unix.SIGTERM, unix.SIGINT)
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

func openFileCtx(ctx context.Context, name string, flag int, perm os.FileMode) (*os.File, error) {
	type result struct {
		f   *os.File
		err error
	}

	done := ctx.Done()
	c := make(chan result)
	log.Debugf(ctx, "Opening file %s...", name)
	go func() {
		var r result
		r.f, r.err = os.OpenFile(name, flag, perm)
		select {
		case c <- r:
		case <-done:
			if r.f != nil {
				r.f.Close()
			}
		}
	}()

	select {
	case r := <-c:
		log.Debugf(ctx, "File %s opened", name)
		return r.f, r.err
	case <-done:
		log.Debugf(ctx, "File %s abandoned before open", name)
		return nil, ctx.Err()
	}
}

func writeCtx(ctx context.Context, w io.Writer, p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}

	done := ctx.Done()
	c := make(chan result)
	go func() {
		var r result
		r.n, r.err = w.Write(p)
		select {
		case c <- r:
		case <-done:
		}
	}()

	select {
	case r := <-c:
		return r.n, r.err
	case <-done:
		return 0, ctx.Err()
	}
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
