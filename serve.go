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
	"embed"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/spf13/cobra"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"zombiezen.com/go/bass/action"
	"zombiezen.com/go/bass/runhttp"
	"zombiezen.com/go/log"
)

func newServeCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "serve [flags] URL",
		Short:                 "Run an HTTP server for a bucket",
		Args:                  cobra.ExactArgs(1),
		SilenceErrors:         true,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
	}
	host := c.Flags().String("host", "localhost", "`interface` to listen on")
	port := c.Flags().Uint16P("port", "p", 8080, "`port` to listen on")
	resources := c.Flags().String("resources", "", "`path` to resource files (defaults to using embedded files)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		listenAddr := net.JoinHostPort(*host, strconv.Itoa(int(*port)))
		resourcesFS := fs.FS(embeddedResources)
		if *resources != "" {
			resourcesFS = os.DirFS(*resources)
		}
		return runServe(cmd.Context(), g, listenAddr, resourcesFS, args[0])
	}
	return c
}

func runServe(ctx context.Context, g *globalConfig, listenAddr string, resources fs.FS, src string) error {
	opener, err := newBucketURLOpener(ctx)
	if err != nil {
		return err
	}
	bucket, err := opener.OpenBucket(ctx, src)
	if err != nil {
		return err
	}
	defer bucket.Close()

	srv := &http.Server{
		Addr: listenAddr,
		Handler: &bucketServer{
			bucket:    bucket,
			resources: resources,
		},
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       30 * time.Second,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}
	return runhttp.Serve(ctx, srv, &runhttp.Options{
		OnStartup: func(ctx context.Context, laddr net.Addr) {
			if tcpAddr, ok := laddr.(*net.TCPAddr); ok && tcpAddr.IP.IsLoopback() {
				log.Infof(ctx, "Listening on http://localhost:%d/", tcpAddr.Port)
			} else {
				log.Infof(ctx, "Listening on http://%v/", laddr)
			}
		},
		OnShutdown: func(ctx context.Context) {
			log.Infof(ctx, "Shutting down...")
		},
		OnShutdownError: func(ctx context.Context, err error) {
			log.Errorf(ctx, "Shutdown error: %v", err)
		},
	})
}

// go:embed templates
var embeddedResources embed.FS

type bucketServer struct {
	bucket    *blob.Bucket
	resources fs.FS
}

func (srv *bucketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := &action.Config[*http.Request]{
		MaxRequestSize: 1 << 20, // 1 MiB
		ReportError: func(ctx context.Context, err error) {
			log.Errorf(ctx, "%s %s: %v", r.Method, r.URL.Path, err)
		},
	}
	var err error
	cfg.TemplateFiles, err = fs.Sub(srv.resources, "templates")
	if err != nil {
		log.Errorf(ctx, "Can't get templates: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if r.URL.Path == "/" {
		index := cfg.NewHandler(srv.serveIndex)
		handlers.MethodHandler{
			http.MethodGet:  index,
			http.MethodHead: index,
		}.ServeHTTP(w, r)
		return
	}

	// Fallback: bucket content.
	handlers.MethodHandler{
		http.MethodGet:  http.HandlerFunc(srv.serveContent),
		http.MethodHead: http.HandlerFunc(srv.serveContent),
	}.ServeHTTP(w, r)
}

func (srv *bucketServer) serveIndex(ctx context.Context, r *http.Request) (*action.Response, error) {
	iterator := srv.bucket.List(&blob.ListOptions{
		Delimiter: "/",
	})
	var data struct {
		StoreObjectHashes []string
	}
	for {
		obj, err := iterator.Next(ctx)
		if err != nil {
			if err != io.EOF {
				log.Errorf(ctx, "Listing bucket: %v", err)
			}
			break
		}
		if !obj.IsDir && strings.HasSuffix(obj.Key, ".narinfo") {
			data.StoreObjectHashes = append(data.StoreObjectHashes, obj.Key)
		}
	}

	return &action.Response{
		HTMLTemplate: "index.html",
		TemplateData: data,
	}, nil
}

func (srv *bucketServer) serveContent(w http.ResponseWriter, r *http.Request) {
	// TODO(someday): Respect request cache headers.
	// TODO(someday): Ensure reading consistent generation on GCS.
	// TODO(someday): Range requests.

	ctx := r.Context()
	attr, err := srv.bucket.Attributes(ctx, r.URL.Path)
	if gcerrors.Code(err) == gcerrors.NotFound {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Errorf(ctx, "Unable to query attributes for %s: %v", r.URL.Path, err)
		http.Error(w, "unable to query attributes for "+r.URL.Path, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(attr.Size, 10))
	w.Header().Set("Content-Type", attr.ContentType)
	if attr.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", attr.ContentEncoding)
	}
	if attr.CacheControl != "" {
		w.Header().Set("Cache-Control", attr.CacheControl)
	}
	if !attr.ModTime.IsZero() {
		w.Header().Set("Last-Modified", attr.ModTime.Format(http.TimeFormat))
	}
	if attr.ETag != "" {
		w.Header().Set("ETag", attr.ETag)
	}

	if r.Method == http.MethodHead {
		return
	}
	err = srv.bucket.Download(ctx, r.URL.Path, w, nil)
	if gcerrors.Code(err) == gcerrors.NotFound {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Errorf(ctx, "Unable to read %s: %v", r.URL.Path, err)
		http.Error(w, "unable to read "+r.URL.Path, http.StatusBadGateway)
		return
	}
}
