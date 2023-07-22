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
	"crypto/md5"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	slashpath "path"
	"strings"

	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nixcached/internal/nixstore"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/ext/refunc"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

//go:embed uicache
var uiCacheSQLFiles embed.FS

func uiCacheSchema() sqlitemigration.Schema {
	src, err := fs.ReadFile(uiCacheSQLFiles, "uicache/schema.sql")
	if err != nil {
		panic(err)
	}
	return sqlitemigration.Schema{
		Migrations: []string{string(src)},
	}
}

func prepareUICacheConn(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = on;", nil); err != nil {
		return err
	}

	if err := refunc.Register(conn); err != nil {
		return err
	}
	conn.CreateFunction("md5", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			sum := md5.Sum(args[0].Blob())
			return sqlite.BlobValue(sum[:]), nil
		},
	})
	conn.CreateFunction("path_base", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(slashpath.Base(args[0].Text())), nil
		},
	})
	conn.CreateFunction("store_path_digest", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			p, err := nix.DefaultStoreDirectory.Object(slashpath.Base(args[0].Text()))
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(p.Digest()), nil
		},
	})
	conn.CreateFunction("store_path_name", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			p, err := nix.DefaultStoreDirectory.Object(slashpath.Base(args[0].Text()))
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(p.Name()), nil
		},
	})
	conn.CreateFunction("hash_type", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			h, err := nix.ParseHash(args[0].Text())
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(h.Type().String()), nil
		},
	})
	conn.CreateFunction("hash_bytes", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			h, err := nix.ParseHash(args[0].Text())
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.BlobValue(h.Bytes(nil)), nil
		},
	})
	conn.CreateFunction("signature_name", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			sig, err := nix.ParseSignature(args[0].Text())
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(sig.Name()), nil
		},
	})
	conn.CreateFunction("signature_data", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			sig, err := nix.ParseSignature(args[0].Text())
			if err != nil {
				return sqlite.Value{}, nil
			}
			s, ok := strings.CutPrefix(sig.String(), sig.Name())
			if !ok {
				return sqlite.Value{}, nil
			}
			s, ok = strings.CutPrefix(s, ":")
			if !ok {
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(s), nil
		},
	})
	return nil
}

type uiCacheConn struct {
	conn  *sqlite.Conn
	store nixstore.Store
}

