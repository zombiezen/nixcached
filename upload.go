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
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dsnet/compress/bzip2"
	"github.com/spf13/cobra"
	"gocloud.dev/blob"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nixcached/internal/nixstore"
)

func newUploadCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:           "upload BUCKET_URL",
		Short:         "Upload paths from a file",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	inputPath := c.Flags().String("input", "", "file with store paths")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		storeDir, err := nixstore.DirectoryFromEnv()
		if err != nil {
			return err
		}
		input := os.Stdin
		if *inputPath != "" {
			f, err := os.Open(*inputPath)
			if err != nil {
				return err
			}
			defer f.Close()
			input = f
		}
		return runUpload(cmd.Context(), g, args[0], input, storeDir)
	}
	return c
}

func runUpload(ctx context.Context, g *globalConfig, destinationBucketURL string, input io.Reader, storeDir nixstore.Directory) error {
	opener, err := newBucketURLOpener(ctx)
	if err != nil {
		return err
	}
	bucket, err := opener.OpenBucket(ctx, destinationBucketURL)
	if err != nil {
		return err
	}
	defer bucket.Close()

	scanner := bufio.NewScanner(input)
	// TODO(soon): Also watch ctx.Done.
	for scanner.Scan() {
		storePath := scanner.Text()
		if _, ok := storeDir.ParseStorePath(storePath); !ok {
			log.Warnf(ctx, "Received invalid store path %q, skipping.", storePath)
			continue
		}
		log.Infof(ctx, "Uploading %s", storePath)
		uploadStorePath(ctx, bucket, storePath)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func uploadStorePath(ctx context.Context, destination *blob.Bucket, storePath string) error {
	objectName, err := nixstore.ParseObjectName(filepath.Base(storePath))
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}
	narCommand := exec.CommandContext(
		ctx,
		"nix", "--extra-experimental-features", "nix-command",
		"nar", "dump-path",
		"--", storePath,
	)
	// TODO(now)
	var err error
	narCommand.Stdout, err = bzip2.NewWriter(io.Discard, &bzip2.WriterConfig{})
	if err != nil {
		return err
	}
}
