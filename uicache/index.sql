with
  "relevant_digests"("path_digest") as (
    select "path_digest"
    from "store_objects"
    where
      (:query is null or :query = '' or instr(lower("path_name"), lower(:query))) and (
        (:after is null or :after = '') or
        ("path_name", "path_digest") >
          (coalesce((select "path_name" from "store_objects" where "path_digest" = :after), ''), :after)
      )
    order by "path_name", "path_digest"
    limit 100
  ),
  "requisites"("path_digest","requisite") as (
    select "path_digest", "reference" from "store_object_references"
      where
        "path_digest" <> store_path_digest("reference") and
        "path_digest" in (select "path_digest" from "relevant_digests")
    union
    select "refs"."path_digest", "refs"."reference"
      from "requisites" as "reqs"
        join "store_object_references" as "refs"
        on store_path_digest("reqs"."requisite") = "refs"."path_digest"
      where "refs"."path_digest" <> store_path_digest("refs"."reference")
  )
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
  "ca" as "ca",
  "file_size" + coalesce((select sum(r."file_size")
    from "requisites"
      join "store_objects" as r on store_path_digest("requisites"."requisite") = r."path_digest"
    where "requisites"."path_digest" = "narinfo"."store_path_digest"), 0) as "closure_file_size",
  "nar_size" + coalesce((select sum(r."nar_size")
    from "requisites"
      join "store_objects" as r on store_path_digest("requisites"."requisite") = r."path_digest"
    where "requisites"."path_digest" = "narinfo"."store_path_digest"), 0) as "closure_nar_size"
from "narinfo"
where "store_path_digest" in (select "path_digest" from "relevant_digests")
order by "store_path_name", "store_path_digest";
