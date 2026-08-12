package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var ErrForbidden = errors.New("forbidden")

type Store struct{ DB *pgxpool.Pool }

type Trip struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"ownerId"`
	Title       string    `json:"title"`
	Destination string    `json:"destination"`
	StartDate   time.Time `json:"startDate"`
	EndDate     time.Time `json:"endDate"`
	CoverColor  string    `json:"coverColor"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Member struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
	Role        string `json:"role"`
}

type Plan struct {
	ID               string     `json:"id"`
	TripID           string     `json:"tripId"`
	CreatedBy        string     `json:"createdBy"`
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	StartsAt         time.Time  `json:"startsAt"`
	EndsAt           *time.Time `json:"endsAt,omitempty"`
	Location         string     `json:"location"`
	ConfirmationCode string     `json:"confirmationCode"`
	Notes            string     `json:"notes"`
	TimeZone         string     `json:"timeZone"`
}

type ChecklistItem struct {
	ID         string `json:"id"`
	TripID     string `json:"tripId"`
	CreatedBy  string `json:"createdBy"`
	Title      string `json:"title"`
	IsComplete bool   `json:"isComplete"`
	SortOrder  int    `json:"sortOrder"`
}

type Document struct {
	ID          string    `json:"id"`
	TripID      string    `json:"tripId"`
	UploadedBy  string    `json:"uploadedBy"`
	Name        string    `json:"name"`
	StoragePath string    `json:"storagePath"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	CreatedAt   time.Time `json:"createdAt"`
}

type RouteOption struct {
	ID              string     `json:"id"`
	TripID          string     `json:"tripId"`
	CreatedBy       string     `json:"createdBy"`
	Title           string     `json:"title"`
	RouteType       string     `json:"routeType"`
	Origin          string     `json:"origin"`
	Destination     string     `json:"destination"`
	DepartsAt       *time.Time `json:"departsAt,omitempty"`
	ArrivesAt       *time.Time `json:"arrivesAt,omitempty"`
	DurationMinutes *int       `json:"durationMinutes,omitempty"`
	Transfers       int        `json:"transfers"`
	PriceAmount     *float64   `json:"priceAmount,omitempty"`
	Currency        *string    `json:"currency,omitempty"`
	BookingURL      string     `json:"bookingUrl"`
	Notes           string     `json:"notes"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{DB: pool}, nil
}

func (s *Store) Close() { s.DB.Close() }

func (s *Store) CanAccessTrip(ctx context.Context, tripID, userID string) (bool, error) {
	var allowed bool
	err := s.DB.QueryRow(ctx, `select exists(
    select 1 from trips t where t.id = $1 and (t.owner_id = $2 or exists (
      select 1 from trip_members tm where tm.trip_id = t.id and tm.user_id = $2
    ))
  )`, tripID, userID).Scan(&allowed)
	return allowed, err
}

func (s *Store) IsOwner(ctx context.Context, tripID, userID string) (bool, error) {
	var owner bool
	err := s.DB.QueryRow(ctx, `select exists(select 1 from trips where id = $1 and owner_id = $2)`, tripID, userID).Scan(&owner)
	return owner, err
}

