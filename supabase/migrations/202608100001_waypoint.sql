create extension if not exists pgcrypto;

create table public.profiles (
  id uuid primary key references auth.users(id) on delete cascade,
  email text not null unique,
  display_name text not null default '',
  avatar_url text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

insert into public.profiles (id, email, display_name, avatar_url)
select
  id,
  coalesce(email, ''),
  coalesce(raw_user_meta_data->>'full_name', raw_user_meta_data->>'name', split_part(coalesce(email, ''), '@', 1)),
  raw_user_meta_data->>'avatar_url'
from auth.users
on conflict (id) do nothing;

create table public.trips (
  id uuid primary key default gen_random_uuid(),
  owner_id uuid not null references public.profiles(id) on delete restrict,
  title text not null check (char_length(title) between 1 and 100),
  destination text not null check (char_length(destination) between 1 and 120),
  start_date date not null,
  end_date date not null check (end_date >= start_date),
  cover_color text not null default '#1d4c46',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table public.trip_members (
  trip_id uuid not null references public.trips(id) on delete cascade,
  user_id uuid not null references public.profiles(id) on delete cascade,
  role text not null default 'member' check (role in ('owner', 'member')),
  created_at timestamptz not null default now(),
  primary key (trip_id, user_id)
);

create table public.plan_items (
  id uuid primary key default gen_random_uuid(),
  trip_id uuid not null references public.trips(id) on delete cascade,
  created_by uuid not null references public.profiles(id) on delete restrict,
  kind text not null check (kind in ('flight', 'stay', 'activity', 'transport', 'food', 'other')),
  title text not null check (char_length(title) between 1 and 180),
  starts_at timestamptz not null,
  ends_at timestamptz,
  location text not null default '',
  confirmation_code text not null default '',
  notes text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check (ends_at is null or ends_at >= starts_at)
);

create table public.checklist_items (
  id uuid primary key default gen_random_uuid(),
  trip_id uuid not null references public.trips(id) on delete cascade,
  created_by uuid not null references public.profiles(id) on delete restrict,
  title text not null check (char_length(title) between 1 and 180),
  is_complete boolean not null default false,
  sort_order integer not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table public.documents (
  id uuid primary key default gen_random_uuid(),
  trip_id uuid not null references public.trips(id) on delete cascade,
  uploaded_by uuid not null references public.profiles(id) on delete restrict,
  name text not null check (char_length(name) between 1 and 180),
  storage_path text not null unique,
  content_type text not null,
  size_bytes bigint not null check (size_bytes >= 0 and size_bytes <= 10485760),
  created_at timestamptz not null default now()
);

create index trips_owner_id_idx on public.trips(owner_id);
create index trip_members_user_id_idx on public.trip_members(user_id);
create index plan_items_trip_starts_at_idx on public.plan_items(trip_id, starts_at);
create index checklist_items_trip_sort_idx on public.checklist_items(trip_id, sort_order);
create index documents_trip_id_idx on public.documents(trip_id);

create or replace function public.is_trip_member(target_trip_id uuid, target_user_id uuid default auth.uid())
returns boolean
language sql
stable
security definer
set search_path = public
as $$
  select exists(
    select 1 from public.trips t
    where t.id = target_trip_id and t.owner_id = target_user_id
  ) or exists(
    select 1 from public.trip_members tm
    where tm.trip_id = target_trip_id and tm.user_id = target_user_id
  );
$$;

create or replace function public.is_trip_owner(target_trip_id uuid, target_user_id uuid default auth.uid())
returns boolean
language sql
stable
security definer
set search_path = public
as $$
  select exists(select 1 from public.trips where id = target_trip_id and owner_id = target_user_id);
$$;

create or replace function public.trip_id_from_storage_path(path text)
returns uuid
language plpgsql
immutable
as $$
begin
  return split_part(path, '/', 1)::uuid;
exception when invalid_text_representation then
  return null;
end;
$$;

create or replace function public.handle_new_user()
returns trigger
language plpgsql
security definer
set search_path = public
as $$
begin
  insert into public.profiles (id, email, display_name, avatar_url)
  values (
    new.id,
    coalesce(new.email, ''),
    coalesce(new.raw_user_meta_data->>'full_name', new.raw_user_meta_data->>'name', split_part(coalesce(new.email, ''), '@', 1)),
    new.raw_user_meta_data->>'avatar_url'
  )
  on conflict (id) do update set
    email = excluded.email,
    display_name = excluded.display_name,
    avatar_url = excluded.avatar_url,
    updated_at = now();
  return new;
end;
$$;

create trigger on_auth_user_created
  after insert or update on auth.users
  for each row execute procedure public.handle_new_user();

create or replace function public.touch_updated_at()
returns trigger language plpgsql as $$ begin new.updated_at = now(); return new; end; $$;

create trigger trips_touch before update on public.trips for each row execute procedure public.touch_updated_at();
create trigger plans_touch before update on public.plan_items for each row execute procedure public.touch_updated_at();
create trigger checklist_touch before update on public.checklist_items for each row execute procedure public.touch_updated_at();

alter table public.profiles enable row level security;
alter table public.trips enable row level security;
alter table public.trip_members enable row level security;
alter table public.plan_items enable row level security;
alter table public.checklist_items enable row level security;
alter table public.documents enable row level security;

create policy "profiles are visible to signed-in users" on public.profiles for select to authenticated using (true);
create policy "users update own profile" on public.profiles for update to authenticated using (id = auth.uid()) with check (id = auth.uid());

create policy "members view trips" on public.trips for select to authenticated using (public.is_trip_member(id));
create policy "users create own trips" on public.trips for insert to authenticated with check (owner_id = auth.uid());
create policy "owners update trips" on public.trips for update to authenticated using (public.is_trip_owner(id)) with check (public.is_trip_owner(id));
create policy "owners delete trips" on public.trips for delete to authenticated using (public.is_trip_owner(id));

create policy "members view trip members" on public.trip_members for select to authenticated using (public.is_trip_member(trip_id));
create policy "owners add members" on public.trip_members for insert to authenticated with check (public.is_trip_owner(trip_id));
create policy "owners remove members" on public.trip_members for delete to authenticated using (public.is_trip_owner(trip_id) or user_id = auth.uid());

create policy "members view plans" on public.plan_items for select to authenticated using (public.is_trip_member(trip_id));
create policy "members add plans" on public.plan_items for insert to authenticated with check (public.is_trip_member(trip_id) and created_by = auth.uid());
create policy "authors edit plans" on public.plan_items for update to authenticated using (created_by = auth.uid()) with check (public.is_trip_member(trip_id) and created_by = auth.uid());
create policy "authors remove plans" on public.plan_items for delete to authenticated using (created_by = auth.uid());

create policy "members view checklist" on public.checklist_items for select to authenticated using (public.is_trip_member(trip_id));
create policy "members add checklist" on public.checklist_items for insert to authenticated with check (public.is_trip_member(trip_id) and created_by = auth.uid());
create policy "authors edit checklist" on public.checklist_items for update to authenticated using (created_by = auth.uid()) with check (public.is_trip_member(trip_id) and created_by = auth.uid());
create policy "authors remove checklist" on public.checklist_items for delete to authenticated using (created_by = auth.uid());

create policy "members view documents" on public.documents for select to authenticated using (public.is_trip_member(trip_id));
create policy "members add documents" on public.documents for insert to authenticated with check (public.is_trip_member(trip_id) and uploaded_by = auth.uid());
create policy "uploaders remove documents" on public.documents for delete to authenticated using (uploaded_by = auth.uid());

insert into storage.buckets (id, name, public, file_size_limit, allowed_mime_types)
values ('trip-documents', 'trip-documents', false, 10485760, array['application/pdf', 'image/jpeg', 'image/png', 'image/webp'])
on conflict (id) do update set public = false, file_size_limit = 10485760, allowed_mime_types = excluded.allowed_mime_types;

create policy "members read trip documents" on storage.objects for select to authenticated
using (bucket_id = 'trip-documents' and public.is_trip_member(public.trip_id_from_storage_path(name)));
create policy "members upload trip documents" on storage.objects for insert to authenticated
with check (bucket_id = 'trip-documents' and public.is_trip_member(public.trip_id_from_storage_path(name)));
create policy "members delete trip documents" on storage.objects for delete to authenticated
using (bucket_id = 'trip-documents' and public.is_trip_member(public.trip_id_from_storage_path(name)));
