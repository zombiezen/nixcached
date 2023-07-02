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
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/dsnet/compress/bzip2"
	"github.com/spf13/cobra"
	"gocloud.dev/blob"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nixcached/internal/nixstore"
)

func newUploadCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "upload [flags] BUCKET_URL",
		Short:                 "Upload paths from a file or pipe",
		Args:                  cobra.ExactArgs(1),
		SilenceErrors:         true,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
	}
	inputPath := c.Flags().String("input", "", "`path` to file with store paths (defaults to stdin)")
	keepAlive := c.Flags().BoolP("keep-alive", "k", false, "if input is a pipe, then keep running even when there are no senders")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		storeDir, err := nixstore.DirectoryFromEnv()
		if err != nil {
			return err
		}
		input := os.Stdin
		if *inputPath != "" {
			openFlag := os.O_RDONLY
			if *keepAlive {
				if inputInfo, err := os.Stat(*inputPath); err == nil && inputInfo.Mode().Type() == os.ModeNamedPipe {
					openFlag = os.O_RDWR
				}
			}
			f, err := openFileCtx(ctx, *inputPath, openFlag, 0)
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
	storeClient := &nixstore.Client{
		Log: os.Stderr,
	}
	c := make(chan bool)
scanLoop:
	for {
		go func() {
			select {
			case c <- scanner.Scan():
			case <-ctx.Done():
			}
		}()

		select {
		case scanned := <-c:
			if !scanned {
				break scanLoop
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		storePath := scanner.Text()
		name, ok := storeDir.ParseStorePath(storePath)
		if !ok {
			log.Warnf(ctx, "Received invalid store path %q, skipping.", storePath)
			continue
		}
		if name.IsDerivation() {
			log.Warnf(ctx, "Received store derivation path %q, skipping.", storePath)
			continue
		}
		log.Infof(ctx, "Uploading %s", storePath)
		if err := uploadStorePath(ctx, storeClient, bucket, storePath); err != nil {
			log.Errorf(ctx, "Failed: %v", err)
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func uploadStorePath(ctx context.Context, storeClient *nixstore.Client, destination *blob.Bucket, storePath string) error {
	objectName, err := nixstore.ParseObjectName(filepath.Base(storePath))
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}

	f, err := os.CreateTemp("", "nixcached-"+objectName.Hash()+"-*.nar.bz2")
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}
	defer func() {
		fname := f.Name()
		f.Close()
		if err := os.Remove(fname); err != nil {
			log.Errorf(ctx, "Unable to clean up %s: %v", fname, err)
		}
	}()

	compressedHashWriter := newHashWriter(nixstore.SHA256, f)
	compressedMD5Writer := newHashWriter(nixstore.MD5, compressedHashWriter)
	bzWriter, err := bzip2.NewWriter(compressedMD5Writer, nil)
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}
	uncompressedHashWriter := newHashWriter(nixstore.SHA256, bzWriter)
	if err := storeClient.DumpPath(ctx, uncompressedHashWriter, storePath); err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}
	if err := bzWriter.Close(); err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}

	// Dump succeeded. Query information about this store path before uploading to bucket.
	queryResult, err := storeClient.Query(ctx, storePath)
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}
	info := queryResult[0]
	if got, want := uncompressedHashWriter.sum(), info.NARHash; got != want {
		return fmt.Errorf("upload %s: uncompressed nar hash %v did not match store-reported hash %v", storePath, got, want)
	}
	if got, want := uncompressedHashWriter.n, info.NARSize; got != want {
		return fmt.Errorf("upload %s: uncompressed nar size %d did not match store-reported size %d", storePath, got, want)
	}
	info.FileHash = compressedHashWriter.sum()
	info.FileSize = compressedHashWriter.n
	info.URL = "nar/" + info.FileHash.RawBase32() + ".nar.bz2"
	info.Compression = nixstore.Bzip2
	narInfoData, err := info.MarshalText()
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}

	// Now that we've succeeded locally, make changes to the bucket.
	// Start with the content so that clients don't observe dangling objects.
	err = destination.Upload(ctx, info.URL, f, &blob.WriterOptions{
		ContentType: "application/x-nix-nar",
		ContentMD5:  compressedMD5Writer.sum().Append(nil),
	})
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}
	err = destination.WriteAll(ctx, objectName.Hash()+".narinfo", narInfoData, &blob.WriterOptions{
		ContentType: "text/x-nix-narinfo",
	})
	if err != nil {
		return fmt.Errorf("upload %s: %v", storePath, err)
	}

	return nil
}

type hashWriter struct {
	w   io.Writer
	typ nixstore.HashType
	h   hash.Hash
	n   int64
}

func newHashWriter(typ nixstore.HashType, w io.Writer) *hashWriter {
	return &hashWriter{
		w:   w,
		typ: typ,
		h:   typ.Hash(),
	}
}

func (w *hashWriter) sum() nixstore.Hash {
	return nixstore.SumHash(w.typ, w.h)
}

func (w *hashWriter) Write(p []byte) (n int, err error) {
	n, err = w.w.Write(p)
	w.h.Write(p[:n])
	w.n += int64(n)
	return
}
