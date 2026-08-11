-- Central reservation inbox. Existing trip-bound imports remain valid; new
-- inbound messages may wait for a user to assign them to a trip.
alter table public.reservation_imports add column if not exists owner_id uuid references public.profiles(id) on delete cascade;
alter table public.reservation_imports alter column trip_id drop not null;
alter table public.reservation_imports drop constraint if exists reservation_imports_status_check;
alter table public.reservation_imports add constraint reservation_imports_status_check check (status in ('queued','processing','review','approved','discarded','failed'));

create index if not exists reservation_imports_owner_created_idx on public.reservation_imports(owner_id, created_at desc);

drop policy if exists "members view imports" on public.reservation_imports;
drop policy if exists "members update imports" on public.reservation_imports;
create policy "users view own or trip imports" on public.reservation_imports for select to authenticated
  using (owner_id = auth.uid() or (trip_id is not null and public.is_trip_member(trip_id)));
create policy "users update own or trip imports" on public.reservation_imports for update to authenticated
  using (owner_id = auth.uid() or (trip_id is not null and public.is_trip_member(trip_id)))
  with check (owner_id = auth.uid() or (trip_id is not null and public.is_trip_member(trip_id)));