func (c uiCacheConn) CacheInfo(ctx context.Context) (*nix.CacheInfo, error) {
	var dir nix.StoreDirectory
	err := sqlitex.Execute(c.conn, `select "directory" from "store" limit 1;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			var err error
			dir, err = nix.CleanStoreDirectory(stmt.ColumnText(0))
			return err
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read nix cache info: %v", err)
	}
	if dir == "" {
		info, err := c.store.CacheInfo(ctx)
		if err != nil {
			return nil, err
		}
		dir = info.StoreDirectory
		if err := updateCachedStoreDirectory(ctx, c.conn, dir); err != nil {
			log.Errorf(ctx, "Update store cache: %v", err)
		}
	}
	return &nix.CacheInfo{
		StoreDirectory: dir,
		WantMassQuery:  true,
	}, nil
}

func (c uiCacheConn) List(ctx context.Context) nixstore.ListIterator {
	stmt := c.conn.Prep(`select "path_digest" from "store_objects" order by 1;`)
	return uiCacheIterator{stmt}
}

type expandedNARInfo struct {
	*nix.NARInfo
	ClosureFileSize int64
	ClosureNARSize  int64
}

func (c uiCacheConn) cachedList(ctx context.Context) ([]expandedNARInfo, error) {
	var list []expandedNARInfo
	err := sqlitex.ExecuteFS(c.conn, uiCacheSQLFiles, "uicache/index.sql", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			info := expandedNARInfo{NARInfo: new(nix.NARInfo)}
			if err := extractNARInfoFromRow(info.NARInfo, stmt); err != nil {
				return err
			}
			info.ClosureFileSize = stmt.GetInt64("closure_file_size")
			info.ClosureNARSize = stmt.GetInt64("closure_nar_size")
			list = append(list, info)
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list nar info: %w", err)
	}
	return list, nil
}

func (c uiCacheConn) NARInfo(ctx context.Context, storePathDigest string) (*nix.NARInfo, *url.URL, error) {
	// Ensure store directory cached.
	cacheInfo, err := c.CacheInfo(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read nar info for %s: %v", storePathDigest, err)
	}

	// Run single query in automatic transaction.
	var info *nix.NARInfo
	err = sqlitex.ExecuteFS(c.conn, uiCacheSQLFiles, "uicache/narinfo.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":store_path_digest": storePathDigest,
		},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			info = new(nix.NARInfo)
			return extractNARInfoFromRow(info, stmt)
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read nar info for %s: %v", storePathDigest, err)
	}
	if info != nil {
		log.Debugf(ctx, "Using cached data for %s", storePathDigest+nix.NARInfoExtension)
		return info, nil, nil
	}

	log.Debugf(ctx, "No cached result for %s; looking up in underlying store", storePathDigest+nix.NARInfoExtension)
	info, err = c.narInfoFromStore(ctx, cacheInfo, storePathDigest)
	if err != nil {
		return nil, nil, err
	}
	err = func() (err error) {
		endFn, err := sqlitex.ImmediateTransaction(c.conn)
		if err != nil {
			return err
		}
		defer endFn(&err)
		return updateCachedStoreObject(ctx, c.conn, info)
	}()
	if err != nil {
		log.Warnf(ctx, "Failed to cache %s: %v", storePathDigest+nix.NARInfoExtension, err)
	} else {
		log.Debugf(ctx, "Added cache for %s", storePathDigest+nix.NARInfoExtension)
	}
	return info, nil, nil
}

func (c uiCacheConn) narInfoFromStore(ctx context.Context, cacheInfo *nix.CacheInfo, storePathDigest string) (*nix.NARInfo, error) {
	info, baseURL, err := c.store.NARInfo(ctx, storePathDigest)
	if err != nil {
		return nil, err
	}
	if info.StoreDirectory() != cacheInfo.StoreDirectory {
		return nil, fmt.Errorf("store path %q is not inside %s", info.StorePath, cacheInfo.StoreDirectory)
	}
	if baseURL != nil {
		u, err := url.Parse(info.URL)
		if err != nil {
			return nil, fmt.Errorf("read nar info for %s: %v", storePathDigest, err)
		}
		info.URL = baseURL.ResolveReference(u).String()
	}
	return info, nil
}

func extractNARInfoFromRow(info *nix.NARInfo, stmt *sqlite.Stmt) error {
	var err error
	info.StorePath, err = nix.ParseStorePath(stmt.GetText("store_path"))
	if err != nil {
		return err
	}
	info.URL = stmt.GetText("url")
	info.Compression = nix.CompressionType(stmt.GetText("compression"))
	if !info.Compression.IsKnown() {
		return fmt.Errorf("unknown compression %q", info.Compression)
	}
	if s := stmt.GetText("file_hash"); s != "" {
		info.FileHash, err = nix.ParseHash(s)
		if err != nil {
			return fmt.Errorf("file hash: %w", err)
		}
	}
	info.FileSize = stmt.GetInt64("file_size")
	if s := stmt.GetText("nar_hash"); s != "" {
		info.NARHash, err = nix.ParseHash(s)
		if err != nil {
			return fmt.Errorf("nar hash: %w", err)
		}
	}
	info.NARSize = stmt.GetInt64("nar_size")
	buf := make([]byte, stmt.GetLen("references"))
	stmt.GetBytes("references", buf)
	if err := json.Unmarshal(buf, &info.References); err != nil {
		return fmt.Errorf("references: %w", err)
	}
	if s := stmt.GetText("deriver"); s != "" {
		info.Deriver, err = nix.ParseStorePath(s)
		if err != nil {
			return fmt.Errorf("deriver: %w", err)
		}
	}
	if n := stmt.GetLen("signatures"); n > len(buf) {
		buf = make([]byte, n)
	} else {
		buf = buf[:n]
	}
	stmt.GetBytes("signatures", buf)
	if err := json.Unmarshal(buf, &info.Sig); err != nil {
		return fmt.Errorf("signatures: %w", err)
	}
	if s := stmt.GetText("ca"); s != "" {
		info.CA, err = nix.ParseContentAddress(s)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c uiCacheConn) Download(ctx context.Context, w io.Writer, u *url.URL) error {
	return c.store.Download(ctx, w, u)
}

type uiCacheIterator struct {
	stmt *sqlite.Stmt
}

func (iter uiCacheIterator) NextDigest(ctx context.Context) (string, error) {
	hasRow, err := iter.stmt.Step()
	if err != nil {
		return "", err
	}
	if !hasRow {
		return "", io.EOF
	}
	return iter.stmt.ColumnText(0), nil
}

func (iter uiCacheIterator) Close() error {
	err1 := iter.stmt.Reset()
	err2 := iter.stmt.ClearBindings()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return nil
}

func (c uiCacheConn) discover(ctx context.Context) int64 {
	// Ensure store directory cached.
	cacheInfo, err := c.CacheInfo(ctx)
	if err != nil {
		log.Errorf(ctx, "During discovery: %v", err)
		return 0
	}

	batch := make([]*nix.NARInfo, 0, 200)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		endFn, err := sqlitex.ImmediateTransaction(c.conn)
		if err != nil {
			log.Errorf(ctx, "Could not open cache for writing: %v", err)
			return false
		}
		for i, info := range batch {
			if err := updateCachedStoreObject(ctx, c.conn, info); err != nil {
				log.Errorf(ctx, "%v", err)
			}
			batch[i] = nil
		}
		endFn(&err)
		batch = batch[:0]
		if err != nil {
			log.Errorf(ctx, "Could not save to cache: %v", err)
			return false
		}
		return true
	}

	var n int64
	for iter := c.store.List(ctx); ; {
		storePathDigest, err := iter.NextDigest(ctx)
		if err != nil {
			if err != io.EOF {
				log.Errorf(ctx, "During discovery: %v", err)
			}
			flush()
			return n
		}
		n++
		if err := ctx.Err(); err != nil {
			return n
		}

		// Run single query in automatic transaction.
		var exists bool
		err = sqlitex.Execute(c.conn, `select exists(select 1 from "store_objects" where "path_digest" = ?);`, &sqlitex.ExecOptions{
			Args: []any{storePathDigest},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				exists = stmt.ColumnBool(0)
				return nil
			},
		})
		if err != nil {
			log.Errorf(ctx, "Querying cache for %s during discovery: %v", storePathDigest+nix.NARInfoExtension, err)
			continue
		}
		if exists {
			log.Debugf(ctx, "Rediscovered %s (already present in cache)", storePathDigest+nix.NARInfoExtension)
			continue
		}

		log.Debugf(ctx, "Discovered %s", storePathDigest+nix.NARInfoExtension)
		info, err := c.narInfoFromStore(ctx, cacheInfo, storePathDigest)
		if err != nil {
			log.Warnf(ctx, "Failed to cache %s: %v", storePathDigest+nix.NARInfoExtension, err)
			continue
		}
		batch = append(batch, info)
		if len(batch) == cap(batch) {
			if !flush() {
				return n
			}
		}
	}
}

func updateCachedStoreDirectory(ctx context.Context, conn *sqlite.Conn, storeDir nix.StoreDirectory) (err error) {
	endFn, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return fmt.Errorf("cache store directory: %v", err)
	}
	defer endFn(&err)

	if err := sqlitex.ExecuteTransient(conn, `delete from "store";`, nil); err != nil {
		return fmt.Errorf("clear cached store directory: %v", err)
	}
	err = sqlitex.ExecuteTransient(conn, `insert into "store" ("directory") values (?);`, &sqlitex.ExecOptions{
		Args: []any{storeDir},
	})
	if err != nil {
		return fmt.Errorf("cache store directory: %v", err)
	}
	return nil
}

func updateCachedStoreObject(ctx context.Context, conn *sqlite.Conn, info *nix.NARInfo) (err error) {
	release := sqlitex.Save(conn)
	defer func() {
		release(&err)
		if err != nil {
			err = fmt.Errorf("update %v cache: %v", info.StorePath.Digest()+nix.NARInfoExtension, err)
		}
	}()

	references := []byte("[]")
	if len(info.References) > 0 {
		var err error
		references, err = json.Marshal(info.References)
		if err != nil {
			return fmt.Errorf("references: %v", err)
		}
	}
	signatures := []byte("[]")
	if len(info.Sig) > 0 {
		var err error
		signatures, err = json.Marshal(info.Sig)
		if err != nil {
			return fmt.Errorf("references: %v", err)
		}
	}
	var caString string
	if !info.CA.IsZero() {
		caString = info.CA.String()
	}
	err = sqlitex.ExecuteScriptFS(conn, uiCacheSQLFiles, "uicache/upsert_object.sql", &sqlitex.ExecOptions{
		Named: map[string]any{
			":store_path":  string(info.StorePath),
			":url":         info.URL,
			":compression": string(info.Compression),
			":file_hash":   info.FileHash.String(),
			":file_size":   info.FileSize,
			":nar_hash":    info.NARHash.String(),
			":nar_size":    info.NARSize,
			":references":  references,
			":deriver":     string(info.Deriver),
			":signatures":  signatures,
			":ca":          caString,
		},
	})
	if err != nil {
		return err
	}

	return nil
}
