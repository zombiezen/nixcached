with
  "requisites"("path_digest","requisite") as (
    select "path_digest", "reference" from "store_object_references"
      where "path_digest" <> store_path_digest("reference")
    union
    select "refs"."path_digest", "refs"."reference"
      from "requisites" as "reqs"
        join "store_object_references" as "refs"
        on store_path_digest("reqs"."requisite") = "refs"."path_digest"
      where "refs"."path_digest" <> store_path_digest("refs"."reference")
  )
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
        where r."path_digest" = "store_objects"."path_digest"
        order by 1)) as "references",
  (select json_group_array("signature_name" || ':' || "signature_data")
    from
      (select
          "signature_name" as "signature_name",
          "signature_data" as "signature_data"
        from "store_object_signatures" as sig
        where sig."path_digest" = "store_objects"."path_digest"
        order by "signature_name")) as "signatures",
  "file_size" + coalesce((select sum(r."file_size")
    from "requisites"
      join "store_objects" as r on store_path_digest("requisites"."requisite") = r."path_digest"
    where "requisites"."path_digest" = "store_objects"."path_digest"), 0) as "closure_file_size",
  "nar_size" + coalesce((select sum(r."nar_size")
    from "requisites"
      join "store_objects" as r on store_path_digest("requisites"."requisite") = r."path_digest"
    where "requisites"."path_digest" = "store_objects"."path_digest"), 0) as "closure_nar_size"
from "store_objects"
order by "path_name", "path_digest";
