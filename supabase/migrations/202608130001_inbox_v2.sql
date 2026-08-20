-- Inbox v2 metadata. Duplicates remain reviewable and are never discarded.
alter table public.reservation_imports
  add column if not exists duplicate_of_import_id uuid references public.reservation_imports(id) on delete set null;

create index if not exists reservation_imports_duplicate_idx
  on public.reservation_imports(duplicate_of_import_id)
  where duplicate_of_import_id is not null;
