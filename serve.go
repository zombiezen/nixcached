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
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/spf13/cobra"
	"zombiezen.com/go/bass/accept"
	"zombiezen.com/go/bass/action"
	"zombiezen.com/go/bass/runhttp"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/nixcached/internal/nixstore"
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
		log.Debugf(ctx, "Removing %s...", tempDir)
		if err := os.RemoveAll(tempDir); err != nil {
			log.Warnf(ctx, "Clean up: %v", err)
		}
	}()

	var store nixstore.Store
	if strings.HasPrefix(src, "/") {
		dir, err := nix.CleanStoreDirectory(src)
		if err != nil {
			return err
		}
		store = &nixstore.Local{Directory: dir}
	} else {
		opener, err := newBucketURLOpener(ctx)
		if err != nil {
			return err
		}
		bstore, err := nixstore.OpenBucket(ctx, opener, src)
		if err != nil {
			return err
		}
		defer bstore.Close()
		store = bstore
	}

	srv := &http.Server{
		Addr: listenAddr,
		Handler: &storeServer{
			store:     store,
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

type storeServer struct {
	store     nixstore.Store
	resources fs.FS
}

func (srv *storeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	if r.URL.Path == "/"+nix.CacheInfoName {
		handler := cfg.NewHandler(srv.serveCacheInfo)
		handlers.MethodHandler{
			http.MethodGet:  handler,
			http.MethodHead: handler,
		}.ServeHTTP(w, r)
		return
	}

	if isNARInfoPath(r.URL.Path) {
		// If weight of HTML and .narinfo are equal, prefer .narinfo.
		// This makes curl-ing easier.
		oldAcceptString := r.Header.Get("Accept")
		oldAccept, err := accept.ParseHeader(oldAcceptString)
		if err != nil || oldAccept.Quality("text/html", map[string]string{"charset": "utf-8"}) == oldAccept.Quality(nix.NARInfoMIMEType, nil) {
			r = r.Clone(ctx)
			newAccept := accept.Header{
				{Range: nix.NARInfoMIMEType, Quality: 1.0},
				{Range: "*/*", Quality: 0.9},
			}
			newAcceptString := newAccept.String()
			log.Debugf(ctx, "Rewriting .narinfo Accept %q -> %q", oldAcceptString, newAcceptString)
			r.Header.Set("Accept", newAccept.String())
		}

		handler := cfg.NewHandler(srv.serveNARInfo)
		handlers.MethodHandler{
			http.MethodGet:  handler,
			http.MethodHead: handler,
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

	if _, _, ok := parseNARURLPath(r.URL.Path); ok {
		handlers.MethodHandler{
			http.MethodGet:  http.HandlerFunc(srv.serveNAR),
			http.MethodHead: http.HandlerFunc(srv.serveNAR),
		}.ServeHTTP(w, r)
		return
	}

	http.NotFound(w, r)
}

func (srv *storeServer) serveIndex(ctx context.Context, r *http.Request) (*action.Response, error) {
	type expandedNARInfo struct {
		*nix.NARInfo
		ClosureFileSize int64
		ClosureNARSize  int64
	}
	var data struct {
		InitialCrawlComplete bool
		CacheInfo            *nix.CacheInfo
		Infos                []expandedNARInfo
	}

	var err error
	data.CacheInfo, err = srv.store.CacheInfo(ctx)
	if err != nil {
		return nil, err
	}
	iter := srv.store.List(ctx)
	for {
		digest, err := iter.NextDigest(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		info, _, err := srv.store.NARInfo(ctx, digest)
		if err != nil {
			log.Warnf(ctx, "%v", err)
			continue
		}
		info.URL, err = buildNARURL(digest, info.Compression)
		if err != nil {
			log.Warnf(ctx, "%v", err)
			continue
		}
		data.Infos = append(data.Infos, expandedNARInfo{
			NARInfo:         info,
			ClosureFileSize: info.FileSize,
			ClosureNARSize:  info.NARSize,
		})
	}
	sort.Slice(data.Infos, func(i, j int) bool {
		nameI := data.Infos[i].StorePath.Name()
		nameJ := data.Infos[j].StorePath.Name()
		if nameI != nameJ {
			return nameI < nameJ
		}
		return data.Infos[i].StorePath.Digest() < data.Infos[j].StorePath.Digest()
	})

	return &action.Response{
		HTMLTemplate: "index.html",
		TemplateData: data,
	}, nil
}

func (srv *storeServer) serveCacheInfo(ctx context.Context, r *http.Request) (_ *action.Response, err error) {
	info, err := srv.store.CacheInfo(ctx)
	if err != nil {
		return nil, err
	}
	data, err := info.MarshalText()
	if err != nil {
		return nil, err
	}
	return &action.Response{
		Other: []*action.Representation{{
			Header: http.Header{
				"Content-Type":   {nix.CacheInfoMIMEType},
				"Content-Length": {strconv.Itoa(len(data))},
			},
			Body: io.NopCloser(bytes.NewReader(data)),
		}},
	}, nil
}

func (srv *storeServer) serveNARInfo(ctx context.Context, r *http.Request) (*action.Response, error) {
	if !isNARInfoPath(r.URL.Path) {
		return nil, action.ErrNotFound
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), nix.NARInfoExtension)
	info, _, err := srv.store.NARInfo(ctx, digest)
	if errors.Is(err, nixstore.ErrNotFound) {
		return nil, action.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	info.URL, err = buildNARURL(digest, info.Compression)
	if err != nil {
		return nil, err
	}
	data, err := info.MarshalText()
	if err != nil {
		return nil, err
	}
	return &action.Response{
		HTMLTemplate: "narinfo.html",
		TemplateData: info,
		Other: []*action.Representation{{
			Header: http.Header{
				"Content-Type":   {nix.NARInfoMIMEType},
				"Content-Length": {strconv.Itoa(len(data))},
			},
			Body: io.NopCloser(bytes.NewReader(data)),
		}},
	}, nil
}

func (srv *storeServer) serveNAR(w http.ResponseWriter, r *http.Request) {
	// TODO(someday): Respect request cache headers.

	ctx := r.Context()
	digest, compression, ok := parseNARURLPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, infoURL, err := srv.store.NARInfo(ctx, digest)
	if errors.Is(err, nixstore.ErrNotFound) || (err == nil && info.Compression != compression) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Errorf(ctx, "%v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	ref, err := url.Parse(info.URL)
	if err != nil {
		log.Errorf(ctx, "%v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	downloadURL := infoURL.ResolveReference(ref)
	w.Header().Set("Content-Length", strconv.FormatInt(info.FileSize, 10))
	w.Header().Set("Content-Type", compressionMIMEType(nar.MIMEType, compression))
	w.Header().Set("ETag", `"`+info.FileHash.SRI()+`"`)

	if r.Method == http.MethodHead {
		return
	}
	err = srv.store.Download(ctx, w, downloadURL)
	if errors.Is(err, nixstore.ErrNotFound) {
		w.Header().Del("Content-Length")
		w.Header().Del("Content-Type")
		w.Header().Del("ETag")
		http.NotFound(w, r)
		return
	}
}

func isNARInfoPath(path string) bool {
	path = strings.TrimPrefix(path, "/")
	digest, hasExt := strings.CutSuffix(path, nix.NARInfoExtension)
	if !hasExt {
		return false
	}
	p, err := nix.DefaultStoreDirectory.Object(digest + "-x")
	return err == nil && p.Digest() == digest && p.Name() == "x"
}

var compressionExtensions = map[nix.CompressionType]string{
	nix.NoCompression: "",
	nix.Bzip2:         ".bz2",
	nix.Gzip:          ".gz",
	nix.Brotli:        ".br",
}

func parseNARURLPath(path string) (digest string, compression nix.CompressionType, ok bool) {
	tail, ok := strings.CutPrefix(path, "/nar/")
	if !ok {
		return "", "", false
	}
	i := strings.LastIndex(tail, nar.Extension)
	if i < 0 {
		return "", "", false
	}
	digest = tail[:i]
	p, err := nix.DefaultStoreDirectory.Object(digest + "-x")
	if err != nil || p.Digest() != digest || p.Name() != "x" {
		return "", "", false
	}
	ext := tail[i+len(nar.Extension):]
	for c, cext := range compressionExtensions {
		if ext == cext {
			return digest, c, true
		}
	}
	return "", "", false
}

func buildNARURL(digest string, compression nix.CompressionType) (string, error) {
	u := "nar/" + digest + nar.Extension
	ext, ok := compressionExtensions[compression]
	if !ok {
		return "", fmt.Errorf("unsupported compression type %q for %s", compression, digest)
	}
	return u + ext, nil
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
