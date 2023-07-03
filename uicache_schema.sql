create table "nar_infos" (
  "hash" text
    not null
    primary key
    check("hash" regexp '^[0-9abcdfghijklmnpqrsvwxyz]+$'),
  "narinfo" blob,
  "store_path" text,
  "file_size" integer,
  "nar_size" integer
);

create table "nix_cache_info" (
  "store_dir" text,
  "nix_cache_info" blob
);

create table "uicache_status" (
  "initial_crawl_complete" boolean not null default false
);

insert into "uicache_status" default values;
