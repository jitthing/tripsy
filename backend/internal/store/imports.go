package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrConflict = errors.New("conflict")

// ImportAddress routes forwarded mail. TripID nil means a personal inbox address:
// imports land in OwnerID's inbox with no trip until they assign one on review.
type ImportAddress struct {
	TripID  *string `json:"tripId,omitempty"`
	Token   string  `json:"token"`
	OwnerID string  `json:"-"`
}

type ReservationImport struct {
	ID              string     `json:"id"`
	TripID          *string    `json:"tripId,omitempty"`
	OwnerID         string     `json:"ownerId"`
	ExternalEmailID string     `json:"externalEmailId"`
	Sender          string     `json:"sender"`
	Subject         string     `json:"subject"`
	ReceivedAt      *time.Time `json:"receivedAt,omitempty"`
	RawStoragePath  string     `json:"rawStoragePath"`
	TextStoragePath string     `json:"textStoragePath"`
	Status          string     `json:"status"`
	ErrorMessage    string     `json:"errorMessage"`
	DuplicateOfID   *string    `json:"duplicateOfImportId,omitempty"`
	UsedLLM         bool       `json:"usedLlm"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExtractionError string     `json:"extractionError"`
}

type ImportAttachment struct {
	ID          string  `json:"id"`
	ImportID    string  `json:"importId"`
	Filename    string  `json:"filename"`
	ContentType string  `json:"contentType"`
	SizeBytes   int64   `json:"sizeBytes"`
	StoragePath string  `json:"storagePath"`
	DocumentID  *string `json:"documentId,omitempty"`
}

type ReservationDraft struct {
	ID               string     `json:"id"`
	ImportID         string     `json:"importId"`
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	Supplier         string     `json:"supplier"`
	ConfirmationCode string     `json:"confirmationCode"`
	StartsAt         *time.Time `json:"startsAt,omitempty"`
	EndsAt           *time.Time `json:"endsAt,omitempty"`
	TimeZone         string     `json:"timeZone"`
	Location         string     `json:"location"`
	Notes            string     `json:"notes"`
	Confidence       float64    `json:"confidence"`
	Status           string     `json:"status"`
	PlanID           *string    `json:"planId,omitempty"`
}

func (s *Store) EnsureImportAddress(ctx context.Context, tripID, userID, token string) (ImportAddress, error) {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return ImportAddress{}, err
	}
	if !allowed {
		return ImportAddress{}, ErrForbidden
	}
	var address ImportAddress
	err = s.DB.QueryRow(ctx, `insert into import_addresses (trip_id, token, created_by, owner_id) values ($1,$2,$3,$3)
    on conflict (trip_id) where trip_id is not null do update set trip_id = excluded.trip_id
    returning trip_id, token, owner_id`, tripID, token, userID).Scan(&address.TripID, &address.Token, &address.OwnerID)
	return address, err
}

// EnsureUserImportAddress returns the caller's personal forwarding address,
// creating it on first use. One per owner, enforced by a partial unique index.
func (s *Store) EnsureUserImportAddress(ctx context.Context, userID, token string) (ImportAddress, error) {
	var address ImportAddress
	err := s.DB.QueryRow(ctx, `insert into import_addresses (trip_id, token, created_by, owner_id) values (null,$1,$2,$2)
    on conflict (owner_id) where trip_id is null do update set owner_id = excluded.owner_id
    returning trip_id, token, owner_id`, token, userID).Scan(&address.TripID, &address.Token, &address.OwnerID)
	return address, err
}

func (s *Store) ImportAddressForToken(ctx context.Context, token string) (ImportAddress, error) {
	var address ImportAddress
	err := s.DB.QueryRow(ctx, `select trip_id, token, owner_id from import_addresses where token=$1`, token).Scan(&address.TripID, &address.Token, &address.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportAddress{}, ErrNotFound
	}
	return address, err
}

func (s *Store) CreateInboundImport(ctx context.Context, tripID *string, ownerID, externalEmailID, webhookID, sender, subject string, receivedAt *time.Time) (ReservationImport, bool, error) {
	var item ReservationImport
	err := s.DB.QueryRow(ctx, `insert into reservation_imports (trip_id, owner_id, external_email_id, webhook_id, sender, subject, received_at)
    values ($1,$2,$3,$4,$5,$6,$7)
    on conflict (external_email_id) do update set external_email_id = excluded.external_email_id
    returning id`, tripID, ownerID, externalEmailID, webhookID, sender, subject, receivedAt).Scan(&item.ID)
	err = s.DB.QueryRow(ctx, `select id,trip_id,owner_id,external_email_id,sender,subject,received_at,raw_storage_path,text_storage_path,status,error_message,duplicate_of_import_id,used_llm,created_at,extraction_error from reservation_imports where external_email_id=$1`, externalEmailID).
		Scan(&item.ID, &item.TripID, &item.OwnerID, &item.ExternalEmailID, &item.Sender, &item.Subject, &item.ReceivedAt, &item.RawStoragePath, &item.TextStoragePath, &item.Status, &item.ErrorMessage, &item.DuplicateOfID, &item.UsedLLM, &item.CreatedAt, &item.ExtractionError)
	if err != nil {
		return ReservationImport{}, false, err
	}
	return item, item.Status != "queued", nil
}

func (s *Store) MarkPotentialDuplicate(ctx context.Context, importID, ownerID string, drafts []ReservationDraft) error {
	for _, draft := range drafts {
		code := strings.TrimSpace(strings.ToLower(draft.ConfirmationCode))
		if code == "" {
			continue
		}
		var duplicateID string
		err := s.DB.QueryRow(ctx, `select i.id from reservation_drafts d join reservation_imports i on i.id=d.import_id where lower(trim(d.confirmation_code))=$1 and i.id<>$2 and i.owner_id=$3 and d.status<>'discarded' order by i.created_at desc limit 1`, code, importID, ownerID).Scan(&duplicateID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		_, err = s.DB.Exec(ctx, `update reservation_imports set duplicate_of_import_id=$2 where id=$1`, importID, duplicateID)
		return err
	}
	return nil
}

func (s *Store) RetryImport(ctx context.Context, importID, userID string) error {
	result, err := s.DB.Exec(ctx, `update reservation_imports set status='queued',error_message='',extraction_error='',processed_at=null where id=$1 and owner_id=$2 and status='failed'`, importID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ClaimQueuedImport(ctx context.Context) (ReservationImport, error) {
	var item ReservationImport
	err := s.DB.QueryRow(ctx, `with next as (
      select id from reservation_imports where status='queued' order by created_at for update skip locked limit 1
    ) update reservation_imports i set status='processing' from next where i.id=next.id
      returning i.id,i.trip_id,i.owner_id,i.external_email_id,i.sender,i.subject,i.received_at,i.raw_storage_path,i.text_storage_path,i.status,i.error_message,i.duplicate_of_import_id,i.used_llm,i.created_at,i.extraction_error`).
		Scan(&item.ID, &item.TripID, &item.OwnerID, &item.ExternalEmailID, &item.Sender, &item.Subject, &item.ReceivedAt, &item.RawStoragePath, &item.TextStoragePath, &item.Status, &item.ErrorMessage, &item.DuplicateOfID, &item.UsedLLM, &item.CreatedAt, &item.ExtractionError)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationImport{}, ErrNotFound
	}
	return item, err
}

func (s *Store) CompleteImport(ctx context.Context, importID, rawPath, textPath string, usedLLM bool, extractionError string, drafts []ReservationDraft, attachments []ImportAttachment) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `update reservation_imports set status='review',raw_storage_path=$2,text_storage_path=$3,used_llm=$4,extraction_error=$5,error_message='',processed_at=now() where id=$1`, importID, rawPath, textPath, usedLLM, extractionError); err != nil {
		return err
	}
	for _, attachment := range attachments {
		if _, err = tx.Exec(ctx, `insert into reservation_import_attachments (import_id,filename,content_type,size_bytes,storage_path) values ($1,$2,$3,$4,$5)`, importID, attachment.Filename, attachment.ContentType, attachment.SizeBytes, attachment.StoragePath); err != nil {
			return err
		}
	}
	for _, draft := range drafts {
		if _, err = tx.Exec(ctx, `insert into reservation_drafts (import_id,kind,title,supplier,confirmation_code,starts_at,ends_at,time_zone,location,notes,confidence) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, importID, draft.Kind, draft.Title, draft.Supplier, draft.ConfirmationCode, draft.StartsAt, draft.EndsAt, draft.TimeZone, draft.Location, draft.Notes, draft.Confidence); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) FailImport(ctx context.Context, importID string, cause error) error {
	_, err := s.DB.Exec(ctx, `update reservation_imports set status='failed',error_message=$2,processed_at=now() where id=$1`, importID, cause.Error())
	return err
}

