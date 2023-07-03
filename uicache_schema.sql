create table "nar_infos" (
  "hash" text
    not null
    primary key
    check("hash" regexp '^[0-9abcdfghijklmnpqrsvwxyz]+$'),
  "narinfo" blob,
  "store_path" text,
  "file_size" integer
    check("file_size" is null or "file_size" >= 0),
  "nar_size" integer
    check("nar_size" is null or "nar_size" >= 0)
);

create table "nix_cache_info" (
  "store_dir" text,
  "nix_cache_info" blob
);

create table "uicache_status" (
  "initial_crawl_complete" boolean not null default false
);

insert into "uicache_status" default values;
