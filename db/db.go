package db

import (
	"database/sql"
	"errors"
	"msim/models"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

var ErrNoRows = errors.New("no rows found")

type DB struct {
	conn *sql.DB
}

func New(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_foreign_keys=1&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.init(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) init() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			login TEXT UNIQUE NOT NULL,
			password TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner TEXT NOT NULL,
			contact TEXT NOT NULL,
			nick TEXT NOT NULL,
			UNIQUE(owner, contact)
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			text TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'sent'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_recipient ON messages(recipient, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_owner ON contacts(owner)`,
	}

	for _, query := range queries {
		if _, err := db.conn.Exec(query); err != nil {
			return err
		}
	}

	return nil
}

// User methods
func (db *DB) CreateUser(login, password string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = db.conn.Exec("INSERT INTO users (login, password) VALUES (?, ?)", login, string(hashed))
	return err
}

func (db *DB) AuthenticateUser(login, password string) (bool, error) {
	var hashedPassword string
	err := db.conn.QueryRow("SELECT password FROM users WHERE login = ?", login).Scan(&hashedPassword)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil, nil
}

func (db *DB) UserExists(login string) (bool, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM users WHERE login = ?", login).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Contact methods
func (db *DB) GetContacts(owner string) ([]models.Contact, error) {
	rows, err := db.conn.Query("SELECT id, owner, contact, nick FROM contacts WHERE owner = ?", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []models.Contact
	for rows.Next() {
		var c models.Contact
		if err := rows.Scan(&c.ID, &c.Owner, &c.Contact, &c.Nick); err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}

	return contacts, rows.Err()
}

func (db *DB) AddContact(owner, contact, nick string) error {
	_, err := db.conn.Exec("INSERT INTO contacts (owner, contact, nick) VALUES (?, ?, ?)", owner, contact, nick)
	return err
}

func (db *DB) UpdateContactNick(owner, contact, nick string) error {
	result, err := db.conn.Exec("UPDATE contacts SET nick = ? WHERE owner = ? AND contact = ?", nick, owner, contact)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (db *DB) DeleteContact(owner, contact string) error {
	result, err := db.conn.Exec("DELETE FROM contacts WHERE owner = ? AND contact = ?", owner, contact)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (db *DB) ContactExists(owner, contact string) (bool, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM contacts WHERE owner = ? AND contact = ?", owner, contact).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Message methods
func (db *DB) SaveMessage(sender, recipient, text string, timestamp time.Time) error {
	_, err := db.conn.Exec(
		"INSERT INTO messages (sender, recipient, text, timestamp, status) VALUES (?, ?, ?, ?, ?)",
		sender, recipient, text, timestamp.Format(time.RFC3339), "sent",
	)
	return err
}

func (db *DB) GetMessages(owner, contact string, offset, limit int) ([]models.Message, error) {
	query := `
		SELECT sender, recipient, text, timestamp, status 
		FROM messages 
		WHERE (sender = ? AND recipient = ?) OR (sender = ? AND recipient = ?)
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?
	`

	rows, err := db.conn.Query(query, owner, contact, contact, owner, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var m models.Message
		var timestampStr string
		if err := rows.Scan(&m.Sender, &m.Recipient, &m.Text, &timestampStr, &m.Status); err != nil {
			return nil, err
		}

		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			return nil, err
		}
		m.Timestamp = timestamp

		messages = append(messages, m)
	}

	return messages, rows.Err()
}

func (db *DB) MarkMessageAcknowledged(sender, recipient string, timestamp time.Time) error {
	_, err := db.conn.Exec(
		"UPDATE messages SET status = 'ackn' WHERE sender = ? AND recipient = ? AND timestamp = ?",
		sender, recipient, timestamp.Format(time.RFC3339),
	)
	return err
}

func (db *DB) ClearHistory(owner, contact string) error {
	_, err := db.conn.Exec(
		"DELETE FROM messages WHERE (sender = ? AND recipient = ?) OR (sender = ? AND recipient = ?)",
		owner, contact, contact, owner,
	)
	return err
}

