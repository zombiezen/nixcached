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
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dsnet/compress/bzip2"
	"github.com/spf13/cobra"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
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
	overwrite := c.Flags().BoolP("force", "f", false, "replace already-uploaded store objects")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		storeDir, err := nix.StoreDirectoryFromEnv()
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
		return runUpload(cmd.Context(), g, args[0], *overwrite, input, storeDir)
	}
	return c
}

func runUpload(ctx context.Context, g *globalConfig, destinationBucketURL string, overwrite bool, input io.Reader, storeDir nix.StoreDirectory) error {
	opener, err := newBucketURLOpener(ctx)
	if err != nil {
		return err
	}
	bucket, err := opener.OpenBucket(ctx, destinationBucketURL)
	if err != nil {
		return err
	}
	defer bucket.Close()
	if err := ensureCacheInfo(ctx, bucket, storeDir); err != nil {
		return err
	}

	storeClient := &nixstore.Client{
		Log: os.Stderr,
	}
	processDone := make(chan struct{})
	storePaths := make(chan nix.StorePath)
	processCtx, cancelProcess := context.WithCancel(ctx)
	defer func() {
		cancelProcess()
		<-processDone
	}()
	go func() {
		defer close(processDone)
		processUploads(processCtx, storeClient, bucket, overwrite, storePaths)
	}()

	scanner := bufio.NewScanner(input)
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

		rawStorePath := scanner.Text()
		log.Debugf(ctx, "Received %q as input", rawStorePath)
		storePath, sub, err := storeDir.ParsePath(rawStorePath)
		if err != nil {
			log.Warnf(ctx, "Received invalid store path %q, skipping: %v", rawStorePath, err)
			continue
		}
		if sub != "" {
			log.Warnf(ctx, "Received path %q with subpath, skipping.", rawStorePath)
			continue
		}
		if storePath.IsDerivation() {
			log.Warnf(ctx, "Received store derivation path %q, skipping.", rawStorePath)
			continue
		}
		select {
		case storePaths <- storePath:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	close(storePaths)
	<-processDone
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func ensureCacheInfo(ctx context.Context, bucket *blob.Bucket, localStoreDir nix.StoreDirectory) (err error) {
	log.Debugf(ctx, "Verifying %s matches store path...", nix.CacheInfoName)
	cacheInfoData, err := bucket.ReadAll(ctx, nix.CacheInfoName)
	if gcerrors.Code(err) == gcerrors.NotFound {
		log.Debugf(ctx, "%s does not exist (will upload): %v", nix.CacheInfoName, err)
		cacheInfoData, err = (&nix.CacheInfo{StoreDirectory: localStoreDir}).MarshalText()
		if err != nil {
			return fmt.Errorf("create %s: %v", nix.CacheInfoName, err)
		}
		err = bucket.WriteAll(ctx, nix.CacheInfoName, cacheInfoData, &blob.WriterOptions{
			ContentType: nix.CacheInfoMIMEType,
		})
		if err != nil {
			return fmt.Errorf("create %s: %v", nix.CacheInfoName, err)
		}
		log.Infof(ctx, "Wrote %s with store directory %s", nix.CacheInfoName, localStoreDir)
		return nil
	}
	if err != nil {
		return fmt.Errorf("get %s: %v", nix.CacheInfoName, err)
	}

	info := new(nix.CacheInfo)
	if err := info.UnmarshalText(cacheInfoData); err != nil {
		return fmt.Errorf("get %s: %v", nix.CacheInfoName, err)
	}
	remoteStoreDir := info.StoreDirectory
	if remoteStoreDir == "" {
		remoteStoreDir = nix.DefaultStoreDirectory
	}
	if remoteStoreDir != localStoreDir {
		return fmt.Errorf("%s: bucket uses store directory %q (expecting %q)", nix.CacheInfoName, remoteStoreDir, localStoreDir)
	}
	return nil
}

// processUploads performs the uploads requested from a channel of store paths.
// It is assumed that the store paths have been validated before being sent on the channel.
func processUploads(ctx context.Context, storeClient *nixstore.Client, destination *blob.Bucket, overwrite bool, storePaths <-chan nix.StorePath) {
	storePathBatch := make([]nix.StorePath, 0, 100)

	for {
		storePathBatch = storePathBatch[:0]
		select {
		case p, ok := <-storePaths:
			if !ok {
				return
			}
			storePathBatch = append(storePathBatch, p)
		case <-ctx.Done():
			return
		}
		// Wait a short amount of time to batch up more store paths.
		t := time.NewTimer(500 * time.Millisecond)
	batchPhase:
		for len(storePathBatch) < cap(storePathBatch) {
			select {
			case p, ok := <-storePaths:
				if !ok {
					break batchPhase
				}
				storePathBatch = append(storePathBatch, p)
			case <-t.C:
				break batchPhase
			case <-ctx.Done():
				t.Stop()
				return
			}
		}
		t.Stop()

		log.Debugf(ctx, "Have batch: %q", storePathBatch)
		set := gatherStorePathSet(ctx, storeClient, storePathBatch)

		// TODO(soon): Make concurrent.
		for _, info := range set {
			log.Infof(ctx, "Uploading %s...", info.StorePath)
			if err := uploadStorePath(ctx, storeClient, destination, info, overwrite); err != nil {
				log.Errorf(ctx, "Failed: %v", err)
				continue
			}
		}
	}
}

// gatherStorePathSet finds information about the transitive closure of store objects
// starting at the given store paths.
// Upon encountering an error, gatherStorePathSet will log the error
// and attempt to gather as much information about other store paths as possible.
func gatherStorePathSet(ctx context.Context, storeClient *nixstore.Client, storePaths []nix.StorePath) []*nix.NARInfo {
	// First: attempt to batch up all store paths.
	queryArgs := make([]string, len(storePaths))
	for i, p := range storePaths {
		queryArgs[i] = string(p)
	}
	result, err := storeClient.QueryRecursive(ctx, queryArgs...)
	if err == nil {
		return result
	} else {
		log.Warnf(ctx, "While gathering store path information: %v", err)
	}

	// Otherwise, we will try each of the store paths individually.
	result = nil
	visited := make(map[nix.StorePath]struct{})
	for _, p := range storePaths {
		if _, found := visited[p]; found {
			continue
		}
		individualResults, err := storeClient.QueryRecursive(ctx, string(p))
		if err != nil {
			log.Warnf(ctx, "While gathering store path information: %v", err)
			continue
		}
		for _, info := range individualResults {
			if _, found := visited[info.StorePath]; found {
				// Deduplicate across multiple calls to QueryRecursive.
				continue
			}
			visited[info.StorePath] = struct{}{}
			if info.IsValid() {
				result = append(result, info)
			}
		}
	}
	return result
}

func uploadStorePath(ctx context.Context, storeClient *nixstore.Client, destination *blob.Bucket, info *nix.NARInfo, overwrite bool) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("upload %s: %v", info.StorePath, err)
		}
	}()

	narInfoKey := info.StorePath.Digest() + nix.NARInfoExtension
	if !overwrite {
		err := checkUploadOverwrite(ctx, destination, narInfoKey, info)
		if errors.Is(err, errStoreObjectExists) {
			log.Infof(ctx, "Skipping %s: %v", info.StorePath, err)
			return nil
		}
		if err != nil {
			return err
		}
	}

	f, err := os.CreateTemp("", "nixcached-"+info.StorePath.Digest()+"-*.nar.bz2")
	if err != nil {
		return err
	}
	defer func() {
		fname := f.Name()
		f.Close()
		if rmErr := os.Remove(fname); rmErr != nil {
			log.Errorf(ctx, "Unable to clean up %s: %v", fname, rmErr)
		}
	}()

	compressedHashWriter := newHashWriter(nix.SHA256, f)
	compressedMD5Writer := newHashWriter(nix.MD5, compressedHashWriter)
	bzWriter, err := bzip2.NewWriter(compressedMD5Writer, nil)
	if err != nil {
		return fmt.Errorf("upload %s: %v", info.StorePath, err)
	}
	uncompressedHashWriter := newHashWriter(nix.SHA256, bzWriter)
	if err := nar.DumpPath(uncompressedHashWriter, string(info.StorePath)); err != nil {
		return fmt.Errorf("upload %s: %v", info.StorePath, err)
	}
	if err := bzWriter.Close(); err != nil {
		return fmt.Errorf("upload %s: %v", info.StorePath, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("upload %s: %v", info.StorePath, err)
	}

	// Dump succeeded. Compare information about this store path
	// to what we had queried before uploading to bucket.
	if got, want := uncompressedHashWriter.sum(), info.NARHash; !got.Equal(want) {
		return fmt.Errorf("upload %s: uncompressed nar hash %v did not match store-reported hash %v", info.StorePath, got, want)
	}
	if got, want := uncompressedHashWriter.n, info.NARSize; got != want {
		return fmt.Errorf("upload %s: uncompressed nar size %d did not match store-reported size %d", info.StorePath, got, want)
	}
	info = info.Clone()
	info.FileHash = compressedHashWriter.sum()
	info.FileSize = compressedHashWriter.n
	info.URL = "nar/" + info.FileHash.RawBase32() + ".nar.bz2"
	info.Compression = nix.Bzip2
	narInfoData, err := info.MarshalText()
	if err != nil {
		return fmt.Errorf("upload %s: %v", info.StorePath, err)
	}

	// Now that we've succeeded locally, make changes to the bucket.
	// Start with the content so that clients don't observe dangling objects.
	err = destination.Upload(ctx, info.URL, f, &blob.WriterOptions{
		ContentType:     "application/x-nix-nar",
		ContentEncoding: "bzip2",
		ContentMD5:      compressedMD5Writer.sum().Bytes(nil),
	})
	if err != nil {
		return fmt.Errorf("upload %s: %v", info.StorePath, err)
	}
	err = destination.WriteAll(ctx, narInfoKey, narInfoData, &blob.WriterOptions{
		ContentType: nix.NARInfoMIMEType,
	})
	if err != nil {
		return fmt.Errorf("upload %s: %v", info.StorePath, err)
	}

	return nil
}

