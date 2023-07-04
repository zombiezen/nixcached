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
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
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
	"zombiezen.com/go/nixcached/internal/nixstore"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
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
	crawlFrequency := c.Flags().Duration("crawl-frequency", 30*time.Second, "minimum `duration` of time between starting crawls")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		listenAddr := net.JoinHostPort(*host, strconv.Itoa(int(*port)))
		resourcesFS := fs.FS(embeddedResources)
		if *resources != "" {
			resourcesFS = os.DirFS(*resources)
		}
		return runServe(cmd.Context(), g, listenAddr, resourcesFS, args[0], *crawlFrequency)
	}
	return c
}

func runServe(ctx context.Context, g *globalConfig, listenAddr string, resources fs.FS, src string, crawlFrequency time.Duration) error {
	tempDir, err := os.MkdirTemp("", "nixcached-serve-*")
	if err != nil {
		return err
	}
	log.Debugf(ctx, "Created %s", tempDir)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			log.Warnf(ctx, "Clean up: %v", err)
		}
	}()
	cachePool := sqlitemigration.NewPool(filepath.Join(tempDir, "cache.db"), uiCacheSchema, sqlitemigration.Options{
		OnStartMigrate: func() {
			log.Debugf(ctx, "Cache database migration starting...")
		},
		OnReady: func() {
			log.Debugf(ctx, "Cache database ready")
		},
		OnError: func(err error) {
			log.Errorf(ctx, "Cache setup: %v", err)
		},
		PrepareConn: prepareUICacheConn,
	})
	defer cachePool.Close()

	opener, err := newBucketURLOpener(ctx)
	if err != nil {
		return err
	}
	bucket, err := opener.OpenBucket(ctx, src)
	if err != nil {
		return err
	}
	defer bucket.Close()

	crawlCtx, cancelCrawl := context.WithCancel(ctx)
	crawlDone := make(chan struct{})
	go func() {
		defer close(crawlDone)
		ticker := time.NewTicker(crawlFrequency)
		defer ticker.Stop()
		for {
			conn, err := cachePool.Get(crawlCtx)
			if err != nil {
				return
			}
			crawl(crawlCtx, conn, bucket)
			cachePool.Put(conn)

			select {
			case <-ticker.C:
			case <-crawlCtx.Done():
				return
			}
		}
	}()
	defer func() {
		log.Debugf(ctx, "Shutting down crawl...")
		cancelCrawl()
		<-crawlDone
		log.Debugf(ctx, "Crawl shutdown complete")
	}()

	srv := &http.Server{
		Addr: listenAddr,
		Handler: &bucketServer{
			bucket:    bucket,
			cache:     cachePool,
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

//go:embed static
//go:embed templates
var embeddedResources embed.FS

type bucketServer struct {
	bucket    *blob.Bucket
	cache     *sqlitemigration.Pool
	resources fs.FS
}

func (srv *bucketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := &action.Config[*http.Request]{
		MaxRequestSize: 1 << 20, // 1 MiB
		ReportError: func(ctx context.Context, err error) {
			log.Errorf(ctx, "%s %s: %v", r.Method, r.URL.Path, err)
		},
		TemplateFuncs: template.FuncMap{
			"size": formatSize,
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

	if r.URL.Path == "/"+nixstore.CacheInfoName {
		index := cfg.NewHandler(srv.serveCacheInfo)
		handlers.MethodHandler{
			http.MethodGet:  index,
			http.MethodHead: index,
		}.ServeHTTP(w, r)
		return
	}

	const staticPrefix = "/_/"
	if strings.HasPrefix(r.URL.Path, staticPrefix) {
		static, err := fs.Sub(srv.resources, "static")
		if err != nil {
			log.Errorf(ctx, "Can't get static: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		http.StripPrefix(staticPrefix, http.FileServer(http.FS(static))).ServeHTTP(w, r)
		return
	}

	// Fallback: bucket content.
	handlers.MethodHandler{
		http.MethodGet:  http.HandlerFunc(srv.serveContent),
		http.MethodHead: http.HandlerFunc(srv.serveContent),
	}.ServeHTTP(w, r)
}

//go:embed uicache_index.sql
var indexQuery string

func (srv *bucketServer) serveIndex(ctx context.Context, r *http.Request) (_ *action.Response, err error) {
	conn, err := srv.cache.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer srv.cache.Put(conn)
	defer sqlitex.Transaction(conn)(&err)

	type expandedNARInfo struct {
		*nixstore.NARInfo
		ClosureFileSize int64
		ClosureNARSize  int64
	}
	var data struct {
		InitialCrawlComplete bool
		Configuration        *nixstore.Configuration
		Infos                []expandedNARInfo
	}
	err = sqlitex.Execute(conn, `select "initial_crawl_complete" from "uicache_status";`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			data.InitialCrawlComplete = stmt.ColumnBool(0)
			return nil
		},
	})
	if err != nil {
		return nil, err
	}

	if data.InitialCrawlComplete {
		data.Configuration, _, err = readCachedConfiguration(ctx, conn)
		if err != nil {
			log.Warnf(ctx, "%v", err)
			data.Configuration = new(nixstore.Configuration)
		}
		if data.Configuration.StoreDir == "" {
			data.Configuration.StoreDir = nixstore.DefaultDirectory
		}
		// To match served version:
		data.Configuration.WantMassQuery = true

		var buf []byte
		err = sqlitex.Execute(conn, strings.TrimSpace(indexQuery), &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				if n := stmt.GetLen("narinfo"); n > cap(buf) {
					buf = make([]byte, n)
				} else {
					buf = buf[:n]
				}
				stmt.GetBytes("narinfo", buf)
				info := new(nixstore.NARInfo)
				if err := info.UnmarshalText(buf); err != nil {
					log.Warnf(ctx, "Unable to parse %s: %v", stmt.GetText("hash")+nixstore.NARInfoExtension, buf)
					return nil
				}
				data.Infos = append(data.Infos, expandedNARInfo{
					NARInfo:         info,
					ClosureFileSize: stmt.GetInt64("closure_file_size"),
					ClosureNARSize:  stmt.GetInt64("closure_nar_size"),
				})
				return nil
			},
		})
		if err != nil {
			return nil, err
		}
	}

	return &action.Response{
		HTMLTemplate: "index.html",
		TemplateData: data,
	}, nil
}

func (srv *bucketServer) serveCacheInfo(ctx context.Context, r *http.Request) (_ *action.Response, err error) {
	conn, err := srv.cache.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer srv.cache.Put(conn)
	defer sqlitex.Transaction(conn)(&err)

	cfg, _, err := readCachedConfiguration(ctx, conn)
	if err != nil {
		return nil, err
	}
	cfg.WantMassQuery = true
	data, err := cfg.MarshalText()
	if err != nil {
		return nil, err
	}

	return &action.Response{
		Other: []*action.Representation{{
			Header: http.Header{
				"Content-Type":   {nixstore.CacheInfoMIMEType},
				"Content-Length": {strconv.Itoa(len(data))},
			},
			Body: io.NopCloser(bytes.NewReader(data)),
		}},
	}, nil
}

func (srv *bucketServer) serveContent(w http.ResponseWriter, r *http.Request) {
	// TODO(someday): Respect request cache headers.
	// TODO(someday): Ensure reading consistent generation on GCS.
	// TODO(someday): Range requests.

	ctx := r.Context()
	key := strings.TrimPrefix(r.URL.Path, "/")
	attr, err := srv.bucket.Attributes(ctx, key)
	if gcerrors.Code(err) == gcerrors.NotFound {
		http.Error(w, "Object "+key+" not found in bucket", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Errorf(ctx, "Unable to query attributes for %s: %v", key, err)
		http.Error(w, "unable to query attributes for "+key, http.StatusBadGateway)
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
	err = srv.bucket.Download(ctx, key, w, nil)
	if gcerrors.Code(err) == gcerrors.NotFound {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Errorf(ctx, "Unable to read %s: %v", key, err)
		http.Error(w, "unable to read "+key, http.StatusBadGateway)
		return
	}
}

func formatSize(v reflect.Value) (string, error) {
	var x float64
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		x = float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		x = float64(v.Uint())
	default:
		return "", fmt.Errorf("cannot use formatSize on %v", v.Type())
	}

	abs := x
	if x < 0 {
		abs = -x
	}
	switch {
	case abs == 1:
		return fmt.Sprintf("%d byte", int16(x)), nil
	case x < 1<<9:
		return fmt.Sprintf("%d bytes", int16(x)), nil
	case x < 1<<20:
		return fmt.Sprintf("%.1f KiB", x/(1<<10)), nil
	case x < 1<<30:
		return fmt.Sprintf("%.1f MiB", x/(1<<20)), nil
	case x < 1<<40:
		return fmt.Sprintf("%.1f GiB", x/(1<<30)), nil
	default:
		return fmt.Sprintf("%.1f TiB", x/(1<<40)), nil
	}
}