func (s *Store) ListTrips(ctx context.Context, userID string) ([]Trip, error) {
	rows, err := s.DB.Query(ctx, `select id, owner_id, title, destination, start_date, end_date, cover_color, created_at
    from trips where owner_id = $1 or exists (select 1 from trip_members where trip_id = trips.id and user_id = $1)
    order by start_date asc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[Trip])
}

func (s *Store) CreateTrip(ctx context.Context, userID string, trip Trip) (Trip, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Trip{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `insert into trips (owner_id, title, destination, start_date, end_date, cover_color)
    values ($1,$2,$3,$4,$5,$6) returning id, owner_id, title, destination, start_date, end_date, cover_color, created_at`,
		userID, trip.Title, trip.Destination, trip.StartDate, trip.EndDate, trip.CoverColor).Scan(&trip.ID, &trip.OwnerID, &trip.Title, &trip.Destination, &trip.StartDate, &trip.EndDate, &trip.CoverColor, &trip.CreatedAt)
	if err != nil {
		return Trip{}, err
	}
	if _, err = tx.Exec(ctx, `insert into trip_members (trip_id, user_id, role) values ($1, $2, 'owner')`, trip.ID, userID); err != nil {
		return Trip{}, err
	}
	return trip, tx.Commit(ctx)
}

func (s *Store) TripDetail(ctx context.Context, tripID, userID string) (Trip, []Plan, []ChecklistItem, []Document, []RouteOption, []Member, error) {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return Trip{}, nil, nil, nil, nil, nil, err
	}
	if !allowed {
		return Trip{}, nil, nil, nil, nil, nil, ErrNotFound
	}
	var trip Trip
	err = s.DB.QueryRow(ctx, `select id, owner_id, title, destination, start_date, end_date, cover_color, created_at from trips where id=$1`, tripID).Scan(&trip.ID, &trip.OwnerID, &trip.Title, &trip.Destination, &trip.StartDate, &trip.EndDate, &trip.CoverColor, &trip.CreatedAt)
	if err != nil {
		return Trip{}, nil, nil, nil, nil, nil, err
	}
	plans, err := s.listPlans(ctx, tripID)
	if err != nil {
		return Trip{}, nil, nil, nil, nil, nil, err
	}
	checklist, err := s.listChecklist(ctx, tripID)
	if err != nil {
		return Trip{}, nil, nil, nil, nil, nil, err
	}
	docs, err := s.listDocuments(ctx, tripID)
	if err != nil {
		return Trip{}, nil, nil, nil, nil, nil, err
	}
	routes, err := s.listRouteOptions(ctx, tripID)
	if err != nil {
		return Trip{}, nil, nil, nil, nil, nil, err
	}
	members, err := s.listMembers(ctx, tripID)
	if err != nil {
		return Trip{}, nil, nil, nil, nil, nil, err
	}
	return trip, plans, checklist, docs, routes, members, nil
}

func (s *Store) UpdateTrip(ctx context.Context, tripID, userID string, trip Trip) (Trip, error) {
	owner, err := s.IsOwner(ctx, tripID, userID)
	if err != nil {
		return Trip{}, err
	}
	if !owner {
		return Trip{}, ErrForbidden
	}
	err = s.DB.QueryRow(ctx, `update trips set title=$3,destination=$4,start_date=$5,end_date=$6,cover_color=$7 where id=$1 and owner_id=$2
    returning id, owner_id, title, destination, start_date, end_date, cover_color, created_at`, tripID, userID, trip.Title, trip.Destination, trip.StartDate, trip.EndDate, trip.CoverColor).
		Scan(&trip.ID, &trip.OwnerID, &trip.Title, &trip.Destination, &trip.StartDate, &trip.EndDate, &trip.CoverColor, &trip.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Trip{}, ErrNotFound
	}
	return trip, err
}

func (s *Store) DeleteTrip(ctx context.Context, tripID, userID string) error {
	tag, err := s.DB.Exec(ctx, `delete from trips where id=$1 and owner_id=$2`, tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddMemberByEmail(ctx context.Context, tripID, ownerID, email string) (Member, error) {
	owner, err := s.IsOwner(ctx, tripID, ownerID)
	if err != nil {
		return Member{}, err
	}
	if !owner {
		return Member{}, ErrForbidden
	}
	var member Member
	err = s.DB.QueryRow(ctx, `with invited as (
      insert into trip_members (trip_id, user_id, role)
      select $1, id, 'member' from profiles where lower(email) = lower($2)
      on conflict (trip_id, user_id) do nothing returning user_id, role
    ) select p.id, p.email, p.display_name, coalesce(p.avatar_url, ''), i.role from invited i join profiles p on p.id=i.user_id`, tripID, email).
		Scan(&member.ID, &member.Email, &member.DisplayName, &member.AvatarURL, &member.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	return member, err
}

func (s *Store) DeleteMember(ctx context.Context, tripID, targetUserID, actorID string) error {
	owner, err := s.IsOwner(ctx, tripID, actorID)
	if err != nil {
		return err
	}
	if !owner && actorID != targetUserID {
		return ErrForbidden
	}
	tag, err := s.DB.Exec(ctx, `delete from trip_members where trip_id=$1 and user_id=$2 and role <> 'owner'`, tripID, targetUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreatePlan(ctx context.Context, userID string, plan Plan) (Plan, error) {
	allowed, err := s.CanAccessTrip(ctx, plan.TripID, userID)
	if err != nil {
		return Plan{}, err
	}
	if !allowed {
		return Plan{}, ErrForbidden
	}
	plan.CreatedBy = userID
	err = s.DB.QueryRow(ctx, `insert into plan_items (trip_id,created_by,kind,title,starts_at,ends_at,location,confirmation_code,notes,time_zone)
	    values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) returning id,trip_id,created_by,kind,title,starts_at,ends_at,location,confirmation_code,notes,time_zone`,
		plan.TripID, plan.CreatedBy, plan.Kind, plan.Title, plan.StartsAt, plan.EndsAt, plan.Location, plan.ConfirmationCode, plan.Notes, defaultTimeZone(plan.TimeZone)).
		Scan(&plan.ID, &plan.TripID, &plan.CreatedBy, &plan.Kind, &plan.Title, &plan.StartsAt, &plan.EndsAt, &plan.Location, &plan.ConfirmationCode, &plan.Notes, &plan.TimeZone)
	return plan, err
}

func (s *Store) UpdatePlan(ctx context.Context, tripID, planID, userID string, plan Plan) (Plan, error) {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return Plan{}, err
	}
	if !allowed {
		return Plan{}, ErrForbidden
	}
	err = s.DB.QueryRow(ctx, `update plan_items set kind=$4,title=$5,starts_at=$6,ends_at=$7,location=$8,confirmation_code=$9,notes=$10,time_zone=$11
	    where id=$1 and trip_id=$2 and created_by=$3 returning id,trip_id,created_by,kind,title,starts_at,ends_at,location,confirmation_code,notes,time_zone`,
		planID, tripID, userID, plan.Kind, plan.Title, plan.StartsAt, plan.EndsAt, plan.Location, plan.ConfirmationCode, plan.Notes, defaultTimeZone(plan.TimeZone)).
		Scan(&plan.ID, &plan.TripID, &plan.CreatedBy, &plan.Kind, &plan.Title, &plan.StartsAt, &plan.EndsAt, &plan.Location, &plan.ConfirmationCode, &plan.Notes, &plan.TimeZone)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	return plan, err
}

func (s *Store) DeletePlan(ctx context.Context, tripID, planID, userID string) error {
	tag, err := s.DB.Exec(ctx, `delete from plan_items where id=$1 and trip_id=$2 and created_by=$3`, planID, tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateChecklist(ctx context.Context, userID string, item ChecklistItem) (ChecklistItem, error) {
	allowed, err := s.CanAccessTrip(ctx, item.TripID, userID)
	if err != nil {
		return ChecklistItem{}, err
	}
	if !allowed {
		return ChecklistItem{}, ErrForbidden
	}
	item.CreatedBy = userID
	err = s.DB.QueryRow(ctx, `insert into checklist_items (trip_id,created_by,title,is_complete,sort_order) values ($1,$2,$3,$4,$5) returning id,trip_id,created_by,title,is_complete,sort_order`, item.TripID, item.CreatedBy, item.Title, item.IsComplete, item.SortOrder).Scan(&item.ID, &item.TripID, &item.CreatedBy, &item.Title, &item.IsComplete, &item.SortOrder)
	return item, err
}

func (s *Store) UpdateChecklist(ctx context.Context, tripID, itemID, userID string, item ChecklistItem) (ChecklistItem, error) {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return ChecklistItem{}, err
	}
	if !allowed {
		return ChecklistItem{}, ErrForbidden
	}
	err = s.DB.QueryRow(ctx, `update checklist_items set title=$4,is_complete=$5,sort_order=$6 where id=$1 and trip_id=$2 and created_by=$3 returning id,trip_id,created_by,title,is_complete,sort_order`, itemID, tripID, userID, item.Title, item.IsComplete, item.SortOrder).Scan(&item.ID, &item.TripID, &item.CreatedBy, &item.Title, &item.IsComplete, &item.SortOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChecklistItem{}, ErrNotFound
	}
	return item, err
}

func (s *Store) DeleteChecklist(ctx context.Context, tripID, itemID, userID string) error {
	tag, err := s.DB.Exec(ctx, `delete from checklist_items where id=$1 and trip_id=$2 and created_by=$3`, itemID, tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateDocument(ctx context.Context, userID string, doc Document) (Document, error) {
	allowed, err := s.CanAccessTrip(ctx, doc.TripID, userID)
	if err != nil {
		return Document{}, err
	}
	if !allowed {
		return Document{}, ErrForbidden
	}
	doc.UploadedBy = userID
	err = s.DB.QueryRow(ctx, `insert into documents (trip_id,uploaded_by,name,storage_path,content_type,size_bytes) values ($1,$2,$3,$4,$5,$6) returning id,trip_id,uploaded_by,name,storage_path,content_type,size_bytes,created_at`, doc.TripID, doc.UploadedBy, doc.Name, doc.StoragePath, doc.ContentType, doc.SizeBytes).Scan(&doc.ID, &doc.TripID, &doc.UploadedBy, &doc.Name, &doc.StoragePath, &doc.ContentType, &doc.SizeBytes, &doc.CreatedAt)
	return doc, err
}

func (s *Store) DeleteDocument(ctx context.Context, tripID, docID, userID string) error {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	tag, err := s.DB.Exec(ctx, `delete from documents where id=$1 and trip_id=$2`, docID, tripID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateRouteOption(ctx context.Context, userID string, option RouteOption) (RouteOption, error) {
	allowed, err := s.CanAccessTrip(ctx, option.TripID, userID)
	if err != nil {
		return RouteOption{}, err
	}
	if !allowed {
		return RouteOption{}, ErrForbidden
	}
	option.CreatedBy = userID
	err = s.DB.QueryRow(ctx, `insert into route_options (trip_id,created_by,title,route_type,origin,destination,departs_at,arrives_at,duration_minutes,transfers,price_amount,currency,booking_url,notes,status)
    values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
    returning id,trip_id,created_by,title,route_type,origin,destination,departs_at,arrives_at,duration_minutes,transfers,price_amount,currency,booking_url,notes,status,created_at`,
		option.TripID, option.CreatedBy, option.Title, option.RouteType, option.Origin, option.Destination, option.DepartsAt, option.ArrivesAt, option.DurationMinutes, option.Transfers, option.PriceAmount, option.Currency, option.BookingURL, option.Notes, option.Status).
		Scan(&option.ID, &option.TripID, &option.CreatedBy, &option.Title, &option.RouteType, &option.Origin, &option.Destination, &option.DepartsAt, &option.ArrivesAt, &option.DurationMinutes, &option.Transfers, &option.PriceAmount, &option.Currency, &option.BookingURL, &option.Notes, &option.Status, &option.CreatedAt)
	return option, err
}

func (s *Store) UpdateRouteOption(ctx context.Context, tripID, optionID, userID string, option RouteOption) (RouteOption, error) {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return RouteOption{}, err
	}
	if !allowed {
		return RouteOption{}, ErrForbidden
	}
	err = s.DB.QueryRow(ctx, `update route_options set title=$4,route_type=$5,origin=$6,destination=$7,departs_at=$8,arrives_at=$9,duration_minutes=$10,transfers=$11,price_amount=$12,currency=$13,booking_url=$14,notes=$15,status=$16
    where id=$1 and trip_id=$2 and created_by=$3
    returning id,trip_id,created_by,title,route_type,origin,destination,departs_at,arrives_at,duration_minutes,transfers,price_amount,currency,booking_url,notes,status,created_at`,
		optionID, tripID, userID, option.Title, option.RouteType, option.Origin, option.Destination, option.DepartsAt, option.ArrivesAt, option.DurationMinutes, option.Transfers, option.PriceAmount, option.Currency, option.BookingURL, option.Notes, option.Status).
		Scan(&option.ID, &option.TripID, &option.CreatedBy, &option.Title, &option.RouteType, &option.Origin, &option.Destination, &option.DepartsAt, &option.ArrivesAt, &option.DurationMinutes, &option.Transfers, &option.PriceAmount, &option.Currency, &option.BookingURL, &option.Notes, &option.Status, &option.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RouteOption{}, ErrNotFound
	}
	return option, err
}

func (s *Store) DeleteRouteOption(ctx context.Context, tripID, optionID, userID string) error {
	tag, err := s.DB.Exec(ctx, `delete from route_options where id=$1 and trip_id=$2 and created_by=$3`, optionID, tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) listPlans(ctx context.Context, tripID string) ([]Plan, error) {
	rows, err := s.DB.Query(ctx, `select id,trip_id,created_by,kind,title,starts_at,ends_at,location,confirmation_code,notes,time_zone from plan_items where trip_id=$1 and deleted_at is null order by starts_at`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[Plan])
}
func (s *Store) listChecklist(ctx context.Context, tripID string) ([]ChecklistItem, error) {
	rows, err := s.DB.Query(ctx, `select id,trip_id,created_by,title,is_complete,sort_order from checklist_items where trip_id=$1 order by sort_order,created_at`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[ChecklistItem])
}
func (s *Store) listDocuments(ctx context.Context, tripID string) ([]Document, error) {
	rows, err := s.DB.Query(ctx, `select id,trip_id,uploaded_by,name,storage_path,content_type,size_bytes,created_at from documents where trip_id=$1 order by created_at desc`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[Document])
}
func (s *Store) listRouteOptions(ctx context.Context, tripID string) ([]RouteOption, error) {
	rows, err := s.DB.Query(ctx, `select id,trip_id,created_by,title,route_type,origin,destination,departs_at,arrives_at,duration_minutes,transfers,price_amount,currency,booking_url,notes,status,created_at from route_options where trip_id=$1 order by status='shortlisted' desc, created_at desc`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[RouteOption])
}

const listMembersQuery = `select p.id,p.email,p.display_name,coalesce(p.avatar_url,'') as avatar_url,tm.role from trip_members tm join profiles p on p.id=tm.user_id where tm.trip_id=$1 order by tm.role desc,p.display_name`

func (s *Store) listMembers(ctx context.Context, tripID string) ([]Member, error) {
	rows, err := s.DB.Query(ctx, listMembersQuery, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[Member])
}

func ValidateKind(kind string) error {
	switch kind {
	case "flight", "stay", "activity", "transport", "food", "other":
		return nil
	}
	return fmt.Errorf("invalid plan kind")
}

func defaultTimeZone(value string) string {
	if value == "" {
		return "UTC"
	}
	return value
}

func ValidateRouteType(routeType string) error {
	switch routeType {
	case "direct_flight", "flight_train", "train", "bus", "other":
		return nil
	}
	return fmt.Errorf("invalid route type")
}

func ValidateRouteStatus(status string) error {
	switch status {
	case "considering", "shortlisted", "booked", "dismissed":
		return nil
	}
	return fmt.Errorf("invalid route status")
}
