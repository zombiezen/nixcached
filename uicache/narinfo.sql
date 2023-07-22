select
  "store_path" as "store_path",
  "url" as "url",
  "compression" as "compression",
  "file_hash" as "file_hash",
  coalesce("file_size", -1) as "file_size",
  "nar_hash" as "nar_hash",
  coalesce("nar_size", -1) as "nar_size",
  "references" as "references",
  "deriver" as "deriver",
  "signatures" as "signatures",
  "ca" as "ca"
from "narinfo"
where "store_path_digest" = :store_path_digest
limit 1;
