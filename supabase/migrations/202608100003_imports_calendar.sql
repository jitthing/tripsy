alter table public.trips add column time_zone text not null default 'UTC';
alter table public.plan_items add column time_zone text not null default 'UTC';
alter table public.plan_items add column deleted_at timestamptz;

create table public.trip_import_addresses (
  id uuid primary key default gen_random_uuid(),
  trip_id uuid not null unique references public.trips(id) on delete cascade,
  token text not null unique check (token ~ '^[A-Za-z0-9_-]{20,128}$'),
  created_by uuid not null references public.profiles(id) on delete restrict,
  created_at timestamptz not null default now()
);

create table public.reservation_imports (
  id uuid primary key default gen_random_uuid(),
  trip_id uuid not null references public.trips(id) on delete cascade,
  external_email_id text not null unique,
  webhook_id text not null unique,
  sender text not null default '',
  subject text not null default '',
  received_at timestamptz,
  raw_storage_path text not null default '',
  text_storage_path text not null default '',
  status text not null default 'queued' check (status in ('queued', 'processing', 'review', 'approved', 'discarded', 'failed')),
  error_message text not null default '',
  used_llm boolean not null default false,
  processed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table public.reservation_import_attachments (
  id uuid primary key default gen_random_uuid(),
  import_id uuid not null references public.reservation_imports(id) on delete cascade,
  filename text not null,
  content_type text not null,
  size_bytes bigint not null default 0 check (size_bytes >= 0 and size_bytes <= 10485760),
  storage_path text not null,
  document_id uuid references public.documents(id) on delete set null,
  created_at timestamptz not null default now()
);

create table public.reservation_drafts (
  id uuid primary key default gen_random_uuid(),
  import_id uuid not null references public.reservation_imports(id) on delete cascade,
  kind text not null check (kind in ('flight', 'stay', 'activity', 'transport', 'food', 'other')),
  title text not null,
  supplier text not null default '',
  confirmation_code text not null default '',
  starts_at timestamptz,
  ends_at timestamptz,
  time_zone text not null default 'UTC',
  location text not null default '',
  notes text not null default '',
  confidence numeric(4,3) not null default 0 check (confidence >= 0 and confidence <= 1),
  status text not null default 'pending' check (status in ('pending', 'approved', 'discarded')),
  plan_id uuid references public.plan_items(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table public.calendar_connections (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null unique references public.profiles(id) on delete cascade,
  google_email text not null default '',
  calendar_id text not null default '',
  encrypted_refresh_token bytea not null,
  sync_token text not null default '',
  status text not null default 'connected' check (status in ('connected', 'error', 'disconnected')),
  last_error text not null default '',
  last_synced_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table public.calendar_event_links (
  id uuid primary key default gen_random_uuid(),
  connection_id uuid not null references public.calendar_connections(id) on delete cascade,
  plan_id uuid not null unique references public.plan_items(id) on delete cascade,
  google_event_id text not null,
  google_etag text not null default '',
  remote_updated_at timestamptz,
  last_synced_at timestamptz,
  last_source text not null default 'waypoint' check (last_source in ('waypoint', 'google')),
  unique (connection_id, google_event_id)
);

create table public.calendar_sync_jobs (
  id uuid primary key default gen_random_uuid(),
  connection_id uuid not null references public.calendar_connections(id) on delete cascade,
  status text not null default 'queued' check (status in ('queued', 'running', 'failed')),
  attempts integer not null default 0,
  run_after timestamptz not null default now(),
  locked_at timestamptz,
  last_error text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create unique index calendar_sync_jobs_active_connection_idx on public.calendar_sync_jobs(connection_id) where status in ('queued', 'running');
create index reservation_imports_trip_created_idx on public.reservation_imports(trip_id, created_at desc);
create index reservation_drafts_import_idx on public.reservation_drafts(import_id, status);
create index calendar_event_links_connection_idx on public.calendar_event_links(connection_id, google_event_id);

create trigger reservation_imports_touch before update on public.reservation_imports for each row execute procedure public.touch_updated_at();
create trigger reservation_drafts_touch before update on public.reservation_drafts for each row execute procedure public.touch_updated_at();
create trigger calendar_connections_touch before update on public.calendar_connections for each row execute procedure public.touch_updated_at();
create trigger calendar_sync_jobs_touch before update on public.calendar_sync_jobs for each row execute procedure public.touch_updated_at();

alter table public.trip_import_addresses enable row level security;
alter table public.reservation_imports enable row level security;
alter table public.reservation_import_attachments enable row level security;
alter table public.reservation_drafts enable row level security;
alter table public.calendar_connections enable row level security;
alter table public.calendar_event_links enable row level security;
alter table public.calendar_sync_jobs enable row level security;

create policy "members view import address" on public.trip_import_addresses for select to authenticated using (public.is_trip_member(trip_id));
create policy "members create import address" on public.trip_import_addresses for insert to authenticated with check (public.is_trip_member(trip_id) and created_by = auth.uid());
create policy "members delete import address" on public.trip_import_addresses for delete to authenticated using (public.is_trip_member(trip_id));
create policy "members view imports" on public.reservation_imports for select to authenticated using (public.is_trip_member(trip_id));
create policy "members update imports" on public.reservation_imports for update to authenticated using (public.is_trip_member(trip_id)) with check (public.is_trip_member(trip_id));
create policy "members view import attachments" on public.reservation_import_attachments for select to authenticated using (exists (select 1 from public.reservation_imports i where i.id = import_id and public.is_trip_member(i.trip_id)));
create policy "members view reservation drafts" on public.reservation_drafts for select to authenticated using (exists (select 1 from public.reservation_imports i where i.id = import_id and public.is_trip_member(i.trip_id)));
create policy "members update reservation drafts" on public.reservation_drafts for update to authenticated using (exists (select 1 from public.reservation_imports i where i.id = import_id and public.is_trip_member(i.trip_id))) with check (exists (select 1 from public.reservation_imports i where i.id = import_id and public.is_trip_member(i.trip_id)));
create policy "users view own calendar connection" on public.calendar_connections for select to authenticated using (user_id = auth.uid());
create policy "users view own calendar links" on public.calendar_event_links for select to authenticated using (exists (select 1 from public.calendar_connections c where c.id = connection_id and c.user_id = auth.uid()));
create policy "users view own calendar jobs" on public.calendar_sync_jobs for select to authenticated using (exists (select 1 from public.calendar_connections c where c.id = connection_id and c.user_id = auth.uid()));

insert into storage.buckets (id, name, public, file_size_limit)
values ('trip-imports', 'trip-imports', false, 10485760)
on conflict (id) do update set public = false, file_size_limit = 10485760;

create policy "members read import source" on storage.objects for select to authenticated using (bucket_id = 'trip-imports' and public.is_trip_member(public.trip_id_from_storage_path(name)));
create policy "members upload import source" on storage.objects for insert to authenticated with check (bucket_id = 'trip-imports' and public.is_trip_member(public.trip_id_from_storage_path(name)));
create policy "members delete import source" on storage.objects for delete to authenticated using (bucket_id = 'trip-imports' and public.is_trip_member(public.trip_id_from_storage_path(name)));
