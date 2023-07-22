drop view if exists "narinfo";
create view "narinfo" (
  "store_path",
  "store_path_digest",
  "store_path_name",
  "url",
  "compression",
  "file_hash",
  "file_size",
  "nar_hash",
  "nar_size",
  "references",
  "deriver",
  "signatures",
  "ca"
) as select
  (select "directory" from "store") || '/' || "path_digest" || '-' || "path_name",
  "path_digest",
  "path_name",
  "url",
  "compression",
  "file_hash_type" || ':' || hex("file_hash_bytes"),
  "file_size",
  "nar_hash_type" || ':' || hex("nar_hash_bytes"),
  "nar_size",
  (select json_group_array((select "directory" from "store") || '/' || "reference")
    from
      (select "reference" as "reference"
        from "store_object_references" as r
        where r."path_digest" = obj."path_digest"
        order by 1)),
  (select "directory" from "store") || '/' || "deriver",
  (select json_group_array("signature_name" || ':' || "signature_data")
    from
      (select
          "signature_name" as "signature_name",
          "signature_data" as "signature_data"
        from "store_object_signatures" as sig
        where sig."path_digest" = obj."path_digest"
        order by "signature_name")),
  "ca"
from "store_objects" as obj;
