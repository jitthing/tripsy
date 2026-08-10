-- A personal/shared comparison board for transport options researched by users.
-- This deliberately stores links and notes only; it does not scrape providers.
create table public.route_options (
  id uuid primary key default gen_random_uuid(),
  trip_id uuid not null references public.trips(id) on delete cascade,
  created_by uuid not null references public.profiles(id) on delete restrict,
  title text not null check (char_length(title) between 1 and 180),
  route_type text not null check (route_type in ('direct_flight', 'flight_train', 'train', 'bus', 'other')),
  origin text not null default '',
  destination text not null default '',
  departs_at timestamptz,
  arrives_at timestamptz,
  duration_minutes integer check (duration_minutes is null or duration_minutes >= 0),
  transfers integer not null default 0 check (transfers >= 0 and transfers <= 20),
  price_amount numeric(12, 2) check (price_amount is null or price_amount >= 0),
  currency char(3),
  booking_url text not null default '' check (booking_url = '' or booking_url ~ '^https://'),
  notes text not null default '',
  status text not null default 'considering' check (status in ('considering', 'shortlisted', 'booked', 'dismissed')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check (arrives_at is null or departs_at is null or arrives_at >= departs_at),
  check ((price_amount is null and currency is null) or (price_amount is not null and currency ~ '^[A-Z]{3}$'))
);

create index route_options_trip_status_idx on public.route_options(trip_id, status, created_at desc);
create trigger route_options_touch before update on public.route_options for each row execute procedure public.touch_updated_at();

alter table public.route_options enable row level security;

create policy "members view route options" on public.route_options for select to authenticated
using (public.is_trip_member(trip_id));
create policy "members add route options" on public.route_options for insert to authenticated
with check (public.is_trip_member(trip_id) and created_by = auth.uid());
create policy "authors edit route options" on public.route_options for update to authenticated
using (created_by = auth.uid()) with check (public.is_trip_member(trip_id) and created_by = auth.uid());
create policy "authors remove route options" on public.route_options for delete to authenticated
using (created_by = auth.uid());
