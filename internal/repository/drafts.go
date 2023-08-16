package repository

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
)

type DraftRepository struct {
	db *sql.DB
}

type Draft struct {
	Id         string       `json:"id"`
	UserId     int64        `json:"-"`
	MessageUid string       `json:"messageUid"`
	ParentUid  *string      `json:"parentUid"`
	ThreadUid  string       `json:"threadUid"`
	Unread     bool         `json:"unread"`
	Starred    bool         `json:"starred"`
	Payload    *MessagePart `json:"payload"`
	LabelIds   *string      `json:"labelIds"`
	CreatedAt  Timestamp    `json:"createdAt"`
	ModifiedAt *Timestamp   `json:"modifiedAt"`
	TimelineId int64        `json:"-"`
	HistoryId  int64        `json:"-"`
	LastStmt   int          `json:"-"`
	DeviceId   *string      `json:"-"`
}

type DraftDeleted struct {
	Id        string  `json:"id"`
	UserId    int64   `json:"-"`
	HistoryId int64   `json:"-"`
	DeviceId  *string `json:"-"`
}

type DraftList struct {
	History int64    `json:"lastHistoryId"`
	Drafts  []*Draft `json:"drafts"`
}

type DraftSync struct {
	History        int64           `json:"lastHistoryId"`
	DraftsInserted []*Draft        `json:"inserted"`
	DraftsUpdated  []*Draft        `json:"updated"`
	DraftsTrashed  []*Draft        `json:"trashed"`
	DraftsDeleted  []*DraftDeleted `json:"deleted"`
}

func (d *Draft) Scan() []interface{} {
	s := reflect.ValueOf(d).Elem()
	numCols := s.NumField()
	columns := make([]interface{}, numCols)
	for i := 0; i < numCols; i++ {
		field := s.Field(i)
		columns[i] = field.Addr().Interface()
	}
	return columns
}

func (c *DraftDeleted) Scan() []interface{} {
	s := reflect.ValueOf(c).Elem()
	numCols := s.NumField()
	columns := make([]interface{}, numCols)
	for i := 0; i < numCols; i++ {
		field := s.Field(i)
		columns[i] = field.Addr().Interface()
	}
	return columns
}

