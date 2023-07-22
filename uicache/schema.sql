create table "store" (
  "directory" text
    not null
    default "/nix/store"
);

create table "store_objects" (
  "path_digest" text
    not null
    primary key
    check("path_digest" regexp '^[0-9abcdfghijklmnpqrsvwxyz]+$'),
  "path_name" text
    not null
    check("path_name" regexp '^[-+._?=0-9a-zA-Z]+$'),
  "url" text
    not null,
  "compression" text
    not null
    default "none",
  "file_hash_type" text
    check("file_hash_type" is null or length("file_hash_type") > 0),
  "file_hash_bytes" blob
    check("file_hash_bytes" is null or length("file_hash_bytes") > 0),
  "file_size" integer
    check("file_size" is null or "file_size" >= 0),
  "nar_hash_type" text
    not null
    check(length("nar_hash_type") > 0),
  "nar_hash_bytes" blob
    not null
    check(length("nar_hash_bytes") > 0),
  "nar_size" integer
    not null
    check("nar_size" >= 0),
  "deriver" text
    check("deriver" is null or "deriver" regexp '^[0-9abcdfghijklmnpqrsvwxyz]+-[-+._?=0-9a-zA-Z]+$'),
  "ca" text
);

create table "store_object_references" (
  "path_digest" text
    not null
    references "store_objects"
      on delete cascade,
  "reference" text
    not null
    check("reference" regexp '^[0-9abcdfghijklmnpqrsvwxyz]+-[-+._?=0-9a-zA-Z]+$'),

  primary key ("path_digest", "reference")
);

create table "store_object_signatures" (
  "path_digest" text
    not null
    references "store_objects"
      on delete cascade,
  "signature_name" text
    not null
    check("signature_name" regexp '^[^:]+$'),
  "signature_data" text
    not null
    check(length("signature_data") > 0),

  primary key ("path_digest", "signature_name")
);
