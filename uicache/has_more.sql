select exists(
  select 1
  from "store_objects"
  where
    (:query is null or :query = '' or instr(lower("path_name"), lower(:query))) and (
      (:after is null or :after = '') or
      ("path_name", "path_digest") >
        (coalesce((select "path_name" from "store_objects" where "path_digest" = :after), ''), :after)
    )
);
