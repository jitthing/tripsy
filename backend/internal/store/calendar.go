package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type CalendarConnection struct {
	ID, UserID, GoogleEmail, CalendarID, SyncToken, Status, LastError string
	EncryptedRefreshToken                                             []byte
	LastSyncedAt                                                      *time.Time
}
type CalendarLink struct {
	ConnectionID, PlanID, GoogleEventID, GoogleETag, LastSource string
	RemoteUpdatedAt, LastSyncedAt                               *time.Time
}

func (s *Store) UpsertCalendarConnection(ctx context.Context, userID, email, calendarID string, encryptedToken []byte) (CalendarConnection, error) {
	var connection CalendarConnection
	err := s.DB.QueryRow(ctx, `insert into calendar_connections (user_id,google_email,calendar_id,encrypted_refresh_token,status,last_error) values ($1,$2,$3,$4,'connected','') on conflict (user_id) do update set google_email=excluded.google_email,calendar_id=excluded.calendar_id,encrypted_refresh_token=excluded.encrypted_refresh_token,status='connected',last_error='',sync_token='' returning id,user_id,google_email,calendar_id,encrypted_refresh_token,sync_token,status,last_error,last_synced_at`, userID, email, calendarID, encryptedToken).Scan(&connection.ID, &connection.UserID, &connection.GoogleEmail, &connection.CalendarID, &connection.EncryptedRefreshToken, &connection.SyncToken, &connection.Status, &connection.LastError, &connection.LastSyncedAt)
	return connection, err
}
func (s *Store) CalendarConnection(ctx context.Context, userID string) (CalendarConnection, error) {
	var connection CalendarConnection
	err := s.DB.QueryRow(ctx, `select id,user_id,google_email,calendar_id,encrypted_refresh_token,sync_token,status,last_error,last_synced_at from calendar_connections where user_id=$1 and status <> 'disconnected'`, userID).Scan(&connection.ID, &connection.UserID, &connection.GoogleEmail, &connection.CalendarID, &connection.EncryptedRefreshToken, &connection.SyncToken, &connection.Status, &connection.LastError, &connection.LastSyncedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CalendarConnection{}, ErrNotFound
	}
	return connection, err
}
func (s *Store) DisconnectCalendar(ctx context.Context, userID string) error {
	tag, err := s.DB.Exec(ctx, `update calendar_connections set status='disconnected',encrypted_refresh_token='' where user_id=$1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) QueueCalendarSync(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx, `insert into calendar_sync_jobs (connection_id,status,run_after) select id,'queued',now() from calendar_connections where user_id=$1 and status='connected' on conflict (connection_id) where calendar_sync_jobs.status in ('queued','running') do update set run_after=least(calendar_sync_jobs.run_after,excluded.run_after),status='queued'`, userID)
	return err
}
func (s *Store) ClaimCalendarSync(ctx context.Context) (CalendarConnection, error) {
	var connection CalendarConnection
	err := s.DB.QueryRow(ctx, `with next as (select id,connection_id from calendar_sync_jobs where status='queued' and run_after<=now() order by run_after for update skip locked limit 1), claimed as (update calendar_sync_jobs j set status='running',locked_at=now(),attempts=attempts+1 from next where j.id=next.id returning next.connection_id) select c.id,c.user_id,c.google_email,c.calendar_id,c.encrypted_refresh_token,c.sync_token,c.status,c.last_error,c.last_synced_at from calendar_connections c join claimed on claimed.connection_id=c.id`).Scan(&connection.ID, &connection.UserID, &connection.GoogleEmail, &connection.CalendarID, &connection.EncryptedRefreshToken, &connection.SyncToken, &connection.Status, &connection.LastError, &connection.LastSyncedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CalendarConnection{}, ErrNotFound
	}
	return connection, err
}
func (s *Store) CalendarPlans(ctx context.Context, userID string) ([]Plan, error) {
	rows, err := s.DB.Query(ctx, `select p.id,p.trip_id,p.created_by,p.kind,p.title,p.starts_at,p.ends_at,p.location,p.confirmation_code,p.notes,p.time_zone from plan_items p join trips t on t.id=p.trip_id where p.deleted_at is null and (t.owner_id=$1 or exists(select 1 from trip_members m where m.trip_id=t.id and m.user_id=$1)) order by p.starts_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Plan])
}
func (s *Store) CalendarLink(ctx context.Context, connectionID, planID string) (CalendarLink, error) {
	var link CalendarLink
	err := s.DB.QueryRow(ctx, `select connection_id,plan_id,google_event_id,google_etag,last_source,remote_updated_at,last_synced_at from calendar_event_links where connection_id=$1 and plan_id=$2`, connectionID, planID).Scan(&link.ConnectionID, &link.PlanID, &link.GoogleEventID, &link.GoogleETag, &link.LastSource, &link.RemoteUpdatedAt, &link.LastSyncedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CalendarLink{}, ErrNotFound
	}
	return link, err
}
func (s *Store) UpsertCalendarLink(ctx context.Context, link CalendarLink) error {
	_, err := s.DB.Exec(ctx, `insert into calendar_event_links (connection_id,plan_id,google_event_id,google_etag,remote_updated_at,last_synced_at,last_source) values ($1,$2,$3,$4,$5,now(),$6) on conflict (plan_id) do update set google_event_id=excluded.google_event_id,google_etag=excluded.google_etag,remote_updated_at=excluded.remote_updated_at,last_synced_at=now(),last_source=excluded.last_source`, link.ConnectionID, link.PlanID, link.GoogleEventID, link.GoogleETag, link.RemoteUpdatedAt, link.LastSource)
	return err
}
func (s *Store) CompleteCalendarSync(ctx context.Context, connectionID, syncToken string) error {
	_, err := s.DB.Exec(ctx, `update calendar_connections set sync_token=$2,last_synced_at=now(),status='connected',last_error='' where id=$1; delete from calendar_sync_jobs where connection_id=$1`, connectionID, syncToken)
	return err
}
func (s *Store) FailCalendarSync(ctx context.Context, connectionID string, cause error) error {
	_, err := s.DB.Exec(ctx, `update calendar_connections set status='error',last_error=$2 where id=$1; update calendar_sync_jobs set status='queued',run_after=now()+interval '5 minutes',last_error=$2 where connection_id=$1`, connectionID, cause.Error())
	return err
}
