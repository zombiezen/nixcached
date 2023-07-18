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
	"crypto/md5"
	_ "embed"
	"fmt"
	"io"
	slashpath "path"
	"strings"

	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/ext/refunc"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

//go:embed uicache_schema.sql
var uiCacheSchemaSource string

var uiCacheSchema = sqlitemigration.Schema{
	Migrations: []string{uiCacheSchemaSource},
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
	conn.CreateFunction("store_path_hash", &sqlite.FunctionImpl{
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
	return nil
}

// crawl iterates over a bucket's .narinfo files
// and downloads them and indexes them.
func crawl(ctx context.Context, conn *sqlite.Conn, bucket *blob.Bucket) {
	endFn, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		log.Errorf(ctx, "Crawl failed to begin transaction: %v", err)
		return
	}
	defer func() {
		var err error
		endFn(&err)
		if err != nil {
			log.Errorf(ctx, "Crawl failed to commit to cache: %v", err)
		} else {
			log.Infof(ctx, "Crawl completed.")
		}
	}()

	log.Infof(ctx, "Starting crawl...")
	err = sqlitex.ExecuteTransient(conn, `update "uicache_status" set "initial_crawl_complete" = true;`, nil)
	if err != nil {
		log.Errorf(ctx, "Could not mark initial crawl (aborting): %v", err)
		return
	}

	// Read /nix-cache-info first.
	storeDir := nix.DefaultStoreDirectory
	if data, err := bucket.ReadAll(ctx, nix.CacheInfoName); err == nil {
		cacheInfo := new(nix.CacheInfo)
		if err := cacheInfo.UnmarshalText(data); err != nil {
			log.Warnf(ctx, "Invalid %s in bucket: %v", nix.CacheInfoName, err)
		} else if cacheInfo.StoreDirectory != "" {
			storeDir = cacheInfo.StoreDirectory
		}

		if data == nil { // nil data is treated as not present.
			data = []byte{}
		}
		if err := updateCacheInfoCache(ctx, conn, data); err != nil {
			log.Warnf(ctx, "Caching %s: %v", nix.CacheInfoMIMEType, err)
		}
	} else if gcerrors.Code(err) == gcerrors.NotFound {
		log.Debugf(ctx, "Bucket does not have a %s file", nix.CacheInfoName)
		if err := updateCacheInfoCache(ctx, conn, nil); err != nil {
			log.Warnf(ctx, "Caching %s 404: %v", nix.CacheInfoMIMEType, err)
		}
	} else {
		log.Warnf(ctx, "Could not read %s: %v", nix.CacheInfoName, err)
	}

	// Read NAR metadata.
	iterator := bucket.List(&blob.ListOptions{
		Delimiter: "/",
	})
	for {
		obj, err := iterator.Next(ctx)
		if err != nil {
			if err != io.EOF {
				log.Errorf(ctx, "Listing bucket for crawl: %v", err)
			}
			break
		}
		digest, hasExt := strings.CutSuffix(obj.Key, nix.NARInfoExtension)
		if obj.IsDir || !hasExt {
			log.Debugf(ctx, "Ignoring %q during crawl", obj.Key)
			continue
		}
		if !isNARInfoPath(obj.Key) {
			log.Warnf(ctx, "Ignoring improperly named %q", obj.Key)
			continue
		}

		log.Debugf(ctx, "Found %s during crawl", obj.Key)
		err = sqlitex.Execute(conn, `insert or ignore into "nar_infos" ("hash") values (?);`, &sqlitex.ExecOptions{
			Args: []any{digest},
		})
		if err != nil {
			log.Errorf(ctx, "Inserting %s into cache: %v", obj.Key, err)
			continue
		}
		var cachedSize int64 = -1
		var cachedMD5 []byte
		err = sqlitex.Execute(
			conn,
			`select coalesce(length("narinfo"), -1) as "narinfo_length", `+
				`md5("narinfo") as "narinfo_md5" `+
				`from "nar_infos" where "hash" = :hash;`,
			&sqlitex.ExecOptions{
				Named: map[string]any{
					":hash": digest,
				},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					cachedSize = stmt.GetInt64("narinfo_length")
					cachedMD5 = make([]byte, stmt.GetLen("narinfo_md5"))
					stmt.GetBytes("narinfo_md5", cachedMD5)
					return nil
				},
			},
		)
		if err != nil {
			log.Errorf(ctx, "Unable to check cache for %s: %v", obj.Key, err)
		} else if cachedSize == obj.Size && (len(obj.MD5) == 0 || bytes.Equal(obj.MD5, cachedMD5)) {
			log.Debugf(ctx, "Crawl cache for %s up-to-date", obj.Key)
			continue
		}

		data, err := bucket.ReadAll(ctx, obj.Key)
		if err != nil {
			log.Errorf(ctx, "Reading %s: %v", obj.Key, err)
			continue
		}
		if err := updateNARInfoCache(ctx, conn, storeDir, digest, data); err != nil {
			log.Errorf(ctx, "Unable to update cache for %s: %v", obj.Key, err)
			continue
		}
	}
}

