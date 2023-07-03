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
	_ "embed"
	"io"
	slashpath "path"
	"strings"

	"gocloud.dev/blob"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nixcached/internal/nixstore"
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
	if err := refunc.Register(conn); err != nil {
		return err
	}
	conn.CreateFunction("store_path_name", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
			if args[0].Type() == sqlite.TypeNull {
				return sqlite.Value{}, nil
			}
			name, err := nixstore.ParseObjectName(slashpath.Base(args[0].Text()))
			if err != nil {
				return sqlite.Value{}, nil
			}
			return sqlite.TextValue(name.Name()), nil
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
	if err := sqlitex.ExecuteTransient(conn, `delete from "nix_cache_info";`, nil); err != nil {
		log.Errorf(ctx, "Could not clear nix_cache_info: %v", err)
	} else {
		data, err := bucket.ReadAll(ctx, nixstore.CacheInfoName)
		if err != nil {
			log.Warnf(ctx, "Could not read %s: %v", nixstore.CacheInfoName, err)
		} else {
			err = sqlitex.ExecuteTransient(conn, `insert into "nix_cache_info" ("nix_cache_info") values (?);`, &sqlitex.ExecOptions{
				Args: []any{data},
			})
			if err != nil {
				log.Errorf(ctx, "Could not store %s: %v", nixstore.CacheInfoName, err)
			}
		}
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
		hash, hasExt := strings.CutSuffix(obj.Key, ".narinfo")
		if obj.IsDir || !hasExt {
			log.Debugf(ctx, "Ignoring %q during crawl", obj.Key)
			continue
		}
		if objectName, err := nixstore.ParseObjectName(hash + "-x"); err != nil || objectName.Hash() != hash {
			log.Warnf(ctx, "Ignoring improperly named %q", obj.Key)
			continue
		}

		log.Debugf(ctx, "Found %s during crawl", obj.Key)
		err = sqlitex.Execute(conn, `insert or ignore into "nar_infos" ("hash") values (?);`, &sqlitex.ExecOptions{
			Args: []any{hash},
		})
		if err != nil {
			log.Errorf(ctx, "Inserting %s into cache: %v", obj.Key, err)
			continue
		}
		data, err := bucket.ReadAll(ctx, obj.Key)
		if err != nil {
			log.Errorf(ctx, "Reading %s: %v", obj.Key, err)
			continue
		}
		info := new(nixstore.NARInfo)
		if err := info.UnmarshalText(data); err != nil {
			log.Errorf(ctx, "Reading %s: %v", obj.Key, err)
			continue
		}
		if info.ObjectName().Hash() != hash {
			log.Errorf(ctx, "%s has store path %q with inconsistent hash (skipping)", obj.Key, info.StorePath)
			continue
		}
		err = sqlitex.Execute(
			conn,
			`update "nar_infos" set "narinfo" = :narinfo, "store_path" = :store_path, `+
				`"file_size" = iif(:file_size > 0, :file_size, null), `+
				`"nar_size" = :nar_size `+
				`where "hash" = :hash;`,
			&sqlitex.ExecOptions{
				Named: map[string]any{
					":hash":       hash,
					":narinfo":    data,
					":store_path": info.StorePath,
					":file_size":  info.FileSize,
					":nar_size":   info.NARSize,
				},
			},
		)
		if err != nil {
			log.Errorf(ctx, "Saving %s from crawl: %v", obj.Key, err)
			continue
		}
	}
}
