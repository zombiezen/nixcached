select
  (select "directory" from "store") || '/' || "path_digest" || '-' || "path_name" as "store_path",
  "url" as "url",
  "compression" as "compression",
  "file_hash_type" || ':' || hex("file_hash_bytes") as "file_hash",
  coalesce("file_size", -1) as "file_size",
  "nar_hash_type" || ':' || hex("nar_hash_bytes") as "nar_hash",
  coalesce("nar_size", -1) as "nar_size",
  (select "directory" from "store") || '/' || "deriver" as "deriver",
  "ca" as "ca",
  (select json_group_array((select "directory" from "store") || '/' || "reference")
    from
      (select "reference" as "reference"
        from "store_object_references" as r
        where r."path_digest" = obj."path_digest"
        order by 1)) as "references",
  (select json_group_array("signature_name" || ':' || "signature_data")
    from
      (select
          "signature_name" as "signature_name",
          "signature_data" as "signature_data"
        from "store_object_signatures" as sig
        where sig."path_digest" = obj."path_digest"
        order by "signature_name")) as "signatures"
from "store_objects" as obj
where "path_digest" = :store_path_digest
limit 1;
