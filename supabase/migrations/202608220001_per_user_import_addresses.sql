-- Forwarding addresses become per-owner, so a shared deployment no longer needs a
-- single RESEND_INBOUND_OWNER_ID. A row with trip_id null is a personal inbox
-- address; a row with a trip_id keeps the existing trip-bound behaviour.
alter table public.trip_import_addresses rename to import_addresses;

alter table public.import_addresses
  add column if not exists owner_id uuid references public.profiles(id) on delete cascade;

update public.import_addresses set owner_id = created_by where owner_id is null;

alter table public.import_addresses alter column owner_id set not null;
alter table public.import_addresses alter column trip_id drop not null;

-- The old table-level unique on trip_id cannot express "one personal address per
-- owner", so both rules move to partial indexes.
alter table public.import_addresses drop constraint if exists trip_import_addresses_trip_id_key;

create unique index if not exists import_addresses_trip_idx
  on public.import_addresses(trip_id) where trip_id is not null;
create unique index if not exists import_addresses_owner_inbox_idx
  on public.import_addresses(owner_id) where trip_id is null;

drop policy if exists "members view import address" on public.import_addresses;
drop policy if exists "members create import address" on public.import_addresses;
drop policy if exists "members delete import address" on public.import_addresses;

create policy "view import address" on public.import_addresses for select to authenticated
  using (owner_id = auth.uid() or (trip_id is not null and public.is_trip_member(trip_id)));
create policy "create import address" on public.import_addresses for insert to authenticated
  with check (owner_id = auth.uid() and (trip_id is null or public.is_trip_member(trip_id)));
create policy "delete import address" on public.import_addresses for delete to authenticated
  using (owner_id = auth.uid() or (trip_id is not null and public.is_trip_member(trip_id)));

-- Forwarding addresses are matched case-insensitively, because mail systems may
-- normalise the local part in transit while the token itself is base64url.
create index if not exists import_addresses_token_lower_idx
  on public.import_addresses(lower(token));
