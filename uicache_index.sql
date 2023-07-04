with
  "requisites"("object_hash","requisite") as (
    select "object_hash", "reference" from "nar_references"
      where "object_hash" <> store_path_hash("reference")
    union
    select "refs"."object_hash", "refs"."reference"
      from "requisites" as "reqs"
        join "nar_references" as "refs"
        on store_path_hash("reqs"."requisite") = "refs"."object_hash"
      where "refs"."object_hash" <> store_path_hash("refs"."reference")
  )
select
  "hash" as "hash",
  "narinfo" as "narinfo",
  "file_size" + coalesce((select sum(r."file_size")
    from "requisites"
      join "nar_infos" as r on store_path_hash("requisites"."requisite") = r."hash"
    where "requisites"."object_hash" = "nar_infos"."hash"), 0) as "closure_file_size",
  "nar_size" + coalesce((select sum(r."nar_size")
    from "requisites"
      join "nar_infos" as r on store_path_hash("requisites"."requisite") = r."hash"
    where "requisites"."object_hash" = "nar_infos"."hash"), 0) as "closure_nar_size"
from "nar_infos"
order by store_path_name("store_path") nulls last;