func (r *DraftRepository) Create(user *User, draft *Draft) (*Draft, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		INSERT
			INTO "Draft" ("userId",
				 "deviceId",
				 "messageUid",
				 "threadUid",
				 "unread",
				 "payload")
			VALUES ($1,
					$2,
					$3,
					$4,
					$5,
					$6)
			RETURNING * ;`

	prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

	// from := user.FullnameAndAddress()
	messageUid := uuid.NewString()
	threadUid := uuid.NewString()
	unread := false

	args := []interface{}{user.Id,
		prefixedDeviceId,
		messageUid,
		threadUid,
		unread,
		draft.Payload}

	err := r.db.QueryRowContext(ctx, query, args...).Scan(draft.Scan()...)
	if err != nil {
		return nil, err
	}

	return draft, nil
}

func (r *DraftRepository) List(user *User) (*DraftList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	query := `
		SELECT *
			FROM "Draft"
			WHERE "userId" = $1 AND
			"lastStmt" < 2
			ORDER BY "createdAt" DESC;`

	args := []interface{}{user.Id}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	draftList := &DraftList{
		Drafts: []*Draft{},
	}

	for rows.Next() {
		var draft Draft

		err := rows.Scan(draft.Scan()...)

		if err != nil {
			return nil, err
		}

		draftList.Drafts = append(draftList.Drafts, &draft)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// history
	query = `
	SELECT "lastHistoryId"
	   FROM "DraftHistorySeq"
	   WHERE "userId" = $1 ;`

	args = []interface{}{user.Id}

	err = tx.QueryRowContext(ctx, query, args...).Scan(&draftList.History)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return draftList, nil
}

func (r *DraftRepository) Sync(user *User, history *History) (*DraftSync, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// inserted rows
	query := `
		SELECT *
			FROM "Draft"
			WHERE "userId" = $1 AND
				"lastStmt" = 0 AND
				("deviceId" <> $2 OR "deviceId" IS NULL) AND
				"historyId" > $3
			ORDER BY "createdAt" DESC;`

	args := []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	draftSync := &DraftSync{
		DraftsInserted: []*Draft{},
		DraftsUpdated:  []*Draft{},
		DraftsTrashed:  []*Draft{},
		DraftsDeleted:  []*DraftDeleted{},
	}

	for rows.Next() {
		var draft Draft

		err := rows.Scan(draft.Scan()...)

		if err != nil {
			return nil, err
		}

		draftSync.DraftsInserted = append(draftSync.DraftsInserted, &draft)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// updated rows
	query = `
		SELECT *
			FROM "Draft"
			WHERE "userId" = $1 AND
				"lastStmt" = 1 AND
				("deviceId" <> $2 OR "deviceId" IS NULL) AND
				"historyId" > $3
			ORDER BY "createdAt" DESC;`

	args = []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var draft Draft

		err := rows.Scan(draft.Scan()...)

		if err != nil {
			return nil, err
		}

		draftSync.DraftsUpdated = append(draftSync.DraftsUpdated, &draft)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// trashed rows
	query = `
		SELECT *
			FROM "Draft"
			WHERE "userId" = $1 AND
				"lastStmt" = 2 AND
				("deviceId" <> $2 OR "deviceId" IS NULL) AND
				"historyId" > $3
			ORDER BY "createdAt" DESC;`

	args = []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var draft Draft

		err := rows.Scan(draft.Scan()...)

		if err != nil {
			return nil, err
		}

		draftSync.DraftsTrashed = append(draftSync.DraftsTrashed, &draft)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// deleted rows
	query = `
		SELECT *
			FROM "DraftDeleted"
			WHERE "userId" = $1 AND
			("deviceId" <> $2 OR "deviceId" IS NULL) AND
			"historyId" > $3;`

	args = []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var draftDeleted DraftDeleted

		err := rows.Scan(draftDeleted.Scan()...)

		if err != nil {
			return nil, err
		}

		draftSync.DraftsDeleted = append(draftSync.DraftsDeleted, &draftDeleted)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// history
	query = `
	SELECT "LastHistoryId"
	   FROM "DraftHistorySeq"
	   WHERE "userId" = $1 ;`

	args = []interface{}{user.Id}

	err = tx.QueryRowContext(ctx, query, args...).Scan(&draftSync.History)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return draftSync, nil
}

func (r *DraftRepository) Update(user *User, draft *Draft) (*Draft, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		UPDATE "Draft"
			SET "payload" = $1,
				"deviceId" = $2
			WHERE "userId" = $3 AND
			      "id" = $4 AND
				  "lastStmt" <> 2
			RETURNING * ;`

	prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

	args := []interface{}{draft.Payload, prefixedDeviceId, user.Id, draft.Id}

	err := r.db.QueryRowContext(ctx, query, args...).Scan(draft.Scan()...)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrDraftNotFound
		default:
			return nil, err
		}
	}

	return draft, nil
}

func (r *DraftRepository) Trash(user *User, idList string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(idList) > 0 {
		query := `
		UPDATE Draft
			SET "lastStmt" = 2,
			"deviceId" = $1
			WHERE "userId" = $2 AND
			"id" IN (SELECT value FROM json_each($3));`

		prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

		args := []interface{}{prefixedDeviceId, user.Id, idList}

		_, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *DraftRepository) Untrash(user *User, idList string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(idList) > 0 {
		query := `
		UPDATE "Draft"
			SET "lastStmt" = 0,
			"deviceId" = $1
			WHERE "userId" = $2 AND
			"id" IN (SELECT value FROM json_each($3));`

		prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

		args := []interface{}{prefixedDeviceId, user.Id, idList}

		_, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r DraftRepository) Delete(user *User, idList string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(idList) > 0 {
		query := `
		DELETE
			FROM "Draft"
			WHERE "userId" = $1 AND
			"id" IN (SELECT value FROM json_each($2));`

		args := []interface{}{user.Id, idList}

		_, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
	}

	return nil
}