func readCachedCacheInfo(ctx context.Context, conn *sqlite.Conn) (info *nix.CacheInfo, raw []byte, err error) {
	err = sqlitex.Execute(conn, `select "nix_cache_info" from "nix_cache_info" limit 1;`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			raw = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, raw)
			return nil
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read cached %s: %w", nix.CacheInfoName, err)
	}
	info = new(nix.CacheInfo)
	if err := info.UnmarshalText(raw); err != nil {
		log.Warnf(ctx, "Invalid %s in bucket: %v", nix.CacheInfoName, err)
		*info = nix.CacheInfo{}
	}
	return info, raw, nil
}

func updateCacheInfoCache(ctx context.Context, conn *sqlite.Conn, data []byte) (err error) {
	defer sqlitex.Save(conn)(&err)

	if err := sqlitex.ExecuteTransient(conn, `delete from "nix_cache_info";`, nil); err != nil {
		return fmt.Errorf("clear nix_cache_info: %v", err)
	}
	if data == nil {
		return nil
	}
	err = sqlitex.ExecuteTransient(conn, `insert into "nix_cache_info" ("nix_cache_info") values (?);`, &sqlitex.ExecOptions{
		Args: []any{data},
	})
	if err != nil {
		return fmt.Errorf("store %s: %v", nix.CacheInfoName, err)
	}
	log.Debugf(ctx, "Cached %d-byte %s file", len(data), nix.CacheInfoName)
	return nil
}

func updateNARInfoCache(ctx context.Context, conn *sqlite.Conn, storeDir nix.StoreDirectory, hash string, data []byte) (err error) {
	key := hash + nix.NARInfoExtension
	defer sqlitex.Save(conn)(&err)

	err = sqlitex.Execute(conn, `insert or ignore into "nar_infos" ("hash") values (?);`, &sqlitex.ExecOptions{
		Args: []any{hash},
	})
	if err != nil {
		return fmt.Errorf("update %s cache: add row: %v", key, err)
	}

	// Reset the parsed fields and set the raw data.
	const clearQuery = `update "nar_infos" set "narinfo" = :narinfo, ` +
		`"store_path" = null, "file_size" = null, "nar_size" = null ` +
		`where "hash" = :hash;`
	err = sqlitex.Execute(conn, clearQuery, &sqlitex.ExecOptions{
		Named: map[string]any{
			":hash":    hash,
			":narinfo": data,
		},
	})
	if err != nil {
		return fmt.Errorf("update %s cache: clear columns: %v", key, err)
	}
	err = sqlitex.Execute(conn, `delete from "nar_references" where "object_hash" = ?;`, &sqlitex.ExecOptions{
		Args: []any{hash},
	})
	if err != nil {
		return fmt.Errorf("update %s cache: clear references: %v", key, err)
	}

	// Parse the raw data and update fields as needed.
	err = func() (err error) {
		defer sqlitex.Save(conn)(&err)
		info := new(nix.NARInfo)
		if err := info.UnmarshalText(data); err != nil {
			return err
		}
		if info.StorePath.Digest() != hash {
			return fmt.Errorf("%s has store path %q with inconsistent hash (skipping)", key, info.StorePath)
		}
		if info.StoreDirectory() != storeDir {
			return fmt.Errorf("%s has store path %q that does not match cache store directory %q", key, info.StorePath, storeDir)
		}
		err = sqlitex.Execute(
			conn,
			`update "nar_infos" set "store_path" = :store_path, `+
				`"file_size" = iif(:file_size > 0, :file_size, null), `+
				`"nar_size" = :nar_size `+
				`where "hash" = :hash;`,
			&sqlitex.ExecOptions{
				Named: map[string]any{
					":hash":       hash,
					":store_path": info.StorePath,
					":file_size":  info.FileSize,
					":nar_size":   info.NARSize,
				},
			},
		)
		if err != nil {
			return err
		}
		for _, ref := range info.References {
			err = sqlitex.Execute(
				conn,
				`insert into "nar_references" ("object_hash", "reference") values (?, ?);`,
				&sqlitex.ExecOptions{
					Args: []any{hash, string(ref.Base())},
				},
			)
			if err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil {
		log.Errorf(ctx, "Updating indexed fields for %s: %v", key, err)
	}
	return nil
}
