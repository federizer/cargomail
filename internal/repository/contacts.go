package repository

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"
)

type ContactRepository struct {
	db *sql.DB
}

type Contact struct {
	Id           string     `json:"id"`
	UserId       int64      `json:"-"`
	EmailAddress *string    `json:"email_address"`
	FirstName    *string    `json:"firstname"`
	LastName     *string    `json:"lastname"`
	CreatedAt    Timestamp  `json:"created_at"`
	ModifiedAt   *Timestamp `json:"modified_at"`
	TimelineId   int64      `json:"-"`
	HistoryId    int64      `json:"-"`
	LastStmt     int        `json:"-"`
	DeviceId     *string    `json:"-"`
}

type ContactDeleted struct {
	Id        string  `json:"id"`
	UserId    int64   `json:"-"`
	HistoryId int64   `json:"-"`
	DeviceId  *string `json:"-"`
}

type ContactList struct {
	History  int64      `json:"last_history_id"`
	Contacts []*Contact `json:"contacts"`
}

type ContactSync struct {
	History          int64             `json:"last_history_id"`
	ContactsInserted []*Contact        `json:"inserted"`
	ContactsUpdated  []*Contact        `json:"updated"`
	ContactsTrashed  []*Contact        `json:"trashed"`
	ContactsDeleted  []*ContactDeleted `json:"deleted"`
}

func (c *Contact) Scan() []interface{} {
	s := reflect.ValueOf(c).Elem()
	numCols := s.NumField()
	columns := make([]interface{}, numCols)
	for i := 0; i < numCols; i++ {
		field := s.Field(i)
		columns[i] = field.Addr().Interface()
	}
	return columns
}

func (c *ContactDeleted) Scan() []interface{} {
	s := reflect.ValueOf(c).Elem()
	numCols := s.NumField()
	columns := make([]interface{}, numCols)
	for i := 0; i < numCols; i++ {
		field := s.Field(i)
		columns[i] = field.Addr().Interface()
	}
	return columns
}