// checkUploadOverwrite determines whether there is
// an existing .narinfo file with the given key.
// If not, then checkUploadOverwrite returns nil.
// If the file already exists, the NAR content is the same,
// and the new info has signatures not present in the old one,
// checkUploadOverwrite will attempt to upload
// a new .narinfo file with the union of the signatures.
// Regardless, if the file already exists,
// checkUploadOverwrite will return errStoreObjectExists.
func checkUploadOverwrite(ctx context.Context, destination *blob.Bucket, narInfoKey string, localInfo *nix.NARInfo) error {
	oldNARInfoData, err := destination.ReadAll(ctx, narInfoKey)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil
	}
	if err != nil {
		return err
	}
	oldInfo := new(nix.NARInfo)
	if err := oldInfo.UnmarshalText(oldNARInfoData); err != nil {
		log.Debugf(ctx, "%s exists for %s, but can't parse file: %v", narInfoKey, localInfo.StorePath, err)
		return errStoreObjectExists
	}
	if !oldInfo.NARHash.Equal(localInfo.NARHash) || oldInfo.NARSize != localInfo.NARSize {
		log.Debugf(ctx, "%s exists for %s, but has hash %v (have %v locally)", narInfoKey, localInfo.StorePath, oldInfo.NARHash, localInfo.NARHash)
		return errStoreObjectExists
	}
	// Hashes match. Let's upload new signatures, if any.
	oldSigLen := len(oldInfo.Sig)
	oldInfo.AddSignatures(localInfo.Sig...)
	if len(oldInfo.Sig) <= oldSigLen {
		log.Debugf(ctx, "%s exists for %s; no signatures to add", narInfoKey, localInfo.StorePath)
		return errStoreObjectExists
	}
	log.Debugf(ctx, "Going to add signatures to %s for %s: %q", narInfoKey, localInfo.StorePath, oldInfo.Sig[oldSigLen:])
	newNARInfoData, err := oldInfo.MarshalText()
	if err != nil {
		log.Debugf(ctx, "Updating %s for %s: %v", narInfoKey, localInfo.StorePath, err)
		return errStoreObjectExists
	}
	// TODO(someday): Read-modify-write semantics on GCS.
	err = destination.WriteAll(ctx, narInfoKey, newNARInfoData, &blob.WriterOptions{
		ContentType: nix.NARInfoMIMEType,
	})
	if err != nil {
		log.Warnf(ctx, "Attempt to update signatures for %s: %v", localInfo.StorePath, err)
	}
	return errStoreObjectExists
}

var errStoreObjectExists = errors.New("store object already present in bucket")

type hashWriter struct {
	w io.Writer
	h *nix.Hasher
	n int64
}

func newHashWriter(typ nix.HashType, w io.Writer) *hashWriter {
	return &hashWriter{
		w: w,
		h: nix.NewHasher(typ),
	}
}

func (w *hashWriter) sum() nix.Hash {
	return w.h.SumHash()
}

func (w *hashWriter) Write(p []byte) (n int, err error) {
	n, err = w.w.Write(p)
	w.h.Write(p[:n])
	w.n += int64(n)
	return
}
