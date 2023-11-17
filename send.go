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
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"zombiezen.com/go/log"
)

func newSendCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:   "send [flags] -o PIPE OUT_PATHS...",
		Short: "Queue store paths for upload",
		Long: "This command appends its arguments to the output file,\n" +
			"one per line.",
		SilenceErrors:         true,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
	}
	outputPath := c.Flags().StringP("output", "o", "", "`path` to queue pipe (or file)")
	timeout := c.Flags().Duration("timeout", 0, "maximum amount of time to send arguments")
	finish := c.Flags().Bool("finish", false, "signal that the uploader should exit after sending the paths")
	c.Args = func(cmd *cobra.Command, args []string) error {
		if !*finish && len(args) < 1 {
			return fmt.Errorf("requires at least 1 arg, only received %d", len(args))
		}
		for i, arg := range args {
			if strings.Contains(arg, "\x00") {
				return fmt.Errorf("arg %d contains a NUL byte", i+1)
			}
		}
		return nil
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if *outputPath == "" {
			return fmt.Errorf("missing --output flag")
		}
		if *finish {
			// Don't want to modify the underlying array passed into the function.
			args = append(args[:len(args):len(args)], inlineEOFLine)
		}
		ctx := cmd.Context()
		if *timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, *timeout)
			defer cancel()
		}
		return runSend(ctx, g, *outputPath, args)
	}
	return c
}

func runSend(ctx context.Context, g *globalConfig, dst string, outPaths []string) (err error) {
	// pipeBufSize is the equivalent of PIPE_BUF in C.
	// On Linux, this is 4096, but on Darwin, it's 512.
	// We use the lower number rather than doing conditional compilation on OS.
	const pipeBufSize = 512

	f, err := openFileCtx(ctx, dst, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	buf := make([]byte, 0, pipeBufSize)
	nfailed := 0
	firstInBatch := 0
	for i, p := range outPaths {
		if strings.Contains(p, "\n") {
			nfailed++
			log.Errorf(ctx, "Cannot send %q: contains newline", p)
			continue
		}
		lineSize := len(p) + len("\n")
		if lineSize > pipeBufSize {
			nfailed++
			log.Errorf(ctx, "Cannot send %q: too large", p)
			continue
		}
		if len(buf)+lineSize > pipeBufSize {
			if _, err := writeCtx(ctx, f, buf); err != nil {
				return err
			}
			if log.IsEnabled(log.Debug) {
				log.Debugf(ctx, "Sent %s", strings.Join(outPaths[firstInBatch:i], " "))
			}
			buf = buf[:0]
			firstInBatch = i
		}
		buf = append(buf, p...)
		buf = append(buf, '\n')
	}
	if len(buf) > 0 {
		if _, err := writeCtx(ctx, f, buf); err != nil {
			return err
		}
		if log.IsEnabled(log.Debug) {
			log.Debugf(ctx, "Sent %s", strings.Join(outPaths[firstInBatch:], " "))
		}
	}

	if nfailed == 1 {
		return fmt.Errorf("unable to send 1 output path")
	}
	if nfailed > 1 {
		return fmt.Errorf("unable to send %d output paths", nfailed)
	}
	return nil
}
