delete from "store_object_references" where
  "path_digest" = store_path_digest(:store_path);

delete from "store_object_signatures" where
  "path_digest" = store_path_digest(:store_path);

insert into "store_objects" (
  "path_digest",
  "path_name",
  "url",
  "compression",
  "file_hash_type",
  "file_hash_bytes",
  "file_size",
  "nar_hash_type",
  "nar_hash_bytes",
  "nar_size",
  "deriver",
  "ca"
) values (
  store_path_digest(:store_path),
  store_path_name(:store_path),
  :url,
  :compression,
  hash_type(:file_hash),
  hash_bytes(:file_hash),
  iif(:file_size > 0, :file_size, null),
  hash_type(:nar_hash),
  hash_bytes(:nar_hash),
  :nar_size,
  path_base(nullif(:deriver, '')),
  nullif(:ca, '')
) on conflict ("path_digest") do update set
  "path_name" = store_path_name(:store_path),
  "url" = :url,
  "compression" = :compression,
  "file_hash_type" = hash_type(:file_hash),
  "file_hash_bytes" = hash_bytes(:file_hash),
  "file_size" = iif(:file_size > 0, :file_size, null),
  "nar_hash_type" = hash_type(:nar_hash),
  "nar_hash_bytes" = hash_bytes(:nar_hash),
  "nar_size" = :nar_size,
  "deriver" = path_base(nullif(:deriver, '')),
  "ca" = nullif(:ca, '');

insert into "store_object_references" (
  "path_digest",
  "reference"
) select
  store_path_digest(:store_path),
  path_base("value")
from json_each(json(:references));

insert into "store_object_signatures" (
  "path_digest",
  "signature_name",
  "signature_data"
) select
  store_path_digest(:store_path),
  signature_name("value"),
  signature_data("value")
from json_each(json(:signatures));