func (s *Store) ListImports(ctx context.Context, tripID, userID string) ([]ReservationImport, error) {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	rows, err := s.DB.Query(ctx, `select id,trip_id,owner_id,external_email_id,sender,subject,received_at,raw_storage_path,text_storage_path,status,error_message,duplicate_of_import_id,used_llm,created_at,extraction_error from reservation_imports where trip_id=$1 order by created_at desc`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ReservationImport])
}

func (s *Store) ListInbox(ctx context.Context, userID string) ([]ReservationImport, error) {
	rows, err := s.DB.Query(ctx, `select id,trip_id,owner_id,external_email_id,sender,subject,received_at,raw_storage_path,text_storage_path,status,error_message,duplicate_of_import_id,used_llm,created_at,extraction_error from reservation_imports where owner_id=$1 order by created_at desc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ReservationImport])
}

func (s *Store) AssignImport(ctx context.Context, importID, tripID, userID string) error {
	allowed, err := s.CanAccessTrip(ctx, tripID, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	result, err := s.DB.Exec(ctx, `update reservation_imports set trip_id=$2 where id=$1 and owner_id=$3 and status not in ('approved','discarded')`, importID, tripID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ImportDetail(ctx context.Context, importID, userID string) (ReservationImport, []ReservationDraft, []ImportAttachment, error) {
	var item ReservationImport
	err := s.DB.QueryRow(ctx, `select i.id,i.trip_id,i.owner_id,i.external_email_id,i.sender,i.subject,i.received_at,i.raw_storage_path,i.text_storage_path,i.status,i.error_message,i.duplicate_of_import_id,i.used_llm,i.created_at,i.extraction_error from reservation_imports i where i.id=$1 and (i.owner_id=$2 or exists (select 1 from trips t where t.id=i.trip_id and (t.owner_id=$2 or exists (select 1 from trip_members m where m.trip_id=t.id and m.user_id=$2))))`, importID, userID).Scan(&item.ID, &item.TripID, &item.OwnerID, &item.ExternalEmailID, &item.Sender, &item.Subject, &item.ReceivedAt, &item.RawStoragePath, &item.TextStoragePath, &item.Status, &item.ErrorMessage, &item.DuplicateOfID, &item.UsedLLM, &item.CreatedAt, &item.ExtractionError)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReservationImport{}, nil, nil, ErrNotFound
	}
	if err != nil {
		return ReservationImport{}, nil, nil, err
	}
	draftRows, err := s.DB.Query(ctx, `select id,import_id,kind,title,supplier,confirmation_code,starts_at,ends_at,time_zone,location,notes,confidence,status,plan_id from reservation_drafts where import_id=$1 order by created_at`, importID)
	if err != nil {
		return ReservationImport{}, nil, nil, err
	}
	defer draftRows.Close()
	drafts, err := pgx.CollectRows(draftRows, pgx.RowToStructByPos[ReservationDraft])
	if err != nil {
		return ReservationImport{}, nil, nil, err
	}
	attachmentRows, err := s.DB.Query(ctx, `select id,import_id,filename,content_type,size_bytes,storage_path,document_id from reservation_import_attachments where import_id=$1`, importID)
	if err != nil {
		return ReservationImport{}, nil, nil, err
	}
	defer attachmentRows.Close()
	attachments, err := pgx.CollectRows(attachmentRows, pgx.RowToStructByPos[ImportAttachment])
	return item, drafts, attachments, err
}

func (s *Store) ApproveDraft(ctx context.Context, draftID, userID string, plan Plan) (Plan, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback(ctx)
	var tripID *string
	var confirmation, supplier string
	err = tx.QueryRow(ctx, `select i.trip_id,d.confirmation_code,d.supplier from reservation_drafts d join reservation_imports i on i.id=d.import_id where d.id=$1 and d.status='pending'`, draftID).Scan(&tripID, &confirmation, &supplier)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, err
	}
	if tripID == nil {
		return Plan{}, errors.New("import must be assigned to a trip before approval")
	}
	allowed, err := canAccessTripTx(ctx, tx, *tripID, userID)
	if err != nil {
		return Plan{}, err
	}
	if !allowed {
		return Plan{}, ErrForbidden
	}
	if plan.ConfirmationCode == "" {
		plan.ConfirmationCode = confirmation
	}
	if plan.Notes == "" {
		plan.Notes = supplier
	}
	if plan.StartsAt.IsZero() {
		return Plan{}, errors.New("a start time is required to approve a reservation")
	}
	err = tx.QueryRow(ctx, `insert into plan_items (trip_id,created_by,kind,title,starts_at,ends_at,location,confirmation_code,notes,time_zone) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) returning id,trip_id,created_by,kind,title,starts_at,ends_at,location,confirmation_code,notes,time_zone`, tripID, userID, plan.Kind, plan.Title, plan.StartsAt, plan.EndsAt, plan.Location, plan.ConfirmationCode, plan.Notes, defaultTimeZone(plan.TimeZone)).Scan(&plan.ID, &plan.TripID, &plan.CreatedBy, &plan.Kind, &plan.Title, &plan.StartsAt, &plan.EndsAt, &plan.Location, &plan.ConfirmationCode, &plan.Notes, &plan.TimeZone)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.Exec(ctx, `update reservation_drafts set status='approved',plan_id=$2 where id=$1`, draftID, plan.ID); err != nil {
		return Plan{}, err
	}
	if _, err = tx.Exec(ctx, `update reservation_imports set status='approved' where id=(select import_id from reservation_drafts where id=$1) and not exists (select 1 from reservation_drafts where import_id=(select import_id from reservation_drafts where id=$1) and status='pending')`, draftID); err != nil {
		return Plan{}, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) DiscardDraft(ctx context.Context, draftID, userID string) error {
	var tripID *string
	err := s.DB.QueryRow(ctx, `select i.trip_id from reservation_drafts d join reservation_imports i on i.id=d.import_id where d.id=$1 and d.status='pending'`, draftID).Scan(&tripID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if tripID == nil {
		return errors.New("import must be assigned to a trip before discarding")
	}
	allowed, err := s.CanAccessTrip(ctx, *tripID, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	_, err = s.DB.Exec(ctx, `update reservation_drafts set status='discarded' where id=$1`, draftID)
	return err
}

func canAccessTripTx(ctx context.Context, tx pgx.Tx, tripID, userID string) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `select exists(select 1 from trips t where t.id=$1 and (t.owner_id=$2 or exists(select 1 from trip_members m where m.trip_id=t.id and m.user_id=$2)))`, tripID, userID).Scan(&allowed)
	return allowed, err
}