func (r *ContactRepository) Create(user *User, contact *Contact) (*Contact, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		INSERT
			INTO contact (user_id, device_id, email_address, firstname, lastname)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING * ;`

	prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

	args := []interface{}{user.Id, prefixedDeviceId, contact.EmailAddress, contact.FirstName, contact.LastName}

	err := r.db.QueryRowContext(ctx, query, args...).Scan(contact.Scan()...)
	if err != nil {
		switch {
		case err.Error() == `UNIQUE constraint failed: contact.email_address, contact.firstname, contact.lastname`:
			return nil, ErrDuplicateContact
		default:
			return nil, err
		}
	}

	return contact, nil
}

func (r *ContactRepository) List(user *User) (*ContactList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	query := `
		SELECT *
			FROM contact
			WHERE user_id = $1 AND
			last_stmt < 2
			ORDER BY created_at DESC;`

	args := []interface{}{user.Id}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	contactList := &ContactList{
		Contacts: []*Contact{},
	}

	for rows.Next() {
		var contact Contact

		err := rows.Scan(contact.Scan()...)

		if err != nil {
			return nil, err
		}

		contactList.Contacts = append(contactList.Contacts, &contact)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// history
	query = `
	SELECT last_history_id
	   FROM contact_history_seq
	   WHERE user_id = $1 ;`

	args = []interface{}{user.Id}

	err = tx.QueryRowContext(ctx, query, args...).Scan(&contactList.History)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return contactList, nil
}

func (r *ContactRepository) Sync(user *User, history *History) (*ContactSync, error) {
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
			FROM contact
			WHERE user_id = $1 AND
				last_stmt = 0 AND
				(device_id <> $2 OR device_id IS NULL) AND
				history_id > $3
			ORDER BY created_at DESC;`

	args := []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	contactSync := &ContactSync{
		ContactsInserted: []*Contact{},
		ContactsUpdated:  []*Contact{},
		ContactsTrashed:  []*Contact{},
		ContactsDeleted:  []*ContactDeleted{},
	}

	for rows.Next() {
		var contact Contact

		err := rows.Scan(contact.Scan()...)

		if err != nil {
			return nil, err
		}

		contactSync.ContactsInserted = append(contactSync.ContactsInserted, &contact)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// updated rows
	query = `
		SELECT *
			FROM contact
			WHERE user_id = $1 AND
				last_stmt = 1 AND
				(device_id <> $2 OR device_id IS NULL) AND
				history_id > $3
			ORDER BY created_at DESC;`

	args = []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var contact Contact

		err := rows.Scan(contact.Scan()...)

		if err != nil {
			return nil, err
		}

		contactSync.ContactsUpdated = append(contactSync.ContactsUpdated, &contact)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// trashed rows
	query = `
		SELECT *
			FROM contact
			WHERE user_id = $1 AND
				last_stmt = 2 AND
				(device_id <> $2 OR device_id IS NULL) AND
				history_id > $3
			ORDER BY created_at DESC;`

	args = []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var contact Contact

		err := rows.Scan(contact.Scan()...)

		if err != nil {
			return nil, err
		}

		contactSync.ContactsTrashed = append(contactSync.ContactsTrashed, &contact)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// deleted rows
	query = `
		SELECT *
			FROM contact_deleted
			WHERE user_id = $1 AND
			(device_id <> $2 OR device_id IS NULL) AND
			history_id > $3;`

	args = []interface{}{user.Id, user.DeviceId, history.Id}

	rows, err = tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var contactDeleted ContactDeleted

		err := rows.Scan(contactDeleted.Scan()...)

		if err != nil {
			return nil, err
		}

		contactSync.ContactsDeleted = append(contactSync.ContactsDeleted, &contactDeleted)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// history
	query = `
	SELECT last_history_id
	   FROM contact_history_seq
	   WHERE user_id = $1 ;`

	args = []interface{}{user.Id}

	err = tx.QueryRowContext(ctx, query, args...).Scan(&contactSync.History)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return contactSync, nil
}

func (r *ContactRepository) Update(user *User, contact *Contact) (*Contact, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		UPDATE contact
			SET email_address = $1,
			    firstname = $2,
				lastname = $3,
				device_id = $4
			WHERE user_id = $5 AND
			      id = $6 AND
				  last_stmt <> 2
			RETURNING * ;`

	prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

	args := []interface{}{contact.EmailAddress, contact.FirstName, contact.LastName, prefixedDeviceId, user.Id, contact.Id}

	err := r.db.QueryRowContext(ctx, query, args...).Scan(contact.Scan()...)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrContactNotFound
		case err.Error() == `UNIQUE constraint failed: contact.email_address, contact.firstname, contact.lastname`:
			return nil, ErrDuplicateContact
		default:
			return nil, err
		}
	}

	return contact, nil
}

func (r *ContactRepository) Trash(user *User, idList string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(idList) > 0 {
		query := `
		UPDATE contact
			SET last_stmt = 2,
			device_id = $1
			WHERE user_id = $2 AND
			id IN (SELECT value FROM json_each($3));`

		prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

		args := []interface{}{prefixedDeviceId, user.Id, idList}

		_, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ContactRepository) Untrash(user *User, idList string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(idList) > 0 {
		query := `
		UPDATE contact
			SET last_stmt = 0,
			device_id = $1
			WHERE user_id = $2 AND
			id IN (SELECT value FROM json_each($3));`

		prefixedDeviceId := getPrefixedDeviceId(user.DeviceId)

		args := []interface{}{prefixedDeviceId, user.Id, idList}

		_, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r ContactRepository) Delete(user *User, idList string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if len(idList) > 0 {
		query := `
		DELETE
			FROM contact
			WHERE user_id = $1 AND
			id IN (SELECT value FROM json_each($2));`

		args := []interface{}{user.Id, idList}

		_, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
	}

	return nil
}
